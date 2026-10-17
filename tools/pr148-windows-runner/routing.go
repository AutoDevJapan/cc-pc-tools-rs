package main

import (
	"bytes"
	"cc/product-ci/pr148scope"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

type readAPI func(string, any) error

func blobID(b []byte) string {
	h := sha1.New()
	fmt.Fprintf(h, "blob %d\x00", len(b))
	h.Write(b)
	return hex.EncodeToString(h.Sum(nil))
}

func workflowBytes(read readAPI, path, head string) ([]byte, error) {
	var v struct{ Type, Encoding, Content, SHA string }
	if e := read("repos/"+repository+"/contents/"+path+"?ref="+head, &v); e != nil {
		return nil, e
	}
	b, e := base64.StdEncoding.DecodeString(strings.ReplaceAll(v.Content, "\n", ""))
	if e != nil || v.Type != "file" || v.Encoding != "base64" || len(b) > 1<<20 || blobID(b) != v.SHA {
		return nil, errors.New("workflow blob bytes rejected")
	}
	return b, nil
}

// Both OS helpers use the SAME canonical transformation and complete tree
// comparator. A parent-supplied digest alone cannot authorize changed commands.
func verifyRouting(p Plan, read readAPI) error {
	return verifyRoutingUsing(p, read, pr148scope.Workflows())
}
func verifyRoutingUsing(p Plan, read readAPI, workflows []pr148scope.Workflow) error {
	if p.Fixture != "" || p.validate() != nil {
		return errors.New("real routing-only plan required")
	}
	var old, next struct {
		SHA     string
		Tree    struct{ SHA string }
		Parents []struct{ SHA string }
	}
	if e := read("repos/"+repository+"/git/commits/"+sourceHead, &old); e != nil {
		return e
	}
	if e := read("repos/"+repository+"/git/commits/"+p.Head, &next); e != nil {
		return e
	}
	if old.SHA != sourceHead || old.Tree.SHA != sourceTree || next.SHA != p.Head || next.Tree.SHA != p.Tree || len(next.Parents) != 1 || next.Parents[0].SHA != sourceHead {
		return errors.New("source commit/tree or sole original parent changed")
	}
	expected := map[string]string{}
	for _, w := range workflows {
		b, e := workflowBytes(read, w.Path, sourceHead)
		if e != nil {
			return e
		}
		want, e := pr148scope.Propose(b, w)
		if e != nil {
			return e
		}
		got, e := workflowBytes(read, w.Path, p.Head)
		if e != nil {
			return e
		}
		if !bytes.Equal(want, got) {
			return errors.New("workflow differs from the canonical finite route")
		}
		if w.Path == p.Workflow && pr148scope.Digest(got) != p.WorkflowDigest {
			return errors.New("explicit job workflow digest changed")
		}
		expected[w.Path] = blobID(got)
	}
	var a, b pr148scope.TreeReply
	if e := read("repos/"+repository+"/git/trees/"+sourceTree+"?recursive=1", &a); e != nil {
		return e
	}
	if e := read("repos/"+repository+"/git/trees/"+p.Tree+"?recursive=1", &b); e != nil {
		return e
	}
	if b.SHA != p.Tree {
		return errors.New("explicit routing tree identity changed")
	}
	return pr148scope.CheckTreeDelta(a, b, expected)
}
