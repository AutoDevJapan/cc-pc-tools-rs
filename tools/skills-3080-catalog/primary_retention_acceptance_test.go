package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"reflect"
	"testing"
)

// The full snapshots stay with the canonical worker. Supplying both explicit
// paths makes this an actual-byte acceptance check; CI without them must skip
// that check, rather than treating the public assertion as execution evidence.
func TestPublishedActualPrimary170Retention(t *testing.T) {
	priorPath := os.Getenv("SKILLS3080_PRIMARY_PRIOR_SNAPSHOT")
	currentPath := os.Getenv("SKILLS3080_PRIMARY_CURRENT_SNAPSHOT")
	if priorPath == "" && currentPath == "" {
		t.Skip("Canonical worker supplies the pinned prior170/current175 snapshots; CI does not claim their actual bytes were compared.")
	}
	if priorPath == "" || currentPath == "" {
		t.Fatal("both explicit pinned primary snapshots are required")
	}
	var published map[string]any
	loadPublicReceipt(t, "skills-3080-primaryflow-next5-evaluation175645.json", &published)
	requireAssessmentInputRegistered(t, "skills-3080-primaryflow-next5-evaluation175645.json", "59e231295f75004536f33de7d45a3369b3c9b55e5730ac11ab3fb99bf6927f19", 5)
	read := func(path string, expected any) []map[string]any {
		t.Helper()
		want, ok := expected.(string)
		if !ok || len(want) != 64 {
			t.Fatal("published snapshot pin absent")
		}
		f, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		st, err := f.Stat()
		if err != nil || !st.Mode().IsRegular() || st.Size() <= 0 || st.Size() > 4<<20 {
			t.Fatal("bounded regular primary snapshot required")
		}
		raw, err := io.ReadAll(io.LimitReader(f, (4<<20)+1))
		if err != nil || travel3Digest(raw) != want {
			t.Fatal("actual primary snapshot differs from its published SHA")
		}
		var v struct {
			Records []map[string]any `json:"records"`
		}
		if err = json.Unmarshal(raw, &v); err != nil {
			t.Fatal(err)
		}
		return v.Records
	}
	prior := read(priorPath, published["prior_prepared170_raw_sha256"])
	current := read(currentPath, published["source_full175_snapshot_sha256"])
	newRecords := []map[string]any{}
	for _, record := range received27Array(t, published["records"]) {
		newRecords = append(newRecords, received27Object(t, record))
	}
	if len(prior) != 170 || len(current) != 175 || len(newRecords) != 5 {
		t.Fatal("exact prior170/current175/published5 required")
	}
	if err := primaryRecordsRetained(prior, current, newRecords); err != nil {
		t.Fatal(err)
	}
	t.Log("Actual pinned prior170 records are unchanged in current175; its only5 additional records equal the published next5, including every actor, null and original field.")
}

func primaryRecordsRetained(prior, current, added []map[string]any) error {
	index := func(records []map[string]any) (map[string]map[string]any, error) {
		out := map[string]map[string]any{}
		for _, record := range records {
			id, ok := record["inventory_id"].(string)
			if !ok || id == "" || out[id] != nil {
				return nil, fmt.Errorf("missing or duplicate primary identity")
			}
			out[id] = record
		}
		return out, nil
	}
	before, err := index(prior)
	if err != nil {
		return err
	}
	after, err := index(current)
	if err != nil {
		return err
	}
	newRecords, err := index(added)
	if err != nil {
		return err
	}
	if len(after) != len(before)+len(newRecords) {
		return fmt.Errorf("prior/current/addition count differs")
	}
	for id, old := range before {
		if !reflect.DeepEqual(old, after[id]) {
			return fmt.Errorf("prior primary record changed or removed")
		}
	}
	for id, record := range newRecords {
		if before[id] != nil || !reflect.DeepEqual(record, after[id]) {
			return fmt.Errorf("new primary record differs or reuses prior identity")
		}
	}
	return nil
}

func TestPrimaryRetentionRejectsChangedMissingAndRepeatedRecords(t *testing.T) {
	a := map[string]any{"inventory_id": "prior-a", "reader": "original", "mask": nil}
	b := map[string]any{"inventory_id": "prior-b", "reader": "original"}
	c := map[string]any{"inventory_id": "new-c", "rights_pending": true}
	prior := []map[string]any{a, b}
	if err := primaryRecordsRetained(prior, []map[string]any{a, c, b}, []map[string]any{c}); err != nil {
		t.Fatal(err)
	}
	for name, current := range map[string][]map[string]any{
		"changed_actor": {a, {"inventory_id": "prior-b", "reader": "different"}, c},
		"removed_null":  {{"inventory_id": "prior-a", "reader": "original"}, b, c},
		"missing_old":   {a, c},
		"duplicate_old": {a, a, c},
		"duplicate_new": {a, b, c, c},
		"wrong_new":     {a, b, {"inventory_id": "new-c", "rights_pending": false}},
		"unexpected":    {a, b, c, {"inventory_id": "unpublished"}},
	} {
		t.Run(name, func(t *testing.T) {
			if primaryRecordsRetained(prior, current, []map[string]any{c}) == nil {
				t.Fatal("changed, incomplete or repeated primary records accepted")
			}
		})
	}
	if primaryRecordsRetained(prior, []map[string]any{a, b}, []map[string]any{a}) == nil {
		t.Fatal("prior identity accepted as a new assessment")
	}
}
