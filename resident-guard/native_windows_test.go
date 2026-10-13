//go:build windows

package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

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
		{`{"state":3,"enabled":true,"executionLimit":"PT0S","multipleInstances":2,"actions":[{"execute":"D:\\CC\\Tools\\resident-guard\\resident-guard.exe","arguments":"--launch-service operations"}]}`, true},
		{`{"state":3,"enabled":true,"executionLimit":"PT72H","multipleInstances":2,"actions":[{"execute":"D:\\CC\\Tools\\resident-guard\\resident-guard.exe","arguments":"--launch-service operations"}]}`, false},
		{`{"state":3,"enabled":true,"executionLimit":"PT0S","multipleInstances":0,"actions":[{"execute":"D:\\CC\\Tools\\resident-guard\\resident-guard.exe","arguments":"--launch-service operations"}]}`, false},
		{`{"state":3,"enabled":true,"actions":[{"execute":"D:\\CC\\Tools\\resident-guard\\resident-guard.exe","arguments":"--launch-service operations"}]}`, false},
		{`{"state":3,"enabled":true,"actions":[{"execute":"D:\\CC\\Tools\\resident-guard\\resident-guard.exe","arguments":"--launch-service chrome"}]}`, false},
		{`{"state":3,"enabled":true,"actions":[{"execute":"C:\\private\\resident-guard.exe","arguments":"--launch-service operations"}]}`, false},
		{`{"state":3,"enabled":true,"actions":[{"execute":"D:\\CC\\Tools\\resident-guard\\resident-guard.exe","arguments":"--launch-service operations --extra"}]}`, false},
	} {
		out, err := decodeTask([]byte(test.body), definition)
		if err != nil || out.Valid != test.valid {
			t.Fatal("wrapper matching did not enforce compiled path and arguments")
		}
	}
}
