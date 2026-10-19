package main

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestGeneratedPRRunUsesNumericRefAndLiveOwnedSourceBranch(t *testing.T) {
	run := apiRun{ID: 7, Event: "dynamic", Path: CodeQLWorkflow, HeadSHA: headSHA, HeadBranch: "refs/pull/149/head"}
	fork := false
	read := func(_ context.Context, ep string, out any) error {
		var value any
		switch ep {
		case "repos/" + Repository + "/pulls/149":
			value = obj{"number": 149, "state": "open", "head": obj{"sha": headSHA, "ref": branch, "repo": obj{"id": RepoID, "full_name": Repository, "fork": fork}}}
		case "repos/" + Repository + "/git/commits/" + headSHA:
			value = obj{"sha": headSHA, "tree": obj{"sha": treeSHA}}
		default:
			return errors.New("unexpected endpoint")
		}
		b, _ := json.Marshal(value)
		return json.Unmarshal(b, out)
	}
	scanner := Scanner{Read: read}
	p, e := scanner.basePlan(context.Background(), run, 1)
	if e != nil || p.PR != 149 || p.Branch != branch || p.ContextSHA != headSHA || p.WorkflowRef != "" || p.WorkflowDigest != "" {
		t.Fatalf("actual generated ref was confused with source branch: %+v %v", p, e)
	}
	fork = true
	if _, e = scanner.basePlan(context.Background(), run, 1); e == nil {
		t.Fatal("generated fork PR admitted to PC queue")
	}
	fork = false
	run.HeadBranch = "refs/pull/150/head"
	if _, e = scanner.basePlan(context.Background(), run, 1); e == nil {
		t.Fatal("unverified numeric PR ref admitted")
	}
}

func TestNormalBuildLinksParentPolicy(t *testing.T) {
	if activeContract.OS(Plan{JobName: "test (ubuntu-latest)"}) != "Linux" {
		t.Fatal("queue binary silently built without its parent policy")
	}
}
