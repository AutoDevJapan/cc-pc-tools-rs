//go:build ignore

// Metadata-only continuation plan for original source9. No saved body is opened.
package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

func main() {
	if e := sitePlan(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
func sitePlan() error {
	if os.Getenv("CC_JOB_ID") != "20261027-175645-todo-3080" {
		return fmt.Errorf("actual parent required")
	}
	p := "D:/CC/Work/unattended-operations-20261013-wt/todo-3080-16-skills-3048/.ci-work/todo3080"
	q, e := os.Open(filepath.Join(p, "execution-queue-next79-still154-175645.jsonl"))
	if e != nil {
		return e
	}
	defer q.Close()
	qh := sha256.New()
	s := bufio.NewScanner(io.TeeReader(q, qh))
	s.Buffer(make([]byte, 65536), 4<<20)
	stages := map[string]string{}
	qn := 0
	for s.Scan() {
		var r struct {
			ID    string `json:"inventory_id"`
			Stage string `json:"execution_stage"`
		}
		if e = json.Unmarshal(s.Bytes(), &r); e != nil {
			return e
		}
		if r.ID == "" || r.Stage == "" || stages[r.ID] != "" {
			return fmt.Errorf("queue identity differs")
		}
		stages[r.ID] = r.Stage
		qn++
	}
	if e = s.Err(); e != nil {
		return e
	}
	if qn != 73220 || hex.EncodeToString(qh.Sum(nil)) != "c833bae9da150bc5f447744d12dfea0fc645c6e4dfe25c74c3157ddd947ed008" {
		return fmt.Errorf("complete current queue differs")
	}
	f, e := os.Open("D:/CC/Work/unattended-operations-20261013-wt/todo-3080-skills-inventory/.ci-work/todo3080/phase-a-catalog.jsonl")
	if e != nil {
		return e
	}
	defer f.Close()
	h := sha256.New()
	s = bufio.NewScanner(io.TeeReader(f, h))
	s.Buffer(make([]byte, 65536), 4<<20)
	n := 0
	counts := map[string]int{}
	rows := []any{}
	next := []any{}
	for s.Scan() {
		n++
		var r struct {
			ID      string `json:"inventory_id"`
			Unit    string `json:"inventory_unit"`
			Name    string `json:"name"`
			URL     string `json:"source_url"`
			Attempt struct {
				SHA    string `json:"sha256"`
				Bytes  int64  `json:"bytes"`
				Path   string `json:"content_path"`
				At     string `json:"retrieved_at"`
				Read   bool   `json:"machine_body_read"`
				Commit bool   `json:"commit_verified"`
			} `json:"effective_attempt"`
		}
		if e = json.Unmarshal(s.Bytes(), &r); e != nil {
			return e
		}
		if r.Unit != "site_index_entry" {
			continue
		}
		stage := stages[r.ID]
		if stage == "" {
			return fmt.Errorf("site missing from complete queue")
		}
		counts[stage]++
		v := map[string]any{"site_ordinal": len(rows) + 1, "inventory_id": r.ID, "source_url": r.URL, "name": r.Name, "fixed_saved_source_sha256": r.Attempt.SHA, "fixed_saved_source_bytes": r.Attempt.Bytes, "content_path": r.Attempt.Path, "retrieved_at": r.Attempt.At, "machine_body_available": r.Attempt.Read, "upstream_commit_verified": r.Attempt.Commit, "current_queue_stage": stage}
		rows = append(rows, v)
		if stage == "needs_semantic_and_individual_rights_review" && r.Attempt.Read && r.ID != "site:find-skills" && len(next) < 12 {
			next = append(next, v)
		}
	}
	if e = s.Err(); e != nil {
		return e
	}
	if n != 73220 || len(rows) != 10290 || hex.EncodeToString(h.Sum(nil)) != "2d73c9a038a41cc220a9687b935bfa1e0b315b65d9bd6d3e54ddb4111972876c" {
		return fmt.Errorf("complete fixed catalog differs")
	}
	out := map[string]any{"domain": "skills3080-source9-full-metadata-continuation-plan-v1", "actual_parent": os.Getenv("CC_JOB_ID"), "observed_at": time.Now().UTC().Format(time.RFC3339Nano), "catalog_sha256": hex.EncodeToString(h.Sum(nil)), "queue_sha256": hex.EncodeToString(qh.Sum(nil)), "site_occurrences": len(rows), "current_queue_stage_counts": counts, "rows": rows, "all_rows_preserved_no_selection_ceiling": true, "body_opens_or_meaning_or_raw_credit_or_rights_or_adoptions": 0, "selection_rule": "Next12 is a bounded display from original source order only; entire10290 remains in plan. Parent find-skills visible reading is separate and unpublished, not canonical assessment credit.", "whole16_complete": false}
	b, e := json.MarshalIndent(out, "", "  ")
	if e != nil {
		return e
	}
	b = append(b, '\n')
	path := filepath.Join(p, "source9-all10290-metadata-continuation-plan175645.json")
	w, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	if _, e = w.Write(b); e != nil {
		w.Close()
		return e
	}
	if e = w.Close(); e != nil {
		return e
	}
	sum := sha256.Sum256(b)
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"file": filepath.Base(path), "sha256": fmt.Sprintf("%x", sum), "site_occurrences": len(rows), "current_queue_stage_counts": counts, "next_bounded_metadata12": next, "body_opens": 0, "whole16_complete": false})
}
