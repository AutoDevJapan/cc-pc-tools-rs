//go:build windows

package main

import (
	"errors"
	"syscall"
	"time"
	"unsafe"
)

// A successful empty job query does not prove that every former member has
// finished. Keep each observed process object, and reject a lifetime count
// which includes an unobserved process. The job and root are borrowed from
// askOwnedChildWithHandshake; only handles opened here are closed here.
type askJobCollection struct {
	api       askJobCollectionAPI
	job       syscall.Handle
	root      syscall.Handle
	rootID    askNativeProcess
	members   map[uint32]askJobMember
	reason    string
	closeErr  error
	closed    bool
	finished  bool
	result    askJobCollectionResult
}

type askJobMember struct {
	process syscall.Handle
	id      askNativeProcess
	owned   bool
}

type askJobMemberList struct {
	assigned uint32
	pids     []uint32
}

type askJobCollectionResult struct {
	Reaped   bool
	Reason   string
	Observed uint32
	Total    uint32
}

// This internal OS seam is not decoded from a caller, CLI or receipt. The
// production constructor always installs askNativeJobCollectionAPI.
type askJobCollectionAPI interface {
	list(syscall.Handle) (askJobMemberList, error)
	open(uint32) (syscall.Handle, error)
	identity(syscall.Handle) (askNativeProcess, error)
	belongs(syscall.Handle, syscall.Handle) (bool, error)
	wait(syscall.Handle) (uint32, error)
	accounting(syscall.Handle) (askJobAccounting, error)
	terminate(syscall.Handle) error
	close(syscall.Handle) error
}

func newAskJobCollector(job, root syscall.Handle, pid uint32, assigned bool) *askJobCollection {
	return newAskJobCollection(askNativeJobCollectionAPI{}, job, root, pid, assigned)
}

func newAskJobCollection(api askJobCollectionAPI, job, root syscall.Handle, pid uint32, assigned bool) *askJobCollection {
	c := &askJobCollection{api: api, job: job, root: root, members: make(map[uint32]askJobMember)}
	if api == nil || job == 0 || root == 0 || pid == 0 || !assigned {
		c.unknown("job_root_unverified")
		return c
	}
	id, err := api.identity(root)
	if err != nil || id.PID != pid || id.Creation == 0 {
		c.unknown("job_root_identity_unverified")
		return c
	}
	member, err := api.belongs(root, job)
	if err != nil || !member {
		c.unknown("job_root_membership_unverified")
		return c
	}
	c.rootID = id
	c.members[pid] = askJobMember{process: root, id: id}
	return c
}

func (c *askJobCollection) unknown(reason string) {
	if c.reason == "" {
		c.reason = reason
	}
}

func (c *askJobCollection) Observe() {
	c.observeUntil(time.Time{})
}

func (c *askJobCollection) observeUntil(deadline time.Time) {
	if c == nil || c.api == nil {
		return
	}
	if c.closed || c.finished || c.rootID.Creation == 0 {
		return
	}
	withinBudget := func() bool {
		if !deadline.IsZero() && !time.Now().Before(deadline) {
			c.unknown("job_collection_deadline_exceeded")
			return false
		}
		return true
	}
	if !withinBudget() {
		return
	}
	list, err := c.api.list(c.job)
	if err != nil {
		c.unknown("job_member_query_failed")
		return
	}
	if uint64(list.assigned) != uint64(len(list.pids)) {
		c.unknown("job_member_list_incomplete")
		return
	}
	if len(list.pids) > 4096 {
		c.unknown("job_member_list_incomplete")
		return
	}
	seen := make(map[uint32]bool, len(list.pids))
	for _, pid := range list.pids {
		if !withinBudget() {
			return
		}
		if pid == 0 || seen[pid] {
			c.unknown("job_member_list_invalid")
			continue
		}
		seen[pid] = true
		if old, exists := c.members[pid]; exists {
			id, identityErr := c.api.identity(old.process)
			if identityErr != nil || id != old.id {
				c.unknown("job_member_generation_unverified")
			}
			// Membership was checked before this handle was retained. The
			// owned job has no breakaway flag; completion uses that same
			// process object, not a post-exit membership lookup by PID.
			continue
		}
		if len(c.members) >= 4096 {
			c.unknown("job_lifetime_member_limit")
			continue
		}
		// Only a previously unobserved ID is opened. An exited process is
		// never rediscovered by PID for a completion wait.
		h, owned := c.root, false
		if pid != c.rootID.PID {
			h, err = c.api.open(pid)
			owned = true
			if err != nil || h == 0 {
				c.unknown("job_member_open_failed")
				continue
			}
		}
		id, identityErr := c.api.identity(h)
		member, membershipErr := c.api.belongs(h, c.job)
		if identityErr != nil || id.PID != pid || id.Creation == 0 || membershipErr != nil || !member {
			c.unknown("job_member_identity_unverified")
			if owned && c.api.close(h) != nil {
				c.unknown("job_member_close_failed")
			}
			continue
		}
		c.members[pid] = askJobMember{process: h, id: id, owned: owned}
	}
}

