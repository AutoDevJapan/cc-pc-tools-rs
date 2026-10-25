package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"runtime/debug"
	"strings"
	"unicode/utf8"
)

func runReviewAdmissionCLI(args []string, out io.Writer, read func() ([]byte, error)) int {
	if len(args) != 0 {
		_ = json.NewEncoder(out).Encode(obj{"ok": false, "code": "review_admission_invalid_arguments"})
		return 2
	}
	b, err := read()
	if err != nil {
		_ = json.NewEncoder(out).Encode(obj{"ok": false, "code": "review_admission_source_unavailable"})
		return 1
	}
	result, err := projectReviewAdmission(b)
	if err != nil {
		_ = json.NewEncoder(out).Encode(obj{"ok": false, "code": err.Error()})
		return 1
	}
	result["producer"] = reviewAdmissionProducer()
	if json.NewEncoder(out).Encode(result) != nil {
		return 1
	}
	return 0
}

func reviewAdmissionEnum(parent obj, key string, allowed string) obj {
	v, present := parent[key]
	if !present {
		return obj{"state": "absent"}
	}
	if v == nil {
		return obj{"state": "null"}
	}
	text, ok := v.(string)
	if !ok || text == "" || len(text) > 128 {
		return obj{"state": "invalid"}
	}
	for _, value := range strings.Split(allowed, "|") {
		if text == value {
			return obj{"state": "recorded", "value": text}
		}
	}
	return obj{"state": "unrecognized"}
}

func reviewAdmissionTime(parent obj, key string) obj {
	v, present := parent[key]
	if !present {
		return obj{"state": "absent"}
	}
	if v == nil {
		return obj{"state": "null"}
	}
	number, ok := v.(json.Number)
	if ok && len(number) <= 40 {
		seconds, err := number.Float64()
		if err == nil && !math.IsNaN(seconds) && !math.IsInf(seconds, 0) && seconds >= 0 && seconds <= 253402300799 {
			return obj{"state": "recorded", "epoch_seconds": number}
		}
	}
	return obj{"state": "invalid"}
}

// Preserve exactly recorded aliases. No requested value becomes an actual model.
func reviewAdmissionSelection(parent obj) obj {
	return obj{
		"model":         reviewAdmissionEnum(parent, "model", "gpt-6.1-sol|gpt-6-sol|gpt-6-luna|gpt-6-astra|anthropic/claude-opus-5-5|anthropic/claude-sonnet-5-5|anthropic/claude-haiku-5-5|claude-opus-5-5|claude-sonnet-5-5|claude-haiku-5-5|opus|sonnet|haiku|auto"),
		"effort":        reviewAdmissionEnum(parent, "effort", "none|minimal|low|medium|high|xhigh|max"),
		"model_origin":  reviewAdmissionEnum(parent, "model_origin", "policy|owner_request"),
		"effort_origin": reviewAdmissionEnum(parent, "effort_origin", "policy|owner_request"),
	}
}

func reviewAdmissionSelectionContainer(parent obj, key string) obj {
	result, source := requiredChecksContainer(parent, key)
	if source != nil {
		result["recorded_values"] = reviewAdmissionSelection(source)
	}
	return result
}

func reviewAdmissionScheduler(row obj) obj {
	result, scheduler := requiredChecksContainer(row, "scheduler")
	if scheduler == nil {
		return result
	}
	result["phase"] = reviewAdmissionEnum(scheduler, "phase", "preparing|prepared|starting|running|retry|done|failed|finished")
	result["not_before"] = reviewAdmissionTime(scheduler, "not_before")
	result["selection"] = reviewAdmissionSelectionContainer(scheduler, "selection")
	result["selection_input"] = reviewAdmissionSelectionContainer(scheduler, "selection_input")
	request, input := requiredChecksContainer(scheduler, "console_request")
	if input != nil {
		request["options"] = reviewAdmissionSelectionContainer(input, "options")
	}
	result["console_request"] = request
	wait, saved := requiredChecksContainer(scheduler, "resource_wait")
	if saved != nil {
		wait["reason"] = reviewAdmissionEnum(saved, "reason", "quota_exhausted|resource_wait|rate_limit|capacity")
		wait["until"] = reviewAdmissionTime(saved, "until")
	}
	result["resource_wait"] = wait
	failure, saved := requiredChecksContainer(scheduler, "preparation_failure")
	if saved != nil {
		failure["reason"] = reviewAdmissionEnum(saved, "reason", "cwd_missing|non_git|origin_missing|branch_mismatch|repository_mismatch|tool_missing|resource_wait|input_rejected|excluded_resource|tool_failed")
		failure["at"] = reviewAdmissionTime(saved, "at")
	}
	result["preparation_failure"] = failure
	return result
}

