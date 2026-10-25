package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestSkills3080ForwardCLIIsAnOperation(t *testing.T) {
	for _, op := range []string{"create-skills3080-forward", "consume-skills3080"} {
		calls := 0
		var out bytes.Buffer
		if !skillsFlag([]string{"--" + op, "D:/request.json"}) {
			t.Errorf("%s is not dispatched to the purpose boundary", op)
		}
		code := runSkillsCLI([]string{"--" + op, "D:/request.json"}, &out, func(got, _ string) object {
			calls++
			return object{"qualified": got == op}
		})
		if code != 0 || calls != 1 {
			t.Errorf("%s: exit=%d calls=%d", op, code, calls)
		}
	}
}

func skillsForwardFixture(t *testing.T) (skillsManifest, skillsRequest, skillsCreatedEvidence, string) {
	t.Helper()
	m := skillsParsedFixture(t)
	root := skillsFixtureRoot(t)
	fs := skillsFS{blocked: func(string) bool { return false }}
	defer fs.close()
	r := skillsRequest{Domain: "skills-3080-operation-v1", Writer: m.Writer, ManifestCommit: strings.Repeat("a", 40),
		ManifestPath: "docs/skills-3080-operations/synthetic.json", ManifestSHA: strings.Repeat("b", 64),
		Review: &skillsResultRequest{Task: 3800, Job: "20261025-100100-todo-3800", SpecSHA: strings.Repeat("c", 64)}}
	producer := skillsProducer{strings.Repeat("d", 64), strings.Repeat("e", 40)}
	if _, ok := skillsIssueCreated(r, strings.Repeat("f", 64), m, root, &fs, producer); ok {
		t.Fatal("uncreated old output acquired issuance")
	}
	if n, reason := skillsCreate(m, root, &fs); reason != "" || n != len(m.Files) {
		t.Fatalf("create %d/%s", n, reason)
	}
	e, ok := skillsIssueCreated(r, strings.Repeat("f", 64), m, root, &fs, producer)
	if !ok || !skillsCreatedBound(e, r, e.RequestSHA, m, producer) {
		t.Fatal("same-handle issuance/binding failed")
	}
	return m, r, e, root
}

func TestSkills3080ForwardConsumesActualPinnedBuffer(t *testing.T) {
	m, _, e, root := skillsForwardFixture(t)
	fs := skillsFS{blocked: func(string) bool { return false }}
	defer fs.close()
	result, reason := skillsConsumeCurrent(m, e, root, &fs, []int{0})
	if reason != "" || result["qualified"] != true || result["same_read_buffer_consumed"] != true ||
		result["historical_physical_continuity_proven"] != false || result["current_os_writer"] != "unproved" ||
		result["general_access_granted"] != false || result["original_registry_recovered"] != false {
		t.Fatalf("unexpected %v/%s", result, reason)
	}
	files := result["files"].([]object)
	if len(files) != 1 || files[0]["content_utf8"] != string(m.Files[0].body) {
		t.Fatal("actual bytes lost")
	}
	// Attempts to change or substitute the validated object fail until the
	// consumer has finished with the very buffer returned above.
	path := filepath.Join(root, filepath.FromSlash(m.Files[0].Target))
	if f, err := os.OpenFile(path, os.O_WRONLY, 0); err == nil {
		f.Close()
		t.Fatal("write sharing was granted")
	}
	if err := os.Rename(path, filepath.Join(root, "substitution.txt")); err == nil {
		t.Fatal("delete sharing was granted")
	}
}

