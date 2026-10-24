package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func ordinaryFixture(t *testing.T) (*Runner, *Request, *Message, *int) {
	t.Helper()
	r, c := fixture(t)
	m := msg(c.Add(time.Second), "500", "300", "この方針訂正を元の作業へ反映して")
	q := captured(m, "origin_pending", c)
	r.State.Requests[m.ID] = q
	server(t, r, func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/channels/500/messages/"+m.ID {
			respond(w, m)
		} else if req.URL.Path == "/channels/500" {
			respond(w, Thread{ID: "500", Parent: requestForum, Guild: "100", Owner: "300", Type: 11})
		} else {
			w.WriteHeader(404)
		}
	})
	db := map[string]any{"projects": map[string]string{"cc-pc-tools-rs": r.Config.Home}, "items": []any{map[string]any{"id": 42, "project": "cc-pc-tools-rs", "status": "wait", "note": "既存の記録\n", "jobs": []string{"worker42"}, "wait_on": []uint64{99}, "resume_generation": 7, "stop_kind": "owner"}}}
	if err := publish(r.TodoPath, db); err != nil {
		t.Fatal(err)
	}
	calls := new(int)
	r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
		if strings.HasSuffix(path, "scrub391.exe") {
			return `{"client_resource":false,"classification_unverified":false}`, nil
		}
		if path != conversationTodo || len(args) != 10 || args[0] != "edit" || args[1] != "42" || args[4] != "--expected-note-sha256" || args[6] != "--scope-file" || args[8] != "--expected-state-file" {
			t.Errorf("unexpected mutation: %s %v", path, args)
			return "", errors.New("unexpected")
		}
		*calls++
		var current struct {
			Projects map[string]string `json:"projects"`
			Items    []map[string]any  `json:"items"`
		}
		if err := readJSON(r.TodoPath, maxTodo, &current); err != nil {
			return "", err
		}
		if bytesHash([]byte(current.Items[0]["note"].(string))) != args[5] {
			return "", errors.New("conversation_note_changed")
		}
		note, err := os.ReadFile(args[3])
		if err != nil {
			return "", err
		}
		current.Items[0]["note"] = string(note)
		if err := publish(r.TodoPath, current); err != nil {
			return "", err
		}
		return "edited 42", nil
	}
	return r, q, &m, calls
}

func TestManagementBindingCASIsReplaySafeAndRetainsWaitAndWorker(t *testing.T) {
	r, q, _, calls := ordinaryFixture(t)
	before, err := r.todoControl(42)
	if err != nil {
		t.Fatal(err)
	}
	plan := IntakeBinding{Todo: 42, Project: "cc-pc-tools-rs", SourceSHA: ordinaryVersion(q), Evidence: "原文の方針訂正と元仕様を照合した"}
	if err := r.bindOrdinary(context.Background(), q.Message, plan); err != nil {
		t.Fatal(err)
	}
	after, _ := r.todoControl(42)
	row, _ := r.todo(42)
	if q.Phase != "source_edit_delivered" || q.DeliveryVersion != plan.SourceSHA || *calls != 1 || before != after || !strings.Contains(row.Note, "既存の記録") {
		t.Fatal("delivery changed a wait/worker or was not durable", q, *calls)
	}
	var state State
	if err := readJSON(filepath.Join(r.Config.Home, "unattended-state.json"), maxState, &state); err != nil {
		t.Fatal(err)
	}
	r.State = &state
	if err := r.bindOrdinary(context.Background(), q.Message, plan); err != nil {
		t.Fatal(err)
	}
	row, _ = r.todo(42)
	if *calls != 1 || strings.Count(row.Note, "intake-owner-source:") != 1 || len(r.State.Requests) != 1 {
		t.Fatal("restart/replay duplicated task delivery")
	}
}

func TestManagementLostSuccessfulCASResponseIsRecoveredByReadback(t *testing.T) {
	r, q, _, calls := ordinaryFixture(t)
	base := r.Run
	r.Run = func(ctx context.Context, p string, a ...string) (string, error) {
		s, e := base(ctx, p, a...)
		if p == conversationTodo && e == nil {
			return "", errors.New("response lost")
		}
		return s, e
	}
	plan := IntakeBinding{Todo: 42, Project: "cc-pc-tools-rs", SourceSHA: ordinaryVersion(q), Evidence: "原仕様照合"}
	if err := r.bindOrdinary(context.Background(), q.Message, plan); err != nil {
		t.Fatal(err)
	}
	if err := r.bindOrdinary(context.Background(), q.Message, plan); err != nil || *calls != 1 {
		t.Fatal("lost response caused a second note write", err, *calls)
	}
}

func TestManagementBindingRejectsStaleVersionAndQuestionAnswer(t *testing.T) {
	for _, short := range []bool{false, true} {
		r, q, m, calls := ordinaryFixture(t)
		plan := IntakeBinding{Todo: 42, Project: "cc-pc-tools-rs", SourceSHA: ordinaryVersion(q), Evidence: "原仕様照合"}
		if short {
			q.Text = "a"
			m.Content = "a"
			plan.SourceSHA = ordinaryVersion(q)
		} else {
			plan.SourceSHA = strings.Repeat("0", 64)
		}
		if err := r.bindOrdinary(context.Background(), q.Message, plan); err == nil || *calls != 0 || q.Todo != 0 {
			t.Fatal("unknown question or stale source became a binding")
		}
	}
}

