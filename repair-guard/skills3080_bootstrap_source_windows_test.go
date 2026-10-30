package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

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
	for _, kind := range []string{"updated", "old", "unknown_bootstrap", "wrong_cwd", "write_environment", "prior_user", "later_user", "duplicate_task", "duplicate_final", "commentary", "wrong_final", "outside_window", "other_generation", "outside_user"} {
		t.Run(kind, func(t *testing.T) {
			cwd, id := "D:/CC/Work/synthetic", "11111111-1111-7111-8111-111111111111"
			start := time.Date(2026, 10, 30, 14, 0, 0, 0, time.UTC)
			source, _ := skillsAssembleBootstrapSource([]byte("new owner policy"), []byte("same project"))
			old := "old owner policy\n\n--- project-doc ---\n\nsame project"
			body := string(source)
			if kind == "old" {
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
			proof := &skillsNativeResultProof{prompt: []byte(prompt), finalSHA: hashBytes([]byte(final)), exactInput: true, bootstrapSHA: hashBytes([]byte(old)), runtimeBootstrap: source, runtimeSourceUsed: kind != "old", cwd: cwd, sandbox: "read-only"}
			_, ok := skillsNativeProjectionBound(&stream, id, cwd, start, start.Add(time.Minute), proof)
			if ok != (kind == "updated" || kind == "old") {
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
