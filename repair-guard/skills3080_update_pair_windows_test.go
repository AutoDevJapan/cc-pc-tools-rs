package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func skillsPairFixture(id string) skillsReceived {
	head := strings.Repeat("b", 40)
	marker, _ := json.Marshal(map[string]any{"domain": "skills-3080-code-review-v1", "head": head, "accepted": true, "must": 0, "suggestion": 0, "nit": 0})
	return skillsReceived{Spec: []byte("SKILLS3080_IMPLEMENTATION_REVIEW " + head), Metadata: object{"id": id, "thread_id": "thread-" + id, "model": "gpt-6-luna", "effort": "max", "model_origin": "owner_request", "effort_origin": "owner_request"}, Native: []object{{"model": "gpt-6-luna", "effort": "max"}}, Body: append([]byte("SKILLS3080_CODE_ACCEPTANCE "), marker...)}
}
func TestSkills3080RuntimeRequiresTwoIndependentNativeReviews(t *testing.T) {
	head := strings.Repeat("b", 40)
	for _, kind := range []string{"valid", "missing_peer", "same_job", "same_thread", "missing_thread", "native_model", "native_effort", "model_origin", "effort_origin", "other_head", "domain", "suggestion", "nit", "missing_counts"} {
		t.Run(kind, func(t *testing.T) {
			a, b := skillsPairFixture("first"), skillsPairFixture("second")
			switch kind {
			case "missing_peer":
				b = skillsReceived{}
			case "same_job":
				b.Metadata["id"] = a.Metadata["id"]
			case "same_thread":
				b.Metadata["thread_id"] = a.Metadata["thread_id"]
			case "missing_thread":
				delete(b.Metadata, "thread_id")
			case "native_model":
				b.Native[0]["model"] = "gpt-6.1-sol"
			case "native_effort":
				b.Native[0]["effort"] = "high"
			case "model_origin":
				b.Metadata["model_origin"] = "policy"
			case "effort_origin":
				b.Metadata["effort_origin"] = "policy"
			case "other_head":
				b.Body = []byte(strings.ReplaceAll(string(b.Body), head, strings.Repeat("c", 40)))
			case "domain":
				b.Body = []byte(strings.ReplaceAll(string(b.Body), "skills-3080-code-review-v1", "other"))
			case "suggestion":
				b.Body = []byte(strings.ReplaceAll(string(b.Body), "\"suggestion\":0", "\"suggestion\":1"))
			case "nit":
				b.Body = []byte(strings.ReplaceAll(string(b.Body), "\"nit\":0", "\"nit\":1"))
			case "missing_counts":
				b.Body = []byte(strings.ReplaceAll(string(b.Body), ",\"suggestion\":0", ""))
			}
			if got := skillsCodeReviewPairAccepted(a, b, head); got != (kind == "valid") {
				t.Fatalf("runtime pair %s=%v", kind, got)
			}
		})
	}
}
func TestSkills3080OnlyRuntimeApplyRecoveryAcceptNewFlags(t *testing.T) {
	for _, op := range []string{"apply-skills3080-update", "recover-skills3080-update"} {
		calls := 0
		code := runSkillsCLI([]string{"--" + op, "D:/fixture/request.json"}, new(strings.Builder), func(got, path string) object {
			calls++
			if got != op {
				t.Fatal("wrong action")
			}
			return object{"qualified": true}
		})
		if calls != 1 || code != 0 || !skillsFlag([]string{"--" + op, "D:/fixture/request.json"}) {
			t.Fatal("finite runtime command not routed")
		}
	}
}
