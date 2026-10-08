package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"
)

const maxMessage = 64 << 20

func appOperation(app string) string {
	return fmt.Sprintf("request_app_access_%x", sha256.Sum256([]byte(app)))
}

func object(raw []byte) (map[string]json.RawMessage, bool) {
	d := json.NewDecoder(bytes.NewReader(raw))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return nil, false
	}
	values := make(map[string]json.RawMessage)
	for d.More() {
		key, err := d.Token()
		name, ok := key.(string)
		if err != nil || !ok {
			return nil, false
		}
		if _, exists := values[name]; exists {
			return nil, false
		}
		var value json.RawMessage
		if d.Decode(&value) != nil {
			return nil, false
		}
		values[name] = value
	}
	if token, err = d.Token(); err != nil || token != json.Delim('}') {
		return nil, false
	}
	if d.Decode(new(json.RawMessage)) != io.EOF {
		return nil, false
	}
	return values, true
}

// A repair of request metadata, never a producer of permission decisions.
func normalize(line []byte) []byte {
	if !bytes.Contains(line, []byte(`"elicitation/create"`)) {
		return line
	}
	message, ok := object(line)
	if !ok || string(message["method"]) != `"elicitation/create"` {
		return line
	}
	params, ok := object(message["params"])
	if !ok {
		return line
	}
	meta, ok := object(params["_meta"])
	if !ok {
		return line
	}
	if _, exists := meta["tool_name"]; exists {
		return line
	}
	if string(meta["codex_approval_kind"]) != `"mcp_tool_call"` || string(meta["connector_id"]) != `"computer-use"` {
		return line
	}
	toolParams, ok := object(meta["tool_params"])
	if !ok || len(toolParams) != 1 {
		return line
	}
	var app, prompt string
	if json.Unmarshal(toolParams["app"], &app) != nil || strings.TrimSpace(app) == "" || app == "computer-audio" {
		return line
	}
	if json.Unmarshal(params["message"], &prompt) != nil || !strings.HasPrefix(prompt, "Allow Codex to use ") || !strings.HasSuffix(prompt, "?") {
		return line
	}
	if mode, exists := params["mode"]; exists && string(mode) != `"form"` {
		return line
	}
	// Keep the SDK's connector, risk, persistence choices, params and request ID.
	// Even a reviewer cache keyed only by operation cannot reuse another app's grant.
	meta["tool_name"], _ = json.Marshal(appOperation(app))
	encoded, err := json.Marshal(meta)
	if err != nil {
		return line
	}
	params["_meta"] = encoded
	encoded, err = json.Marshal(params)
	if err != nil {
		return line
	}
	message["params"] = encoded
	encoded, err = json.Marshal(message)
	if err != nil {
		return line
	}
	if bytes.HasSuffix(line, []byte{'\n'}) {
		encoded = append(encoded, '\n')
	}
	return encoded
}

type messageWriter struct {
	destination io.Writer
	pending     []byte
	fatal       chan<- struct{}
	shapes      []requestShape
	shapeReport string
}

