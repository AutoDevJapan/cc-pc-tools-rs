//go:build windows

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

type serviceFixtureIdentity struct {
	PID        int
	Created    syscall.Filetime
	Executable string
	Directory  string
}

func serviceFixtureFile(root, name string) string { return filepath.Join(root, name) }

func writeServiceFixture(root, name string, value any) {
	body, err := json.Marshal(value)
	if err != nil || os.WriteFile(serviceFixtureFile(root, name), body, 0600) != nil {
		os.Exit(91)
	}
}

func waitServiceFixture(root, name string, limit time.Duration) bool {
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(serviceFixtureFile(root, name)); err == nil {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// Only the test binary understands this argument; production service IDs and
// installed task definitions are never changed to admit a fixture.
func TestServiceProcessFixture(t *testing.T) {
	if len(os.Args) < 5 || os.Args[len(os.Args)-3] != "--service-process-fixture" {
		return
	}
	mode, root := os.Args[len(os.Args)-2], os.Args[len(os.Args)-1]
	executable, err := os.Executable()
	if err != nil {
		os.Exit(92)
	}
	process, err := syscall.GetCurrentProcess()
	if err != nil {
		os.Exit(93)
	}
	var created, exited, kernel, user syscall.Filetime
	if syscall.GetProcessTimes(process, &created, &exited, &kernel, &user) != nil {
		os.Exit(94)
	}
	writeServiceFixture(root, mode+"-identity.json", serviceFixtureIdentity{os.Getpid(), created, executable, root})
	if mode == "descendant" {
		fmt.Fprint(os.Stdout, "fixture stdout ready\n")
		fmt.Fprint(os.Stderr, "fixture stderr ready\n")
		writeServiceFixture(root, "descendant-ready", true)
		if !waitServiceFixture(root, "stop-descendant", 30*time.Second) {
			os.Exit(95)
		}
		os.Exit(0)
	}
	if mode == "live" {
		writeServiceFixture(root, "service-ready", true)
		if !waitServiceFixture(root, "stop-service", 30*time.Second) {
			os.Exit(96)
		}
		os.Exit(0)
	}
	descendant := exec.Command(executable, "-test.run=^TestServiceProcessFixture$", "--", "--service-process-fixture", "descendant", root)
	descendant.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	descendant.Stdout, descendant.Stderr = os.Stdout, os.Stderr
	if descendant.Start() != nil {
		os.Exit(97)
	}
	if !waitServiceFixture(root, "descendant-ready", 5*time.Second) {
		os.Exit(98)
	}
	writeServiceFixture(root, "service-exiting", true)
	if !waitServiceFixture(root, "exit-main", 10*time.Second) {
		os.Exit(99)
	}
	if mode == "failure" {
		os.Exit(7)
	}
	os.Exit(0)
}

func serviceFixtureCommand(t *testing.T, mode string) (*exec.Cmd, string) {
	t.Helper()
	root := t.TempDir()
	// The operator can retain identities inside a safe-delete-owned root until
	// plan/delete/finish. Ordinary CI uses Go's per-test temporary directory.
	if owned := os.Getenv("CC_SERVICE_FIXTURE_ROOT"); owned != "" {
		var err error
		root, err = os.MkdirTemp(owned, "service-fixture-")
		if err != nil {
			t.Fatal(err)
		}
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(serviceFixtureFile(root, "owner.json"), []byte(fmt.Sprintf("{\"test\":%q,\"owner_pid\":%d}", t.Name(), os.Getpid())), 0600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(executable, "-test.run=^TestServiceProcessFixture$", "--", "--service-process-fixture", mode, root)
	return command, root
}

// Retain the verified process handle rather than reopening a PID at cleanup.
func retainServiceFixture(t *testing.T, root, mode string) syscall.Handle {
	t.Helper()
	if !waitServiceFixture(root, mode+"-identity.json", 5*time.Second) {
		t.Fatal("fixture did not record identity")
	}
	body, err := os.ReadFile(serviceFixtureFile(root, mode+"-identity.json"))
	var identity serviceFixtureIdentity
	if err != nil || json.Unmarshal(body, &identity) != nil || identity.Directory != root {
		t.Fatal("fixture ownership record unavailable")
	}
	handle, err := syscall.OpenProcess(0x1000|0x100000|0x1, false, uint32(identity.PID))
	if err != nil {
		t.Fatal("fixture process unavailable", err)
	}
	var created, exited, kernel, user syscall.Filetime
	if syscall.GetProcessTimes(handle, &created, &exited, &kernel, &user) != nil || created != identity.Created {
		syscall.CloseHandle(handle)
		t.Fatal("fixture creation time mismatch")
	}
	buffer := make([]uint16, 32768)
	length := uint32(len(buffer))
	ok, _, _ := kernel32.NewProc("QueryFullProcessImageNameW").Call(uintptr(handle), 0, uintptr(unsafe.Pointer(&buffer[0])), uintptr(unsafe.Pointer(&length)))
	executable, exeErr := os.Executable()
	if ok == 0 || exeErr != nil || syscall.UTF16ToString(buffer[:length]) != executable || identity.Executable != executable {
		syscall.CloseHandle(handle)
		t.Fatal("fixture executable mismatch")
	}
	t.Logf("owned fixture identity: %s", body)
	t.Cleanup(func() {
		defer syscall.CloseHandle(handle)
		stop := "stop-" + mode
		if mode == "live" {
			stop = "stop-service"
		} else if mode == "success" || mode == "failure" {
			stop = "exit-main"
		}
		if err := os.WriteFile(serviceFixtureFile(root, stop), []byte("stop"), 0600); err != nil {
			t.Error("fixture stop request failed", err)
		}
		state, err := syscall.WaitForSingleObject(handle, 3000)
		if err != nil || state != syscall.WAIT_OBJECT_0 {
			// This is the retained, creation-time/executable-checked fixture
			// handle. No application lookup or process-tree termination.
			_ = syscall.TerminateProcess(handle, 99)
			_, _ = syscall.WaitForSingleObject(handle, 3000)
			t.Error("fixture needed forced cleanup")
		}
	})
	return handle
}

func TestServiceProcessReturnsAfterMainExitWithInheritedOutput(t *testing.T) {
	for _, mode := range []string{"success", "failure"} {
		t.Run(mode, func(t *testing.T) {
			command, root := serviceFixtureCommand(t, mode)
			result := make(chan error, 1)
			go func() { result <- runServiceProcess(command) }()
			if !waitServiceFixture(root, "service-exiting", 5*time.Second) {
				t.Fatal("fixture did not reach service exit")
			}
			descendant := retainServiceFixture(t, root, "descendant")
			main := retainServiceFixture(t, root, mode)
			if err := os.WriteFile(serviceFixtureFile(root, "exit-main"), []byte("exit"), 0600); err != nil {
				t.Fatal(err)
			}
			if state, err := syscall.WaitForSingleObject(main, 3000); err != nil || state != syscall.WAIT_OBJECT_0 {
				t.Fatal("fixture main did not exit", err)
			}
			select {
			case err := <-result:
				if (mode == "success" && err != nil) || (mode == "failure" && (err == nil || err.Error() != "service_process_failed")) {
					t.Fatal("service exit classified incorrectly", err)
				}
			case <-time.After(time.Second):
				t.Error("launcher still waits for descendant output after main exit")
				// Release only the fixture, then drain the runner before returning.
				_ = os.WriteFile(serviceFixtureFile(root, "stop-descendant"), []byte("stop"), 0600)
				select {
				case <-result:
				case <-time.After(5 * time.Second):
					t.Error("fixture runner did not drain")
				}
				return
			}
			if state, err := syscall.WaitForSingleObject(descendant, 0); err != nil || state != syscall.WAIT_TIMEOUT {
				t.Fatal("launcher terminated the descendant", err)
			}
		})
	}
}

func TestServiceProcessRetainsLiveMain(t *testing.T) {
	command, root := serviceFixtureCommand(t, "live")
	result := make(chan error, 1)
	go func() { result <- runServiceProcess(command) }()
	if !waitServiceFixture(root, "service-ready", 5*time.Second) {
		t.Fatal("service did not become ready")
	}
	main := retainServiceFixture(t, root, "live")
	select {
	case err := <-result:
		t.Fatal("launcher returned while main was live", err)
	case <-time.After(250 * time.Millisecond):
	}
	if state, err := syscall.WaitForSingleObject(main, 0); err != nil || state != syscall.WAIT_TIMEOUT {
		t.Fatal("live service was stopped", err)
	}
	if err := os.WriteFile(serviceFixtureFile(root, "stop-service"), []byte("stop"), 0600); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if err != nil {
			t.Fatal("normal service exit failed", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("launcher did not return after service exit")
	}
}

func TestServiceProcessReportsStartFailure(t *testing.T) {
	command := exec.Command(filepath.Join(t.TempDir(), "absent.exe"))
	if err := runServiceProcess(command); err != nil && err.Error() == "service_process_failed" {
		return
	}
	t.Fatal("start failure was not reported")
}
