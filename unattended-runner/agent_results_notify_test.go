package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func resultReadback(r *Runner, p AgentResultRecord, notify bool) map[string]any {
	body := agentResultBody(p)
	flags := 4100
	mentions := []map[string]string{}
	if notify {
		body += "\n<@" + r.State.Destination.Owner + ">"
		flags = 4
		mentions = append(mentions, map[string]string{"id": r.State.Destination.Owner})
	}
	return map[string]any{"id": "735", "channel_id": p.Event.Thread,
		"author":  map[string]any{"id": r.State.Destination.Bot, "bot": true},
		"content": body + "\n\n-# recovery:aaaaaaaaaaaaaaaaaaaaaaaa:log:0",
		"flags":   flags, "mentions": mentions, "mention_roles": []string{}, "mention_everyone": false}
}

func legacyAgentReceipt(t *testing.T, r *Runner, e AgentResultEvent) AgentResultRecord {
	t.Helper()
	p, err := r.recordAgentResult(e)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var old map[string]any
	if json.Unmarshal(b, &old) != nil {
		t.Fatal("fixture")
	}
	delete(old, "notify_owner") // Actual pre-correction persisted format.
	if err := publish(r.agentResultPath(p.ID), old); err != nil {
		t.Fatal(err)
	}
	return r.mustAgentResult(t, p.ID)
}

func TestAgentResultNewRepliesStopsAndItemsNotifyOwner(t *testing.T) {
	for _, state := range []string{"completed", "stopped", "start_failed", "item_deferred"} {
		t.Run(state, func(t *testing.T) {
			r, _ := fixture(t)
			e := resultEvent()
			e.Thread = "620" // A question's progress thread, distinct from the result topic.
			e.State = state
			if state == "completed" {
				e.Reason, e.Remaining, e.Next = "completed", "none", "none"
				e.OwnerReply = true // Parent's actual response to an owner request.
			}
			if state == "start_failed" {
				e.Source, e.Parent, e.Child, e.Reason = "launcher", "", "", "initialization_rejected"
			}
			if state == "item_deferred" {
				e = agentDeferredEvent(e, AgentDeferredItem{ID: "owned-item", Kind: "scratch",
					Transition: "hold", Reason: "cleanup_busy", Scope: "unknown", Next: "retry"})
			}
			p, err := r.recordAgentResult(e)
			if err != nil {
				t.Fatal(err)
			}
			b, _ := json.Marshal(p)
			var receipt map[string]any
			_ = json.Unmarshal(b, &receipt)
			if receipt["notify_owner"] != true {
				t.Error("new parent receipt did not persist the owner-notification decision")
			}
			calls := 0
			r.Run = func(_ context.Context, _ string, args ...string) (string, error) {
				calls++
				if !slices.Contains(args, "--notify") {
					t.Error("new result did not use the normal notifying sender")
				}
				if slices.Contains(args, "--thread") {
					t.Error("notifying result targeted a possibly silent question-progress thread")
				}
				return `{"domain":"new-owner-recovery-messages","delivery":"verified","thread_id":"610","message_id":"735"}`, nil
			}
			server(t, r, func(w http.ResponseWriter, req *http.Request) {
				if req.Method != "GET" || req.URL.Path != "/channels/610/messages/735" {
					t.Errorf("unexpected readback %s %s", req.Method, req.URL.Path)
				}
				m := resultReadback(r, p, true)
				m["channel_id"] = "610"
				respond(w, m)
			})
			r.agentResultPhase(context.Background(), &Health{})
			saved := r.mustAgentResult(t, p.ID)
			if saved.Verified == "" || saved.Message != "735" || saved.Event.Thread != "620" || !strings.Contains(saved.URL, "/610/735") {
				t.Fatal("owner mention was not GET-verified")
			}
			again, err := r.recordAgentResult(e)
			if err != nil || again.ID != p.ID || again.Verified == "" {
				t.Fatal("same transition lost its verified receipt")
			}
			r.agentResultPhase(context.Background(), &Health{})
			if calls != 1 {
				t.Fatal("same verified transition notified more than once")
			}
		})
	}
}

