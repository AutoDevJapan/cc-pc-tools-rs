package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func savedReplyFixture(t *testing.T) (*progressFixture, *Request) {
	t.Helper()
	f := progressTestFixture(t)
	var h Health
	f.r.reportProducts(context.Background(), &h)
	thread := f.r.State.ProductReports[2639].Thread
	f.r.loadProgressLinks(context.Background(), map[string]bool{thread: true})
	origin := f.r.ProgressLinks[thread].ProgressOrigin
	m := msg(f.now.Add(time.Second), thread, "300", "b")
	f.mu.Lock()
	f.messages[thread+"/"+m.ID] = m
	f.mu.Unlock()
	req := &Request{Thread: thread, Guild: "100", RecordVersion: 2, Message: m.ID, Author: "300", Created: m.Timestamp, Source: "discord-api-v10", Text: m.Content, Context: "Literal short reply, not a new permission.", Key: requestKey(thread, m.ID), Phase: "captured", Origin: &origin}
	f.r.State.Requests[m.ID] = req
	if err := f.r.registerOrigin(context.Background(), req, origin); err != nil {
		t.Fatal(err)
	}
	f.rows[0]["status"] = "wait"
	f.rows[0]["wait_on"] = []uint64{2407}
	f.rows[0]["wait_ready"] = map[string]any{"condition": "retry-time", "since": 100, "not_before": 200}
	f.rows[0]["stop_kind"] = "owner"
	f.rows[0]["dispatchable"] = false
	f.rows[0]["resume_generation"] = 7
	f.saveRows()
	return f, req
}
func applyNormalNoteForTest(t *testing.T, f *progressFixture, plan SteeringDeliveryResult) {
	t.Helper()
	note, err := os.ReadFile(plan.NoteFile)
	if err != nil {
		t.Fatal(err)
	}
	f.rows[0]["note_history"] = []any{map[string]any{"note": f.rows[0]["note"]}}
	f.rows[0]["note"] = string(note)
	f.saveRows()
}
func TestSteeringNormalTodoReferenceDeliveryAndActualApplicationAreSeparate(t *testing.T) {
	f, req := savedReplyFixture(t)
	beforeControl, err := f.r.todoControl(req.Todo)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(f.r.TodoPath)
	plan, err := f.r.prepareSteeringDelivery(context.Background(), req.Message)
	if err != nil {
		t.Fatal(err)
	}
	afterPrepare, _ := os.ReadFile(f.r.TodoPath)
	if string(before) != string(afterPrepare) || plan.Job != "fixture-job" || plan.Phase != "prepared" || plan.Executable != "D:/CC/Tools/todo/todo.exe" || len(plan.Args) != 4 || plan.Args[0] != "edit" || plan.Args[2] != "--note-file" || req.Phase != "product_reply_saved" {
		t.Fatal("preparation mutated a task or fabricated delivery")
	}
	if _, err := f.r.verifySteeringDelivery(context.Background(), req.Message); err == nil {
		t.Fatal("save alone was marked delivered")
	}
	applyNormalNoteForTest(t, f, plan)
	result, err := f.r.verifySteeringDelivery(context.Background(), req.Message)
	if err != nil {
		t.Fatal(err)
	}
	if result.WorkerApplied || req.Phase != "product_reply_delivered" || result.Job != plan.Job {
		t.Fatal("normal todo reference was confused with live worker application")
	}
	row, _ := f.r.todo(req.Todo)
	afterControl, _ := f.r.todoControl(req.Todo)
	if beforeControl != afterControl || !strings.Contains(row.Note, "PRIVATE_RAW_NOTE") || !strings.Contains(row.Note, plan.Job) || !strings.Contains(row.Note, f.r.steeringReceiptPath(req.Todo, req.Message)) || req.Text != "b" {
		t.Fatal("normal delivery lost old notes, wait/stop/generation or literal reply")
	}
	proof := map[string]any{"todo_id": req.Todo, "message_id": req.Message, "job_id": "different-job", "revision": "fixture-revision", "verification": "fixture-validation"}
	if err := publish(filepath.Join(f.r.Config.Home, "applied", req.Message+".json"), proof); err != nil {
		t.Fatal(err)
	}
	result, err = f.r.verifySteeringDelivery(context.Background(), req.Message)
	if err != nil || result.WorkerApplied {
		t.Fatal("another worker's application proof was accepted")
	}
	proof["job_id"] = plan.Job
	_ = publish(filepath.Join(f.r.Config.Home, "applied", req.Message+".json"), proof)
	result, err = f.r.verifySteeringDelivery(context.Background(), req.Message)
	if err != nil || !result.WorkerApplied || req.Phase != "product_reply_applied" {
		t.Fatal("exact existing worker application proof was not recorded", err)
	}
	if f.sends != 1 {
		t.Fatal("delivery launched, notified or changed external effect scope")
	}
}

