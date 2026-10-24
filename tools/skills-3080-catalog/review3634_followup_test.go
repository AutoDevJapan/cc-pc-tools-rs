package main

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

func validateParentDecisionUpdate(added int, ids []*string, excluded []cohortLedgerRow) error {
	if added != len(ids) {
		return errors.New("parent decision update count differs")
	}
	known := map[string]bool{}
	for _, r := range excluded {
		if r.Disposition == "excluded" {
			known[r.ID] = true
		}
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if id == nil || *id == "" || seen[*id] || !known[*id] {
			return errors.New("parent decision update contains null, unknown or repeated exclusion")
		}
		seen[*id] = true
	}
	return nil
}

func TestParentDecisionUpdatesAreRealExclusionsWithoutNullSentinels(t *testing.T) {
	var ledger struct {
		Excluded []cohortLedgerRow `json:"excluded_entries"`
	}
	loadPublicReceipt(t, "skills-3080-adoptions.json", &ledger)
	for _, file := range []string{"skills-3080-sixmile-partial-evaluation.json", "skills-3080-sixmile-next45-evaluation.json", "skills-3080-sixmile-next20-evaluation.json", "skills-3080-sixmile-parent6-evaluation.json"} {
		var a struct {
			Update *struct {
				Added int       `json:"added_exclusions"`
				IDs   []*string `json:"inventory_ids"`
			} `json:"parent_decision_updates"`
		}
		loadPublicReceipt(t, file, &a)
		if a.Update != nil {
			if err := validateParentDecisionUpdate(a.Update.Added, a.Update.IDs, ledger.Excluded); err != nil {
				t.Errorf("%s: %v", file, err)
			}
		}
	}
	unknown := "not-in-ledger"
	for _, tc := range []struct {
		name  string
		count int
		ids   []*string
	}{{"null", 1, []*string{nil}}, {"count", 0, []*string{&unknown}}, {"unknown", 1, []*string{&unknown}}} {
		t.Run(tc.name, func(t *testing.T) {
			if validateParentDecisionUpdate(tc.count, tc.ids, ledger.Excluded) == nil {
				t.Fatal("invalid parent decision accepted")
			}
		})
	}
}

func validateParent6CopyScopes(a extra45Assessment) error {
	expected := map[string]bool{
		"8bd0632a9aa3e03bf1521b4569d60b56b9d6adff2173b5781a828628424a2713": true,
		"23ed0a914aad35e60ca7dac150b4d9b8bf50c25d85e59bbfe737914bb433fb1f": true,
		"5720b4212fb326bed08e3af5b06b0183c4b104d21d9edc38ed618ca43ebe3eed": true,
		"57815bea6b5bf51924b689620cfa3a673348d0aabab7d416b50fcee1d066aeb3": false,
		"bd42c8dc7b807e3674a6be6b54abaaa266877515d64521ba16e8cc4312d06f98": true,
	}
	if len(a.Entries) != 6 {
		return errors.New("parent6 copy scope count differs")
	}
	accepted := 0
	for _, e := range a.Entries {
		want, ok := expected[e.Rights.SHA]
		if !ok || e.Rights.Copy == nil || *e.Rights.Copy != want || !x45False(e.Rights.Support) {
			return errors.New("parent6 individual notice copy scope changed")
		}
		if want {
			accepted++
		}
	}
	if accepted != 5 {
		return errors.New("parent6 accepted author-text scopes differ")
	}
	return nil
}

func TestParent6CopyScopesStayBoundToEachIndividualNotice(t *testing.T) {
	for i := -1; i < 6; i++ {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			var a extra45Assessment
			loadPublicReceipt(t, "skills-3080-sixmile-parent6-evaluation.json", &a)
			if i >= 0 {
				v := !*a.Entries[i].Rights.Copy
				a.Entries[i].Rights.Copy = &v
			}
			err := validateParent6CopyScopes(a)
			if i < 0 && err != nil {
				t.Fatal(err)
			}
			if i >= 0 && err == nil {
				t.Fatal("reversed individual scope accepted")
			}
		})
	}
}

