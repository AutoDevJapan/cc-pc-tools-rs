package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
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
		{3, `"pr":149`, `"pr":150`},
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
	for _, which := range []string{"root", "deny", "proof", "expiry"} {
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
			a.now = func() time.Time {
				if changed && which == "expiry" {
					return time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)
				}
				return a.at
			}
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
			if !changed {
				t.Fatal("fixture did not reach external verification")
			}
		})
	}
}

func TestOwned3107OfficialImmutableIdentity(t *testing.T) {
	run := object{"id": uint64(39423559387), "run_attempt": 1, "head_sha": owned3107Head, "path": ".github/workflows/rust.yml", "event": "pull_request", "head_branch": "scheduler/actions-cost-pr-concurrency-20261019", "repository_id": uint64(1392152644), "head_repository_id": uint64(1392152644)}
	job := object{"id": uint64(118630300541), "run_id": uint64(39423559387), "run_attempt": 1, "head_sha": owned3107Head, "name": "test (windows-latest)", "labels": []any{"self-hosted", "Windows", "X64", "civ6-ci-windows", "civ6-ci-run-39423559387-1-test-Windows"}}
	// The real API path uses the strict JSON reader's json.Number values.
	for _, v := range []*object{&run, &job} {
		b, e := json.Marshal(*v)
		if e != nil {
			t.Fatal(e)
		}
		var ok bool
		*v, ok = conversationJSON(b)
		if !ok {
			t.Fatal("fixture JSON invalid")
		}
	}
	if !owned3107OfficialRunMatches(run) || !owned3107OfficialJobMatches(job) {
		t.Fatal("original run/job rejected")
	}
	for _, path := range []string{".github/workflows/rust.yml", ".github/workflows/rust.yml@refs/pull/149/merge"} {
		run["path"] = path
		if !owned3107OfficialRunMatches(run) {
			t.Fatal("original workflow representation rejected")
		}
	}
	for _, path := range []string{".github/workflows/rust.yml@refs/pull/150/merge", ".github/workflows/rust.yml@refs/heads/main", ".github/workflows/rust.yml@@refs/pull/149/merge", ".github/workflows/other.yml"} {
		run["path"] = path
		if owned3107OfficialRunMatches(run) {
			t.Fatal("unrelated workflow or ref accepted")
		}
	}
	run["path"] = ".github/workflows/rust.yml"
	for k := range run {
		original := run[k]
		run[k] = "other"
		if owned3107OfficialRunMatches(run) {
			t.Fatal("unbound run accepted")
		}
		run[k] = original
	}
	// A present-day PR association is not the historical creation authority.
	// Its absence or change must neither revoke nor grant this fixed run.
	for _, prs := range [][]any{nil, {json.Number("149")}, {json.Number("150")}, {json.Number("149"), json.Number("150")}} {
		run["prs"] = prs
		if !owned3107OfficialRunMatches(run) {
			t.Fatal("current PR association revoked the fixed historical run")
		}
	}
	delete(run, "prs")
	for k := range job {
		original := job[k]
		job[k] = "other"
		if owned3107OfficialJobMatches(job) {
			t.Fatal("unbound job accepted")
		}
		job[k] = original
	}
	labels := job["labels"].([]any)
	for i := range labels {
		original := labels[i]
		labels[i] = "other"
		if owned3107OfficialJobMatches(job) {
			t.Fatal("changed individual label accepted")
		}
		labels[i] = original
	}
}

func owned3107ChangeProof(t *testing.T, a *owned3107Access, i int, change func(object)) {
	t.Helper()
	p := a.proofs[i].path
	b, e := os.ReadFile(p)
	if e != nil {
		t.Fatal(e)
	}
	m, ok := conversationJSON(b)
	if !ok {
		t.Fatal("fixture JSON invalid")
	}
	change(m)
	b, e = json.Marshal(m)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(p, b, 0600); e != nil {
		t.Fatal(e)
	}
	a.proofs[i].sha = hashBytes(b)
}

func TestOwned3107RejectsUnboundObservationAndCreation(t *testing.T) {
	for _, which := range []string{"inventory-data", "inventory-incomplete", "deployment-domain", "deployment-sha", "volume", "early-create", "late-create", "old-child", "recorded-reparse"} {
		t.Run(which, func(t *testing.T) {
			a, target := owned3107Fixture(t)
			if !owned3107Allowed(target, a) {
				t.Fatal("positive fixture rejected")
			}
			i := 0
			if strings.HasPrefix(which, "inventory") || which == "old-child" || which == "recorded-reparse" {
				i = 4
			}
			if strings.HasPrefix(which, "deployment") {
				i = 5
			}
			owned3107ChangeProof(t, &a, i, func(m object) {
				switch which {
				case "inventory-data":
					m["data_read"] = true
				case "inventory-incomplete":
					m["attribute_walk_complete"] = false
				case "deployment-domain":
					m["Domain"] = "other"
				case "deployment-sha":
					m["ContractSHA256"] = strings.Repeat("0", 64)
				case "volume":
					m["volume"] = 1
				case "early-create", "late-create":
					delta := int64(-1)
					if which == "late-create" {
						delta = 10000001
					}
					m["created_at"] = time.Unix(0, (a.expected.Created+delta-116444736000000000)*100).UTC().Format(time.RFC3339Nano)
				case "old-child":
					obj(obj(m["objects"])["known.log"])["Created"] = a.expected.Created - 1
				case "recorded-reparse":
					obj(obj(m["objects"])["known.log"])["Attributes"] = syscall.FILE_ATTRIBUTE_REPARSE_POINT
				}
			})
			if which == "old-child" {
				original := a.identity
				a.identity = func(p string) (audio2906Metadata, error) {
					m, e := original(p)
					if same(p, target) {
						m.Created = a.expected.Created - 1
					}
					return m, e
				}
			}
			if owned3107Allowed(target, a) {
				t.Fatal("unbound or ineligible observation accepted")
			}
		})
	}
}

func TestOwned3107NestedAncestryAndRealJunction(t *testing.T) {
	a, _ := owned3107Fixture(t)
	nested := filepath.Join(a.root, "nested")
	if e := os.Mkdir(nested, 0700); e != nil {
		t.Fatal(e)
	}
	target := filepath.Join(nested, "known.log")
	if e := os.WriteFile(target, []byte("fixture only"), 0600); e != nil {
		t.Fatal(e)
	}
	dirMeta, e := a.identity(nested)
	if e != nil {
		t.Fatal(e)
	}
	fileMeta, e := a.identity(target)
	if e != nil {
		t.Fatal(e)
	}
	owned3107ChangeProof(t, &a, 4, func(m object) { v := obj(m["objects"]); v["nested"] = dirMeta; v["nested/known.log"] = fileMeta })
	if !owned3107Allowed(target, a) {
		t.Fatal("recorded nested target rejected")
	}
	moved := nested + "-old"
	if e := os.Rename(nested, moved); e != nil {
		t.Fatal(e)
	}
	cmd := exec.Command("cmd", "/c", "mklink", "/J", nested, moved)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	if out, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("junction fixture unavailable: %v %s", e, out)
	}
	defer os.Remove(nested)
	if owned3107Allowed(target, a) || owned3107Allowed(nested, a) {
		t.Fatal("junction ancestry accepted")
	}
	a.plain = func(string) bool { return true }
	if owned3107Allowed(target, a) {
		t.Fatal("attribute ancestry check accepted a junction")
	}
}
