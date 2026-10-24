package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRegisteredReceivedEvidenceCannotMixIndependentAnchors(t *testing.T) {
	for _, r := range receivedRegistrations {
		var a ReceivedCohort
		loadPublicReceipt(t, r.Artifact, &a)
		spec, err := registeredReceivedSpec(a)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = validateReceivedCohort(a, spec); err != nil {
			t.Fatal(err)
		}
		a.InputSHA = strings.Repeat("0", 64)
		if _, err = registeredReceivedSpec(a); err == nil {
			t.Fatal("unknown reading input accepted")
		}
		a.InputSHA = received50Spec.InputSHA
		a.EntrySHA = received3674Spec.EntrySHA
		if _, err = registeredReceivedSpec(a); err == nil {
			t.Fatal("two different original inputs combined")
		}
	}
}

func TestSavedUnreadSupportHasKnownIdentityAndNoInventedReader(t *testing.T) {
	for _, name := range []string{"valid", "reader", "mask", "raw", "workflow", "partial", "scan", "unknown_size"} {
		t.Run(name, func(t *testing.T) {
			var a ReceivedCohort
			loadPublicReceipt(t, "skills-3080-sixmile-received-next50-evaluation.json", &a)
			var entries []map[string]any
			if err := json.Unmarshal(a.Entries, &entries); err != nil {
				t.Fatal(err)
			}
			var target map[string]any
			var actualReader any
			for _, e := range entries {
				for _, item := range e["support_read_states"].([]any) {
					s := item.(map[string]any)
					if s["state"] == "NR" {
						target, actualReader = s, e["reader_job"]
						break
					}
				}
				if target != nil {
					break
				}
			}
			if target == nil {
				t.Fatal("actual saved-unread source absent")
			}
			switch name {
			case "reader":
				target["reader_job"] = actualReader
			case "mask":
				target["masked_value_count"] = float64(0)
			case "raw":
				target["raw_read_complete"] = true
			case "workflow":
				target["workflow_read_complete"] = true
			case "partial":
				target["partial_read"] = true
			case "scan":
				target["scan_only"] = true
			case "unknown_size":
				target["bytes"] = nil
			}
			// A new, independently anchored synthetic receipt must still satisfy
			// the saved/unread/null-actor rules. Anchor checks cannot mask them.
			b, err := json.Marshal(entries)
			if err != nil {
				t.Fatal(err)
			}
			a.Entries = b
			v, err := uniqueEvidenceJSON(b)
			if err != nil {
				t.Fatal(err)
			}
			canonical, err := json.Marshal(v)
			if err != nil {
				t.Fatal(err)
			}
			h := sha256.Sum256(canonical)
			spec := received3674Spec
			a.EntrySHA, spec.EntrySHA = hex.EncodeToString(h[:]), hex.EncodeToString(h[:])
			_, err = validateReceivedCohort(a, spec)
			if name == "valid" && err != nil {
				t.Fatal(err)
			}
			if name != "valid" && err == nil {
				t.Fatalf("invented saved-unread state %s accepted", name)
			}
		})
	}
}

func TestReceivedNext50ProjectionRetainsUnreadAndUnknownSeparately(t *testing.T) {
	var a ReceivedCohort
	raw := loadPublicReceipt(t, "skills-3080-sixmile-received-next50-evaluation.json", &a)
	p, err := projectReceivedEvidence(raw, "skills-3080-sixmile-received-next50-evaluation.json", received3674Spec)
	if err != nil {
		t.Fatal(err)
	}
	if p.Counters["raw_unmasked_primary_full_read"] != 14 || p.Counters["workflow_primary_full_read"] != 50 || p.Counters["support_saved_unread"] != 207 || p.Counters["support_unsaved"] != 165 || p.Counters["support_workflow_full_read"] != 77 {
		t.Fatal("unread or unknown metadata became a full reading")
	}
}

func TestReceivedProjectionPublishesCompleteBytesWithoutReplacingExistingData(t *testing.T) {
	root := t.TempDir()
	output := filepath.Join(root, "projection.json")
	input := filepath.Join("..", "..", "docs", "skills-3080-sixmile-received-next50-evaluation.json")
	if err := writeReceivedProjection(input, output); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	var p ReceivedProjection
	if json.Unmarshal(raw, &p) != nil || p.Counters["candidate_paths"] != 50 || p.Counters["support_saved_unread"] != 207 {
		t.Fatal("incomplete projection was published")
	}
	if err = writeReceivedProjection(input, output); err == nil {
		t.Fatal("existing finalized projection replaced")
	}
	current, err := os.ReadFile(output)
	if err != nil || string(current) != string(raw) {
		t.Fatal("existing projection changed")
	}
	stages, err := filepath.Glob(filepath.Join(root, ".received-projection-*"))
	if err != nil || len(stages) != 0 {
		t.Fatal("owned stage remained after completed calls")
	}
}
