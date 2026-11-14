//go:build windows

package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

type askJobCollectionModel struct {
	listValue askJobMemberList
	listError, openError, identityError, membershipError, accountingError, terminateError, waitError error
	notMember bool
	ids map[syscall.Handle]askNativeProcess
	states map[syscall.Handle]uint32
	account askJobAccounting
	opens map[uint32]int
	closes map[syscall.Handle]int
	waits map[syscall.Handle]int
	terminates int
	latePID uint32
	lateWaitPID uint32
	closeError bool
	clock time.Time
	closeAdvance time.Duration
	accountingCalls int
	accountingFailAfter int
	accountingAdvance time.Duration
	waitAdvanceHandle syscall.Handle
	waitAdvance time.Duration
}

func newAskJobCollectionModel() *askJobCollectionModel {
	return &askJobCollectionModel{
		listValue: askJobMemberList{assigned: 2, pids: []uint32{1, 2}},
		ids: map[syscall.Handle]askNativeProcess{11: {PID: 1, Creation: 100}, 12: {PID: 2, Creation: 200}},
		states: map[syscall.Handle]uint32{11: syscall.WAIT_OBJECT_0, 12: syscall.WAIT_OBJECT_0},
		account: askJobAccounting{Total: 2},
		opens: make(map[uint32]int), closes: make(map[syscall.Handle]int), waits: make(map[syscall.Handle]int),
	}
}

func (m *askJobCollectionModel) list(syscall.Handle) (askJobMemberList, error) {
	return m.listValue, m.listError
}
func (m *askJobCollectionModel) open(pid uint32) (syscall.Handle, error) {
	m.opens[pid]++
	return syscall.Handle(pid + 10), m.openError
}
func (m *askJobCollectionModel) identity(h syscall.Handle) (askNativeProcess, error) {
	return m.ids[h], m.identityError
}
func (m *askJobCollectionModel) belongs(syscall.Handle, syscall.Handle) (bool, error) {
	return !m.notMember, m.membershipError
}
func (m *askJobCollectionModel) wait(h syscall.Handle) (uint32, error) {
	m.waits[h]++
	if h == m.waitAdvanceHandle && !m.clock.IsZero() { m.clock = m.clock.Add(m.waitAdvance) }
	if m.lateWaitPID != 0 {
		id := m.lateWaitPID
		m.lateWaitPID = 0
		m.listValue.pids = append(m.listValue.pids, id)
		m.listValue.assigned++
		m.account.Total++
		m.ids[syscall.Handle(id+10)] = askNativeProcess{PID: id, Creation: uint64(id) * 100}
		m.states[syscall.Handle(id+10)] = syscall.WAIT_TIMEOUT
	}
	return m.states[h], m.waitError
}
func (m *askJobCollectionModel) accounting(syscall.Handle) (askJobAccounting, error) {
	m.accountingCalls++
	if m.accountingFailAfter != 0 && m.accountingCalls > m.accountingFailAfter { return askJobAccounting{}, errAskJobCollection }
	if !m.clock.IsZero() { m.clock = m.clock.Add(m.accountingAdvance) }
	return m.account, m.accountingError
}
func (m *askJobCollectionModel) terminate(syscall.Handle) error {
	m.terminates++
	if m.latePID != 0 {
		id := m.latePID
		m.listValue.pids = append(m.listValue.pids, id)
		m.listValue.assigned++
		m.account.Total++
		m.ids[syscall.Handle(id+10)] = askNativeProcess{PID: id, Creation: uint64(id) * 100}
		m.states[syscall.Handle(id+10)] = syscall.WAIT_OBJECT_0
	}
	return m.terminateError
}
func (m *askJobCollectionModel) close(h syscall.Handle) error {
	m.closes[h]++
	if !m.clock.IsZero() { m.clock = m.clock.Add(m.closeAdvance) }
	if m.closeError {
		return errAskJobCollection
	}
	return nil
}

func (m *askJobCollectionModel) now() time.Time {
	if !m.clock.IsZero() { return m.clock }
	return time.Now()
}