func TestSkills3080ForwardCurrentNegativeCases(t *testing.T) {
	for _, name := range []string{"changed_bytes", "byte_identical_replacement", "hardlink", "other_root", "other_parent", "protected_root", "protected_leaf", "bad_index", "duplicate_index"} {
		t.Run(name, func(t *testing.T) {
			m, _, e, root := skillsForwardFixture(t)
			path := filepath.Join(root, filepath.FromSlash(m.Files[0].Target))
			indices := []int{0}
			block := func(string) bool { return false }
			switch name {
			case "changed_bytes":
				body := append([]byte(nil), m.Files[0].body...)
				body[0] ^= 1
				if err := os.WriteFile(path, body, 0600); err != nil {
					t.Fatal(err)
				}
			case "byte_identical_replacement":
				if err := os.Rename(path, filepath.Join(root, "retained-original.txt")); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, m.Files[0].body, 0600); err != nil {
					t.Fatal(err)
				}
			case "hardlink":
				if err := os.Link(path, filepath.Join(root, "alias.txt")); err != nil {
					t.Fatal(err)
				}
			case "other_root":
				root = skillsFixtureRoot(t)
			case "other_parent":
				parent := filepath.Dir(path)
				if err := os.Rename(parent, filepath.Join(root, "retained-parent")); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(parent, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, m.Files[0].body, 0600); err != nil {
					t.Fatal(err)
				}
			case "protected_root":
				block = func(p string) bool { return same(p, root) }
			case "protected_leaf":
				block = func(p string) bool { return same(p, path) }
			case "bad_index":
				indices = []int{len(m.Files)}
			case "duplicate_index":
				indices = []int{0, 0}
			}
			fs := skillsFS{blocked: block}
			defer fs.close()
			result, reason := skillsConsumeCurrent(m, e, root, &fs, indices)
			if reason == "" || result != nil {
				t.Fatal("negative returned body")
			}
			if strings.HasPrefix(name, "protected_") && (reason != "protection" || len(fs.files) != 0 || len(fs.dirs) != 0) {
				t.Fatal("protection must precede target IO")
			}
		})
	}
}

func TestSkills3080ForwardEvidenceCannotBeRebound(t *testing.T) {
	m, r, e, _ := skillsForwardFixture(t)
	for _, name := range []string{"writer", "job", "spec", "manifest", "review", "request", "producer", "old_receipt", "missing_file", "alias", "hash", "ID"} {
		t.Run(name, func(t *testing.T) {
			raw, _ := json.Marshal(e)
			var copy skillsCreatedEvidence
			if !skillsStrict(raw, &copy) {
				t.Fatal("copy fixture")
			}
			switch name {
			case "writer":
				copy.Writer.Task = 3686
			case "job":
				copy.Writer.Job = "20261025-110000-todo-3080"
			case "spec":
				copy.Writer.SpecSHA = strings.Repeat("9", 64)
			case "manifest":
				copy.ManifestSHA = strings.Repeat("2", 64)
			case "review":
				copy.Review.Task++
			case "request":
				copy.RequestSHA = strings.Repeat("3", 64)
			case "producer":
				copy.Producer.SHA = strings.Repeat("4", 64)
			case "old_receipt":
				copy.Domain = "skills-3080-purpose-v1"
			case "missing_file":
				copy.Files = nil
			case "alias":
				copy.Files[0].Target = "other/SKILL.md"
			case "hash":
				copy.Files[0].SHA = strings.Repeat("5", 64)
			case "ID":
				copy.Files[0].ID.File = "legacy64"
			}
			if skillsCreatedBound(copy, r, e.RequestSHA, m, e.Producer) {
				t.Fatal("rebound receipt accepted")
			}
		})
	}
}

func skillsNativeCreationFixture(t *testing.T, e skillsCreatedEvidence, c skillsConsumeRequest) []object {
	t.Helper()
	stamp := "2026-10-25T12:00:01Z"
	output, _ := json.Marshal(object{"qualified": true, "operation": "CreateNew", "request_sha256": e.RequestSHA,
		"manifest_sha256": e.ManifestSHA, "source_commit": e.ManifestCommit, "file_count": len(e.Files), "created_files": len(e.Files),
		"general_access_granted": false, "original_registry_recovered": false, "creation_evidence": e})
	wrapper, _ := json.Marshal(object{"exit_code": 0, "wall_time_seconds": 1.0, "original_token_count": 900, "output": string(output) + "\n"})
	return []object{
		{"type": "session_meta", "timestamp": stamp, "payload": object{"id": "synthetic-thread", "cwd": c.Creator.Workspace}},
		{"type": "response_item", "timestamp": stamp, "payload": object{"type": "custom_tool_call", "call_id": c.CallID, "name": "exec", "input": skillsForwardInvocation(c.RequestPath, c.Creator.Workspace)}},
		{"type": "response_item", "timestamp": "2026-10-25T12:00:02Z", "payload": object{"type": "custom_tool_call_output", "call_id": c.CallID, "output": []object{{"type": "input_text", "text": "Script completed\nWall time 1.0 seconds\nOutput:\n"}, {"type": "input_text", "text": string(wrapper)}}}},
	}
}

