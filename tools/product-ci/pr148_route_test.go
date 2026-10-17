package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestPR148NonForceRoutingTransaction(t *testing.T) {
	prior := pr148Workflows
	defer func() { pr148Workflows = prior }()
	pr148Workflows = append([]pr148Workflow(nil), prior...)
	oldBytes, newBytes := map[string][]byte{}, map[string][]byte{}
	oldTree := pr148APITree{SHA: pr148Tree}
	newTree := pr148APITree{SHA: strings.Repeat("2", 40)}
	for i, w := range pr148Workflows {
		b := pr148Fixture(w)
		pr148Workflows[i].OriginalSHA = digest(b)
		out, e := proposePR148(b, pr148Workflows[i])
		if e != nil {
			t.Fatal(e)
		}
		oldBytes[w.Path], newBytes[w.Path] = b, out
		oldTree.Entries = append(oldTree.Entries, pr148TreeEntry{w.Path, "100644", "blob", gitObjectID("blob", b)})
		newTree.Entries = append(newTree.Entries, pr148TreeEntry{w.Path, "100644", "blob", gitObjectID("blob", out)})
	}
	unchanged := pr148TreeEntry{"fixture.rs", "100644", "blob", strings.Repeat("8", 40)}
	oldTree.Entries = append(oldTree.Entries, unchanged)
	newTree.Entries = append(newTree.Entries, unchanged)
	payload := "tree " + newTree.SHA + "\nparent " + pr148Head + "\nauthor fixture <fixture@example.invalid> 1 +0000\ncommitter fixture <fixture@example.invalid> 1 +0000\n\nfixture\n"
	head := gitObjectID("commit", []byte(payload))
	decode := func(v, out any) error {
		b, e := json.Marshal(v)
		if e != nil {
			return e
		}
		return json.Unmarshal(b, out)
	}
	for _, bad := range []string{"", "source-race", "tree-content", "ref-refused"} {
		t.Run(bad, func(t *testing.T) {
			moved, patches := false, 0
			methods := []string{}
			read := func(_ context.Context, ep string, out any) error {
				repo := map[string]any{"id": 1392152644, "full_name": pr148Repo, "private": true, "fork": false, "permissions": map[string]bool{"admin": true}, "owner": map[string]string{"login": "AutoDevJapan"}}
				current := pr148Head
				if moved {
					current = head
				}
				if bad == "source-race" && len(methods) == 2 {
					current = strings.Repeat("9", 40)
				}
				var v any
				switch {
				case ep == "repositories/1392152644":
					v = repo
				case ep == "repos/"+pr148Repo+"/pulls/148":
					v = map[string]any{"state": "open", "draft": true, "head": map[string]any{"sha": current, "ref": pr148Branch, "repo": repo}, "base": map[string]any{"sha": pr148Base, "ref": "main", "repo": repo}}
				case strings.Contains(ep, "/git/ref/heads/"):
					v = map[string]any{"object": map[string]string{"sha": current}}
				case ep == "repos/"+pr148Repo+"/git/commits/"+pr148Head:
					v = map[string]any{"tree": map[string]string{"sha": pr148Tree}}
				case ep == "repos/"+pr148Repo+"/git/commits/"+head:
					v = map[string]any{"tree": map[string]string{"sha": newTree.SHA}, "parents": []any{map[string]string{"sha": pr148Head}}, "verification": map[string]string{"payload": payload}}
				case ep == "repos/"+pr148Repo+"/git/trees/"+pr148Tree+"?recursive=1":
					v = oldTree
				case ep == "repos/"+pr148Repo+"/git/trees/"+newTree.SHA+"?recursive=1":
					n := newTree
					n.Entries = append([]pr148TreeEntry(nil), newTree.Entries...)
					if bad == "tree-content" {
						n.Entries[3].SHA = strings.Repeat("9", 40)
					}
					v = n
				case strings.Contains(ep, "/contents/"):
					for path, b := range oldBytes {
						if ep == "repos/"+pr148Repo+"/contents/"+path+"?ref="+pr148Head {
							v = map[string]string{"type": "file", "encoding": "base64", "content": base64.StdEncoding.EncodeToString(b), "sha": gitObjectID("blob", b)}
						}
					}
					for path, b := range newBytes {
						if ep == "repos/"+pr148Repo+"/contents/"+path+"?ref="+head {
							v = map[string]string{"type": "file", "encoding": "base64", "content": base64.StdEncoding.EncodeToString(b), "sha": gitObjectID("blob", b)}
						}
					}
				case strings.Contains(ep, "/check-runs/"):
					v = []any{map[string]string{"message": "The job was not started because an Actions budget is preventing further use.", "annotation_level": "failure"}}
				default:
					for _, w := range pr148Workflows {
						prefix := fmt.Sprintf("repos/%s/actions/runs/%d", pr148Repo, w.RunID)
						if ep == prefix {
							v = map[string]any{"id": w.RunID, "run_attempt": 1, "path": w.Path, "head_sha": pr148Head, "event": "pull_request", "status": "completed", "repository": repo, "head_repository": repo}
						}
						if ep == prefix+"/attempts/1/jobs?per_page=100" {
							jobs := []any{}
							for i := 0; i < w.Jobs; i++ {
								name := w.Job
								if w.Job == "test" {
									name = []string{"test (ubuntu-latest)", "test (windows-latest)"}[i]
								}
								jobs = append(jobs, map[string]any{"id": 100 + i, "run_id": w.RunID, "name": name, "status": "completed", "conclusion": "failure", "runner_id": 0, "steps": []any{}})
							}
							v = map[string]any{"total_count": w.Jobs, "jobs": jobs}
						}
					}
				}
				if v == nil {
					return fmt.Errorf("unexpected read: %s", ep)
				}
				return decode(v, out)
			}
			write := func(_ context.Context, method, ep string, body, out any) error {
				methods = append(methods, method)
				switch ep {
				case "repos/" + pr148Repo + "/git/trees":
					if method != "POST" {
						t.Fatal(method)
					}
					var b struct {
						Base string `json:"base_tree"`
						Tree []struct{ Path, Mode, Type, Content string }
					}
					if e := decode(body, &b); e != nil {
						t.Fatal(e)
					}
					if b.Base != pr148Tree || len(b.Tree) != 3 {
						t.Fatal("tree scope")
					}
					for _, x := range b.Tree {
						if x.Mode != "100644" || x.Type != "blob" || x.Content != string(newBytes[x.Path]) {
							t.Fatal("workflow bytes")
						}
					}
					return decode(map[string]string{"sha": newTree.SHA}, out)
				case "repos/" + pr148Repo + "/git/commits":
					var b struct {
						Tree    string
						Parents []string
					}
					if e := decode(body, &b); e != nil {
						t.Fatal(e)
					}
					if method != "POST" || b.Tree != newTree.SHA || !reflect.DeepEqual(b.Parents, []string{pr148Head}) {
						t.Fatal("commit parent scope")
					}
					return decode(map[string]string{"sha": head}, out)
				case "repos/" + pr148Repo + "/git/refs/heads/" + pr148Branch:
					patches++
					if method != "PATCH" || !reflect.DeepEqual(body, map[string]any{"sha": head, "force": false}) {
						t.Fatal("non-force ref contract")
					}
					if bad == "ref-refused" {
						return errors.New("ref refused")
					}
					moved = true
					return nil
				default:
					t.Fatalf("unexpected mutation: %s", ep)
					return nil
				}
			}
			dir := t.TempDir()
			e := routePR148Using(context.Background(), dir, read, write)
			if bad == "" {
				if e != nil {
					t.Fatal(e)
				}
				if patches != 1 {
					t.Fatal("missing ref")
				}
				var r pr148RoutingReceipt
				b, _ := os.ReadFile(filepath.Join(dir, "pr148-routing-result.private.json"))
				if json.Unmarshal(b, &r) != nil || !r.BranchVerified || r.AllRequiredChecksSuccess || r.Head != head {
					t.Fatal("result evidence")
				}
			} else {
				if e == nil {
					t.Fatal("failure accepted")
				}
				if bad != "ref-refused" && patches != 0 {
					t.Fatal("ref changed after rejection")
				}
				if _, e := os.Stat(filepath.Join(dir, "pr148-routing-result.private.json")); !os.IsNotExist(e) {
					t.Fatal("false success receipt")
				}
			}
		})
	}
}
