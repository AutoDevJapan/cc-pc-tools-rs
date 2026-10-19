package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func testBroker(t *testing.T, run func(context.Context, string) result) broker {
	t.Helper()
	b := broker{root: t.TempDir(), poll: time.Millisecond, run: run}
	if err := b.directories(); err != nil {
		t.Fatal(err)
	}
	return b
}

func fixtureScript(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "read-only.ps1")
	if err := os.WriteFile(path, []byte("Write-Output 'fixture'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func startFixture(t *testing.T, b broker) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- b.serve(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Errorf("serve: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("fixture broker did not stop")
		}
	})
}

func awaitFixture(t *testing.T, b broker, id string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var out bytes.Buffer
	if err := b.await(ctx, id, &out); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func TestRequestsArrivingDuringExecutionAndAfterIdleAreConsumed(t *testing.T) {
	entered := make(chan struct{}, 3)
	release := make(chan struct{})
	b := testBroker(t, func(ctx context.Context, script string) result {
		entered <- struct{}{}
		select {
		case <-release:
			return result{Stdout: "consumed"}
		case <-ctx.Done():
			return result{Exit: 124}
		}
	})
	script := fixtureScript(t)
	first, err := b.enqueue(script, time.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	startFixture(t, b)
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("first request did not start")
	}
	second, err := b.enqueue(script, time.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	close(release)
	for _, id := range []string{first, second} {
		if !strings.Contains(awaitFixture(t, b, id), "consumed") {
			t.Fatal("result did not contain actual runner output")
		}
	}
	// Await the archive too: result publication precedes queue cleanup.
	waitForFile(t, b.path("done", second+".ps1"))
	third, err := b.enqueue(script, time.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	awaitFixture(t, b, third)
	waitForFile(t, b.path("done", third+".ps1"))
	entries, err := os.ReadDir(b.path("requests", ""))
	if err != nil || len(entries) != 0 {
		t.Fatalf("queue not collected: %d entries, %v", len(entries), err)
	}
}

func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		if !time.Now().Before(deadline) {
			t.Fatal("expected fixture file was not published")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestExpiredAndInvalidMetadataAreNotExecuted(t *testing.T) {
	for _, tc := range []struct {
		name, meta string
		exit       int
	}{
		{"expired", `{"deadline":"2000-01-01T00:00:00Z"}`, 124},
		{"invalid", `{"deadline":"broken"}`, 125},
		{"zero", `{}`, 125},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			b := testBroker(t, func(context.Context, string) result { calls++; return result{} })
			if err := os.WriteFile(b.path("requests", "fixture.ps1"), []byte("fixture"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(b.path("requests", "fixture.meta.json"), []byte(tc.meta), 0600); err != nil {
				t.Fatal(err)
			}
			if err := b.process(context.Background(), "fixture.ps1"); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(b.path("results", "fixture.json"))
			if err != nil {
				t.Fatal(err)
			}
			var r result
			if err := json.Unmarshal(data, &r); err != nil {
				t.Fatal(err)
			}
			if calls != 0 || r.Exit != tc.exit {
				t.Fatalf("calls=%d result=%+v", calls, r)
			}
		})
	}
}

func TestLegacyRequestHasFiniteDeadline(t *testing.T) {
	var remaining time.Duration
	b := testBroker(t, func(ctx context.Context, _ string) result {
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Error("legacy request has no execution deadline")
		}
		remaining = time.Until(deadline)
		return result{Stdout: "legacy"}
	})
	if err := os.WriteFile(b.path("requests", "20261019-000000-123.ps1"), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := b.process(context.Background(), "20261019-000000-123.ps1"); err != nil {
		t.Fatal(err)
	}
	if remaining <= 0 || remaining > requestLimit {
		t.Fatalf("legacy deadline=%v", remaining)
	}
}

func TestClaimsAndExistingResultsAreNeverReplayed(t *testing.T) {
	calls := 0
	b := testBroker(t, func(context.Context, string) result { calls++; return result{} })
	for _, name := range []string{"interrupted", "completed"} {
		if err := os.WriteFile(b.path("running", name+".ps1"), []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := writeJSON(b.path("results", "completed.json"), result{Stdout: "original"}); err != nil {
		t.Fatal(err)
	}
	if err := b.recoverClaims(); err == nil {
		t.Fatal("interrupted claim incorrectly considered complete")
	}
	data, err := os.ReadFile(b.path("results", "interrupted.json"))
	if err != nil {
		t.Fatal(err)
	}
	var interrupted result
	if err := json.Unmarshal(data, &interrupted); err != nil {
		t.Fatal(err)
	}
	if interrupted.Exit != 125 || !interrupted.CleanupPending || !strings.Contains(interrupted.Stderr, "not replayed") {
		t.Fatal(interrupted)
	}
	if _, err := os.Stat(b.path("running", "interrupted.ps1")); err != nil {
		t.Fatal("interrupted claim was not preserved", err)
	}
	if _, err := os.Stat(b.path("done", "interrupted.ps1")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("unverified interrupted claim marked done")
	}
	if err := os.WriteFile(b.path("requests", "existing.ps1"), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(b.path("results", "existing.json"), result{Stdout: "existing result"}); err != nil {
		t.Fatal(err)
	}
	if err := b.process(context.Background(), "existing.ps1"); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatalf("replayed %d requests", calls)
	}
	for _, id := range []string{"completed", "existing"} {
		data, err := os.ReadFile(b.path("results", id+".json"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), map[string]string{"completed": "original", "existing": "existing result"}[id]) {
			t.Fatal("existing result overwritten")
		}
	}
}

func TestCleanupFailureRemainsRunningAcrossRecovery(t *testing.T) {
	calls := 0
	b := testBroker(t, func(context.Context, string) result {
		calls++
		return result{Exit: 125, CleanupPending: true, Stderr: "fixture cleanup failure"}
	})
	id, err := b.enqueue(fixtureScript(t), time.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if err := b.process(context.Background(), id+".ps1"); err == nil {
		t.Fatal("unverified cleanup considered complete")
	}
	if err := b.recoverClaims(); err == nil {
		t.Fatal("unverified cleanup considered recovered")
	}
	if calls != 1 {
		t.Fatalf("unverified request replayed %d times", calls)
	}
	if _, err := os.Stat(b.path("running", id+".ps1")); err != nil {
		t.Fatal("claim lost", err)
	}
	if _, err := os.Stat(b.path("requests", id+".meta.json")); err != nil {
		t.Fatal("recovery metadata lost", err)
	}
	if _, err := os.Stat(b.path("done", id+".ps1")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("unverified claim marked done")
	}
	var output bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := b.await(ctx, id, &output); err == nil {
		t.Fatal("unverified cleanup reported success")
	}
}

func TestConcurrentEnqueuePublishesCompleteUniqueRequests(t *testing.T) {
	b := testBroker(t, nil)
	script := fixtureScript(t)
	const count = 24
	ids := make(chan string, count)
	errs := make(chan error, count)
	var wg sync.WaitGroup
	for range count {
		wg.Go(func() {
			id, err := b.enqueue(script, time.Now().Add(time.Minute))
			if err != nil {
				errs <- err
				return
			}
			ids <- id
		})
	}
	wg.Wait()
	close(ids)
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	seen := map[string]bool{}
	for id := range ids {
		if seen[id] {
			t.Fatal("duplicate request id")
		}
		seen[id] = true
		data, err := os.ReadFile(b.path("requests", id+".ps1"))
		if err != nil || string(data) != "Write-Output 'fixture'\n" {
			t.Fatal("incomplete request", err)
		}
		if _, err := os.Stat(b.path("requests", id+".meta.json")); err != nil {
			t.Fatal(err)
		}
	}
	if len(seen) != count {
		t.Fatalf("got %d requests", len(seen))
	}
	entries, err := os.ReadDir(b.path("requests", ""))
	if err != nil || len(entries) != count*2 {
		t.Fatal("staging files leaked", err)
	}
}

func TestUnrelatedFormatsAndStagingFilesRemainUntouched(t *testing.T) {
	b := testBroker(t, func(context.Context, string) result { t.Error("unrelated file executed"); return result{} })
	for _, name := range []string{"legacy.json", ".broker-incomplete", ".hidden.ps1"} {
		if err := os.WriteFile(b.path("requests", name), []byte("untouched"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := b.serve(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(b.path("requests", ""))
	if err != nil || len(entries) != 3 {
		t.Fatal("unrelated file changed", err)
	}
}
