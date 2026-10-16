package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

const retryFixtureJob = "20261016-190000-original-product-retry-1626"

// This fake models only the CLI contract and Go crash recovery. Actual locks,
// guard/PID checks and queue admission are covered by the API's Rust tests.
type retryFake struct {
	r            *Runner
	db           map[string]any
	at           time.Time
	base         string
	effective    string
	attempt      uint64
	args         []string
	calls        []string
	jobs         map[string]string
	models       int
	binds        int
	continued    bool
	reserveError bool
	runLost      bool
	bindLost     bool
	runNoise     bool
	onReserve    func()
	onRun        func()
	onBind       func()
}

func retryDispatchFixture(t *testing.T) *retryFake {
	t.Helper()
	r, _ := fixture(t)
	db, at := productCandidateFixture(t)
	candidateRow(db)["resume_generation"] = 6
	candidateRow(db)["note"] = "preserve original note"
	candidateRow(db)["needs"] = []any{"cargo"}
	candidateRow(db)["spec"] = "original-spec.md"
	f := &retryFake{r: r, db: db, at: at, jobs: map[string]string{}}
	r.Now = func() time.Time { return f.at }
	c, err := selectProductRetry(candidateBytes(t, db), at)
	if err != nil || c == nil {
		t.Fatal("missing candidate", err)
	}
	f.base = c.Key
	f.effective = c.Key
	f.args = []string{"run", "--name", "original-product-retry-1626", "--launch-key", c.Key, "--original-product-retry-key", c.Key, "--todo-id", "1626", "--resume-session", "01a134e8-aac0-7942-8996-a0fc69357a3d", "--cwd", productRetryTree, "--model", "fixture-subscription-model", "--sandbox", "danger-full-access", "--prompt-file", "fixture saved prompt.md"}
	f.save(t)
	r.Run = func(_ context.Context, exe string, args ...string) (string, error) { return f.run(t, exe, args) }
	return f
}

func (f *retryFake) save(t *testing.T) {
	t.Helper()
	if err := os.WriteFile(f.r.TodoPath, candidateBytes(t, f.db), 0600); err != nil {
		t.Fatal(err)
	}
}
func fakeJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
func (f *retryFake) run(t *testing.T, exe string, args []string) (string, error) {
	t.Helper()
	if len(args) == 0 {
		t.Fatal("missing args")
	}
	f.calls = append(f.calls, args[0])
	switch args[0] {
	case "product-retry-reserve":
		if exe != productRetryTodoCLI || len(args) != 6 || args[1] != "1626" || args[2] != "--key" || args[3] != f.base || args[4] != "--expected-worker" || args[5] != candidateRow(f.db)["worker_receipt"].(map[string]any)["job"] {
			t.Fatal("reserve contract", exe, args)
		}
		if f.onReserve != nil {
			f.onReserve()
		}
		if f.reserveError {
			return "", errors.New("live, stop, changed, or busy")
		}
		if f.continued {
			return fakeJSON(t, map[string]any{"id": 1626, "continued_existing": true, "saved": true, "job": retryFixtureJob, "launched": 0}), nil
		}
		return fakeJSON(t, map[string]any{"id": 1626, "reserved": true, "saved": true, "key": f.effective, "base_key": f.base, "attempt": f.attempt, "launched": 0, "wait_preserved": true, "run_executable": productRetryRunCLI, "run_args": f.args}), nil
	case "run":
		if exe != productRetryRunCLI || !reflect.DeepEqual(args, f.args) {
			t.Fatal("API argument array changed", exe, args)
		}
		if f.onRun != nil {
			f.onRun()
		}
		if candidateRow(f.db)["stopped"] == true {
			return "", errors.New("owner stopped during registration")
		}
		if _, exists := f.jobs[f.effective]; !exists {
			f.jobs[f.effective] = retryFixtureJob
		}
		if f.runLost {
			f.runLost = false
			return "", errors.New("response lost after durable registration")
		}
		if f.runNoise {
			f.runNoise = false
			return "warning line\n" + retryFixtureJob + "\n", nil
		}
		return f.jobs[f.effective] + "\r\n", nil
	case "product-retry-bind":
		if exe != productRetryTodoCLI || !reflect.DeepEqual(args, []string{"product-retry-bind", "1626", "--key", f.effective, "--job", retryFixtureJob}) {
			t.Fatal("bind contract", exe, args)
		}
		if f.onBind != nil {
			f.onBind()
		}
		row := candidateRow(f.db)
		if row["stopped"] == true || row["wait_on"] != nil {
			return "", errors.New("original stop or dependency changed")
		}
		f.binds++
		row["status"] = "doing"
		delete(row, "wait_ready")
		row["jobs"] = append(row["jobs"].([]any), retryFixtureJob)
		row["worker_receipt"].(map[string]any)["job"] = retryFixtureJob
		row["scheduler"].(map[string]any)["job"] = retryFixtureJob
		row["resume_generation"] = 7
		f.save(t)
		f.models++ // Fake normal queue admits only after the binding is saved.
		if f.bindLost {
			f.bindLost = false
			return "", errors.New("response lost after binding saved")
		}
		return fakeJSON(t, map[string]any{"id": 1626, "bound": true, "reused": false, "job": retryFixtureJob, "launched": 0}), nil
	default:
		t.Fatal("unexpected command", exe, args)
		return "", nil
	}
}

