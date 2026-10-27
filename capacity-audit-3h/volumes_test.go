package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func deviceFixture() rawInventory {
	total, free := uint64(100), uint64(30)
	return rawInventory{Volumes: []rawVolume{{ID: "volume-1", Drive: "C", Total: &total, Free: &free, Health: "Healthy"}, {ID: "recovery", Total: &total, Free: &free, Health: "Warning"}}, Logical: []rawVolume{{ID: "volume-1", Drive: "C", Total: &total, Free: &free}, {ID: "volume-F", Drive: "F", Total: &total, Free: &free}}, Partitions: []partition{{Disk: 0, Number: 1, Type: "System", Bytes: testBytes(100)}, {Disk: 0, Number: 2, Type: "Reserved", Bytes: testBytes(16)}, {Disk: 0, Number: 4, Type: "Recovery", Bytes: testBytes(90)}}, Disks: []disk{{Number: 0, Bytes: testBytes(1000), Health: "Healthy", Status: "Online"}}, Errors: []string{}}
}

func testBytes(n uint64) *uint64 { return &n }

func normalizedFixture(t *testing.T, raw rawInventory, previous *volumeInventory) volumeInventory {
	t.Helper()
	data, _ := json.Marshal(raw)
	r, err := normalizeInventory(data, previous, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestCensusFindsAdditionalAndLetterlessVolumes(t *testing.T) {
	r := normalizedFixture(t, deviceFixture(), nil)
	if len(r.Volumes) != 3 || len(r.Partitions) != 3 || r.Status != "complete_device_inventory" {
		t.Fatalf("incomplete devices: %+v", r)
	}
	var extra, hidden bool
	for _, v := range r.Volumes {
		extra = extra || v.Drive == "F"
		hidden = hidden || (v.Drive == "" && v.Scan == "not_scanned_no_drive_letter")
	}
	if !extra || !hidden {
		t.Fatal("fixed letters or hidden partition lost")
	}
	for _, p := range r.Partitions {
		if !p.Keep {
			t.Fatal("OS partition not retained")
		}
	}
	data, _ := json.Marshal(r)
	if strings.Contains(string(data), "volume-1") || strings.Contains(string(data), "recovery\"") {
		t.Fatal("raw device identity persisted")
	}
}

func TestDisconnectReadFailureAndDeltaAreDifferent(t *testing.T) {
	raw := deviceFixture()
	first := normalizedFixture(t, raw, nil)
	free := uint64(15)
	raw.Volumes[0].Free = &free
	second := normalizedFixture(t, raw, &first)
	for _, v := range second.Volumes {
		if v.Drive == "C" && (v.FreeChange == nil || *v.FreeChange != -15) {
			t.Fatal("delta lost")
		}
	}
	raw.Volumes = []rawVolume{}
	raw.Logical = []rawVolume{}
	third := normalizedFixture(t, raw, &second)
	for _, v := range third.Volumes {
		if v.Status != "disconnected" || v.Free != nil || v.Total != nil || v.FreeChange != nil {
			t.Fatal("disconnect became zero or retained current value")
		}
	}
	raw.Errors = []string{"volumes_unavailable"}
	fourth := normalizedFixture(t, raw, &second)
	for _, v := range fourth.Volumes {
		if v.Status != "discovery_unavailable" {
			t.Fatal("read error claimed disconnect")
		}
	}
	raw = deviceFixture()
	raw.Volumes[0].Free = nil
	fifth := normalizedFixture(t, raw, &second)
	for _, v := range fifth.Volumes {
		if v.Drive == "C" && (v.Total != nil || v.Free != nil || v.FreeChange != nil || v.Status != "capacity_unavailable") {
			t.Fatal("unknown size or delta fabricated")
		}
	}
	returned := normalizedFixture(t, deviceFixture(), &third)
	for _, v := range returned.Volumes {
		if v.FreeChange != nil {
			t.Fatal("gap silently bridged")
		}
	}
}

func TestInvalidInventoryCannotMasqueradeAsEmptySuccess(t *testing.T) {
	for _, data := range []string{`{}`, `{"volumes":[],"logical":[],"partitions":[],"disks":[],"errors":["private error"]}`, `null`} {
		if _, err := normalizeInventory([]byte(data), nil, time.Now()); err == nil {
			t.Fatal("invalid inventory accepted")
		}
	}
}

func TestDriveWalkResumesByNameAcrossRemovals(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"a", "b", "c"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("1234"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	st := &driveScan{Pending: []driveCursor{{Path: root}}}
	allow := func(context.Context, string) string { return "clear" }
	r := scanDrive(context.Background(), root, st, 1, allow)
	if r.Status != "partial" || r.Files != 1 {
		t.Fatal("unbounded walk")
	}
	if err := os.Remove(filepath.Join(root, "a")); err != nil {
		t.Fatal(err)
	}
	for range 4 {
		r = scanDrive(context.Background(), root, st, 1, allow)
		if len(st.Pending) == 0 {
			break
		}
	}
	if r.Status != "complete_metadata_scan" || r.Files != 3 || r.Bytes == nil || *r.Bytes != 12 {
		t.Fatalf("cursor skipped remaining names: %+v", r)
	}
	encoded, _ := json.Marshal(r)
	if strings.Contains(string(encoded), root) {
		t.Fatal("private path leaked")
	}
}

func TestProtectedWalkNeverReadsBelowUnclassifiedRoot(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "keep"), []byte("keep"), 0600)
	st := &driveScan{Pending: []driveCursor{{Path: root}}}
	calls := 0
	r := scanDrive(context.Background(), root, st, 100, func(context.Context, string) string { calls++; return "protected_unclassified" })
	if calls != 1 || r.Bytes != nil || r.Files != 0 || r.Blocked != 1 || r.Status != "partial_protected" {
		t.Fatalf("protected walk claimed measured data: %+v", r)
	}
	if _, err := os.Stat(filepath.Join(root, "keep")); err != nil {
		t.Fatal("retained file lost")
	}
}

func TestOwnerRuleProtectionCountsSurviveResumeAndReachDriveReport(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"first", "second"} {
		if os.WriteFile(filepath.Join(root, name), nil, 0600) != nil {
			t.Fatal("fixture file")
		}
	}
	st := &driveScan{Pending: []driveCursor{{Path: root}}}
	guard := func(_ context.Context, path string) string {
		if path == root {
			return "clear"
		}
		if filepath.Base(path) == "first" {
			return "protected_owner_rule_unattributed"
		}
		return "protected"
	}
	var result driveResult
	for range 5 {
		result = scanDrive(context.Background(), root, st, 1, guard)
		if len(st.Pending) == 0 {
			break
		}
	}
	encoded, err := json.Marshal(result)
	if err != nil || result.ProtectionBasisCounts["protected"] != 1 || result.ProtectionBasisCounts["protected_owner_rule_unattributed"] != 1 || strings.Contains(string(encoded), root) {
		t.Fatal("protection evidence lost in resumed drive report")
	}
	if !strings.Contains(string(encoded), `"protection_basis_counts"`) || result.Classification != "protected_unclassified" {
		t.Fatal("wire-compatible report omitted protection evidence")
	}
}

