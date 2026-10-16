package main

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func productCandidateFixture(t *testing.T) (map[string]any, time.Time) {
	t.Helper()
	at := time.Date(2026, 10, 16, 6, 0, 0, 0, time.UTC)
	proof := productRetryProof{ID: productRetryID, Verified: true, Expires: "2026-10-21T00:00:00Z", SessionVerified: true, Session: productRetrySession, Worktree: productRetryTree, SourceJob: productRetrySourceJob, Request: "original-request.md", RequestHash: productRetryHash, Snapshot: json.RawMessage(`{"id":1626}`)}
	return map[string]any{
		"items": []any{map[string]any{
			"id": productRetryID, "status": "wait", "dispatchable": false,
			"wait_ready": map[string]any{"condition": "retry-time", "since": at.Unix() - 1200, "not_before": at.Unix(), "reason": "fixture technical retry"},
			"jobs":       []any{"fixture-worker"}, "scheduler": map[string]any{"job": "fixture-worker"},
			"worker_receipt":               map[string]any{"job": "fixture-worker", "generation": 6},
			"product_resume_qualification": map[string]any{"receipt": proof},
		}},
	}, at
}

func candidateBytes(t *testing.T, db map[string]any) []byte {
	t.Helper()
	b, err := json.Marshal(db)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func candidateRow(db map[string]any) map[string]any { return db["items"].([]any)[0].(map[string]any) }

func TestProductRetryCandidateRequiresOriginalFiniteDueWait(t *testing.T) {
	tests := []struct {
		name   string
		change func(map[string]any, *time.Time)
	}{
		{"not due", func(db map[string]any, at *time.Time) { *at = at.Add(-time.Second) }},
		{"expired", func(db map[string]any, at *time.Time) { *at = time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC) }},
		{"before scope", func(db map[string]any, at *time.Time) { *at = time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC) }},
		{"different original", func(db map[string]any, _ *time.Time) { candidateRow(db)["id"] = 2475 }},
		{"owner block", func(db map[string]any, _ *time.Time) { candidateRow(db)["status"] = "block" }},
		{"running state", func(db map[string]any, _ *time.Time) { candidateRow(db)["status"] = "doing" }},
		{"done", func(db map[string]any, _ *time.Time) { candidateRow(db)["status"] = "done" }},
		{"dispatch permission changed", func(db map[string]any, _ *time.Time) { candidateRow(db)["dispatchable"] = true }},
		{"missing permission", func(db map[string]any, _ *time.Time) { delete(candidateRow(db), "dispatchable") }},
		{"stopped", func(db map[string]any, _ *time.Time) { candidateRow(db)["stopped"] = true }},
		{"cancel requested", func(db map[string]any, _ *time.Time) { candidateRow(db)["cancel_requested"] = true }},
		{"postponed", func(db map[string]any, _ *time.Time) { candidateRow(db)["owner_postponement_recorded"] = true }},
		{"stop kind", func(db map[string]any, _ *time.Time) { candidateRow(db)["stop_kind"] = "person" }},
		{"decision", func(db map[string]any, _ *time.Time) { candidateRow(db)["decision_wait"] = map[string]any{} }},
		{"scheduler stop", func(db map[string]any, _ *time.Time) {
			candidateRow(db)["scheduler"].(map[string]any)["owner_stop_job"] = "fixture-worker"
		}},
		{"missing dependency", func(db map[string]any, _ *time.Time) { candidateRow(db)["wait_on"] = []any{999} }},
		{"self dependency", func(db map[string]any, _ *time.Time) { candidateRow(db)["wait_on"] = []any{1626} }},
		{"unfinished dependency", func(db map[string]any, _ *time.Time) {
			candidateRow(db)["wait_on"] = []any{999}
			db["items"] = append(db["items"].([]any), map[string]any{"id": 999, "status": "doing"})
		}},
		{"missing source receipt", func(db map[string]any, _ *time.Time) { delete(candidateRow(db), "worker_receipt") }},
		{"source mismatch", func(db map[string]any, _ *time.Time) { candidateRow(db)["scheduler"].(map[string]any)["job"] = "other" }},
		{"too short", func(db map[string]any, _ *time.Time) {
			w := candidateRow(db)["wait_ready"].(map[string]any)
			w["since"] = w["not_before"].(int64) - 59
		}},
		{"too long", func(db map[string]any, _ *time.Time) {
			w := candidateRow(db)["wait_ready"].(map[string]any)
			w["since"] = w["not_before"].(int64) - 86401
		}},
		{"different readiness", func(db map[string]any, _ *time.Time) {
			candidateRow(db)["wait_ready"].(map[string]any)["condition"] = "chatgpt-plus"
		}},
		{"no qualification", func(db map[string]any, _ *time.Time) { delete(candidateRow(db), "product_resume_qualification") }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db, at := productCandidateFixture(t)
			test.change(db, &at)
			before := candidateBytes(t, db)
			candidate, err := selectProductRetry(before, at)
			if err != nil || candidate != nil {
				t.Fatalf("unexpected candidate: %+v %v", candidate, err)
			}
			if !bytes.Equal(before, candidateBytes(t, db)) {
				t.Fatal("metadata changed")
			}
		})
	}
}

