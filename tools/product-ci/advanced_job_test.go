package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
)

func advancedFixture(c candidate, fault string) api {
	repo := map[string]any{"id": c.ID, "full_name": "AutoDevJapan/GoDD-MCP-Alpha", "private": true, "fork": false, "owner": map[string]any{"login": "AutoDevJapan"}, "permissions": map[string]any{"admin": true}}
	pr := map[string]any{"state": "open", "head": map[string]any{"sha": c.SHA, "ref": "codex/todo2641-http-response-pseudo-20261015", "repo": repo}, "base": map[string]any{"repo": repo}}
	run := map[string]any{"id": 7, "run_attempt": 1, "path": ".github/workflows/codeql.yml", "event": "workflow_dispatch", "head_sha": c.SHA, "head_branch": "codex/todo2641-http-response-pseudo-20261015", "status": "queued", "repository": repo, "head_repository": repo}
	data := map[string]any{fmt.Sprintf("repositories/%d", c.ID): repo, fmt.Sprintf("repos/AutoDevJapan/GoDD-MCP-Alpha/pulls/%d", c.PR): pr, "repos/AutoDevJapan/GoDD-MCP-Alpha/git/ref/heads/codex/todo2641-http-response-pseudo-20261015": map[string]any{"object": map[string]any{"sha": c.SHA}}, "repos/AutoDevJapan/GoDD-MCP-Alpha/code-scanning/default-setup": map[string]any{"state": "not-configured"}, "repos/AutoDevJapan/GoDD-MCP-Alpha/actions/runs/7": run, "repos/AutoDevJapan/GoDD-MCP-Alpha/contents/.github/workflows/codeql.yml?ref=" + c.SHA: map[string]any{"type": "file", "encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte("all original codeql checks"))}, "repos/AutoDevJapan/GoDD-MCP-Alpha/git/commits/" + c.SHA: map[string]any{"tree": map[string]any{"sha": strings.Repeat("a", 40)}}}
	const route = "repos/AutoDevJapan/GoDD-MCP-Alpha/actions/variables/CC_PRODUCT_2737_CI"
	data[route] = map[string]any{"name": "CC_PRODUCT_2737_CI", "value": "todo2737-0b2414b10418d265c7823a4f87bb5881"}
	switch fault {
	case "wrong-lease":
		data[route] = map[string]any{"name": "CC_PRODUCT_2737_CI", "value": "todo2737-" + strings.Repeat("d", 32)}
	case "missing-lease":
		delete(data, route)
	case "empty-lease":
		data[route] = map[string]any{"name": "CC_PRODUCT_2737_CI", "value": ""}
	case "coordinated-other-branch":
		pr["head"].(map[string]any)["ref"] = "another-branch"
		run["head_branch"] = "another-branch"
		data["repos/AutoDevJapan/GoDD-MCP-Alpha/git/ref/heads/another-branch"] = map[string]any{"object": map[string]any{"sha": c.SHA}}
	case "coordinated-other-repository":
		repo["full_name"] = "AutoDevJapan/another-repository"
	case "default-enabled":
		data["repos/AutoDevJapan/GoDD-MCP-Alpha/code-scanning/default-setup"] = map[string]any{"state": "configured"}
	case "automatic-pr":
		run["event"] = "pull_request"
	case "wrong-source":
		run["head_sha"] = strings.Repeat("b", 40)
	case "wrong-branch":
		run["head_branch"] = "main"
	case "fork":
		run["head_repository"] = map[string]any{"id": c.ID, "full_name": "AutoDevJapan/GoDD-MCP-Alpha", "fork": true}
	case "finished":
		run["status"] = "completed"
	case "wrong-workflow":
		run["path"] = ".github/workflows/other.yml"
	}
	return func(_ context.Context, key string, out any) error {
		if fault == "coordinated-other-repository" {
			key = strings.ReplaceAll(key, "AutoDevJapan/another-repository", "AutoDevJapan/GoDD-MCP-Alpha")
		}
		key = strings.ReplaceAll(key, url.PathEscape("codex/todo2641-http-response-pseudo-20261015"), "codex/todo2641-http-response-pseudo-20261015")
		v, ok := data[key]
		if !ok {
			return errors.New("unexpected fixture endpoint")
		}
		b, e := json.Marshal(v)
		if e != nil {
			return e
		}
		return json.Unmarshal(b, out)
	}
}

func TestAdvancedCodeQLPreservesFullDispatchAndRejectsOtherSources(t *testing.T) {
	c := advancedCandidate(candidates[2])
	expected := digest([]byte("all original codeql checks"))
	p, e := planAdvancedWithPolicy(context.Background(), c, 7, advancedFixture(c, ""), expected)
	if e != nil || p.SHA != c.SHA || p.Kind != "advanced" || p.ContextSHA != "" || !validGuestPlan(p) {
		t.Fatalf("full original advanced workflow rejected: %v", e)
	}
	if e = checkCodeQLHook(p, p.Repo, c.SHA, "7", "1"); e != nil {
		t.Fatal(e)
	}
	if checkCodeQLHook(p, p.Repo, strings.Repeat("b", 40), "7", "1") == nil {
		t.Fatal("advanced dispatch accepted a merge context")
	}
	for _, fault := range []string{"wrong-lease", "missing-lease", "empty-lease", "coordinated-other-branch", "coordinated-other-repository", "default-enabled", "automatic-pr", "wrong-source", "wrong-branch", "fork", "finished", "wrong-workflow"} {
		t.Run(fault, func(t *testing.T) {
			if _, e = planAdvancedWithPolicy(context.Background(), c, 7, advancedFixture(c, fault), expected); e == nil {
				t.Fatal("changed advanced configuration accepted")
			}
		})
	}
	if _, e = planAdvancedWithPolicy(context.Background(), c, 7, advancedFixture(c, ""), digest([]byte("changed checks"))); e == nil {
		t.Fatal("unreviewed workflow accepted")
	}
}

func TestNewAdvancedPinDoesNotChangeHistoricalOrOtherProductModes(t *testing.T) {
	legacy := candidates[2]
	fixed := advancedCandidate(legacy)
	if legacy.SHA != "b194d66524ef86330bb73ca3f22f9e5dd6ae0a07" || fixed.SHA != advanced2737Head || fixed.ID != legacy.ID || fixed.PR != legacy.PR {
		t.Fatal("explicit advanced source pin changed historical scope")
	}
	for _, c := range []candidate{candidates[0], candidates[1], candidates[3], {ID: legacy.ID, PR: legacy.PR + 1, SHA: legacy.SHA}} {
		if advancedCandidate(c) != c {
			t.Fatal("another product or PR was advanced")
		}
	}
	p := codeQLPlan{RepoID: fixed.ID, PR: fixed.PR, Candidate: fixed.SHA, Kind: "advanced", Label: codeQLLabel(fixed.ID)}
	if !validGuestPlan(p) {
		t.Fatal("fixed advanced source rejected by guest")
	}
	p.Kind = ""
	if validGuestPlan(p) {
		t.Fatal("new advanced source accepted as default setup")
	}
	p.Kind = "secret"
	p.RepoID = candidates[0].ID
	p.PR = candidates[0].PR
	p.Label = secretLabel(p.RepoID)
	p.ContextSHA = strings.Repeat("a", 40)
	if validGuestPlan(p) {
		t.Fatal("new advanced source accepted as secret scan")
	}
}
