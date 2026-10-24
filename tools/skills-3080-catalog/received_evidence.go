package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"strings"
)

// A cohort is immutable reading evidence. Current adoption decisions live in
// the adoption ledger, independently of the original reader's proposal flags.
type ReceivedCohort struct {
	Source    string          `json:"source"`
	Commit    string          `json:"commit"`
	Tree      string          `json:"tree_sha"`
	Entries   json.RawMessage `json:"entries"`
	EntrySHA  string          `json:"immutable_entries_sha256"`
	InputSHA  string          `json:"independent_received_input_sha256"`
	Counts    map[string]int  `json:"counts"`
	NoCap     *bool           `json:"no_adoption_cap"`
	Executed  *bool           `json:"source_code_executed"`
	Central   int             `json:"central_operations"`
	Decisions int             `json:"new_final_dispositions"`
}
type ReceivedEntry struct {
	ID         string            `json:"inventory_id"`
	Source     string            `json:"source"`
	Commit     string            `json:"source_commit"`
	Path       string            `json:"path"`
	SHA        string            `json:"source_sha256"`
	Blob       string            `json:"git_blob"`
	Bytes      int               `json:"bytes"`
	Reader     string            `json:"reader_job"`
	Todo       int               `json:"reader_todo"`
	Row        int               `json:"reader_row"`
	Raw        *bool             `json:"body_read_complete"`
	Workflow   *bool             `json:"body_workflow_read_complete"`
	Mask       *int              `json:"body_mask_count"`
	Method     string            `json:"primary_read_method"`
	Executed   *bool             `json:"source_code_executed"`
	Installed  *bool             `json:"installed_by_todo3080"`
	Integrated *bool             `json:"integrated_by_todo3080"`
	Excluded   *bool             `json:"excluded_by_todo3080"`
	Use        string            `json:"use"`
	Value      string            `json:"value"`
	Reason     string            `json:"reason"`
	Notices    []ReceivedNotice  `json:"individual_notices"`
	Support    []ReceivedSupport `json:"support_read_states"`
}
type ReceivedSupport struct {
	Path           string          `json:"path"`
	SHA            string          `json:"source_sha256"`
	Blob           string          `json:"git_blob"`
	Bytes          *int            `json:"bytes"`
	Reader         *string         `json:"reader_job"`
	State          string          `json:"state"`
	Saved          *bool           `json:"body_saved"`
	Raw            *bool           `json:"raw_read_complete"`
	Workflow       *bool           `json:"workflow_read_complete"`
	Partial        *bool           `json:"partial_read"`
	Scan           *bool           `json:"scan_only"`
	Executed       *bool           `json:"source_executed"`
	Mask           *int            `json:"masked_value_count"`
	Representative json.RawMessage `json:"representative"`
}
type ReceivedNotice struct {
	Path         string           `json:"path"`
	Copy         *bool            `json:"copy_license_accepted"`
	SourceState  *ReceivedSupport `json:"source_state"`
	State        *ReceivedSupport `json:"state"`
	DefinitionID string           `json:"content_definition"`
	RightsRef    string           `json:"rights_ref"`
}
type ReceivedSpec struct {
	Source   string
	Commit   string
	Tree     string
	EntrySHA string
	InputSHA string
	Readers  map[int]string
	Counts   map[string]int
}

var received50Spec = ReceivedSpec{
	Source: "6mile/skills", Commit: "920714f17d81e0a95c7a9bc51faf0a7921cd982d", Tree: "5782ae7deffe5024e15b6e090327385c10f76273",
	EntrySHA: "3e17ae8e08437fbab19f48c06e10fff6bbe704cee2358e8415c6489e23d17003",
	InputSHA: "3443256d3174544b06c0fed07e923640f30e7e5b7f82a6623298dd38b8500c65",
	Readers:  map[int]string{3647: "20261024-211840-todo-3647", 3663: "20261024-223825-todo-3663"},
	Counts:   map[string]int{"candidates": 50, "primary_raw": 26, "primary_masked": 24, "primary_workflow": 50, "primary_ordered_differences_reported": 5, "notices": 51, "notice_content_definitions_by_actor": 33, "support": 261, "saved": 175, "direct_raw_support": 106, "direct_masked_support": 47, "same_byte_notice_reuse": 18, "ordered_notice_difference": 1, "full_workflow_support": 172, "partial": 3, "scan": 0, "saved_unread": 0, "unsaved": 86},
}

