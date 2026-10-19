package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func countState(l *Ledger, st string) int {
	n := 0
	for _, e := range l.Entries {
		if e.State == st {
			n++
		}
	}
	return n
}

func TestPR149AdmitsExactlySixJobsOnceAndLaunchesOnePerOS(t *testing.T) {
	h := newHarness(t)
	h.gh.pr149()
	r1 := h.tick()
	if r1.Admitted != 4 || r1.Launched != 2 {
		t.Fatalf("first tick %+v", r1)
	}
	r2 := h.tick()
	if r2.Admitted != 2 || r2.Launched != 0 {
		t.Fatalf("second tick %+v", r2)
	}
	verified := h.c.verified
	for i := 0; i < 3; i++ {
		if r := h.tick(); r.Admitted != 0 || r.Launched != 0 {
			t.Fatalf("duplicate tick admitted or launched: %+v", r)
		}
	}
	if h.c.verified != verified {
		t.Fatal("known jobs were verified again")
	}
	l := h.ledger()
	if len(l.Entries) != 6 || len(l.Budget.Initial) != 6 || len(l.Budget.Normal) != 0 {
		t.Fatalf("ledger %d entries, budget %+v", len(l.Entries), l.Budget)
	}
	if countState(l, StateLaunching) != 2 || countState(l, StateReserved) != 4 || len(h.spawned) != 2 {
		t.Fatalf("one worker per OS expected: %v", h.spawned)
	}
	osSeen := map[string]int{}
	for _, e := range l.Entries {
		if e.State == StateLaunching {
			osSeen[e.OS]++
		}
	}
	if osSeen["Linux"] != 1 || osSeen["Windows"] != 1 {
		t.Fatalf("OS workers %v", osSeen)
	}
}

func TestSeventhInitialJobIsBlockedWithoutContinuation(t *testing.T) {
	h := newHarness(t)
	h.gh.pr149()
	h.gh.addRun(105, ".github/workflows/native-driver.yml", "pull_request", 149, map[int64]string{1051: "native-driver"})
	for i := 0; i < 4; i++ {
		h.tick()
	}
	l := h.ledger()
	if len(l.Entries) != 6 {
		t.Fatalf("budget exceeded: %d", len(l.Entries))
	}
	if _, ok := l.Refusals["105/1/1051"]; !ok {
		t.Fatal("blocked job should leave a refusal backoff, not an entry")
	}
	h.d.Cfg.Continuation.Enabled = true
	h.now = h.now.Add(10 * time.Minute)
	h.tick()
	l = h.ledger()
	if len(l.Budget.Continuation) != 1 || len(l.Budget.Initial) != 6 {
		t.Fatalf("continuation not recorded separately: %+v", l.Budget)
	}
}

func TestForeignForkAndAdvancedHeadAreNeverAdmitted(t *testing.T) {
	h := newHarness(t)
	foreign := m{"id": 201, "run_attempt": 1, "path": ".github/workflows/rust.yml", "event": "pull_request", "status": "queued", "head_sha": headSHA, "head_branch": "x", "repository": m{"id": 5, "full_name": "Other/repo"}, "head_repository": m{"id": 5, "full_name": "Other/repo"}, "pull_requests": []m{{"number": 1}}}
	h.gh.addRunObj(foreign, map[int64]string{2011: "test (ubuntu-latest)"})
	forkRepo := m{"id": 999, "full_name": "fork/civ6-banseiki", "fork": true}
	fork := m{"id": 202, "run_attempt": 1, "path": ".github/workflows/rust.yml", "event": "pull_request", "status": "queued", "head_sha": headSHA, "head_branch": "x", "repository": repoObj(), "head_repository": forkRepo, "pull_requests": []m{}}
	h.gh.addRunObj(fork, map[int64]string{2021: "test (ubuntu-latest)"})
	// A run whose PR head moved on: the run's head differs from the PR head.
	h.gh.addRun(203, ".github/workflows/rust.yml", "pull_request", 149, map[int64]string{2031: "test (ubuntu-latest)"})
	h.gh.data["repos/"+Repository+"/pulls/149"] = m{"number": 149, "state": "open", "merge_commit_sha": mergeSHA, "head": m{"sha": strings.Repeat("d", 40), "ref": branch, "repo": repoObj()}}
	r := h.tick()
	if r.Admitted != 0 || len(h.ledger().Entries) != 0 || len(h.spawned) != 0 {
		t.Fatalf("unsafe job admitted: %+v", r)
	}
	if r.Scan.Rejected == 0 {
		t.Fatalf("advanced head must be recorded as rejected: %+v", r.Scan)
	}
}

