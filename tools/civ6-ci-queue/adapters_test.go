package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type lifeRecord struct {
	fences  []string
	running int
	helper  []ProcID
	logs    map[string][]byte
}

func writePinned(t *testing.T, name, body string) Pinned {
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
	return Pinned{Path: p, SHA256: digestHex([]byte(body))}
}

func newLife(t *testing.T) (*LifeCtx, *lifeRecord, string) {
	instance := strings.Repeat("ab", 16)
	root := filepath.Join(t.TempDir(), "codex-owned-"+instance)
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	rec := &lifeRecord{logs: map[string][]byte{}}
	lc := &LifeCtx{Key: "1/1/2", Plan: Plan{Version: 1, RepoID: RepoID, Head: headSHA, Tree: treeSHA, Event: "pull_request", Branch: branch, PR: 149, ContextSHA: mergeSHA, WorkflowSHA: mergeSHA, Workflow: ".github/workflows/native-driver.yml", RunID: 1, Attempt: 1, JobID: 2, Job: "native-driver", JobName: "native-driver"},
		Instance: instance, Root: root, EntryDir: t.TempDir(),
		Fence:    func(r string) { rec.fences = append(rec.fences, r) },
		Running:  func() { rec.running++ },
		OnHelper: func(id ProcID) { rec.helper = append(rec.helper, id) },
		Log:      func(n string, b []byte) { rec.logs[n] = append([]byte(nil), b...) },
	}
	return lc, rec, root
}

type winFake struct {
	treeUncertain bool
	calls         []string
	execErr       error
	stdin         []byte
	prepareOK     bool
	executeOut    string
	executeExit   int
	collectOut    string
	scripts       map[string]string
	helperPinned  []Pinned
}

func newWinFake() *winFake {
	return &winFake{prepareOK: true, executeOut: `{"event":"execute-end","ok":true,"service_started":true,"service_stopped":true,"fence":false}`, collectOut: `{"event":"collect-end","complete":true,"shared_profile_retained":true}`, scripts: map[string]string{}}
}

func (w *winFake) admin(root string) AdminFunc {
	return func(ctx context.Context, script string, timeoutSec int) ([]byte, error) {
		b, _ := os.ReadFile(script)
		body := string(b)
		switch {
		case strings.Contains(body, " collect-unstarted "):
			w.calls = append(w.calls, "admin:collect-unstarted")
			return []byte(w.collectOut), nil
		case strings.Contains(body, " collect "):
			w.calls = append(w.calls, "admin:collect")
			return []byte("exit=0\n" + w.collectOut), nil
		case strings.Contains(body, " prepare "):
			w.calls = append(w.calls, "admin:prepare")
			w.scripts["prepare"] = body
			if w.prepareOK {
				_ = os.WriteFile(filepath.Join(root, "prepared.json"), []byte("{}"), 0600)
			}
			return []byte("exit=0"), nil
		}
		return nil, errors.New("unexpected script")
	}
}

func (w *winFake) exec(ctx context.Context, pin Pinned, args []string, stdin []byte, onStart func(ProcID)) (StepResult, error) {
	w.calls = append(w.calls, "exec:"+args[0])
	w.helperPinned = append(w.helperPinned, pin)
	if args[0] == "execute" {
		w.stdin = append([]byte(nil), stdin...)
		if onStart != nil {
			onStart(ProcID{PID: 901, Created: 3})
		}
		return StepResult{Exit: w.executeExit, Output: []byte(w.executeOut), TreeGone: !w.treeUncertain}, w.execErr
	}
	return StepResult{}, nil
}

func winHelper(t *testing.T) windowsHelper {
	return windowsHelper{cfg: HelperConfig{Exe: writePinned(t, "civ6-ci-windows.exe", "helper-bytes"), RunnerArchive: writePinned(t, "runner.zip", "zip-bytes"), Seconds: 600, RunnerVersion: "2.338.0", RunnerDigest: strings.Repeat("a", 64), ControllerSID: "S-1-5-21-1-2-3-4"}}
}

