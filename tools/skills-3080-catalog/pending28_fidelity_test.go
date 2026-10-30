package main

import (
	"reflect"
	"testing"
)

func TestOriginal3719Next18RetainsThreeWholePacketsAndNulls(t *testing.T) {
	checkOriginal3719PendingSnapshot(t, "skills-3080-original3719-next18-210227-evaluation175645.json", "729615f614ed7164363b412a6b0d1ff2ac52ea7b874114c3e7094b4490d2bbe5", 210, 227, 3, "20261028-171959-todo-3719", 238, 63, 175)
}
func TestPrimaryflowNext8RetainsWholePacketsAndMemberRights(t *testing.T) {
	checkPrimaryflowOriginalMeaning(t, "skills-3080-primaryflow-next8-evaluation175645.json", "fa8670f53f66d72b1f4d9850e06241f583e44057a05037bdd296c55e5910aa7f", 8, 3, 8)
}
func TestSource9Next2RetainsUnknownUpstreamAndAllMaskedRanges(t *testing.T) {
	checkSource9VisibleReceipt(t, "skills-3080-source9-next2-visible-evaluation175645.json", "8f9f22d4273a317acf5026dcfa791c9ad34695ad9fd84a544218d6e8545bb86a", map[string]string{
		"site:amazon-a-plus-content-brief": "14e4307b089d11e9187d39f7ff15aad14047a596223971492222d668425bc3b4",
		"site:amazon-brand-store-planner":  "7ff3654cbee657b2d0c98d324eb22f07f6f74c8e88e6087573a9c3795d49e880",
	}, 488, 448, 40)
}

func TestSource9Next2PreservesWholeOriginalParentMeaning(t *testing.T) {
	var v map[string]any
	loadPublicReceipt(t, "skills-3080-source9-next2-visible-evaluation175645.json", &v)
	if v["all_original_parent_fields_losslessly_preserved"] != true {
		t.Fatal("original parent context absent")
	}
	common := received27Object(t, v["original_parent_common"])
	whole := map[string]any{}
	for key, value := range common {
		whole[key] = value
	}
	originals := []any{}
	for _, item := range received27Array(t, v["entries"]) {
		entry := received27Object(t, item)
		original := received27Object(t, entry["original_parent_meaning"])
		if original["inventory_id"] != entry["inventory_id"] || original["source_sha256"] != entry["source_sha256"] || !reflect.DeepEqual(original["findings"], entry["findings"]) {
			t.Fatal("original source meaning changed")
		}
		originals = append(originals, original)
	}
	whole["entries"] = originals
	if received27Digest(t, whole) != v["original_parent_canonical_sha256"] {
		t.Fatal("complete original parent common/entry context lost")
	}
}