func TestAskJobCollectionKeepsFailuresUnknown(t *testing.T) {
	for _, fault := range []string{"query", "truncated", "open", "creation", "membership-error", "not-member", "zero-pid", "duplicate-pid", "held-generation", "root-wait", "active", "unsignalled", "wait-error", "accounting", "termination", "historical-gap", "close-error", "deadline"} {
		t.Run(fault, func(t *testing.T) {
			m := newAskJobCollectionModel()
			c := newAskJobCollection(m, 9999, 11, 1, true)
			rootWait := true
			deadline := time.Now().Add(5 * time.Millisecond)
			switch fault {
			case "query": m.listError = errAskJobCollection
			case "truncated": m.listValue.assigned = 130
			case "open": m.openError = errAskJobCollection
			case "creation": m.ids[12] = askNativeProcess{PID: 2}
			case "membership-error": m.membershipError = errAskJobCollection
			case "not-member": m.notMember = true
			case "zero-pid": m.listValue.pids[1] = 0
			case "duplicate-pid": m.listValue.pids[1] = 1
			case "held-generation": c.Observe(); m.ids[12] = askNativeProcess{PID: 2, Creation: 300}
			case "root-wait": rootWait = false
			case "active": m.account.Active = 1
			case "unsignalled": m.states[12] = syscall.WAIT_TIMEOUT
			case "wait-error": m.waitError = errAskJobCollection
			case "accounting": m.accountingError = errAskJobCollection
			case "termination": m.terminateError = errAskJobCollection
			case "historical-gap": m.account.Total = 3
			case "close-error": m.closeError = true
			case "deadline": deadline = time.Now().Add(-time.Second)
			}
			r := c.Finish(rootWait, deadline)
			if r.Reaped || r.Reason == "" || m.terminates != 1 {
				t.Fatal("incomplete collection accepted", fault, r, m.terminates)
			}
			// Removing the fault cannot turn a previously unknown observation
			// into a completed lifetime proof, nor repeat job termination.
			m.listError, m.openError, m.membershipError = nil, nil, nil
			if again := c.Finish(true, time.Now().Add(time.Second)); again != r || m.terminates != 1 {
				t.Fatal("unknown result was overwritten", again, r)
			}
			firstClose, secondClose := c.Close(), c.Close()
			if (firstClose != nil) != (fault == "close-error") || firstClose != secondClose || m.closes[11] != 0 || m.closes[9999] != 0 || m.closes[12] > m.opens[2] {
				t.Fatal("borrowed handles were closed")
			}
		})
	}
}

func TestAskJobCollectionDeadlineIncludesClose(t *testing.T) {
	for _, late := range []bool{false, true} {
		t.Run(strconv.FormatBool(late), func(t *testing.T) {
			m := newAskJobCollectionModel()
			m.clock = time.Unix(100, 0)
			if late { m.closeAdvance = 10*time.Millisecond }
			deadline := m.clock.Add(5*time.Millisecond)
			c := newAskJobCollection(m, 9999, 11, 1, true)
			defer c.Close()
			r := c.Finish(true, deadline)
			// These asserts precede any separate Close call, so a timeout
			// before reaching the intended cleanup cannot satisfy the case.
			if m.closes[12] != 1 || r.Observed != 2 || r.Total != 2 || m.waits[11] == 0 || m.waits[12] == 0 {
				t.Fatal("close boundary was not exercised", r, m.closes, m.waits)
			}
			if r.Reaped != !late || late && r.Reason != "job_collection_deadline_exceeded" || !late && r.Reason != "" {
				t.Fatal("post-close deadline result", r, late)
			}
		})
	}
}

func TestAskJobCollectionKeepsLastObservedTotal(t *testing.T) {
	for _, fault := range []string{"active", "unsignalled", "accounting-fails", "after-accounting-fails", "before-deadline", "normal", "history-gap"} {
		t.Run(fault, func(t *testing.T) {
			m := newAskJobCollectionModel()
			m.clock = time.Unix(100, 0)
			c := newAskJobCollection(m, 9999, 11, 1, true)
			defer c.Close()
			deadline := m.clock.Add(5*time.Millisecond)
			switch fault {
			case "active": m.account.Active = 1; m.accountingAdvance = 10*time.Millisecond
			case "unsignalled": m.states[12] = syscall.WAIT_TIMEOUT; m.waitAdvanceHandle = 12; m.waitAdvance = 10*time.Millisecond
			case "accounting-fails": m.accountingError = errAskJobCollection
			case "after-accounting-fails": m.account.Active = 1; m.accountingFailAfter = 1
			case "before-deadline": deadline = m.clock.Add(-time.Second)
			case "history-gap": m.account.Total = 3
			}
			r := c.Finish(true, deadline)
			known := fault != "accounting-fails" && fault != "before-deadline"
			want := uint32(2)
			if fault == "history-gap" { want = 3 }
			if !known { want = 0 }
			if r.TotalKnown != known || r.Total != want || r.Reaped != (fault == "normal") { t.Fatal("last observation lost or promoted to proof", fault, r) }
			if known && m.accountingCalls == 0 || !known && r.Total != 0 { t.Fatal("missing observation represented as known", fault, r) }
			if fault == "unsignalled" && (m.waits[12] == 0 || r.Reason != "job_collection_deadline_exceeded") { t.Fatal("unsignalled wait boundary was not reached", r, m.waits) }
		})
	}
}

