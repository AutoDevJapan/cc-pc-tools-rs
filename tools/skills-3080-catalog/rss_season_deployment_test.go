package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strings"
	"testing"
	"time"
)

const rssSeasonBundleSHA = "97e3d3b442e14a093f75bdeecd81e55d0ba17f13428f53f3806c8bc85a998bfb"
const rssSeasonManagedNamesSHA = "3eddb49df34088da3bd6c5feb64bab042f830a4406af9c4f492012cabedbd449"

type rssSeasonReceipt struct {
	ObservedAt  string `json:"observed_at"`
	Done        *bool  `json:"overall_complete"`
	Bundle      string `json:"bundle"`
	BundleSHA   string `json:"bundle_sha256"`
	Manifest    string `json:"manifest"`
	ManifestSHA string `json:"manifest_sha256"`
	Design      string `json:"designs"`
	DesignSHA   string `json:"design_sha256"`
	Created     int    `json:"new_targets_created"`
	Files       int    `json:"files_readback_verified"`
	OldWrites   *int   `json:"old_target_writes"`
	Changed     *bool  `json:"guard_or_registration_changed"`
	Discovery   struct {
		CheckedAt      string             `json:"checked_at"`
		Total          int                `json:"total_discovered_metadata"`
		Matches        int                `json:"target_matches"`
		Enabled        int                `json:"enabled_count"`
		Errors         *int               `json:"parse_errors"`
		SameNames      *int               `json:"same_names_other_paths"`
		ContextMarkers *int               `json:"context_budget_marker_count"`
		Warnings       int                `json:"scan_traversal_warning_markers"`
		Cause          *bool              `json:"warning_cause_linked"`
		Implicit       *bool              `json:"implicit_selection_proven"`
		Budget         *bool              `json:"full_context_budget_proven"`
		Model          *bool              `json:"model_called"`
		Exited         *bool              `json:"process_tree_exit_confirmed"`
		Drained        *bool              `json:"pending_stdout_read_drained"`
		Found          []newSkillMetadata `json:"found"`
	} `json:"discovery"`
	Retained struct {
		CheckedAt    string   `json:"checked_at"`
		IDs          []string `json:"managed_adoption_inventory_ids"`
		Expected     int      `json:"expected_count"`
		Found        int      `json:"found_count"`
		Enabled      int      `json:"enabled_count"`
		Errors       *int     `json:"parse_errors"`
		Descriptions int      `json:"new_description_match_count"`
		NamesSHA     string   `json:"known_target_names_sha256"`
		Model        *bool    `json:"model_called"`
		SavedNames   *bool    `json:"raw_existing_names_or_paths_saved"`
		AllBytes     *bool    `json:"all_disk_hash_or_prechange_equality_proven"`
		Exited       *bool    `json:"process_tree_exit_confirmed"`
		Drained      *bool    `json:"pending_stdout_read_drained"`
	} `json:"retained_known_metadata"`
	Use struct {
		Core     *bool `json:"core_passed"`
		Site     *bool `json:"real_site_extracted"`
		HTTP     *int  `json:"http_requests"`
		Hosted   *bool `json:"feed_hosted"`
		Renderer *bool `json:"real_renderer_executed"`
		UI       *bool `json:"UI_implemented"`
		PostTurn *bool `json:"postdeployment_model_turn_executed"`
	} `json:"explicit_application"`
	Checks []struct {
		Source string `json:"source"`
		SHA    string `json:"sha256"`
		Tests  int    `json:"tests"`
		Passed *bool  `json:"passed"`
	} `json:"self_authored_checks"`
}

