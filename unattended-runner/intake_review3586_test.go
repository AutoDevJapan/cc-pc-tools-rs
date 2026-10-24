package main

import "testing"

func TestIntake3586AttachmentOnlySourcesRetainTheirOriginalRouting(t *testing.T) {
	for _, kind := range []string{"unknown", "legacy", "managed", "question", "product"} {
		t.Run(kind, func(t *testing.T) {
			r, q, _, _ := ordinaryFixture(t)
			q.Text = " \n"
			attachments := []historyReadAttachment{{ID: "700"}}
			q.Attachments = &attachments
			thread := Thread{ID: q.Thread, Parent: requestForum, Guild: "100", Owner: "300", Type: 11}
			want := "task_binding_pending"
			switch kind {
			case "legacy":
				want = "captured"
			case "managed":
				thread.Managed = true
			case "question":
				thread.Owner, thread.Managed = "400", true
				r.KnownQuestionThreads = map[string]bool{q.Thread: true}
				want = "context_pending"
			case "product":
				thread.Managed = true
				q.Origin = &ProgressOrigin{Scope: productReportScope, Todo: 42}
				want = "source_context_pending"
			}
			if kind != "unknown" {
				r.State.Threads[q.Thread] = thread
			}
			r.classifyCaptured(q, thread, "")
			if q.Phase != want || q.Attachments == nil || len(*q.Attachments) != 1 || q.Todo != 0 {
				t.Fatal("attachment-only source was dropped or lost its routing", q.Phase, want)
			}
		})
	}
}