func TestAgentResultNotifyRequiresExactOwnerReadback(t *testing.T) {
	for _, mismatch := range []string{"silent", "missing", "foreign", "extra", "duplicate_body", "role", "roles_missing", "everyone"} {
		t.Run(mismatch, func(t *testing.T) {
			r, _ := fixture(t)
			p, _ := r.recordAgentResult(resultEvent())
			r.Run = func(context.Context, string, ...string) (string, error) {
				return `{"domain":"new-owner-recovery-messages","delivery":"verified","thread_id":"610","message_id":"735"}`, nil
			}
			server(t, r, func(w http.ResponseWriter, _ *http.Request) {
				m := resultReadback(r, p, true)
				switch mismatch {
				case "silent":
					m["flags"] = 4100
				case "missing":
					m["mentions"] = []any{}
				case "foreign":
					m["mentions"] = []map[string]string{{"id": "999"}}
				case "extra":
					m["mentions"] = []map[string]string{{"id": "300"}, {"id": "999"}}
				case "duplicate_body":
					m["content"] = strings.Replace(m["content"].(string), "<@300>", "<@300>\n<@300>", 1)
				case "role":
					m["mention_roles"] = []string{"999"}
				case "roles_missing":
					delete(m, "mention_roles")
				case "everyone":
					m["mention_everyone"] = true
				}
				respond(w, m)
			})
			r.agentResultPhase(context.Background(), &Health{})
			saved := r.mustAgentResult(t, p.ID)
			if saved.Verified != "" || saved.LastError != "delivery_unverified" {
				t.Fatal("mismatched owner notification was marked reported")
			}
		})
	}
}

func TestAgentResultLegacyReceiptsDoNotAcquireNewMentions(t *testing.T) {
	r, _ := fixture(t)
	p := legacyAgentReceipt(t, r, resultEvent())
	calls := 0
	r.Run = func(_ context.Context, _ string, args ...string) (string, error) {
		calls++
		if slices.Contains(args, "--notify") {
			t.Error("old reserved input was changed into a mention")
		}
		return `{"domain":"new-owner-recovery-messages","delivery":"verified","thread_id":"610","message_id":"735"}`, nil
	}
	server(t, r, func(w http.ResponseWriter, _ *http.Request) { respond(w, resultReadback(r, p, false)) })
	r.agentResultPhase(context.Background(), &Health{})
	if r.mustAgentResult(t, p.ID).Verified == "" {
		t.Fatal("legacy silent reservation could not finish unchanged")
	}
	now := r.Now().Add(time.Hour)
	r.Now = func() time.Time { return now }
	_, err := r.recordAgentResult(resultEvent())
	if err != nil {
		t.Fatal(err)
	}
	r.agentResultPhase(context.Background(), &Health{})
	if calls != 1 {
		t.Fatal("old verified receipt was notified again")
	}
}

func TestAgentResultTellsOwnerWhetherActionIsNeeded(t *testing.T) {
	r, _ := fixture(t)
	e := resultEvent()
	p, err := r.recordAgentResult(e)
	if err != nil || !strings.HasPrefix(agentResultBody(p), "対応不要です。AI側で保存した結果を確認して作業を続けます。") {
		t.Fatal("no-action notice did not lead with the concrete AI action", err)
	}
	e.Transition = "question-required"
	e.OwnerRequired, e.Next = true, "owner_decision"
	if _, err := r.recordAgentResult(e); err == nil {
		t.Fatal("action-required notice accepted without a concrete question")
	}
	e.OwnerQuestion = AgentOwnerQuestion{Question: "この対象の処置を選んでください。", ChoiceA: "保持して調査を続ける", ChoiceB: "確認済みの不要物だけ回収する", URL: "https://discord.com/channels/100/620/621"}
	p, err = r.recordAgentResult(e)
	if err != nil {
		t.Fatal(err)
	}
	body := agentResultBody(p)
	if !strings.HasPrefix(body, "回答が必要："+e.OwnerQuestion.Question) || !strings.Contains(body, "a: "+e.OwnerQuestion.ChoiceA) || !strings.Contains(body, "b: "+e.OwnerQuestion.ChoiceB) || !strings.Contains(body, e.OwnerQuestion.URL) || !strings.Contains(body, "この対象だけ回答を待ち") {
		t.Fatal("question, choices or limited waiting scope missing")
	}
	e.OwnerRequired, e.Next = false, "review_result"
	e.Transition = "unexpected-question"
	if _, err := r.recordAgentResult(e); err == nil {
		t.Fatal("no-action notice accepted an owner question")
	}
}

