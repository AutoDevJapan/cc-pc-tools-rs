package observer

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

var packagesByFamily = kernel.NewProc("GetPackagesByPackageFamily")
var packagePath = kernel.NewProc("GetPackagePathByFullName")
var processAUMID = kernel.NewProc("GetApplicationUserModelId")
var processPackage = kernel.NewProc("GetPackageFullName")
var processImage = kernel.NewProc("QueryFullProcessImageNameW")
var processSession = kernel.NewProc("ProcessIdToSessionId")
var wts = syscall.NewLazyDLL("wtsapi32.dll")
var wtsQuery = wts.NewProc("WTSQuerySessionInformationW")
var wtsFree = wts.NewProc("WTSFreeMemory")

type windowsScope struct {
	session     uint32
	sid         string
	fullPackage string
	executable  string
	target      string
}

// WindowsSource reads public registration metadata and narrowly scoped process
// and event information. None of its methods launches, stops or signs in an app.
// An instance is used by one finite engine invocation, never shared concurrently.
type WindowsSource struct {
	scope      windowsScope
	eventPID   uint32
	eventStart uint64
}

func utf16Pointer(v string) *uint16 { p, _ := syscall.UTF16PtrFromString(v); return p }

func tokenSID(h syscall.Handle) (string, error) {
	var token syscall.Token
	if err := syscall.OpenProcessToken(h, syscall.TOKEN_QUERY, &token); err != nil {
		return "", err
	}
	defer token.Close()
	u, err := token.GetTokenUser()
	if err != nil {
		return "", err
	}
	return u.User.Sid.String()
}

func sessionID(pid uint32) (uint32, error) {
	var session uint32
	ok, _, err := processSession.Call(uintptr(pid), uintptr(unsafe.Pointer(&session)))
	if ok == 0 {
		return 0, err
	}
	return session, nil
}

func activeSession(session uint32) bool {
	var p *uint32
	var size uint32
	ok, _, _ := wtsQuery.Call(0, uintptr(session), 8, uintptr(unsafe.Pointer(&p)), uintptr(unsafe.Pointer(&size)))
	if ok == 0 {
		return false
	}
	defer wtsFree.Call(uintptr(unsafe.Pointer(p)))
	return session != 0 && size >= 4 && p != nil && *p == 0 // WTSActive
}

