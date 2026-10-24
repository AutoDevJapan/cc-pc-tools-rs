package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestLargeDeviceCensusDoesNotBreakTheExistingQueueLimit(t *testing.T) {
	raw := deviceFixture()
	raw.Volumes = []rawVolume{}
	raw.Logical = []rawVolume{}
	for n := range 256 {
		total, free := uint64(4_000_000_000_000), uint64(1_000_000_000_000)
		raw.Volumes = append(raw.Volumes, rawVolume{ID: fmt.Sprintf("virtual-volume-%d", n), Total: &total, Free: &free, Health: "Healthy"})
	}
	inventory := normalizedFixture(t, raw, nil)
	r := report{Schema: 1, Time: time.Now().UTC().Format(time.RFC3339), Categories: []categoryResult{{Category: "windows", Status: "partial"}}, AllDrives: &allDriveReport{Inventory: inventory}}
	large, _ := json.Marshal(r)
	if len(large) <= 65536 {
		t.Fatal("fixture does not exercise the canonical ToDo input limit")
	}
	got := queueDecisionWith(r, t.TempDir(), func(_ context.Context, args ...string) ([]byte, error) {
		data, err := os.ReadFile(args[2])
		if err != nil {
			t.Fatal(err)
		}
		if len(data) > 65536 {
			return nil, errors.New("canonical capacity input too large")
		}
		var payload map[string]any
		if json.Unmarshal(data, &payload) != nil {
			t.Fatal("invalid queue payload")
		}
		if _, duplicated := payload["all_drives"]; duplicated {
			t.Fatal("sidecar duplicated into the bounded legacy queue")
		}
		return []byte(`{"todo_id":19,"generation":"` + strings.Repeat("a", 64) + `","action":"queued","worker_launched":false}`), nil
	})
	if got.Status != "queued" {
		t.Fatalf("large device inventory broke the legacy queue (%d bytes)", len(large))
	}
	if r.AllDrives == nil || len(r.AllDrives.Inventory.Volumes) != 256 {
		t.Fatal("queue projection erased the persisted census")
	}
}
