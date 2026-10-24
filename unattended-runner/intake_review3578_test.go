package main

import (
	"context"
	"testing"
	"time"
)

func TestIntake3578UnknownEmptyAndStatusKeepTheirNonTaskDisposition(t *testing.T) {
	for _, body := range []string{"", "今作業してる？"} {
		t.Run(body, func(t *testing.T) {
			r, q, _, _ := ordinaryFixture(t)
			q.Text = body
			r.classifyCaptured(q, Thread{}, "")
			want := "empty"
			if body != "" {
				want = "status_query"
			}
			if q.Phase != want || q.Todo != 0 || q.PendingOwner != "" || q.NextAction != "" {
				t.Fatal("non-task source acquired an unresolved task disposition", q.Phase, q.NextAction)
			}
		})
	}
}

func TestIntake3578UnknownStatusGatePreventsANewSend(t *testing.T) {
	f := progressTestFixture(t)
	f.r.Config.ProductReports = nil
	f.source.Content = "今作業してる？"
	f.messages[f.source.Channel+"/"+f.source.ID] = f.source
	q := captured(f.source, "status_query", f.now)
	f.r.State.Requests[q.Message] = q
	delete(f.r.State.Threads, q.Thread)
	if err := f.r.answerStatusQuery(context.Background(), q); err != nil || q.Phase != "task_binding_pending" || q.Todo != 0 || f.sends != 0 {
		t.Fatal("unknown status question bypassed its direct send gate", err, q.Phase, f.sends)
	}
	// The identical caller can send after real discovery, proving the spy is live.
	f.r.State.Threads[q.Thread] = Thread{ID: q.Thread, Parent: requestForum, Guild: "100", Owner: "300", Type: 11}
	q.Phase = "status_query"
	if err := f.r.answerStatusQuery(context.Background(), q); err != nil || q.Phase != "status_answered" || f.sends != 1 {
		t.Fatal("known-thread control did not reach the normal sender", err, q.Phase, f.sends)
	}
}

func TestIntake3578UnknownStatusStillRecognizesItsVerifiedExistingAnswer(t *testing.T) {
	f, q := existingStatusAnswerFixture(t)
	delete(f.r.State.Threads, q.Thread)
	f.r.classifyCaptured(q, Thread{}, "")
	if q.Phase != "status_query" {
		t.Fatal("classification hid the existing-answer verification path", q.Phase)
	}
	if err := f.r.answerStatusQuery(context.Background(), q); err != nil || q.Phase != "status_answered" || q.Todo != 2705 || f.sends != 0 {
		t.Fatal("verified prior answer was lost or resent", err, q.Phase, f.sends)
	}
}

func TestIntake3578SavedEmptyAndStatusEditsStayUnbound(t *testing.T) {
	for _, phase := range []string{"empty", "status_query", "status_answered"} {
		t.Run(phase, func(t *testing.T) {
			r, q, m, calls := ordinaryFixture(t)
			q.Phase, q.Text = phase, ""
			m.Content, m.Edited = "元の依頼への訂正", r.Now().Add(-time.Second).Format(time.RFC3339Nano)
			if changed, err := r.observeOrdinary(q, historyReadMessage{Message: *m}); err != nil || !changed || q.Phase != "task_binding_pending" || q.Todo != 0 || *calls != 0 {
				t.Fatal("saved non-task edit escaped unknown-thread routing", err, q.Phase, *calls)
			}
		})
	}
}

func TestIntake3578ExplicitDeliveryAndPreparationRetireOldWatch(t *testing.T) {
	t.Run("ordinary delivery", func(t *testing.T) {
		r, q, _, calls := ordinaryFixture(t)
		q.Todo, q.Phase = 42, "source_edit_saved"
		q.Binding = &IntakeBinding{Todo: 42, Project: r.Config.Project, SourceSHA: ordinaryVersion(q), Evidence: "original task scope verified"}
		q.HistoryWatch = &HistoryWatch{Receipt: "old-post-cutover.private.json"}
		if err := r.deliverOrdinaryTask(context.Background(), q, false); err != nil || q.HistoryWatch != nil || q.Phase != "source_edit_delivered" || *calls != 1 {
			t.Fatal("explicit delivery waited for fair refresh", err, q.Phase, *calls)
		}
	})
	t.Run("steering preparation", func(t *testing.T) {
		f, q := savedReplyFixture(t)
		q.HistoryWatch = &HistoryWatch{Receipt: "old-post-cutover.private.json"}
		if p, err := f.r.prepareSteeringDelivery(context.Background(), q.Message); err != nil || q.HistoryWatch != nil || p.Phase != "prepared" || q.Phase != "product_reply_saved" {
			t.Fatal("preparation waited for fair refresh", err, q.Phase)
		}
	})
}
