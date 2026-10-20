package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestConversationStatusExposesStageIDsWithoutPrivateText(t *testing.T) {
	f := conversationTest(t, true)
	q := f.ask(t)
	reply := f.reply(t, 8, "private owner reply fixture")
	f.r.processConversations(context.Background(), &Health{})
	before := len(f.commands)
	status, err := f.r.conversationStatus()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(status)
	if !strings.Contains(string(b), q.RequestID) || !strings.Contains(string(b), reply.Message) || !strings.Contains(string(b), "conversation_reply_delivered") {
		t.Fatal("saved question/reply stages cannot be reconciled individually")
	}
	if strings.Contains(string(b), "private owner reply fixture") || strings.Contains(string(b), "aかb") || strings.Contains(string(b), f.r.Config.Home) || strings.Contains(string(b), "fixture-token") || len(f.commands) != before || f.close != 0 {
		t.Fatal("status disclosed private material, executed a command, or inferred application")
	}
}
