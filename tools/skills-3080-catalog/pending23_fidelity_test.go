package main

import (
	"encoding/json"
	"io"
	"os"
	"reflect"
	"testing"
)

func TestOriginal3719Next6RetainsWholePacketsAndUnknownSupports(t *testing.T) {
	checkOriginal3719PendingSnapshot(t, "skills-3080-original3719-next6-240245-evaluation175645.json", "c9face778d8c89d4832323ff6dabd508291a905b361c2788b70133460795603e", 240, 245, 4, "20261028-171959-todo-3719", 48, 18, 30)
}

func TestPrimaryflowNext15RetainsWholePacketsAndEachMemberRights(t *testing.T) {
	checkPrimaryflowOriginalMeaning(t, "skills-3080-primaryflow-next15-evaluation175645.json", "e0d0d65c02bafd1157ebb03873f7f95dc3b2831cae787824338bbcb63023218b", 15, 6, 17)
}

func TestSource9Next7And8KeepUnknownRightsAndEveryVisibleRange(t *testing.T) {
	checkSource9VisibleReceipt(t, "skills-3080-source9-next2-7to8-visible-evaluation175645.json", "d5e2d1972f51d44fbd8b9ed32e50ec17a60f80fb13776de480c15ba357a4bcb3", map[string]string{
		"site:amazon-bullet-attribute-benefit":        "09ea36fe3eb263040fa8c27c42427fe57a77f23aa4f9663f08002ad9e6b936d4",
		"site:amazon-catalog-conflict-ticket-builder": "5ca54cb23781881486dff1260e3981a9ee2ebae52dfc7331504ebd7c3a788310",
	}, 472, 434, 38)
	var v map[string]any
	loadPublicReceipt(t, "skills-3080-source9-next2-7to8-visible-evaluation175645.json", &v)
	common := received27Object(t, v["original_parent_common"])
	if len(common) != 21 || received27Digest(t, common) != "d513354ae935f39626a7db1c30e0199951e3846f467ed9a2519a21c222943c4c" || v["original_parent_meaning_sha256"] != "732fd773bca6e531ee66bc3fbe5b8c0349391ba27636b9b6e53eefad6aa98be3" {
		t.Fatal("original parent common context omitted or rewritten")
	}
	whole := map[string]any{}
	for k, value := range common {
		whole[k] = value
	}
	originals := []any{}
	for _, value := range received27Array(t, v["entries"]) {
		row := received27Object(t, value)
		original := received27Object(t, row["original_parent_meaning"])
		if original["inventory_id"] != row["inventory_id"] || original["source_sha256"] != row["source_sha256"] || !reflect.DeepEqual(original["findings"], row["findings"]) {
			t.Fatal("source meaning projection changed")
		}
		originals = append(originals, original)
	}
	whole["entries"] = originals
	if received27Digest(t, whole) != "16384082e0bb04fc4a118897ea5af16ada4281878391ccfad51aee3086c19fff" {
		t.Fatal("whole independent parent source no longer reconstructs")
	}
}

// Local actual-byte comparison is explicit. CI without the two owned snapshots
// verifies the public packets above and skips this separate private-byte check.
func TestPublishedActualPrimary175RetentionIn190(t *testing.T) {
	priorPath, currentPath := os.Getenv("SKILLS3080_NEXT_PRIMARY_PRIOR_SNAPSHOT"), os.Getenv("SKILLS3080_NEXT_PRIMARY_CURRENT_SNAPSHOT")
	if priorPath == "" && currentPath == "" {
		t.Skip("Canonical worker supplies the pinned prior175/current190 bytes; no CI read is claimed.")
	}
	if priorPath == "" || currentPath == "" {
		t.Fatal("both explicit next primary snapshots required")
	}
	var published map[string]any
	loadPublicReceipt(t, "skills-3080-primaryflow-next15-evaluation175645.json", &published)
	read := func(path string, want any) []map[string]any {
		t.Helper()
		sha, ok := want.(string)
		if !ok || len(sha) != 64 {
			t.Fatal("independent published snapshot SHA absent")
		}
		f, e := os.Open(path)
		if e != nil {
			t.Fatal(e)
		}
		defer f.Close()
		st, e := f.Stat()
		if e != nil || !st.Mode().IsRegular() || st.Size() <= 0 || st.Size() > 4<<20 {
			t.Fatal("bounded regular primary snapshot required")
		}
		raw, e := io.ReadAll(io.LimitReader(f, (4<<20)+1))
		if e != nil || travel3Digest(raw) != sha {
			t.Fatal("actual snapshot differs from its independent pin")
		}
		var v struct {
			Records []map[string]any `json:"records"`
		}
		if e = json.Unmarshal(raw, &v); e != nil {
			t.Fatal(e)
		}
		return v.Records
	}
	prior, current := read(priorPath, published["prior_prepared175_raw_sha256"]), read(currentPath, published["source_full190_snapshot_sha256"])
	added := []map[string]any{}
	for _, value := range received27Array(t, published["records"]) {
		added = append(added, received27Object(t, value))
	}
	if len(prior) != 175 || len(current) != 190 || len(added) != 15 {
		t.Fatal("exact prior175/current190/new15 required")
	}
	if e := primaryRecordsRetained(prior, current, added); e != nil {
		t.Fatal(e)
	}
}
