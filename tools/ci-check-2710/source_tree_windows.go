package main

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type treeNode struct {
	children map[string]*treeNode
	mode     string
	oid      []byte
}

func gitObject(kind string, data []byte) []byte {
	h := sha1.New()
	fmt.Fprintf(h, "%s %d\x00", kind, len(data))
	h.Write(data)
	return h.Sum(nil)
}
func encodeTree(n *treeNode) ([]byte, error) {
	keys := make([]string, 0, len(n.children))
	for k := range n.children {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := keys[i], keys[j]
		if n.children[a].mode == "40000" {
			a += "/"
		}
		if n.children[b].mode == "40000" {
			b += "/"
		}
		return a < b
	})
	var data []byte
	for _, k := range keys {
		x := n.children[k]
		oid := x.oid
		if x.mode == "40000" {
			var e error
			oid, e = encodeTree(x)
			if e != nil {
				return nil, e
			}
		}
		if len(oid) != 20 {
			return nil, errors.New("source object invalid")
		}
		data = append(data, []byte(x.mode+" "+k+"\x00")...)
		data = append(data, oid...)
	}
	return gitObject("tree", data), nil
}

// Independently check actual consumer-visible bytes, without loading Git config,
// running filters/hooks or requiring the test account to perform source intake.
func winSourceTree(v winPlan) error {
	root := &treeNode{children: map[string]*treeNode{}}
	for _, e := range v.Entries {
		parts := strings.Split(e.Name, "/")
		node := root
		for _, p := range parts[:len(parts)-1] {
			if p == "" || p == "." || p == ".." {
				return errors.New("source path invalid")
			}
			if node.children[p] == nil {
				node.children[p] = &treeNode{children: map[string]*treeNode{}, mode: "40000"}
			}
			node = node.children[p]
			if node.mode != "40000" {
				return errors.New("source collision")
			}
		}
		var oid []byte
		if e.Mode == "160000" {
			var err error
			oid, err = hex.DecodeString(e.SHA)
			if err != nil {
				return err
			}
		} else {
			p := filepath.Join(v.Root, "source", filepath.FromSlash(e.Name))
			info, err := os.Lstat(p)
			if err != nil || !info.Mode().IsRegular() {
				return errors.New("source file unavailable")
			}
			b, err := os.ReadFile(p)
			if err != nil {
				return errors.New("source file unreadable")
			}
			oid = gitObject("blob", b)
		}
		node.children[parts[len(parts)-1]] = &treeNode{mode: e.Mode, oid: oid}
	}
	oid, e := encodeTree(root)
	if e != nil {
		return e
	}
	if hex.EncodeToString(oid) != v.Tree {
		return errors.New("consumer source tree mismatch")
	}
	return nil
}
