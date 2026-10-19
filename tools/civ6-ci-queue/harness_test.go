package main

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// The test binary doubles as the fake fixed helper executable.
func TestMain(m *testing.M) {
	if os.Getenv("CIQ_FAKE_HELPER") == "1" {
		if ms := os.Getenv("CIQ_SLEEP_MS"); ms != "" {
			n, _ := strconv.Atoi(ms)
			time.Sleep(time.Duration(n) * time.Millisecond)
		}
		if len(os.Args) >= 3 && os.Args[1] == "execute" {
			b, _ := io.ReadAll(os.Stdin)
			_ = os.WriteFile(filepath.Join(os.Args[2], "received.txt"), []byte(strconv.Itoa(len(b))), 0600)
		}
		code, _ := strconv.Atoi(os.Getenv("CIQ_EXIT"))
		os.Exit(code)
	}
	os.Exit(m.Run())
}

const (
	headSHA  = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	mergeSHA = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	treeSHA  = "cccccccccccccccccccccccccccccccccccccccc"
	branch   = "scheduler/actions-cost-pr-concurrency-20261019"
)

func blobSHA(b []byte) string {
	h := sha1.New()
	fmt.Fprintf(h, "blob %d\x00", len(b))
	h.Write(b)
	return hex.EncodeToString(h.Sum(nil))
}

func digestHex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// fakeGH answers "gh api <endpoint>" from a table, counting every call.
type fakeGH struct {
	mu    sync.Mutex
	data  map[string]any
	calls []string
}

func (g *fakeGH) run(ctx context.Context, exe string, stdin []byte, limit int64, args ...string) ([]byte, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(args) != 2 || args[0] != "api" {
		return nil, errors.New("unexpected gh arguments")
	}
	g.calls = append(g.calls, args[1])
	v, ok := g.data[args[1]]
	if !ok {
		return nil, errors.New("not found")
	}
	return json.Marshal(v)
}

type obj = map[string]any

func repoObj() obj { return obj{"id": RepoID, "full_name": Repository, "fork": false} }

func (g *fakeGH) addRun(id int64, path, event string, pr int, jobs map[int64]string) {
	run := obj{"id": id, "run_attempt": 1, "path": path, "event": event, "status": "queued", "head_sha": headSHA, "head_branch": branch, "repository": repoObj(), "head_repository": repoObj()}
	if pr > 0 {
		run["pull_requests"] = []obj{{"number": pr}}
	} else {
		run["pull_requests"] = []obj{}
	}
	g.addRunObj(run, jobs)
}

func (g *fakeGH) addRunObj(run obj, jobs map[int64]string) {
	q := "repos/" + Repository + "/actions/runs?status=queued&per_page=30"
	l, _ := g.data[q].(obj)
	if l == nil {
		l = obj{"workflow_runs": []obj{}}
	}
	l["workflow_runs"] = append(l["workflow_runs"].([]obj), run)
	g.data[q] = l
	var js []obj
	for id, name := range jobs {
		js = append(js, obj{"id": id, "run_id": run["id"], "run_attempt": 1, "name": name, "status": "queued", "labels": []string{"x"}})
	}
	g.data[fmt.Sprintf("repos/%s/actions/runs/%d/attempts/1/jobs?per_page=100", Repository, run["id"])] = obj{"total_count": len(js), "jobs": js}
	full, _ := run["path"].(string)
	p, _, _ := strings.Cut(full, "@")
	if p != CodeQLWorkflow {
		b := []byte("name: " + p + "\n")
		g.data["repos/"+Repository+"/contents/"+p+"?ref="+mergeSHA] = obj{"type": "file", "encoding": "base64", "content": base64.StdEncoding.EncodeToString(b), "sha": blobSHA(b)}
		g.data["repos/"+Repository+"/contents/"+p+"?ref="+headSHA] = obj{"type": "file", "encoding": "base64", "content": base64.StdEncoding.EncodeToString(b), "sha": blobSHA(b)}
	}
}