func TestAskJobCollectionRequiresRootAndEveryHeldMember(t *testing.T) {
	m := newAskJobCollectionModel()
	c := newAskJobCollection(m, 9999, 11, 1, true)
	c.Observe()
	if len(c.members) != 2 || c.members[1].process != 11 || c.members[2].process != 12 {
		t.Fatal("model did not pin actual fixture identities")
	}
	r := c.Finish(true, time.Now().Add(time.Second))
	if !r.Reaped || r.Reason != "" || r.Observed != 2 || r.Total != 2 || m.waits[11] == 0 || m.waits[12] == 0 {
		t.Fatal("complete collection not accepted", r)
	}
	if c.Close() != nil || m.closes[12] != 1 || m.closes[11] != 0 || m.closes[9999] != 0 || m.opens[2] != 1 {
		t.Fatal("pin ownership changed", m.closes, m.opens)
	}
}

func TestAskJobCollectionObservationFailureIsSticky(t *testing.T) {
	m := newAskJobCollectionModel()
	c := newAskJobCollection(m, 9999, 11, 1, true)
	defer c.Close()
	m.listError = errAskJobCollection
	c.Observe()
	m.listError = nil
	c.Observe()
	if len(c.members) != 2 { t.Fatal("recovered query did not pin both fixture processes") }
	r := c.Finish(true, time.Now().Add(time.Second))
	if r.Reaped || r.Reason != "job_member_query_failed" || r.Observed != 2 || r.Total != 2 {
		t.Fatal("successful later query erased an unknown interval", r)
	}
}

func TestAskJobCollectionLateMemberCannotUseEarlierSignals(t *testing.T) {
	m := newAskJobCollectionModel()
	m.listValue = askJobMemberList{assigned: 1, pids: []uint32{1}}
	m.account.Total = 1
	m.lateWaitPID = 2
	c := newAskJobCollection(m, 9999, 11, 1, true)
	defer c.Close()
	r := c.Finish(true, time.Now().Add(5*time.Millisecond))
	if r.Reaped || len(c.members) != 2 || m.waits[12] == 0 {
		t.Fatal("late unsignalled member used the earlier root-only barrier", r)
	}
}

func TestAskJobCollectionCapacityAndNativeFailureResults(t *testing.T) {
	for _, fault := range []string{"over-capacity", "root-zero", "nil-api", "job-zero", "root-creation-zero", "closed"} {
		t.Run(fault, func(t *testing.T) {
			m := newAskJobCollectionModel()
			var api askJobCollectionAPI = m
			root, job := syscall.Handle(11), syscall.Handle(9999)
			switch fault {
			case "over-capacity": m.listValue.assigned = 4097; m.listValue.pids = make([]uint32, 4097)
			case "root-zero": root = 0
			case "nil-api": api = nil
			case "job-zero": job = 0
			case "root-creation-zero": m.ids[11] = askNativeProcess{PID: 1}
			}
			c := newAskJobCollection(api, job, root, 1, true)
			defer c.Close()
			if fault == "closed" { c.Close() }
			if r := c.Finish(true, time.Now().Add(5*time.Millisecond)); r.Reaped || r.Reason == "" {
				t.Fatal("missing native proof accepted", fault, r)
			}
		})
	}
}

func TestAskJobCollectionHandlesMoreThan128AndLateMembers(t *testing.T) {
	for _, size := range []uint32{2, 130} {
		t.Run(strconv.FormatUint(uint64(size), 10), func(t *testing.T) {
			m := newAskJobCollectionModel()
			m.listValue = askJobMemberList{assigned: size}
			m.account.Total = size
			for i := uint32(1); i <= size; i++ {
				m.listValue.pids = append(m.listValue.pids, i)
				m.ids[syscall.Handle(i+10)] = askNativeProcess{PID: i, Creation: uint64(i) * 100}
				m.states[syscall.Handle(i+10)] = syscall.WAIT_OBJECT_0
			}
			m.latePID = size + 1
			c := newAskJobCollection(m, 9999, 11, 1, true)
			defer c.Close()
			c.Observe()
			r := c.Finish(true, time.Now().Add(time.Second))
			if !r.Reaped || r.Observed != size+1 || r.Total != size+1 || m.waits[syscall.Handle(m.latePID+10)] == 0 {
				t.Fatal("late member was not pinned and waited", r)
			}
		})
	}
}

func TestAskJobCollectionRejectsRootAndPidReuse(t *testing.T) {
	for _, fault := range []string{"unassigned", "wrong-pid", "identity-error", "root-not-job", "reused-list-pid"} {
		t.Run(fault, func(t *testing.T) {
			m := newAskJobCollectionModel()
			assigned := true
			switch fault {
			case "unassigned": assigned = false
			case "wrong-pid": m.ids[11] = askNativeProcess{PID: 5, Creation: 100}
			case "identity-error": m.identityError = errAskJobCollection
			case "root-not-job": m.notMember = true
			case "reused-list-pid": m.ids[12] = askNativeProcess{PID: 3, Creation: 300}
			}
			c := newAskJobCollection(m, 9999, 11, 1, assigned)
			defer c.Close()
			if r := c.Finish(true, time.Now().Add(5*time.Millisecond)); r.Reaped || r.Reason == "" {
				t.Fatal("unrelated root/member accepted", fault, r)
			}
		})
	}
}

