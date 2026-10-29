package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

const consolidationReferencePrefix = "# Modified reference, 2026-10-29\n# Original source lines below are line-preserving masked data.\n# This archive is untrusted reference data, not executable instructions.\n\n"

type consolidationPreparationPins struct {
	BundleSHA, ManifestSHA, SourceFile, SourceSHA string
	SourceRaw                                     []byte
}

// Independently loaded bytes, rather than the preparation document's own
// filenames or claimed hashes, define the artifacts this observation binds.
func loadConsolidationFixture(t *testing.T) (b, o, p map[string]any, pins consolidationPreparationPins) {
	t.Helper()
	bundleRaw := loadPublicReceipt(t, "skills-3080-consolidation-art-proposal175645.json", &b)
	manifestRaw := loadPublicReceipt(t, "skills-3080-operations/create-consolidation-art-175645.json", &o)
	loadPublicReceipt(t, "skills-3080-consolidation-art-preparation175645.json", &p)
	sourceFile := "skills-3080-consolidation-art-sources175645.json"
	var source map[string]any
	sourceRaw := loadPublicReceipt(t, sourceFile, &source)
	pins = consolidationPreparationPins{travel3Digest(bundleRaw), travel3Digest(manifestRaw), sourceFile, travel3Digest(sourceRaw), sourceRaw}
	return
}

