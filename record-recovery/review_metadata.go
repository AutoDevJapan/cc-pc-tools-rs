package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const reviewMetadataStore = "D:/CC/Tools/todo/todo.json"
const reviewMetadataLimit = 64 << 20
const reviewMetadataID = int64(2191)
const reviewMetadataRequest = "todo29-db-count-review-52aaffb-20261010"

// This is one metadata-only case, not a general store reader or a qualification.
// The production command has no source, target, output or authentication options.
func runReviewMetadataCLI(args []string, out io.Writer, read func() ([]byte, error)) int {
	if len(args) != 0 {
		_ = json.NewEncoder(out).Encode(obj{"ok": false, "code": "review_metadata_invalid_arguments"})
		return 2
	}
	b, err := read()
	if err != nil {
		// Even an injected OS/parser error must not disclose a source path or value.
		_ = json.NewEncoder(out).Encode(obj{"ok": false, "code": "review_metadata_source_unavailable"})
		return 1
	}
	r, err := projectReviewMetadata(b)
	if err != nil {
		_ = json.NewEncoder(out).Encode(obj{"ok": false, "code": err.Error()})
		return 1
	}
	if json.NewEncoder(out).Encode(r) != nil {
		return 1
	}
	return 0
}

func readReviewSnapshot(path string) ([]byte, error) {
	// Reject statically visible links before and after opening. SameFile is a
	// best-effort identity check, not an atomic guard against concurrent changes
	// to ancestors, the leaf or hard-link aliases by the same local user.
	if err := reviewPlainPath(path); err != nil {
		return nil, err
	}
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() || before.Size() > reviewMetadataLimit {
		return nil, errors.New("review_metadata_source_unavailable")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, errors.New("review_metadata_source_unavailable")
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(before, opened) || reviewPlainPath(path) != nil {
		return nil, errors.New("review_metadata_source_unavailable")
	}
	return readReviewBytes(f)
}

func reviewPlainPath(path string) error {
	for p := filepath.Clean(path); ; p = filepath.Dir(p) {
		info, err := os.Lstat(p)
		if err != nil || info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
			return errors.New("review_metadata_source_unavailable")
		}
		if filepath.Dir(p) == p {
			break
		}
	}
	return nil
}

func readReviewBytes(r io.Reader) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, reviewMetadataLimit+1))
	if err != nil || len(b) > reviewMetadataLimit {
		return nil, errors.New("review_metadata_source_unavailable")
	}
	return b, nil
}

func reviewSeparators(b []byte) int {
	quoted, escaped, count := false, false, 0
	for _, c := range b {
		if quoted {
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				quoted = false
			}
		} else if c == '"' {
			quoted = true
		} else if c == ':' {
			count++
		}
	}
	return count
}

func reviewInteger(v any) (int64, bool) {
	n, ok := v.(json.Number)
	if !ok || len(n) == 0 {
		return 0, false
	}
	for _, c := range n.String() {
		if c < '0' || c > '9' {
			return 0, false
		}
	}
	i, err := strconv.ParseInt(n.String(), 10, 64)
	return i, err == nil && i > 0
}

// Never forward untyped data, including strings hidden in an unexpected shape.
func reviewField(m obj, key string, kind string) obj {
	v, exists := m[key]
	if !exists {
		return obj{"state": "absent"}
	}
	if v == nil {
		return obj{"state": "null"}
	}
	r := obj{"state": "invalid"}
	switch kind {
	case "id":
		if i, ok := reviewInteger(v); ok {
			return obj{"state": "recorded", "value": i}
		}
	case "status":
		s, ok := v.(string)
		if ok {
			for _, allowed := range []string{"todo", "doing", "wait", "block", "done", "recovery_unverified"} {
				if s == allowed {
					return obj{"state": "recorded", "value": s}
				}
			}
		}
	case "bool":
		if b, ok := v.(bool); ok {
			return obj{"state": "recorded", "value": b}
		}
	case "sha256":
		if s, ok := v.(string); ok && len(s) == 64 {
			if _, err := hex.DecodeString(s); err == nil {
				return obj{"state": "recorded", "sha256": strings.ToLower(s), "content_verified": false}
			}
		}
	case "reference":
		if s, ok := v.(string); ok && len(s) > 0 && len(s) <= 4096 {
			return obj{"state": "recorded", "reference_opened": false}
		}
	case "time":
		if s, ok := v.(string); ok && len(s) <= 40 {
			for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04"} {
				if _, err := time.Parse(layout, s); err == nil {
					return obj{"state": "recorded", "value": s}
				}
			}
		}
	case "ids":
		if a, ok := v.([]any); ok && len(a) <= 256 {
			ids := []int64{}
			seen := map[int64]bool{}
			for _, value := range a {
				i, ok := reviewInteger(value)
				if !ok || seen[i] {
					return r
				}
				seen[i] = true
				ids = append(ids, i)
			}
			return obj{"state": "recorded", "values": ids}
		}
	case "jobs":
		if a, ok := v.([]any); ok {
			return obj{"state": "recorded", "reference_count": len(a), "references_opened": false}
		}
	}
	return r
}

