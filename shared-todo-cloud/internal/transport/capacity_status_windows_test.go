//go:build windows

package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestCapacityStatusRustUsesVerifiedCacheWithoutWriterSession(t *testing.T) {
	cli, bridge := os.Getenv("TODO_CAPACITY_TEST_CLI"), os.Getenv("TODO_CAPACITY_TEST_BRIDGE")
	if cli == "" || bridge == "" {
		t.Skip("fresh Rust CLI and Go bridge fixture binaries not supplied")
	}
	state := strings.Repeat("a", 64)
	const revision = uint64(9007199254740993)
	local := []byte("\xef\xbb\xbf{\"items\":[{\"id\":7,\"status\":\"todo\",\"capacity_decision\":{\"key\":\"capacity-decision-v1\",\"active_generation\":\"fixture-current\"}},{\"id\":8,\"title\":\"fixture-private-item\"}]}\n")
	meta := string(wire(cacheState{Revision: revision, StateID: state, SHA256: digest(local), Bytes: int64(len(local))}))
	for _, fixture := range []struct {
		name         string
		remote       Version
		metadata     string
		marker       string
		mutate       bool
		status       int
		wantCalls    int32
		wantError    string
		omitCache    bool
	}{
		{name: "valid", remote: Version{Revision: revision, StateID: state}, metadata: meta, wantCalls: 1},
		{
			name: "missing cache", remote: Version{Revision: revision, StateID: state}, metadata: meta,
			omitCache: true, wantCalls: 1, wantError: "todo_remote_cache_unavailable",
		},
		{
			name: "newer revision", remote: Version{Revision: revision + 1, StateID: state}, metadata: meta, wantCalls: 1,
			wantError: "todo_remote_cache_revision_mismatch",
		},
		{
			name: "same revision but different state", remote: Version{Revision: revision, StateID: strings.Repeat("b", 64)},
			metadata: meta, wantCalls: 1, wantError: "todo_remote_cache_revision_mismatch",
		},
		{
			name: "legacy sidecar", metadata: "{\"revision\":9007199254740993}", wantCalls: 0,
			wantError: "todo_remote_cache_revision_mismatch",
		},
		{
			name: "invalid sidecar type", metadata: strings.Replace(meta, "9007199254740993", "\"9007199254740993\"", 1), wantCalls: 0,
			wantError: "todo_remote_cache_revision_mismatch",
		},
		{
			name: "missing sidecar", wantCalls: 0, wantError: "todo_remote_cache_unavailable",
		},
		{
			name: "pending", metadata: meta, marker: "todo.json.remote-pending", wantCalls: 0,
			wantError: "todo_remote_cache_revision_mismatch",
		},
		{
			name: "invalidated", metadata: meta, marker: "todo.json.remote-invalid", wantCalls: 0,
			wantError: "todo_remote_cache_revision_mismatch",
		},
		{
			name: "mismatching bytes with equal size", remote: Version{Revision: revision, StateID: state}, metadata: meta,
			mutate: true, wantCalls: 1, wantError: "todo_remote_cache_revision_mismatch",
		},
		{
			name: "unauthorized", status: 401, metadata: meta, wantCalls: 1, wantError: "todo_remote_http_401",
		},
		{
			name: "old server", status: 404, metadata: meta, wantCalls: 1, wantError: "todo_remote_http_404",
		},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			home := t.TempDir()
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Method != http.MethodGet || r.URL.RequestURI() != "/api/v1/version" {
					t.Error("unexpected operation while reading capacity")
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				// The Rust caller must hold the exact writer lock through the GET.
				peer, openErr := os.OpenFile(filepath.Join(home, "todo.lock"), os.O_RDWR, 0)
				if openErr != nil {
					t.Error("could not open the fixture lock:", openErr)
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				lockErr := lockFile(peer)
				if lockErr == nil {
					unlockFile(peer)
				}
				peer.Close()
				if !errors.Is(lockErr, windows.ERROR_LOCK_VIOLATION) && !errors.Is(lockErr, windows.ERROR_IO_PENDING) {
					t.Error("version GET did not hold the writer byte lock:", lockErr)
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				if fixture.status != 0 {
					w.WriteHeader(fixture.status)
					_, _ = w.Write([]byte("fixture-private-response"))
					return
				}
				_ = json.NewEncoder(w).Encode(fixture.remote)
			}))
			t.Cleanup(server.Close)
			cache := filepath.Join(home, "todo.json")
			credentials := filepath.Join(home, "fixture.dpapi")
			cfg := Config{BaseURL: server.URL, SyncToken: "fixture-token", ClientID: "fixture", CachePath: cache}
			if err := SaveConfig(credentials, cfg); err != nil {
				t.Fatal(err)
			}
			files := map[string][]byte{
				"todo.json":        local,
				"todo.lock":        {},
				"todo.remote.json": wire(map[string]string{"bridge": bridge, "credentials": credentials}),
			}
			if fixture.metadata != "" {
				files["todo.json.remote-revision"] = []byte(fixture.metadata)
			}
			if fixture.marker != "" {
				files[fixture.marker] = []byte("owned marker")
			}
			if fixture.mutate {
				files["todo.json"] = bytes.Replace(local, []byte("fixture-current"), []byte("fixture-changed"), 1)
			}
			if fixture.omitCache {
				delete(files, "todo.json")
			}
			for name, body := range files {
				if err := os.WriteFile(filepath.Join(home, name), body, 0600); err != nil {
					t.Fatal(err)
				}
			}
			before := capacityFixtureFiles(t, home)
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, cli, "capacity-status")
			hideCommand(cmd)
			cmd.Env = append(os.Environ(), "CC_TODO_HOME="+home, "CC_TODO_JOBS="+home)
			cmd.WaitDelay = 5 * time.Second
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			err := cmd.Run()
			if fixture.wantError == "" {
				var projection map[string]json.RawMessage
				if err != nil || stderr.Len() != 0 || json.Unmarshal(stdout.Bytes(), &projection) != nil ||
					string(projection["registered"]) != "true" || string(projection["todo_id"]) != "7" ||
					string(projection["active_generation"]) != "\"fixture-current\"" ||
					string(projection["launch_policy"]) != "\"ordinary_scheduler\"" ||
					bytes.Contains(stdout.Bytes(), []byte("fixture-private-item")) {
					t.Fatalf("unexpected capacity projection: exit=%v; stdout=%q; stderr=%q", err, stdout.String(), stderr.String())
				}
			} else if err == nil || stdout.Len() != 0 || !strings.Contains(stderr.String(), fixture.wantError) ||
				strings.Contains(stderr.String(), "fixture-private-response") {
				t.Fatalf("inspection did not fail closed: exit=%v; stdout=%q; stderr=%q", err, stdout.String(), stderr.String())
			}
			if calls.Load() != fixture.wantCalls || !reflect.DeepEqual(capacityFixtureFiles(t, home), before) {
				t.Fatal("capacity read used extra HTTP operations or changed the store")
			}
			assertCapacityWriterLock(t, home)
		})
	}
}