type mcpSupportFingerprintEvidence struct {
	Official struct {
		Definition  string `json:"support_fingerprint_definition"`
		Fingerprint string `json:"support_fingerprint"`
		Support     []struct {
			Path string `json:"path"`
			Mode string `json:"mode"`
			Type string `json:"type"`
			Blob string `json:"sha"`
		} `json:"support"`
	} `json:"official_metadata_support_proof"`
}

const mcpSupportFingerprintDefinition = "Sort support entries by relative path bytes; concatenate UTF-8 mode + space + type + space + git_blob_sha1 + space + relative_path + LF; SHA-256 of the concatenation. Size is metadata verified by the preserved official tree proof, outside this fingerprint."

func validateMCPSupportFingerprint(a mcpSupportFingerprintEvidence) error {
	o := a.Official
	if len(o.Support) != 8 || o.Definition != mcpSupportFingerprintDefinition {
		return errors.New("MCP support fingerprint scope or formula absent")
	}
	entries := append([]struct {
		Path string `json:"path"`
		Mode string `json:"mode"`
		Type string `json:"type"`
		Blob string `json:"sha"`
	}(nil), o.Support...)
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	var lines []string
	seen := map[string]bool{}
	for _, r := range entries {
		if r.Mode != "100644" || r.Type != "blob" || seen[r.Path] || !x45Hex(r.Blob, 40) || !strings.HasPrefix(r.Path, "reference/") && !strings.HasPrefix(r.Path, "scripts/") {
			return errors.New("MCP support identity differs")
		}
		seen[r.Path] = true
		lines = append(lines, fmt.Sprintf("%s %s %s %s\n", r.Mode, r.Type, r.Blob, r.Path))
	}
	// x45Digest sorts whole rows; this formula explicitly sorts by path instead.
	if fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(lines, "")))) != "484bdc219079d5764d67e22bee92807b0c49da6889c439fbb3ed03e534874448" || o.Fingerprint != "484bdc219079d5764d67e22bee92807b0c49da6889c439fbb3ed03e534874448" {
		return errors.New("MCP support rows differ from independent fingerprint")
	}
	return nil
}
func TestMCPSupportRowsActuallyReproduceTheFixedFingerprint(t *testing.T) {
	for _, name := range []string{"valid", "changed_path", "changed_blob", "missing_formula"} {
		t.Run(name, func(t *testing.T) {
			var a mcpSupportFingerprintEvidence
			loadPublicReceipt(t, "skills-3080-mcp-sibling-duplicate-evidence.json", &a)
			switch name {
			case "changed_path":
				a.Official.Support[0].Path = "reference/changed.md"
			case "changed_blob":
				a.Official.Support[0].Blob = strings.Repeat("0", 40)
			case "missing_formula":
				a.Official.Definition = ""
			}
			err := validateMCPSupportFingerprint(a)
			if name == "valid" && err != nil {
				t.Fatal(err)
			}
			if name != "valid" && err == nil {
				t.Fatal("changed support accepted")
			}
		})
	}
}

type readingProseLeaf struct{ Location, Text string }

func collectReadingProse(v any, location string, out *[]readingProseLeaf) {
	switch x := v.(type) {
	case string:
		*out = append(*out, readingProseLeaf{location, x})
	case []any:
		for i, e := range x {
			collectReadingProse(e, fmt.Sprintf("%s/%d", location, i), out)
		}
	case map[string]any:
		for k, e := range x {
			collectReadingProse(e, location+"/"+k, out)
		}
	}
}

var globalReadingRatio = regexp.MustCompile(`(?i)\b(?:raw)?([0-9]{2,3})/(?:workflow)?([0-9]{2,3})\b`)

