package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestSkills3080UncertainCreatorNeverAllocatesTwice(t *testing.T) {
	for _, outcome := range []string{"timeout_after_creation", "nonzero_after_creation", "malformed_output", "process_crash"} {
		t.Run(outcome, func(t *testing.T) {
			workspace := skillsFixtureRoot(t)
			if os.Mkdir(filepath.Join(workspace, ".ci-work"), 0700) != nil {
				t.Fatal("existing private workspace")
			}
			request := filepath.Join(workspace, "request.json")
			if os.WriteFile(request, []byte("synthetic request"), 0600) != nil {
				t.Fatal("request")
			}
			getID := func(path string) skillsObjectID {
				f, e := os.Open(path)
				if e != nil {
					t.Fatal("request alias")
				}
				defer f.Close()
				id, ok := skillsHandleID(syscall.Handle(f.Fd()))
				if !ok {
					t.Fatal("request identity")
				}
				return id
			}
			id := getID(request)
			alias := getID(strings.ToUpper(filepath.ToSlash(request)))
			if id != alias {
				t.Fatal("real alias control")
			}
			w := skillsWriter{Task: 3080, Job: "20300101-000000-todo-3080", Workspace: workspace, SpecSHA: hashBytes([]byte("spec"))}
			calls := 0
			created := filepath.Join(workspace, "simulated-owned-root")
			create := func() (string, string, bool) {
				calls++
				if os.Mkdir(created, 0700) != nil {
					t.Fatal("only one allocation may occur")
				}
				return "", "", false
			}
			first := skillsFS{blocked: func(string) bool { return false }}
			_, _, ok, claimed := skillsRuntimeSourceAllocateOnce(&first, w, id, 6291, hashBytes([]byte("body")), create)
			first.close() // release every process handle, as after a crash/unknown result
			if ok || !claimed || calls != 1 {
				t.Fatal("unknown creation did not leave a durable attempt")
			}
			second := skillsFS{blocked: func(string) bool { return false }}
			defer second.close()
			_, _, _, claimed = skillsRuntimeSourceAllocateOnce(&second, w, alias, 6291, hashBytes([]byte("body")), create)
			if claimed || calls != 1 {
				t.Fatal("unknown outcome was retried through a request alias")
			}
			if info, e := os.Stat(created); e != nil || !info.IsDir() {
				t.Fatal("uncertain original root must remain for reconciliation")
			}
		})
	}
}

