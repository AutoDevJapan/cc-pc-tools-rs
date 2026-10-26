package main

import (
	"context"
	"encoding/json"
	"os"
	"testing"
)

func originReferenceFixture(t *testing.T) (*actorFixture, conversationOriginContract) {
	t.Helper()
	f := actorTest(t, false)
	q := f.ask(t)
	q.Phase = "reserved"
	ix, _ := f.r.loadConversations()
	ix.Questions[q.RequestID] = q
	if err := publish(f.r.conversationPath(), ix); err != nil { t.Fatal(err) }
	return f, conversationOriginContract{RequestID: q.RequestID, SenderID: q.SenderID, Origin: q.OriginThread, InputSHA: q.InputSHA}
}

func TestConversationOriginReferenceReadOnlyAndCurrent(t *testing.T) {
	f, bound := originReferenceFixture(t)
	beforeTask, _ := os.ReadFile(f.r.TodoPath)
	beforeIndex, _ := os.ReadFile(f.r.conversationPath())
	proof, err := f.r.conversationOriginReference(context.Background(), bound.RequestID, bound)
	if err != nil || !proof.Verified || proof.Todo != 1171 || proof.Actor != 3277 || proof.SenderID != bound.SenderID || proof.NormalID != bound.RequestID || proof.Origin != bound.Origin || proof.InputSHA != bound.InputSHA || proof.SourceSHA != canonicalHash(f.proof) { t.Fatal("exact current reference rejected", err) }
	if proof.BodySHA == "" || proof.DecisionSHA == "" { t.Fatal("input binding missing") }
	afterTask, _ := os.ReadFile(f.r.TodoPath)
	afterIndex, _ := os.ReadFile(f.r.conversationPath())
	if string(beforeTask) != string(afterTask) || string(beforeIndex) != string(afterIndex) || f.posts != 1 { t.Fatal("reference verification changed state or sent a question") }
}

func TestConversationOriginReferenceRejectsChangedScope(t *testing.T) {
	for _, change := range []string{"request", "origin", "sender", "input", "body", "decision", "actor", "stop", "native", "phase", "project", "attachment", "spec"} {
		t.Run(change, func(t *testing.T) {
			f, bound := originReferenceFixture(t)
			if _, err := f.r.conversationOriginReference(context.Background(), bound.RequestID, bound); err != nil { t.Fatal("positive control", err) }
			ix, _ := f.r.loadConversations()
			q := ix.Questions[bound.RequestID]
			switch change {
			case "request": bound.RequestID = "other"
			case "origin": q.OriginThread = "999"
			case "sender": q.SenderID = "conversation-v1-other"
			case "input": q.InputSHA = "changed"
			case "body": _ = os.WriteFile(q.BodyFile, []byte("changed question"), 0600)
			case "decision": _ = os.WriteFile(q.DecisionFile, []byte(`{"todo_id":1171,"scope":"other"}`), 0600)
			case "actor": q.Actor.Todo = 999
			case "stop": f.mutate(t, 1171, func(row map[string]any) { row["owner_stopped"] = true })
			case "native": f.proof["source_kind"] = "assistant_quote"
			case "phase": q.Phase = "posted"
			case "project": f.mutate(t, 3277, func(row map[string]any) { row["project"] = "other" })
			case "attachment": q.Attachments = []string{"other.png"}
			case "spec": _ = os.WriteFile(f.r.Config.Home+"/actor.md", []byte("changed spec"), 0600)
			}
			ix.Questions[q.RequestID] = q
			if publish(f.r.conversationPath(), ix) != nil { t.Fatal("save") }
			before, _ := os.ReadFile(f.r.TodoPath)
			if _, err := f.r.conversationOriginReference(context.Background(), bound.RequestID, bound); err == nil { t.Fatal("unrelated or held source accepted") }
			after, _ := os.ReadFile(f.r.TodoPath)
			if string(before) != string(after) || f.posts != 1 { t.Fatal("rejection changed work or posted") }
		})
	}
}

func TestConversationOriginReferenceProductionContractIsFinite(t *testing.T) {
	b, _ := json.Marshal(fixedConversationOrigin)
	if fixedConversationOrigin.RequestID != "normal-ask-9620ba9f12adf45e1a01ee67010dafe616750231a1625ffcca0c7ed470a6a862" || fixedConversationOrigin.Origin != "1556320676015640587" || fixedConversationOrigin.InputSHA != "486a3210dca7e8110ff2cf3fb94ff0c1b2bd06ba99a344a6a8be54f800b7803c" || len(b) == 0 { t.Fatal("fixed original question changed") }
}
