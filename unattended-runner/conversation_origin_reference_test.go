package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func originReferenceFixture(t *testing.T) (*actorFixture, conversationOriginContract) {
	t.Helper()
	f := actorTest(t, false)
	q := f.ask(t)
	q.Phase = "reserved"
	ix, _ := f.r.loadConversations()
	ix.Questions[q.RequestID] = q
	if err := publish(f.r.conversationPath(), ix); err != nil {
		t.Fatal(err)
	}
	return f, conversationOriginContract{RequestID: q.RequestID, SenderID: q.SenderID, Origin: q.OriginThread, InputSHA: q.InputSHA}
}

func TestConversationOriginReferenceReadOnlyAndCurrent(t *testing.T) {
	f, bound := originReferenceFixture(t)
	beforeTask, _ := os.ReadFile(f.r.TodoPath)
	beforeIndex, _ := os.ReadFile(f.r.conversationPath())
	proof, err := f.r.conversationOriginReference(context.Background(), bound.RequestID, bound)
	if err != nil || !proof.Verified || proof.Todo != 1171 || proof.Actor != 3277 || proof.SenderID != bound.SenderID || proof.NormalID != bound.RequestID || proof.Origin != bound.Origin || proof.InputSHA != bound.InputSHA || proof.SourceSHA != canonicalHash(f.proof) {
		t.Fatal("exact current reference rejected", err)
	}
	if proof.BodySHA == "" || proof.DecisionSHA == "" {
		t.Fatal("input binding missing")
	}
	afterTask, _ := os.ReadFile(f.r.TodoPath)
	afterIndex, _ := os.ReadFile(f.r.conversationPath())
	if string(beforeTask) != string(afterTask) || string(beforeIndex) != string(afterIndex) || f.posts != 1 {
		t.Fatal("reference verification changed state or sent a question")
	}
}

func TestConversationOriginReferenceRejectsChangedScope(t *testing.T) {
	for _, change := range []string{"request", "origin", "sender", "input", "body", "decision", "actor", "stop", "native", "phase", "project", "attachment", "spec"} {
		t.Run(change, func(t *testing.T) {
			f, bound := originReferenceFixture(t)
			if _, err := f.r.conversationOriginReference(context.Background(), bound.RequestID, bound); err != nil {
				t.Fatal("positive control", err)
			}
			ix, _ := f.r.loadConversations()
			q := ix.Questions[bound.RequestID]
			switch change {
			case "request":
				bound.RequestID = "other"
			case "origin":
				q.OriginThread = "999"
			case "sender":
				q.SenderID = "conversation-v1-other"
			case "input":
				q.InputSHA = "changed"
			case "body":
				_ = os.WriteFile(q.BodyFile, []byte("changed question"), 0600)
			case "decision":
				_ = os.WriteFile(q.DecisionFile, []byte(`{"todo_id":1171,"scope":"other"}`), 0600)
			case "actor":
				q.Actor.Todo = 999
			case "stop":
				f.mutate(t, 1171, func(row map[string]any) { row["owner_stopped"] = true })
			case "native":
				f.proof["source_kind"] = "assistant_quote"
			case "phase":
				q.Phase = "posted"
			case "project":
				f.mutate(t, 3277, func(row map[string]any) { row["project"] = "other" })
			case "attachment":
				q.Attachments = []string{"other.png"}
			case "spec":
				_ = os.WriteFile(f.r.Config.Home+"/actor.md", []byte("changed spec"), 0600)
			}
			ix.Questions[q.RequestID] = q
			if publish(f.r.conversationPath(), ix) != nil {
				t.Fatal("save")
			}
			before, _ := os.ReadFile(f.r.TodoPath)
			if _, err := f.r.conversationOriginReference(context.Background(), bound.RequestID, bound); err == nil {
				t.Fatal("unrelated or held source accepted")
			}
			after, _ := os.ReadFile(f.r.TodoPath)
			if string(before) != string(after) || f.posts != 1 {
				t.Fatal("rejection changed work or posted")
			}
		})
	}
}

func TestConversationOriginReferenceProductionContractIsFinite(t *testing.T) {
	b, _ := json.Marshal(fixedConversationOrigin)
	if fixedConversationOrigin.RequestID != "normal-ask-9620ba9f12adf45e1a01ee67010dafe616750231a1625ffcca0c7ed470a6a862" || fixedConversationOrigin.Origin != "1556320676015640587" || fixedConversationOrigin.InputSHA != "486a3210dca7e8110ff2cf3fb94ff0c1b2bd06ba99a344a6a8be54f800b7803c" || len(b) == 0 {
		t.Fatal("fixed original question changed")
	}
}