func (w *messageWriter) report() error {
	if w.shapeReport == "" {
		return nil
	}
	data, err := json.MarshalIndent(w.shapes, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(w.shapeReport, data, 0600)
}

type requestShape struct {
	MessageKeys  []string `json:"message_keys"`
	ParamsKeys   []string `json:"params_keys"`
	MetadataKeys []string `json:"metadata_keys"`
	SchemaKeys   []string `json:"schema_keys"`
	Repaired     bool     `json:"repaired"`
}

func keys(object map[string]json.RawMessage) []string {
	result := make([]string, 0, len(object))
	for key := range object {
		result = append(result, key)
	}
	sort.Strings(result)
	return result
}

func shape(line []byte, repaired bool) (requestShape, bool) {
	message, ok := object(line)
	if !ok || string(message["method"]) != `"elicitation/create"` {
		return requestShape{}, false
	}
	params, _ := object(message["params"])
	meta, _ := object(params["_meta"])
	schema, _ := object(params["requestedSchema"])
	return requestShape{keys(message), keys(params), keys(meta), keys(schema), repaired}, true
}

func (w *messageWriter) fail(err error) (int, error) {
	if w.fatal != nil {
		select {
		case w.fatal <- struct{}{}:
		default:
		}
	}
	return 0, err
}

func (w *messageWriter) Write(p []byte) (int, error) {
	total := len(p)
	for len(p) > 0 {
		end := bytes.IndexByte(p, '\n')
		chunk := p
		if end >= 0 {
			chunk = p[:end+1]
		}
		if len(w.pending)+len(chunk) > maxMessage {
			return w.fail(errors.New("MCP message exceeds 64 MiB"))
		}
		w.pending = append(w.pending, chunk...)
		p = p[len(chunk):]
		if end >= 0 {
			normalized := normalize(w.pending)
			if len(w.shapes) < 16 {
				if s, ok := shape(w.pending, !bytes.Equal(normalized, w.pending)); ok {
					w.shapes = append(w.shapes, s)
					if err := w.report(); err != nil {
						return w.fail(errors.New("MCP request-shape report unavailable"))
					}
				}
			}
			if err := writeAll(w.destination, normalized); err != nil {
				return w.fail(err)
			}
			w.pending = w.pending[:0]
		}
	}
	return total, nil
}

func writeAll(w io.Writer, p []byte) error {
	for len(p) > 0 {
		n, err := w.Write(p)
		if err != nil {
			return err
		}
		if n <= 0 || n > len(p) {
			return io.ErrShortWrite
		}
		p = p[n:]
	}
	return nil
}

func run(args []string, input io.Reader, output, diagnostics io.Writer) int {
	var shapeReport string
	if len(args) >= 2 && args[0] == "--shape-report" {
		shapeReport = args[1]
		args = args[2:]
	}
	if len(args) < 2 || args[0] != "--" {
		fmt.Fprintln(diagnostics, "usage: cua_approval_metadata -- <installed node_repl executable> [arguments]")
		return 2
	}
	fatal := make(chan struct{}, 1)
	w := &messageWriter{destination: output, fatal: fatal, shapeReport: shapeReport}
	if shapeReport != "" {
		if err := w.report(); err != nil {
			fmt.Fprintln(diagnostics, "MCP request-shape report could not be written")
			return 1
		}
		defer func() {
			err := w.report()
			if err != nil {
				fmt.Fprintln(diagnostics, "MCP request-shape report could not be written")
			}
		}()
	}
	stdin, stdinWriter, err := os.Pipe()
	if err != nil {
		return 1
	}
	defer stdin.Close()
	defer stdinWriter.Close()
	cmd := exec.Command(args[1], args[2:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, w, diagnostics
	cmd.WaitDelay = 3 * time.Second
	hidden(cmd)
	if err := cmd.Start(); err != nil {
		fmt.Fprintln(diagnostics, "SDK process could not start")
		return 1
	}
	cleanup, err := contain(cmd.Process)
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		fmt.Fprintln(diagnostics, "SDK process could not be contained")
		return 1
	}
	cleanup = sync.OnceFunc(cleanup)
	defer cleanup()
	done := make(chan struct{})
	go func() {
		select {
		case <-fatal:
			cleanup()
		case <-done:
		}
	}()
	// Don't supply MCP initialization until the SDK child is in its owned job.
	go func() {
		_, _ = io.Copy(stdinWriter, input)
		_ = stdinWriter.Close()
	}()
	err = cmd.Wait()
	close(done)
	if len(w.pending) > 0 {
		// A truncated last frame is forwarded as-is, never repaired or accepted.
		if flushErr := writeAll(output, w.pending); flushErr != nil {
			return 1
		}
	}
	if err == nil {
		return 0
	}
	if exit, ok := err.(*exec.ExitError); ok {
		return exit.ExitCode()
	}
	fmt.Fprintln(diagnostics, "SDK stream did not complete")
	return 1
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
