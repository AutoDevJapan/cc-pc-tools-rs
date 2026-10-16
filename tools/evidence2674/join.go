package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

func receiptKey(r Receipt) string { return r.Source + "\x00" + r.Call }

var exitLine = regexp.MustCompile(`(?m)^(?:Process exited with code |Process exit code: |Exit code: )(-?[0-9]+)\s*$`)

// terminalExit accepts an actual terminal envelope, never an exit number quoted
// somewhere inside a command, a prompt, or arbitrary JSON text.
func terminalExit(r Receipt) (int, bool) {
	if r.Exit != nil {
		return *r.Exit, true
	}
	if m := exitLine.FindStringSubmatch(r.Output); m != nil {
		n, e := strconv.Atoi(m[1])
		return n, e == nil
	}
	var v struct {
		ExitCode  *int   `json:"exit_code"`
		SessionID any    `json:"session_id"`
		Output    string `json:"output"`
	}
	if json.Unmarshal([]byte(r.Output), &v) == nil && v.ExitCode != nil && v.SessionID == nil {
		return *v.ExitCode, true
	}
	return 0, false
}

type Joined struct {
	Record        int       `json:"record"`
	SourceHash    string    `json:"source_sha256"`
	CallHash      string    `json:"call_sha256"`
	ReceiptHashes []string  `json:"receipt_sha256"`
	Receipts      []Receipt `json:"receipts"`
}

func joinAll(path, private string) {
	f, e := os.Open(filepath.Join(private, "selected.private.jsonl"))
	if e != nil {
		fail()
	}
	groups := map[string]*Joined{}
	order := []string{}
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 65536), 32*1024*1024)
	for s.Scan() {
		var r Receipt
		if json.Unmarshal(s.Bytes(), &r) != nil {
			fail()
		}
		key := receiptKey(r)
		if groups[key] == nil {
			order = append(order, key)
			groups[key] = &Joined{Record: len(order), SourceHash: hash([]byte(r.Source)), CallHash: hash([]byte(r.Call))}
		}
	}
	if s.Err() != nil {
		fail()
	}
	f.Close()
	input, e := os.Open(path)
	if e != nil {
		fail()
	}
	defer input.Close()
	h := sha256.New()
	s = bufio.NewScanner(io.TeeReader(input, h))
	s.Buffer(make([]byte, 65536), 32*1024*1024)
	total, bad := 0, 0
	for s.Scan() {
		total++
		var r Receipt
		if json.Unmarshal(s.Bytes(), &r) != nil {
			bad++
			continue
		}
		g := groups[receiptKey(r)]
		if g != nil {
			g.Receipts = append(g.Receipts, r)
			g.ReceiptHashes = append(g.ReceiptHashes, hash(s.Bytes()))
		}
	}
	if s.Err() != nil {
		fail()
	}
	raw := newPrivate(filepath.Join(private, "joined.private.jsonl"))
	defer raw.Close()
	safe := newPrivate(filepath.Join(private, "joined-safe.jsonl"))
	defer safe.Close()
	counts := map[string]int{"records": total, "decode_failures": bad, "groups": len(groups)}
	for _, key := range order {
		g := groups[key]
		commands := map[string]bool{}
		exitCodes := map[int]bool{}
		hasWrite, hasBuild := false, false
		start, denied := false, false
		sessions := 0
		for _, r := range g.Receipts {
			if r.Command != "" {
				commands[hash([]byte(r.Command))] = true
			}
			hasWrite = hasWrite || directWrite.MatchString(r.Command)
			hasBuild = hasBuild || buildRE.MatchString(r.Command)
			start = start || r.Matched
			denied = denied || deniedRE.MatchString(r.Output)
			if n, ok := terminalExit(r); ok {
				exitCodes[n] = true
			}
			if r.Session != nil || sessionRE.MatchString(r.Output) {
				sessions++
			}
		}
		state := "unproven"
		if denied {
			state = "failure_receipt"
		} else if len(exitCodes) == 1 && len(commands) == 1 && start && sessions == 0 {
			if exitCodes[0] {
				state = "completed_zero"
			} else {
				state = "completed_nonzero"
			}
		}
		if len(g.Receipts) == 0 {
			counts["not_observed"]++
		}
		counts[state]++
		if hasWrite {
			counts["write_"+state]++
		}
		if hasBuild {
			counts["build_"+state]++
		}
		public := map[string]any{"record": g.Record, "source_sha256": g.SourceHash, "call_sha256": g.CallHash, "receipt_sha256": g.ReceiptHashes, "receipts": len(g.Receipts), "command_versions": len(commands), "terminal_versions": len(exitCodes), "session_mentions": sessions, "write_syntax": hasWrite, "build_syntax": hasBuild, "state": state}
		if json.NewEncoder(raw).Encode(g) != nil || json.NewEncoder(safe).Encode(public) != nil {
			fail()
		}
	}
	if raw.Sync() != nil || safe.Sync() != nil {
		fail()
	}
	summary := map[string]any{"counts": counts, "source_sha256": hex.EncodeToString(h.Sum(nil)), "success": bad == 0, "actual_file_write": "unproven", "readback": "unproven", "independent_saved_sha": "unproven"}
	out := newPrivate(filepath.Join(private, "joined-summary.json"))
	defer out.Close()
	if json.NewEncoder(out).Encode(summary) != nil {
		fail()
	}
	emit(summary)
	// Known status names are compared internally; never echo an unknown string.
	known := []string{"function_call", "function_call_output", "custom_tool_call", "custom_tool_call_output", "command", "command_execution", "codex", "native", "completed", "started", "success", "failed", "running"}
	classes := map[string]int{}
	for _, g := range groups {
		for _, r := range g.Receipts {
			for _, v := range known {
				if strings.EqualFold(r.Kind, v) || strings.EqualFold(r.Status, v) {
					classes[v]++
				}
			}
		}
	}
	emit(classes)
}