func TestProductRetryMinutePathBindsSameScopeAndKeepsOriginalFields(t *testing.T) {
	f := retryDispatchFixture(t)
	var h Health
	f.r.releaseRetries(context.Background(), &h)
	if !reflect.DeepEqual(f.calls, []string{"product-retry-reserve", "run", "product-retry-bind"}) || h.Retried != 1 || len(h.Errors) != 0 || len(f.jobs) != 1 || f.models != 1 {
		t.Fatal(f.calls, h, f.models)
	}
	row := candidateRow(f.db)
	if row["dispatchable"] != false || row["spec"] != "original-spec.md" || row["note"] != "preserve original note" || !reflect.DeepEqual(row["needs"], []any{"cargo"}) {
		t.Fatal("original fields changed")
	}
	var observation productRetryObservation
	if err := readJSON(filepath.Join(f.r.Config.Home, "product-retry-observation.json"), maxState, &observation); err != nil || observation.Job != retryFixtureJob || observation.Phase != "bound" {
		t.Fatal("missing observation", err)
	}
	f.r.releaseRetries(context.Background(), &h)
	if len(f.calls) != 3 || f.models != 1 || f.binds != 1 {
		t.Fatal("bound worker duplicated")
	}
}

func TestProductRetryAcceptedCrashAndNoiseRecoverSameKey(t *testing.T) {
	for _, kind := range []string{"lost registration", "stdout noise", "lost binding"} {
		t.Run(kind, func(t *testing.T) {
			f := retryDispatchFixture(t)
			switch kind {
			case "lost registration":
				f.runLost = true
			case "stdout noise":
				f.runNoise = true
			case "lost binding":
				f.bindLost = true
			}
			var h Health
			f.r.releaseRetries(context.Background(), &h)
			if len(f.jobs) != 1 || len(h.Errors) != 1 {
				t.Fatal("missing durable registration/deferred", h)
			}
			if kind != "lost binding" && f.models != 0 {
				t.Fatal("unbound model started")
			}
			f.r.releaseRetries(context.Background(), &h)
			f.r.releaseRetries(context.Background(), &h)
			if len(f.jobs) != 1 || f.models != 1 || f.binds != 1 || candidateRow(f.db)["resume_generation"] != 7 {
				t.Fatal("duplicate after crash", f.calls, f.models, f.binds)
			}
		})
	}
}

func TestProductRetryReserveStopsAndLiveNeverReachRun(t *testing.T) {
	for _, kind := range []string{"busy", "live", "owner stop"} {
		t.Run(kind, func(t *testing.T) {
			f := retryDispatchFixture(t)
			f.reserveError = true
			if kind == "owner stop" {
				f.onReserve = func() { candidateRow(f.db)["stopped"] = true; f.save(t) }
			}
			var h Health
			f.r.releaseRetries(context.Background(), &h)
			if len(f.calls) != 1 || len(f.jobs) != 0 || f.models != 0 || candidateRow(f.db)["wait_ready"] == nil || h.Retried != 0 {
				t.Fatal("unsafe launch", f.calls, h)
			}
		})
	}
}