func TestContractRejectionBacksOffInsteadOfRepeating(t *testing.T) {
	h := newHarness(t)
	h.gh.addRun(101, ".github/workflows/rust.yml", "pull_request", 149, map[int64]string{1011: "test (ubuntu-latest)"})
	h.c.verifyErr = func(Plan) error { return errors.New("source workflow differs from reviewed continuation template") }
	h.tick()
	n := h.c.verified
	h.tick()
	if h.c.verified != n || len(h.ledger().Entries) != 0 {
		t.Fatal("rejected job re-verified inside its backoff or admitted")
	}
	h.now = h.now.Add(11 * time.Minute)
	h.tick()
	if h.c.verified != n+1 {
		t.Fatal("backoff should expire and verify once")
	}
}

func TestUnwiredContractNeverAdmits(t *testing.T) {
	h := newHarness(t)
	h.gh.pr149()
	h.d.C = unwiredContract{}
	r := h.tick()
	if len(h.ledger().Entries) != 0 || len(r.Errors) == 0 {
		t.Fatalf("%+v", r)
	}
}

func TestSupervisorSuccessRecordsFourSeparateFacts(t *testing.T) {
	h := newHarness(t)
	key := h.seed("Windows", 301, 3011, "test (windows-latest)", true)
	if err := h.d.Supervise(context.Background(), key); err != nil {
		t.Fatal(err)
	}
	e := h.ledger().Entries[key]
	if e.State != StateSucceeded || !e.Official.Success || e.Cleanup.State != "removed" || e.Queued.At == "" || e.Started.HelperAt == "" || e.Started.ExitCode == nil || *e.Started.ExitCode != 0 {
		t.Fatalf("%+v", e)
	}
	if got := h.steps.names(); got != "Windows:prepare,Windows:execute,Windows:collect" {
		t.Fatalf("steps %s", got)
	}
	if string(h.steps.stdin) != "REGISTRATION-TOKEN-SECRET-123456" {
		t.Fatal("registration credential must reach the helper stdin")
	}
	if _, err := os.Stat(e.Cleanup.Root); !os.IsNotExist(err) {
		t.Fatal("owned root should be retired")
	}
	if len(h.sd.finished) != 1 || len(h.sd.quarantined) != 0 || len(h.ledger().Fences) != 0 {
		t.Fatal("finish recorded once, no fence expected")
	}
	// The original log bytes and hashes are kept before the root is deleted.
	kept, err := os.ReadFile(filepath.Join(e.Cleanup.Evidence, "root", "runner.log"))
	if err != nil || string(kept) != "original runner log\n" || len(e.Cleanup.ManifestSHA) != 64 {
		t.Fatalf("evidence %v %q", err, kept)
	}
	ledgerBytes, _ := os.ReadFile(h.d.Store.Path)
	if strings.Contains(string(ledgerBytes), "REGISTRATION-TOKEN-SECRET") {
		t.Fatal("secret reached the ledger")
	}
	if err := h.d.Supervise(context.Background(), key); err == nil {
		t.Fatal("a finished entry must never run again")
	}
}

func TestProcessExitAloneIsNotSuccessAndFailureIsNeverRerun(t *testing.T) {
	h := newHarness(t)
	h.c.successErr = errors.New("official target job success not confirmed")
	key := h.seed("Linux", 302, 3021, "test (ubuntu-latest)", true)
	if err := h.d.Supervise(context.Background(), key); err != nil {
		t.Fatal(err)
	}
	e := h.ledger().Entries[key]
	if e.State != StateFailed || e.Official.Success || e.Cleanup.State != "removed" {
		t.Fatalf("exit 0 without official success must fail: %+v", e)
	}
	h.spawned = nil
	for i := 0; i < 3; i++ {
		h.tick()
	}
	if len(h.spawned) != 0 || h.ledger().Entries[key].Started.Attempts != 1 {
		t.Fatal("failed job was rerun")
	}

	h2 := newHarness(t)
	h2.steps.exit[StepExecute] = 1
	k2 := h2.seed("Linux", 303, 3031, "test (ubuntu-latest)", true)
	_ = h2.d.Supervise(context.Background(), k2)
	e2 := h2.ledger().Entries[k2]
	if e2.State != StateFailed || e2.Cleanup.State != "removed" || !strings.Contains(h2.steps.names(), "collect") || len(h2.ledger().Fences) != 0 {
		t.Fatalf("failed job must still be collected and cleaned: %+v", e2)
	}
}