func TestManagementEditedSourceCannotReusePriorAppliedReceipt(t *testing.T) {
	r, q, m, calls := ordinaryFixture(t)
	plan := IntakeBinding{Todo: 42, Project: "cc-pc-tools-rs", SourceSHA: ordinaryVersion(q), Evidence: "原仕様照合"}
	if err := r.bindOrdinary(context.Background(), q.Message, plan); err != nil {
		t.Fatal(err)
	}
	old := ordinaryVersion(q)
	m.Content = "同じ依頼のさらに新しい訂正"
	m.Edited = r.Now().Add(-time.Second).Format(time.RFC3339Nano)
	if err := r.validateRequest(context.Background(), q); err == nil || q.Phase != "source_edit_saved" {
		t.Fatal("edit was not captured separately")
	}
	if err := r.deliverOrdinary(context.Background(), q); err != nil {
		t.Fatal(err)
	}
	if *calls != 2 || old == q.DeliveryVersion {
		t.Fatal("edit did not get exactly one new source version")
	}
	now := r.Now().Add(time.Minute)
	r.Now = func() time.Time { return now }
	proof := map[string]any{"source": "discord-api-v10", "todo_id": 42, "message_id": q.Message, "thread_id": q.Thread, "author_id": q.Author, "source_sha256": old, "job_id": "worker42", "revision": strings.Repeat("a", 40), "applied_at": now.Format(time.RFC3339Nano), "verification": "実反映をテスト"}
	path := filepath.Join(r.Config.Home, "applied", q.Message+".json")
	if err := publish(path, proof); err != nil {
		t.Fatal(err)
	}
	if err := r.observeOrdinaryApplication(context.Background(), q); err == nil || q.Phase == "source_edit_applied" {
		t.Fatal("old applied receipt completed an edited source")
	}
	proof["source_sha256"] = ordinaryVersion(q)
	if err := publish(path, proof); err != nil {
		t.Fatal(err)
	}
	if err := r.observeOrdinaryApplication(context.Background(), q); err != nil || q.Phase != "source_edit_applied" || q.NextAction != "verify_parent_receipt_and_product_delivery" {
		t.Fatal("matching worker application was not tracked", err)
	}
}

func TestManagementBindingCASConflictRetainsOriginalAndRetries(t *testing.T) {
	r, q, _, calls := ordinaryFixture(t)
	base := r.Run
	first := true
	r.Run = func(ctx context.Context, p string, a ...string) (string, error) {
		if p == conversationTodo && first {
			first = false
			return "", errors.New("conversation_note_changed")
		}
		return base(ctx, p, a...)
	}
	plan := IntakeBinding{Todo: 42, Project: "cc-pc-tools-rs", SourceSHA: ordinaryVersion(q), Evidence: "原仕様照合"}
	if err := r.bindOrdinary(context.Background(), q.Message, plan); err == nil || q.Phase != "source_edit_saved" || *calls != 0 {
		t.Fatal("CAS failure was marked delivered")
	}
	if err := r.deliverOrdinary(context.Background(), q); err != nil || *calls != 1 {
		t.Fatal("saved binding could not resume", err)
	}
}

func TestManagementIntakeStatusMasksBodyAndPaginates(t *testing.T) {
	r, q, _, _ := ordinaryFixture(t)
	var out strings.Builder
	load := func(string, bool) (*Runner, func(), error) { return r, func() {}, nil }
	if code := intakeCLIWith([]string{"status"}, &out, load, func(context.Context, string) bool { return true }); code != 0 {
		t.Fatal(code, out.String())
	}
	var result struct {
		Metadata bool             `json:"metadata_only"`
		Items    []map[string]any `json:"items"`
	}
	if json.Unmarshal([]byte(out.String()), &result) != nil || !result.Metadata || len(result.Items) != 1 || result.Items[0]["source_sha256"] != ordinaryVersion(q) || strings.Contains(out.String(), q.Text) {
		t.Fatal("status exposed body or lost version/owner")
	}
}

func TestManagementReplayRecoversDeeperLatePageWithoutRewindingWatermark(t *testing.T) {
	r, c := fixture(t)
	r.State.Scans["500"] = &Scan{After: snowflake(c.Add(time.Hour))}
	r.State.Threads["500"] = Thread{ID: "500"}
	late := msg(c.Add(time.Second), "500", "300", "深いpageの未取得指示")
	server(t, r, func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Query().Get("before") != "" {
			respond(w, []Message{late})
			return
		}
		rows := make([]Message, 100)
		for i := range rows {
			rows[i] = msg(c.Add(time.Duration(i+100)*time.Second), "500", "999", "other")
		}
		respond(w, rows)
	})
	var h Health
	r.replayManagement(context.Background(), &h)
	if r.State.Scans["500"].ReplayBefore == "" {
		t.Fatal("bounded replay did not retain continuation")
	}
	var restored State
	if err := readJSON(filepath.Join(r.Config.Home, "unattended-state.json"), maxState, &restored); err != nil {
		t.Fatal(err)
	}
	r.State = &restored
	r.replayManagement(context.Background(), &h)
	if r.State.Requests[late.ID] == nil || r.State.Scans["500"].After != snowflake(c.Add(time.Hour)) || r.State.Scans["500"].ReplayBefore != "" {
		t.Fatal("deep late event was lost across restart")
	}
}