func TestWindowsLifecycleUsesDocumentedStepsWithoutPregateCredential(t *testing.T) {
	h := winHelper(t)
	lc, rec, root := newLife(t)
	w := newWinFake()
	lc.Exec, lc.Admin = w.exec, w.admin(root)
	out := h.Lifecycle(context.Background(), lc)
	if !out.Started || !out.HelperOK || !out.Collected || out.Residue || out.Runner != "CC-CIW-"+lc.Instance || out.Exit != 0 {
		t.Fatalf("%+v", out)
	}
	if got := strings.Join(w.calls, ","); got != "admin:prepare,exec:execute,admin:collect" {
		t.Fatalf("%s", got)
	}
	if len(w.stdin) != 0 {
		t.Fatal("no credential may be obtained before Cargo admission")
	}
	if filepath.Base(w.helperPinned[0].Path) != "helper.exe" || filepath.Dir(w.helperPinned[0].Path) != root || w.helperPinned[0].SHA256 != h.cfg.Exe.SHA256 {
		t.Fatalf("execute must use the pinned copy inside the root: %+v", w.helperPinned[0])
	}
	if !strings.Contains(w.scripts["prepare"], "bootstrap.exe' prepare '"+root+"' '"+h.cfg.RunnerArchive.Path+"'") {
		t.Fatalf("%s", w.scripts["prepare"])
	}
	pj, _ := os.ReadFile(filepath.Join(root, "plan.json"))
	var plan map[string]any
	if json.Unmarshal(pj, &plan) != nil || plan["instance"] != lc.Instance || plan["controller_sid"] != "S-1-5-21-1-2-3-4" || plan["job_name"] != "native-driver" || plan["runner_digest"] != strings.Repeat("a", 64) {
		t.Fatalf("%s", pj)
	}
	if len(rec.fences) != 1 || rec.running != 1 || len(rec.helper) != 1 {
		t.Fatalf("%+v", rec)
	}
	for n, b := range rec.logs {
		if strings.Contains(string(b), "REGISTRATION-TOKEN") {
			t.Fatalf("secret in log %s", n)
		}
	}
}

func TestWindowsLifecycleStopsOnceWhenTheServiceMayStillRun(t *testing.T) {
	h := winHelper(t)
	lc, _, root := newLife(t)
	w := newWinFake()
	w.executeExit = 1
	w.executeOut = `{"event":"execute-begin"}`
	lc.Exec, lc.Admin = w.exec, w.admin(root)
	out := h.Lifecycle(context.Background(), lc)
	if out.HelperOK || out.Exit != 1 || !out.Collected || !strings.Contains(strings.Join(w.calls, ","), "exec:execute,exec:stop,admin:collect") {
		t.Fatalf("%+v %v", out, w.calls)
	}
	if strings.Count(strings.Join(w.calls, ","), "exec:stop") != 1 {
		t.Fatal("exactly one stop attempt expected")
	}

	// When the helper itself reported its ending (even with a fence), it already
	// made its one stop attempt: the queue goes straight to collect.
	lc2, _, root2 := newLife(t)
	w2 := newWinFake()
	w2.executeExit = 1
	w2.executeOut = `{"event":"execute-end","ok":false,"service_started":true,"service_stopped":false,"fence":true}`
	lc2.Exec, lc2.Admin = w2.exec, w2.admin(root2)
	h.Lifecycle(context.Background(), lc2)
	if strings.Contains(strings.Join(w2.calls, ","), "exec:stop") {
		t.Fatalf("a stop was retried: %v", w2.calls)
	}

	// Same when a wait error (deadline) accompanies the helper's own ending.
	lc3, _, root3 := newLife(t)
	w3 := newWinFake()
	w3.execErr = errors.New("helper step deadline")
	w3.executeExit = 1
	w3.executeOut = `{"event":"execute-end","ok":false,"service_started":true,"service_stopped":false,"fence":true}`
	lc3.Exec, lc3.Admin = w3.exec, w3.admin(root3)
	h.Lifecycle(context.Background(), lc3)
	if strings.Contains(strings.Join(w3.calls, ","), "exec:stop") {
		t.Fatalf("a stop was retried after an error with the helper's ending: %v", w3.calls)
	}
}

