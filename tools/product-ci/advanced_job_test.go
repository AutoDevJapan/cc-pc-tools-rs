package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func advancedFixture(c candidate, fault string) api {
	repo := map[string]any{"id": c.ID, "full_name": "AutoDevJapan/opaque-advanced", "private": true, "fork": false, "owner": map[string]any{"login": "AutoDevJapan"}, "permissions": map[string]any{"admin": true}}
	pr := map[string]any{"state": "open", "head": map[string]any{"sha": c.SHA, "ref": "fixed-branch", "repo": repo}, "base": map[string]any{"repo": repo}}
	run := map[string]any{"id": 7, "run_attempt": 1, "path": ".github/workflows/codeql.yml", "event": "workflow_dispatch", "head_sha": c.SHA, "head_branch": "fixed-branch", "status": "queued", "repository": repo, "head_repository": repo}
	data := map[string]any{fmt.Sprintf("repositories/%d", c.ID): repo, fmt.Sprintf("repos/AutoDevJapan/opaque-advanced/pulls/%d", c.PR): pr, "repos/AutoDevJapan/opaque-advanced/git/ref/heads/fixed-branch": map[string]any{"object": map[string]any{"sha": c.SHA}}, "repos/AutoDevJapan/opaque-advanced/code-scanning/default-setup": map[string]any{"state": "not-configured"}, "repos/AutoDevJapan/opaque-advanced/actions/runs/7": run, "repos/AutoDevJapan/opaque-advanced/contents/.github/workflows/codeql.yml?ref=" + c.SHA: map[string]any{"type": "file", "encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte("all original codeql checks"))}, "repos/AutoDevJapan/opaque-advanced/git/commits/" + c.SHA: map[string]any{"tree": map[string]any{"sha": strings.Repeat("a", 40)}}}
	switch fault {
	case "default-enabled":
		data["repos/AutoDevJapan/opaque-advanced/code-scanning/default-setup"] = map[string]any{"state": "configured"}
	case "automatic-pr":
		run["event"] = "pull_request"
	case "wrong-source":
		run["head_sha"] = strings.Repeat("b", 40)
	case "wrong-branch":
		run["head_branch"] = "main"
	case "fork":
		run["head_repository"] = map[string]any{"id": c.ID, "full_name": "AutoDevJapan/opaque-advanced", "fork": true}
	case "finished":
		run["status"] = "completed"
	case "wrong-workflow":
		run["path"] = ".github/workflows/other.yml"
	}
	return func(_ context.Context, key string, out any) error {
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
	c := candidates[2]
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
	for _, fault := range []string{"default-enabled", "automatic-pr", "wrong-source", "wrong-branch", "fork", "finished", "wrong-workflow"} {
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