func TestAgentResultInternalCompletionAndObservationStaySilent(t *testing.T) {
	for _, reason := range []string{"completed", "observation_unavailable", "result_wait_limit"} {
		r, _ := fixture(t)
		e := resultEvent()
		e.Reason = reason
		if reason == "completed" {
			e.State, e.Remaining, e.Next = "completed", "none", "none"
		} else {
			e.State = "unverified"
		}
		p, err := r.recordAgentResult(e)
		if err != nil {
			t.Fatal(err)
		}
		if agentResultPublic(p)["notify_owner"] != false {
			t.Errorf("internal completion/periodic observation notified the owner: %s", reason)
		}
	}
}

func TestAgentResultCapacityRetryClockDoesNotRepeatTheSameWait(t *testing.T) {
	r, _ := fixture(t)
	r.JobsPath = filepath.Join(t.TempDir(), "jobs")
	r.AgentResultGuard = func(context.Context, string) bool { return true }
	e := resultEvent()
	e.Source, e.Child = "codex_jobs", "worker-capacity"
	path := filepath.Join(r.JobsPath, e.Child, "job.json")
	if publish(path, map[string]any{"id": e.Child, "status": "running"}) != nil || r.watchAgentResult(e) != nil {
		t.Fatal("watch fixture")
	}
	for i, retry := range []string{"2026-10-21T01:00:00Z", "2026-10-21T01:10:00Z"} {
		if publish(path, map[string]any{"id": e.Child, "status": "queued", "last_failure_kind": "capacity", "capacity_retry_at": retry, "attempts": i + 1}) != nil {
			t.Fatal("capacity fixture")
		}
		r.observeAgentResults(context.Background(), &Health{})
	}
	files, _ := agentResultFiles(r.Config.Home, "records")
	if len(files) != 1 {
		t.Fatalf("one continuous capacity wait generated %d notifications", len(files))
	}
}

func TestAgentResultQuestionDeliveryAndOutOfScopeFailure(t *testing.T) {
	for _, guild := range []string{"100", "999"} {
		t.Run(guild, func(t *testing.T) {
			r, _ := fixture(t)
			e := resultEvent()
			e.OwnerRequired, e.Next = true, "owner_decision"
			e.OwnerQuestion = AgentOwnerQuestion{Question: "保持方針を選んでください。", ChoiceA: "保持を続ける", ChoiceB: "確認済みの不要物だけ回収する", URL: "https://discord.com/channels/" + guild + "/620/621"}
			p, err := r.recordAgentResult(e)
			if err != nil {
				t.Fatal(err)
			}
			sends := 0
			r.Run = func(context.Context, string, ...string) (string, error) {
				sends++
				return `{"domain":"new-owner-recovery-messages","delivery":"verified","thread_id":"610","message_id":"735"}`, nil
			}
			server(t, r, func(w http.ResponseWriter, _ *http.Request) { respond(w, resultReadback(r, p, true)) })
			r.agentResultPhase(context.Background(), &Health{})
			saved := r.mustAgentResult(t, p.ID)
			if guild == "100" {
				if sends != 1 || saved.Verified == "" {
					t.Fatal("concrete question was not verified")
				}
			} else {
				if sends != 0 || saved.Verified != "" || agentResultPublic(saved)["error_code"] != "owner_question_scope_invalid" {
					t.Fatal("foreign question URL was sent or cause was hidden")
				}
				r.agentResultPhase(context.Background(), &Health{})
				if r.mustAgentResult(t, p.ID).Attempts != 1 {
					t.Fatal("permanent question scope failure was retried")
				}
			}
		})
	}
}

