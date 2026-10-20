package main

import (
	"encoding/json"
	"fmt"
	"testing"
)

func collectionReader(p Plan, state string, bad string, others bool) readAPI {
	return func(ep string, out any) error {
		repo := map[string]any{"id": repositoryID, "full_name": repository, "private": true, "fork": false, "owner": map[string]string{"login": "AutoDevJapan"}, "permissions": map[string]bool{"admin": true}}
		var v any
		switch ep {
		case "repos/" + repository:
			if bad == "public" {
				repo["private"] = false
			}
			if bad == "repo-id" {
				repo["id"] = repositoryID + 1
			}
			if bad == "repo-name" {
				repo["full_name"] = "AutoDevJapan/other"
			}
			if bad == "fork" {
				repo["fork"] = true
			}
			if bad == "no-admin" {
				repo["permissions"] = map[string]bool{"admin": false}
			}
			if bad == "owner" {
				repo["owner"] = map[string]string{"login": "other"}
			}
			v = repo
		case fmt.Sprintf("repos/%s/actions/runs/%d", repository, p.RunID):
			run := map[string]any{"id": p.RunID, "run_attempt": p.Attempt, "head_sha": p.Head, "event": p.Event, "path": p.Workflow, "head_branch": p.Branch, "repository": repo, "head_repository": repo, "status": state}
			switch bad {
			case "run":
				run["id"] = p.RunID + 1
			case "attempt":
				run["run_attempt"] = p.Attempt + 1
			case "source":
				run["head_sha"] = fixtureHead
			case "event":
				run["event"] = "workflow_dispatch"
			case "branch":
				run["head_branch"] = "main"
			case "fork":
				repo["fork"] = true
			case "workflow":
				run["path"] = "other.yml"
			case "pathref":
				run["path"] = p.Workflow + "@" + p.Ref()
			case "pathref-wrong-ref":
				run["path"] = p.Workflow + "@refs/heads/other"
			case "pathref-wrong-workflow":
				run["path"] = ".github/workflows/other.yml@" + p.Ref()
			}
			v = run
		case "repos/" + repository + "/actions/runners?per_page=100":
			runners := []any{}
			if others {
				// The long-lived Linux/Windows workers and the Linux ephemeral
				// runner are not this plan's registration and must not matter.
				runners = append(runners, map[string]any{"id": 1, "name": "long-lived-windows"}, map[string]any{"id": 2, "name": "CC-CIL-other"})
			}
			total := len(runners)
			switch bad {
			case "remaining":
				runners = append(runners, map[string]any{"id": 123, "name": p.name()})
				total = len(runners)
			case "inconsistent-list":
				total++
			case "duplicate":
				runners = append(runners, map[string]any{"id": 123, "name": p.name()}, map[string]any{"id": 124, "name": p.name()})
				total = len(runners)
			}
			v = map[string]any{"total_count": total, "runners": runners}
		default:
			return fmt.Errorf("collection must not read live PR/ref/check success: %s", ep)
		}
		if bad == "read-failed" {
			return fmt.Errorf("synthetic API failure")
		}
		b, e := json.Marshal(v)
		if e != nil {
			return e
		}
		return json.Unmarshal(b, out)
	}
}

func TestCollectionAcceptsApprovedPublicRepositoryWithoutWideningIdentity(t *testing.T) {
	p := fixturePlan()
	if err := checkCollectionRemote(p, collectionReader(p, "queued", "public", true)); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"repo-id", "repo-name", "owner", "fork", "no-admin", "remaining"} {
		if checkCollectionRemote(p, collectionReader(p, "queued", bad, true)) == nil {
			t.Fatalf("unsafe collection accepted: %s", bad)
		}
	}
}

func TestCollectionAcceptsTerminalRunsWithoutCertifyingResults(t *testing.T) {
	for _, p := range []Plan{fixturePlan(), pushPlan(".github/workflows/native-driver.yml", "native-driver")} {
		for _, state := range []string{"queued", "in_progress", "completed"} {
			for _, others := range []bool{false, true} {
				if e := checkCollectionRemote(p, collectionReader(p, state, "", others)); e != nil {
					t.Fatalf("%s/%s/others=%v: %v", p.Event, state, others, e)
				}
			}
		}
	}
}

// The REST API may return the bare path or path@ref; both identify the same run.
func TestCollectionAcceptsBareAndRefQualifiedRunPath(t *testing.T) {
	for _, p := range []Plan{fixturePlan(), pushPlan(".github/workflows/live-chain.yml", "live-chain")} {
		if e := checkCollectionRemote(p, collectionReader(p, "completed", "pathref", false)); e != nil {
			t.Fatalf("%s: %v", p.Event, e)
		}
	}
}

func TestCollectionRejectsWrongIdentityAndRemainingRegistration(t *testing.T) {
	p := fixturePlan()
	for _, bad := range []string{"owner", "run", "attempt", "source", "event", "branch", "fork", "workflow", "pathref-wrong-ref", "pathref-wrong-workflow", "remaining", "duplicate", "inconsistent-list", "read-failed"} {
		t.Run(bad, func(t *testing.T) {
			if checkCollectionRemote(p, collectionReader(p, "completed", bad, true)) == nil {
				t.Fatal("unsafe collection identity accepted")
			}
		})
	}
}