func TestCapacityStatusCLIExitTerminatesTheVersionBridge(t *testing.T) {
	cli, bridge := os.Getenv("TODO_CAPACITY_TEST_CLI"), os.Getenv("TODO_CAPACITY_TEST_BRIDGE")
	if cli == "" || bridge == "" {
		t.Skip("fresh Rust CLI and Go bridge fixture binaries not supplied")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	entered := make(chan struct{}, 1)
	httpDone := make(chan struct{}, 1)
	cancelFixture := make(chan struct{})
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) != 1 || r.Method != http.MethodGet || r.URL.RequestURI() != "/api/v1/version" ||
			r.Header.Get("Authorization") != "Bearer fixture-token" {
			t.Error("unexpected operation in the cancellation fixture")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		entered <- struct{}{}
		select {
		case <-r.Context().Done():
			httpDone <- struct{}{}
		case <-cancelFixture:
		}
	}))
	defer func() {
		close(cancelFixture)
		server.Close()
	}()

	home := t.TempDir()
	cache := filepath.Join(home, "todo.json")
	credentials := filepath.Join(home, "fixture.dpapi")
	cfg := Config{BaseURL: server.URL, SyncToken: "fixture-token", ClientID: "fixture", CachePath: cache}
	if err := SaveConfig(credentials, cfg); err != nil {
		t.Fatal(err)
	}
	local := []byte("{\"items\":[]}")
	files := map[string][]byte{
		"todo.json":                 local,
		"todo.lock":                 {},
		"todo.remote.json":         wire(map[string]string{"bridge": bridge, "credentials": credentials}),
		"todo.json.remote-revision": wire(cacheState{
			Revision: 7, StateID: strings.Repeat("a", 64), SHA256: digest(local), Bytes: int64(len(local)),
		}),
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(home, name), body, 0600); err != nil {
			t.Fatal(err)
		}
	}
	before := capacityFixtureFiles(t, home)

	cmd := exec.CommandContext(ctx, cli, "capacity-status")
	hideCommand(cmd)
	cmd.Env = append(os.Environ(), "CC_TODO_HOME="+home, "CC_TODO_JOBS="+home)
	cmd.WaitDelay = time.Second
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	finished := make(chan struct{})
	var exit error
	go func() {
		exit = cmd.Wait()
		close(finished)
	}()
	defer func() {
		select {
		case <-finished:
		default:
			_ = cmd.Process.Kill()
			<-finished
		}
	}()
	select {
	case <-entered:
	case <-finished:
		t.Fatal("CLI exited before sending the version request")
	case <-ctx.Done():
		t.Fatal("version request did not reach the fixture")
	}

	// Hold the identified live child before killing the CLI. Waiting on that
	// handle cannot accidentally treat a later process with the same PID as it.
	child := holdCapacityBridge(t, cmd.Process, bridge)
	defer windows.CloseHandle(child)
	defer func() {
		status, err := windows.WaitForSingleObject(child, 0)
		if err == nil && status == windows.WAIT_OBJECT_0 {
			return
		}
		// Test cleanup also owns this exact child on a regression.
		_ = windows.TerminateProcess(child, 1)
		status, err = windows.WaitForSingleObject(child, 2000)
		if err != nil || status != windows.WAIT_OBJECT_0 {
			t.Errorf("fixture bridge was not collected: wait=%d err=%v", status, err)
		}
	}()
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal("could not cancel the fixture CLI:", err)
	}
	select {
	case <-finished:
		if exit == nil || out.Len() != 0 || stderr.Len() != 0 {
			t.Fatal("canceled CLI produced a normal outcome")
		}
	case <-ctx.Done():
		t.Fatal("canceled CLI did not finish")
	}

	status, err := windows.WaitForSingleObject(child, 2000)
	if err != nil || status != windows.WAIT_OBJECT_0 {
		t.Fatalf("bridge outlived its canceled CLI: wait=%d err=%v", status, err)
	}
	select {
	case <-httpDone:
	case <-time.After(2 * time.Second):
		t.Fatal("fixture version request was not canceled")
	}
	assertCapacityWriterLock(t, home)
	if calls.Load() != 1 || !reflect.DeepEqual(capacityFixtureFiles(t, home), before) {
		t.Fatal("cancellation added an operation or changed the fixture")
	}
}

