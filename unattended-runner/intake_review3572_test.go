package main

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestIntake3572UnknownStarterStaysHeldAfterEmptyOrStatusEdit(t *testing.T) {
	for _, original := range []string{"", "今作業してる？"} {
		t.Run(original, func(t *testing.T) {
			r, q, m, _ := ordinaryFixture(t)
			q.Thread, m.Channel = q.Message, q.Message
			q.Key = requestKey(q.Thread, q.Message)
			q.Text, m.Content, q.Phase = original, original, "history_watch"
			q.HistoryWatch = &HistoryWatch{Receipt: "old-monthly.private.json"}
			server(t, r, func(w http.ResponseWriter, req *http.Request) {
				switch req.URL.Path {
				case "/channels/" + q.Thread:
					respond(w, Thread{ID: q.Thread, Guild: "100", Parent: requestForum, Owner: "300", Type: 11})
				case "/channels/" + q.Thread + "/messages/" + q.Message:
					respond(w, m)
				default:
					w.WriteHeader(404)
				}
			})
			adds, sends := 0, 0
			base := r.Run
			r.Run = func(ctx context.Context, exe string, args ...string) (string, error) {
				if exe == conversationTodo && len(args) != 0 && args[0] == "add" {
					adds++
					return "added 99", nil
				}
				if strings.HasSuffix(exe, "discord_ask.exe") {
					sends++
					return "", errors.New("unexpected status send")
				}
				return base(ctx, exe, args...)
			}
			var h Health
			r.refreshOrdinary(context.Background(), &h)
			initialHeld := q.Phase == "task_binding_pending"
			_ = r.results(context.Background())
			m.Content, m.Edited = "元の依頼への具体的な訂正", r.Now().Add(-time.Second).Format(time.RFC3339Nano)
			r.refreshOrdinary(context.Background(), &h)
			r.admit(context.Background(), &h)
			if !initialHeld || q.HistoryWatch != nil || q.Phase != "task_binding_pending" || q.Todo != 0 || adds != 0 || sends != 0 {
				t.Fatal("unknown starter escaped its hold", initialHeld, q.Phase, adds, sends)
			}
			// The add spy must be reachable when discovery supplies a known legacy
			// thread; the zero-call assertion above is not a disconnected mock.
			r.State.Threads[q.Thread] = Thread{ID: q.Thread, Guild: "100", Parent: requestForum, Owner: "300", Type: 11}
			q.Phase = "captured"
			if err := r.register(context.Background(), q); err != nil || adds != 1 || q.Todo != 99 {
				t.Fatal("known-thread positive control did not reach registration", err, adds)
			}
		})
	}
}

func TestIntake3572ProductStatusEditMustSaveARealWorkerReceipt(t *testing.T) {
	f, q := savedReplyFixture(t)
	q.Phase = "status_answered"
	f.mu.Lock()
	m := f.messages[q.Thread+"/"+q.Message]
	m.Content, m.Edited = "元の製品への具体的な追加指示", f.now.Add(2*time.Second).Format(time.RFC3339Nano)
	f.messages[q.Thread+"/"+q.Message] = m
	f.mu.Unlock()
	if _, err := f.r.observeOrdinary(q, historyReadMessage{Message: m}); err != nil || q.Phase != "source_context_pending" {
		t.Fatal("a status record became a saved product reply without its current worker receipt", err, q.Phase)
	}
	if err := f.r.recoverOrdinaryProductReply(context.Background(), q); err != nil {
		t.Fatal(err)
	}
	var receipt Request
	if err := readJSON(f.r.steeringReceiptPath(q.Todo, q.Message), maxState, &receipt); err != nil || receipt.Text != m.Content || receipt.Edited != m.Edited {
		t.Fatal("recovery did not write the actual edited source", err)
	}
}

func TestIntake3572CachedQuestionContextCannotRegisterAnUnknownThread(t *testing.T) {
	r, q, _, _ := ordinaryFixture(t)
	q.Phase, q.Context = "captured", "verified question context"
	q.QuestionContextVersion = ordinaryVersion(q)
	r.KnownQuestionThreads = map[string]bool{q.Thread: true}
	adds := 0
	base := r.Run
	r.Run = func(ctx context.Context, exe string, args ...string) (string, error) {
		if exe == conversationTodo && len(args) != 0 && args[0] == "add" {
			adds++
			return "added 99", nil
		}
		return base(ctx, exe, args...)
	}
	if err := r.register(context.Background(), q); err != nil || adds != 0 || q.Todo != 0 || q.Phase != "source_context_pending" {
		t.Fatal("cached question context bypassed unknown-thread routing", err, adds, q.Phase)
	}
}

func TestIntake3572OldProductAdviceIsReclassifiedWithoutAnotherEdit(t *testing.T) {
	f, q := savedReplyFixture(t)
	q.Origin, q.Todo, q.Phase = nil, 999, "source_context_pending"
	q.NextAction = "verify_original_product_scope_and_use_finite_steering_delivery"
	var h Health
	f.r.refreshOrdinary(context.Background(), &h)
	if q.NextAction != "review_original_product_link_and_task_association" || q.Phase != "source_context_pending" || f.sends != 1 || len(h.Errors) != 0 {
		t.Fatal("old saved advice still offers an unavailable delivery", q.NextAction, h.Errors)
	}
}

func TestIntake3572OldWorkerPathIsRemovedBeforePreparingANewPlan(t *testing.T) {
	f, q := savedReplyFixture(t)
	old := *q
	old.HistoryWatch = &HistoryWatch{Receipt: "PRIVATE_OLD_MONTHLY_PATH", ReceiptSHA: strings.Repeat("a", 64)}
	if err := publish(f.r.steeringReceiptPath(q.Todo, q.Message), &old); err != nil {
		t.Fatal(err)
	}
	if _, err := f.r.prepareSteeringDelivery(context.Background(), q.Message); err == nil {
		t.Fatal("old worker path was accepted as a current delivery receipt")
	}
	if p, err := f.r.prepareSteeringDelivery(context.Background(), q.Message); err != nil || p.Phase != "prepared" {
		t.Fatal("receipt migration did not leave an executable new plan", err)
	}
	var saved Request
	if err := readJSON(filepath.Join(f.r.Config.Home, "steering", "2639-"+q.Message+".json"), maxState, &saved); err != nil || saved.HistoryWatch != nil {
		t.Fatal("obsolete worker path remained after migration", err)
	}
}

func TestIntake3572ExplicitBindRetiresWatchBeforeItsFairRefreshTurn(t *testing.T) {
	r, q, _, calls := ordinaryFixture(t)
	q.HistoryWatch = &HistoryWatch{Receipt: "old-post-cutover.private.json"}
	plan := IntakeBinding{Todo: 42, Project: r.Config.Project, SourceSHA: ordinaryVersion(q), Evidence: "original task scope verified"}
	if err := r.bindOrdinary(context.Background(), q.Message, plan); err != nil || q.HistoryWatch != nil || *calls != 1 || q.Phase != "source_edit_delivered" {
		t.Fatal("explicit delivery waited for an unrelated fair cursor", err, q.Phase)
	}
}