func TestSkills3080UnknownAttemptNativeBlocksAfterRecordRemoval(t *testing.T) {
	for _, outcome := range []string{"normal_failure", "legacy_failure", "empty_success", "invalid_success", "malformed_output", "missing_output"} {
		t.Run(outcome, func(t *testing.T) {
			workspace := skillsFixtureRoot(t)
			if os.Mkdir(filepath.Join(workspace, ".ci-work"), 0700) != nil {
				t.Fatal("private fixture")
			}
			path := filepath.Join(workspace, "request.json")
			if os.WriteFile(path, []byte("synthetic"), 0600) != nil {
				t.Fatal("request")
			}
			f, e := os.Open(path)
			if e != nil {
				t.Fatal("request object")
			}
			id, ok := skillsHandleID(syscall.Handle(f.Fd()))
			f.Close()
			if !ok {
				t.Fatal("request ID")
			}
			w := skillsWriter{Task: 3080, Job: "20300101-000000-todo-3080", Workspace: workspace, SpecSHA: hashBytes([]byte("spec"))}
			calls := 0
			create := func() (string, string, bool) {
				calls++
				p := filepath.Join(workspace, "allocation-"+strings.Repeat("x", calls))
				if os.Mkdir(p, 0700) != nil {
					t.Fatal("controlled allocation")
				}
				return "", "", false
			}
			fs := skillsFS{blocked: func(string) bool { return false }}
			_, _, _, attempted := skillsRuntimeSourceAllocateOnce(&fs, w, id, 6291, hashBytes([]byte("request")), create)
			fs.close()
			if !attempted || calls != 1 {
				t.Fatal("unknown allocation control")
			}
			record := filepath.Join(workspace, ".ci-work", "skills3080-source-attempt-"+skillsProjectionSHA(object{"writer_job": w.Job, "request_object": id})+".json")
			if os.Remove(record) != nil {
				t.Fatal("controlled deletion after handles close")
			}
			if _, e := os.Stat(record); !os.IsNotExist(e) {
				t.Fatal("record still present")
			}
			var cli bytes.Buffer
			code := runSkillsCLI([]string{"--prepare-skills3080-runtime-source", path}, &cli, func(string, string) object {
				if outcome == "legacy_failure" {
					return object{"qualified": false, "reason": "source", "failure_stage": "source_owned_root"}
				}
				if outcome == "empty_success" {
					return object{"qualified": true, "general_access_granted": false, "original_registry_recovered": false, "data_access_granted": false}
				}
				if outcome == "invalid_success" {
					return object{"qualified": true, "general_access_granted": false, "original_registry_recovered": false, "data_access_granted": false, "operation": "prepare_runtime_source", "runtime_source_preparation": object{"task": 6291}}
				}
				return skillsRuntimeSourceFailure("source_owned_root", "", "", true, id)
			})
			if code != 1 && outcome != "empty_success" && outcome != "invalid_success" {
				t.Fatal("unknown allocation must fail without fabricating ownership")
			}
			wrapper, _ := json.Marshal(object{"exit_code": code, "output": cli.String()})
			start := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
			thread := "11111111-1111-7111-8111-111111111111"
			flag := "--prepare-skills3080-runtime-source"
			var native bytes.Buffer
			enc := json.NewEncoder(&native)
			enc.Encode(object{"type": "session_meta", "payload": object{"id": thread, "cwd": workspace}})
			enc.Encode(object{"type": "response_item", "timestamp": start.Add(time.Second).Format(time.RFC3339Nano), "payload": object{"type": "custom_tool_call", "name": "exec", "call_id": "call_prior", "input": skillsRuntimeSourceInvocationText(path, workspace, flag)}})
			if outcome != "missing_output" {
				output := any([]object{{"type": "input_text", "text": "Script completed\nWall time 0.5 seconds\nOutput:\n"}, {"type": "input_text", "text": string(wrapper)}})
				if outcome == "malformed_output" {
					output = "unknown outcome"
				}
				enc.Encode(object{"type": "response_item", "timestamp": start.Add(2 * time.Second).Format(time.RFC3339Nano), "payload": object{"type": "custom_tool_call_output", "call_id": "call_prior", "output": output}})
			}
			alias := strings.ToUpper(filepath.ToSlash(path))
			enc.Encode(object{"type": "response_item", "timestamp": start.Add(3 * time.Second).Format(time.RFC3339Nano), "payload": object{"type": "custom_tool_call", "name": "exec", "call_id": "call_current", "input": skillsRuntimeSourceInvocationText(alias, workspace, flag)}})
			_, accepted := skillsRuntimeSourcePendingCall(&native, thread, workspace, alias, flag, start, start.Add(time.Minute), id)
			if accepted {
				again := skillsFS{blocked: func(string) bool { return false }}
				skillsRuntimeSourceAllocateOnce(&again, w, id, 6291, hashBytes([]byte("request")), create)
				again.close()
			}
			if accepted || calls != 1 {
				t.Fatal("deleting the attempt record enabled another allocation through a spelling alias")
			}
		})
	}
}

func TestSkills3080AttemptWriteMustFinishBeforeCreator(t *testing.T) {
	workspace := skillsFixtureRoot(t) // no .ci-work: do not create an unowned folder
	w := skillsWriter{Task: 3080, Job: "20300101-000000-todo-3080", Workspace: workspace, SpecSHA: hashBytes([]byte("spec"))}
	fs := skillsFS{blocked: func(string) bool { return false }}
	defer fs.close()
	calls := 0
	_, _, _, claimed := skillsRuntimeSourceAllocateOnce(&fs, w, skillsObjectID{"123", strings.Repeat("1", 32)}, 6291, hashBytes([]byte("body")), func() (string, string, bool) { calls++; return "", "", false })
	if claimed || calls != 0 {
		t.Fatal("creator ran without a durable CreateNew attempt record")
	}
}
