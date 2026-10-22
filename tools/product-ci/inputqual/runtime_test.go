package inputqual

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runtimeFixture(t *testing.T, kind, head string) (*fixture, runtimeHandoff) {
	t.Helper()
	f := newFixture(t)
	if err := os.Mkdir(filepath.Join(f.p.root, ".ci-work"), 0700); err != nil {
		t.Fatal(err)
	}
	h := runtimeHandoff{Version: 1, Task: 3059, Origin: 3056, Repo: 1285649124, PR: 995, Head: head, Tree: strings.Repeat("b", 40), Base: fixedBase, Run: 9001, Attempt: 2, Jobs: []int64{9011}, Kind: kind}
	if kind == "secret" {
		h.Context = fixedContext
	} else {
		h.Jobs = []int64{9011, 9012, 9013, 9014}
	}
	for _, role := range []string{"review", "acceptance"} {
		output := f.put(filepath.Join(f.p.root, role+"-new-output.json"), map[string]any{"result": role, "private_canary": "never print me"})
		must := 0
		result := runtimeResult{Origin: 3056, Repo: h.Repo, PR: h.PR, Head: h.Head, Tree: h.Tree, Kind: role, Success: true, Must: &must, OutputFile: output.path, OutputSHA: output.sha}
		a := f.put(filepath.Join(f.p.root, role+"-new-result.json"), result)
		ref := runtimeReference{File: a.path, SHA: a.sha}
		if role == "review" {
			h.Review = ref
		} else {
			h.Acceptance = ref
		}
	}
	f.put(filepath.Join(f.p.root, ".ci-work", runtimeLeaf), h)
	return f, h
}

func TestRuntimeTwoHeadsAndBothKinds(t *testing.T) {
	for _, kind := range []string{"secret", "codeql"} {
		for _, head := range []string{strings.Repeat("c", 40), strings.Repeat("d", 40)} {
			t.Run(kind+head[:1], func(t *testing.T) {
				f, h := runtimeFixture(t, kind, head)
				s, e := constructRuntime(f.p, func(string) bool { return true })
				if e != nil {
					t.Fatal(e)
				}
				r := s.Report()
				if !r.Qualified || r.Head != head || r.Run != h.Run || r.OfficialCISuccess || r.OriginalInputFilesRead {
					t.Fatal("wrong authority/result")
				}
				e = s.Consume(func(pin, review, acceptance []byte) error {
					var p map[string]any
					if json.Unmarshal(pin, &p) != nil || p["head_sha"] != head || p["kind"] != kind || hash(review) != r.ReviewSHA || hash(acceptance) != r.AcceptanceSHA {
						t.Fatal("not the frozen bytes")
					}
					return nil
				})
				if e != nil {
					t.Fatal(e)
				}
			})
		}
	}
}

func TestRuntimeRejectsBeforeUnapprovedRead(t *testing.T) {
	f, _ := runtimeFixture(t, "secret", strings.Repeat("c", 40))
	if _, e := constructRuntime(f.p, func(string) bool { return false }); e == nil {
		t.Fatal("unclassified handoff accepted")
	}
	// A denied role may contain invalid JSON; it must not reach the parser.
	path := filepath.Join(f.p.root, "review-new-result.json")
	if os.WriteFile(path, []byte("unreadable private canary"), 0600) != nil {
		t.Fatal("fixture")
	}
	_, e := constructRuntime(f.p, func(p string) bool { return p != path })
	if e == nil || !strings.Contains(e.Error(), "runtime_read_permission") {
		t.Fatal("denied receipt was read")
	}
}

