package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var cellRE = regexp.MustCompile(`^Script running with cell ID ([a-zA-Z0-9_-]+)`)

func followAsync(private string) {
	byRecord := map[int]Joined{}
	f, e := os.Open(filepath.Join(private, "joined.private.jsonl"))
	if e != nil {
		fail()
	}
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 65536), 64*1024*1024)
	for s.Scan() {
		var g Joined
		if json.Unmarshal(s.Bytes(), &g) != nil {
			fail()
		}
		byRecord[g.Record] = g
	}
	if s.Err() != nil {
		fail()
	}
	f.Close()
	f, e = os.Open(filepath.Join(private, "source-witnesses.private.jsonl"))
	if e != nil {
		fail()
	}
	s = bufio.NewScanner(f)
	s.Buffer(make([]byte, 65536), 32*1024*1024)
	type pending struct {
		Record int
		Line   int
		Cell   string
		Source string
	}
	ps := []pending{}
	for s.Scan() {
		var row struct {
			Record int             `json:"record"`
			Line   int             `json:"line"`
			Raw    json.RawMessage `json:"raw"`
		}
		if json.Unmarshal(s.Bytes(), &row) != nil {
			fail()
		}
		event, ok := unwrapEvent(row.Raw)
		if !ok || !strings.HasSuffix(event.Type, "_output") {
			continue
		}
		texts := toolTexts(event.Output)
		if enclosingState(texts) != "pending" {
			continue
		}
		m := cellRE.FindStringSubmatch(strings.TrimSpace(texts[0]))
		if m == nil {
			continue
		}
		g := byRecord[row.Record]
		if len(g.Receipts) == 0 {
			fail()
		}
		ps = append(ps, pending{row.Record, row.Line, m[1], g.Receipts[0].Source})
	}
	if s.Err() != nil {
		fail()
	}
	f.Close()
	raw := newPrivate(filepath.Join(private, "async-witnesses.private.jsonl"))
	defer raw.Close()
	counts := map[string]int{"pending": len(ps)}
	for _, p := range ps {
		if !allowedSource(p.Source) || !noLinkAncestors(p.Source) {
			counts["unproven"]++
			continue
		}
		fi, e := os.Lstat(p.Source)
		if e != nil || !fi.Mode().IsRegular() || fi.Size() > 64*1024*1024 {
			counts["unproven"]++
			continue
		}
		f, e := os.Open(p.Source)
		if e != nil {
			counts["unproven"]++
			continue
		}
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 65536), 32*1024*1024)
		line := 0
		waitCalls := map[string]bool{}
		completed := false
		for scanner.Scan() {
			line++
			if line <= p.Line {
				continue
			}
			event, ok := unwrapEvent(scanner.Bytes())
			if !ok {
				continue
			}
			keep := false
			if event.Type == "function_call" && (event.Name == "functions.wait" || event.Name == "wait") {
				var args struct {
					Cell string `json:"cell_id"`
				}
				if json.Unmarshal([]byte(event.Arguments), &args) == nil && args.Cell == p.Cell {
					waitCalls[event.Call] = true
					keep = true
					counts["wait_calls"]++
				}
			}
			if strings.HasSuffix(event.Type, "_output") && waitCalls[event.Call] {
				keep = true
				state := enclosingState(toolTexts(event.Output))
				if state == "enclosing_completed" {
					completed = true
				}
			}
			if keep {
				row := map[string]any{"record": p.Record, "line": line, "cell_sha256": hash([]byte(p.Cell)), "raw": json.RawMessage(append([]byte(nil), scanner.Bytes()...))}
				if json.NewEncoder(raw).Encode(row) != nil {
					fail()
				}
			}
		}
		readOK := scanner.Err() == nil
		f.Close()
		if completed && readOK {
			counts["enclosing_completed"]++
		} else {
			counts["unproven"]++
		}
	}
	if raw.Sync() != nil {
		fail()
	}
	out := newPrivate(filepath.Join(private, "async-summary.json"))
	defer out.Close()
	if json.NewEncoder(out).Encode(counts) != nil {
		fail()
	}
	emit(counts)
}