func TestAgentResultIncompleteHeldItemReturnsOneParentDiagnostic(t *testing.T) {
	r, _ := fixture(t)
	r.JobsPath = filepath.Join(t.TempDir(), "jobs")
	r.AgentResultGuard = func(context.Context, string) bool { return true }
	e := resultEvent()
	e.Source, e.Child = "codex_jobs", "worker-incomplete-item"
	path := filepath.Join(r.JobsPath, e.Child, "job.json")
	if publish(path, map[string]any{"id": e.Child, "status": "running"}) != nil || r.watchAgentResult(e) != nil {
		t.Fatal("watch")
	}
	item := AgentDeferredItem{ID: "held", Kind: "scratch", Transition: "hold-1", Reason: "cleanup_busy", Scope: "unknown", Next: "owner_decision", OwnerRequired: true}
	for range 2 {
		if publish(path, map[string]any{"id": e.Child, "status": "running", "item_deferred": []AgentDeferredItem{item}}) != nil {
			t.Fatal("item")
		}
		r.observeAgentResults(context.Background(), &Health{})
	}
	files, _ := agentResultFiles(r.Config.Home, "records")
	if len(files) != 1 {
		t.Fatal("incomplete item was lost or repeatedly returned")
	}
	notice := r.mustAgentResult(t, files[0])
	if notice.Event.Reason != "item_progress_invalid" || notice.ParentReceived == "" || notice.NotifyOwner {
		t.Fatal("parent diagnostic asked owner to act without a question")
	}
}

func TestAgentResultOldWaitSignatureDoesNotReplayAtDeployment(t *testing.T) {
	r, _ := fixture(t)
	r.JobsPath = filepath.Join(t.TempDir(), "jobs")
	r.AgentResultGuard = func(context.Context, string) bool { return true }
	e := resultEvent()
	e.Source, e.Child = "codex_jobs", "worker-old-wait"
	path := filepath.Join(r.JobsPath, e.Child, "job.json")
	save := func(status string) {
		t.Helper()
		if publish(path, map[string]any{"id": e.Child, "status": status, "last_failure_kind": "capacity", "capacity_retry_at": "2026-10-21T02:00:00Z"}) != nil {
			t.Fatal("job")
		}
	}
	save("running")
	if r.watchAgentResult(e) != nil {
		t.Fatal("watch")
	}
	watchPath := r.agentWatchPath(agentWatchID(e))
	var w agentResultWatch
	if readJSON(watchPath, 1<<20, &w) != nil {
		t.Fatal("watch read")
	}
	w.StableWaitSignature, w.Signature, w.Generation = false, strings.Repeat("b", 64), 1
	if publish(watchPath, w) != nil {
		t.Fatal("legacy watch")
	}
	save("queued")
	r.observeAgentResults(context.Background(), &Health{})
	files, _ := agentResultFiles(r.Config.Home, "records")
	if len(files) != 0 {
		t.Fatal("signature migration reposted an existing wait")
	}
	save("running")
	r.observeAgentResults(context.Background(), &Health{})
	save("queued")
	r.observeAgentResults(context.Background(), &Health{})
	files, _ = agentResultFiles(r.Config.Home, "records")
	if len(files) != 1 || !r.mustAgentResult(t, files[0]).NotifyOwner {
		t.Fatal("a new wait after running was not reported")
	}
}

