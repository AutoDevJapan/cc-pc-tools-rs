package main

import (
	contract "cc/civ6-ci-contract"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRetirement3250RealMetadataQualification(t *testing.T) {
	if os.Getenv("CC_RETIREMENT3250_ACCEPTANCE") != "1" {
		t.Skip("explicit metadata-only acceptance")
	}
	root := filepath.Join(retirement3250Temp, "codex-owned-bbdecd35aa4599f01cf61a3bfaa272b1")
	if stage := retirement3250Qualification(root); stage != "" {
		t.Fatal(stage)
	}
}

func TestRetirement3250SharedLeafDoesNotBlockAtomicWriter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.json")
	if e := os.WriteFile(path, []byte("synthetic"), 0600); e != nil {
		t.Fatal(e)
	}
	f, e := retirement3250OpenShared(path)
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	if e = os.Rename(path, path+".old"); e != nil {
		t.Fatal("reader blocked writer rename", e)
	}
	if e = os.WriteFile(path, []byte("replacement"), 0600); e != nil {
		t.Fatal(e)
	}
}

func TestRetirement3250InventoryIsOpaqueAndRejectsExternalLinksAndDeny(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if e := os.Mkdir(filepath.Join(outside, "must-not-enumerate"), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.Symlink(outside, filepath.Join(root, "opaque")); e != nil {
		t.Fatal(e)
	}
	p := filepath.Join(root, "proxy")
	if e := os.WriteFile(p, []byte("synthetic"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.Link(p, filepath.Join(root, "alias")); e != nil {
		t.Fatal(e)
	}
	m, e := resourceIdentity(root, true)
	if e != nil {
		t.Fatal(e)
	}
	calls := 0
	identity := func(path string) (audio2906Metadata, error) {
		calls++
		if strings.Contains(path, "must-not-enumerate") {
			t.Fatal("followed opaque target")
		}
		m, e := resourceIdentity(path, true)
		m.Owner = "S-1-5-32-544"
		return m, e
	}
	deny := func(string) bool { return false }
	if stage := retirement3250InventoryWith(root, m.Volume, deny, identity); stage != "" {
		t.Fatal(stage)
	}
	if calls != 4 {
		t.Fatalf("unexpected enumeration count: %d", calls)
	}
	if stage := retirement3250InventoryWith(root, m.Volume, func(p string) bool { return filepath.Base(p) == "proxy" }, identity); stage == "" {
		t.Fatal("deny ignored")
	}
	if e := os.Link(p, filepath.Join(outside, "external")); e != nil {
		t.Fatal(e)
	}
	if stage := retirement3250InventoryWith(root, m.Volume, deny, identity); stage != "external-hardlink" {
		t.Fatal(stage)
	}
}

func retirement3250Fixture(now time.Time) (retirement3250Record, retirement3250Ledger) {
	id := "bbdecd35aa4599f01cf61a3bfaa272b1"
	r := retirement3250Record{retirement3250Identity: retirement3250Identity{Version: 2, ID: id, Root: retirement3250Temp, Path: filepath.Join(retirement3250Temp, "codex-owned-"+id), Volume: 1, FileID: 2, Created: now.Add(-time.Hour).Format(time.RFC3339), Project: "civ6-ci-queue", Content: "job-39423559387-1-118630300541"}, State: "owned"}
	p := contract.Plan{Version: 1, RepoID: 1392152644, Head: owned3107Head, Tree: owned3107Tree, Event: "pull_request", Branch: "scheduler/actions-cost-pr-concurrency-20261019", PR: 149, ContextSHA: owned3107Context, WorkflowSHA: owned3107Context, WorkflowRef: "AutoDevJapan/civ6-banseiki/.github/workflows/rust.yml@refs/pull/149/merge", Workflow: ".github/workflows/rust.yml", WorkflowDigest: owned3107WorkflowSHA, RunID: 39423559387, Attempt: 1, JobID: 118630300541, Job: "test", JobName: "test (windows-latest)"}
	e := &retirement3250Entry{Key: owned3107Key, OS: "Windows", Phase: "initial", State: "needs_recovery", Plan: p, Cleanup: retirement3250Cleanup{ID: id, Instance: id, Root: r.Path}}
	e.Queued.At = now.Add(-2 * time.Hour).Format(time.RFC3339)
	e.Queued.Deferrals = 1
	l := retirement3250Ledger{Version: 1, Entries: map[string]*retirement3250Entry{owned3107Key: e}}
	l.Budget.Initial = []string{owned3107Key}
	b, _ := json.Marshal(r.retirement3250Identity)
	r.Integrity = hashBytes(b)
	return r, l
}

func TestRetirement3250ScopeIsFiniteAndProvenanceBound(t *testing.T) {
	now := time.Date(2026, 10, 21, 3, 0, 0, 0, time.UTC)
	r, l := retirement3250Fixture(now)
	if _, ok := retirement3250Binding(r.Path, r, l, now); !ok {
		t.Fatal("normal newly owned collected input refused")
	}
	for _, change := range []string{"other-root", "old-root", "other-repo", "other-head", "no-budget", "duplicate-budget", "continuation", "too-many-retries", "running", "no-instance", "old-creation", "closed"} {
		t.Run(change, func(t *testing.T) {
			r, l := retirement3250Fixture(now)
			e := l.Entries[owned3107Key]
			path := r.Path
			switch change {
			case "other-root":
				path += "-other"
			case "old-root":
				r.ID = owned3107ID
				r.Path = owned3107Root
				path = r.Path
			case "other-repo":
				e.Plan.RepoID++
			case "other-head":
				e.Plan.Head = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
			case "no-budget":
				l.Budget.Initial = nil
			case "duplicate-budget":
				l.Budget.Initial = append(l.Budget.Initial, owned3107Key)
			case "continuation":
				e.Phase = "continuation"
			case "too-many-retries":
				e.Queued.Deferrals = 6
			case "running":
				e.State = "running"
			case "no-instance":
				e.Cleanup.Instance = ""
			case "old-creation":
				r.Created = "2026-10-01T00:00:00Z"
			case "closed":
				r.State = "quarantined"
			}
			if _, ok := retirement3250Binding(path, r, l, now); ok {
				t.Fatal("out-of-scope input admitted")
			}
		})
	}
}

func TestRetirement3250NormalAndTimeBoundaries(t *testing.T) {
	now := time.Date(2026, 10, 21, 3, 0, 0, 0, time.UTC)
	r, l := retirement3250Fixture(now)
	e := l.Entries[owned3107Key]
	e.Phase = "normal"
	e.Plan.Event = "push"
	e.Plan.PR = 0
	e.Plan.Branch = "main"
	e.Plan.ContextSHA = e.Plan.Head
	e.Plan.WorkflowSHA = e.Plan.Head
	e.Plan.WorkflowRef = "AutoDevJapan/civ6-banseiki/.github/workflows/rust.yml@refs/heads/main"
	l.Budget.Normal = l.Budget.Initial
	l.Budget.Initial = nil
	if _, ok := retirement3250Binding(r.Path, r, l, now); !ok {
		t.Fatal("normal postmerge plan refused before official merge check")
	}
	e.Queued.At = now.Format(time.RFC3339)
	if _, ok := retirement3250Binding(r.Path, r, l, now); ok {
		t.Fatal("queued after creation admitted")
	}
	e.Queued.At = now.Add(-25*time.Hour - time.Second).Format(time.RFC3339)
	if _, ok := retirement3250Binding(r.Path, r, l, now); ok {
		t.Fatal("creation outside queued age admitted")
	}
}