// These are string facts about one fixed case, not a source identity receipt.
// No label, path, reference digest, or referenced content is returned or opened.
func reviewSourceReference(m obj, key string) obj {
	r := reviewField(m, key, "reference")
	if r["state"] != "recorded" {
		return r
	}
	s, ok := m[key].(string)
	if !ok {
		return obj{"state": "invalid"}
	}
	exact := s == reviewMetadataRequest || s == "manual:"+reviewMetadataRequest
	r["original_request_text_present"] = strings.Contains(s, reviewMetadataRequest)
	r["exact_original_request_text"] = exact
	r["reference_identity_verified"] = false
	r["reference_format"] = "other"
	switch {
	case exact:
		r["reference_format"] = "known_request_text"
	case s == "queue" || s == "queued":
		r["reference_format"] = "queue_marker"
	case strings.HasPrefix(s, "queue:") || strings.HasPrefix(s, "queued:"):
		r["reference_format"] = "queue_prefix"
	default:
		if len(s) < 17 || s[15] != '-' {
			return r
		}
		for _, c := range s[16:] {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
				return r
			}
		}
		stamp, err := time.Parse("20060102-150405", s[:15])
		if err != nil {
			return r
		}
		r["reference_format"] = "job_name"
		r["job_name_time"] = stamp.Format("2006-01-02 15:04:05")
		r["job_name_time_zone"] = "unspecified"
		r["job_existence_verified"] = false
	}
	return r
}

func projectReviewMetadata(b []byte) (obj, error) {
	fail := func(code string) (obj, error) { return nil, errors.New("review_metadata_" + code) }
	if len(b) > reviewMetadataLimit || !utf8.Valid(b) {
		return fail("invalid_source")
	}
	raw := bytes.TrimPrefix(b, []byte{239, 187, 191})
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var db obj
	if d.Decode(&db) != nil || db == nil {
		return fail("invalid_source")
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return fail("invalid_source")
	}
	encoded, err := json.Marshal(db)
	if err != nil || reviewSeparators(raw) != reviewSeparators(encoded) {
		return fail("duplicate_keys")
	}
	items, ok := db["items"].([]any)
	if !ok {
		return fail("invalid_source")
	}
	var selected obj
	for _, value := range items {
		row, ok := value.(map[string]any)
		if !ok {
			return fail("invalid_source")
		}
		id, ok := reviewInteger(row["id"])
		if !ok {
			return fail("invalid_source")
		}
		if id == reviewMetadataID {
			if selected != nil {
				return fail("ambiguous_record")
			}
			selected = row
		}
	}
	if selected == nil {
		return fail("record_missing")
	}
	if selected["project"] != "GoDD-Press" {
		return fail("scope_mismatch")
	}
	r := obj{
		"ok": true, "domain": "original-review-metadata-v1", "case_todo_id": 3255,
		"record_id": reviewMetadataID, "snapshot_sha256": digest(b), "metadata_only": true,
		"live_state_changed": false, "data_access_granted": false, "original_registry_recovered": false,
		"review_completed":           false,
		"status":                     reviewField(selected, "status", "status"),
		"updated":                    reviewField(selected, "updated", "time"),
		"dispatchable":               reviewField(selected, "dispatchable", "bool"),
		"wait_on":                    reviewField(selected, "wait_on", "ids"),
		"jobs":                       reviewField(selected, "jobs", "jobs"),
		"agent_reference":            reviewField(selected, "agent", "reference"),
		"wait_transition_provenance": obj{"verified": false, "reason": "snapshot_is_not_a_transition_receipt"},
		"console_queue_observed":     false,
	}
	if value, exists := selected["recovery"]; !exists || value == nil {
		state := "absent"
		if exists {
			state = "null"
		}
		r["recovery"] = obj{"state": state}
	} else if recovery, ok := value.(map[string]any); ok {
		r["recovery"] = obj{
			"state":                           "recorded",
			"original_id":                     reviewField(recovery, "original_id", "id"),
			"verified_status":                 reviewField(recovery, "verified_status", "status"),
			"request_source_job_id":           reviewSourceReference(recovery, "request_source_job_id"),
			"source_job_sha256":               reviewField(recovery, "source_job_sha256", "sha256"),
			"preserved_worker_request":        reviewField(recovery, "preserved_worker_request", "reference"),
			"preserved_worker_request_sha256": reviewField(recovery, "preserved_worker_request_sha256", "sha256"),
			"verified_at":                     reviewField(recovery, "verified_at", "time"),
			"recovered_at":                    reviewField(recovery, "recovered_at", "time"),
			"original_wait_on":                reviewField(recovery, "original_wait_on", "ids"),
			"verified_wait_on":                reviewField(recovery, "verified_wait_on", "ids"),
		}
	} else {
		r["recovery"] = obj{"state": "invalid"}
	}
	if value, exists := selected["scheduler"]; !exists || value == nil {
		state := "absent"
		if exists {
			state = "null"
		}
		r["scheduler"] = obj{"state": state}
	} else if scheduler, ok := value.(map[string]any); ok {
		r["scheduler"] = obj{"state": "recorded"}
		r["scheduler_job_reference"] = reviewField(scheduler, "job", "reference")
		r["console_request_reference"] = reviewField(scheduler, "console_request", "reference")
	} else {
		r["scheduler"] = obj{"state": "invalid"}
	}
	return r, nil
}
