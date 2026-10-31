package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func skillsRuntimeOriginFixture() (skillsTask, object, time.Time, map[string]object) {
	start := time.Date(2030, 1, 1, 6, 0, 0, 0, time.UTC)
	id := "11111111-1111-7111-8111-111111111111"
	oldID, newID := "20300101-010101-todo-6296", "20300101-020202-todo-6296"
	current := object{"id": newID, "todo_id": json.Number("6296"), "thread_id": id, "resume_session": id, "cwd": "D:/CC/Work/synthetic-consumer", "sandbox": "read-only", "model": "gpt-6-luna", "effort": "max", "started_at": start.Format(time.RFC3339Nano)}
	prior := object{"id": oldID, "todo_id": json.Number("6296"), "thread_id": id, "cwd": current["cwd"], "sandbox": "read-only", "model": "gpt-6-luna", "effort": "max", "status": "succeeded", "started_at": start.Add(-4 * time.Hour).Format(time.RFC3339Nano), "finished_at": start.Add(-3 * time.Hour).Format(time.RFC3339Nano)}
	return skillsTask{ID: 6296, Jobs: []string{oldID, newID}}, current, start, map[string]object{oldID: prior}
}

func TestSkills3080RuntimeSourceOriginNeedsTheRegisteredContinuation(t *testing.T) {
	for _, kind := range []string{"continued", "fresh", "missing_first_started", "false_first_started", "missing_prior", "unregistered_prior", "unregistered_current", "duplicate_current", "duplicate_prior", "wrong_resume", "wrong_task", "wrong_prior_task", "wrong_prior_id", "wrong_thread", "wrong_cwd", "wrong_sandbox", "wrong_model", "wrong_effort", "running_prior", "prior_after_current", "overlapping_prior", "imported_initial_resume", "two_continuations", "broken_middle_resume", "wrong_current_start"} {
		t.Run(kind, func(t *testing.T) {
			row, job, start, jobs := skillsRuntimeOriginFixture()
			old := row.Jobs[0]
			prior := jobs[old]
			switch kind {
			case "fresh":
				delete(job, "resume_session")
				row.Jobs = row.Jobs[1:]
			case "missing_first_started":
				delete(job, "first_started_at")
			case "false_first_started":
				job["first_started_at"] = "2000-01-01T00:00:00Z"
			case "missing_prior":
				delete(jobs, old)
			case "unregistered_prior":
				row.Jobs = row.Jobs[1:]
			case "unregistered_current":
				row.Jobs = row.Jobs[:1]
			case "duplicate_current":
				row.Jobs = append(row.Jobs, row.Jobs[1])
			case "duplicate_prior":
				row.Jobs = append([]string{old}, row.Jobs...)
			case "wrong_resume":
				job["resume_session"] = "22222222-2222-7222-8222-222222222222"
			case "wrong_task":
				job["todo_id"] = json.Number("4297")
			case "wrong_prior_task":
				prior["todo_id"] = json.Number("4297")
			case "wrong_prior_id":
				prior["id"] = "20300101-010101-todo-6297"
			case "wrong_thread":
				prior["thread_id"] = "22222222-2222-7222-8222-222222222222"
			case "wrong_cwd":
				prior["cwd"] = "D:/CC/Work/other"
			case "wrong_sandbox":
				prior["sandbox"] = "danger-full-access"
			case "wrong_model":
				prior["model"] = "gpt-6.1-sol"
			case "wrong_effort":
				prior["effort"] = "high"
			case "running_prior":
				prior["status"] = "running"
			case "prior_after_current":
				prior["started_at"] = start.Add(time.Second).Format(time.RFC3339Nano)
			case "overlapping_prior":
				prior["finished_at"] = start.Add(time.Second).Format(time.RFC3339Nano)
			case "imported_initial_resume":
				prior["resume_session"] = prior["thread_id"]
			case "wrong_current_start":
				job["started_at"] = start.Add(time.Second).Format(time.RFC3339Nano)
			case "two_continuations", "broken_middle_resume":
				midID := "20300101-030303-todo-6296"
				mid := object{}
				for k, v := range prior {
					mid[k] = v
				}
				mid["id"] = midID
				mid["resume_session"] = job["thread_id"]
				mid["started_at"] = start.Add(-2 * time.Hour).Format(time.RFC3339Nano)
				mid["finished_at"] = start.Add(-time.Hour).Format(time.RFC3339Nano)
				if kind == "broken_middle_resume" {
					delete(mid, "resume_session")
				}
				jobs[midID] = mid
				row.Jobs = []string{old, midID, str(job["id"])}
			}
			got, ok := skillsRuntimeSourceStartWith(row, job, start, func(id string) (object, bool) { j, ok := jobs[id]; return j, ok })
			want := kind == "continued" || kind == "fresh" || kind == "missing_first_started" || kind == "false_first_started" || kind == "two_continuations"
			if ok != want {
				t.Fatalf("registered source origin accepted=%v want=%v", ok, want)
			}
			if want {
				expected := start.Add(-4 * time.Hour)
				if kind == "fresh" {
					expected = start
				}
				if !got.Equal(expected) {
					t.Fatalf("source start %s want %s", got, expected)
				}
			}
		})
	}
}

