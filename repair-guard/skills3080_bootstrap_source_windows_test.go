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

func TestSkills3080AvailableRuntimeSourceDoesNotDowngrade(t *testing.T) {
	p := &skillsNativeResultProof{bootstrapSHA: hashBytes([]byte("old author instructions")), runtimeBootstrap: []byte("new runtime instructions")}
	if p.bootstrapMatchesBody("old author instructions") {
		t.Fatal("available independent source downgraded to old author")
	}
	if !p.bootstrapMatchesBody("new runtime instructions") {
		t.Fatal("independent source rejected")
	}
	p.runtimeBootstrap = nil
	if !p.bootstrapMatchesBody("old author instructions") {
		t.Fatal("existing contract lost when current source unavailable")
	}
}

func TestSkills3080UserEventsMustMirrorTheOneRegisteredInput(t *testing.T) {
	for _, kind := range []string{"no_event", "matching_event", "different_input", "duplicate_event", "later_same_input", "attachments", "outside_window", "unknown_field"} {
		t.Run(kind, func(t *testing.T) {
			cwd, id := "D:/CC/Work/synthetic", "11111111-1111-7111-8111-111111111111"
			start := time.Date(2026, 10, 30, 14, 0, 0, 0, time.UTC)
			inputAt := start.Add(time.Second)
			msg := func(role, phase, text string, at time.Time) object {
				return object{"type": "response_item", "timestamp": at.Format(time.RFC3339Nano), "payload": object{"type": "message", "role": role, "phase": phase, "content": []object{{"type": "input_text", "text": text}}}}
			}
			records := []object{{"type": "session_meta", "timestamp": start.Format(time.RFC3339Nano), "payload": object{"id": id, "cwd": cwd}}, {"type": "turn_context", "timestamp": start.Format(time.RFC3339Nano), "payload": object{"model": "gpt-6-luna", "effort": "max", "cwd": cwd}}, msg("user", "", skillsBootstrapFixture(cwd, "runtime instructions", true), start), msg("user", "", "exact task", inputAt)}
			event := object{"type": "event_msg", "timestamp": inputAt.Format(time.RFC3339Nano), "payload": object{"type": "user_message", "message": "exact task", "images": []any{}, "local_images": []any{}, "text_elements": []any{}}}
			switch kind {
			case "different_input":
				obj(event["payload"])["message"] = "additional private input"
			case "later_same_input":
				event["timestamp"] = inputAt.Add(time.Second).Format(time.RFC3339Nano)
			case "attachments":
				obj(event["payload"])["images"] = []any{"image"}
			case "outside_window":
				event["timestamp"] = start.Add(-time.Second).Format(time.RFC3339Nano)
			case "unknown_field":
				obj(event["payload"])["extra_instruction"] = "unregistered"
			}
			if kind != "no_event" {
				records = append(records, event)
			}
			if kind == "duplicate_event" {
				records = append(records, event)
			}
			records = append(records, msg("assistant", "final_answer", "original final", start.Add(3*time.Second)))
			var b bytes.Buffer
			for _, v := range records {
				raw, _ := json.Marshal(v)
				b.Write(raw)
				b.WriteByte('\n')
			}
			p := &skillsNativeResultProof{prompt: []byte("exact task"), finalSHA: hashBytes([]byte("original final")), exactInput: true, runtimeBootstrap: []byte("runtime instructions"), runtimeSourceUsed: true, cwd: cwd, sandbox: "read-only"}
			_, ok := skillsNativeProjectionBound(&b, id, cwd, start, start.Add(time.Minute), p)
			if ok != (kind == "no_event" || kind == "matching_event") {
				t.Fatalf("native input event %s accepted=%v", kind, ok)
			}
		})
	}
}

