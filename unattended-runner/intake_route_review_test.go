package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestIntake3485WatchCannotHidePostCutoverSource(t *testing.T) {
	for _, offset := range []time.Duration{0, time.Second} {
		t.Run(offset.String(), func(t *testing.T) {
			r, m, in, reads := historyWatchFixture(t)
			cutoff, err := time.Parse(time.RFC3339Nano, r.State.Since)
			if err != nil || !r.Now().After(cutoff.Add(time.Second)) {
				t.Fatal("fixture has no post-cutover audit interval")
			}
			m.Timestamp = cutoff.Add(offset).Format(time.RFC3339Nano)
			var page monthlyReadPage
			if err := readJSON(in.Receipt, 4<<20, &page); err != nil {
				t.Fatal(err)
			}
			page.Until, page.Acquired = r.Now().Format(time.RFC3339Nano), r.Now().Format(time.RFC3339Nano)
			page.Messages[0].Message = *m
			if err := publish(in.Receipt, page); err != nil {
				t.Fatal(err)
			}
			b, _ := boundedRegular(in.Receipt)
			in.ReceiptSHA = bytesHash(b)
			if err := r.watchHistory(context.Background(), in, func(context.Context, string) bool { return true }); err == nil {
				t.Fatal("post-cutover owner input became a passive watch")
			}
			if len(r.State.Requests) != 0 || *reads != 0 {
				t.Fatal("invalid watch accessed or replaced ordinary intake", *reads)
			}
		})
	}
}

func TestIntake3485LoadedWatchMustRemainBeforeCutover(t *testing.T) {
	r, m, in, _ := historyWatchFixture(t)
	if err := r.watchHistory(context.Background(), in, func(context.Context, string) bool { return true }); err != nil {
		t.Fatal(err)
	}
	q := r.State.Requests[m.ID]
	if !r.validHistoryWatch(q) {
		t.Fatal("positive historical watch is invalid")
	}
	r.State.Since = q.Created
	if r.validHistoryWatch(q) {
		t.Fatal("loaded watch at cutover could conceal normal intake")
	}
}

func TestIntake3485ConfiguredProductEditHasDurableDedicatedNextAction(t *testing.T) {
	f := progressTestFixture(t)
	configureStateRequestForTest(f, 2705)
	q := f.r.State.Requests[f.source.ID]
	if !f.r.isConfiguredRequestReport(q.Todo) {
		t.Fatal("configured report positive control is missing")
	}
	before, err := os.ReadFile(f.r.TodoPath)
	if err != nil {
		t.Fatal(err)
	}
	live := f.source
	live.Content, live.Edited = "元依頼に関する新しい訂正", f.now.Add(-time.Second).Format(time.RFC3339Nano)
	if _, err := f.r.observeOrdinary(q, historyReadMessage{Message: live}); err != nil {
		t.Fatal(err)
	}
	if q.Phase != "source_context_pending" || q.NextAction != "verify_configured_request_source_and_deliver_original_task" {
		t.Fatal("fresh edit did not classify the dedicated route")
	}
	// Resume the older saved phase as well: deployment must recover a delivery
	// left by the previous receiver, without repeating the excluded operation.
	q.Phase, q.NextAction = "source_edit_saved", "deliver_current_version_to_original_task"
	if err := f.r.save(); err != nil {
		t.Fatal(err)
	}
	var recovered State
	if err := readJSON(filepath.Join(f.r.Config.Home, "unattended-state.json"), maxState, &recovered); err != nil {
		t.Fatal(err)
	}
	f.r.State = &recovered
	q = f.r.State.Requests[q.Message]
	var h Health
	f.r.processOrdinaryPending(context.Background(), &h)
	if q.Phase != "source_context_pending" || q.PendingOwner != "OMB" || q.NextAction != "verify_configured_request_source_and_deliver_original_task" || len(h.Errors) != 0 {
		t.Fatal("product edit has no actionable dedicated route", q.Phase, q.NextAction, h.Errors)
	}
	f.r.processOrdinaryPending(context.Background(), &h)
	after, _ := os.ReadFile(f.r.TodoPath)
	var saved State
	if err := readJSON(filepath.Join(f.r.Config.Home, "unattended-state.json"), maxState, &saved); err != nil {
		t.Fatal(err)
	}
	if len(h.Errors) != 0 || f.sends != 0 || string(before) != string(after) || saved.Requests[q.Message].NextAction != q.NextAction {
		t.Fatal("product edit retried, changed task controls, or was not saved", h.Errors)
	}
}

