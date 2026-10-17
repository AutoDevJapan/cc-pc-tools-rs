package main

import (
	"cc/product-ci/pr148scope"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestRoutingProofPreservesOriginalImplementation(t *testing.T) {
	workflows := pr148scope.Workflows()
	oldBytes, newBytes := map[string][]byte{}, map[string][]byte{}
	a := pr148scope.TreeReply{SHA: sourceTree}
	p := fixturePlan()
	p.Tree = strings.Repeat("2", 40)
	b := pr148scope.TreeReply{SHA: p.Tree}
	for i, w := range workflows {
		data := []byte("on:\n  workflow_dispatch:\njobs:\n  " + w.Job + ":\n    runs-on: " + w.OriginalRoute + "\n    steps:\n      - uses: actions/checkout@0123456789012345678901234567890123456789\n      - run: cargo test --locked\n")
		workflows[i].OriginalSHA = digest(data)
		out, e := pr148scope.Propose(data, workflows[i])
		if e != nil {
			t.Fatal(e)
		}
		oldBytes[w.Path], newBytes[w.Path] = data, out
		a.Entries = append(a.Entries, pr148scope.TreeEntry{Path: w.Path, Mode: "100644", Type: "blob", SHA: blobID(data)})
		b.Entries = append(b.Entries, pr148scope.TreeEntry{Path: w.Path, Mode: "100644", Type: "blob", SHA: blobID(out)})
		if w.Path == p.Workflow {
			p.WorkflowDigest = digest(out)
		}
	}
	implementation := pr148scope.TreeEntry{Path: "fixture.rs", Mode: "100644", Type: "blob", SHA: strings.Repeat("8", 40)}
	a.Entries = append(a.Entries, implementation)
	b.Entries = append(b.Entries, implementation)
	for _, bad := range []string{"", "parent", "nonworkflow", "mode", "truncated", "command", "wrong-tree", "old-head"} {
		t.Run(bad, func(t *testing.T) {
			read := func(ep string, out any) error {
				var v any
				switch ep {
				case "repos/" + repository + "/git/commits/" + sourceHead:
					v = map[string]any{"sha": sourceHead, "tree": map[string]string{"sha": sourceTree}}
				case "repos/" + repository + "/git/commits/" + p.Head:
					parent := sourceHead
					if bad == "parent" {
						parent = baseHead
					}
					v = map[string]any{"sha": p.Head, "tree": map[string]string{"sha": p.Tree}, "parents": []any{map[string]string{"sha": parent}}}
				case "repos/" + repository + "/git/trees/" + sourceTree + "?recursive=1":
					v = a
				case "repos/" + repository + "/git/trees/" + p.Tree + "?recursive=1":
					x := b
					x.Entries = append([]pr148scope.TreeEntry(nil), b.Entries...)
					if bad == "nonworkflow" {
						x.Entries[3].SHA = strings.Repeat("9", 40)
					}
					if bad == "mode" {
						x.Entries[3].Mode = "100755"
					}
					if bad == "truncated" {
						x.Truncated = true
					}
					if bad == "wrong-tree" {
						x.SHA = sourceTree
					}
					v = x
				default:
					for path, data := range oldBytes {
						if ep == "repos/"+repository+"/contents/"+path+"?ref="+sourceHead {
							v = map[string]string{"type": "file", "encoding": "base64", "sha": blobID(data), "content": base64.StdEncoding.EncodeToString(data)}
						}
					}
					for path, data := range newBytes {
						if ep == "repos/"+repository+"/contents/"+path+"?ref="+p.Head {
							if bad == "command" {
								data = append(append([]byte(nil), data...), []byte("# unauthorized extra\n")...)
							}
							v = map[string]string{"type": "file", "encoding": "base64", "sha": blobID(data), "content": base64.StdEncoding.EncodeToString(data)}
						}
					}
				}
				if v == nil {
					return fmt.Errorf("unapproved endpoint: %s", ep)
				}
				raw, e := json.Marshal(v)
				if e != nil {
					return e
				}
				return json.Unmarshal(raw, out)
			}
			pin := p
			if bad == "old-head" {
				pin.Head = sourceHead
			}
			e := verifyRoutingUsing(pin, read, workflows)
			if (bad == "" && e != nil) || (bad != "" && e == nil) {
				t.Fatalf("case %q: %v", bad, e)
			}
		})
	}
}

func TestWindowsDispatchRequiresFullOSMatrix(t *testing.T) {
	p := fixturePlan()
	windows := apiJob{ID: p.JobID, RunID: p.RunID, Attempt: p.Attempt, Name: p.JobName, Labels: []string{p.Label}}
	linux := apiJob{ID: p.JobID + 1, RunID: p.RunID, Attempt: p.Attempt, Name: "test (ubuntu-latest)", Labels: []string{"cc-product-2736-1392152644-ubuntu-latest"}}
	for _, bad := range []string{"", "partial", "duplicate", "attempt", "wrong-os"} {
		t.Run(bad, func(t *testing.T) {
			jobs := []apiJob{windows, linux}
			switch bad {
			case "partial":
				jobs = jobs[:1]
			case "duplicate":
				jobs[1] = windows
			case "attempt":
				jobs[1].Attempt++
			case "wrong-os":
				jobs[1].Name = "test (macos-latest)"
			}
			e := checkJobSet(p, len(jobs), jobs)
			if (bad == "" && e != nil) || (bad != "" && e == nil) {
				t.Fatalf("%s: %v", bad, e)
			}
		})
	}
}
