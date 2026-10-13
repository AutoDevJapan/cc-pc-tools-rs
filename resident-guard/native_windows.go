//go:build windows

package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

var (
	kernel32    = syscall.NewLazyDLL("kernel32.dll")
	createMutex = kernel32.NewProc("CreateMutexW")
	moveFileEx  = kernel32.NewProc("MoveFileExW")
)

func acquireMutex(name string) (func(), bool, error) {
	wide, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return nil, false, err
	}
	// Retaining the named object is the single-flight lease. Do not take
	// thread ownership: Go goroutines can return on a different OS thread.
	handle, _, callErr := createMutex.Call(0, 0, uintptr(unsafe.Pointer(wide)))
	if handle == 0 {
		return nil, false, callErr
	}
	if callErr == syscall.Errno(183) { // ERROR_ALREADY_EXISTS
		syscall.CloseHandle(syscall.Handle(handle))
		return func() {}, false, nil
	}
	return func() { syscall.CloseHandle(syscall.Handle(handle)) }, true, nil
}

func replaceFile(source, destination string) error {
	sourceWide, err := syscall.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	destWide, err := syscall.UTF16PtrFromString(destination)
	if err != nil {
		return err
	}
	result, _, callErr := moveFileEx.Call(uintptr(unsafe.Pointer(sourceWide)), uintptr(unsafe.Pointer(destWide)), 0x1|0x8)
	if result == 0 {
		return callErr
	}
	return nil
}

func systemFile(parts ...string) string {
	root := os.Getenv("SystemRoot")
	if root == "" {
		root = "C:/Windows"
	}
	return filepath.Join(append([]string{root, "System32"}, parts...)...)
}

func productionServices() []service {
	user, _ := os.UserHomeDir()
	wscript := systemFile("wscript.exe")
	return []service{
		{ID: "operations", URL: "http://127.0.0.1:10012/api/health", Address: "127.0.0.1:10012", Console: true,
			Task: "local-ai-web-autostart-10012", Execute: wscript,
			Arguments: `//B //Nologo "D:\CC\Tools\localai-web-rs\resident-hidden.vbs"`},
		{ID: "opencodex", URL: "http://127.0.0.1:10100/healthz", Address: "127.0.0.1:10100",
			Task: "opencodex-proxy", Execute: wscript,
			Arguments: `/b /nologo "` + filepath.Join(user, ".opencodex", "opencodex-service-launcher.vbs") + `"`},
		{ID: "godd_ao", URL: "http://127.0.0.1:10101/healthz", Address: "127.0.0.1:10101", Dependency: "opencodex",
			Task: "godd-ao-semantic-proxy", Execute: wscript,
			Arguments: `/b /nologo "` + filepath.Join(user, ".opencodex", "godd-ao", "semantic-proxy-launcher.vbs") + `"`},
	}
}

type nativeBackend struct{}

func connectionRefused(err error) bool {
	// WSAECONNREFUSED, not the POSIX-oriented syscall.ECONNREFUSED constant.
	// https://learn.microsoft.com/en-us/windows/win32/winsock/windows-sockets-error-codes-2
	return errors.Is(err, syscall.Errno(10061))
}

func (nativeBackend) Probe(ctx context.Context, definition service) probeResult {
	return probeHTTP(ctx, definition, 3*time.Second)
}

// boundedOutput continues draining the child without retaining its raw output.
// Only validated scheduler fields are returned; no child diagnostics enter logs.
type boundedOutput struct {
	mu       sync.Mutex
	bytes    []byte
	limit    int
	overflow bool
}

func (output *boundedOutput) Write(data []byte) (int, error) {
	output.mu.Lock()
	defer output.mu.Unlock()
	remaining := output.limit - len(output.bytes)
	if remaining < len(data) {
		output.overflow = true
	}
	if remaining > 0 {
		output.bytes = append(output.bytes, data[:min(remaining, len(data))]...)
	}
	return len(data), nil
}

