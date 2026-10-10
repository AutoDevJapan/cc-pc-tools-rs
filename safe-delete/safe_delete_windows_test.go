//go:build windows

package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func fixture(t *testing.T) (config, string) {
	t.Helper()
	root := t.TempDir()
	return config{Store: filepath.Join(root, "receipts"), Quarantine: filepath.Join(root, "quarantine"), Temp: root, MaxEntries: 10000, MaxBytes: 512 * 1024 * 1024}, root
}
func owned(t *testing.T, c config, root string) receipt {
	t.Helper()
	r, err := c.create(root, "test", "scratch")
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func code(t *testing.T, err error, want string) *failure {
	t.Helper()
	var e *failure
	if !errors.As(err, &e) || e.Code != want {
		t.Fatalf("got %v, want %s", err, want)
	}
	return e
}
func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
}
func modify(t *testing.T, c config, r receipt) {
	t.Helper()
	p, err := c.recordPath(r.ID)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(r)
	if os.WriteFile(p, b, 0600) != nil {
		t.Fatal("write test receipt")
	}
}

func TestVisibleDeletionPlanAndFinish(t *testing.T) {
	c, root := fixture(t)
	r := owned(t, c, root)
	child := filepath.Join(r.Path, "child")
	if os.Mkdir(child, 0700) != nil {
		t.Fatal("mkdir")
	}
	write(t, filepath.Join(child, "file.txt"), "disposable")
	p, err := c.plan(r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if p["bytes"] != int64(10) || p["entries"] != 2 {
		t.Fatal(p)
	}
	cmd := p["powershell_command"].(string)
	if strings.Contains(cmd, "-Force") {
		t.Fatal("force deletion in visible plan")
	}
	if _, err = runHidden(15*time.Second, "pwsh.exe", "-NoProfile", "-Command", cmd); err != nil {
		t.Fatal(err)
	}
	if exists(r.Path) {
		t.Fatal("target remained")
	}
	f, err := c.finish(r.ID)
	if err != nil || f["state"] != "source_absent" {
		t.Fatal(f, err)
	}
	_, err = c.plan(r.ID)
	code(t, err, "RECEIPT_NOT_OWNED")
}

func TestContainedHardLinksAreAllowedAndRemainSubjectToSizeLimit(t *testing.T) {
	c, root := fixture(t)
	r := owned(t, c, root)
	dir := filepath.Join(r.Path, "debug")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	first := filepath.Join(dir, "app.exe")
	second := filepath.Join(dir, "deps", "app-hash.exe")
	if err := os.Mkdir(filepath.Dir(second), 0700); err != nil {
		t.Fatal(err)
	}
	write(t, first, "binary")
	if err := os.Link(first, second); err != nil {
		t.Fatal(err)
	}
	p, err := c.plan(r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if p["action"] != "submit_to_exec_tool" || p["bytes"] != int64(12) {
		t.Fatalf("contained links should be measured by each owned name: %#v", p)
	}
	c.MaxBytes = 8
	p, err = c.plan(r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if p["action"] != "quarantine" || p["reason"] != "size" || p["bytes"] != int64(12) {
		t.Fatalf("size limit must still route the owned tree to quarantine: %#v", p)
	}
	if !exists(first) || !exists(second) {
		t.Fatal("planning must not change the owned tree")
	}
}

func TestHardLinkOutsideOwnedRootRemainsRefused(t *testing.T) {
	c, root := fixture(t)
	r := owned(t, c, root)
	inside := filepath.Join(r.Path, "owned.exe")
	outside := filepath.Join(root, "outside.exe")
	write(t, outside, "binary")
	if err := os.Link(outside, inside); err != nil {
		t.Fatal(err)
	}
	_, err := c.plan(r.ID)
	code(t, err, "HARD_LINK")
	_, err = c.quarantine(r.ID, "policy")
	code(t, err, "HARD_LINK")
	if !exists(inside) || !exists(outside) {
		t.Fatal("external hard link rejection must preserve both names")
	}
}
func TestRootTamperingRefused(t *testing.T) {
	c, root := fixture(t)
	r := owned(t, c, root)
	r.Path = root
	modify(t, c, r)
	_, err := c.plan(r.ID)
	code(t, err, "RECEIPT_INTEGRITY_MISMATCH")
	if !exists(root) {
		t.Fatal("root lost")
	}
}
func TestIdentityTamperingRefused(t *testing.T) {
	c, root := fixture(t)
	r := owned(t, c, root)
	r.FileID++
	modify(t, c, r)
	_, err := c.plan(r.ID)
	code(t, err, "RECEIPT_INTEGRITY_MISMATCH")
}
func TestActualFolderReplacementRefused(t *testing.T) {
	c, root := fixture(t)
	r := owned(t, c, root)
	if os.Rename(r.Path, filepath.Join(root, "original")) != nil {
		t.Fatal("rename")
	}
	if os.Mkdir(r.Path, 0700) != nil {
		t.Fatal("mkdir")
	}
	_, err := c.plan(r.ID)
	code(t, err, "TARGET_IDENTITY_CHANGED")
}
func TestReceiptTraversalRefused(t *testing.T) {
	c, _ := fixture(t)
	_, err := c.load("../../auth", true)
	code(t, err, "INVALID_RECEIPT_ID")
}
func TestEditedLabelStopsQuarantine(t *testing.T) {
	c, root := fixture(t)
	r := owned(t, c, root)
	r.Project = "other"
	modify(t, c, r)
	_, err := c.quarantine(r.ID, "policy")
	code(t, err, "RECEIPT_INTEGRITY_MISMATCH")
	if !exists(r.Path) || exists(c.Quarantine) {
		t.Fatal("unexpected movement")
	}
}
func TestCreateFailureReportsLeftover(t *testing.T) {
	c, root := fixture(t)
	c.Store = filepath.Join(root, "store-is-file")
	write(t, c.Store, "block")
	r, err := c.create(root, "test", "scratch")
	e := code(t, err, "OWNERSHIP_RECORD_WRITE_FAILED")
	if e.Details["path"] != r.Path || e.Details["id"] != r.ID || !exists(r.Path) {
		t.Fatal(e, r)
	}
}
func TestGitMetadataStopsPlan(t *testing.T) {
	c, root := fixture(t)
	r := owned(t, c, root)
	if os.Mkdir(filepath.Join(r.Path, ".git"), 0700) != nil {
		t.Fatal("mkdir")
	}
	_, err := c.plan(r.ID)
	code(t, err, "PROTECTED_PATH")
}
func TestJunctionStopsPlanAndQuarantine(t *testing.T) {
	c, root := fixture(t)
	r := owned(t, c, root)
	external := t.TempDir()
	proof := filepath.Join(external, "keep.txt")
	write(t, proof, "keep")
	if _, err := runHidden(10*time.Second, "cmd.exe", "/c", "mklink", "/J", filepath.Join(r.Path, "link"), external); err != nil {
		t.Fatal(err)
	}
	_, err := c.plan(r.ID)
	code(t, err, "LINK_OR_REPARSE_POINT")
	_, err = c.quarantine(r.ID, "policy")
	code(t, err, "LINK_OR_REPARSE_POINT")
	b, err := os.ReadFile(proof)
	if err != nil || string(b) != "keep" {
		t.Fatal("external file lost")
	}
}
func TestReadonlyHiddenFileQuarantined(t *testing.T) {
	c, root := fixture(t)
	r := owned(t, c, root)
	f := filepath.Join(r.Path, "protected.txt")
	write(t, f, "protected")
	if _, err := runHidden(10*time.Second, "attrib.exe", "+R", "+H", f); err != nil {
		t.Fatal(err)
	}
	p, err := c.plan(r.ID)
	if err != nil || p["action"] != "quarantine" || p["reason"] != "attributes" {
		t.Fatal(p, err)
	}
	q, err := c.quarantine(r.ID, "attributes")
	if err != nil {
		t.Fatal(err)
	}
	dest := q["destination"].(string)
	b, err := os.ReadFile(filepath.Join(dest, "files", "protected.txt"))
	if err != nil || string(b) != "protected" || exists(r.Path) {
		t.Fatal("move failed")
	}
	why, err := os.ReadFile(filepath.Join(dest, "理由.txt"))
	if err != nil || !strings.Contains(string(why), r.Path) || !strings.Contains(string(why), "9 byte") || !strings.Contains(string(why), "退避状態: 完了") {
		t.Fatal("reason missing")
	}
	_, err = c.quarantine(r.ID, "policy")
	code(t, err, "RECEIPT_NOT_OWNED")
}
func TestFinishRefusesExistingTarget(t *testing.T) {
	c, root := fixture(t)
	r := owned(t, c, root)
	_, err := c.finish(r.ID)
	code(t, err, "TARGET_STILL_EXISTS")
}
func TestFinishDoesNotClaimDeletionAfterMove(t *testing.T) {
	c, root := fixture(t)
	r := owned(t, c, root)
	dest := filepath.Join(root, "moved")
	if os.Rename(r.Path, dest) != nil {
		t.Fatal("rename")
	}
	f, err := c.finish(r.ID)
	if err != nil || f["state"] != "source_absent" || !exists(dest) {
		t.Fatal(f, err)
	}
}
func TestSizeAndEntryLimits(t *testing.T) {
	c, root := fixture(t)
	r := owned(t, c, root)
	write(t, filepath.Join(r.Path, "large"), "12345")
	c.MaxBytes = 4
	p, err := c.plan(r.ID)
	if err != nil || p["reason"] != "size" {
		t.Fatal(p, err)
	}
	c.MaxEntries = 0
	_, err = c.plan(r.ID)
	code(t, err, "ENTRY_LIMIT_EXCEEDED")
}
func TestCrossDriveFailureReportsCapacity(t *testing.T) {
	c, root := fixture(t)
	r := owned(t, c, root)
	write(t, filepath.Join(r.Path, "keep.txt"), "keep")
	drive := "D:"
	if same(filepath.VolumeName(root), drive) {
		drive = "C:"
	}
	c.Quarantine = drive + `\safe-delete-destination-never-created`
	_, err := c.quarantine(r.ID, "policy")
	e := code(t, err, "QUARANTINE_CROSS_DRIVE")
	if e.Details["bytes"] != int64(4) || len(e.Details["drive_space"].([]map[string]any)) == 0 || !exists(r.Path) || exists(c.Quarantine) {
		t.Fatal(e)
	}
}
func TestCollisionDoesNotOverwriteEvidence(t *testing.T) {
	c, root := fixture(t)
	r := owned(t, c, root)
	dest := filepath.Join(c.Quarantine, time.Now().Format("20060102")+"-test-scratch-"+r.ID[:8])
	if os.MkdirAll(dest, 0700) != nil {
		t.Fatal("mkdir")
	}
	proof := filepath.Join(dest, "keep.txt")
	write(t, proof, "keep")
	_, err := c.quarantine(r.ID, "policy")
	code(t, err, "QUARANTINE_DESTINATION_CREATE_FAILED")
	b, e := os.ReadFile(proof)
	if e != nil || string(b) != "keep" || !exists(r.Path) {
		t.Fatal("overwrote evidence")
	}
}
func TestGitRootAdmission(t *testing.T) {
	c, root := fixture(t)
	repo := filepath.Join(root, "fixture-repo")
	if _, err := runHidden(10*time.Second, "git", "init", "--quiet", repo); err != nil {
		t.Fatal(err)
	}
	c.Temp = filepath.Join(root, "only-allowed-temp")
	if os.Mkdir(c.Temp, 0700) != nil {
		t.Fatal("mkdir")
	}
	r, err := c.create(repo, "test", "worktree")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.plan(r.ID); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(repo, "nested")
	if os.Mkdir(nested, 0700) != nil {
		t.Fatal("mkdir")
	}
	_, err = c.create(nested, "test", "nested")
	code(t, err, "ROOT_MUST_BE_TEMP_OR_WORKTREE_TOP")
}
func TestShellMissingReportsLocationsBeforeDestinationCreated(t *testing.T) {
	c, root := fixture(t)
	r := owned(t, c, root)
	c.FindShell = func() (string, error) { return "", os.ErrNotExist }
	_, err := c.quarantine(r.ID, "policy")
	e := code(t, err, "POWERSHELL_NOT_FOUND")
	if e.Details["source"] != r.Path || e.Details["destination"] == nil || exists(c.Quarantine) || !exists(r.Path) {
		t.Fatal(e)
	}
}
func TestReasonWriteFailureReportsExistingDestination(t *testing.T) {
	c, root := fixture(t)
	r := owned(t, c, root)
	c.WriteReason = func(string, []byte) error { return os.ErrPermission }
	_, err := c.quarantine(r.ID, "policy")
	e := code(t, err, "QUARANTINE_REASON_WRITE_FAILED")
	if e.Details["phase"] != "reason" || !exists(e.Details["destination"].(string)) || !exists(r.Path) {
		t.Fatal(e)
	}
}
func TestInterruptedRecordUpdateKeepsValidOldReceipt(t *testing.T) {
	c, root := fixture(t)
	r := owned(t, c, root)
	p, _ := c.recordPath(r.ID)
	write(t, p+".writing", "interrupted")
	r.State = "source_absent"
	err := c.save(r, false)
	code(t, err, "RECEIPT_WRITE_FAILED")
	old, err := c.load(r.ID, true)
	if err != nil || old.State != "owned" {
		t.Fatal(old, err)
	}
}

func TestLockedDirectoryMoveFailureIsMarkedIncomplete(t *testing.T) {
	c, root := fixture(t)
	r := owned(t, c, root)
	write(t, filepath.Join(r.Path, "keep.txt"), "keep")
	u, _ := syscall.UTF16PtrFromString(r.Path)
	h, err := syscall.CreateFile(u, syscall.GENERIC_READ, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE,
		nil, syscall.OPEN_EXISTING, syscall.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.CloseHandle(h)
	_, err = c.quarantine(r.ID, "locked")
	e := code(t, err, "QUARANTINE_MOVE_FAILED")
	if !exists(r.Path) || e.Details["phase"] != "move" {
		t.Fatal(e)
	}
	why, err := os.ReadFile(filepath.Join(e.Details["destination"].(string), "理由.txt"))
	if err != nil || !strings.Contains(string(why), "退避状態: 未完了") || strings.Contains(string(why), "退避状態: 完了") {
		t.Fatal("misreported move status", err)
	}
}
