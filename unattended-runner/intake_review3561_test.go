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

func TestIntake3561RetiresPostCutoverWatchWithoutLosingBinding(t *testing.T) {
	for _, phase := range []string{"task_binding_pending", "source_context_pending", "source_edit_saved"} {
		t.Run(phase, func(t *testing.T) {
			r, m, in, _ := historyWatchFixture(t)
			if err := r.watchHistory(context.Background(), in, func(context.Context, string) bool { return true }); err != nil {
				t.Fatal(err)
			}
			q := r.State.Requests[m.ID]
			r.State.Since, q.Phase = q.Created, phase
			q.PendingOwner, q.NextAction = "OMB", "preserve_existing_delivery"
			if phase == "source_edit_saved" {
				q.Todo = 42
				q.Binding = &IntakeBinding{Todo: 42, Project: r.Config.Project, SourceSHA: ordinaryVersion(q), Evidence: "existing binding"}
			}
			binding := q.Binding
			if err := r.retireOrdinaryWatch(q); err != nil || q.HistoryWatch != nil || q.Phase != phase || q.Binding != binding || q.NextAction != "preserve_existing_delivery" {
				t.Fatal("watch retirement changed an existing edit or binding", err)
			}
			var h Health
			r.refreshOrdinary(context.Background(), &h)
			expectedPhase := phase
			expectedAction := "preserve_existing_delivery"
			if phase == "source_context_pending" {
				// The separate context migration may now correct a stale disposition.
				expectedPhase = "task_binding_pending"
				expectedAction = "verify_thread_discovery_and_bind_original_task"
			}
			if q.HistoryWatch != nil || q.Phase != expectedPhase || q.NextAction != expectedAction || q.Binding != binding || len(h.Errors) != 0 {
				t.Fatal("watch migration stranded an existing edit or binding", q.Phase, h.Errors)
			}
			var saved State
			if err := readJSON(filepath.Join(r.Config.Home, "unattended-state.json"), maxState, &saved); err != nil {
				t.Fatal(err)
			}
			r.State = &saved
			r.refreshOrdinary(context.Background(), &h)
			files, err := os.ReadDir(filepath.Join(r.Config.Home, "requests", "edits"))
			original, readErr := os.ReadFile(in.Receipt)
			if err != nil || readErr != nil || len(files) != 1 || bytesHash(original) != in.ReceiptSHA || saved.Requests[m.ID].HistoryWatch != nil || saved.Requests[m.ID].Todo != q.Todo || len(h.Errors) != 0 {
				t.Fatal("restart repeated the migration or changed the frozen evidence", err, readErr, h.Errors)
			}
		})
	}
}

func TestIntake3561ScanRetiresWatchBeforeObservingEdit(t *testing.T) {
	r, m, in, _ := historyWatchFixture(t)
	if err := r.watchHistory(context.Background(), in, func(context.Context, string) bool { return true }); err != nil {
		t.Fatal(err)
	}
	q := r.State.Requests[m.ID]
	r.State.Since = q.Created
	m.Content, m.Edited = "scan sees the correction first", r.Now().Add(-time.Second).Format(time.RFC3339Nano)
	server(t, r, func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/channels/500/messages" {
			w.WriteHeader(404)
			return
		}
		respond(w, []historyReadMessage{{Message: *m, Type: q.MessageType, Attachments: q.Attachments}})
	})
	if n, err := r.scan(context.Background(), Thread{ID: "500"}); err != nil || n != 0 || q.HistoryWatch != nil || q.Text != m.Content || len(q.PriorEdits) != 1 {
		t.Fatal("scan saved an edit with an invalid watch still attached", n, err)
	}
	if n, err := r.scan(context.Background(), Thread{ID: "500"}); err != nil || n != 0 || len(q.PriorEdits) != 1 || len(r.State.Requests) != 1 {
		t.Fatal("scan replay duplicated the owner source", n, err)
	}
}