func validateRssSeasonReceipt(e rssSeasonReceipt) bool {
	d, r, u := e.Discovery, e.Retained, e.Use
	if validateLastVerification(e.ObservedAt, d.CheckedAt, r.CheckedAt) != nil || len(r.IDs) != 35 || r.NamesSHA != rssSeasonManagedNamesSHA {
		return false
	}
	seenIDs := map[string]bool{}
	for _, id := range r.IDs {
		if id == "" || seenIDs[id] {
			return false
		}
		seenIDs[id] = true
	}
	if e.OldWrites == nil || *e.OldWrites != 0 || d.Errors == nil || *d.Errors != 0 || d.SameNames == nil || *d.SameNames != 0 || d.ContextMarkers == nil || *d.ContextMarkers != 0 || r.Errors == nil || *r.Errors != 0 || u.HTTP == nil || *u.HTTP != 0 {
		return false
	}
	if !recordedFalse(e.Done) || e.Created != 2 || e.Files != 10 || !recordedFalse(e.Changed) || e.BundleSHA != rssSeasonBundleSHA || d.Total != 330 || d.Matches != 2 || d.Enabled != 2 || d.Warnings != 2 || !recordedFalse(d.Cause) || !recordedFalse(d.Implicit) || !recordedFalse(d.Budget) || !recordedFalse(d.Model) || !recordedTrue(d.Exited) || !recordedTrue(d.Drained) || r.Expected != 48 || r.Found != 48 || r.Enabled != 48 || r.Descriptions != 2 || !validHex(r.NamesSHA, 64) || !recordedFalse(r.Model) || !recordedFalse(r.SavedNames) || !recordedFalse(r.AllBytes) || !recordedTrue(r.Exited) || !recordedTrue(r.Drained) || !recordedTrue(u.Core) || !recordedFalse(u.Site) || !recordedFalse(u.Hosted) || !recordedFalse(u.Renderer) || !recordedFalse(u.UI) || !recordedFalse(u.PostTurn) {
		return false
	}
	seen := map[string]bool{}
	for _, f := range d.Found {
		if (f.Name != "rss-feed-generator" && f.Name != "3d-four-seasons") || seen[f.Name] || !recordedTrue(f.Central) || !recordedTrue(f.Description) || !recordedTrue(f.EnabledPresent) || !recordedTrue(f.Enabled) {
			return false
		}
		seen[f.Name] = true
	}
	return len(seen) == 2
}
func TestRssSeasonActualBundleManifestAndOwnedFixtureProofsRemainBound(t *testing.T) {
	var e rssSeasonReceipt
	loadPublicReceipt(t, "skills-3080-rss-season-installation-evidence.json", &e)
	if !validateRssSeasonReceipt(e) {
		t.Fatal("RSS/season actual deployment, metadata retention or scope differs")
	}
	entries, original := managedMetadataSources(t)
	if err := validateManagedMetadataSnapshot(e.Retained.IDs, e.Retained.Expected, e.Retained.NamesSHA, entries, original); err != nil {
		t.Fatal(err)
	}
	if e.Bundle != "skills-3080-rss-season-reviewed-bundle.json" || e.Manifest != "skills-3080-installed-rss-season-manifest.json" || e.Design != "skills-3080-rss-season-designs.json" {
		t.Fatal("unexpected public artifact")
	}
	type file struct {
		Path    string `json:"path"`
		SHA     string `json:"sha256"`
		Content string `json:"content"`
		Bytes   int    `json:"bytes"`
	}
	type target struct {
		scenarioReceipt
		New    bool   `json:"new"`
		Before string `json:"before_sha256"`
		Files  []file `json:"files"`
	}
	var b struct {
		Root    string   `json:"root"`
		Targets []target `json:"targets"`
	}
	raw := loadPublicReceipt(t, e.Bundle, &b)
	h := sha256.Sum256(raw)
	if hex.EncodeToString(h[:]) != rssSeasonBundleSHA || b.Root != "D:/CC/Skills" || len(b.Targets) != 2 {
		t.Fatal("fixed bundle changed")
	}
	var m struct {
		BundleSHA string   `json:"bundle_sha256"`
		Targets   []target `json:"targets"`
	}
	raw = loadPublicReceipt(t, e.Manifest, &m)
	h = sha256.Sum256(raw)
	if hex.EncodeToString(h[:]) != e.ManifestSHA || m.BundleSHA != rssSeasonBundleSHA || len(m.Targets) != 2 {
		t.Fatal("actual installed manifest changed")
	}
	var designs struct {
		Targets []struct {
			scenarioReceipt
			ID        string `json:"inventory_id"`
			Repo      string `json:"source_repo"`
			Commit    string `json:"source_commit"`
			Path      string `json:"source_path"`
			SHA       string `json:"source_sha256"`
			EntrySHA  string `json:"entrypoint_sha256"`
			NoticeSHA string `json:"individual_notice_sha256"`
		} `json:"targets"`
	}
	raw = loadPublicReceipt(t, e.Design, &designs)
	h = sha256.Sum256(raw)
	if hex.EncodeToString(h[:]) != e.DesignSHA || len(designs.Targets) != 2 {
		t.Fatal("individually scoped design changed")
	}
	order := []string{"references/upstream-original.txt", "references/LICENSE.txt", "references/source.json", "references/sources.md", "SKILL.md"}
	wantNames := []string{"rss-feed-generator", "3d-four-seasons"}
	wantPrimary := []string{"ac4596e3710e45feb5056190934ffafc60d4226a9b8b8975d27c44653f7b5def", "2696252a795b1c234d4b06a1091418e68ae305f4d773e9b69e941392fa7286c7"}
	wantNotice := []string{"86fe9e21807d53a58fd97e61b070101241425b83ac9825686a09eb1894a797fc", "b2a071ba22fefd803f4e9d1f5c777dd79c62d191695586a1a73fd4d0ebd051c6"}
	for i, want := range wantNames {
		bt, mt, d := b.Targets[i], m.Targets[i], designs.Targets[i]
		if bt.Name != want || mt.Name != want || d.Name != want || !mt.New || mt.Before != "absent" || len(bt.Files) != 5 || len(mt.Files) != 5 || d.SHA != wantPrimary[i] || d.NoticeSHA != wantNotice[i] || d.ID == "" || d.Repo == "" || !validHex(d.Commit, 40) || d.Path == "" {
			t.Fatal("source/target/notice identity differs")
		}
		for j, bf := range bt.Files {
			mf := mt.Files[j]
			h := sha256.Sum256([]byte(bf.Content))
			if bf.Path != order[j] || mf.Path != bf.Path || bf.SHA != mf.SHA || bf.SHA != hex.EncodeToString(h[:]) || mf.Bytes != len([]byte(bf.Content)) {
				t.Fatal("ten installed file correspondence differs")
			}
		}
		if bt.Files[0].SHA != wantPrimary[i] || bt.Files[1].SHA != wantNotice[i] || bt.Files[4].SHA != d.EntrySHA {
			t.Fatal("original, notice and modified entrypoint were conflated")
		}
		if err := validateScenarioReceipt(d.scenarioReceipt, bt.scenarioReceipt, mt.scenarioReceipt, bt.Files[4].Content); err != nil {
			t.Fatal(err)
		}
	}
	wanted := map[string]struct {
		SHA   string
		Tests int
	}{"skills-3080-rss-fixture.go.txt": {"1dad2882d15f5df761749f42b82052e6babd0be44c59bce95a7f70ff68070678", 1}, "skills-3080-rss-xml-chars.go.txt": {"b62c30299956661dfe396c98d85e62a585464748cd63cf39b694e794ffd6f36b", 2}, "skills-3080-season-blend-model.go.txt": {"837b7cf5c160fde5f68c083608be30613c61c0bc5cde54f305dfc4ff90e5d579", 4}}
	seen := map[string]bool{}
	for _, c := range e.Checks {
		want, ok := wanted[c.Source]
		if !ok || seen[c.Source] || c.SHA != want.SHA || c.Tests != want.Tests || !recordedTrue(c.Passed) {
			t.Fatal("owned fixture/model source identity differs")
		}
		seen[c.Source] = true
		source, err := os.ReadFile("../../docs/" + c.Source)
		if err != nil {
			t.Fatal(err)
		}
		h := sha256.Sum256(source)
		if hex.EncodeToString(h[:]) != c.SHA || strings.Contains(string(source), "\r") {
			t.Fatal("owned fixture/model bytes changed")
		}
	}
	if len(seen) != 3 {
		t.Fatal("owned fixture/model proof missing")
	}
}