func hasGlobalReadingRatio(s string) bool {
	for _, m := range globalReadingRatio.FindAllStringSubmatch(s, -1) {
		a, _ := strconv.Atoi(m[1])
		b, _ := strconv.Atoi(m[2])
		if a >= 200 && b >= a && b-a <= 100 {
			return true
		}
	}
	return false
}
func historicalReadingLocation(location string) bool {
	for _, part := range strings.Split(location, "/") {
		if strings.Contains(part, "historical") || strings.Contains(part, "snapshot") || strings.Contains(part, "at_preparation") || strings.Contains(part, "at_intake") || part == "review_history" || part == "received_review_history" || part == "review_receptions" {
			return true
		}
	}
	return false
}
func TestAllReadingProseKeepsMutableGlobalRatiosOutOfCurrentNarrative(t *testing.T) {
	for _, file := range []string{"skills-3080-progress.json", "skills-3080-sixmile-partial-evaluation.json", "skills-3080-sixmile-next45-evaluation.json", "skills-3080-sixmile-next20-evaluation.json", "skills-3080-sixmile-parent6-evaluation.json", "skills-3080-sixmile-evaluation-index.json"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "docs", file))
		if err != nil {
			t.Fatal(err)
		}
		var value any
		if err = json.Unmarshal(data, &value); err != nil {
			t.Fatal(err)
		}
		var leaves []readingProseLeaf
		collectReadingProse(value, "", &leaves)
		for _, leaf := range leaves {
			// A preserved line-set comparison is a fixed source finding, not a global
			// reading count. Only these exact bytes at this exact location are exempt.
			fixedLineFinding := file == "skills-3080-sixmile-next45-evaluation.json" && leaf.Location == "/entries/12/findings/0" && leaf.Text == "#3との差はfrontmatterのname・versionの有無だけ(空でない行は225/226が同一)"
			if hasGlobalReadingRatio(leaf.Text) && !historicalReadingLocation(leaf.Location) && !fixedLineFinding {
				t.Errorf("mutable global reading ratio in %s%s", file, leaf.Location)
			}
		}
	}
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "skills-3080-next20-acceptance.md"))
	if err != nil {
		t.Fatal(err)
	}
	for i, paragraph := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n\n") {
		if hasGlobalReadingRatio(paragraph) && !strings.Contains(paragraph, "履歴") && !strings.Contains(paragraph, "時点") {
			t.Errorf("mutable global reading ratio in next20 acceptance paragraph%d", i+1)
		}
	}
	for _, text := range []string{"全体474/475へ投影する。", "Published raw474/workflow475.", "現在480/482です。"} {
		if !hasGlobalReadingRatio(text) {
			t.Fatalf("missed mutable ratio: %s", text)
		}
	}
	if hasGlobalReadingRatio("本文20/25の固定群") {
		t.Fatal("fixed small cohort mistaken for global aggregate")
	}
}

func followupValue(m map[string]any, keys ...string) any {
	var v any = m
	for _, k := range keys {
		x, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = x[k]
	}
	return v
}
func followupInt(v any) int { n, _ := v.(float64); return int(n) }
func validateFollowupDisplayRelations(p, ledger, evidence, index, duplicates, rights map[string]any) error {
	var source map[string]any
	for _, v := range ledger["entries"].([]any) {
		r := v.(map[string]any)
		if r["inventory_id"] == "github:b42ae7debd7346ed232f75cdb212f9d8a9cb05bd347c93c8b7de82e8d5433fc8" {
			if source != nil {
				return errors.New("v2 display has duplicate source")
			}
			source = r
		}
	}
	if source == nil || source["independent_review_pending"] != false || source["independent_review_pending_at_application"] != true || evidence["independent_review_pending"] != false || evidence["independent_review_pending_at_application"] != true || followupValue(p, "integration_acceptance", "independent_review_pending") != false {
		return errors.New("v2 accepted display reverted to pending or lost history")
	}
	for _, a := range []any{source["independent_review_acceptance"], evidence["public_content_acceptance"]} {
		r, ok := a.(map[string]any)
		if !ok || r["raw_sha256"] != "f5925ada6f6042b1e6f16da5b711790310cdbd03f78240fd486367a902260298" || r["must"] != float64(0) || followupInt(r["accepted_pr"]) != 381 || r["merge"] != "6f3c62f59e0138825df434e6b132ab28c0590905" {
			return errors.New("v2 display lost its original limited acceptance")
		}
	}
	if followupValue(p, "integration_acceptance", "independent_review", "raw_sha256") != source["independent_review_acceptance"].(map[string]any)["raw_sha256"] || followupInt(followupValue(p, "integration_acceptance", "independent_review", "todo")) != 3378 {
		return errors.New("v2 progress acceptance differs")
	}
	if followupInt(followupValue(p, "parent6_reflection_delta", "current_branch_raw")) != followupInt(p["all_fulltext_primary_paths_including_partial_sources"]) || followupInt(followupValue(p, "parent6_reflection_delta", "current_branch_workflow")) != followupInt(p["all_workflow_primary_paths_including_partial_sources"]) {
		return errors.New("current branch reading display differs")
	}
	delta := p["individual_rights_and_mcp_duplicates_delta"].(map[string]any)
	if followupInt(delta["new_individual_rights_exclusions"])+followupInt(delta["new_duplicate_exclusions"]) != followupInt(delta["total_new_exclusions"]) || followupInt(delta["branch_excluded"]) != followupInt(ledger["excluded"]) || followupInt(delta["branch_pending"]) != followupInt(ledger["pending_occurrences"]) || followupInt(delta["main_accepted_excluded_before_this_delta"])+followupInt(delta["total_new_exclusions"]) != followupInt(delta["branch_excluded"]) || followupInt(delta["current_duplicate_entries"]) != len(duplicates["entries"].([]any)) || followupInt(delta["current_individual_rights_entries"]) != len(rights["entries"].([]any)) {
		return errors.New("rights and duplicate delta display differs")
	}
	if followupInt(followupValue(p, "sixmile_body_read_coverage", "full_body_read")) != followupInt(index["raw_unmasked_primary_full_read"]) || followupInt(followupValue(p, "sixmile_body_read_coverage", "workflow_body_read")) != followupInt(index["workflow_primary_full_read"]) {
		return errors.New("source reading display differs")
	}
	held := p["central_target_holds_current"].(map[string]any)
	observations, ok := held["observations"].([]any)
	if !ok || followupInt(held["baseline_count"]) != 5 || followupInt(held["count"]) != 5+len(observations) || held["classification_unverified"] != true || held["existence_checked"] != false || held["size_bytes"] != nil || held["post_guard_target_read_write"] != float64(0) || held["configuration_or_exception_changes"] != float64(0) || held["owner_action_required"] != false {
		return errors.New("anonymous hold display invents clearance, size or count")
	}
	seen := map[any]bool{}
	for _, v := range observations {
		r, ok := v.(map[string]any)
		if !ok || r["case"] == nil || seen[r["case"]] {
			return errors.New("anonymous hold observation repeated")
		}
		seen[r["case"]] = true
	}
	return nil
}