func TestRuntimeRejectsInvalidScopeAndEvidence(t *testing.T) {
	for _, change := range []string{"repo", "pr", "kind", "head", "review", "output", "role", "same-output", "old-input", "old-review-output", "old-acceptance-output", "duplicate", "stop", "must", "missing-must", "not-success"} {
		t.Run(change, func(t *testing.T) {
			f, h := runtimeFixture(t, "secret", strings.Repeat("c", 40))
			switch change {
			case "repo":
				h.Repo++
			case "pr":
				h.PR = 939
			case "kind":
				h.Kind = "unknown"
			case "head":
				h.Head = strings.Repeat("d", 40)
			case "review":
				h.Review.SHA = strings.Repeat("0", 64)
			case "output":
				os.WriteFile(filepath.Join(f.p.root, "review-new-output.json"), []byte("changed"), 0600)
			case "role":
				h.Review, h.Acceptance = h.Acceptance, h.Review
			case "same-output":
				var r runtimeResult
				data, _ := os.ReadFile(h.Acceptance.File)
				json.Unmarshal(data, &r)
				var review runtimeResult
				data, _ = os.ReadFile(h.Review.File)
				json.Unmarshal(data, &review)
				r.OutputFile, r.OutputSHA = review.OutputFile, review.OutputSHA
				a := f.put(h.Acceptance.File, r)
				h.Acceptance.SHA = a.sha
			case "old-input":
				h.Review.File = f.p.review
			case "old-review-output", "old-acceptance-output":
				var r runtimeResult
				data, _ := os.ReadFile(h.Review.File)
				json.Unmarshal(data, &r)
				var output []byte
				if change == "old-review-output" {
					output = []byte("private synthetic independently retained original review output")
				} else {
					output, _ = os.ReadFile(f.p.original.path)
				}
				if err := os.WriteFile(r.OutputFile, output, 0600); err != nil {
					t.Fatal(err)
				}
				r.OutputSHA = hash(output)
				h.Review.SHA = f.put(h.Review.File, r).sha
			case "must", "missing-must", "not-success":
				var r runtimeResult
				data, _ := os.ReadFile(h.Review.File)
				json.Unmarshal(data, &r)
				if change == "must" {
					must := 1
					r.Must = &must
				}
				if change == "missing-must" {
					r.Must = nil
				}
				if change == "not-success" {
					r.Success = false
				}
				a := f.put(h.Review.File, r)
				h.Review.SHA = a.sha
			case "stop":
				data, err := os.ReadFile(f.p.db)
				if err != nil {
					t.Fatal(err)
				}
				var db map[string]any
				if err := json.Unmarshal(data, &db); err != nil {
					t.Fatal(err)
				}
				for _, value := range db["items"].([]any) {
					row := value.(map[string]any)
					if row["id"] == float64(3059) {
						row["owner_stopped"] = true
					}
				}
				f.put(f.p.db, db)
			}
			path := filepath.Join(f.p.root, ".ci-work", runtimeLeaf)
			f.put(path, h)
			if change == "duplicate" {
				data, _ := os.ReadFile(path)
				os.WriteFile(path, append([]byte(`{"Version":2,`), data[1:]...), 0600)
			}
			if _, e := constructRuntime(f.p, func(string) bool { return true }); e == nil {
				t.Fatal("invalid original scope or evidence accepted")
			}
		})
	}
}

func TestRuntimeSnapshotDoesNotReopenChangedOutputs(t *testing.T) {
	f, h := runtimeFixture(t, "secret", strings.Repeat("c", 40))
	s, e := constructRuntime(f.p, func(string) bool { return true })
	if e != nil {
		t.Fatal(e)
	}
	var r runtimeResult
	data, err := os.ReadFile(h.Review.File)
	if err != nil || json.Unmarshal(data, &r) != nil {
		t.Fatal("fixture")
	}
	if os.WriteFile(r.OutputFile, []byte("replaced after qualification"), 0600) != nil {
		t.Fatal("fixture")
	}
	if e = s.Consume(func(pin, review, acceptance []byte) error {
		if hash(review) != s.Report().ReviewSHA {
			t.Fatal("snapshot changed")
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	if _, e = constructRuntime(f.p, func(string) bool { return true }); e == nil {
		t.Fatal("new invocation accepted missing original")
	}
}
