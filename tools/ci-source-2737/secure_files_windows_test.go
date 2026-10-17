package sourcepacket

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestCheckoutRejectsWindowsParentAndMetadataJunction(t *testing.T) {
	for _, part := range []string{".github", ".git"} {
		t.Run(part, func(t *testing.T) {
			p, root := fixture(t)
			dest := filepath.Join(root, "guest")
			if _, e := Materialize(context.Background(), p, p.Pin, dest); e != nil {
				t.Fatal(e)
			}
			if _, e := VerifyCheckout(context.Background(), p.Pin, p.TreeEntries, dest); e != nil {
				t.Fatal("positive proof rejected")
			}
			link := filepath.Join(dest, part)
			target := filepath.Join(root, "external-identical")
			if os.Rename(link, target) != nil {
				t.Fatal("move fixture")
			}
			cmd := exec.Command(filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe"), "/d", "/c", "mklink", "/J", link, target)
			configureProcess(cmd)
			cmd.Stdout = io.Discard
			cmd.Stderr = io.Discard
			if cmd.Run() != nil {
				t.Fatal("junction fixture unavailable")
			}
			if _, e := VerifyCheckout(context.Background(), p.Pin, p.TreeEntries, dest); e == nil {
				t.Fatal("byte-identical external junction accepted")
			}
		})
	}
}
func TestWindowsAncestorHandlePreventsReplacement(t *testing.T) {
	root := t.TempDir()
	if os.Mkdir(filepath.Join(root, "dir"), 0700) != nil || os.WriteFile(filepath.Join(root, "dir", "file"), []byte("same"), 0600) != nil {
		t.Fatal("fixture")
	}
	fs, e := secureRoot(root)
	if e != nil {
		t.Fatal(e)
	}
	defer fs.Close()
	if _, _, e = fs.read(filepath.Join("dir", "file"), 32); e != nil {
		t.Fatal("positive pinned read failed")
	}
	if os.Rename(filepath.Join(root, "dir"), filepath.Join(root, "moved")) == nil {
		t.Fatal("held parent was replaceable")
	}
	if fs.revalidate() != nil {
		t.Fatal("stable pinned source failed after refused swap")
	}
}