func holdCapacityBridge(t *testing.T, parent *os.Process, image string) windows.Handle {
	t.Helper()
	var parentHandle windows.Handle
	var duplicateErr error
	if err := parent.WithHandle(func(handle uintptr) {
		current := windows.CurrentProcess()
		duplicateErr = windows.DuplicateHandle(current, windows.Handle(handle), current, &parentHandle, 0, false, windows.DUPLICATE_SAME_ACCESS)
	}); err != nil || duplicateErr != nil {
		t.Fatal("could not hold the fixture CLI:", err, duplicateErr)
	}
	defer windows.CloseHandle(parentHandle)
	parentPID, err := windows.GetProcessId(parentHandle)
	if err != nil || parentPID != uint32(parent.Pid) {
		t.Fatal("invalid fixture CLI handle:", err)
	}
	var parentCreated, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(parentHandle, &parentCreated, &exit, &kernel, &user); err != nil {
		t.Fatal(err)
	}
	if status, err := windows.WaitForSingleObject(parentHandle, 0); err != nil || status != uint32(windows.WAIT_TIMEOUT) {
		t.Fatal("fixture CLI is already exiting")
	}
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	for err = windows.Process32First(snapshot, &entry); err == nil; err = windows.Process32Next(snapshot, &entry) {
		if entry.ParentProcessID != parentPID ||
			!strings.EqualFold(windows.UTF16ToString(entry.ExeFile[:]), filepath.Base(image)) {
			continue
		}
		child, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.PROCESS_TERMINATE, false, entry.ProcessID)
		if err != nil {
			t.Fatal(err)
		}
		name := make([]uint16, 32768)
		size := uint32(len(name))
		queryErr := windows.QueryFullProcessImageName(child, 0, &name[0], &size)
		var created windows.Filetime
		timeErr := windows.GetProcessTimes(child, &created, &exit, &kernel, &user)
		status, waitErr := windows.WaitForSingleObject(child, 0)
		cliStatus, cliErr := windows.WaitForSingleObject(parentHandle, 0)
		if queryErr != nil || size == 0 || size > uint32(len(name)) ||
			!samePath(windows.UTF16ToString(name[:size]), image) ||
			timeErr != nil || created.Nanoseconds() < parentCreated.Nanoseconds() ||
			waitErr != nil || status != uint32(windows.WAIT_TIMEOUT) ||
			cliErr != nil || cliStatus != uint32(windows.WAIT_TIMEOUT) {
			windows.CloseHandle(child)
			t.Fatal("could not bind the live version bridge to its fixture")
		}
		return child
	}
	t.Fatal("version GET had no matching direct bridge child")
	return 0
}

