package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func cutoverFixture(t *testing.T) (*fakeLedger, Config, string, string, []byte) {
	t.Helper()
	l, c := newLedger(t)
	path := cachePath(t)
	home := filepath.Dir(path)
	cfg := Config{BaseURL: c.base, SyncToken: "fixture-token", ClientID: "fixture-pc", CachePath: path}
	configPath := filepath.Join(home, "fixture.dpapi")
	if err := SaveConfig(configPath, cfg); err != nil {
		t.Fatal(err)
	}
	bridgePath := filepath.Join(home, "fixture-bridge.exe")
	if err := os.WriteFile(bridgePath, []byte("not executed by fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	// Preserve the latest local bytes, including BOM, CRLF and unknown schema.
	original := append([]byte{0xef, 0xbb, 0xbf}, []byte("{\r\n \"next_id\":4,\"projects\":{\"demo\":\"C:/fixture\"},\"custom\":{\"float\":1.2300e-7,\"null\":null},\"items\":[{\"id\":1,\"lastJob\":\"LATEST\",\"lastjob\":null,\"spec\":\"C:/fixture/specs/original.md\",\"generation\":4,\"receipt\":{\"unknown\":true}},{\"id\":3,\"status\":\"todo\"}]}\r\n")...)
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	return l, cfg, configPath, bridgePath, original
}

func TestCutoverPreservesLatestRawAppliesFinalDeltaAndActivatesLast(t *testing.T) {
	l, cfg, configPath, bridgePath, original := cutoverFixture(t)
	result, err := Cutover(context.Background(), cfg, configPath, bridgePath)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Activated || !result.LeaseReleased || result.Revision != 2 || result.OriginalSHA256 != digest(original) {
		t.Fatal("incomplete cutover result")
	}
	if !bytes.Equal(read(t, result.BackupPath), original) {
		t.Fatal("raw original not preserved")
	}
	canonical, rev, err := loadCache(cfg.CachePath)
	if err != nil || rev != 2 {
		t.Fatal("final cache/revision mismatch")
	}
	if canonical.items[2] != nil || canonical.items[3] == nil || !bytes.Contains(canonical.items[1], []byte(`"lastJob":"LATEST"`)) || !bytes.Contains(canonical.meta["custom"], []byte(`1.2300e-7`)) {
		t.Fatal("latest local delta lost schema or IDs")
	}
	var marker map[string]string
	if json.Unmarshal(read(t, filepath.Join(filepath.Dir(cfg.CachePath), "todo.remote.json")), &marker) != nil || marker["bridge"] != bridgePath || marker["credentials"] != configPath {
		t.Fatal("invalid activation marker")
	}
	if bytes.Contains(read(t, filepath.Join(filepath.Dir(cfg.CachePath), "todo.remote.json")), []byte(cfg.SyncToken)) {
		t.Fatal("marker exposed credential")
	}
	l.mu.Lock()
	commits, releases := l.commits, l.releases
	l.mu.Unlock()
	if commits != 1 || releases != 1 {
		t.Fatal("cutover not one transaction and one release")
	}
	before := read(t, cfg.CachePath)
	if _, err = Cutover(context.Background(), cfg, configPath, bridgePath); err == nil {
		t.Fatal("active store cut over again")
	}
	if !bytes.Equal(read(t, cfg.CachePath), before) {
		t.Fatal("duplicate cutover changed cache")
	}
	assertNoCutoverStage(t, cfg)
}

func TestCutoverNetworkFailureLeavesOriginalAndMarkerInactive(t *testing.T) {
	_, cfg, configPath, bridgePath, original := cutoverFixture(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	cfg.BaseURL = "http://" + listener.Addr().String()
	_ = listener.Close()
	if err = SaveConfig(configPath, cfg); err != nil {
		t.Fatal(err)
	}
	result, err := Cutover(context.Background(), cfg, configPath, bridgePath)
	if err == nil || result.Activated {
		t.Fatal("network failure activated remote mode")
	}
	assertOriginalAndInactive(t, cfg, result.BackupPath, original)
	assertNoCutoverStage(t, cfg)
}

func TestCutoverUncertainAppliedCommitRetainsOriginalAndCanBeExplicitlyRetried(t *testing.T) {
	l, cfg, configPath, bridgePath, original := cutoverFixture(t)
	l.dropAllReplies = true
	l.operationStatus = 503
	result, err := Cutover(context.Background(), cfg, configPath, bridgePath)
	if err == nil || result.Activated {
		t.Fatal("uncertain commit activated marker")
	}
	assertOriginalAndInactive(t, cfg, result.BackupPath, original)
	if result.RecoveryPath == "" {
		t.Fatal("uncertain cutover did not retain its recovery directory")
	}
	if pending, err := readPending(filepath.Join(result.RecoveryPath, "todo.json")); err != nil || pending == nil {
		t.Fatal("uncertain cutover discarded the exact operation journal")
	}
	l.mu.Lock()
	commits, released := l.commits, l.releases
	l.dropAllReplies = false
	l.operationStatus = 0
	l.mu.Unlock()
	if commits != 1 || released != 1 {
		t.Fatal("uncertain commit duplicated data or leaked lease")
	}
	// Retry is a second explicit finite command, not a watcher or fallback. The
	// committed original IDs are observed as such and must not be created twice.
	retry, err := CutoverAtRevision(context.Background(), cfg, configPath, bridgePath, 2)
	if err != nil || !retry.Activated {
		t.Fatal("explicit retry did not recover")
	}
	if retry.BackupPath != result.BackupPath || !bytes.Equal(read(t, retry.BackupPath), original) {
		t.Fatal("retry discarded the original")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.commits != 1 || len(l.doc.items) != 2 || l.doc.items[3] == nil {
		t.Fatal("retry replayed final delta")
	}
}

func TestCutoverCommitConflictNeverReplacesLocalDocument(t *testing.T) {
	l, cfg, configPath, bridgePath, original := cutoverFixture(t)
	l.commitStatus = 409
	result, err := Cutover(context.Background(), cfg, configPath, bridgePath)
	if err == nil || result.Activated {
		t.Fatal("stale revision activated marker")
	}
	assertOriginalAndInactive(t, cfg, result.BackupPath, original)
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.commits != 0 || l.releases != 1 {
		t.Fatal("conflict mutated ledger or leaked lease")
	}
}

func TestCutoverRejectsCloudChangesAfterImport(t *testing.T) {
	l, cfg, configPath, bridgePath, original := cutoverFixture(t)
	l.mu.Lock()
	l.rev = 2
	l.doc.items[50] = json.RawMessage(`{"id":50,"title":"new web task"}`)
	l.mu.Unlock()
	result, err := Cutover(context.Background(), cfg, configPath, bridgePath)
	if err == nil || result.Activated {
		t.Fatal("changed cloud was overwritten")
	}
	assertOriginalAndInactive(t, cfg, result.BackupPath, original)
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.commits != 0 || l.doc.items[50] == nil {
		t.Fatal("cutover removed a cloud-only task")
	}
}

func TestCutoverExpectedRevisionCannotAuthorizeDeletingWebChanges(t *testing.T) {
	l, cfg, configPath, bridgePath, original := cutoverFixture(t)
	l.mu.Lock()
	l.rev = 2
	l.doc.items[50] = json.RawMessage(`{"id":50,"title":"new web task"}`)
	l.mu.Unlock()
	result, err := CutoverAtRevision(context.Background(), cfg, configPath, bridgePath, 2)
	if err == nil || result.Activated {
		t.Fatal("revision override discarded a cloud change")
	}
	assertOriginalAndInactive(t, cfg, result.BackupPath, original)
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.commits != 0 || l.doc.items[50] == nil {
		t.Fatal("cloud-only task was deleted")
	}
}

func TestCutoverUsesExistingByteLockAndVerifiesMarkerConfiguration(t *testing.T) {
	_, cfg, configPath, bridgePath, original := cutoverFixture(t)
	f, err := os.OpenFile(filepath.Join(filepath.Dir(cfg.CachePath), "todo.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err = lockFile(f); err != nil {
		t.Fatal(err)
	}
	if _, err = Cutover(context.Background(), cfg, configPath, bridgePath); err == nil {
		t.Fatal("cutover ignored byte lock")
	}
	unlockFile(f)
	wrong := cfg
	wrong.ClientID = "different-fixture"
	if _, err = Cutover(context.Background(), wrong, configPath, bridgePath); err == nil {
		t.Fatal("different activation config accepted")
	}
	if !bytes.Equal(read(t, cfg.CachePath), original) {
		t.Fatal("preflight failure changed original")
	}
}

func assertOriginalAndInactive(t *testing.T, cfg Config, backup string, original []byte) {
	t.Helper()
	if !bytes.Equal(read(t, cfg.CachePath), original) || !bytes.Equal(read(t, backup), original) {
		t.Fatal("failure lost original raw bytes")
	}
	for _, path := range []string{filepath.Join(filepath.Dir(cfg.CachePath), "todo.remote.json"), cfg.CachePath + ".remote-revision"} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("failure installed remote mode")
		}
	}
}
func assertNoCutoverStage(t *testing.T, cfg Config) {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(filepath.Dir(cfg.CachePath), ".todo-cutover-*"))
	if err != nil || len(paths) != 0 {
		t.Fatal("cutover staging cache left behind")
	}
}

func TestUnactivatedSyncCannotOverwriteLatestLocalDocument(t *testing.T) {
	_, cfg, _, _, original := cutoverFixture(t)
	if err := Sync(context.Background(), cfg); err == nil {
		t.Fatal("sync ran before cutover")
	}
	if !bytes.Equal(read(t, cfg.CachePath), original) {
		t.Fatal("unactivated sync changed latest local data")
	}
}

func TestCutoverRequiresInitialCloudImport(t *testing.T) {
	l, cfg, configPath, bridgePath, original := cutoverFixture(t)
	l.mu.Lock()
	l.rev = 0
	l.mu.Unlock()
	result, err := Cutover(context.Background(), cfg, configPath, bridgePath)
	if err == nil || result.Activated {
		t.Fatal("cutover skipped initial cloud import")
	}
	assertOriginalAndInactive(t, cfg, result.BackupPath, original)
}

func TestCutoverFiniteCommandAgainstSyntheticLedger(t *testing.T) {
	bridge := os.Getenv("TODO_REMOTE_TEST_BRIDGE")
	if bridge == "" {
		t.Skip("fresh bridge fixture binary not supplied")
	}
	l, cfg, configPath, _, original := cutoverFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bridge, "cutover", "--config", configPath, "--bridge", bridge)
	hideCommand(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("finite cutover command failed: %s", out)
	}
	var result CutoverResult
	if json.Unmarshal(out, &result) != nil || !result.Activated || !result.LeaseReleased {
		t.Fatal("finite command did not report completed activation/release")
	}
	if bytes.Contains(out, []byte(cfg.SyncToken)) || !bytes.Equal(read(t, result.BackupPath), original) {
		t.Fatal("finite command exposed credential or lost original")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.commits != 1 || l.releases != 1 {
		t.Fatal("finite command left work running")
	}
}
