//go:build windows

package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

type askJobLimits struct {
	User, Kernel                                               int64
	Flags                                                      uint32
	Min, Max                                                   uintptr
	Active                                                     uint32
	Affinity                                                   uintptr
	Priority, Scheduling                                       uint32
	IO                                                         [6]uint64
	ProcessMemory, JobMemory, PeakProcessMemory, PeakJobMemory uintptr
}
type askJobAccounting struct {
	User, Kernel, PeriodUser, PeriodKernel int64
	Faults, Total, Active, Terminated      uint32
}

// Strict collection is requested only by the fixed fresh-guard inspection.
// Faults can only remove evidence and are package-private test controls.
type askStrictJobKey struct{}
type askStrictJobFaultKey struct{}
const askStrictJobDiagnosticLimit = 16
type askStrictJobFaults struct {
    failListOnce, partialListOnce, failMemberOpenOnce, failMembershipOnce, failRootIdentityOnce, failRootMembershipOnce, omitNonRoot, failWaitOnce bool
    listFaultSeen, partialSeen, openFaultSeen, membershipFaultSeen, rootIdentityFaultSeen, rootMembershipFaultSeen, waitFaultSeen bool
    listCalls int
    finalTotal, finalActive uint32
    finalHeld, finalSignalled int
    finalRootCreation uint64
    zeroObserved, finalQueryOK bool
    captureDiagnostics bool
    finalMembers []askStrictJobMemberDiagnostic
    finalOmitted int
    diagnosticOmittedOverflow bool
}

// Test-only output contains no image path or command line.
type askStrictJobMemberDiagnostic struct {
    PID uint32
    Root bool
    CreationNonZero bool
    ImagePathClass string
}

type askStrictJobMember struct { handle syscall.Handle; created uint64 }

type askStrictJobCollector struct {
    root uint32
    members map[uint32]askStrictJobMember
    unknown bool
    faults *askStrictJobFaults
    historyObservation *unslopHistoryCollectionObservation
}

func askStrictProcessCreation(kernel *syscall.LazyDLL,handle syscall.Handle,pid uint32) (uint64,bool) {
    if kernel==nil||handle==0||pid==0{return 0,false}
    actual,_,_:=kernel.NewProc("GetProcessId").Call(uintptr(handle))
    if actual==0||uint64(actual)!=uint64(pid){return 0,false}
    var created,exited,kernelTime,userTime syscall.Filetime
    if syscall.GetProcessTimes(handle,&created,&exited,&kernelTime,&userTime)!=nil{return 0,false}
    value:=uint64(created.HighDateTime)<<32|uint64(created.LowDateTime)
    return value,value!=0
}

func (c *askStrictJobCollector) collect(kernel *syscall.LazyDLL,job uintptr) {
    if c==nil||kernel==nil||job==0 {if c!=nil {c.unknown=true};return}
    if c.faults!=nil {
        c.faults.listCalls++
        if c.faults.failListOnce {c.faults.failListOnce=false;c.faults.listFaultSeen=true;c.unknown=true;return}
    }
    var list struct {
        assigned,listed uint32
        ids [128]uintptr
    }
    ok,_,_:=kernel.NewProc("QueryInformationJobObject").Call(job,3,uintptr(unsafe.Pointer(&list)),unsafe.Sizeof(list),0)
    if ok==0 {c.unknown=true;return}
    if c.faults!=nil&&c.faults.partialListOnce {
        c.faults.partialListOnce=false;c.faults.partialSeen=true
        if list.listed==^uint32(0){c.unknown=true;return}
        list.assigned=list.listed+1
    }
    if list.listed>uint32(len(list.ids))||list.assigned!=list.listed {c.unknown=true;return}
    for _,raw:=range list.ids[:list.listed] {
        if raw==0||uint64(raw)>uint64(^uint32(0)){c.unknown=true;continue}
        id:=uint32(raw)
        if _,exists:=c.members[id];exists{continue}
        if c.faults!=nil&&c.faults.omitNonRoot&&id!=c.root {continue}
        if c.faults!=nil&&c.faults.failMemberOpenOnce&&id!=c.root {
            c.faults.failMemberOpenOnce=false;c.faults.openFaultSeen=true;c.unknown=true;continue
        }
        handle,err:=syscall.OpenProcess(syscall.SYNCHRONIZE|0x1000,false,id)
        if err!=nil {c.unknown=true;continue}
        if c.faults!=nil&&c.faults.failMembershipOnce&&id!=c.root {
            c.faults.failMembershipOnce=false;c.faults.membershipFaultSeen=true
            if syscall.CloseHandle(handle)!=nil {c.unknown=true};c.unknown=true;continue
        }
        var belongs uint32
        ok,_,_:=kernel.NewProc("IsProcessInJob").Call(uintptr(handle),job,uintptr(unsafe.Pointer(&belongs)))
        if ok==0||belongs==0 {
            if syscall.CloseHandle(handle)!=nil {c.unknown=true}
            c.unknown=true;continue
        }
        created,valid:=askStrictProcessCreation(kernel,handle,id)
        if !valid {if syscall.CloseHandle(handle)!=nil {c.unknown=true};c.unknown=true;continue}
        member:=askStrictJobMember{handle:handle,created:created}
        c.members[id]=member
        c.recordDiagnostic(kernel,id,member)
    }
}

