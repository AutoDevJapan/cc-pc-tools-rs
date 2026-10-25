package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func reviewAdmissionFixture() obj {
	return obj{"items": []any{obj{"id": 3649, "project": "localai-web-rs", "status": "todo", "jobs": []any{},
		"dispatchable": true, "model": "anthropic/claude-opus-5-5", "effort": "max",
		"scheduler": obj{"phase": "preparing", "not_before": 1793030399.891,
			"resource_wait": obj{"reason": "quota_exhausted", "until": 1793030399.891}}}}}
}

func reviewAdmissionProject(t *testing.T, fixture obj) obj {
	t.Helper()
	b, err := json.Marshal(fixture)
	if err != nil {
		t.Fatal(err)
	}
	r, err := projectReviewAdmission(b)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestReviewAdmissionProjectsRecordedSelectionAndQuota(t *testing.T) {
	fixture := obj{"projects": obj{"localai-web-rs": "synthetic-private"}, "items": []any{
		obj{"id": 3649, "project": "localai-web-rs", "status": "todo", "jobs": []any{},
			"dispatchable": true, "model": "anthropic/claude-opus-5-5", "effort": "max",
			"title": "synthetic-private", "scheduler": obj{"phase": "preparing",
				"not_before":    1793030399.891,
				"resource_wait": obj{"reason": "quota_exhausted", "until": 1793030399.891}}},
		obj{"id": 99, "project": "synthetic-private", "model": "synthetic-private"},
	}}
	b, err := json.Marshal(fixture)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	reads := 0
	code := runReviewAdmissionCLI(nil, &output, func() ([]byte, error) { reads++; return b, nil })
	if code != 0 || reads != 1 {
		t.Fatalf("valid admission snapshot was not read once: exit=%d reads=%d", code, reads)
	}
	if strings.Contains(output.String(), "synthetic-private") {
		t.Fatal("private value escaped fixed projection")
	}
	var result obj
	if json.Unmarshal(output.Bytes(), &result) != nil {
		t.Fatal("invalid output")
	}
	row := result["record"].(map[string]any)
	model := row["requested"].(map[string]any)
	if model["model"].(map[string]any)["value"] != "anthropic/claude-opus-5-5" ||
		model["model_origin"].(map[string]any)["state"] != "absent" {
		t.Fatal("model or missing origin was changed")
	}
	for _, key := range []string{"launch_performed", "fallback_authorized", "execution_verified", "data_access_granted", "live_state_changed"} {
		if result[key] != false {
			t.Fatalf("unobserved effect asserted: %s", key)
		}
	}
	keys := func(value map[string]any) string {
		result := []string{}
		for key := range value {
			result = append(result, key)
		}
		sort.Strings(result)
		return strings.Join(result, "|")
	}
	if keys(result) != "case_todo_id|data_access_granted|domain|execution_verified|fallback_authorized|launch_performed|live_state_changed|metadata_only|model_execution_observed|ok|origin_independently_verified|producer|quota_currently_observed|record|snapshot_sha256|source|source_scheduler_version" ||
		keys(row) != "dependency_references|dispatchable|jobs|record_id|requested|scheduler|status|stops" ||
		keys(model) != "effort|effort_origin|model|model_origin" {
		t.Fatal("fixed output keys changed")
	}
}

func TestReviewAdmissionDoesNotInferOriginsOrCurrentQuota(t *testing.T) {
	for _, test := range []struct {
		name, state string
		value       any
		absent      bool
	}{
		{"absent", "absent", nil, true}, {"null", "null", nil, false},
		{"type", "invalid", true, false}, {"empty", "invalid", "", false},
		{"unlisted", "unrecognized", "private-origin", false},
		{"policy", "recorded", "policy", false}, {"owner", "recorded", "owner_request", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := reviewAdmissionFixture()
			row := fixture["items"].([]any)[0].(obj)
			if !test.absent {
				row["model_origin"] = test.value
			}
			r := reviewAdmissionProject(t, fixture)
			got := r["record"].(obj)["requested"].(obj)["model_origin"].(obj)
			if got["state"] != test.state || test.state != "recorded" && len(got) != 1 {
				t.Fatal("unknown origin was inferred or leaked")
			}
			for _, key := range []string{"origin_independently_verified", "quota_currently_observed", "model_execution_observed", "fallback_authorized"} {
				if r[key] != false {
					t.Fatalf("saved snapshot became authority: %s", key)
				}
			}
		})
	}
}

