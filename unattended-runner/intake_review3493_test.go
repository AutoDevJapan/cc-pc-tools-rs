package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestIntake3493LoadedPostCutoverWatchResumesAdmission(t *testing.T) {
	r, m, in, _ := historyWatchFixture(t)
	guard := func(context.Context, string) bool { return true }
	if err := r.watchHistory(context.Background(), in, guard); err != nil {
		t.Fatal(err)
	}
	q := r.State.Requests[m.ID]
	r.State.Since = q.Created
	if err := r.watchHistory(context.Background(), in, guard); err == nil {
		t.Fatal("same-input registration accepted an invalid saved watch")
	}
	if err := r.validateSourceMode(context.Background(), q, false, false); err == nil {
		t.Fatal("live validation accepted an invalid saved watch")
	}
	var h Health
	r.refreshOrdinary(context.Background(), &h)
	r.admit(context.Background(), &h)
	if q.HistoryWatch != nil || q.Phase != "task_binding_pending" || q.Todo != 0 || len(h.Errors) != 0 {
		t.Fatal("loaded watch still conceals ordinary intake", q.Phase, h.Errors)
	}
	entries, err := os.ReadDir(filepath.Join(r.Config.Home, "requests", "edits"))
	if err != nil || len(entries) != 1 {
		t.Fatal("retired watch evidence missing", err)
	}
	before, err := os.ReadFile(filepath.Join(r.Config.Home, "unattended-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	r.refreshOrdinary(context.Background(), &h)
	r.admit(context.Background(), &h)
	after, err := os.ReadFile(filepath.Join(r.Config.Home, "unattended-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	entries, err = os.ReadDir(filepath.Join(r.Config.Home, "requests", "edits"))
	if err != nil || len(entries) != 1 || string(before) != string(after) || len(r.State.Requests) != 1 {
		t.Fatal("watch recovery repeated or created work", err)
	}
}

func TestIntake3493PendingEditsReclassifyInBothDirections(t *testing.T) {
	for _, phase := range []string{"task_binding_pending", "source_context_pending"} {
		t.Run(phase, func(t *testing.T) {
			r, q, m, _ := ordinaryFixture(t)
			q.Phase, q.PendingOwner, q.NextAction = phase, "OMB", "read_source_and_bind_original_task"
			m.Content, m.Edited = "a", r.Now().Add(-2*time.Second).Format(time.RFC3339Nano)
			if _, err := r.observeOrdinary(q, historyReadMessage{Message: *m}); err != nil {
				t.Fatal(err)
			}
			if q.Phase != "source_context_pending" || q.NextAction != "verify_exact_original_question_generation" {
				t.Fatal("short answer retains binding advice", q.Phase, q.NextAction)
			}
			m.Content, m.Edited = "元の依頼を詳しく訂正する", r.Now().Add(-time.Second).Format(time.RFC3339Nano)
			if _, err := r.observeOrdinary(q, historyReadMessage{Message: *m}); err != nil {
				t.Fatal(err)
			}
			if q.Phase != "task_binding_pending" || q.NextAction != "verify_thread_discovery_and_bind_original_task" {
				t.Fatal("ordinary correction retains question advice", q.Phase, q.NextAction)
			}
		})
	}
}

func TestIntake3493LegacyPendingQuestionMigratesOnce(t *testing.T) {
	r, q, _, calls := ordinaryFixture(t)
	q.Text, q.Phase, q.NextAction = "a", "task_binding_pending", "read_source_and_bind_original_task"
	var h Health
	r.admit(context.Background(), &h)
	r.admit(context.Background(), &h)
	if q.Phase != "source_context_pending" || q.NextAction != "verify_exact_original_question_generation" || *calls != 0 {
		t.Fatal("legacy hold stayed unexecutable", q.Phase, q.NextAction)
	}
}

func TestIntake3493AutomaticAdmissionHasNoBindingAdvice(t *testing.T) {
	r, q, _, _ := ordinaryFixture(t)
	q.Phase = "captured"
	var out strings.Builder
	load := func(string, bool) (*Runner, func(), error) { return r, func() {}, nil }
	if code := intakeCLIWith([]string{"status"}, &out, load, func(context.Context, string) bool { return true }); code != 0 {
		t.Fatal(code)
	}
	var result struct {
		Items []struct {
			Owner string `json:"pending_owner"`
			Next  string `json:"next_action"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(out.String()), &result); err != nil || len(result.Items) != 1 || result.Items[0].Owner != "" || result.Items[0].Next != "" {
		t.Fatal("automatic admission asks for manual binding", out.String(), err)
	}
}

func TestIntake3493NormalScanIncludesCutoverTimestamp(t *testing.T) {
	r, c := fixture(t)
	m := msg(c, "500", "300", "受信開始と同時刻の元依頼")
	server(t, r, func(w http.ResponseWriter, req *http.Request) {
		respond(w, []Message{m})
	})
	if n, err := r.scan(context.Background(), Thread{ID: "500"}); err != nil || n != 1 || r.State.Requests[m.ID] == nil {
		t.Fatal("cutover timestamp fell between scan and watch", n, err)
	}
}

func TestIntake3493VerifiedProductLinkRecoversDedicatedDelivery(t *testing.T) {
	f, q := savedReplyFixture(t)
	q.Origin = nil
	q.Phase, q.NextAction = "source_context_pending", "verify_original_product_scope_and_use_finite_steering_delivery"
	before, err := f.r.todoControl(q.Todo)
	if err != nil {
		t.Fatal(err)
	}
	var h Health
	f.r.refreshOrdinary(context.Background(), &h)
	if q.Phase != "product_reply_saved" || q.Origin == nil || len(h.Errors) != 0 {
		t.Fatal("verified product reply has no executable delivery route", q.Phase, h.Errors)
	}
	plan, err := f.r.prepareSteeringDelivery(context.Background(), q.Message)
	if err != nil || plan.Phase != "prepared" {
		t.Fatal("recovered route cannot prepare actual delivery", err)
	}
	after, err := f.r.todoControl(q.Todo)
	if err != nil || before != after || len(f.r.State.Requests) != 1 {
		t.Fatal("recovery changed controls or created work", err)
	}
}

func TestIntake3493ConfiguredRequestSourceReachesOriginalTask(t *testing.T) {
	r, q, m, calls := ordinaryFixture(t)
	q.Todo, q.Phase = 42, "registered"
	r.Config.ProductReports = []ProductReportConfig{{Todo: 42, Project: r.Config.Project, Source: OwnerSource{Kind: stateRequestSource, Message: q.Message}}}
	before, err := r.todoControl(42)
	if err != nil {
		t.Fatal(err)
	}
	m.Content, m.Edited = "元の依頼の実行条件を訂正する", r.Now().Add(-time.Second).Format(time.RFC3339Nano)
	if _, err := r.observeOrdinary(q, historyReadMessage{Message: *m}); err != nil {
		t.Fatal(err)
	}
	var h Health
	r.refreshOrdinary(context.Background(), &h)
	r.processOrdinaryPending(context.Background(), &h)
	r.refreshOrdinary(context.Background(), &h)
	r.processOrdinaryPending(context.Background(), &h)
	after, err := r.todoControl(42)
	row, rowErr := r.todo(42)
	if err != nil || rowErr != nil || before != after || *calls != 1 || q.Phase != "source_edit_delivered" || !strings.Contains(row.Note, "intake-owner-source:"+q.Message+":"+ordinaryVersion(q)) || len(h.Errors) != 0 {
		t.Fatal("configured source edit has no canonical route or duplicated delivery", q.Phase, *calls, h.Errors, err, rowErr)
	}
}

func TestIntake3493ConfiguredSourceCannotDeliverDifferentOrStaleContext(t *testing.T) {
	for _, kind := range []string{"different-message", "stale-body", "status-question", "short-answer", "unsupported-type"} {
		t.Run(kind, func(t *testing.T) {
			r, q, m, calls := ordinaryFixture(t)
			q.Todo, q.Phase = 42, "source_context_pending"
			cfg := ProductReportConfig{Todo: 42, Project: r.Config.Project, Source: OwnerSource{Kind: stateRequestSource, Message: q.Message}}
			switch kind {
			case "different-message":
				cfg.Source.Message = "600"
			case "stale-body":
				m.Content = "まだ保存していない別の本文"
			case "status-question":
				cfg.Source.Purpose, cfg.Source.AnswerMessage, cfg.Source.AnswerReceipt, cfg.Source.AnswerReceiptSHA = "status_question", "600", filepath.Join(r.Config.Home, "status-answer.json"), strings.Repeat("a", 64)
			case "short-answer":
				q.Text, m.Content = "a", "a"
			case "unsupported-type":
				typ := 6
				q.MessageType = &typ
			}
			r.Config.ProductReports = []ProductReportConfig{cfg}
			if err := r.recoverConfiguredRequestSource(context.Background(), q); err != nil || *calls != 0 || q.Phase != "source_context_pending" {
				t.Fatal("dedicated source delivery broadened its context", kind, err, *calls)
			}
		})
	}
}

func TestIntake3493ProductRecoveryDoesNotTrustUnverifiedOrForeignLinks(t *testing.T) {
	for _, kind := range []string{"unverified", "foreign-todo", "blocked"} {
		t.Run(kind, func(t *testing.T) {
			f, q := savedReplyFixture(t)
			q.Origin = nil
			q.Phase = "source_context_pending"
			if kind == "unverified" {
				delete(f.r.ProgressLinks, q.Thread)
			} else if kind == "blocked" {
				f.r.BlockedProgressThreads[q.Thread] = true
			} else {
				q.Todo = 999
			}
			var h Health
			f.r.refreshOrdinary(context.Background(), &h)
			if q.Origin != nil || q.Phase != "source_context_pending" || len(h.Errors) != 0 || f.sends != 1 {
				t.Fatal("unverified or foreign product link became a delivery", q.Phase, h.Errors)
			}
		})
	}
}