// This diagnostic is called only when the owned test requests it, while the
// member handle is first retained. It never returns the path or affects Reaped.
func askStrictJobImagePathClass(kernel *syscall.LazyDLL,handle syscall.Handle) string {
    if kernel==nil||handle==0{return "unavailable"}
    var name [32768]uint16
    defer clear(name[:])
    size:=uint32(len(name))
    proc:=kernel.NewProc("QueryFullProcessImageNameW")
    if proc.Find()!=nil{return "unavailable"}
    ok,_,_:=proc.Call(uintptr(handle),0,uintptr(unsafe.Pointer(&name[0])),uintptr(unsafe.Pointer(&size)))
    if ok==0||size==0||size>uint32(len(name)){return "unavailable"}
    executable,err:=os.Executable();if err!=nil{return "unavailable"}
    if strings.EqualFold(syscall.UTF16ToString(name[:size]),executable){return "same_path"}
    return "other_path"
}

// Diagnostic rows and image queries are capped; omission is not Job proof.
func (c *askStrictJobCollector) recordDiagnostic(kernel *syscall.LazyDLL,pid uint32,member askStrictJobMember) {
    if c==nil||c.faults==nil||!c.faults.captureDiagnostics{return}
    if len(c.faults.finalMembers)>=askStrictJobDiagnosticLimit {
        maxInt:=int(^uint(0)>>1)
        if c.faults.finalOmitted==maxInt {c.faults.diagnosticOmittedOverflow=true} else {c.faults.finalOmitted++}
        return
    }
    c.faults.finalMembers=append(c.faults.finalMembers,askStrictJobMemberDiagnostic{
        PID:pid,Root:pid==c.root,CreationNonZero:member.created!=0,
        ImagePathClass:askStrictJobImagePathClass(kernel,member.handle),
    })
}

func (c *askStrictJobCollector) finish(kernel *syscall.LazyDLL,job uintptr,deadline time.Time,terminated,assigned,rootWaitVerified bool) bool {
    if c==nil||kernel==nil||job==0{return false}
    if !terminated||!assigned||!rootWaitVerified {c.unknown=true}
    var initial askJobAccounting
    zero:=false
    for terminated&&time.Now().Before(deadline) {
        var a askJobAccounting
        ok,_,_:=kernel.NewProc("QueryInformationJobObject").Call(job,1,uintptr(unsafe.Pointer(&a)),unsafe.Sizeof(a),0)
        if ok==0 {c.unknown=true;break}
        if a.Active==0 {initial=a;zero=true;break}
        time.Sleep(5*time.Millisecond)
    }
    if !zero {c.unknown=true}
    signalled:=0
    for pid,member:=range c.members {
        created,valid:=askStrictProcessCreation(kernel,member.handle,pid)
        if !valid||created!=member.created {c.unknown=true;continue}
        left:=time.Until(deadline)
        if left<0 {c.unknown=true;continue}
        if c.faults!=nil&&c.faults.failWaitOnce {
            c.faults.failWaitOnce=false;c.faults.waitFaultSeen=true;c.unknown=true;continue
        }
        state,err:=syscall.WaitForSingleObject(member.handle,uint32(left/time.Millisecond))
        if err!=nil||state!=syscall.WAIT_OBJECT_0 {c.unknown=true;continue}
        after,valid:=askStrictProcessCreation(kernel,member.handle,pid)
        if !valid||after!=member.created {c.unknown=true;continue}
        signalled++
    }
    var final askJobAccounting
    ok,_,_:=kernel.NewProc("QueryInformationJobObject").Call(job,1,uintptr(unsafe.Pointer(&final)),unsafe.Sizeof(final),0)
    if ok==0||!zero||final.Active!=0||final.Total!=initial.Total {c.unknown=true}
    rootMember,rootHeld:=c.members[c.root]
    if !rootHeld||rootMember.created==0||final.Total==0||uint64(final.Total)!=uint64(len(c.members))||signalled!=len(c.members) {c.unknown=true}
    if c.faults!=nil {c.faults.finalTotal=final.Total;c.faults.finalActive=final.Active;c.faults.finalHeld=len(c.members);c.faults.finalSignalled=signalled;c.faults.finalRootCreation=rootMember.created;c.faults.zeroObserved=zero;c.faults.finalQueryOK=ok!=0}
    if c.historyObservation != nil {
        *c.historyObservation = unslopHistoryCollectionObservation{
            rootPID: c.root, rootCreation: rootMember.created,
            observed: uint32(len(c.members)), signalled: uint32(signalled),
            total: final.Total, totalKnown: ok != 0, collected: !c.unknown,
        }
    }
    return !c.unknown
}