func validateReceivedCohort(a ReceivedCohort, spec ReceivedSpec) ([]ReceivedEntry, error) {
	bad := func(reason string) ([]ReceivedEntry, error) { return nil, errors.New("received cohort: " + reason) }
	if a.Source != spec.Source || a.Commit != spec.Commit || a.Tree != spec.Tree || a.EntrySHA != spec.EntrySHA || a.InputSHA != spec.InputSHA {
		return bad("fixed source or independent input differs")
	}
	value, err := uniqueEvidenceJSON(a.Entries)
	if err != nil {
		return bad("ambiguous JSON")
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return bad("canonical evidence")
	}
	h := sha256.Sum256(canonical)
	if hex.EncodeToString(h[:]) != spec.EntrySHA {
		return bad("immutable reading evidence differs")
	}
	if !evidenceTrue(a.NoCap) || !evidenceFalse(a.Executed) || a.Central != 0 || a.Decisions != 0 {
		return bad("reading promoted to execution or disposition")
	}
	var entries []ReceivedEntry
	if err = json.Unmarshal(a.Entries, &entries); err != nil {
		return bad("entry shape")
	}
	counts := map[string]int{}
	ids, paths, rows := map[string]bool{}, map[string]bool{}, map[string]bool{}
	priorRaw := map[string]bool{}
	noticeDefinitions := map[string]bool{}
	readers := map[int]int{}
	for _, e := range entries {
		if !strings.HasPrefix(e.ID, "github:") || !evidenceHex(strings.TrimPrefix(e.ID, "github:"), 64) || ids[e.ID] || paths[e.Path] || !evidencePath(e.Path) || e.Source != a.Source || e.Commit != a.Commit || !evidenceHex(e.SHA, 64) || !evidenceHex(e.Blob, 40) || e.Bytes <= 0 {
			return bad("entry identity or namespace")
		}
		ids[e.ID], paths[e.Path] = true, true
		actor, ok := spec.Readers[e.Todo]
		if !ok || actor != e.Reader || e.Row <= 0 || rows[fmt.Sprint(e.Todo, ":", e.Row)] {
			return bad("reader binding")
		}
		rows[fmt.Sprint(e.Todo, ":", e.Row)] = true
		readers[e.Todo]++
		if !evidenceTrue(e.Workflow) || e.Raw == nil || !evidenceFalse(e.Executed) || !evidenceFalse(e.Installed) || !evidenceFalse(e.Integrated) || !evidenceFalse(e.Excluded) || e.Use == "" || e.Value == "" || e.Reason == "" {
			return bad("incomplete semantic reading or historical decision flags")
		}
		counts["candidates"]++
		counts["primary_workflow"]++
		if *e.Raw {
			if e.Mask == nil || *e.Mask != 0 {
				return bad("raw primary is masked")
			}
			counts["primary_raw"]++
		} else {
			if e.Mask != nil && *e.Mask <= 0 {
				return bad("masked primary claims zero masks")
			}
			counts["primary_masked"]++
		}
		if strings.HasPrefix(e.Method, "diff") {
			counts["primary_ordered_differences_reported"]++
		}
		supportByPath := map[string]ReceivedSupport{}
		for _, s := range e.Support {
			if !evidencePath(s.Path) || supportByPath[s.Path].Path != "" || !evidenceHex(s.Blob, 40) || s.Saved == nil || s.Raw == nil || s.Workflow == nil || !evidenceFalse(s.Executed) {
				return bad("support identity or explicit state")
			}
			supportByPath[s.Path] = s
			counts["support"]++
			if !*s.Saved {
				if s.State != "U" && s.State != "UN" || s.Bytes != nil || s.SHA != "" || s.Reader != nil || s.Mask != nil || *s.Raw || *s.Workflow || evidenceTrue(s.Partial) || evidenceTrue(s.Scan) {
					return bad("unsaved support has invented bytes or reading")
				}
				counts["unsaved"]++
				continue
			}
			if s.Bytes == nil || *s.Bytes < 0 || !evidenceHex(s.SHA, 64) || s.Reader == nil || *s.Reader != e.Reader {
				return bad("saved support lacks byte identity or actor")
			}
			counts["saved"]++
			key := fmt.Sprint(e.Reader, "|", s.SHA, "|", s.Blob, "|", *s.Bytes)
			switch s.State {
			case "FR", "R":
				if !*s.Raw || !*s.Workflow || s.Mask == nil || *s.Mask != 0 || evidenceTrue(s.Partial) || evidenceTrue(s.Scan) {
					return bad("raw support state differs")
				}
				counts["direct_raw_support"]++
				priorRaw[key] = true
			case "FW", "W":
				if *s.Raw || !*s.Workflow || s.Mask != nil && *s.Mask <= 0 || evidenceTrue(s.Partial) || evidenceTrue(s.Scan) {
					return bad("masked support state differs")
				}
				counts["direct_masked_support"]++
			case "RU", "I":
				if *s.Raw || !*s.Workflow || !priorRaw[key] || len(s.Representative) == 0 || bytes.Equal(bytes.TrimSpace(s.Representative), []byte("null")) || s.Mask == nil || *s.Mask != 0 || evidenceTrue(s.Partial) || evidenceTrue(s.Scan) || !noticeEvidencePath(s.Path) {
					return bad("reuse lacks earlier same-reader whole-byte notice")
				}
				counts["same_byte_notice_reuse"]++
			case "DF":
				if *s.Raw || !*s.Workflow || s.Mask == nil || *s.Mask != 0 || evidenceTrue(s.Partial) || evidenceTrue(s.Scan) || !noticeEvidencePath(s.Path) {
					return bad("reported ordered notice difference promoted to raw")
				}
				counts["ordered_notice_difference"]++
			case "PT":
				if *s.Raw || *s.Workflow || !evidenceTrue(s.Partial) || evidenceTrue(s.Scan) {
					return bad("partial support promoted to full")
				}
				counts["partial"]++
			case "SC":
				if *s.Raw || *s.Workflow || !evidenceTrue(s.Scan) || evidenceTrue(s.Partial) {
					return bad("scan promoted to full")
				}
				counts["scan"]++
			case "NR":
				if *s.Raw || *s.Workflow || evidenceTrue(s.Partial) || evidenceTrue(s.Scan) {
					return bad("unread support promoted to reading")
				}
				counts["saved_unread"]++
			default:
				return bad("unknown support state")
			}
			if *s.Workflow {
				counts["full_workflow_support"]++
			}
		}
		for _, n := range e.Notices {
			definition := n.DefinitionID
			if definition == "" {
				definition = n.RightsRef
			}
			if definition == "" {
				return bad("missing individual notice definition")
			}
			noticeDefinitions[e.Reader+"|"+definition] = true
			state := n.SourceState
			if state == nil {
				state = n.State
			} else if n.State != nil {
				return bad("ambiguous notice state aliases")
			}
			s, ok := supportByPath[n.Path]
			if state == nil || !ok || !noticeEvidencePath(n.Path) || !evidenceFalse(n.Copy) || !equalEvidenceSupport(*state, s) {
				return bad(fmt.Sprintf("individual notice namespace or copied permission differs: reader%d row%d", e.Todo, e.Row))
			}
			counts["notices"]++
		}
	}
	counts["notice_content_definitions_by_actor"] = len(noticeDefinitions)
	for todo, n := range readers {
		for row := 1; row <= n; row++ {
			if !rows[fmt.Sprint(todo, ":", row)] {
				return bad("noncontiguous original reader rows")
			}
		}
	}
	for key, want := range spec.Counts {
		got, ok := a.Counts[key]
		if !ok || got != want || counts[key] != want {
			return bad("derived counter differs: " + key)
		}
	}
	return entries, nil
}

