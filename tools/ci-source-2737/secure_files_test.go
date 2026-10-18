package sourcepacket

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSecureReadUsesAnchoredBytesAndRejectsHardlinks(t *testing.T) {
	root := t.TempDir()
	if os.Mkdir(filepath.Join(root, "dir"), 0700) != nil {
		t.Fatal("mkdir")
	}
	file := filepath.Join(root, "dir", "file")
	if os.WriteFile(file, []byte("same"), 0600) != nil {
		t.Fatal("write")
	}
	fs, e := secureRoot(root)
	if e != nil {
		t.Fatal("secure root")
	}
	b, _, e := fs.read(filepath.Join("dir", "file"), 64)
	if e != nil || string(b) != "same" {
		t.Fatal("positive no-follow read rejected")
	}
	if fs.revalidate() != nil {
		t.Fatal("stable path rejected")
	}
	fs.Close()
	if e = os.Link(file, filepath.Join(root, "external-alias")); e != nil {
		t.Fatal("hardlink fixture unavailable")
	}
	fs, e = secureRoot(root)
	if e != nil {
		t.Fatal(e)
	}
	defer fs.Close()
	if _, _, e = fs.read(filepath.Join("dir", "file"), 64); e == nil {
		t.Fatal("shared external physical file accepted")
	}
}

func TestCheckoutRejectsLinkedMetadata(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("link attack runs on actual Linux; Windows reparse fixture separate")
	}
	p, root := fixture(t)
	dest := filepath.Join(root, "guest")
	if _, e := Materialize(context.Background(), p, p.Pin, dest); e != nil {
		t.Fatal(e)
	}
	if _, e := VerifyCheckout(context.Background(), p.Pin, p.TreeEntries, dest); e != nil {
		t.Fatal("positive checkout rejected")
	}
	external := filepath.Join(root, "external-git")
	if os.Rename(filepath.Join(dest, ".git"), external) != nil {
		t.Fatal("move fixture")
	}
	if os.Symlink(external, filepath.Join(dest, ".git")) != nil {
		t.Fatal("link fixture")
	}
	if _, e := VerifyCheckout(context.Background(), p.Pin, p.TreeEntries, dest); e == nil {
		t.Fatal("external linked git metadata accepted")
	}
}

func TestCheckoutRejectsSecondaryWorktreeConfiguration(t *testing.T) {
	p, root := fixture(t)
	dest := filepath.Join(root, "guest")
	if _, e := Materialize(context.Background(), p, p.Pin, dest); e != nil {
		t.Fatal(e)
	}
	if _, e := VerifyCheckout(context.Background(), p.Pin, p.TreeEntries, dest); e != nil {
		t.Fatal("positive proof rejected")
	}
	run(t, dest, nil, "config", "extensions.worktreeConfig", "true")
	if os.WriteFile(filepath.Join(dest, ".git", "config.worktree"), []byte("[http \"https://github.com/\"]\n\textraheader = synthetic-inert-marker\n"), 0600) != nil {
		t.Fatal("metadata fixture failed")
	}
	if _, e := VerifyCheckout(context.Background(), p.Pin, p.TreeEntries, dest); e == nil {
		t.Fatal("secondary checkout authentication accepted")
	}
}