func (c *askStrictJobCollector) closeAll() bool {
    if c==nil{return false}
    ok:=true
    for _,member:=range c.members {if syscall.CloseHandle(member.handle)!=nil {ok=false}}
    c.members=nil
    return ok
}

type askChildResult struct {
	Raw                             []byte
	Diagnostic                      *compilerDiagnostic
	Exit                            int
	TimedOut, Reaped                bool
	Started, Assigned, WaitVerified bool
	Capture                         *askChildCaptureResult
	CollectionReason                string
	CollectionObserved, CollectionTotal uint32
	CollectionSignalled             uint32
	CollectionTotalKnown            bool
	CollectionRootPID               uint32
	CollectionRootCreation          uint64
}

// Start suspended, assign the exact process to an owned kill-on-close job, then
// resume. This includes future Git children before any worker code executes.
// No goroutine performing native writes is abandoned by the parent.
func askOwnedChild(ctx context.Context, executable string, args ...string) (result askChildResult) {
	return askOwnedChildWithHandshake(ctx, executable, nil, args...)
}

type askChildWorkingDirectory struct{}
type askChildEnvironment struct{}
type askChildExactEnvironment struct{}

type askChildCaptureKey struct{}

type askChildCapture struct {
	Stdin []byte
}

type askNativeProcess struct {
	PID      uint32
	Creation uint64
}

type askChildCaptureResult struct {
	NativeProcess *askNativeProcess
	Stdout        []byte
	Overflow      bool
	Complete      bool
	protocolReason string
	protocolWritesComplete bool
	protocolWriteFailed bool
	protocolIOFailed bool
	historyStdin unslopHistoryStdinFacts
}

type askCaptureOutput struct {
	data     []byte
	n        int
	overflow bool
	eof      bool
}

func (w *askCaptureOutput) Write(p []byte) (int, error) {
	n := copy(w.data[w.n:], p)
	w.n += n
	w.overflow = w.overflow || n != len(p)
	// Keep draining after overflow. No caller-supplied Read/Write function can
	// block the process supervisor, and retained storage never grows.
	return len(p), nil
}

func (w *askCaptureOutput) ReadFrom(r io.Reader) (int64, error) {
	var scratch [65536]byte
	defer clear(scratch[:])
	var total int64
	for {
		n, err := r.Read(scratch[:])
		if n > 0 {
			_, _ = w.Write(scratch[:n])
			total += int64(n)
		}
		if err == io.EOF {
			w.eof = true
			return total, nil
		}
		if err != nil {
			return total, err
		}
	}
}

type askCaptureDiagnostic struct {
	buffer *compilerCapture
	eof    bool
}

func (w *askCaptureDiagnostic) Write(p []byte) (int, error) { return w.buffer.Write(p) }
func (w *askCaptureDiagnostic) ReadFrom(r io.Reader) (int64, error) {
	var scratch [65536]byte
	defer clear(scratch[:])
	var total int64
	for {
		n, err := r.Read(scratch[:])
		if n > 0 {
			_, _ = w.Write(scratch[:n])
			total += int64(n)
		}
		if err == io.EOF {
			w.eof = true
			return total, nil
		}
		if err != nil {
			return total, err
		}
	}
}

func askChildEnvironmentValues(ctx context.Context, parent []string, permit string) []string {
	var result []string
	if exact, _ := ctx.Value(askChildExactEnvironment{}).(bool); !exact {
		result = append(result, parent...)
	}
	result = append(result, permit)
	if changes, _ := ctx.Value(askChildEnvironment{}).(map[string]string); changes != nil {
		for key, value := range changes {
			filtered := result[:0]
			for _, entry := range result {
				if !strings.HasPrefix(strings.ToUpper(entry), strings.ToUpper(key)+"=") {
					filtered = append(filtered, entry)
				}
			}
			result = append(filtered, key+"="+value)
		}
	}
	return result
}

