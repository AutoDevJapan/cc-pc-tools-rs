//go:build windows

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConsole3451RequiresTheOriginalProducerIdentity(t *testing.T) {
	var r receipt
	raw := `{"version":2,"id":"8810a8e6743a392c14257036d9e13619","root":"C:\\Users\\makhmeto\\AppData\\Local\\Temp","path":"C:\\Users\\makhmeto\\AppData\\Local\\Temp\\codex-owned-8810a8e6743a392c14257036d9e13619","volume":2229119083,"file_id":20547673302021654,"created_at":"2026-10-23T21:55:15.4048148+09:00","project":"localai-console","content":"ui3450-ci","integrity":"117f29851c129d28f1fcd162a6835aa83887708ce03e488e5dc45af9e6ac6ce3","state":"owned"}`
	if json.Unmarshal([]byte(raw), &r) != nil || !console3451Receipt(r) {
		t.Fatal("original receipt rejected")
	}
	for _, edit := range []func(*receipt){func(x *receipt) { x.ID = strings.Repeat("a", 32) }, func(x *receipt) { x.Path += "-other" }, func(x *receipt) { x.FileID++ }, func(x *receipt) { x.Project = "other" }, func(x *receipt) { x.Integrity = "changed" }, func(x *receipt) { x.State = "quarantined" }} {
		copy := r
		edit(&copy)
		if console3451Receipt(copy) {
			t.Fatal("cross-scope receipt accepted")
		}
	}
}
func TestConsole3451PreservesOnlyTheDeclaredGitAndRejectsExternalAliases(t *testing.T) {
	root := t.TempDir()
	git := filepath.Join(root, filepath.FromSlash(console3451Git))
	if e := os.MkdirAll(git, 0700); e != nil {
		t.Fatal(e)
	}
	write(t, filepath.Join(git, "HEAD"), "fixture")
	meta, e := ci3174Identity(root)
	if e != nil {
		t.Fatal(e)
	}
	s := &ci3174Scope{maxEntries: 10000, maxBytes: 16 << 30, identity: ci3174Identity, volume: meta.Volume, rootID: meta.ID, rootBirth: ci3174Birth(meta), captureCollected: true, internalLinks: true, preservedGit: console3451Git, createdOwner: meta.Owner}
	inventory, _, e := s.snapshot(root)
	if e != nil || len(inventory) == 0 {
		t.Fatalf("declared Git rejected: %v", e)
	}
	s.createdOwner = "S-1-0-0"
	_, _, e = s.snapshot(root)
	code(t, e, "CI_OBJECT_CHANGED")
	s.createdOwner = meta.Owner
	if e = os.MkdirAll(filepath.Join(root, "other", ".git"), 0700); e != nil {
		t.Fatal(e)
	}
	_, _, e = s.snapshot(root)
	code(t, e, "PROTECTED_PATH")
}
func TestConsole3451RequiresFinishedMatchingOfficialAcceptance(t *testing.T) {
	var p console3451Acceptance
	raw := `{"version":1,"repository":"AutoDevJapan/local-ai-console","repository_id":1429803672,"head":"1111111111111111111111111111111111111111","tree":"2222222222222222222222222222222222222222","run":{"id":1,"workflow_id":392978784,"path":".github/workflows/windows.yml","head_sha":"1111111111111111111111111111111111111111","status":"completed","conclusion":"success","event":"pull_request","repository":{"id":1429803672},"head_repository":{"id":1429803672}},"job":{"id":2,"run_id":1,"head_sha":"1111111111111111111111111111111111111111","status":"completed","conclusion":"success","name":"acceptance"},"pr":{"merged":true,"merge_commit_sha":"3333333333333333333333333333333333333333","head":{"sha":"1111111111111111111111111111111111111111","repo":{"id":1429803672}},"base":{"ref":"main","repo":{"id":1429803672}}},"main":{"commit":{"sha":"3333333333333333333333333333333333333333","commit":{"tree":{"sha":"2222222222222222222222222222222222222222"}}}},"installed":{"binary_installed":true,"restart_requested":true,"source":"1111111111111111111111111111111111111111","tree":"2222222222222222222222222222222222222222"}}`
	if json.Unmarshal([]byte(raw), &p) != nil || !console3451AcceptanceValid(p) {
		t.Fatal("matching acceptance rejected")
	}
	parts := map[string][]byte{}
	parts["run"], _ = json.Marshal(p.Run)
	parts["job"], _ = json.Marshal(map[string]any{"jobs": []any{p.Job}})
	parts["pr"], _ = json.Marshal(p.PR)
	parts["main"], _ = json.Marshal(p.Main)
	if _, e := console3451ApplyOfficial(p, parts); e != nil {
		t.Fatal(e)
	}
	var incomplete map[string]any
	_ = json.Unmarshal(parts["run"], &incomplete)
	delete(incomplete, "Conclusion")
	delete(incomplete, "conclusion")
	parts["run"], _ = json.Marshal(incomplete)
	if _, e := console3451ApplyOfficial(p, parts); e == nil {
		t.Fatal("missing official conclusion inherited a summary claim")
	}
	for _, edit := range []func(*console3451Acceptance){func(x *console3451Acceptance) { x.Run.Status = "in_progress" }, func(x *console3451Acceptance) { x.Job.Conclusion = "failure" }, func(x *console3451Acceptance) { x.Job.RunID++ }, func(x *console3451Acceptance) { x.PR.Merged = false }, func(x *console3451Acceptance) { x.PR.Head.Repo.ID++ }, func(x *console3451Acceptance) { x.Installed.Source = "unrelated" }} {
		copy := p
		edit(&copy)
		if console3451AcceptanceValid(copy) {
			t.Fatal("unverified acceptance accepted")
		}
	}
}
