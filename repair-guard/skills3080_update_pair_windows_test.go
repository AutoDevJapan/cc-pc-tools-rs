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
	a := &skillsResultRequest{Task: 4300, Job: "20261031-010000-todo-4300", SpecSHA: strings.Repeat("a", 64), FinalSHA: strings.Repeat("b", 64)}
	b := &skillsResultRequest{Task: 4301, Job: "20261031-010100-todo-4301", SpecSHA: strings.Repeat("c", 64), FinalSHA: strings.Repeat("d", 64)}
	if !skillsRuntimeReviewTasks(a, b) {
		t.Fatal("original own reviews rejected")
	}
	for _, id := range []int{3712, 3870, 3876, 3896, 4115, 4184, 4198, 4199, 9999} {
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

func TestSkills3080Frozen624PairOnlyReadsBackItsOriginalSoftware(t *testing.T) {
	r := skillsRequest{Review: &skillsResultRequest{Task: 4198}, Runtime: &skillsRuntimeRequest{SourceHead: official3894CreationPathPredecessorHead, ReviewedHead: "4284eef98c916358d68abe134ff057421e82a87d", CandidateSHA: official3894CreationPathPredecessorSHA, PeerReview: &skillsResultRequest{Task: 4199}}}
	if !skillsRuntimeHistoricalNoticePair(r) || skillsRuntimeReviewTasks(r.Review, r.Runtime.PeerReview) {
		t.Fatal("frozen PR624 pair lost readback or gained new apply authority")
	}
	for _, field := range []string{"source", "reviewed", "sha", "self", "peer"} {
		copy := r
		u := *r.Runtime
		a := *r.Review
		b := *u.PeerReview
		copy.Runtime, copy.Review, u.PeerReview = &u, &a, &b
		switch field {
		case "source":
			u.SourceHead = strings.Repeat("a", 40)
		case "reviewed":
			u.ReviewedHead = strings.Repeat("b", 40)
		case "sha":
			u.CandidateSHA = strings.Repeat("c", 64)
		case "self":
			a.Task = 4203
		case "peer":
			b.Task = 4204
		}
		if skillsRuntimeHistoricalNoticePair(copy) || skillsRuntimeReviewTasks(copy.Review, copy.Runtime.PeerReview) {
			t.Fatalf("frozen pair accepted mutation %s", field)
		}
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

func TestSkills3080FrozenBootstrapSoftwareIsReadonlyAndNewPairCannotDowngrade(t *testing.T) {
	r := skillsRequest{Review: &skillsResultRequest{Task: 4203}, Runtime: &skillsRuntimeRequest{PeerReview: &skillsResultRequest{Task: 4204}, SourceHead: official3894PublicManifestPredecessorHead, ReviewedHead: "930bf282717e6b78a9a8b1fde7e7a85be944ebeb", CandidateSHA: official3894PublicManifestPredecessorSHA}}
	if !skillsRuntimeHistoricalNoticePair(r) || skillsRuntimeNewCIReview(r) || skillsRuntimeReviewTasks(r.Review, r.Runtime.PeerReview) {
		t.Fatal("accepted bootstrap pair acquired new write authority or lost readonly history")
	}
	r.Runtime.SourceHead = strings.Repeat("a", 40)
	if skillsRuntimeHistoricalNoticePair(r) || !skillsRuntimeNewCIReview(r) {
		t.Fatal("changed frozen tuple escaped new CI/review gate")
	}
	r.Review.Task = 4300
	if !skillsRuntimeNewCIReview(r) || skillsRuntimeReviewTasks(r.Review, r.Runtime.PeerReview) {
		t.Fatal("mixed old/new pair downgraded")
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
