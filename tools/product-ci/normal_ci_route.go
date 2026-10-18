package main

import (
	"bytes"
	"cc/product-ci/normalci"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const normalRouteRepo = "AutoDevJapan/GoDD-MCP-Alpha"
const normalRouteRef = "codex/todo2641-http-response-pseudo-20261015"
const normalRouteTree = "f7eea82c96449b7749f98dd6c4b0344b64f1d651"

type normalRouteIntent struct {
	Todo                                              int
	RepoID                                            int64
	Lease, OldHead, OldTree, OldWorkflow, NewWorkflow string
}
type normalRouteCommit struct{ Head, Tree, Workflow, Lease string }
type normalMutation = normalci.Mutation

func normalRecord(root, name string, v any) error {
	data, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return e
	}
	f, e := os.OpenFile(filepath.Join(root, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return errors.New("exclusive route receipt unavailable")
	}
	_, e = f.Write(data)
	if e == nil {
		e = f.Sync()
	}
	return errors.Join(e, f.Close())
}
func normalBlobID(b []byte) string {
	h := sha1.New()
	fmt.Fprintf(h, "blob %d%c", len(b), 0)
	_, _ = h.Write(b)
	return hex.EncodeToString(h.Sum(nil))
}
func normalRouteCandidate() candidate {
	return candidate{ID: normalCIRepo, PR: 3613, SHA: "b194d66524ef86330bb73ca3f22f9e5dd6ae0a07"}
}
func verifyNormalRouteSource(ctx context.Context, c candidate, read api) (repository, error) {
	r, e := verify(ctx, c, read)
	if e != nil {
		return r, e
	}
	var p pull
	if r.FullName != normalRouteRepo || read(ctx, "repos/"+normalRouteRepo+"/pulls/3613", &p) != nil || p.Head.Ref != normalRouteRef || p.Head.SHA != c.SHA {
		return repository{}, errors.New("normal route PR/ref changed")
	}
	return r, nil
}

// Apply only the original four scalar routes, on the original PR branch. All
// source bodies and API mutation payloads stay in RAM. No workflow is dispatched.
func applyNormalRoute(ctx context.Context, root string, read api, write normalMutation) (result normalRouteCommit, ret error) {
	return applyNormalRouteForWorkflow(ctx, root, normalCIWorkflowSHA, read, write)
}
func applyNormalRouteForWorkflow(ctx context.Context, root, expectedWorkflow string, read api, write normalMutation) (result normalRouteCommit, ret error) {
	if !filepath.IsAbs(root) {
		return result, errors.New("owned route root must be absolute")
	}
	c := normalRouteCandidate()
	r, e := verifyNormalRouteSource(ctx, c, read)
	if e != nil {
		return result, e
	}
	if r.FullName != normalRouteRepo {
		return result, errors.New("route repository changed")
	}
	var old struct {
		SHA  string
		Tree struct{ SHA string }
	}
	if read(ctx, "repos/"+normalRouteRepo+"/git/commits/"+c.SHA, &old) != nil || old.SHA != c.SHA || old.Tree.SHA != normalRouteTree {
		return result, errors.New("route base tree rejected")
	}
	var original struct{ Encoding, Content string }
	if read(ctx, "repos/"+normalRouteRepo+"/contents/.github/workflows/ci.yml?ref="+c.SHA, &original) != nil || original.Encoding != "base64" {
		return result, errors.New("route workflow unavailable")
	}
	raw, e := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(original.Content), ""))
	if e != nil || digest(raw) != expectedWorkflow {
		return result, errors.New("route workflow source changed")
	}
	var oldFiles struct {
		SHA       string
		Truncated bool
		Tree      []struct{ Path, Mode, Type, SHA string }
	}
	if read(ctx, "repos/"+normalRouteRepo+"/git/trees/"+normalRouteTree+"?recursive=1", &oldFiles) != nil || oldFiles.SHA != normalRouteTree || oldFiles.Truncated {
		return result, errors.New("original route file mode unavailable")
	}
	mode := ""
	for _, f := range oldFiles.Tree {
		if f.Path == ".github/workflows/ci.yml" {
			if mode != "" || f.Type != "blob" || f.SHA != normalBlobID(raw) || (f.Mode != "100644" && f.Mode != "100755") {
				return result, errors.New("original route file mode rejected")
			}
			mode = f.Mode
		}
	}
	if mode == "" {
		return result, errors.New("original route file not present")
	}
	var nonce [16]byte
	if _, e = rand.Read(nonce[:]); e != nil {
		return result, e
	}
	lease := hex.EncodeToString(nonce[:])
	proposed, e := proposeNormalCIRouteForLease(raw, expectedWorkflow, lease)
	if e != nil {
		return result, e
	}
	raw = nil
	parsed, e := normalCIYAML(proposed)
	if e != nil {
		return result, e
	}
	if _, e = countWindowsCargo(parsed); e != nil {
		return result, e
	}
	if _, found, e := normalci.RouteVariableState(ctx, read); e != nil {
		return result, e
	} else if found {
		return result, errors.New("route switch already exists")
	}
	intent := normalRouteIntent{Todo: 2737, RepoID: normalCIRepo, Lease: lease, OldHead: c.SHA, OldTree: normalRouteTree, OldWorkflow: expectedWorkflow, NewWorkflow: digest(proposed)}
	if e = normalRecord(root, "normal-route-intent.private.json", intent); e != nil {
		return result, e
	}
	mutator := write
	sequence := 0
	var persistenceError error
	write = func(ctx context.Context, method, endpoint string, body, out any) error {
		sequence++
		start := fmt.Sprintf("normal-route-api-%02d-start.private.json", sequence)
		if e := normalRecord(root, start, map[string]any{"method": method, "endpoint": endpoint}); e != nil {
			persistenceError = errors.Join(persistenceError, e)
			return e
		}
		e := mutator(ctx, method, endpoint, body, out)
		save := normalRecord(root, fmt.Sprintf("normal-route-api-%02d-end.private.json", sequence), map[string]any{"method": method, "request_failed": e != nil})
		persistenceError = errors.Join(persistenceError, save)
		return errors.Join(e, save)
	}
	mayExist := true
	defer func() {
		if ret != nil && mayExist {
			clean, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			ce := normalci.RemoveRouteVariable(clean, lease, read, write)
			ret = errors.Join(ret, ce)
			ret = errors.Join(ret, persistenceError, normalRecord(root, "normal-route-failure.private.json", map[string]any{"route_switch_removed": ce == nil, "source_ref_reset": false, "success": false}))
		}
	}()
	if e = normalci.CreateRouteVariable(ctx, lease, read, write); e != nil || persistenceError != nil {
		return result, errors.Join(e, persistenceError)
	}
	if e = normalRecord(root, "normal-route-variable.private.json", map[string]any{"name": normalci.RouteVariable, "value": normalci.LeaseValue(lease), "created": true}); e != nil {
		return result, e
	}
	var blob struct{ SHA string }
	if write(ctx, "POST", "repos/"+normalRouteRepo+"/git/blobs", map[string]any{"content": base64.StdEncoding.EncodeToString(proposed), "encoding": "base64"}, &blob) != nil || blob.SHA != normalBlobID(proposed) {
		return result, errors.New("new route blob unverified")
	}
	var tree struct{ SHA string }
	treeBody := map[string]any{"base_tree": normalRouteTree, "tree": []any{map[string]any{"path": ".github/workflows/ci.yml", "mode": mode, "type": "blob", "sha": blob.SHA}}}
	if write(ctx, "POST", "repos/"+normalRouteRepo+"/git/trees", treeBody, &tree) != nil || !normalci.Revision(tree.SHA) {
		return result, errors.New("new route tree unverified")
	}
	var commit struct{ SHA string }
	if write(ctx, "POST", "repos/"+normalRouteRepo+"/git/commits", map[string]any{"message": "Route finite normal CI to isolated per-job runners", "tree": tree.SHA, "parents": []string{c.SHA}}, &commit) != nil || !normalci.Revision(commit.SHA) {
		return result, errors.New("new route commit unverified")
	}
	var actual struct {
		SHA     string
		Tree    struct{ SHA string }
		Parents []struct{ SHA string }
	}
	if read(ctx, "repos/"+normalRouteRepo+"/git/commits/"+commit.SHA, &actual) != nil || actual.SHA != commit.SHA || actual.Tree.SHA != tree.SHA || len(actual.Parents) != 1 || actual.Parents[0].SHA != c.SHA {
		return result, errors.New("route commit parent rejected")
	}
	var comparison struct {
		Status string
		Files  []struct{ Filename, Status, SHA string }
	}
	if read(ctx, "repos/"+normalRouteRepo+"/compare/"+c.SHA+"..."+commit.SHA, &comparison) != nil || comparison.Status != "ahead" || len(comparison.Files) != 1 || comparison.Files[0].Filename != ".github/workflows/ci.yml" || comparison.Files[0].Status != "modified" || comparison.Files[0].SHA != blob.SHA {
		return result, errors.New("non-route product change rejected")
	}
	result = normalRouteCommit{Head: commit.SHA, Tree: tree.SHA, Workflow: intent.NewWorkflow, Lease: lease}
	if e = normalRecord(root, "normal-route-commit.private.json", result); e != nil {
		return result, e
	}
	if _, e = verifyNormalRouteSource(ctx, c, read); e != nil {
		return result, e
	}
	state, found, e := normalci.RouteVariableState(ctx, read)
	if e != nil || !found || state.Value != normalci.LeaseValue(lease) {
		return result, errors.New("route switch changed before publish")
	}
	if e = normalRecord(root, "normal-route-ref-intent.private.json", map[string]any{"expected_old": c.SHA, "head": commit.SHA, "force": false}); e != nil {
		return result, e
	}
	// A sole old parent plus force=false makes a concurrent advance non-fast-
	// forward, even if it races the immediately preceding expected-head read.
	update := write(ctx, "PATCH", "repos/"+normalRouteRepo+"/git/refs/heads/"+url.PathEscape(normalRouteRef), map[string]any{"sha": commit.SHA, "force": false}, nil)
	if persistenceError != nil {
		return result, persistenceError
	}
	next := candidate{ID: c.ID, PR: c.PR, SHA: commit.SHA}
	if _, e = verifyNormalRouteSource(ctx, next, read); e != nil {
		return result, errors.Join(errors.New("route publish unverified; keep source expectations"), update)
	}
	var content struct{ Encoding, Content string }
	if read(ctx, "repos/"+normalRouteRepo+"/contents/.github/workflows/ci.yml?ref="+commit.SHA, &content) != nil || content.Encoding != "base64" {
		return result, errors.New("published route content unavailable")
	}
	saved, e := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(content.Content), ""))
	if e != nil || !bytes.Equal(saved, proposed) {
		return result, errors.New("published route bytes changed")
	}
	if e = normalRecord(root, "normal-route-published.private.json", result); e != nil {
		return result, e
	}
	mayExist = false // Deliberately retained for the finite run; explicit cleanup.
	return result, nil
}

