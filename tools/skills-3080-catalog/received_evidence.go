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
	// Rich snapshots retain the original unread/partial/reused primary state.
	// These fields do not change the stricter historical full-read cohorts.
	OriginalRead   []json.RawMessage `json:"original_read_tuple"`
	OriginalActor  string            `json:"original_actor_namespace"`
	Original       json.RawMessage   `json:"original_reader_assessment"`
	PrimaryPartial *bool             `json:"partial_read"`
	PrimaryUnread  *bool             `json:"saved_unread_primary"`
	Copy           *bool             `json:"copy_license_accepted"`
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
	Source         string
	Commit         string
	Tree           string
	EntrySHA       string
	InputSHA       string
	Readers        map[int]string
	Counts         map[string]int
	RichPrimaries  bool
	FirstReaderRow map[int]int
	// These anchors are fixed accepted literal observations, not caller fields.
	// They seed provenance only and never add a new reading to this cohort.
	PriorLiteralSupports []receivedLiteralSupport
}

var received50Spec = ReceivedSpec{
	Source: "6mile/skills", Commit: "920714f17d81e0a95c7a9bc51faf0a7921cd982d", Tree: "5782ae7deffe5024e15b6e090327385c10f76273",
	EntrySHA: "3e17ae8e08437fbab19f48c06e10fff6bbe704cee2358e8415c6489e23d17003",
	InputSHA: "3443256d3174544b06c0fed07e923640f30e7e5b7f82a6623298dd38b8500c65",
	Readers:  map[int]string{3647: "20261024-211840-todo-3647", 3663: "20261024-223825-todo-3663"},
	Counts:   map[string]int{"candidates": 50, "primary_raw": 26, "primary_masked": 24, "primary_workflow": 50, "primary_ordered_differences_reported": 5, "notices": 51, "notice_content_definitions_by_actor": 33, "support": 261, "saved": 175, "direct_raw_support": 106, "direct_masked_support": 47, "same_byte_notice_reuse": 18, "ordered_notice_difference": 1, "full_workflow_support": 172, "partial": 3, "scan": 0, "saved_unread": 0, "unsaved": 86},
}

var received3674Spec = ReceivedSpec{
	Source: "6mile/skills", Commit: "920714f17d81e0a95c7a9bc51faf0a7921cd982d", Tree: "5782ae7deffe5024e15b6e090327385c10f76273",
	EntrySHA: "79fef1a1db9c714643dd47b4c6f73ecd416191a5bda412738ae3659919f0ef59",
	InputSHA: "93ffb14c7401daeb9cc7ba3f655a879935003b0ab4065667ee41492fc2f9cda7",
	Readers:  map[int]string{3674: "20261024-231416-todo-3674"},
	Counts:   map[string]int{"candidates": 50, "primary_raw": 14, "primary_masked": 36, "primary_workflow": 50, "primary_ordered_differences_reported": 0, "notices": 51, "notice_content_definitions_by_actor": 33, "support": 449, "saved": 284, "direct_raw_support": 47, "direct_masked_support": 12, "same_byte_notice_reuse": 18, "ordered_notice_difference": 0, "full_workflow_support": 77, "partial": 0, "scan": 0, "saved_unread": 207, "unsaved": 165},
}

var receivedStream90Spec = ReceivedSpec{
	Source: "6mile/skills", Commit: "920714f17d81e0a95c7a9bc51faf0a7921cd982d", Tree: "5782ae7deffe5024e15b6e090327385c10f76273",
	EntrySHA:      "ed467d85e91aeddc3f0495df0b9cc5c4f574d5f5bb8a66a3b00e9d37d98884a4",
	InputSHA:      "661c6b70142269294d7191b1d2fc06c4a54a85ef6cab9501c233d6e8bd55044c",
	Readers:       map[int]string{3719: "20261025-041905-todo-3719"},
	RichPrimaries: true,
	Counts:        map[string]int{"candidates": 90, "primary_raw": 2, "primary_masked": 85, "primary_direct_masked": 84, "primary_exact_reuse": 1, "primary_workflow": 87, "primary_saved_unread": 1, "primary_partial": 2, "primary_ordered_differences_reported": 0, "notices": 90, "notice_content_definitions_by_actor": 4, "support": 445, "saved": 328, "direct_raw_support": 17, "direct_masked_support": 99, "same_byte_notice_reuse": 86, "same_byte_support_reuse": 3, "ordered_notice_difference": 0, "full_workflow_support": 205, "partial": 0, "scan": 0, "saved_unread": 123, "unsaved": 117},
}

