package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestSkills3080CLIForwardsOnlyFixedFailureStages(t *testing.T) {
	for _, tc := range []struct {
		name  string
		stage any
		keep  bool
	}{
		{"source_integrity", "source_catalog_integrity", true},
		{"original_issuer", "creation_header", true},
		{"current_snapshot", "current_snapshot", true},
		{"current_writer", "writer_recheck", true},
		{"unknown", "arbitrary_new_stage", false},
		{"path", "D:/unrelated/private.txt", false},
		{"error_text", "access denied for private data", false},
		{"suffix", "creation_header_private", false},
		{"newline", "creation_header\nprivate data", false},
		{"object", object{"private": "data"}, false},
		{"number", 7, false},
		{"empty", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			calls := 0
			code := runSkillsCLI([]string{"--consume-skills3080", "D:/synthetic/request.json"}, &out, func(op, path string) object {
				calls++
				if op != "consume-skills3080" || path != "D:/synthetic/request.json" {
					t.Fatal("real CLI dispatch boundary not reached")
				}
				return object{"qualified": false, "reason": "source", "failure_stage": tc.stage, "final_response": "never return this body", "private_field": "never return this metadata"}
			})
			var got object
			if code != 1 || calls != 1 || json.Unmarshal(out.Bytes(), &got) != nil || got["qualified"] != false || got["reason"] != "source" {
				t.Fatal("failure status or ordinary reason changed")
			}
			stage, exists := got["failure_stage"]
			if exists != tc.keep || (tc.keep && stage != tc.stage) {
				t.Fatalf("CLI lost a fixed stage or retained an unsafe one: %s", tc.name)
			}
			wantFields := 2
			if tc.keep {
				wantFields++
			}
			if len(got) != wantFields || strings.Contains(out.String(), "never return") || strings.Contains(out.String(), "private") {
				t.Fatal("refused CLI leaked body, error text or arbitrary metadata")
			}
		})
	}
}
