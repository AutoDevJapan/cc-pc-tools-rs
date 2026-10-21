package pressci

import (
	"bytes"
	sourcepacket "cc/ci-source-2737"
	"cc/product-ci/normalci"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
)

func TestAllOriginalRoutesChangeOnlyRunsOnAndIsolateMatrixAttempts(t *testing.T) {
	lease := strings.Repeat("a", 32)
	labels := map[string]bool{}
	count := 0
	for name, raw := range fixtures() {
		raw = append([]byte("# 公開の合成fixture\n"), raw...)
		prepared, e := prepareRoutes(name, raw, lease)
		if e != nil {
			t.Fatal(e)
		}
		out, e := outlineWorkflow(raw, name)
		if e != nil {
			t.Fatal(e)
		}
		root, e := normalci.ParseYAML(raw)
		if e != nil {
			t.Fatal(e)
		}
		restored := append([]byte{}, prepared...)
		seen := map[string]bool{}
		for _, j := range out.Jobs {
			role := Role(j)
			label := RuntimeLabel(name, role, 77, 2)
			if label == "" || labels[label] || label == RuntimeLabel(name, role, 77, 3) || label == RuntimeLabel(name, role, 78, 2) {
				t.Fatal("matrix/run/attempt registration shared")
			}
			labels[label] = true
			count++
			if !seen[j.ID] {
				expr, e := runnerExpression(name, j, lease)
				if e != nil {
					t.Fatal(e)
				}
				old := normalci.Field(normalci.Field(normalci.Field(root, "jobs"), j.ID), "runs-on")
				restored = bytes.Replace(restored, []byte(expr), []byte(old.Value), 1)
				seen[j.ID] = true
			}
		}
		if !bytes.Equal(restored, raw) {
			t.Fatal("non-route byte changed")
		}
		if _, e := prepareRoutes(name, raw, "arbitrary"); e == nil {
			t.Fatal("unowned route value accepted")
		}
		if _, e := PrepareRoutes(name, raw, lease); e == nil {
			t.Fatal("public synthetic blob substituted for fixed original")
		}
	}
	if count != 13 {
		t.Fatal("required original role missing")
	}
}