// This is an actual owned Windows process fixture. Its root is started
// suspended and assigned to a kill-on-close job before it can spawn children.
// The descendant stays alive until the test terminates that exact job.
func TestAskJobCollectionOwnedWindowsTree(t *testing.T) {
	for _, fault := range []string{"normal", "late-child", "query-failure", "early-exit"} {
		t.Run(fault, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			job, _, _ := askJobCollectionKernel.NewProc("CreateJobObjectW").Call(0, 0)
			if job == 0 { t.Fatal("create owned job") }
			limits := askJobLimits{Flags: 0x2000}
			ok, _, _ := askJobCollectionKernel.NewProc("SetInformationJobObject").Call(job, 9, uintptr(unsafe.Pointer(&limits)), unsafe.Sizeof(limits))
			if ok == 0 { syscall.CloseHandle(syscall.Handle(job)); t.Fatal("set owned job limits") }
			api := askNativeJobCollectionAPI{}
			defer syscall.CloseHandle(syscall.Handle(job))
			defer api.terminate(syscall.Handle(job))
			exe, err := os.Executable()
			if err != nil { t.Fatal(err) }
			cmd := exec.CommandContext(ctx, exe, "-test.run=^TestAskJobCollectionProcessHelper$")
			mode := "root"
			if fault == "early-exit" { mode = "early-exit" }
			cmd.Env = append(os.Environ(), "ASK_JOB_COLLECTION_TEST="+mode)
			cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000 | 0x00000004}
			output, outputWrite, err := os.Pipe()
			if err != nil { t.Fatal(err) }
			defer output.Close()
			defer outputWrite.Close()
			cmd.Stdout = outputWrite
			input, err := cmd.StdinPipe()
			if err != nil { t.Fatal(err) }
			defer input.Close()
			if err := cmd.Start(); err != nil { t.Fatal(err) }
			if err := outputWrite.Close(); err != nil { t.Error("parent stdout writer close", err) }
			waited := make(chan error, 1)
			go func() { waited <- cmd.Wait() }()
			var root, child, lateChild syscall.Handle
			var collector *askJobCollection
			var cleanupCollector *askJobCollection
			var readyDone chan struct{}
			var readerErr chan error
			joined := false
			defer func() {
				if cleanupCollector != nil { cleanupCollector.Observe() }
				_ = api.terminate(syscall.Handle(job))
				_ = cmd.Process.Kill()
				_ = input.Close()
				cancel()
				if !joined {
					select { case <-waited: joined = true; case <-time.After(2*time.Second): t.Error("owned root not reaped") }
				}
				if cleanupCollector != nil {
					rootWaitVerified := false
					if joined { rootWaitVerified = askRootWaitVerified(cmd.ProcessState, nil) }
					cleanup := cleanupCollector.Finish(rootWaitVerified, time.Now().Add(2*time.Second))
					if !cleanup.Reaped { t.Error("owned fixture collection not established", cleanup) }
					if cleanupCollector.Close() != nil { t.Error("owned cleanup pins not closed") }
				}
				_ = output.Close()
				if readyDone != nil {
					select { case <-readyDone: case <-time.After(time.Second): t.Error("owned reader not reaped") }
				}
				if readerErr != nil {
					select { case err := <-readerErr: if err != nil { t.Error("owned stdout reader", err) }; default: t.Error("reader completion not observed") }
				}
				for _, handle := range []syscall.Handle{child, lateChild, root} {
					if handle != 0 {
						if state, err := syscall.WaitForSingleObject(handle, 2000); err != nil || state != syscall.WAIT_OBJECT_0 { t.Error("owned process not signalled") }
					}
				}
				if collector != nil && collector.Close() != nil { t.Error("owned collection handles not closed") }
				if child != 0 { syscall.CloseHandle(child) }
				if lateChild != 0 { syscall.CloseHandle(lateChild) }
				if root != 0 { syscall.CloseHandle(root) }
			}()
			root, err = syscall.OpenProcess(syscall.SYNCHRONIZE|0x1000|0x0800|0x0100|0x0001, false, uint32(cmd.Process.Pid))
			if err != nil { t.Fatal(err) }
			ok, _, _ = askJobCollectionKernel.NewProc("AssignProcessToJobObject").Call(job, uintptr(root))
			if ok == 0 { t.Fatal("assign owned root") }
			cleanupCollector = newAskJobCollector(syscall.Handle(job), root, uint32(cmd.Process.Pid), true)
			cleanupCollector.Observe()
			var access askJobCollectionAPI = api
			if fault == "query-failure" { access = askJobCollectionQueryFailure{askJobCollectionAPI: api} }
			collector = newAskJobCollection(access, syscall.Handle(job), root, uint32(cmd.Process.Pid), true)
			code, _, _ := syscall.NewLazyDLL("ntdll.dll").NewProc("NtResumeProcess").Call(uintptr(root))
			if code != 0 { t.Fatal("resume owned root") }
			ready := make(chan string, 4)
			readyDone = make(chan struct{})
			readerErr = make(chan error, 1)
			go readAskJobFixtureOutput(ctx, output, ready, readerErr, readyDone)
			var childPID uint64
			select {
			case line, live := <-ready:
				if !live {
					if fault != "early-exit" { t.Fatal("missing owned child identity") }
					select { case <-waited: joined = true; case <-ctx.Done(): t.Fatal("early root exit not collected") }
					select { case err := <-readerErr: if err != nil { t.Fatal("early exit read error", err) }; case <-ctx.Done(): t.Fatal("early exit reader not complete") }
					readerErr = nil
					return
				}
				if fault == "early-exit" || !strings.HasPrefix(line, "CHILD ") { t.Fatal("missing owned child identity", line) }
				childPID, err = strconv.ParseUint(strings.TrimPrefix(line, "CHILD "), 10, 32)
				if err != nil || childPID == 0 { t.Fatal("invalid owned child identity") }
			case <-ctx.Done(): t.Fatal("owned child readiness deadline")
			}
			child, err = api.open(uint32(childPID))
			if err != nil { t.Fatal(err) }
			id, err := api.identity(child)
			belongs, memberErr := api.belongs(child, syscall.Handle(job))
			state, waitErr := api.wait(child)
			if err != nil || id.PID != uint32(childPID) || id.Creation == 0 || memberErr != nil || !belongs || waitErr != nil || state != syscall.WAIT_TIMEOUT { t.Fatal("live child precondition failed") }
			rootID, err := api.identity(root)
			if err != nil || rootID.Creation == 0 { t.Fatal("root fixture identity unavailable") }
			cleanupCollector.Observe()
			collector.Observe()
			logAskJobCollectionFixture(t, collector, "first-child")
			logAskJobCollectionFixture(t, cleanupCollector, "first-child-cleanup")
			assertAskJobFixtureMembers(t, cleanupCollector, rootID, id)
			if fault != "query-failure" { assertAskJobFixtureMembers(t, collector, rootID, id) }
			expectedIDs := []askNativeProcess{rootID, id}
			if fault == "late-child" {
				if _, err := input.Write([]byte("SPAWN\n")); err != nil { t.Fatal(err) }
				select {
				case line, live := <-ready:
					if !live || !strings.HasPrefix(line, "CHILD ") { t.Fatal("late child readiness", line) }
					pid, err := strconv.ParseUint(strings.TrimPrefix(line, "CHILD "), 10, 32)
					if err != nil || pid == 0 || pid == childPID { t.Fatal("late child identity") }
					lateChild, err = api.open(uint32(pid))
					if err != nil { t.Fatal(err) }
					member, err := api.belongs(lateChild, syscall.Handle(job))
					if err != nil || !member { t.Fatal("late child job membership") }
					lateID, err := api.identity(lateChild)
					if err != nil || lateID.PID != uint32(pid) || lateID.Creation == 0 { t.Fatal("late child fixture identity unavailable") }
					expectedIDs = append(expectedIDs, lateID)
				case <-ctx.Done(): t.Fatal("late child readiness deadline")
				}
				cleanupCollector.Observe()
				collector.Observe()
				logAskJobCollectionFixture(t, collector, "late-child")
				assertAskJobFixtureMembers(t, cleanupCollector, expectedIDs...)
				assertAskJobFixtureMembers(t, collector, expectedIDs...)
			}
			if _, err := input.Write([]byte("EXIT\n")); err != nil { t.Fatal(err) }
			input.Close()
			select { case <-waited: joined = true; case <-ctx.Done(): t.Fatal("root exit deadline") }
			cleanupCollector.Observe()
			collector.Observe()
			logAskJobCollectionFixture(t, cleanupCollector, "root-exited")
			// A normal root exits before its descendant. Only Finish may turn
			// this into a tree-completion fact, after retained-handle signals.
			if state, err := api.wait(child); err != nil || state != syscall.WAIT_TIMEOUT { t.Fatal("root exit did not leave child live") }
			result := collector.Finish(askRootWaitVerified(cmd.ProcessState, nil), time.Now().Add(2*time.Second))
			if result.Reaped != (fault != "query-failure") { t.Fatal("actual collection result", fault, result) }
			if result.Reaped && (result.Observed != result.Total || result.Total < uint32(len(expectedIDs))) { t.Fatal("full Job inventory was not collected", result) }
			state, err = syscall.WaitForSingleObject(child, 2000)
			if err != nil || state != syscall.WAIT_OBJECT_0 { t.Fatal("same held child was not terminated") }
			if lateChild != 0 {
				if state, err := api.wait(lateChild); err != nil || state != syscall.WAIT_OBJECT_0 { t.Fatal("same held late child not terminated") }
			}
			state, err = api.wait(root)
			if err != nil || state != syscall.WAIT_OBJECT_0 { t.Fatal("same held root was not terminated") }
			select { case <-readyDone: case <-ctx.Done(): t.Fatal("stdout EOF deadline") }
		})
	}
}

