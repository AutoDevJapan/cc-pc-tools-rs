package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestAllDriveFailureStillPersistsAndQueuesLegacyMeasurement(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("native audit lock")
	}
	for _, broken := range []string{`{}`, strings.Repeat(" ", 8*1024*1024+1)} {
		home := t.TempDir()
		path := filepath.Join(home, "all-drive-state-v1.json")
		if os.WriteFile(path, []byte(broken), 0600) != nil {
			t.Fatal("fixture write")
		}
		queued := 0
		err := runAudit(home, nil, auditDependencies{
			Collect:   collectAllDrives,
			Space:     func(string) (uint64, uint64, error) { return 100, 1000, nil },
			RootFiles: func() map[string]*uint64 { return map[string]*uint64{} },
			Queue: func(r report) *decisionReceipt {
				queued++
				if !r.AllDrivesFailed || r.AllDrives.Inventory.Status != "collection_unavailable" || r.Free["C"] == nil || *r.Free["C"] != 100 {
					t.Fatal("independent measurement lost")
				}
				return &decisionReceipt{Status: "queued", TodoID: 19}
			},
		})
		if err == nil || queued != 1 {
			t.Fatal("failure hidden or existing queue stopped")
		}
		persisted, e := readCheckpoint(filepath.Join(home, "state.json"))
		if e != nil || persisted.Last == nil || !persisted.Last.AllDrivesFailed || persisted.Last.Decision.Status != "queued" {
			t.Fatal("independent results not durable")
		}
		kept, _ := os.ReadFile(path)
		if string(kept) != broken {
			t.Fatal("corrupt state silently reset")
		}
		latest, _ := os.ReadFile(filepath.Join(home, "all-drive-latest-v1.json"))
		var failed allDriveReport
		if json.Unmarshal(latest, &failed) != nil || failed.Inventory.Status != "collection_unavailable" || failed.Inventory.Volumes != nil {
			t.Fatal("failure became empty successful survey")
		}
	}
}

func TestCheckpointWriteCeilingCannotPoisonNextRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	st := allDriveState{Schema: 1, Scans: map[string]*driveScan{}}
	if saveAllDriveJSON(path, st) != nil {
		t.Fatal("fixture checkpoint")
	}
	before, _ := os.ReadFile(path)
	if saveAllDriveJSON(path, strings.Repeat("x", 8*1024*1024)) == nil {
		t.Fatal("oversized next checkpoint admitted")
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("valid old checkpoint lost")
	}
}

func TestDeadlineDoesNotBecomeGuardFailure(t *testing.T) {
	root := t.TempDir()
	for _, atFile := range []bool{false, true} {
		if os.WriteFile(filepath.Join(root, "a"), []byte("1234"), 0600) != nil {
			t.Fatal("fixture write")
		}
		ctx, cancel := context.WithCancel(context.Background())
		st := &driveScan{Pending: []driveCursor{{Path: root}}}
		r := scanDrive(ctx, root, st, 10, func(_ context.Context, path string) string {
			if !atFile || path != root {
				cancel()
				return "guard_unavailable"
			}
			return "clear"
		})
		cancel()
		if r.Status != "partial" || r.Blocked != 0 || len(st.Pending) != 1 {
			t.Fatalf("deadline misreported: %+v", r)
		}
		r = scanDrive(context.Background(), root, st, 10, func(context.Context, string) string { return "clear" })
		if r.Files != 1 || r.Bytes == nil || *r.Bytes != 4 || r.Status != "complete_metadata_scan" {
			t.Fatal("deadline cursor did not resume")
		}
	}
}

func TestFallbackUsesSameIdentityThroughPrimaryQueryFailure(t *testing.T) {
	raw := deviceFixture()
	first := normalizedFixture(t, raw, nil)
	raw.Volumes = []rawVolume{}
	raw.Errors = []string{"volumes_unavailable"}
	middle := normalizedFixture(t, raw, &first)
	last := normalizedFixture(t, deviceFixture(), &middle)
	for _, sample := range []volumeInventory{first, middle, last} {
		count := 0
		for _, v := range sample.Volumes {
			if v.Drive == "C" {
				count++
				if v.Status != "available" {
					t.Fatal("present volume became disconnected")
				}
			}
		}
		if count != 1 {
			t.Fatal("fallback created a phantom device")
		}
	}
}

func TestUnknownDeviceSizeStaysNull(t *testing.T) {
	raw := deviceFixture()
	raw.Disks[0].Bytes = nil
	raw.Partitions[0].Bytes = nil
	r := normalizedFixture(t, raw, nil)
	data, _ := json.Marshal(r)
	if r.Disks[0].Bytes != nil || r.Partitions[0].Bytes != nil || !strings.Contains(string(data), `"bytes":null`) {
		t.Fatal("unknown partition/disk size became zero")
	}
}

func TestWideTreeKeepsDepthFirstCheckpointBounded(t *testing.T) {
	root := t.TempDir()
	for n := range 300 {
		if os.Mkdir(filepath.Join(root, fmt.Sprintf("directory-%04d", n)), 0700) != nil {
			t.Fatal("directory fixture")
		}
	}
	st := &driveScan{Root: root, Pending: []driveCursor{{Path: root}}}
	scanDrive(context.Background(), root, st, 100, func(context.Context, string) string { return "clear" })
	if len(st.Pending) > 2 || st.Directories != 100 {
		t.Fatalf("fanout inflated persistent queue: pending=%d directories=%d", len(st.Pending), st.Directories)
	}
}

func TestMountMoveRebasesTheSameVolumesCursor(t *testing.T) {
	oldRoot, newRoot := t.TempDir(), t.TempDir()
	st := &driveScan{Root: oldRoot, Pending: []driveCursor{{Path: filepath.Join(oldRoot, "sub"), After: "a"}}, Files: 3, Bytes: 12}
	moved := scanAtMount(st, newRoot)
	if moved.Pending[0].Path != filepath.Join(newRoot, "sub") || moved.Pending[0].After != "a" || moved.Files != 3 || moved.Bytes != 12 {
		t.Fatal("mount change lost same-volume progress")
	}
}
