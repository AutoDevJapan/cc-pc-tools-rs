// civ6-ci-route publishes only the reviewed three-workflow child of PR149.
package main

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	contract "cc/civ6-ci-contract"
)

const apiPrefix = "repos/" + contract.Repository
const gh = `D:\CC\Tools\codex-jobs\gh_gate.exe`

type api func(context.Context, string, string, any, any) error
type manifest struct {
	RepoID            int64     `json:"repo_id"`
	Head              string    `json:"original_head"`
	Tree              string    `json:"original_tree"`
	Base              string    `json:"base"`
	Files             []filePin `json:"files"`
	OfficialCISuccess bool      `json:"official_ci_success"`
}
type filePin struct {
	Path     string `json:"path"`
	Original string `json:"original_sha256"`
	Proposed string `json:"proposed_sha256"`
	Blob     string `json:"proposed_blob"`
}
type treeEntry struct {
	Path    string `json:"path"`
	Mode    string `json:"mode"`
	Type    string `json:"type"`
	SHA     string `json:"sha"`
	Content string `json:"content,omitempty"`
}
type tree struct {
	SHA       string
	Truncated bool
	Entries   []treeEntry `json:"tree"`
}
type commit struct {
	SHA     string
	Tree    struct{ SHA string }
	Parents []struct{ SHA string }
}
type objects struct {
	Head, Tree        string
	Files             []filePin
	BranchVerified    bool
	OfficialCISuccess bool
}

// Define Write on a named buffer so io.Copy cannot bypass the bound through
// an embedded Buffer.ReadFrom optimization.
type bounded struct {
	buf   bytes.Buffer
	limit int
}

