//go:build windows

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

func TestStateDirectoryRejectsWindowsJunctionAncestor(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "physical")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "junction")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, systemFile("cmd.exe"), "/c", "mklink", "/J", link, target)
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("could not create an owned junction fixture: %v, %s", err, output)
	}
	defer os.Remove(link)
	if err := validateStateDirectory(filepath.Join(link, "state")); err == nil {
		t.Fatal("a junction ancestor was accepted")
	}
	if err := validateStateDirectory(filepath.Join(target, "state")); err != nil {
		t.Fatal("physical state directory was rejected", err)
	}
}

func TestStateDirectoryAcceptsWindowsShortName(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "state directory with long name")
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	wide, err := syscall.UTF16PtrFromString(directory)
	if err != nil {
		t.Fatal(err)
	}
	buffer := make([]uint16, 32768)
	length, _, callErr := kernel32.NewProc("GetShortPathNameW").Call(
		uintptr(unsafe.Pointer(wide)), uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)))
	if length == 0 || length >= uintptr(len(buffer)) {
		t.Fatal("could not inspect the owned directory's short name", callErr)
	}
	shortName := syscall.UTF16ToString(buffer[:length])
	if strings.EqualFold(shortName, directory) {
		t.Skip("this volume does not generate short names")
	}
	if err := validateStateDirectory(shortName); err != nil {
		t.Fatal("the same physical directory's short name was rejected", err)
	}
}

func TestNamedMutexRejectsConcurrentTickAndReleases(t *testing.T) {
	name := `Local\CC-ResidentGuard-test-` + strconv.Itoa(os.Getpid())
	release, acquired, err := acquireMutex(name)
	if err != nil || !acquired {
		t.Fatal("first acquisition failed", err)
	}
	_, duplicate, err := acquireMutex(name)
	if err != nil || duplicate {
		release()
		t.Fatal("duplicate acquisition allowed", err)
	}
	release()
	release, acquired, err = acquireMutex(name)
	if err != nil || !acquired {
		t.Fatal("mutex was not released", err)
	}
	release()
}

func TestTaskDefinitionValidationRejectsChangedActionsAndMissingState(t *testing.T) {
	definition := service{Execute: `C:\Windows\System32\wscript.exe`, Arguments: `//B "D:\CC\Tools\localai-web-rs\resident-hidden.vbs"`}
	for _, test := range []struct {
		body                           string
		valid, enabled, busy, rejected bool
	}{
		{`{"state":3,"enabled":true,"actions":[{"execute":"C:\\WINDOWS\\system32\\wscript.exe","arguments":"//B \"D:\\CC\\Tools\\localai-web-rs\\resident-hidden.vbs\""}]}`, true, true, false, false},
		{`{"state":4,"enabled":true,"actions":[{"execute":"C:\\Windows\\System32\\wscript.exe","arguments":"//B \"D:\\CC\\Tools\\localai-web-rs\\resident-hidden.vbs\""}]}`, true, true, true, false},
		{`{"state":2,"enabled":true,"actions":[{"execute":"C:\\Windows\\System32\\wscript.exe","arguments":"//B \"D:\\CC\\Tools\\localai-web-rs\\resident-hidden.vbs\""}]}`, true, true, true, false},
		{`{"state":1,"enabled":false,"actions":[]}`, false, false, false, false},
		{`{"state":3,"enabled":true,"actions":[{"execute":"another.exe","arguments":""}]}`, false, true, false, false},
		{`{"state":3,"actions":[]}`, false, false, false, true},
		{`{"state":0,"enabled":true,"actions":[]}`, false, false, false, true},
	} {
		out, err := decodeTask([]byte(test.body), definition)
		if (err != nil) != test.rejected || (err == nil && (out.Valid != test.valid || out.Enabled != test.enabled || out.Running != test.busy)) {
			t.Fatalf("unexpected task validation: %+v, %v", out, err)
		}
	}
}