func TestWindowsLifecycleCredentialFailureCollectsUnstarted(t *testing.T) {
	h := winHelper(t)
	lc, _, root := newLife(t)

	w := newWinFake()
	w.executeExit = 1
	w.executeOut = `{"event":"execute-end","ok":false,"service_started":false}`
	lc.Exec, lc.Admin = w.exec, w.admin(root)
	out := h.Lifecycle(context.Background(), lc)
	if out.Started || out.HelperOK || !out.Collected || strings.Join(w.calls, ",") != "admin:prepare,exec:execute,admin:collect-unstarted" {
		t.Fatalf("%+v %v", out, w.calls)
	}
	w2 := newWinFake()
	w2.prepareOK = false
	lc2, _, root2 := newLife(t)
	lc2.Exec, lc2.Admin = w2.exec, w2.admin(root2)
	out = h.Lifecycle(context.Background(), lc2)
	if out.Started || strings.Join(w2.calls, ",") != "admin:prepare,admin:collect-unstarted" {
		t.Fatalf("%v", w2.calls)
	}
}

func TestWindowsExpiredBeforeServiceStartUsesOnlyUnstartedCollection(t *testing.T) {
	h := winHelper(t)
	lc, _, root := newLife(t)
	w := newWinFake()
	w.executeExit = 1
	w.executeOut = `{"event":"execute-end","ok":false,"service_started":false,"service_stopped":true,"fence":false}`
	lc.Exec, lc.Admin = w.exec, w.admin(root)
	out := h.Lifecycle(context.Background(), lc)
	if out.Started || out.HelperOK || !out.Collected || strings.Join(w.calls, ",") != "admin:prepare,exec:execute,admin:collect-unstarted" {
		t.Fatalf("%+v %v", out, w.calls)
	}
}

func TestWindowsGateNeverAdmittedRequiresTreeProofBeforeUnstartedCollection(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		h := winHelper(t)
		lc, _, root := newLife(t)
		w := newWinFake()
		w.executeExit = 125
		w.executeOut = ""
		w.execErr = context.DeadlineExceeded
		w.treeUncertain = unknown
		lc.Exec, lc.Admin = w.exec, w.admin(root)
		out := h.Lifecycle(context.Background(), lc)
		if strings.Contains(strings.Join(w.calls, ","), "exec:stop") {
			t.Fatal("never-admitted gate was stopped through SCM")
		}
		if unknown {
			if out.Collected || out.Deferred || strings.Contains(strings.Join(w.calls, ","), "admin:collect") {
				t.Fatal("unknown tree was collected")
			}
		} else if out.Started || !out.Collected || !out.Deferred || !strings.Contains(strings.Join(w.calls, ","), "admin:collect-unstarted") {
			t.Fatalf("%+v %v", out, w.calls)
		}
	}
}

func TestUnstartedResupplyRequiresExplicitTransientCause(t *testing.T) {
	for _, reason := range []string{"deadline-before-gate", "queue-busy", "credential-denied", "audit-rejected", "helper-creation-failed"} {
		t.Run(reason, func(t *testing.T) {
			h := winHelper(t)
			lc, _, root := newLife(t)
			w := newWinFake()
			w.executeExit = 1
			w.executeOut = `{"event":"execute-end","ok":false,"service_started":false,"retryable_prestart":false}`
			want := false
			switch reason {
			case "deadline-before-gate":
				w.executeOut = ""
				w.execErr = context.DeadlineExceeded
				want = true
			case "queue-busy":
				w.executeOut = `{"event":"execute-end","ok":false,"service_started":false,"retryable_prestart":true}`
				want = true
			case "helper-creation-failed":
				w.executeOut = ""
				w.execErr = errors.New("creation failed")
			}
			lc.Exec, lc.Admin = w.exec, w.admin(root)
			o := h.Lifecycle(context.Background(), lc)
			if !o.Collected || o.Started || o.Deferred != want {
				t.Fatalf("unexpected retry classification: %+v", o)
			}
		})
	}
}

