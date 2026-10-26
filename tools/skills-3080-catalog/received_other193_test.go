package main

import (
	"encoding/json"
	"fmt"
	"testing"
)

func other193Reading(raw []json.RawMessage) (int, error) {
	if len(raw) != 6 {
		return 0, fmt.Errorf("six reading fields required")
	}
	var state, last int
	if json.Unmarshal(raw[0], &state) != nil || json.Unmarshal(raw[5], &last) != nil {
		return 0, fmt.Errorf("state/last absent")
	}
	if state == 1 {
		if string(raw[1]) != "null" || string(raw[2]) != "null" || string(raw[3]) != "false" || string(raw[4]) != "false" || last != 0 {
			return 0, fmt.Errorf("unread observation gained a reader or completion")
		}
		return state, nil
	}
	var mask, actor int
	if json.Unmarshal(raw[1], &mask) != nil || json.Unmarshal(raw[2], &actor) != nil || actor != 0 || last < 1 || string(raw[4]) != "true" || (state != 2 && state != 3) || (state == 2 && (mask != 0 || string(raw[3]) != "true")) || (state == 3 && (mask < 1 || string(raw[3]) != "false")) {
		return 0, fmt.Errorf("raw/masked observation promoted")
	}
	return state, nil
}

func TestReceivedOther193PreservesOriginalUnreadAndSeparateCurrentAmendment(t *testing.T) {
	var received struct {
		Entries []struct {
			executionIdentity
			Reader   string `json:"actual_assessment_reader"`
			Original struct {
				Read   []json.RawMessage `json:"read"`
				Source []json.RawMessage `json:"source"`
			} `json:"original_assessment"`
		} `json:"entries"`
		Amendments []struct {
			ID        string            `json:"inventory_id"`
			Reader    string            `json:"reader_job"`
			Read      []json.RawMessage `json:"read"`
			Preserved bool              `json:"old_original_unread_receipt_preserved"`
			Grants    int               `json:"new_grants"`
			Decisions int               `json:"new_final_dispositions"`
		} `json:"parent_primary_read_amendments"`
		Grants    int  `json:"new_copy_grants"`
		Decisions int  `json:"new_final_dispositions"`
		Central   int  `json:"central_operations"`
		Complete  bool `json:"whole16_complete"`
	}
	loadPublicReceipt(t, "skills-3080-other193-evaluation214717.json", &received)
	seen, states := map[string]bool{}, map[int]int{}
	for _, entry := range received.Entries {
		if entry.ID == "" || seen[entry.ID] || entry.Path != entry.SourcePath || !evidencePath(entry.Path) || !evidenceHex(entry.SHA, 64) || entry.Reader == "" || len(entry.Original.Source) != 7 {
			t.Fatal("received source identity/reader missing or repeated")
		}
		var id, path, hash string
		if json.Unmarshal(entry.Original.Source[0], &id) != nil || json.Unmarshal(entry.Original.Source[3], &path) != nil || json.Unmarshal(entry.Original.Source[4], &hash) != nil || id != entry.ID || path != entry.Path || hash != entry.SHA {
			t.Fatal("normalized source differs from original tuple")
		}
		state, err := other193Reading(entry.Original.Read)
		if err != nil {
			t.Fatal(err)
		}
		states[state]++
		seen[entry.ID] = true
	}
	if len(seen) != 193 || states[1] != 1 || states[2] != 96 || states[3] != 96 || received.Grants != 0 || received.Decisions != 0 || received.Central != 0 || received.Complete || len(received.Amendments) != 1 {
		t.Fatal("received observations became grants, final decisions or full coverage")
	}
	amendment := received.Amendments[0]
	if !seen[amendment.ID] || amendment.Reader != "20261026-214717-todo-3080" || !amendment.Preserved || amendment.Grants != 0 || amendment.Decisions != 0 {
		t.Fatal("current reading replaced the original actor or decision")
	}
	if state, err := other193Reading(amendment.Read); err != nil || state != 2 {
		t.Fatal("separate actual current full reading absent")
	}
}

func TestReceivedOther193RejectsPromotedUnreadAndMaskedObservations(t *testing.T) {
	for _, text := range []string{`[1,0,null,false,false,0]`, `[1,null,0,false,false,0]`, `[1,null,null,false,true,193]`, `[2,1,0,true,true,193]`, `[3,0,0,false,true,193]`, `[3,4,0,true,true,193]`} {
		var reading []json.RawMessage
		if err := json.Unmarshal([]byte(text), &reading); err != nil {
			t.Fatal(err)
		}
		if _, err := other193Reading(reading); err == nil {
			t.Fatal("invalid reading promoted:", text)
		}
	}
}