func askOwnedChildAt(ctx context.Context, cwd, executable string, args ...string) askChildResult {
	return askOwnedChild(context.WithValue(ctx, askChildWorkingDirectory{}, cwd), executable, args...)
}

type askReadyCapture struct {
	askGitOutput
	ready chan []byte
	sent  bool
	first []byte
}

func (w *askReadyCapture) ReadFrom(r io.Reader) (int64, error) {
	var total int64
	buf := make([]byte, 4096)
	defer clear(buf)
	for {
		n, e := r.Read(buf)
		if n > 0 {
			written, we := w.Write(buf[:n])
			total += int64(written)
			if we != nil {
				return total, we
			}
		}
		if e == io.EOF {
			return total, nil
		}
		if e != nil {
			return total, e
		}
	}
}

func (w *askReadyCapture) Write(p []byte) (int, error) {
	n, e := w.askGitOutput.Write(p)
	if e == nil && !w.sent {
		if i := bytes.IndexByte(w.Bytes(), '\n'); i >= 0 {
			w.sent = true
			w.first = append([]byte(nil), w.Bytes()[:i]...)
			w.ready <- append([]byte(nil), w.Bytes()[:i]...)
			remainder := append([]byte(nil), w.Bytes()[i+1:]...)
			clear(w.Bytes())
			w.Buffer.Reset()
			w.Buffer.Write(remainder)
			clear(remainder)
		}
	}
	return n, e
}

