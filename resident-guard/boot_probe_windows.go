//go:build windows

package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

func inspectSession(ctx context.Context, expectedSID string) (sessionReport, error) {
	out := sessionReport{Version: 1, CheckedAt: time.Now().UTC(), ProcessID: os.Getpid(), Services: []serviceReport{}, CLIAuth: []cliAuthStatus{}}
	if !strings.HasPrefix(expectedSID, "S-1-") || strings.ContainsAny(expectedSID, "\r\n") {
		return out, errors.New("expected_user_invalid")
	}
	token, err := syscall.OpenCurrentProcessToken()
	if err != nil {
		return out, errors.New("session_token_unavailable")
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	if err != nil {
		return out, errors.New("session_user_unavailable")
	}
	currentSID, err := user.User.Sid.String()
	if err != nil {
		return out, errors.New("session_user_unavailable")
	}
	out.SameExpectedUser = observedBool(strings.EqualFold(currentSID, expectedSID))
	tokenProfile, profileErr := token.GetUserProfileDirectory()
	userProfile, homeErr := os.UserHomeDir()
	out.ProfileMatchesToken = observedBool(profileErr == nil && homeErr == nil && strings.EqualFold(filepath.Clean(tokenProfile), filepath.Clean(userProfile)))
	var elevation, returned uint32
	if syscall.GetTokenInformation(token, 20, (*byte)(unsafe.Pointer(&elevation)), 4, &returned) != nil || returned != 4 {
		return out, errors.New("session_elevation_unavailable")
	}
	out.Elevated = observedBool(elevation != 0)
	restricted, medium, err := restrictionState(token)
	if err != nil {
		return out, errors.New("session_restriction_unavailable")
	}
	out.TokenRestricted, out.MediumIntegrity = observedBool(restricted), observedBool(medium)
	administrator, err := administratorEnabled(token)
	if err != nil {
		return out, errors.New("session_admin_membership_unavailable")
	}
	out.AdministratorEnabled = observedBool(administrator)
	sessionCall := kernel32.NewProc("ProcessIdToSessionId")
	var sessionID uint32
	ok, _, _ := sessionCall.Call(uintptr(os.Getpid()), uintptr(unsafe.Pointer(&sessionID)))
	if ok == 0 {
		return out, errors.New("session_id_unavailable")
	}
	out.SessionID = &sessionID
	out.NonInteractive = observedBool(sessionID == 0)
	if !*out.SameExpectedUser || !*out.ProfileMatchesToken || administrator || !medium {
		return out, errors.New("session_identity_or_level_mismatch")
	}
	for _, definition := range productionServices() {
		out.Services = append(out.Services, serviceReport{ID: definition.ID, Probe: (nativeBackend{}).Probe(ctx, definition), Action: "read_only"})
	}
	appdata := os.Getenv("APPDATA")
	codexPath := ""
	for _, relative := range []string{
		"npm/node_modules/@openai/codex-win32-x64/vendor/x86_64-pc-windows-msvc/bin/codex.exe",
		"npm/node_modules/@openai/codex/vendor/x86_64-pc-windows-msvc/bin/codex.exe",
	} {
		candidate := filepath.Join(appdata, filepath.FromSlash(relative))
		if entry, err := os.Stat(candidate); err == nil && entry.Mode().IsRegular() {
			codexPath = candidate
			break
		}
	}
	if codexPath != "" {
		out.CLIAuth = append(out.CLIAuth, inspectCLIAuth(ctx, "codex", codexPath, "login", "status"))
	} else {
		// The official npm launcher can keep its native binary in a linked
		// package. Use the installed status command, whose login branch skips
		// the service autostart shim, rather than relocating credentials.
		out.CLIAuth = append(out.CLIAuth, inspectCLIAuth(ctx, "codex", systemFile("WindowsPowerShell", "v1.0", "powershell.exe"),
			"-NoProfile", "-NonInteractive", "-Command", `& (Join-Path $env:APPDATA 'npm\codex.cmd') login status 2>&1 | Write-Output; exit $LASTEXITCODE`))
	}
	out.CLIAuth = append(out.CLIAuth, inspectCLIAuth(ctx, "claude", filepath.Join(userProfile, ".local", "bin", "claude.exe"), "auth", "status"))
	// This public endpoint needs no token, account, or integrated Windows login.
	// A successful socket/TLS read is recorded separately from network credentials.
	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://discord.com/api/v10/gateway", nil)
	out.AnonymousHTTPS = "unconfirmed"
	if err == nil {
		response, requestErr := client.Do(request)
		if requestErr == nil {
			response.Body.Close()
			if response.StatusCode == http.StatusOK {
				out.AnonymousHTTPS = "public_https_200"
			}
		}
	}
	out.FinishedAt = time.Now().UTC()
	return out, nil
}

func inspectCLIAuth(ctx context.Context, cli, executable string, arguments ...string) cliAuthStatus {
	childCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	command := exec.CommandContext(childCtx, executable, arguments...)
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	command.WaitDelay = time.Second
	output := &boundedOutput{limit: 16 * 1024}
	command.Stdout = output
	command.Stderr = output
	err := command.Run()
	code := -1
	if command.ProcessState != nil {
		code = command.ProcessState.ExitCode()
	}
	if childCtx.Err() != nil {
		return cliAuthStatus{CLI: cli, Exit: code, Category: "command_deadline"}
	}
	if output.overflow {
		return cliAuthStatus{CLI: cli, Exit: code, Category: "command_output_exceeded"}
	}
	if err != nil && command.ProcessState == nil {
		return cliAuthStatus{CLI: cli, Exit: code, Category: "cli_unavailable"}
	}
	return classifyCLIAuth(cli, code, output.bytes)
}
