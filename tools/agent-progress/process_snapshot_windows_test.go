//go:build windows

package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

func processFixture(headers ...systemProcessInformation) []byte {
	var data []byte
	for i, h := range headers {
		size := int(unsafe.Sizeof(h)) + int(h.NumberOfThreads)*int(unsafe.Sizeof(systemThreadInformation{})) + 16
		if i < len(headers)-1 {
			h.NextEntryOffset = uint32(size)
		}
		entry := make([]byte, size)
		copy(entry, unsafe.Slice((*byte)(unsafe.Pointer(&h)), int(unsafe.Sizeof(h))))
		data = append(data, entry...)
	}
	return data
}

func replaceFixtureHeader(data []byte, h systemProcessInformation) []byte {
	copy(data, unsafe.Slice((*byte)(unsafe.Pointer(&h)), int(unsafe.Sizeof(h))))
	return data
}

func TestProcessSnapshotTypedLayoutAndBounds(t *testing.T) {
	if !supportedProcessLayout() {
		t.Skip("unsupported_process_layout")
	}
	head := systemProcessInformation{UniqueProcessID: 44, CreateTime: 123456789, SessionID: 0, NumberOfThreads: 2}
	second := systemProcessInformation{UniqueProcessID: 88, CreateTime: 987654321, SessionID: 1}
	overflowPID := uint64(1) << 32
	data := processFixture(head, second)
	processes, err := parseProcessSnapshot(data, uint32(len(data)))
	if err != nil || len(processes) != 2 || processes[44].created == nil || *processes[44].created != uint64(head.CreateTime) ||
		processes[88].session != 1 || presence(44, uintPtr(123456789), func(pid uint32) processObservation { return processes[pid] }) != "verified_alive" {
		t.Fatal("typed variable process entries did not preserve FILETIME creation units")
	}
	for _, tc := range []struct {
		name     string
		data     []byte
		returned uint32
	}{
		{"empty", nil, 0}, {"short", data[:12], 12},
		{"return_length_over_buffer", data, uint32(len(data) + 1)},
		{"return_length_zero", data, 0},
		{"next_overflow", processFixture(systemProcessInformation{NextEntryOffset: ^uint32(0), UniqueProcessID: 44}), 272},
		{"next_inside_header", processFixture(systemProcessInformation{NextEntryOffset: 8, UniqueProcessID: 44}), 272},
		{"next_unaligned", processFixture(systemProcessInformation{NextEntryOffset: 257, UniqueProcessID: 44}), 272},
		{"next_without_header", processFixture(systemProcessInformation{NextEntryOffset: 272, UniqueProcessID: 44}), 272},
		{"negative_creation", processFixture(systemProcessInformation{UniqueProcessID: 44, CreateTime: -1}), 272},
		{"pid_overflow", processFixture(systemProcessInformation{UniqueProcessID: uintptr(overflowPID)}), 272},
		{"duplicate_pid", processFixture(second, second), 544},
		{"truncated_thread_array", replaceFixtureHeader(processFixture(second), systemProcessInformation{UniqueProcessID: 88, NumberOfThreads: ^uint32(0)}), 272},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseProcessSnapshot(tc.data, tc.returned)
			if err == nil || got != nil {
				t.Fatal("malformed snapshot became successful partial process evidence")
			}
		})
	}
	idle := processFixture(systemProcessInformation{UniqueProcessID: 0}, second)
	if p, err := parseProcessSnapshot(idle, uint32(len(idle))); err != nil || p[0].created != nil {
		t.Fatal("zero creation identity must remain unverified")
	}
}

