//go:build windows

package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
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
type askChildResult struct {
	Raw                             []byte
	Diagnostic                      *compilerDiagnostic
	Exit                            int
	TimedOut, Reaped                bool
	Started, Assigned, WaitVerified bool
	Capture                         *askChildCaptureResult
	CollectionReason                string
	CollectionObserved, CollectionTotal uint32
	CollectionTotalKnown             bool
	CollectionQuery                  askJobQueryDiagnostic
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

type askOwnedProducerKey struct{}

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
	producer, hasProducer := ctx.Value(askOwnedProducerKey{}).(*askProducerRuntime)
	capture, hasCapture := ctx.Value(askChildCaptureKey{}).(askChildCapture)
	if hasProducer && (hasCapture || onReady != nil) || hasCapture && onReady != nil {
		return
	}
	var captured *askCaptureOutput
	var history *unslopHistoryExchange
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
		if value := ctx.Value(unslopHistoryProtocolKey{}); value != nil {
			plan, valid := value.(unslopHistoryProtocol)
			if !valid || len(capture.Stdin) != 0 { result.Capture.protocolReason = "history_protocol_plan_invalid"; return }
			var err error
			history, err = newUnslopHistoryExchange(ctx, plan)
			if err != nil { result.Capture.protocolReason = "history_protocol_plan_invalid"; return }
			defer history.Close()
			historyFailure = history.failed
		}
	}
	if ctx.Value(unslopHistoryProtocolKey{}) != nil && !hasCapture { return }
	kernel := syscall.NewLazyDLL("kernel32.dll")
	job, _, _ := kernel.NewProc("CreateJobObjectW").Call(0, 0)
	if job == 0 {
		return
	}
	defer syscall.CloseHandle(syscall.Handle(job))
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
		if history != nil { cmd.Stdin = history } else { cmd.Stdin = bytes.NewReader(fixedInput) }
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
		if history != nil { cmd.Stdout = &unslopHistoryCaptureOutput{capture:captured,exchange:history} }
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
	if hasProducer {
		cmd.Stdout = &producer.output
		captureErr = &askCaptureDiagnostic{buffer: new(compilerCapture)}
		defer captureErr.buffer.erase()
		cmd.Stderr = captureErr
		var pipeErr error
		input, pipeErr = cmd.StdinPipe()
		if pipeErr != nil {
			return
		}
		defer input.Close()
	}
	if cmd.Start() != nil {
		return
	}
	result.Started = true
	access := uint32(0x0100 | 0x0001 | 0x0800 | syscall.SYNCHRONIZE | 0x1000)
	if hasCapture || hasProducer {
		access |= 0x1000
	}
	process, err := syscall.OpenProcess(access, false, uint32(cmd.Process.Pid))
	assigned := false
	var collection *askJobCollection
	if err == nil {
		defer syscall.CloseHandle(process)
		ok, _, _ = kernel.NewProc("AssignProcessToJobObject").Call(job, uintptr(process))
		assigned = ok != 0
		if assigned && (hasCapture || hasProducer) {
			var created, exited, kernelTime, userTime syscall.Filetime
			creation := uint64(0)
			if syscall.GetProcessTimes(process, &created, &exited, &kernelTime, &userTime) == nil {
				creation = uint64(created.HighDateTime)<<32 | uint64(created.LowDateTime)
			}
			if creation == 0 || ctx.Err() != nil {
				assigned = false
			} else {
				identity := &askNativeProcess{PID: uint32(cmd.Process.Pid), Creation: creation}
				if hasCapture {
					result.Capture.NativeProcess = identity
				}
				if hasProducer {
					producer.native = identity
				}
			}
		}
		if assigned {
			collection = newAskJobCollector(syscall.Handle(job), process, uint32(cmd.Process.Pid), true)
			collection.Observe()
			code, _, _ := syscall.NewLazyDLL("ntdll.dll").NewProc("NtResumeProcess").Call(uintptr(process))
			if code != 0 {
				assigned = false
			}
		}
	}
	if !assigned {
		_ = cmd.Process.Kill()
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
	if collection == nil {
		collection = newAskJobCollector(syscall.Handle(job), process, uint32(cmd.Process.Pid), false)
	}
	defer collection.Close()
	var waitErr error
	producerReady, producerACKSent, producerACKAttempted := false, false, false
	var producerWrite chan error
loop:
	for {
		collection.Observe()
		select {
		case waitErr = <-waited:
			break loop
		case <-historyFailure:
			historyFailure = nil
			_ = cmd.Process.Kill()
		case <-ctx.Done():
			if input != nil {
				input.Close()
			}
			_ = cmd.Process.Kill()
		case frame := <-func() <-chan []byte {
			if hasProducer {
				return producer.output.frames
			}
			return nil
		}():
			// Capture any startup members before exposing Ready or allowing
			// a rejected Final to cancel and terminate the producer.
			collection.Observe()
			if !producerReady {
				if !producer.receiveReady(frame) {
					producer.cancel()
				} else {
					producerReady = true
				}
			} else {
				producer.receiveFinal(frame, producerACKAttempted)
			}
			clear(frame)
		case ack := <-func() <-chan askProducerACK {
			if hasProducer && producerReady && !producerACKAttempted {
				return producer.accept
			}
			return nil
		}():
			if !producer.validACK(ack) || ctx.Err() != nil {
				producer.cancel()
				continue
			}
			payload, err := json.Marshal(ack)
			if err != nil || len(payload) >= 4096 {
				clear(payload)
				producer.cancel()
				continue
			}
			payload = append(payload, '\n')
			producerACKAttempted = true
			producerWrite = make(chan error, 1)
			go func(raw []byte, done chan error) {
				defer clear(raw)
				n, err := input.Write(raw)
				if err == nil && n != len(raw) {
					err = io.ErrShortWrite
				}
				input.Close()
				done <- err
			}(payload, producerWrite)
		case err := <-producerWrite:
			producerACKSent = err == nil
			producerWrite = nil
			if err != nil {
				producer.cancel()
			}
		case payload := <-func() <-chan []byte {
			if ready != nil {
				return ready.ready
			}
			return nil
		}():
			collection.Observe()
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
	if hasProducer && producerWrite != nil {
		input.Close()
		producerACKSent = (<-producerWrite) == nil
	}
	result.TimedOut = ctx.Err() != nil
	result.WaitVerified = askRootWaitVerified(cmd.ProcessState, waitErr)
	if cmd.ProcessState != nil {
		result.Exit = cmd.ProcessState.ExitCode()
	}
	// Root Wait, all retained process signals and the job's complete lifetime
	// member count must agree. Observation failures remain unknown throughout.
	finishAskChildCollection(&result, collection, time.Now().Add(2*time.Second))
	if hasCapture {
		if history != nil { result.Capture.protocolReason = history.resultReason() }
		result.Capture.Complete = assigned && result.WaitVerified && result.Reaped && !result.TimedOut && ctx.Err() == nil && !captured.overflow && captured.eof && captureErr.eof && (history == nil || result.Capture.protocolReason == "history_protocol_complete")
	}
	if hasProducer {
		producer.finish(result, producerACKSent, captureErr.eof)
	}
	if compilerMode && assigned && result.WaitVerified && result.Reaped && !result.TimedOut && result.Exit != 0 {
		result.Diagnostic = compilerCause(compilerOut.data[:compilerOut.n], compilerErr.data[:compilerErr.n], compilerOut.truncated || compilerErr.truncated)
	}
	if !compilerMode && !hasCapture && !hasProducer && assigned && result.WaitVerified && result.Reaped && !result.TimedOut && (waitErr == nil || result.Exit == 1) && errOut.Len() == 0 {
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

// The supervisor and focused collection controls use the same final result path.
// Collection diagnostics do not grant protocol, input or process authority.
func finishAskChildCollection(result *askChildResult, collection *askJobCollection, deadline time.Time) {
	collected := collection.Finish(result.WaitVerified, deadline)
	result.Reaped = collected.Reaped
	result.CollectionReason = collected.Reason
	result.CollectionObserved, result.CollectionTotal = collected.Observed, collected.Total
	result.CollectionTotalKnown = collected.TotalKnown
	result.CollectionQuery = collected.Query
}

func askRootWaitVerified(state *os.ProcessState, _ error) bool {
	// Cmd.Wait may return a context or pipe-copy error after Process.Wait has
	// already collected an exiting root. Keep that native fact separate from
	// cancellation, EOF and protocol completion, which remain failure gates.
	return state != nil && state.Exited()
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