func TestBoundedCommandHelper(t *testing.T) {
	if len(os.Args) < 4 || os.Args[len(os.Args)-3] != "--resident-guard-helper" {
		return
	}
	mode := os.Args[len(os.Args)-2]
	if os.WriteFile(os.Args[len(os.Args)-1], []byte(mode), 0600) != nil {
		os.Exit(4)
	}
	switch mode {
	case "sleep":
		time.Sleep(30 * time.Second)
	case "output":
		fmt.Print(strings.Repeat("x", 32*1024))
	case "diagnostic":
		fmt.Fprint(os.Stderr, "secret fixture diagnostic")
		fmt.Print("ok")
		os.Exit(1)
	}
	os.Exit(0)
}

func TestChildDeadlineAndOutputLimits(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	before := time.Now()
	sleepMarker := t.TempDir() + `\sleep-marker`
	data, err := boundedCommand(context.Background(), time.Second, executable,
		"-test.run=^TestBoundedCommandHelper$", "--", "--resident-guard-helper", "sleep", sleepMarker)
	reached, readErr := os.ReadFile(sleepMarker)
	if err == nil || err.Error() != "command_deadline" || len(data) != 0 || time.Since(before) > 3*time.Second || readErr != nil || string(reached) != "sleep" {
		t.Fatal("child timeout was not bounded", err)
	}
	outputMarker := t.TempDir() + `\output-marker`
	data, err = boundedCommand(context.Background(), 2*time.Second, executable,
		"-test.run=^TestBoundedCommandHelper$", "--", "--resident-guard-helper", "output", outputMarker)
	reached, readErr = os.ReadFile(outputMarker)
	if err == nil || len(data) != 0 || readErr != nil || string(reached) != "output" {
		t.Fatal("oversized child output accepted", err)
	}
	diagnosticMarker := t.TempDir() + `\diagnostic-marker`
	data, err = boundedCommand(context.Background(), 2*time.Second, executable,
		"-test.run=^TestBoundedCommandHelper$", "--", "--resident-guard-helper", "diagnostic", diagnosticMarker)
	reached, readErr = os.ReadFile(diagnosticMarker)
	if err == nil || len(data) != 0 || strings.Contains(err.Error(), "secret") || readErr != nil || string(reached) != "diagnostic" {
		t.Fatal("raw child diagnostic leaked", err)
	}
}

