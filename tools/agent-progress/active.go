package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"time"
)

// Activity is evidence of progress, not a supervisor heartbeat or a ToDo label.
// Hide workers when the evidence expires; process presence alone proves no work.
const processingEvidenceAge = 30 * time.Second

func processingJob(j *Job, now time.Time) bool {
	if j.RecordedStatus != "running" || j.Execution != "running" ||
		j.WorkerPresence != "verified_alive" || j.cancelRequested || j.resourceBlocked ||
		j.Source.Status != "ok" || !j.Source.Complete || len(j.WaitReasons) != 0 ||
		j.LastActivity.State != "fresh" || j.LastActivity.At == nil {
		return false
	}
	at, err := time.Parse(time.RFC3339Nano, *j.LastActivity.At)
	if err != nil || at.After(now) || now.Sub(at) > processingEvidenceAge {
		return false
	}
	switch j.Phase {
	case "thinking", "tool", "editing", "research", "delegating":
		return true
	default:
		// Reporting, planning and handoff text do not certify processing.
		return false
	}
}

// The existing gate publishes exact process identities for waiting and active
// siblings. A waiting gate is not work, but an active sibling still is.
func processingResources(ctx context.Context, opts Options, id string, now time.Time, probe processProbe) bool {
	if queuedResource(ctx, opts, id, now, probe) {
		return false
	}
	dir := filepath.Join(opts.JobsDir, id, "resource-waits")
	info, err := os.Lstat(dir)
	if os.IsNotExist(err) {
		return true
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return false
	}
	f, err := os.Open(dir)
	if err != nil {
		return false
	}
	defer f.Close()
	entries, err := f.ReadDir(129)
	if err != nil && err != io.EOF {
		return false
	}
	if len(entries) > 128 {
		return false
	}
	waiting, active := false, false
	for _, entry := range entries {
		if ctx.Err() != nil {
			return false
		}
		var marker struct {
			JobID   string  `json:"job_id"`
			Kind    string  `json:"kind"`
			Reason  string  `json:"reason"`
			PID     uint32  `json:"pid"`
			Created *uint64 `json:"pid_created"`
		}
		source := readProjection(filepath.Join(dir, entry.Name()), 8192, &marker, now, opts.StaleAfter)
		if source.Status != "ok" {
			return false
		}
		if marker.JobID != id {
			continue
		}
		switch presence(marker.PID, marker.Created, probe) {
		case "absent", "identity_mismatch":
			continue
		case "verified_alive":
		default:
			return false
		}
		switch marker.Kind {
		case "cargo-active":
			active = true
		case "cargo":
			waiting = true
		case "":
			if marker.Reason != "cargoの共有ビルド枠待ち" && marker.Reason != "cargoの共有キャッシュ枠待ち" {
				return false
			}
			waiting = true
		default:
			return false
		}
	}
	return !waiting || active
}

// live_chain publishes a separate queue with the same exact PID identity.
// Never infer the resource state from the private command or message text.
func queuedResource(ctx context.Context, opts Options, id string, now time.Time, probe processProbe) bool {
	if opts.ResourceQueueDir == "" {
		return false
	}
	info, err := os.Lstat(opts.ResourceQueueDir)
	if os.IsNotExist(err) {
		return false
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return true
	}
	f, err := os.Open(opts.ResourceQueueDir)
	if err != nil {
		return true
	}
	defer f.Close()
	entries, err := f.ReadDir(129)
	if (err != nil && err != io.EOF) || len(entries) > 128 {
		return true
	}
	for _, entry := range entries {
		if ctx.Err() != nil {
			return true
		}
		var marker struct {
			Job     string  `json:"job"`
			PID     uint32  `json:"pid"`
			Created *uint64 `json:"pid_created_filetime"`
		}
		if readProjection(filepath.Join(opts.ResourceQueueDir, entry.Name()), 8192, &marker, now, opts.StaleAfter).Status != "ok" {
			return true
		}
		if marker.Job != id {
			continue
		}
		state := presence(marker.PID, marker.Created, probe)
		if state != "absent" && state != "identity_mismatch" {
			return true
		}
	}
	return false
}

func processingTask(item todoItem, items map[int64]todoItem, now time.Time, stale time.Duration) bool {
	return (item.Status == "doing" || item.Status == "todo") && len(taskWaits(item, items, now, stale)) == 0
}
