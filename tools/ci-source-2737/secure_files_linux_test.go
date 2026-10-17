package sourcepacket

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCheckoutRejectsExternalIdenticalParentAndRoot(t *testing.T) {
	for _, part := range []string{"parent", "root", "ancestor"} {
		t.Run(part, func(t *testing.T) {
			p, root := fixture(t)
			dest := filepath.Join(root, "guest")
			if _, e := Materialize(context.Background(), p, p.Pin, dest); e != nil {
				t.Fatal(e)
			}
			if _, e := VerifyCheckout(context.Background(), p.Pin, p.TreeEntries, dest); e != nil {
				t.Fatal("positive proof rejected")
			}
			from, to := filepath.Join(dest, ".github"), filepath.Join(root, "external-identical")
			if part == "root" {
				from = dest
			}
			if part == "ancestor" {
				from = root
				to = root + "-relocated"
				t.Cleanup(func() { _ = os.Remove(root); _ = os.Rename(to, root) })
			}
			if os.Rename(from, to) != nil || os.Symlink(to, from) != nil {
				t.Fatal("attack fixture failed")
			}
			// The final workflow file stays regular with byte-identical content, as in
			// the original Must. Refusal must come from the parent/root link boundary.
			if _, e := VerifyCheckout(context.Background(), p.Pin, p.TreeEntries, dest); e == nil {
				t.Fatal("byte-identical linked parent accepted")
			}
		})
	}
}
func TestSecureHandlesRejectParentReplacementDuringProof(t *testing.T) {
	root := t.TempDir()
	if os.Mkdir(filepath.Join(root, "dir"), 0700) != nil || os.WriteFile(filepath.Join(root, "dir", "file"), []byte("original"), 0600) != nil {
		t.Fatal("fixture")
	}
	fs, e := secureRoot(root)
	if e != nil {
		t.Fatal(e)
	}
	defer fs.Close()
	if _, _, e = fs.read(filepath.Join("dir", "file"), 64); e != nil {
		t.Fatal("positive read")
	}
	if os.Rename(filepath.Join(root, "dir"), filepath.Join(root, "moved")) != nil || os.Symlink(filepath.Join(root, "moved"), filepath.Join(root, "dir")) != nil {
		t.Fatal("swap fixture")
	}
	// Deterministic swap BETWEEN read and final path validation, without sleeps.
	if fs.revalidate() == nil {
		t.Fatal("path replacement during proof accepted")
	}
}
