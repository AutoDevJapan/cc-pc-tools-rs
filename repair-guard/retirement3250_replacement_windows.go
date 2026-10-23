package main

import (
	contract "cc/civ6-ci-contract"
	"encoding/json"
	"fmt"
	"slices"
	"time"
)

type retirement3250Replacement struct {
	Original    string
	Replacement string
	ProofSHA256 string `json:"proof_sha256"`
}

// Only the normally deployed queue can produce these controller-owned records.
// Three original initial slots remain intact; each may have one next attempt.
func retirement3250ReplacementKeys(l retirement3250Ledger) ([]string, bool) {
	keys := append(append([]string{}, l.Budget.Initial...), l.Budget.Normal...)
	if len(l.Budget.Replacements) > 3 {
		return nil, false
	}
	used := map[string]bool{}
	for _, r := range l.Budget.Replacements {
		old, next := l.Entries[r.Original], l.Entries[r.Replacement]
		if !slices.Contains(l.Budget.Initial, r.Original) || slices.Contains(keys, r.Replacement) || used[r.Original] ||
			old == nil || next == nil || !conversationSHA.MatchString(r.ProofSHA256) ||
			old.Key != r.Original || next.Key != r.Replacement || old.Replaces != "" || next.Replaces != r.Original ||
			old.State != "refused" || old.Phase != "initial" || next.Phase != "initial" || old.OS != "Windows" || next.OS != "Windows" ||
			old.Plan.Validate() != nil || old.Plan.Attempt != 1 || next.Plan.Attempt != 2 || old.Plan.JobID == next.Plan.JobID ||
			old.Plan.PR != 149 || old.Plan.Event != "pull_request" || old.Plan.Head != owned3107Head || old.Plan.Tree != owned3107Tree ||
			r.Original != fmt.Sprintf("%d/1/%d", old.Plan.RunID, old.Plan.JobID) ||
			r.Replacement != fmt.Sprintf("%d/2/%d", next.Plan.RunID, next.Plan.JobID) {
			return nil, false
		}
		want := old.Plan
		want.Attempt, want.JobID = 2, next.Plan.JobID
		if next.Plan != want {
			return nil, false
		}
		used[r.Original] = true
		keys = append(keys, r.Replacement)
	}
	return keys, true
}

func retirement3250ReplacementProofValid(e *retirement3250Entry, l retirement3250Ledger, b []byte) bool {
	if e == nil || e.Replaces == "" {
		return false
	}
	matched := false
	for _, r := range l.Budget.Replacements {
		matched = matched || r.Original == e.Replaces && r.Replacement == e.Key && r.ProofSHA256 == hashBytes(b)
	}
	if !matched {
		return false
	}
	if _, valid := conversationJSON(b); !valid {
		return false
	}
	var p struct {
		Original    *retirement3250Entry `json:"original_entry"`
		Replacement contract.Plan        `json:"replacement_plan"`
		Cancelled   struct {
			ID                       int64
			RunID                    int64  `json:"run_id"`
			Attempt                  int    `json:"run_attempt"`
			Head                     string `json:"head_sha"`
			Name, Status, Conclusion string
			RunnerID                 *int64  `json:"runner_id"`
			RunnerName               *string `json:"runner_name"`
			Steps                    []json.RawMessage
			StartedAt                string `json:"started_at"`
			CompletedAt              string `json:"completed_at"`
		} `json:"cancelled_job"`
		Census struct {
			Queryable  *int `json:"queryable_account_process_count"`
			Unreadable *int `json:"unreadable_process_count"`
		} `json:"process_census"`
	}
	old := l.Entries[e.Replaces]
	if json.Unmarshal(b, &p) != nil || old == nil || p.Original == nil || p.Replacement != e.Plan ||
		(p.Original.State != "needs_recovery" && p.Original.State != "reserved") ||
		p.Census.Queryable == nil || *p.Census.Queryable != 0 || p.Census.Unreadable == nil || *p.Census.Unreadable < 0 {
		return false
	}
	before := *p.Original
	before.State = old.State
	if before != *old {
		return false
	}
	j := p.Cancelled
	started, se := time.Parse(time.RFC3339Nano, j.StartedAt)
	ended, ee := time.Parse(time.RFC3339Nano, j.CompletedAt)
	original, oe := time.Parse(time.RFC3339Nano, old.Queued.At)
	next, ne := time.Parse(time.RFC3339Nano, e.Queued.At)
	return j.ID == old.Plan.JobID && j.RunID == old.Plan.RunID && j.Attempt == 1 && j.Head == old.Plan.Head && j.Name == old.Plan.JobName &&
		j.Status == "completed" && j.Conclusion == "cancelled" && j.RunnerID != nil && *j.RunnerID == 0 && j.RunnerName != nil && *j.RunnerName == "" && j.Steps != nil && len(j.Steps) == 0 &&
		se == nil && ee == nil && oe == nil && ne == nil && !ended.Before(started.Add(24*time.Hour)) && !next.Before(ended) && !next.Before(original.Add(24*time.Hour))
}
