//go:build windows && amd64

package windowsrunner

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

const runnerUserSID = "S-1-5-19" // LocalService, not SYSTEM or Administrators.

// LogonUser's documented LocalService logon uses no password. Keep the SCM
// agent as SYSTEM for clock/control; only config/listener and their children
// receive this non-administrator primary token inside the disposable guest.
func newRunnerToken() (syscall.Token, error) {
	if !IsLocalSystem() {
		return 0, errors.New("runner logon requires guest SYSTEM service")
	}
	user, _ := syscall.UTF16PtrFromString("LocalService")
	domain, _ := syscall.UTF16PtrFromString("NT AUTHORITY")
	var token syscall.Token
	ok, _, _ := syscall.NewLazyDLL(`C:\Windows\System32\advapi32.dll`).NewProc("LogonUserW").Call(uintptr(unsafe.Pointer(user)), uintptr(unsafe.Pointer(domain)), 0, 5, 0, uintptr(unsafe.Pointer(&token)))
	if ok == 0 {
		return 0, errors.New("fixed runner logon failed")
	}
	if e := validateRunnerToken(token); e != nil {
		token.Close()
		return 0, e
	}
	return token, nil
}

func validateRunnerToken(token syscall.Token) error {
	if token == 0 {
		return errors.New("runner token absent")
	}
	user, e := token.GetTokenUser()
	if e != nil {
		return errors.New("runner user query failed")
	}
	sid, e := user.User.Sid.String()
	if e != nil || sid != runnerUserSID {
		return errors.New("runner user rejected")
	}
	var size uint32
	if e := syscall.GetTokenInformation(token, syscall.TokenGroups, nil, 0, &size); e != syscall.ERROR_INSUFFICIENT_BUFFER || size < 8 || size > 64<<10 {
		return errors.New("runner group query failed")
	}
	buffer := make([]byte, size)
	defer func() { runtime.KeepAlive(buffer) }()
	if e := syscall.GetTokenInformation(token, syscall.TokenGroups, &buffer[0], size, &size); e != nil || size > uint32(len(buffer)) || size < 8 {
		return errors.New("runner group read failed")
	}
	count := *(*uint32)(unsafe.Pointer(&buffer[0]))
	// TOKEN_GROUPS aligns SID_AND_ATTRIBUTES at byte 8 on this amd64 build.
	if count == 0 || uint64(count) > uint64(size-8)/uint64(unsafe.Sizeof(syscall.SIDAndAttributes{})) {
		return errors.New("runner group count rejected")
	}
	groups := unsafe.Slice((*syscall.SIDAndAttributes)(unsafe.Pointer(&buffer[8])), int(count))
	for _, group := range groups {
		sid, e := group.Sid.String()
		if e != nil || sid == "S-1-5-32-544" {
			return errors.New("runner administrator group rejected")
		}
	}
	return nil
}

// Only fixed guest runtime directories are writable. No inherited grant at
// sdkRoot: private logs, downloads and service bootstrap state stay SYSTEM-only.
func runnerAccessCommands() [][]string {
	commands := [][]string{{sdkRoot, "/grant:r", "*" + runnerUserSID + ":X"}, {helperPath, "/grant:r", "*" + runnerUserSID + ":RX"}, {hookRoot, "/grant:r", "*" + runnerUserSID + ":(OI)(CI)RX"}}
	for _, name := range []string{"rust-1.97.1", "git", "powershell", "sccache", "vs-build-tools"} {
		commands = append(commands, []string{sdkRoot + `\` + name, "/grant:r", "*" + runnerUserSID + ":(OI)(CI)RX"})
	}
	for _, name := range []string{"runner", "staging", "cargo-home", "rustup-home", "target", "sccache-cache", "service-profile"} {
		commands = append(commands, []string{sdkRoot + `\` + name, "/grant:r", "*" + runnerUserSID + ":(OI)(CI)M"})
	}
	// Git rejects a worktree/.git owned by another account. Give the freshly
	// verified checkout to its actual executor, without a safe.directory bypass.
	commands = append(commands, []string{sourceRoot, "/setowner", "*" + runnerUserSID, "/T"})
	return commands
}

func prepareRunnerAccess(ctx context.Context, env []string) error {
	if !IsLocalSystem() {
		return errors.New("runner ACL setup requires guest SYSTEM service")
	}
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	const exe = `C:\Windows\System32\icacls.exe`
	for _, args := range runnerAccessCommands() {
		words := []string{syscall.EscapeArg(exe)}
		for _, a := range args {
			words = append(words, syscall.EscapeArg(a))
		}
		r := runTree(ctx, exe, strings.Join(words, " "), sdkRoot, env, "")
		if r.err != nil || r.exit != 0 || !r.drained {
			return errors.New("fixed guest runner ACL setup failed")
		}
	}
	return nil
}
