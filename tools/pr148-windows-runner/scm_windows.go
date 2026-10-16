//go:build windows && amd64

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

func wide(s string) *uint16 {
	p, e := syscall.UTF16PtrFromString(s)
	if e != nil {
		panic("invalid internal UTF16")
	}
	return p
}

var publicStrings sync.Map // API pointers remain alive; contains public paths/identities only.
func ptr(s string) uintptr {
	v, _ := publicStrings.LoadOrStore(s, wide(s))
	return uintptr(unsafe.Pointer(v.(*uint16)))
}
func closeSC(h uintptr) { advapi.NewProc("CloseServiceHandle").Call(h) }
func openSC() (uintptr, error) {
	r, _, e := advapi.NewProc("OpenSCManagerW").Call(0, 0, 3)
	if r == 0 {
		return 0, e
	}
	return r, nil
}
func openService(p Plan, access uintptr) (uintptr, error) {
	scm, e := openSC()
	if e != nil {
		return 0, e
	}
	defer closeSC(scm)
	r, _, e := advapi.NewProc("OpenServiceW").Call(scm, ptr(p.name()), access)
	if r == 0 {
		return 0, e
	}
	return r, nil
}
func installService(p Plan) error {
	scm, e := openSC()
	if e != nil {
		return e
	}
	defer closeSC(scm)
	// CREATE_NEW semantics: never reconfigure or replace an existing service.
	image := syscall.EscapeArg(filepath.Join(p.Root, "helper.exe")) + " service " + syscall.EscapeArg(p.Root)
	s, _, e := advapi.NewProc("CreateServiceW").Call(scm, ptr(p.name()), ptr(p.name()), 0xf01ff, 0x10, 3, 1, ptr(image), 0, 0, 0, ptr(`NT SERVICE\`+p.name()), 0)
	if s == 0 {
		return fmt.Errorf("create finite virtual service: %w", e)
	}
	defer closeSC(s)
	v := uint32(1)
	r, _, e := advapi.NewProc("ChangeServiceConfig2W").Call(s, 5, uintptr(unsafe.Pointer(&v)))
	if r == 0 {
		return e
	}
	return nil
}
func serviceSID(p Plan) (string, error) {
	var size, domain, typ uint32
	advapi.NewProc("LookupAccountNameW").Call(0, ptr(`NT SERVICE\`+p.name()), 0, uintptr(unsafe.Pointer(&size)), 0, uintptr(unsafe.Pointer(&domain)), uintptr(unsafe.Pointer(&typ)))
	if size == 0 || size > 1024 || domain > 1024 {
		return "", errors.New("virtual service SID size invalid")
	}
	b := make([]byte, size)
	d := make([]uint16, domain+1)
	r, _, e := advapi.NewProc("LookupAccountNameW").Call(0, ptr(`NT SERVICE\`+p.name()), uintptr(unsafe.Pointer(&b[0])), uintptr(unsafe.Pointer(&size)), uintptr(unsafe.Pointer(&d[0])), uintptr(unsafe.Pointer(&domain)), uintptr(unsafe.Pointer(&typ)))
	if r == 0 {
		return "", e
	}
	var txt *uint16
	r, _, e = sidToString.Call(uintptr(unsafe.Pointer(&b[0])), uintptr(unsafe.Pointer(&txt)))
	if r == 0 {
		return "", e
	}
	defer kernel.NewProc("LocalFree").Call(uintptr(unsafe.Pointer(txt)))
	return syscall.UTF16ToString(unsafe.Slice(txt, 256)), nil
}
func queryService(s uintptr) (state, pid, code uint32, err error) {
	var v [9]uint32
	var n uint32
	r, _, e := advapi.NewProc("QueryServiceStatusEx").Call(s, 0, uintptr(unsafe.Pointer(&v[0])), unsafe.Sizeof(v), uintptr(unsafe.Pointer(&n)))
	if r == 0 {
		return 0, 0, 0, e
	}
	return v[1], v[7], v[3], nil
}
func startFinite(p Plan) error {
	s, e := openService(p, 0x10|4)
	if e != nil {
		return e
	}
	defer closeSC(s)
	r, _, e := advapi.NewProc("StartServiceW").Call(s, 0, 0)
	if r == 0 {
		return fmt.Errorf("finite service start: %w", e)
	}
	return nil
}
func stopFinite(p Plan) error {
	s, e := openService(p, 0x20|4)
	if e != nil {
		return e
	}
	defer closeSC(s)
	state, _, _, e := queryService(s)
	if e != nil {
		return e
	}
	if state == 1 {
		return nil
	}
	var status [7]uint32
	r, _, e := advapi.NewProc("ControlService").Call(s, 1, uintptr(unsafe.Pointer(&status[0])))
	if r == 0 {
		return e
	}
	until := time.Now().Add(40 * time.Second)
	for time.Now().Before(until) {
		state, _, _, e = queryService(s)
		if e != nil {
			return e
		}
		if state == 1 {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return errors.New("service stop deadline; retain resource")
}

type serviceTable struct {
	Name *uint16
	Main uintptr
}
type serviceStatus struct{ Type, State, Accepts, ExitCode, Specific, Checkpoint, Wait uint32 }

var activePlan Plan
var stopService = make(chan struct{})
var stopOnce sync.Once
var statusHandle uintptr

func status(state, code uint32) {
	s := serviceStatus{Type: 0x10, State: state, ExitCode: code}
	if state == 4 {
		s.Accepts = 1 | 4
	}
	if state == 3 {
		s.Wait = 35000
		s.Checkpoint = 1
	}
	setStatus.Call(statusHandle, uintptr(unsafe.Pointer(&s)))
}
func handler(control, event, data, context uintptr) uintptr {
	if control == 1 || control == 5 {
		status(3, 0)
		stopOnce.Do(func() { close(stopService) })
	}
	return 0
}
func serviceMain(argc, argv uintptr) uintptr {
	h, _, _ := registerHandler.Call(ptr(activePlan.name()), syscall.NewCallback(handler), 0)
	if h == 0 {
		return 0
	}
	statusHandle = h
	status(4, 0)
	e := runOwnedWorker(activePlan)
	code := uint32(0)
	if e != nil {
		code = 1
	}
	// Error text from untrusted job output is never included in this receipt.
	_ = saveNew(filepath.Join(activePlan.runtime(), "service.json"), map[string]any{"ok": e == nil, "service_exit": code, "official_ci_claimed": false})
	status(1, code)
	return 0
}
func runService(p Plan) error {
	activePlan = p
	if e := protectLauncher(); e != nil {
		return e
	}
	defer syscall.CloseHandle(launcherToken)
	// Verify the account is exactly this newly created virtual service user.
	want, e := serviceSID(p)
	if e != nil {
		return e
	}
	got, e := tokenUserSID(launcherToken)
	if e != nil || got != want {
		return errors.New("unexpected service account")
	}
	table := [2]serviceTable{{Name: wide(p.name()), Main: syscall.NewCallback(serviceMain)}, {}}
	r, _, e := startDispatcher.Call(uintptr(unsafe.Pointer(&table[0])))
	if r == 0 {
		return e
	}
	return nil
}

func platform(mode string, p Plan, args []string) error {
	switch mode {
	case "prepare":
		if len(args) > 1 {
			return errors.New("prepare accepts one official archive")
		}
		archive := ""
		if len(args) == 1 {
			archive = args[0]
		}
		return prepare(p, archive)
	case "service":
		return runService(p)
	case "worker":
		return worker(p)
	case "fixture-child":
		time.Sleep(10 * time.Minute)
		return nil
	case "execute":
		return execute(p)
	case "stop":
		return stopFinite(p)
	case "collect":
		return collect(p)
	case "hook-start", "hook-completed", "verify-source":
		return hook(p, mode)
	case "audit":
		return audit(p)
	}
	return fmt.Errorf("unknown finite mode %q", mode)
}

func envFor(p Plan) []string {
	return []string{`SystemRoot=C:\Windows`, `WINDIR=C:\Windows`, `SystemDrive=C:`, `COMSPEC=C:\Windows\System32\cmd.exe`, `PATHEXT=.COM;.EXE;.BAT;.CMD`, `PATH=C:\Windows\System32;C:\Program Files\Git\cmd;C:\Program Files\PowerShell\7`, "TEMP=" + filepath.Join(p.runtime(), "tmp"), "TMP=" + filepath.Join(p.runtime(), "tmp"), "HOME=" + filepath.Join(p.runtime(), "home"), "USERPROFILE=" + filepath.Join(p.runtime(), "home"), "LOCALAPPDATA=" + filepath.Join(p.runtime(), "home", "AppData", "Local"), "APPDATA=" + filepath.Join(p.runtime(), "home", "AppData", "Roaming"), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=NUL", "CI=true", "ACTIONS_RUNNER_HOOK_JOB_STARTED=" + filepath.Join(p.Root, "hook-start.ps1"), "ACTIONS_RUNNER_HOOK_JOB_COMPLETED=" + filepath.Join(p.Root, "hook-completed.ps1"), "CARGO_HOME=" + filepath.Join(p.runtime(), "cargo"), "RUSTUP_HOME=" + filepath.Join(p.runtime(), "rustup"), "CARGO_TARGET_DIR=" + filepath.Join(p.runtime(), "target"), `RUSTC_WRAPPER=D:\CC\Tools\sccache\sccache.exe`, `SCCACHE_DIR=D:\CC\BuildCache\sccache`, `SCCACHE_CACHE_SIZE=30G`}
}

var _ = os.ErrNotExist