func TestSkills3080MissingPublicProjectCannotDowngradeCurrentRuntime(t *testing.T) {
	home := skillsFixtureRoot(t)
	t.Setenv("USERPROFILE", home)
	cwd, id, turn := "D:/CC/Work/synthetic", "11111111-1111-7111-8111-111111111111", "22222222-2222-7222-8222-222222222222"
	start := time.Date(2026, 10, 30, 14, 0, 0, 0, time.UTC)
	dir := filepath.Join(home, ".codex", "sessions", "2026", "10", "30")
	if e := os.MkdirAll(dir, 0700); e != nil {
		t.Fatal(e)
	}
	project := []byte("PUBLIC project")
	body, _ := skillsAssembleBootstrapSource([]byte("same historical owner body"), project)
	for _, kind := range []string{"current_with_project", "current_missing_project", "known_old", "unknown"} {
		version := "0.165.0"
		if kind == "known_old" {
			version = "0.163.0"
		}
		if kind == "unknown" {
			version = "0.999.0"
		}
		msg := func(text string, at time.Time) object {
			return object{"type": "response_item", "timestamp": at.Format(time.RFC3339Nano), "payload": object{"type": "message", "role": "user", "content": []object{{"type": "input_text", "text": text}}}}
		}
		records := []object{{"type": "session_meta", "timestamp": start.Format(time.RFC3339Nano), "payload": object{"id": id, "cwd": cwd, "source": "exec", "originator": "codex_exec", "cli_version": version}}, {"type": "event_msg", "timestamp": start.Format(time.RFC3339Nano), "payload": object{"type": "task_started", "turn_id": turn}}, msg(skillsBootstrapFixture(cwd, string(body), true), start), {"type": "world_state", "timestamp": start.Format(time.RFC3339Nano), "payload": object{"full": true, "state": object{"agents_md": object{"directory": cwd, "text": string(body)}}}}, {"type": "turn_context", "timestamp": start.Format(time.RFC3339Nano), "payload": object{"turn_id": turn, "cwd": cwd, "model": "gpt-6-luna", "effort": "max"}}, msg("exact task", start.Add(time.Second))}
		var b bytes.Buffer
		for _, v := range records {
			raw, _ := json.Marshal(v)
			b.Write(raw)
			b.WriteByte('\n')
		}
		if e := os.WriteFile(filepath.Join(dir, "rollout-fixture-"+id+".jsonl"), b.Bytes(), 0600); e != nil {
			t.Fatal(e)
		}
		p := &skillsNativeResultProof{prompt: []byte("exact task"), inputOnly: true, exactInput: true, bootstrapSHA: hashBytes(body), requireRuntimeSource: true, cwd: cwd}
		if kind == "current_with_project" {
			p.runtimeProject = project
		}
		job := object{"thread_id": id, "started_at": start.Format(time.RFC3339Nano), "cwd": cwd, "sandbox": "read-only", "model": "gpt-6-luna", "effort": "max"}
		s := skillsFS{blocked: func(string) bool { return false }}
		_, ok := skillsNativeWindow(&s, job, p, start.Add(time.Minute))
		s.close()
		if ok != (kind == "current_with_project" || kind == "known_old") {
			t.Fatalf("missing PUBLIC source %s accepted=%v", kind, ok)
		}
	}
}

func TestSkills3080UnknownUserPayloadIsNeverAnIgnoredEvent(t *testing.T) {
	cwd, id := "D:/CC/Work/synthetic", "11111111-1111-7111-8111-111111111111"
	meta, _ := json.Marshal(object{"type": "session_meta", "payload": object{"id": id, "cwd": cwd}})
	boot, _ := json.Marshal(object{"type": "response_item", "payload": object{"type": "message", "role": "user", "content": []object{{"type": "input_text", "text": skillsBootstrapFixture(cwd, "known", true)}}}})
	unknown := `{"type":"response_item","payload":{"type":"other","role":"user","content":[{"type":"input_text","text":"private extra"}]}}`
	if _, ok := skillsBootstrapBodyProjection(strings.NewReader(string(meta)+"\n"+unknown+"\n"+string(boot)+"\n"), id, cwd, "read-only"); ok {
		t.Fatal("unknown prior user hidden")
	}
	proof := &skillsNativeResultProof{prompt: []byte("exact task"), exactInput: true, cwd: cwd}
	if proof.observe([]byte(unknown), time.Now()) {
		t.Fatal("unknown later user hidden")
	}
}