func newGH() *fakeGH {
	g := &fakeGH{data: map[string]any{}}
	g.data["repos/"+Repository+"/actions/runs?status=in_progress&per_page=30"] = obj{"workflow_runs": []obj{}}
	g.data["repos/"+Repository+"/pulls/149"] = obj{"number": 149, "state": "open", "merge_commit_sha": mergeSHA, "head": obj{"sha": headSHA, "ref": branch, "repo": repoObj()}}
	g.data["repos/"+Repository+"/git/commits/"+headSHA] = obj{"sha": headSHA, "tree": obj{"sha": treeSHA}}
	return g
}

// pr149 adds the six official PR149 jobs.
func (g *fakeGH) pr149() {
	g.addRun(101, ".github/workflows/rust.yml", "pull_request", 149, map[int64]string{1011: "test (ubuntu-latest)", 1012: "test (windows-latest)"})
	g.addRun(102, ".github/workflows/native-driver.yml", "pull_request", 149, map[int64]string{1021: "native-driver"})
	g.addRun(103, ".github/workflows/live-chain.yml", "pull_request", 149, map[int64]string{1031: "live-chain"})
	g.addRun(104, CodeQLWorkflow, "dynamic", 149, map[int64]string{1041: "Analyze (actions)", 1042: "Analyze (rust)"})
}

type fakeContract struct {
	mu         sync.Mutex
	verified   int
	verifyErr  func(Plan) error
	successErr error
	runner     string
}

func (f *fakeContract) Validate(p Plan) error {
	if p.Version != 1 || p.RepoID != RepoID {
		return errors.New("fixed repository required")
	}
	return nil
}
func (f *fakeContract) Labels(Plan) []string { return nil }
func (f *fakeContract) OS(p Plan) string {
	if p.JobName == "test (ubuntu-latest)" || strings.HasPrefix(p.JobName, "Analyze") {
		return "Linux"
	}
	return "Windows"
}
func (f *fakeContract) MatchRunPath(p Plan, runPath string) bool {
	if runPath == p.Workflow {
		return true
	}
	w, ref, found := strings.Cut(runPath, "@")
	return found && w == p.Workflow && (ref == "refs/pull/149/merge" || ref == "refs/heads/main")
}
func (f *fakeContract) Verify(ctx context.Context, p Plan, read ReadAPI) error {
	f.mu.Lock()
	f.verified++
	fn := f.verifyErr
	f.mu.Unlock()
	if fn != nil {
		return fn(p)
	}
	return nil
}
func (f *fakeContract) RequireSuccess(ctx context.Context, p Plan, runner string, read ReadAPI) error {
	f.mu.Lock()
	f.runner = runner
	f.mu.Unlock()
	return f.successErr
}

// fakeHelper is a scripted OS lifecycle for the supervisor tests.
type fakeHelper struct {
	os    string
	mu    sync.Mutex
	calls []string
	out   Outcome
}

func newFakeHelper(osName string) *fakeHelper {
	return &fakeHelper{os: osName, out: Outcome{Started: true, HelperOK: true, Collected: true}}
}
func (f *fakeHelper) OSName() string      { return f.os }
func (f *fakeHelper) Seconds() int        { return 600 }
func (f *fakeHelper) CleanupSeconds() int { return 0 }
func (f *fakeHelper) Lifecycle(ctx context.Context, lc *LifeCtx) Outcome {
	f.mu.Lock()
	f.calls = append(f.calls, f.os+":lifecycle")
	out := f.out
	f.mu.Unlock()
	lc.Running()
	lc.Fence("fake: resources may exist")
	lc.OnHelper(ProcID{PID: 777, Created: 5})
	_ = os.WriteFile(filepath.Join(lc.Root, "runner.log"), []byte("original runner log\n"), 0600)
	if out.Runner == "" {
		out.Runner = "R-" + lc.Instance
	}
	return out
}
func (f *fakeHelper) names() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return strings.Join(f.calls, ",")
}

type fakeSD struct {
	base        string
	n           int
	finished    []string
	quarantined []string
	failCreate  bool
}

