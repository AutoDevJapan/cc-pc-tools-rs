package main

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

func TestSecretWorkflowBindsReviewedFileOriginalSourceAndMergeContext(t *testing.T) {
	c := candidates[0]
	path := ".github/workflows/opaque-secret.yml"
	source := []byte("synthetic pinned scanner workflow, never product source")
	fixture := func(fault string) api {
		return codeQLFixture(t, func(d map[string]any) {
			r := d["repos/AutoDevJapan/opaque-fixture/actions/runs/7"].(map[string]any)
			r["path"] = path
			r["event"] = "pull_request"
			r["pull_requests"] = []map[string]any{{"number": c.PR}}
			pr := d["repos/AutoDevJapan/opaque-fixture/pulls/1002"].(map[string]any)
			pr["merge_commit_sha"] = strings.Repeat("b", 40)
			d["repos/AutoDevJapan/opaque-fixture/contents/"+path+"?ref="+c.SHA] = map[string]any{"type": "file", "encoding": "base64", "content": base64.StdEncoding.EncodeToString(source)}
			switch fault {
			case "advanced-head":
				r["head_sha"] = strings.Repeat("d", 40)
			case "fork":
				r["head_repository"] = map[string]any{"id": c.ID, "full_name": "AutoDevJapan/opaque-fixture", "fork": true}
			case "wrong-event":
				r["event"] = "pull_request_target"
			case "wrong-path":
				r["path"] = ".github/workflows/other.yml"
			case "wrong-pr":
				r["pull_requests"] = []map[string]any{{"number": 999}}
			case "missing-merge":
				pr["merge_commit_sha"] = ""
			case "changed-workflow":
				d["repos/AutoDevJapan/opaque-fixture/contents/"+path+"?ref="+c.SHA] = map[string]any{"type": "file", "encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte("changed executable workflow"))}
			}
		})
	}
	p, e := planSecretJobWithPolicy(context.Background(), c, 7, fixture(""), digest([]byte(path)), digest(source))
	if e != nil || p.SHA != c.SHA || p.ContextSHA != strings.Repeat("b", 40) || p.Label != secretLabel(c.ID) || p.Kind != "secret" {
		t.Fatalf("valid original source workflow rejected: %v", e)
	}
	if e = checkCodeQLHook(p, p.Repo, p.ContextSHA, "7", "2"); e != nil {
		t.Fatal(e)
	}
	if checkCodeQLHook(p, p.Repo, p.SHA, "7", "2") == nil {
		t.Fatal("unbound job context admitted")
	}
	dynamic := p
	dynamic.Kind = ""
	dynamic.Label = codeQLLabel(c.ID)
	if validGuestPlan(dynamic) || checkCodeQLHook(dynamic, dynamic.Repo, dynamic.ContextSHA, "7", "2") == nil {
		t.Fatal("dynamic CodeQL accepted a merge context")
	}
	for _, fault := range []string{"advanced-head", "fork", "wrong-event", "wrong-path", "wrong-pr", "missing-merge", "changed-workflow"} {
		t.Run(fault, func(t *testing.T) {
			if _, e := planSecretJobWithPolicy(context.Background(), c, 7, fixture(fault), digest([]byte(path)), digest(source)); e == nil {
				t.Fatal("unreviewed workflow or source accepted")
			}
		})
	}
	calls := 0
	read := func(context.Context, string, any) error { calls++; return errors.New("unexpected API") }
	if _, e = productPlan(context.Background(), c, 7, "arbitrary", read); e == nil || calls != 0 {
		t.Fatal("unknown workflow kind reached remote API")
	}
	if _, e = productPlan(context.Background(), c, 7, "secret", read); e == nil || calls != 1 {
		t.Fatal("API spy did not observe the admitted workflow preflight")
	}
}
