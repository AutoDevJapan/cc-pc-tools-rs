package main

import (
	"bytes"
	"cc/product-ci/normalci"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type routeFixture struct {
	head               string
	variable           *normalci.Variable
	original, proposed []byte
	writes             []string
	fault              string
	wrongForce         bool
	wrongTree          bool
}

func newRouteFixture(fault string) *routeFixture {
	return &routeFixture{head: normalRouteCandidate().SHA, original: normalCIFixture(), fault: fault}
}
func routeInto(value, out any) error {
	b, e := json.Marshal(value)
	if e != nil {
		return e
	}
	return json.Unmarshal(b, out)
}
func (s *routeFixture) read(_ context.Context, key string, out any) error {
	repo := map[string]any{"id": normalCIRepo, "full_name": normalRouteRepo, "private": true, "fork": false, "owner": map[string]string{"login": "AutoDevJapan"}, "permissions": map[string]bool{"admin": true}}
	old := normalRouteCandidate().SHA
	next := strings.Repeat("d", 40)
	tree := strings.Repeat("e", 40)
	prefix := "repos/" + normalRouteRepo
	switch {
	case key == "repositories/1154066087":
		return routeInto(repo, out)
	case key == prefix+"/pulls/3613":
		ref := normalRouteRef
		if s.fault == "wrong-ref" {
			ref = "another-ref"
		}
		return routeInto(map[string]any{"state": "open", "head": map[string]any{"sha": s.head, "ref": ref, "repo": repo}, "base": map[string]any{"repo": repo}}, out)
	case strings.HasPrefix(key, prefix+"/git/ref/heads/"):
		return routeInto(map[string]any{"object": map[string]string{"sha": s.head}}, out)
	case key == prefix+"/git/commits/"+old:
		return routeInto(map[string]any{"sha": old, "tree": map[string]string{"sha": normalRouteTree}}, out)
	case key == prefix+"/git/commits/"+next:
		return routeInto(map[string]any{"sha": next, "tree": map[string]string{"sha": tree}, "parents": []any{map[string]string{"sha": old}}}, out)
	case key == prefix+"/git/trees/"+normalRouteTree+"?recursive=1":
		return routeInto(map[string]any{"sha": normalRouteTree, "truncated": false, "tree": []any{map[string]string{"path": ".github/workflows/ci.yml", "mode": "100644", "type": "blob", "sha": normalBlobID(s.original)}}}, out)
	case strings.HasPrefix(key, prefix+"/contents/.github/workflows/ci.yml?ref="):
		b := s.original
		if strings.HasSuffix(key, next) {
			b = s.proposed
		}
		return routeInto(map[string]string{"encoding": "base64", "content": base64.StdEncoding.EncodeToString(b)}, out)
	case strings.HasPrefix(key, prefix+"/actions/variables?"):
		items := []normalci.Variable{}
		if s.variable != nil {
			items = append(items, *s.variable)
		}
		return routeInto(map[string]any{"total_count": len(items), "variables": items}, out)
	case strings.HasPrefix(key, prefix+"/compare/"):
		files := []any{map[string]string{"filename": ".github/workflows/ci.yml", "status": "modified", "sha": normalBlobID(s.proposed)}}
		if s.fault == "extra-file" {
			files = append(files, map[string]string{"filename": "unexpected", "status": "added"})
		}
		return routeInto(map[string]any{"status": "ahead", "files": files}, out)
	}
	return errors.New("unexpected fixed-route read")
}
func (s *routeFixture) write(_ context.Context, method, endpoint string, body, out any) error {
	s.writes = append(s.writes, method+" "+endpoint)
	prefix := "repos/" + normalRouteRepo
	switch {
	case method == "POST" && endpoint == prefix+"/actions/variables":
		v, ok := body.(normalci.Variable)
		if !ok {
			return errors.New("variable payload")
		}
		s.variable = &v
		if s.fault == "lost-create" {
			return errors.New("lost POST reply")
		}
		return nil
	case method == "DELETE" && endpoint == prefix+"/actions/variables/"+normalci.RouteVariable:
		s.variable = nil
		return nil
	case method == "POST" && endpoint == prefix+"/git/blobs":
		m, ok := body.(map[string]any)
		if !ok {
			return errors.New("blob body")
		}
		encoded, ok := m["content"].(string)
		if !ok {
			return errors.New("blob encoding")
		}
		b, e := base64.StdEncoding.DecodeString(encoded)
		if e != nil {
			return e
		}
		s.proposed = b
		return routeInto(map[string]string{"sha": normalBlobID(b)}, out)
	case method == "POST" && endpoint == prefix+"/git/trees":
		m, ok := body.(map[string]any)
		if !ok {
			return errors.New("tree body")
		}
		entries, ok := m["tree"].([]any)
		if !ok || len(entries) != 1 {
			return errors.New("tree entries")
		}
		entry, ok := entries[0].(map[string]any)
		if !ok {
			return errors.New("tree entry")
		}
		if m["base_tree"] != normalRouteTree || entry["path"] != ".github/workflows/ci.yml" || entry["mode"] != "100644" {
			s.wrongTree = true
			return errors.New("unexpected tree mutation")
		}
		return routeInto(map[string]string{"sha": strings.Repeat("e", 40)}, out)
	case method == "POST" && endpoint == prefix+"/git/commits":
		m, ok := body.(map[string]any)
		if !ok {
			return errors.New("commit body")
		}
		parents, ok := m["parents"].([]string)
		if !ok || len(parents) != 1 || parents[0] != normalRouteCandidate().SHA {
			return errors.New("commit parent")
		}
		return routeInto(map[string]string{"sha": strings.Repeat("d", 40)}, out)
	case method == "PATCH" && strings.HasPrefix(endpoint, prefix+"/git/refs/heads/"):
		m, ok := body.(map[string]any)
		if !ok {
			return errors.New("ref body")
		}
		if m["force"] != false {
			s.wrongForce = true
			return errors.New("force update forbidden")
		}
		if s.fault == "head-race" {
			s.head = strings.Repeat("f", 40)
			return errors.New("non-fast-forward")
		}
		s.head = strings.Repeat("d", 40)
		if s.fault == "lost-patch" {
			return errors.New("lost PATCH reply")
		}
		return nil
	}
	return errors.New("unapproved route mutation")
}
func TestNormalRoutePublishesOnlyFourScalarsAndReconcilesLostReplies(t *testing.T) {
	for _, fault := range []string{"", "lost-create", "lost-patch"} {
		t.Run(fault, func(t *testing.T) {
			root := t.TempDir()
			s := newRouteFixture(fault)
			result, e := applyNormalRouteForWorkflow(context.Background(), root, digest(s.original), s.read, s.write)
			if e != nil || result.Head != s.head || s.variable == nil || s.variable.Value != normalci.LeaseValue(result.Lease) || s.wrongForce || s.wrongTree {
				t.Fatalf("route apply failed: %v", e)
			}
			restored := append([]byte(nil), s.proposed...)
			for _, r := range normalCIRoutes() {
				expr, e := normalci.RunnerExpressionForLease(r, result.Lease)
				if e != nil {
					t.Fatal(e)
				}
				restored = bytes.ReplaceAll(restored, []byte(expr), []byte(r.Previous))
			}
			if !bytes.Equal(restored, s.original) {
				t.Fatal("non-runner bytes changed")
			}
			if _, e = os.Stat(filepath.Join(root, "normal-route-published.private.json")); e != nil {
				t.Fatal("published receipt absent")
			}
			entries, e := os.ReadDir(root)
			if e != nil {
				t.Fatal(e)
			}
			failedReplies := 0
			for _, entry := range entries {
				b, e := os.ReadFile(filepath.Join(root, entry.Name()))
				if e != nil {
					t.Fatal(e)
				}
				if bytes.Contains(b, []byte("cargo test --locked")) {
					t.Fatal("workflow source persisted on host")
				}
				if bytes.Contains(b, []byte(`"request_failed": true`)) {
					failedReplies++
				}
			}
			if fault != "" && failedReplies != 1 {
				t.Fatal("initial lost reply was not preserved")
			}
		})
	}
}
func TestNormalRouteRejectsOccupiedFlagExtraSourceAndConcurrentHead(t *testing.T) {
	for _, fault := range []string{"occupied", "extra-file", "head-race", "wrong-ref"} {
		t.Run(fault, func(t *testing.T) {
			s := newRouteFixture(fault)
			if fault == "occupied" {
				s.variable = &normalci.Variable{Name: normalci.RouteVariable, Value: "another-owner"}
			}
			_, e := applyNormalRouteForWorkflow(context.Background(), t.TempDir(), digest(s.original), s.read, s.write)
			if e == nil || s.wrongForce {
				t.Fatal("unsafe route accepted")
			}
			if fault == "occupied" {
				if len(s.writes) != 0 || s.variable.Value != "another-owner" {
					t.Fatal("existing flag changed")
				}
			} else if s.variable != nil {
				t.Fatal("own flag not recovered after failure")
			}
			if fault == "wrong-ref" && len(s.writes) != 0 {
				t.Fatal("another PR ref was mutated")
			}
			if fault == "head-race" && s.head != strings.Repeat("f", 40) {
				t.Fatal("concurrent source overwritten")
			}
		})
	}
}

func TestRouteReleaseUsesRecordedNonceWithoutResettingAdvancedSource(t *testing.T) {
	root := t.TempDir()
	s := newRouteFixture("")
	s.head = strings.Repeat("f", 40)
	lease := strings.Repeat("a", 32)
	s.variable = &normalci.Variable{Name: normalci.RouteVariable, Value: normalci.LeaseValue(lease)}
	intent := normalRouteIntent{Todo: 2737, RepoID: normalCIRepo, Lease: lease, OldHead: normalRouteCandidate().SHA, OldTree: normalRouteTree, OldWorkflow: normalCIWorkflowSHA, NewWorkflow: strings.Repeat("a", 64)}
	if e := normalRecord(root, "normal-route-intent.private.json", intent); e != nil {
		t.Fatal(e)
	}
	for n := 0; n < 2; n++ {
		if e := releaseNormalRoute(context.Background(), root, s.read, s.write); e != nil {
			t.Fatal(e)
		}
	}
	if s.variable != nil || s.head != strings.Repeat("f", 40) || len(s.writes) != 1 || !strings.HasPrefix(s.writes[0], "DELETE ") {
		t.Fatal("cleanup changed source or repeated a deletion")
	}
}

func TestMalformedRouteIntentCannotDeleteSwitch(t *testing.T) {
	root := t.TempDir()
	s := newRouteFixture("")
	lease := strings.Repeat("a", 32)
	s.variable = &normalci.Variable{Name: normalci.RouteVariable, Value: normalci.LeaseValue(lease)}
	p := normalRouteIntent{Todo: 2737, RepoID: normalCIRepo, Lease: lease, OldHead: normalRouteCandidate().SHA, OldTree: normalRouteTree, OldWorkflow: normalCIWorkflowSHA}
	if normalRecord(root, "normal-route-intent.private.json", p) != nil {
		t.Fatal("fixture receipt failed")
	}
	if releaseNormalRoute(context.Background(), root, s.read, s.write) == nil || len(s.writes) != 0 || s.variable == nil {
		t.Fatal("malformed recovery intent mutated a switch")
	}
}
