package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

const checkMetadataSecret = "synthetic_private_value_never_in_output"

func requiredChecksFixture() obj {
	return obj{
		"projects": obj{"localai-web-rs": "C:/synthetic-private/console", "other": checkMetadataSecret},
		"private":  checkMetadataSecret,
		"items": []any{
			obj{"id": 3649, "project": "localai-web-rs", "status": "todo", "jobs": []any{}, "spec": checkMetadataSecret,
				"title": checkMetadataSecret, "scheduler": obj{"worktree": "C:/synthetic-private/console-wt/todo-3649-work",
					"branch": "scheduler/todo-3649-work", "phase": "preparing", "key": checkMetadataSecret,
					"selection": obj{"prompt": checkMetadataSecret}, "workspace_receipt": obj{"cwd": checkMetadataSecret}}},
			obj{"id": 99, "project": checkMetadataSecret, "prompt": checkMetadataSecret},
			obj{"id": 3648, "project": "localai-web-rs", "status": "todo", "jobs": []any{checkMetadataSecret},
				"spec": checkMetadataSecret, "scheduler": obj{"worktree": "c:\\SYNTHETIC-PRIVATE\\console-wt\\todo-3648-cargo",
					"branch": "scheduler/todo-3648-cargo", "phase": "preparing", "job": checkMetadataSecret,
					"selection": obj{"environment": checkMetadataSecret}}},
		},
	}
}

