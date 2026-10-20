package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func owned3107Fixture(t *testing.T) (owned3107Access, string) {
	t.Helper()
	dir, e := physical(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	root := filepath.Join(dir, "owned")
	if e = os.Mkdir(root, 0700); e != nil {
		t.Fatal(e)
	}
	target := filepath.Join(root, "known.log")
	if e = os.WriteFile(target, []byte("never read resource content"), 0600); e != nil {
		t.Fatal(e)
	}
	a := owned3107Access{root: root, expected: civ6Record(t, root), parent: civ6Record(t, dir),
		at: time.Date(2026, 10, 20, 13, 0, 0, 0, time.UTC), protected: func(string) bool { return false },
		plain: conversationPhysical, identity: func(p string) (audio2906Metadata, error) { return resourceIdentity(p, true) },
		qualified: func() bool { return true }, runtime: func(civ6Deployment) bool { return true }, official: func() bool { return true }}
	rootMeta, e := a.identity(root)
	if e != nil {
		t.Fatal(e)
	}
	targetMeta, e := a.identity(target)
	if e != nil {
		t.Fatal(e)
	}
	plan := object{"version": 1, "repo_id": 1392152644, "head": owned3107Head, "tree": owned3107Tree,
		"event": "pull_request", "branch": "scheduler/actions-cost-pr-concurrency-20261019", "pr": 149,
		"context_sha": owned3107Context, "workflow_sha": owned3107Context,
		"workflow_ref": "AutoDevJapan/civ6-banseiki/.github/workflows/rust.yml@refs/pull/149/merge",
		"workflow":     ".github/workflows/rust.yml", "workflow_digest": owned3107WorkflowSHA,
		"run_id": int64(39423559387), "attempt": 1, "job_id": int64(118630300541), "job": "test", "job_name": "test (windows-latest)"}
	entry := object{"key": owned3107Key, "plan": plan, "os": "Windows", "phase": "initial",
		"cleanup": object{"root": root, "safe_delete_id": owned3107ID, "instance": owned3107ID}}
	bodies := []any{
		object{"version": 2, "id": owned3107ID, "root": dir, "path": root, "volume": rootMeta.Volume, "file_id": rootMeta.ID,
			"created_at": time.Unix(0, (rootMeta.Created-116444736000000000)*100).UTC().Format(time.RFC3339Nano),
			"project":    "civ6-ci-queue", "content": "job-39423559387-1-118630300541", "state": "owned"},
		object{"source_head": owned3107SourceFloor, "files": []any{object{"name": "civ6-ci-queue.exe", "sha256": owned3107QueueSHA}, object{"name": "civ6-ci-windows.exe", "sha256": owned3107HelperSHA}}},
		object{"Head": owned3107Head, "Tree": owned3107Tree, "BranchVerified": true},
		object{"version": 1, "entries": object{owned3107Key: entry}},
		object{"data_read": false, "attribute_walk_complete": true, "objects": map[string]audio2906Metadata{".": rootMeta, "known.log": targetMeta}},
		civ6Deployment{Domain: "civ6-ci-2962-deployment-v1", ContractSHA256: hashBytes(civ6Contract), SourceHead: owned3107SourceFloor, Objects: map[string]civ6Object{
			"civ6-ci-queue.exe": {SHA256: owned3107QueueSHA}, "civ6-ci-windows.exe": {SHA256: owned3107HelperSHA}}},
	}
	for i, v := range bodies {
		p := filepath.Join(dir, "proof-"+string(rune('a'+i))+".json")
		b, e := json.Marshal(v)
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(p, b, 0600); e != nil {
			t.Fatal(e)
		}
		a.proofs = append(a.proofs, owned3107Proof{p, hashBytes(b), 4 << 20})
	}
	a.read = func(p string, n int64) ([]byte, bool) {
		if within(root, p) {
			t.Fatal("resource body opened")
		}
		return conversationBytes(p, n, func(string) bool { return false })
	}
	return a, target
}

func TestOwned3107ExactResourceAndRecordedChild(t *testing.T) {
	a, target := owned3107Fixture(t)
	if !owned3107Allowed(a.root, a) || !owned3107Allowed(target, a) {
		t.Fatal("bound resource rejected")
	}
	for _, p := range []string{filepath.Dir(a.root), a.root + "-sibling", filepath.Join(a.root, "absent"), owned2736Root, civ6Source} {
		b := a
		b.qualified = func() bool { t.Fatal("unrelated path qualified"); return false }
		if owned3107Allowed(p, b) {
			t.Fatal("unknown boundary accepted")
		}
	}
	fresh := filepath.Join(a.root, "new.log")
	if e := os.WriteFile(fresh, []byte("new"), 0600); e != nil {
		t.Fatal(e)
	}
	if owned3107Allowed(fresh, a) {
		t.Fatal("new unrecorded child adopted")
	}
	if e := os.WriteFile(target, []byte("changed"), 0600); e != nil {
		t.Fatal(e)
	}
	if owned3107Allowed(target, a) {
		t.Fatal("changed child adopted")
	}
}

func TestOwned3107QualificationFailsClosed(t *testing.T) {
	for _, which := range []string{"source", "runtime", "official", "expired", "before", "root-id", "parent-id", "root-owner", "birth", "deny-root", "deny-proof", "deny-parent", "missing-proof", "changed-proof"} {
		t.Run(which, func(t *testing.T) {
			a, _ := owned3107Fixture(t)
			switch which {
			case "source":
				a.qualified = func() bool { return false }
			case "runtime":
				a.runtime = func(civ6Deployment) bool { return false }
			case "official":
				a.official = func() bool { return false }
			case "expired":
				a.at = time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)
			case "before":
				a.at = time.Date(2026, 10, 20, 12, 38, 0, 0, time.UTC)
			case "root-id":
				a.expected.ID++
			case "parent-id":
				a.parent.ID++
			case "root-owner":
				a.expected.Owner = "other"
			case "birth":
				a.expected.Created++
			case "deny-root":
				a.protected = func(p string) bool { return same(p, a.root) }
			case "deny-proof":
				a.protected = func(p string) bool { return same(p, a.proofs[0].path) }
			case "deny-parent":
				a.protected = func(p string) bool { return same(p, filepath.Dir(a.root)) }
			case "missing-proof":
				if e := os.Remove(a.proofs[0].path); e != nil {
					t.Fatal(e)
				}
			case "changed-proof":
				if e := os.WriteFile(a.proofs[0].path, []byte("{}"), 0600); e != nil {
					t.Fatal(e)
				}
			}
			if owned3107Allowed(a.root, a) {
				t.Fatal("unqualified resource accepted")
			}
		})
	}
}

