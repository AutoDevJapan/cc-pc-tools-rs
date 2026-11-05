package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"
)

// All fixtures are synthetic; these tests never open the PC task ledger.
var domainAt = time.Date(2026, 11, 5, 1, 0, 0, 0, time.UTC)

func domainFixture(items ...string) json.RawMessage {
	return json.RawMessage(`{"next_id":100,"future_root":{"keep":true},"items":[` + strings.Join(items, ",") + `]}`)
}
func domainRows(t *testing.T, updates []json.RawMessage) map[int64]map[string]json.RawMessage {
	t.Helper()
	rows := make(map[int64]map[string]json.RawMessage)
	for _, update := range updates {
		var row map[string]json.RawMessage
		if err := json.Unmarshal(update, &row); err != nil {
			t.Fatal(err)
		}
		id, err := strconv.ParseInt(string(row["id"]), 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		if rows[id] != nil {
			t.Fatalf("duplicate upsert %d", id)
		}
		rows[id] = row
	}
	return rows
}
func domainEdit(t *testing.T, document json.RawMessage, title, note, status string, percent *int) map[int64]map[string]json.RawMessage {
	t.Helper()
	before := bytes.Clone(document)
	updates, err := editDocument(document, 1, title, note, status, percent, domainAt)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(document, before) {
		t.Fatal("input snapshot was mutated")
	}
	return domainRows(t, updates)
}
func domainWant(t *testing.T, row map[string]json.RawMessage, key, want string) {
	t.Helper()
	if string(row[key]) != want {
		t.Errorf("%s = %s, want %s", key, row[key], want)
	}
}
func domainAbsent(t *testing.T, row map[string]json.RawMessage, keys ...string) {
	t.Helper()
	for _, key := range keys {
		if _, exists := row[key]; exists {
			t.Errorf("%s should be absent", key)
		}
	}
}
func domainReject(t *testing.T, document json.RawMessage, title, note, status string, percent *int) {
	t.Helper()
	before := bytes.Clone(document)
	updates, err := editDocument(document, 1, title, note, status, percent, domainAt)
	if err == nil || len(updates) != 0 {
		t.Fatalf("rejected edit returned %d upserts, error %v", len(updates), err)
	}
	if !bytes.Equal(before, document) {
		t.Fatal("failed edit mutated snapshot")
	}
}

func TestDomainEditsPreserveUnknownFieldsTypesAndCase(t *testing.T) {
	row := map[string]json.RawMessage{}
	if err := json.Unmarshal([]byte(`{"id":1,"title":"架空の作業","note":"旧メモ","status":null,"progress":"40","updated":{"legacy":7},"lastJob":null,"lastjob":{"counter":9007199254740993},"jobs":["synthetic"],"note_history":[{"note":"以前","updated":1},"legacy"],"note_history_archives":[{"sha":"synthetic"}],"spec_history":{"legacy":true}}`), &row); err != nil {
		t.Fatal(err)
	}
	values := []json.RawMessage{json.RawMessage(`null`), json.RawMessage(`true`), json.RawMessage(`1.23000e-40`), json.RawMessage(`18446744073709551615`), json.RawMessage(`{"literal":"<>&","nested":[1.0,null,"日本語"]}`)}
	for i := 0; i < 574; i++ {
		row[fmt.Sprintf("future_%03d", i)] = values[i%len(values)]
	}
	// The standard encoder is deliberately not used here: the original lexemes
	// include HTML characters and numbers that must survive untouched.
	document := domainFixture(string(domainObject(row)))
	got := domainEdit(t, document, "架空の作業を修正", "新メモ", "", nil)[1]
	for key, original := range row {
		switch key {
		case "title", "note", "updated", "note_history":
			continue
		}
		if !bytes.Equal(got[key], original) {
			t.Errorf("unedited field %s changed", key)
		}
	}
	domainWant(t, got, "status", "null")
	domainWant(t, got, "updated", `"2026-11-05 10:00"`)
	var history []json.RawMessage
	if err := json.Unmarshal(got["note_history"], &history); err != nil {
		t.Fatal(err)
	}
	if len(history) != 3 {
		t.Fatalf("history length = %d", len(history))
	}
	var original []json.RawMessage
	_ = json.Unmarshal(row["note_history"], &original)
	if !bytes.Equal(history[0], original[0]) || !bytes.Equal(history[1], original[1]) {
		t.Fatal("old history was rewritten")
	}
	var entry map[string]json.RawMessage
	_ = json.Unmarshal(history[2], &entry)
	domainWant(t, entry, "note", `"旧メモ"`)
	domainWant(t, entry, "updated", `{"legacy":7}`)
	if len(entry) != 2 {
		t.Fatal("note history must use CLI note/updated shape")
	}
}

func TestDomainNoteNormalizationAndNoop(t *testing.T) {
	doc := domainFixture(`{"id":1,"title":"架空","note":"a\nb\nc","status":"doing","updated":"old","note_history":[]}`)
	rows := domainEdit(t, doc, "改題", "\ufeffa\r\nb\rc", "doing", nil)
	domainWant(t, rows[1], "note", `"a\nb\nc"`)
	domainWant(t, rows[1], "note_history", `[]`)
	domainAbsent(t, rows[1], "resume_generation", "worker_receipt")
	updates, err := editDocument(doc, 1, "架空", "a\r\nb\r\nc", "doing", nil, domainAt)
	if err != nil || len(updates) != 0 {
		t.Fatalf("normalized no-op returned %d updates: %v", len(updates), err)
	}
	for _, invalid := range []string{"", " \t\r\n", "\x1c\x1f", strings.Repeat("x", (1<<20)+1)} {
		domainReject(t, doc, "改題", invalid, "doing", nil)
	}
}

func TestDomainMalformedHistoryIsNeverReplaced(t *testing.T) {
	for _, value := range []string{`null`, `{ "legacy": true }`, `"legacy"`, `17`, `false`} {
		t.Run(value, func(t *testing.T) {
			doc := domainFixture(`{"id":1,"title":"架空","note":"旧","status":"todo","note_history":` + value + `}`)
			domainReject(t, doc, "架空", "新", "", nil)
			rows := domainEdit(t, doc, "改題", "旧", "block", nil)
			domainWant(t, rows[1], "note_history", value)
		})
	}
	doc := domainFixture(`{"id":1,"title":"架空","status":null,"note":{"legacy":true},"note_history":false}`)
	rows := domainEdit(t, doc, "改題", "", "", nil)
	domainWant(t, rows[1], "note", `{"legacy":true}`)
	domainWant(t, rows[1], "status", `null`)
}

func TestDomainUnspecifiedProgressAndCompletion(t *testing.T) {
	for _, from := range []string{"todo", "doing", "block", "wait", "recovery_unverified", "done"} {
		for _, to := range []string{"doing", "block"} {
			if from == "recovery_unverified" && to == "doing" {
				continue
			}
			for _, saved := range []string{"", `,"progress":42`, `,"progress":"legacy"`} {
				t.Run(from+"_"+to+saved, func(t *testing.T) {
					doc := domainFixture(`{"id":1,"title":"架空","note":"同じ","status":"` + from + `"` + saved + `}`)
					// Change title so unchanged statuses still produce one upsert.
					got := domainEdit(t, doc, "改題", "同じ", to, nil)[1]
					if from == "done" || saved == "" {
						domainAbsent(t, got, "progress")
					} else if strings.Contains(saved, "legacy") {
						domainWant(t, got, "progress", `"legacy"`)
					} else {
						domainWant(t, got, "progress", `42`)
					}
					p := 0
					domainWant(t, domainEdit(t, doc, "架空", "同じ", to, &p)[1], "progress", "0")
				})
			}
		}
	}
	doc := domainFixture(`{"id":1,"title":"架空","note":"保持するメモ","status":"wait","progress":27,"wait_on":[99],"wait_ready":{"condition":"chatgpt-plus"}}`)
	got := domainEdit(t, doc, "架空", "", "done", nil)[1]
	domainWant(t, got, "progress", "100")
	domainWant(t, got, "note", `"保持するメモ"`)
	domainAbsent(t, got, "wait_on", "wait_ready", "note_history")
	for _, p := range []int{-1, 0, 99, 101} {
		domainReject(t, doc, "架空", "保持するメモ", "done", &p)
	}
	done := domainFixture(`{"id":1,"title":"架空","note":"同じ","status":"done","progress":100}`)
	p := 50
	domainReject(t, done, "改題", "同じ", "done", &p)
	for _, p := range []int{-1, 101} {
		domainReject(t, done, "架空", "同じ", "block", &p)
	}
}

func TestDomainResumeWithoutWorkerRetainsOrRetiresOwnership(t *testing.T) {
	for _, phase := range []string{"running", "finished", "cancelled"} {
		for _, received := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s_%v", phase, received), func(t *testing.T) {
				processed := `{"older":{"generation":1,"reason":"saved"}}`
				if received {
					processed = `{"worker":null,"older":{"generation":1,"reason":"saved"}}`
				}
				doc := domainFixture(`{"id":1,"title":"架空","note":"同じ","status":"block","resume_generation":4,"dependency_ready":true,"jobs":["worker"],"lastJob":"upper","lastjob":"lower","scheduler":{"job":"worker","phase":"` + phase + `","key":"key","not_before":123,"selection":{"keep":true},"pending_cancel":true,"notice_pending":true,"lastJob":1,"lastjob":2},"processed_results":` + processed + `}`)
				got := domainEdit(t, doc, "架空", "同じ", "doing", nil)[1]
				domainWant(t, got, "resume_generation", "5")
				domainWant(t, got, "worker_receipt", `{"generation":5,"job":null}`)
				domainWant(t, got, "dependency_ready", "true")
				domainWant(t, got, "lastJob", `"upper"`)
				domainWant(t, got, "lastjob", `"lower"`)
				domainWant(t, got, "jobs", `["worker"]`)
				sc := object(got["scheduler"])
				domainWant(t, sc, "not_before", "123")
				domainWant(t, sc, "selection", `{"keep":true}`)
				domainWant(t, sc, "lastJob", "1")
				domainWant(t, sc, "lastjob", "2")
				if phase == "running" && !received {
					domainWant(t, sc, "job", `"worker"`)
					domainWant(t, sc, "pending_cancel", "true")
				} else {
					domainWant(t, sc, "job", "null")
					domainWant(t, sc, "key", "null")
					domainWant(t, sc, "last_job", `"worker"`)
					domainWant(t, sc, "phase", `"resumed"`)
					domainAbsent(t, sc, "pending_cancel", "notice_pending")
					ledger := object(got["processed_results"])
					if received {
						domainWant(t, ledger, "worker", "null")
					} else {
						domainWant(t, ledger, "worker", `{"generation":5,"reason":"superseded"}`)
					}
					domainWant(t, ledger, "older", `{"generation":1,"reason":"saved"}`)
				}
			})
		}
	}
	for _, value := range []string{`null`, `"legacy"`, `[]`, `false`} {
		doc := domainFixture(`{"id":1,"title":"架空","note":"同じ","status":"todo","scheduler":` + value + `}`)
		domainWant(t, domainEdit(t, doc, "架空", "同じ", "doing", nil)[1], "scheduler", value)
	}
	for _, counter := range []struct{ saved, next string }{{`null`, `1`}, {`2.5`, `3.5`}, {`2.0`, `3.0`}, {`"legacy"`, `1.0`}, {`9007199254740993`, `9007199254740994`}} {
		doc := domainFixture(`{"id":1,"title":"架空","note":"同じ","status":"todo","resume_generation":` + counter.saved + `}`)
		domainWant(t, domainEdit(t, doc, "架空", "同じ", "doing", nil)[1], "resume_generation", counter.next)
	}
	// A malformed result history must not be discarded to retire a worker.
	doc := domainFixture(`{"id":1,"title":"架空","note":"同じ","status":"block","scheduler":{"job":"worker","phase":"finished"},"processed_results":["legacy"]}`)
	domainReject(t, doc, "架空", "同じ", "doing", nil)
}

