package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// This opt-in fixture uses freshly built test binaries, never installed tools.
// Supply TODO_REMOTE_TEST_CLI and TODO_REMOTE_TEST_BRIDGE to run it.
func TestRustCLIToProtectedGoBridgeTransaction(t *testing.T) {
	cli, bridge := os.Getenv("TODO_REMOTE_TEST_CLI"), os.Getenv("TODO_REMOTE_TEST_BRIDGE")
	if cli == "" || bridge == "" {
		t.Skip("fresh CLI and bridge fixture binaries not supplied")
	}
	l, c := newLedger(t)
	path := cachePath(t)
	home := filepath.Dir(path)
	credentials := filepath.Join(home, "fixture.dpapi")
	if err := SaveConfig(credentials, Config{BaseURL: c.base, SyncToken: "fixture-token", ClientID: "fixture-pc", CachePath: path}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "todo.remote.json"), wire(map[string]string{"bridge": bridge, "credentials": credentials}), 0600); err != nil {
		t.Fatal(err)
	}
	localHome := t.TempDir()
	if err := os.WriteFile(filepath.Join(localHome, "todo.json"), []byte(fixture), 0600); err != nil {
		t.Fatal(err)
	}
	run := func(dir string, args ...string) ([]byte, error) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, cli, args...)
		hideCommand(cmd)
		cmd.Env = append(os.Environ(), "CC_TODO_HOME="+dir, "CC_TODO_JOBS="+dir)
		return cmd.CombinedOutput()
	}
	expected, err := run(localHome, "progress", "1", "42")
	if err != nil {
		t.Fatal("fixture local CLI failed")
	}
	actual, err := run(home, "progress", "1", "42")
	if err != nil {
		t.Fatalf("fixture remote CLI failed: %s", actual)
	}
	if !bytes.Equal(expected, actual) {
		t.Fatal("remote changed legacy stdout")
	}
	d, rev, err := loadCache(path)
	if err != nil || rev != 2 {
		t.Fatal("remote cache revision not committed")
	}
	var item map[string]json.RawMessage
	_ = json.Unmarshal(d.items[1], &item)
	if string(item["progress"]) != "42" || string(item["lastJob"]) != `"UPPER"` || string(item["lastjob"]) != "null" || string(item["spec"]) != `"C:/fixture/original.md"` {
		t.Fatal("Rust transaction lost original fields")
	}
	for _, fragment := range []string{`9007199254740993`, `1.2300e-7`, `0.10000000000000001`} {
		if !bytes.Contains(read(t, path), []byte(fragment)) {
			t.Fatal("Rust round trip altered an untouched original number")
		}
	}
	// Reacquire at the new server revision and serialize simultaneous processes.
	done := make(chan error, 2)
	go func() { _, err := run(home, "progress", "1", "43"); done <- err }()
	go func() { _, err := run(home, "progress", "2", "44"); done <- err }()
	for i := 0; i < 2; i++ {
		if err := <-done; err != nil {
			t.Fatal("concurrent CLI failed")
		}
	}
	_, rev, err = loadCache(path)
	if err != nil || rev != 4 {
		t.Fatal("reacquired revision was stale")
	}
	l.mu.Lock()
	released := l.releases
	l.commitStatus = 503
	l.mu.Unlock()
	if released != 3 {
		t.Fatal("Rust guard did not release lease before returning")
	}
	original, revision := read(t, path), read(t, path+".remote-revision")
	if _, err := run(home, "progress", "1", "45"); err == nil {
		t.Fatal("failed remote commit reported success")
	}
	if !bytes.Equal(read(t, path), original) || !bytes.Equal(read(t, path+".remote-revision"), revision) {
		t.Fatal("Rust fell back to local write")
	}
	l.mu.Lock()
	l.commitStatus = 0
	l.mu.Unlock()
	if _, err := run(home, "progress", "999", "10"); err == nil {
		t.Fatal("legacy validation was bypassed")
	}
	if !bytes.Equal(read(t, path), original) {
		t.Fatal("invalid command mutated cache")
	}
	l.mu.Lock()
	released = l.releases
	l.mu.Unlock()
	if released != 5 {
		t.Fatal("error or validation exit leaked remote lease")
	}
	pending, err := filepath.Glob(filepath.Join(home, ".todo-remote-*"))
	if err != nil || len(pending) != 0 {
		t.Fatal("candidate cleanup failed")
	}
}

