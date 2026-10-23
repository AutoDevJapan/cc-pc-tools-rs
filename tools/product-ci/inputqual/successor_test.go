package inputqual

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func constructTestSuccessor(p policy, refs map[string]string, locations map[string]struct{ pathSHA, contentSHA string }, readable func(string) bool) (Snapshot, error) {
	identity := successorIdentity{Author: hash([]byte("synthetic-author")), Message: hash([]byte("synthetic-message")), Question: hash([]byte("synthetic-question")), Request: hash([]byte("synthetic-request"))}
	return constructSuccessorWithIdentity(p, refs, locations, readable, identity)
}

func successorFixture(t *testing.T, kind string, pr int, head string) (*fixture, map[string]string, map[string]struct{ pathSHA, contentSHA string }) {
	t.Helper()
	f, h := runtimeFixture(t, kind, head)
	h.Version, h.PR = 2, pr
	for _, ref := range []*runtimeReference{&h.Review, &h.Acceptance} {
		var result runtimeResult
		json.Unmarshal(mustReadFixture(t, ref.File), &result)
		result.PR = pr
		ref.SHA = f.put(ref.File, result).sha
	}
	handoff := filepath.Join(f.p.root, ".ci-work", successorLeaf)
	f.put(handoff, h)
	context, _ := json.Marshal(map[string]any{"request_id": "synthetic-request", "question_message_id": "synthetic-question", "original_decision": map[string]any{"todo_id": 3059, "scope": map[string]string{"action": "extend-current-pr-runtime-to-parent-qualified-successor-corrections"}}})
	owner := f.put(filepath.Join(f.p.root, "synthetic-owner.json"), map[string]any{"source": "discord-api-v10", "author_id": "synthetic-author", "message_id": "synthetic-message", "thread_id": "synthetic-question", "text": "a\nsynthetic fixture", "question_context": string(context)})
	refs := map[string]string{"owner_reply": owner.path, "handoff": handoff}
	locations := map[string]struct{ pathSHA, contentSHA string }{"owner_reply": {locationDigest(owner.path), owner.sha}, "handoff": {locationDigest(handoff), ""}}
	return f, refs, locations
}

