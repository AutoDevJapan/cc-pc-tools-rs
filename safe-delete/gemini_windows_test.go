//go:build windows

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sharedFixture(t *testing.T) (config, receipt, *geminiScope) {
	t.Helper()
	c, root := fixture(t)
	r := owned(t, c, root)
	external := filepath.Join(root, "completed.mp4")
	write(t, external, "completed media")
	if err := os.Link(external, filepath.Join(r.Path, "verified-preview.mp4")); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(r.Path, "scratch.txt"), "scratch")
	i, err := fileIdentity(external)
	if err != nil {
		t.Fatal(err)
	}
	h, err := contentHash(external)
	if err != nil {
		t.Fatal(err)
	}
	return c, r, &geminiScope{external: external, identity: i, hash: h, entries: 2, bytes: 22}
}

func TestSharedMediaQuarantinePreservesBothNamesAndReceipt(t *testing.T) {
	c, r, s := sharedFixture(t)
	// Fixture scopes are only passed internally by tests. The public command
	// refuses this receipt even though its shared-file layout is valid.
	_, err := c.quarantine(r.ID, "hardlink")
	code(t, err, "HARDLINK_RECOVERY_SCOPE_MISMATCH")
	_, err = c.plan(r.ID)
	code(t, err, "HARD_LINK")
	q, err := c.quarantineScoped(r.ID, "hardlink", s)
	if err != nil {
		t.Fatal(err)
	}
	dest := q["destination"].(string)
	if exists(r.Path) || !exists(filepath.Join(dest, "preservation.json")) {
		t.Fatal(q)
	}
	i, err := fileIdentity(filepath.Join(dest, "files", "verified-preview.mp4"))
	if err != nil || geminiMetadata(i) != geminiMetadata(s.identity) {
		t.Fatal("shared file identity or link count changed", err)
	}
	if err = s.externalUnchanged(); err != nil {
		t.Fatal(err)
	}
	p, _ := c.recordPath(r.ID)
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var saved receipt
	if err = json.Unmarshal(b, &saved); err != nil || saved.State != "quarantined" || saved.Destination != dest || saved.Integrity != r.Integrity {
		t.Fatal("receipt not committed with original identity", err)
	}
}

func TestSharedScopeRejectsAdditionalExternalName(t *testing.T) {
	c, r, s := sharedFixture(t)
	out := filepath.Join(filepath.Dir(r.Path), "unrelated.txt")
	write(t, out, "other")
	if err := os.Link(out, filepath.Join(r.Path, "unrelated.txt")); err != nil {
		t.Fatal(err)
	}
	_, err := c.quarantineScoped(r.ID, "hardlink", s)
	code(t, err, "HARD_LINK")
	if !exists(r.Path) || exists(c.Quarantine) {
		t.Fatal("scope rejection moved data")
	}
}

func TestSharedScopeRejectsChangedContentWithRestoredTime(t *testing.T) {
	_, r, s := sharedFixture(t)
	info, err := os.Stat(s.external)
	if err != nil {
		t.Fatal(err)
	}
	write(t, s.external, "different media")
	if err = os.Chtimes(s.external, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	_, err = s.snapshot(r.Path)
	code(t, err, "SHARED_MEDIA_CONTENT_CHANGED")
}

func TestSharedScopeRejectsRenameOrIdentityReplacement(t *testing.T) {
	c, r, s := sharedFixture(t)
	if err := os.Rename(filepath.Join(r.Path, "verified-preview.mp4"), filepath.Join(r.Path, "renamed.mp4")); err != nil {
		t.Fatal(err)
	}
	_, err := c.quarantineScoped(r.ID, "hardlink", s)
	code(t, err, "HARD_LINK")
	if !exists(r.Path) || exists(c.Quarantine) {
		t.Fatal("scope rejection moved data")
	}
}

func TestSharedScopeVerifyRejectsOtherFileContentChanges(t *testing.T) {
	_, r, s := sharedFixture(t)
	before, err := s.snapshot(r.Path)
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(r.Path, "scratch.txt"), "changed")
	err = s.verify(r.Path, before, r)
	code(t, err, "PRESERVATION_MISMATCH")
}

func TestSharedScopeRequiresPinnedSharedName(t *testing.T) {
	_, r, s := sharedFixture(t)
	// Keep the original two-link identity and the same inventory capacity, but
	// put the second shared name outside the owned folder. A regular replacement
	// of the expected name must not pass the special recovery.
	inside := filepath.Join(r.Path, "verified-preview.mp4")
	if err := os.Rename(inside, filepath.Join(filepath.Dir(r.Path), "other-shared.mp4")); err != nil {
		t.Fatal(err)
	}
	write(t, inside, "completed media")
	_, err := s.snapshot(r.Path)
	code(t, err, "SHARED_MEDIA_IDENTITY_CHANGED")
}

func TestSharedQuarantineRecordFailureDoesNotClaimCompletion(t *testing.T) {
	c, r, s := sharedFixture(t)
	p, err := c.recordPath(r.ID)
	if err != nil {
		t.Fatal(err)
	}
	write(t, p+".writing", "interrupted prior update")
	_, err = c.quarantineScoped(r.ID, "hardlink", s)
	e := code(t, err, "RECEIPT_WRITE_FAILED")
	dest := e.Details["destination"].(string)
	why, err := os.ReadFile(filepath.Join(dest, "理由.txt"))
	if err != nil || strings.Contains(string(why), "退避状態: 完了") {
		t.Fatal("failed receipt update claimed completion", err)
	}
	if exists(r.Path) || !exists(filepath.Join(dest, "files", "verified-preview.mp4")) {
		t.Fatal("moved data missing")
	}
	if err = s.externalUnchanged(); err != nil {
		t.Fatal(err)
	}
}
