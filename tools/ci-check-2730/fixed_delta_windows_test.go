package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestFixedDeltaRejectsChangedScope(t *testing.T) {
	commit := `{"sha":"` + fixedSHA + `","tree":{"sha":"` + fixedTree + `"},"parents":[{"sha":"` + parentSHA + `"}]}`
	comparison := `{"status":"ahead","ahead_by":1,"behind_by":0,"files":[{"filename":"` + changedPath + `","status":"modified","sha":"cf3398189ba7fb6cf96ce152903d4cdaa6387c50"}]}`
	for _, tc := range []struct {
		name, c, d string
		ok         bool
	}{
		{"fixed", commit, comparison, true},
		{"old source", strings.Replace(commit, fixedSHA, parentSHA, 1), comparison, false},
		{"wrong tree", strings.Replace(commit, fixedTree, strings.Repeat("0", 40), 1), comparison, false},
		{"different parent", strings.Replace(commit, parentSHA, strings.Repeat("1", 40), 1), comparison, false},
		{"different blob", commit, strings.Replace(comparison, "cf3398189ba7fb6cf96ce152903d4cdaa6387c50", strings.Repeat("2", 40), 1), false},
		{"different file", commit, strings.Replace(comparison, changedPath, "other.rs", 1), false},
		{"two commits", commit, strings.Replace(comparison, `"ahead_by":1`, `"ahead_by":2`, 1), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			read := func(_ context.Context, endpoint string, v any) error {
				data := tc.d
				if strings.Contains(endpoint, "/git/commits/") {
					data = tc.c
				}
				return json.Unmarshal([]byte(data), v)
			}
			if err := verifyDelta(context.Background(), "fixture/repository", read); (err == nil) != tc.ok {
				t.Fatalf("acceptance=%v expected=%v", err == nil, tc.ok)
			}
		})
	}
}
