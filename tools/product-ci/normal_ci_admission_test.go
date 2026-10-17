package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func normalJobFixture(t *testing.T, fault string) (normalCIJobPin, api) {
	t.Helper()
	c := candidates[2]
	data, e := proposeNormalCIRoute(normalCIFixture(), digest(normalCIFixture()))
	if e != nil {
		t.Fatal(e)
	}
	for _, route := range normalCIRoutes() {
		minutes := 5
		if route.Job == "verify" {
			minutes = 30
		}
		if route.Job == "windows-native-check" {
			minutes = 20
		}
		prefix := fmt.Sprintf("  %s:\r\n    runs-on: %s # unchanged comment\r\n    timeout-minutes: ", route.Job, normalCIRunnerExpression(route))
		data = []byte(strings.Replace(string(data), prefix+"20", prefix+fmt.Sprint(minutes), 1))
	}
	p := normalCIJobPin{Source: c, Tree: "f7eea82c96449b7749f98dd6c4b0344b64f1d651", WorkflowSHA: digest(data), RunID: 777, Attempt: 1, Role: "verify"}
	repo := map[string]any{"id": c.ID, "full_name": "AutoDevJapan/GoDD-MCP-Alpha", "fork": false}
	run := map[string]any{"id": p.RunID, "run_attempt": 1, "path": ".github/workflows/ci.yml", "event": "workflow_dispatch", "head_sha": c.SHA, "head_branch": "codex/todo2641-http-response-pseudo-20261015", "status": "in_progress", "repository": repo, "head_repository": repo}
	job := map[string]any{"id": int64(888), "run_id": p.RunID, "name": "verify", "status": "queued", "runner_id": 0, "runner_name": "", "labels": []string{"cc-product-2737-1154066087-linux"}}
	switch fault {
	case "wrong-run-sha":
		run["head_sha"] = strings.Repeat("a", 40)
	case "wrong-attempt":
		run["run_attempt"] = 2
	case "wrong-event":
		run["event"] = "pull_request"
	case "wrong-workflow":
		run["path"] = "other.yml"
	case "other-run":
		job["run_id"] = 778
	case "other-label":
		job["labels"] = []string{"ubuntu-latest"}
	case "other-job":
		job["name"] = "foreign"
	case "already-claimed":
		job["status"] = "in_progress"
		job["runner_id"] = 9
		job["runner_name"] = "foreign"
	case "bad-tree":
		p.Tree = strings.Repeat("b", 40)
	case "live", "foreign-live":
		p.RunnerID = 19
		p.RunnerName = "todo2737-" + strings.Repeat("a", 32)
		job["status"] = "in_progress"
		job["runner_id"] = p.RunnerID
		job["runner_name"] = p.RunnerName
		if fault == "foreign-live" {
			job["runner_id"] = 20
		}
	}
	legacy := normalCIAPI(t, fault)
	return p, func(ctx context.Context, key string, out any) error {
		var value any
		switch key {
		case "repos/AutoDevJapan/GoDD-MCP-Alpha/actions/runs/777":
			value = run
		case "repos/AutoDevJapan/GoDD-MCP-Alpha/actions/variables/" + normalCIRouteVariable:
			value = map[string]string{"name": normalCIRouteVariable, "value": "enabled"}
		case "repos/AutoDevJapan/GoDD-MCP-Alpha/actions/runs/777/attempts/1/jobs?per_page=100":
			value = map[string]any{"total_count": 1, "jobs": []any{job}}
		case "repos/AutoDevJapan/GoDD-MCP-Alpha/contents/.github/workflows/ci.yml?ref=" + c.SHA:
			value = map[string]any{"encoding": "base64", "content": base64.StdEncoding.EncodeToString(data)}
		default:
			var original any
			if e := legacy(ctx, strings.ReplaceAll(key, "AutoDevJapan/GoDD-MCP-Alpha", "AutoDevJapan/opaque-normal-ci"), &original); e != nil {
				return e
			}
			b, e := json.Marshal(original)
			if e != nil {
				return e
			}
			b = []byte(strings.ReplaceAll(string(b), "AutoDevJapan/opaque-normal-ci", "AutoDevJapan/GoDD-MCP-Alpha"))
			return json.Unmarshal(b, out)
		}
		b, e := json.Marshal(value)
		if e != nil {
			return e
		}
		return json.Unmarshal(b, out)
	}
}
func TestNormalCIJobBindingBeforeAndAfterOfficialClaim(t *testing.T) {
	for _, phase := range []string{"", "live"} {
		p, read := normalJobFixture(t, phase)
		b, e := normalCIJobPlan(context.Background(), p, read)
		if e != nil || b.JobID != 888 || b.RunID != 777 || b.TimeoutMinutes != 30 || b.SHA != p.Source.SHA || b.Role != "verify" {
			t.Fatalf("official binding rejected: %v", e)
		}
	}
}
func TestNormalCIRejectsSourceRunJobAndRunnerSubstitution(t *testing.T) {
	for _, fault := range []string{"foreign-owner", "advanced-head", "head-advanced-during-inspection", "wrong-run-sha", "wrong-attempt", "wrong-event", "wrong-workflow", "other-run", "other-label", "other-job", "already-claimed", "bad-tree", "foreign-live"} {
		t.Run(fault, func(t *testing.T) {
			p, read := normalJobFixture(t, fault)
			if _, e := normalCIJobPlan(context.Background(), p, read); e == nil {
				t.Fatal("substituted source/run/job/runner accepted")
			}
		})
	}
}