func TestAgentResultOldWaitMigrationStillReportsChangedWaitKind(t *testing.T) {
	r, _ := fixture(t)
	r.JobsPath = filepath.Join(t.TempDir(), "jobs")
	r.AgentResultGuard = func(context.Context, string) bool { return true }
	e := resultEvent()
	e.Source, e.Child = "codex_jobs", "worker-changed-wait"
	path := filepath.Join(r.JobsPath, e.Child, "job.json")
	if publish(path, map[string]any{"id": e.Child, "status": "running"}) != nil || r.watchAgentResult(e) != nil {
		t.Fatal("watch")
	}
	watchPath := r.agentWatchPath(agentWatchID(e))
	var w agentResultWatch
	if readJSON(watchPath, 1<<20, &w) != nil {
		t.Fatal("watch read")
	}
	w.StableWaitSignature, w.Signature, w.Generation = false, strings.Repeat("c", 64), 1
	prior := e
	prior.Transition = "observed-1-" + strings.Repeat("c", 32)
	prior.State, prior.Reason, prior.Next = "dependency_wait", "dependency", "wait_dependency"
	p, err := r.recordAgentResult(prior)
	if err != nil {
		t.Fatal(err)
	}
	p.Verified = r.Now().UTC().Format(time.RFC3339Nano)
	if publish(r.agentResultPath(p.ID), p) != nil || r.archiveAgentFile("records", "verified", p.ID) != nil || publish(watchPath, w) != nil {
		t.Fatal("archived legacy wait")
	}
	if publish(path, map[string]any{"id": e.Child, "status": "queued", "last_failure_kind": "capacity"}) != nil {
		t.Fatal("capacity")
	}
	r.observeAgentResults(context.Background(), &Health{})
	files, _ := agentResultFiles(r.Config.Home, "records")
	if len(files) != 1 {
		t.Fatal("known change from dependency to capacity wait was dropped")
	}
	saved := r.mustAgentResult(t, files[0])
	if saved.Event.State != "capacity_wait" || !saved.NotifyOwner {
		t.Fatal("new capacity wait was not a notifying result")
	}
}

func TestAgentResultDistinctStopsDoNotAliasAtTheOrdinarySender(t *testing.T) {
	r, _ := fixture(t)
	index, err := r.loadConversations()
	if err != nil {
		t.Fatal(err)
	}
	index.Topics = map[string]string{"【PCの道具】担当の結果": "610"}
	if publish(r.conversationPath(), index) != nil {
		t.Fatal("existing ordinary result topic")
	}
	receipts := []AgentResultRecord{}
	for _, transition := range []string{"stop-1", "stop-2"} {
		e := resultEvent()
		e.Transition = transition
		p, err := r.recordAgentResult(e)
		if err != nil {
			t.Fatal(err)
		}
		receipts = append(receipts, p)
	}
	// The real sender deduplicates log presentation fingerprints, NOT request IDs
	// (cc-ask/recovery_messages.rs Request::fingerprint and reserve).
	byBody, byID := map[string]string{}, map[string]string{}
	r.Run = func(_ context.Context, _ string, args ...string) (string, error) {
		body, err := boundedRegular(argValue(args, "--body-file"))
		if err != nil {
			return "", err
		}
		if argValue(args, "--thread") != "610" {
			t.Error("existing ordinary topic binding lost")
		}
		key := bytesHash(body)
		id := byBody[key]
		if id == "" {
			id = fmt.Sprintf("%d", 740+len(byBody))
			byBody[key], byID[id] = id, string(body)
		}
		return fmt.Sprintf(`{"domain":"new-owner-recovery-messages","delivery":"verified","thread_id":"610","message_id":"%s"}`, id), nil
	}
	server(t, r, func(w http.ResponseWriter, req *http.Request) {
		id := strings.TrimPrefix(req.URL.Path, "/channels/610/messages/")
		m := resultReadback(r, AgentResultRecord{Event: resultEvent(), Body: byID[id]}, true)
		m["id"] = id
		respond(w, m)
	})
	for range 4 {
		r.agentResultPhase(context.Background(), &Health{})
	}
	a, b := r.mustAgentResult(t, receipts[0].ID), r.mustAgentResult(t, receipts[1].ID)
	if len(byBody) != 2 || a.Verified == "" || b.Verified == "" || a.Message == b.Message {
		t.Fatal("distinct actual stops were aliased into one owner notification")
	}
}