func evidenceTrue(v *bool) bool  { return v != nil && *v }
func evidenceFalse(v *bool) bool { return v != nil && !*v }
func evidenceHex(v string, n int) bool {
	if len(v) != n {
		return false
	}
	_, err := hex.DecodeString(v)
	return err == nil && strings.ToLower(v) == v
}
func evidencePath(v string) bool {
	return v != "" && path.Clean(v) == v && !path.IsAbs(v) && v != ".." && !strings.HasPrefix(v, "../") && !strings.ContainsAny(v, "\\:\x00\r\n")
}
func noticeEvidencePath(v string) bool {
	leaf := strings.ToLower(path.Base(v))
	return strings.HasPrefix(leaf, "license") || strings.HasPrefix(leaf, "licence") || strings.HasPrefix(leaf, "notice") || leaf == "copying"
}
func equalEvidenceSupport(a, b ReceivedSupport) bool {
	ar, br := a.Representative, b.Representative
	a.Representative, b.Representative = nil, nil
	if !reflect.DeepEqual(a, b) {
		return false
	}
	if len(ar) == 0 || len(br) == 0 {
		return len(ar) == len(br)
	}
	av, ae := uniqueEvidenceJSON(ar)
	bv, be := uniqueEvidenceJSON(br)
	return ae == nil && be == nil && reflect.DeepEqual(av, bv)
}

