package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func civ6Config() *Civ6QueueConfig {
	// Absolute on every platform the tests run on.
	base := filepath.ToSlash(filepath.Join(os.TempDir(), "civ6-ci-queue"))
	return &Civ6QueueConfig{Exe: base + "/civ6-ci-queue.exe", SHA256: strings.Repeat("a", 64), Config: base + "/queue-config.json"}
}

func TestCiv6QueueIsOffWithoutDeployedConfiguration(t *testing.T) {
	r, _ := fixture(t)
	called := false
	r.Run = func(context.Context, string, ...string) (string, error) { called = true; return "", nil }
	h := Health{Errors: []string{}}
	r.civ6CIQueue(context.Background(), &h)
	if called || h.Civ6Queue != nil || len(h.Errors) != 0 {
		t.Fatal("queue pass ran without a pinned configuration")
	}
}

func TestCiv6QueueRunsOnlyTheFixedPinnedCommand(t *testing.T) {
	r, _ := fixture(t)
	r.Config.Civ6Queue = civ6Config()
	r.Civ6Verify = func(path, sha string) error {
		if path != r.Config.Civ6Queue.Exe || sha != r.Config.Civ6Queue.SHA256 {
			t.Fatal("wrong pin checked")
		}
		return nil
	}
	var got []string
	r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
		got = append([]string{path}, args...)
		dl, ok := ctx.Deadline()
		if !ok || time.Until(dl) > 16*time.Second {
			t.Fatal("queue pass must carry a short deadline")
		}
		return `{"status":"ok","admitted":4,"launched":2,"active":2,"budget_blocked":0,"fenced":["Windows:301/1/3011:reason"]}`, nil
	}
	h := Health{Errors: []string{}}
	r.civ6CIQueue(context.Background(), &h)
	want := []string{r.Config.Civ6Queue.Exe, "tick", "--config", r.Config.Civ6Queue.Config}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("%v", got)
	}
	if h.Civ6Queue == nil || h.Civ6Queue.Admitted != 4 || h.Civ6Queue.Launched != 2 || len(h.Errors) != 1 || h.Errors[0] != "civ6_ci_queue_fenced" {
		t.Fatalf("%+v %v", h.Civ6Queue, h.Errors)
	}
}

func TestCiv6QueueRefusesAChangedExecutableAndBadConfiguration(t *testing.T) {
	r, _ := fixture(t)
	called := false
	r.Run = func(context.Context, string, ...string) (string, error) { called = true; return "{}", nil }
	r.Config.Civ6Queue = civ6Config()
	r.Civ6Verify = func(string, string) error { return errors.New("differs") }
	h := Health{Errors: []string{}}
	r.civ6CIQueue(context.Background(), &h)
	if called || len(h.Errors) != 1 || h.Errors[0] != "civ6_ci_queue_pin_rejected" {
		t.Fatalf("%v called=%v", h.Errors, called)
	}
	for _, bad := range []Civ6QueueConfig{
		{Exe: "relative.exe", SHA256: strings.Repeat("a", 64), Config: "D:/x.json"},
		{Exe: "D:/x.exe", SHA256: "short", Config: "D:/x.json"},
		{Exe: "D:/x.exe", SHA256: strings.Repeat("a", 64), Config: "../x.json"},
	} {
		c := bad
		r.Config.Civ6Queue = &c
		h = Health{Errors: []string{}}
		r.civ6CIQueue(context.Background(), &h)
		if called || len(h.Errors) != 1 || h.Errors[0] != "civ6_ci_queue_configuration_invalid" {
			t.Fatalf("%+v %v", bad, h.Errors)
		}
	}
}

func TestCiv6QueueIsSkippedWhenTheTickIsAlmostOutOfTime(t *testing.T) {
	r, _ := fixture(t)
	r.Config.Civ6Queue = civ6Config()
	r.Civ6Verify = func(string, string) error { return nil }
	called := false
	r.Run = func(context.Context, string, ...string) (string, error) { called = true; return "{}", nil }
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	h := Health{Errors: []string{}}
	r.civ6CIQueue(ctx, &h)
	if called || len(h.Errors) != 1 || h.Errors[0] != "civ6_ci_queue_skipped_no_time" {
		t.Fatalf("%v", h.Errors)
	}
}

func TestCiv6QueueFailureOrGarbageOnlyAddsANote(t *testing.T) {
	r, _ := fixture(t)
	r.Config.Civ6Queue = civ6Config()
	r.Civ6Verify = func(string, string) error { return nil }
	r.Run = func(context.Context, string, ...string) (string, error) { return "", errors.New("timed out") }
	h := Health{Errors: []string{}}
	r.civ6CIQueue(context.Background(), &h)
	if h.Civ6Queue != nil || len(h.Errors) != 1 || h.Errors[0] != "civ6_ci_queue_deferred" {
		t.Fatalf("%v", h.Errors)
	}
	r.Run = func(context.Context, string, ...string) (string, error) { return "not json", nil }
	h = Health{Errors: []string{}}
	r.civ6CIQueue(context.Background(), &h)
	if len(h.Errors) != 1 || h.Errors[0] != "civ6_ci_queue_report_unreadable" {
		t.Fatalf("%v", h.Errors)
	}
}

type civ6FailTransport struct{}

func (civ6FailTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return nil, errors.New("offline")
}

// The queue runs after intake, scheduling and retries, and its failure leaves
// the earlier stages' results untouched.
func TestCiv6QueueRunsLastInTheExistingPeriodicTick(t *testing.T) {
	r, _ := fixture(t)
	r.Client.Transport = civ6FailTransport{}
	r.Config.Civ6Queue = civ6Config()
	r.Civ6Verify = func(string, string) error { return nil }
	var order []string
	r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
		switch {
		case len(args) != 0 && args[0] == "repair-step":
			order = append(order, "scheduler")
			return `{"repair_mode":true,"error":null}`, nil
		case len(args) != 0 && args[0] == "tick":
			order = append(order, "civ6")
			return "", errors.New("boom")
		}
		return "", errors.New("unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
	defer cancel()
	h := r.periodicTick(ctx)
	if len(order) < 2 || order[0] != "scheduler" || order[len(order)-1] != "civ6" || !h.Scheduler {
		t.Fatalf("order %v scheduler=%v", order, h.Scheduler)
	}
	found := false
	for _, e := range h.Errors {
		found = found || e == "civ6_ci_queue_deferred"
	}
	if !found {
		t.Fatalf("%v", h.Errors)
	}
}
