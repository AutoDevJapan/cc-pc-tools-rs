package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// Full positive/negative API fixtures contain synthetic workflows and commit
// bytes only. There is no network, checkout, GitHub write or product source.
func TestPR148LinuxNewCommitAndDispatchProof(t *testing.T) {
	prior := pr148Workflows
	defer func() { pr148Workflows = prior }()
	pr148Workflows = append([]pr148Workflow{}, prior...)
	oldBytes, newBytes := map[string][]byte{}, map[string][]byte{}
	oldTree := pr148APITree{SHA: pr148Tree}
	newTree := pr148APITree{SHA: strings.Repeat("2", 40)}
	for i, w := range pr148Workflows {
		b := pr148Fixture(w)
		pr148Workflows[i].OriginalSHA = digest(b)
		w = pr148Workflows[i]
		out, e := proposePR148(b, w)
		if e != nil {
			t.Fatal(e)
		}
		oldBytes[w.Path] = b
		newBytes[w.Path] = out
		oldTree.Entries = append(oldTree.Entries, pr148TreeEntry{w.Path, "100644", "blob", gitObjectID("blob", b)})
		newTree.Entries = append(newTree.Entries, pr148TreeEntry{w.Path, "100644", "blob", gitObjectID("blob", out)})
	}
	oldTree.Entries = append(oldTree.Entries, pr148TreeEntry{"fixture.rs", "100644", "blob", strings.Repeat("8", 40)})
	newTree.Entries = append(newTree.Entries, oldTree.Entries[3])
	payload := "tree " + newTree.SHA + "\nparent " + pr148Head + "\nauthor fixture <fixture@example.invalid> 1 +0000\ncommitter fixture <fixture@example.invalid> 1 +0000\n\nfixture\n"
	p := pr148LinuxPlan{gitObjectID("commit", []byte(payload)), newTree.SHA, digest(newBytes[pr148Workflows[0].Path]), 101, 102, 1}
	for _, bad := range []string{"", "parent", "bytes", "nonworkflow", "oldhead", "event", "attempt", "wrongjob", "partial-matrix", "owner"} {
		t.Run(bad, func(t *testing.T) {
			read := func(_ context.Context, ep string, out any) error {
				repo := map[string]any{"id": 1392152644, "full_name": pr148Repo, "private": true, "fork": false, "permissions": map[string]any{"admin": true}, "owner": map[string]string{"login": "AutoDevJapan"}}
				var v any
				switch {
				case ep == "repositories/1392152644":
					if bad == "owner" {
						repo["owner"] = map[string]string{"login": "other"}
					}
					v = repo
				case ep == "repos/"+pr148Repo+"/pulls/148":
					v = map[string]any{"state": "open", "draft": true, "head": map[string]any{"sha": p.Head, "ref": pr148Branch, "repo": repo}, "base": map[string]any{"sha": pr148Base, "ref": "main", "repo": repo}}
				case strings.Contains(ep, "/git/ref/heads/"):
					v = map[string]any{"object": map[string]string{"sha": p.Head}}
				case ep == "repos/"+pr148Repo+"/git/commits/"+pr148Head:
					v = map[string]any{"tree": map[string]string{"sha": pr148Tree}}
				case ep == "repos/"+pr148Repo+"/git/commits/"+p.Head:
					parent := pr148Head
					if bad == "parent" {
						parent = pr148Base
					}
					v = map[string]any{"tree": map[string]string{"sha": p.Tree}, "parents": []any{map[string]string{"sha": parent}}, "verification": map[string]string{"payload": payload}}
				case ep == "repos/"+pr148Repo+"/git/trees/"+pr148Tree+"?recursive=1":
					v = oldTree
				case ep == "repos/"+pr148Repo+"/git/trees/"+p.Tree+"?recursive=1":
					n := newTree
					n.Entries = append([]pr148TreeEntry{}, newTree.Entries...)
					if bad == "nonworkflow" {
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
						if ep == "repos/"+pr148Repo+"/contents/"+path+"?ref="+p.Head {
							if bad == "bytes" {
								b = append(append([]byte{}, b...), '\n')
							}
							v = map[string]string{"type": "file", "encoding": "base64", "content": base64.StdEncoding.EncodeToString(b), "sha": gitObjectID("blob", b)}
						}
					}
				case ep == "repos/"+pr148Repo+"/actions/runs/101":
					event := "workflow_dispatch"
					attempt := 1
					if bad == "event" {
						event = "pull_request"
					}
					if bad == "attempt" {
						attempt = 2
					}
					v = map[string]any{"id": 101, "run_attempt": attempt, "head_sha": p.Head, "head_branch": pr148Branch, "event": event, "path": pr148Workflows[0].Path, "status": "queued", "repository": repo, "head_repository": repo}
				case ep == "repos/"+pr148Repo+"/actions/runs/101/attempts/1/jobs?per_page=100":
					linuxID := int64(102)
					if bad == "wrongjob" {
						linuxID = 999
					}
					jobs := []any{}
					for i, os := range []string{"ubuntu-latest", "windows-latest"} {
						id := linuxID
						if i == 1 {
							id = 103
						}
						jobs = append(jobs, map[string]any{"id": id, "run_id": 101, "run_attempt": 1, "name": "test (" + os + ")", "status": "queued", "labels": []string{"cc-product-2736-1392152644-" + os}})
					}
					if bad == "partial-matrix" {
						jobs = jobs[:1]
					}
					v = map[string]any{"total_count": len(jobs), "jobs": jobs}
				}
				if v == nil {
					return fmt.Errorf("unapproved API endpoint in fixture: %s", ep)
				}
				b, e := json.Marshal(v)
				if e != nil {
					return e
				}
				return json.Unmarshal(b, out)
			}
			pin := p
			if bad == "oldhead" {
				pin.Head = pr148Head
			}
			e := verifyPR148LinuxRun(context.Background(), pin, read, false)
			if (bad == "" && e != nil) || (bad != "" && e == nil) {
				t.Fatalf("case %q error=%v", bad, e)
			}
		})
	}
}

func TestPR148OriginalCargoPolicy(t *testing.T) {
	prior := pr148Workflows[0].OriginalSHA
	defer func() { pr148Workflows[0].OriginalSHA = prior }()
	data := "jobs:\n  test:\n    steps:\n      - run: |\n"
	for _, args := range pr148CargoArgs {
		data += "          cargo " + strings.Join(args, " ") + "\n"
	}
	pr148Workflows[0].OriginalSHA = digest([]byte(data))
	p, e := pr148Policy([]byte(data))
	if e != nil || p != pr148SyntheticPolicy() {
		t.Fatalf("original command bytes: %v", e)
	}
	if _, e = pr148Policy([]byte(data + "# changed\n")); e == nil {
		t.Fatal("unreviewed Cargo workflow accepted")
	}
}
