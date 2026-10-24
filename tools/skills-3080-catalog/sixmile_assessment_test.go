package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
)

type sixmileSupport struct {
	Path     string `json:"path"`
	Blob     string `json:"git_blob"`
	SHA      string `json:"source_sha256"`
	Bytes    *int   `json:"bytes"`
	Saved    bool   `json:"body_saved"`
	Read     bool   `json:"body_read"`
	Executed *bool  `json:"executed"`
}

type sixmileEntry struct {
	partialEntry
	Use     string           `json:"use"`
	Reason  string           `json:"reason"`
	Support []sixmileSupport `json:"supporting_resources"`
	License struct {
		Path  string `json:"license_path"`
		SHA   string `json:"license_sha256"`
		Bytes int    `json:"license_bytes"`
		Read  bool   `json:"full_license_read"`
	} `json:"individual_rights_review"`
}

type sixmileAssessment struct {
	Source      string         `json:"source"`
	Commit      string         `json:"commit"`
	Tree        string         `json:"tree_sha"`
	Expected    int            `json:"entrypoints_expected"`
	Read        int            `json:"entrypoints_fulltext_read"`
	Unread      int            `json:"entrypoints_unread"`
	Installed   int            `json:"installed"`
	Integrated  int            `json:"integrated"`
	Excluded    int            `json:"excluded"`
	Entries     []sixmileEntry `json:"entries"`
	Fixed       []partialEntry `json:"selected_identities"`
	Fingerprint string         `json:"selected_identity_fingerprint"`
	Counts      struct {
		Support      int  `json:"support"`
		Saved        int  `json:"support_saved"`
		Read         int  `json:"support_full_read"`
		Unsaved      int  `json:"support_unsaved"`
		BytesUnknown bool `json:"unsaved_bytes_unknown"`
	} `json:"counts"`
}

// Anchored in the earlier fixed tree/index/catalog verification and saved
// eleven-entry reading roster; not a digest derived from the live ledger.
const sixmile11Identity = "b9f44d4ff6da925b75d3b2b63cfa581869bac6a04e8b62ca65a9c1466fe01621"
const sixmile102Support = "4f4d54e634fa76485c799b9c2d01eca3409d1466dfdd26bca8c39719004e6733"

