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
	if actor := row.conversationActorBinding; actor != nil {
		if len(row.conversationOriginState) == 0 || row.ID != actor.Todo {
			return "", errors.New("conversation actor unverified")
		}
		originFile := filepath.Join(r.Config.Home, "conversations", q.RequestID, "origin-state.json")
		if err = writeText(originFile, string(row.conversationOriginState)); err != nil {
			return "", err
		}
		b, err = json.Marshal(map[string]string{"domain": "discord-conversation-actor-todo-scope-v1", "todo_id": scope.Todo, "project": scope.Project, "project_path_sha256": scope.RootSHA, "origin_state_file": originFile, "actor_spec_sha256": actor.SpecSHA, "origin_spec_sha256": actor.OriginSpecSHA})
		if err != nil {
			return "", err
		}
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
