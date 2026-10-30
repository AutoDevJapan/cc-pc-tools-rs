package main

import "testing"

const canonicalDeployFourFile = "skills-3080-deployment-aliases.json"

var canonicalDeployFourTargets = map[string]string{
	"site:cloudflare-deploy":    "cloudflare-deploy",
	"site:netlify-deploy":       "netlify-deploy",
	"site:render-deploy":        "render-deploy",
	"site:vercel-deploy-110b98": "vercel-deploy",
}

func validateCanonicalDeployFour(v, manifest, ledger map[string]any) error {
	if err := next4Fields(v, map[string]any{
		"todo":    float64(3080),
		"aliases": float64(4), "new_integrations": float64(4), "final_dispositions_recorded": float64(4),
		"current_instructions_files": float64(8), "current_instruction_bytes": float64(13862),
		"new_Skills": float64(0), "new_central_writes": float64(0), "new_permissions": float64(0),
		"creation_manifest_sha256":                "f2ac5a8415c2ad119aeb8fb49d34a07c341d7036e79d65d3bb96e898a3e7e1b4",
		"source_bundle_unchanged":                 true,
		"site_git_tuple_not_filled_from_upstream": true, "general_access_granted": false,
		"historical_continuity_proven": false, "whole16_complete": false,
	}); err != nil {
		return err
	}
	if err := next4Fields(next4Object(v["fresh_discovery"]), map[string]any{
		"requested": float64(4), "found": float64(4), "enabled": float64(4),
		"parse_errors": float64(0), "total_metadata": float64(484), "scan_limit_warnings": float64(2),
		"model_turn_or_context_budget_proof": false,
	}); err != nil {
		return err
	}
	return validateCanonicalSiteEntries(v, manifest, ledger, canonicalDeployFourTargets, canonicalDeployFourFile)
}

func TestCanonicalDeployFourReusesEachExistingSourceAndOwnNotice(t *testing.T) {
	var manifest, ledger map[string]any
	if travel3Digest(loadPublicReceipt(t, "skills-3080-operations/create-openai11-214717.json", &manifest)) != "f2ac5a8415c2ad119aeb8fb49d34a07c341d7036e79d65d3bb96e898a3e7e1b4" {
		t.Fatal("independent frozen manifest changed")
	}
	loadPublicReceipt(t, "skills-3080-adoptions.json", &ledger)
	for _, change := range []string{"valid", "missing entry", "duplicate entry", "transferred notice", "different fixed URL", "invented site Git", "changed current instruction", "central write credited"} {
		t.Run(change, func(t *testing.T) {
			var v map[string]any
			loadPublicReceipt(t, canonicalDeployFourFile, &v)
			entries := v["entries"].([]any)
			e := next4Object(entries[0])
			switch change {
			case "missing entry":
				v["entries"] = entries[:3]
			case "duplicate entry":
				entries[3] = entries[0]
			case "transferred notice":
				e["individual_upstream_notice"] = next4Object(entries[1])["individual_upstream_notice"]
			case "different fixed URL":
				next4Object(e["site_occurrence"])["normalized_canonical_url"] = "https://example.invalid/identical-bytes"
			case "invented site Git":
				next4Object(e["site_occurrence"])["repo"] = "openai/skills"
			case "changed current instruction":
				next4Object(e["current_instruction_files"].([]any)[0])["content_utf8"] = "unverified replacement"
			case "central write credited":
				v["new_central_writes"] = float64(4)
			}
			err := validateCanonicalDeployFour(v, manifest, ledger)
			if (err == nil) != (change == "valid") {
				t.Fatalf("%s: %v", change, err)
			}
		})
	}
}

func TestDeployFourComesFromCompleteEffectiveMetadataJoin(t *testing.T) {
	var proof map[string]any
	if travel3Digest(loadPublicReceipt(t, "skills-3080-source-alias-metadata.json", &proof)) != "edd507e80992df08519fbefdf3c256f253fce807013d44ab5ac65658e3403f3a" {
		t.Fatal("independent complete metadata projection changed")
	}
	if err := next4Fields(proof, map[string]any{"catalog_rows": float64(73220), "accepted_occurrences": float64(179), "matching_unclassified_occurrences": float64(9), "effective_attempt_precedence_matches_current_catalog_reader": true, "source_body_opens": float64(0), "new_permissions": float64(0), "new_final_dispositions": float64(0), "new_central_writes": float64(0), "whole16_complete": false}); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	different := 0
	for _, value := range proof["matches"].([]any) {
		m := next4Object(value)
		source := next4Object(m["unclassified_occurrence"])
		id, _ := source["inventory_id"].(string)
		if !m["same_actual_canonical_URL_and_saved_SHA"].(bool) {
			different++
			continue
		}
		if canonicalDeployFourTargets[id] == "" || seen[id] {
			t.Fatal("unreviewed or duplicate canonical occurrence gained scope")
		}
		seen[id] = true
		matches := m["accepted_matches"].([]any)
		if len(matches) != 1 {
			t.Fatal("one actual accepted upstream required")
		}
		up := next4Object(next4Object(matches[0])["catalog_occurrence"])
		if source["sha256"] != up["sha256"] || source["normalized_canonical_url"] != up["normalized_canonical_url"] || source["repo"] != "" || source["commit"] != "" || source["path"] != "" {
			t.Fatal("actual primary SHA/URL or unknown site tuple differs")
		}
		if next4Object(source["effective_attempt"])["sha256"] != source["sha256"] || source["catalog_original_sha256"] != "" {
			t.Fatal("effective observation was conflated with the original top-level field")
		}
	}
	if len(seen) != 4 || different != 5 {
		t.Fatal("four fixed-source leads and five other-source holds must remain separate")
	}
}
