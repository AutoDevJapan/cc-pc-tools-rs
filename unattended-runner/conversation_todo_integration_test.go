package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// This optional acceptance check uses an actual freshly built canonical CLI
// against a temporary database. It never selects the production ToDo store.
func TestConversationActualCanonicalTodoRoundTrip(t *testing.T) {
	candidate := os.Getenv("CC_CONVERSATION_TODO_BINARY")
	if candidate == "" {
		t.Skip("fresh canonical ToDo binary not supplied")
	}
	if !filepath.IsAbs(candidate) {
		t.Fatal("candidate must be absolute")
	}
	f := conversationTest(t, true)
	f.r.Now = time.Now
	if err := publish(filepath.Join(f.r.Config.Home, "isolated-notify.json"), map[string]any{"guild_id": "100", "owner_user_id": "300", "application_id": "400", "task_channel_id": "200"}); err != nil {
		t.Fatal(err)
	}
	backend := f.r.Run
	f.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
		if path != conversationTodo {
			return backend(ctx, path, args...)
		}
		command := exec.CommandContext(ctx, candidate, args...)
		hide(command)
		command.WaitDelay = time.Second
		command.Env = append(os.Environ(), "CC_TODO_HOME="+f.r.Config.Home, "CC_TODO_JOBS="+filepath.Join(f.r.Config.Home, "isolated-jobs"), "CC_ASK_CONFIG="+filepath.Join(f.r.Config.Home, "isolated-notify.json"))
		out, err := command.Output()
		if err != nil {
			return "", errors.New("isolated canonical command rejected")
		}
		return string(out), nil
	}
	f.ask(t)
	row, err := f.r.todo(42)
	if err != nil || row.Status != "block" || !conversationWaitRecorded(row, f.q) {
		t.Fatal("actual wait not recorded")
	}
	reply := f.reply(t, 8, "a")
	health := Health{}
	f.r.processConversations(context.Background(), &health)
	if len(health.Errors) != 0 {
		t.Fatalf("actual answer transition failed: %v", health.Errors)
	}
	row, err = f.r.todo(42)
	if err != nil || row.Status != "todo" || len(row.QuestionAnswers) != 1 || !strings.Contains(row.Note, "以前の作業を保持") || !strings.Contains(row.Note, "conversation-owner-reply:") || strings.Contains(row.Note, "[discord-conversation-question-v1 ") || string(row.Spec) != `"unchanged-original-spec"` || len(row.Jobs) != 1 {
		t.Fatal("actual canonical release lost task state")
	}
	if req := f.r.State.Requests[reply.Message]; req == nil || req.Todo != 42 || req.Phase != "conversation_reply_delivered" {
		t.Fatal("actual answer was not linked to original task")
	}
}
