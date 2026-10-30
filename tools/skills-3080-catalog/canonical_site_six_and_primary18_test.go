package main

import (
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

const canonicalSiteSixFile = "skills-3080-openai6-site-integrations095653.json"
const nextPrimary18File = "skills-3080-primaryflow-next18-after230-evaluation095653.json"
const nextPrimary18SHA = "02775423e57ab88a02f49158b96f50b12d8e59ca31d94014b064e7a0e2a0bba5"
const canonicalSiteSixScope = "Same site canonical URL and saved primary bytes resolved to the already accepted fixed upstream workflow and its own individual notice. Reuse its existing Japanese adaptation. No independent site authorship, notice for site editorial text, package/assets/templates/scripts/service rights or source re-read is claimed."

var canonicalSiteSixTargets = map[string]string{
	"site:security-threat-model-7dae2b": "security-threat-model",
	"site:aspnet-core":                  "aspnet-core", "site:winui-app": "winui-app",
	"site:chatgpt-apps": "chatgpt-apps", "site:security-ownership-map": "security-ownership-map",
	"site:linear-5b7724": "linear",
}

func validateCanonicalSiteSix(v, manifest, ledger map[string]any) error {
	if v["current_instructions_files"] != float64(12) || v["current_instruction_bytes"] != float64(21416) || v["final_dispositions_recorded"] != float64(6) {
		return fmt.Errorf("actual current instruction and source-occurrence units changed")
	}
	if err := next4Fields(v, map[string]any{"todo": float64(3080), "publisher_job": "20261030-095653-todo-3080", "aliases": float64(6), "new_integrations": float64(6), "new_Skills": float64(0), "new_central_writes": float64(0), "new_permissions": float64(0), "creation_manifest_sha256": "af55ac1965aa0ba6796898f23c9e231d045b88f3eff9424314ed57bdcaa8b8b5", "frozen_creator_review_task": float64(4049), "frozen_creator_not_modified": true, "site_git_tuple_not_filled_from_upstream": true, "general_access_granted": false, "historical_continuity_proven": false, "current_OS_writer": "unproved", "whole16_complete": false}); err != nil {
		return err
	}
	if err := next4Fields(next4Object(v["fresh_discovery"]), map[string]any{"requested": float64(6), "found": float64(6), "enabled": float64(6), "parse_errors": float64(0), "total_metadata": float64(484), "scan_limit_warnings": float64(2), "model_turn_or_context_budget_proof": false}); err != nil {
		return err
	}
	entries, ok := v["entries"].([]any)
	if !ok || len(entries) != 6 {
		return fmt.Errorf("six canonical site occurrences required")
	}
	adopted := map[string]map[string]any{}
	for _, value := range ledger["entries"].([]any) {
		row := next4Object(value)
		adopted[row["inventory_id"].(string)] = row
	}
	seen := map[string]bool{}
	for _, value := range entries {
		e := next4Object(value)
		id, _ := e["inventory_id"].(string)
		target := canonicalSiteSixTargets[id]
		if target == "" || seen[id] || e["target"] != target || e["disposition"] != "integrated_existing" {
			return fmt.Errorf("canonical occurrence repeated, changed or unintegrated")
		}
		seen[id] = true
		site, up := next4Object(e["site_occurrence"]), next4Object(e["accepted_upstream_occurrence"])
		path := "skills/.curated/" + target + "/SKILL.md"
		if err := next4Fields(site, map[string]any{"inventory_id": id, "inventory_unit": "site_index_entry", "repo": "", "commit": "", "path": "", "machine_body_read": true}); err != nil {
			return err
		}
		if err := next4Fields(e, map[string]any{"source_repo": "", "source_commit": "", "path": "", "new_permissions": float64(0), "new_source_literal_reads": float64(0), "new_central_writes": float64(0), "new_Skill_creations": float64(0)}); err != nil {
			return err
		}
		if err := next4Fields(up, map[string]any{"repo": "openai/skills", "commit": "49f948faa9258a0c61caceaf225e179651397431", "path": path}); err != nil {
			return err
		}
		url := "https://raw.githubusercontent.com/openai/skills/49f948faa9258a0c61caceaf225e179651397431/" + path
		if site["normalized_canonical_url"] != url || up["normalized_canonical_url"] != url || site["sha256"] != up["sha256"] || e["source_sha256"] != up["sha256"] {
			return fmt.Errorf("same fixed canonical URL and primary bytes required")
		}
		upID, _ := e["upstream_adoption_inventory_id"].(string)
		a := adopted[upID]
		if a == nil || up["inventory_id"] != upID || a["target"] != target || a["disposition"] != "installed_new_adaptation" || a["source_repo"] != up["repo"] || a["source_commit"] != up["commit"] || a["source_path"] != up["path"] || a["source_sha256"] != up["sha256"] || a["source_notice_preserved"] != true {
			return fmt.Errorf("site occurrence not linked to its accepted upstream adaptation")
		}
		var source map[string]any
		for _, value := range manifest["sources"].([]any) {
			s := next4Object(value)
			if s["path"] == path {
				source = s
			}
		}
		if source == nil || source["repository"] != up["repo"] || source["commit"] != up["commit"] || source["sha256"] != up["sha256"] {
			return fmt.Errorf("fixed creation source mismatch")
		}
		wantNotice := next4Object(source["license"])
		notice := next4Object(e["individual_upstream_notice"])
		for _, key := range []string{"path", "sha256", "scope", "accepted"} {
			if notice[key] != wantNotice[key] {
				return fmt.Errorf("individual notice changed: %s", key)
			}
		}
		if notice["path"] != "skills/.curated/"+target+"/LICENSE.txt" || notice["accepted"] != true || !strings.Contains(notice["scope"].(string), "no supporting scripts/templates") {
			return fmt.Errorf("individual primary-only permission boundary changed")
		}
		preserved, ok := e["preserved_source_and_notice"].([]any)
		if !ok || len(preserved) != 2 {
			return fmt.Errorf("preserved source and own notice required")
		}
		preservedPaths := map[string]bool{}
		for _, value := range preserved {
			p := next4Object(value)
			rel, _ := p["path"].(string)
			if preservedPaths[rel] {
				return fmt.Errorf("preserved source cannot replace its individual notice")
			}
			preservedPaths[rel] = true
			want := up["sha256"]
			if rel == "references/LICENSE.txt" {
				want = notice["sha256"]
			} else if rel != "references/original-skill.txt" {
				return fmt.Errorf("unknown preserved file")
			}
			if p["sha256"] != want || p["semantic_literal_reread_by_current_parent"] != false {
				return fmt.Errorf("preserved scope/hash or current source read changed")
			}
			var original map[string]any
			for _, value := range manifest["files"].([]any) {
				candidate := next4Object(value)
				if candidate["target"] == target+"/"+rel {
					original = candidate
				}
			}
			if original == nil {
				return fmt.Errorf("preserved primary/notice missing from fixed manifest")
			}
			raw, err := base64.StdEncoding.DecodeString(original["content_base64"].(string))
			if err != nil {
				return err
			}
			blob := sha1.Sum(append([]byte(fmt.Sprintf("blob %d\x00", len(raw))), raw...))
			if travel3Digest(raw) != want || original["sha256"] != want || p["bytes"] != float64(len(raw)) || p["git_blob_from_preserved_fixed_buffer"] != fmt.Sprintf("%x", blob) {
				return fmt.Errorf("preserved original/notice bytes or Git blob no longer match fixed source")
			}
		}
		instructions, ok := e["current_instruction_files"].([]any)
		if !ok || len(instructions) != 2 {
			return fmt.Errorf("both current instruction files required")
		}
		paths := map[string]bool{}
		for _, value := range instructions {
			f := next4Object(value)
			rel, _ := f["path"].(string)
			if (rel != "SKILL.md" && rel != "references/guide.md") || paths[rel] {
				return fmt.Errorf("current instruction membership changed")
			}
			paths[rel] = true
			var original map[string]any
			for _, value := range manifest["files"].([]any) {
				candidate := next4Object(value)
				if candidate["target"] == target+"/"+rel {
					original = candidate
				}
			}
			if original == nil {
				return fmt.Errorf("instruction not in fixed original manifest")
			}
			raw, err := base64.StdEncoding.DecodeString(original["content_base64"].(string))
			if err != nil {
				return err
			}
			text, ok := f["content_utf8"].(string)
			if !ok || text != string(raw) || travel3Digest(raw) != f["sha256"] || f["sha256"] != original["sha256"] || f["bytes"] != float64(len(raw)) || f["same_normal_current_buffer_fully_read"] != true {
				return fmt.Errorf("current qualified instruction bytes changed")
			}
		}
		if e["target_entrypoint_sha256"] != a["entrypoint_sha256"] || e["target_manifest"] != a["manifest"] || e["target_evidence"] != a["evidence"] {
			return fmt.Errorf("existing target acceptance changed")
		}
		final := adopted[id]
		if final == nil || final["target"] != target || final["disposition"] != "integrated_existing" || final["evidence"] != canonicalSiteSixFile || final["source_sha256"] != site["sha256"] || final["source_repo"] != "" || final["source_commit"] != "" || final["source_path"] != "" || final["entrypoint_sha256"] != a["entrypoint_sha256"] || final["individual_notice_sha256"] != notice["sha256"] {
			return fmt.Errorf("final ledger no longer matches actual site and individual upstream notice")
		}
		if e["scope"] != canonicalSiteSixScope || final["selected_source_scope"] != canonicalSiteSixScope || final["resolved_upstream_inventory_id"] != upID || final["resolved_upstream_repo"] != up["repo"] || final["resolved_upstream_commit"] != up["commit"] || final["resolved_upstream_path"] != up["path"] || final["canonical_URL"] != url || final["individual_notice_path"] != notice["path"] || final["manifest"] != a["manifest"] || final["source_notice_preserved"] != true || final["upstream_original_preserved"] != true || final["copy_grant_extended_to_other_material"] != false || final["new_central_writes"] != float64(0) {
			return fmt.Errorf("resolved upstream provenance, preserved notice or limited ledger scope changed")
		}
	}
	return nil
}

func TestCanonicalSiteSixPreservesUnknownSiteGitAndEachAcceptedNotice(t *testing.T) {
	var manifest, ledger map[string]any
	if travel3Digest(loadPublicReceipt(t, "skills-3080-operations/create-openai6-214717.json", &manifest)) != "af55ac1965aa0ba6796898f23c9e231d045b88f3eff9424314ed57bdcaa8b8b5" {
		t.Fatal("independent frozen manifest changed")
	}
	loadPublicReceipt(t, "skills-3080-adoptions.json", &ledger)
	for _, change := range []string{"valid", "site repo filled", "site URL changed", "notice transferred", "target changed", "instruction changed", "current raw invented", "scan warning erased", "permission extended", "ledger notice changed", "duplicate preserved source", "wrong preserved bytes", "wrong resolved source", "wrong ledger scope"} {
		t.Run(change, func(t *testing.T) {
			var v map[string]any
			loadPublicReceipt(t, canonicalSiteSixFile, &v)
			e := next4Object(v["entries"].([]any)[0])
			copyBytes, _ := json.Marshal(ledger)
			var currentLedger map[string]any
			json.Unmarshal(copyBytes, &currentLedger)
			switch change {
			case "site repo filled":
				next4Object(e["site_occurrence"])["repo"] = "openai/skills"
			case "site URL changed":
				next4Object(e["site_occurrence"])["normalized_canonical_url"] = "https://example.invalid/same-bytes"
			case "notice transferred":
				next4Object(e["individual_upstream_notice"])["path"] = "LICENSE.txt"
			case "target changed":
				e["target"] = "another-target"
			case "instruction changed":
				next4Object(e["current_instruction_files"].([]any)[0])["content_utf8"] = "different current instruction"
			case "current raw invented":
				e["new_source_literal_reads"] = float64(1)
			case "scan warning erased":
				next4Object(v["fresh_discovery"])["scan_limit_warnings"] = float64(0)
			case "permission extended":
				next4Object(e["individual_upstream_notice"])["scope"] = "all files and third-party services"
			case "ledger notice changed":
				for _, value := range currentLedger["entries"].([]any) {
					row := next4Object(value)
					if row["inventory_id"] == e["inventory_id"] {
						row["individual_notice_sha256"] = strings.Repeat("0", 64)
					}
				}
			case "duplicate preserved source":
				files := e["preserved_source_and_notice"].([]any)
				files[1] = files[0]
			case "wrong preserved bytes":
				next4Object(e["preserved_source_and_notice"].([]any)[0])["bytes"] = float64(1)
			case "wrong resolved source", "wrong ledger scope":
				for _, value := range currentLedger["entries"].([]any) {
					row := next4Object(value)
					if row["inventory_id"] == e["inventory_id"] {
						if change == "wrong resolved source" {
							row["resolved_upstream_repo"] = "different/source"
						} else {
							row["selected_source_scope"] = "all sources and services"
						}
					}
				}
			}
			err := validateCanonicalSiteSix(v, manifest, currentLedger)
			if (err == nil) != (change == "valid") {
				t.Fatalf("%s: %v", change, err)
			}
		})
	}
}

func validateNextPrimary18(v map[string]any) error {
	actors := map[string][5]string{}
	for _, sha := range []string{"d9b2f06db52793e336cbfce404ff65add52a230bb2a921c5d04d9867b96b1634", "5d599c53cd8f7242624810c1a51eb831b64d8adb37cd8417c3411b2ccab0539b", "d7a419697a5cc0eb655f4b9b58b42c292e363432996b5f44245e3ce99e69067b"} {
		actors[sha] = [5]string{"20261030-033405-todo-4134", "20261030-095653-todo-3080", "gpt-6.1-sol", "b0bfc631a8d355a1c406003da7c417b668dabd49dccf48b1d11ffa18a3132b72", ""}
	}
	return validateResumedVisibleSnapshot(v, 18, 0, 18, 3, map[string]any{"records": float64(229), "sha256": "9b34f3cec8b9895826d64cc289a5c41736f75c36e16f87d58834afa38303be0e", "all_records_and67_original_contexts_and_held1_exactly_retained": true}, map[string]any{"records": float64(247), "held_groups": float64(1), "acknowledged_groups": float64(248), "original_packets": float64(70), "sha256": "5f80f085429c44f0c29a60b2bbc0a97bbed35bf2efff6a54af225d3608efaec2"}, actors)
}

func TestNextPrimaryEighteenRetainsOriginalNullParentAndNoOldHeldCredit(t *testing.T) {
	requireAssessmentInputRegistered(t, nextPrimary18File, nextPrimary18SHA, 18)
	for _, change := range []string{"valid", "old held credited", "actor changed", "parent missing", "parent empty", "member changed", "record repo changed", "raw invented"} {
		t.Run(change, func(t *testing.T) {
			var v map[string]any
			if travel3Digest(loadPublicReceipt(t, nextPrimary18File, &v)) != nextPrimary18SHA {
				t.Fatal("fixed18 source changed")
			}
			row := next4Object(v["records"].([]any)[0])
			contexts := next4Object(v["shared_original_contexts"])
			c := next4Object(contexts[row["original_packet_sha256"].(string)])
			switch change {
			case "old held credited":
				v["held_records"] = []any{row}
			case "actor changed":
				row["actual_assessment_reader"] = "20261029-174632-todo-4134"
			case "parent missing":
				delete(next4Object(c["independent_native_metadata"]), "actual_launch_parent")
			case "parent empty":
				next4Object(c["independent_native_metadata"])["actual_launch_parent"] = ""
			case "member changed":
				for _, value := range next4Object(c["member_metadata"]) {
					next4Object(value.([]any)[0])["path"] = "other/SKILL.md"
					break
				}
			case "record repo changed":
				row["source_repo"] = "different/source"
			case "raw invented":
				next4Object(next4Object(row["original_assessment"])["read"])["raw_full"] = true
			}
			err := validateNextPrimary18(v)
			if (err == nil) != (change == "valid") {
				t.Fatalf("%s: %v", change, err)
			}
		})
	}
}
