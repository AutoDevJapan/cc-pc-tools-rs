package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"
)

type iosCompilerJobBasic struct {
	User, Kernel         int64
	Flags                uint32
	Min, Max             uintptr
	Active               uint32
	Affinity             uintptr
	Priority, Scheduling uint32
}
type iosCompilerJobLimits struct {
	Basic                                                      iosCompilerJobBasic
	IO                                                         [6]uint64
	ProcessMemory, JobMemory, PeakProcessMemory, PeakJobMemory uintptr
}
type iosCompilerJobAccounting struct {
	User, Kernel, PeriodUser, PeriodKernel int64
	Faults, Total, Active, Terminated      uint32
}
type iosCompilerStartupEx struct {
	syscall.StartupInfo
	Attributes uintptr
}

var iosCompilerKernel = syscall.NewLazyDLL("kernel32.dll")

const iosCompilerUserConfigFlag = "--ios-compiler-user-config-location"

type iosCompilerUserConfigLocation struct {
	Domain string `json:"domain"`
	Path   string `json:"path"`
}

func iosCompilerUserConfigFlagPresent(args []string) bool {
	for _, arg := range args {
		if arg == iosCompilerUserConfigFlag || strings.HasPrefix(arg, iosCompilerUserConfigFlag+"=") {
			return true
		}
	}
	return false
}

// This fixed helper only reads the current user's configured folder location.
// It does not read credentials, qualify resources, create files or run an API.
func runIOSCompilerUserConfigCLI(args []string, out io.Writer, location func() (string, error)) int {
	if len(args) != 1 || args[0] != iosCompilerUserConfigFlag {
		return 2
	}
	path, err := location()
	if err != nil || !iosCompilerUserConfigPath(path) {
		return 1
	}
	if json.NewEncoder(iosCompilerExactWriter{out}).Encode(iosCompilerUserConfigLocation{"ios-compiler-current-user-config-v1", path}) != nil {
		return 1
	}
	return 0
}

func iosCompilerUserConfigPath(path string) bool {
	return filepath.IsAbs(path) && !strings.ContainsAny(path, "\x00\r\n")
}

func iosCompilerReadUserConfig(raw []byte) (string, error) {
	var location iosCompilerUserConfigLocation
	if len(raw) > 64<<10 || !skillsStrict(raw, &location) || location.Domain != "ios-compiler-current-user-config-v1" || !iosCompilerUserConfigPath(location.Path) {
		return "", errIOSCompiler
	}
	return location.Path, nil
}

func iosCompilerRoamingCommand(ctx context.Context, exe string, args []string, observe func(uint32, uint32)) (string, error) {
	// Use the unaugmented projection here: the helper must never recursively
	// resolve its own APPDATA. Its entire synchronous API lives in the owned job.
	raw, err := iosCompilerCommandWithEnvironment(ctx, exe, args, observe, func() ([]string, error) {
		return official3894Env(), nil
	})
	defer clear(raw)
	if err != nil {
		return "", errIOSCompiler
	}
	return iosCompilerReadUserConfig(raw)
}

func iosCompilerRoamingIsolated(ctx context.Context) (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", errIOSCompiler
	}
	// Retain the ordinary executable handle through child creation/collection.
	// Windows keeps this named helper from being replaced during the invocation.
	pin, err := os.Open(exe)
	if err != nil {
		return "", errIOSCompiler
	}
	defer pin.Close()
	return iosCompilerRoamingCommand(ctx, exe, []string{iosCompilerUserConfigFlag}, nil)
}

// JOB_LIST attaches the process and future gate/gh children atomically before
// execution. Killing only CommandContext's immediate gate is insufficient.
// This is limited to fixed metadata GETs; it starts no artifact or GUI.
func iosCompilerCommand(ctx context.Context, path string, args ...string) ([]byte, error) {
	return iosCompilerCommandObserved(ctx, path, args, nil)
}

// Some native tool hosts intentionally omit APPDATA. Letting gh silently use
// a different config directory then loses the current user's normal account.
// Recover only this missing location from Windows, never credentials or a
// caller-supplied account/cache. Existing nonempty environment values survive.
func iosCompilerEnvironment(lookup func(string) (string, bool), roaming func() (string, error)) ([]string, error) {
	env := official3894EnvWith(lookup)
	if value, ok := lookup("APPDATA"); ok && value != "" {
		return env, nil
	}
	value, err := roaming()
	if err != nil || !iosCompilerUserConfigPath(value) {
		return nil, errIOSCompiler
	}
	for i, field := range env {
		if strings.HasPrefix(field, "APPDATA=") {
			env[i] = "APPDATA=" + value
			return env, nil
		}
	}
	return append(env, "APPDATA="+value), nil
}

