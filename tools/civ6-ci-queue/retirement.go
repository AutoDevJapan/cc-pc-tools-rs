package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// Exact v2/v3 identity encoding used by the existing safe-delete producer. This
// does not create, adopt, edit or regenerate an ownership record.
type retirementIdentity struct {
	Version      int                  `json:"version"`
	ID           string               `json:"id"`
	Root         string               `json:"root"`
	Path         string               `json:"path"`
	Volume       uint32               `json:"volume"`
	FileID       uint64               `json:"file_id"`
	Birth        uint64               `json:"birth,omitempty"`
	Created      string               `json:"created_at"`
	Project      string               `json:"project"`
	Content      string               `json:"content"`
	ProducerJob  string               `json:"producer_job,omitempty"`
	ProducerTodo uint64               `json:"producer_todo,omitempty"`
	ProducerKind string               `json:"producer_kind,omitempty"`
	Ancestors    []retirementAncestor `json:"ancestors,omitempty"`
}
type retirementAncestor struct {
	Path   string `json:"path"`
	Volume uint32 `json:"volume"`
	ID     uint64 `json:"file_id"`
	Birth  uint64 `json:"birth"`
}
type retirementRecord struct {
	retirementIdentity
	Integrity   string `json:"integrity"`
	State       string `json:"state"`
	Destination string `json:"destination,omitempty"`
}

func retiredReceipt(pin Pinned, cl CleanupRec, key string) (CleanupRec, error) {
	if err := pin.Verify(); err != nil {
		return cl, err
	}
	b, err := boundedRegular(filepath.Join(filepath.Dir(pin.Path), "receipts-go", cl.SafeDeleteID+".json"), 8192)
	if err != nil {
		return cl, err
	}
	var r retirementRecord
	if json.Unmarshal(b, &r) != nil {
		return cl, errors.New("retirement receipt invalid")
	}
	if r.Content != "job-"+safeKey(key) {
		return cl, errors.New("retirement job identity differs")
	}
	return checkRetirementRecord(r, cl)
}

func checkRetirementRecord(r retirementRecord, cl CleanupRec) (CleanupRec, error) {
	b, _ := json.Marshal(r.retirementIdentity)
	if (r.Version != 2 && r.Version != 3) || r.ID != cl.SafeDeleteID || r.Path != cl.Root || r.Root != filepath.Dir(cl.Root) ||
		r.Project != "civ6-ci-queue" || r.FileID == 0 || r.Volume == 0 || r.Integrity != digestHexBytes(b) {
		return cl, errors.New("retirement receipt identity or integrity differs")
	}
	if r.Version == 3 && (r.Birth == 0 || len(r.Ancestors) == 0 || (r.ProducerKind != "scheduler" && r.ProducerKind != "ordinary-only")) {
		return cl, errors.New("v3 producer metadata missing")
	}
	if _, err := os.Lstat(cl.Root); !os.IsNotExist(err) {
		return cl, errors.New("old root is still present or uncertain")
	}
	switch r.State {
	case "source_absent":
		if r.Destination != "" {
			return cl, errors.New("unexpected retirement destination")
		}
		cl.State = "removed"
	case "quarantined":
		if !absClean(r.Destination) || filepath.VolumeName(r.Destination) != filepath.VolumeName(cl.Root) ||
			!retiredIdentityMatches(filepath.Join(r.Destination, "files"), r.Volume, r.FileID) {
			return cl, errors.New("quarantined original directory identity differs")
		}
		cl.State = "quarantined"
	default:
		return cl, errors.New("safe-delete retirement is not complete")
	}
	cl.Detail = "closed safe-delete identity verified; old root is absent"
	return cl, nil
}
