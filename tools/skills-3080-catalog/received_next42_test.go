package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
)

func TestNext42PreservesOriginalOffsetsAndPriorLiteralNotice(t *testing.T) {
	var a ReceivedCohort
	loadPublicReceipt(t, "skills-3080-sixmile-next42-evaluation.json", &a)
	spec, err := registeredReceivedSpec(a)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := validateReceivedCohort(a, spec)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 42 || entries[0].Row != 91 || entries[41].Row != 132 || a.Counts["primary_raw"] != 0 || a.Counts["primary_workflow"] != 39 || a.Counts["primary_saved_unread"] != 2 || a.Counts["primary_partial"] != 1 || len(spec.PriorLiteralSupports) != 1 {
		t.Fatal("original42 state or reader cursor changed")
	}
	var prior ReceivedCohort
	loadPublicReceipt(t, "skills-3080-sixmile-stream90-evaluation.json", &prior)
	previous, err := validateReceivedCohort(prior, receivedStream90Spec)
	if err != nil {
		t.Fatal(err)
	}
	anchor := spec.PriorLiteralSupports[0]
	if anchor.CohortSHA != prior.EntrySHA {
		t.Fatal("prior literal cohort pin not independently accepted")
	}
	found := false
	for _, r := range previous {
		for _, s := range r.Support {
			if r.Row == anchor.Row && s.Path == anchor.Support.Path && s.SHA == anchor.Support.SHA && s.Blob == anchor.Support.Blob && s.Reader != nil && anchor.Support.Reader != nil && *s.Reader == *anchor.Support.Reader && s.Bytes != nil && anchor.Support.Bytes != nil && *s.Bytes == *anchor.Support.Bytes && evidenceTrue(s.Raw) && evidenceTrue(s.Workflow) && s.Mask != nil && *s.Mask == 0 {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("fixed notice anchor has no earlier same-reader literal source")
	}
}

func TestNext42RejectsUnprovedPriorLiteralAndStatePromotion(t *testing.T) {
	for _, kind := range []string{"valid", "missing_anchor", "other_actor", "other_row", "other_cohort", "wrong_offset", "unread_reader", "unread_workflow", "partial_raw", "copy_grant", "missing_original", "unsaved_bytes"} {
		t.Run(kind, func(t *testing.T) {
			var a ReceivedCohort
			loadPublicReceipt(t, "skills-3080-sixmile-next42-evaluation.json", &a)
			spec := receivedNext42Spec
			spec.PriorLiteralSupports = append([]receivedLiteralSupport(nil), spec.PriorLiteralSupports...)
			var entries []map[string]any
			if err := json.Unmarshal(a.Entries, &entries); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "missing_anchor":
				spec.PriorLiteralSupports = nil
			case "other_actor":
				actor := "other"
				spec.PriorLiteralSupports[0].Support.Reader = &actor
			case "other_row":
				spec.PriorLiteralSupports[0].Row = 9
			case "other_cohort":
				spec.PriorLiteralSupports[0].CohortSHA = strings.Repeat("a", 64)
			case "wrong_offset":
				spec.FirstReaderRow = map[int]int{3719: 1}
			case "unread_reader":
				entries[0]["reader_job"] = "20261025-041905-todo-3719"
			case "unread_workflow":
				entries[0]["body_workflow_read_complete"] = true
			case "partial_raw":
				found := false
				for _, e := range entries {
					if e["partial_read"] == true {
						e["body_read_complete"] = true
						e["body_workflow_read_complete"] = true
						found = true
						break
					}
				}
				if !found {
					t.Fatal("partial fixture absent")
				}
			case "copy_grant":
				entries[0]["copy_license_accepted"] = true
			case "missing_original":
				delete(entries[0], "original_reader_assessment")
			case "unsaved_bytes":
				found := false
				for _, e := range entries {
					for _, v := range e["support_read_states"].([]any) {
						s := v.(map[string]any)
						if s["body_saved"] == false {
							s["bytes"] = float64(0)
							found = true
							break
						}
					}
					if found {
						break
					}
				}
				if !found {
					t.Fatal("unsaved fixture absent")
				}
			}
			// Mutate the local expected digest too: prove the role/provenance
			// checks reject the claim independently of the fixed production pin.
			raw, err := json.Marshal(entries)
			if err != nil {
				t.Fatal(err)
			}
			h := sha256.Sum256(raw)
			a.Entries = raw
			a.EntrySHA = hex.EncodeToString(h[:])
			spec.EntrySHA = a.EntrySHA
			_, err = validateReceivedCohort(a, spec)
			if (err == nil) != (kind == "valid") {
				t.Fatalf("%s accepted=%v: %v", kind, err == nil, err)
			}
		})
	}
}
