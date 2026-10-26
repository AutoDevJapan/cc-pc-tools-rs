package main

import (
	"encoding/json"
	"errors"
	"testing"
)

type received35Row struct {
	ID       string            `json:"inventory_id"`
	Path     string            `json:"path"`
	SHA      string            `json:"source_sha256"`
	Blob     string            `json:"git_blob"`
	Bytes    int               `json:"bytes"`
	Ordinal  int               `json:"reader_row"`
	Tuple    []json.RawMessage `json:"original_read_tuple"`
	Grant    *bool             `json:"copy_license_accepted"`
	Decision *bool             `json:"new_final_disposition"`
}

func validateReceived35(rows []received35Row, packages map[string]received35Row, rawCredit, workflowCredit int) error {
	if len(rows) != 35 || len(packages) != 35 || rawCredit != 0 || workflowCredit != 0 {
		return errors.New("assessment publication must not imply extra reading or permission")
	}
	seen, states, total := map[string]bool{}, map[int]int{}, 0
	for i, r := range rows {
		p, ok := packages[r.ID]
		var state int
		var complete bool
		if !ok || seen[r.ID] || r.Ordinal != 133+i || r.Path != p.Path || r.SHA != p.SHA || r.Blob != p.Blob || r.Bytes != p.Bytes || !validHex(r.SHA, 64) || !validHex(r.Blob, 40) || len(r.Tuple) != 6 || !recordedFalse(r.Grant) || !recordedFalse(r.Decision) {
			return errors.New("received primary no longer matches its independently checked source")
		}
		if json.Unmarshal(r.Tuple[0], &state) != nil || json.Unmarshal(r.Tuple[4], &complete) != nil || !complete || (state != 3 && state != 4) {
			return errors.New("original masking or exact reuse state was promoted")
		}
		seen[r.ID], total = true, total+r.Bytes
		states[state]++
	}
	if states[3] != 30 || states[4] != 5 || total != 501009 {
		return errors.New("original received scope differs")
	}
	return nil
}

func TestReceived35KeepsSourceIdentityReuseAndNoNewReadingCredit(t *testing.T) {
	var a struct {
		Entries  []received35Row `json:"entries"`
		Raw      int             `json:"new_global_raw_read_credit"`
		Workflow int             `json:"new_global_workflow_read_credit"`
	}
	loadPublicReceipt(t, "skills-3080-sixmile-next35-evaluation090101.json", &a)
	var physical struct {
		Packages []received35Row `json:"packages"`
		Support  int             `json:"support_paths"`
		Saved    int             `json:"support_saved"`
		Unsaved  int             `json:"support_unsaved"`
		Reads    int             `json:"new_semantic_reads"`
	}
	loadPublicReceipt(t, "skills-3080-sixmile-next35-physical090101.json", &physical)
	if physical.Support != 639 || physical.Saved != 356 || physical.Unsaved != 283 || physical.Reads != 0 || physical.Support != physical.Saved+physical.Unsaved {
		t.Fatal("physical metadata was confused with a source reading")
	}
	packages := map[string]received35Row{}
	for _, p := range physical.Packages {
		packages[p.ID] = p
	}
	for _, kind := range []string{"valid", "missing", "different_source", "claimed_copy_grant", "claimed_final", "reuse_as_fresh_raw", "extra_workflow_credit"} {
		t.Run(kind, func(t *testing.T) {
			rows := append([]received35Row(nil), a.Entries...)
			raw, workflow, yes := a.Raw, a.Workflow, true
			switch kind {
			case "missing":
				rows = rows[1:]
			case "different_source":
				rows[0].SHA = rows[1].SHA
			case "claimed_copy_grant":
				rows[0].Grant = &yes
			case "claimed_final":
				rows[0].Decision = &yes
			case "reuse_as_fresh_raw":
				raw++
			case "extra_workflow_credit":
				workflow++
			}
			if err := validateReceived35(rows, packages, raw, workflow); (err != nil) != (kind != "valid") {
				t.Fatalf("%s: %v", kind, err)
			}
		})
	}
}