func TestIntake3561RetiredUnknownThreadStaysManaged(t *testing.T) {
	r, m, in, _ := historyWatchFixture(t)
	if err := r.watchHistory(context.Background(), in, func(context.Context, string) bool { return true }); err != nil {
		t.Fatal(err)
	}
	q := r.State.Requests[m.ID]
	r.State.Since = q.Created
	var h Health
	r.refreshOrdinary(context.Background(), &h)
	if q.HistoryWatch != nil || q.Phase != "task_binding_pending" || q.PendingOwner != "OMB" || q.Todo != 0 || len(h.Errors) != 0 {
		t.Fatal("missing thread metadata fell through to automatic registration", q.Phase, h.Errors)
	}
}

func TestIntake3561ProductRecoveryRetainsRetryAfterValidationFailure(t *testing.T) {
	for _, failure := range []string{"scope", "owner-edit"} {
		t.Run(failure, func(t *testing.T) {
			f, q := savedReplyFixture(t)
			q.Origin, q.Phase = nil, "source_context_pending"
			if err := os.Remove(f.r.steeringReceiptPath(q.Todo, q.Message)); err != nil {
				t.Fatal(err)
			}
			if failure == "scope" {
				changed := false
				link := f.r.ProgressLinks[q.Thread]
				server(t, f.r, func(w http.ResponseWriter, req *http.Request) {
					if !changed && req.URL.Path == "/channels/"+q.Thread+"/messages/"+link.Message {
						changed = true
						f.mu.Lock()
						f.rows[0]["project"] = "different-fixture-project"
						f.saveRows()
						f.mu.Unlock()
					}
					f.serve(w, req)
				})
			} else {
				f.mu.Lock()
				m := f.messages[q.Thread+"/"+q.Message]
				m.Content, m.Edited = "corrected product instruction", f.now.Add(2*time.Second).Format(time.RFC3339Nano)
				f.messages[q.Thread+"/"+q.Message] = m
				f.mu.Unlock()
			}
			if err := f.r.recoverOrdinaryProductReply(context.Background(), q); err == nil || q.Origin != nil || q.Phase != "source_context_pending" {
				t.Fatal("failed recovery left a committed-looking origin without a worker receipt", err, q.Phase)
			}
			f.rows[0]["project"] = "fixture-project"
			f.saveRows()
			if err := f.r.recoverOrdinaryProductReply(context.Background(), q); err != nil {
				t.Fatal(err)
			}
			if plan, err := f.r.prepareSteeringDelivery(context.Background(), q.Message); err != nil || plan.Phase != "prepared" || f.sends != 1 {
				t.Fatal("failed recovery cannot later prepare a real finite delivery", err)
			}
		})
	}
}

func TestIntake3561OnlyConfiguredOriginalReceivesDeliveryAdvice(t *testing.T) {
	for _, kind := range []string{"different-message", "status-question"} {
		t.Run(kind, func(t *testing.T) {
			r, q, _, calls := ordinaryFixture(t)
			q.Todo, q.Phase = 42, "source_context_pending"
			cfg := ProductReportConfig{Todo: 42, Project: r.Config.Project, Source: OwnerSource{Kind: stateRequestSource, Message: q.Message}}
			if kind == "different-message" {
				other := *q
				other.Message, other.Key = "600", requestKey(q.Thread, "600")
				r.State.Requests[other.Message] = &other
				q = &other
			} else {
				cfg.Source.Purpose, cfg.Source.AnswerMessage, cfg.Source.AnswerReceipt, cfg.Source.AnswerReceiptSHA = "status_question", "600", filepath.Join(r.Config.Home, "answer.json"), strings.Repeat("a", 64)
			}
			r.Config.ProductReports = []ProductReportConfig{cfg}
			if action := r.ordinaryContextAction(q); action == "" || action == "verify_configured_request_source_and_deliver_original_task" {
				t.Fatal("a rejected source is instructed to retry the dedicated delivery", action)
			}
			if err := r.recoverConfiguredRequestSource(context.Background(), q); err != nil || *calls != 0 {
				t.Fatal("non-original configured source mutated a task", err)
			}
		})
	}
}

func TestIntake3561ForeignProductLinkReceivesAssociationAdvice(t *testing.T) {
	f, q := savedReplyFixture(t)
	q.Todo, q.Origin = 999, nil
	if action := f.r.ordinaryContextAction(q); action == "" || action == "verify_original_product_scope_and_use_finite_steering_delivery" {
		t.Fatal("a different original task is instructed to use an unavailable steering route", action)
	}
}