func (b *bounded) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.buf.Len() {
		return 0, errors.New("response exceeds finite limit")
	}
	return b.buf.Write(p)
}
func call(ctx context.Context, method, path string, input, output any) error {
	if !allowed(method, path) {
		return errors.New("operation outside fixed PR149 routing")
	}
	c := exec.CommandContext(ctx, gh, "api", "--method", method, path)
	hide(c)
	c.WaitDelay = 2 * time.Second
	for _, kv := range os.Environ() {
		key, _, _ := strings.Cut(kv, "=")
		switch strings.ToUpper(key) {
		case "GH_TOKEN", "GITHUB_TOKEN", "GH_ENTERPRISE_TOKEN", "GITHUB_ENTERPRISE_TOKEN", "OPENAI_API_KEY", "ANTHROPIC_API_KEY":
			continue
		}
		c.Env = append(c.Env, kv)
	}
	c.Env = append(c.Env, "GH_PROMPT_DISABLED=1", "GH_NO_UPDATE_NOTIFIER=1")
	if input != nil {
		b, e := json.Marshal(input)
		if e != nil {
			return e
		}
		c.Args = append(c.Args, "--input", "-")
		c.Stdin = bytes.NewReader(b)
	}
	out := bounded{limit: 16 << 20}
	c.Stdout = &out
	c.Stderr = io.Discard
	if e := c.Run(); e != nil {
		return errors.New("fixed GitHub operation failed; no automatic retry")
	}
	if output != nil && json.Unmarshal(out.buf.Bytes(), output) != nil {
		return errors.New("fixed GitHub response invalid")
	}
	return nil
}
func allowed(method, path string) bool {
	switch method {
	case "GET":
		return path == fmt.Sprintf("repositories/%d", contract.RepoID) || path == apiPrefix+"/pulls/149" || path == apiPrefix+"/git/ref/heads/"+contract.OriginalBranch || path == apiPrefix+"/git/ref/heads/main" || immutableRead(path)
	case "POST":
		return path == apiPrefix+"/git/trees" || path == apiPrefix+"/git/commits"
	case "PATCH":
		return path == apiPrefix+"/git/refs/heads/"+contract.OriginalBranch
	}
	return false
}
func immutableRead(path string) bool {
	for _, prefix := range []string{apiPrefix + "/git/commits/", apiPrefix + "/git/trees/"} {
		if strings.HasPrefix(path, prefix) {
			sha := strings.TrimSuffix(strings.TrimPrefix(path, prefix), "?recursive=1")
			return contract.ValidSHA(sha) && (path == prefix+sha || (strings.Contains(prefix, "trees") && path == prefix+sha+"?recursive=1"))
		}
	}
	for _, w := range contract.Workflows() {
		if path == apiPrefix+"/contents/"+w.Path+"?ref="+contract.OriginalHead {
			return true
		}
	}
	return false
}
func blobID(b []byte) string {
	h := sha1.New()
	fmt.Fprintf(h, "blob %d\x00", len(b))
	h.Write(b)
	return hex.EncodeToString(h.Sum(nil))
}
func save(path string, v any) error {
	b, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return e
	}
	return saveBytes(path, append(b, '\n'))
}
func saveBytes(path string, b []byte) error {
	f, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	_, e = f.Write(b)
	if e == nil {
		e = f.Sync()
	}
	return errors.Join(e, f.Close())
}
func inspect(ctx context.Context, request api) error {
	var r struct {
		ID            int64
		FullName      string `json:"full_name"`
		Private, Fork bool
		Owner         struct{ Login string }
		Permissions   struct{ Admin bool }
	}
	if e := request(ctx, "GET", fmt.Sprintf("repositories/%d", contract.RepoID), nil, &r); e != nil {
		return e
	}
	if r.ID != contract.RepoID || r.FullName != contract.Repository || !r.Private || r.Fork || r.Owner.Login != "AutoDevJapan" || !r.Permissions.Admin {
		return errors.New("explicit owned repository mismatch")
	}
	var p struct {
		Number int
		State  string
		Merged bool
		Head   struct {
			SHA, Ref string
			Repo     struct {
				ID       int64
				FullName string `json:"full_name"`
				Fork     bool
			}
		}
		Base struct {
			SHA, Ref string
			Repo     struct {
				ID       int64
				FullName string `json:"full_name"`
				Fork     bool
			}
		}
	}
	if e := request(ctx, "GET", apiPrefix+"/pulls/149", nil, &p); e != nil {
		return e
	}
	if p.Number != 149 || p.State != "open" || p.Merged || p.Head.SHA != contract.OriginalHead || p.Head.Ref != contract.OriginalBranch || p.Base.SHA != contract.OriginalBase || p.Base.Ref != "main" || p.Head.Repo.ID != contract.RepoID || p.Base.Repo.ID != contract.RepoID || p.Head.Repo.FullName != contract.Repository || p.Base.Repo.FullName != contract.Repository || p.Head.Repo.Fork || p.Base.Repo.Fork {
		return errors.New("original PR/head/base changed")
	}
	for branch, sha := range map[string]string{contract.OriginalBranch: contract.OriginalHead, "main": contract.OriginalBase} {
		var ref struct{ Object struct{ SHA string } }
		if e := request(ctx, "GET", apiPrefix+"/git/ref/heads/"+branch, nil, &ref); e != nil {
			return e
		}
		if ref.Object.SHA != sha {
			return errors.New("original branch advanced")
		}
	}
	var c commit
	if e := request(ctx, "GET", apiPrefix+"/git/commits/"+contract.OriginalHead, nil, &c); e != nil {
		return e
	}
	if c.SHA != contract.OriginalHead || c.Tree.SHA != contract.OriginalTree {
		return errors.New("original commit/tree mismatch")
	}
	return nil
}
func proposal(ctx context.Context, request api) (manifest, map[string][]byte, error) {
	m := manifest{RepoID: contract.RepoID, Head: contract.OriginalHead, Tree: contract.OriginalTree, Base: contract.OriginalBase}
	files := map[string][]byte{}
	if e := inspect(ctx, request); e != nil {
		return m, nil, e
	}
	for _, w := range contract.Workflows() {
		var v struct{ Type, Encoding, Content, SHA string }
		if e := request(ctx, "GET", apiPrefix+"/contents/"+w.Path+"?ref="+contract.OriginalHead, nil, &v); e != nil {
			return m, nil, e
		}
		if len(v.Content) > 2<<20 || v.Type != "file" || v.Encoding != "base64" {
			return m, nil, errors.New("original workflow response invalid")
		}
		b, e := base64.StdEncoding.DecodeString(strings.ReplaceAll(v.Content, "\n", ""))
		if e != nil || len(b) > 1<<20 || blobID(b) != v.SHA {
			return m, nil, errors.New("original Git blob mismatch")
		}
		next, e := contract.Propose(b, w)
		if e != nil {
			return m, nil, e
		}
		files[w.Path] = next
		m.Files = append(m.Files, filePin{w.Path, contract.Digest(b), contract.Digest(next), blobID(next)})
	}
	return m, files, nil
}
func prepare(ctx context.Context, dir string, request api) error {
	m, files, e := proposal(ctx, request)
	if e != nil {
		return e
	}
	for _, f := range m.Files {
		if e = saveBytes(filepath.Join(dir, filepath.Base(f.Path)+".candidate"), files[f.Path]); e != nil {
			return e
		}
	}
	return save(filepath.Join(dir, "proposal.private.json"), m)
}
func compareTree(old, next tree, newSHA string, files []filePin) error {
	if old.SHA != contract.OriginalTree || old.Truncated || next.SHA != newSHA || next.Truncated || len(old.Entries) != len(next.Entries) || len(files) != 3 {
		return errors.New("complete tree inventory required")
	}
	expected := map[string]string{}
	for _, f := range files {
		if _, ok := contract.WorkflowByPath(f.Path); !ok || expected[f.Path] != "" || !contract.ValidSHA(f.Blob) {
			return errors.New("exactly three workflow blobs required")
		}
		expected[f.Path] = f.Blob
	}
	prior := map[string]treeEntry{}
	for _, x := range old.Entries {
		if x.Path == "" || prior[x.Path].Path != "" || !contract.ValidSHA(x.SHA) {
			return errors.New("original tree ambiguous")
		}
		prior[x.Path] = x
	}
	changed := 0
	for _, n := range next.Entries {
		x, ok := prior[n.Path]
		if !ok || x.Mode != n.Mode || x.Type != n.Type || !contract.ValidSHA(n.SHA) {
			return errors.New("unrelated path/mode/type changed")
		}
		delete(prior, n.Path)
		if want, ok := expected[n.Path]; ok {
			if n.SHA != want || n.Type != "blob" || n.SHA == x.SHA {
				return errors.New("proposed workflow blob differs")
			}
			changed++
		} else if x.SHA != n.SHA && !(n.Type == "tree" && (n.Path == ".github" || n.Path == ".github/workflows")) {
			return errors.New("unrelated source changed")
		}
	}
	if changed != 3 || len(prior) != 0 {
		return errors.New("tree comparison incomplete")
	}
	return nil
}
func publish(ctx context.Context, dir string, request api) error {
	// Every mutation is single use. A lost result retains its journal and cannot
	// trigger a second publication, a revert or an alternate transport.
	m, files, e := proposal(ctx, request)
	if e != nil {
		return e
	}
	saved, e := os.ReadFile(filepath.Join(dir, "proposal.private.json"))
	if e != nil || len(saved) > 65536 {
		return errors.New("saved reviewed proposal required")
	}
	var prior manifest
	if json.Unmarshal(saved, &prior) != nil {
		return errors.New("saved proposal invalid")
	}
	a, _ := json.Marshal(prior)
	b, _ := json.Marshal(m)
	if !bytes.Equal(a, b) {
		return errors.New("saved proposal drift")
	}
	for _, f := range m.Files {
		actual, e := os.ReadFile(filepath.Join(dir, filepath.Base(f.Path)+".candidate"))
		if e != nil || !bytes.Equal(actual, files[f.Path]) {
			return errors.New("saved candidate drift")
		}
	}
	if e = save(filepath.Join(dir, "publish-attempt.private.json"), m); e != nil {
		return e
	}
	entries := []treeEntry{}
	for _, f := range m.Files {
		entries = append(entries, treeEntry{Path: f.Path, Mode: "100644", Type: "blob", Content: string(files[f.Path])})
	}
	var t struct{ SHA string }
	if e = request(ctx, "POST", apiPrefix+"/git/trees", map[string]any{"base_tree": contract.OriginalTree, "tree": entries}, &t); e != nil {
		return e
	}
	if !contract.ValidSHA(t.SHA) || t.SHA == contract.OriginalTree {
		return errors.New("new tree identity invalid")
	}
	var old, next tree
	if e = request(ctx, "GET", apiPrefix+"/git/trees/"+contract.OriginalTree+"?recursive=1", nil, &old); e != nil {
		return e
	}
	if e = request(ctx, "GET", apiPrefix+"/git/trees/"+t.SHA+"?recursive=1", nil, &next); e != nil {
		return e
	}
	if e = compareTree(old, next, t.SHA, m.Files); e != nil {
		return e
	}
	var c struct{ SHA string }
	if e = request(ctx, "POST", apiPrefix+"/git/commits", map[string]any{"message": "ci: run checks on isolated repository runners and retain workspace doctests", "tree": t.SHA, "parents": []string{contract.OriginalHead}}, &c); e != nil {
		return e
	}
	if !contract.ValidSHA(c.SHA) || c.SHA == contract.OriginalHead {
		return errors.New("new commit invalid")
	}
	var actual commit
	if e = request(ctx, "GET", apiPrefix+"/git/commits/"+c.SHA, nil, &actual); e != nil {
		return e
	}
	if actual.SHA != c.SHA || actual.Tree.SHA != t.SHA || len(actual.Parents) != 1 || actual.Parents[0].SHA != contract.OriginalHead {
		return errors.New("new commit ancestry differs")
	}
	rec := objects{Head: c.SHA, Tree: t.SHA, Files: m.Files}
	if e = save(filepath.Join(dir, "objects.private.json"), rec); e != nil {
		return e
	}
	if e = inspect(ctx, request); e != nil {
		return e
	}
	if e = save(filepath.Join(dir, "ref-attempt.private.json"), rec); e != nil {
		return e
	}
	writeErr := request(ctx, "PATCH", apiPrefix+"/git/refs/heads/"+contract.OriginalBranch, map[string]any{"sha": c.SHA, "force": false}, nil)
	var ref struct{ Object struct{ SHA string } }
	readErr := request(ctx, "GET", apiPrefix+"/git/ref/heads/"+contract.OriginalBranch, nil, &ref)
	rec.BranchVerified = readErr == nil && ref.Object.SHA == c.SHA
	if e = save(filepath.Join(dir, "result.private.json"), rec); e != nil {
		return errors.Join(writeErr, readErr, e)
	}
	if writeErr != nil || readErr != nil || !rec.BranchVerified {
		return errors.New("branch update uncertain; inspect original journal, do not retry")
	}
	return nil
}
func owned(ctx context.Context, dir string) error {
	abs, e := filepath.Abs(dir)
	if e != nil || abs != filepath.Clean(dir) {
		return errors.New("absolute owned root required")
	}
	id := strings.TrimPrefix(filepath.Base(abs), "codex-owned-")
	if len(id) != 32 || strings.Trim(id, "0123456789abcdef") != "" {
		return errors.New("safe-delete owned root required")
	}
	c := exec.CommandContext(ctx, `D:\CC\Tools\safe-delete\safe-delete.exe`, "plan", "--id", id)
	hide(c)
	c.WaitDelay = 2 * time.Second
	b, e := c.Output()
	var p struct{ ID, Path, Action string }
	if e != nil || len(b) > 65536 || json.Unmarshal(b, &p) != nil || p.ID != id || !strings.EqualFold(filepath.Clean(p.Path), abs) || p.Action != "submit_to_exec_tool" {
		return errors.New("owned root verification failed")
	}
	return nil
}
func main() {
	if len(os.Args) != 3 || (os.Args[1] != "prepare" && os.Args[1] != "publish") {
		fmt.Fprintln(os.Stderr, "usage: civ6-ci-route prepare|publish <safe-delete-owned-root>")
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	err := owned(ctx, os.Args[2])
	if err == nil {
		if os.Args[1] == "prepare" {
			err = prepare(ctx, os.Args[2], call)
		} else {
			err = publish(ctx, os.Args[2], call)
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(`{"operation_complete":true,"official_ci_success":false}`)
}