func assertAskJobFixtureMembers(t *testing.T, c *askJobCollection, expected ...askNativeProcess) {
	t.Helper()
	a, err := c.api.accounting(c.job)
	if err != nil || c.reason != "" || uint64(a.Total) != uint64(len(c.members)) || a.Active > a.Total {
		t.Fatal("complete owned Job inventory not pinned", a, c.reason, len(c.members))
	}
	for _, id := range expected {
		member, exists := c.members[id.PID]
		if !exists || id.Creation == 0 || member.id != id { t.Fatal("expected process generation not pinned", id) }
	}
	// Console hosts or any other genuine members are retained as well.
	// Neither their names nor an assumed fixed process count is an exemption.
	for _, member := range c.members {
		id, identityErr := c.api.identity(member.process)
		belongs, memberErr := c.api.belongs(member.process, c.job)
		if identityErr != nil || id != member.id || memberErr != nil || !belongs { t.Fatal("held Job member identity changed") }
	}
}

func readAskJobFixtureOutput(ctx context.Context, input io.Reader, ready chan<- string, terminal chan<- error, done chan<- struct{}) {
	defer close(done)
	defer close(ready)
	s := bufio.NewScanner(input)
	for s.Scan() {
		select { case ready <- s.Text(): case <-ctx.Done(): terminal <- ctx.Err(); return }
	}
	terminal <- s.Err()
}

