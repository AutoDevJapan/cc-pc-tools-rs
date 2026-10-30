package main

import "testing"

// These are provenance checks against the original received packets, not
// evidence of a new source-body read or of an individual licence grant.
func TestOriginal3719Next12RetainsWholePacketsAndInventoryDelta(t *testing.T) {
	checkOriginal3719PendingSnapshot(t, "skills-3080-original3719-next12-228239-evaluation175645.json", "c23a189ea75961f03bf43d492f2a872c3711a9a188913536feaea7f78b51e475", 228, 239, 3, "20261028-171959-todo-3719", 123, 39, 84)
	var snapshot map[string]any
	loadPublicReceipt(t, "skills-3080-original3719-next12-228239-evaluation175645.json", &snapshot)
	contexts := received27Object(t, snapshot["shared_original_contexts"])
	var delta map[string]any
	for _, value := range contexts {
		common := received27Object(t, received27Object(t, value)["common"])
		if value, exists := common["support_inventory_delta"]; exists {
			if delta != nil {
				t.Fatal("inventory delta duplicated")
			}
			delta = received27Object(t, value)
		}
	}
	if delta == nil || delta["base_row"] != float64(215) || delta["base_count"] != float64(100) || delta["final_count"] != float64(102) || delta["not_a_support_body_read"] != true || delta["default_inventory_sha256"] != "924af88c22265065e76b80a02139783f6615c2e7b6dc085d167f5d3178c52582" {
		t.Fatal("inherited inventory metadata lost or promoted to a body read")
	}
	if len(received27Array(t, delta["added"])) != 2 || len(received27Array(t, delta["removed_paths"])) != 0 || len(received27Array(t, delta["replacements"])) != 17 {
		t.Fatal("original inventory delta dropped or changed")
	}
}

func TestPrimaryflowNext5RetainsWholePacketsAndPendingRights(t *testing.T) {
	checkPrimaryflowOriginalMeaning(t, "skills-3080-primaryflow-next5-evaluation175645.json", "59e231295f75004536f33de7d45a3369b3c9b55e5730ac11ab3fb99bf6927f19", 5, 3, 5)
}