func TestRegistrationFailureCollectsUnstartedWithoutExecute(t *testing.T) {
	h := newHarness(t)
	h.d.Token = fakeToken{err: errors.New("denied")}
	key := h.seed("Linux", 304, 3041, "test (ubuntu-latest)", true)
	_ = h.d.Supervise(context.Background(), key)
	e := h.ledger().Entries[key]
	if e.State != StateFailed || strings.Contains(h.steps.names(), "execute") || !strings.Contains(h.steps.names(), "collect-unstarted") {
		t.Fatalf("%s %+v", h.steps.names(), e)
	}
}

func TestFailedCollectFencesTheOSAndKeepsTheRoot(t *testing.T) {
	h := newHarness(t)
	h.steps.exit[StepCollect] = 2
	key := h.seed("Windows", 305, 3051, "test (windows-latest)", true)
	_ = h.d.Supervise(context.Background(), key)
	l := h.ledger()
	e := l.Entries[key]
	if e.State != StateNeedsRecovery || l.Fences["Windows"].Key != key {
		t.Fatalf("%+v %+v", e, l.Fences)
	}
	if _, err := os.Stat(e.Cleanup.Root); err != nil {
		t.Fatal("root must stay for recovery")
	}
	// The fenced OS gets no new worker, the other OS still does.
	h.seed("Windows", 306, 3061, "test (windows-latest)", false)
	h.seed("Linux", 306, 3062, "test (ubuntu-latest)", false)
	h.gh.data["repos/"+Repository+"/actions/runs?status=queued&per_page=30"] = m{"workflow_runs": []m{}}
	h.tick()
	if len(h.spawned) != 1 {
		t.Fatalf("spawned %v", h.spawned)
	}
	if !strings.HasPrefix(h.spawned[0], "306/1/3062") {
		t.Fatalf("only the unfenced OS may start: %v", h.spawned)
	}
	// A status that is clean while the root is not retired keeps the fence.
	h.steps.clean = true
	h.tick()
	if _, ok := h.ledger().Fences["Windows"]; !ok {
		t.Fatal("fence released without a retired owned root")
	}
}

func TestUncertainCreateKeepsTheFence(t *testing.T) {
	h := newHarness(t)
	h.sd.failCreate = true
	key := h.seed("Linux", 307, 3071, "test (ubuntu-latest)", true)
	_ = h.d.Supervise(context.Background(), key)
	l := h.ledger()
	if l.Entries[key].State != StateNeedsRecovery || l.Fences["Linux"].Key != key || h.steps.names() != "" {
		t.Fatalf("%+v", l)
	}
}

func TestOSLockKeepsASecondWorkerOut(t *testing.T) {
	h := newHarness(t)
	key := h.seed("Linux", 308, 3081, "test (ubuntu-latest)", true)
	unlock, err := h.d.LockOS("Linux")
	if err != nil {
		t.Fatal(err)
	}
	if err := h.d.Supervise(context.Background(), key); err != nil {
		t.Fatal(err)
	}
	e := h.ledger().Entries[key]
	if e.State != StateReserved || e.Started.Attempts != 0 || h.steps.names() != "" || h.sd.n != 0 {
		t.Fatalf("busy lock must leave the entry untouched: %+v", e)
	}
	unlock()
	// A different OS is independent.
	k2 := h.seed("Windows", 308, 3082, "test (windows-latest)", true)
	u2, _ := h.d.LockOS("Linux")
	defer u2()
	if err := h.d.Supervise(context.Background(), k2); err != nil || h.ledger().Entries[k2].State != StateSucceeded {
		t.Fatalf("%v", err)
	}
}

func TestCrashedSupervisorFencesAndPIDReuseDoesNotUnlock(t *testing.T) {
	h := newHarness(t)
	key := h.seed("Linux", 309, 3091, "test (ubuntu-latest)", true)
	// The recorded supervisor is gone; a different live process now owns that PID.
	self, err := procIdentity(uint32(os.Getpid()))
	if err != nil {
		t.Fatal(err)
	}
	reused := ProcID{PID: self.PID, Created: self.Created + 12345}
	_ = h.d.mutate(key, func(l *Ledger, e *Entry) { e.Started.Supervisor = reused })
	h.d.Alive = procAlive
	r := h.tick()
	l := h.ledger()
	if l.Entries[key].State != StateNeedsRecovery || l.Fences["Linux"].Key != key || r.Reconciled != 1 {
		t.Fatalf("PID reuse must not look alive: %+v %+v", l.Entries[key], r)
	}
	// The very same identity does count as alive.
	h2 := newHarness(t)
	k2 := h2.seed("Linux", 310, 3101, "test (ubuntu-latest)", true)
	_ = h2.d.mutate(k2, func(l *Ledger, e *Entry) { e.Started.Supervisor = self })
	h2.d.Alive = procAlive
	h2.tick()
	if h2.ledger().Entries[k2].State != StateLaunching || len(h2.ledger().Fences) != 0 {
		t.Fatal("a live recorded supervisor must be left alone")
	}
}