func iosCompilerRoamingData() (string, error) {
	id := syscall.GUID{Data1: 0x3eb685db, Data2: 0x65f9, Data3: 0x4cf6, Data4: [8]byte{0xa0, 0x3a, 0xe3, 0xef, 0x65, 0x72, 0x9f, 0x3d}}
	var path *uint16
	// Resolve the configured current-user path without probing a redirected
	// network folder. The metadata consumer performs its own bounded I/O.
	hr, _, _ := syscall.NewLazyDLL("shell32.dll").NewProc("SHGetKnownFolderPath").Call(uintptr(unsafe.Pointer(&id)), 0x4000, 0, uintptr(unsafe.Pointer(&path)))
	if path != nil {
		defer syscall.NewLazyDLL("ole32.dll").NewProc("CoTaskMemFree").Call(uintptr(unsafe.Pointer(path)))
	}
	if hr != 0 || path == nil {
		return "", errIOSCompiler
	}
	chars := make([]uint16, 0, 256)
	for i := 0; i < 32768; i++ {
		ch := *(*uint16)(unsafe.Add(unsafe.Pointer(path), i*2))
		if ch == 0 {
			return syscall.UTF16ToString(chars), nil
		}
		chars = append(chars, ch)
	}
	return "", errIOSCompiler
}

func iosCompilerCommandObserved(ctx context.Context, path string, args []string, observe func(uint32, uint32)) ([]byte, error) {
	return iosCompilerCommandWithEnvironment(ctx, path, args, observe, func() ([]string, error) {
		return iosCompilerEnvironment(os.LookupEnv, func() (string, error) { return iosCompilerRoamingIsolated(ctx) })
	})
}

