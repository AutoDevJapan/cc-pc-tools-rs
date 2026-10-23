package main

import "testing"

func TestMengtoPartialEvaluationPreservesCompleteTreeAndLiveLedger(t *testing.T) {
	var a partialAssessment
	loadPublicReceipt(t, "skills-3080-mengto-partial-evaluation.json", &a)
	var roster struct {
		Source string   `json:"source"`
		Commit string   `json:"commit"`
		Tree   string   `json:"tree_sha"`
		SHA    string   `json:"path_set_sha256"`
		Paths  []string `json:"paths"`
	}
	loadPublicReceipt(t, "skills-3080-mengto-fixed-entrypoints.json", &roster)
	const pinned = "e607809a33ba5fa19352576bbe85fab030441a44560a6b4aba928125ac6cde45"
	if roster.Source != "MengTo/Skills" || roster.Commit != "398cd41c86ba42fbb30e944ae507f463648f08b7" || roster.Tree != "8afedc8719f37d2026b55aef282ac27d8d873d76" || roster.SHA != pinned || len(roster.Paths) != 177 || pathSetSHA(roster.Paths) != pinned {
		t.Fatal("complete source tree anchor changed")
	}
	var ledger struct {
		Entries  []partialAdoption `json:"entries"`
		Excluded []partialAdoption `json:"excluded_entries"`
	}
	loadPublicReceipt(t, "skills-3080-adoptions.json", &ledger)
	if err := validatePartial(a, append(ledger.Entries, ledger.Excluded...), roster.Source, roster.Commit, len(roster.Paths), pinned); err != nil {
		t.Fatal(err)
	}
	var p struct {
		Partial struct {
			Read       int  `json:"full_body_read"`
			Unread     int  `json:"unread"`
			Expected   int  `json:"expected"`
			Installed  int  `json:"installed"`
			Integrated int  `json:"integrated"`
			Excluded   int  `json:"excluded"`
			Complete   bool `json:"complete"`
		} `json:"mengto_partial_entrypoint_evaluation"`
	}
	loadPublicReceipt(t, "skills-3080-progress.json", &p)
	if a.Read < 45 || p.Partial.Read != a.Read || p.Partial.Unread != a.Unread || p.Partial.Expected != a.Expected || p.Partial.Installed != a.Installed || p.Partial.Integrated != a.Integrated || p.Partial.Excluded != a.Excluded || p.Partial.Complete != (a.Unread == 0) {
		t.Fatal("partial progress lost an assessed or unfinished path")
	}
}
