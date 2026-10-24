package contract

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These are the two public commits named by ToDo3576, not caller-selected pins.
const fixtureMain = "392e7f9bbcea6ee5867b78da37de1efd9f9fec48"
const fixtureMainTree = "b5680a158cfcad929e49e97702eec210cc82135d"
const fixtureOld = "2bd3a366f8974ba54df83927102aa5bdad3983e5"
const fixtureOldTree = "a4e37a064e0f2ea316d0f15d9aaf710abf9b65f6"

const fixtureConcurrency = "concurrency:\n  group: ${{ github.workflow }}-${{ github.event_name }}-${{ github.event.pull_request.number || github.run_id }}\n  cancel-in-progress: ${{ github.event_name == 'pull_request' }}\n\n"

func mergeFixture(t *testing.T) (Plan, map[string]any) {
	t.Helper()
	p, f := nativeFixture(t)
	prefix := "repos/" + Repository + "/"
	f[prefix+"git/ref/heads/main"] = map[string]any{"object": map[string]string{"sha": fixtureMain}}
	f[prefix+"git/commits/"+p.Head] = map[string]any{"sha": p.Head, "tree": map[string]string{"sha": p.Tree}, "parents": []map[string]string{{"sha": fixtureOld}, {"sha": fixtureMain}}}
	f[prefix+"git/commits/"+fixtureMain] = map[string]any{"sha": fixtureMain, "tree": map[string]string{"sha": fixtureMainTree}}
	f[prefix+"git/commits/"+fixtureOld] = map[string]any{"sha": fixtureOld, "tree": map[string]string{"sha": fixtureOldTree}, "parents": []map[string]string{{"sha": OriginalHead}}}
	mainTree := f[prefix+"git/trees/"+OriginalTree+"?recursive=1"].(treeReply)
	mainTree.SHA = fixtureMainTree
	mainTree.Entries = append([]treeEntry(nil), mainTree.Entries...)
	oldTree := f[prefix+"git/trees/"+p.Tree+"?recursive=1"].(treeReply)
	oldTree.SHA = fixtureOldTree
	oldTree.Entries = append([]treeEntry(nil), oldTree.Entries...)
	newTree := oldTree
	newTree.SHA = p.Tree
	newTree.Entries = append([]treeEntry(nil), oldTree.Entries...)
	for _, w := range Workflows() {
		data, err := os.ReadFile(filepath.Join("testdata/main392", filepath.Base(w.Path)))
		if err != nil {
			t.Fatal(err)
		}
		// A byte-level expected candidate independent of the admission verifier.
		next := strings.Replace(string(data), "jobs:\n", fixtureConcurrency+"jobs:\n", 1)
		next = strings.Replace(next, "    runs-on: "+w.OriginalRoute, "    runs-on: "+Route(w), 1)
		next = strings.Replace(next, "os: [ubuntu-latest, windows-latest]", "os: [windows-latest]", 1)
		next = strings.Replace(next, "ref: ${{ github.sha }}", "ref: "+CheckoutRef, 1)
		if w.Job == "test" {
			step := "      - name: Record release binary hashes\n        shell: pwsh\n        run: |\n"
			for _, line := range strings.Split(strings.TrimSuffix(ArtifactHashCommand, "\n"), "\n") {
				step += "          " + line + "\n"
			}
			next = strings.Replace(next, "      - name: Check existing scenario JSON\n", step+"      - name: Check existing scenario JSON\n", 1)
		}
		old, err := Propose(originals(t)[w.Path], w)
		if err != nil {
			t.Fatal(err)
		}
		for sha, body := range map[string][]byte{fixtureMain: data, fixtureOld: old, p.Head: []byte(next), p.ContextSHA: []byte(next)} {
			f[prefix+"contents/"+w.Path+"?ref="+sha] = map[string]string{"type": "file", "encoding": "base64", "content": base64.StdEncoding.EncodeToString(body), "sha": blobID(body)}
		}
		for i, entry := range mainTree.Entries {
			if entry.Path == w.Path {
				mainTree.Entries[i].SHA = blobID(data)
				newTree.Entries[i].SHA = blobID([]byte(next))
			}
		}
		if w.Path == p.Workflow {
			p.WorkflowDigest = Digest([]byte(next))
		}
	}
	f[prefix+"git/trees/"+fixtureMainTree+"?recursive=1"] = mainTree
	f[prefix+"git/trees/"+fixtureOldTree+"?recursive=1"] = oldTree
	f[prefix+"git/trees/"+p.Tree+"?recursive=1"] = newTree
	return p, f
}

