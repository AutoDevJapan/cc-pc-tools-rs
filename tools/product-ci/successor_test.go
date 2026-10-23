package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

const successorReceiptFixture = "279e270a1deae7918e7f3efb17f610ecc3891852f7e12b020bdad26aa3d1ae7b"

func successorPinFixture(t *testing.T, kind string, pr int) ([]byte, []byte, []byte, api, string, string) {
	t.Helper()
	b, originalRead, path, workflow := currentPRFixture(t, kind, nil)
	b.PR = pr
	outputs := map[string][]byte{}
	for _, role := range []string{"review", "acceptance"} {
		original := []byte("synthetic source-bound successor " + role)
		v := currentPREvidence{OriginTodo: 3056, RepoID: b.RepoID, PR: pr, Head: b.Head, Tree: b.Tree, Kind: role, Success: true, OriginalSHA256: digest(original), Original: original}
		outputs[role], _ = json.Marshal(v)
	}
	b.ReviewSHA256, b.AcceptanceSHA256 = digest(outputs["review"]), digest(outputs["acceptance"])
	encoded, _ := json.Marshal(currentPRInput{currentPRBinding: b, ReviewFile: "canonical-parent-review", AcceptanceFile: "canonical-parent-acceptance"})
	var pin map[string]any
	json.Unmarshal(encoded, &pin)
	pin["origin_todo_id"], pin["owner_scope_sha256"] = 3056, successorReceiptFixture
	encoded, _ = json.Marshal(pin)
	read := func(ctx context.Context, endpoint string, out any) error {
		endpoint = strings.ReplaceAll(endpoint, fmt.Sprintf("/pulls/%d", pr), "/pulls/995")
		var raw json.RawMessage
		if err := originalRead(ctx, endpoint, &raw); err != nil {
			return err
		}
		raw = []byte(strings.ReplaceAll(string(raw), `"number":995`, fmt.Sprintf(`"number":%d`, pr)))
		return json.Unmarshal(raw, out)
	}
	return encoded, outputs["review"], outputs["acceptance"], read, path, workflow
}

func TestSuccessorSourceBoundParserPlannerAndGuest(t *testing.T) {
	for _, pr := range []int{1016, 1017} {
		for _, kind := range []string{"secret", "codeql"} {
			t.Run(fmt.Sprint(pr)+kind, func(t *testing.T) {
				pin, review, acceptance, read, path, workflow := successorPinFixture(t, kind, pr)
				b, err := parseCurrentPRSnapshot(pin, review, acceptance)
				if err != nil {
					t.Fatalf("qualified successor did not reach planner: %v", err)
				}
				p, err := planCurrentPRWithPolicy(context.Background(), b, read, path, workflow)
				if err != nil || !validGuestPlan(p) || p.PR != pr || p.Current != b {
					t.Fatalf("successor source/guest rejected: %v", err)
				}
				if err := checkCurrentPRReady(context.Background(), p, read); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestSuccessorRejectsUnqualifiedScopeAndOldSource(t *testing.T) {
	for _, fault := range []string{"missing-owner", "other-owner", "missing-parent", "other-parent", "other-repo", "other-pr", "old-head", "swapped-role"} {
		t.Run(fault, func(t *testing.T) {
			pin, review, acceptance, _, _, _ := successorPinFixture(t, "secret", 1016)
			var value map[string]any
			json.Unmarshal(pin, &value)
			switch fault {
			case "missing-owner":
				delete(value, "owner_scope_sha256")
			case "other-owner":
				value["owner_scope_sha256"] = strings.Repeat("e", 64)
			case "missing-parent":
				delete(value, "origin_todo_id")
			case "other-parent":
				value["origin_todo_id"] = 3059
			case "other-repo":
				value["repo_id"] = 1285649125
			case "other-pr":
				value["pr"] = 1017
			case "old-head":
				value["head_sha"] = strings.Repeat("e", 40)
			case "swapped-role":
				review, acceptance = acceptance, review
			}
			pin, _ = json.Marshal(value)
			if _, err := parseCurrentPRSnapshot(pin, review, acceptance); err == nil {
				t.Fatal("scope or source mismatch accepted")
			}
		})
	}
}