func TestReviewAdmissionSeparatesCurrentRequestedAndCachedValues(t *testing.T) {
	fixture := reviewAdmissionFixture()
	row := fixture["items"].([]any)[0].(obj)
	row["model_origin"] = "owner_request"
	scheduler := row["scheduler"].(obj)
	scheduler["selection"] = obj{"model": "gpt-6-luna", "model_origin": "policy", "effort": "max", "prompt": "private-value"}
	scheduler["selection_input"] = obj{"model": "anthropic/claude-sonnet-5-5"}
	scheduler["console_request"] = obj{"key": "private-value", "prompt": "private-value", "options": obj{"model": "gpt-6-astra"}}
	scheduler["preparation_failure"] = obj{"reason": "excluded_resource", "at": 1700000000}
	inputBytes, err := json.Marshal(fixture)
	if err != nil {
		t.Fatal(err)
	}
	r := reviewAdmissionProject(t, fixture)
	record := r["record"].(obj)
	if record["requested"].(obj)["model"].(obj)["value"] != "anthropic/claude-opus-5-5" {
		t.Fatal("cached selection replaced current requested model")
	}
	result := record["scheduler"].(obj)
	if result["selection"].(obj)["recorded_values"].(obj)["model"].(obj)["value"] != "gpt-6-luna" ||
		result["selection_input"].(obj)["recorded_values"].(obj)["model"].(obj)["value"] != "anthropic/claude-sonnet-5-5" ||
		result["console_request"].(obj)["options"].(obj)["recorded_values"].(obj)["model"].(obj)["value"] != "gpt-6-astra" {
		t.Fatal("independent saved sources were merged")
	}
	if result["resource_wait"].(obj)["reason"].(obj)["value"] != "quota_exhausted" ||
		result["preparation_failure"].(obj)["reason"].(obj)["value"] != "excluded_resource" {
		t.Fatal("old failure and resource wait were collapsed")
	}
	b, _ := json.Marshal(r)
	if strings.Contains(string(b), "private-value") {
		t.Fatal("unlisted input escaped projection")
	}
	selected := func(model string, origin string, effort string) obj {
		r := obj{"model": obj{"state": "recorded", "value": model}, "effort": obj{"state": "absent"},
			"model_origin": obj{"state": "absent"}, "effort_origin": obj{"state": "absent"}}
		if origin != "" {
			r["model_origin"] = obj{"state": "recorded", "value": origin}
		}
		if effort != "" {
			r["effort"] = obj{"state": "recorded", "value": effort}
		}
		return obj{"state": "recorded", "recorded_values": r}
	}
	want := obj{
		"ok": true, "domain": "unstarted-review-admission-metadata-v1", "case_todo_id": 3255,
		"snapshot_sha256": digest(inputBytes), "metadata_only": true,
		"record": obj{
			"record_id": int64(3649), "status": obj{"state": "recorded", "value": "todo"},
			"dispatchable":          obj{"state": "recorded", "value": true},
			"jobs":                  obj{"state": "recorded", "reference_count": 0, "references_opened": false},
			"dependency_references": obj{"state": "absent"},
			"stops": obj{"stopped": obj{"state": "absent"}, "owner_stopped": obj{"state": "absent"},
				"owner_postponement_recorded": obj{"state": "absent"}, "wait_ready": obj{"state": "absent"},
				"owner_question_wait": obj{"state": "absent"}, "decision_wait": obj{"state": "absent"},
				"stop_kind": obj{"state": "absent"}},
			"requested": obj{"model": obj{"state": "recorded", "value": "anthropic/claude-opus-5-5"},
				"model_origin": obj{"state": "recorded", "value": "owner_request"},
				"effort":       obj{"state": "recorded", "value": "max"}, "effort_origin": obj{"state": "absent"}},
			"scheduler": obj{"state": "recorded", "phase": obj{"state": "recorded", "value": "preparing"},
				"not_before":      obj{"state": "recorded", "epoch_seconds": json.Number("1793030399.891")},
				"selection":       selected("gpt-6-luna", "policy", "max"),
				"selection_input": selected("anthropic/claude-sonnet-5-5", "", ""),
				"console_request": obj{"state": "recorded", "options": selected("gpt-6-astra", "", "")},
				"resource_wait": obj{"state": "recorded", "reason": obj{"state": "recorded", "value": "quota_exhausted"},
					"until": obj{"state": "recorded", "epoch_seconds": json.Number("1793030399.891")}},
				"preparation_failure": obj{"state": "recorded", "reason": obj{"state": "recorded", "value": "excluded_resource"},
					"at": obj{"state": "recorded", "epoch_seconds": json.Number("1700000000")}}}},
		"model_execution_observed": false, "origin_independently_verified": false, "quota_currently_observed": false,
		"launch_performed": false, "fallback_authorized": false, "execution_verified": false,
		"data_access_granted": false, "live_state_changed": false,
		"source": "persisted_todo_snapshot", "source_scheduler_version": obj{"state": "unavailable"},
	}
	if !reflect.DeepEqual(r, want) {
		t.Fatal("complete nested projection differs from fixed contract")
	}
}