func TestSteeringDeliveryRejectsWaitStopWorkerOrReceiptChange(t *testing.T) {
	for _, change := range []string{"wait", "stop", "generation", "job", "receipt", "reference"} {
		t.Run(change, func(t *testing.T) {
			f, req := savedReplyFixture(t)
			plan, err := f.r.prepareSteeringDelivery(context.Background(), req.Message)
			if err != nil {
				t.Fatal(err)
			}
			applyNormalNoteForTest(t, f, plan)
			switch change {
			case "wait":
				f.rows[0]["wait_on"] = []uint64{999}
			case "stop":
				f.rows[0]["stop_kind"] = "changed"
			case "generation":
				f.rows[0]["resume_generation"] = 8
			case "job":
				f.rows[0]["worker_receipt"] = map[string]any{"job": "another-job"}
			case "receipt":
				file := f.r.steeringReceiptPath(req.Todo, req.Message)
				bytes, _ := os.ReadFile(file)
				bytes = append(bytes, ' ')
				_ = os.WriteFile(file, bytes, 0600)
			case "reference":
				f.rows[0]["note"] = "no reference"
			}
			f.saveRows()
			if _, err := f.r.verifySteeringDelivery(context.Background(), req.Message); err == nil || req.Phase != "product_reply_saved" {
				t.Fatal("changed wait, stop, worker or private receipt was marked delivered")
			}
		})
	}
}

func TestSteeringPreparationRepeatsSamePlanAndDoesNotDuplicateNote(t *testing.T) {
	f, req := savedReplyFixture(t)
	one, err := f.r.prepareSteeringDelivery(context.Background(), req.Message)
	if err != nil {
		t.Fatal(err)
	}
	applyNormalNoteForTest(t, f, one)
	two, err := f.r.prepareSteeringDelivery(context.Background(), req.Message)
	if err != nil {
		t.Fatal(err)
	}
	note, _ := os.ReadFile(two.NoteFile)
	if one.Plan != two.Plan || one.Job != two.Job || strings.Count(string(note), "unattended-steering-delivery:") != 1 {
		t.Fatal("repeated delivery preparation duplicated or rerouted its reference")
	}
	bytes, _ := json.Marshal(two)
	if strings.Contains(string(bytes), "PRIVATE_RAW") || strings.Contains(string(bytes), "Literal short reply") {
		t.Fatal("delivery command output included private text")
	}
}

func TestOwnerEditRequiresNewDeliveryAndCannotReuseAnOldApplicationProof(t *testing.T) {
	f, req := savedReplyFixture(t)
	one, err := f.r.prepareSteeringDelivery(context.Background(), req.Message)
	if err != nil {
		t.Fatal(err)
	}
	applyNormalNoteForTest(t, f, one)
	if _, err := f.r.verifySteeringDelivery(context.Background(), req.Message); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	m := f.messages[req.Thread+"/"+req.Message]
	m.Content = "revised literal instruction"
	m.Edited = f.now.Add(10 * time.Second).Format(time.RFC3339Nano)
	f.messages[req.Thread+"/"+req.Message] = m
	f.mu.Unlock()
	if _, err := f.r.prepareSteeringDelivery(context.Background(), req.Message); err == nil {
		t.Fatal("owner edit was silently delivered as the old version")
	}
	if _, err := f.r.prepareSteeringDelivery(context.Background(), req.Message); err == nil {
		t.Fatal("old private delivery receipt was silently retained")
	}
	two, err := f.r.prepareSteeringDelivery(context.Background(), req.Message)
	if err != nil {
		t.Fatal(err)
	}
	if two.Plan == one.Plan || len(req.PriorEdits) == 0 || req.Phase != "product_reply_saved" {
		t.Fatal("owner edit lost version history or reused delivery identity")
	}
	applyNormalNoteForTest(t, f, two)
	proof := map[string]any{"todo_id": req.Todo, "message_id": req.Message, "job_id": two.Job, "revision": "old-revision", "verification": "old-validation"}
	_ = publish(filepath.Join(f.r.Config.Home, "applied", req.Message+".json"), proof)
	result, err := f.r.verifySteeringDelivery(context.Background(), req.Message)
	if err != nil || result.WorkerApplied {
		t.Fatal("old application proof was reused for edited owner text")
	}
	var plan SteeringDeliveryPlan
	_ = readJSON(f.r.steeringPlanPath(req.Message), 1<<20, &plan)
	proof["receipt_sha256"] = plan.ReceiptSHA
	_ = publish(filepath.Join(f.r.Config.Home, "applied", req.Message+".json"), proof)
	result, err = f.r.verifySteeringDelivery(context.Background(), req.Message)
	if err != nil || !result.WorkerApplied {
		t.Fatal("exact edited receipt application was not recorded")
	}
}

func TestRunnerSteeringArgumentsDoNotCreateAnotherExecutionMode(t *testing.T) {
	for _, args := range [][]string{{"--prepare-steering-delivery", "123"}, {"--config", "fixture.json", "--verify-steering-delivery", "123"}} {
		_, mode, id, err := runnerArguments(args)
		if err != nil || mode == "" || id != "123" {
			t.Fatal("normal private steering operation unavailable")
		}
	}
	for _, args := range [][]string{{"--run", "123"}, {"--prepare-steering-delivery", "../x"}, {"--verify-steering-delivery"}, {"--prepare-steering-delivery", "123", "--verify-steering-delivery", "123"}} {
		if _, _, _, err := runnerArguments(args); err == nil {
			t.Fatal("unsupported or ambiguous execution mode accepted")
		}
	}
}
