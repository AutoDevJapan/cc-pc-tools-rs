package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestConservesOccurrencesAndDoesNotApproveDuplicates(t *testing.T) {
	h := strings.Repeat("a", 64)
	input := `{"inventory_id":"core:one","inventory_unit":"github_skill_entry","repo":"a/b","sha256":"` + h + `","machine_body_read":true,"normalized_canonical_url":"https://example/a"}` + "\n" +
		`{"inventory_id":"site:one","inventory_unit":"site_index_entry","effective_attempt":{"sha256":"` + h + `","machine_body_read":true},"normalized_canonical_url":"https://example/a"}` + "\n" +
		`{"inventory_id":"site:missing","inventory_unit":"site_index_entry"}` + "\n" +
		`{"inventory_id":"held:one","inventory_unit":"unread_additional_text_support_shape"}` + "\n"
	var out bytes.Buffer
	s, err := scan(strings.NewReader(input), &out)
	if err != nil {
		t.Fatal(err)
	}
	if s.Records != 4 || s.Available != 2 || s.UniqueBodies != 1 || s.RedundantBodies != 1 || s.RedundantURLs != 1 || s.States["held_body_unavailable"] != 1 || s.States["held_execution_review"] != 1 {
		t.Fatalf("lost/incorrectly classified records: %+v", s)
	}
	d := json.NewDecoder(&out)
	for i := 0; i < 4; i++ {
		var q QueueRow
		if err := d.Decode(&q); err != nil {
			t.Fatal(err)
		}
		if q.Installed || q.LicenseApproved || q.SemanticApproved {
			t.Fatal("triage manufactured approval")
		}
		if i == 1 && q.FirstBody != "core:one" {
			t.Fatal("missing cross-source duplicate relation")
		}
	}
}

func TestRejectsInvalidCheckpointInput(t *testing.T) {
	for _, input := range []string{"", `{`, `{"inventory_id":"a"}`, `{"inventory_id":"a","inventory_unit":"core","machine_body_read":true}`, "{\"inventory_id\":\"a\",\"inventory_unit\":\"core\"}\n{\"inventory_id\":\"a\",\"inventory_unit\":\"core\"}"} {
		if _, err := scan(strings.NewReader(input), &bytes.Buffer{}); err == nil {
			t.Fatalf("accepted corrupt input: %q", input)
		}
	}
}
