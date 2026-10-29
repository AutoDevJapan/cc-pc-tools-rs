package main

import (
	"encoding/json"
	"io"
	"os"
	"reflect"
	"testing"
)

const primaryNext16File = "skills-3080-primaryflow-next16-after195-evaluation175645.json"

func TestOriginal3719Next6RetainsWholeActorSupportAndUnknownStates(t *testing.T) {
	checkOriginal3719PendingSnapshot(t, "skills-3080-original3719-next6-250255-evaluation175645.json", "831104060ccb98ddd92a16aaf92011b6dff2b8fda05b28c7e35868d145a03167", 250, 255, 6, "20261028-171959-todo-3719", 163, 152, 11)
}
func TestPrimaryNext16RetainsWholeOriginalPacketsAndMemberLimits(t *testing.T) {
	checkPrimaryflowOriginalMeaningWithCorrection(t, primaryNext16File, "fc5168d8c3d1709b44d6d1dc4850815bf776c281ee6604e742c960a6c4b1c16f", 16, 7, 17, false)
	var pub map[string]any
	loadPublicReceipt(t, primaryNext16File, &pub)
	contexts := received27Object(t, pub["shared_original_contexts"])
	mixed, unknown := 0, 0
	for _, value := range contexts {
		c := received27Object(t, value)
		common := received27Object(t, c["common"])
		meaning := received27Object(t, c["independent_parent_meaning"])
		// This pin hashes the original private file bytes, not a new canonical
		// serialization of its object. Full snapshot retention below compares
		// the original objects; the independently fixed public pin binds this copy.
		meaningPin, _ := c["independent_parent_meaning_sha256"].(string)
		if !validHex(meaningPin, 64) || meaning["all_entry_meanings_and_limits_read"] != true || meaning["packet_sha256"] != c["independent_parent_ack"].(map[string]any)["packet_sha256"] || meaning["member_context_rights_support_and_dispositions_pending"] != true {
			t.Fatal("independent parent meaning or limits changed")
		}
		if common["packet_id"] == "aa8c4ec095b94ec8a1d3ea8ce5ef724d" {
			mixed++
			p := received27Object(t, c["mixed_reader_range_provenance"])
			if c["packet_publisher_not_relabelled_as_all_range_reader"] != true || c["mixed_reader_range_provenance_sha256"] != "d4482793e7fc4de7b10bec5fdfcbf68dcb77db5250c2fb33ab1e5495b878acfd" || p["all_four_actual_view_ranges_correlated"] != true || p["old_reader_initial_1to180_not_relabelled_as_current"] != true {
				t.Fatal("mixed actual readers lost")
			}
			rows := received27Array(t, p["proofs"])
			if len(rows) != 4 {
				t.Fatal("four actual ranges required")
			}
			for i, value := range rows {
				r := received27Object(t, value)
				old := i < 2
				reader, call, first, last := "20261029-015516-todo-4134", "call_4b4afbcb0d86437596c7986c69e1738f", 1, 180
				if !old {
					reader = "20261029-063706-todo-4134"
					call = "call_0eb86d247e024300b0fdd0f5056fffe0"
					first = 181
					if i == 2 {
						last = 196
					} else {
						last = 190
					}
				}
				if e := next4Fields(r, map[string]any{"ordinal": float64(88 + i%2), "reader_job": reader, "actual_tool_call_id": call, "first": float64(first), "through": float64(last), "source_archive_reopened_by_parent": false}); e != nil {
					t.Fatal(e)
				}
				for _, key := range []string{"range_sha256", "source_sha256", "whole_masked_view_sha256", "tool_input_sha256", "native_tool_output_representation_sha256"} {
					s, _ := r[key].(string)
					if !validHex(s, 64) {
						t.Fatal("actual native range pin absent")
					}
				}
			}
		}
		if common["packet_id"] == "d38b652b8b1a4ac09ab68bcb2b87775b" {
			unknown++
			v, present := meaning["failed_prior_view_calls"]
			if !present || v != nil || meaning["reader_job"] != "20261029-081529-todo-4134" || meaning["view_calls"] != float64(2) {
				t.Fatal("unknown failed views were zeroed or old unconfirmed reads gained credit")
			}
		}
	}
	if mixed != 1 || unknown != 1 {
		t.Fatal("required mixed reader or unknown failure context omitted")
	}
}

