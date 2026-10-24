package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"testing"
)

type mengtoIdentity struct {
	Path  string `json:"path"`
	Blob  string `json:"git_blob"`
	SHA   string `json:"source_sha256"`
	Bytes int    `json:"bytes"`
}

// Both fingerprints were independently recomputed from the complete fixed
// tree/index/gzip corpus, rather than the growing read/pending classification.
const mengtoBlobFingerprint = "6ab27671db61bc922908e978d1ee15699a0948aa7cc4fba202881d4786c83afc"
const mengtoBodyFingerprint = "e10d13fe7aa34876f4b45050f71d0eb2bf505c2fa04200f657417f3e09dca0f3"

func validateMengtoIdentities(fixed []mengtoIdentity, read []partialEntry) error {
	ordered := append([]mengtoIdentity(nil), fixed...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Path < ordered[j].Path })
	blobs, bodies := sha256.New(), sha256.New()
	byPath := map[string]mengtoIdentity{}
	for _, e := range ordered {
		if e.Path == "" || byPath[e.Path].Path != "" || !validHex(e.Blob, 40) || !validHex(e.SHA, 64) || e.Bytes < 1 {
			return errors.New("invalid or repeated fixed source identity")
		}
		byPath[e.Path] = e
		fmt.Fprintf(blobs, "%s\t%s\t%d\n", e.Path, e.Blob, e.Bytes)
		fmt.Fprintf(bodies, "%s\t%s\t%d\n", e.Path, e.SHA, e.Bytes)
	}
	if len(ordered) != 177 || hex.EncodeToString(blobs.Sum(nil)) != mengtoBlobFingerprint || hex.EncodeToString(bodies.Sum(nil)) != mengtoBodyFingerprint {
		return errors.New("complete source identities differ from independent pinned fingerprints")
	}
	seen := map[string]bool{}
	for _, e := range read {
		f, ok := byPath[e.Path]
		if !ok || seen[e.Path] || e.SHA != f.SHA || e.Blob != f.Blob || e.Bytes != f.Bytes {
			return errors.New("assessed source differs from fixed path/blob/body/bytes")
		}
		seen[e.Path] = true
	}
	return nil
}

func TestMengtoPartialEvaluationPreservesCompleteTreeAndLiveLedger(t *testing.T) {
	var a partialAssessment
	loadPublicReceipt(t, "skills-3080-mengto-partial-evaluation.json", &a)
	var roster struct {
		Source string           `json:"source"`
		Commit string           `json:"commit"`
		Tree   string           `json:"tree_sha"`
		SHA    string           `json:"path_set_sha256"`
		Paths  []string         `json:"paths"`
		Fixed  []mengtoIdentity `json:"entries"`
	}
	loadPublicReceipt(t, "skills-3080-mengto-fixed-entrypoints.json", &roster)
	const pinned = "e607809a33ba5fa19352576bbe85fab030441a44560a6b4aba928125ac6cde45"
	if roster.Source != "MengTo/Skills" || roster.Commit != "398cd41c86ba42fbb30e944ae507f463648f08b7" || roster.Tree != "8afedc8719f37d2026b55aef282ac27d8d873d76" || roster.SHA != pinned || len(roster.Paths) != 177 || pathSetSHA(roster.Paths) != pinned {
		t.Fatal("complete source tree anchor changed")
	}
	fixedPaths := make([]string, 0, len(roster.Fixed))
	for _, f := range roster.Fixed {
		fixedPaths = append(fixedPaths, f.Path)
	}
	if pathSetSHA(fixedPaths) != pinned {
		t.Fatal("fixed identities differ from complete path roster")
	}
	if err := validateMengtoIdentities(roster.Fixed, a.Entries); err != nil {
		t.Fatal(err)
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
	if a.Read < 92 || p.Partial.Read != a.Read || p.Partial.Unread != a.Unread || p.Partial.Expected != a.Expected || p.Partial.Installed != a.Installed || p.Partial.Integrated != a.Integrated || p.Partial.Excluded != a.Excluded || p.Partial.Complete != (a.Unread == 0) {
		t.Fatal("partial progress lost an assessed or unfinished path")
	}
}

func TestMengtoIdentitiesRejectValidLookingSubstitutions(t *testing.T) {
	var roster struct {
		Fixed []mengtoIdentity `json:"entries"`
	}
	var a partialAssessment
	loadPublicReceipt(t, "skills-3080-mengto-fixed-entrypoints.json", &roster)
	loadPublicReceipt(t, "skills-3080-mengto-partial-evaluation.json", &a)
	for _, kind := range []string{"valid", "read_sha", "read_blob", "read_bytes", "read_path", "duplicate_read", "fixed_sha", "fixed_blob", "fixed_bytes", "same_forged_sha", "duplicate_fixed"} {
		t.Run(kind, func(t *testing.T) {
			fixed := append([]mengtoIdentity(nil), roster.Fixed...)
			read := append([]partialEntry(nil), a.Entries...)
			switch kind {
			case "read_sha", "same_forged_sha":
				read[0].SHA = mengtoBodyFingerprint
				if kind == "same_forged_sha" {
					for i := range fixed {
						if fixed[i].Path == read[0].Path {
							fixed[i].SHA = read[0].SHA
						}
					}
				}
			case "read_blob":
				read[0].Blob = "8afedc8719f37d2026b55aef282ac27d8d873d76"
			case "read_bytes":
				read[0].Bytes++
			case "read_path":
				read[0].Path = "unknown/SKILL.md"
			case "duplicate_read":
				read = append(read, read[0])
			case "fixed_sha":
				fixed[0].SHA = mengtoBodyFingerprint
			case "fixed_blob":
				fixed[0].Blob = "8afedc8719f37d2026b55aef282ac27d8d873d76"
			case "fixed_bytes":
				fixed[0].Bytes++
			case "duplicate_fixed":
				fixed[1] = fixed[0]
			}
			if err := validateMengtoIdentities(fixed, read); (err != nil) != (kind != "valid") {
				t.Fatalf("%s: %v", kind, err)
			}
		})
	}
}