func TestSuccessorAuthorityAndFrozenDifferentSources(t *testing.T) {
	for _, kind := range []string{"secret", "codeql"} {
		for i, pr := range []int{1016, 1017} {
			f, refs, locations := successorFixture(t, kind, pr, strings.Repeat(string(rune('c'+i)), 40))
			s, err := constructTestSuccessor(f.p, refs, locations, func(string) bool { return true })
			if err != nil {
				t.Fatal(err)
			}
			if s.Report().PR != pr || s.Report().OwnerScopeSHA != SuccessorScopeSHA256 || s.Report().OfficialCISuccess || s.Report().OriginalInputFilesRead {
				t.Fatal("lost source, authority or result distinction")
			}
			if err := s.Consume(func(pin, review, acceptance []byte) error {
				var b struct {
					Origin int    `json:"origin_todo_id"`
					Scope  string `json:"owner_scope_sha256"`
					PR     int    `json:"pr"`
				}
				if json.Unmarshal(pin, &b) != nil || b.Origin != 3056 || b.Scope != SuccessorScopeSHA256 || b.PR != pr || hash(review) != s.Report().ReviewSHA || hash(acceptance) != s.Report().AcceptanceSHA {
					t.Fatal("wrong source-bound bytes")
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if _, err := constructRuntime(f.p, func(string) bool { return true }); err == nil {
				t.Fatal("old995 runtime silently expanded")
			}
		}
	}
}

func TestSuccessorRejectsAuthorityBeforeParentRead(t *testing.T) {
	for _, fault := range []string{"denied-owner", "changed-owner", "copied-owner", "other-author", "other-question", "choice-b", "other-scope", "stop", "parent-done"} {
		t.Run(fault, func(t *testing.T) {
			f, refs, locations := successorFixture(t, "secret", 1016, strings.Repeat("c", 40))
			switch fault {
			case "changed-owner":
				os.WriteFile(refs["owner_reply"], []byte("changed private canary"), 0600)
			case "copied-owner":
				copy := filepath.Join(f.p.root, "copied-owner.json")
				os.WriteFile(copy, mustReadFixture(t, refs["owner_reply"]), 0600)
				refs["owner_reply"] = copy
			case "other-author", "other-question", "choice-b", "other-scope":
				var v map[string]any
				json.Unmarshal(mustReadFixture(t, refs["owner_reply"]), &v)
				if fault == "other-author" {
					v["author_id"] = "unrelated"
				}
				if fault == "choice-b" {
					v["text"] = "b"
				}
				if fault == "other-question" {
					v["thread_id"] = "unrelated"
				}
				if fault == "other-scope" {
					v["question_context"] = strings.Replace(v["question_context"].(string), "parent-qualified-successor-corrections", "unrelated", 1)
				}
				a := f.put(refs["owner_reply"], v)
				want := locations["owner_reply"]
				want.contentSHA = a.sha
				locations["owner_reply"] = want
			case "stop", "parent-done":
				var db map[string]any
				json.Unmarshal(mustReadFixture(t, f.p.db), &db)
				for _, value := range db["items"].([]any) {
					row := value.(map[string]any)
					if fault == "stop" && row["id"] == float64(3059) {
						row["owner_stopped"] = true
					}
					if fault == "parent-done" && row["id"] == float64(3056) {
						row["status"] = "done"
					}
				}
				f.put(f.p.db, db)
			}
			parentRead := false
			_, err := constructTestSuccessor(f.p, refs, locations, func(path string) bool {
				if path == refs["handoff"] {
					parentRead = true
				}
				return fault != "denied-owner"
			})
			if err == nil || parentRead {
				t.Fatal("invalid authority reached parent handoff")
			}
		})
	}
}

func TestSuccessorRejectsMismatchedParentAndGuard(t *testing.T) {
	for _, fault := range []string{"parent", "repo", "head", "role", "output", "guard"} {
		t.Run(fault, func(t *testing.T) {
			f, refs, locations := successorFixture(t, "secret", 1016, strings.Repeat("c", 40))
			var h runtimeHandoff
			json.Unmarshal(mustReadFixture(t, refs["handoff"]), &h)
			switch fault {
			case "parent":
				h.Origin++
			case "repo":
				h.Repo++
			case "head":
				h.Head = strings.Repeat("d", 40)
			case "role":
				h.Review, h.Acceptance = h.Acceptance, h.Review
			case "output":
				os.WriteFile(filepath.Join(f.p.root, "review-new-output.json"), []byte("changed"), 0600)
			}
			f.put(refs["handoff"], h)
			_, err := constructTestSuccessor(f.p, refs, locations, func(path string) bool { return fault != "guard" || path != h.Review.File })
			if err == nil {
				t.Fatal("mismatched source or denied evidence accepted")
			}
		})
	}
}

func TestSuccessorRejectsReceiverHandoffOutsideVerifiedRoot(t *testing.T) {
	f, refs, locations := successorFixture(t, "secret", 1016, strings.Repeat("c", 40))
	receiver, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	copy := filepath.Join(receiver, "current-ci-successor-handoff-3056.private.json")
	if err := os.WriteFile(copy, mustReadFixture(t, refs["handoff"]), 0600); err != nil {
		t.Fatal(err)
	}
	refs["handoff"] = copy
	locations["handoff"] = struct{ pathSHA, contentSHA string }{locationDigest(copy), ""}
	if _, err := constructTestSuccessor(f.p, refs, locations, func(string) bool { return true }); err == nil {
		t.Fatal("receiver-owned handoff outside verified source root accepted")
	}
}

func TestSuccessorSurvivesRepairCompletionWhileProductWorkContinues(t *testing.T) {
	f, refs, locations := successorFixture(t, "secret", 1016, strings.Repeat("c", 40))
	var db map[string]any
	if err := json.Unmarshal(mustReadFixture(t, f.p.db), &db); err != nil {
		t.Fatal(err)
	}
	for _, value := range db["items"].([]any) {
		row := value.(map[string]any)
		if row["id"] == float64(3059) {
			row["status"], row["dispatchable"] = "done", false
		}
	}
	f.put(f.p.db, db)
	if _, err := constructTestSuccessor(f.p, refs, locations, func(string) bool { return true }); err != nil {
		t.Fatalf("repair completion disabled still-authorized product work: %v", err)
	}
	// A completed repair is not an exemption from a later explicit owner stop.
	for _, value := range db["items"].([]any) {
		row := value.(map[string]any)
		if row["id"] == float64(3059) {
			row["owner_stopped"] = true
		}
	}
	f.put(f.p.db, db)
	if _, err := constructTestSuccessor(f.p, refs, locations, func(string) bool { return true }); err == nil {
		t.Fatal("completed repair ignored an explicit stop")
	}
}

func TestSuccessorRejectsV1AtCanonicalParentLocation(t *testing.T) {
	f, refs, locations := successorFixture(t, "secret", 1016, strings.Repeat("c", 40))
	var h runtimeHandoff
	if err := json.Unmarshal(mustReadFixture(t, refs["handoff"]), &h); err != nil {
		t.Fatal(err)
	}
	h.Version = 1
	f.put(refs["handoff"], h)
	_, err := constructTestSuccessor(f.p, refs, locations, func(string) bool { return true })
	if err == nil || !strings.Contains(err.Error(), "runtime_handoff") {
		t.Fatal("v1 handoff admitted through successor route")
	}
}
