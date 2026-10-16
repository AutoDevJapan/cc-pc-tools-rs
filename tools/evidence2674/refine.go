package main

// Refine saved witnesses only. Historical commands are never evaluated.
import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var canonicalChild = regexp.MustCompile(`(?s)^\s*(?:text|notify)\(\s*await\s+tools\.exec_command\((.*)\)\s*\)\s*;?\s*$`)

// Decode a bounded JS string literal without evaluating it. Unicode/hex escape
// dialects and template interpolation remain unresolved instead of guessing.
func staticString(text string, start int) (string, int, bool) {
	quote := text[start]
	if quote == '"' {
		for i := start + 1; i < len(text); i++ {
			if text[i] == '\\' {
				i++
				continue
			}
			if text[i] == '"' {
				var value string
				if json.Unmarshal([]byte(text[start:i+1]), &value) != nil {
					return "", 0, false
				}
				// Go replaces unpaired JSON surrogate escapes. Retain those as
				// unresolved rather than treating replacement bytes as originals.
				if strings.Contains(value, "\ufffd") && strings.Contains(text[start:i+1], `\u`) {
					return "", 0, false
				}
				return value, i + 1, true
			}
		}
		return "", 0, false
	}
	var out strings.Builder
	for i := start + 1; i < len(text); i++ {
		c := text[i]
		if c == quote {
			return out.String(), i + 1, true
		}
		if quote == '`' && c == '$' && i+1 < len(text) && text[i+1] == '{' {
			return "", 0, false
		}
		if c == '\n' || c == '\r' {
			if quote != '`' {
				return "", 0, false
			}
			out.WriteByte(c)
			continue
		}
		if c != '\\' {
			out.WriteByte(c)
			continue
		}
		i++
		if i >= len(text) {
			return "", 0, false
		}
		switch text[i] {
		case '\\', '\'', '"', '`', '/':
			out.WriteByte(text[i])
		case 'n':
			out.WriteByte('\n')
		case 'r':
			out.WriteByte('\r')
		case 't':
			out.WriteByte('\t')
		case 'b':
			out.WriteByte('\b')
		case 'f':
			out.WriteByte('\f')
		default:
			return "", 0, false
		}
	}
	return "", 0, false
}

// Only static JSON values, string literals and unquoted identifier keys are
// accepted. No JS evaluation, interpolation, comments, spreads or expressions.
func staticObject(text string) (map[string]json.RawMessage, bool) {
	var b strings.Builder
	for i := 0; i < len(text); {
		c := text[i]
		if c == '"' || c == '\'' || c == '`' {
			value, end, ok := staticString(text, i)
			if !ok {
				return nil, false
			}
			encoded, _ := json.Marshal(value)
			b.Write(encoded)
			i = end
			continue
		}
		if c == '/' {
			return nil, false
		}
		if c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' {
			start := i
			for i < len(text) && (text[i] == '_' || text[i] >= 'a' && text[i] <= 'z' || text[i] >= 'A' && text[i] <= 'Z' || text[i] >= '0' && text[i] <= '9') {
				i++
			}
			j := i
			for j < len(text) && strings.ContainsRune(" \r\n\t", rune(text[j])) {
				j++
			}
			if j < len(text) && text[j] == ':' {
				b.WriteByte('"')
				b.WriteString(text[start:i])
				b.WriteByte('"')
			} else {
				b.WriteString(text[start:i])
			}
			continue
		}
		b.WriteByte(c)
		i++
	}
	d := json.NewDecoder(strings.NewReader(b.String()))
	token, e := d.Token()
	if e != nil || token != json.Delim('{') {
		return nil, false
	}
	out := map[string]json.RawMessage{}
	for d.More() {
		key, e := d.Token()
		if e != nil {
			return nil, false
		}
		k, ok := key.(string)
		if !ok {
			return nil, false
		}
		if _, duplicate := out[k]; duplicate {
			return nil, false
		}
		var value json.RawMessage
		if d.Decode(&value) != nil {
			return nil, false
		}
		out[k] = value
	}
	if _, e = d.Token(); e != nil {
		return nil, false
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return nil, false
	}
	return out, true
}

func savedChildCommand(event Event) (string, bool) {
	var object string
	if event.Name == "exec_command" || event.Name == "functions.exec_command" {
		object = event.Arguments
	} else if event.Name == "functions.exec" || event.Name == "exec" {
		code := event.Input
		if code == "" {
			code = event.Arguments
		}
		m := canonicalChild.FindStringSubmatch(code)
		if m == nil {
			return "", false
		}
		object = m[1]
	} else {
		return "", false
	}
	args, ok := staticObject(object)
	if !ok {
		return "", false
	}
	var command string
	if json.Unmarshal(args["cmd"], &command) != nil || command == "" {
		return "", false
	}
	return command, true
}