func TestSkills3080ForwardCanonicalCreationNative(t *testing.T) {
	_, _, e, _ := skillsForwardFixture(t)
	c := skillsConsumeRequest{Creator: e.Writer, RequestPath: e.Writer.Workspace + "/.ci-work/forward-request.json", RequestSHA: e.RequestSHA, CallID: "call_forward_fixture"}
	start, _ := time.Parse(time.RFC3339, "2026-10-25T12:00:00Z")
	end := start.Add(time.Minute)
	for _, name := range []string{"real", "self_attested_json", "forged_echo", "other_path", "other_job", "other_request", "wrong_call", "wrong_thread", "late_response", "early_call", "duplicate_call", "duplicate_output", "failed", "yielded", "truncated", "wrong_native_cwd"} {
		t.Run(name, func(t *testing.T) {
			records := skillsNativeCreationFixture(t, e, c)
			call := obj(records[1]["payload"])
			response := obj(records[2]["payload"])
			switch name {
			case "self_attested_json":
				response["type"] = "message"
			case "forged_echo":
				call["input"] = `text({"qualified":true});`
			case "other_path":
				call["input"] = skillsForwardInvocation(c.RequestPath+".copy", c.Creator.Workspace)
			case "other_job":
				e2 := e
				e2.Writer.Job += "x"
				records = skillsNativeCreationFixture(t, e2, c)
			case "other_request":
				e2 := e
				e2.RequestSHA = strings.Repeat("1", 64)
				records = skillsNativeCreationFixture(t, e2, c)
			case "wrong_call":
				call["call_id"] = "call_unrelated"
			case "wrong_thread":
				obj(records[0]["payload"])["id"] = "unrelated"
			case "wrong_native_cwd":
				obj(records[0]["payload"])["cwd"] = "D:/other"
			case "late_response":
				records[2]["timestamp"] = end.Add(time.Second).Format(time.RFC3339)
			case "early_call":
				records[1]["timestamp"] = start.Add(-time.Second).Format(time.RFC3339)
			case "duplicate_call":
				records = append(records, records[1])
			case "duplicate_output":
				records = append(records, records[2])
			case "failed", "yielded", "truncated":
				blocks := response["output"].([]object)
				w, _ := conversationJSON([]byte(str(blocks[1]["text"])))
				if name == "failed" {
					w["exit_code"] = 1
				}
				if name == "yielded" {
					w["session_id"] = 42
				}
				if name == "truncated" {
					w["output"] = `{"qualified":true`
				}
				b, _ := json.Marshal(w)
				blocks[1]["text"] = string(b)
			}
			var stream bytes.Buffer
			for _, record := range records {
				json.NewEncoder(&stream).Encode(record)
			}
			got, ok := skillsCreationNativeProjection(&stream, "synthetic-thread", c.Creator.Workspace, start, end, c)
			if ok != (name == "real") || (ok && skillsProjectionSHA(got) != skillsProjectionSHA(e)) {
				t.Fatalf("native qualification=%v", ok)
			}
		})
	}
}

