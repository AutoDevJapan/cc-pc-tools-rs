//go:build windows && amd64

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

// Explicit opt-in, credential-free, newly prepared fixture roots only. This
// reduces the caller's OWN token; it never obtains another user's token or
// loads a profile. No product, registration, shared gate or Cargo is executed.
func TestNonAdminFixtureController(t *testing.T) {
	root := os.Getenv("PR148_TEST_ROOT")
	if root == "" {
		t.Skip("explicit newly prepared fixture root required")
	}
	p, e := readPlan(root)
	if e != nil || p.Fixture == "" {
		t.Fatal("credential-free fixture plan required", e)
	}
	var own syscall.Handle
	r, _, e := openToken.Call(^uintptr(0), 0xb, uintptr(unsafe.Pointer(&own)))
	if r == 0 {
		t.Fatal(e)
	}
	defer syscall.CloseHandle(own)
	sid, e := tokenUserSID(own)
	if e != nil || sid != p.ControllerSID {
		t.Fatal("only the caller's own controller token is accepted")
	}
	var ba uintptr
	r, _, e = convertSID.Call(ptr("S-1-5-32-544"), uintptr(unsafe.Pointer(&ba)))
	if r == 0 {
		t.Fatal(e)
	}
	defer kernel.NewProc("LocalFree").Call(ba)
	disable := sidAndAttributes{SID: ba}
	var limited syscall.Handle
	r, _, e = restrictToken.Call(uintptr(own), 1, 1, uintptr(unsafe.Pointer(&disable)), 0, 0, 0, 0, uintptr(unsafe.Pointer(&limited)))
	if r == 0 {
		t.Fatal(e)
	}
	defer syscall.CloseHandle(limited)
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	env := []string{`SystemRoot=C:\Windows`, `WINDIR=C:\Windows`, `PATH=C:\Windows\System32`, "PR148_TEST_CHILD_ROOT=" + root}
	var block []uint16
	for _, entry := range env {
		u, err := syscall.UTF16FromString(entry)
		if err != nil {
			t.Fatal(err)
		}
		block = append(block, u...)
	}
	block = append(block, 0)
	args := wide(syscall.EscapeArg(exe) + " -test.run=^TestFixtureControllerProcess$ -test.v")
	si := syscall.StartupInfo{Cb: uint32(unsafe.Sizeof(syscall.StartupInfo{}))}
	var pi syscall.ProcessInformation
	r, _, e = processWithToken.Call(uintptr(limited), 0, ptr(exe), uintptr(unsafe.Pointer(args)), 0x08000000|0x400, uintptr(unsafe.Pointer(&block[0])), ptr(filepath.Dir(exe)), uintptr(unsafe.Pointer(&si)), uintptr(unsafe.Pointer(&pi)))
	if r == 0 {
		t.Fatal(e)
	}
	defer syscall.CloseHandle(pi.Process)
	defer syscall.CloseHandle(pi.Thread)
	w, e := syscall.WaitForSingleObject(pi.Process, uint32((time.Duration(p.Seconds)*time.Second + 45*time.Second).Milliseconds()))
	if e != nil || w != 0 {
		_ = syscall.TerminateProcess(pi.Process, 125)
		_, _ = syscall.WaitForSingleObject(pi.Process, 30000)
		t.Fatal("restricted controller deadline", e)
	}
	var code uint32
	if e = syscall.GetExitCodeProcess(pi.Process, &code); e != nil || code != 0 {
		t.Fatal("restricted controller failed; inspect its owned result", code, e)
	}
}
func TestFixtureControllerProcess(t *testing.T) {
	root := os.Getenv("PR148_TEST_CHILD_ROOT")
	if root == "" {
		t.Skip("restricted controller child only")
	}
	p, e := readPlan(root)
	if e != nil || p.Fixture == "" {
		t.Fatal("fixture required", e)
	}
	exe, _ := os.Executable()
	result := map[string]any{"case": p.Fixture, "controller_pid": os.Getpid(), "official_runner_executed": false}
	e = restrictedControllerCase(p, result)
	result["success"] = e == nil
	if e != nil {
		result["error"] = e.Error()
	}
	write := saveNew(filepath.Join(filepath.Dir(exe), "controller-"+p.Instance+".json"), result)
	if e != nil || write != nil {
		t.Fatal(e, write)
	}
}
func restrictedControllerCase(p Plan, out map[string]any) error {
	var ba uintptr
	r, _, e := convertSID.Call(ptr("S-1-5-32-544"), uintptr(unsafe.Pointer(&ba)))
	if r == 0 {
		return e
	}
	defer kernel.NewProc("LocalFree").Call(ba)
	var member uint32
	r, _, e = advapi.NewProc("CheckTokenMembership").Call(0, ba, uintptr(unsafe.Pointer(&member)))
	if r == 0 || member != 0 {
		return errors.New("controller still has administrator membership")
	}
	out["admin_membership_disabled"] = true
	h, err := manager(3)
	if err == nil {
		closeSC(h)
		return errors.New("controller retained SCM create rights")
	}
	if !errors.Is(err, syscall.ERROR_ACCESS_DENIED) {
		return err
	}
	out["scm_create_denied"] = true
	if _, err = os.ReadDir(p.runtime()); !errors.Is(err, syscall.ERROR_ACCESS_DENIED) {
		return errors.New("private runtime enumeration was not denied")
	}
	out["private_runtime_read_denied"] = true
	if e = checkControl(p.Root, true); e != nil {
		return e
	}
	out["control_preflight_passed"] = true
	done := make(chan error, 1)
	go func() { done <- execute(p) }()
	if p.Fixture == "cancel" {
		until := time.Now().Add(20 * time.Second)
		ready := false
		for time.Now().Before(until) {
			report, err := readLaunchResult(p)
			if err == nil && report.FixtureChildPID != 0 {
				ready = true
				out["observed_child_pid"] = report.FixtureChildPID
				break
			}
			select {
			case err := <-done:
				return fmt.Errorf("controller ended before explicit stop: %v", err)
			default:
			}
			time.Sleep(50 * time.Millisecond)
		}
		if !ready {
			return errors.New("public child-ready report missing")
		}
		if e = stopFinite(p); e != nil {
			return e
		}
		out["explicit_stop_succeeded"] = true
	}
	select {
	case e = <-done:
		if e != nil {
			return e
		}
	case <-time.After(time.Duration(p.Seconds)*time.Second + 40*time.Second):
		return errors.New("controller execution deadline")
	}
	report, e := readLaunchResult(p)
	if e != nil {
		return e
	}
	sid, e := serviceSID(p)
	if e != nil {
		return e
	}
	if !report.ServiceSuccess || !report.Drained || !report.TokenAUDenyOnlyVerified || report.AccountSID != sid || report.WorkerExit == nil {
		return errors.New("measured protected service report incomplete")
	}
	expected := uint32(0)
	if p.Fixture == "cancel" {
		expected = 125
	}
	if *report.WorkerExit != expected || report.Cancelled != (p.Fixture == "cancel") {
		return errors.New("actual exit/cancellation mismatch")
	}
	out["protected_launcher_report"] = report
	return nil
}