func TestDomainDoneReleasesDependenciesWithoutRemovingOwnerBlock(t *testing.T) {
	doc := domainFixture(
		`{"id":1,"title":"架空","note":"同じ","status":"doing"}`,
		`{"id":2,"status":"wait","wait_on":[1],"dependency_generation":7,"updated":"unchanged","jobs":["old"],"scheduler":{"job":"old","key":"k","selection":{},"phase":"waiting","not_before":99,"worktree":"synthetic","lastJob":1,"lastjob":2},"stall_recovery":{"pending":true,"report":"saved","since":17,"next_at":19,"unknown":true},"discord_restart_not_before":888}`,
		`{"id":3,"status":"block","wait_on":[1],"scheduler":{"job":"held","phase":"blocked"},"owner_question_wait":{"keep":true}}`,
		`{"id":4,"status":"wait","wait_on":[1,99]}`,
		`{"id":5,"status":"wait","wait_on":[1],"wait_ready":{"condition":"retry-time","since":1,"not_before":61}}`,
		`{"id":6,"status":"doing","wait_on":[1],"scheduler":false}`,
		`{"id":7,"status":"wait","wait_on":[1],"dependency_generation":18446744073709551615,"scheduler":"legacy","stall_recovery":["legacy"]}`,
	)
	rows := domainEdit(t, doc, "架空", "同じ", "done", nil)
	if len(rows) != 5 || rows[4] != nil || rows[5] != nil {
		t.Fatalf("wrong dependency upsert set (%d rows)", len(rows))
	}
	domainWant(t, rows[2], "status", `"todo"`)
	domainWant(t, rows[2], "dependency_ready", "true")
	domainWant(t, rows[2], "dependency_generation", "8")
	domainWant(t, rows[2], "updated", `"unchanged"`)
	domainWant(t, rows[2], "jobs", `["old"]`)
	domainAbsent(t, rows[2], "wait_on", "discord_restart_not_before")
	sc := object(rows[2]["scheduler"])
	domainAbsent(t, sc, "job", "key", "selection")
	domainWant(t, sc, "last_job", `"old"`)
	domainWant(t, sc, "phase", `"retry"`)
	domainWant(t, sc, "not_before", "0")
	domainWant(t, sc, "resume", "true")
	domainWant(t, sc, "worktree", `"synthetic"`)
	domainWant(t, sc, "lastJob", "1")
	domainWant(t, sc, "lastjob", "2")
	recovery := object(rows[2]["stall_recovery"])
	domainAbsent(t, recovery, "pending", "report", "since")
	domainWant(t, recovery, "next_at", "0")
	domainWant(t, recovery, "unknown", "true")
	domainWant(t, rows[3], "status", `"block"`)
	domainWant(t, rows[3], "scheduler", `{"job":"held","phase":"blocked"}`)
	domainWant(t, rows[3], "owner_question_wait", `{"keep":true}`)
	domainAbsent(t, rows[3], "wait_on", "dependency_ready", "dependency_generation")
	domainWant(t, rows[6], "status", `"doing"`)
	domainWant(t, rows[6], "scheduler", "false")
	domainWant(t, rows[7], "dependency_generation", "18446744073709551615")
	domainWant(t, rows[7], "scheduler", `"legacy"`)
	domainWant(t, rows[7], "stall_recovery", `["legacy"]`)
}

