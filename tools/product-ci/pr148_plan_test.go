package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// Synthetic workflows, not a product checkout. The command bytes deliberately
// include failure exits, Unicode comments and strings that resemble routes.
func pr148Fixture(w pr148Workflow) []byte {
	options := ""
	matrix := ""
	if w.Job == "test" {
		matrix = "    strategy:\n      fail-fast: false\n      matrix:\n        os: [ubuntu-latest, windows-latest]\n"
	} else {
		options = "        with:\n          persist-credentials: false\n"
	}
	return []byte("# 日本語の fixture\nname: fixture\non:\n  push:\n    branches: [main]\n  pull_request:\n  workflow_dispatch:\npermissions:\n  contents: read\njobs:\n  " + w.Job + ":\n" + matrix + "    runs-on: " + w.OriginalRoute + "\n    timeout-minutes: 20\n    steps:\n      - uses: actions/checkout@0123456789012345678901234567890123456789 # keep\n" + options + "      - name: Test failure propagation\n        shell: pwsh\n        run: |\n          cargo test --locked\n          if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }\n          echo windows-latest\n")
}
func TestPR148ProposalPreservesAcceptance(t *testing.T) {
	for _, w := range pr148Workflows {
		t.Run(w.Job, func(t *testing.T) {
			data := pr148Fixture(w)
			w.OriginalSHA = digest(data)
			out, e := proposePR148(data, w)
			if e != nil {
				t.Fatal(e)
			}
			if !bytes.Contains(out, []byte("          cargo test --locked\n          if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }\n          echo windows-latest\n")) {
				t.Fatal("command bytes changed")
			}
			if !bytes.Contains(out, []byte("# 日本語の fixture\n")) || !bytes.Contains(out, []byte(" # keep\n")) {
				t.Fatal("comments changed")
			}
			parsed, e := pr148YAML(out)
			if e != nil {
				t.Fatal(e)
			}
			job := pr148Field(pr148Field(parsed, "jobs"), w.Job)
			route := pr148Field(job, "runs-on").Value
			if !strings.Contains(route, pr148Dedicated) || !strings.Contains(route, " || ") {
				t.Fatal("finite route or main fallback missing")
			}
			with := pr148Field(pr148Field(job, "steps").Content[0], "with")
			if pr148Field(with, "ref").Value != "${{ github.event.pull_request.head.sha || github.sha }}" || pr148Field(with, "persist-credentials").Value != "false" {
				t.Fatal("head or auth binding missing")
			}
			if bytes.Contains(out, []byte("success")) {
				t.Fatal("proposal must not add success result")
			}
			if _, e = proposePR148(out, w); e == nil {
				t.Fatal("already changed workflow accepted")
			}
		})
	}
}
func TestPR148RejectsAmbiguousWorkflow(t *testing.T) {
	for _, data := range []string{"a: 1\na: 2\n", "a: &x 1\nb: *x\n", "a: 1\n---\na: 2\n", "- list\n"} {
		if _, e := pr148YAML([]byte(data)); e == nil {
			t.Fatalf("accepted %q", data)
		}
	}
	w := pr148Workflows[0]
	data := pr148Fixture(w)
	w.OriginalSHA = digest(data)
	changed := bytes.Replace(data, []byte("cargo test --locked"), []byte("echo skipped"), 1)
	if _, e := proposePR148(changed, w); e == nil {
		t.Fatal("changed original command accepted")
	}
	changed = bytes.Replace(data, []byte("runs-on: ${{ matrix.os }}"), []byte("runs-on: windows-latest"), 1)
	w.OriginalSHA = digest(changed)
	if _, e := proposePR148(changed, w); e == nil {
		t.Fatal("wrong original route accepted")
	}
}