func TestInstalledTaskDefinitionsReadOnly(t *testing.T) {
	if os.Getenv("CC_RESIDENT_INSPECT_INSTALLED_TASKS") != "1" {
		t.Skip("explicit read-only deployment inspection only")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	for _, definition := range productionServices() {
		t.Run(definition.ID, func(t *testing.T) {
			task, err := (nativeBackend{}).Task(ctx, definition)
			if err != nil || !task.Valid || !task.Enabled {
				t.Fatal("installed action or enabled state did not match compiled service definition")
			}
		})
	}
}

func TestWrappedTaskOnlyAcceptsExactCompiledServiceAction(t *testing.T) {
	definition := service{ID: "operations", Execute: `C:\Windows\System32\wscript.exe`, Arguments: `original`}
	for _, test := range []struct {
		body  string
		valid bool
	}{
		{`{"state":3,"enabled":true,"executionLimit":"PT0S","multipleInstances":1,"actions":[{"execute":"D:\\CC\\Tools\\resident-guard\\resident-guard.exe","arguments":"--launch-service operations"}]}`, true},
		{`{"state":3,"enabled":true,"executionLimit":"PT0S","multipleInstances":2,"actions":[{"execute":"D:\\CC\\Tools\\resident-guard\\resident-guard.exe","arguments":"--launch-service operations"}]}`, false},
		{`{"state":3,"enabled":true,"executionLimit":"PT72H","multipleInstances":1,"actions":[{"execute":"D:\\CC\\Tools\\resident-guard\\resident-guard.exe","arguments":"--launch-service operations"}]}`, false},
		{`{"state":3,"enabled":true,"executionLimit":"PT0S","multipleInstances":0,"actions":[{"execute":"D:\\CC\\Tools\\resident-guard\\resident-guard.exe","arguments":"--launch-service operations"}]}`, false},
		{`{"state":3,"enabled":true,"actions":[{"execute":"D:\\CC\\Tools\\resident-guard\\resident-guard.exe","arguments":"--launch-service operations"}]}`, false},
		{`{"state":3,"enabled":true,"executionLimit":"PT0S","multipleInstances":1,"actions":[{"execute":"D:\\CC\\Tools\\resident-guard\\resident-guard.exe","arguments":"--launch-service chrome"}]}`, false},
		{`{"state":3,"enabled":true,"executionLimit":"PT0S","multipleInstances":1,"actions":[{"execute":"C:\\private\\resident-guard.exe","arguments":"--launch-service operations"}]}`, false},
		{`{"state":3,"enabled":true,"executionLimit":"PT0S","multipleInstances":1,"actions":[{"execute":"D:\\CC\\Tools\\resident-guard\\resident-guard.exe","arguments":"--launch-service operations --extra"}]}`, false},
	} {
		out, err := decodeTask([]byte(test.body), definition)
		if err != nil || out.Valid != test.valid {
			t.Fatal("wrapper matching did not enforce compiled path and arguments")
		}
	}
}

func TestManagedTaskRequiresServiceSpecificLifetimePolicy(t *testing.T) {
	for _, id := range []string{"operations", "opencodex", "godd_ao", "opencodex_watchdog"} {
		for _, policy := range []struct {
			name, value string
		}{
			{"parallel", "0"}, {"queue", "1"}, {"ignore_new", "2"},
			{"stop_existing", "3"}, {"missing", "null"}, {"unknown", "4"},
		} {
			for _, limit := range []string{"PT0S", "PT72H", ""} {
				t.Run(id+"/"+policy.name+"/"+limit, func(t *testing.T) {
					body := fmt.Sprintf(`{"state":4,"enabled":true,"executionLimit":%q,"multipleInstances":%s,"actions":[{"execute":"D:\\CC\\Tools\\resident-guard\\resident-guard.exe","arguments":%q}]}`,
						limit, policy.value, "--launch-service "+id)
					valid := limit == "PT0S" && ((id == "operations" && policy.value == "1") || (id != "operations" && policy.value == "2"))
					out, err := decodeTask([]byte(body), service{ID: id})
					if err != nil || out.Valid != valid || !out.Enabled || !out.Running {
						t.Fatalf("unexpected managed task validation: %+v, %v", out, err)
					}
				})
			}
		}
	}
}

func TestManagedOperationsQueuePreservesTaskState(t *testing.T) {
	for _, test := range []struct {
		state, enabled           string
		wantEnabled, wantRunning bool
	}{
		{"1", "false", false, false}, {"2", "true", true, true},
		{"3", "true", true, false}, {"4", "true", true, true},
		{"3", "false", false, false},
	} {
		t.Run(test.state+"/"+test.enabled, func(t *testing.T) {
			body := fmt.Sprintf(`{"state":%s,"enabled":%s,"executionLimit":"PT0S","multipleInstances":1,"actions":[{"execute":"D:\\CC\\Tools\\resident-guard\\resident-guard.exe","arguments":"--launch-service operations"}]}`,
				test.state, test.enabled)
			out, err := decodeTask([]byte(body), service{ID: "operations"})
			if err != nil || !out.Valid || out.Enabled != test.wantEnabled || out.Running != test.wantRunning {
				t.Fatalf("unexpected operations task state: %+v, %v", out, err)
			}
		})
	}
}

func TestLegacyTaskActionsRemainValidDuringMigration(t *testing.T) {
	for _, definition := range productionServices() {
		t.Run(definition.ID, func(t *testing.T) {
			body, err := json.Marshal(map[string]any{
				"state": 3, "enabled": true, "executionLimit": "PT0S", "multipleInstances": 2,
				"actions": []map[string]string{{"execute": definition.Execute, "arguments": definition.Arguments}},
			})
			if err != nil {
				t.Fatal(err)
			}
			out, err := decodeTask(body, definition)
			if err != nil || !out.Valid || !out.Enabled || out.Running {
				t.Fatalf("legacy migration action rejected or treated as a managed running task: %+v, %v", out, err)
			}
		})
	}
}
