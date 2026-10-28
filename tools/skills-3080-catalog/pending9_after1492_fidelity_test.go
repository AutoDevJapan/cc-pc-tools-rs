package main

import (
	"encoding/json"
	"io"
	"os"
	"testing"
)

func TestOriginal3719After245Next4RetainsWholeActorAndUnreadStates(t *testing.T) {
	checkOriginal3719PendingSnapshot(t, "skills-3080-original3719-next4-246249-evaluation175645.json", "a92204428795cf0f3bbbb8c33bd458e1e3c549c6e1156de2fcc0bbd3ae1c2aac", 246, 249, 3, "20261028-171959-todo-3719", 68, 56, 12)
}
func TestPrimaryflowAfter190Next5RetainsWholePacketsAndPendingMembers(t *testing.T) {
	checkPrimaryflowOriginalMeaning(t, "skills-3080-primaryflow-next5-after190-evaluation175645.json", "0d0a901edf7c43bc71b6f3eb4a0aacf4cbd73ca74006db73b352483a5417c2c5", 5, 3, 9)
}
func TestPublishedActualPrimary190RetentionIn195(t *testing.T) {
	priorPath, currentPath := os.Getenv("SKILLS3080_LATEST_PRIMARY_PRIOR_SNAPSHOT"), os.Getenv("SKILLS3080_LATEST_PRIMARY_CURRENT_SNAPSHOT")
	if priorPath == "" && currentPath == "" {
		t.Skip("Canonical worker supplies pinned prior190/current195 bytes; CI does not claim their private-byte comparison.")
	}
	if priorPath == "" || currentPath == "" {
		t.Fatal("both explicit latest primary snapshots required")
	}
	var pub map[string]any
	loadPublicReceipt(t, "skills-3080-primaryflow-next5-after190-evaluation175645.json", &pub)
	read := func(path string, want any) []map[string]any {
		t.Helper()
		sha, ok := want.(string)
		if !ok || len(sha) != 64 {
			t.Fatal("independent published SHA absent")
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
			t.Fatal("actual snapshot does not match independent pin")
		}
		var v struct {
			Records []map[string]any `json:"records"`
		}
		if e = json.Unmarshal(b, &v); e != nil {
			t.Fatal(e)
		}
		return v.Records
	}
	prior, current := read(priorPath, pub["prior_prepared190_raw_sha256"]), read(currentPath, pub["source_full195_snapshot_sha256"])
	added := []map[string]any{}
	for _, v := range received27Array(t, pub["records"]) {
		added = append(added, received27Object(t, v))
	}
	if len(prior) != 190 || len(current) != 195 || len(added) != 5 {
		t.Fatal("exact prior190/current195/new5 required")
	}
	if e := primaryRecordsRetained(prior, current, added); e != nil {
		t.Fatal(e)
	}
}