type askJobFixtureReadError struct{}
func (askJobFixtureReadError) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestAskJobFixtureReaderReportsTerminal(t *testing.T) {
	for _, mode := range []string{"normal", "early-eof", "read-error"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			var input io.Reader = strings.NewReader("CHILD 42\n")
			if mode == "early-eof" { input = strings.NewReader("") }
			if mode == "read-error" { input = askJobFixtureReadError{} }
			ready, terminal, done := make(chan string, 2), make(chan error, 1), make(chan struct{})
			go readAskJobFixtureOutput(ctx, input, ready, terminal, done)
			select { case <-done: case <-ctx.Done(): t.Fatal("reader terminal not signalled") }
			var lines []string
			for line := range ready { lines = append(lines, line) }
			err := <-terminal
			if mode == "normal" && (len(lines) != 1 || lines[0] != "CHILD 42" || err != nil) { t.Fatal("normal reader result", lines, err) }
			if mode == "early-eof" && (len(lines) != 0 || err != nil) { t.Fatal("EOF was hidden", lines, err) }
			if mode == "read-error" && (len(lines) != 0 || err != io.ErrUnexpectedEOF) { t.Fatal("reader error was hidden", lines, err) }
		})
	}
}

// Only objects already pinned and verified to belong to this owned fixture
// are diagnosed. No command lines, environment or unrelated processes are read.
func logAskJobCollectionFixture(t *testing.T, c *askJobCollection, phase string) {
	t.Helper()
	a, err := c.api.accounting(c.job)
	t.Logf("phase=%s observed=%d total=%d active=%d reason=%s accounting_ok=%t", phase, len(c.members), a.Total, a.Active, c.reason, err == nil)
	for _, member := range c.members {
		belongs, err := c.api.belongs(member.process, c.job)
		if err != nil || !belongs { t.Log("owned member membership unavailable"); continue }
		var path [512]uint16
		length := uint32(len(path))
		ok, _, _ := askJobCollectionKernel.NewProc("QueryFullProcessImageNameW").Call(uintptr(member.process), 0, uintptr(unsafe.Pointer(&path[0])), uintptr(unsafe.Pointer(&length)))
		name := "unavailable"
		if ok != 0 && length <= uint32(len(path)) { name = filepath.Base(syscall.UTF16ToString(path[:length])) }
		t.Logf("member pid=%d creation=%d image=%s", member.id.PID, member.id.Creation, name)
	}
}