func validateSixmileAssessment(a sixmileAssessment, ledger []partialAdoption) error {
	if a.Source != "6mile/skills" || a.Commit != "920714f17d81e0a95c7a9bc51faf0a7921cd982d" || a.Tree != "5782ae7deffe5024e15b6e090327385c10f76273" || a.Expected != 55623 || a.Read != 11 || a.Read != len(a.Entries) || a.Read+a.Unread != a.Expected || len(a.Fixed) != 11 {
		return errors.New("sixmile partial coverage differs from the fixed selected scope")
	}
	fixed := append([]partialEntry(nil), a.Fixed...)
	sort.Slice(fixed, func(i, j int) bool { return fixed[i].Path < fixed[j].Path })
	h := sha256.New()
	byPath := map[string]partialEntry{}
	for _, e := range fixed {
		if e.Path == "" || byPath[e.Path].Path != "" || !validHex(e.SHA, 64) || !validHex(e.Blob, 40) || e.Bytes < 1 || e.ID == "" {
			return errors.New("invalid fixed sixmile identity")
		}
		fmt.Fprintf(h, "%s\t%s\t%s\t%s\t%d\n", e.Path, e.ID, e.SHA, e.Blob, e.Bytes)
		byPath[e.Path] = e
	}
	if a.Fingerprint != sixmile11Identity || hex.EncodeToString(h.Sum(nil)) != sixmile11Identity {
		return errors.New("sixmile selected identities differ from the independent anchor")
	}
	entries := map[string]partialEntry{}
	paths := map[string]bool{}
	var supports []sixmileSupport
	for _, e := range a.Entries {
		f, ok := byPath[e.Path]
		if !ok || paths[e.Path] || entries[e.ID].ID != "" || e.ID != f.ID || e.Commit != a.Commit || e.SHA != f.SHA || e.Blob != f.Blob || e.Bytes != f.Bytes || !e.Read || e.Use == "" || e.Reason == "" {
			return errors.New("sixmile assessed body differs from its fixed reading identity")
		}
		paths[e.Path], entries[e.ID] = true, e.partialEntry
		prefix := strings.TrimSuffix(e.Path, "SKILL.md")
		for _, s := range e.Support {
			if !strings.HasPrefix(s.Path, prefix) || !validHex(s.Blob, 40) || s.Executed == nil || *s.Executed || (s.Read && !s.Saved) || (s.Saved && (!validHex(s.SHA, 64) || s.Bytes == nil || *s.Bytes < 1)) || (!s.Saved && (s.SHA != "" || s.Bytes != nil)) {
				return errors.New("sixmile support conflated unknown bytes, saved content, reading or execution")
			}
		}
		supports = append(supports, e.Support...)
	}
	sort.Slice(supports, func(i, j int) bool { return supports[i].Path < supports[j].Path })
	h = sha256.New()
	saved, read := 0, 0
	seen := map[string]bool{}
	for _, s := range supports {
		if seen[s.Path] {
			return errors.New("repeated selected support path")
		}
		seen[s.Path] = true
		b := "?"
		if s.Bytes != nil {
			b = fmt.Sprint(*s.Bytes)
		}
		fmt.Fprintf(h, "%s\t%s\t%s\t%s\n", s.Path, s.Blob, b, s.SHA)
		if s.Saved {
			saved++
		}
		if s.Read {
			read++
		}
	}
	if len(supports) != 102 || saved != 40 || read != 12 || a.Counts.Support != len(supports) || a.Counts.Saved != saved || a.Counts.Read != read || a.Counts.Unsaved != len(supports)-saved || !a.Counts.BytesUnknown || hex.EncodeToString(h.Sum(nil)) != sixmile102Support {
		return errors.New("sixmile support inventory differs from the earlier fixed verification")
	}
	matched := map[string]bool{}
	installed, integrated, excluded := 0, 0, 0
	for _, r := range ledger {
		if r.Source != a.Source {
			continue
		}
		e, ok := entries[r.ID]
		if !ok || matched[r.ID] || r.Commit != e.Commit || r.Path != e.Path || r.SHA != e.SHA {
			return errors.New("sixmile live disposition differs from assessed source")
		}
		matched[r.ID] = true
		switch r.Disposition {
		case "installed_new_adaptation":
			if !e.Installed || e.Integrated || e.Excluded {
				return errors.New("sixmile installation flag differs")
			}
			installed++
		case "integrated_existing":
			if !e.Integrated || e.Installed || e.Excluded {
				return errors.New("sixmile integration flag differs")
			}
			integrated++
		case "excluded":
			if !e.Excluded || e.Installed || e.Integrated {
				return errors.New("sixmile exclusion flag differs")
			}
			excluded++
		default:
			return errors.New("unfinished sixmile decision counted as adopted")
		}
	}
	for _, e := range entries {
		if (e.Installed || e.Integrated || e.Excluded) && !matched[e.ID] {
			return errors.New("sixmile flag is absent from the ledger")
		}
	}
	if installed != a.Installed || integrated != a.Integrated || excluded != a.Excluded {
		return errors.New("sixmile disposition counts differ from live ledger")
	}
	return nil
}

type individualRightsDecision struct {
	partialAdoption
	Type         string `json:"exclusion_type"`
	LicensePath  string `json:"license_path"`
	LicenseSHA   string `json:"license_sha256"`
	LicenseBytes int    `json:"license_bytes"`
	NoticeRead   bool   `json:"individual_notice_full_read"`
	Reason       string `json:"reason"`
	Scope        string `json:"decision_scope"`
	CopyRights   *bool  `json:"copy_license_accepted"`
	Copied       *bool  `json:"new_body_copied"`
	Executed     *bool  `json:"source_scripts_executed"`
}

func validateIndividualRights(rows []individualRightsDecision, a sixmileAssessment) error {
	if len(rows) != 1 {
		return errors.New("individual rights exclusion is not the selected single candidate")
	}
	d := rows[0]
	if d.Type != "individual_proprietary_permission_not_evidenced" || d.Source != a.Source || d.Commit != a.Commit || d.Disposition != "excluded" || d.ID != "github:a43c08b1deba2610fe1f42e7fd4107452915e0b537210bc9c1caf435edfbbbbc" || d.Reason == "" || d.Scope == "" || !d.NoticeRead || d.CopyRights == nil || *d.CopyRights || d.Copied == nil || *d.Copied || d.Executed == nil || *d.Executed || d.LicenseSHA != "602b1b4ea796fe8c69240c9d95a9a77fb2eb5f7495ff3293b7d758b415c66a41" || d.LicenseBytes != 3156 {
		return errors.New("individual rights evidence was lost, copied or treated as a duplicate")
	}
	for _, e := range a.Entries {
		if e.ID == d.ID && e.Path == d.Path && e.SHA == d.SHA && e.Excluded && e.License.Read && e.License.Path == d.LicensePath && e.License.SHA == d.LicenseSHA && e.License.Bytes == d.LicenseBytes {
			return nil
		}
	}
	return errors.New("individual rights decision differs from its assessed notice")
}