func requiredChecksEncode(t *testing.T, value obj) []byte {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func requiredChecksResult(t *testing.T, value obj) obj {
	t.Helper()
	r, err := projectRequiredChecksMetadata(requiredChecksEncode(t, value))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestRequiredChecksMetadataProjectsOnlyFixedRecordsAndComparisons(t *testing.T) {
	fixture := requiredChecksFixture()
	b := requiredChecksEncode(t, fixture)
	before := append([]byte(nil), b...)
	r, err := projectRequiredChecksMetadata(b)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(b, before) || r["snapshot_sha256"] != digest(before) {
		t.Fatal("projection changed or did not bind the input bytes")
	}
	rows := r["records"].([]obj)
	if len(rows) != 2 || rows[0]["record_id"] != int64(3648) || rows[1]["record_id"] != int64(3649) {
		t.Fatal("projection did not select exactly the fixed records in fixed order")
	}
	for _, row := range rows {
		scheduler := row["scheduler"].(obj)
		for _, key := range []string{"worktree_matches_registered_layout", "branch_matches_case"} {
			if !reflect.DeepEqual(scheduler[key], obj{"state": "compared", "matches": true}) {
				t.Fatalf("canonical recorded layout did not match: %s", key)
			}
		}
	}
	if rows[0]["jobs"].(obj)["reference_count"] != 1 || rows[1]["jobs"].(obj)["reference_count"] != 0 {
		t.Fatal("reference counts did not preserve the two array lengths")
	}
	reference := obj{"state": "recorded", "reference_opened": false}
	compared := obj{"state": "compared", "matches": true}
	firstScheduler := obj{"state": "recorded", "worktree": reference, "worktree_matches_registered_layout": compared,
		"branch": reference, "branch_matches_case": compared, "key_reference": obj{"state": "absent"},
		"job_reference": reference, "selection": obj{"state": "recorded"}, "workspace_receipt": obj{"state": "absent"},
		"phase": obj{"state": "recorded", "value": "preparing"}}
	secondScheduler := obj{"state": "recorded", "worktree": reference, "worktree_matches_registered_layout": compared,
		"branch": reference, "branch_matches_case": compared, "key_reference": reference,
		"job_reference": obj{"state": "absent"}, "selection": obj{"state": "recorded"}, "workspace_receipt": obj{"state": "recorded"},
		"phase": obj{"state": "recorded", "value": "preparing"}}
	want := obj{
		"ok": true, "domain": "original-review-required-checks-metadata-v1", "case_todo_id": 3255,
		"snapshot_sha256": digest(before), "metadata_only": true,
		"project_registration": obj{"state": "recorded", "root_reference": reference},
		"records": []obj{
			{"record_id": int64(3648), "status": obj{"state": "recorded", "value": "todo"},
				"jobs": obj{"state": "recorded", "reference_count": 1, "references_opened": false}, "spec_reference": reference, "scheduler": firstScheduler},
			{"record_id": int64(3649), "status": obj{"state": "recorded", "value": "todo"},
				"jobs": obj{"state": "recorded", "reference_count": 0, "references_opened": false}, "spec_reference": reference, "scheduler": secondScheduler},
		},
		"source_owner_attested": false, "filesystem_observed": false, "preparation_authorized": false,
		"live_state_changed": false, "data_access_granted": false, "original_registry_recovered": false,
		"review_completed": false, "console_queue_observed": false,
	}
	if !reflect.DeepEqual(r, want) {
		t.Fatal("complete positive projection differs from its fixed contract")
	}
	output := string(requiredChecksEncode(t, r))
	for _, forbidden := range []string{checkMetadataSecret, "synthetic-private", "SYNTHETIC-PRIVATE", "localai-web-rs", "todo-3648-cargo", "todo-3649-work"} {
		if strings.Contains(output, forbidden) {
			t.Fatal("projection disclosed a reference, path or unrelated text")
		}
	}
	for _, key := range []string{"source_owner_attested", "filesystem_observed", "preparation_authorized", "live_state_changed", "data_access_granted", "original_registry_recovered", "review_completed", "console_queue_observed"} {
		if r[key] != false {
			t.Fatalf("projection asserted an unverified effect: %s", key)
		}
	}
	assertRequiredChecksKeys(t, r)
}

func TestRequiredChecksMetadataKeepsMismatchesAndUnavailableDistinct(t *testing.T) {
	for _, key := range []string{"worktree", "branch"} {
		for _, test := range []struct {
			name, state string
			value       any
			absent      bool
		}{
			{"absent", "absent", nil, true}, {"null", "null", nil, false},
			{"type", "invalid", obj{"private": checkMetadataSecret}, false},
			{"empty", "invalid", "", false}, {"long", "invalid", strings.Repeat("x", 4097), false},
			{"mismatch", "recorded", "C:/different-private-value", false},
		} {
			t.Run(key+"/"+test.name, func(t *testing.T) {
				fixture := requiredChecksFixture()
				source := fixture["items"].([]any)[2].(obj)["scheduler"].(obj)
				if test.absent {
					delete(source, key)
				} else {
					source[key] = test.value
				}
				r := requiredChecksResult(t, fixture)
				scheduler := r["records"].([]obj)[0]["scheduler"].(obj)
				if scheduler[key].(obj)["state"] != test.state {
					t.Fatal("reference state was flattened")
				}
				comparison := "branch_matches_case"
				if key == "worktree" {
					comparison = "worktree_matches_registered_layout"
				}
				want := obj{"state": "unavailable"}
				if test.state == "recorded" {
					want = obj{"state": "compared", "matches": false}
				}
				if !reflect.DeepEqual(scheduler[comparison], want) {
					t.Fatal("unknown was converted to a comparison or mismatch was lost")
				}
				assertRequiredChecksKeys(t, r)
			})
		}
	}
}

func TestRequiredChecksMetadataKeepsContainersAndRegistrationUnknown(t *testing.T) {
	for _, key := range []string{"projects", "scheduler", "selection", "workspace_receipt"} {
		for _, state := range []string{"absent", "null", "invalid"} {
			t.Run(key+"/"+state, func(t *testing.T) {
				fixture := requiredChecksFixture()
				parent := fixture
				if key != "projects" {
					parent = fixture["items"].([]any)[2].(obj)
					if key != "scheduler" {
						parent = parent["scheduler"].(obj)
					}
				}
				switch state {
				case "absent":
					delete(parent, key)
				case "null":
					parent[key] = nil
				case "invalid":
					parent[key] = checkMetadataSecret
				}
				r := requiredChecksResult(t, fixture)
				result := r["project_registration"].(obj)
				if key != "projects" {
					result = r["records"].([]obj)[0]["scheduler"].(obj)
					if key != "scheduler" {
						result = result[key].(obj)
					}
				}
				if result["state"] != state {
					t.Fatal("container state was flattened")
				}
				if key == "projects" {
					for _, record := range r["records"].([]obj) {
						if !reflect.DeepEqual(record["scheduler"].(obj)["worktree_matches_registered_layout"], obj{"state": "unavailable"}) {
							t.Fatal("missing registration became a path comparison")
						}
					}
				}
				assertRequiredChecksKeys(t, r)
			})
		}
	}
	for _, value := range []any{nil, 1, "", "relative/private", "C:/private\nvalue", "C:/private\x00value"} {
		fixture := requiredChecksFixture()
		fixture["projects"].(obj)["localai-web-rs"] = value
		r := requiredChecksResult(t, fixture)
		for _, record := range r["records"].([]obj) {
			if !reflect.DeepEqual(record["scheduler"].(obj)["worktree_matches_registered_layout"], obj{"state": "unavailable"}) {
				t.Fatal("invalid root became a comparison")
			}
		}
	}
}

func TestRequiredChecksMetadataRejectsBrokenOrOutOfScopeSnapshots(t *testing.T) {
	valid := requiredChecksEncode(t, requiredChecksFixture())
	for _, test := range []struct {
		name, code string
		value      []byte
	}{
		{"empty", "invalid_source", nil}, {"null", "invalid_source", []byte("null")},
		{"no_rows", "record_missing", []byte(`{"items":[]}`)},
		{"two_documents", "invalid_source", append(append([]byte(nil), valid...), []byte("{}")...)},
		{"utf8", "invalid_source", []byte{255}}, {"over_limit", "invalid_source", make([]byte, reviewMetadataLimit+1)},
		{"duplicate_top", "duplicate_keys", []byte(`{"items":[],"items":[]}`)},
		{"duplicate_nested", "duplicate_keys", []byte(`{"items":[{"id":1,"id":2}]}`)},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := projectRequiredChecksMetadata(test.value); err == nil || err.Error() != "required_checks_"+test.code {
				t.Fatal("invalid snapshot did not return its fixed error code")
			}
		})
	}
	for _, test := range []struct{ kind, code string }{
		{"missing", "record_missing"}, {"duplicate", "ambiguous_record"}, {"scope", "scope_mismatch"},
		{"bad_other_id", "invalid_source"}, {"bad_row", "invalid_source"},
	} {
		fixture := requiredChecksFixture()
		items := fixture["items"].([]any)
		switch test.kind {
		case "missing":
			fixture["items"] = items[:2]
		case "duplicate":
			fixture["items"] = append(items, items[0])
		case "scope":
			items[0].(obj)["project"] = checkMetadataSecret
		case "bad_other_id":
			items[1].(obj)["id"] = checkMetadataSecret
		case "bad_row":
			fixture["items"] = append(items, checkMetadataSecret)
		}
		if _, err := projectRequiredChecksMetadata(requiredChecksEncode(t, fixture)); err == nil || err.Error() != "required_checks_"+test.code {
			t.Fatalf("case did not return the correct fixed error: %s", test.kind)
		}
	}
	if _, err := projectRequiredChecksMetadata(append([]byte{239, 187, 191}, valid...)); err != nil {
		t.Fatal("UTF8 BOM was not accepted")
	}
}

