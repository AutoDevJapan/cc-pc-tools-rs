package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sort"
	"strconv"
	"time"
)

// The legacy watchdog released technical waits. The new receiver must retain
// that capability for the explicitly verified PC repairs, without touching
// owner holds, private projects or other readiness conditions.
func (r *Runner) releaseRetries(ctx context.Context, health *Health) {
	repairContext, cancelRepairs := context.WithTimeout(ctx, 10*time.Second)
	defer cancelRepairs()
	var body json.RawMessage
	if err := readJSON(r.TodoPath, maxTodo, &body); err != nil {
		health.Errors = append(health.Errors, "technical_retry_metadata_deferred")
		return
	}
	var db struct {
		Projects map[string]string `json:"projects"`
		Items    []struct {
			ID      uint64          `json:"id"`
			Project string          `json:"project"`
			Status  string          `json:"status"`
			Ready   json.RawMessage `json:"wait_ready"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &db); err != nil {
		health.Errors = append(health.Errors, "technical_retry_metadata_deferred")
		return
	}
	stamp := r.Now().Unix()
	if stamp < 0 {
		return
	}
	type candidate struct {
		id      uint64
		root    string
		product *productRetryCandidate
	}
	var due []candidate
	product, err := selectProductRetry(body, r.Now())
	if err != nil {
		health.Errors = append(health.Errors, "product_retry_metadata_deferred")
	} else if product != nil {
		due = append(due, candidate{id: productRetryID, product: product})
	}
	for _, row := range db.Items {
		if row.ID == 0 || row.ID == productRetryID || row.Status != "wait" || (row.Project != "cc-pc-tools-rs" && row.Project != "pc-capacity-audit" && row.Project != "localai-web-rs") {
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
			due = append(due, candidate{id: row.ID, root: root})
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
		if item.product == nil && repairContext.Err() != nil {
			continue
		}
		r.State.RetryCursor = item.id
		if err := r.save(); err != nil {
			health.Errors = append(health.Errors, "technical_retry_cursor_deferred")
			return
		}
		if item.product != nil {
			r.resumeProductRetry(ctx, health, item.product)
			continue
		}
		allowed, known := verified[item.root]
		if !known {
			raw, err := r.Run(repairContext, "D:/CC/Tools/client-records/scrub391.exe", "--guard-path", item.root)
			var proof struct {
				ClientResource *bool `json:"client_resource"`
			}
			allowed = err == nil && json.Unmarshal([]byte(raw), &proof) == nil && proof.ClientResource != nil && !*proof.ClientResource
			verified[item.root] = allowed
		}
		if !allowed {
			continue
		}
		raw, err := r.Run(repairContext, "D:/CC/Tools/todo/todo.exe", "resume-ready", strconv.FormatUint(item.id, 10), "--condition", "retry-time")
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
