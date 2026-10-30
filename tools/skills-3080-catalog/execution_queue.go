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
	"regexp"
	"strings"
)

type ExecutionManifest struct {
	CatalogSHA string              `json:"catalog_sha256"`
	Records    int                 `json:"catalog_records"`
	Artifacts  []ExecutionArtifact `json:"assessment_artifacts"`
	Ledger     ExecutionArtifact   `json:"adoption_ledger"`
}
type ExecutionArtifact struct {
	File         string                 `json:"file"`
	SHA          string                 `json:"sha256"`
	Rows         int                    `json:"rows"`
	SiteBindings []ExecutionSiteBinding `json:"site_occurrence_bindings,omitempty"`
}
type executionGitIdentity struct {
	Repo   string `json:"repo"`
	Commit string `json:"commit"`
	Path   string `json:"path"`
}
type ExecutionSiteBinding struct {
	Row        int                   `json:"one_based_row"`
	ID         string                `json:"inventory_id"`
	SHA        string                `json:"source_sha256"`
	Unit       string                `json:"inventory_unit"`
	Role       string                `json:"recorded_identity_role"`
	Recorded   executionGitIdentity  `json:"recorded_git_fields"`
	Occurrence *executionGitIdentity `json:"catalog_occurrence_git_fields"`
}
type ExecutionReference struct {
	File     string                `json:"artifact"`
	Row      int                   `json:"one_based_row"`
	Identity *ExecutionSiteBinding `json:"identity_interpretation,omitempty"`
}
type executionIdentity struct {
	ID           string `json:"inventory_id"`
	SHA          string `json:"source_sha256"`
	Path         string `json:"path"`
	Commit       string `json:"commit"`
	SourceCommit string `json:"source_commit"`
	SourcePath   string `json:"source_path"`
	SourceRepo   string `json:"source_repo"`
	Repo         string `json:"repo"`
	Source       string `json:"source"`
	Disposition  string `json:"disposition"`
	PathKind     string `json:"path_kind"`
	occurrence   *ExecutionSiteBinding
}
type ExecutionEvidence struct {
	Assessments map[string]executionIdentity
	References  map[string][]ExecutionReference
	Decisions   map[string]executionIdentity
}

var executionRepositoryName = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

// Older assessment formats recorded the same per-row repository under repo or
// source. Only an explicit repository-shaped source is an alias; a URL, label,
// artifact name or catalog match is never used to invent missing provenance.
func normalizeExecutionRepository(r *executionIdentity) error {
	aliases := []string{r.Repo}
	if executionRepositoryName.MatchString(r.Source) {
		aliases = append(aliases, r.Source)
	}
	for _, repo := range aliases {
		if repo == "" {
			continue
		}
		if r.SourceRepo != "" && r.SourceRepo != repo {
			return errors.New("conflicting repository fields in evidence row")
		}
		r.SourceRepo = repo
	}
	return nil
}

type ExecutionRow struct {
	Record
	Stage         string                `json:"execution_stage"`
	Next          string                `json:"next_operation"`
	References    []ExecutionReference  `json:"assessment_evidence,omitempty"`
	RelatedBody   string                `json:"related_assessed_body_id,omitempty"`
	Decision      string                `json:"recorded_final_disposition,omitempty"`
	FinalIdentity *ExecutionSiteBinding `json:"recorded_final_identity_interpretation,omitempty"`
}

func executionSiteBindings(pin ExecutionArtifact) (map[int]ExecutionSiteBinding, error) {
	bindings := map[int]ExecutionSiteBinding{}
	for _, b := range pin.SiteBindings {
		if b.Row < 1 || b.Row > pin.Rows || bindings[b.Row].ID != "" || !strings.HasPrefix(b.ID, "site:") || !evidenceHex(b.SHA, 64) || b.Unit != "site_index_entry" || b.Occurrence == nil || *b.Occurrence != (executionGitIdentity{}) {
			return nil, errors.New("invalid explicit site occurrence identity binding")
		}
		bindings[b.Row] = b
	}
	return bindings, nil
}
func bindExecutionSiteIdentity(r *executionIdentity, b ExecutionSiteBinding, final bool) error {
	if b.ID != r.ID || b.SHA != r.SHA || b.Recorded != (executionGitIdentity{r.SourceRepo, r.Commit, r.Path}) {
		return errors.New("site identity binding differs from the original pinned evidence row")
	}
	switch b.Role {
	case "saved_archive_location_not_upstream_git":
		if final || r.SourceRepo != "" || r.Commit != "" || r.PathKind != "fixed_saved_archive_relative_path_not_upstream_path" || r.Path != "site/objects/"+r.SHA+".gz" {
			return errors.New("archive location was conflated with a Git source identity")
		}
	case "legacy_upstream_source_claim_unverified":
		if !executionRepositoryName.MatchString(r.SourceRepo) || !evidenceHex(r.Commit, 40) || !evidencePath(r.Path) || final && r.Disposition != "excluded" {
			return errors.New("unverified historical source claim cannot approve a new adoption")
		}
	default:
		return errors.New("unknown historical site identity role")
	}
	copy := b
	r.occurrence = &copy
	return nil
}
func sameExecutionOccurrence(r Record, a executionIdentity) bool {
	actual := executionGitIdentity{a.SourceRepo, a.Commit, a.Path}
	if a.occurrence != nil {
		if r.Unit != a.occurrence.Unit {
			return false
		}
		actual = *a.occurrence.Occurrence
	}
	return !(strings.HasPrefix(r.ID, "site:") && r.Unit != "site_index_entry") && actual == (executionGitIdentity{r.Repo, r.Commit, r.Path})
}
func sameExecutionAssessmentIdentity(a, b executionIdentity) bool {
	left, right := executionGitIdentity{a.SourceRepo, a.Commit, a.Path}, executionGitIdentity{b.SourceRepo, b.Commit, b.Path}
	if a.occurrence != nil {
		left = *a.occurrence.Occurrence
	}
	if b.occurrence != nil {
		right = *b.occurrence.Occurrence
	}
	// Different saved locations or unverified historical source claims are
	// retained per reference. They do not replace the occurrence's identity.
	return a.SHA == b.SHA && left == right
}

