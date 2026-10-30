package main

import "testing"

// Public runtime records must keep site provenance and masked reading distinct
// from upstream Git identity, raw full reads and individual copy permission.
func TestSource9FourReadReceiptsKeepUnknownRightsAndFullMaskedRanges(t *testing.T) {
	checkSource9VisibleReceipt(t, "skills-3080-source9-first4-visible-evaluation175645.json", "645df498826e6b38814f2e30ea322097400121df7b22b19b9ae299718c3c670f", map[string]string{
		"site:find-skills":              "b77341cc0f76616e1a61bf336a4de87740b148ec6bd0e48b5d493761260711c6",
		"site:rakuten-seo":              "614be4659e921758fa10a5ea4e5ecd6efb647b1daac22469d22d4e579e493d60",
		"site:amazon-seo-jp":            "95fb3e7844645879efdaeaf6dedf18cbc475259d3f52600a4c556f2bd5a7dea7",
		"site:rakuten-bulk-control-csv": "0145904bc61a1fc5a4f3a903dd30b051a847be8d1f2648d36837fb7f522cccb2",
	}, 939, 873, 66)
}
func checkSource9VisibleReceipt(t *testing.T, file, expectedSHA string, want map[string]string, wantPhysical, wantVisible, wantWithheld int) {
	t.Helper()
	requireAssessmentInputRegistered(t, file, expectedSHA, len(want))
	var v map[string]any
	raw := loadPublicReceipt(t, file, &v)
	if travel3Digest(raw) != expectedSHA || v["site_total"] != float64(10290) || v["site_saved_body_occurrences"] != float64(9862) || v["site_held_body_occurrences"] != float64(428) || v["whole16_complete"] != false {
		t.Fatal("fixed site scope changed")
	}
	for _, key := range []string{"new_raw_reads", "new_permissions", "new_final_dispositions", "central_operations"} {
		if v[key] != float64(0) {
			t.Fatal("visible evaluation gained raw reads or authority")
		}
	}
	entries := received27Array(t, v["entries"])
	if len(entries) != len(want) {
		t.Fatal("site assessments dropped or duplicated")
	}
	physical, visible, withheld := 0, 0, 0
	for _, item := range entries {
		e := received27Object(t, item)
		id, _ := e["inventory_id"].(string)
		sha, ok := want[id]
		if !ok || e["source_sha256"] != sha || e["source_repo"] != nil || e["source_commit"] != "" || e["upstream_commit_verified"] != false || e["path"] != "site/objects/"+sha+".gz" || e["path_kind"] != "fixed_saved_archive_relative_path_not_upstream_path" || e["actual_assessment_reader"] != "20261027-175645-todo-3080" || e["read_kind"] != "complete_visible_masked_workflow_read" {
			t.Fatal("saved site identity was lost or promoted to upstream proof")
		}
		delete(want, id)
		for _, key := range []string{"upstream_publisher_and_version_verified", "individual_copy_permission_accepted", "final_disposition_recorded"} {
			if e[key] != false {
				t.Fatal("unknown source context became accepted")
			}
		}
		for _, key := range []string{"new_raw_credit", "source_script_or_network_execution", "new_adoption_or_central_operations"} {
			if e[key] != float64(0) {
				t.Fatal("source evaluation gained execution or installation")
			}
		}
		next, nvisible, nwithheld := 1, 0, 0
		for _, r := range received27Array(t, e["contiguous_read_ranges"]) {
			rangeValue := received27Object(t, r)
			first, through := int(rangeValue["first"].(float64)), int(rangeValue["through"].(float64))
			masked := received27Array(t, rangeValue["withheld_lines"])
			if first != next || through < first || through-first+1 > 180 || rangeValue["visible_lines"] != float64(through-first+1-len(masked)) {
				t.Fatal("visible range has a gap, overlap or false full-read count")
			}
			next = through + 1
			nvisible += through - first + 1 - len(masked)
			nwithheld += len(masked)
		}
		if e["total_physical_lines"] != float64(next-1) || e["visible_lines"] != float64(nvisible) || len(received27Array(t, e["withheld_physical_lines"])) != nwithheld {
			t.Fatal("full visible workflow differs from its ranges")
		}
		physical, visible, withheld = physical+next-1, visible+nvisible, withheld+nwithheld
	}
	if len(want) != 0 || physical != wantPhysical || visible != wantVisible || withheld != wantWithheld {
		t.Fatal("site range or unknown-line totals changed")
	}
}