func TestSkills3080RuntimeWorldStateAnchorsTheLaunchInstructions(t *testing.T) {
	for _, kind := range []string{"valid", "changed_common_version", "runtime_developer_prefix", "unknown_cli", "wrong_source", "wrong_originator", "missing_snapshot", "delta_snapshot", "wrong_snapshot_body", "wrong_directory", "wrong_project", "late_snapshot", "duplicate_snapshot", "user_fake_snapshot", "wrong_turn", "wrong_model", "before_job", "unknown_user_type", "system_before_bootstrap", "unknown_role_before_bootstrap", "developer_after_bootstrap"} {
		t.Run(kind, func(t *testing.T) {
			cwd, id, turn := "D:/CC/Work/synthetic", "11111111-1111-7111-8111-111111111111", "22222222-2222-7222-8222-222222222222"
			start := time.Date(2026, 10, 30, 14, 0, 0, 0, time.UTC)
			project := []byte("public project instructions")
			common := "launch owner instructions"
			if kind == "changed_common_version" {
				common = "new independent launch owner instructions"
			}
			body, _ := skillsAssembleBootstrapSource([]byte(common), project)
			meta := object{"type": "session_meta", "timestamp": start.Format(time.RFC3339Nano), "payload": object{"id": id, "cwd": cwd, "source": "exec", "originator": "codex_exec", "cli_version": "0.165.0"}}
			if kind == "wrong_source" {
				obj(meta["payload"])["source"] = "user"
			}
			if kind == "unknown_cli" {
				obj(meta["payload"])["cli_version"] = "0.999.0"
			}
			if kind == "wrong_originator" {
				obj(meta["payload"])["originator"] = "other"
			}
			if kind == "before_job" {
				meta["timestamp"] = start.Add(-time.Second).Format(time.RFC3339Nano)
			}
			msg := object{"type": "response_item", "timestamp": start.Add(time.Second).Format(time.RFC3339Nano), "payload": object{"type": "message", "role": "user", "content": []object{{"type": "input_text", "text": skillsBootstrapFixture(cwd, string(body), true)}}}}
			if kind == "unknown_user_type" {
				obj(msg["payload"])["type"] = "other"
			}
			snapshot := object{"type": "world_state", "ordinal": json.Number("6"), "timestamp": start.Add(2 * time.Second).Format(time.RFC3339Nano), "payload": object{"full": true, "state": object{"agents_md": object{"directory": cwd, "text": string(body)}}}}
			if kind == "delta_snapshot" {
				obj(snapshot["payload"])["full"] = false
			}
			if kind == "wrong_snapshot_body" {
				obj(obj(obj(snapshot["payload"])["state"])["agents_md"])["text"] = "post-hoc file contents"
			}
			if kind == "wrong_directory" {
				obj(obj(obj(snapshot["payload"])["state"])["agents_md"])["directory"] = "D:/other"
			}
			if kind == "wrong_project" {
				project = []byte("different public project")
			}
			context := object{"type": "turn_context", "timestamp": start.Add(3 * time.Second).Format(time.RFC3339Nano), "payload": object{"turn_id": turn, "cwd": cwd, "model": "gpt-6-luna", "effort": "max"}}
			if kind == "wrong_turn" {
				obj(context["payload"])["turn_id"] = id
			}
			if kind == "wrong_model" {
				obj(context["payload"])["model"] = "gpt-6.1-sol"
			}
			records := []object{meta, {"type": "event_msg", "timestamp": start.Format(time.RFC3339Nano), "payload": object{"type": "task_started", "turn_id": turn}}, msg}
			if kind == "runtime_developer_prefix" || kind == "system_before_bootstrap" || kind == "unknown_role_before_bootstrap" {
				role := "developer"
				if kind == "system_before_bootstrap" {
					role = "system"
				}
				if kind == "unknown_role_before_bootstrap" {
					role = "unrecognized"
				}
				dev := object{"type": "response_item", "timestamp": start.Format(time.RFC3339Nano), "payload": object{"type": "message", "role": role, "content": []object{{"type": "input_text", "text": "runtime launch instructions"}}}}
				records = append(records[:2], dev, msg)
			}
			if kind == "developer_after_bootstrap" {
				records = append(records, object{"type": "response_item", "timestamp": start.Add(time.Second).Format(time.RFC3339Nano), "payload": object{"type": "message", "role": "developer", "content": []object{{"type": "input_text", "text": "extra later instruction"}}}})
			}
			if kind != "missing_snapshot" && kind != "late_snapshot" && kind != "user_fake_snapshot" {
				records = append(records, snapshot)
			}
			if kind == "duplicate_snapshot" {
				records = append(records, snapshot)
			}
			if kind == "user_fake_snapshot" {
				b, _ := json.Marshal(snapshot)
				records = append(records, object{"type": "response_item", "timestamp": start.Add(2 * time.Second).Format(time.RFC3339Nano), "payload": object{"type": "message", "role": "user", "content": []object{{"type": "input_text", "text": string(b)}}}})
			}
			records = append(records, context)
			if kind == "late_snapshot" {
				records = append(records, snapshot)
			}
			var b bytes.Buffer
			for _, v := range records {
				raw, _ := json.Marshal(v)
				b.Write(raw)
				b.WriteByte('\n')
			}
			job := object{"thread_id": id, "cwd": cwd, "sandbox": "read-only", "model": "gpt-6-luna", "effort": "max"}
			source, modern, ok := skillsRuntimeBootstrapProjection(&b, job, project, start, start.Add(time.Minute))
			want := kind == "valid" || kind == "changed_common_version" || kind == "runtime_developer_prefix"
			if ok != want || (want && (!modern || string(source) != string(body))) {
				t.Fatalf("runtime snapshot %s accepted=%v modern=%v", kind, ok, modern)
			}
		})
	}
}

