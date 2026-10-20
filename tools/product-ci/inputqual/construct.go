package inputqual

import "encoding/json"

// RecordBytes has no authority. It can be assembled from caller-owned bytes,
// but cannot create a Snapshot or enter an executable CLI route.
type RecordBytes struct{ Pin, Review, Acceptance []byte }

// AssembleRecords is a pure representation constructor. Qualification separately
// proves the original task and retained evidence before calling it. Keeping the
// representation separate lets the official parser exercise these exact bytes.
func AssembleRecords(receipt []byte) (RecordBytes, error) {
	var original struct {
		Review json.RawMessage `json:"final_review"`
	}
	if decode(receipt, &original, false) != nil || len(original.Review) == 0 {
		return RecordBytes{}, rejected("construction_original")
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
		return RecordBytes{}, rejected("construction_review")
	}
	acceptance, err := makeEvidence("acceptance", receipt)
	if err != nil {
		return RecordBytes{}, rejected("construction_acceptance")
	}
	// New hashes describe these records only, never new authority. The qualified
	// caller proves the fixed identities; the live API must also match context.
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
	}{1285649124, 995, fixedHead, fixedTree, fixedBase, 39384472279, 1, []int64{118500893552}, "secret", fixedContext, hash(review), hash(acceptance), "independent-original-review-record", "independent-original-acceptance-receipt"})
	if err != nil {
		return RecordBytes{}, rejected("construction_plan")
	}
	return RecordBytes{pin, review, acceptance}, nil
}

// Only this already-qualified path turns assembled bytes into a Snapshot. The
// review is the original receipt's record; the raw reviewer response is not read.
func constructSnapshot(checked map[string][]byte, p policy) (Snapshot, error) {
	records, err := AssembleRecords(checked["original"])
	if err != nil {
		return Snapshot{}, err
	}
	r := Report{Qualified: true, Purpose: Purpose, Task: 3059, OriginTask: 3056, AdditionalTask: 3068, RepoID: 1285649124, PR: 995, Head: fixedHead, Tree: fixedTree, Run: 39384472279, Job: 118500893552, PinSHA: hash(records.Pin), ReviewSHA: hash(records.Review), AcceptanceSHA: hash(records.Acceptance), ScopeSHA: p.scope.sha, IndependentReceiptSHA: p.original.sha, GeneralClassificationUnverified: true, OriginalCreator: "prior-original-task3059-operator", ConstructedInputCreator: "official-product-ci-helper", InputConstruction: "official-helper-from-independent-original-receipts", OriginalInputFilesRead: false, ReviewRepresentation: "retained-final-review-record; raw response not read", BaseAuthority: "pinned-final-candidate-receipt", ContextAuthority: "reviewed-helper-workflow-constant; requires live API match"}
	return Snapshot{pin: records.Pin, review: records.Review, acceptance: records.Acceptance, report: r}, nil
}