func TestSkills3080ForwardCreatorIsARegisteredGeneration(t *testing.T) {
	creator := skillsTestManifest().Writer
	consumer := creator
	consumer.Job = "20261025-235900-todo-3080"
	row := skillsTask{ID: 3080, Project: "cc-pc-tools-rs", Jobs: []string{creator.Job, consumer.Job}}
	row.Scheduler.Job = consumer.Job
	reg := skillsRegistry{Tasks: map[int]skillsTask{3080: row}}
	job := object{"id": creator.Job, "todo_id": json.Number("3080"), "cwd": creator.Workspace, "model": "gpt-6.1-sol", "status": "failed"}
	if !skillsCreationAuthorBound(creator, consumer, reg, job) {
		t.Fatal("normal continuation rejected")
	}
	for _, name := range []string{"unregistered", "future", "wrong_task", "wrong_cwd", "not_started"} {
		t.Run(name, func(t *testing.T) {
			copy := object{}
			for k, v := range job {
				copy[k] = v
			}
			other := row
			switch name {
			case "unregistered":
				other.Jobs = []string{consumer.Job}
			case "future":
				other.Jobs = []string{consumer.Job, creator.Job}
			case "wrong_task":
				copy["todo_id"] = json.Number("3686")
			case "wrong_cwd":
				copy["cwd"] = "D:/other"
			case "not_started":
				copy["status"] = "queued"
			}
			if skillsCreationAuthorBound(creator, consumer, skillsRegistry{Tasks: map[int]skillsTask{3080: other}}, copy) {
				t.Fatal("other generation accepted")
			}
		})
	}
}

func TestSkills3080ForwardIDDoesNotClaimLongTermContinuity(t *testing.T) {
	// An ID observation is not a USN journal, nor proof of the current OS writer.
	// Known public byte equality is independently required by the actual reader.
	m, _, e, root := skillsForwardFixture(t)
	path := filepath.Join(root, filepath.FromSlash(m.Files[0].Target))
	name, _ := syscall.UTF16PtrFromString(path)
	h, err := syscall.CreateFile(name, 0x80, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE, nil, syscall.OPEN_EXISTING, syscall.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		t.Fatal(err)
	}
	id, ok := skillsHandleID(h)
	syscall.CloseHandle(h)
	if !ok || id != e.Files[0].ID {
		t.Fatal("creation ID was not observed from the held handle")
	}
}

func TestSkills3080ForwardRejectsReparseParentBeforeBody(t *testing.T) {
	m, _, e, root := skillsForwardFixture(t)
	parent := filepath.Join(root, "sample")
	retained := filepath.Join(root, "retained")
	if err := os.Rename(parent, retained); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", parent, retained)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("synthetic junction: %v %s", err, output)
	}
	fs := skillsFS{blocked: func(string) bool { return false }}
	defer fs.close()
	result, reason := skillsConsumeCurrent(m, e, root, &fs, []int{0})
	if result != nil || reason != "current_identity" || len(fs.files) != 0 {
		t.Fatal("reparse parent opened a body")
	}
}

func TestSkills3080ForwardRealCreateNativeAndConsume(t *testing.T) {
	m, r, issued, root := skillsForwardFixture(t)
	c := skillsConsumeRequest{Creator: issued.Writer, RequestPath: issued.Writer.Workspace + "/.ci-work/forward-request.json", RequestSHA: issued.RequestSHA, CallID: "call_end_to_end"}
	start, _ := time.Parse(time.RFC3339, "2026-10-25T12:00:00Z")
	var stream bytes.Buffer
	for _, row := range skillsNativeCreationFixture(t, issued, c) {
		json.NewEncoder(&stream).Encode(row)
	}
	e, ok := skillsCreationNativeProjection(&stream, "synthetic-thread", c.Creator.Workspace, start, start.Add(time.Minute), c)
	if !ok || !skillsCreatedBound(e, r, c.RequestSHA, m, issued.Producer) {
		t.Fatal("real synthetic creation proof rejected")
	}
	fs := skillsFS{blocked: func(string) bool { return false }}
	defer fs.close()
	result, reason := skillsConsumeCurrent(m, e, root, &fs, []int{0})
	if reason != "" || result["files"].([]object)[0]["content_utf8"] != string(m.Files[0].body) {
		t.Fatal("creation did not reach the actual consumer")
	}
}

func TestSkills3080ForwardCannotChangeOrdinaryClassification(t *testing.T) {
	for _, legacy := range []ownerLegacyState{ownerLegacyAbsent, ownerLegacyUnavailable, ownerLegacyClient} {
		v := ownerFixturePolicy("D:/unknown-central/SKILL.md", false, legacy)
		if v.AccessAllowed {
			t.Fatal("new purpose route changed an ordinary unknown/protected verdict")
		}
	}
}