func reviewAdmissionProducer() obj {
	result := obj{"state": "unavailable", "runtime_scheduler_version_verified": false}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return result
	}
	for _, field := range info.Settings {
		if field.Key == "vcs.revision" && len(field.Value) == 40 {
			if _, err := hex.DecodeString(field.Value); err == nil {
				result["state"] = "recorded"
				result["compiled_revision"] = strings.ToLower(field.Value)
			}
		}
		if field.Key == "vcs.modified" && (field.Value == "true" || field.Value == "false") {
			result["compiled_source_modified"] = field.Value == "true"
		}
	}
	return result
}

// These are exact completed normal API receipts, not requested models. The
// existing collector independently binds each result SHA/time to its real turn.
func reviewAdmissionExpectedRoots() []omb3255ExpectedRoot {
	return []omb3255ExpectedRoot{
		{"18354623-b5f5-4844-9cbd-7e195fb4e6c5", 1792934743070, 1792934894306, "573cbbd16d50f6923de53b2ba7cec4992687189791548d50bff989b668469135"},
		{"f62d2ae2-ca93-4009-8ae7-6ce748d00960", 1792934991980, 1792935076088, "3dbbb1b568ad83e0cc30e5e3b75bf77e88199ce4248cd8e6e53c2541b12f478b"},
		{"9fac998f-4bd0-463d-8916-31efadadeb9b", 1792935161876, 1792935212850, "96d42c846b7eb9828dcf41d44b3cf9955c125f518a713d48360d2c0c9b15f815"},
	}
}

func projectReviewAdmission(b []byte) (obj, error) {
	fail := func(code string) (obj, error) { return nil, errors.New("review_admission_" + code) }
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
		if id != 3649 {
			continue
		}
		if selected != nil {
			return fail("ambiguous_record")
		}
		if row["project"] != "localai-web-rs" {
			return fail("scope_mismatch")
		}
		selected = row
	}
	if selected == nil {
		return fail("record_missing")
	}
	stops := obj{}
	for _, key := range []string{"stopped", "owner_stopped", "owner_postponement_recorded"} {
		stops[key] = reviewField(selected, key, "bool")
	}
	for _, key := range []string{"wait_ready", "owner_question_wait", "decision_wait"} {
		stops[key], _ = requiredChecksContainer(selected, key)
	}
	stops["stop_kind"] = reviewAdmissionEnum(selected, "stop_kind", "owner|owner_stop|decision_wait|dependency_wait|resource_wait|technical_wait")
	return obj{
		"ok": true, "domain": "unstarted-review-admission-metadata-v1", "case_todo_id": 3255,
		"snapshot_sha256": digest(b), "metadata_only": true,
		"record": obj{"record_id": int64(3649), "status": reviewField(selected, "status", "status"),
			"dispatchable": reviewField(selected, "dispatchable", "bool"), "jobs": reviewField(selected, "jobs", "jobs"),
			"dependency_references": reviewField(selected, "wait_on", "jobs"), "stops": stops,
			"requested": reviewAdmissionSelection(selected), "scheduler": reviewAdmissionScheduler(selected)},
		"model_execution_observed": false, "origin_independently_verified": false, "quota_currently_observed": false,
		"launch_performed": false, "fallback_authorized": false, "execution_verified": false,
		"data_access_granted": false, "live_state_changed": false,
		"source": "persisted_todo_snapshot", "source_scheduler_version": obj{"state": "unavailable"},
	}, nil
}