func TestAcceptedReviewReadingDecisionAndAnonymousHoldDisplaysAgree(t *testing.T) {
	for _, name := range []string{"valid", "v2_pending", "v2_history_lost", "v2_wrong_receipt", "branch_read_count", "delta_sum", "duplicate_count", "hold_count", "hold_size"} {
		t.Run(name, func(t *testing.T) {
			var p, l, e, i, d, r map[string]any
			loadPublicReceipt(t, "skills-3080-progress.json", &p)
			loadPublicReceipt(t, "skills-3080-adoptions.json", &l)
			loadPublicReceipt(t, "skills-3080-gh-ci-integration-v2-evidence.json", &e)
			loadPublicReceipt(t, "skills-3080-sixmile-evaluation-index.json", &i)
			loadPublicReceipt(t, "skills-3080-duplicate-decisions.json", &d)
			loadPublicReceipt(t, "skills-3080-rights-decisions.json", &r)
			switch name {
			case "v2_pending":
				e["independent_review_pending"] = true
			case "v2_history_lost":
				e["independent_review_pending_at_application"] = false
			case "v2_wrong_receipt":
				e["public_content_acceptance"].(map[string]any)["raw_sha256"] = "forged"
			case "branch_read_count":
				p["parent6_reflection_delta"].(map[string]any)["current_branch_raw"] = float64(1)
			case "delta_sum":
				p["individual_rights_and_mcp_duplicates_delta"].(map[string]any)["total_new_exclusions"] = float64(1)
			case "duplicate_count":
				p["individual_rights_and_mcp_duplicates_delta"].(map[string]any)["current_duplicate_entries"] = float64(1)
			case "hold_count":
				p["central_target_holds_current"].(map[string]any)["count"] = float64(1)
			case "hold_size":
				p["central_target_holds_current"].(map[string]any)["size_bytes"] = float64(0)
			}
			err := validateFollowupDisplayRelations(p, l, e, i, d, r)
			if name == "valid" && err != nil {
				t.Fatal(err)
			}
			if name != "valid" && err == nil {
				t.Fatal("contradictory current display accepted")
			}
		})
	}
}