func TestProductRetryStopOrDependencyAfterAcceptanceRetainsWait(t *testing.T) {
	for _, kind := range []string{"owner stop", "dependency"} {
		t.Run(kind, func(t *testing.T) {
			f := retryDispatchFixture(t)
			f.onBind = func() {
				if kind == "owner stop" {
					candidateRow(f.db)["stopped"] = true
				} else {
					candidateRow(f.db)["wait_on"] = []any{999}
				}
				f.save(t)
			}
			var h Health
			f.r.releaseRetries(context.Background(), &h)
			if len(f.jobs) != 1 || f.models != 0 || candidateRow(f.db)["wait_ready"] == nil || candidateRow(f.db)["resume_generation"] != 6 || h.Retried != 0 {
				t.Fatal("original hold overwritten", h)
			}
			f.r.releaseRetries(context.Background(), &h)
			if len(f.calls) != 3 {
				t.Fatal("hold retried", f.calls)
			}
		})
	}
}

func TestProductRetryEffectiveKeyAndExistingContinuation(t *testing.T) {
	t.Run("rotated effective key", func(t *testing.T) {
		f := retryDispatchFixture(t)
		f.effective = strings.Repeat("b", 32)
		f.attempt = 1
		for i, v := range f.args {
			if v == f.base {
				f.args[i] = f.effective
			}
		}
		var h Health
		f.r.releaseRetries(context.Background(), &h)
		if f.binds != 1 || f.jobs[f.effective] != retryFixtureJob || h.Retried != 1 {
			t.Fatal("BASE confused with effective key", h)
		}
	})
	t.Run("continued queued", func(t *testing.T) {
		f := retryDispatchFixture(t)
		f.continued = true
		var h Health
		f.r.releaseRetries(context.Background(), &h)
		if len(f.calls) != 1 || f.models != 0 || f.binds != 0 || h.Retried != 1 {
			t.Fatal("queued job duplicated", f.calls, h)
		}
	})
}

func TestProductRetryExpiryBetweenStagesNeverExtendsAuthorization(t *testing.T) {
	for _, stage := range []string{"reserve", "run"} {
		t.Run(stage, func(t *testing.T) {
			f := retryDispatchFixture(t)
			expire := func() { f.at = time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC) }
			if stage == "reserve" {
				f.onReserve = expire
			} else {
				f.onRun = expire
			}
			var h Health
			f.r.releaseRetries(context.Background(), &h)
			if f.models != 0 || f.binds != 0 || candidateRow(f.db)["wait_ready"] == nil {
				t.Fatal("expired authorization used")
			}
			if stage == "reserve" && len(f.jobs) != 0 {
				t.Fatal("registered after expiry")
			}
		})
	}
}

func TestProductRetryBadReserveEnvelopeNeverExecutesReturnedCommand(t *testing.T) {
	for _, change := range []string{"executable", "base", "effective", "args", "launched", "id"} {
		t.Run(change, func(t *testing.T) {
			f := retryDispatchFixture(t)
			actual := f.r.Run
			f.r.Run = func(ctx context.Context, exe string, args ...string) (string, error) {
				raw, err := actual(ctx, exe, args...)
				if args[0] != "product-retry-reserve" {
					t.Fatal("bad receipt reached execution")
				}
				var receipt map[string]any
				if json.Unmarshal([]byte(raw), &receipt) != nil {
					t.Fatal("fake receipt invalid")
				}
				switch change {
				case "executable":
					receipt["run_executable"] = "unapproved-alias.exe"
				case "base":
					receipt["base_key"] = strings.Repeat("a", 32)
				case "effective":
					receipt["key"] = "invalid"
				case "args":
					receipt["run_args"] = []string{"run", "--todo-id", "2475"}
				case "launched":
					receipt["launched"] = 1
				case "id":
					receipt["id"] = 2475
				}
				return fakeJSON(t, receipt), err
			}
			var h Health
			f.r.releaseRetries(context.Background(), &h)
			if len(f.calls) != 1 || len(h.Errors) != 1 || len(f.jobs) != 0 {
				t.Fatal("bad envelope accepted", h)
			}
		})
	}
}