func TestPR148InspectionBindsOriginalScope(t *testing.T) {
	// A complete fake API also checks that the inspector only reads these fixed
	// endpoints. Any mutation, unrelated repo, CodeQL rerun or archive fails.
	original := pr148Workflows
	defer func() { pr148Workflows = original }()
	pr148Workflows = append([]pr148Workflow(nil), original...)
	fixtures := map[string][]byte{}
	for i, w := range pr148Workflows {
		data := pr148Fixture(w)
		pr148Workflows[i].OriginalSHA = digest(data)
		fixtures[w.Path] = data
	}
	for _, bad := range []string{"", "base", "fork", "branch", "tree", "started", "annotation", "head-after"} {
		t.Run(bad, func(t *testing.T) {
			checks := 0
			read := func(ctx context.Context, ep string, out any) error {
				var v any
				switch {
				case ep == "repositories/1392152644":
					v = map[string]any{"id": 1392152644, "full_name": "AutoDevJapan/fixture", "owner": map[string]any{"login": "AutoDevJapan"}, "private": true, "fork": bad == "fork", "permissions": map[string]any{"admin": true}}
				case strings.HasSuffix(ep, "/pulls/148"):
					checks++
					head := pr148Head
					base := pr148Base
					if bad == "base" {
						base = strings.Repeat("1", 40)
					}
					if bad == "head-after" && checks > 2 {
						head = strings.Repeat("2", 40)
					}
					repo := map[string]any{"id": 1392152644, "full_name": "AutoDevJapan/fixture", "fork": false}
					v = map[string]any{"state": "open", "draft": true, "head": map[string]any{"sha": head, "ref": pr148Branch, "repo": repo}, "base": map[string]any{"sha": base, "ref": "main", "repo": repo}}
				case strings.Contains(ep, "/git/ref/heads/"):
					sha := pr148Head
					if bad == "branch" {
						sha = strings.Repeat("3", 40)
					}
					v = map[string]any{"object": map[string]any{"sha": sha}}
				case strings.Contains(ep, "/git/commits/"):
					tree := pr148Tree
					if bad == "tree" {
						tree = strings.Repeat("4", 40)
					}
					v = map[string]any{"tree": map[string]any{"sha": tree}}
				case strings.Contains(ep, "/contents/"):
					for p, data := range fixtures {
						if strings.Contains(ep, p+"?ref="+pr148Head) {
							v = map[string]any{"content": base64.StdEncoding.EncodeToString(data), "encoding": "base64", "type": "file"}
						}
					}
				case strings.Contains(ep, "/check-runs/"):
					message := "The job was not started because an Actions budget is preventing further use."
					if bad == "annotation" {
						message = "a test failed"
					}
					v = []any{map[string]any{"message": message}, map[string]any{"message": "OS migration notice", "annotation_level": "notice"}}
				default:
					for _, w := range pr148Workflows {
						prefix := fmt.Sprintf("repos/AutoDevJapan/fixture/actions/runs/%d", w.RunID)
						if ep == prefix {
							repo := map[string]any{"id": 1392152644, "full_name": "AutoDevJapan/fixture", "fork": false}
							v = map[string]any{"id": w.RunID, "run_attempt": 1, "path": w.Path, "head_sha": pr148Head, "event": "pull_request", "status": "completed", "repository": repo, "head_repository": repo}
						}
						if ep == prefix+"/attempts/1/jobs?per_page=100" {
							jobs := []any{}
							for i := 0; i < w.Jobs; i++ {
								steps := []any{}
								if bad == "started" {
									steps = append(steps, map[string]any{"number": 1})
								}
								jobs = append(jobs, map[string]any{"id": 100 + i, "run_id": w.RunID, "status": "completed", "conclusion": "failure", "runner_id": 0, "steps": steps})
							}
							v = map[string]any{"total_count": w.Jobs, "jobs": jobs}
						}
					}
				}
				if v == nil {
					return fmt.Errorf("unexpected endpoint")
				}
				b, e := json.Marshal(v)
				if e != nil {
					return e
				}
				return json.Unmarshal(b, out)
			}
			p, e := inspectPR148(context.Background(), read)
			if bad == "" {
				if e != nil {
					t.Fatal(e)
				}
				if len(p.Workflows) != 3 || !p.ProposalOnly || !p.SourceRevalidated || p.AllChecksSuccess {
					t.Fatal("wrong success or coverage")
				}
			} else if e == nil {
				t.Fatal("changed scope accepted")
			}
		})
	}
}