func releaseNormalRoute(ctx context.Context, root string, read api, write normalMutation) error {
	f, e := os.Open(filepath.Join(root, "normal-route-intent.private.json"))
	if e != nil {
		return errors.New("route recovery intent unavailable")
	}
	defer f.Close()
	raw, e := io.ReadAll(io.LimitReader(f, 8193))
	if e != nil || len(raw) > 8192 {
		return errors.New("route recovery intent exceeds bound")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	var p normalRouteIntent
	if d.Decode(&p) != nil || d.Decode(new(any)) != io.EOF || p.Todo != 2737 || p.RepoID != normalCIRepo || p.OldHead != normalRouteCandidate().SHA || p.OldTree != normalRouteTree || p.OldWorkflow != normalCIWorkflowSHA || !normalci.ValidLease(p.Lease) || len(p.NewWorkflow) != 64 || strings.Trim(p.NewWorkflow, "0123456789abcdef") != "" {
		return errors.New("route recovery intent rejected")
	}
	if e = normalci.RemoveRouteVariable(ctx, p.Lease, read, write); e != nil {
		return e
	}
	return normalRecord(root, fmt.Sprintf("normal-route-released-%s-%d.private.json", p.Lease, time.Now().UnixNano()), map[string]any{"route_variable_absent": true, "product_branch_reset": false})
}
