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
			if !r.BodyRead || a.SHA != r.SHA || r.Path != "" && a.Path != r.Path || r.Commit != "" && a.Commit != r.Commit {
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
			if !r.BodyRead || d.SHA != r.SHA || r.Path != "" && d.Path != r.Path || r.Commit != "" && d.Commit != r.Commit || r.Repo != "" && d.SourceRepo != "" && r.Unit != "site_index_entry" && d.SourceRepo != r.Repo {
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
	// The summary is the commit marker. Existing complete pairs are immutable.
	if _, err := os.Lstat(output + ".summary.json"); err == nil {
		return errors.New("execution summary already exists")
	} else if !os.IsNotExist(err) {
		return err
	}
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
	out, err := os.CreateTemp(filepath.Dir(output), "."+filepath.Base(output)+".pending-*")
	if err != nil {
		return err
	}
	defer os.Remove(out.Name())
	defer out.Close()
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
	summary, err := os.CreateTemp(filepath.Dir(output), "."+filepath.Base(output)+".summary-pending-*")
	if err != nil {
		return err
	}
	defer os.Remove(summary.Name())
	defer summary.Close()
	_, writeErr := summary.Write(data)
	syncErr = summary.Sync()
	closeErr = summary.Close()
	for _, e := range []error{writeErr, syncErr, closeErr} {
		if e != nil {
			return e
		}
	}
	if err = publishExecutionPair(out.Name(), summary.Name(), output, s.QueueSHA, os.Link); err != nil {
		return err
	}
	fmt.Printf("execution_queue=%d assessment_records=%d recorded_final=%d; upstream_body_reads=0 new_decisions=0 installs=0\n", s.Records, s.Assessments, s.Final)
	return nil
}

// Both staged files are complete, synced and closed before publishing either.
// Hard links provide a no-replace publication on Windows and Unix. A process
// stop between publications leaves a complete queue without a commit marker;
// a retry may finish that exact pair after comparing the existing queue bytes.
func publishExecutionPair(queue, summary, output, expectedSHA string, link func(string, string) error) error {
	if _, err := os.Lstat(output + ".summary.json"); err == nil {
		return errors.New("execution summary already exists")
	} else if !os.IsNotExist(err) {
		return err
	}
	created := false
	if err := link(queue, output); err == nil {
		created = true
	} else if !os.IsExist(err) {
		return err
	} else if err = matchInterruptedExecutionQueue(queue, output, expectedSHA); err != nil {
		return err
	}
	if err := link(summary, output+".summary.json"); err != nil {
		// Another writer may have completed the same queue while this call
		// was preparing its marker. Its committed pair must remain intact.
		if _, markerErr := os.Lstat(output + ".summary.json"); !os.IsNotExist(markerErr) {
			return err
		}
		// Remove only the queue this call published, while the stage still pins
		// its identity. Never remove a preexisting or concurrently replaced file.
		if created {
			staged, stageErr := os.Stat(queue)
			published, publishedErr := os.Lstat(output)
			if stageErr == nil && publishedErr == nil && os.SameFile(staged, published) {
				if cleanupErr := os.Remove(output); cleanupErr != nil {
					return errors.Join(err, cleanupErr)
				}
			}
		}
		return err
	}
	return nil
}

func matchInterruptedExecutionQueue(stage, output, expectedSHA string) error {
	old, err := os.Lstat(output)
	if err != nil {
		return err
	}
	staged, err := os.Stat(stage)
	if err != nil {
		return err
	}
	if !old.Mode().IsRegular() || old.Size() != staged.Size() {
		return errors.New("existing execution queue is not the complete expected output")
	}
	f, err := os.Open(output)
	if err != nil {
		return err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(old, opened) {
		return errors.New("existing execution queue identity changed")
	}
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return err
	}
	if hex.EncodeToString(h.Sum(nil)) != expectedSHA {
		return errors.New("existing execution queue bytes differ")
	}
	return nil
}