func TestAllDriveCheckpointFailsClosedAndRetainsCursor(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	st, err := readAllDriveState(path)
	if err != nil || st.Schema != 1 {
		t.Fatal("new checkpoint unavailable")
	}
	st.Scans["id"] = &driveScan{Pending: []driveCursor{{Path: filepath.Dir(path), After: "a"}}, Files: 7}
	if saveAllDriveJSON(path, st) != nil {
		t.Fatal("checkpoint write")
	}
	got, err := readAllDriveState(path)
	if err != nil || got.Scans["id"].Files != 7 || got.Scans["id"].Pending[0].After != "a" {
		t.Fatal("cursor lost")
	}
	os.WriteFile(path, []byte(`{}`), 0600)
	if _, err := readAllDriveState(path); err == nil {
		t.Fatal("corrupt state became empty")
	}
}

func TestGuardOutageRetainsTheUnprocessedFile(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"a", "b"} {
		if os.WriteFile(filepath.Join(root, name), []byte("1234"), 0600) != nil {
			t.Fatal("fixture write")
		}
	}
	st := &driveScan{Pending: []driveCursor{{Path: root}}}
	r := scanDrive(context.Background(), root, st, 10, func(_ context.Context, path string) string {
		if filepath.Base(path) == "b" {
			return "guard_unavailable"
		}
		return "clear"
	})
	if r.Status != "partial_guard_unavailable" || r.Blocked != 0 || r.Files != 1 || len(st.Pending) != 1 {
		t.Fatalf("outage lost pending file: %+v", r)
	}
	r = scanDrive(context.Background(), root, st, 10, func(context.Context, string) string { return "clear" })
	if r.Status != "complete_metadata_scan" || r.Files != 2 || *r.Bytes != 8 {
		t.Fatalf("outage retry double counted or skipped: %+v", r)
	}
}

func TestGrowthPriorityRetainsRotatingFairness(t *testing.T) {
	a, b := int64(-100), int64(-20)
	volumes := []volume{{}, {FreeChange: &a}, {FreeChange: &b}, {}}
	for next := range len(volumes) {
		order := driveOrder(next, volumes)
		if order[0] != next {
			t.Fatal("growing volume starved rotation")
		}
		seen := map[int]bool{}
		for _, n := range order {
			if seen[n] {
				t.Fatal("duplicate scan")
			}
			seen[n] = true
		}
		if len(seen) != 4 {
			t.Fatal("volume omitted")
		}
	}
}

func TestWindowsDeviceDiscovery(t *testing.T) {
	if os.Getenv("CAPACITY_DEVICE_ACCEPTANCE") != "1" {
		t.Skip("explicit real Windows device census")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	data, err := discoverDevices(ctx)
	if err != nil {
		t.Fatal(err)
	}
	r, err := normalizeInventory(data, nil, time.Now())
	if err != nil || r.Status != "complete_device_inventory" || len(r.Volumes) == 0 || len(r.Partitions) == 0 {
		t.Fatal("real census incomplete")
	}
	t.Logf("real device census: %d volumes, %d partitions, %d disks", len(r.Volumes), len(r.Partitions), len(r.Disks))
	// Fresh isolated state only; never read the installed old checkpoint/log.
	collected, err := collectAllDrives(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if collected.WholeDriveScan || collected.DeletionAuthority || len(collected.Inventory.Volumes) == 0 {
		t.Fatal("read-only census claimed cleaning completion")
	}
	for _, result := range collected.Scans {
		if result.Status == "partial_protected" && result.Files == 0 && result.Bytes != nil {
			t.Fatal("unknown protected root reported zero capacity")
		}
	}
	t.Logf("real guarded metadata: %d drive results; deletion authority false", len(collected.Scans))
}