func TestRequiredChecksMetadataCLIUsesValidInputAndRejectsArgumentsBeforeRead(t *testing.T) {
	reads := 0
	read := func() ([]byte, error) {
		reads++
		return requiredChecksEncode(t, requiredChecksFixture()), nil
	}
	var out bytes.Buffer
	if runRequiredChecksMetadataCLI(nil, &out, read) != 0 || reads != 1 || !bytes.Contains(out.Bytes(), []byte(`"ok":true`)) {
		t.Fatal("positive control did not consume one valid input")
	}
	reads = 0
	out.Reset()
	if runRequiredChecksMetadataCLI([]string{"--id=99"}, &out, read) != 2 || reads != 0 || strings.Contains(out.String(), "99") {
		t.Fatal("arguments were not rejected before source access")
	}
	out.Reset()
	if runRequiredChecksMetadataCLI(nil, &out, func() ([]byte, error) { return nil, errors.New(checkMetadataSecret) }) != 1 || strings.Contains(out.String(), checkMetadataSecret) {
		t.Fatal("source error was not sanitized")
	}
	if out.String() != "{\"code\":\"required_checks_source_unavailable\",\"ok\":false}\n" {
		t.Fatal("source failure code was not preserved")
	}
	out.Reset()
	if runRequiredChecksMetadataCLI(nil, &out, func() ([]byte, error) { return []byte(`{"items":[]}`), nil }) != 1 ||
		out.String() != "{\"code\":\"required_checks_record_missing\",\"ok\":false}\n" {
		t.Fatal("CLI did not preserve the projection failure code")
	}
}

func TestRequiredChecksMetadataDoesNotForwardUnknownPhases(t *testing.T) {
	for _, test := range []struct {
		value any
		state string
	}{
		{nil, "null"}, {12, "invalid"}, {checkMetadataSecret, "unlisted"}, {"preparing", "recorded"},
	} {
		fixture := requiredChecksFixture()
		fixture["items"].([]any)[2].(obj)["scheduler"].(obj)["phase"] = test.value
		r := requiredChecksResult(t, fixture)
		phase := r["records"].([]obj)[0]["scheduler"].(obj)["phase"].(obj)
		if phase["state"] != test.state || strings.Contains(string(requiredChecksEncode(t, r)), checkMetadataSecret) {
			t.Fatal("unknown phase was exposed or its state was lost")
		}
		if test.state == "recorded" && phase["value"] != "preparing" {
			t.Fatal("listed phase value was lost")
		}
		assertRequiredChecksKeys(t, r)
	}
}

