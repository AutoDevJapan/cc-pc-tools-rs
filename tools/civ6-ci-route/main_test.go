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

type fixture struct {
	data     map[string]any
	writes   []string
	failRef  bool
	badTree  bool
	advanced bool
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	repo := map[string]any{"id": contract.RepoID, "full_name": contract.Repository, "private": true, "fork": false, "owner": map[string]string{"login": "AutoDevJapan"}, "permissions": map[string]bool{"admin": true}}
	f := &fixture{data: map[string]any{
		"repositories/1392152644":                               repo,
		apiPrefix + "/pulls/149":                                map[string]any{"number": 149, "state": "open", "merged": false, "head": map[string]any{"sha": contract.OriginalHead, "ref": contract.OriginalBranch, "repo": repo}, "base": map[string]any{"sha": contract.OriginalBase, "ref": "main", "repo": repo}},
		apiPrefix + "/git/commits/" + contract.OriginalHead:     map[string]any{"sha": contract.OriginalHead, "tree": map[string]string{"sha": contract.OriginalTree}},
		apiPrefix + "/git/ref/heads/" + contract.OriginalBranch: map[string]any{"object": map[string]string{"sha": contract.OriginalHead}},
		apiPrefix + "/git/ref/heads/main":                       map[string]any{"object": map[string]string{"sha": contract.OriginalBase}},
	}}
	old := tree{SHA: contract.OriginalTree}
	for _, w := range contract.Workflows() {
		b, e := os.ReadFile(filepath.Join("../civ6-ci-contract/testdata", filepath.Base(w.Path)))
		if e != nil {
			t.Fatal(e)
		}
		f.data[apiPrefix+"/contents/"+w.Path+"?ref="+contract.OriginalHead] = map[string]string{"type": "file", "encoding": "base64", "content": base64.StdEncoding.EncodeToString(b), "sha": blobID(b)}
		old.Entries = append(old.Entries, treeEntry{Path: w.Path, Mode: "100644", Type: "blob", SHA: blobID(b)})
	}
	old.Entries = append(old.Entries, treeEntry{Path: "tools/Cargo.toml", Mode: "100644", Type: "blob", SHA: strings.Repeat("e", 40)})
	f.data[apiPrefix+"/git/trees/"+contract.OriginalTree+"?recursive=1"] = old
	return f
}
func (f *fixture) call(ctx context.Context, method, path string, input, out any) error {
	if !allowed(method, path) {
		return errors.New("fixture endpoint outside contract")
	}
	if method != "GET" {
		f.writes = append(f.writes, method+" "+path)
	}
	switch method + " " + path {
	case "POST " + apiPrefix + "/git/trees":
		m := input.(map[string]any)
		if m["base_tree"] != contract.OriginalTree {
			return errors.New("wrong original tree")
		}
		entries := m["tree"].([]treeEntry)
		if len(entries) != 3 {
			return errors.New("wrong number of blobs")
		}
		for _, entry := range entries {
			b, _ := json.Marshal(entry)
			var wire map[string]any
			if json.Unmarshal(b, &wire) != nil || wire["content"] == "" {
				return errors.New("workflow content missing from mutation")
			}
			if _, both := wire["sha"]; both {
				return errors.New("GitHub tree entry must not send both content and sha")
			}
		}
		old := f.data[apiPrefix+"/git/trees/"+contract.OriginalTree+"?recursive=1"].(tree)
		n := tree{SHA: strings.Repeat("a", 40)}
		n.Entries = append([]treeEntry(nil), old.Entries...)
		for _, e := range entries {
			for i := range n.Entries {
				if n.Entries[i].Path == e.Path {
					n.Entries[i].SHA = blobID([]byte(e.Content))
				}
			}
		}
		if f.badTree {
			n.Entries[len(n.Entries)-1].SHA = strings.Repeat("d", 40)
		}
		f.data[apiPrefix+"/git/trees/"+n.SHA+"?recursive=1"] = n
		b, _ := json.Marshal(map[string]string{"sha": n.SHA})
		return json.Unmarshal(b, out)
	case "POST " + apiPrefix + "/git/commits":
		m := input.(map[string]any)
		if m["tree"] != strings.Repeat("a", 40) || len(m["parents"].([]string)) != 1 || m["parents"].([]string)[0] != contract.OriginalHead {
			return errors.New("wrong commit parent")
		}
		sha := strings.Repeat("b", 40)
		f.data[apiPrefix+"/git/commits/"+sha] = map[string]any{"sha": sha, "tree": map[string]string{"sha": strings.Repeat("a", 40)}, "parents": []map[string]string{{"sha": contract.OriginalHead}}}
		b, _ := json.Marshal(map[string]string{"sha": sha})
		return json.Unmarshal(b, out)
	case "PATCH " + apiPrefix + "/git/refs/heads/" + contract.OriginalBranch:
		m := input.(map[string]any)
		if m["force"] != false || m["sha"] != strings.Repeat("b", 40) {
			return errors.New("not exact nonforce update")
		}
		f.data[apiPrefix+"/git/ref/heads/"+contract.OriginalBranch] = map[string]any{"object": map[string]string{"sha": strings.Repeat("b", 40)}}
		if f.failRef {
			return errors.New("synthetic lost response")
		}
		return nil
	}
	if method != "GET" {
		return errors.New("unknown mutation")
	}
	v, ok := f.data[path]
	if !ok {
		return errors.New("missing fixed read")
	}
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	return json.Unmarshal(b, out)
}
func TestPrepareIsReadOnlyAndPublishExactlyThreeWorkflows(t *testing.T) {
	f := newFixture(t)
	dir := t.TempDir()
	if e := prepare(context.Background(), dir, f.call); e != nil {
		t.Fatal(e)
	}
	if len(f.writes) != 0 {
		t.Fatal("prepare mutated GitHub")
	}
	if e := publish(context.Background(), dir, f.call); e != nil {
		t.Fatal(e)
	}
	if len(f.writes) != 3 || !strings.HasPrefix(f.writes[2], "PATCH ") {
		t.Fatal("unexpected mutations")
	}
	var r objects
	b, e := os.ReadFile(filepath.Join(dir, "result.private.json"))
	if e != nil || json.Unmarshal(b, &r) != nil || !r.BranchVerified || r.OfficialCISuccess {
		t.Fatal("publication falsely certifies CI or omits ref proof")
	}
}
func TestChangedCurrentSourceOrCandidateRejectsBeforeMutation(t *testing.T) {
	for _, kind := range []string{"main", "pr", "bytes", "manifest"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t)
			dir := t.TempDir()
			if e := prepare(context.Background(), dir, f.call); e != nil {
				t.Fatal(e)
			}
			switch kind {
			case "main":
				f.data[apiPrefix+"/git/ref/heads/main"] = map[string]any{"object": map[string]string{"sha": strings.Repeat("c", 40)}}
			case "pr":
				f.data[apiPrefix+"/pulls/149"].(map[string]any)["state"] = "closed"
			case "bytes":
				if e := os.WriteFile(filepath.Join(dir, "rust.yml.candidate"), []byte("changed"), 0600); e != nil {
					t.Fatal(e)
				}
			case "manifest":
				if e := os.WriteFile(filepath.Join(dir, "proposal.private.json"), []byte(`{"repo_id":1}`), 0600); e != nil {
					t.Fatal(e)
				}
			}
			if publish(context.Background(), dir, f.call) == nil || len(f.writes) != 0 {
				t.Fatal("changed source/candidate published")
			}
		})
	}
}
func TestUnrelatedTreeAndLostRefResponseCannotRetryOrCertifySuccess(t *testing.T) {
	for _, mode := range []string{"unrelated", "lost-ref"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t)
			dir := t.TempDir()
			if e := prepare(context.Background(), dir, f.call); e != nil {
				t.Fatal(e)
			}
			f.badTree = mode == "unrelated"
			f.failRef = mode == "lost-ref"
			if publish(context.Background(), dir, f.call) == nil {
				t.Fatal("unsafe or uncertain publish succeeded")
			}
			n := len(f.writes)
			if mode == "unrelated" && n != 1 {
				t.Fatal("changed source reached commit/ref mutation")
			}
			if publish(context.Background(), dir, f.call) == nil || len(f.writes) != n {
				t.Fatal("uncertain publication repeated a write")
			}
			if mode == "lost-ref" {
				var rec objects
				b, _ := os.ReadFile(filepath.Join(dir, "result.private.json"))
				if json.Unmarshal(b, &rec) != nil || !rec.BranchVerified || rec.OfficialCISuccess {
					t.Fatal("uncertain ref proof lost or CI fabricated")
				}
			}
		})
	}
}
func TestOnlyExactMutationEndpoints(t *testing.T) {
	for _, v := range []struct{ m, p string }{{"POST", apiPrefix + "/actions/runners/registration-token"}, {"POST", apiPrefix + "/statuses/" + contract.OriginalHead}, {"PATCH", apiPrefix + "/git/refs/heads/main"}, {"GET", "repos/other/repo/pulls/149"}, {"GET", apiPrefix + "/contents/.env?ref=" + contract.OriginalHead}, {"GET", apiPrefix + "/git/trees/" + contract.OriginalTree + "?recursive=1&other=x"}} {
		if allowed(v.m, v.p) {
			t.Fatal("outside routing operation accepted")
		}
	}
}
