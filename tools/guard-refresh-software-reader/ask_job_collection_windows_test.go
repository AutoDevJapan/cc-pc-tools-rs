//go:build windows

package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
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
	if m.closeError {
		return errAskJobCollection
	}
	return nil
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
	for _, fault := range []string{"normal", "late-child", "query-failure"} {
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
			cmd.Env = append(os.Environ(), "ASK_JOB_COLLECTION_TEST=root")
			cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000 | 0x00000004}
			output, err := cmd.StdoutPipe()
			if err != nil { t.Fatal(err) }
			defer output.Close()
			input, err := cmd.StdinPipe()
			if err != nil { t.Fatal(err) }
			defer input.Close()
			if err := cmd.Start(); err != nil { t.Fatal(err) }
			waited := make(chan error, 1)
			go func() { waited <- cmd.Wait() }()
			var root, child, lateChild syscall.Handle
			var collector *askJobCollection
			var cleanupCollector *askJobCollection
			var readyDone chan struct{}
			joined := false
			defer func() {
				if cleanupCollector != nil { cleanupCollector.Observe() }
				_ = api.terminate(syscall.Handle(job))
				_ = cmd.Process.Kill()
				_ = input.Close()
				cancel()
				if !joined {
					select { case <-waited: case <-time.After(2*time.Second): t.Error("owned root not reaped") }
				}
				if cleanupCollector != nil {
					cleanup := cleanupCollector.Finish(askRootWaitVerified(cmd.ProcessState, nil), time.Now().Add(2*time.Second))
					if !cleanup.Reaped { t.Error("owned fixture collection not established", cleanup) }
					if cleanupCollector.Close() != nil { t.Error("owned cleanup pins not closed") }
				}
				_ = output.Close()
				if readyDone != nil {
					select { case <-readyDone: case <-time.After(time.Second): t.Error("owned reader not reaped") }
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
			go func() {
				defer close(readyDone)
				s := bufio.NewScanner(output)
				for s.Scan() {
					select { case ready <- s.Text(): case <-ctx.Done(): return }
				}
			}()
			var childPID uint64
			select {
			case line := <-ready:
				if !strings.HasPrefix(line, "CHILD ") { t.Fatal("missing owned child identity", line) }
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
			cleanupCollector.Observe()
			collector.Observe()
			if fault != "query-failure" && len(collector.members) != 2 { t.Fatal("actual job members not pinned") }
			if fault == "late-child" {
				if _, err := input.Write([]byte("SPAWN\n")); err != nil { t.Fatal(err) }
				select {
				case line := <-ready:
					if !strings.HasPrefix(line, "CHILD ") { t.Fatal("late child readiness", line) }
					pid, err := strconv.ParseUint(strings.TrimPrefix(line, "CHILD "), 10, 32)
					if err != nil || pid == 0 || pid == childPID { t.Fatal("late child identity") }
					lateChild, err = api.open(uint32(pid))
					if err != nil { t.Fatal(err) }
					member, err := api.belongs(lateChild, syscall.Handle(job))
					if err != nil || !member { t.Fatal("late child job membership") }
				case <-ctx.Done(): t.Fatal("late child readiness deadline")
				}
				cleanupCollector.Observe()
				collector.Observe()
				if len(collector.members) != 3 { t.Fatal("late child not pinned") }
			}
			if _, err := input.Write([]byte("EXIT\n")); err != nil { t.Fatal(err) }
			input.Close()
			select { case <-waited: joined = true; case <-ctx.Done(): t.Fatal("root exit deadline") }
			// A normal root exits before its descendant. Only Finish may turn
			// this into a tree-completion fact, after retained-handle signals.
			if state, err := api.wait(child); err != nil || state != syscall.WAIT_TIMEOUT { t.Fatal("root exit did not leave child live") }
			result := collector.Finish(askRootWaitVerified(cmd.ProcessState, nil), time.Now().Add(2*time.Second))
			if result.Reaped != (fault != "query-failure") { t.Fatal("actual collection result", fault, result) }
			state, err = syscall.WaitForSingleObject(child, 2000)
			if err != nil || state != syscall.WAIT_OBJECT_0 { t.Fatal("same held child was not terminated") }
			if lateChild != 0 {
				if state, err := api.wait(lateChild); err != nil || state != syscall.WAIT_OBJECT_0 { t.Fatal("same held late child not terminated") }
			}
			state, err = api.wait(root)
			if err != nil || state != syscall.WAIT_OBJECT_0 { t.Fatal("same held root was not terminated") }
		})
	}
}

type askJobCollectionQueryFailure struct { askJobCollectionAPI }
func (askJobCollectionQueryFailure) list(syscall.Handle) (askJobMemberList, error) {
	return askJobMemberList{}, errAskJobCollection
}

func TestAskJobCollectionProcessHelper(t *testing.T) {
	switch os.Getenv("ASK_JOB_COLLECTION_TEST") {
	case "child":
		time.Sleep(30*time.Second)
	case "root":
		exe, err := os.Executable()
		if err != nil { t.Fatal(err) }
		spawn := func() {
			child := exec.Command(exe, "-test.run=^TestAskJobCollectionProcessHelper$")
			child.Env = append(os.Environ(), "ASK_JOB_COLLECTION_TEST=child")
			child.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
			if err := child.Start(); err != nil { t.Fatal(err) }
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
