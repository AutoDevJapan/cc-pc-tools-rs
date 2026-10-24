package main

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"
)

const cliTarget = `D:\CC\Tools\todo\todo.exe`

func TestCLIAcceptsOnlyThePurposeFlag(t *testing.T) {
	for _, args := range [][]string{
		{"--verify-todo-runtime-update", cliTarget},
		{"-verify-todo-runtime-update", cliTarget},
		{"--verify-todo-runtime-update=" + cliTarget},
		{"-verify-todo-runtime-update=" + cliTarget},
	} {
		called := ""
		var out, errOut strings.Builder
		code := runCLI(args, &out, &errOut, func(path string) (qualification, string) {
			called = path
			return qualification{}, "baseline_mismatch"
		})
		if called != cliTarget || code != 1 || out.String() != "{\"qualified\":false}\n" {
			t.Errorf("%q: called %q code %d stdout %q", args, called, code, out.String())
		}
	}
	for _, args := range [][]string{
		{"--verify-todo-runtime-update"},
		{"--verify-todo-runtime-update", "todo.exe"},
		{"--verify-todo-runtime-update", `tools\todo\todo.exe`},
		{"--verify-todo-runtime-update", cliTarget, "extra"},
		{"--verify-todo-runtime-update", cliTarget, "--verify-todo-runtime-update", cliTarget},
		{"--manifest", "m.json", "--verify-todo-runtime-update", cliTarget},
		{"--verify-todo-runtime-update", cliTarget, "--sha256", strings.Repeat("0", 64)},
		{"--guard-path", cliTarget},
		{"--verify-capacity-runtime-update", cliTarget},
		{"--verify-model-runtime-update", cliTarget},
		{"--verify-todo-runtime-update="},
		{"--verify-todo-runtime-update=todo.exe"},
		{"verify-todo-runtime-update", cliTarget},
		{"---verify-todo-runtime-update", cliTarget},
		{"--Verify-Todo-Runtime-Update", cliTarget},
		{"--help", "--verify-todo-runtime-update", cliTarget},
	} {
		called := false
		var out, errOut strings.Builder
		code := runCLI(args, &out, &errOut, func(string) (qualification, string) {
			called = true
			return qualification{Qualified: true}, ""
		})
		if called || code != 1 || out.String() != "{\"qualified\":false}\n" || !strings.Contains(errOut.String(), "argument") {
			t.Errorf("%q: called %v code %d stdout %q stderr %q", args, called, code, out.String(), errOut.String())
		}
	}
}

func TestCLIHelpAndUsage(t *testing.T) {
	for _, args := range [][]string{{"-h"}, {"--help"}, {"-help"}} {
		var out, errOut strings.Builder
		code := runCLI(args, &out, &errOut, func(string) (qualification, string) {
			t.Fatal("help must not verify")
			return qualification{}, ""
		})
		if code != 0 || out.Len() != 0 || !strings.Contains(errOut.String(), "-verify-todo-runtime-update") {
			t.Errorf("%q: code %d stdout %q stderr %q", args, code, out.String(), errOut.String())
		}
	}
	var out, errOut strings.Builder
	if code := runCLI(nil, &out, &errOut, nil); code != 2 || out.Len() != 0 || !strings.Contains(errOut.String(), "Usage") {
		t.Errorf("no arguments: code %d stdout %q stderr %q", code, out.String(), errOut.String())
	}
}

func TestCLIPrintsOnlyTheFixedQualification(t *testing.T) {
	var out, errOut strings.Builder
	code := runCLI([]string{"--verify-todo-runtime-update", cliTarget}, &out, &errOut, func(string) (qualification, string) {
		return qualification{Qualified: true, Domain: "todo-runtime-update-3190-v1", TodoID: 3190, VerifierTodoID: 3463, TargetKind: "todo", BuildCommand: []string{"cargo"}}, ""
	})
	if code != 0 || errOut.Len() != 0 {
		t.Fatalf("code %d stderr %q", code, errOut.String())
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out.String()), &got); err != nil {
		t.Fatal(err)
	}
	var keys []string
	for k := range got {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	want := "baseline_qualified_by baseline_source_receipt_sha256 build_command build_job build_log_sha256 build_receipt_sha256 current_sha256 data_access_granted dep_info_entries dep_info_sha256 domain inputs_verified local_crates new_bytes new_sha256 original_registry_recovered qualified source_head source_repository source_root source_tree target target_kind todo_id verifier_todo_id"
	if strings.Join(keys, " ") != want {
		t.Fatalf("keys %s\nwant %s", strings.Join(keys, " "), want)
	}
	if got["data_access_granted"] != false || got["qualified"] != true {
		t.Fatalf("output %v", got)
	}
}

func TestCLIHidesRejectionDetails(t *testing.T) {
	var out, errOut strings.Builder
	leak := strings.Repeat("d", 64)
	code := runCLI([]string{"--verify-todo-runtime-update", cliTarget}, &out, &errOut, func(string) (qualification, string) {
		return qualification{CurrentSHA256: leak, SourceRoot: "D:/private"}, "baseline_mismatch"
	})
	if code != 1 || out.String() != "{\"qualified\":false}\n" || !strings.Contains(errOut.String(), "baseline_mismatch") ||
		strings.Contains(out.String()+errOut.String(), leak) || strings.Contains(out.String()+errOut.String(), "private") {
		t.Fatalf("code %d stdout %q stderr %q", code, out.String(), errOut.String())
	}
}
