package main

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestCollectionAcceptsTerminalRunsWithoutCertifyingResults(t *testing.T) {
	p := fixturePlan()
	for _, state := range []string{"queued", "in_progress", "completed"} {
		for _, outcome := range []string{"success", "failure", "cancelled", "timed_out"} {
			t.Run(state+"/"+outcome, func(t *testing.T) {
				calls := 0
				read := func(ep string, out any) error {
					calls++
					var v any
					repo := map[string]any{"id": repositoryID, "full_name": repository, "private": true, "fork": false, "owner": map[string]string{"login": "AutoDevJapan"}, "permissions": map[string]bool{"admin": true}}
					switch ep {
					case "repos/" + repository:
						v = repo
					case fmt.Sprintf("repos/%s/actions/runs/%d", repository, p.RunID):
						v = map[string]any{"id": p.RunID, "run_attempt": p.Attempt, "head_sha": p.Head, "event": "workflow_dispatch", "path": p.Workflow, "head_branch": branch, "repository": repo, "head_repository": repo, "status": state, "conclusion": outcome}
					case "repos/" + repository + "/actions/runners?per_page=100":
						v = map[string]any{"total_count": 0, "runners": []any{}}
					default:
						return fmt.Errorf("collection must not read live PR/ref/check success: %s", ep)
					}
					b, e := json.Marshal(v)
					if e != nil {
						return e
					}
					return json.Unmarshal(b, out)
				}
				if e := checkCollectionRemote(p, read); e != nil {
					t.Fatal(e)
				}
				if calls != 3 {
					t.Fatal("incomplete collection identity check")
				}
			})
		}
	}
}

func TestCollectionRejectsWrongIdentityAndRemainingRegistration(t *testing.T) {
	p := fixturePlan()
	for _, bad := range []string{"owner", "run", "attempt", "source", "fork", "workflow", "remaining", "inconsistent-list", "read-failed"} {
		t.Run(bad, func(t *testing.T) {
			read := func(ep string, out any) error {
				repo := map[string]any{"id": repositoryID, "full_name": repository, "private": true, "fork": false, "owner": map[string]string{"login": "AutoDevJapan"}, "permissions": map[string]bool{"admin": true}}
				var v any
				switch ep {
				case "repos/" + repository:
					if bad == "owner" {
						repo["owner"] = map[string]string{"login": "other"}
					}
					v = repo
				case fmt.Sprintf("repos/%s/actions/runs/%d", repository, p.RunID):
					run := map[string]any{"id": p.RunID, "run_attempt": p.Attempt, "head_sha": p.Head, "event": "workflow_dispatch", "path": p.Workflow, "head_branch": branch, "repository": repo, "head_repository": repo}
					switch bad {
					case "run":
						run["id"] = p.RunID + 1
					case "attempt":
						run["run_attempt"] = p.Attempt + 1
					case "source":
						run["head_sha"] = sourceHead
					case "fork":
						repo["fork"] = true
					case "workflow":
						run["path"] = "other.yml"
					}
					v = run
				case "repos/" + repository + "/actions/runners?per_page=100":
					total := 0
					runners := []any{}
					if bad == "remaining" {
						total = 1
					}
					if bad == "inconsistent-list" {
						runners = append(runners, map[string]any{"id": 123, "name": p.name()})
					}
					v = map[string]any{"total_count": total, "runners": runners}
				default:
					return fmt.Errorf("unexpected endpoint")
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
			if checkCollectionRemote(p, read) == nil {
				t.Fatal("unsafe collection identity accepted")
			}
		})
	}
}