type askJobCollectionQueryFailure struct { askJobCollectionAPI }
func (askJobCollectionQueryFailure) list(syscall.Handle) (askJobMemberList, error) {
	return askJobMemberList{}, errAskJobCollection
}

func TestAskJobCollectionProcessHelper(t *testing.T) {
	switch os.Getenv("ASK_JOB_COLLECTION_TEST") {
	case "supervised-hold":
		if !askWorkerParent() { os.Exit(7) }
		fmt.Fprintln(os.Stdout, "READY")
		time.Sleep(30*time.Second)
	case "early-exit":
		os.Exit(0)
	case "child":
		fmt.Printf("READY %d\n", os.Getpid())
		time.Sleep(30*time.Second)
	case "root":
		exe, err := os.Executable()
		if err != nil { t.Fatal(err) }
		spawn := func() {
			readyRead, readyWrite, err := os.Pipe()
			if err != nil { t.Fatal(err) }
			defer readyRead.Close()
			defer readyWrite.Close()
			child := exec.Command(exe, "-test.run=^TestAskJobCollectionProcessHelper$")
			child.Env = append(os.Environ(), "ASK_JOB_COLLECTION_TEST=child")
			child.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
			child.Stdout = readyWrite
			if err := child.Start(); err != nil { t.Fatal(err) }
			if err := readyWrite.Close(); err != nil { t.Fatal(err) }
			// The parent's outer deadline owns this whole Job, including a
			// child that never reaches READY. Report the child only after its
			// runtime/console startup reached the explicit pipe barrier.
			s := bufio.NewScanner(readyRead)
			if !s.Scan() || s.Text() != fmt.Sprintf("READY %d", child.Process.Pid) || s.Err() != nil { t.Fatal("owned descendant not ready") }
			pid := child.Process.Pid
			if err := child.Process.Release(); err != nil { t.Fatal(err) }
			fmt.Printf("CHILD %d\n", pid)
		}
		spawn()
		input := bufio.NewScanner(os.Stdin)
		for input.Scan() {
			switch input.Text() {
			case "SPAWN": spawn()
			case "EXIT": return
			default: t.Fatal("owned root command invalid")
			}
		}
		t.Fatal("owned root command missing")
	}
}

func TestAskJobCollectionSupervisorKeepsFastCancelFacts(t *testing.T) {
	exe, err := os.Executable()
	if err != nil { t.Fatal(err) }
	t.Setenv("ASK_JOB_COLLECTION_TEST", "supervised-hold")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	called := false
	result := askOwnedChildWithHandshake(ctx, exe, func(raw []byte) bool {
		if string(raw) != "READY" { t.Fatal("supervised fixture readiness") }
		called = true
		cancel()
		return false
	}, "-test.run=^TestAskJobCollectionProcessHelper$")
	defer clear(result.Raw)
	t.Logf("fast_cancel started=%t assigned=%t root_wait=%t timed_out=%t reaped=%t collection_reason=%s observed=%d total=%d total_known=%t", result.Started, result.Assigned, result.WaitVerified, result.TimedOut, result.Reaped, result.CollectionReason, result.CollectionObserved, result.CollectionTotal, result.CollectionTotalKnown)
	if !called || !result.Started || !result.Assigned || !result.WaitVerified || !result.TimedOut || !result.Reaped {
		t.Fatal("supervised fast cancellation was not collected", result)
	}
	if result.CollectionQuery != (askJobQueryDiagnostic{}) || result.CollectionReason != "" || !result.CollectionTotalKnown || result.CollectionObserved != result.CollectionTotal || result.CollectionTotal == 0 {
		t.Fatal("collection facts were not preserved", result)
	}
}


func TestAskJobNativeQueryDiagnosticsKeepActualFailure(t *testing.T) {
	for _, mode := range []string{"normal", "native", "unknown-native", "capacity", "counts", "invalid-pid"} {
		t.Run(mode, func(t *testing.T) {
			buffer := &askJobNativeProcessList{assigned: 2, returned: 2}
			buffer.ids[0], buffer.ids[1] = 1, 2
			ok, nativeErr := uintptr(1), error(syscall.Errno(5))
			want := askJobQueryDiagnostic{Assigned: 2, Returned: 2, ReturnLength: 24}
			switch mode {
			case "native": ok = 0; want.Kind = "native_query_failed"; want.Win32Code = 5
			case "unknown-native": ok = 0; nativeErr = fmt.Errorf("private native path must not escape"); want.Kind = "native_query_failed"
			case "capacity": buffer.assigned = 4097; buffer.returned = 4097; want.Kind = "capacity_exceeded"; want.Assigned = 4097; want.Returned = 4097
			case "counts": buffer.assigned = 3; want.Kind = "count_mismatch"; want.Assigned = 3
			case "invalid-pid": buffer.ids[1] = 0; want.Kind = "invalid_pid"
			}
			list, err := decodeAskJobProcessList(ok, nativeErr, 24, buffer)
			if mode == "normal" {
				if err != nil || list.assigned != 2 || len(list.pids) != 2 || list.pids[0] != 1 || list.pids[1] != 2 { t.Fatal("successful native list was changed", list, err) }
				return
			}
			failure, typed := err.(*askJobQueryError)
			if !typed || failure.diagnostic != want || list.assigned != 0 || len(list.pids) != 0 { t.Fatal("native query cause was discarded or treated as a list", mode, failure, list) }
			if err.Error() != "job_collection_native_unavailable" || strings.Contains(fmt.Sprint(err), "private") { t.Fatal("raw native error escaped") }
		})
	}
}

