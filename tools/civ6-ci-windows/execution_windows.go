//go:build windows && amd64

package main

import (
	contract "cc/civ6-ci-contract"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

func pipeName(p Plan) string { return `\\.\pipe\` + p.name() }
func pipeInput(p Plan) ([]byte, error) {
	// The service receives one bounded message from the parent's stdin client.
	// No token is ever a service argument, SCM configuration, file or log.
	sid, e := serviceSID(p)
	if e != nil {
		return nil, e
	}
	var sd uintptr
	r, _, e := convertDescriptor.Call(ptr("D:P(A;;GA;;;SY)(A;;GA;;;BA)(A;;GA;;;"+sid+")(A;;0x40000080;;;"+p.ControllerSID+")"), 1, uintptr(unsafe.Pointer(&sd)), 0)
	if r == 0 {
		return nil, e
	}
	defer kernel.NewProc("LocalFree").Call(sd)
	sa := syscall.SecurityAttributes{Length: uint32(unsafe.Sizeof(syscall.SecurityAttributes{})), SecurityDescriptor: sd}
	h, _, e := kernel.NewProc("CreateNamedPipeW").Call(ptr(pipeName(p)), 0x1|0x40000000|0x80000, 0x4|0x2|0x8, 1, 4096, 4096, 0, uintptr(unsafe.Pointer(&sa)))
	if h == ^uintptr(0) {
		return nil, e
	}
	defer syscall.CloseHandle(syscall.Handle(h))
	event, _, e := kernel.NewProc("CreateEventW").Call(0, 1, 0, 0)
	if event == 0 {
		return nil, e
	}
	defer syscall.CloseHandle(syscall.Handle(event))
	ov := syscall.Overlapped{HEvent: syscall.Handle(event)}
	r, _, e = kernel.NewProc("ConnectNamedPipe").Call(h, uintptr(unsafe.Pointer(&ov)))
	if r == 0 && e != syscall.Errno(997) && e != syscall.Errno(535) {
		return nil, e
	}
	if e == syscall.Errno(997) {
		if e = waitIO(h, &ov, 30*time.Second); e != nil {
			return nil, e
		}
	}
	kernel.NewProc("ResetEvent").Call(event)
	ov = syscall.Overlapped{HEvent: syscall.Handle(event)}
	b := make([]byte, 4096)
	var n uint32
	e = syscall.ReadFile(syscall.Handle(h), b, &n, &ov)
	if e == syscall.Errno(997) {
		e = waitIO(h, &ov, 30*time.Second)
		if e == nil {
			r, _, callErr := kernel.NewProc("GetOverlappedResult").Call(h, uintptr(unsafe.Pointer(&ov)), uintptr(unsafe.Pointer(&n)), 0)
			if r == 0 {
				e = callErr
			}
		}
	}
	if e != nil {
		return nil, e
	}
	if n < 1 || n >= 4096 {
		return nil, errors.New("registration input size rejected")
	}
	var client uint32
	r, _, e = kernel.NewProc("GetNamedPipeClientProcessId").Call(h, uintptr(unsafe.Pointer(&client)))
	if r == 0 {
		return nil, e
	}
	var a admission
	if json.Unmarshal(b[:n], &a) != nil || a.validate() != nil || a.SenderPID != client {
		return nil, errors.New("registration allocation/client identity mismatch")
	}
	return b[:n], nil
}
func waitIO(h uintptr, ov *syscall.Overlapped, budget time.Duration) error {
	until := time.Now().Add(budget)
	for time.Now().Before(until) {
		w, e := syscall.WaitForSingleObject(ov.HEvent, 50)
		if e != nil {
			return e
		}
		if w == 0 {
			return nil
		}
		select {
		case <-stopService:
			goto cancel
		default:
		}
	}
cancel:
	kernel.NewProc("CancelIoEx").Call(h, uintptr(unsafe.Pointer(ov)))
	// Wait for the cancelled OVERLAPPED before releasing its storage.
	var n uint32
	kernel.NewProc("GetOverlappedResult").Call(h, uintptr(unsafe.Pointer(ov)), uintptr(unsafe.Pointer(&n)), 1)
	return errors.New("registration channel cancelled or expired")
}

func execute(p Plan) error {
	if p.Fixture == "" || p.RuntimeFixture {
		return executeThroughGate(p)
	}
	return executeAdmitted(p)
}
func executeAdmitted(p Plan) (result error) {
	var secret []byte
	var e error
	started, sawStopped := false, false
	var serviceStart time.Time
	emitEvent("execute-begin", p, nil)
	defer func() {
		// One stop request at most, and only when the loop did not see STOPPED.
		// Then hold (observe only) until the launcher proves the Job Object drained
		// or the absolute deadline passes; only that releases the Cargo slot.
		fence := false
		if started {
			if !sawStopped {
				result = errors.Join(result, stopFinite(p))
			}
			hold := holdUntilDrained(realHoldOps(p), executionDeadline(p, serviceStart).Add(90*time.Second), 250*time.Millisecond)
			fence = !(hold.Stopped && hold.Drained)
			result = errors.Join(result, hold.Err)
			if fence && result == nil {
				result = errors.New("service drain not proven")
			}
		}
		// A begin without a clean end, or fence=true, tells the queue that the
		// worker may still hold the namespace: it must not start another one.
		emitEvent("execute-end", p, map[string]any{"ok": result == nil, "service_started": started, "service_stopped": !fence, "fence": fence, "retryable_prestart": !started && retryablePrestart(result)})
	}()
	if e = prestartBudget(p, time.Now()); e != nil {
		return e
	}
	if p.Fixture == "" || p.RuntimeFixture {
		if e = verifyGateAllocation(); e != nil {
			return e
		}
		if p.Fixture == "" {
			if e = audit(p); e != nil {
				return e
			}
			if e = checkRegistrationQueue(p); e != nil {
				return e
			}
		}
		secret = []byte("credential-free-fixture")
		if !p.RuntimeFixture {
			secret, e = registrationCredential(p)
		}
		if e != nil {
			return e
		}
		if len(secret) < 1 || strings.ContainsAny(string(secret), "\x00\r\n") {
			clear(secret)
			return errors.New("registration credential rejected")
		}
		defer clear(secret)
	}
	// SCM can start the service yet return an error (e.g. a startup timeout).
	// The one-shot deferred stop queries actual state even after that error.
	serviceStart = time.Now()
	if e = prestartBudget(p, serviceStart); e != nil {
		return e
	}
	started = true
	if e = startFinite(p); e != nil {
		return e
	}
	if p.Fixture == "" || p.RuntimeFixture {
		until := time.Now().Add(30 * time.Second)
		for {
			f, err := openRegistrationPipe(p)
			if err == nil {
				e = verifyPipeServer(p, f)
				if e != nil {
					f.Close()
					break
				}
				payload, _ := json.Marshal(admission{Token: string(secret), Held: os.Getenv("CODEX_JOBS_CARGO_LOCK_HELD"), SenderPID: uint32(os.Getpid()), Jobs: os.Getenv("CARGO_BUILD_JOBS"), Threads: os.Getenv("RUST_TEST_THREADS"), Deadline: executionDeadline(p, serviceStart).UnixNano()})
				if len(payload) >= 4096 {
					e = errors.New("registration frame exceeds pipe bound")
				} else {
					_, e = f.Write(payload)
				}
				clear(payload)
				e = errors.Join(e, f.Close())
				break
			}
			if !errors.Is(err, syscall.ERROR_FILE_NOT_FOUND) && !errors.Is(err, syscall.Errno(231)) {
				e = err
				break
			}
			if time.Now().After(until) {
				e = errors.New("service registration channel deadline")
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		if e != nil {
			return e
		}
	}
	s, e := openService(p, 4)
	if e != nil {
		return e
	}
	defer closeSC(s)
	until := time.Now().Add(time.Duration(p.Seconds)*time.Second + 40*time.Second)
	mon := newMonitor(p)
	for time.Now().Before(until) {
		state, _, code, e := queryService(s)
		if e != nil {
			return e
		}
		if state == 1 {
			sawStopped = true
			if code != 0 {
				return errors.New("finite service reported failure; inspect receipts")
			}
			if p.Fixture == "" || p.RuntimeFixture {
				if p.RuntimeFixture {
					return nil
				}
				if e = validateLauncherSuccess(p); e != nil {
					return e
				}
				if e = auditFinished(p); e == nil {
					emitEvent("job-success-verified", p, nil)
				}
				return e
			}
			return nil
		}
		if p.Fixture == "" {
			// Hook-start and hook-complete cross-checks use the controller's
			// OAuth context; the filtered service account has none.
			if e = mon.tick(time.Now()); e != nil {
				return e
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	return errors.New("finite execution deadline")
}

func retryablePrestart(err error) bool {
	return errors.Is(err, errPrestartBudget) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, contract.ErrRegistrationQueueBusy) || errors.Is(err, errGitHubReadTransport)
}

func runOwnedWorker(p Plan) error {
	if e := saveNew(filepath.Join(p.runtime(), "started.json"), map[string]any{"service_pid": os.Getpid(), "single_use": true}); e != nil {
		return errors.New("service execution is single-use")
	}
	var secret []byte
	var err error
	var admitted admission
	if p.Fixture == "" || p.RuntimeFixture {
		secret, err = pipeInput(p)
		if err != nil {
			return err
		}
		defer clear(secret)
		if json.Unmarshal(secret, &admitted) != nil || admitted.validate() != nil {
			return errors.New("bounded gate admission rejected")
		}
	}
	token, err := narrowToken()
	if err != nil {
		return err
	}
	defer syscall.CloseHandle(token)
	if err = authenticatedUsersDenyOnly(token); err != nil {
		return err
	}
	launchReport.TokenAUDenyOnlyVerified = true
	launchReport.AccountSID, err = tokenUserSID(token)
	if err != nil {
		return err
	}
	job, _, e := createJob.Call(0, 0)
	if job == 0 {
		return e
	}
	defer syscall.CloseHandle(syscall.Handle(job))
	limits := jobExtendedLimits{}
	limits.Basic.Flags = 0x2000
	r, _, e := setJob.Call(job, 9, uintptr(unsafe.Pointer(&limits)), unsafe.Sizeof(limits))
	if r == 0 {
		return e
	}
	// Explicit environment excludes all host credentials, owner homes and jobs.
	env := envFor(p)
	env = append(env, "CIW_LAUNCHER_PID="+strconv.Itoa(os.Getpid()))
	if p.Fixture == "" || p.RuntimeFixture {
		env = append(env, "ACTIONS_RUNNER_INPUT_TOKEN="+admitted.Token, "CODEX_JOBS_CARGO_LOCK_HELD="+admitted.Held, "CARGO_BUILD_JOBS="+admitted.Jobs, "RUST_TEST_THREADS="+admitted.Threads, "NEXTEST_TEST_THREADS="+admitted.Threads)
	}
	var block []uint16
	for _, v := range env {
		u, err := syscall.UTF16FromString(v)
		if err != nil {
			return err
		}
		block = append(block, u...)
	}
	block = append(block, 0)
	defer clear(block)
	image := filepath.Join(p.Root, "helper.exe")
	cmd := wide(syscall.EscapeArg(image) + " worker " + syscall.EscapeArg(p.Root))
	info := syscall.StartupInfo{Cb: uint32(unsafe.Sizeof(syscall.StartupInfo{}))}
	var process syscall.ProcessInformation
	r, _, e = processAsUser.Call(uintptr(token), ptr(image), uintptr(unsafe.Pointer(cmd)), 0, 0, 0, 0x08000000|4|0x400, uintptr(unsafe.Pointer(&block[0])), ptr(p.runtime()), uintptr(unsafe.Pointer(&info)), uintptr(unsafe.Pointer(&process)))
	if r == 0 {
		cmd = wide(syscall.EscapeArg(image) + " worker " + syscall.EscapeArg(p.Root))
		r, _, e = processWithToken.Call(uintptr(token), 0, ptr(image), uintptr(unsafe.Pointer(cmd)), 0x08000000|4|0x400, uintptr(unsafe.Pointer(&block[0])), ptr(p.runtime()), uintptr(unsafe.Pointer(&info)), uintptr(unsafe.Pointer(&process)))
	}
	if r == 0 {
		return fmt.Errorf("start filtered service child: %w", e)
	}
	defer syscall.CloseHandle(process.Process)
	defer syscall.CloseHandle(process.Thread)
	launchReport.WorkerPID = process.ProcessId
	r, _, e = assignJob.Call(job, uintptr(process.Process))
	if r == 0 {
		_ = syscall.TerminateProcess(process.Process, 1)
		_, waitErr := syscall.WaitForSingleObject(process.Process, 30000)
		return errors.Join(e, waitErr)
	}
	// The shared absolute deadline also bounds the time spent starting up: never
	// resume a worker that would outlive what the controller is holding for.
	until, e := workerDeadline(p, admitted, time.Now())
	if e != nil {
		return errors.Join(e, killOwned(job, process.Process))
	}
	count, _, e := resumeThread.Call(uintptr(process.Thread))
	if count != 1 {
		return errors.Join(fmt.Errorf("initial suspend count %d: %v", count, e), killOwned(job, process.Process))
	}
	cancelled := false
	var exit uint32
	for {
		if p.Fixture != "" && launchReport.FixtureChildPID == 0 {
			var ready struct {
				Worker   uint32 `json:"worker_pid"`
				Child    uint32 `json:"child_pid"`
				Observed bool   `json:"child_observed_alive"`
			}
			if b, err := os.ReadFile(filepath.Join(p.runtime(), "fixture.json")); err == nil && json.Unmarshal(b, &ready) == nil && ready.Worker == process.ProcessId && ready.Child != 0 && ready.Observed {
				launchReport.FixtureChildPID = ready.Child
				if err = publishLauncherReport(); err != nil {
					return errors.Join(err, killOwned(job, process.Process))
				}
			}
		}
		w, e := syscall.WaitForSingleObject(process.Process, 50)
		if e != nil {
			return errors.Join(e, killOwned(job, process.Process))
		}
		if w == 0 {
			if e = syscall.GetExitCodeProcess(process.Process, &exit); e != nil {
				return e
			}
			break
		}
		select {
		case <-stopService:
			cancelled = true
		default:
		}
		if cancelled || time.Now().After(until) {
			cancelled = true
			break
		}
	}
	// Retained member handles + active-zero + root signalled. A timeout is a
	// failure, never proof of drain. SCM/controller keeps the residual visible.
	pids, drainErr := killOwnedReport(job, process.Process)
	e = drainErr
	if e == nil {
		e = syscall.GetExitCodeProcess(process.Process, &exit)
		if e == nil {
			launchReport.WorkerExit = &exit
		}
	}
	launchReport.Cancelled = cancelled
	launchReport.Drained = e == nil
	launchReport.MemberPIDs = pids
	receipt := map[string]any{"root_pid": process.ProcessId, "retained_member_pids": pids, "retained_handles_signalled": e == nil, "root_exit": launchReport.WorkerExit, "cancelled": cancelled, "job_drained": e == nil, "account_sid_observed": launchReport.AccountSID, "token_au_deny_only_verified": launchReport.TokenAUDenyOnlyVerified, "load_user_profile_api_requested": false, "scm_profile_state_not_measured_here": true, "official_ci_claimed": false}
	writeErr := saveNew(filepath.Join(p.runtime(), "drain.json"), receipt)
	if e != nil || writeErr != nil {
		return errors.Join(e, writeErr)
	}
	if cancelled && p.Fixture != "cancel" {
		return errors.New("finite service cancelled or expired")
	}
	if !cancelled && p.Fixture == "cancel" {
		return errors.New("cancel fixture did not receive cancellation")
	}
	if exit != 0 && !cancelled {
		return errors.New("worker exit propagated")
	}
	return nil
}
func killOwned(job uintptr, root syscall.Handle) error {
	_, e := killOwnedReport(job, root)
	return e
}
func killOwnedReport(job uintptr, root syscall.Handle) ([]uint32, error) {
	handles, snapshotErr := runnerMembers(job)
	var pids []uint32
	for _, h := range handles {
		pid, _, err := kernel.NewProc("GetProcessId").Call(uintptr(h))
		if pid == 0 {
			snapshotErr = errors.Join(snapshotErr, err)
		} else {
			pids = append(pids, uint32(pid))
		}
	}
	defer func() {
		for _, h := range handles {
			syscall.CloseHandle(h)
		}
	}()
	r, _, e := terminateJob.Call(job, 125)
	if r == 0 {
		return pids, errors.Join(snapshotErr, e)
	}
	return pids, errors.Join(snapshotErr, waitRunnerJob(job, root, handles, 30*time.Second))
}

func worker(p Plan) error {
	var t syscall.Handle
	r, _, e := openToken.Call(^uintptr(0), 8, uintptr(unsafe.Pointer(&t)))
	if r == 0 {
		return e
	}
	defer syscall.CloseHandle(t)
	sid, e := tokenUserSID(t)
	want, lookup := serviceSID(p)
	if e != nil || lookup != nil || sid != want {
		return errors.New("worker virtual identity rejected")
	}
	if e = authenticatedUsersDenyOnly(t); e != nil {
		return e
	}
	if p.Fixture == "" || p.RuntimeFixture {
		if e = initializeSDK(p); e != nil {
			return e
		}
		stopCache, err := startOwnedCache(&p)
		if err != nil {
			return err
		}
		defer stopCache()
	}
	if p.Fixture != "" {
		return serviceFixture(p, sid, e == nil)
	}
	token := os.Getenv("ACTIONS_RUNNER_INPUT_TOKEN")
	os.Unsetenv("ACTIONS_RUNNER_INPUT_TOKEN")
	if len(token) == 0 || len(token) > 4095 {
		return errors.New("registration token missing")
	}
	listener := filepath.Join(p.runner(), "bin", "Runner.Listener.exe")
	c := exec.Command(listener, configureArgs(p)...)
	hidden(c)
	c.Dir = p.runner()
	c.Env = append(workerEnv(p), "ACTIONS_RUNNER_INPUT_TOKEN="+token)
	e = runRunnerCommand(p.runtime(), "configure", c)
	token = ""
	c.Env = nil
	if e != nil {
		return errors.New("official runner configure failed")
	}
	// Listener credentials remain only in runtime/runner, ACL limited to the
	// dedicated account, SYSTEM and administrators until deregister + collect.
	c = exec.Command(listener, "run", "--startuptype", "service")
	hidden(c)
	c.Dir = p.runner()
	c.Env, e = runtimeSDKEnv(p, workerEnv(p))
	if e != nil {
		return e
	}
	if e = runRunnerCommand(p.runtime(), "listener", c); e != nil {
		return errors.New("official runner listener failed")
	}
	return nil
}
func serviceFixture(p Plan, sid string, auChecked bool) error {
	if p.RuntimeFixture {
		if e := checkoutFixture(p); e != nil {
			return e
		}
		if e := runtimeFixtureCommands(p); e != nil {
			return e
		}
	}
	c := exec.Command(filepath.Join(p.Root, "helper.exe"), "fixture-child", p.Root)
	hidden(c)
	c.Env = envFor(p)
	var rustPID uint32
	if p.RuntimeFixture && p.Fixture == "cancel" {
		ready := filepath.Join(p.runtime(), "tmp", "rust-ready")
		c = exec.Command(filepath.Join(p.runtime(), "cargo", "bin", "cargo.exe"), "+stable", "test", "--locked", "--offline", "--target-dir", p.target(), "--", "--test-threads=1")
		hidden(c)
		env, err := runtimeSDKEnv(p, workerEnv(p))
		if err != nil {
			return err
		}
		c.Env = append(env, "CIW_FIXTURE_READY="+ready)
		c.Dir = filepath.Join(p.source(), "tools")
		f, err := os.OpenFile(filepath.Join(p.runtime(), "fixture-cancel-cargo.log"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		defer f.Close()
		c.Stdout = f
		c.Stderr = f
	}
	if e := c.Start(); e != nil {
		return e
	}
	if p.RuntimeFixture && p.Fixture == "cancel" {
		until := time.Now().Add(60 * time.Second)
		for time.Now().Before(until) {
			if b, err := os.ReadFile(filepath.Join(p.runtime(), "tmp", "rust-ready")); err == nil {
				n, err := strconv.ParseUint(string(b), 10, 32)
				if err == nil && n != 0 {
					rustPID = uint32(n)
					break
				}
			}
			time.Sleep(50 * time.Millisecond)
		}
		if rustPID == 0 {
			return errors.New("actual Rust test readiness deadline")
		}
		h, err := syscall.OpenProcess(0x1000|0x100000, false, rustPID)
		if err != nil {
			return err
		}
		defer syscall.CloseHandle(h)
		w, err := syscall.WaitForSingleObject(h, 0)
		if err != nil || w != 258 {
			return errors.New("actual Rust fixture is not alive")
		}
		var member uint32
		r, _, _ := inJob.Call(uintptr(h), 0, uintptr(unsafe.Pointer(&member)))
		if r == 0 || member == 0 {
			return errors.New("actual Rust fixture outside Job Object")
		}
	}
	h, e := syscall.OpenProcess(0x100000|0x1000, false, uint32(c.Process.Pid))
	if e != nil {
		return e
	}
	defer syscall.CloseHandle(h)
	w, e := syscall.WaitForSingleObject(h, 0)
	if e != nil || w != 258 {
		return errors.New("fixture child not observed alive")
	}
	launcherID, err := strconv.ParseUint(os.Getenv("CIW_LAUNCHER_PID"), 10, 32)
	if err != nil || launcherID == 0 {
		return errors.New("explicit launcher PID missing")
	}
	launcher, err := syscall.OpenProcess(0x1000, false, uint32(launcherID))
	if err == nil {
		syscall.CloseHandle(launcher)
		return errors.New("filtered child reopened launcher")
	}
	if !errors.Is(err, syscall.ERROR_ACCESS_DENIED) {
		return errors.New("launcher denial not proven")
	}
	write, err := os.OpenFile(filepath.Join(p.Root, "plan.json"), os.O_WRONLY, 0)
	if err == nil {
		write.Close()
		return errors.New("worker can overwrite immutable plan")
	}
	if !errors.Is(err, syscall.ERROR_ACCESS_DENIED) {
		return errors.New("immutable plan protection not proven")
	}
	if e = saveNew(filepath.Join(p.runtime(), "fixture.json"), map[string]any{"worker_pid": os.Getpid(), "child_pid": c.Process.Pid, "rust_test_pid": rustPID, "child_observed_alive": true, "case": p.Fixture, "account_sid_observed": sid, "explicit_launcher_pid": launcherID, "launcher_reopen_denied": true, "control_write_denied": true, "authenticated_users_deny_only_verified": auChecked}); e != nil {
		return e
	}
	if p.Fixture == "cancel" {
		time.Sleep(time.Duration(p.Seconds+10) * time.Second)
	} else {
		time.Sleep(200 * time.Millisecond)
	}
	return nil
}

func runtimeFixtureCommands(p Plan) error {
	env, err := runtimeSDKEnv(p, workerEnv(p))
	if err != nil {
		return err
	}
	for _, v := range []struct {
		stage, name string
		args        []string
	}{
		{"fixture-rustup", "rustup.exe", []string{"show", "active-toolchain"}},
		{"fixture-cargo-1.95.0", "cargo.exe", []string{"+1.95.0", "test", "--locked", "--offline", "--target-dir", p.target(), "--", "--test-threads=1"}},
		{"fixture-cargo-stable", "cargo.exe", []string{"+stable", "test", "--locked", "--offline", "--target-dir", p.target(), "--", "--test-threads=1"}},
	} {
		if _, e := sdkCommand(p, v.stage, filepath.Join(p.runtime(), "cargo", "bin", v.name), env, v.args...); e != nil {
			return e
		}
	}
	// The changed source must fail before the underlying tool emits a version.
	file := filepath.Join(p.source(), "tools", "src", "lib.rs")
	original, e := os.ReadFile(file)
	if e != nil {
		return e
	}
	if e = os.WriteFile(file, []byte("synthetic tamper"), 0600); e != nil {
		return e
	}
	_, refused := sdkCommand(p, "fixture-source-reject", filepath.Join(p.runtime(), "cargo", "bin", "rustup.exe"), env, "--version")
	if e = os.WriteFile(file, original, 0600); e != nil {
		return e
	}
	if refused == nil {
		return errors.New("tool entry accepted changed source")
	}
	var rejection struct{ Exit int }
	b, e := os.ReadFile(filepath.Join(p.runtime(), "sdk-fixture-source-reject.json"))
	if e != nil || json.Unmarshal(b, &rejection) != nil || rejection.Exit != 1 {
		return errors.New("source refusal exit was not preserved")
	}
	raw, e := os.ReadFile(filepath.Join(p.runtime(), "sdk-fixture-source-reject.log"))
	if e != nil || strings.TrimSpace(string(raw)) != "actual tracked bytes mismatch" {
		return errors.New("source refusal reason not measured")
	}
	_, failed := sdkCommand(p, "fixture-cargo-failure", filepath.Join(p.runtime(), "cargo", "bin", "cargo.exe"), env, "not-a-real-cargo-subcommand")
	if failed == nil {
		return errors.New("original Cargo failure was hidden")
	}
	b, e = os.ReadFile(filepath.Join(p.runtime(), "sdk-fixture-cargo-failure.json"))
	if e != nil || json.Unmarshal(b, &rejection) != nil || rejection.Exit != 101 {
		return errors.New("original Cargo exit 101 was not propagated")
	}
	return nil
}