func TestRustSchedulerAndQuestionWriterUseSameRemoteTransaction(t *testing.T) {
	bridge := os.Getenv("TODO_REMOTE_TEST_BRIDGE")
	for _, fixture := range []struct{ env, filter, field string }{
		{"TODO_REMOTE_TEST_SCHEDULER", "common::remote_tests::bridge_transaction_fixture", "scheduler_fixture"},
		{"TODO_REMOTE_TEST_ASK", "core::remote_tests::bridge_transaction_fixture", "ask_fixture"},
	} {
		t.Run(fixture.field, func(t *testing.T) {
			binary := os.Getenv(fixture.env)
			if binary == "" || bridge == "" {
				t.Skip("fresh Rust writer fixture binary not supplied")
			}
			l, c := newLedger(t)
			path := cachePath(t)
			home := filepath.Dir(path)
			credentials := filepath.Join(home, "fixture.dpapi")
			if err := SaveConfig(credentials, Config{BaseURL: c.base, SyncToken: "fixture-token", ClientID: "fixture-pc", CachePath: path}); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(home, "todo.remote.json"), wire(map[string]string{"bridge": bridge, "credentials": credentials}), 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, binary, "--exact", fixture.filter, "--ignored", "--nocapture")
			hideCommand(cmd)
			cmd.Env = append(os.Environ(), "TODO_REMOTE_FIXTURE_HOME="+home)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("Rust writer failed: %s", out)
			}
			if !bytes.Contains(out, []byte("1 passed")) {
				t.Fatal("Rust writer fixture did not run")
			}
			d, revision, err := loadCache(path)
			if err != nil || revision != 2 || !bytes.Contains(d.items[1], []byte(fixture.field)) {
				t.Fatal("writer bypassed remote authority")
			}
			l.mu.Lock()
			defer l.mu.Unlock()
			if l.commits != 1 || l.releases != 1 {
				t.Fatal("writer did not commit and release exactly once")
			}
		})
	}
}

