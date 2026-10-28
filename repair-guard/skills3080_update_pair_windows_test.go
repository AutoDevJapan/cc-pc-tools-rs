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
func TestSkills3080RuntimePinsOriginalTwoReviewTasks(t *testing.T) {
	a := &skillsResultRequest{Task: 4198, Job: "20261028-210000-todo-4198", SpecSHA: strings.Repeat("a", 64), FinalSHA: strings.Repeat("b", 64)}
	b := &skillsResultRequest{Task: 4199, Job: "20261028-210100-todo-4199", SpecSHA: strings.Repeat("c", 64), FinalSHA: strings.Repeat("d", 64)}
	if !skillsRuntimeReviewTasks(a, b) {
		t.Fatal("original own reviews rejected")
	}
	for _, id := range []int{3712, 3870, 3876, 3896, 4115, 4184, 9999} {
		c := *b
		c.Task = id
		if skillsRuntimeReviewTasks(a, &c) {
			t.Fatal("other task accepted as required rubber review")
		}
	}
	if skillsRuntimeReviewTasks(b, a) || skillsRuntimeReviewTasks(a, nil) {
		t.Fatal("wrong role or missing peer accepted")
	}
}
func TestSkills3080RuntimeRequiresDurableOutcome(t *testing.T) {
	for _, recovery := range []bool{false, true} {
		r := skillsUpdateResult{Installed: !recovery, Restored: recovery}
		if skillsUpdateOutcomeQualified(r, recovery, false) || !skillsUpdateOutcomeQualified(r, recovery, true) {
			t.Fatal("success without durable outcome or durable success rejected")
		}
	}
}

func TestSkills3080FrozenAliasPairOnlyMatchesItsExactHistoricalSoftware(t *testing.T) {
	r := skillsRequest{Review: &skillsResultRequest{Task: 3870}, Runtime: &skillsRuntimeRequest{SourceHead: official3894CanonicalAssignmentPredecessorHead, ReviewedHead: "76a3133cee8be5a4a1f8b2b10714b6badb5f1967", CandidateSHA: official3894CanonicalAssignmentPredecessorSHA, PeerReview: &skillsResultRequest{Task: 3876}}}
	if !skillsRuntimeHistoricalNoticePair(r) || skillsRuntimeReviewTasks(r.Review, r.Runtime.PeerReview) {
		t.Fatal("frozen pair lost its readback or gained new apply authority")
	}
	for _, field := range []string{"source", "reviewed", "sha"} {
		u := *r.Runtime
		switch field {
		case "source":
			u.SourceHead = strings.Repeat("a", 40)
		case "reviewed":
			u.ReviewedHead = strings.Repeat("b", 40)
		case "sha":
			u.CandidateSHA = strings.Repeat("c", 64)
		}
		r2 := r
		r2.Runtime = &u
		if skillsRuntimeHistoricalNoticePair(r2) {
			t.Fatal("historical pair admitted different software")
		}
	}
	sha, head := official3894CanonicalAssignmentPredecessorSHA, official3894CanonicalAssignmentPredecessorHead
	if skillsGuardBaselines[sha] != head || !official3894InstalledBaseline(sha, head) || official3894InstalledBaseline(sha, strings.Repeat("a", 40)) {
		t.Fatal("independent predecessor pair changed")
	}
}
