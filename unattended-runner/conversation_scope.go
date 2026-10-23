package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
)

// This binds the exact project/root checked by the caller. It is not a guard
// exemption or a claim that a hash alone proves authorization.
type conversationTodoScope struct {
	Domain  string `json:"domain"`
	Todo    string `json:"todo_id"`
	Project string `json:"project"`
	RootSHA string `json:"project_path_sha256"`
}

func (r *Runner) conversationScopeFile(row *TodoRow, q ConversationQuestion) (string, error) {
	scope := row.conversationScope
	if scope.Domain != "discord-conversation-todo-scope-v1" || scope.Todo != fmt.Sprint(q.Todo) || scope.Project != q.Project || !shaPattern.MatchString(scope.RootSHA) {
		return "", errors.New("conversation guarded scope unavailable")
	}
	b, err := json.Marshal(scope)
	if err != nil {
		return "", err
	}
	path := filepath.Join(r.Config.Home, "conversations", q.RequestID, "todo-scope.json")
	return path, writeText(path, string(b))
}

func (r *Runner) conversationSnapshotFile(row *TodoRow, q ConversationQuestion, message string) (string, error) {
	if len(row.conversationState) == 0 {
		return "", errors.New("conversation guarded row unavailable")
	}
	path := filepath.Join(r.Config.Home, "conversations", q.RequestID, "replies", message+".expected.json")
	return path, writeText(path, string(row.conversationState))
}
