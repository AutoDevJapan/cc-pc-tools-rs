package main

import (
	"strings"
	"testing"
)

func TestSkills3080RuntimeReviewRequiresActualOwnerSelectedLuna(t *testing.T) {
	for _, kind := range []string{"valid", "other_model", "writer_model", "other_effort", "policy_model_origin", "policy_effort_origin", "missing_model_origin", "missing_effort_origin", "native_model", "native_effort"} {
		t.Run(kind, func(t *testing.T) {
			head := strings.Repeat("b", 40)
			r := skillsReceived{Spec: []byte("SKILLS3080_IMPLEMENTATION_REVIEW " + head), Metadata: object{"model": "gpt-6-luna", "effort": "max", "model_origin": "owner_request", "effort_origin": "owner_request"}, Native: []object{{"model": "gpt-6-luna", "effort": "max"}}, Body: []byte("SKILLS3080_CODE_ACCEPTANCE " + `{"domain":"skills-3080-code-review-v1","head":"` + head + `","accepted":true,"must":0}`)}
			switch kind {
			case "other_model":
				r.Metadata["model"], r.Native[0]["model"] = "claude-sonnet-5-5", "claude-sonnet-5-5"
			case "writer_model":
				r.Metadata["model"], r.Native[0]["model"] = "gpt-6.1-sol", "gpt-6.1-sol"
			case "other_effort":
				r.Metadata["effort"], r.Native[0]["effort"] = "high", "high"
			case "policy_model_origin":
				r.Metadata["model_origin"] = "policy"
			case "policy_effort_origin":
				r.Metadata["effort_origin"] = "policy"
			case "missing_model_origin":
				delete(r.Metadata, "model_origin")
			case "missing_effort_origin":
				delete(r.Metadata, "effort_origin")
			case "native_model":
				r.Native[0]["model"] = "claude-sonnet-5-5"
			case "native_effort":
				r.Native[0]["effort"] = "high"
			}
			if got := skillsCodeReviewAccepted(r, head); got != (kind == "valid") {
				t.Fatalf("actual owner-selected code review %s accepted=%v", kind, got)
			}
		})
	}
}
