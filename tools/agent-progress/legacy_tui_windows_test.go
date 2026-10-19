//go:build windows

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLegacyTUIKeepsAnActuallyObservedWorkerAndMeasuredProgress(t *testing.T) {
	f := fixtureFor(t, `{"items":[{"id":1,"status":"doing","progress":83,"jobs":["worker"]},{"id":2,"status":"wait"}]}`)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	observed := newNativeProbe(ctx)(uint32(os.Getpid()))
	if !observed.alive || !observed.known || observed.created == nil {
		t.Fatal("self identity not observed")
	}
	f.job(t, "worker", fmt.Sprintf(`{"id":"worker","status":"running","codex_pid":%d,"codex_pid_created":%d,"last_event_at":%q,"activity":"running: bounded test"}`, os.Getpid(), *observed.created, time.Now().UTC().Format(time.RFC3339Nano)))
	legacy := filepath.Join(f.root, "output", "snapshot-v1.json")
	var stdout, stderr bytes.Buffer
	args := []string{"--once", "--deadline", "4s", "--max-rows", "1000", "--output", legacy, "--todo", f.opts.TodoFile, "--jobs", f.opts.JobsDir, "--priority-file", "", "--resource-queue", f.opts.ResourceQueueDir}
	if runWithLegacyOutput(args, &stdout, &stderr, legacy) != 0 || stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatal("legacy acquisition failed")
	}
	data, err := os.ReadFile(legacy)
	if err != nil {
		t.Fatal(err)
	}
	var s Snapshot
	if json.Unmarshal(data, &s) != nil || s.SchemaVersion != 1 || len(s.Tasks) != 1 || len(s.Jobs) != 1 || s.Tasks[0].ID != 1 || s.Tasks[0].Percent == nil || *s.Tasks[0].Percent != 83 || s.Jobs[0].WorkerPresence != "verified_alive" {
		t.Fatal("real worker or measured progress was lost")
	}
}