func registeredScope(ctx context.Context) (windowsScope, error) {
	var scope windowsScope
	host, err := os.Hostname()
	if err != nil || !strings.EqualFold(host, "MinerPC") {
		return scope, errors.New("host_scope_unknown")
	}
	scope.session, err = sessionID(uint32(os.Getpid()))
	if err != nil || !activeSession(scope.session) {
		return scope, errors.New("interactive_session_unknown")
	}
	current, err := syscall.GetCurrentProcess()
	if err != nil {
		return scope, err
	}
	scope.sid, err = tokenSID(current)
	if err != nil {
		return scope, err
	}
	var count, chars uint32
	family := utf16Pointer(PackageFamily)
	code, _, _ := packagesByFamily.Call(uintptr(unsafe.Pointer(family)), uintptr(unsafe.Pointer(&count)), 0, uintptr(unsafe.Pointer(&chars)), 0)
	if code != 122 || count != 1 || chars == 0 || chars > 32768 {
		return scope, errors.New("registration_unknown")
	}
	names := make([]*uint16, count)
	buffer := make([]uint16, chars)
	code, _, _ = packagesByFamily.Call(uintptr(unsafe.Pointer(family)), uintptr(unsafe.Pointer(&count)), uintptr(unsafe.Pointer(&names[0])), uintptr(unsafe.Pointer(&chars)), uintptr(unsafe.Pointer(&buffer[0])))
	if code != 0 || count != 1 {
		return scope, errors.New("registration_changed")
	}
	// The API points into our own bounded buffer; do not follow an unbounded
	// returned pointer to decode the registered full package name.
	start := uintptr(unsafe.Pointer(names[0]))
	base := uintptr(unsafe.Pointer(&buffer[0]))
	end := base + uintptr(len(buffer))*2
	if start < base || start >= end || (start-base)%2 != 0 {
		return scope, errors.New("registration_invalid")
	}
	scope.fullPackage = syscall.UTF16ToString(buffer[(start-base)/2:])
	runtime.KeepAlive(buffer)
	var n uint32
	full := utf16Pointer(scope.fullPackage)
	code, _, _ = packagePath.Call(uintptr(unsafe.Pointer(full)), uintptr(unsafe.Pointer(&n)), 0)
	if code != 122 || n == 0 || n > 32768 {
		return scope, errors.New("package_path_unknown")
	}
	pathBuf := make([]uint16, n)
	code, _, _ = packagePath.Call(uintptr(unsafe.Pointer(full)), uintptr(unsafe.Pointer(&n)), uintptr(unsafe.Pointer(&pathBuf[0])))
	if code != 0 {
		return scope, errors.New("package_path_unknown")
	}
	root := syscall.UTF16ToString(pathBuf)
	if err := ctx.Err(); err != nil {
		return scope, err
	}
	f, err := os.Open(filepath.Join(root, "AppxManifest.xml"))
	if err != nil {
		return scope, errors.New("manifest_unreadable")
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxStateBytes+1))
	if err != nil || len(b) > maxStateBytes {
		return scope, errors.New("manifest_unreadable")
	}
	var manifest struct {
		Apps []struct {
			ID         string `xml:"Id,attr"`
			Executable string `xml:"Executable,attr"`
		} `xml:"Applications>Application"`
	}
	if xml.Unmarshal(b, &manifest) != nil {
		return scope, errors.New("manifest_unknown")
	}
	matches := 0
	for _, a := range manifest.Apps {
		if a.ID == "App" && a.Executable != "" {
			rel := filepath.Clean(a.Executable)
			if filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || !strings.EqualFold(filepath.Base(rel), "ChatGPT.exe") {
				return scope, errors.New("app_executable_unknown")
			}
			scope.executable = filepath.Join(root, rel)
			matches++
		}
	}
	if matches != 1 {
		return scope, errors.New("registered_app_unknown")
	}
	scope.target = Fingerprint(AUMID, scope.sid, strconv.FormatUint(uint64(scope.session), 10))
	return scope, nil
}

var errNotRegisteredApp = errors.New("not_registered_app")

func identityFailure(code uintptr) error {
	if code == 15700 || code == 15703 {
		return errNotRegisteredApp
	}
	return errors.New("process_identity_unknown")
}

func identityString(proc *syscall.LazyProc, h syscall.Handle) (string, error) {
	var n uint32
	code, _, _ := proc.Call(uintptr(h), uintptr(unsafe.Pointer(&n)), 0)
	if code != 122 || n == 0 || n > 32768 {
		return "", identityFailure(code)
	}
	b := make([]uint16, n)
	code, _, _ = proc.Call(uintptr(h), uintptr(unsafe.Pointer(&n)), uintptr(unsafe.Pointer(&b[0])))
	if code != 0 {
		return "", identityFailure(code)
	}
	return syscall.UTF16ToString(b), nil
}

func processStarted(h syscall.Handle) (uint64, error) {
	var created, exited, kernelTime, userTime syscall.Filetime
	if err := syscall.GetProcessTimes(h, &created, &exited, &kernelTime, &userTime); err != nil {
		return 0, err
	}
	return uint64(created.HighDateTime)<<32 | uint64(created.LowDateTime), nil
}

func instanceFingerprint(target string, pid uint32, started uint64) string {
	return Fingerprint(target, strconv.FormatUint(uint64(pid), 10), strconv.FormatUint(started, 10))
}