func TestReviewAdmissionKeepsContainerAndTimestampStates(t *testing.T) {
	for _, key := range []string{"selection", "selection_input", "resource_wait", "console_request", "preparation_failure"} {
		for _, test := range []struct {
			name, state string
			value       any
			absent      bool
		}{
			{"absent", "absent", nil, true}, {"null", "null", nil, false},
			{"type", "invalid", "private-value", false},
		} {
			t.Run(key+"/"+test.name, func(t *testing.T) {
				fixture := reviewAdmissionFixture()
				scheduler := fixture["items"].([]any)[0].(obj)["scheduler"].(obj)
				if test.absent {
					delete(scheduler, key)
				} else {
					scheduler[key] = test.value
				}
				got := reviewAdmissionProject(t, fixture)["record"].(obj)["scheduler"].(obj)[key]
				if !reflect.DeepEqual(got, obj{"state": test.state}) {
					t.Fatal("container state was inferred")
				}
			})
		}
	}
	for _, test := range []struct {
		name, state string
		value       any
		absent      bool
	}{
		{"absent", "absent", nil, true}, {"null", "null", nil, false},
		{"type", "invalid", "private-value", false}, {"negative", "invalid", -1, false},
		{"huge", "invalid", json.Number("1e100"), false}, {"fraction", "recorded", json.Number("1793030399.891"), false},
	} {
		t.Run("time/"+test.name, func(t *testing.T) {
			fixture := reviewAdmissionFixture()
			scheduler := fixture["items"].([]any)[0].(obj)["scheduler"].(obj)
			if test.absent {
				delete(scheduler, "not_before")
			} else {
				scheduler["not_before"] = test.value
			}
			got := reviewAdmissionProject(t, fixture)["record"].(obj)["scheduler"].(obj)["not_before"].(obj)
			if got["state"] != test.state || test.state != "recorded" && len(got) != 1 {
				t.Fatal("invalid timestamp escaped output")
			}
			if test.state == "recorded" && got["epoch_seconds"].(json.Number).String() != "1793030399.891" {
				t.Fatal("fractional deadline changed")
			}
		})
	}
}

func TestReviewAdmissionRejectsBrokenAndOutOfScopeSources(t *testing.T) {
	for _, text := range []string{
		`null`, `{}`, `{"items":null}`, `{"items":[null]}`, `{"items":[{"id":"3649"}]}`,
		`{"items":[{"id":3649,"project":"other"}]}`,
		`{"items":[{"id":3649,"project":"localai-web-rs"},{"id":3649,"project":"localai-web-rs"}]}`,
		`{"items":[{"id":3649,"project":"localai-web-rs","model":"opus","model":"sonnet"}]}`,
		`{"items":[],"items":[]}`, `{"items":[]} {}`, "\xff",
	} {
		t.Run(text, func(t *testing.T) {
			if _, err := projectReviewAdmission([]byte(text)); err == nil {
				t.Fatal("invalid source accepted")
			}
		})
	}
	if _, err := projectReviewAdmission(bytes.Repeat([]byte{' '}, reviewMetadataLimit+1)); err == nil {
		t.Fatal("oversized source accepted")
	}
}

func TestReviewAdmissionCLIRefusesArgumentsAndDoesNotEchoReadErrors(t *testing.T) {
	for _, args := range [][]string{{"3649"}, {"--source", "private-value"}, {"--apply"}} {
		var output bytes.Buffer
		reads := 0
		code := runReviewAdmissionCLI(args, &output, func() ([]byte, error) { reads++; return nil, errors.New("private-value") })
		if code != 2 || reads != 0 || output.String() != "{\"code\":\"review_admission_invalid_arguments\",\"ok\":false}\n" {
			t.Fatal("argument refusal did not precede reading")
		}
	}
	var output bytes.Buffer
	code := runReviewAdmissionCLI(nil, &output, func() ([]byte, error) { return nil, errors.New("private-value") })
	if code != 1 || strings.Contains(output.String(), "private-value") {
		t.Fatal("reader error was exposed")
	}
}

func TestReviewAdmissionMainHelper(t *testing.T) {
	if os.Getenv("CC_REVIEW_ADMISSION_MAIN_HELPER") != "1" {
		return
	}
	os.Args = []string{"record-recovery", "review-admission-metadata", "--source=synthetic-forbidden-source"}
	main()
	t.Fatal("main did not exit")
}

func TestReviewAdmissionMainDispatchRefusesArguments(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(executable, "-test.run=^TestReviewAdmissionMainHelper$")
	cmd.Env = append(os.Environ(), "CC_REVIEW_ADMISSION_MAIN_HELPER=1")
	hideCommand(cmd)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	exit, ok := err.(*exec.ExitError)
	if !ok || exit.ExitCode() != 2 || stderr.Len() != 0 || string(output) != "{\"code\":\"review_admission_invalid_arguments\",\"ok\":false}\n" {
		t.Fatalf("main did not dispatch fixed admission refusal: %v", err)
	}
}

func TestReviewAdmissionProducerUsesOnlyBuildMetadata(t *testing.T) {
	result := reviewAdmissionProducer()
	allowed := map[string]bool{"state": true, "runtime_scheduler_version_verified": true,
		"compiled_revision": true, "compiled_source_modified": true}
	for key := range result {
		if !allowed[key] {
			t.Fatal("producer exposed an unexpected build field")
		}
	}
	if result["runtime_scheduler_version_verified"] != false {
		t.Fatal("own build became runtime scheduler attestation")
	}
	if revision, exists := result["compiled_revision"]; exists {
		value, ok := revision.(string)
		if !ok || len(value) != 40 || result["state"] != "recorded" {
			t.Fatal("invalid compiled revision")
		}
	} else if result["state"] != "unavailable" {
		t.Fatal("missing build metadata became recorded")
	}
}