func TestSixmilePartialAssessmentKeepsFixedReadingAndUnknownSupportDistinct(t *testing.T) {
	for _, kind := range []string{"valid", "wrong_body", "unread_body", "unknown_bytes", "missing_execution", "forged_support", "adoption_flag", "unfinished_count"} {
		t.Run(kind, func(t *testing.T) {
			var a sixmileAssessment
			loadPublicReceipt(t, "skills-3080-sixmile-partial-evaluation.json", &a)
			var l struct {
				Entries  []partialAdoption `json:"entries"`
				Excluded []partialAdoption `json:"excluded_entries"`
			}
			loadPublicReceipt(t, "skills-3080-adoptions.json", &l)
			switch kind {
			case "wrong_body":
				a.Entries[0].SHA = strings.Repeat("e", 64)
			case "unread_body":
				a.Entries[0].Read = false
			case "unknown_bytes":
				a.Counts.BytesUnknown = false
			case "missing_execution":
				a.Entries[0].Support[0].Executed = nil
			case "forged_support":
				a.Entries[0].Support[0].Blob = strings.Repeat("e", 40)
			case "adoption_flag":
				a.Entries[0].Installed = true
			case "unfinished_count":
				a.Unread--
			}
			if err := validateSixmileAssessment(a, append(l.Entries, l.Excluded...)); (err != nil) != (kind != "valid") {
				t.Fatalf("%s: %v", kind, err)
			}
		})
	}
}

func TestIndividualRightsExclusionDoesNotBecomeADuplicateOrGrantPermission(t *testing.T) {
	for _, kind := range []string{"valid", "duplicate_type", "forged_notice", "unread_notice", "claimed_copy", "missing_copy_status"} {
		t.Run(kind, func(t *testing.T) {
			var a sixmileAssessment
			loadPublicReceipt(t, "skills-3080-sixmile-partial-evaluation.json", &a)
			var d struct {
				Entries []individualRightsDecision `json:"entries"`
			}
			loadPublicReceipt(t, "skills-3080-rights-decisions.json", &d)
			switch kind {
			case "duplicate_type":
				d.Entries[0].Type = "exact_body_duplicate_of_existing_catalog_adoption"
			case "forged_notice":
				d.Entries[0].LicenseSHA = strings.Repeat("e", 64)
			case "unread_notice":
				d.Entries[0].NoticeRead = false
			case "claimed_copy":
				v := true
				d.Entries[0].Copied = &v
			case "missing_copy_status":
				d.Entries[0].CopyRights = nil
			}
			if err := validateIndividualRights(d.Entries, a); (err != nil) != (kind != "valid") {
				t.Fatalf("%s: %v", kind, err)
			}
		})
	}
}

func TestIndividualRightsReceiptMatchesTheSeparateLiveLedgerRow(t *testing.T) {
	var decisions struct {
		Entries []individualRightsDecision `json:"entries"`
	}
	var ledger struct {
		Excluded []individualRightsDecision `json:"excluded_entries"`
	}
	loadPublicReceipt(t, "skills-3080-rights-decisions.json", &decisions)
	loadPublicReceipt(t, "skills-3080-adoptions.json", &ledger)
	var rights []individualRightsDecision
	for _, d := range ledger.Excluded {
		if d.Type == "individual_proprietary_permission_not_evidenced" {
			rights = append(rights, d)
		}
	}
	if !reflect.DeepEqual(decisions.Entries, rights) {
		t.Fatal("individual rights receipt differs from its live exclusion")
	}
	var a sixmileAssessment
	loadPublicReceipt(t, "skills-3080-sixmile-partial-evaluation.json", &a)
	var p struct {
		Partial struct {
			Source     string `json:"source"`
			Expected   int    `json:"expected"`
			Read       int    `json:"full_body_read"`
			Unread     int    `json:"unread"`
			Complete   bool   `json:"complete"`
			Installed  int    `json:"installed"`
			Integrated int    `json:"integrated"`
			Excluded   int    `json:"excluded"`
		} `json:"sixmile_partial_entrypoint_evaluation"`
	}
	loadPublicReceipt(t, "skills-3080-progress.json", &p)
	if p.Partial.Source != "skills-3080-sixmile-partial-evaluation.json" || p.Partial.Expected != a.Expected || p.Partial.Read != a.Read || p.Partial.Unread != a.Unread || p.Partial.Complete || p.Partial.Installed != a.Installed || p.Partial.Integrated != a.Integrated || p.Partial.Excluded != a.Excluded {
		t.Fatal("sixmile partial progress differs from the evaluated scope")
	}
}