func TestIntake3485ProductLinkEditAlsoParksWithoutRetry(t *testing.T) {
	r, q, m, calls := ordinaryFixture(t)
	q.Todo, q.Phase = 42, "registered"
	r.State.ProgressLinks = map[string]ProgressLink{q.Thread: {Scope: productReportScope, Todo: 42}}
	m.Content, m.Edited = "限定された元作業への訂正", r.Now().Add(-time.Second).Format(time.RFC3339Nano)
	if _, err := r.observeOrdinary(q, historyReadMessage{Message: *m}); err != nil {
		t.Fatal(err)
	}
	var h Health
	r.processOrdinaryPending(context.Background(), &h)
	r.processOrdinaryPending(context.Background(), &h)
	if q.Phase != "source_context_pending" || q.NextAction != "verify_original_product_scope_and_use_finite_steering_delivery" || len(h.Errors) != 0 || *calls != 0 {
		t.Fatal("product progress link entered generic delivery", q.Phase, q.NextAction, h.Errors)
	}
}

func TestIntake3485WatchedQuestionEditNamesExactGeneration(t *testing.T) {
	r, m, in, _ := historyWatchFixture(t)
	if err := r.watchHistory(context.Background(), in, func(context.Context, string) bool { return true }); err != nil {
		t.Fatal(err)
	}
	r.KnownQuestionThreads = map[string]bool{m.Channel: true}
	m.Content, m.Edited = "b", r.Now().Add(-time.Second).Format(time.RFC3339Nano)
	var h Health
	r.refreshOrdinary(context.Background(), &h)
	q := r.State.Requests[m.ID]
	if q.NextAction != "verify_exact_original_question_generation" || q.Phase != "source_context_pending" || q.Todo != 0 {
		t.Fatal("watched question asks for a binding that is forbidden", q.Phase, q.NextAction)
	}
}

func TestIntake3485UneditedWatchCannotBeBound(t *testing.T) {
	r, m, in, _ := historyWatchFixture(t)
	if err := r.watchHistory(context.Background(), in, func(context.Context, string) bool { return true }); err != nil {
		t.Fatal(err)
	}
	q := r.State.Requests[m.ID]
	db := map[string]any{"projects": map[string]string{"cc-pc-tools-rs": r.Config.Home}, "items": []any{map[string]any{"id": 42, "project": "cc-pc-tools-rs", "status": "wait", "note": "retained original", "jobs": []string{"worker42"}}}}
	if err := publish(r.TodoPath, db); err != nil {
		t.Fatal(err)
	}
	mutations := 0
	r.Run = func(_ context.Context, path string, _ ...string) (string, error) {
		if strings.HasSuffix(path, "scrub391.exe") {
			return `{"client_resource":false,"classification_unverified":false}`, nil
		}
		mutations++
		return "", errors.New("unexpected watch mutation")
	}
	plan := IntakeBinding{Todo: 42, Project: "cc-pc-tools-rs", SourceSHA: ordinaryVersion(q), Evidence: "元の投稿を再実行しない"}
	if err := r.bindOrdinary(context.Background(), m.ID, plan); err == nil || q.Phase != "history_watch" || q.Todo != 0 || mutations != 0 {
		t.Fatal("unchanged historical baseline was delivered")
	}
	if q.PendingOwner != "" {
		t.Fatal("unchanged baseline was advertised as active OMB work")
	}
	var out strings.Builder
	load := func(string, bool) (*Runner, func(), error) { return r, func() {}, nil }
	if intakeCLIWith([]string{"status"}, &out, load, nil) != 0 || strings.Contains(out.String(), "read_source_and_bind_original_task") {
		t.Fatal("passive watch status invented a task binding", out.String())
	}
}