func TestDomainReleaseTimingAndMixedDependencyTypes(t *testing.T) {
	for _, status := range []string{"", "block", "doing"} {
		doc := domainFixture(`{"id":1,"title":"架空","note":"同じ","status":"todo"}`, `{"id":2,"status":"wait","wait_on":[3]}`, `{"id":3,"status":"done"}`)
		rows := domainEdit(t, doc, "改題", "同じ", status, nil)
		if (rows[2] != nil) != (status == "doing") {
			t.Errorf("release timing for %q", status)
		}
	}
	doc := domainFixture(`{"id":1,"title":"架空","note":"同じ","status":"wait","wait_on":[2,"missing",null,2.0,{},true],"scheduler":{"job":"old","phase":"waiting"}}`, `{"id":2,"status":"done"}`)
	got := domainEdit(t, doc, "架空", "同じ", "doing", nil)[1]
	domainWant(t, got, "status", `"doing"`)
	domainWant(t, got, "dependency_generation", "1")
	domainWant(t, got, "dependency_ready", "true")
	domainWant(t, object(got["scheduler"]), "phase", `"retry"`)
	domainWant(t, object(got["scheduler"]), "last_job", `"old"`)
	domainAbsent(t, got, "wait_on")
	for _, deps := range []string{`[2]`, `[99]`, `[1]`} {
		doc := domainFixture(`{"id":1,"title":"架空","note":"同じ","status":"wait","wait_on":`+deps+`}`, `{"id":2,"status":"doing"}`)
		domainReject(t, doc, "改題", "同じ", "doing", nil)
	}
}