func TestInheritedSupportDoesNotPromoteUnknownContentToReads(t *testing.T) {
	for _, change := range []string{"valid", "reader", "mask", "state", "count", "index", "flag", "other flag", "trailing value", "trailing unknown"} {
		t.Run(change, func(t *testing.T) {
			state := []any{float64(1), nil, nil, false, false, float64(0)}
			v := map[string]any{"count": float64(116), "saved_in_index": float64(116), "default_current_saved_read": state}
			switch change {
			case "reader":
				state[1] = "reader"
			case "mask":
				state[2] = float64(0)
			case "state":
				state[0] = float64(2)
			case "count":
				v["count"] = float64(2)
			case "index":
				v["saved_in_index"] = float64(115)
			case "flag":
				state[3] = true
			case "other flag":
				state[4] = true
			case "trailing value":
				state[5] = float64(1)
			case "trailing unknown":
				state[5] = nil
			}
			n, e := original3719InheritedUnread(v, 3)
			if (change == "valid" && (e != nil || n != 113)) || (change != "valid" && e == nil) {
				t.Fatalf("%s: count=%d err=%v", change, n, e)
			}
		})
	}
}

func TestPublishedActualPrimary195RetentionIn211(t *testing.T) {
	priorPath, currentPath := os.Getenv("SKILLS3080_RECOVERY_PRIMARY_PRIOR_SNAPSHOT"), os.Getenv("SKILLS3080_RECOVERY_PRIMARY_CURRENT_SNAPSHOT")
	if priorPath == "" && currentPath == "" {
		t.Skip("Canonical worker supplies pinned prior195/current211 bytes; CI does not claim their private-byte comparison.")
	}
	if priorPath == "" || currentPath == "" {
		t.Fatal("both explicit recovery snapshots required")
	}
	var pub map[string]any
	loadPublicReceipt(t, primaryNext16File, &pub)
	read := func(path string, want any) map[string]any {
		t.Helper()
		sha, ok := want.(string)
		if !ok || !validHex(sha, 64) {
			t.Fatal("independent snapshot SHA absent")
		}
		f, e := os.Open(path)
		if e != nil {
			t.Fatal(e)
		}
		defer f.Close()
		st, e := f.Stat()
		if e != nil || !st.Mode().IsRegular() || st.Size() <= 0 || st.Size() > 4<<20 {
			t.Fatal("bounded regular snapshot required")
		}
		b, e := io.ReadAll(io.LimitReader(f, (4<<20)+1))
		if e != nil || travel3Digest(b) != sha {
			t.Fatal("actual snapshot differs from independent published pin")
		}
		var value map[string]any
		if json.Unmarshal(b, &value) != nil {
			t.Fatal("actual snapshot invalid")
		}
		return value
	}
	prior := read(priorPath, received27Object(t, pub["prior_snapshot"])["sha256"])
	current := read(currentPath, received27Object(t, pub["current_snapshot"])["sha256"])
	rows := func(m map[string]any) []map[string]any {
		out := []map[string]any{}
		for _, value := range received27Array(t, m["records"]) {
			out = append(out, received27Object(t, value))
		}
		return out
	}
	p, c, added := rows(prior), rows(current), rows(pub)
	if len(p) != 195 || len(c) != 211 || len(added) != 16 {
		t.Fatal("exact195/211/16 records required")
	}
	if e := primaryRecordsRetained(p, c, added); e != nil {
		t.Fatal(e)
	}
	pctx, cctx, delta := received27Object(t, prior["shared_original_contexts"]), received27Object(t, current["shared_original_contexts"]), received27Object(t, pub["shared_original_contexts"])
	if len(pctx) != 55 || len(cctx) != 62 || len(delta) != 7 {
		t.Fatal("exact55/62/7 whole contexts required")
	}
	for key, value := range pctx {
		if !reflect.DeepEqual(value, cctx[key]) {
			t.Fatal("prior context rewritten")
		}
	}
	for key, value := range delta {
		if pctx[key] != nil || !reflect.DeepEqual(value, cctx[key]) {
			t.Fatal("delta not exact new whole contexts")
		}
	}
}
