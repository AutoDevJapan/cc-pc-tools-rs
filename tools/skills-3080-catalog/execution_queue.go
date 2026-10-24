package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type ExecutionManifest struct {
	CatalogSHA string              `json:"catalog_sha256"`
	Records    int                 `json:"catalog_records"`
	Artifacts  []ExecutionArtifact `json:"assessment_artifacts"`
	Ledger     ExecutionArtifact   `json:"adoption_ledger"`
}
type ExecutionArtifact struct {
	File string `json:"file"`
	SHA  string `json:"sha256"`
	Rows int    `json:"rows"`
}
type ExecutionReference struct {
	File string `json:"artifact"`
	Row  int    `json:"one_based_row"`
}
type executionIdentity struct {
	ID           string `json:"inventory_id"`
	SHA          string `json:"source_sha256"`
	Path         string `json:"path"`
	Commit       string `json:"commit"`
	SourceCommit string `json:"source_commit"`
	SourcePath   string `json:"source_path"`
	SourceRepo   string `json:"source_repo"`
	Disposition  string `json:"disposition"`
}
type ExecutionEvidence struct {
	Assessments map[string]executionIdentity
	References  map[string][]ExecutionReference
	Decisions   map[string]executionIdentity
}
type ExecutionRow struct {
	Record
	Stage       string               `json:"execution_stage"`
	Next        string               `json:"next_operation"`
	References  []ExecutionReference `json:"assessment_evidence,omitempty"`
	RelatedBody string               `json:"related_assessed_body_id,omitempty"`
	Decision    string               `json:"recorded_final_disposition,omitempty"`
}
type ExecutionSummary struct {
	State       string         `json:"state"`
	CatalogSHA  string         `json:"catalog_sha256"`
	ManifestSHA string         `json:"manifest_sha256"`
	QueueSHA    string         `json:"queue_sha256"`
	Records     int            `json:"occurrences"`
	Assessments int            `json:"occurrences_with_assessment_records"`
	Final       int            `json:"recorded_final_occurrences"`
	Stages      map[string]int `json:"stages"`
	Decisions   map[string]int `json:"recorded_final_dispositions"`
	Limits      []string       `json:"limits"`
}

