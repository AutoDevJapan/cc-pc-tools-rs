package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

const installedLauncher = "D:/CC/Tools/codex-progress-tui/codex-launch.exe"
const maxProtocolFrame = 2 << 20

var errInputSource = errors.New("input source unavailable")

type protocolFrame struct {
	ID     *int64          `json:"id"`
	Method string          `json:"method"`
	Result json.RawMessage `json:"result"`
	Error  json.RawMessage `json:"error"`
}

type frameResult struct {
	frame protocolFrame
	err   error
}

// The decoder's lifetime ends with its connection, including a caller timeout.
func decodeInputFrames(ctx context.Context, in io.Reader, results chan<- frameResult) {
	defer close(results)
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 4096), maxProtocolFrame)
	for scanner.Scan() {
		var frame protocolFrame
		err := json.Unmarshal(scanner.Bytes(), &frame)
		if err != nil {
			err = errInputSource
		}
		select {
		case results <- frameResult{frame, err}:
		case <-ctx.Done():
			return
		}
		if err != nil {
			return
		}
	}
	if scanner.Err() != nil {
		select {
		case results <- frameResult{err: errInputSource}:
		case <-ctx.Done():
		}
	}
}

type appServerRPC struct {
	ctx    context.Context
	cancel context.CancelFunc
	in     io.WriteCloser
	frames <-chan frameResult
	done   <-chan error
	nextID int64
	closed bool
}

func (r *appServerRPC) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if r.closed || ctx.Err() != nil || r.ctx.Err() != nil {
		return nil, errInputSource
	}
	switch method {
	case "initialize", "thread/read", "thread/turns/list", "thread/items/list", "thread/items/read":
	default:
		return nil, errInputSource
	}
	r.nextID++
	request := struct {
		ID     int64  `json:"id"`
		Method string `json:"method"`
		Params any    `json:"params"`
	}{r.nextID, method, params}
	if json.NewEncoder(r.in).Encode(request) != nil {
		return nil, errInputSource
	}
	for notifications := 0; notifications <= 16; notifications++ {
		select {
		case <-ctx.Done():
			return nil, errInputSource
		case <-r.ctx.Done():
			return nil, errInputSource
		case result, ok := <-r.frames:
			if !ok || result.err != nil {
				return nil, errInputSource
			}
			frame := result.frame
			if frame.ID == nil && frame.Method != "" {
				continue
			}
			if frame.ID == nil || *frame.ID != r.nextID || len(frame.Error) != 0 && string(frame.Error) != "null" || len(frame.Result) == 0 || string(frame.Result) == "null" {
				return nil, errInputSource
			}
			if ctx.Err() != nil || r.ctx.Err() != nil {
				return nil, errInputSource
			}
			return frame.Result, nil
		}
	}
	return nil, errInputSource
}

func (r *appServerRPC) Close() error {
	if r.closed {
		return nil
	}
	r.closed = true
	_ = r.in.Close()
	grace := time.NewTimer(time.Second)
	defer grace.Stop()
	select {
	case err := <-r.done:
		r.cancel()
		return err
	case <-grace.C:
		r.cancel()
	case <-r.ctx.Done():
		r.cancel()
	}
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	select {
	case <-r.done:
	case <-deadline.C:
	}
	return errInputSource
}

func inputSourceEnvironment(environ []string) []string {
	result := make([]string, 0, len(environ))
	for _, entry := range environ {
		key, _, _ := strings.Cut(entry, "=")
		switch strings.ToUpper(key) {
		case "OPENAI_API_KEY", "ANTHROPIC_API_KEY", "AZURE_OPENAI_API_KEY", "CODEX_API_KEY", "OPENROUTER_API_KEY":
			continue
		}
		result = append(result, entry)
	}
	return result
}

func connectInstalledAppServer(ctx context.Context) (inputRPC, error) {
	life, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(life, installedLauncher, "app-server", "--listen", "stdio://")
	hideInputProcess(cmd)
	cmd.Env = inputSourceEnvironment(os.Environ())
	cmd.Stderr = io.Discard
	cmd.WaitDelay = time.Second
	in, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, errInputSource
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		_ = in.Close()
		cancel()
		return nil, errInputSource
	}
	if cmd.Start() != nil {
		_ = in.Close()
		_ = out.Close()
		cancel()
		return nil, errInputSource
	}
	frames := make(chan frameResult)
	done := make(chan error, 1)
	go decodeInputFrames(life, out, frames)
	go func() { done <- cmd.Wait() }()
	rpc := &appServerRPC{ctx: life, cancel: cancel, in: in, frames: frames, done: done}
	params := map[string]any{"clientInfo": map[string]string{"name": "cc_question_input_reference", "version": "1.0.0"}, "capabilities": map[string]bool{"experimentalApi": true}}
	if _, err := rpc.Call(ctx, "initialize", params); err != nil {
		_ = rpc.Close()
		return nil, errInputSource
	}
	if json.NewEncoder(in).Encode(map[string]any{"method": "initialized", "params": map[string]any{}}) != nil {
		_ = rpc.Close()
		return nil, errInputSource
	}
	return rpc, nil
}
