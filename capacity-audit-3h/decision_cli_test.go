package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// This opt-in acceptance runs the real ToDo CLI against a new isolated store.
func TestDecisionQueueCLI(t *testing.T) {
	exe := os.Getenv("CAPACITY_TODO_TEST_EXE")
	if exe == "" {
		t.Skip("set CAPACITY_TODO_TEST_EXE for cross-tool acceptance")
	}
	home := t.TempDir()
	call := func(args ...string) []byte {
		cmd := exec.Command(exe, args...)
		cmd.Env = append(os.Environ(), "CC_TODO_HOME="+home, "CC_TODO_JOBS="+filepath.Join(home, "jobs"))
		hide(cmd)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("ToDo CLI %v failed: %v: %s", args, err, out)
		}
		return out
	}
	call("project", "fixture", home)
	report := filepath.Join(home, "fresh.json")
	spec := filepath.Join(home, "finite.md")
	data, _ := json.Marshal(map[string]any{"schema": 1, "time": time.Now().UTC().Add(-time.Minute).Format(time.RFC3339), "whole_drive_scan": false, "bytes_reclaimed": 0, "categories": []map[string]any{{"category": "windows", "status": "partial", "logical_bytes": 12}, {"category": "owner_data", "status": "protected_unclassified", "logical_bytes": nil}}})
	if err := os.WriteFile(report, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(spec, []byte("現在の適格な資源を有限に判定し、未調査/候補/保持/回収/必要質問を区別する。"), 0600); err != nil {
		t.Fatal(err)
	}
	call("capacity-cycle", "--report-file", report, "--spec", spec, "--project", "fixture")
	call("capacity-cycle", "--report-file", report, "--spec", spec, "--project", "fixture")
	stored, err := os.ReadFile(filepath.Join(home, "todo.json"))
	if err != nil {
		t.Fatal(err)
	}
	var db struct {
		Items []struct {
			ID           int    `json:"id"`
			Status       string `json:"status"`
			Dispatchable bool   `json:"dispatchable"`
			Model        string `json:"model"`
			Effort       string `json:"effort"`
		} `json:"items"`
		NextID int `json:"next_id"`
	}
	if json.Unmarshal(stored, &db) != nil || len(db.Items) != 1 || db.NextID != 2 || db.Items[0].ID != 1 || db.Items[0].Status != "todo" || !db.Items[0].Dispatchable || db.Items[0].Model != "gpt-6.1-sol" || db.Items[0].Effort != "max" {
		t.Fatal("real CLI did not create exactly one normally dispatchable row")
	}
}