func TestDomainReadinessAndRecovery(t *testing.T) {
	for _, tc := range []struct {
		name, wait string
		allowed    bool
	}{
		{"null", `null`, true},
		{"due", `{"condition":"retry-time","since":1793840300,"not_before":1793840399}`, true},
		{"future", `{"condition":"retry-time","since":1793840400,"not_before":1793840460}`, false},
		{"too_short", `{"condition":"retry-time","since":1793840300,"not_before":1793840301}`, false},
		{"too_long", `{"condition":"retry-time","since":1793700000,"not_before":1793840399}`, false},
		{"backwards", `{"condition":"retry-time","since":1793840399,"not_before":1793840300}`, false},
		{"fraction", `{"condition":"retry-time","since":1793840300.0,"not_before":1793840399}`, false},
		{"external", `{"condition":"chatgpt-plus","since":1}`, false},
		{"unknown", `false`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := domainFixture(`{"id":1,"title":"架空","note":"同じ","status":"wait","wait_on":[2],"wait_ready":`+tc.wait+`}`, `{"id":2,"status":"done"}`)
			if !tc.allowed {
				domainReject(t, doc, "架空", "同じ", "doing", nil)
				return
			}
			got := domainEdit(t, doc, "架空", "同じ", "doing", nil)[1]
			domainAbsent(t, got, "wait_ready")
			if tc.name == "due" {
				// Start calls release_ready before removing wait_ready, so this
				// already-complete edge remains, exactly as in the CLI.
				domainWant(t, got, "wait_on", `[2]`)
				domainAbsent(t, got, "dependency_ready")
			}
		})
	}
	doc := domainFixture(`{"id":1,"title":"架空","note":"同じ","status":"recovery_unverified","recovery":{"keep":true}}`)
	domainReject(t, doc, "改題", "同じ", "doing", nil)
	for _, status := range []string{"", "block", "done"} {
		got := domainEdit(t, doc, "改題", "同じ", status, nil)[1]
		domainWant(t, got, "recovery", `{"keep":true}`)
	}
}