func TestAskJobQueryDiagnosticKeepsFirstFailureAndPhase(t *testing.T) {
	for _, phase := range []string{"normal", "observe", "finish", "unclassified"} {
		t.Run(phase, func(t *testing.T) {
			m := newAskJobCollectionModel()
			m.clock = time.Unix(100, 0)
			c := newAskJobCollection(m, 9999, 11, 1, true)
			defer c.Close()
			deadline := m.clock.Add(time.Second)
			want := askJobQueryDiagnostic{}
			if phase != "normal" {
				want = askJobQueryDiagnostic{Kind: "native_query_failed", Phase: phase, Win32Code: 5, Assigned: 2, Returned: 1, ReturnLength: 16}
				m.listError = &askJobQueryError{diagnostic: want}
			}
			if phase == "unclassified" { m.listError = fmt.Errorf("private query text"); want = askJobQueryDiagnostic{Kind: "unclassified_query_failure", Phase: "observe"} }
			if phase == "observe" || phase == "unclassified" {
				c.Observe()
				m.listError = &askJobQueryError{diagnostic: askJobQueryDiagnostic{Kind: "count_mismatch", Assigned: 99, Returned: 98, ReturnLength: 80}}
				c.Observe()
				m.listError = nil
				c.Observe()
			}
			r := c.Finish(true, deadline)
			if r.Query != want || r.Reaped != (phase == "normal") || phase != "normal" && r.Reason != "job_member_query_failed" { t.Fatal("query diagnostic changed collection proof or first cause", phase, r, want) }
			if again := c.Finish(true, deadline); again != r { t.Fatal("completed query observation changed") }
		})
	}
}

func TestAskChildCollectionCarriesFirstQueryToFinalResult(t *testing.T) {
	for _, phase := range []string{"normal", "observe", "finish"} {
		t.Run(phase, func(t *testing.T) {
			m := newAskJobCollectionModel()
			m.clock = time.Unix(100, 0)
			c := newAskJobCollection(m, 9999, 11, 1, true)
			defer c.Close()
			want := askJobQueryDiagnostic{}
			if phase == "finish" { c.Observe() }
			if phase != "normal" {
				want = askJobQueryDiagnostic{Kind: "native_query_failed", Phase: phase, Win32Code: 5, Assigned: 2, Returned: 1, ReturnLength: 16}
				m.listError = &askJobQueryError{diagnostic: want}
			}
			if phase == "observe" {
				c.Observe()
				m.listError = &askJobQueryError{diagnostic: askJobQueryDiagnostic{Kind: "count_mismatch", Assigned: 9, Returned: 8, ReturnLength: 80}}
				c.Observe()
				m.listError = nil
			}
			child := askChildResult{Started: true, Assigned: true, WaitVerified: true, TimedOut: phase != "normal", Exit: 1}
			// This is the supervisor's actual Finish-to-Child projection, not a
			// test copy of its assignments. Nonzero Query is the required outlet.
			finishAskChildCollection(&child, c, m.clock.Add(time.Second))
			if child.CollectionQuery != want { t.Fatal("first query diagnostic did not reach final Child", child.CollectionQuery, want) }
			if child.Reaped != (phase == "normal") || child.CollectionObserved != 2 || !child.CollectionTotalKnown || child.CollectionTotal != 2 { t.Fatal("final Child lost collection facts or changed strict proof", child) }
			if phase != "normal" && child.CollectionReason != "job_member_query_failed" || phase == "normal" && child.CollectionReason != "" { t.Fatal("final Child lost fixed collection reason", child) }
			if !child.Started || !child.Assigned || !child.WaitVerified || child.TimedOut != (phase != "normal") || child.Exit != 1 { t.Fatal("collection projection changed independent child facts", child) }
			if m.accountingCalls == 0 || c.result.Query != want { t.Fatal("control did not traverse real collector Finish", c.result) }
			if strings.Contains(fmt.Sprint(child.CollectionQuery), "private") { t.Fatal("query outlet exposed error text") }
		})
	}
}