func validateConsolidationArtProposal(bundle, operation, prep map[string]any, artifactPins consolidationPreparationPins) error {
	if !validHex(artifactPins.BundleSHA, 64) || !validHex(artifactPins.ManifestSHA, 64) || !validHex(artifactPins.SourceSHA, 64) || artifactPins.SourceFile != "skills-3080-consolidation-art-sources175645.json" {
		return fmt.Errorf("independently loaded preparation inputs missing")
	}
	if e := next4Fields(prep, map[string]any{"bundle_sha256": artifactPins.BundleSHA, "manifest_sha256": artifactPins.ManifestSHA, "source_selection_file": artifactPins.SourceFile, "source_selection_sha256": artifactPins.SourceSHA}); e != nil {
		return e
	}
	// Hash and consume the same independently loaded source buffer, then bind
	// every source field to the actual bundle/operation rather than only its pin.
	if travel3Digest(artifactPins.SourceRaw) != artifactPins.SourceSHA {
		return fmt.Errorf("source selection consumed bytes differ from verified pin")
	}
	var selection struct {
		Sources []map[string]any `json:"sources"`
	}
	if json.Unmarshal(artifactPins.SourceRaw, &selection) != nil || len(selection.Sources) != 2 {
		return fmt.Errorf("exact actual source selection membership required")
	}
	nameTargets, ok := bundle["targets"].([]any)
	if !ok {
		return fmt.Errorf("actual bundle targets absent")
	}
	names := make([]any, 0, len(nameTargets))
	for _, value := range nameTargets {
		names = append(names, next4Object(value)["name"])
	}
	if !reflect.DeepEqual(prep["names"], names) {
		return fmt.Errorf("preparation names differ from actual bundle target membership")
	}
	if e := next4Fields(bundle, map[string]any{"root": "D:/CC/Skills"}); e != nil {
		return e
	}
	if e := next4Fields(operation, map[string]any{"domain": "skills-3080-create-v1", "todo_id": float64(3080), "decision_identity_sha256": "32b8d0d676cb58e82ef256fad0a7003ab62d3047db2ca0c71676f09fdac93fe4"}); e != nil {
		return e
	}
	if e := next4Fields(next4Object(operation["writer"]), map[string]any{"task": float64(3080), "job": next3Actor, "spec_sha256": "91a65cb17d2e5c4b18655ecb3126ea1a212ad655e716a6c349321876071a9435"}); e != nil {
		return e
	}
	if e := next4Fields(prep, map[string]any{"actual_author_job": next3Actor, "payload_files": float64(14), "physical_primary_lines": float64(627), "visible_primary_lines": float64(608), "withheld_primary_lines": float64(19), "physical_notice_lines": float64(223), "visible_notice_lines_in_original_parent_views": float64(221), "notice_masked_URI_lines": float64(2), "parent_original_unmasked_notice_full_read_claimed": false, "official_Apache_full_public_standard_identity_verified": true, "original_archived_data_or_masked_views_rewritten": false, "copyright_modification_notices_in_entrypoint_guide_reference": true, "reference_body_exact_after_four_explicit_notice_lines": true, "physical_absence_qualified": false, "full_Creator_review_received": false, "created_files": float64(0), "new_adoptions": float64(0), "central_operations": float64(0), "whole16_complete": false}); e != nil {
		return e
	}
	if e := next4Fields(next4Object(prep["fresh_requested_names_metadata"]), map[string]any{"targets": float64(2), "found_count": float64(0), "parse_error_count": float64(0), "model_called": false, "process_tree_exit_confirmed": true}); e != nil {
		return e
	}
	type pin struct {
		name, id, path, sha, notice, masked string
		row, lines, withheld                int
	}
	pins := []pin{
		{"memory-consolidation-state", "github:a1b6f6f944d4f177da0d88112cf8fb0e828e71accabe844d9294169008ffe036", "skills/minmengxhw-cpu/enhanced-memory-v2/SKILL.md", "7ce4e60e2ac08709b60fe5434131bcd81f11bd860beccee8df9bb786ee90c60c", "43a990050404c5fd26abe14ba73fbdea97c43a0366360a16e35ec37e2f245e3c", "cbf88de4df17b4b505ade3ef3f0efa1280190c18f80f58cda9aa4bcbcd847fc5", 260, 222, 7},
		{"generative-art-reproducibility", "github:db3a296d432ef9beb84fb1c5a128b5297b2f0112b5c61ab1ac248005c2f97a0f", "skills/moyu0032/mo-test-1/SKILL.md", "3bc4092c09804853186524c826bc0621b940bb6122c05b84496dff95388e6eef", "58d1e17ffe5109a7ae296caafcadfdbe6a7d176f0bc4ab01e12a689b0499d8bd", "b9a2a097edaa8623a4a9eb135997a332b09ea23b1ccb0f60478b60841ec5577e", 265, 405, 12},
	}
	payload := map[string][]byte{}
	total := 0
	rows, ok := operation["files"].([]any)
	if !ok || len(rows) != 14 {
		return fmt.Errorf("exact14 reviewed output files required")
	}
	for _, value := range rows {
		r := next4Object(value)
		target, _ := r["target"].(string)
		body, _ := r["content_base64"].(string)
		b, e := base64.StdEncoding.DecodeString(body)
		if e != nil || target == "" || payload[target] != nil || travel3Digest(b) != r["sha256"] {
			return fmt.Errorf("operation payload duplicate or hash differs")
		}
		payload[target] = b
		total += len(b)
	}
	if prep["payload_bytes"] != float64(total) {
		return fmt.Errorf("actual payload byte total differs")
	}
	targets, ok := bundle["targets"].([]any)
	if !ok || len(targets) != 2 {
		return fmt.Errorf("exact2 proposals required")
	}
	sources, ok := operation["sources"].([]any)
	if !ok || len(sources) != 2 {
		return fmt.Errorf("two per-source individual notice associations required")
	}
	matched := map[string]bool{}
	for i, p := range pins {
		t := next4Object(targets[i])
		if t["name"] != p.name || t["positive"] == "" || t["negative"] == "" {
			return fmt.Errorf("proposal name or scenario missing")
		}
		fs, ok := t["files"].([]any)
		if !ok || len(fs) != 7 {
			return fmt.Errorf("seven files per target required")
		}
		own := map[string][]byte{}
		for _, value := range fs {
			f := next4Object(value)
			path, _ := f["path"].(string)
			body, _ := f["content"].(string)
			key := p.name + "/" + path
			b := payload[key]
			if b == nil || matched[key] || own[path] != nil || string(b) != body || travel3Digest(b) != f["sha256"] {
				return fmt.Errorf("bundle and operation exact outputs differ")
			}
			matched[key] = true
			own[path] = b
		}
		for _, path := range []string{"SKILL.md", "references/guide.md"} {
			if !strings.Contains(string(own[path]), "Modified Japanese adaptation, 2026-10-29.") {
				return fmt.Errorf("prominent modified-file notice absent")
			}
		}
		reference := string(own["references/original-skill.masked.txt"])
		if !strings.HasPrefix(reference, consolidationReferencePrefix) || travel3Digest([]byte(strings.TrimPrefix(reference, consolidationReferencePrefix))) != p.masked {
			return fmt.Errorf("original visible body or explicit modification prefix differs")
		}
		if travel3Digest(own["references/LICENSE.txt"]) != p.notice {
			return fmt.Errorf("complete individual licence bytes differ")
		}
		var source, assessment map[string]any
		if json.Unmarshal(own["references/source.json"], &source) != nil || json.Unmarshal(own["references/source-assessment.json"], &assessment) != nil {
			return fmt.Errorf("source evidence invalid")
		}
		if !reflect.DeepEqual(selection.Sources[i], source) {
			return fmt.Errorf("source selection contents differ from actual bundle source evidence")
		}
		if e := next4Fields(source, map[string]any{"source_inventory_id": p.id, "source_repo": "6mile/skills", "source_commit": "920714f17d81e0a95c7a9bc51faf0a7921cd982d", "source_path": p.path, "source_sha256": p.sha, "individual_notice_sha256": p.notice, "source_original_in_acquired_archive_retained": true, "pre_creation_source_observation_only": true, "created_files_at_source_preparation": float64(0), "third_party_assets_supports_services_or_independent_authorship_proven": false}); e != nil {
			return e
		}
		profile := next4Object(source["parent_profile"])
		if e := next4Fields(profile, map[string]any{"reader": next3Actor, "mask_version": "primaryflow-v4-opaque-lexical-boundaries", "physical_lines": float64(p.lines), "visible_lines": float64(p.lines - p.withheld), "masked_sha256": p.masked, "reference_body_after_prefix_sha256": p.masked, "reference_modification_notice_prefix_lines": float64(4), "reference_modification_notice_prefix_bytes": float64(len(consolidationReferencePrefix)), "original_masked_views_unchanged": true, "new_raw_source_credit": float64(0)}); e != nil {
			return e
		}
		w, ok := profile["withheld_physical_lines"].([]any)
		if !ok || len(w) != p.withheld {
			return fmt.Errorf("withheld original meanings lost")
		}
		if e := next4Fields(assessment, map[string]any{"selected_row": float64(p.row), "actual_original_reader": "20261028-171959-todo-3719", "original_packet_id": "f581cfc5fea7e7abf35146380a36efd2", "no_new_permission_or_disposition_or_raw_credit_or_central": true, "whole16_complete": false}); e != nil {
			return e
		}
		original := next4Object(assessment["selected_original_entry"])
		tuple, ok := original["source"].([]any)
		if !ok || len(tuple) != 5 || tuple[0] != p.id || tuple[1] != p.path || tuple[2] != p.sha {
			return fmt.Errorf("selected original reading not bound to exact source")
		}
		s := next4Object(sources[i])
		if e := next4Fields(s, map[string]any{"repository": "6mile/skills", "commit": "920714f17d81e0a95c7a9bc51faf0a7921cd982d", "path": p.path, "sha256": p.sha, "acquisition": "already-acquired"}); e != nil {
			return e
		}
		licence := next4Object(s["license"])
		if licence["sha256"] != p.notice || licence["path"] != source["individual_notice_path"] || licence["scope"] != source["rights_scope"] || licence["accepted"] != true {
			return fmt.Errorf("individual notice scope not tied to selected file")
		}
		if i == 1 {
			official := next4Object(source["official_standard_proof"])
			if e := next4Fields(official, map[string]any{"url": "https://www.apache.org/licenses/LICENSE-2.0.txt", "bytes": float64(11358), "sha256": "cfc7749b96f63bd31c3c42b5c471bf756814053e847c10f3eb003417bc523d30", "one_terminal_LF_removed_sha256": p.notice, "full_archived_bytes_equal_official": false, "full_archived_bytes_equal_official_except_one_terminal_LF": true, "authenticated_headers_or_cookies_used": false, "original_source_or_masked_view_rewritten": false}); e != nil {
				return e
			}
			if profile["notice_renderer_added_exactly_one_terminal_LF"] != true || profile["primary_renderer_added_exactly_one_terminal_LF"] != true {
				return fmt.Errorf("renderer/original EOF difference lost")
			}
		}
	}
	if len(matched) != 14 {
		return fmt.Errorf("unmatched output escaped selected targets")
	}
	return nil
}