func TestProductRetryCandidateRetainsOriginalAndStableQueueKey(t *testing.T) {
	db, at := productCandidateFixture(t)
	candidateRow(db)["wait_on"] = []any{999}
	db["items"] = append(db["items"].([]any), map[string]any{"id": 999, "status": "done"})
	body := candidateBytes(t, db)
	first, err := selectProductRetry(body, at)
	if err != nil || first == nil || len(first.Key) != 32 {
		t.Fatalf("candidate: %+v %v", first, err)
	}
	candidateRow(db)["note"] = "A note does not create a new launch."
	second, err := selectProductRetry(candidateBytes(t, db), at.Add(time.Minute))
	if err != nil || second == nil || first.Key != second.Key {
		t.Fatal("mutable note changed queue key")
	}
	var snapshot map[string]any
	if err := json.Unmarshal(first.Snapshot, &snapshot); err != nil {
		t.Fatal(err)
	}
	var original map[string]any
	var parsed struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatal(err)
	}
	original = parsed.Items[0]
	if !reflect.DeepEqual(snapshot, original) {
		t.Fatal("original state not retained")
	}
	candidateRow(db)["wait_ready"].(map[string]any)["not_before"] = at.Unix() - 1
	third, err := selectProductRetry(candidateBytes(t, db), at)
	if err != nil || third == nil || third.Key == first.Key {
		t.Fatal("new wait reused old queue key")
	}
}

func TestProductRetryRejectsInvalidOrExtendedQualification(t *testing.T) {
	for _, field := range []string{"verified", "id", "session", "session_receipt_verified", "job", "request_sha256", "worktree", "request", "expires_at", "snapshot"} {
		t.Run(field, func(t *testing.T) {
			db, at := productCandidateFixture(t)
			body := candidateBytes(t, db)
			var copyDB map[string]any
			if err := json.Unmarshal(body, &copyDB); err != nil {
				t.Fatal(err)
			}
			proof := candidateRow(copyDB)["product_resume_qualification"].(map[string]any)["receipt"].(map[string]any)
			switch field {
			case "verified", "session_receipt_verified":
				proof[field] = false
			case "id":
				proof[field] = 2475
			case "expires_at":
				proof[field] = "2026-10-22T00:00:00Z"
			case "snapshot":
				proof[field] = nil
			default:
				proof[field] = ""
			}
			if candidate, err := selectProductRetry(candidateBytes(t, copyDB), at); err != nil || candidate != nil {
				t.Fatalf("bad proof: %+v %v", candidate, err)
			}
		})
	}
}

func TestProductRetryDuplicateOriginalFailsClosed(t *testing.T) {
	db, at := productCandidateFixture(t)
	db["items"] = append(db["items"].([]any), candidateRow(db))
	if candidate, err := selectProductRetry(candidateBytes(t, db), at); err == nil || candidate != nil {
		t.Fatal("duplicate identity accepted")
	}
}
