package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var digestRE = regexp.MustCompile(`(?i)\b[0-9a-f]{64}\b`)

func assess(private string) {
	b, e := os.ReadFile("D:/CC/Tools/todo/todo.json")
	if e != nil {
		fail()
	}
	var store struct {
		Items []map[string]any `json:"items"`
	}
	if json.Unmarshal(b, &store) != nil {
		fail()
	}
	jobs := map[string]bool{}
	for _, t := range store.Items {
		id, _ := t["id"].(float64)
		if id == 391 {
			if js, ok := t["jobs"].([]any); ok {
				for _, j := range js {
					if v, ok := j.(string); ok {
						jobs[v] = true
					}
					if m, ok := j.(map[string]any); ok {
						for _, key := range []string{"id", "job_id"} {
							if v, ok := m[key].(string); ok {
								jobs[v] = true
							}
						}
					}
				}
			}
		}
	}
	f, e := os.Open(filepath.Join(private, "matches.private.jsonl"))
	if e != nil {
		fail()
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 65536), 32*1024*1024)
	out := newPrivate(filepath.Join(private, "hash-readback.private.jsonl"))
	defer out.Close()
	counts := map[string]int{"current_391_job_references": len(jobs)}
	n := 0
	for s.Scan() {
		n++
		var r Receipt
		if json.Unmarshal(s.Bytes(), &r) != nil {
			fail()
		}
		if buildRE.MatchString(r.Command) {
			counts["build_syntax"]++
			if r.JobID != "" && jobs[r.JobID] {
				counts["explicit_current_391_job_match"]++
			}
		}
		if !shaRE.MatchString(r.Command) || !strings.Contains(strings.ToLower(r.Command), "client-projects.txt") {
			continue
		}
		counts["hash_command_mentions"]++
		ds := digestRE.FindAllString(r.Output, -1)
		if len(ds) > 0 {
			counts["hash_output_mentions"]++
		}
		record := map[string]any{"record": n, "receipt": r, "output_digest_mentions": len(ds), "original_association": "unproven"}
		if json.NewEncoder(out).Encode(record) != nil {
			fail()
		}
	}
	if s.Err() != nil || out.Sync() != nil {
		fail()
	}
	summary := newPrivate(filepath.Join(private, "readback-summary.json"))
	defer summary.Close()
	if json.NewEncoder(summary).Encode(counts) != nil {
		fail()
	}
	emit(counts)
}