// Preserve numbers and nulls, reject duplicate keys before canonicalization,
// and consume exactly one value from the same bytes that are hashed.
func uniqueEvidenceJSON(raw []byte) (any, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var read func(int) (any, error)
	read = func(depth int) (any, error) {
		if depth > 128 {
			return nil, errors.New("evidence depth")
		}
		tok, err := d.Token()
		if err != nil {
			return nil, err
		}
		switch tok {
		case json.Delim('{'):
			m := map[string]any{}
			for d.More() {
				key, err := d.Token()
				if err != nil {
					return nil, err
				}
				s, ok := key.(string)
				if !ok {
					return nil, errors.New("object key")
				}
				if _, exists := m[s]; exists {
					return nil, errors.New("duplicate key")
				}
				v, err := read(depth + 1)
				if err != nil {
					return nil, err
				}
				m[s] = v
			}
			if _, err = d.Token(); err != nil {
				return nil, err
			}
			return m, nil
		case json.Delim('['):
			a := []any{}
			for d.More() {
				v, err := read(depth + 1)
				if err != nil {
					return nil, err
				}
				a = append(a, v)
			}
			if _, err = d.Token(); err != nil {
				return nil, err
			}
			return a, nil
		case json.Delim('}'), json.Delim(']'):
			return nil, errors.New("unexpected delimiter")
		default:
			return tok, nil
		}
	}
	v, err := read(0)
	if err != nil {
		return nil, err
	}
	if _, err = d.Token(); err != io.EOF {
		return nil, errors.New("trailing JSON")
	}
	return v, nil
}

type ReceivedProjection struct {
	Artifact    string         `json:"artifact"`
	ArtifactSHA string         `json:"artifact_sha256"`
	EntrySHA    string         `json:"entry_sha256"`
	Source      string         `json:"source"`
	Commit      string         `json:"commit"`
	Tree        string         `json:"tree_sha"`
	Counters    map[string]int `json:"counters"`
}

func projectReceivedEvidence(raw []byte, artifact string, spec ReceivedSpec) (ReceivedProjection, error) {
	var p ReceivedProjection
	if _, err := uniqueEvidenceJSON(raw); err != nil {
		return p, err
	}
	var a ReceivedCohort
	if err := json.Unmarshal(raw, &a); err != nil {
		return p, err
	}
	entries, err := validateReceivedCohort(a, spec)
	if err != nil {
		return p, err
	}
	h := sha256.Sum256(raw)
	p = ReceivedProjection{Artifact: artifact, ArtifactSHA: hex.EncodeToString(h[:]), EntrySHA: a.EntrySHA, Source: a.Source, Commit: a.Commit, Tree: a.Tree, Counters: map[string]int{}}
	c := p.Counters
	mapping := map[string]string{
		"candidate_paths": "candidates", "raw_unmasked_primary_full_read": "primary_raw", "workflow_primary_full_read": "primary_workflow", "masked_workflow_primary": "primary_masked",
		"support": "support", "support_saved": "saved", "support_raw_unmasked_full_read": "direct_raw_support", "support_masked_workflow_full_read": "direct_masked_support", "support_workflow_full_read": "full_workflow_support", "support_scan_only": "scan", "support_partial_read": "partial", "support_saved_unread": "saved_unread", "support_unsaved": "unsaved",
	}
	for to, from := range mapping {
		c[to] = a.Counts[from]
	}
	c["support_unmasked_full_via_comparison_or_reuse"] = a.Counts["same_byte_notice_reuse"] + a.Counts["ordered_notice_difference"]
	c["metadata_only_primary"], c["comparison_only_primary"] = 0, 0
	// Actual entry enumeration is checked independently of the declared counters.
	if c["candidate_paths"] != len(entries) {
		return ReceivedProjection{}, errors.New("projection row count")
	}
	return p, nil
}

func writeReceivedProjection(input, output string) error {
	raw, err := os.ReadFile(input)
	if err != nil {
		return err
	}
	p, err := projectReceivedEvidence(raw, filepath.Base(input), received50Spec)
	if err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	f, err := os.OpenFile(output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, writeErr := f.Write(encoded)
	syncErr := f.Sync()
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	if syncErr != nil {
		return syncErr
	}
	if closeErr != nil {
		return closeErr
	}
	fmt.Printf("validated_received=%d raw=%d workflow=%d; upstream_reads=0 new_decisions=0 central_writes=0\n", p.Counters["candidate_paths"], p.Counters["raw_unmasked_primary_full_read"], p.Counters["workflow_primary_full_read"])
	return nil
}