func TestCrashBetweenIntentAndSpawnRecordIsFenced(t *testing.T) {
	h := newHarness(t)
	key := h.seed("Windows", 311, 3111, "test (windows-latest)", true)
	h.tick()
	l := h.ledger()
	if l.Entries[key].State != StateNeedsRecovery || l.Fences["Windows"].Key != key {
		t.Fatalf("%+v", l.Entries[key])
	}
	if len(h.spawned) != 0 {
		t.Fatal("no new worker while fenced")
	}
}

func TestResultFileWrittenBeforeCrashIsImported(t *testing.T) {
	h := newHarness(t)
	key := h.seed("Linux", 312, 3121, "test (ubuntu-latest)", true)
	_ = h.d.mutate(key, func(l *Ledger, e *Entry) { e.Started.Supervisor = ProcID{PID: 4242, Created: 3} })
	code := 0
	if err := writeAtomic(h.d.resultPath(key), Result{Key: key, State: StateSucceeded, ExitCode: &code, Official: OfficialRec{Success: true}, Cleanup: CleanupRec{State: "removed"}, At: stamp(h.now)}); err != nil {
		t.Fatal(err)
	}
	h.tick()
	l := h.ledger()
	if l.Entries[key].State != StateSucceeded || len(l.Fences) != 0 {
		t.Fatalf("%+v", l.Entries[key])
	}
}

func TestTickKeepsLaunchingWhenGitHubIsDown(t *testing.T) {
	h := newHarness(t)
	h.seed("Linux", 313, 3131, "test (ubuntu-latest)", false)
	h.d.Reader = func() *Reader {
		return &Reader{Run: func(context.Context, string, []byte, int64, ...string) ([]byte, error) {
			return nil, errors.New("down")
		}, pinned: true}
	}
	r := h.tick()
	if len(h.spawned) != 1 || r.Status != "ok" || len(r.Errors) == 0 {
		t.Fatalf("%+v spawned=%v", r, h.spawned)
	}
}

func TestConcurrentProcessesReserveOnce(t *testing.T) {
	h := newHarness(t)
	p := Plan{Version: 1, RepoID: RepoID, Head: headSHA, Event: "push", Branch: "main", RunID: 400, Attempt: 1, JobID: 4001}
	var wg sync.WaitGroup
	var mu sync.Mutex
	won := 0
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s := &Store{Path: h.d.Store.Path} // a separate handle, as another process would have
			_ = s.Update(func(l *Ledger) error {
				if e, _ := l.reserve(p, "Linux", false, h.now); e != nil {
					mu.Lock()
					won++
					mu.Unlock()
				}
				return nil
			})
		}()
	}
	wg.Wait()
	l := h.ledger()
	if won != 1 || len(l.Entries) != 1 || len(l.Budget.Normal) != 1 {
		t.Fatalf("won=%d %+v", won, l.Budget)
	}
}

func TestEndpointAllowlistAndPinnedExecutables(t *testing.T) {
	for _, ep := range []string{"user", "repos/Other/repo/pulls/1", "repos/AutoDevJapan/civ6-banseiki/actions/runners/registration-token", "https://api.github.com/repos/AutoDevJapan/civ6-banseiki", "repos/AutoDevJapan/civ6-banseiki/../x", "repos/AutoDevJapan/civ6-banseiki/actions/jobs/1/logs"} {
		if checkEndpoint(ep) == nil {
			t.Errorf("%s accepted", ep)
		}
	}
	for _, ep := range []string{"repositories/1392152644", "repos/AutoDevJapan/civ6-banseiki/pulls/149", "repos/AutoDevJapan/civ6-banseiki/compare/" + headSHA + "..." + mergeSHA, "repos/AutoDevJapan/civ6-banseiki/git/ref/heads/scheduler%2Fx"} {
		if err := checkEndpoint(ep); err != nil {
			t.Errorf("%s rejected: %v", ep, err)
		}
	}
	exe, _ := os.Executable()
	good, err := hashFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	if (Pinned{Path: exe, SHA256: good}).Verify() != nil {
		t.Fatal("matching pin rejected")
	}
	if (Pinned{Path: exe, SHA256: strings.Repeat("0", 64)}).Verify() == nil {
		t.Fatal("mismatching pin accepted")
	}
	if (Pinned{Path: "relative.exe", SHA256: good}).Verify() == nil {
		t.Fatal("relative path accepted")
	}
	h := fixedHelper{os: "Linux", cfg: HelperConfig{Exe: Pinned{Path: exe, SHA256: strings.Repeat("0", 64)}, Seconds: 600}}
	if _, err := runStep(context.Background(), h, StepPrepare, t.TempDir(), nil, nil); err == nil {
		t.Fatal("helper started despite a wrong pin")
	}
	if _, err := runStep(context.Background(), fixedHelper{os: "Linux", cfg: HelperConfig{Exe: Pinned{Path: exe, SHA256: good}}}, "rm -rf", t.TempDir(), nil, nil); err == nil {
		t.Fatal("arbitrary step verb accepted")
	}
}