func askOwnedChildWithHandshake(ctx context.Context, executable string, onReady func([]byte) bool, args ...string) (result askChildResult) {
	result.Exit = -1
	strict, _ := ctx.Value(askStrictJobKey{}).(bool)
	historyRequested := ctx.Value(unslopHistoryProtocolKey{}) != nil
	strict = strict || historyRequested
	var strictCollector *askStrictJobCollector
	if strict {
		faults, _ := ctx.Value(askStrictJobFaultKey{}).(*askStrictJobFaults)
		strictCollector = &askStrictJobCollector{members: make(map[uint32]askStrictJobMember), faults: faults}
	}
	capture, hasCapture := ctx.Value(askChildCaptureKey{}).(askChildCapture)
	if hasCapture && onReady != nil {
		return
	}
	var captured *askCaptureOutput
	var history *unslopHistoryExchange
	var historyStdin *unslopHistoryStdin
	var historyFailure <-chan struct{}
	var fixedInput []byte
	if hasCapture {
		result.Capture = &askChildCaptureResult{}
		if len(capture.Stdin) > 1<<20 || ctx.Err() != nil {
			return
		}
		fixedInput = append([]byte(nil), capture.Stdin...)
		defer clear(fixedInput)
		captured = &askCaptureOutput{data: make([]byte, 8<<20)}
		defer func() {
			result.Capture.Stdout = captured.data[:captured.n]
			result.Capture.Overflow = captured.overflow
		}()
		if historyRequested {
			plan, valid := ctx.Value(unslopHistoryProtocolKey{}).(unslopHistoryProtocol)
			if !valid || len(capture.Stdin) != 0 { result.Capture.protocolReason = "history_protocol_plan_invalid"; return }
			var err error
			history, err = newUnslopHistoryExchange(ctx, plan)
			if err != nil { result.Capture.protocolReason = "history_protocol_plan_invalid"; return }
			defer history.Close()
			historyFailure = history.failed
			strictCollector.historyObservation = new(unslopHistoryCollectionObservation)
			defer func(){ publishUnslopHistoryCollection(&result, strictCollector.historyObservation) }()
		}
	}
	if historyRequested && !hasCapture { return }
	kernel := syscall.NewLazyDLL("kernel32.dll")
	job, _, _ := kernel.NewProc("CreateJobObjectW").Call(0, 0)
	if job == 0 {
		return
	}
	if strict {
		defer func() {
			if syscall.CloseHandle(syscall.Handle(job)) != nil {
				result.Reaped = false
				clear(result.Raw); result.Raw = nil
				if result.Capture != nil { result.Capture.Complete = false }
			}
		}()
		defer func() {
			if !strictCollector.closeAll() {
				result.Reaped = false
				clear(result.Raw); result.Raw = nil
				if result.Capture != nil { result.Capture.Complete = false }
			}
		}()
	} else {
		defer syscall.CloseHandle(syscall.Handle(job))
	}
	limits := askJobLimits{Flags: 0x2000}
	ok, _, _ := kernel.NewProc("SetInformationJobObject").Call(job, 9, uintptr(unsafe.Pointer(&limits)), unsafe.Sizeof(limits))
	if ok == 0 {
		return
	}
	cmd := exec.CommandContext(ctx, executable, args...)
	if dir, _ := ctx.Value(askChildWorkingDirectory{}).(string); dir != "" {
		cmd.Dir = dir
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000 | 0x00000004}
	permitRead, permitWrite, permitErr := os.Pipe()
	if permitErr != nil {
		return
	}
	defer permitRead.Close()
	defer permitWrite.Close()
	if syscall.SetHandleInformation(syscall.Handle(permitRead.Fd()), 1, 1) != nil {
		return
	}
	cmd.SysProcAttr.AdditionalInheritedHandles = []syscall.Handle{syscall.Handle(permitRead.Fd())}
	cmd.Env = askChildEnvironmentValues(ctx, os.Environ(), "ASK_SOFTWARE_PARENT_HANDLE="+strconv.FormatUint(uint64(permitRead.Fd()), 10))
	cmd.WaitDelay = 100 * time.Millisecond
	out := askGitOutput{limit: 65536}
	errOut := askGitOutput{limit: 65536}
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	compilerMode, _ := ctx.Value(compilerDiagnosticContext{}).(bool)
	compilerMode = compilerMode && onReady == nil
	if hasCapture {
		if history != nil {
			var pipeErr error
			historyStdin,pipeErr=prepareUnslopHistoryStdin(ctx,history)
			if pipeErr!=nil {result.Capture.protocolReason="history_protocol_stdin_unavailable";return}
			defer func(){
				_ = historyStdin.Close()
				result.Capture.historyStdin=historyStdin.facts()
				stdin:=result.Capture.historyStdin
				if stdin.CloseFailed||stdin.CloseAttempted&&!stdin.CloseCompleted||stdin.ReaderCloseFailed||stdin.CopyStarted&&(!stdin.CopyFinished||stdin.CopyFailed) {result.Capture.protocolIOFailed=true;result.Capture.Complete=false}
			}()
			cmd.Stdin=historyStdin.reader.file
		} else { cmd.Stdin = bytes.NewReader(fixedInput) }
	}
	var compilerOut, compilerErr *compilerCapture
	var captureErr *askCaptureDiagnostic
	if compilerMode {
		compilerOut, compilerErr = new(compilerCapture), new(compilerCapture)
		defer compilerOut.erase()
		defer compilerErr.erase()
		cmd.Stdout, cmd.Stderr = compilerOut, compilerErr
	}
	if hasCapture {
		cmd.Stdout = captured
		if history != nil { cmd.Stdout = &unslopHistoryCaptureOutput{capture: captured, exchange: history} }
		if compilerErr == nil {
			compilerErr = new(compilerCapture)
			defer compilerErr.erase()
		}
		captureErr = &askCaptureDiagnostic{buffer: compilerErr}
		cmd.Stderr = captureErr
	}
	var ready *askReadyCapture
	var input io.WriteCloser
	if onReady != nil {
		ready = &askReadyCapture{askGitOutput: askGitOutput{limit: 65536}, ready: make(chan []byte, 1)}
		cmd.Stdout = ready
		var pipeError error
		input, pipeError = cmd.StdinPipe()
		if pipeError != nil {
			return
		}
		defer input.Close()
	}
	if cmd.Start() != nil {
		return
	}
	result.Started = true
	if historyStdin!=nil {historyStdin.start()}
	access := uint32(0x0100 | 0x0001 | 0x0800)
	if strict { access |= syscall.SYNCHRONIZE | 0x1000 }
	if hasCapture {
		access |= 0x1000
	}
	process, err := syscall.OpenProcess(access, false, uint32(cmd.Process.Pid))
	assigned, retainedRoot := false, false
	if err == nil {
		ok, _, _ = kernel.NewProc("AssignProcessToJobObject").Call(job, uintptr(process))
		assigned = ok != 0
		if strict && assigned {
			strictCollector.root = uint32(cmd.Process.Pid)
			created, valid := askStrictProcessCreation(kernel, process, strictCollector.root)
			if strictCollector.faults != nil && strictCollector.faults.failRootIdentityOnce {
				strictCollector.faults.failRootIdentityOnce = false; strictCollector.faults.rootIdentityFaultSeen = true; valid = false
			}
			var belongs uint32
			memberOK, _, _ := kernel.NewProc("IsProcessInJob").Call(uintptr(process), job, uintptr(unsafe.Pointer(&belongs)))
			if strictCollector.faults != nil && strictCollector.faults.failRootMembershipOnce {
				strictCollector.faults.failRootMembershipOnce = false; strictCollector.faults.rootMembershipFaultSeen = true; belongs = 0
			}
			if !valid || memberOK == 0 || belongs == 0 { strictCollector.unknown = true; assigned = false }
			member := askStrictJobMember{handle: process, created: created}
			strictCollector.members[strictCollector.root] = member
			strictCollector.recordDiagnostic(kernel, strictCollector.root, member)
			retainedRoot = true
		}
		if assigned && hasCapture {
			var created, exited, kernelTime, userTime syscall.Filetime
			creation := uint64(0)
			if syscall.GetProcessTimes(process, &created, &exited, &kernelTime, &userTime) == nil {
				creation = uint64(created.HighDateTime)<<32 | uint64(created.LowDateTime)
			}
			if creation == 0 || ctx.Err() != nil {
				assigned = false
			} else {
				if history != nil {
					member, held := strictCollector.members[strictCollector.root]
					if !held || strictCollector.root != uint32(cmd.Process.Pid) || member.created != creation { assigned = false }
				}
				if assigned { result.Capture.NativeProcess = &askNativeProcess{PID: uint32(cmd.Process.Pid), Creation: creation} }
			}
		}
		if assigned {
			code, _, _ := syscall.NewLazyDLL("ntdll.dll").NewProc("NtResumeProcess").Call(uintptr(process))
			if code != 0 {
				assigned = false
			}
		}
		if !retainedRoot { syscall.CloseHandle(process) }
	} else if strict {
		strictCollector.unknown = true
	}
	if !assigned {
		_ = cmd.Process.Kill()
		if strict && retainedRoot {
			if stopped, _, _ := kernel.NewProc("TerminateJobObject").Call(job, 1); stopped == 0 { strictCollector.unknown = true }
		}
	} else {
		var permit [12]byte
		copy(permit[:8], "ASKSW001")
		binary.LittleEndian.PutUint32(permit[8:], uint32(os.Getpid()))
		if _, e := permitWrite.Write(permit[:]); e != nil {
			_ = cmd.Process.Kill()
		}
	}
	result.Assigned = assigned
	permitWrite.Close()
	permitRead.Close()
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()
	// Pin members observed while the root is alive. Job accounting can reach
	// active0 shortly before a terminated process's handle becomes signalled.
	members := map[uint32]syscall.Handle{}
	defer func() {
		for _, h := range members {
			syscall.CloseHandle(h)
		}
	}()
	collect := func() {
		if strict { strictCollector.collect(kernel, job); return }
		var ids [130]uintptr
		ok, _, _ := kernel.NewProc("QueryInformationJobObject").Call(job, 3, uintptr(unsafe.Pointer(&ids[0])), unsafe.Sizeof(ids), 0)
		if ok == 0 {
			return
		}
		n := *(*uint32)(unsafe.Add(unsafe.Pointer(&ids[0]), 4))
		if n > 128 {
			return
		}
		for _, pid := range ids[1 : n+1] {
			id := uint32(pid)
			if _, exists := members[id]; exists {
				continue
			}
			h, e := syscall.OpenProcess(syscall.SYNCHRONIZE|0x1000, false, id)
			if e != nil {
				continue
			}
			var belongs uint32
			ok, _, _ := kernel.NewProc("IsProcessInJob").Call(uintptr(h), job, uintptr(unsafe.Pointer(&belongs)))
			if ok != 0 && belongs != 0 {
				members[id] = h
			} else {
				syscall.CloseHandle(h)
			}
		}
	}
	var waitErr error
loop:
	for {
		collect()
		select {
		case waitErr = <-waited:
			break loop
		case <-historyFailure:
			historyFailure = nil
			_ = cmd.Process.Kill()
		case payload := <-func() <-chan []byte {
			if ready != nil {
				return ready.ready
			}
			return nil
		}():
			accepted := onReady(payload)
			clear(payload)
			if accepted && ctx.Err() == nil {
				_, _ = input.Write([]byte("ACCEPT\n"))
			} else {
				_, _ = input.Write([]byte("REJECT\n"))
			}
			input.Close()
		case <-time.After(5 * time.Millisecond):
		}
	}
	result.TimedOut = ctx.Err() != nil
	var exitError *exec.ExitError
	result.WaitVerified = cmd.ProcessState != nil && (waitErr == nil || errors.As(waitErr, &exitError))
	if history != nil {
		// ProcessState records the root wait even when a protocol copier failed.
		// Protocol/EOF/cancel remain separate required success conditions.
		result.WaitVerified = cmd.ProcessState != nil && cmd.ProcessState.Exited() && cmd.ProcessState.Pid() == int(strictCollector.root)
	}
	if cmd.ProcessState != nil {
		result.Exit = cmd.ProcessState.ExitCode()
	}
	if strict && (cmd.ProcessState == nil || cmd.ProcessState.Pid() != int(strictCollector.root)) {
		strictCollector.unknown = true
	}
	// Wait is over before reconciliation. A root exit alone does not prove
	// descendants gone; terminate the exact job and require its active count0.
	if strict { collect() }
	killed, _, _ := kernel.NewProc("TerminateJobObject").Call(job, 1)
	deadline := time.Now().Add(2 * time.Second)
	if strict {
		result.Reaped = strictCollector.finish(kernel, job, deadline, killed != 0, assigned, result.WaitVerified)
	} else {
	for killed != 0 && time.Now().Before(deadline) {
		var a askJobAccounting
		ok, _, _ = kernel.NewProc("QueryInformationJobObject").Call(job, 1, uintptr(unsafe.Pointer(&a)), unsafe.Sizeof(a), 0)
		if ok == 0 {
			break
		}
		if a.Active == 0 {
			result.Reaped = true
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	for _, h := range members {
		left := time.Until(deadline)
		if left < 0 {
			result.Reaped = false
			break
		}
		state, e := syscall.WaitForSingleObject(h, uint32(left/time.Millisecond))
		if e != nil || state != syscall.WAIT_OBJECT_0 {
			result.Reaped = false
		}
	}
	}
	if hasCapture {
		if history != nil {
			result.Capture.historyStdin = historyStdin.finish()
			result.Capture.protocolReason = history.resultReason()
			result.Capture.protocolWritesComplete = history.writesComplete()
			result.Capture.protocolWriteFailed = history.writeDidFail()
			result.Capture.protocolIOFailed = unslopHistoryIOFailed(waitErr, result.Capture.protocolWriteFailed, captured.eof, captureErr.eof, result.Capture.historyStdin)
		}
		result.Capture.Complete = assigned && result.WaitVerified && result.Reaped && !result.TimedOut && ctx.Err() == nil && !captured.overflow && captured.eof && captureErr.eof && (history == nil || result.Capture.protocolReason == "history_protocol_complete" && result.Capture.protocolWritesComplete && !result.Capture.protocolIOFailed)
	}
	if compilerMode && assigned && result.WaitVerified && result.Reaped && !result.TimedOut && result.Exit != 0 {
		result.Diagnostic = compilerCause(compilerOut.data[:compilerOut.n], compilerErr.data[:compilerErr.n], compilerOut.truncated || compilerErr.truncated)
	}
	if !compilerMode && !hasCapture && assigned && result.WaitVerified && result.Reaped && !result.TimedOut && (waitErr == nil || result.Exit == 1) && errOut.Len() == 0 {
		if ready != nil {
			out.Buffer = ready.Buffer
			if out.Len() == 0 && result.Exit != 0 {
				out.Buffer.Write(ready.first)
			}
		}
		result.Raw = append([]byte(nil), out.Bytes()...)
	}
	clear(out.Bytes())
	if ready != nil {
		clear(ready.first)
	}
	clear(errOut.Bytes())
	return
}

// Ten seconds bounds the producer process tree, not each synchronous API.
// A failed/stopped producer is followed by a separate bounded read-only
// reconciliation. Unknown is preserved and can never authorize a switch.
func supervisedAskSoftware(prepare bool) updateResult {
	return supervisedFixedSoftware(prepare, []string{"--ask-software-worker"}, []string{"--ask-software-inspection-worker"})
}

func supervisedFixedSoftware(prepare bool, workerArgs, inspectionArgs []string) updateResult {
	exe, err := os.Executable()
	if err != nil {
		return updateResult{Reason: "qualification_unavailable"}
	}
	operation := "verify"
	if prepare {
		operation = "prepare"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	child := askOwnedChild(ctx, exe, append(workerArgs, operation)...)
	cancel()
	defer clear(child.Raw)
	var r updateResult
	if child.Reaped && !child.TimedOut && json.Unmarshal(child.Raw, &r) == nil && child.Exit == 0 && r.Qualified && (!prepare || r.Prepared) {
		return r
	}
	if json.Unmarshal(child.Raw, &r) != nil {
		r = updateResult{Reason: "qualification_unavailable"}
	}
	r.Qualified, r.Prepared = false, false
	// A producer whose job could not be collected cannot safely be observed
	// as quiescent. Return explicit unknown without touching its output names.
	unknown := askReconciliation{Domain: "owner-question-fixed-software-reconcile-v1", Target: askReconciledObject{State: "unknown"}, Stage: askReconciledObject{State: "unknown"}, Backup: askReconciledObject{State: "unknown"}}
	r.Reconciliation = &unknown
	if !child.Reaped {
		return r
	}
	ctx, cancel = context.WithTimeout(context.Background(), 2*time.Second)
	observed := askOwnedChild(ctx, exe, inspectionArgs...)
	cancel()
	defer clear(observed.Raw)
	var snapshot askReconciliation
	if observed.Reaped && !observed.TimedOut && observed.Exit == 0 && json.Unmarshal(observed.Raw, &snapshot) == nil && validAskReconciliation(snapshot) {
		r.Reconciliation = &snapshot
	}
	return r
}

func runAskWorker(args []string, out io.Writer) int {
	if !askWorkerParent() {
		return 2
	}
	if len(args) == 1 && args[0] == ghaFreshWorkerFlag {
		return runGHAFreshInspectionWorker(args, out)
	}
	if (len(args) == 2 || len(args) == 3) && args[0] == "--mp4-build-worker" {
		build, ok := mp4BuildPlan(args[1])
		if !ok {
			return 2
		}
		if !build.GoRunner {
			if len(args) != 3 {
				return 2
			}
			directory, ok := admitSupervisedCargoDirectory(build, args[2])
			if !ok {
				return 2
			}
			return runSoftwareBuildWorker(build, out, context.WithValue(context.Background(), mp4SupervisedCargoDirectory{}, directory))
		}
		if len(args) != 2 {
			return 2
		}
		return runSoftwareBuildWorker(build, out)
	}
	if len(args) > 0 && (args[0] == "--mp4-software-worker" || args[0] == "--mp4-inspection-worker") {
		return runMP4SoftwareWorker(args, out)
	}
	if len(args) == 1 && args[0] == "--ask-software-inspection-worker" {
		return runAskReconciliation([]string{"--inspect-ask-privacy-update"}, out)
	}
	if len(args) == 1 && args[0] == "--ask-build-worker" {
		return runAskBuildWorker(out)
	}
	if len(args) != 2 || args[0] != "--ask-software-worker" || args[1] != "verify" && args[1] != "prepare" {
		return 2
	}
	r := performAskSoftware(args[1] == "prepare")
	if json.NewEncoder(out).Encode(r) != nil || !r.Qualified || args[1] == "prepare" && !r.Prepared {
		return 1
	}
	return 0
}

// A CLI string or environment value alone is not an internal-worker permit.
// Require a real inherited pipe and the actual OS parent to be this same
// executable. Direct shell callers cannot claim that native parent identity.
func askWorkerParent() bool {
	value, err := strconv.ParseUint(os.Getenv("ASK_SOFTWARE_PARENT_HANDLE"), 10, 64)
	if err != nil || value == 0 {
		return false
	}
	h := syscall.Handle(value)
	k := syscall.NewLazyDLL("kernel32.dll")
	typ, _, _ := k.NewProc("GetFileType").Call(uintptr(h))
	if typ != 3 {
		return false
	}
	defer syscall.CloseHandle(h)
	var basic struct {
		Exit     uintptr
		Peb      uintptr
		Affinity uintptr
		Priority uintptr
		PID      uintptr
		Parent   uintptr
	}
	code, _, _ := syscall.NewLazyDLL("ntdll.dll").NewProc("NtQueryInformationProcess").Call(^uintptr(0), 0, uintptr(unsafe.Pointer(&basic)), unsafe.Sizeof(basic), 0)
	if code != 0 || basic.Parent == 0 {
		return false
	}
	want := uint32(basic.Parent)
	parent, err := syscall.OpenProcess(0x1000, false, want)
	if err != nil {
		return false
	}
	defer syscall.CloseHandle(parent)
	name := make([]uint16, 32768)
	size := uint32(len(name))
	ok, _, _ := k.NewProc("QueryFullProcessImageNameW").Call(uintptr(parent), 0, uintptr(unsafe.Pointer(&name[0])), uintptr(unsafe.Pointer(&size)))
	if ok == 0 {
		return false
	}
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	a, err := os.Stat(exe)
	if err != nil {
		return false
	}
	b, err := os.Stat(syscall.UTF16ToString(name[:size]))
	if err != nil {
		return false
	}
	if !os.SameFile(a, b) {
		return false
	}
	// Read only after independently validating the actual parent. Use one
	// non-consuming kernel snapshot; Peek->Read could block if another reader
	// consumed bytes between the two calls.
	var raw [12]byte
	var n, available uint32
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		ok, _, _ := k.NewProc("PeekNamedPipe").Call(uintptr(h), uintptr(unsafe.Pointer(&raw[0])), 12, uintptr(unsafe.Pointer(&n)), uintptr(unsafe.Pointer(&available)), 0)
		if ok == 0 {
			return false
		}
		if n == 12 {
			return string(raw[:8]) == "ASKSW001" && binary.LittleEndian.Uint32(raw[8:]) == want
		}
		time.Sleep(time.Millisecond)
	}
	return false
}