func TestRequiredChecksMetadataPathSyntaxAndTrailingSeparators(t *testing.T) {
	for _, name := range []string{"registered_root", "worktree"} {
		for _, test := range []struct {
			name, value string
			absent      bool
		}{
			{"absent", "", true}, {"UNC", `\\server\share\private`, false}, {"bad_drive", "1:/private", false},
			{"drive_relative", "C:private", false}, {"drive_root", "C:/", false}, {"relative", "private/path", false},
			{"LF", "C:/private\nvalue", false}, {"NUL", "C:/private\x00value", false},
		} {
			t.Run(name+"/"+test.name, func(t *testing.T) {
				fixture := requiredChecksFixture()
				parent, key := fixture["projects"].(obj), "localai-web-rs"
				if name == "worktree" {
					parent, key = fixture["items"].([]any)[2].(obj)["scheduler"].(obj), "worktree"
				}
				if test.absent {
					delete(parent, key)
				} else {
					parent[key] = test.value
				}
				r := requiredChecksResult(t, fixture)
				if !reflect.DeepEqual(r["records"].([]obj)[0]["scheduler"].(obj)["worktree_matches_registered_layout"], obj{"state": "unavailable"}) {
					t.Fatal("unsupported path syntax became a comparison")
				}
			})
		}
	}
	fixture := requiredChecksFixture()
	fixture["projects"].(obj)["localai-web-rs"] = "C:/synthetic-private/console///"
	fixture["items"].([]any)[2].(obj)["scheduler"].(obj)["worktree"] = "C:/synthetic-private/console-wt/todo-3648-cargo\\\\"
	r := requiredChecksResult(t, fixture)
	if !reflect.DeepEqual(r["records"].([]obj)[0]["scheduler"].(obj)["worktree_matches_registered_layout"], obj{"state": "compared", "matches": true}) {
		t.Fatal("documented trailing separator comparison changed")
	}
}

func assertRequiredChecksKeys(t *testing.T, result obj) {
	t.Helper()
	allowed := map[string]string{
		"":                      "ok domain case_todo_id snapshot_sha256 metadata_only project_registration records source_owner_attested filesystem_observed preparation_authorized live_state_changed data_access_granted original_registry_recovered review_completed console_queue_observed",
		"/project_registration": "state root_reference", "/project_registration/root_reference": "state reference_opened",
		"/records/[]": "record_id status jobs spec_reference scheduler", "/records/[]/status": "state value",
		"/records/[]/jobs": "state reference_count references_opened", "/records/[]/spec_reference": "state reference_opened",
		"/records/[]/scheduler":          "state worktree worktree_matches_registered_layout branch branch_matches_case key_reference job_reference selection workspace_receipt phase",
		"/records/[]/scheduler/worktree": "state reference_opened", "/records/[]/scheduler/branch": "state reference_opened",
		"/records/[]/scheduler/worktree_matches_registered_layout": "state matches", "/records/[]/scheduler/branch_matches_case": "state matches",
		"/records/[]/scheduler/key_reference": "state reference_opened", "/records/[]/scheduler/job_reference": "state reference_opened",
		"/records/[]/scheduler/selection": "state", "/records/[]/scheduler/workspace_receipt": "state", "/records/[]/scheduler/phase": "state value",
	}
	var decoded any
	if err := json.Unmarshal(requiredChecksEncode(t, result), &decoded); err != nil {
		t.Fatal(err)
	}
	var walk func(any, string)
	walk = func(value any, path string) {
		switch typed := value.(type) {
		case map[string]any:
			keys, ok := allowed[path]
			if !ok {
				t.Fatalf("unexpected output object at %s", path)
			}
			keySet := map[string]bool{}
			for _, key := range strings.Fields(keys) {
				keySet[key] = true
			}
			for key, child := range typed {
				if !keySet[key] {
					t.Fatalf("unexpected output key at %s", path)
				}
				walk(child, path+"/"+key)
			}
		case []any:
			for _, child := range typed {
				walk(child, path+"/[]")
			}
		}
	}
	walk(decoded, "")
}