func (s *fakeSD) Create(ctx context.Context, project, content string) (OwnedRec, error) {
	if s.failCreate {
		return OwnedRec{}, errors.New("unconfirmed")
	}
	s.n++
	id := fmt.Sprintf("%032x", s.n)
	p := filepath.Join(s.base, "codex-owned-"+id)
	if err := os.MkdirAll(p, 0700); err != nil {
		return OwnedRec{}, err
	}
	return OwnedRec{ID: id, Path: p}, nil
}
func (s *fakeSD) Plan(ctx context.Context, id string) (string, error) {
	return filepath.Join(s.base, "codex-owned-"+id), nil
}
func (s *fakeSD) Finish(ctx context.Context, id string) error {
	s.finished = append(s.finished, id)
	return nil
}
func (s *fakeSD) Quarantine(ctx context.Context, id, reason string) error {
	s.quarantined = append(s.quarantined, id)
	return nil
}

type fakeToken struct{ err error }

func (f fakeToken) Registration(ctx context.Context) ([]byte, error) {
	if f.err != nil {
		return nil, f.err
	}
	return []byte("REGISTRATION-TOKEN-SECRET-123456"), nil
}

type harness struct {
	t       *testing.T
	d       *Deps
	gh      *fakeGH
	c       *fakeContract
	lin     *fakeHelper
	win     *fakeHelper
	sd      *fakeSD
	spawned []string
	live    bool
	now     time.Time
}

func newHarness(t *testing.T) *harness {
	home := t.TempDir()
	h := &harness{t: t, gh: newGH(), c: &fakeContract{}, lin: newFakeHelper("Linux"), win: newFakeHelper("Windows"), sd: &fakeSD{base: t.TempDir()}, live: true, now: time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)}
	d := &Deps{Cfg: Config{Version: 1, Home: home}, Now: func() time.Time { return h.now }, C: h.c, SD: h.sd, Token: fakeToken{}}
	d.Store = &Store{Path: filepath.Join(home, "ledger.json")}
	d.Reader = func() *Reader { return &Reader{Run: h.gh.run, pinned: true, MaxCalls: 200} }
	d.Alive = func(id ProcID) bool { return h.live && id.PID >= 5000 && id.PID < 6000 }
	d.Self = func() ProcID { return ProcID{PID: 9000, Created: 7} }
	d.LockOS = func(osName string) (func(), error) {
		_ = os.MkdirAll(d.home("locks"), 0700)
		return tryLock(d.home("locks", "os-"+osName+".lock"))
	}
	d.Spawn = func(key string) (ProcID, bool, error) {
		h.spawned = append(h.spawned, key)
		return ProcID{PID: uint32(5000 + len(h.spawned)), Created: 11}, true, nil
	}
	d.Helpers = map[string]Helper{"Linux": h.lin, "Windows": h.win}
	h.d = d
	return h
}

func (h *harness) tick() TickReport {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return h.d.Tick(ctx)
}

func (h *harness) ledger() *Ledger {
	l, err := h.d.Store.View()
	if err != nil {
		h.t.Fatal(err)
	}
	return l
}

// seed stores a plan as an already reserved (and optionally launching) first attempt.
func (h *harness) seed(osName string, run, job int64, jobName string, launching bool) string {
	p := Plan{Version: 1, RepoID: RepoID, Head: headSHA, Tree: treeSHA, Event: "pull_request", Branch: branch, PR: 149, ContextSHA: mergeSHA, WorkflowSHA: mergeSHA, Workflow: ".github/workflows/rust.yml", RunID: run, Attempt: 1, JobID: job, Job: "test", JobName: jobName}
	var key string
	err := h.d.Store.Update(func(l *Ledger) error {
		e, why := l.reserve(p, osName, false, h.now)
		if e == nil {
			return errors.New(why)
		}
		if launching {
			e.State, e.Started.Attempts, e.Started.IntentAt = StateLaunching, 1, stamp(h.now)
		}
		key = e.Key
		return nil
	})
	if err != nil {
		h.t.Fatal(err)
	}
	return key
}
