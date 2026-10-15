package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Existing operational session logs only; no customer source, browser profile,
// credentials, recursive discovery, or resolution of aliases from commands.
func allowedSource(path string) bool {
	p := strings.ToLower(filepath.ToSlash(filepath.Clean(path)))
	if !strings.HasSuffix(p, ".jsonl") {
		return false
	}
	for _, root := range []string{"d:/cc/tools/codex-jobs/jobs/", "c:/users/makhmeto/.codex/sessions/"} {
		if strings.HasPrefix(p, root) {
			return true
		}
	}
	return false
}

func noLinkAncestors(path string) bool {
	for p := filepath.Clean(path); ; p = filepath.Dir(p) {
		fi, e := os.Lstat(p)
		if e != nil || fi.Mode()&os.ModeSymlink != 0 {
			return false
		}
		parent := filepath.Dir(p)
		if parent == p {
			return true
		}
	}
}

type Event struct {
	Type      string          `json:"type"`
	Name      string          `json:"name"`
	Call      string          `json:"call_id"`
	Arguments string          `json:"arguments"`
	Input     string          `json:"input"`
	Output    json.RawMessage `json:"output"`
	Payload   json.RawMessage `json:"payload"`
}

func unwrapEvent(b []byte) (Event, bool) {
	var v Event
	if json.Unmarshal(b, &v) != nil {
		return v, false
	}
	if v.Type == "response_item" && len(v.Payload) > 0 {
		var inner Event
		if json.Unmarshal(v.Payload, &inner) != nil {
			return v, false
		}
		v = inner
	}
	switch v.Type {
	case "function_call", "custom_tool_call", "function_call_output", "custom_tool_call_output":
		return v, v.Call != ""
	}
	return v, false
}
func eventCommand(e Event) string {
	if e.Input != "" {
		return e.Input
	}
	var arg struct {
		Cmd     string `json:"cmd"`
		Command string `json:"command"`
	}
	if json.Unmarshal([]byte(e.Arguments), &arg) == nil {
		if arg.Cmd != "" {
			return arg.Cmd
		}
		if arg.Command != "" {
			return arg.Command
		}
	}
	return e.Arguments
}
func sourceProof(private string) { sourceProofMode(private, false) }
func sourceProofMode(private string, large bool) {
	f, e := os.Open(filepath.Join(private, "joined.private.jsonl"))
	if e != nil {
		fail()
	}
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 65536), 64*1024*1024)
	targets := map[string]map[string]*Joined{}
	order := []string{}
	counts := map[string]int{}
	for s.Scan() {
		var g Joined
		if json.Unmarshal(s.Bytes(), &g) != nil {
			fail()
		}
		write := false
		for _, r := range g.Receipts {
			write = write || directWrite.MatchString(r.Command)
		}
		if !write || len(g.Receipts) == 0 {
			continue
		}
		r := g.Receipts[0]
		if targets[r.Source] == nil {
			targets[r.Source] = map[string]*Joined{}
			order = append(order, r.Source)
		}
		copy := g
		targets[r.Source][r.Call] = &copy
		counts["write_groups"]++
	}
	if s.Err() != nil {
		fail()
	}
	f.Close()
	prefix := "source"
	if large {
		prefix = "source-large"
	}
	raw := newPrivate(filepath.Join(private, prefix+"-witnesses.private.jsonl"))
	defer raw.Close()
	safe := newPrivate(filepath.Join(private, prefix+"-safe.jsonl"))
	defer safe.Close()
	var budget int64 = 512 * 1024 * 1024
	maxFile := int64(64 * 1024 * 1024)
	if large {
		budget = 1024 * 1024 * 1024
		maxFile = 512 * 1024 * 1024
	}
	for n, path := range order {
		groups := targets[path]
		if !allowedSource(path) || !noLinkAncestors(path) {
			counts["source_outside_allowlist"]++
			continue
		}
		fi, e := os.Lstat(path)
		if e != nil {
			counts["source_unavailable"]++
			continue
		}
		if large && fi.Size() <= 64*1024*1024 {
			continue
		}
		if !fi.Mode().IsRegular() || fi.Size() > maxFile || fi.Size() > budget {
			counts["source_not_read_limit_or_type"]++
			continue
		}
		budget -= fi.Size()
		src, e := os.Open(path)
		if e != nil {
			counts["source_unavailable"]++
			continue
		}
		h := sha256.New()
		scanner := bufio.NewScanner(io.TeeReader(src, h))
		scanner.Buffer(make([]byte, 65536), 32*1024*1024)
		line := 0
		calls := map[string]int{}
		outputs := map[string]int{}
		exact := map[string]bool{}
		terminal := map[string]bool{}
		for scanner.Scan() {
			line++
			b := scanner.Bytes()
			event, ok := unwrapEvent(b)
			if !ok {
				continue
			}
			g := groups[event.Call]
			if g == nil {
				continue
			}
			if strings.HasSuffix(event.Type, "_output") {
				outputs[event.Call]++
				var output string
				if json.Unmarshal(event.Output, &output) == nil {
					_, terminal[event.Call] = terminalExit(Receipt{Output: output})
				}
			} else {
				calls[event.Call]++
				for _, r := range g.Receipts {
					exact[event.Call] = exact[event.Call] || eventCommand(event) == r.Command
				}
			}
			record := map[string]any{"source_record": n + 1, "line": line, "record": g.Record, "raw": json.RawMessage(append([]byte(nil), b...))}
			if json.NewEncoder(raw).Encode(record) != nil {
				fail()
			}
		}
		complete := scanner.Err() == nil
		src.Close()
		if complete {
			counts["sources_read_complete"]++
		} else {
			counts["sources_read_failure"]++
		}
		sum := hex.EncodeToString(h.Sum(nil))
		for call, g := range groups {
			if calls[call] > 0 {
				counts["actual_call_observed"]++
			}
			if outputs[call] > 0 {
				counts["actual_output_observed"]++
			}
			if exact[call] {
				counts["exact_command_observed"]++
			}
			if terminal[call] {
				counts["terminal_observed"]++
			}
			result := map[string]any{"record": g.Record, "source_record": n + 1, "source_bytes_sha256": sum, "calls": calls[call], "outputs": outputs[call], "exact_command": exact[call], "terminal": terminal[call], "success": complete}
			if json.NewEncoder(safe).Encode(result) != nil {
				fail()
			}
		}
	}
	counts["sources_selected"] = len(order)
	if raw.Sync() != nil || safe.Sync() != nil {
		fail()
	}
	out := newPrivate(filepath.Join(private, prefix+"-summary.json"))
	defer out.Close()
	if json.NewEncoder(out).Encode(counts) != nil {
		fail()
	}
	emit(counts)
}
