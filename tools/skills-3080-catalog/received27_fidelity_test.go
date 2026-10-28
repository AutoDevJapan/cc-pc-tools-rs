package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"testing"
)

func received27Object(t *testing.T, v any) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatal("required original object absent")
	}
	return m
}

func received27Array(t *testing.T, v any) []any {
	t.Helper()
	a, ok := v.([]any)
	if !ok {
		t.Fatal("required original array absent")
	}
	return a
}

func received27Digest(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// Reconstruct complete original packets, including unknown/null/privacy and
// support fields. Identity-only execution projections cannot replace them.
func TestReceivedAssessmentPacketsRetainOriginalActorsAndFields(t *testing.T) {
	files := []struct {
		name               string
		rows, sourceFields int
	}{
		{"skills-3080-sixmile-next6-187192-evaluation175645.json", 6, 5},
		{"skills-3080-sixmile-next2-193194-evaluation175645.json", 2, 5},
		{"skills-3080-sixmile-next2-195196-evaluation175645.json", 2, 5},
		{"skills-3080-other16-288303-evaluation175645.json", 16, 7},
		{"skills-3080-other1-304304-evaluation175645.json", 1, 7},
		{"skills-3080-other8-305312-evaluation175645.json", 8, 7},
		{"skills-3080-sixmile-next2-197198-evaluation175645.json", 2, 5},
		{"skills-3080-other8-313320-evaluation175645.json", 8, 7},
		{"skills-3080-other8-321328-evaluation175645.json", 8, 7},
		{"skills-3080-other5-329333-evaluation175645.json", 5, 7},
		{"skills-3080-other5-334338-evaluation175645.json", 5, 7},
		{"skills-3080-other7-339345-evaluation175645.json", 7, 7},
		{"skills-3080-other7-346352-evaluation175645.json", 7, 7},
		{"skills-3080-other7-353359-evaluation175645.json", 7, 7},
		{"skills-3080-other7-360366-evaluation175645.json", 7, 7},
		{"skills-3080-other7-367373-evaluation175645.json", 7, 7},
	}
	seen := map[string]bool{}
	contexts := 0
	for _, file := range files {
		t.Run(file.name, func(t *testing.T) {
			var outer map[string]any
			loadPublicReceipt(t, file.name, &outer)
			if outer["all_original_selected_chunks_losslessly_reconstructed"] != true {
				t.Fatal("original packet fidelity not recorded")
			}
			pub := received27Object(t, outer["publication_preparation"])
			if pub["publisher_job"] != "20261027-175645-todo-3080" {
				t.Fatal("publisher became the original reader")
			}
			for _, key := range []string{"new_source_body_reads", "new_global_raw_or_workflow_credit", "new_permissions", "new_final_dispositions", "central_operations"} {
				if n, ok := pub[key].(float64); !ok || n != 0 {
					t.Fatalf("publication gained %s", key)
				}
			}
			entries := received27Array(t, outer["entries"])
			if len(entries) != file.rows {
				t.Fatal("assessment identity projection count differs")
			}
			var records []any
			if file.sourceFields == 5 {
				records = entries
			} else {
				records = received27Array(t, outer["records"])
				if len(records) != len(entries) {
					t.Fatal("original records lost")
				}
			}
			grouped := map[string][]any{}
			for i, value := range records {
				r := received27Object(t, value)
				projection := received27Object(t, entries[i])
				id, ok := r["inventory_id"].(string)
				if !ok || id == "" || seen[id] || projection["inventory_id"] != id {
					t.Fatal("missing/repeated/replaced primary identity")
				}
				seen[id] = true
				key, reader := "original_assessment", r["actual_assessment_reader"]
				if file.sourceFields == 5 {
					key, reader = "original_reader_assessment", r["assessment_writer_job"]
				}
				original := received27Object(t, r[key])
				source := received27Array(t, original["source"])
				if len(source) != file.sourceFields || source[0] != id {
					t.Fatal("original source tuple lost")
				}
				pathAt, shaAt := 1, 2
				if file.sourceFields == 7 {
					pathAt, shaAt = 3, 4
				}
				if source[pathAt] != r["path"] || source[shaAt] != r["source_sha256"] || projection["path"] != r["path"] || projection["source_sha256"] != r["source_sha256"] {
					t.Fatal("normalized identity differs from original source")
				}
				if reader != "20261027-072935-todo-3719" && reader != "20261027-105225-todo-3778" && reader != "20261027-194701-todo-3778" {
					t.Fatal("original assessment actor inferred from publisher or missing")
				}
				if file.sourceFields == 5 {
					if !reflect.DeepEqual(r["original_read_tuple"], original["read"]) || r["actual_body_reader_inferred_from_assessment_writer"] != false || r["copy_license_accepted"] != false || r["new_final_disposition"] != false {
						t.Fatal("original read profile or permission changed")
					}
					chunkID, ok := r["original_context_ref"].(string)
					if !ok {
						t.Fatal("original context pointer absent")
					}
					defs := received27Object(t, outer["original_chunk_definitions"])
					whole := received27Object(t, defs[chunkID])
					if whole["reader_job"] != reader {
						t.Fatal("packet reader differs")
					}
					found := false
					for _, old := range received27Array(t, whole["entries"]) {
						if reflect.DeepEqual(old, original) {
							found = true
						}
					}
					if !found {
						t.Fatal("complete original assessment changed")
					}
				} else {
					sha, ok := r["original_chunk_sha256"].(string)
					if !ok {
						t.Fatal("packet SHA absent")
					}
					grouped[sha] = append(grouped[sha], original)
				}
			}
			if file.sourceFields == 5 {
				defs := received27Object(t, outer["original_chunk_definitions"])
				pins := received27Array(t, outer["whole_original_ACK_pins"])
				if len(pins) != len(defs) {
					t.Fatal("independent ACK pins lost")
				}
				for id, whole := range defs {
					matches := 0
					for _, value := range pins {
						pin := received27Object(t, value)
						if pin["chunk_id"] == id && pin["chunk_sha256"] == received27Digest(t, whole) {
							matches++
						}
					}
					if matches != 1 {
						t.Fatal("whole original packet does not match unique independent ACK")
					}
					contexts++
				}
			} else {
				shared := received27Object(t, outer["shared_original_contexts"])
				if len(grouped) != len(shared) {
					t.Fatal("whole packet membership changed")
				}
				for sha, originals := range grouped {
					common := received27Object(t, received27Object(t, shared[sha])["common"])
					whole := map[string]any{}
					for k, v := range common {
						whole[k] = v
					}
					whole["entries"] = originals
					if received27Digest(t, whole) != sha {
						t.Fatal("shared form lost original fields, unknowns, or order")
					}
					contexts++
				}
			}
		})
	}
	if len(seen) != 98 || contexts != 20 {
		t.Fatalf("received coverage differs: %d primaries, %d original packets", len(seen), contexts)
	}
}