func (w *WindowsSource) ReadProcess(ctx context.Context) (ProcessSample, error) {
	p := ProcessSample{Presence: Unknown}
	scope, err := registeredScope(ctx)
	if err != nil {
		return p, err
	}
	if w.eventPID != 0 && w.scope.fullPackage != scope.fullPackage {
		return p, errors.New("registered_package_changed")
	}
	w.scope = scope
	p.Target = scope.target
	p.ScopeKnown = true
	p.InScope = true
	snapshot, err := syscall.CreateToolhelp32Snapshot(syscall.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return p, errors.New("process_snapshot_unknown")
	}
	defer syscall.CloseHandle(snapshot)
	var entry syscall.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	err = syscall.Process32First(snapshot, &entry)
	found := 0
	for entries := 0; err == nil; entries++ {
		if entries >= 32768 || ctx.Err() != nil {
			return p, errors.New("process_observation_timeout")
		}
		if strings.EqualFold(syscall.UTF16ToString(entry.ExeFile[:]), "ChatGPT.exe") {
			session, err := sessionID(entry.ProcessID)
			if err != nil {
				return p, errors.New("process_session_unknown")
			}
			if session == scope.session {
				h, err := syscall.OpenProcess(0x1000, false, entry.ProcessID)
				if err != nil {
					return p, errors.New("process_access_unknown")
				}
				match, instance, probeErr := matchProcess(h, entry.ProcessID, scope)
				syscall.CloseHandle(h)
				if probeErr != nil {
					return p, probeErr
				}
				if match {
					found++
					p.Instance = instance
				}
			}
		}
		err = syscall.Process32Next(snapshot, &entry)
	}
	if err != syscall.ERROR_NO_MORE_FILES {
		return p, errors.New("process_enumeration_unknown")
	}
	if found > 1 {
		return p, errors.New("process_generation_ambiguous")
	}
	p.Presence = Absent
	if found == 1 {
		p.Presence = Present
	}
	if found == 0 && w.eventPID != 0 {
		if !missingGeneration(w.eventPID, w.eventStart) {
			p.Presence = Unknown
			return p, errors.New("event_process_generation_unknown")
		}
		p.AbsenceFor = instanceFingerprint(scope.target, w.eventPID, w.eventStart)
	}
	return p, nil
}

func matchProcess(h syscall.Handle, pid uint32, scope windowsScope) (bool, string, error) {
	app, err := identityString(processAUMID, h)
	if errors.Is(err, errNotRegisteredApp) {
		return false, "", nil
	}
	if err != nil {
		return false, "", err
	}
	if app != AUMID {
		return false, "", nil
	}
	full, err := identityString(processPackage, h)
	if err != nil {
		return false, "", err
	}
	if full != scope.fullPackage {
		return false, "", nil
	}
	sid, err := tokenSID(h)
	if err != nil {
		return false, "", errors.New("process_user_unknown")
	}
	if sid != scope.sid {
		return false, "", nil
	}
	buf := make([]uint16, 32768)
	size := uint32(len(buf))
	ok, _, _ := processImage.Call(uintptr(h), 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)))
	if ok == 0 {
		return false, "", errors.New("process_image_unknown")
	}
	if !strings.EqualFold(syscall.UTF16ToString(buf[:size]), scope.executable) {
		return false, "", nil
	}
	created, err := processStarted(h)
	if err != nil {
		return false, "", errors.New("process_start_unknown")
	}
	return true, instanceFingerprint(scope.target, pid, created), nil
}

func missingGeneration(pid uint32, started uint64) bool {
	h, err := syscall.OpenProcess(0x1000, false, pid)
	if err == syscall.Errno(87) {
		return true
	} // No such PID; access denied is unknown.
	if err != nil {
		return false
	}
	defer syscall.CloseHandle(h)
	created, err := processStarted(h)
	if err != nil || created != started {
		return false
	} // PID reused.
	var exit uint32
	return syscall.GetExitCodeProcess(h, &exit) == nil && exit != 259
}

func parseHex(v string) (uint64, error) {
	if !strings.HasPrefix(strings.ToLower(v), "0x") {
		return 0, fmt.Errorf("unexpected_hex_schema")
	}
	return strconv.ParseUint(v[2:], 16, 64)
}