func TestConversationSenderFailureProtocolDoesNotExposeRawDiagnostics(t *testing.T) {
	for _, stage := range []string{"origin_scope", "source_scope", "busy", "network", "rate_limited", "deadline"} {
		b, _ := json.Marshal(map[string]string{"domain": "new-owner-recovery-messages", "delivery": "not_verified", "failure_stage": stage})
		if err := conversationSenderFailure(string(b)); err == nil || conversationFailureStage(err) != "sender_"+stage {
			t.Fatal("fixed sender cause collapsed", stage, err)
		}
	}
	for _, raw := range []string{"private body and token detail", `{"domain":"new-owner-recovery-messages","delivery":"not_verified","failure_stage":"private body and token detail"}`, `{"domain":"other","delivery":"not_verified","failure_stage":"origin_scope"}`} {
		if conversationSenderFailure(raw) != nil {
			t.Fatal("untrusted diagnostics accepted")
		}
	}
	var out bytes.Buffer
	if conversationOriginReferenceCLI([]string{"--request-id", "private-secret-value"}, &out) != 1 || out.String() != "conversation source reference unverified\n" {
		t.Fatal("invalid input echoed or reached production loader")
	}
}

func TestConversationSourceSenderBudgetIsFiniteAndRequestScoped(t *testing.T) {
	args := []string{"recovery-ask", "--conversation-source-request", fixedConversationOrigin.RequestID}
	budget := commandBudget(conversationSender, args)
	if budget <= 20*time.Second || budget >= conversationCLIDeadline {
		t.Fatal("sender budget cannot contain its bounded reader and HTTP work", budget)
	}
	for _, input := range []struct {
		path string
		args []string
	}{
		{conversationSender, []string{"recovery-ask"}},
		{conversationSender, []string{"recovery-ask", "--conversation-source-request", "unrelated"}},
		{conversationSender, []string{"recovery-log", "--conversation-source-request", fixedConversationOrigin.RequestID}},
		{"other", args},
	} {
		if commandBudget(input.path, input.args) != 20*time.Second {
			t.Fatal("unrelated subprocess budget expanded")
		}
	}
	if conversationFailureStage(conversationSenderFailure(`{"domain":"new-owner-recovery-messages","delivery":"not_verified","failure_stage":"operation_pending"}`)) != "send_pending" {
		t.Fatal("unconfirmed sender result lost its public pending stage")
	}
}

func TestConversationSourceMaintenanceDefersShortTurnBeforeAnyChild(t *testing.T) {
	f, _ := originReferenceFixture(t)
	ix, _ := f.r.loadConversations()
	old := ix.Questions[f.input.RequestID]
	q := old
	q.RequestID = fixedConversationOrigin.RequestID
	q.SenderID = fixedConversationOrigin.SenderID
	dir := filepath.Join(f.r.Config.Home, "conversations", q.RequestID)
	if os.MkdirAll(dir, 0700) != nil {
		t.Fatal("new private source directory")
	}
	for _, name := range []string{"question.txt", "decision.json", "actor-succession.json"} {
		b, err := os.ReadFile(filepath.Join(f.r.Config.Home, "conversations", old.RequestID, name))
		if err != nil || os.WriteFile(filepath.Join(dir, name), b, 0600) != nil {
			t.Fatal("copy exact fixture", err)
		}
	}
	q.BodyFile, q.DecisionFile = filepath.Join(dir, "question.txt"), filepath.Join(dir, "decision.json")
	delete(ix.Questions, old.RequestID)
	ix.Questions[q.RequestID] = q
	if publish(f.r.conversationPath(), ix) != nil {
		t.Fatal("index")
	}
	f.input.RequestID = q.RequestID
	backend := f.r.Run
	calls := 0
	f.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
		calls++
		return backend(ctx, path, args...)
	}
	before, _ := os.ReadFile(f.r.TodoPath)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	health := Health{}
	f.r.processConversationMaintenance(ctx, &health, map[string]bool{})
	if calls != 0 || len(health.Errors) != 1 || health.Errors[0] != "conversation_source_send_budget_deferred" {
		t.Fatal("short periodic turn started the fixed sender", calls, health.Errors)
	}
	after, _ := os.ReadFile(f.r.TodoPath)
	if string(before) != string(after) {
		t.Fatal("deferral changed original work")
	}
	afterIndex, err := f.r.loadConversations()
	if err != nil || afterIndex.Cursor != q.RequestID {
		t.Fatal("deferred turn did not advance the maintenance cursor")
	}
	long, stop := context.WithTimeout(context.Background(), time.Minute)
	defer stop()
	health = Health{}
	f.r.processConversationMaintenance(long, &health, map[string]bool{})
	if calls == 0 || len(health.Errors) != 0 {
		t.Fatal("valid long turn was not admitted", calls, health.Errors)
	}
}
