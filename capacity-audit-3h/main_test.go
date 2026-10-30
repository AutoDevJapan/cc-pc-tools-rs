package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestOwnerRuleClassificationKeepsDefaultAttributionDistinct(t *testing.T) {
	for _, tc := range []struct{ body, want string }{
		{`{"client_resource":true,"classification_unverified":true}`, "protected_unclassified"},
		{`{"client_resource":true,"classification_policy":"owner-client-rule-20261027","classification_basis":"owner_rule_unattributed_location"}`, "protected_owner_rule_unattributed"},
		{`{"client_resource":true,"classification_policy":"owner-client-rule-20261027","classification_basis":"client_department"}`, "protected"},
		{`{"client_resource":false}`, "clear"},
		{`{}`, "guard_unavailable"},
	} {
		if classifyGuardResponse([]byte(tc.body)) != tc.want {
			t.Fatal("classification evidence conflated")
		}
	}
}

func TestOwnerRuleProtectionBasisReachesCategoryReport(t *testing.T) {
	result := protectedCategoryResult(rootSpec{Category: "synthetic"}, "protected_owner_rule_unattributed")
	encoded, err := json.Marshal(report{Schema: 1, Categories: []categoryResult{result}})
	if err != nil || !strings.Contains(string(encoded), `"protection_basis":"protected_owner_rule_unattributed"`) || !strings.Contains(string(encoded), `"status":"protected_unclassified"`) {
		t.Fatal("protection basis lost in category report")
	}
}

func TestBoundary(t *testing.T) {
	root := t.TempDir()
	for _, path := range []string{"", filepath.Dir(root), root + "-sibling", root} {
		if within(root, path, false) {
			t.Fatal("accepted non-child")
		}
	}
	if !within(root, filepath.Join(root, "..cache"), false) {
		t.Fatal("rejected real child")
	}
	if !within(root, root, true) {
		t.Fatal("root denied for metadata scan")
	}
	if runtime.GOOS == "windows" && within(root, `E:\outside`, true) {
		t.Fatal("different drive admitted")
	}
}
func TestBoundedScanResumesAndDoesNotClaimFullDrive(t *testing.T) {
	root := t.TempDir()
	os.Mkdir(filepath.Join(root, "sub"), 0700)
	for _, path := range []string{"a.bin", "b.bin", filepath.Join("sub", "c.bin")} {
		if err := os.WriteFile(filepath.Join(root, path), []byte("1234"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	st := &scanState{Pending: []pendingDir{{root, 0}}, Started: time.Now().UTC().Format(time.RFC3339)}
	r := scan(context.Background(), rootSpec{"fixture", root, true}, st, 1)
	if r.Status != "partial" || len(st.Pending) == 0 {
		t.Fatal("partial scan falsely complete")
	}
	for range 10 {
		if len(st.Pending) == 0 {
			break
		}
		r = scan(context.Background(), rootSpec{"fixture", root, true}, st, 1)
	}
	if r.Status != "complete_metadata_scan" || r.Bytes == nil || *r.Bytes != 12 || r.Files != 3 || r.Directories != 1 {
		t.Fatalf("unexpected aggregate: %+v", r)
	}
	encoded, _ := json.Marshal(r)
	if strings.Contains(string(encoded), root) || strings.Contains(string(encoded), "a.bin") {
		t.Fatal("path leaked")
	}
}

func TestNativeSingleFlightAndJunction(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("native Windows acceptance")
	}
	root := t.TempDir()
	unlock, err := lock(filepath.Join(root, "run.lock"))
	if err != nil {
		t.Fatal(err)
	}
	if second, err := lock(filepath.Join(root, "run.lock")); !errors.Is(err, errLocked) {
		if second != nil {
			second()
		}
		t.Fatal("parallel instance admitted")
	}
	unlock()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "keep.bin"), []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(root, "linked")
	cmd := exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", linked, outside)
	hide(cmd)
	if err := cmd.Run(); err != nil {
		t.Fatal("could not create owned junction fixture")
	}
	defer os.Remove(linked)
	st := &scanState{Pending: []pendingDir{{root, 0}}}
	r := scan(context.Background(), rootSpec{"fixture", root, true}, st, 100)
	if r.Links != 1 || r.Bytes == nil || *r.Bytes != 0 || r.Files != 1 {
		t.Fatalf("followed junction or counted external contents: %+v", r)
	}
	if !safeStateFile(filepath.Join(root, "new-state.json")) || safeStateFile(linked) {
		t.Fatal("unsafe state boundary")
	}
}
func TestCheckpointCannotScanOutsideItsFixedRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "private.bin"), make([]byte, 64), 0600)
	st := &scanState{Pending: []pendingDir{{outside, 0}}}
	r := scan(context.Background(), rootSpec{"fixture", root, true}, st, 100)
	if r.Files != 0 || r.Bytes == nil || *r.Bytes != 0 || r.Errors != 1 || r.Status != "partial_with_errors" {
		t.Fatal("outside cursor consumed")
	}
}
func TestDeadlineAndRotatingOrder(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	root := t.TempDir()
	st := &scanState{Pending: []pendingDir{{root, 0}}}
	if scan(ctx, rootSpec{"fixture", root, true}, st, 10).Status != "partial" {
		t.Fatal("expired scan claimed complete")
	}
	order := rootOrder(2, 3)
	if len(order) != 3 || order[0] != 2 || order[1] != 0 || order[2] != 1 {
		t.Fatal("cursor starvation")
	}
}
func TestCheckpointRoundtripAndCorruptionFailClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	st := checkpoint{Schema: 1, NextRoot: 3, Scans: map[string]*scanState{"fixture": {Bytes: 17}}}
	if err := saveCheckpoint(path, st); err != nil {
		t.Fatal(err)
	}
	loaded, err := readCheckpoint(path)
	if err != nil || loaded.NextRoot != 3 || loaded.Scans["fixture"].Bytes != 17 {
		t.Fatal("state lost")
	}
	os.WriteFile(path, []byte("{}"), 0600)
	if _, err := readCheckpoint(path); err == nil {
		t.Fatal("bad state accepted")
	}
}

func TestProtectedCategoryIsUnknownRatherThanZero(t *testing.T) {
	value := categoryResult{Category: "private", Status: "protected_unclassified"}
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if json.Unmarshal(data, &decoded) != nil {
		t.Fatal("invalid report")
	}
	if decoded["logical_bytes"] != nil {
		t.Fatal("unscanned private category reported zero capacity")
	}
}
