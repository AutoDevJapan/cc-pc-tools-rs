// evidence2674 reads a finite set of operational receipts as data. It never
// executes recorded commands or prints source strings, paths, or errors.
package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func hash(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func emit(v any)           { b, _ := json.Marshal(v); fmt.Println(string(b)) }
func fail()                { emit(map[string]bool{"success": false}); os.Exit(1) }

var refs = regexp.MustCompile(`(?i)(?:D:[/\\]CC[/\\]Work[/\\])(?:[a-z0-9._-]+[/\\])*[a-z0-9._-]+`)
var ids = map[int]bool{2467: true, 2488: true, 2648: true, 2665: true, 2669: true, 2671: true, 2674: true}

func todo() {
	b, e := os.ReadFile("D:/CC/Tools/todo/todo.json")
	if e != nil {
		fail()
	}
	var root map[string]json.RawMessage
	if json.Unmarshal(b, &root) != nil {
		fail()
	}
	var items []map[string]any
	if json.Unmarshal(root["items"], &items) != nil {
		fail()
	}
	for _, t := range items {
		n, _ := t["id"].(float64)
		if !ids[int(n)] {
			continue
		}
		out := map[string]any{"record": int(n)}
		if status, ok := t["status"].(string); ok {
			switch status {
			case "todo", "doing", "done", "wait", "block":
				out["status"] = status
			}
		}
		if p, ok := t["progress"].(float64); ok && p >= 0 && p <= 100 {
			out["progress"] = p
		}
		if waits, ok := t["wait_on"].([]any); ok {
			numbers := []int{}
			for _, v := range waits {
				if n, ok := v.(float64); ok && n > 0 && n == float64(int(n)) {
					numbers = append(numbers, int(n))
				}
			}
			out["wait_on"] = numbers
		}
		// References stay inside the private index. They are not evidence of authority.
		raw, _ := json.Marshal(t)
		out["sha256"] = hash(raw)
		out["references"] = len(refs.FindAll(raw, -1))
		if project, ok := t["project"].(string); ok {
			for _, known := range []string{"unattended-operations", "unattended-operations-20261013", "cc-pc-tools-rs", "pc-tools"} {
				if project == known {
					out["project"] = known
				}
			}
		}
		emit(out)
	}
}

func main() {
	if len(os.Args) < 2 {
		fail()
	}
	switch os.Args[1] {
	case "todo":
		todo()
	case "source-check":
		if len(os.Args) != 3 {
			fail()
		}
		sourceCheck(os.Args[2])
	case "assess":
		if len(os.Args) != 3 {
			fail()
		}
		assess(os.Args[2])
	case "async":
		if len(os.Args) != 3 {
			fail()
		}
		followAsync(os.Args[2])
	case "outputs":
		if len(os.Args) != 3 {
			fail()
		}
		inspectOutputs(os.Args[2])
	case "source":
		if len(os.Args) != 3 {
			fail()
		}
		sourceProof(os.Args[2])
	case "source-large":
		if len(os.Args) != 3 {
			fail()
		}
		sourceProofMode(os.Args[2], "large")
	case "source-remainder":
		if len(os.Args) != 3 {
			fail()
		}
		sourceProofMode(os.Args[2], "remainder")
	case "witness":
		if len(os.Args) != 3 {
			fail()
		}
		witness(os.Args[2])
	case "join":
		if len(os.Args) != 4 {
			fail()
		}
		joinAll(os.Args[2], os.Args[3])
	case "detail":
		if len(os.Args) != 3 {
			fail()
		}
		detail(os.Args[2])
	case "audit":
		if len(os.Args) != 4 {
			fail()
		}
		audit(os.Args[2], os.Args[3])
	default:
		fail()
	}
}

type Receipt struct {
	Call       string `json:"call_id"`
	Command    string `json:"command"`
	Output     string `json:"output"`
	Kind       string `json:"kind"`
	Status     string `json:"command_status"`
	Source     string `json:"source_path"`
	SourceHash string `json:"source_hash"`
	Line       int    `json:"source_line"`
	Matched    bool   `json:"start_matched"`
	Exit       *int   `json:"exit_code"`
	Session    any    `json:"session_id"`
	Tool       string `json:"tool_name"`
	JobID      string `json:"job_id"`
}
type Finding struct {
	Record       int    `json:"record"`
	SHA          string `json:"sha256"`
	CallHash     string `json:"call_sha256"`
	SourceHash   string `json:"source_sha256"`
	CommandBytes int    `json:"command_bytes"`
	Matched      bool   `json:"start_matched"`
	Terminal     bool   `json:"terminal"`
	ExitZero     bool   `json:"exit_zero"`
	Write        bool   `json:"write_mention"`
	Read         bool   `json:"read_mention"`
	SHARead      bool   `json:"hash_mention"`
	Dry          bool   `json:"dry_or_quote_mention"`
	Denied       bool   `json:"denied"`
}

var writeRE = regexp.MustCompile(`(?i)set-content|add-content|out-file|writealltext|writeallbytes|os\.writefile|writefile\(|add file:|update file:|write_text|write_bytes|copy-item|move-item|\.write\(`)
var readRE = regexp.MustCompile(`(?i)get-content|readalltext|readallbytes|readfile\(|read_text|read_bytes|--guard-path`)
var shaRE = regexp.MustCompile(`(?i)get-filehash|sha256|sha256sum`)
var deniedRE = regexp.MustCompile(`(?i)rejected|blocked by|permission denied|access is denied|access.*denied|UnauthorizedAccess`)

func classify(n int, b []byte, r Receipt) Finding {
	c := strings.ToLower(r.Command)
	code, terminal := terminalExit(r)
	zero := terminal && code == 0
	return Finding{n, hash(b), hash([]byte(r.Call)), hash([]byte(r.Source)), len(r.Command), r.Matched, terminal, zero, writeRE.MatchString(c), readRE.MatchString(c), shaRE.MatchString(c), strings.Contains(c, "dry-run") || strings.Contains(c, "whatif") || strings.Contains(c, "prompt") || strings.Contains(c, "rg "), deniedRE.MatchString(r.Output)}
}
func newPrivate(path string) *os.File {
	f, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		fail()
	}
	return f
}
func audit(path, private string) {
	// Destination must already have been restricted by the caller; no raw data on stdout.
	f, e := os.Open(path)
	if e != nil {
		fail()
	}
	defer f.Close()
	raw := newPrivate(filepath.Join(private, "matches.private.jsonl"))
	defer raw.Close()
	safe := newPrivate(filepath.Join(private, "findings.jsonl"))
	defer safe.Close()
	h := sha256.New()
	s := bufio.NewScanner(io.TeeReader(f, h))
	s.Buffer(make([]byte, 65536), 32*1024*1024)
	i, hits, writes, bad := 0, 0, 0, 0
	kinds := map[string]int{}
	statuses := map[string]int{}
	for s.Scan() {
		i++
		b := s.Bytes()
		if !bytes.Contains(bytes.ToLower(b), []byte("client-projects.txt")) && !bytes.Contains(bytes.ToLower(b), []byte("scrub391")) {
			continue
		}
		var r Receipt
		if json.Unmarshal(b, &r) != nil {
			bad++
			continue
		}
		// Only actual command fields are inspected. Output-only hits cannot create a call.
		c := strings.ToLower(r.Command)
		if !strings.Contains(c, "client-projects.txt") && !strings.Contains(c, "scrub391") {
			continue
		}
		hits++
		v := classify(i, b, r)
		kinds[hash([]byte(r.Kind))]++
		statuses[hash([]byte(r.Status))]++
		if v.Write {
			writes++
			if writes <= 30 {
				emit(v)
			}
		}
		if _, e = raw.Write(append(append([]byte(nil), b...), '\n')); e != nil {
			fail()
		}
		if e = json.NewEncoder(safe).Encode(v); e != nil {
			fail()
		}
	}
	if s.Err() != nil {
		fail()
	}
	if raw.Sync() != nil || safe.Sync() != nil {
		fail()
	}
	summary := map[string]any{"records": i, "command_matches": hits, "write_mentions": writes, "decode_failures": bad, "source_sha256": hex.EncodeToString(h.Sum(nil)), "kinds_sha256": kinds, "statuses_sha256": statuses, "success": true}
	out := newPrivate(filepath.Join(private, "audit-summary.json"))
	if json.NewEncoder(out).Encode(summary) != nil || out.Close() != nil {
		fail()
	}
	emit(summary)
}

