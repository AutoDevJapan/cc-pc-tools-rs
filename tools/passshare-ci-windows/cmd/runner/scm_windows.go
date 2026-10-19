//go:build windows && amd64

package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	return manager(1)
}
func manager(access uintptr) (uintptr, error) {
	r, _, e := advapi.NewProc("OpenSCManagerW").Call(0, 0, access)
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
	r, _, e := advapi.NewProc("OpenServiceW").Call(scm, ptr(p.svc()), access)
	if r == 0 {
		return 0, e
	}
	return r, nil
}

// ensureService creates the fixed shared service on first use and otherwise
// re-points it at this job's helper and root. It never takes the identity from a
// running service, from live processes of its account, or from a previous job
// whose root was neither deleted nor collected.
func ensureService(p Plan) error {
	// One operation at a time across processes: read-check-reconfigure is not atomic.
	if e := prepareClaimLock(p); e != nil {
		return e
	}
	unlock, e := lockClaim(p)
	if e != nil {
		return e
	}
	defer unlock()
	scm, e := manager(3)
	if e != nil {
		return e
	}
	defer closeSC(scm)
	image := syscall.EscapeArg(filepath.Join(p.Root, "helper.exe")) + " service " + syscall.EscapeArg(p.Root)
	created := false
	s, _, e := advapi.NewProc("OpenServiceW").Call(scm, ptr(p.svc()), 0xf01ff)
	if s == 0 {
		if !errors.Is(e, syscall.Errno(1060)) {
			return fmt.Errorf("open shared service: %w", e)
		}
		created = true
		s, _, e = advapi.NewProc("CreateServiceW").Call(scm, ptr(p.svc()), ptr(p.svc()), 0xf01ff, 0x10, 3, 1, ptr(image), 0, 0, 0, ptr(`NT SERVICE\`+p.svc()), 0)
		if s == 0 {
			return fmt.Errorf("create shared virtual service: %w", e)
		}
		v := uint32(1)
		if r, _, e := advapi.NewProc("ChangeServiceConfig2W").Call(s, 5, uintptr(unsafe.Pointer(&v))); r == 0 {
			closeSC(s)
			return e
		}
	}
	defer closeSC(s)
	state, pid, _, e := queryService(s)
	if e != nil {
		return e
	}
	if state != 1 || pid != 0 {
		return errors.New("shared service identity is in use; not taken over")
	}
	if sid, err := serviceSID(p); err == nil {
		if n, _, err := ownedProcessCensus(sid); err != nil || n != 0 {
			return errors.Join(errors.New("processes of the shared account remain; identity not reused"), err)
		}
	} else {
		return err
	}
	if !created {
		image0, e := readImagePath(s)
		if e != nil {
			return e
		}
		prev, e := imageRoot(image0)
		if e != nil {
			return fmt.Errorf("existing shared service is not understood; not reused: %w", e)
		}
		collected, e := collectedRoot(p)
		if e != nil {
			return e
		}
		if e := previousRootSettled(prev, p.Root, collected); e != nil {
			return e
		}
	}
	// SERVICE_NO_CHANGE for type/start/error control, new image path only.
	if r, _, e := advapi.NewProc("ChangeServiceConfigW").Call(s, 0xffffffff, 0xffffffff, 0xffffffff, ptr(image), 0, 0, 0, 0, 0, 0); r == 0 {
		return fmt.Errorf("point shared service at this job: %w", e)
	}
	var sd uintptr
	r, _, e := convertDescriptor.Call(ptr("D:P(A;;GA;;;SY)(A;;GA;;;BA)(A;;0x35;;;"+p.ControllerSID+")"), 1, uintptr(unsafe.Pointer(&sd)), 0)
	if r == 0 {
		return e
	}
	defer kernel.NewProc("LocalFree").Call(sd)
	if r, _, e = advapi.NewProc("SetServiceObjectSecurity").Call(s, 4, sd); r == 0 {
		return e
	}
	return nil
}

// readImagePath returns the service image path from QUERY_SERVICE_CONFIGW,
// reading only inside the returned buffer and only up to its NUL.
func readImagePath(s uintptr) (string, error) {
	var need uint32
	advapi.NewProc("QueryServiceConfigW").Call(s, 0, 0, uintptr(unsafe.Pointer(&need)))
	if need < 64 || need > 65536 {
		return "", errors.New("service configuration size rejected")
	}
	buf := make([]byte, need)
	if r, _, e := advapi.NewProc("QueryServiceConfigW").Call(s, uintptr(unsafe.Pointer(&buf[0])), uintptr(need), uintptr(unsafe.Pointer(&need))); r == 0 {
		return "", e
	}
	// QUERY_SERVICE_CONFIGW (amd64): three DWORDs, 4 bytes padding, lpBinaryPathName.
	base := uint64(uintptr(unsafe.Pointer(&buf[0])))
	at := binary.LittleEndian.Uint64(buf[16:24])
	if at < base+64 || at >= base+uint64(len(buf)) || (at-base)%2 != 0 {
		return "", errors.New("service image path pointer outside the returned buffer")
	}
	var units []uint16
	for off := int(at - base); off+1 < len(buf); off += 2 {
		c := binary.LittleEndian.Uint16(buf[off : off+2])
		if c == 0 {
			return syscall.UTF16ToString(units), nil
		}
		units = append(units, c)
	}
	return "", errors.New("service image path is not NUL terminated inside the buffer")
}

// requireBoundTo refuses to act on the shared service for a job that no longer
// owns it (a delayed start/stop/collect for an earlier root).
func requireBoundTo(s uintptr, p Plan) error {
	image, e := readImagePath(s)
	if e != nil {
		return e
	}
	root, e := imageRoot(image)
	if e != nil {
		return e
	}
	if !strings.EqualFold(filepath.Clean(root), filepath.Clean(p.Root)) {
		return errors.New("the shared service now belongs to another job; no action taken")
	}
	return nil
}
func serviceSID(p Plan) (string, error) {
	var size, domain, typ uint32
	advapi.NewProc("LookupAccountNameW").Call(0, ptr(`NT SERVICE\`+p.svc()), 0, uintptr(unsafe.Pointer(&size)), 0, uintptr(unsafe.Pointer(&domain)), uintptr(unsafe.Pointer(&typ)))
	if size == 0 || size > 1024 || domain > 1024 {
		return "", errors.New("virtual service SID size invalid")
	}
	b := make([]byte, size)
	d := make([]uint16, domain+1)
	r, _, e := advapi.NewProc("LookupAccountNameW").Call(0, ptr(`NT SERVICE\`+p.svc()), uintptr(unsafe.Pointer(&b[0])), uintptr(unsafe.Pointer(&size)), uintptr(unsafe.Pointer(&d[0])), uintptr(unsafe.Pointer(&domain)), uintptr(unsafe.Pointer(&typ)))
	if r == 0 {
		return "", e
	}
	return (*syscall.SID)(unsafe.Pointer(&b[0])).String()
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
	unlock, e := lockClaim(p)
	if e != nil {
		return e
	}
	defer unlock()
	s, e := openService(p, 0x10|4|1)
	if e != nil {
		return e
	}
	defer closeSC(s)
	if e = requireBoundTo(s, p); e != nil {
		return e
	}
	// A collected job can never start again, even though the image path still names it.
	if done, e := collectedRoot(p); e != nil || strings.EqualFold(filepath.Clean(done), filepath.Clean(p.Root)) {
		return errors.Join(errors.New("this job was already collected; not started"), e)
	}
	r, _, e := advapi.NewProc("StartServiceW").Call(s, 0, 0)
	if r == 0 {
		return fmt.Errorf("finite service start: %w", e)
	}
	return nil
}
func stopFinite(p Plan) error {
	s, e := openService(p, 0x20|4|1)
	if e != nil {
		return e
	}
	defer closeSC(s)
	// Check-and-issue is one exclusive step; the wait below is not, so it never
	// blocks a collection or the next claim.
	state, e := func() (uint32, error) {
		unlock, e := lockClaim(p)
		if e != nil {
			return 0, e
		}
		defer unlock()
		if e = requireBoundTo(s, p); e != nil {
			return 0, e
		}
		state, _, _, e := queryService(s)
		if e != nil || state == 1 {
			return state, e
		}
		var status [7]uint32
		if state != 3 {
			r, _, callErr := advapi.NewProc("ControlService").Call(s, 1, uintptr(unsafe.Pointer(&status[0])))
			if r == 0 && callErr != syscall.Errno(1061) && callErr != syscall.Errno(1062) {
				return state, callErr
			}
		}
		return state, nil
	}()
	if e != nil {
		return e
	}
	if state == 1 {
		return nil
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
var statusMu sync.Mutex
var statusTerminal bool

func status(state, code uint32) {
	statusMu.Lock()
	defer statusMu.Unlock()
	if statusTerminal {
		return
	}
	if state == 1 {
		statusTerminal = true
	}
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
	h, _, _ := registerHandler.Call(ptr(activePlan.svc()), syscall.NewCallback(handler), 0)
	if h == 0 {
		return 0
	}
	statusHandle = h
	status(4, 0)
	launchReport = launchResult{Instance: activePlan.Instance, ServicePID: os.Getpid()}
	report, openErr := openLauncherReport(activePlan)
	if openErr != nil {
		status(1, 1)
		return 0
	}
	defer report.Close()
	activeReport = report
	e := runOwnedWorker(activePlan)
	if e == nil && activePlan.Fixture == "" {
		e = validateHookFiles(activePlan)
		launchReport.HooksVerified = e == nil
	}
	launchReport.ServiceSuccess = e == nil
	jsonErr := publishLauncherReport()
	e = errors.Join(e, jsonErr)
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
	table := [2]serviceTable{{Name: wide(p.svc()), Main: syscall.NewCallback(serviceMain)}, {}}
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
		e := runService(p)
		if e != nil && p.Fixture != "" {
			_ = saveNew(filepath.Join(p.runtime(), "startup-error.json"), map[string]any{"error": e.Error()})
		}
		return e
	case "worker":
		e := worker(p)
		if e != nil && p.Fixture != "" {
			_ = saveNew(filepath.Join(p.runtime(), "worker-error.json"), map[string]any{"error": e.Error()})
		}
		return e
	case "fixture-child":
		time.Sleep(10 * time.Minute)
		return nil
	case "execute":
		return execute(p)
	case "stop":
		return stopFinite(p)
	case "collect":
		return collect(p)
	case "collect-unstarted":
		return collectUnstarted(p)
	case "hook-start", "hook-completed", "verify-source":
		return hook(p, mode)
	case "audit":
		return audit(p)
	case "status":
		return statusReport(p)
	}
	return fmt.Errorf("unknown finite mode %q", mode)
}

func envFor(p Plan) []string {
	return []string{`SystemRoot=C:\Windows`, `WINDIR=C:\Windows`, `SystemDrive=C:`, `ProgramFiles=C:\Program Files`, `ProgramFiles(x86)=C:\Program Files (x86)`, `ProgramW6432=C:\Program Files`, `COMSPEC=C:\Windows\System32\cmd.exe`, `PATHEXT=.COM;.EXE;.BAT;.CMD`, "PATH=" + nodeHome(p) + ";" + filepath.Join(p.runtime(), "cargo", "bin") + ";" + filepath.Join(p.runtime(), "sdk", "bootstrap", "bin") + `;C:\Windows\System32;C:\Program Files\Git\cmd;C:\Program Files\Git\bin;C:\Program Files\PowerShell\7`, "TEMP=" + filepath.Join(p.runtime(), "tmp"), "TMP=" + filepath.Join(p.runtime(), "tmp"), "HOME=" + filepath.Join(p.runtime(), "home"), "USERPROFILE=" + filepath.Join(p.runtime(), "home"), "LOCALAPPDATA=" + filepath.Join(p.runtime(), "home", "AppData", "Local"), "APPDATA=" + filepath.Join(p.runtime(), "home", "AppData", "Roaming"), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + filepath.Join(p.runtime(), "home", ".gitconfig"), "CI=true", "PSW_HELPER=" + filepath.Join(p.Root, "helper.exe"), "PSW_ROOT=" + p.Root, "RUSTUP_TOOLCHAIN=stable", "npm_config_userconfig=" + filepath.Join(p.runtime(), "home", ".npmrc"), "npm_config_cache=" + filepath.Join(p.runtime(), "npm-cache"), "PNPM_HOME=" + nodeHome(p), "ACTIONS_RUNNER_HOOK_JOB_STARTED=" + filepath.Join(p.Root, "hook-start.ps1"), "ACTIONS_RUNNER_HOOK_JOB_COMPLETED=" + filepath.Join(p.Root, "hook-completed.ps1"), "CARGO_HOME=" + filepath.Join(p.runtime(), "cargo"), "RUSTUP_HOME=" + filepath.Join(p.runtime(), "rustup"), "CARGO_TARGET_DIR=" + p.target(), `RUSTC_WRAPPER=D:\CC\Tools\sccache\sccache.exe`, "SCCACHE_DIR=" + filepath.Join(p.runtime(), "sccache"), `SCCACHE_CACHE_SIZE=30G`, `SCCACHE_IDLE_TIMEOUT=0`, fmt.Sprintf("SCCACHE_SERVER_PORT=%d", cachePort(p)), `RUSTUP_NO_UPDATE_CHECK=1`, `GIT_TERMINAL_PROMPT=0`}
}