func TestSkills3080DuplicateRoleCannotHideBeforeBootstrap(t *testing.T) {
	cwd, id := "D:/CC/Work/synthetic", "11111111-1111-7111-8111-111111111111"
	meta, _ := json.Marshal(object{"type": "session_meta", "payload": object{"id": id, "cwd": cwd}})
	bootstrap, _ := json.Marshal(object{"type": "response_item", "payload": object{"type": "message", "role": "user", "content": []object{{"type": "input_text", "text": skillsBootstrapFixture(cwd, "known instructions", true)}}}})
	for _, bad := range []string{
		`{"type":"response_item","payload":{"type":"message","role":"user","role":"assistant","content":[]}}`,
		`{"type":"response_item","type":"event_msg","payload":{"type":"message","role":"user","content":[]}}`,
		`{"type":"response_item","payload":{"type":"message","type":"other","role":"user","content":[]}}`,
	} {
		stream := string(meta) + "\n" + bad + "\n" + string(bootstrap) + "\n"
		if _, ok := skillsBootstrapBodyProjection(strings.NewReader(stream), id, cwd, "read-only"); ok {
			t.Fatal("duplicate header hid prior user")
		}
	}
}

func TestSkills3080DuplicateRoleCannotHideAcrossTheJobWindow(t *testing.T) {
	cwd, id := "D:/CC/Work/synthetic", "11111111-1111-7111-8111-111111111111"
	start := time.Date(2026, 10, 30, 14, 0, 0, 0, time.UTC)
	encode := func(v object) string { b, _ := json.Marshal(v); return string(b) + "\n" }
	msg := func(role, phase, text string, at time.Time) object {
		return object{"type": "response_item", "timestamp": at.Format(time.RFC3339Nano), "payload": object{"type": "message", "role": role, "phase": phase, "content": []object{{"type": "input_text", "text": text}}}}
	}
	prefix := encode(object{"type": "session_meta", "timestamp": start.Format(time.RFC3339Nano), "payload": object{"id": id, "cwd": cwd}}) + encode(object{"type": "turn_context", "timestamp": start.Format(time.RFC3339Nano), "payload": object{"model": "gpt-6-luna", "effort": "max", "cwd": cwd}}) + encode(msg("user", "", skillsBootstrapFixture(cwd, "runtime instructions", true), start))
	tail := encode(msg("user", "", "exact task", start.Add(time.Second))) + encode(msg("assistant", "final_answer", "original final", start.Add(2*time.Second)))
	for _, outside := range []bool{false, true} {
		for _, duplicate := range []bool{false, true} {
			at := start
			if outside {
				at = start.Add(-time.Second)
			}
			role := `"role":"assistant"`
			if duplicate {
				role = `"role":"user","role":"assistant"`
			}
			extra := `{"type":"response_item","timestamp":"` + at.Format(time.RFC3339Nano) + `","payload":{"type":"message",` + role + `,"phase":"commentary","content":[]}}` + "\n"
			proof := &skillsNativeResultProof{prompt: []byte("exact task"), finalSHA: hashBytes([]byte("original final")), exactInput: true, runtimeBootstrap: []byte("runtime instructions"), runtimeSourceUsed: true, cwd: cwd, sandbox: "read-only"}
			_, ok := skillsNativeProjectionBound(strings.NewReader(prefix+extra+tail), id, cwd, start, start.Add(time.Minute), proof)
			if ok == duplicate {
				t.Fatalf("role duplicate=%v outside=%v accepted=%v", duplicate, outside, ok)
			}
		}
	}
}