// Finish is called only after the parent's Cmd.Wait has returned. Its deadline
// is the existing whole reconciliation budget, not a fresh per-process wait.
// TotalProcesses is a lifetime count, including failed limit admissions. Any
// such unexplained count is unknown, rather than inferred process completion.
func (c *askJobCollection) Finish(rootWaitVerified bool, deadline time.Time) askJobCollectionResult {
	if c == nil {
		return askJobCollectionResult{Reason: "job_collection_unavailable"}
	}
	if c.finished {
		return c.result
	}
	if c.closed || c.api == nil {
		return askJobCollectionResult{Reason: "job_collection_closed"}
	}
	if !rootWaitVerified {
		c.unknown("job_root_wait_unverified")
	}
	c.observeUntil(deadline)
	if c.api.terminate(c.job) != nil {
		c.unknown("job_termination_unverified")
	}
	if c.rootID.Creation == 0 {
		c.finished = true
		c.result = askJobCollectionResult{Reason: c.reason}
		return c.result
	}
	for time.Now().Before(deadline) {
		c.observeUntil(deadline)
		accounting, err := c.api.accounting(c.job)
		if err != nil {
			c.unknown("job_accounting_unverified")
			break
		}
		allSignalled := len(c.members) != 0 && c.rootID.Creation != 0
		observed := len(c.members)
		for _, member := range c.members {
			if !time.Now().Before(deadline) {
				allSignalled = false
				break
			}
			id, err := c.api.identity(member.process)
			if err != nil || id != member.id {
				c.unknown("job_held_identity_unverified")
				allSignalled = false
				continue
			}
			state, err := c.api.wait(member.process)
			if err != nil || state != syscall.WAIT_OBJECT_0 && state != syscall.WAIT_TIMEOUT {
				c.unknown("job_process_wait_failed")
			}
			if err != nil || state != syscall.WAIT_OBJECT_0 {
				allSignalled = false
			}
		}
		if accounting.Active == 0 && allSignalled && time.Now().Before(deadline) {
			// Requery after the last handle wait. A late child cannot hide
			// behind root exit or a transient earlier active=0 snapshot.
			c.observeUntil(deadline)
			final, finalErr := c.api.accounting(c.job)
			if finalErr != nil {
				c.unknown("job_accounting_unverified")
				break
			}
			// A newly observed member has not necessarily been waited yet.
			// Never finish using the previous iteration's signal results.
			if final.Active != 0 || final.Total != accounting.Total || len(c.members) != observed {
				continue
			}
			if !time.Now().Before(deadline) {
				break
			}
			if uint64(observed) != uint64(final.Total) {
				c.unknown("job_lifetime_members_unverified")
			}
			// An unobserved historical process cannot be recovered by opening
			// an arbitrary PID after termination. Its lifetime-count gap is
			// permanently unknown, even with an otherwise empty job.
			if c.Close() != nil {
				c.unknown("job_member_close_failed")
			}
			c.result = askJobCollectionResult{Reaped: c.reason == "", Reason: c.reason, Observed: uint32(len(c.members)), Total: final.Total}
			c.finished = true
			return c.result
		}
		time.Sleep(time.Millisecond)
	}
	c.unknown("job_collection_deadline_exceeded")
	c.finished = true
	c.result = askJobCollectionResult{Reason: c.reason, Observed: uint32(len(c.members))}
	return c.result
}