// These are candidate selectors, not a shell parser or proof of execution.
// A literal can occur inside quoted code, a prompt, or a deferred script.
var directWrite = regexp.MustCompile(`(?i)(?:set-content|add-content|out-file|writealltext|writeallbytes|os\.writefile|write_text|write_bytes)[^\r\n;]{0,180}client-projects\.txt|client-projects\.txt[^\r\n;]{0,100}(?:write_text|write_bytes)|(?:add|update) file:[^\r\n]*client-projects\.txt`)
var directRead = regexp.MustCompile(`(?i)(?:get-content|readalltext|readallbytes|os\.readfile|read_text|read_bytes|get-filehash|sha256sum)[^\r\n;]{0,180}client-projects\.txt`)
var buildRE = regexp.MustCompile(`(?i)go build[^\r\n]{0,500}scrub391|(?:add|update) file:[^\r\n]*scrub391`)

func detail(private string) {
	f, e := os.Open(filepath.Join(private, "matches.private.jsonl"))
	if e != nil {
		fail()
	}
	defer f.Close()
	save := newPrivate(filepath.Join(private, "selected.private.jsonl"))
	defer save.Close()
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 65536), 32*1024*1024)
	i := 0
	counts := map[string]int{}
	for s.Scan() {
		i++
		var r Receipt
		if json.Unmarshal(s.Bytes(), &r) != nil {
			fail()
		}
		w := directWrite.MatchString(r.Command)
		rd := directRead.MatchString(r.Command)
		build := buildRE.MatchString(r.Command)
		if !w && !rd && !build {
			continue
		}
		v := classify(i, s.Bytes(), r)
		if w {
			counts["direct_write_syntax"]++
		}
		if rd {
			counts["direct_read_syntax"]++
		}
		if build {
			counts["build_syntax"]++
		}
		if w || build {
			emit(map[string]any{"record": i, "sha256": v.SHA, "call_sha256": v.CallHash, "source_sha256": v.SourceHash, "write_syntax": w, "build_syntax": build, "read_syntax": rd, "terminal": v.Terminal, "exit_zero": v.ExitZero, "denied": v.Denied, "quoted_or_dry": v.Dry})
			if _, e = save.Write(append(append([]byte(nil), s.Bytes()...), '\n')); e != nil {
				fail()
			}
		}
	}
	if s.Err() != nil || save.Sync() != nil {
		fail()
	}
	out := newPrivate(filepath.Join(private, "selection-summary.json"))
	defer out.Close()
	json.NewEncoder(out).Encode(counts)
	emit(counts)
}