func TestDomainCapacityCompletionUsesOnlyTheOriginalContract(t *testing.T) {
	for _, tc := range []struct {
		name, fields string
		allowed      bool
	}{
		{"ordinary", ``, true},
		{"legacy_cycle", `,"capacity_cycle":{"legacy":true}`, true},
		{"null", `,"capacity_decision":null`, true},
		{"other_key", `,"capacity_decision":{"key":"other"}`, true},
		{"missing_result", `,"capacity_decision":{"key":"capacity-decision-v1","active_generation":"g"}`, false},
		{"stale_result", `,"capacity_decision":{"key":"capacity-decision-v1","active_generation":"g","result":{"generation":"old"}}`, false},
		{"matching", `,"capacity_decision":{"key":"capacity-decision-v1","active_generation":"g","result":{"generation":"g","keep":true}}`, true},
		{"typed_numbers", `,"capacity_decision":{"key":"capacity-decision-v1","active_generation":1,"result":{"generation":1.0}}`, false},
		{"large_numbers", `,"capacity_decision":{"key":"capacity-decision-v1","active_generation":9007199254740992,"result":{"generation":9007199254740993}}`, false},
		{"float_values", `,"capacity_decision":{"key":"capacity-decision-v1","active_generation":1.0,"result":{"generation":1e0}}`, true},
		{"missing_generations", `,"capacity_decision":{"key":"capacity-decision-v1","result":{}}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := domainFixture(`{"id":1,"title":"架空","note":"同じ","status":"todo"` + tc.fields + `}`)
			if tc.allowed {
				domainWant(t, domainEdit(t, doc, "架空", "同じ", "done", nil)[1], "progress", "100")
			} else {
				domainReject(t, doc, "改題", "同じ", "done", nil)
				// Incomplete measurements do not forbid ordinary edits or blocks.
				domainWant(t, domainEdit(t, doc, "改題", "同じ", "block", nil)[1], "status", `"block"`)
			}
		})
	}
}

func TestDomainRejectsInvalidCollectionsAndDoesNotInventStatusTransitions(t *testing.T) {
	for _, doc := range []string{`null`, `{}`, `{"items":null}`, `{"items":{}}`, `{"items":[null]}`, `{"items":[{"id":1},{"id":1}]}`, `{"items":[{"id":"1"}]}`, `{"items":[{"id":2}]}`} {
		domainReject(t, json.RawMessage(doc), "架空", "同じ", "", nil)
	}
	doc := domainFixture(`{"id":1,"title":"架空","note":"同じ","status":"doing"}`)
	for _, status := range []string{"todo", "wait", "recovery_unverified", "unknown"} {
		domainReject(t, doc, "架空", "同じ", status, nil)
	}
	domainReject(t, doc, " \t", "同じ", "", nil)
	legacy := domainFixture(`{"id":1,"title":"架空","note":"同じ","status":{"legacy":true},"progress":false}`)
	got := domainEdit(t, legacy, "改題", "同じ", "", nil)[1]
	domainWant(t, got, "status", `{"legacy":true}`)
	domainWant(t, got, "progress", `false`)
}
