package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	contract "cc/civ6-ci-contract"
)

type mainFixture struct {
	*fixture
	loseRef         bool
	badParents      bool
	changeBeforeRef bool
}

func newMainFixture(t *testing.T) *mainFixture {
	t.Helper()
	f := newFixture(t)
	p := f.data[apiPrefix+"/pulls/149"].(map[string]any)
	p["head"].(map[string]any)["sha"] = contract.OldRoutedHead
	f.data[apiPrefix+"/git/ref/heads/"+contract.OriginalBranch] = map[string]any{"object": map[string]string{"sha": contract.OldRoutedHead}}
	f.data[apiPrefix+"/git/ref/heads/main"] = map[string]any{"object": map[string]string{"sha": contract.MergeMain}}
	f.data[apiPrefix+"/git/commits/"+contract.OldRoutedHead] = map[string]any{"sha": contract.OldRoutedHead, "tree": map[string]string{"sha": contract.OldRoutedTree}, "parents": []map[string]string{{"sha": contract.OriginalHead}}}
	f.data[apiPrefix+"/git/commits/"+contract.MergeMain] = map[string]any{"sha": contract.MergeMain, "tree": map[string]string{"sha": contract.MergeMainTree}}
	original := f.data[apiPrefix+"/git/trees/"+contract.OriginalTree+"?recursive=1"].(tree)
	old, main := tree{SHA: contract.OldRoutedTree, Entries: append([]treeEntry(nil), original.Entries...)}, tree{SHA: contract.MergeMainTree, Entries: append([]treeEntry(nil), original.Entries...)}
	for _, w := range contract.Workflows() {
		b, err := os.ReadFile(filepath.Join("../civ6-ci-contract/testdata", filepath.Base(w.Path)))
		if err != nil {
			t.Fatal(err)
		}
		o, err := contract.Propose(b, w)
		if err != nil {
			t.Fatal(err)
		}
		m, err := os.ReadFile(filepath.Join("../civ6-ci-contract/testdata/main392", filepath.Base(w.Path)))
		if err != nil {
			t.Fatal(err)
		}
		for sha, body := range map[string][]byte{contract.OldRoutedHead: o, contract.MergeMain: m} {
			f.data[apiPrefix+"/contents/"+w.Path+"?ref="+sha] = map[string]string{"type": "file", "encoding": "base64", "content": base64.StdEncoding.EncodeToString(body), "sha": blobID(body)}
		}
		for i, e := range original.Entries {
			if e.Path == w.Path {
				old.Entries[i].SHA = blobID(o)
				main.Entries[i].SHA = blobID(m)
			}
		}
	}
	f.data[apiPrefix+"/git/trees/"+contract.OldRoutedTree+"?recursive=1"] = old
	f.data[apiPrefix+"/git/trees/"+contract.MergeMainTree+"?recursive=1"] = main
	return &mainFixture{fixture: f}
}

func (f *mainFixture) call(ctx context.Context, method, path string, input, out any) error {
	if !allowed(method, path) {
		return errors.New("endpoint outside fixed source")
	}
	if method == "GET" {
		return f.fixture.call(ctx, method, path, input, out)
	}
	f.writes = append(f.writes, method+" "+path)
	m := input.(map[string]any)
	put := func(v any) error { b, _ := json.Marshal(v); return json.Unmarshal(b, out) }
	switch method + " " + path {
	case "POST " + apiPrefix + "/git/trees":
		if m["base_tree"] != contract.MergeMainTree {
			return errors.New("not based on fixed main tree")
		}
		a := f.data[apiPrefix+"/git/trees/"+contract.MergeMainTree+"?recursive=1"].(tree)
		b := tree{SHA: strings.Repeat("a", 40), Entries: append([]treeEntry(nil), a.Entries...)}
		entries := m["tree"].([]treeEntry)
		if len(entries) != 3 {
			return errors.New("exactly three main workflow edits required")
		}
		for _, e := range entries {
			for i, x := range b.Entries {
				if x.Path == e.Path {
					b.Entries[i].SHA = blobID([]byte(e.Content))
				}
			}
		}
		if f.badTree {
			b.Entries[len(b.Entries)-1].SHA = strings.Repeat("d", 40)
		}
		f.data[apiPrefix+"/git/trees/"+b.SHA+"?recursive=1"] = b
		return put(map[string]string{"sha": b.SHA})
	case "POST " + apiPrefix + "/git/commits":
		parents := append([]string(nil), m["parents"].([]string)...)
		if len(parents) != 2 || parents[0] != contract.OldRoutedHead || parents[1] != contract.MergeMain {
			return errors.New("not a normal ordered old-tip/main merge")
		}
		if f.badParents {
			parents[0], parents[1] = parents[1], parents[0]
		}
		sha := strings.Repeat("b", 40)
		f.data[apiPrefix+"/git/commits/"+sha] = map[string]any{"sha": sha, "tree": map[string]string{"sha": strings.Repeat("a", 40)}, "parents": []map[string]string{{"sha": parents[0]}, {"sha": parents[1]}}}
		if f.changeBeforeRef {
			f.data[apiPrefix+"/git/ref/heads/main"] = map[string]any{"object": map[string]string{"sha": strings.Repeat("e", 40)}}
		}
		return put(map[string]string{"sha": sha})
	case "PATCH " + apiPrefix + "/git/refs/heads/" + contract.OriginalBranch:
		if m["force"] != false || m["sha"] != strings.Repeat("b", 40) {
			return errors.New("not fixed nonforce publication")
		}
		f.data[apiPrefix+"/git/ref/heads/"+contract.OriginalBranch] = map[string]any{"object": map[string]string{"sha": m["sha"].(string)}}
		if f.loseRef {
			return errors.New("response lost")
		}
		return nil
	}
	return errors.New("unexpected mutation")
}