func TestFixedMainMergeAdmission(t *testing.T) {
	p, f := mergeFixture(t)
	if err := Verify(context.Background(), p, fixtureRead(f)); err != nil {
		t.Fatalf("fixed main merge rejected: %v", err)
	}
}

func TestFixedMainMergeRejectsChangedAuthorityParentsAndTree(t *testing.T) {
	for _, kind := range []string{"owner", "same-name-repo", "fork", "base", "main-advance", "parent-order", "extra-parent", "other-parent", "other-tree", "extra-file", "command", "checkout", "workflow-sha", "original-constant"} {
		t.Run(kind, func(t *testing.T) {
			p, f := mergeFixture(t)
			prefix := "repos/" + Repository + "/"
			switch kind {
			case "owner":
				f["repositories/1392152644"].(map[string]any)["owner"] = map[string]string{"login": "other"}
			case "same-name-repo":
				f["repositories/1392152644"].(map[string]any)["id"] = int64(1)
			case "fork":
				f[prefix+"pulls/149"].(map[string]any)["head"].(map[string]any)["repo"].(map[string]any)["fork"] = true
			case "base":
				f[prefix+"pulls/149"].(map[string]any)["base"].(map[string]any)["sha"] = strings.Repeat("7", 40)
			case "main-advance":
				f[prefix+"git/ref/heads/main"] = map[string]any{"object": map[string]string{"sha": strings.Repeat("7", 40)}}
			case "parent-order", "extra-parent", "other-parent":
				parents := []map[string]string{{"sha": fixtureOld}, {"sha": fixtureMain}}
				if kind == "parent-order" {
					parents[0], parents[1] = parents[1], parents[0]
				}
				if kind == "extra-parent" {
					parents = append(parents, map[string]string{"sha": OriginalHead})
				}
				if kind == "other-parent" {
					parents[0]["sha"] = OriginalHead
				}
				f[prefix+"git/commits/"+p.Head].(map[string]any)["parents"] = parents
			case "other-tree", "extra-file":
				key := prefix + "git/trees/" + p.Tree + "?recursive=1"
				tr := f[key].(treeReply)
				if kind == "other-tree" {
					tr.Entries[len(tr.Entries)-1].SHA = strings.Repeat("2", 40)
				} else {
					tr.Entries = append(tr.Entries, treeEntry{"new.go", "100644", "blob", strings.Repeat("2", 40)})
				}
				f[key] = tr
			case "command", "checkout":
				key := prefix + "contents/" + p.Workflow + "?ref=" + p.Head
				v := f[key].(map[string]string)
				b, _ := base64.StdEncoding.DecodeString(v["content"])
				if kind == "command" {
					b = []byte(strings.Replace(string(b), "--all-targets", "--lib", 1))
				} else {
					b = []byte(strings.Replace(string(b), "actions/checkout@d23441a48e516b6c34aea4fa41551a30e30af803", "actions/checkout@main", 1))
				}
				v["content"], v["sha"] = base64.StdEncoding.EncodeToString(b), blobID(b)
			case "workflow-sha":
				p.WorkflowSHA = strings.Repeat("7", 40)
			case "original-constant":
				key := prefix + "contents/" + p.Workflow + "?ref=" + OriginalHead
				v := f[key].(map[string]string)
				v["content"], v["sha"] = base64.StdEncoding.EncodeToString([]byte("wrong")), blobID([]byte("wrong"))
			}
			if Verify(context.Background(), p, fixtureRead(f)) == nil {
				t.Fatal("changed merged source admitted")
			}
		})
	}
}

func TestProposeMainRetainsRealMainCommandsCheckoutAndSingleDoctest(t *testing.T) {
	p, f := mergeFixture(t)
	for _, w := range MainWorkflows() {
		b, err := os.ReadFile(filepath.Join("testdata/main392", filepath.Base(w.Path)))
		if err != nil {
			t.Fatal(err)
		}
		got, err := ProposeMain(b, w)
		if err != nil {
			t.Fatal(err)
		}
		v := f["repos/"+Repository+"/contents/"+w.Path+"?ref="+p.Head].(map[string]string)
		want, _ := base64.StdEncoding.DecodeString(v["content"])
		if string(got) != string(want) {
			t.Fatal("independent expected main candidate differs")
		}
		for _, line := range strings.Split(string(b), "\n") {
			if strings.Contains(line, "run:") || strings.Contains(line, "cargo ") || strings.Contains(line, "uses:") {
				if !strings.Contains(string(got), line) {
					t.Fatal("main command/checkout version lost", line)
				}
			}
		}
		if w.Job == "test" && strings.Count(string(got), DoctestCommand) != 1 {
			t.Fatal("main doctest duplicated or lost")
		}
		if _, err := Propose(b, w); err == nil {
			t.Fatal("main fixture reinterpreted as original generation")
		}
		if _, err := ProposeMain(append(b, ' '), w); err == nil {
			t.Fatal("unfixed main input accepted")
		}
	}
}