func runRustCLIOutcome(t *testing.T, cli, home string, args ...string) (int, []byte, []byte) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, cli, args...)
	hideCommand(cmd)
	cmd.Env = append(os.Environ(), "CC_TODO_HOME="+home, "CC_TODO_JOBS="+home)
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	err := cmd.Run()
	if err == nil {
		return 0, out.Bytes(), errOut.Bytes()
	}
	if exit, ok := err.(*exec.ExitError); ok {
		return exit.ExitCode(), out.Bytes(), errOut.Bytes()
	}
	t.Fatal("fixture CLI failed to start")
	return 0, nil, nil
}
func TestRustCLIConfirmedAddCacheFailureDoesNotReportFailedAdd(t *testing.T) {
	cli, bridge := os.Getenv("TODO_REMOTE_TEST_CLI"), os.Getenv("TODO_REMOTE_TEST_BRIDGE")
	if cli == "" || bridge == "" {
		t.Skip("fresh CLI and bridge fixture binaries not supplied")
	}
	l, c := newLedger(t)
	path := cachePath(t)
	home := filepath.Dir(path)
	cfg := activateFixture(t, c, path)
	marker := filepath.Join(home, "todo.remote.json")
	if err := atomicWrite(marker, wire(map[string]string{"bridge": bridge, "credentials": filepath.Join(home, "fixture.dpapi")})); err != nil {
		t.Fatal(err)
	}
	l.mu.Lock()
	l.changeFailAt = 1
	l.mu.Unlock()
	code, out, errOut := runRustCLIOutcome(t, cli, home, "add", "demo", "fixture-confirmed-add")
	if code != 0 || !bytes.Equal(out, []byte("added 3\n")) {
		t.Fatalf("confirmed add was exposed as failed: exit=%d stderr=%s", code, errOut)
	}
	if !needsFresh(cfg.CachePath) {
		t.Fatal("confirmed add lost durable cache invalidation")
	}
	code, _, _ = runRustCLIOutcome(t, cli, home, "list", "demo")
	if code != 0 {
		t.Fatal("next CLI did not reconcile confirmed add")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.commits != 1 || len(l.doc.items) != 3 {
		t.Fatal("cache failure duplicated add")
	}
}
func TestRustCLIUnconfirmedHasDistinctExitAndResolvesBeforeNextCommand(t *testing.T) {
	cli, bridge := os.Getenv("TODO_REMOTE_TEST_CLI"), os.Getenv("TODO_REMOTE_TEST_BRIDGE")
	if cli == "" || bridge == "" {
		t.Skip("fresh CLI and bridge fixture binaries not supplied")
	}
	l, c := newLedger(t)
	path := cachePath(t)
	home := filepath.Dir(path)
	_ = activateFixture(t, c, path)
	if err := atomicWrite(filepath.Join(home, "todo.remote.json"), wire(map[string]string{"bridge": bridge, "credentials": filepath.Join(home, "fixture.dpapi")})); err != nil {
		t.Fatal(err)
	}
	l.mu.Lock()
	l.dropAllReplies = true
	l.operationStatus = 503
	l.mu.Unlock()
	code, out, errOut := runRustCLIOutcome(t, cli, home, "progress", "1", "42")
	if code != 76 || len(out) != 0 || !bytes.Contains(errOut, []byte(`"error":"todo_remote_commit_unconfirmed"`)) {
		t.Fatal("unknown result became generic exit1/retry or false success")
	}
	code, _, _ = runRustCLIOutcome(t, cli, home, "list", "demo")
	if code != 76 {
		t.Fatal("unresolved journal allowed another command")
	}
	l.mu.Lock()
	l.dropAllReplies = false
	l.operationStatus = 0
	l.mu.Unlock()
	code, _, errOut = runRustCLIOutcome(t, cli, home, "list", "demo")
	if code != 0 {
		t.Fatalf("known operation did not recover: code=%d stderr=%s", code, errOut)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.commits != 1 {
		t.Fatal("resolution reran domain action")
	}
}

func TestRustDirectWritersPreserveUnconfirmedOutcome(t *testing.T) {
	bridge := os.Getenv("TODO_REMOTE_TEST_BRIDGE")
	for _, fixture := range []struct{ env, filter, field string }{
		{"TODO_REMOTE_TEST_SCHEDULER", "common::remote_tests::bridge_unconfirmed_fixture", "scheduler_unknown"},
		{"TODO_REMOTE_TEST_ASK", "core::remote_tests::bridge_unconfirmed_fixture", "ask_unknown"},
	} {
		t.Run(fixture.field, func(t *testing.T) {
			binary := os.Getenv(fixture.env)
			if bridge == "" || binary == "" {
				t.Skip("fresh writer fixtures not supplied")
			}
			l, c := newLedger(t)
			path := cachePath(t)
			home := filepath.Dir(path)
			_ = activateFixture(t, c, path)
			if err := atomicWrite(filepath.Join(home, "todo.remote.json"), wire(map[string]string{"bridge": bridge, "credentials": filepath.Join(home, "fixture.dpapi")})); err != nil {
				t.Fatal(err)
			}
			l.mu.Lock()
			l.commitStatus = 503
			l.mu.Unlock()
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, binary, "--exact", fixture.filter, "--ignored", "--nocapture")
			hideCommand(cmd)
			cmd.Env = append(os.Environ(), "TODO_REMOTE_FIXTURE_HOME="+home)
			out, err := cmd.CombinedOutput()
			if err != nil || !bytes.Contains(out, []byte("1 passed")) {
				t.Fatalf("writer did not preserve outcome type: %s", out)
			}
			if bytes.Contains(read(t, path), []byte(fixture.field)) || !needsFresh(path) {
				t.Fatal("unknown result fell back to a local write or lost journal")
			}
		})
	}
}