func TestCapacityStatusReleasesTheLockBeforeOutputIsDrained(t *testing.T) {
	cli := os.Getenv("TODO_CAPACITY_TEST_CLI")
	if cli == "" {
		t.Skip("fresh Rust CLI fixture binary not supplied")
	}
	home := t.TempDir()
	largeValue := strings.Repeat("x", 2<<20)
	files := map[string][]byte{
		"todo.json": wire(map[string]any{"items": []map[string]any{{
			"id": 7, "status": "todo",
			"capacity_decision": map[string]any{"key": "capacity-decision-v1", "result": largeValue},
		}}}),
		"todo.lock": {},
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(home, name), body, 0600); err != nil {
			t.Fatal(err)
		}
	}
	before := capacityFixtureFiles(t, home)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, cli, "capacity-status")
	hideCommand(cmd)
	cmd.Env = append(os.Environ(), "CC_TODO_HOME="+home, "CC_TODO_JOBS="+home)
	cmd.WaitDelay = time.Second
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	var readHandle, writeHandle windows.Handle
	if err := windows.CreatePipe(&readHandle, &writeHandle, nil, 4096); err != nil {
		t.Fatal(err)
	}
	pipe := os.NewFile(uintptr(readHandle), "capacity stdout reader")
	defer pipe.Close()
	writer := os.NewFile(uintptr(writeHandle), "capacity stdout writer")
	defer writer.Close()
	cmd.Stdout = writer
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	// Observe stdout without draining it. PeekNamedPipe can report the bytes
	// of a pending write, so availability alone says nothing about completion.
	peek := windows.NewLazySystemDLL("kernel32.dll").NewProc("PeekNamedPipe")
	for {
		var available uint32
		ok, _, err := peek.Call(pipe.Fd(), 0, 0, 0, uintptr(unsafe.Pointer(&available)), 0)
		if ok == 0 {
			t.Fatal("could not observe pending fixture stdout:", err)
		}
		if available > 0 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("no output became pending")
		case <-time.After(10 * time.Millisecond):
		}
	}
	// The competing writer must acquire byte 0 before stdout is drained.
	assertCapacityWriterLock(t, home)
	var status uint32
	var waitErr error
	if err := cmd.Process.WithHandle(func(handle uintptr) {
		status, waitErr = windows.WaitForSingleObject(windows.Handle(handle), 0)
	}); err != nil || waitErr != nil || status != uint32(windows.WAIT_TIMEOUT) {
		t.Fatalf("capacity CLI was not running after lock acquisition with stdout undrained: wait=%d error=%v handle=%v", status, waitErr, err)
	}

	// Read EOF after checking the lock. Hitting the cap does not prove EOF.
	maxOutput := len(largeValue) + 4096
	body, err := io.ReadAll(io.LimitReader(pipe, int64(maxOutput)))
	if err != nil {
		t.Fatal(err)
	}
	if len(body) == maxOutput {
		t.Fatal("capacity fixture stdout exceeded its bound")
	}
	if err := cmd.Wait(); err != nil || stderr.Len() != 0 {
		t.Fatalf("capacity CLI did not finish cleanly: %v", err)
	}
	var result map[string]json.RawMessage
	var value string
	if err := json.Unmarshal(body, &result); err != nil || string(result["registered"]) != "true" ||
		json.Unmarshal(result["result"], &value) != nil || value != largeValue {
		t.Fatal("lock release changed the published capacity projection")
	}
	if !reflect.DeepEqual(capacityFixtureFiles(t, home), before) {
		t.Fatal("capacity status changed the fixture")
	}
}