func TestManagedMetadataSnapshotsRejectUnboundHistoricalSets(t *testing.T) {
	for _, tc := range []struct{ name, want string }{
		{"valid", ""},
		{"duplicate_id", "historical metadata identity is absent or repeated"},
		{"unknown_id", "historical metadata identity is absent or repeated"},
		{"wrong_hash", "managed metadata fingerprint differs from its public snapshot sources"},
		{"renamed_target", "managed metadata fingerprint differs from its public snapshot sources"},
		{"missing_original", "original3048 target count differs"},
		{"duplicated_original", "original3048 target is absent or repeated"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var e rssSeasonReceipt
			loadPublicReceipt(t, "skills-3080-rss-season-installation-evidence.json", &e)
			entries, original := managedMetadataSources(t)
			switch tc.name {
			case "duplicate_id":
				e.Retained.IDs[1] = e.Retained.IDs[0]
			case "unknown_id":
				e.Retained.IDs[0] = "unassessed"
			case "wrong_hash":
				e.Retained.NamesSHA = strings.Repeat("a", 64)
			case "renamed_target":
				changed := false
				for i := range entries {
					if entries[i].ID == e.Retained.IDs[0] {
						entries[i].Target = "different-target"
						changed = true
						break
					}
				}
				if !changed {
					t.Fatal("no actual snapshot source to mutate")
				}
			case "missing_original":
				original = original[:13]
			case "duplicated_original":
				original[1] = original[0]
			}
			err := validateManagedMetadataSnapshot(e.Retained.IDs, e.Retained.Expected, e.Retained.NamesSHA, entries, original)
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || err.Error() != tc.want {
				t.Fatalf("%s reached another guard: %v", tc.name, err)
			}
		})
	}
}
func TestRssSeasonDiscoveryAndScopeRejectMissingOrOverclaimedEvidence(t *testing.T) {
	for _, kind := range []string{"valid", "disabled", "wrong_target", "parse_error", "old_name_lost", "same_name_elsewhere", "model_called", "cause_claimed", "context_claimed", "site_claimed", "UI_claimed", "postturn_claimed", "oldbytes_claimed", "overall_complete", "missing_old_writes", "missing_parse_errors", "missing_same_name_count", "missing_context_markers", "missing_retained_parse_errors", "missing_http_count", "observation_missing", "observation_early", "observation_late", "discovery_time_missing", "retained_time_missing", "wrong_valid_hex_names_hash", "snapshot_ids_missing", "snapshot_ids_duplicated"} {
		t.Run(kind, func(t *testing.T) {
			var e rssSeasonReceipt
			loadPublicReceipt(t, "skills-3080-rss-season-installation-evidence.json", &e)
			yes, no := true, false
			switch kind {
			case "disabled":
				e.Discovery.Found[0].Enabled = &no
			case "wrong_target":
				e.Discovery.Found[0].Name = "other"
			case "parse_error":
				*e.Discovery.Errors++
			case "old_name_lost":
				e.Retained.Found--
			case "same_name_elsewhere":
				*e.Discovery.SameNames++
			case "model_called":
				e.Discovery.Model = &yes
			case "cause_claimed":
				e.Discovery.Cause = &yes
			case "context_claimed":
				e.Discovery.Budget = &yes
			case "site_claimed":
				e.Use.Site = &yes
			case "UI_claimed":
				e.Use.UI = &yes
			case "postturn_claimed":
				e.Use.PostTurn = &yes
			case "oldbytes_claimed":
				e.Retained.AllBytes = &yes
			case "overall_complete":
				e.Done = &yes
			case "missing_old_writes":
				e.OldWrites = nil
			case "missing_parse_errors":
				e.Discovery.Errors = nil
			case "missing_same_name_count":
				e.Discovery.SameNames = nil
			case "missing_context_markers":
				e.Discovery.ContextMarkers = nil
			case "missing_retained_parse_errors":
				e.Retained.Errors = nil
			case "missing_http_count":
				e.Use.HTTP = nil
			case "observation_missing":
				e.ObservedAt = ""
			case "observation_early":
				e.ObservedAt = e.Discovery.CheckedAt
			case "observation_late":
				at, err := time.Parse(time.RFC3339Nano, e.ObservedAt)
				if err != nil {
					t.Fatal(err)
				}
				e.ObservedAt = at.Add(time.Second).Format(time.RFC3339Nano)
			case "discovery_time_missing":
				e.Discovery.CheckedAt = ""
			case "retained_time_missing":
				e.Retained.CheckedAt = ""
			case "wrong_valid_hex_names_hash":
				e.Retained.NamesSHA = strings.Repeat("a", 64)
			case "snapshot_ids_missing":
				e.Retained.IDs = nil
			case "snapshot_ids_duplicated":
				e.Retained.IDs[1] = e.Retained.IDs[0]
			}
			if validateRssSeasonReceipt(e) != (kind == "valid") {
				t.Fatalf("%s: scope/discovery acceptance differed", kind)
			}
		})
	}
}
