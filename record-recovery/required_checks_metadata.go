package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"unicode/utf8"
)

// This one diagnostic never prepares a workspace or grants qualification.
func runRequiredChecksMetadataCLI(args []string, out io.Writer, read func() ([]byte, error)) int {
	if len(args) != 0 {
		_ = json.NewEncoder(out).Encode(obj{"ok": false, "code": "required_checks_invalid_arguments"})
		return 2
	}
	b, err := read()
	if err != nil {
		_ = json.NewEncoder(out).Encode(obj{"ok": false, "code": "required_checks_source_unavailable"})
		return 1
	}
	result, err := projectRequiredChecksMetadata(b)
	if err != nil {
		_ = json.NewEncoder(out).Encode(obj{"ok": false, "code": err.Error()})
		return 1
	}
	if json.NewEncoder(out).Encode(result) != nil {
		return 1
	}
	return 0
}

func requiredChecksContainer(parent obj, key string) (obj, obj) {
	value, exists := parent[key]
	if !exists {
		return obj{"state": "absent"}, nil
	}
	if value == nil {
		return obj{"state": "null"}, nil
	}
	if object, ok := value.(map[string]any); ok {
		return obj{"state": "recorded"}, object
	}
	return obj{"state": "invalid"}, nil
}

// Syntax only. Do not clean, resolve, stat, open or hash a referenced path.
func requiredChecksPath(parent obj, key string) obj {
	result := reviewField(parent, key, "reference")
	if result["state"] != "recorded" {
		return result
	}
	value, ok := parent[key].(string)
	if !ok || len(value) < 4 || value[1] != ':' || (value[2] != '/' && value[2] != '\\') ||
		!((value[0] >= 'A' && value[0] <= 'Z') || (value[0] >= 'a' && value[0] <= 'z')) ||
		strings.ContainsAny(value, "\x00\r\n") {
		return obj{"state": "invalid"}
	}
	return result
}

func requiredChecksPathText(value string) string {
	return strings.ToLower(strings.TrimRight(strings.ReplaceAll(value, "\\", "/"), "/"))
}

func requiredChecksComparison(reference obj, actual any, expected string) obj {
	if reference["state"] != "recorded" {
		return obj{"state": "unavailable"}
	}
	text, ok := actual.(string)
	if !ok {
		return obj{"state": "unavailable"}
	}
	return obj{"state": "compared", "matches": text == expected}
}

func requiredChecksScheduler(row obj, root obj, rootText string, leaf string) obj {
	result, scheduler := requiredChecksContainer(row, "scheduler")
	if scheduler == nil {
		return result
	}
	worktreeReference := requiredChecksPath(scheduler, "worktree")
	result["worktree"] = worktreeReference
	result["worktree_matches_registered_layout"] = obj{"state": "unavailable"}
	worktree, ok := scheduler["worktree"].(string)
	if root["state"] == "recorded" && worktreeReference["state"] == "recorded" && ok {
		result["worktree_matches_registered_layout"] = obj{
			"state": "compared", "matches": requiredChecksPathText(worktree) == requiredChecksPathText(rootText)+"-wt/"+leaf,
		}
	}
	branchReference := reviewField(scheduler, "branch", "reference")
	result["branch"] = branchReference
	result["branch_matches_case"] = requiredChecksComparison(branchReference, scheduler["branch"], "scheduler/"+leaf)
	result["key_reference"] = reviewField(scheduler, "key", "reference")
	result["job_reference"] = reviewField(scheduler, "job", "reference")
	result["selection"], _ = requiredChecksContainer(scheduler, "selection")
	result["workspace_receipt"], _ = requiredChecksContainer(scheduler, "workspace_receipt")
	phase := reviewField(scheduler, "phase", "reference")
	if phase["state"] == "recorded" {
		allowed := map[string]bool{"preparing": true, "prepared": true, "starting": true, "running": true, "retry": true, "done": true, "failed": true}
		if text, ok := scheduler["phase"].(string); ok && allowed[text] {
			phase = obj{"state": "recorded", "value": text}
		} else {
			phase = obj{"state": "invalid"}
		}
	}
	result["phase"] = phase
	return result
}

func projectRequiredChecksMetadata(b []byte) (obj, error) {
	fail := func(code string) (obj, error) { return nil, errors.New("required_checks_" + code) }
	if len(b) > reviewMetadataLimit || !utf8.Valid(b) {
		return fail("invalid_source")
	}
	raw := bytes.TrimPrefix(b, []byte{239, 187, 191})
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var db obj
	if decoder.Decode(&db) != nil || db == nil {
		return fail("invalid_source")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
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
	selected := map[int64]obj{}
	for _, value := range items {
		row, ok := value.(map[string]any)
		if !ok {
			return fail("invalid_source")
		}
		id, ok := reviewInteger(row["id"])
		if !ok {
			return fail("invalid_source")
		}
		if id != 3648 && id != 3649 {
			continue
		}
		if selected[id] != nil {
			return fail("ambiguous_record")
		}
		if row["project"] != "localai-web-rs" {
			return fail("scope_mismatch")
		}
		selected[id] = row
	}
	if len(selected) != 2 {
		return fail("record_missing")
	}
	registration, projects := requiredChecksContainer(db, "projects")
	root := obj{"state": "unavailable"}
	rootText := ""
	if projects != nil {
		root = requiredChecksPath(projects, "localai-web-rs")
		rootText, _ = projects["localai-web-rs"].(string)
		registration["root_reference"] = root
	}
	records := []obj{}
	for index, id := range []int64{3648, 3649} {
		row := selected[id]
		leaf := []string{"todo-3648-cargo", "todo-3649-work"}[index]
		records = append(records, obj{
			"record_id": id, "status": reviewField(row, "status", "status"),
			"jobs": reviewField(row, "jobs", "jobs"), "spec_reference": reviewField(row, "spec", "reference"),
			"scheduler": requiredChecksScheduler(row, root, rootText, leaf),
		})
	}
	return obj{
		"ok": true, "domain": "original-review-required-checks-metadata-v1", "case_todo_id": 3255,
		"snapshot_sha256": digest(b), "metadata_only": true, "project_registration": registration, "records": records,
		"source_owner_attested": false, "filesystem_observed": false, "preparation_authorized": false,
		"live_state_changed": false, "data_access_granted": false, "original_registry_recovered": false,
		"review_completed": false, "console_queue_observed": false,
	}, nil
}
