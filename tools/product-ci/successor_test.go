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

func TestSuccessorPlanRoundTripHooksAndLiveAdmission(t *testing.T) {
	for _, kind := range []string{"secret", "codeql"} {
		t.Run(kind, func(t *testing.T) {
			pin, review, acceptance, read, path, workflow := successorPinFixture(t, kind, 1016)
			b, err := parseCurrentPRSnapshot(pin, review, acceptance)
			if err != nil {
				t.Fatal(err)
			}
			p, err := planCurrentPRWithPolicy(context.Background(), b, read, path, workflow)
			if err != nil {
				t.Fatal(err)
			}
			packet := p
			packet.Repo = "" // The name is carried by the separate private packet.
			data, err := json.Marshal(packet)
			if err != nil {
				t.Fatal(err)
			}
			var round codeQLPlan
			if err := json.Unmarshal(data, &round); err != nil || round != packet || !validGuestPlan(round) {
				t.Fatal("successor authority changed across host/guest JSON")
			}
			round.Repo = p.Repo
			contextSHA := p.SHA
			if kind == "secret" {
				contextSHA = p.ContextSHA
			}
			if err := checkCodeQLHook(round, p.Repo, contextSHA, fmt.Sprint(p.RunID), fmt.Sprint(p.Attempt)); err != nil {
				t.Fatal(err)
			}
			if checkCodeQLHook(round, p.Repo, strings.Repeat("e", 40), fmt.Sprint(p.RunID), fmt.Sprint(p.Attempt)) == nil {
				t.Fatal("changed successor hook source accepted")
			}
			if kind != "codeql" {
				return
			}
			assigned := func(ctx context.Context, endpoint string, out any) error {
				if err := read(ctx, endpoint, out); err != nil {
					return err
				}
				if strings.Contains(endpoint, "/actions/jobs/") {
					data, _ := json.Marshal(out)
					var job map[string]any
					json.Unmarshal(data, &job)
					job["runner_name"], job["runner_id"], job["status"] = "owned-successor", 44, "in_progress"
					data, _ = json.Marshal(job)
					return json.Unmarshal(data, out)
				}
				return nil
			}
			c := candidate{b.RepoID, b.PR, b.Head}
			if err := verifyCodeQLAdmissionPlan(context.Background(), c, p, "owned-successor", assigned); err != nil {
				t.Fatal(err)
			}
			stale := func(ctx context.Context, endpoint string, out any) error {
				if strings.Contains(endpoint, "/pulls/1016") {
					return fmt.Errorf("synthetic source changed")
				}
				return assigned(ctx, endpoint, out)
			}
			if verifyCodeQLAdmissionPlan(context.Background(), c, p, "owned-successor", stale) == nil {
				t.Fatal("live successor source change accepted")
			}
		})
	}
}

func TestSuccessorBindingsCannotEnterOldFiniteBatch(t *testing.T) {
	for _, index := range []int{0, 1} {
		p := batchFixture()
		b := p.Batch[index]
		b.OriginTodo, b.OwnerScopeSHA256 = 3056, successorReceiptFixture
		if !b.valid() {
			t.Fatal("positive successor control invalid")
		}
		p.Batch[index] = b
		if validFiniteBindings(p.Batch) || validFiniteSecretBatch(p) {
			t.Fatal("successor authority entered finite batch")
		}
		if index == 1 && finiteDependencyBinding(b) {
			t.Fatal("successor entered old dependency route")
		}
	}
}
