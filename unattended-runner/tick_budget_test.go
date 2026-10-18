package main

import (
	"context"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunCommandBudgetHelper(t *testing.T) {
	if len(os.Args) == 0 || os.Args[len(os.Args)-1] != "--repair-budget-test-child" {
		return
	}
	// A real subprocess outlives the former twenty-second command limit.
	time.Sleep(21 * time.Second)
	os.Exit(0)
}

func TestRunCommandSchedulerBudgetReachesTheActualSubprocess(t *testing.T) {
	const scheduler = "D:/CC/Tools/scheduler/scheduler.exe"
	budget := commandBudget(scheduler, []string{"repair-step"})
	if budget != 35*time.Second {
		t.Fatal("repair command retained its old inner timeout", budget)
	}
	for _, input := range []struct {
		path string
		args []string
	}{
		{scheduler, []string{"status"}},
		{scheduler, []string{"repair-step", "other"}},
		{"another.exe", []string{"repair-step"}},
	} {
		if commandBudget(input.path, input.args) != 20*time.Second {
			t.Fatal("an unrelated command received the longer repair budget")
		}
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"-test.run=^TestRunCommandBudgetHelper$", "--", "--repair-budget-test-child"}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	if _, err := runCommandBounded(ctx, budget, exe, args...); err != nil {
		t.Fatal("the real repair subprocess was stopped at the old command limit", err)
	}
	// The outer caller still cancels the same actual process promptly.
	short, stop := context.WithCancel(context.Background())
	stop()
	if _, err := runCommandBounded(short, budget, exe, args...); err == nil {
		t.Fatal("cancelled caller started a repair process")
	}
}

type tickBudgetTransport struct {
	onRequest func(context.Context)
}

func (t tickBudgetTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.onRequest(req.Context())
	return nil, req.Context().Err()
}

func TestTickIntakeCancellationLeavesSchedulingBudget(t *testing.T) {
	r, _ := fixture(t)
	parent, cancelParent := context.WithTimeout(context.Background(), time.Minute)
	defer cancelParent()
	intake, cancelIntake := context.WithCancel(parent)
	defer cancelIntake()
	requests, scheduled := 0, 0
	var requestErr error
	var schedulerErr error
	var schedulerDeadline time.Time
	r.Client.Transport = tickBudgetTransport{onRequest: func(ctx context.Context) {
		requests++
		cancelIntake()
		requestErr = ctx.Err()
	}}
	r.Run = func(ctx context.Context, _ string, args ...string) (string, error) {
		if len(args) != 0 && args[0] == "repair-step" {
			scheduled++
			schedulerErr = ctx.Err()
			schedulerDeadline, _ = ctx.Deadline()
		}
		return `{"repair_mode":true,"error":null}`, nil
	}
	h := r.tickWithIntakeContext(parent, intake)
	parentDeadline, _ := parent.Deadline()
	if requests != 1 || intake.Err() == nil || requestErr == nil {
		t.Fatal("intake did not actually exhaust its own context", requests)
	}
	if scheduled != 1 || schedulerErr != nil || !h.Scheduler {
		t.Fatal("intake cancellation starved scheduling", scheduled, schedulerErr, h)
	}
	if schedulerDeadline.IsZero() || !schedulerDeadline.Before(parentDeadline) {
		t.Fatal("scheduler escaped its bounded caller deadline")
	}
}

func TestTickCallerCancellationDoesNotStartScheduler(t *testing.T) {
	r, _ := fixture(t)
	parent, cancelParent := context.WithCancel(context.Background())
	defer cancelParent()
	intake, cancelIntake := context.WithCancel(parent)
	defer cancelIntake()
	requests, scheduled := 0, 0
	r.Client.Transport = tickBudgetTransport{onRequest: func(context.Context) {
		requests++
		cancelParent()
	}}
	r.Run = func(context.Context, string, ...string) (string, error) {
		scheduled++
		return `{"repair_mode":true,"error":null}`, nil
	}
	h := r.tickWithIntakeContext(parent, intake)
	if requests != 1 || parent.Err() == nil {
		t.Fatal("caller cancellation was not exercised")
	}
	if scheduled != 0 || h.Scheduler {
		t.Fatal("cancelled caller started or reported successful scheduling", scheduled, h)
	}
}

func TestTickAllowsSchedulerRepairBeyondTwentySecondsWithinTheCallerDeadline(t *testing.T) {
	r, _ := fixture(t)
	server(t, r, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	parent, cancel := context.WithTimeout(context.Background(), 50*time.Second)
	defer cancel()
	parentDeadline, _ := parent.Deadline()
	scheduled := 0
	r.Run = func(ctx context.Context, _ string, args ...string) (string, error) {
		if len(args) == 0 || args[0] != "repair-step" {
			return "", nil
		}
		scheduled++
		deadline, bounded := ctx.Deadline()
		// The measured native repair exceeds the previous twenty-second cap.
		// Check available time directly instead of sleeping through that period.
		if !bounded || deadline.After(parentDeadline) || time.Until(deadline) < 22*time.Second {
			return "", context.DeadlineExceeded
		}
		return `{"repair_mode":true,"error":null}`, nil
	}
	h := r.tick(parent)
	if scheduled != 1 || !h.Scheduler {
		t.Fatal("a bounded repair still cannot finish its measured work", scheduled, h)
	}
}

func TestTickSchedulesBeforeIndependentProductReporting(t *testing.T) {
	f := progressTestFixture(t)
	f.r.State.ReportTurn = "products"
	var scheduled, sourceRead, sourceReadAfterScheduler atomic.Bool
	actual := f.r.Run
	f.r.Run = func(ctx context.Context, exe string, args ...string) (string, error) {
		if len(args) != 0 && args[0] == "repair-step" {
			scheduled.Store(true)
		}
		return actual(ctx, exe, args...)
	}
	server(t, f.r, func(w http.ResponseWriter, req *http.Request) {
		if strings.HasSuffix(req.URL.Path, "/messages/"+f.source.ID) {
			sourceRead.Store(true)
			sourceReadAfterScheduler.Store(scheduled.Load())
		}
		f.serve(w, req)
	})
	h := f.r.tick(context.Background())
	if !scheduled.Load() || !sourceRead.Load() || !sourceReadAfterScheduler.Load() {
		t.Fatal("report source reads ran before scheduling or were never exercised")
	}
	if !h.Scheduler || h.ProductReports != 1 || f.sends != 1 {
		t.Fatal("scheduling or the existing single verified report was lost", h, f.sends)
	}
}

func TestTickCancellationDuringSchedulingDoesNotReportSuccess(t *testing.T) {
	r, _ := fixture(t)
	server(t, r, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	scheduled := 0
	r.Run = func(context.Context, string, ...string) (string, error) {
		scheduled++
		cancel()
		return `{"repair_mode":true,"error":null}`, nil
	}
	h := r.tick(parent)
	if scheduled != 1 || parent.Err() == nil || h.Scheduler {
		t.Fatal("cancellation during scheduling became a success", scheduled, h)
	}
}
