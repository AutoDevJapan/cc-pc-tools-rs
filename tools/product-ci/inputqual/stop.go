package inputqual

import "path/filepath"

// The independently pinned original outcome, final receipt and guard stop are
// evidence of the finite interrupted inspection, not a new owner instruction.
func verifyStop(checked map[string][]byte, p policy, pinSHA, ownedID string) error {
	var outcome struct {
		Task     int    `json:"origin_todo_id"`
		Final    string `json:"final_candidate_receipt"`
		FinalSHA string `json:"final_candidate_receipt_sha256"`
		Stop     string `json:"guard_stop_file"`
		Run      int64  `json:"target995_run"`
		Job      int64  `json:"target995_job"`
		Inspect  int    `json:"target995_inspect_exit"`
		Started  bool   `json:"target995_execution_started"`
		Success  bool   `json:"target995_secret_success"`
	}
	if decode(checked["outcome"], &outcome, false) != nil || outcome.Task != 3059 || !same(outcome.Final, p.final.path) || outcome.FinalSHA != p.final.sha || !same(outcome.Stop, p.stop.path) || outcome.Run != 39384472279 || outcome.Job != 118500893552 || outcome.Inspect != 0 || outcome.Started || outcome.Success {
		return rejected("original_outcome_binding")
	}
	var final struct {
		Task      int `json:"origin_todo_id"`
		Candidate struct {
			Repo    int64  `json:"repo_id"`
			PR      int    `json:"pr"`
			Head    string `json:"head"`
			Tree    string `json:"tree"`
			Base    string `json:"base"`
			Source  string `json:"source_evidence_sha256"`
			Pin     string `json:"current_secret_pin"`
			PinSHA  string `json:"current_secret_pin_sha256"`
			Run     int64  `json:"secret_run"`
			Job     int64  `json:"secret_job"`
			Attempt int    `json:"secret_attempt"`
		} `json:"current_product_candidate"`
	}
	if decode(checked["final"], &final, false) != nil || final.Task != 3059 || final.Candidate.Repo != 1285649124 || final.Candidate.PR != 995 || final.Candidate.Head != fixedHead || final.Candidate.Tree != fixedTree || final.Candidate.Base != fixedBase || final.Candidate.Source != p.original.sha || !same(final.Candidate.Pin, p.pin) || final.Candidate.PinSHA != pinSHA || final.Candidate.Run != 39384472279 || final.Candidate.Job != 118500893552 || final.Candidate.Attempt != 1 {
		return rejected("final_candidate_binding")
	}
	var stop struct {
		Task      int      `json:"origin_todo_id"`
		Protected bool     `json:"guard_client_resource"`
		Unknown   bool     `json:"guard_classification_unverified"`
		Recovered bool     `json:"original_registry_recovered"`
		Started   bool     `json:"secret_execution_started"`
		Inputs    []string `json:"input_files"`
		Owned     string   `json:"input_owned_id"`
	}
	if decode(checked["stop"], &stop, false) != nil || stop.Task != 3059 || !stop.Protected || !stop.Unknown || stop.Recovered || stop.Started || len(stop.Inputs) != 3 || stop.Owned != ownedID {
		return rejected("original_guard_stop")
	}
	seen := map[string]bool{}
	for _, path := range stop.Inputs {
		role := ""
		for k, v := range map[string]string{"pin": p.pin, "review": p.review, "acceptance": p.acceptance} {
			// The original stop records leaf names plus its independently bound
			// owned-directory ID. It does not supply new readable paths.
			if path == filepath.Base(v) {
				role = k
			}
		}
		if role == "" || seen[role] {
			return rejected("original_guard_inputs")
		}
		seen[role] = true
	}
	return nil
}