var receivedNext42Spec = ReceivedSpec{
	Source: "6mile/skills", Commit: "920714f17d81e0a95c7a9bc51faf0a7921cd982d", Tree: "5782ae7deffe5024e15b6e090327385c10f76273",
	EntrySHA: "1750eea9807800038ebf87239cd622b2ba14d9d1aecbe7e6390e51dccdb0e152", InputSHA: "0d9fa38449f2f89af5c4d3146aea5f9028812b52329cb9ac87e030ba3900817b",
	Readers: map[int]string{3719: "20261025-041905-todo-3719"}, RichPrimaries: true, FirstReaderRow: map[int]int{3719: 91},
	Counts:               map[string]int{"candidates": 42, "primary_raw": 0, "primary_masked": 39, "primary_direct_masked": 39, "primary_exact_reuse": 0, "primary_workflow": 39, "primary_saved_unread": 2, "primary_partial": 1, "primary_ordered_differences_reported": 0, "notices": 42, "notice_content_definitions_by_actor": 3, "support": 151, "saved": 96, "direct_raw_support": 5, "direct_masked_support": 45, "same_byte_notice_reuse": 40, "same_byte_support_reuse": 0, "ordered_notice_difference": 0, "full_workflow_support": 90, "partial": 0, "scan": 0, "saved_unread": 6, "unsaved": 55},
	PriorLiteralSupports: []receivedLiteralSupport{acceptedStream90NoticeLiteral()},
}

func acceptedStream90NoticeLiteral() receivedLiteralSupport {
	yes, no, n, mask, reader := true, false, 1072, 0, "20261025-041905-todo-3719"
	return receivedLiteralSupport{Row: 10, CohortSHA: "ed467d85e91aeddc3f0495df0b9cc5c4f574d5f5bb8a66a3b00e9d37d98884a4", Support: ReceivedSupport{Path: "skills/bobbyzzhao/gmail-1-0-6/LICENSE.txt", SHA: "ec9e952fec4d029fab649746d033bf91802b15f59ffa40a0d3a3a0e1a1587802", Blob: "4813de20b72e365983fe0370d79d7cac6ea21d9b", Bytes: &n, Reader: &reader, State: "R", Saved: &yes, Raw: &yes, Workflow: &yes, Partial: &no, Scan: &no, Executed: &no, Mask: &mask}}
}

type ReceivedRegistration struct {
	ID       string
	Artifact string
	Spec     ReceivedSpec
}

var receivedRegistrations = []ReceivedRegistration{
	{"received50", "skills-3080-sixmile-received50-evaluation.json", received50Spec},
	{"received3674", "skills-3080-sixmile-received-next50-evaluation.json", received3674Spec},
	{"stream90", "skills-3080-sixmile-stream90-evaluation.json", receivedStream90Spec},
	{"next42", "skills-3080-sixmile-next42-evaluation.json", receivedNext42Spec},
}

