package main

import (
	"bytes"
	"cc/product-ci/pr148scope"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// Entirely synthetic API source. No product API, archive or local product
// directory is used, including by the opt-in restricted-account fixture.
func syntheticSource() (string, string, map[string]any) {
	api := map[string]any{}
	files := map[string]string{".gitignore": "target/\n", "tools/Cargo.toml": "[package]\nname=\"finite-fixture\"\nversion=\"0.1.0\"\nedition=\"2021\"\n", "tools/Cargo.lock": "version = 4\n\n[[package]]\nname = \"finite-fixture\"\nversion = \"0.1.0\"\n", "tools/src/lib.rs": "#[test] fn cpu_only() { if let Ok(p) = std::env::var(\"PR148_FIXTURE_READY\") { std::fs::write(p, std::process::id().to_string()).unwrap(); std::thread::sleep(std::time::Duration::from_secs(600)); } assert_eq!(2 + 2, 4); }\n", "tools/ignored.bin": "\x00\xff\r\n"}
	var entries []pr148scope.TreeEntry
	for path, body := range files {
		id := objectID("blob", []byte(body))
		entries = append(entries, pr148scope.TreeEntry{Path: path, Mode: "100644", Type: "blob", SHA: id})
		api["repos/"+repository+"/git/blobs/"+id] = map[string]any{"sha": id, "encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte(body)), "size": len(body)}
	}
	var build func(string) string
	build = func(dir string) string {
		children := []pr148scope.TreeEntry{}
		if dir == "" {
			for _, sub := range []string{"tools/src", "tools"} {
				id := build(sub)
				entries = append(entries, pr148scope.TreeEntry{Path: sub, Mode: "040000", Type: "tree", SHA: id})
			}
		}
		for _, x := range entries {
			parent := ""
			if i := strings.LastIndex(x.Path, "/"); i >= 0 {
				parent = x.Path[:i]
			}
			if parent == dir {
				children = append(children, x)
			}
		}
		sort.Slice(children, func(i, j int) bool {
			a, b := filepath.Base(children[i].Path), filepath.Base(children[j].Path)
			if children[i].Type == "tree" {
				a += "/"
			}
			if children[j].Type == "tree" {
				b += "/"
			}
			return a < b
		})
		var raw bytes.Buffer
		for _, x := range children {
			m := x.Mode
			if x.Type == "tree" {
				m = "40000"
			}
			fmt.Fprintf(&raw, "%s %s%c", m, filepath.Base(x.Path), byte(0))
			b, _ := hex.DecodeString(x.SHA)
			raw.Write(b)
		}
		return objectID("tree", raw.Bytes())
	}
	tree := build("")
	body := "tree " + tree + "\nauthor Fixture <fixture@example.invalid> 1792195200 +0000\ncommitter Fixture <fixture@example.invalid> 1792195200 +0000\n\nsynthetic source\n"
	head := objectID("commit", []byte(body))
	api["repos/"+repository+"/git/commits/"+head] = map[string]any{"sha": head, "tree": map[string]string{"sha": tree}, "verification": map[string]string{"payload": body}}
	api["repos/"+repository+"/git/trees/"+tree+"?recursive=1"] = pr148scope.TreeReply{SHA: tree, Entries: entries}
	return head, tree, api
}
func syntheticRead(data map[string]any) readAPI {
	return func(path string, v any) error {
		x, ok := data[path]
		if !ok {
			return fmt.Errorf("unprovided fixture route")
		}
		b, e := json.Marshal(x)
		if e != nil {
			return e
		}
		return json.Unmarshal(b, v)
	}
}