func boundedCommand(ctx context.Context, timeout time.Duration, executable string, arguments ...string) ([]byte, error) {
	childContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	command := exec.CommandContext(childContext, executable, arguments...)
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	command.WaitDelay = time.Second
	output := &boundedOutput{limit: 16 * 1024}
	command.Stdout = output
	command.Stderr = io.Discard
	err := command.Run()
	if childContext.Err() != nil {
		return nil, errors.New("command_deadline")
	}
	if err != nil || output.overflow {
		return nil, errors.New("command_failed")
	}
	return output.bytes, nil
}

func (nativeBackend) Task(ctx context.Context, definition service) (taskResult, error) {
	// Service names are compiled constants, never owner text or data from a health response.
	if strings.ContainsAny(definition.Task, "'\r\n") {
		return taskResult{}, errors.New("task_name_invalid")
	}
	script := `$ErrorActionPreference='Stop';$t=Get-ScheduledTask -TaskName '` + definition.Task +
		`';[ordered]@{state=[int]$t.State;enabled=$t.Settings.Enabled;executionLimit=$t.Settings.ExecutionTimeLimit;multipleInstances=[int]$t.Settings.MultipleInstances;actions=@($t.Actions|ForEach-Object{[ordered]@{execute=$_.Execute;arguments=$_.Arguments}})}|ConvertTo-Json -Depth 4 -Compress`
	data, err := boundedCommand(ctx, 12*time.Second, systemFile("WindowsPowerShell", "v1.0", "powershell.exe"),
		"-NoProfile", "-NonInteractive", "-Command", script)
	if err != nil {
		return taskResult{}, err
	}
	return decodeTask(data, definition)
}

func decodeTask(data []byte, definition service) (taskResult, error) {
	var response struct {
		State             int    `json:"state"`
		Enabled           *bool  `json:"enabled"`
		ExecutionLimit    string `json:"executionLimit"`
		MultipleInstances *int   `json:"multipleInstances"`
		Actions           []struct {
			Execute, Arguments string
		} `json:"actions"`
	}
	if json.Unmarshal(data, &response) != nil || response.Enabled == nil || response.State < 1 || response.State > 4 {
		return taskResult{}, errors.New("task_response_invalid")
	}
	valid := len(response.Actions) == 1 &&
		strings.EqualFold(filepath.Clean(response.Actions[0].Execute), filepath.Clean(definition.Execute)) &&
		strings.EqualFold(strings.TrimSpace(response.Actions[0].Arguments), strings.TrimSpace(definition.Arguments))
	if !valid && len(response.Actions) == 1 && validServiceID(definition.ID) {
		valid = strings.EqualFold(filepath.Clean(response.Actions[0].Execute), filepath.Clean("D:/CC/Tools/resident-guard/resident-guard.exe")) &&
			response.Actions[0].Arguments == "--launch-service "+definition.ID
	}
	if valid && validServiceID(definition.ID) && strings.EqualFold(filepath.Clean(response.Actions[0].Execute), filepath.Clean("D:/CC/Tools/resident-guard/resident-guard.exe")) {
		valid = response.ExecutionLimit == "PT0S" && response.MultipleInstances != nil && *response.MultipleInstances == 2
	}
	return taskResult{Valid: valid, Enabled: *response.Enabled && response.State != 1, Running: response.State == 4 || response.State == 2}, nil
}

func (nativeBackend) Start(ctx context.Context, definition service) error {
	// Validate again just before requesting startup, preserving intentional stops
	// and refusing a changed scheduler action after the earlier observation.
	current, err := (nativeBackend{}).Task(ctx, definition)
	if err != nil || !current.Valid || !current.Enabled || current.Running {
		return errors.New("task_changed_before_start")
	}
	if (nativeBackend{}).Probe(ctx, definition).Port != "closed" {
		return errors.New("port_changed_before_start")
	}
	_, err = boundedCommand(ctx, 8*time.Second, systemFile("schtasks.exe"), "/Run", "/TN", definition.Task)
	return err
}