func operationalGuard(path string) bool {
	command := exec.Command("D:/CC/Tools/client-records/scrub391.exe", "--guard-path", path)
	hideGuard(command)
	b, e := command.Output()
	if e != nil {
		return false
	}
	var v struct {
		OK         bool  `json:"ok"`
		Client     *bool `json:"client_resource"`
		Unverified *bool `json:"classification_unverified"`
	}
	return json.Unmarshal(b, &v) == nil && v.OK && v.Client != nil && v.Unverified != nil && !*v.Client && !*v.Unverified
}

type savedHandoff struct {
	Source     int      `json:"source_todo"`
	Receiver   int      `json:"receiver_todo"`
	Root       string   `json:"private_root"`
	References []string `json:"references"`
	Calls      int      `json:"actual_calls"`
	Sources    int      `json:"source_logs"`
	Completed  int      `json:"enclosing_completed"`
	Unproven   int      `json:"enclosing_unproven"`
	Affiliated int      `json:"explicit_current_391_affiliation_mentions"`
}

func readBounded(path string, limit int64) ([]byte, error) {
	info, e := os.Lstat(path)
	if e != nil || !info.Mode().IsRegular() || info.Size() > limit || !strictSavedPath(path) {
		return nil, errors.New("saved_receipt_unavailable")
	}
	f, e := os.Open(path)
	if e != nil {
		return nil, errors.New("saved_receipt_unavailable")
	}
	defer f.Close()
	opened, e := f.Stat()
	if e != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) || !strictSavedPath(path) {
		return nil, errors.New("saved_receipt_changed")
	}
	b, e := io.ReadAll(io.LimitReader(f, limit+1))
	if e != nil || int64(len(b)) > limit {
		return nil, errors.New("saved_receipt_unreadable")
	}
	after, e := os.Lstat(path)
	if e != nil || !os.SameFile(opened, after) || !strictSavedPath(path) {
		return nil, errors.New("saved_receipt_changed")
	}
	return b, nil
}

// Pins are fingerprints of the already completed 2674 investigation, never a
// permission list or an authority to access a resource rejected by the guard.
const fixed2674HandoffSHA = "8aebf13488ffc18a8c760d77cfb01aefecc61bc0904da84293f29fcb0e356239"

var fixed2674WitnessSHA = map[string]string{
	"source-witnesses.private.jsonl":           "cee0f4c20e880fbc9a04b691af09586e40a8ade3173893e514a48d399222713c",
	"source-large-witnesses.private.jsonl":     "3d82da16953526dbce7fad0f79bdcdd1f305991331de0370a360807da73ab6db",
	"source-remainder-witnesses.private.jsonl": "9d1e594701f23f534e2a4f47dd7cacea334f9959a495a91272d2c3c98508281e",
}