func TestOwned3107RejectsHardlinkReparseAndReplacement(t *testing.T) {
	a, target := owned3107Fixture(t)
	if e := os.Link(target, target+"-alias"); e != nil {
		t.Fatal(e)
	}
	if owned3107Allowed(target, a) {
		t.Fatal("hardlink accepted")
	}
	if e := os.Link(a.proofs[0].path, a.proofs[0].path+"-alias"); e != nil {
		t.Fatal(e)
	}
	if owned3107Allowed(a.root, a) {
		t.Fatal("hardlinked proof accepted")
	}
	a, _ = owned3107Fixture(t)
	identity := a.identity
	a.identity = func(p string) (audio2906Metadata, error) {
		m, e := identity(p)
		if same(p, a.root) {
			m.Attributes |= 0x400
		}
		return m, e
	}
	if owned3107Allowed(a.root, a) {
		t.Fatal("reparse accepted")
	}
	a, _ = owned3107Fixture(t)
	if e := os.Rename(a.root, a.root+"-old"); e != nil {
		t.Fatal(e)
	}
	if e := os.Mkdir(a.root, 0700); e != nil {
		t.Fatal(e)
	}
	if owned3107Allowed(a.root, a) {
		t.Fatal("replacement adopted")
	}
}

func TestOwned3107ProofBindings(t *testing.T) {
	for _, change := range []struct {
		i        int
		from, to string
	}{
		{0, owned3107ID, strings.Repeat("f", 32)}, {0, "civ6-ci-queue", "unrelated"},
		{1, owned3107SourceFloor, strings.Repeat("f", 40)}, {2, owned3107Head, strings.Repeat("f", 40)},
		{3, `"repo_id":1392152644`, `"repo_id":1`}, {3, `"attempt":1`, `"attempt":2`},
		{3, `"instance":"` + owned3107ID + `"`, `"instance":"other"`},
	} {
		a, _ := owned3107Fixture(t)
		p := a.proofs[change.i].path
		b, e := os.ReadFile(p)
		if e != nil {
			t.Fatal(e)
		}
		changed := strings.Replace(string(b), change.from, change.to, 1)
		if changed == string(b) {
			t.Fatal("mutation ineffective")
		}
		if e = os.WriteFile(p, []byte(changed), 0600); e != nil {
			t.Fatal(e)
		}
		a.proofs[change.i].sha = hashBytes([]byte(changed)) // even a matching byte pin needs semantic binding
		if owned3107Allowed(a.root, a) {
			t.Fatal("unbound proof accepted")
		}
	}
}

func TestOwned3107RechecksAfterExternalVerification(t *testing.T) {
	for _, which := range []string{"root", "deny", "proof"} {
		t.Run(which, func(t *testing.T) {
			a, target := owned3107Fixture(t)
			changed := false
			id := a.identity
			a.identity = func(p string) (audio2906Metadata, error) {
				m, e := id(p)
				if changed && which == "root" && same(p, a.root) {
					m.ID++
				}
				return m, e
			}
			a.protected = func(p string) bool { return changed && which == "deny" && same(p, target) }
			a.official = func() bool {
				changed = true
				if which == "proof" {
					if e := os.WriteFile(a.proofs[0].path, []byte("{}"), 0600); e != nil {
						t.Fatal(e)
					}
				}
				return true
			}
			if owned3107Allowed(target, a) {
				t.Fatal("change during verification accepted")
			}
		})
	}
}

func TestOwned3107OfficialImmutableIdentity(t *testing.T) {
	run := object{"id": uint64(39423559387), "run_attempt": 1, "head_sha": owned3107Head, "path": ".github/workflows/rust.yml", "event": "pull_request", "repository_id": uint64(1392152644), "head_repository_id": uint64(1392152644)}
	job := object{"id": uint64(118630300541), "run_id": uint64(39423559387), "run_attempt": 1, "head_sha": owned3107Head, "name": "test (windows-latest)", "labels": []any{"self-hosted", "Windows", "X64", "civ6-ci-windows", "civ6-ci-run-39423559387-1-test-Windows"}}
	if !owned3107OfficialRunMatches(run) || !owned3107OfficialJobMatches(job) {
		t.Fatal("original run/job rejected")
	}
	for k := range run {
		original := run[k]
		run[k] = "other"
		if owned3107OfficialRunMatches(run) {
			t.Fatal("unbound run accepted")
		}
		run[k] = original
	}
	for k := range job {
		original := job[k]
		job[k] = "other"
		if owned3107OfficialJobMatches(job) {
			t.Fatal("unbound job accepted")
		}
		job[k] = original
	}
}
