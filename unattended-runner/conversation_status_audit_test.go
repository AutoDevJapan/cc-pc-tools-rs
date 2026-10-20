package main

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestConversationStatusReadsSavedFilesWhileResidentLockIsHeld(t *testing.T) {
	f := conversationConfigFixture(t)
	conversationWriteJSON(t, filepath.Join(f.config.Home, "conversation-questions.json"), conversationIndex{Domain: conversationDomain, Since: f.config.Since, Destination: f.destination, Questions: map[string]ConversationQuestion{}})
	before := conversationFixtureSnapshot(t, f.root)
	release, err := acquire(filepath.Join(f.config.Home, "runner.lock"))
	if err != nil {
		t.Fatal(err)
	}
	released := false
	defer func() {
		if !released {
			release()
		}
	}()
	var out bytes.Buffer
	if code := conversationCLI([]string{"--config", f.configPath, "status"}, &out); code != 0 {
		t.Fatalf("read-only status blocked by resident: %d %s", code, out.String())
	}
	out.Reset()
	if code := conversationCLI([]string{"--config", f.configPath, "replies", "--request-id", "fixture"}, &out); code != 2 {
		t.Fatal("a non-status operation bypassed the resident lock")
	}
	release()
	released = true
	if !reflect.DeepEqual(before, conversationFixtureSnapshot(t, f.root)) {
		t.Fatal("read-only status changed saved files")
	}
}

func TestConversationStatusDoesNotLoadUnusedCredential(t *testing.T) {
	f := conversationConfigFixture(t)
	conversationWriteJSON(t, filepath.Join(f.config.Home, "conversation-questions.json"), conversationIndex{Domain: conversationDomain, Since: f.config.Since, Destination: f.destination, Questions: map[string]ConversationQuestion{}})
	backend, release, err := loadConversationStatusRunner(f.configPath)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if backend.(*Runner).Token != "" {
		t.Fatal("status unnecessarily loaded a credential")
	}
}

func TestConversationStatusExposesStageIDsWithoutPrivateText(t *testing.T) {
	f := conversationTest(t, true)
	q := f.ask(t)
	reply := f.reply(t, 8, "private owner reply fixture")
	f.r.processConversations(context.Background(), &Health{})
	f.r.State.Requests["777111777111777111"] = &Request{Message: "777111777111777111", Author: f.r.State.Destination.Owner, Text: "private ordinary request fixture", Phase: "captured"}
	before := len(f.commands)
	status, err := f.r.conversationStatus()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(status)
	if !strings.Contains(string(b), q.RequestID) || !strings.Contains(string(b), reply.Message) || !strings.Contains(string(b), "conversation_reply_delivered") {
		t.Fatal("saved question/reply stages cannot be reconciled individually")
	}
	if len(status["reply_stages"].([]map[string]any)) != 1 || len(status["intake_stages"].([]map[string]any)) != 2 || !strings.Contains(string(b), "777111777111777111") {
		t.Fatal("ordinary intake was omitted or misclassified as a new conversation reply")
	}
	if strings.Contains(string(b), "private owner reply fixture") || strings.Contains(string(b), "private ordinary request fixture") || strings.Contains(string(b), "aかb") || strings.Contains(string(b), f.r.Config.Home) || strings.Contains(string(b), "fixture-token") || len(f.commands) != before || f.close != 0 {
		t.Fatal("status disclosed private material, executed a command, or inferred application")
	}
}