func TestWindowsCollectionOutcomeComesFromTheHelperEventNotTheBrokerExit(t *testing.T) {
	h := winHelper(t)
	for _, c := range []struct {
		out                string
		collected, residue bool
	}{
		{`{"event":"collect-end","complete":false,"residue_only":false}`, false, false},
		{`{"event":"collect-end","complete":false,"residue_only":true}`, false, false},
		{"exit=0", false, false},
		{`{"event":"collect-end","complete":true,"residue_only":false}`, true, false},
		{`{"event":"collect-end","complete":true,"shared_profile_retained":true}`, true, false},
		{`{"event":"collect-end","complete":false,"shared_profile_retained":true}`, false, false},
	} {
		lc, _, root := newLife(t)
		w := newWinFake()
		w.collectOut = c.out
		lc.Exec, lc.Admin = w.exec, w.admin(root)
		out := h.Lifecycle(context.Background(), lc)
		if out.Collected != c.collected || out.Residue != c.residue {
			t.Errorf("%q: %+v", c.out, out)
		}
	}
}

func TestAdminScriptsRejectInjectionShapes(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(t.TempDir(), "codex-owned-"+strings.Repeat("a", 32))
	for _, bad := range []string{root + "'; calc; '", root + "$(x)", root + ";x", root + "|x", root + "`x", "relative"} {
		if _, err := adminScript(dir, "prepare", bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	if _, err := adminScript(dir, "format-disk", root); err == nil {
		t.Fatal("arbitrary verb accepted")
	}
	if _, err := adminScript(dir, "collect", root); err != nil {
		t.Fatal(err)
	}
}

func linuxHelperFor(t *testing.T) linuxHelper {
	return linuxHelper{cfg: HelperConfig{Exe: writePinned(t, "civ6-ci-linux.exe", "linux-helper"), Guest: writePinned(t, "guest", "elf"), PwshTar: writePinned(t, "pwsh.tar.gz", "tar"), Seconds: 600}}
}

func linuxExec(t *testing.T, root string, receipt string, exit int, seen *[]string) ExecFunc {
	return func(ctx context.Context, pin Pinned, args []string, stdin []byte, onStart func(ProcID)) (StepResult, error) {
		*seen = append([]string(nil), args...)
		if stdin != nil {
			t.Error("the Linux helper obtains its own credential; none may be supplied")
		}
		if entries, _ := os.ReadDir(root); len(entries) != 0 {
			t.Error("the helper requires a new, empty owned root")
		}
		if receipt != "" {
			_ = os.WriteFile(filepath.Join(root, "intent.private.json"), []byte("{}"), 0600)
			if receipt != "intent-only" {
				_ = os.WriteFile(filepath.Join(root, "result.private.json"), []byte(receipt), 0600)
			}
		}
		if onStart != nil {
			onStart(ProcID{PID: 902, Created: 4})
		}
		return StepResult{Exit: exit}, nil
	}
}

func TestLinuxLifecycleRunsTheOneShotCLIAndReadsTheReceipt(t *testing.T) {
	h := linuxHelperFor(t)
	lc, rec, root := newLife(t)
	var args []string
	good := `{"Namespace":{"Name":"civ6-ci-linux-ns1","Removed":true,"TreeGone":true},"RegistrationMayExist":true,"RegistrationGone":true,"LockReleased":true,"AllRequiredChecksSuccess":true}`
	lc.Exec = linuxExec(t, root, good, 0, &args)
	out := h.Lifecycle(context.Background(), lc)
	if !out.Started || !out.HelperOK || !out.Collected || out.Runner != "civ6-ci-linux-ns1" || out.Exit != 0 {
		t.Fatalf("%+v", out)
	}
	plan := filepath.Join(lc.EntryDir, "plan.json")
	if strings.Join(args, "|") != strings.Join([]string{"run", plan, root, h.cfg.Guest.Path, h.cfg.PwshTar.Path}, "|") {
		t.Fatalf("%v", args)
	}
	if _, err := os.Stat(plan); err != nil {
		t.Fatal("plan must live outside the owned root")
	}
	if len(rec.fences) != 1 || rec.running != 1 {
		t.Fatalf("%+v", rec)
	}
}

func TestLinuxCollectionRequiresNamespaceRegistrationAndLock(t *testing.T) {
	h := linuxHelperFor(t)
	for _, c := range []struct {
		name, receipt string
		exit          int
		collected     bool
		helperOK      bool
		started       bool
	}{
		{"lock kept", `{"Namespace":{"Name":"n","Removed":true},"RegistrationMayExist":true,"RegistrationGone":true,"LockReleased":false,"AllRequiredChecksSuccess":true}`, 0, false, true, true},
		{"namespace left", `{"Namespace":{"Name":"n","Removed":false},"RegistrationMayExist":false,"LockReleased":true}`, 1, false, false, true},
		{"registration not proven gone", `{"Namespace":{"Name":"n","Removed":true},"RegistrationMayExist":true,"RegistrationGone":false,"LockReleased":true}`, 1, false, false, true},
		{"exit zero without success flag", `{"Namespace":{"Name":"n","Removed":true},"RegistrationMayExist":true,"RegistrationGone":true,"LockReleased":true,"AllRequiredChecksSuccess":false}`, 0, true, false, true},
		{"intent without receipt", "intent-only", 1, false, false, false},
		{"refused before any resource", "", 1, true, false, false},
	} {
		lc, _, root := newLife(t)
		var args []string
		lc.Exec = linuxExec(t, root, c.receipt, c.exit, &args)
		out := h.Lifecycle(context.Background(), lc)
		if out.Collected != c.collected || out.HelperOK != c.helperOK || out.Started != c.started {
			t.Errorf("%s: %+v", c.name, out)
		}
	}
}

func TestLinuxLifecycleRefusesUnpinnedInputs(t *testing.T) {
	h := linuxHelperFor(t)
	h.cfg.Guest.SHA256 = strings.Repeat("0", 64)
	lc, _, _ := newLife(t)
	called := false
	lc.Exec = func(context.Context, Pinned, []string, []byte, func(ProcID)) (StepResult, error) {
		called = true
		return StepResult{}, nil
	}
	out := h.Lifecycle(context.Background(), lc)
	if called || out.Started || out.Failure == "" {
		t.Fatalf("%+v", out)
	}
}

func TestResidueOnlyCollectionFencesUnlessAllowed(t *testing.T) {
	for _, allow := range []bool{false, true} {
		h := newHarness(t)
		h.win.out = Outcome{Started: true, HelperOK: true, Collected: true, Residue: true}
		h.d.Cfg.Helpers = map[string]HelperConfig{"Windows": {AllowResidueOnly: allow}}
		key := h.seed("Windows", 320, 3201, "test (windows-latest)", true)
		if err := h.d.Supervise(context.Background(), key); err != nil {
			t.Fatal(err)
		}
		l := h.ledger()
		e := l.Entries[key]
		if allow {
			if e.State != StateSucceeded || l.ResidueCount != 1 || len(l.Fences) != 0 || !strings.Contains(e.Cleanup.Detail, "residue_only") {
				t.Fatalf("allowed residue should be counted: %+v", e)
			}
		} else if e.State != StateNeedsRecovery || l.Fences["Windows"].Key != key || l.ResidueCount != 0 {
			t.Fatalf("residue must fence by default: %+v", e)
		}
	}
}