func TestIntake3561DiscoveryScanDoesNotSkipExactCutoverLast(t *testing.T) {
	r, c := fixture(t)
	m := msg(c, "500", "300", "exact first timestamp")
	r.State.Threads["500"] = Thread{ID: "500", Last: m.ID}
	server(t, r, func(w http.ResponseWriter, req *http.Request) { respond(w, []Message{m}) })
	var h Health
	r.scanIntakeThreads(context.Background(), &h, false, 1)
	if h.Scanned != 1 || h.Captured != 1 || r.State.Requests[m.ID] == nil || len(h.Errors) != 0 {
		t.Fatal("unscanned thread skipped the exact lower-bound message", h)
	}
}

func TestIntake3561PassiveWatchDoesNotRetainOldWorkerOwner(t *testing.T) {
	r, m, in, _ := historyWatchFixture(t)
	if err := r.watchHistory(context.Background(), in, func(context.Context, string) bool { return true }); err != nil {
		t.Fatal(err)
	}
	q := r.State.Requests[m.ID]
	q.PendingOwner = "OMB"
	var h Health
	r.refreshOrdinary(context.Background(), &h)
	var output strings.Builder
	load := func(string, bool) (*Runner, func(), error) { return r, func() {}, nil }
	if code := intakeCLIWith([]string{"status"}, &output, load, func(context.Context, string) bool { return true }); code != 0 {
		t.Fatal(code)
	}
	var status struct {
		Items []struct {
			Owner string `json:"pending_owner"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(output.String()), &status); err != nil || q.PendingOwner != "" || q.Phase != "history_watch" || len(status.Items) != 1 || status.Items[0].Owner != "" || len(h.Errors) != 0 {
		t.Fatal("passive watch looks like active worker work", err, h.Errors)
	}
}

func TestIntake3561SteeringReceiptKeepsMonthlyPathAtReceiver(t *testing.T) {
	f, q := savedReplyFixture(t)
	q.HistoryWatch = &HistoryWatch{Receipt: "PRIVATE_MONTHLY_RECEIPT", ReceiptSHA: strings.Repeat("a", 64), ScopeSHA: strings.Repeat("b", 64), Scope: monthlyAuditScope{Todo: 3387}}
	for _, text := range []string{"original source", "edited source"} {
		q.Text = text
		if err := f.r.publishSteeringSource(q); err != nil {
			t.Fatal(err)
		}
		var worker Request
		if err := readJSON(f.r.steeringReceiptPath(q.Todo, q.Message), maxState, &worker); err != nil || worker.HistoryWatch == nil || worker.HistoryWatch.Receipt != "" || worker.HistoryWatch.ReceiptSHA != q.HistoryWatch.ReceiptSHA || worker.HistoryWatch.ScopeSHA != q.HistoryWatch.ScopeSHA || worker.HistoryWatch.Scope.Todo != 3387 || worker.Text != text || q.HistoryWatch.Receipt != "PRIVATE_MONTHLY_RECEIPT" {
			t.Fatal("worker receipt leaked a monthly path or discarded source provenance", err)
		}
	}
}

func TestIntake3561ConfiguredOriginalInBotThreadKeepsDedicatedRoute(t *testing.T) {
	r, q, m, calls := ordinaryFixture(t)
	q.Todo, q.Phase = 42, "source_context_pending"
	r.Config.ProductReports = []ProductReportConfig{{Todo: 42, Project: r.Config.Project, Source: OwnerSource{Kind: stateRequestSource, Message: q.Message}}}
	server(t, r, func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/channels/500":
			respond(w, Thread{ID: "500", Parent: r.State.Destination.Forum, Guild: "100", Owner: "400", Type: 11})
		case "/channels/500/messages/" + m.ID:
			respond(w, m)
		default:
			w.WriteHeader(404)
		}
	})
	before, err := r.todoControl(42)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.recoverConfiguredRequestSource(context.Background(), q); err != nil || q.Phase != "source_edit_delivered" || *calls != 1 {
		t.Fatal("verified configured original fell into generic origin admission", err, q.Phase)
	}
	after, err := r.todoControl(42)
	if err != nil || before != after {
		t.Fatal("dedicated delivery changed task controls", err)
	}
}