func (c *askJobCollection) Close() error {
	if c == nil || c.closed {
		if c != nil {
			return c.closeErr
		}
		return nil
	}
	// Retire ownership before CloseHandle. Even a failed close is not retried
	// against a numeric value which another owner may have invalidated.
	c.closed = true
	for pid, member := range c.members {
		if member.owned {
			member.owned = false
			c.members[pid] = member
			if err := c.api.close(member.process); err != nil {
				c.closeErr = errAskJobCollection
			}
		}
	}
	return c.closeErr
}

var askJobCollectionKernel = syscall.NewLazyDLL("kernel32.dll")
var errAskJobCollection = errors.New("job_collection_native_unavailable")

type askNativeJobCollectionAPI struct{}

func (askNativeJobCollectionAPI) list(job syscall.Handle) (askJobMemberList, error) {
	var buffer struct {
		assigned, returned uint32
		ids                [4096]uintptr
	}
	ok, _, _ := askJobCollectionKernel.NewProc("QueryInformationJobObject").Call(uintptr(job), 3, uintptr(unsafe.Pointer(&buffer)), unsafe.Sizeof(buffer), 0)
	if ok == 0 || buffer.returned > uint32(len(buffer.ids)) || buffer.assigned != buffer.returned {
		return askJobMemberList{}, errAskJobCollection
	}
	result := askJobMemberList{assigned: buffer.assigned, pids: make([]uint32, buffer.returned)}
	for i := range result.pids {
		if buffer.ids[i] == 0 || uint64(buffer.ids[i]) > uint64(^uint32(0)) {
			return askJobMemberList{}, errAskJobCollection
		}
		result.pids[i] = uint32(buffer.ids[i])
	}
	return result, nil
}

func (askNativeJobCollectionAPI) open(pid uint32) (syscall.Handle, error) {
	return syscall.OpenProcess(syscall.SYNCHRONIZE|0x1000, false, pid)
}

func (askNativeJobCollectionAPI) identity(process syscall.Handle) (askNativeProcess, error) {
	pid, _, _ := askJobCollectionKernel.NewProc("GetProcessId").Call(uintptr(process))
	var creation, exit, kernel, user syscall.Filetime
	if pid == 0 || syscall.GetProcessTimes(process, &creation, &exit, &kernel, &user) != nil {
		return askNativeProcess{}, errAskJobCollection
	}
	return askNativeProcess{PID: uint32(pid), Creation: uint64(creation.HighDateTime)<<32 | uint64(creation.LowDateTime)}, nil
}

func (askNativeJobCollectionAPI) belongs(process, job syscall.Handle) (bool, error) {
	var belongs uint32
	ok, _, _ := askJobCollectionKernel.NewProc("IsProcessInJob").Call(uintptr(process), uintptr(job), uintptr(unsafe.Pointer(&belongs)))
	if ok == 0 {
		return false, errAskJobCollection
	}
	return belongs != 0, nil
}

func (askNativeJobCollectionAPI) wait(process syscall.Handle) (uint32, error) {
	return syscall.WaitForSingleObject(process, 0)
}

func (askNativeJobCollectionAPI) accounting(job syscall.Handle) (askJobAccounting, error) {
	var accounting askJobAccounting
	ok, _, _ := askJobCollectionKernel.NewProc("QueryInformationJobObject").Call(uintptr(job), 1, uintptr(unsafe.Pointer(&accounting)), unsafe.Sizeof(accounting), 0)
	if ok == 0 {
		return askJobAccounting{}, errAskJobCollection
	}
	return accounting, nil
}

func (askNativeJobCollectionAPI) terminate(job syscall.Handle) error {
	ok, _, _ := askJobCollectionKernel.NewProc("TerminateJobObject").Call(uintptr(job), 1)
	if ok == 0 {
		return errAskJobCollection
	}
	return nil
}

func (askNativeJobCollectionAPI) close(process syscall.Handle) error {
	return syscall.CloseHandle(process)
}