type ExecutionSummary struct {
	State              string         `json:"state"`
	CatalogSHA         string         `json:"catalog_sha256"`
	ManifestSHA        string         `json:"manifest_sha256"`
	QueueSHA           string         `json:"queue_sha256"`
	Records            int            `json:"occurrences"`
	Assessments        int            `json:"occurrences_with_assessment_records"`
	Final              int            `json:"recorded_final_occurrences"`
	AssessmentAndFinal int            `json:"occurrences_with_assessment_and_final_records"`
	FinalOnly          int            `json:"final_records_without_assessment_record"`
	AssessmentOrFinal  int            `json:"occurrences_with_assessment_or_final_records"`
	Stages             map[string]int `json:"stages"`
	Decisions          map[string]int `json:"recorded_final_dispositions"`
	Limits             []string       `json:"limits"`
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
		bindings, err := executionSiteBindings(pin)
		if err != nil {
			return e, err
		}
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
			if r.ID == "" || own[r.ID] || !evidenceHex(r.SHA, 64) {
				return e, errors.New("invalid assessment identity")
			}
			own[r.ID] = true
			if r.Commit == "" {
				r.Commit = r.SourceCommit
			}
			if r.Path == "" {
				r.Path = r.SourcePath
			}
			if r.SourcePath != "" && r.SourcePath != r.Path || r.SourceCommit != "" && r.SourceCommit != r.Commit {
				return e, errors.New("conflicting original path or commit fields")
			}
			if err := normalizeExecutionRepository(&r); err != nil {
				return e, err
			}
			// A site occurrence can retain its genuinely unknown Git tuple.
			// Its resolved upstream is separate evidence, never a replacement
			// for the site's missing repository, commit or path.
			unknownSiteTuple := strings.HasPrefix(r.ID, "site:") && r.SourceRepo == "" && r.Commit == "" && r.Path == ""
			if !unknownSiteTuple && !evidencePath(r.Path) {
				return e, errors.New("invalid assessment path")
			}
			if b, ok := bindings[row+1]; ok {
				if err := bindExecutionSiteIdentity(&r, b, false); err != nil {
					return e, err
				}
			}
			if prior, ok := e.Assessments[r.ID]; ok && !sameExecutionAssessmentIdentity(prior, r) {
				return e, errors.New("conflicting assessment references")
			}
			e.Assessments[r.ID] = r
			e.References[r.ID] = append(e.References[r.ID], ExecutionReference{File: pin.File, Row: row + 1, Identity: r.occurrence})
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
	bindings, err := executionSiteBindings(m.Ledger)
	if err != nil {
		return e, err
	}
	if len(rows) != m.Ledger.Rows {
		return e, errors.New("ledger row count differs")
	}
	for row, r := range rows {
		if r.Path == "" {
			r.Path = r.SourcePath
		}
		if r.Commit == "" {
			r.Commit = r.SourceCommit
		}
		if err := normalizeExecutionRepository(&r); err != nil {
			return e, err
		}
		if r.SourcePath != "" && r.SourcePath != r.Path || r.SourceCommit != "" && r.SourceCommit != r.Commit {
			return e, errors.New("conflicting final path or commit fields")
		}
		if b, ok := bindings[row+1]; ok {
			if err := bindExecutionSiteIdentity(&r, b, true); err != nil {
				return e, err
			}
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
		"Explicit site identity interpretations retain original archive locations and unverified historical upstream claims separately from catalog occurrence Git fields; historical exclusion claims still require reconfirmation.",
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
			if !r.BodyRead || a.SHA != r.SHA || !sameExecutionOccurrence(r, a) {
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
			if !r.BodyRead || d.SHA != r.SHA || !sameExecutionOccurrence(r, d) {
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
			q.FinalIdentity = d.occurrence
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
	for id := range matchedDecision {
		if matchedAssessment[id] {
			s.AssessmentAndFinal++
		} else {
			s.FinalOnly++
		}
	}
	s.AssessmentOrFinal = s.Assessments + s.Final - s.AssessmentAndFinal
	s.Limits = append(s.Limits, fmt.Sprintf("Assessment records %d = assessed without final disposition %d + assessment/final intersection %d. Final records %d include %d final-only legacy records without assessment. The assessment/final union is %d occurrences; these categories are not additive independent totals.", s.Assessments, s.Stages["assessed_needs_current_disposition"], s.AssessmentAndFinal, s.Final, s.FinalOnly, s.AssessmentOrFinal))
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
	if err := link(queue, output); err != nil {
		if !os.IsExist(err) {
			return err
		}
		if err = matchInterruptedExecutionQueue(queue, output, expectedSHA); err != nil {
			return err
		}
	}
	// A published queue is always complete. Retain it on any marker failure:
	// another writer may already have validated it or committed its marker.
	// A later attempt can recover the exact queue using the same SHA check.
	return link(summary, output+".summary.json")
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