func TestNamedPipeServerPIDIsKernelObserved(t *testing.T) {
	name := fmt.Sprintf(`\\.\pipe\CC-PR148-unit-%d-%d`, os.Getpid(), time.Now().UnixNano())
	h, _, e := kernel.NewProc("CreateNamedPipeW").Call(ptr(name), 1|0x40000000|0x80000, 4|2|8, 1, 4096, 4096, 0, 0)
	if h == ^uintptr(0) {
		t.Fatal(e)
	}
	defer syscall.CloseHandle(syscall.Handle(h))
	event, _, e := kernel.NewProc("CreateEventW").Call(0, 1, 0, 0)
	if event == 0 {
		t.Fatal(e)
	}
	defer syscall.CloseHandle(syscall.Handle(event))
	ov := syscall.Overlapped{HEvent: syscall.Handle(event)}
	r, _, e := kernel.NewProc("ConnectNamedPipe").Call(h, uintptr(unsafe.Pointer(&ov)))
	if r == 0 && e != syscall.Errno(997) {
		t.Fatal(e)
	}
	client, e := syscall.CreateFile(wide(name), 0x40000080, 0, nil, 3, 0, 0)
	if e != nil {
		t.Fatal(e)
	}
	defer syscall.CloseHandle(client)
	var server uint32
	r, _, e = kernel.NewProc("GetNamedPipeServerProcessId").Call(uintptr(client), uintptr(unsafe.Pointer(&server)))
	if r == 0 || server != uint32(os.Getpid()) {
		t.Fatal("kernel named-pipe identity mismatch", e)
	}
	w, e := syscall.WaitForSingleObject(ov.HEvent, 1000)
	if e != nil || w != 0 {
		t.Fatal("pipe connection incomplete", e)
	}
	// No registration token or even placeholder token is written in this test.
}