func readExecutionArtifact(root string, pin ExecutionArtifact) ([]byte, error) {
	if filepath.Base(pin.File) != pin.File || !strings.HasPrefix(pin.File, "skills-3080-") || !strings.HasSuffix(pin.File, ".json") || !evidenceHex(pin.SHA, 64) {
		return nil, errors.New("invalid public evidence binding")
	}
	file := filepath.Join(root, pin.File)
	info, err := os.Lstat(file)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("evidence is not a regular file")
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	h := sha256.Sum256(raw)
	if hex.EncodeToString(h[:]) != pin.SHA {
		return nil, errors.New("public evidence bytes changed")
	}
	if _, err = uniqueEvidenceJSON(raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func loadExecutionEvidence(root string, m ExecutionManifest) (ExecutionEvidence, error) {
	e := ExecutionEvidence{Assessments: map[string]executionIdentity{}, References: map[string][]ExecutionReference{}, Decisions: map[string]executionIdentity{}}
	files := map[string]bool{}
	for _, pin := range m.Artifacts {
		if files[pin.File] {
			return e, errors.New("repeated evidence artifact")
		}
		files[pin.File] = true
		raw, err := readExecutionArtifact(root, pin)
		if err != nil {
			return e, err
		}
		var a struct {
			Entries []executionIdentity `json:"entries"`
		}
		if err = json.Unmarshal(raw, &a); err != nil {
			return e, err
		}
		if len(a.Entries) != pin.Rows || pin.Rows <= 0 {
			return e, errors.New("assessment row count differs")
		}
		own := map[string]bool{}
		for row, r := range a.Entries {
			if r.ID == "" || own[r.ID] || !evidenceHex(r.SHA, 64) || !evidencePath(r.Path) {
				return e, errors.New("invalid assessment identity")
			}
			own[r.ID] = true
			if r.Commit == "" {
				r.Commit = r.SourceCommit
			}
			if prior, ok := e.Assessments[r.ID]; ok && (prior.SHA != r.SHA || prior.Path != r.Path || prior.Commit != r.Commit) {
				return e, errors.New("conflicting assessment references")
			}
			e.Assessments[r.ID] = r
			e.References[r.ID] = append(e.References[r.ID], ExecutionReference{pin.File, row + 1})
		}
	}
	raw, err := readExecutionArtifact(root, m.Ledger)
	if err != nil {
		return e, err
	}
	var ledger struct {
		Entries  []executionIdentity `json:"entries"`
		Excluded []executionIdentity `json:"excluded_entries"`
	}
	if err = json.Unmarshal(raw, &ledger); err != nil {
		return e, err
	}
	rows := append(ledger.Entries, ledger.Excluded...)
	if len(rows) != m.Ledger.Rows {
		return e, errors.New("ledger row count differs")
	}
	for _, r := range rows {
		if r.Path == "" {
			r.Path = r.SourcePath
		}
		if r.Commit == "" {
			r.Commit = r.SourceCommit
		}
		if r.ID == "" || !evidenceHex(r.SHA, 64) {
			return e, errors.New("invalid ledger identity")
		}
		if _, ok := e.Decisions[r.ID]; ok {
			return e, errors.New("repeated final identity")
		}
		switch r.Disposition {
		case "installed_new_adaptation", "integrated_existing", "excluded":
		default:
			return e, errors.New("unfinished decision in final ledger")
		}
		e.Decisions[r.ID] = r
	}
	return e, nil
}

// Every catalog occurrence is retained. Equal body bytes only point at prior
// evidence; they never transfer a grant or create an exclusion.
func executionQueue(input io.Reader, output io.Writer, m ExecutionManifest, e ExecutionEvidence) (ExecutionSummary, error) {
	s := ExecutionSummary{Stages: map[string]int{}, Decisions: map[string]int{}, Limits: []string{
		"An assessment reference is not a full raw read, licence grant, current adoption or completed acceptance; consult its recorded actor, scope and state.",
		"Equal body bytes do not transfer source ownership, individual permission or support-resource review.",
		"No source body or URL is opened, no model or worker is launched, no permission is changed and nothing is installed.",
		"The queue is complete only when its separate summary is complete and its recorded queue SHA256 matches the actual bytes.",
	}}
	h := sha256.New()
	scanner := bufio.NewScanner(io.TeeReader(input, h))
	scanner.Buffer(make([]byte, 65536), 16<<20)
	enc := json.NewEncoder(output)
	seen, matchedAssessment, matchedDecision := map[string]bool{}, map[string]bool{}, map[string]bool{}
	bodyEvidence := map[string]string{}
	for id, a := range e.Assessments {
		if prior := bodyEvidence[a.SHA]; prior == "" || id < prior {
			bodyEvidence[a.SHA] = id
		}
	}
	for scanner.Scan() {
		var r Record
		if err := json.Unmarshal(scanner.Bytes(), &r); err != nil {
			return s, err
		}
		if r.ID == "" || seen[r.ID] || r.Unit == "" {
			return s, errors.New("invalid or repeated catalog identity")
		}
		seen[r.ID] = true
		s.Records++
		if r.Effective != nil {
			r.SHA, r.BodyRead = r.Effective.SHA, r.Effective.BodyRead
		}
		q := ExecutionRow{Record: r, Stage: "needs_semantic_and_individual_rights_review", Next: "evaluate_fixed_saved_source"}
		if r.Unit == "unread_additional_text_support_shape" {
			if r.BodyRead {
				return s, errors.New("held source claims acquired bytes")
			}
			q.Stage = "held_execution_review"
			q.Next = "preserve_hold_no_fetch_or_alternate_retry"
		} else if !r.BodyRead {
			q.Stage = "held_body_unavailable"
			q.Next = "preserve_recorded_unavailability"
		} else if !evidenceHex(r.SHA, 64) {
			return s, errors.New("available source lacks exact hash")
		}
		if a, ok := e.Assessments[r.ID]; ok {
			if !r.BodyRead || a.SHA != r.SHA || r.Path != "" && a.Path != r.Path || r.Commit != "" && a.Commit != "" && a.Commit != r.Commit {
				return s, errors.New("assessment does not match fixed catalog occurrence")
			}
			matchedAssessment[r.ID] = true
			s.Assessments++
			q.References = e.References[r.ID]
			q.Stage = "assessed_needs_current_disposition"
			q.Next = "resolve_recorded_rights_comparison_and_adaptation_prerequisites"
		} else if r.BodyRead {
			q.RelatedBody = bodyEvidence[r.SHA]
		}
		if d, ok := e.Decisions[r.ID]; ok {
			if !r.BodyRead || d.SHA != r.SHA || r.Path != "" && d.Path != r.Path || r.Commit != "" && d.Commit != "" && d.Commit != r.Commit || r.Repo != "" && d.SourceRepo != "" && r.Unit != "site_index_entry" && d.SourceRepo != r.Repo {
				return s, errors.New("final decision does not match acquired catalog occurrence")
			}
			switch d.Disposition {
			case "installed_new_adaptation", "integrated_existing", "excluded":
			default:
				return s, errors.New("unfinished final disposition")
			}
			matchedDecision[r.ID] = true
			s.Final++
			s.Decisions[d.Disposition]++
			q.Stage = "recorded_final_disposition"
			q.Next = "preserve_existing_acceptance_and_source_evidence"
			q.Decision = d.Disposition
		}
		s.Stages[q.Stage]++
		if err := enc.Encode(q); err != nil {
			return s, err
		}
	}
	if err := scanner.Err(); err != nil {
		return s, err
	}
	s.CatalogSHA = hex.EncodeToString(h.Sum(nil))
	if s.Records != m.Records || s.CatalogSHA != m.CatalogSHA || len(matchedAssessment) != len(e.Assessments) || len(matchedDecision) != len(e.Decisions) {
		return s, errors.New("catalog/evidence universe incomplete or changed")
	}
	s.State = "complete"
	return s, nil
}

func writeExecutionQueue(catalog, manifest, root, output string) error {
	raw, err := os.ReadFile(manifest)
	if err != nil {
		return err
	}
	if _, err = uniqueEvidenceJSON(raw); err != nil {
		return err
	}
	var m ExecutionManifest
	if err = json.Unmarshal(raw, &m); err != nil {
		return err
	}
	if !evidenceHex(m.CatalogSHA, 64) || m.Records <= 0 {
		return errors.New("missing fixed catalog contract")
	}
	evidence, err := loadExecutionEvidence(root, m)
	if err != nil {
		return err
	}
	in, err := os.Open(catalog)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	hash := sha256.New()
	w := bufio.NewWriter(io.MultiWriter(out, hash))
	s, scanErr := executionQueue(in, w, m, evidence)
	flushErr := w.Flush()
	syncErr := out.Sync()
	closeErr := out.Close()
	for _, e := range []error{scanErr, flushErr, syncErr, closeErr} {
		if e != nil {
			return e
		}
	}
	mh := sha256.Sum256(raw)
	s.ManifestSHA = hex.EncodeToString(mh[:])
	s.QueueSHA = hex.EncodeToString(hash.Sum(nil))
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	summary, err := os.OpenFile(output+".summary.json", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, writeErr := summary.Write(data)
	syncErr = summary.Sync()
	closeErr = summary.Close()
	for _, e := range []error{writeErr, syncErr, closeErr} {
		if e != nil {
			return e
		}
	}
	fmt.Printf("execution_queue=%d assessment_records=%d recorded_final=%d; upstream_body_reads=0 new_decisions=0 installs=0\n", s.Records, s.Assessments, s.Final)
	return nil
}