// Exact fixed handoff roles only, with the current guard checked for every
// input. A path in an index never grants access, scope or product eligibility.
func refineSaved(handoff, output string) error {
	if !operationalGuard(handoff) {
		return errors.New("handoff_guard_denied")
	}
	b, e := readBounded(handoff, 1<<20)
	if e != nil {
		return e
	}
	var h savedHandoff
	if hash(b) != fixed2674HandoffSHA || json.Unmarshal(b, &h) != nil || h.Source != 2674 || h.Receiver != 2467 || h.Calls != 136 || h.Sources != 85 || filepath.Clean(filepath.Dir(handoff)) != filepath.Clean(h.Root) {
		return errors.New("fixed_handoff_mismatch")
	}
	listed := map[string]bool{}
	for _, ref := range h.References {
		if filepath.Base(ref) == ref {
			listed[ref] = true
		}
	}
	// Source logs are deliberately never reopened: only saved witness files.
	names := []string{"source-witnesses.private.jsonl", "source-large-witnesses.private.jsonl", "source-remainder-witnesses.private.jsonl"}
	type witness struct {
		Record       int             `json:"record"`
		SourceRecord int             `json:"source_record"`
		Raw          json.RawMessage `json:"raw"`
	}
	type call struct {
		event        Event
		rawHash      string
		sourceRecord int
	}
	calls := map[int]call{}
	outputs := map[int]Event{}
	outputSources := map[int]int{}
	receipts := []map[string]any{}
	for _, name := range names {
		if !listed[name] {
			return errors.New("saved_witness_not_referenced")
		}
		path := filepath.Join(h.Root, name)
		if !operationalGuard(path) {
			return errors.New("saved_witness_guard_denied")
		}
		data, e := readBounded(path, 16<<20)
		if e != nil {
			return e
		}
		if hash(data) != fixed2674WitnessSHA[name] {
			return errors.New("saved_witness_fingerprint_changed")
		}
		receipts = append(receipts, map[string]any{"role": name, "bytes": len(data), "sha256": hash(data)})
		s := bufio.NewScanner(bytes.NewReader(data))
		s.Buffer(make([]byte, 65536), 16<<20)
		for s.Scan() {
			var w witness
			if json.Unmarshal(s.Bytes(), &w) != nil || w.Record <= 0 || w.Record > 414 || w.SourceRecord <= 0 || w.SourceRecord > h.Sources {
				return errors.New("invalid_saved_witness")
			}
			ev, ok := unwrapEvent(w.Raw)
			if !ok {
				return errors.New("invalid_saved_event")
			}
			if strings.HasSuffix(ev.Type, "_output") {
				if _, exists := outputs[w.Record]; exists {
					return errors.New("duplicate_saved_output")
				}
				outputs[w.Record] = ev
				outputSources[w.Record] = w.SourceRecord
			} else {
				if _, exists := calls[w.Record]; exists {
					return errors.New("duplicate_saved_call")
				}
				calls[w.Record] = call{ev, hash(w.Raw), w.SourceRecord}
			}
		}
		if s.Err() != nil {
			return errors.New("saved_witness_incomplete")
		}
	}
	if len(calls) != h.Calls || len(outputs) != h.Calls {
		return errors.New("saved_witness_partition_changed")
	}
	rows := []map[string]any{}
	private := []map[string]any{}
	decoded, candidates, versions := 0, 0, map[string]bool{}
	identities := map[string]bool{}
	records := []int{}
	for record := range calls {
		records = append(records, record)
	}
	sort.Ints(records)
	for _, record := range records {
		c := calls[record]
		key := fmt.Sprintf("%d:%s", c.sourceRecord, c.event.Call)
		if identities[key] {
			return errors.New("duplicate_source_call_identity")
		}
		identities[key] = true
		o := outputs[record]
		if c.event.Call != o.Call || c.sourceRecord != outputSources[record] {
			return errors.New("saved_call_output_mismatch")
		}
		command, ok := savedChildCommand(c.event)
		row := map[string]any{"record": record, "call_sha256": hash([]byte(c.event.Call)), "saved_event_sha256": c.rawHash, "static_child_decoded": ok, "file_write_proven": false, "readback_proven": false, "independent_saved_sha_proven": false}
		if ok {
			decoded++
			row["decoded_command_sha256"] = hash([]byte(command))
			row["target_mention"] = strings.Contains(strings.ToLower(command), "client-projects.txt")
			values := literals(command)
			row["literal_syntax_candidates"] = len(values)
			for _, value := range values {
				candidates++
				versions[hash(value)] = true
				private = append(private, map[string]any{"record": record, "literal": string(value), "literal_sha256": hash(value), "file_write_proven": false})
			}
		}
		rows = append(rows, row)
	}
	// The output is an explicitly new directory; the caller must restrict its ACL
	// before invoking this command. Never overwrite an existing receipt.
	info, e := os.Lstat(output)
	if e != nil || !info.IsDir() || !strictSavedPath(output) {
		return errors.New("private_output_required")
	}
	for _, name := range []string{"refinement-report.json", "refinement.private.json"} {
		if _, e = os.Lstat(filepath.Join(output, name)); !os.IsNotExist(e) {
			return errors.New("new_output_required")
		}
	}
	summary := map[string]any{"kind": "saved-witness-static-refinement", "handoff_sha256": hash(b), "saved_calls": len(calls), "saved_outputs": len(outputs), "static_child_commands": decoded, "unresolved_wrappers": len(calls) - decoded, "literal_syntax_candidates": candidates, "literal_syntax_versions": len(versions), "source_logs_reopened": 0, "settings_written": 0, "guard_changed": false, "delivery_or_application_verified": false, "original_task_complete": false, "input_receipts": receipts, "rows": rows}
	f := newPrivate(filepath.Join(output, "refinement.private.json"))
	e = json.NewEncoder(f).Encode(map[string]any{"handoff": handoff, "candidates": private})
	if e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e != nil || closeErr != nil {
		return errors.New("private_receipt_write_failed")
	}
	// The private directory ACL excludes other users. Concurrent replacement by
	// the same owner/admin is outside this finite read-only audit's threat model;
	// still reject a changed directory before publishing its completion report.
	afterOutput, checkErr := os.Lstat(output)
	if checkErr != nil || !os.SameFile(info, afterOutput) || !strictSavedPath(output) {
		return errors.New("private_output_changed")
	}
	f = newPrivate(filepath.Join(output, "refinement-report.json"))
	e = json.NewEncoder(f).Encode(summary)
	if e == nil {
		e = f.Sync()
	}
	closeErr = f.Close()
	if e != nil || closeErr != nil {
		return errors.New("report_write_failed")
	}
	delete(summary, "rows")
	emit(summary)
	return nil
}