func iosCompilerCommandWithEnvironment(ctx context.Context, path string, args []string, observe func(uint32, uint32), environment func() ([]string, error)) ([]byte, error) {
	if ctx.Err() != nil || !filepath.IsAbs(path) {
		return nil, errIOSCompiler
	}
	j, _, _ := iosCompilerKernel.NewProc("CreateJobObjectW").Call(0, 0)
	if j == 0 {
		return nil, errIOSCompiler
	}
	defer syscall.CloseHandle(syscall.Handle(j))
	limits := iosCompilerJobLimits{}
	limits.Basic.Flags = 0x2000
	ok, _, _ := iosCompilerKernel.NewProc("SetInformationJobObject").Call(j, 9, uintptr(unsafe.Pointer(&limits)), unsafe.Sizeof(limits))
	if ok == 0 {
		return nil, errIOSCompiler
	}
	r, w, err := os.Pipe()
	if err != nil {
		return nil, errIOSCompiler
	}
	defer r.Close()
	defer w.Close()
	null, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return nil, errIOSCompiler
	}
	defer null.Close()
	handles := []syscall.Handle{syscall.Handle(null.Fd()), syscall.Handle(w.Fd())}
	for _, h := range handles {
		if syscall.SetHandleInformation(h, 1, 1) != nil {
			return nil, errIOSCompiler
		}
	}
	init := iosCompilerKernel.NewProc("InitializeProcThreadAttributeList")
	var size uintptr
	init.Call(0, 2, 0, uintptr(unsafe.Pointer(&size)))
	if size == 0 || size > 64<<10 {
		return nil, errIOSCompiler
	}
	attributes := make([]byte, int(size))
	attr := uintptr(unsafe.Pointer(&attributes[0]))
	ok, _, _ = init.Call(attr, 2, 0, uintptr(unsafe.Pointer(&size)))
	if ok == 0 {
		return nil, errIOSCompiler
	}
	defer func() {
		iosCompilerKernel.NewProc("DeleteProcThreadAttributeList").Call(attr)
		runtime.KeepAlive(attributes)
	}()
	jobs := []syscall.Handle{syscall.Handle(j)}
	var pins runtime.Pinner
	pins.Pin(&handles[0])
	pins.Pin(&jobs[0])
	defer pins.Unpin()
	update := iosCompilerKernel.NewProc("UpdateProcThreadAttribute")
	ok, _, _ = update.Call(attr, 0, 0x20002, uintptr(unsafe.Pointer(&handles[0])), uintptr(len(handles))*unsafe.Sizeof(handles[0]), 0, 0)
	if ok == 0 {
		return nil, errIOSCompiler
	}
	ok, _, _ = update.Call(attr, 0, 0x2000D, uintptr(unsafe.Pointer(&jobs[0])), unsafe.Sizeof(jobs[0]), 0, 0)
	if ok == 0 {
		return nil, errIOSCompiler
	}
	application, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, errIOSCompiler
	}
	argv := append([]string{path}, args...)
	for i := range argv {
		argv[i] = syscall.EscapeArg(argv[i])
	}
	line, err := syscall.UTF16FromString(strings.Join(argv, " "))
	if err != nil {
		return nil, errIOSCompiler
	}
	dir, err := syscall.UTF16PtrFromString(filepath.Dir(path))
	if err != nil {
		return nil, errIOSCompiler
	}
	env, err := environment()
	if err != nil || ctx.Err() != nil {
		return nil, errIOSCompiler
	}
	sort.Slice(env, func(i, j int) bool { return strings.ToUpper(env[i]) < strings.ToUpper(env[j]) })
	for _, v := range env {
		if strings.ContainsRune(v, 0) {
			return nil, errIOSCompiler
		}
	}
	block := utf16.Encode([]rune(strings.Join(env, "\x00") + "\x00\x00"))
	defer clear(block)
	si := iosCompilerStartupEx{StartupInfo: syscall.StartupInfo{Cb: uint32(unsafe.Sizeof(iosCompilerStartupEx{})), Flags: 0x100, StdInput: handles[0], StdOutput: handles[1], StdErr: handles[0]}, Attributes: attr}
	var pi syscall.ProcessInformation
	if ctx.Err() != nil {
		return nil, errIOSCompiler
	}
	ok, _, _ = iosCompilerKernel.NewProc("CreateProcessW").Call(uintptr(unsafe.Pointer(application)), uintptr(unsafe.Pointer(&line[0])), 0, 0, 1, 0x08000000|0x80000|0x400, uintptr(unsafe.Pointer(&block[0])), uintptr(unsafe.Pointer(dir)), uintptr(unsafe.Pointer(&si)), uintptr(unsafe.Pointer(&pi)))
	runtime.KeepAlive(handles)
	runtime.KeepAlive(jobs)
	runtime.KeepAlive(block)
	runtime.KeepAlive(attributes)
	if ok == 0 {
		return nil, errIOSCompiler
	}
	defer syscall.CloseHandle(pi.Process)
	defer syscall.CloseHandle(pi.Thread)
	w.Close()
	type captured struct {
		raw []byte
		err error
	}
	done := make(chan captured, 1)
	go func() {
		raw := make([]byte, 64<<10+1)
		n := 0
		var e error
		for n < len(raw) {
			var got int
			got, e = r.Read(raw[n:])
			n += got
			if e != nil {
				break
			}
			if got == 0 {
				e = errIOSCompiler
				break
			}
		}
		if n > 64<<10 {
			e = errIOSCompiler
		}
		done <- captured{raw[:n], e}
	}()
	failed := false
	for {
		state, e := syscall.WaitForSingleObject(pi.Process, 10)
		if e != nil {
			failed = true
			break
		}
		if state == 0 {
			break
		}
		if ctx.Err() != nil {
			failed = true
			break
		}
	}
	var code uint32
	if syscall.GetExitCodeProcess(pi.Process, &code) != nil || code != 0 {
		failed = true
	}
	var before iosCompilerJobAccounting
	if observe != nil {
		ok, _, _ = iosCompilerKernel.NewProc("QueryInformationJobObject").Call(j, 1, uintptr(unsafe.Pointer(&before)), unsafe.Sizeof(before), 0)
		if ok == 0 {
			failed = true
		}
	}
	// Successful root exit does not prove its descendants ended. Terminate the
	// exact owned job, then require the kernel's active count to reach zero.
	ok, _, _ = iosCompilerKernel.NewProc("TerminateJobObject").Call(j, 1)
	if ok == 0 {
		failed = true
	}
	deadline := time.Now().Add(3 * time.Second)
	gone := false
	for time.Now().Before(deadline) {
		var a iosCompilerJobAccounting
		ok, _, _ = iosCompilerKernel.NewProc("QueryInformationJobObject").Call(j, 1, uintptr(unsafe.Pointer(&a)), unsafe.Sizeof(a), 0)
		if ok == 0 {
			failed = true
			break
		}
		if a.Active == 0 {
			gone = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !gone {
		failed = true
	}
	if observe != nil {
		after := uint32(1)
		if gone {
			after = 0
		}
		observe(before.Active, after)
	}
	var output captured
	select {
	case output = <-done:
	case <-time.After(time.Second):
		r.Close()
		output = <-done
		failed = true
	}
	if failed || output.err != nil && output.err != io.EOF || ctx.Err() != nil {
		clear(output.raw)
		return nil, errIOSCompiler
	}
	return output.raw, nil
}