func TestCapacityStatusReadsLegacyCacheAfterOrdinarySyncRepairsSize(t *testing.T) {
	cli, bridge := os.Getenv("TODO_CAPACITY_TEST_CLI"), os.Getenv("TODO_CAPACITY_TEST_BRIDGE")
	if cli == "" || bridge == "" {
		t.Skip("fresh Rust CLI and Go bridge fixture binaries not supplied")
	}
	l, c := newLedger(t)
	l.mu.Lock()
	l.stateID = strings.Repeat("a", 64)
	id := l.stateID
	l.mu.Unlock()
	cache := cachePath(t)
	home := filepath.Dir(cache)
	cfg := activateFixture(t, c, cache)
	local := []byte("\xef\xbb\xbf{\"items\":[{\"id\":7,\"status\":\"todo\",\"capacity_decision\":{\"key\":\"capacity-decision-v1\"}}]}\r\n")
	legacy := cacheState{Revision: 1, StateID: id, SHA256: digest(local)}
	files := map[string][]byte{
		"todo.json":                 local,
		"todo.lock":                 {},
		"todo.json.remote-revision": wire(legacy),
		"todo.remote.json": wire(map[string]string{
			"bridge": bridge, "credentials": filepath.Join(home, "fixture.dpapi"),
		}),
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(home, name), body, 0600); err != nil {
			t.Fatal(err)
		}
	}
	run := func() (stdout, stderr []byte, err error) {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, cli, "capacity-status")
		hideCommand(cmd)
		cmd.Env = append(os.Environ(), "CC_TODO_HOME="+home, "CC_TODO_JOBS="+home)
		cmd.WaitDelay = time.Second
		var errbuf bytes.Buffer
		cmd.Stderr = &errbuf
		out, err := cmd.Output()
		return out, errbuf.Bytes(), err
	}
	before := capacityFixtureFiles(t, home)
	out, stderr, err := run()
	if err == nil || len(out) != 0 || !bytes.Contains(stderr, []byte("todo_remote_cache_revision_mismatch")) {
		t.Fatal("legacy size mismatch did not fail read-only")
	}
	if !reflect.DeepEqual(capacityFixtureFiles(t, home), before) {
		t.Fatal("capacity reader tried to fill the legacy size")
	}
	l.mu.Lock()
	readsBeforeSync, leasesBeforeSync := l.versionReads, l.acquires
	l.mu.Unlock()
	if readsBeforeSync != 0 || leasesBeforeSync != 0 {
		t.Fatal("unqualified cache reached the fixture service")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := Sync(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	legacy.Bytes = int64(len(local))
	afterSync := capacityFixtureFiles(t, home)
	before["todo.json.remote-revision"] = digest(wire(legacy))
	if !reflect.DeepEqual(afterSync, before) {
		t.Fatal("sync changed more than the sidecar size")
	}
	out, stderr, err = run()
	var projection map[string]json.RawMessage
	if err != nil || len(stderr) != 0 || json.Unmarshal(out, &projection) != nil ||
		string(projection["registered"]) != "true" || string(projection["todo_id"]) != "7" {
		t.Fatalf("capacity reader rejected the repaired cache: %v", err)
	}
	if !reflect.DeepEqual(capacityFixtureFiles(t, home), afterSync) {
		t.Fatal("capacity reader wrote the repaired cache")
	}
	assertCapacityWriterLock(t, home)
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.acquires != 1 || l.releases != 1 || l.versionReads != 1 || l.stateReads != 0 ||
		l.changeReads != 0 || l.commits != 0 {
		t.Fatal("more than the one ordinary sync and one successful version GET reached the fixture")
	}
}

func assertCapacityWriterLock(t *testing.T, home string) {
	t.Helper()
	lock, err := os.OpenFile(filepath.Join(home, "todo.lock"), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := lockFile(lock); err != nil {
		t.Fatal("writer could not acquire the shared byte:", err)
	}
	unlockFile(lock)
}