func TestPressAdmissionBindsOriginalContractCandidateAndActualExecution(t *testing.T) {
	for _, workflow := range []string{"local-checks.yml", "rust-v2-https.yml", "windows-hidden-fixtures.yml"} {
		for _, which := range []string{"accept", "running", "bad-profile", "bad-role", "not-draft", "repository", "candidate", "main", "merge-ref", "parents", "lease", "workflow", "non-route", "partial-tree", "run", "event", "attempt", "run-source", "job-source", "job-attempt", "foreign-job", "duplicate-job", "shared-shard", "claimed", "advance-after"} {
			if (which == "main" || which == "merge-ref" || which == "parents") && workflow == "local-checks.yml" {
				continue
			}
			t.Run(workflow+"/"+which, func(t *testing.T) {
				raw := fixtures()
				prepared := map[string][]byte{}
				var originals []workflowSpecForTest
				lease := strings.Repeat("1", 32)
				for _, w := range workflows() {
					b, e := prepareRoutes(w.name, raw[w.name], lease)
					if e != nil {
						t.Fatal(e)
					}
					prepared[w.name] = b
					originals = append(originals, workflowSpecForTest{w.name, blobID(raw[w.name])})
				}
				wf, e := outlineWorkflow(raw[workflow], workflow)
				if e != nil {
					t.Fatal(e)
				}
				role := Role(wf.Jobs[0])
				if workflow == "rust-v2-https.yml" {
					role = "rust-v2-https-windows-2022"
				}
				h := sha256.Sum256(prepared[workflow])
				p := Pin{Source: sourcepacket.Pin{Product: sourcepacket.PressProduct, Workflow: workflow, HeadSHA: strings.Repeat("a", 40), TreeSHA: strings.Repeat("b", 40), WorkflowSHA256: hex.EncodeToString(h[:])}, RunID: 77, Attempt: 2, Role: role, RouteLease: lease}
				if workflow != "local-checks.yml" {
					p.Source.PullHeadSHA, p.Source.MergeBaseSHA = strings.Repeat("d", 40), strings.Repeat("e", 40)
				}
				if which == "bad-profile" {
					p.Source.Product = "other"
				}
				if which == "bad-role" {
					p.Role = "verify"
				}
				if which == "running" {
					p.RunnerID, p.RunnerName = 42, "todo2737-"+strings.Repeat("2", 32)
				}
				calls, repoCalls := 0, 0
				read := func(_ context.Context, ep string, out any) error {
					calls++
					var v any
					repo := map[string]any{"id": RepoID, "full_name": Repository, "private": true, "fork": false, "owner": map[string]string{"login": "AutoDevJapan"}, "permissions": map[string]bool{"admin": true}}
					switch {
					case ep == "repositories/1386702183":
						repoCalls++
						if which == "repository" {
							repo["id"] = 1154066087
						}
						v = repo
					case strings.HasSuffix(ep, "/pulls/175"):
						head := p.Source.CandidateSHA()
						if which == "candidate" || which == "advance-after" && repoCalls > 1 {
							head = strings.Repeat("f", 40)
						}
						v = map[string]any{"state": "open", "draft": which != "not-draft", "merge_commit_sha": p.Source.HeadSHA, "head": map[string]any{"sha": head, "ref": Ref, "repo": repo}, "base": map[string]any{"sha": Base, "repo": repo}}
					case strings.Contains(ep, "/git/ref/"):
						sha := p.Source.CandidateSHA()
						if strings.HasSuffix(ep, "heads/main") {
							sha = p.Source.MergeBaseSHA
							if which == "main" {
								sha = p.Source.HeadSHA
							}
						}
						if strings.HasSuffix(ep, "pull/175/merge") {
							sha = p.Source.HeadSHA
							if which == "merge-ref" {
								sha = p.Source.PullHeadSHA
							}
						}
						v = map[string]any{"object": map[string]string{"sha": sha}}
					case strings.Contains(ep, "/git/commits/"):
						sha := strings.Split(ep, "/git/commits/")[1]
						tree := p.Source.TreeSHA
						if sha != p.Source.HeadSHA {
							tree = strings.Repeat("c", 40)
						}
						parents := []map[string]string{}
						if sha == p.Source.HeadSHA && p.Source.PullHeadSHA != "" {
							parents = []map[string]string{{"sha": p.Source.MergeBaseSHA}, {"sha": p.Source.PullHeadSHA}}
							if which == "parents" {
								parents[0], parents[1] = parents[1], parents[0]
							}
						}
						v = map[string]any{"sha": sha, "tree": map[string]string{"sha": tree}, "parents": parents}
					case strings.Contains(ep, "/git/trees/"):
						sha := strings.Split(strings.Split(ep, "/git/trees/")[1], "?")[0]
						entries := []map[string]string{{"path": "public.rs", "mode": "100644", "type": "blob", "sha": strings.Repeat("f", 40)}}
						for _, w := range workflows() {
							body := prepared[w.name]
							if sha == Tree {
								body = raw[w.name]
							}
							entries = append(entries, map[string]string{"path": ".github/workflows/" + w.name, "mode": "100644", "type": "blob", "sha": blobID(body)})
						}
						if which == "non-route" && sha != Tree {
							entries[0]["sha"] = strings.Repeat("0", 40)
						}
						v = map[string]any{"sha": sha, "truncated": which == "partial-tree", "tree": entries}
					case strings.Contains(ep, "/contents/.github/workflows/"):
						suffix := strings.Split(ep, "/workflows/")[1]
						name := strings.Split(suffix, "?")[0]
						body := prepared[name]
						if strings.HasSuffix(ep, "?ref="+Head) {
							body = raw[name]
						} else if which == "workflow" {
							body = append(append([]byte{}, body...), '\n')
						}
						v = map[string]string{"sha": blobID(body), "encoding": "base64", "content": base64.StdEncoding.EncodeToString(body)}
					case strings.HasSuffix(ep, "/actions/variables/"+RouteVariable):
						value := LeaseValue(lease)
						if which == "lease" {
							value = "other"
						}
						v = map[string]string{"name": RouteVariable, "value": value}
					case ep == "repos/"+Repository+"/actions/runs/77":
						event := "pull_request"
						if workflow == "local-checks.yml" {
							event = "workflow_dispatch"
						}
						if which == "event" {
							event = "push"
						}
						run, attempt, sha := int64(77), 2, p.Source.CandidateSHA()
						if which == "run" {
							run++
						}
						if which == "attempt" {
							attempt++
						}
						if which == "run-source" {
							sha = strings.Repeat("f", 40)
						}
						v = map[string]any{"id": run, "run_attempt": attempt, "path": ".github/workflows/" + workflow, "event": event, "status": "in_progress", "head_sha": sha, "head_branch": Ref, "repository": repo, "head_repository": repo, "pull_requests": []map[string]int{{"number": PR}}}
					case strings.HasSuffix(ep, "/attempts/2/jobs?per_page=100"):
						var jobs []map[string]any
						for i, j := range wf.Jobs {
							item := map[string]any{"id": int64(100 + i), "run_id": int64(77), "run_attempt": 2, "head_sha": p.Source.CandidateSHA(), "name": j.Name, "status": "queued", "runner_id": int64(0), "runner_name": "", "labels": []string{RuntimeLabel(workflow, Role(j), 77, 2)}}
							if Role(j) == role && which == "running" {
								item["status"], item["runner_id"], item["runner_name"] = "in_progress", p.RunnerID, p.RunnerName
							}
							jobs = append(jobs, item)
						}
						if which == "job-source" {
							jobs[0]["head_sha"] = strings.Repeat("f", 40)
						}
						if which == "job-attempt" {
							jobs[0]["run_attempt"] = 3
						}
						if which == "foreign-job" {
							jobs[0]["name"] = "other"
						}
						if which == "duplicate-job" {
							jobs = append(jobs, jobs[0])
						}
						if which == "shared-shard" {
							jobs[0]["labels"] = []string{RuntimeLabel(workflow, Role(wf.Jobs[0]), 77, 1)}
						}
						if which == "claimed" {
							for i, j := range wf.Jobs {
								if Role(j) == role {
									jobs[i]["runner_id"] = int64(42)
								}
							}
						}
						v = map[string]any{"total_count": len(jobs), "jobs": jobs}
					default:
						t.Fatalf("unexpected public fixture endpoint %s", ep)
					}
					b, e := json.Marshal(v)
					if e != nil {
						return e
					}
					return json.Unmarshal(b, out)
				}
				b, e := plan(context.Background(), p, read, toOriginals(originals))
				accept := which == "accept" || which == "running"
				if (e == nil) != accept {
					t.Fatalf("binding acceptance changed (%s): %v", which, e)
				}
				if accept && (b.RepoID != RepoID || b.SHA != p.Source.HeadSHA || b.Role != role || b.JobID < 1 || repoCalls != 2) {
					t.Fatal("unbound execution returned")
				}
				if (which == "bad-profile" || which == "bad-role") && calls != 0 {
					t.Fatal("rejected request still read APIs")
				}
				if !accept && b.JobID != 0 {
					t.Fatal("partial binding returned")
				}
			})
		}
	}
}

// Aliases keep fixture construction separate from the immutable public policy.
type workflowSpecForTest struct{ name, blob string }

func toOriginals(items []workflowSpecForTest) []workflow {
	var out []workflow
	for _, w := range items {
		out = append(out, workflow{w.name, w.blob})
	}
	return out
}
