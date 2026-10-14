package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sort"
	"strconv"
)

// The legacy watchdog released technical waits. The new receiver must retain
// that capability for the explicitly verified PC repairs, without touching
// owner holds, private projects or other readiness conditions.
func (r *Runner) releaseRetries(ctx context.Context, health *Health) {
	var db struct {
		Projects map[string]string `json:"projects"`
		Items    []struct {
			ID      uint64          `json:"id"`
			Project string          `json:"project"`
			Status  string          `json:"status"`
			Ready   json.RawMessage `json:"wait_ready"`
		} `json:"items"`
	}
	if err := readJSON(r.TodoPath, maxTodo, &db); err != nil {
		health.Errors = append(health.Errors, "technical_retry_metadata_deferred")
		return
	}
	stamp := r.Now().Unix()
	if stamp < 0 {
		return
	}
	type candidate struct {
		id   uint64
		root string
	}
	var due []candidate
	for _, row := range db.Items {
		if row.ID == 0 || row.Status != "wait" || (row.Project != "cc-pc-tools-rs" && row.Project != "pc-capacity-audit" && row.Project != "localai-web-rs") {
			continue
		}
		var wait struct {
			Condition string `json:"condition"`
			Since     uint64 `json:"since"`
			NotBefore uint64 `json:"not_before"`
		}
		if json.Unmarshal(row.Ready, &wait) != nil || wait.Condition != "retry-time" || wait.NotBefore < wait.Since || wait.NotBefore-wait.Since < 60 || wait.NotBefore-wait.Since > 86400 || wait.NotBefore > uint64(stamp) {
			continue
		}
		root := db.Projects[row.Project]
		if filepath.IsAbs(root) {
			due = append(due, candidate{row.ID, root})
		}
	}
	sort.Slice(due, func(i, j int) bool { return due[i].id < due[j].id })
	start := sort.Search(len(due), func(i int) bool { return due[i].id > r.State.RetryCursor })
	verified := make(map[string]bool)
	for i := 0; i < min(4, len(due)); i++ {
		if ctx.Err() != nil {
			return
		}
		item := due[(start+i)%len(due)]
		r.State.RetryCursor = item.id
		if err := r.save(); err != nil {
			health.Errors = append(health.Errors, "technical_retry_cursor_deferred")
			return
		}
		allowed, known := verified[item.root]
		if !known {
			raw, err := r.Run(ctx, "D:/CC/Tools/client-records/scrub391.exe", "--guard-path", item.root)
			var proof struct {
				ClientResource *bool `json:"client_resource"`
			}
			allowed = err == nil && json.Unmarshal([]byte(raw), &proof) == nil && proof.ClientResource != nil && !*proof.ClientResource
			verified[item.root] = allowed
		}
		if !allowed {
			continue
		}
		raw, err := r.Run(ctx, "D:/CC/Tools/todo/todo.exe", "resume-ready", strconv.FormatUint(item.id, 10), "--condition", "retry-time")
		var receipt struct {
			ID      uint64 `json:"id"`
			Resumed bool   `json:"resumed"`
		}
		if err != nil || json.Unmarshal([]byte(raw), &receipt) != nil || receipt.ID != item.id {
			health.Errors = append(health.Errors, "technical_retry_release_deferred")
			continue
		}
		if receipt.Resumed {
			health.Retried++
		}
	}
}