func TestProcessSnapshotQueryLengthRetryAndFailure(t *testing.T) {
	if !supportedProcessLayout() {
		t.Skip("unsupported_process_layout")
	}
	data := processFixture(systemProcessInformation{UniqueProcessID: 44, CreateTime: 50})
	calls := 0
	processes, err := queryProcessSnapshot(context.Background(), func(buffer []byte, returned *uint32) uint32 {
		calls++
		if calls == 1 {
			*returned = 256 << 10
			return statusInfoLengthMismatch
		}
		if len(buffer) < 256<<10 {
			t.Error("ReturnLength not honored")
		}
		copy(buffer, data)
		*returned = uint32(len(data))
		return 0
	})
	if err != nil || len(processes) != 1 || calls != 2 {
		t.Fatal("bounded size retry did not yield the full snapshot")
	}
	for _, status := range []uint32{statusInfoLengthMismatch, statusBufferTooSmall, statusBufferOverflow} {
		calls = 0
		p, err := queryProcessSnapshot(context.Background(), func(_ []byte, _ *uint32) uint32 { calls++; return status })
		if err == nil || p != nil || calls != processSnapshotAttempts {
			t.Fatal("length race was not bounded")
		}
	}
	for _, tc := range []struct{ status, length uint32 }{
		{0xc0000022, 0}, {statusInfoLengthMismatch, processSnapshotMaxBytes + 1},
		{0, 0}, {0, processSnapshotMaxBytes},
	} {
		calls = 0
		p, err := queryProcessSnapshot(context.Background(), func(_ []byte, returned *uint32) uint32 { calls++; *returned = tc.length; return tc.status })
		if err == nil || p != nil || calls != 1 {
			t.Fatal("query failure or invalid ReturnLength became process absence")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls = 0
	if p, err := queryProcessSnapshot(ctx, func([]byte, *uint32) uint32 { calls++; return 0 }); err == nil || p != nil || calls != 0 {
		t.Fatal("expired process acquisition still queried")
	}
}

func TestProcessSnapshotDeniedHandleFallbackAndCache(t *testing.T) {
	queries, handles := 0, 0
	probe := snapshotFallbackProbe(context.Background(), func(uint32) processObservation { handles++; return processObservation{} },
		func(context.Context) (map[uint32]processObservation, error) {
			queries++
			return map[uint32]processObservation{44: {known: true, alive: true, created: uintPtr(50)}}, nil
		})
	if presence(44, uintPtr(50), probe) != "verified_alive" || presence(44, uintPtr(51), probe) != "identity_mismatch" || presence(55, uintPtr(50), probe) != "absent" {
		t.Fatal("fallback did not distinguish exact creation identity, reuse, and snapshot absence")
	}
	if queries != 1 || handles != 2 {
		t.Fatal("fallback snapshot or handle observation repeated inside one acquisition")
	}
	probe = snapshotFallbackProbe(context.Background(), func(uint32) processObservation { return processObservation{} },
		func(context.Context) (map[uint32]processObservation, error) { return nil, errors.New("query_failed") })
	if presence(44, uintPtr(50), probe) != "unknown" {
		t.Fatal("failed fallback became absent")
	}
	probe = snapshotFallbackProbe(context.Background(), func(uint32) processObservation { return processObservation{} },
		func(context.Context) (map[uint32]processObservation, error) {
			return map[uint32]processObservation{44: {known: true, alive: true, created: uintPtr(50)}}, errors.New("partial_query_failed")
		})
	if presence(44, uintPtr(50), probe) != "unknown" {
		t.Fatal("an errored partial fallback became valid identity evidence")
	}
	probe = snapshotFallbackProbe(context.Background(), func(uint32) processObservation { return processObservation{known: true} },
		func(context.Context) (map[uint32]processObservation, error) {
			t.Fatal("absent handle triggered fallback")
			return nil, nil
		})
	if presence(44, uintPtr(50), probe) != "absent" {
		t.Fatal("known handle absence was lost")
	}
}

func TestNativeWindowsSnapshotCreationCrossCheck(t *testing.T) {
	if !supportedProcessLayout() {
		t.Skip("unsupported_process_layout")
	}
	processes, err := validatedProcessSnapshot(context.Background())
	if err != nil {
		t.Fatal("real native snapshot identity validation failed")
	}
	pid := uint32(os.Getpid())
	observed := processes[pid]
	handle := nativeProbe(pid)
	if observed.created == nil || handle.created == nil || *observed.created != *handle.created {
		t.Fatal("native CreateTime overlay differs from same-handle GetProcessTimes FILETIME")
	}
}

func TestSnapshotReaderTransientRetryAndPreviousGeneration(t *testing.T) {
	for _, errno := range []syscall.Errno{5, 32} {
		elapsed, calls := time.Duration(0), 0
		fakeNow := func() time.Time { return testNow.Add(elapsed) }
		sleep := func(d time.Duration) {
			if d <= 0 {
				t.Fatal("nonpositive retry delay")
			}
			elapsed += d
		}
		read := func() ([]byte, error) {
			calls++
			if calls < 3 {
				return []byte("partial"), &os.PathError{Op: "open", Err: errno}
			}
			return []byte("next"), nil
		}
		data, err := retrySnapshotRead(read, fakeNow, sleep)
		if err != nil || string(data) != "next" || calls != 3 || elapsed != 10*time.Millisecond {
			t.Fatal("transient Windows read was not retried")
		}
		elapsed, calls = 0, 0
		previous := "previous_validated_generation"
		data, err = retrySnapshotRead(func() ([]byte, error) { calls++; return []byte("partial"), errno }, fakeNow, sleep)
		if err == nil {
			previous = string(data)
		}
		if data != nil || err == nil || previous != "previous_validated_generation" || elapsed != snapshotReadBudget || calls != 31 {
			t.Fatal("reader retry exceeded the budget or displaced the previous valid generation")
		}
	}
	calls := 0
	if data, err := retrySnapshotRead(func() ([]byte, error) { calls++; return nil, os.ErrNotExist }, time.Now, func(time.Duration) { t.Fatal("nontransient error slept") }); err == nil || data != nil || calls != 1 {
		t.Fatal("nontransient reader error was retried or ignored")
	}
}

// Explicit finite operational measurement. Only counts and closed state enums
// are logged. No PID, creation timestamp, name, key, path, or source text leaves
// this test; it does not launch, stop, repair, or modify any operational process.
func TestNativeWindowsOperationalCounts(t *testing.T) {
	if os.Getenv("AGENT_PROGRESS_NATIVE_SAMPLE") != "1" {
		t.Skip("native_sample_not_requested")
	}
	opts := defaults()
	ctx, cancel := context.WithTimeout(context.Background(), opts.Deadline)
	defer cancel()
	now := time.Now()
	var store todoStore
	todoSource := readProjection(opts.TodoFile, opts.TodoBytes, &store, now, opts.StaleAfter)
	if !todoSource.Complete {
		t.Fatal("operational_todo_read_failed")
	}
	items := map[int64]todoItem{}
	for _, item := range store.Items {
		items[item.ID] = item
	}
	loaded, source := loadJobs(ctx, opts, now, taskReferences(items, true))
	processes, err := validatedProcessSnapshot(ctx)
	if err != nil {
		t.Fatal("operational_process_snapshot_unverified")
	}
	probe := snapshotFallbackProbe(ctx, nativeProbe, func(context.Context) (map[uint32]processObservation, error) { return processes, nil })
	legacyCache := map[uint32]processObservation{}
	legacy := func(pid uint32) processObservation {
		if p, ok := legacyCache[pid]; ok {
			return p
		}
		p := nativeProbe(pid)
		legacyCache[pid] = p
		return p
	}
	counts := map[string]int{"jobs_read": len(loaded), "tasks_read": len(items), "current_session0_verified_alive": 0, "current_session0_handle_unknown": 0, "terminal_unverified_resurrected": 0, "terminal_legacy_unknown": 0, "sources_changed_during_sample": 0,
		"job_source_" + source.Status: 1, "job_source_failed_reads": source.FailedReads, "source_fingerprint_failed": 0}
	before := map[string][32]byte{}
	fingerprint := func(path string, max int64) ([32]byte, bool) {
		f, err := os.Open(path)
		if err != nil {
			return [32]byte{}, false
		}
		defer f.Close()
		hash := sha256.New()
		if _, err := io.Copy(hash, io.LimitReader(f, max+1)); err != nil {
			return [32]byte{}, false
		}
		var sum [32]byte
		copy(sum[:], hash.Sum(nil))
		return sum, true
	}
	paths := []string{opts.TodoFile}
	for _, loaded := range loaded {
		paths = append(paths, filepath.Join(opts.JobsDir, loaded.meta.ID, "job.json"))
	}
	for _, path := range paths {
		if sum, ok := fingerprint(path, opts.TodoBytes); ok {
			before[path] = sum
		} else {
			counts["source_fingerprint_failed"]++
		}
	}
	for _, loaded := range loaded {
		j := loaded.meta
		counts["job_record_"+loaded.source.Status]++
		worker, supervisor := presence(j.CodexPID, j.CodexPIDCreated, probe), presence(j.PID, j.PIDCreated, probe)
		if j.Status == "running" || j.Status == "starting" {
			counts["current_worker_"+worker]++
			counts["current_supervisor_"+supervisor]++
			for _, p := range []struct {
				pid      uint32
				expected *uint64
			}{{j.CodexPID, j.CodexPIDCreated}, {j.PID, j.PIDCreated}} {
				if observed, exists := processes[p.pid]; exists && observed.session == 0 && presence(p.pid, p.expected, probe) == "verified_alive" {
					counts["current_session0_verified_alive"]++
					if presence(p.pid, p.expected, legacy) == "unknown" {
						counts["current_session0_handle_unknown"]++
					}
				}
			}
		} else if terminalStatus(j.Status) {
			counts["terminal_worker_"+worker]++
			if presence(j.CodexPID, j.CodexPIDCreated, legacy) == "unknown" || presence(j.PID, j.PIDCreated, legacy) == "unknown" {
				counts["terminal_legacy_unknown"]++
			}
			if worker != "verified_alive" && execution(j, worker, supervisor) != "finished" {
				counts["terminal_unverified_resurrected"]++
			}
		}
	}
	for path, sum := range before {
		if after, ok := fingerprint(path, opts.TodoBytes); !ok || sum != after {
			counts["sources_changed_during_sample"]++
		}
	}
	encoded, _ := json.Marshal(counts)
	t.Log(string(encoded))
	if counts["terminal_unverified_resurrected"] != 0 || counts["current_session0_verified_alive"] == 0 {
		t.Fatal("native operational acceptance failed")
	}
}
