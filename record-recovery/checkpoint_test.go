package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckpointKeepsOriginalSpecNoteAndIncompleteTodo(t *testing.T) {
	root := correlationTestRoot(t)
	source := filepath.Join(root, "source")
	if e := privateDir(source); e != nil {
		t.Fatal(e)
	}
	report := obj{"ok": true, "old_request_rows": 8, "histories_verified": 34, "acquisition_content_conflicts": 0, "owner_replies_identity_verified": 154, "question_candidates": 40, "explicit_reference_bundles": 6, "saved_worker_documents_read": 15, "question_generation_records_read": 0, "deliveries": 0, "applications_verified": 0, "live_imports": 0, "verified_decision_scopes": 0, "reply_categories": obj{"explicit_same_thread_reference": 6, "explicit_reference_unverified": 8, "missing_question": 58, "sequence_candidate": 82}}
	rows := []any{}
	for i := 0; i < 8; i++ {
		workers := 0
		todos := 0
		if i < 5 {
			workers = 1
		}
		if i < 2 {
			todos = 1
		}
		rows = append(rows, obj{"worker_candidates": workers, "todo_candidates": todos})
	}
	report["requests"] = rows
	writeNew(filepath.Join(source, "correlation-report.json"), report)
	spec := filepath.Join(root, "original.md")
	original := "架空の私的指示\n元の完了条件\n記号 \\n\\n## 2026-10-13 はそのまま"
	os.WriteFile(spec, []byte(original), 0600)
	survey := filepath.Join(root, "survey")
	if e := privateDir(survey); e != nil {
		t.Fatal(e)
	}
	writeNew(filepath.Join(survey, "survey.private.json"), obj{"kind": "original-correspondence-survey", "original_spec_sha256": digest([]byte(original))})
	writeNew(filepath.Join(source, "correlation.private.json"), obj{"kind": "original-correspondence-correlation", "survey_source": survey})
	todo := filepath.Join(root, "todo.json")
	writeNew(todo, obj{"items": []any{obj{"id": 2467, "status": "doing", "note": "元の私的メモ", "spec": spec, "spec_history": []any{obj{"spec": "private-previous-revision"}}}}})
	before, _ := os.ReadFile(todo)
	out := filepath.Join(root, "checkpoint")
	r, e := checkpoint(source, todo, spec, out)
	if e != nil {
		t.Fatal(e)
	}
	b, e := os.ReadFile(filepath.Join(out, "resume-spec.private.md"))
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(b), original) || !strings.Contains(string(b), "全復旧は未完了") {
		t.Fatal("raw original entered model prompt or continuation treated as done")
	}
	stored, e := os.ReadFile(filepath.Join(out, "original-spec.private.md"))
	if e != nil || string(stored) != original {
		t.Fatal("original instructions changed")
	}
	note, e := os.ReadFile(filepath.Join(out, "todo-note.private.md"))
	if e != nil || strings.Contains(string(note), "元の私的メモ") {
		t.Fatal("raw original note entered model-visible todo note")
	}
	stored, e = os.ReadFile(filepath.Join(out, "original-note.private.md"))
	if e != nil || string(stored) != "元の私的メモ" {
		t.Fatal("original note discarded")
	}
	after, _ := os.ReadFile(todo)
	if string(before) != string(after) || r["complete"] != false || r["queued"] != false {
		t.Fatal("checkpoint changed original task state")
	}
	kept, _, e := readJSON(filepath.Join(out, "original-todo.private.json"))
	if e != nil || len(array(kept["spec_history"])) != 1 {
		t.Fatal("intermediate original restrictions lost")
	}
	wrongSpec := filepath.Join(root, "wrong-original.md")
	os.WriteFile(wrongSpec, []byte("different source"), 0600)
	_, e = checkpoint(source, todo, wrongSpec, filepath.Join(root, "wrong-source"))
	if e == nil || e.Error() != "original_spec_source_mismatch" {
		t.Fatal("unrelated specification became original")
	}
	report["owner_replies_identity_verified"] = 155
	raw, _ := json.MarshalIndent(report, "", "  ")
	os.WriteFile(filepath.Join(source, "correlation-report.json"), raw, 0600)
	_, e = checkpoint(source, todo, spec, filepath.Join(root, "changed"))
	if e == nil || e.Error() != "checkpoint_measurements_changed" {
		t.Fatal("changed snapshot used frozen continuation facts")
	}
}
