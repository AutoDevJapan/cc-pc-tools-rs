package inputqual

import (
	"os"
	"path/filepath"
	"strings"
)

// This is the independently acquired owner answer's receipt, not a source/head
// constant. New parent-qualified corrections do not require changing it.
const SuccessorScopeSHA256 = "279e270a1deae7918e7f3efb17f610ecc3891852f7e12b020bdad26aa3d1ae7b"

var successorLocations = map[string]struct{ pathSHA, contentSHA string }{
	"owner_reply": {"7b4b3886c42acc3bdd4baeaf667b270d451fb4db34d1514e5e0b10d4e4dde216", SuccessorScopeSHA256},
	"handoff":     {"bf4d42b6feb9d4140f86e293a92548b61d36e947f0c49dd00572430c5ab10471", ""},
}

func ConstructSuccessor() (Snapshot, error) {
	p, err := productionPolicy()
	if err != nil {
		return Snapshot{}, err
	}
	exe, err := os.Executable()
	if err != nil {
		return Snapshot{}, rejected("successor_locator")
	}
	data, err := readExact(filepath.Join(filepath.Dir(exe), "successor-input-3059.json"), 32<<10)
	if err != nil {
		return Snapshot{}, rejected("successor_locator")
	}
	var refs map[string]string
	if decode(data, &refs, true) != nil {
		return Snapshot{}, rejected("successor_locator_schema")
	}
	return constructSuccessor(p, refs, successorLocations, runtimeReadable)
}

func constructSuccessor(p policy, refs map[string]string, locations map[string]struct{ pathSHA, contentSHA string }, readable func(string) bool) (Snapshot, error) {
	if !runtimeScope(p) {
		return Snapshot{}, rejected("runtime_task_scope")
	}
	if len(refs) != 2 || len(locations) != 2 {
		return Snapshot{}, rejected("successor_locator_schema")
	}
	for _, role := range []string{"owner_reply", "handoff"} {
		want, ok := locations[role]
		if !ok || !canonical(refs[role]) || locationDigest(refs[role]) != want.pathSHA {
			return Snapshot{}, rejected("successor_location")
		}
	}
	owner := refs["owner_reply"]
	if readable == nil || !readable(owner) {
		return Snapshot{}, rejected("successor_owner_read_permission")
	}
	data, err := readExact(owner, 64<<10)
	if err != nil || hash(data) != locations["owner_reply"].contentSHA {
		return Snapshot{}, rejected("successor_owner_receipt")
	}
	var reply struct {
		Source  string `json:"source"`
		Author  string `json:"author_id"`
		Message string `json:"message_id"`
		Thread  string `json:"thread_id"`
		Text    string `json:"text"`
		Context string `json:"question_context"`
	}
	var context struct {
		Request  string `json:"request_id"`
		Question string `json:"question_message_id"`
		Decision struct {
			Todo  int `json:"todo_id"`
			Scope struct {
				Action string `json:"action"`
			} `json:"scope"`
		} `json:"original_decision"`
	}
	if decode(data, &reply, false) != nil || reply.Source != "discord-api-v10" || hash([]byte(reply.Author)) != dependencyApproval.Author || reply.Message != "1562951926155317390" || reply.Thread != "1562901983306711121" || decode([]byte(reply.Context), &context, false) != nil || context.Request != "conversation-v1-305da59f578d2683be0abddca6f9b36d" || context.Question != reply.Thread || context.Decision.Todo != 3059 || context.Decision.Scope.Action != "extend-current-pr-runtime-to-parent-qualified-successor-corrections" {
		return Snapshot{}, rejected("successor_owner_identity")
	}
	text := strings.TrimSpace(reply.Text)
	if strings.HasPrefix(text, "<@") {
		_, text, _ = strings.Cut(text, ">")
		text = strings.TrimSpace(text)
	}
	choice, _, _ := strings.Cut(text, "\n")
	if strings.TrimSpace(choice) != "a" {
		return Snapshot{}, rejected("successor_owner_choice")
	}
	return constructRuntimeAt(p, readable, refs["handoff"], true)
}