func registeredReceivedSpec(a ReceivedCohort) (ReceivedSpec, error) {
	for _, r := range receivedRegistrations {
		if a.InputSHA == r.Spec.InputSHA && a.EntrySHA == r.Spec.EntrySHA {
			return r.Spec, nil
		}
	}
	return ReceivedSpec{}, errors.New("unregistered independent reading evidence")
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
	priorLiteralSupports := map[string]receivedLiteralSupport{}
	for _, anchor := range spec.PriorLiteralSupports {
		s := anchor.Support
		if !spec.RichPrimaries || !evidenceHex(anchor.CohortSHA, 64) || anchor.Row < 1 || s.Reader == nil || s.Bytes == nil || !evidenceTrue(s.Raw) || !evidenceTrue(s.Workflow) || !evidenceTrue(s.Saved) || !evidencePath(s.Path) || !evidenceHex(s.SHA, 64) || !evidenceHex(s.Blob, 40) {
			return bad("invalid fixed prior literal anchor")
		}
		priorRaw[fmt.Sprint(*s.Reader, "|", s.SHA, "|", s.Blob, "|", *s.Bytes)] = true
		priorLiteralSupports[*s.Reader+"|"+s.Path] = anchor
	}
	priorPrimaries := map[int]ReceivedEntry{}
	noticeDefinitions := map[string]bool{}
	readers := map[int]int{}
	for _, e := range entries {
		if spec.RichPrimaries {
			originalRows, ok := value.([]any)
			if !ok || len(originalRows) != len(entries) {
				return bad("rich entry array shape")
			}
			m, ok := originalRows[counts["candidates"]].(map[string]any)
			if !ok {
				return bad("rich entry object shape")
			}
			for _, key := range []string{"reader_job", "body_mask_count", "body_read_complete", "body_workflow_read_complete", "partial_read", "saved_unread_primary", "copy_license_accepted"} {
				if _, present := m[key]; !present {
					return bad("missing explicit primary role/state: " + key)
				}
				for supplied := range m {
					if supplied != key && strings.EqualFold(supplied, key) {
						return bad("ambiguous primary role/state alias")
					}
				}
			}
		}
		if !strings.HasPrefix(e.ID, "github:") || !evidenceHex(strings.TrimPrefix(e.ID, "github:"), 64) || ids[e.ID] || paths[e.Path] || !evidencePath(e.Path) || e.Source != a.Source || e.Commit != a.Commit || !evidenceHex(e.SHA, 64) || !evidenceHex(e.Blob, 40) || e.Bytes <= 0 {
			return bad("entry identity or namespace")
		}
		ids[e.ID], paths[e.Path] = true, true
		actor, ok := spec.Readers[e.Todo]
		if !ok || actor == "" || (!spec.RichPrimaries && actor != e.Reader) || e.Row <= 0 || rows[fmt.Sprint(e.Todo, ":", e.Row)] {
			return bad("reader binding")
		}
		rows[fmt.Sprint(e.Todo, ":", e.Row)] = true
		readers[e.Todo]++
		if (!spec.RichPrimaries && !evidenceTrue(e.Workflow)) || e.Raw == nil || !evidenceFalse(e.Executed) || !evidenceFalse(e.Installed) || !evidenceFalse(e.Integrated) || !evidenceFalse(e.Excluded) || e.Use == "" || e.Value == "" || e.Reason == "" {
			return bad("incomplete semantic reading or historical decision flags")
		}
		counts["candidates"]++
		if spec.RichPrimaries {
			if err := countRichReceivedPrimary(e, actor, priorPrimaries, counts); err != nil {
				return bad(err.Error())
			}
		} else if *e.Raw {
			counts["primary_workflow"]++
			if e.Mask == nil || *e.Mask != 0 {
				return bad("raw primary is masked")
			}
			counts["primary_raw"]++
		} else {
			counts["primary_workflow"]++
			if e.Mask != nil && *e.Mask <= 0 {
				return bad("masked primary claims zero masks")
			}
			counts["primary_masked"]++
		}
		priorPrimaries[e.Row] = e
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
			if s.Bytes == nil || *s.Bytes < 0 || !evidenceHex(s.SHA, 64) {
				return bad("saved support lacks byte identity or actor")
			}
			counts["saved"]++
			if s.State == "NR" {
				if s.Reader != nil || s.Mask != nil || *s.Raw || *s.Workflow || evidenceTrue(s.Partial) || evidenceTrue(s.Scan) {
					return bad("unread support has invented reader, masks or reading")
				}
				counts["saved_unread"]++
				continue
			}
			if s.Reader == nil || *s.Reader != actor {
				return bad("read support lacks the actual reader")
			}
			key := fmt.Sprint(actor, "|", s.SHA, "|", s.Blob, "|", *s.Bytes)
			switch s.State {
			case "FR", "R":
				if !*s.Raw || !*s.Workflow || s.Mask == nil || *s.Mask != 0 || evidenceTrue(s.Partial) || evidenceTrue(s.Scan) {
					return bad("raw support state differs")
				}
				counts["direct_raw_support"]++
				priorRaw[key] = true
				priorLiteralSupports[actor+"|"+s.Path] = receivedLiteralSupport{Row: e.Row, Support: s}
			case "FW", "W":
				if *s.Raw || !*s.Workflow || s.Mask != nil && *s.Mask <= 0 || evidenceTrue(s.Partial) || evidenceTrue(s.Scan) {
					return bad("masked support state differs")
				}
				counts["direct_masked_support"]++
			case "RU", "I":
				if *s.Raw || !*s.Workflow || !priorRaw[key] || len(s.Representative) == 0 || bytes.Equal(bytes.TrimSpace(s.Representative), []byte("null")) || s.Mask == nil || *s.Mask != 0 || evidenceTrue(s.Partial) || evidenceTrue(s.Scan) || (!spec.RichPrimaries && !noticeEvidencePath(s.Path)) {
					return bad("reuse lacks earlier same-reader whole-byte notice")
				}
				if spec.RichPrimaries && !validateRichSupportReuse(s, actor, e.Row, priorLiteralSupports) {
					return bad("support reuse lacks its exact earlier literal actor/source")
				}
				if noticeEvidencePath(s.Path) {
					counts["same_byte_notice_reuse"]++
				} else {
					counts["same_byte_support_reuse"]++
				}
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
			noticeDefinitions[actor+"|"+definition] = true
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
		first := spec.FirstReaderRow[todo]
		if first == 0 {
			first = 1
		}
		for row := first; row < first+n; row++ {
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
		"candidate_paths": "candidates", "raw_unmasked_primary_full_read": "primary_raw", "workflow_primary_full_read": "primary_workflow", "masked_workflow_primary": "primary_masked", "metadata_only_primary": "primary_saved_unread", "partial_primary": "primary_partial",
		"support": "support", "support_saved": "saved", "support_raw_unmasked_full_read": "direct_raw_support", "support_masked_workflow_full_read": "direct_masked_support", "support_workflow_full_read": "full_workflow_support", "support_scan_only": "scan", "support_partial_read": "partial", "support_saved_unread": "saved_unread", "support_unsaved": "unsaved",
	}
	for to, from := range mapping {
		c[to] = a.Counts[from]
	}
	c["support_unmasked_full_via_comparison_or_reuse"] = a.Counts["same_byte_notice_reuse"] + a.Counts["same_byte_support_reuse"] + a.Counts["ordered_notice_difference"]
	c["comparison_only_primary"] = 0
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
	var a ReceivedCohort
	if _, err = uniqueEvidenceJSON(raw); err != nil {
		return err
	}
	if err = json.Unmarshal(raw, &a); err != nil {
		return err
	}
	spec, err := registeredReceivedSpec(a)
	if err != nil {
		return err
	}
	p, err := projectReceivedEvidence(raw, filepath.Base(input), spec)
	if err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	f, err := os.CreateTemp(filepath.Dir(output), ".received-projection-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
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
	if err = os.Link(f.Name(), output); err != nil {
		return err
	}
	fmt.Printf("validated_received=%d raw=%d workflow=%d; upstream_reads=0 new_decisions=0 central_writes=0\n", p.Counters["candidate_paths"], p.Counters["raw_unmasked_primary_full_read"], p.Counters["workflow_primary_full_read"])
	return nil
}
