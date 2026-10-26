//go:build windows && amd64

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

func TestInputAppServerHelper(t *testing.T) {
	mode := ""
	for i, arg := range os.Args {
		if arg == "input-test-mode" && i+1 < len(os.Args) {
			mode = os.Args[i+1]
		}
	}
	if mode == "" {
		return
	}
	if mode == "grandchild" {
		for {
			time.Sleep(time.Hour)
		}
	}
	childPID := 0
	if mode == "ignore-eof" || mode == "orphan-writer" {
		exe, err := os.Executable()
		if err != nil {
			os.Exit(8)
		}
		cmd := exec.Command(exe, "-test.run=^TestInputAppServerHelper$", "--", "input-test-mode", "grandchild")
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if cmd.Start() != nil {
			os.Exit(9)
		}
		childPID = cmd.Process.Pid
	}
	reader := bufio.NewScanner(os.Stdin)
	for reader.Scan() {
		var request struct {
			ID     *int64 `json:"id"`
			Method string `json:"method"`
		}
		if json.Unmarshal(reader.Bytes(), &request) != nil {
			os.Exit(10)
		}
		if request.ID == nil {
			continue
		}
		result := map[string]any{}
		if request.Method == "thread/read" {
			result["child_pid"] = childPID
			if mode == "environment" {
				paths, err := nativeInputPaths()
				if err != nil {
					os.Exit(11)
				}
				values := inputSourceEnvironment(paths)
				result["hostile_values_excluded"] = !strings.Contains(strings.Join(values, "\n"), "ATTACKER-")
				result["native_profile_independent"] = paths.Profile != os.Getenv("USERPROFILE")
				result["canonical_home_present"] = false
				for _, entry := range values {
					if entry == "CODEX_HOME="+filepath.Join(paths.Profile, ".codex") {
						result["canonical_home_present"] = true
					}
				}
			}
		}
		if json.NewEncoder(os.Stdout).Encode(map[string]any{"id": *request.ID, "result": result}) != nil {
			os.Exit(12)
		}
	}
	if mode == "nonzero" {
		os.Exit(7)
	}
	if mode == "ignore-eof" {
		for {
			time.Sleep(time.Hour)
		}
	}
	os.Exit(0)
}

func connectInputFixture(t *testing.T, mode string, env []string) inputRPC {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	rpc, err := connectInputCommand(ctx, exe, []string{"-test.run=^TestInputAppServerHelper$", "--", "input-test-mode", mode}, env)
	if err != nil {
		t.Fatal("synthetic AppServer did not initialize")
	}
	t.Cleanup(func() { _ = rpc.Close() })
	return rpc
}

func TestInputNativeEnvironmentCannotBeRedirectedByInheritedSettings(t *testing.T) {
	paths, err := nativeInputPaths()
	if err != nil {
		t.Fatal("native paths unavailable")
	}
	var env []string
	for _, entry := range inputSourceEnvironment(paths) {
		key, _, _ := strings.Cut(entry, "=")
		if key != "CODEX_HOME" && key != "PATH" && key != "USERPROFILE" {
			env = append(env, entry)
		}
	}
	env = append(env, "CODEX_HOME=ATTACKER-RECORDS", "PATH=ATTACKER-EXECUTABLES", "USERPROFILE=ATTACKER-PROFILE", "UNRELATED_API_KEY=ATTACKER-KEY", "CLAUDE_CODE_OAUTH_TOKEN=ATTACKER-TOKEN")
	rpc := connectInputFixture(t, "environment", env)
	raw, err := rpc.Call(context.Background(), "thread/read", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	var proof struct {
		Excluded bool `json:"hostile_values_excluded"`
		Native   bool `json:"native_profile_independent"`
		Home     bool `json:"canonical_home_present"`
	}
	if json.Unmarshal(raw, &proof) != nil || !proof.Excluded || !proof.Native || !proof.Home {
		t.Fatal("native source environment followed caller overrides")
	}
	if rpc.Close() != nil {
		t.Fatal("environment fixture did not exit cleanly")
	}
}

func TestInputNativeCloseDrainsEOFAndKillsOnlyOwnedStuckDescendants(t *testing.T) {
	paths, err := nativeInputPaths()
	if err != nil {
		t.Fatal("native paths unavailable")
	}
	for _, mode := range []string{"eof", "nonzero", "ignore-eof", "orphan-writer"} {
		t.Run(mode, func(t *testing.T) {
			rpc := connectInputFixture(t, mode, inputSourceEnvironment(paths))
			raw, err := rpc.Call(context.Background(), "thread/read", map[string]any{})
			if err != nil {
				t.Fatal(err)
			}
			var result struct {
				PID uint32 `json:"child_pid"`
			}
			if json.Unmarshal(raw, &result) != nil {
				t.Fatal("fixture identity missing")
			}
			var held syscall.Handle
			if mode == "ignore-eof" || mode == "orphan-writer" {
				if result.PID == 0 {
					t.Fatal("fixture did not spawn its descendant")
				}
				held, err = syscall.OpenProcess(0x00100000|0x0400, false, result.PID)
				if err != nil {
					t.Fatal("cannot hold fixture descendant handle")
				}
				defer syscall.CloseHandle(held)
				var inJob int32
				child := rpc.(*appServerRPC).child.(*nativeInputChild)
				ok, _, _ := inputKernel.NewProc("IsProcessInJob").Call(uintptr(held), uintptr(child.job), uintptr(unsafe.Pointer(&inJob)))
				if ok == 0 || inJob == 0 {
					t.Fatal("descendant escaped atomically assigned Job")
				}
			}
			start := time.Now()
			closeErr := rpc.Close()
			if time.Since(start) > 5*time.Second {
				t.Fatal("source close exceeded its finite budget")
			}
			if (mode == "eof") != (closeErr == nil) {
				t.Fatalf("wrong close result for %s: %v", mode, closeErr)
			}
			if held != 0 {
				state, err := syscall.WaitForSingleObject(held, 0)
				if err != nil || state != syscall.WAIT_OBJECT_0 {
					t.Fatal("held descendant is not terminated")
				}
			}
			if rpc.Close() != nil {
				t.Fatal("close is not idempotent")
			}
		})
	}
}

func TestInputNativeCreationFailureIsFinite(t *testing.T) {
	if _, err := startInputChild("D:/no-such-synthetic-input-helper.exe", nil, []string{"SYSTEMROOT=C:/Windows"}); err == nil {
		t.Fatal("missing executable was started")
	}
	if _, err := startInputChild("relative.exe", nil, nil); err == nil {
		t.Fatal("relative executable accepted")
	}
}