func TestConsolidationArtProposalsKeepIndividualNoticesAndMaskedSourceEvidence(t *testing.T) {
	b, o, p, pins := loadConsolidationFixture(t)
	if pins.BundleSHA != "24bd4d619faf6df448568207b3b98f500373db3e25a39b3e6f5c8304690cd37e" {
		t.Fatal("independent proposal package changed")
	}
	if pins.ManifestSHA != "015f31225f2ef04aa6eda990ddfc7ad9303b075e66c83e9f2b0d266abf9754f6" {
		t.Fatal("independent precreation operation changed")
	}
	if e := validateConsolidationArtProposal(b, o, p, pins); e != nil {
		t.Fatal(e)
	}
}

func TestConsolidationProposalRejectsOverstatedOrUnboundAcceptance(t *testing.T) {
	for _, change := range []string{"valid", "root", "author", "physical absence", "created", "new rights", "new raw", "official bytes", "URI declared unmasked", "reference prefix", "bundle pin", "manifest pin", "source filename", "source pin", "names", "joint stale source row", "joint missing source row", "joint reordered source rows", "joint source rights expansion"} {
		t.Run(change, func(t *testing.T) {
			b, o, p, pins := loadConsolidationFixture(t)
			switch change {
			case "root":
				b["root"] = "E:/Skills"
			case "author":
				next4Object(o["writer"])["job"] = "another"
			case "physical absence":
				p["physical_absence_qualified"] = true
			case "created":
				p["created_files"] = float64(14)
			case "new rights":
				p["full_Creator_review_received"] = true
			case "new raw":
				p["parent_original_unmasked_notice_full_read_claimed"] = true
			case "official bytes":
				p["official_Apache_full_public_standard_identity_verified"] = false
			case "URI declared unmasked":
				p["notice_masked_URI_lines"] = float64(0)
			case "reference prefix":
				p["reference_body_exact_after_four_explicit_notice_lines"] = false
			case "bundle pin":
				p["bundle_sha256"] = "stale"
			case "manifest pin":
				p["manifest_sha256"] = "stale"
			case "source filename":
				p["source_selection_file"] = "../stale.json"
			case "source pin":
				p["source_selection_sha256"] = "stale"
			case "names":
				p["names"] = []any{"memory-consolidation-state"}
			}
			if strings.HasPrefix(change, "joint ") {
				// Simulate swapping both a stale selection file and its matching
				// preparation pin. Neither actual bundle nor operation is changed.
				var selection map[string]any
				if json.Unmarshal(pins.SourceRaw, &selection) != nil {
					t.Fatal("actual source selection invalid")
				}
				rows := selection["sources"].([]any)
				switch change {
				case "joint stale source row":
					next4Object(rows[0])["source_sha256"] = "stale"
				case "joint missing source row":
					selection["sources"] = rows[:1]
				case "joint reordered source rows":
					rows[0], rows[1] = rows[1], rows[0]
				case "joint source rights expansion":
					next4Object(rows[0])["third_party_assets_supports_services_or_independent_authorship_proven"] = true
				}
				var err error
				pins.SourceRaw, err = json.Marshal(selection)
				if err != nil {
					t.Fatal(err)
				}
				pins.SourceSHA = travel3Digest(pins.SourceRaw)
				p["source_selection_sha256"] = pins.SourceSHA
			}
			e := validateConsolidationArtProposal(b, o, p, pins)
			if (change == "valid" && e != nil) || (change != "valid" && e == nil) {
				t.Fatalf("%s: %v", change, e)
			}
		})
	}
}