func TestMergedMainContinuationUsesActualMergedGeneration(t *testing.T) {
	p, f := mergeFixture(t)
	prefix := "repos/" + Repository + "/"
	origin := p.Head
	pr := f[prefix+"pulls/149"].(map[string]any)
	pr["merged"], pr["state"], pr["merge_commit_sha"] = true, "closed", origin
	p.Head, p.Tree = strings.Repeat("9", 40), strings.Repeat("8", 40)
	p.Event, p.PR, p.Branch, p.ContextSHA, p.WorkflowSHA = "push", 0, "main", p.Head, p.Head
	p.WorkflowRef = Repository + "/" + p.Workflow + "@" + p.Ref()
	f[prefix+"git/ref/heads/main"] = map[string]any{"object": map[string]string{"sha": p.Head}}
	f[prefix+"git/commits/"+p.Head] = map[string]any{"sha": p.Head, "tree": map[string]string{"sha": p.Tree}}
	f[prefix+"compare/"+origin+"..."+p.Head] = map[string]string{"status": "ahead"}
	for _, w := range MainWorkflows() {
		f[prefix+"contents/"+w.Path+"?ref="+p.Head] = f[prefix+"contents/"+w.Path+"?ref="+origin]
	}
	run := f[prefix+"actions/runs/7"].(map[string]any)
	run["head_sha"], run["event"], run["head_branch"], run["pull_requests"] = p.Head, "push", "main", []any{}
	if err := Verify(context.Background(), p, fixtureRead(f)); err != nil {
		t.Fatal(err)
	}
	// The historical source remains verifiable after branch advancement, while
	// attempting to execute it is rejected by the ordinary current-source check.
	oldPlan, _ := mergeFixture(t)
	if Verify(context.Background(), oldPlan, fixtureRead(f)) == nil {
		t.Fatal("obsolete source executed after merge")
	}
	if err := VerifyOldRoutedSource(context.Background(), fixtureRead(f)); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(f[prefix+"git/commits/"+origin])
	if !strings.Contains(string(b), fixtureMain) {
		t.Fatal("continuation fixture lost merged original source")
	}
}

func TestHistoricalInitialUsesStoredOriginalDigestWithoutOrphanMergeRead(t *testing.T) {
	for _, kind := range []string{"valid", "digest", "head", "attempt", "phase", "run", "repository"} {
		t.Run(kind, func(t *testing.T) {
			p, f := mergeFixture(t)
			p.Head, p.Tree = OldRoutedHead, OldRoutedTree
			prefix := "repos/" + Repository + "/"
			v := f[prefix+"contents/"+p.Workflow+"?ref="+OldRoutedHead].(map[string]string)
			body, _ := base64.StdEncoding.DecodeString(v["content"])
			p.WorkflowDigest = Digest(body)
			repo := f["repositories/1392152644"]
			run := map[string]any{"id": p.RunID, "run_attempt": 1, "head_sha": p.Head, "head_branch": p.Branch, "event": "pull_request", "path": p.Workflow, "status": "completed", "repository": repo, "head_repository": repo, "pull_requests": []map[string]int{{"number": 149}}}
			f[prefix+"actions/runs/7/attempts/1"] = run
			switch kind {
			case "digest":
				p.WorkflowDigest = strings.Repeat("0", 64)
			case "head":
				p.Head = MergeMain
			case "attempt":
				p.Attempt = 2
			case "phase":
				p.Event = "workflow_dispatch"
			case "run":
				run["status"] = "in_progress"
			case "repository":
				run["head_repository"] = map[string]any{"id": int64(1), "full_name": Repository}
			}
			read := fixtureRead(f)
			err := VerifyHistoricalInitial(context.Background(), p, func(ctx context.Context, path string, out any) error {
				if strings.Contains(path, "/contents/") && strings.HasSuffix(path, "?ref="+p.ContextSHA) {
					t.Fatal("historical cleanup depended on orphan merge commit")
				}
				return read(ctx, path, out)
			})
			if (err == nil) != (kind == "valid") {
				t.Fatal("historical immutable source result differs", err)
			}
		})
	}
}
