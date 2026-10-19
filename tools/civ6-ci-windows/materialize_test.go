package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMaterializeExactSyntheticGitSource(t *testing.T) {
	h, tr, data := syntheticSource()
	s, e := fetchSource(h, tr, syntheticRead(data))
	if e != nil {
		t.Fatal(e)
	}
	dest := filepath.Join(t.TempDir(), "source")
	if e = writeSource(dest, s); e != nil {
		t.Fatal(e)
	}
	for expr, want := range map[string]string{"HEAD": h, "HEAD^{tree}": tr} {
		b, e := sourceGit(dest, "rev-parse", "--verify", expr)
		if e != nil || strings.TrimSpace(string(b)) != want {
			t.Fatal(expr, e, string(b))
		}
	}
	if e = verifyTrackedBytes(dest, tr); e != nil {
		t.Fatal(e)
	}
	if e = writeSource(dest, s); e == nil {
		t.Fatal("overwrote source")
	}
	if _, e = sourceGit(dest, "update-index", "--assume-unchanged", "tools/src/lib.rs"); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(dest, "tools/src/lib.rs"), []byte("changed"), 0600); e != nil {
		t.Fatal(e)
	}
	if verifyTrackedBytes(dest, tr) == nil {
		t.Fatal("assume-unchanged hid altered tracked bytes")
	}
}

func TestSourceRejectsIncompleteAndAlteredAPI(t *testing.T) {
	for _, which := range []string{"commit", "truncated", "tree", "mode", "path", "collision", "blob", "missing"} {
		t.Run(which, func(t *testing.T) {
			h, tr, data := syntheticSource()
			key := "repos/" + repository + "/git/trees/" + tr + "?recursive=1"
			tree := data[key].(TreeReply)
			switch which {
			case "commit":
				data["repos/"+repository+"/git/commits/"+h] = map[string]any{}
			case "truncated":
				tree.Truncated = true
			case "tree":
				tree.Entries = tree.Entries[1:]
			case "mode":
				tree.Entries[0].Mode = "120000"
			case "path":
				tree.Entries[0].Path = ".git/config"
			case "collision":
				x := tree.Entries[0]
				x.Path = strings.ToUpper(x.Path)
				tree.Entries = append(tree.Entries, x)
			case "blob":
				for k := range data {
					if strings.Contains(k, "/git/blobs/") {
						data[k] = map[string]any{}
					}
				}
			case "missing":
				delete(data, key)
			}
			if which != "missing" {
				data[key] = tree
			}
			if _, e := fetchSource(h, tr, syntheticRead(data)); e == nil {
				t.Fatal("accepted corrupt source")
			}
		})
	}
}
func TestWindowsSourcePathRejectsAliasesAndDevices(t *testing.T) {
	for _, s := range []string{"../x", "/x", "a//x", "a/../x", ".GIT/config", "git~1/config", "a:x", "a/NUL.txt", "a/COM1", "a/x.", "a/x ", "a\\x"} {
		if sourcePathOK(s) {
			t.Fatal("accepted", s)
		}
	}
}

func TestSourceMetadataAcceptsOnlyInertCheckoutWorktreeConfig(t *testing.T) {
	h, tree, data := syntheticSource()
	s, e := fetchSource(h, tree, syntheticRead(data))
	if e != nil {
		t.Fatal(e)
	}
	dest := filepath.Join(t.TempDir(), "source")
	if e = writeSource(dest, s); e != nil {
		t.Fatal(e)
	}
	for _, raw := range []string{"[core]\n\tsparseCheckout = false\n\tsparseCheckoutCone = false\n", "[core]\n\tsparseCheckout = false\n\tsparseCheckoutCone = false\n[index]\n\tsparse = false\n", "", "[credential]\nhelper = unsafe\n", "[core]\nsparseCheckout = true\n", "[index]\nsparse = true\n", "[include]\npath = other\n"} {
		if e = os.WriteFile(filepath.Join(dest, ".git", "config.worktree"), []byte(raw), 0600); e != nil {
			t.Fatal(e)
		}
		err := verifyTrackedBytes(dest, tree)
		want := raw == "" || strings.Contains(raw, "sparseCheckout = false")
		if (err == nil) != want {
			t.Fatal("wrong inert-file acceptance", err)
		}
	}
	if !blockedGitKey("extensions.worktreeconfig") {
		t.Fatal("active worktree config allowed")
	}
}