func TestSkills3080ConsumerWindowInheritsJobSandbox(t *testing.T) {
	// The self-hosted CI service uses a short Windows TEMP alias. Normalize
	// this synthetic home just like the other held-handle fixtures.
	home := skillsFixtureRoot(t)
	t.Setenv("USERPROFILE", home)
	id, cwd := "11111111-1111-7111-8111-111111111111", "D:/CC/Work/synthetic"
	start := time.Date(2026, 10, 30, 14, 0, 0, 0, time.UTC)
	dir := filepath.Join(home, ".codex", "sessions", "2026", "10", "30")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, readonly := range []bool{false, true} {
		msg := func(text string, at time.Time) object {
			return object{"type": "response_item", "timestamp": at.Format(time.RFC3339Nano), "payload": object{"type": "message", "role": "user", "content": []object{{"type": "input_text", "text": text}}}}
		}
		records := []object{{"type": "session_meta", "payload": object{"id": id, "cwd": cwd}}, {"type": "turn_context", "timestamp": start.Format(time.RFC3339Nano), "payload": object{"model": "gpt-6-luna", "effort": "max", "cwd": cwd}}, msg(skillsBootstrapFixture(cwd, "known instructions", readonly), start), msg("exact task", start.Add(time.Second))}
		var b bytes.Buffer
		for _, r := range records {
			v, _ := json.Marshal(r)
			b.Write(v)
			b.WriteByte('\n')
		}
		path := filepath.Join(dir, "rollout-synthetic-"+id+".jsonl")
		if err := os.WriteFile(path, b.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
		s := skillsFS{blocked: func(string) bool { return false }}
		job := object{"thread_id": id, "started_at": start.Format(time.RFC3339Nano), "cwd": cwd, "sandbox": "read-only"}
		p := &skillsNativeResultProof{prompt: []byte("exact task"), inputOnly: true, exactInput: true, bootstrapSHA: hashBytes([]byte("known instructions")), cwd: cwd}
		_, ok := skillsNativeWindow(&s, job, p, start.Add(time.Minute))
		s.close()
		if ok != readonly {
			t.Fatalf("consumer environment readonly=%v accepted=%v", readonly, ok)
		}
	}
}

func TestSkills3080RuntimeBootstrapRequiresIndependentWholeDocuments(t *testing.T) {
	global := []byte("updated owner instructions\r\nkeep scope and stops\r\n")
	project := []byte("project operating instructions\r\n")
	source, ok := skillsAssembleBootstrapSource(global, project)
	if !ok {
		t.Fatal("independent documents rejected")
	}
	for _, kind := range []string{"updated", "windows", "old", "self_digest", "prefix", "suffix", "label", "project_changed", "missing_source", "split_source"} {
		t.Run(kind, func(t *testing.T) {
			body, expected := string(source), source
			switch kind {
			case "windows":
				body = strings.ReplaceAll(body, "\n", "\r\n")
				expected = []byte(body)
			case "old":
				body = "old owner instructions\n\n--- project-doc ---\n\nproject operating instructions"
			case "self_digest":
				expected = []byte(hashBytes([]byte(body)))
			case "prefix":
				body = "arbitrary extra instructions\n" + body
			case "suffix":
				body += "\nadditional private scope"
			case "label":
				body = "# AGENTS.md\n" + body
			case "project_changed":
				body = strings.ReplaceAll(body, "project operating", "other project operating")
			case "missing_source":
				expected = nil
			case "split_source":
				body = strings.ReplaceAll(body, "--- project-doc ---", "--- AGENTS.md ---")
			}
			if got := skillsBootstrapSourceMatches(body, expected); got != (kind == "updated" || kind == "windows") {
				t.Fatalf("whole source comparison %s accepted=%v", kind, got)
			}
		})
	}
}

func TestSkills3080UpdatedBootstrapAndExactTurnRemainSeparate(t *testing.T) {
	for _, kind := range []string{"updated", "old", "legacy_without_source", "unknown_bootstrap", "wrong_cwd", "write_environment", "prior_user", "later_user", "duplicate_task", "duplicate_final", "commentary", "wrong_final", "outside_window", "other_generation", "outside_user"} {
		t.Run(kind, func(t *testing.T) {
			cwd, id := "D:/CC/Work/synthetic", "11111111-1111-7111-8111-111111111111"
			start := time.Date(2026, 10, 30, 14, 0, 0, 0, time.UTC)
			source, _ := skillsAssembleBootstrapSource([]byte("new owner policy"), []byte("same project"))
			old := "old owner policy\n\n--- project-doc ---\n\nsame project"
			body := string(source)
			if kind == "old" || kind == "legacy_without_source" {
				body = old
			}
			if kind == "unknown_bootstrap" {
				body = "unproven owner policy\n\n--- project-doc ---\n\nsame project"
			}
			bootstrap := skillsBootstrapFixture(cwd, body, kind != "write_environment")
			if kind == "wrong_cwd" {
				bootstrap = strings.ReplaceAll(bootstrap, cwd, "D:/other")
			}
			msg := func(role, phase, text string, at time.Time) object {
				return object{"type": "response_item", "timestamp": at.Format(time.RFC3339Nano), "payload": object{"type": "message", "role": role, "phase": phase, "content": []object{{"type": "input_text", "text": text}}}}
			}
			prompt, final := "exact saved scheduler execution input", "original final"
			metaAt := start
			if kind == "other_generation" {
				metaAt = start.Add(-time.Hour)
			}
			records := []object{{"type": "session_meta", "timestamp": metaAt.Format(time.RFC3339Nano), "payload": object{"id": id, "cwd": cwd}}, {"type": "turn_context", "timestamp": start.Format(time.RFC3339Nano), "payload": object{"model": "gpt-6-luna", "effort": "max", "cwd": cwd}}, msg("user", "", bootstrap, start)}
			if kind == "outside_user" {
				records = append(records, msg("user", "", "arbitrary prior generation", start.Add(-time.Second)))
			}
			if kind == "prior_user" {
				records = append(records, msg("user", "", "arbitrary prior user", start))
			}
			records = append(records, msg("user", "", prompt, start.Add(time.Second)))
			if kind == "later_user" || kind == "duplicate_task" {
				text := "scope expansion"
				if kind == "duplicate_task" {
					text = prompt
				}
				records = append(records, msg("user", "", text, start.Add(2*time.Second)))
			}
			phase, result, at := "final_answer", final, start.Add(3*time.Second)
			if kind == "commentary" {
				phase = "commentary"
			}
			if kind == "wrong_final" {
				result = "different original final"
			}
			if kind == "outside_window" {
				at = start.Add(2 * time.Hour)
			}
			records = append(records, msg("assistant", phase, result, at))
			if kind == "duplicate_final" {
				records = append(records, msg("assistant", phase, result, at.Add(time.Second)))
			}
			var stream bytes.Buffer
			for _, record := range records {
				b, _ := json.Marshal(record)
				stream.Write(b)
				stream.WriteByte('\n')
			}
			if kind == "legacy_without_source" {
				source = nil
			}
			proof := &skillsNativeResultProof{prompt: []byte(prompt), finalSHA: hashBytes([]byte(final)), exactInput: true, bootstrapSHA: hashBytes([]byte(old)), runtimeBootstrap: source, runtimeSourceUsed: len(source) > 0, cwd: cwd, sandbox: "read-only"}
			_, ok := skillsNativeProjectionBound(&stream, id, cwd, start, start.Add(time.Minute), proof)
			if ok != (kind == "updated" || kind == "legacy_without_source") {
				t.Fatalf("runtime bootstrap %s accepted=%v", kind, ok)
			}
		})
	}
}

func TestSkills3080BootstrapRejectsTransportAndIdentityAliases(t *testing.T) {
	cwd, id := "D:/CC/Work/synthetic", "11111111-1111-7111-8111-111111111111"
	message, _ := json.Marshal(object{"type": "response_item", "payload": object{"type": "message", "role": "user", "content": []object{{"type": "input_text", "text": skillsBootstrapFixture(cwd, "operating instructions", true)}}}})
	meta, _ := json.Marshal(object{"type": "session_meta", "payload": object{"id": id, "cwd": cwd}})
	base := string(meta) + "\n" + string(message) + "\n"
	for _, mutation := range []struct{ old, replacement string }{
		{`"type":"session_meta"`, `"type":"other","type":"session_meta"`},
		{`"id":`, `"ID":`}, {`"cwd":`, `"Cwd":`}, {`"payload":`, `"Payload":`},
		{`"role":`, `"ROLE":`}, {`"content":`, `"Content":`}, {`"text":`, `"Text":`},
	} {
		stream := strings.Replace(base, mutation.old, mutation.replacement, 1)
		if _, ok := skillsBootstrapBodyProjection(strings.NewReader(stream), id, cwd, "read-only"); ok {
			t.Fatalf("bootstrap transport alias accepted: %s", mutation.old)
		}
	}
}