func TestSkills3080UnregisteredBootstrapClaimsCannotCreateAnOrigin(t *testing.T) {
	row, job, start, jobs := skillsRuntimeOriginFixture()
	row.Jobs = row.Jobs[1:]
	job["first_started_at"] = start.Add(-4 * time.Hour).Format(time.RFC3339Nano)
	job["bootstrap_sha256"] = strings.Repeat("a", 64)
	if _, ok := skillsRuntimeSourceStartWith(row, job, start, func(id string) (object, bool) { j, ok := jobs[id]; return j, ok }); ok {
		t.Fatal("caller claims replaced an independent registered source")
	}
}

func TestSkills3080ResumedRuntimeSourceAndCurrentTaskHaveSeparateWindows(t *testing.T) {
	row, job, start, jobs := skillsRuntimeOriginFixture()
	project := []byte("PUBLIC project instructions")
	body, _ := skillsAssembleBootstrapSource([]byte("initial independently captured policy"), project)
	initial := start.Add(-4 * time.Hour)
	end := start.Add(time.Minute)
	id := str(job["thread_id"])
	turn := "22222222-2222-7222-8222-222222222222"
	msg := func(role, phase, text string, at time.Time) object {
		p := object{"type": "message", "role": role, "phase": phase, "content": []object{{"type": "input_text", "text": text}}}
		if role == "assistant" {
			tid := turn
			if !at.Before(start) {
				tid = "33333333-3333-7333-8333-333333333333"
			}
			p["id"] = "msg_" + hashBytes([]byte(text + at.String()))[:20]
			p["internal_chat_message_metadata_passthrough"] = object{"turn_id": tid}
		}
		return object{"type": "response_item", "timestamp": at.Format(time.RFC3339Nano), "payload": p}
	}
	records := []object{
		{"type": "session_meta", "timestamp": initial.Format(time.RFC3339Nano), "payload": object{"id": id, "cwd": job["cwd"], "source": "exec", "originator": "codex_exec", "cli_version": "0.165.0"}},
		{"type": "event_msg", "timestamp": initial.Format(time.RFC3339Nano), "payload": object{"type": "task_started", "turn_id": turn}},
		msg("user", "", skillsBootstrapFixture(str(job["cwd"]), string(body), true), initial),
		{"type": "world_state", "timestamp": initial.Format(time.RFC3339Nano), "payload": object{"full": true, "state": object{"agents_md": object{"directory": job["cwd"], "text": string(body)}}}},
		{"type": "turn_context", "timestamp": initial.Format(time.RFC3339Nano), "payload": object{"turn_id": turn, "cwd": job["cwd"], "model": "gpt-6-luna", "effort": "max"}},
		msg("user", "", "old different task", initial.Add(time.Second)), msg("assistant", "final_answer", "old different final", initial.Add(2*time.Second)),
		{"type": "turn_context", "timestamp": start.Format(time.RFC3339Nano), "payload": object{"turn_id": "33333333-3333-7333-8333-333333333333", "cwd": job["cwd"], "model": "gpt-6-luna", "effort": "max"}},
		msg("user", "", "registered current task", start.Add(time.Second)), msg("assistant", "final_answer", "current original final", start.Add(2*time.Second)),
	}
	var raw bytes.Buffer
	for _, r := range skillsCompleteRuntimeFixture(records) {
		b, _ := json.Marshal(r)
		raw.Write(b)
		raw.WriteByte('\n')
	}
	origin := func() (skillsRuntimeOrigin, bool) {
		return skillsRuntimeSourceOriginWith(row, job, start, func(id string) (object, bool) { j, ok := jobs[id]; return j, ok })
	}
	if _, _, ok := skillsRuntimeBootstrapProjection(bytes.NewReader(raw.Bytes()), job, project, start, end); ok {
		t.Fatal("unproven earlier source was accepted")
	}
	context, contextOK := skillsRuntimeSourceOriginWith(row, job, start, func(id string) (object, bool) { j, ok := jobs[id]; return j, ok })
	for i := range context.Windows {
		context.Windows[i].Input = []byte("old different task")
	}
	if !contextOK {
		t.Fatal("normal context")
	}
	source, modern, ok := skillsRuntimeBootstrapProjectionWithOrigin(bytes.NewReader(raw.Bytes()), job, project, start, end, origin)
	if !ok || !modern || !bytes.Equal(source, body) {
		t.Fatal("normal registered same-session continuation lost its independent source")
	}
	t.Run("incomplete_checkpoint_and_missing_developer", func(t *testing.T) {
		var prefix bytes.Buffer
		for _, original := range skillsCompleteRuntimeFixture(records) {
			b, _ := json.Marshal(original)
			var r object
			json.Unmarshal(b, &r)
			if r["type"] == "response_item" && obj(r["payload"])["role"] == "developer" {
				continue
			}
			if r["type"] == "world_state" {
				st := obj(obj(r["payload"])["state"])
				for _, key := range []string{"model", "permissions", "collaboration_mode", "host_skills", "top_level_tools"} {
					delete(st, key)
				}
			}
			json.NewEncoder(&prefix).Encode(r)
		}
		if _, _, ok := skillsRuntimeBootstrapProjectionWithOrigin(&prefix, job, project, start, end, origin); ok {
			t.Fatal("incomplete runtime checkpoint and missing rendered content qualified")
		}
	})
	for _, kind := range []string{"source_meta_in_gap", "source_meta_claims_current", "bootstrap_after_source_end"} {
		t.Run(kind, func(t *testing.T) {
			var prefix bytes.Buffer
			prefixRecords := []object{}
			firstEnd := context.Windows[len(context.Windows)-1].End
			for i := 0; i < 5; i++ {
				r := object{}
				b, _ := json.Marshal(records[i])
				json.Unmarshal(b, &r)
				if kind != "bootstrap_after_source_end" || i > 0 {
					at := firstEnd.Add(time.Second)
					if kind == "source_meta_claims_current" {
						at = start
					}
					r["timestamp"] = at.Format(time.RFC3339Nano)
				}
				prefixRecords = append(prefixRecords, r)
			}
			for _, r := range skillsCompleteRuntimeFixture(prefixRecords) {
				json.NewEncoder(&prefix).Encode(r)
			}
			if _, _, ok := skillsRuntimeBootstrapProjectionWithOrigin(&prefix, job, project, start, end, origin); ok {
				t.Fatal("source prefix escaped its original registered window")
			}
		})
	}
	// Real Windows snapshot, bootstrap projection and current-task projection
	// share the same frozen handle; only the metadata store is a test seam.
	home := skillsFixtureRoot(t)
	t.Setenv("USERPROFILE", home)
	dir := filepath.Join(home, ".codex", "sessions", "2030", "01", "01")
	if e := os.MkdirAll(dir, 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(dir, "rollout-fixture-"+id+".jsonl"), raw.Bytes(), 0600); e != nil {
		t.Fatal(e)
	}
	job["status"] = "succeeded"
	fs := skillsFS{blocked: func(string) bool { return false }}
	defer fs.close()
	bound := &skillsNativeResultProof{prompt: []byte("registered current task"), finalSHA: hashBytes([]byte("current original final")), exactInput: true, bootstrapSHA: hashBytes([]byte("older author policy")), runtimeProject: project, requireRuntimeSource: true, cwd: str(job["cwd"]), sandbox: "read-only", frozen: true}
	observed, valid := skillsNativeWindowWithSourceOrigin(&fs, job, bound, end, func(at time.Time) (skillsRuntimeOrigin, bool) {
		c, ok := skillsRuntimeSourceOriginWith(row, job, at, func(id string) (object, bool) { j, ok := jobs[id]; return j, ok })
		for i := range c.Windows {
			c.Windows[i].Input = []byte("old different task")
		}
		return c, ok
	})
	if !valid || len(observed) != 1 || !bound.runtimeSourceUsed {
		t.Fatal("frozen registered-resume proof did not reach current result")
	}
	for _, kind := range []string{"registered", "inserted_prior_user", "inserted_prior_event", "unregistered_origin", "retimed_session_meta", "bootstrap_moved_to_current", "bootstrap_retimed_to_current", "bootstrap_copied_to_current"} {
		t.Run("legacy_0163_"+kind, func(t *testing.T) {
			legacy := bytes.Replace(skillsLegacyRuntimeFixture(raw.Bytes()), []byte(`"cli_version":"0.165.0"`), []byte(`"cli_version":"0.163.0"`), 1)
			if kind == "retimed_session_meta" {
				legacy = bytes.Replace(legacy, []byte(initial.Format(time.RFC3339Nano)), []byte(start.Format(time.RFC3339Nano)), 1)
			}
			if strings.HasPrefix(kind, "bootstrap_") {
				var rewritten bytes.Buffer
				var moved object
				for _, line := range bytes.Split(legacy, []byte{'\n'}) {
					if len(line) == 0 {
						continue
					}
					var r object
					json.Unmarshal(line, &r)
					if r["type"] == "response_item" && obj(r["payload"])["role"] == "user" && moved == nil {
						moved = r
						moved["timestamp"] = start.Format(time.RFC3339Nano)
						if kind == "bootstrap_retimed_to_current" {
							json.NewEncoder(&rewritten).Encode(moved)
							continue
						}
						if kind == "bootstrap_moved_to_current" {
							continue
						}
						r = object{}
						json.Unmarshal(line, &r)
					}
					if r["type"] == "turn_context" && r["timestamp"] == start.Format(time.RFC3339Nano) && kind != "bootstrap_retimed_to_current" {
						json.NewEncoder(&rewritten).Encode(moved)
					}
					json.NewEncoder(&rewritten).Encode(r)
				}
				legacy = rewritten.Bytes()
			}
			if kind == "inserted_prior_user" || kind == "inserted_prior_event" {
				v := msg("user", "", "unregistered prior instruction", initial.Add(3*time.Second))
				if kind == "inserted_prior_event" {
					v = object{"type": "event_msg", "timestamp": initial.Add(3 * time.Second).Format(time.RFC3339Nano), "payload": object{"type": "user_message", "message": "unregistered prior instruction"}}
				}
				x, _ := json.Marshal(v)
				boundary, _ := json.Marshal(records[7])
				at := bytes.Index(legacy, boundary)
				if at < 0 {
					t.Fatal("current boundary")
				}
				prefix := append([]byte{}, legacy[:at]...)
				prefix = append(prefix, append(x, '\n')...)
				legacy = append(prefix, legacy[at:]...)
			}
			home := skillsFixtureRoot(t)
			t.Setenv("USERPROFILE", home)
			dir := filepath.Join(home, ".codex", "sessions", "2030", "01", "01")
			if os.MkdirAll(dir, 0700) != nil || os.WriteFile(filepath.Join(dir, "rollout-legacy-"+id+".jsonl"), legacy, 0600) != nil {
				t.Fatal("legacy fixture")
			}
			fs := skillsFS{blocked: func(string) bool { return false }}
			defer fs.close()
			p := &skillsNativeResultProof{prompt: []byte("registered current task"), finalSHA: hashBytes([]byte("current original final")), exactInput: true, bootstrapSHA: hashBytes(body), runtimeProject: project, requireRuntimeSource: true, cwd: str(job["cwd"]), sandbox: "read-only", frozen: true}
			_, ok := skillsNativeWindowWithSourceOrigin(&fs, job, p, end, func(at time.Time) (skillsRuntimeOrigin, bool) {
				if kind == "unregistered_origin" {
					return skillsRuntimeOrigin{}, false
				}
				c, ok := skillsRuntimeSourceOriginWith(row, job, at, func(id string) (object, bool) { j, ok := jobs[id]; return j, ok })
				for i := range c.Windows {
					c.Windows[i].Input = []byte("old different task")
				}
				return c, ok
			})
			if ok != (kind == "registered") || (ok && p.runtimeSourceUsed) {
				t.Fatalf("legacy digest/history contract %s accepted=%v", kind, ok)
			}
		})
	}
	for _, kind := range []string{"current", "wrong_current_input", "old_final_digest", "extra_current_user", "extra_current_final", "gap_user", "gap_event", "gap_developer", "backdated_prior_user", "inserted_prior_user", "inserted_prior_event", "inserted_prior_developer", "unverified_initial_developer", "missing_prior_input", "duplicate_prior_event", "valid_prior_context", "wrong_prior_context_model", "wrong_prior_context_cwd", "wrong_prior_context_effort", "unregistered_gap_context", "valid_prior_assistant", "missing_prior_generated_turn", "wrong_prior_generated_turn", "duplicate_prior_generated_id", "orphan_prior_tool_output", "valid_prior_tool_pair", "wrong_prior_tool_pair", "unmatched_prior_tool_call", "valid_prior_configuration", "wrong_prior_configuration", "extra_prior_configuration"} {
		t.Run(kind, func(t *testing.T) {
			b := raw.Bytes()
			prompt, final := []byte("registered current task"), []byte("current original final")
			switch kind {
			case "valid_prior_configuration", "wrong_prior_configuration", "extra_prior_configuration":
				payload := object{"type": "configuration_update", "reasoning": object{"effort": "max"}}
				if kind == "wrong_prior_configuration" {
					obj(payload["reasoning"])["effort"] = "low"
				}
				if kind == "extra_prior_configuration" {
					payload["other"] = "unregistered"
				}
				x, _ := json.Marshal(object{"type": "response_item", "timestamp": initial.Add(3 * time.Second).Format(time.RFC3339Nano), "payload": payload})
				boundary, _ := json.Marshal(records[7])
				pos := bytes.Index(b, boundary)
				if pos < 0 {
					t.Fatal("boundary")
				}
				prefix := append([]byte{}, b[:pos]...)
				prefix = append(prefix, append(x, '\n')...)
				b = append(prefix, b[pos:]...)
			case "valid_prior_assistant", "missing_prior_generated_turn", "wrong_prior_generated_turn", "duplicate_prior_generated_id", "orphan_prior_tool_output", "valid_prior_tool_pair", "wrong_prior_tool_pair", "unmatched_prior_tool_call":
				at := initial.Add(3 * time.Second)
				v := msg("assistant", "commentary", "generated history", at)
				if kind == "missing_prior_generated_turn" {
					delete(obj(v["payload"]), "internal_chat_message_metadata_passthrough")
				}
				if kind == "wrong_prior_generated_turn" {
					obj(v["payload"])["internal_chat_message_metadata_passthrough"] = object{"turn_id": id}
				}
				if kind == "duplicate_prior_generated_id" {
					obj(v["payload"])["id"] = obj(records[6]["payload"])["id"]
				}
				insert := []object{v}
				if strings.Contains(kind, "tool_") {
					call := object{"type": "response_item", "timestamp": at.Format(time.RFC3339Nano), "payload": object{"type": "custom_tool_call", "id": "tool_call_item", "name": "exec", "call_id": "call_prior_bound", "input": "opaque", "internal_chat_message_metadata_passthrough": object{"turn_id": turn}}}
					output := object{"type": "response_item", "timestamp": at.Add(time.Second).Format(time.RFC3339Nano), "payload": object{"type": "custom_tool_call_output", "id": "tool_output_item", "call_id": "call_prior_bound", "output": "opaque", "internal_chat_message_metadata_passthrough": object{"turn_id": turn}}}
					insert = []object{call, output}
					if kind == "orphan_prior_tool_output" {
						insert = insert[1:]
					}
					if kind == "unmatched_prior_tool_call" {
						insert = insert[:1]
					}
					if kind == "wrong_prior_tool_pair" {
						obj(output["payload"])["call_id"] = "call_other"
					}
				}
				var added bytes.Buffer
				for _, r := range insert {
					json.NewEncoder(&added).Encode(r)
				}
				boundary, _ := json.Marshal(records[7])
				pos := bytes.Index(b, boundary)
				if pos < 0 {
					t.Fatal("current boundary")
				}
				prefix := append([]byte{}, b[:pos]...)
				prefix = append(prefix, added.Bytes()...)
				b = append(prefix, b[pos:]...)
			case "valid_prior_context", "wrong_prior_context_model", "wrong_prior_context_cwd", "wrong_prior_context_effort", "unregistered_gap_context":
				at := initial.Add(3 * time.Second)
				if kind == "unregistered_gap_context" {
					at = start.Add(-2 * time.Hour)
				}
				payload := object{"turn_id": turn, "cwd": job["cwd"], "model": "gpt-6-luna", "effort": "max"}
				if kind == "wrong_prior_context_model" {
					payload["model"] = "gpt-6.1-sol"
				}
				if kind == "wrong_prior_context_cwd" {
					payload["cwd"] = "D:/CC/Work/other"
				}
				if kind == "wrong_prior_context_effort" {
					payload["effort"] = "low"
				}
				x, _ := json.Marshal(object{"type": "turn_context", "timestamp": at.Format(time.RFC3339Nano), "payload": payload})
				boundary, _ := json.Marshal(records[7])
				pos := bytes.Index(b, boundary)
				if pos < 0 {
					t.Fatal("current boundary")
				}
				prefix := append([]byte{}, b[:pos]...)
				prefix = append(prefix, append(x, '\n')...)
				b = append(prefix, b[pos:]...)
			case "unverified_initial_developer":
				dev := msg("developer", "", "normal runtime envelope", initial)
				copied := append([]object{}, records[:2]...)
				copied = append(copied, dev)
				copied = append(copied, records[2:]...)
				var buf bytes.Buffer
				for _, v := range skillsCompleteRuntimeFixture(copied) {
					x, _ := json.Marshal(v)
					buf.Write(x)
					buf.WriteByte('\n')
				}
				b = buf.Bytes()
			case "duplicate_prior_event":
				at := initial.Add(time.Second)
				v := object{"type": "event_msg", "timestamp": at.Format(time.RFC3339Nano), "payload": object{"type": "user_message", "message": "old different task", "images": []any{}, "local_images": []any{}, "text_elements": []any{}}}
				x, _ := json.Marshal(v)
				firstCurrent, _ := json.Marshal(records[7])
				boundary := bytes.Index(b, firstCurrent)
				if boundary < 0 {
					t.Fatal("current boundary")
				}
				prefix := append([]byte{}, b[:boundary]...)
				prefix = append(prefix, append(x, '\n')...)
				prefix = append(prefix, append(x, '\n')...)
				b = append(prefix, b[boundary:]...)
			case "wrong_current_input":
				prompt = []byte("unregistered replacement")
			case "old_final_digest":
				final = []byte("old different final")
			case "gap_user", "gap_developer", "backdated_prior_user", "gap_event", "inserted_prior_user", "inserted_prior_event", "inserted_prior_developer":
				at := start.Add(-2 * time.Hour)
				role := "user"
				if kind == "gap_developer" || kind == "inserted_prior_developer" {
					role = "developer"
				}
				if kind == "backdated_prior_user" || strings.HasPrefix(kind, "inserted_prior_") {
					at = initial.Add(3 * time.Second)
				}
				value := msg(role, "", "unregistered outside task", at)
				if kind == "gap_event" || kind == "inserted_prior_event" {
					value = object{"type": "event_msg", "timestamp": at.Format(time.RFC3339Nano), "payload": object{"type": "user_message", "message": "unregistered outside task", "images": []any{}, "local_images": []any{}, "text_elements": []any{}}}
				}
				extra, _ := json.Marshal(value)
				if kind == "backdated_prior_user" {
					b = append(append([]byte{}, b...), append(extra, '\n')...)
				} else {
					firstCurrent, _ := json.Marshal(records[7])
					at := bytes.Index(b, firstCurrent)
					if at < 0 {
						t.Fatal("current fixture boundary")
					}
					withGap := append([]byte{}, b[:at]...)
					withGap = append(withGap, append(extra, '\n')...)
					b = append(withGap, b[at:]...)
				}
			case "extra_current_user", "extra_current_final":
				role, phase, text := "user", "", "unregistered additional task"
				if kind == "extra_current_final" {
					role, phase, text = "assistant", "final_answer", "another final"
				}
				extra, _ := json.Marshal(msg(role, phase, text, start.Add(3*time.Second)))
				b = append(append([]byte{}, b...), append(extra, '\n')...)
			}
			envelope := map[string]bool{}
			_, _, projected := skillsRuntimeBootstrapProjectionWithOrigin(bytes.NewReader(b), job, project, start, end, origin, envelope)
			if kind == "unverified_initial_developer" {
				if projected {
					t.Fatal("unverified initial developer qualified its own record hash")
				}
				return
			}
			proof := &skillsNativeResultProof{prompt: prompt, finalSHA: hashBytes(final), exactInput: true, bootstrapSHA: hashBytes([]byte("older author policy")), runtimeBootstrap: source, runtimeSourceUsed: true, runtimePriorWindows: append([]skillsRuntimePriorWindow{}, context.Windows...), runtimeEnvelopeRecords: envelope, bootstrapBound: true, cwd: str(job["cwd"]), sandbox: "read-only"}
			if kind == "missing_prior_input" {
				proof.runtimePriorWindows[0].Input = nil
			}
			for i := range proof.runtimePriorWindows {
				proof.runtimePriorWindows[i].proof = &skillsNativeResultProof{prompt: proof.runtimePriorWindows[i].Input, inputOnly: true, exactInput: true, runtimeBootstrap: source, bootstrapBound: true, cwd: str(job["cwd"]), sandbox: "read-only"}
			}
			_, ok := skillsNativeProjectionBound(bytes.NewReader(b), id, str(job["cwd"]), start, end, proof)
			if ok != (kind == "current" || kind == "valid_prior_context" || kind == "valid_prior_assistant" || kind == "valid_prior_tool_pair" || kind == "valid_prior_configuration") {
				t.Fatalf("current task/final isolation %s accepted=%v", kind, ok)
			}
		})
	}
}
