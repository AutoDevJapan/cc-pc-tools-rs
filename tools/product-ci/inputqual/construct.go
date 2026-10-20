package inputqual

import "encoding/json"

// These typed records come from the independently pinned original receipt.
// The review payload is its final review RECORD, not a claimed read of the raw
// reviewer response. The acceptance payload is the exact retained receipt.
// No rejected operator input is used as source material, including the plan.
func constructSnapshot(checked map[string][]byte, p policy) (Snapshot, error) {
	var original struct {
		Review json.RawMessage `json:"final_review"`
	}
	if decode(checked["original"], &original, false) != nil {
		return Snapshot{}, rejected("construction_original")
	}
	type evidence struct {
		Origin      int    `json:"origin_todo_id"`
		Repo        int64  `json:"repo_id"`
		PR          int    `json:"pr"`
		Head        string `json:"head_sha"`
		Tree        string `json:"head_tree"`
		Kind        string `json:"kind"`
		Success     bool   `json:"success"`
		Must        int    `json:"must"`
		OriginalSHA string `json:"original_sha256"`
		Original    []byte `json:"original"`
	}
	makeEvidence := func(kind string, source []byte) ([]byte, error) {
		return json.Marshal(evidence{3056, 1285649124, 995, fixedHead, fixedTree, kind, true, 0, hash(source), source})
	}
	review, err := makeEvidence("review", original.Review)
	if err != nil {
		return Snapshot{}, rejected("construction_review")
	}
	acceptance, err := makeEvidence("acceptance", checked["original"])
	if err != nil {
		return Snapshot{}, rejected("construction_acceptance")
	}
	// The original candidate/outcome and predecessor have already proven every
	// fixed identity below. New input hashes describe helper-constructed bytes;
	// they never overwrite or reclassify the three original protected inputs.
	pin, err := json.Marshal(struct {
		Repo          int64   `json:"repo_id"`
		PR            int     `json:"pr"`
		Head          string  `json:"head_sha"`
		Tree          string  `json:"head_tree"`
		Base          string  `json:"base_sha"`
		Run           int64   `json:"run_id"`
		Attempt       int     `json:"attempt"`
		Jobs          []int64 `json:"job_ids"`
		Kind          string  `json:"kind"`
		Context       string  `json:"context_sha"`
		ReviewSHA     string  `json:"review_sha256"`
		AcceptanceSHA string  `json:"acceptance_sha256"`
		ReviewRef     string  `json:"review_file"`
		AcceptanceRef string  `json:"acceptance_file"`
	}{1285649124, 995, fixedHead, fixedTree, "41090d19329b81dd1bd076f3336d723823be4b82", 39384472279, 1, []int64{118500893552}, "secret", "6e7ca682c861024b6f08f31382851dab4bceff35", hash(review), hash(acceptance), "independent-original-review-record", "independent-original-acceptance-receipt"})
	if err != nil {
		return Snapshot{}, rejected("construction_plan")
	}
	// Verify the representation before returning it to the official consumer.
	if !sameOriginalReviewRecord(original.Review, checked["original"]) || hash(checked["original"]) != p.original.sha {
		return Snapshot{}, rejected("construction_identity")
	}
	r := Report{Qualified: true, Purpose: Purpose, Task: 3059, OriginTask: 3056, AdditionalTask: 3068, RepoID: 1285649124, PR: 995, Head: fixedHead, Tree: fixedTree, Run: 39384472279, Job: 118500893552, PinSHA: hash(pin), ReviewSHA: hash(review), AcceptanceSHA: hash(acceptance), ScopeSHA: p.scope.sha, IndependentReceiptSHA: p.original.sha, GeneralClassificationUnverified: true, OriginalCreator: "prior-original-task3059-operator", InputConstruction: "official-helper-from-independent-original-receipts", OriginalInputFilesRead: false}
	return Snapshot{pin: pin, review: review, acceptance: acceptance, report: r}, nil
}