func TestRealHelperProcessGetsStdinAndExitCode(t *testing.T) {
	exe, _ := os.Executable()
	sum, _ := hashFile(exe)
	h := fixedHelper{os: "Linux", cfg: HelperConfig{Exe: Pinned{Path: exe, SHA256: sum}, Seconds: 600}}
	t.Setenv("CIQ_FAKE_HELPER", "1")
	t.Setenv("CIQ_EXIT", "3")
	root := t.TempDir()
	var started ProcID
	res, err := runStep(context.Background(), h, StepExecute, root, []byte("token-bytes"), func(id ProcID) { started = id })
	if err != nil || res.Exit != 3 {
		t.Fatalf("%v %+v", err, res)
	}
	got, _ := os.ReadFile(filepath.Join(root, "received.txt"))
	if string(got) != "11" {
		t.Fatalf("stdin not delivered: %q", got)
	}
	if started.PID == 0 {
		t.Fatal("helper identity not reported")
	}
}

func TestDetachedStartHasNoWindowAndIdentityIsReal(t *testing.T) {
	exe, _ := os.Executable()
	c := exec.Command(exe, "x", "y")
	c.Env = append(os.Environ(), "CIQ_FAKE_HELPER=1", "CIQ_SLEEP_MS=1500")
	if _, err := startDetached(c); err != nil {
		t.Fatal(err)
	}
	id, err := procIdentity(uint32(c.Process.Pid))
	if err != nil || !procAlive(id) {
		t.Fatalf("%v", err)
	}
	if procAlive(ProcID{PID: id.PID, Created: id.Created + 1}) {
		t.Fatal("PID alone must not be enough")
	}
	_ = c.Wait()
	if procAlive(id) {
		t.Fatal("exited process still alive")
	}
}

func TestConfigIsStrict(t *testing.T) {
	dir := t.TempDir()
	exe, _ := os.Executable()
	sum, _ := hashFile(exe)
	pin := `{"path":"` + strings.ReplaceAll(exe, "\\", "\\\\") + `","sha256":"` + sum + `"}`
	write := func(body string) string {
		p := filepath.Join(dir, "c.json")
		_ = os.WriteFile(p, []byte(body), 0600)
		return p
	}
	home := strings.ReplaceAll(dir, "\\", "\\\\")
	ok := `{"version":1,"home":"` + home + `","gh":` + pin + `,"safe_delete":` + pin + `,"helpers":{},"continuation":{"enabled":false}}`
	if _, err := LoadConfig(write(ok)); err != nil {
		t.Fatal(err)
	}
	bad := []string{
		strings.Replace(ok, `"helpers":{}`, `"helpers":{},"command":"calc"`, 1),
		strings.Replace(ok, `"enabled":false`, `"enabled":true`, 1),
		strings.Replace(ok, home, "relative/home", 1),
		strings.Replace(ok, `"helpers":{}`, `"helpers":{"Mac":{}}`, 1),
	}
	for i, b := range bad {
		if _, err := LoadConfig(write(b)); err == nil {
			t.Errorf("bad config %d accepted", i)
		}
	}
}

func TestRemoveOwnedRefusesAnythingButTheRecordedRoot(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "codex-owned-"+strings.Repeat("a", 32))
	_ = os.MkdirAll(root, 0700)
	if removeOwned(root, filepath.Join(base, "other")) == nil || removeOwned(base, base) == nil {
		t.Fatal("wrong target accepted")
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatal("refusal must not delete")
	}
	if err := removeOwned(root, root); err != nil {
		t.Fatal(err)
	}
}

func hashFile(p string) (string, error) {
	b, err := os.ReadFile(p)
	if err != nil {
		return "", err
	}
	return digestHex(b), nil
}
