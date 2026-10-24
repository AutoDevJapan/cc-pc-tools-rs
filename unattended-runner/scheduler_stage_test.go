package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSchedulerStageKeepsItsBudgetAndUsesOnlyTheNormalDispatcher(t *testing.T) {
	home := t.TempDir()
	now := time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)
	called := 0
	report, err := runSchedulerStage(context.Background(), home, func() time.Time { return now }, func(ctx context.Context, path string, args ...string) (string, error) {
		called++
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) < 149*time.Second {
			t.Fatal("dispatcher inherited intake's short deadline")
		}
		if path != schedulerStageProgram || len(args) != 1 || args[0] != "repair-step" {
			t.Fatal("ordinary dispatcher bypassed")
		}
		return `{"repair_mode":true,"step_failures":[]}`, nil
	})
	if err != nil || !report.OK || called != 1 || !schedulerStageHealthy(home, now) {
		t.Fatal("successful actual result was not retained")
	}
}

func TestSchedulerStageNeverTurnsFailureBusyOrUnknownIntoSuccess(t *testing.T) {
	for _, kind := range []string{"error", "busy", "failure", "malformed", "cancelled"} {
		t.Run(kind, func(t *testing.T) {
			home := t.TempDir()
			now := time.Now()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			report, err := runSchedulerStage(ctx, home, func() time.Time { return now }, func(context.Context, string, ...string) (string, error) {
				switch kind {
				case "error":
					return "", errors.New("private underlying error")
				case "busy":
					return `{"repair_mode":true,"busy":true}`, nil
				case "failure":
					return `{"repair_mode":true,"step_failures":["dispatch"]}`, nil
				case "malformed":
					return "unrelated text", nil
				default:
					cancel()
					return `{"repair_mode":true}`, nil
				}
			})
			if err != nil || report.OK || schedulerStageHealthy(home, now) || report.Error != "scheduler_step_deferred" {
				t.Fatal("failed stage claimed success or exposed raw error")
			}
		})
	}
}

func TestSchedulerStageKeepsOneOwnerAndRejectsStaleFutureOrDifferentReceipts(t *testing.T) {
	home := t.TempDir()
	now := time.Now()
	unlock, err := acquire(filepath.Join(home, "scheduler-stage.lock"))
	if err != nil {
		t.Fatal(err)
	}
	called := false
	_, err = runSchedulerStage(context.Background(), home, time.Now, func(context.Context, string, ...string) (string, error) { called = true; return "", nil })
	unlock()
	if err == nil || called {
		t.Fatal("second owner was launched")
	}
	for _, kind := range []string{"stale", "future", "backwards", "domain", "mode", "long", "missing"} {
		r := schedulerStageReport{Domain: "scheduler-stage-result-v1", Mode: "repair-step", Started: now.Add(-time.Second).Format(time.RFC3339Nano), Finished: now.Format(time.RFC3339Nano), OK: true}
		switch kind {
		case "stale":
			r.Started = now.Add(-5 * time.Minute).Format(time.RFC3339Nano)
			r.Finished = now.Add(-4 * time.Minute).Format(time.RFC3339Nano)
		case "future":
			r.Finished = now.Add(time.Second).Format(time.RFC3339Nano)
		case "backwards":
			r.Started = now.Add(time.Second).Format(time.RFC3339Nano)
		case "domain":
			r.Domain = "other"
		case "mode":
			r.Mode = "step"
		case "long":
			r.Started = now.Add(-4 * time.Minute).Format(time.RFC3339Nano)
		case "missing":
			r.Finished = ""
		}
		if publish(filepath.Join(home, "scheduler-stage-health.json"), r) != nil {
			t.Fatal("fixture write")
		}
		if schedulerStageHealthy(home, now) {
			t.Fatalf("%s report accepted", kind)
		}
	}
}

func TestSeparatedTickObservesActualResultWithoutStartingASecondDispatcher(t *testing.T) {
	r, _ := fixture(t)
	r.SeparatedScheduler = true
	if publish(filepath.Join(r.Config.Home, "scheduler-stage-health.json"), schedulerStageReport{Domain: "scheduler-stage-result-v1", Mode: "repair-step", Started: r.Now().Add(-time.Second).Format(time.RFC3339Nano), Finished: r.Now().Format(time.RFC3339Nano), OK: true}) != nil {
		t.Fatal("fixture write")
	}
	r.Run = func(context.Context, string, ...string) (string, error) {
		t.Fatal("expired intake started another dispatcher or command")
		return "", nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	health := r.tickWithIntakeContext(ctx, ctx)
	if !health.Scheduler {
		t.Fatal("separate actual result inherited exhausted intake budget")
	}
	if schedulerStageEnabled(r.Config.Home) {
		t.Fatal("separation enabled without explicit setup")
	}
	for _, raw := range []string{`{"domain":"other","enabled":true}`, `{"domain":"separated-repair-dispatcher-v1","enabled":false}`, `broken`} {
		if os.WriteFile(filepath.Join(r.Config.Home, "scheduler-stage-mode.json"), []byte(raw), 0600) != nil {
			t.Fatal("fixture write")
		}
		if schedulerStageEnabled(r.Config.Home) {
			t.Fatal("bad mode enabled separation")
		}
	}
	if os.WriteFile(filepath.Join(r.Config.Home, "scheduler-stage-mode.json"), []byte(`{"domain":"separated-repair-dispatcher-v1","enabled":true}`), 0600) != nil {
		t.Fatal("fixture write")
	}
	if !schedulerStageEnabled(r.Config.Home) {
		t.Fatal("explicit separation mode not recognized")
	}
	if !schedulerStageActive(r.Config.Home, r.Now()) {
		t.Fatal("actual fresh successful stage did not activate separation")
	}
	for _, proof := range []schedulerStageReport{
		{Domain: "scheduler-stage-result-v1", Mode: "repair-step", OK: false, Started: r.Now().Format(time.RFC3339Nano), Finished: r.Now().Format(time.RFC3339Nano)},
		{Domain: "scheduler-stage-result-v1", Mode: "repair-step", OK: true, Started: r.Now().Add(-5 * time.Minute).Format(time.RFC3339Nano), Finished: r.Now().Add(-4 * time.Minute).Format(time.RFC3339Nano)},
	} {
		if publish(filepath.Join(r.Config.Home, "scheduler-stage-health.json"), proof) != nil {
			t.Fatal("fixture write")
		}
		if schedulerStageActive(r.Config.Home, r.Now()) {
			t.Fatal("armed marker removed inline dispatcher without an actual usable result")
		}
	}
	if os.Remove(filepath.Join(r.Config.Home, "scheduler-stage-health.json")) != nil {
		t.Fatal("fixture removal")
	}
	if schedulerStageActive(r.Config.Home, r.Now()) {
		t.Fatal("missing result removed original dispatcher")
	}
	var output bytes.Buffer
	if schedulerStageCLI([]string{"--command", "other"}, &output) != 2 {
		t.Fatal("arbitrary caller operation accepted")
	}
}
