//go:build windows && amd64

package windowsrunner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// Called only after the hash-pinned stock archive creates a fresh runner root.
// The unprivileged worker can inspect all ancestors of these mutable paths;
// SYSTEM's proof metadata remains under the inaccessible SDK private parent.
func prepareWorkerDirectories(root string) error {
	for _, name := range []string{"_temp", "_profile", "_target"} {
		if os.Mkdir(filepath.Join(root, name), 0700) != nil {
			return errors.New("worker directory not fresh")
		}
	}
	for _, name := range []string{"Roaming", "Local"} {
		if os.MkdirAll(filepath.Join(root, "_profile", "AppData", name), 0700) != nil {
			return errors.New("worker profile preparation failed")
		}
	}
	return nil
}

const developerKey = `SOFTWARE\Microsoft\Windows\CurrentVersion\AppModelUnlock`
const developerValue = "AllowDevelopmentWithoutDevLicense"

func fixedGuestService(exe string, system bool) bool {
	return system && strings.EqualFold(filepath.Clean(exe), helperPath)
}

// This changes only the disposable running guest, not the retained SDK, host,
// account rights or runner token. Developer Mode supplies Windows' prerequisite
// for unprivileged symlink creation; mandatory real fixtures must still succeed.
// Never skip them or elevate the worker to get a passing result.
func enableGuestSymlinkSupport(ctx context.Context, env []string) error {
	exe, e := os.Executable()
	if e != nil || !fixedGuestService(exe, IsLocalSystem()) {
		return errors.New("symlink setup requires the fixed guest SYSTEM agent")
	}
	return applyGuestSymlinkSupport(ctx, env, runTree, readGuestDeveloperMode)
}

func applyGuestSymlinkSupport(ctx context.Context, env []string, run func(context.Context, string, string, string, []string, string) processResult, read func() (uint32, error)) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	const exe = `C:\Windows\System32\reg.exe`
	line := syscall.EscapeArg(exe) + ` add "HKLM\` + developerKey + `" /v ` + developerValue + ` /t REG_DWORD /d 1 /f /reg:64`
	r := run(ctx, exe, line, runnerRoot, env, "")
	if r.err != nil || r.exit != 0 || !r.drained {
		return errors.New("guest unprivileged symlink setup failed")
	}
	v, e := read()
	if e != nil || v != 1 {
		return errors.New("guest unprivileged symlink readback failed")
	}
	return nil
}

func readGuestDeveloperMode() (uint32, error) {
	path, _ := syscall.UTF16PtrFromString(developerKey)
	var key syscall.Handle
	if e := syscall.RegOpenKeyEx(syscall.HKEY_LOCAL_MACHINE, path, 0, syscall.KEY_QUERY_VALUE|0x0100, &key); e != nil {
		return 0, e
	}
	defer syscall.RegCloseKey(key)
	name, _ := syscall.UTF16PtrFromString(developerValue)
	var value, kind uint32
	size := uint32(4)
	if e := syscall.RegQueryValueEx(key, name, nil, &kind, (*byte)(unsafe.Pointer(&value)), &size); e != nil {
		return 0, e
	}
	if kind != syscall.REG_DWORD || size != 4 {
		return 0, errors.New("guest developer mode type rejected")
	}
	return value, nil
}