func TestPrepareMainIsReadOnlyAndPublishHasOrderedMergeParents(t *testing.T) {
	f := newMainFixture(t)
	dir := t.TempDir()
	if err := prepareMain(context.Background(), dir, f.call); err != nil {
		t.Fatal(err)
	}
	if len(f.writes) != 0 {
		t.Fatal("prepare wrote to GitHub")
	}
	b, _ := os.ReadFile(filepath.Join(dir, "proposal.private.json"))
	var m manifest
	if json.Unmarshal(b, &m) != nil || m.Generation != contract.MergeGeneration || m.Head != contract.OldRoutedHead || m.Tree != contract.OldRoutedTree || m.BaselineTree != contract.MergeMainTree || len(m.Parents) != 2 {
		t.Fatal("new generation manifest loses original identities")
	}
	if err := publishUsing(context.Background(), dir, f.call, proposalMain, inspectMain); err != nil {
		t.Fatal(err)
	}
	if len(f.writes) != 3 {
		t.Fatal("unexpected publication effects")
	}
	var rec objects
	b, _ = os.ReadFile(filepath.Join(dir, "result.private.json"))
	if json.Unmarshal(b, &rec) != nil || !rec.BranchVerified || rec.OfficialCISuccess {
		t.Fatal("publication fabricated CI success")
	}
}

func TestMainPublishRejectsChangedSourceAndLegacyPreparedManifest(t *testing.T) {
	for _, kind := range []string{"old-manifest", "main", "tip", "base", "owner", "repo", "fork", "candidate", "manifest-parents", "tree", "parents", "before-ref", "lost-ref"} {
		t.Run(kind, func(t *testing.T) {
			f := newMainFixture(t)
			dir := t.TempDir()
			if kind == "old-manifest" {
				if err := prepare(context.Background(), dir, newFixture(t).call); err != nil {
					t.Fatal(err)
				}
			} else if err := prepareMain(context.Background(), dir, f.call); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "main":
				f.data[apiPrefix+"/git/ref/heads/main"] = map[string]any{"object": map[string]string{"sha": strings.Repeat("e", 40)}}
			case "tip":
				f.data[apiPrefix+"/git/ref/heads/"+contract.OriginalBranch] = map[string]any{"object": map[string]string{"sha": contract.OriginalHead}}
			case "base":
				f.data[apiPrefix+"/pulls/149"].(map[string]any)["base"].(map[string]any)["sha"] = strings.Repeat("e", 40)
			case "owner":
				f.data["repositories/1392152644"].(map[string]any)["owner"] = map[string]string{"login": "foreign"}
			case "repo":
				f.data["repositories/1392152644"].(map[string]any)["id"] = 1
			case "fork":
				f.data[apiPrefix+"/pulls/149"].(map[string]any)["head"].(map[string]any)["repo"].(map[string]any)["fork"] = true
			case "candidate":
				_ = os.WriteFile(filepath.Join(dir, "rust.yml.candidate"), []byte("changed"), 0600)
			case "manifest-parents":
				b, _ := os.ReadFile(filepath.Join(dir, "proposal.private.json"))
				var m manifest
				_ = json.Unmarshal(b, &m)
				m.Parents[0], m.Parents[1] = m.Parents[1], m.Parents[0]
				b, _ = json.Marshal(m)
				_ = os.WriteFile(filepath.Join(dir, "proposal.private.json"), b, 0600)
			case "tree":
				f.badTree = true
			case "parents":
				f.badParents = true
			case "before-ref":
				f.changeBeforeRef = true
			case "lost-ref":
				f.loseRef = true
			}
			if err := publishUsing(context.Background(), dir, f.call, proposalMain, inspectMain); err == nil {
				t.Fatal("changed or uncertain publication accepted")
			}
			limit := 0
			switch kind {
			case "tree":
				limit = 1
			case "parents", "before-ref":
				limit = 2
			case "lost-ref":
				limit = 3
			}
			if len(f.writes) != limit {
				t.Fatal("unexpected mutation count", f.writes)
			}
			if limit > 0 {
				if err := publishUsing(context.Background(), dir, f.call, proposalMain, inspectMain); err == nil || len(f.writes) != limit {
					t.Fatal("uncertain publication was retried")
				}
			}
			if kind == "lost-ref" {
				b, _ := os.ReadFile(filepath.Join(dir, "result.private.json"))
				var r objects
				if json.Unmarshal(b, &r) != nil || !r.BranchVerified || r.OfficialCISuccess {
					t.Fatal("lost response observation was discarded")
				}
			}
		})
	}
}