func TestIntake3485ExplicitWatchSurvivesManagementDisable(t *testing.T) {
	r, m, in, reads := historyWatchFixture(t)
	if err := r.watchHistory(context.Background(), in, func(context.Context, string) bool { return true }); err != nil {
		t.Fatal(err)
	}
	r.State.Threads[m.Channel] = Thread{ID: m.Channel, Managed: true}
	r.Config.ManagementIntake = false
	before := *reads
	var h Health
	r.refreshOrdinary(context.Background(), &h)
	if *reads != before+1 || len(h.Errors) != 0 || r.State.Requests[m.ID].Phase != "history_watch" {
		t.Fatal("explicit watch was disabled with general discovery", *reads-before, h.Errors)
	}
}

func TestIntake3485LegacyPreeditedApplicationChecksCurrentSteering(t *testing.T) {
	for _, kind := range []string{"matching", "changed", "missing"} {
		t.Run(kind, func(t *testing.T) {
			r, q, _, _ := ordinaryFixture(t)
			q.Todo, q.Phase, q.Edited = 42, "steering_saved", r.Now().Add(-time.Second).Format(time.RFC3339Nano)
			snapshot := *q
			if kind == "changed" {
				snapshot.Text = "別の版"
			}
			if kind != "missing" {
				if err := publish(r.steeringReceiptPath(42, q.Message), &snapshot); err != nil {
					t.Fatal(err)
				}
			}
			proof := map[string]any{"todo_id": 42, "message_id": q.Message, "job_id": "worker42", "revision": "old actual revision", "verification": "old actual result"}
			if err := publish(filepath.Join(r.Config.Home, "applied", q.Message+".json"), proof); err != nil {
				t.Fatal(err)
			}
			row, _ := r.todo(42)
			ok, err := r.observeLegacySteeringApplication(q, row)
			if err != nil || ok != (kind == "matching") || ok && q.Phase != "steering_applied" {
				t.Fatal("legacy source matching differs", kind, ok, err)
			}
		})
	}
}

func TestIntake3485WorkerSourceOmitsWholeChannelReceiptPath(t *testing.T) {
	r, q, _, _ := ordinaryFixture(t)
	q.HistoryWatch = &HistoryWatch{Receipt: filepath.Join(r.Config.Home, "original-monthly.json"), ReceiptSHA: strings.Repeat("a", 64), ScopeSHA: strings.Repeat("b", 64)}
	path, err := r.ordinarySourceFile(q)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var source Request
	if json.Unmarshal(b, &source) != nil || source.HistoryWatch == nil || source.HistoryWatch.Receipt != "" || source.HistoryWatch.ReceiptSHA != q.HistoryWatch.ReceiptSHA || q.HistoryWatch.Receipt == "" {
		t.Fatal("worker can follow a whole-channel receipt or canonical provenance was lost", string(b))
	}
	steering := ordinaryWorkerSource(q)
	b, err = json.Marshal(&steering)
	if err != nil || strings.Contains(string(b), "receipt_file") || steering.HistoryWatch.ReceiptSHA != q.HistoryWatch.ReceiptSHA {
		t.Fatal("worker steering exposes the whole-channel receipt", string(b), err)
	}
}

func TestIntake3485ReportedBindingRereadRemainsSuccessful(t *testing.T) {
	r, q, _, calls := ordinaryFixture(t)
	plan := IntakeBinding{Todo: 42, Project: "cc-pc-tools-rs", SourceSHA: ordinaryVersion(q), Evidence: "元の依頼と配送済みの版を照合"}
	if err := r.bindOrdinary(context.Background(), q.Message, plan); err != nil || *calls != 1 {
		t.Fatal("positive canonical delivery failed", err)
	}
	q.Phase = "reported"
	if err := r.bindOrdinary(context.Background(), q.Message, plan); err != nil || q.Phase != "reported" || *calls != 1 {
		t.Fatal("completed binding reread became a hold or duplicate", err, q.Phase, *calls)
	}
}
