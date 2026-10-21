package sourcepacket

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func disabledSparseCheckout(t *testing.T) (Packet, string) {
	t.Helper()
	p, root := fixture(t)
	dest := filepath.Join(root, "checkout")
	if _, err := Materialize(context.Background(), p, p.Pin, dest); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyCheckout(context.Background(), p.Pin, p.TreeEntries, dest); err != nil {
		t.Fatal("positive checkout proof failed")
	}
	// These are the checkout action's actual Git operations. Do not manufacture
	// a manifest, replace source bytes or enable a broader metadata policy.
	run(t, dest, nil, "sparse-checkout", "disable")
	run(t, dest, nil, "config", "--local", "--unset-all", "extensions.worktreeConfig")
	b, err := os.ReadFile(filepath.Join(dest, ".git", "config.worktree"))
	if err != nil || len(b) != 83 || digest(b) != "443a5f645c23c3d0c0aa09f634b2ad111d46ef61946b598a2fb311678ab47454" {
		t.Fatal("Git did not produce the observed disabled sparse residue")
	}
	return p, dest
}

func TestCheckoutAllowsGitDisabledSparseResidue(t *testing.T) {
	p, dest := disabledSparseCheckout(t)
	proof, err := VerifyCheckout(context.Background(), p.Pin, p.TreeEntries, dest)
	if err != nil || !proof.PhysicalBlobsVerified || proof.HeadSHA != p.Pin.HeadSHA || proof.TreeSHA != p.Pin.TreeSHA {
		t.Fatalf("inert checkout residue rejected: %v", err)
	}
}

func TestCheckoutDisabledSparseResidueDoesNotAdmitOtherConfiguration(t *testing.T) {
	for _, mode := range []string{"active", "changed", "extra", "empty", "hardlink", "directory", "symlink"} {
		t.Run(mode, func(t *testing.T) {
			if mode == "symlink" && runtime.GOOS != "linux" {
				t.Skip("real Linux symlink; Windows reparse coverage is separate")
			}
			p, dest := disabledSparseCheckout(t)
			file := filepath.Join(dest, ".git", "config.worktree")
			b, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "active":
				run(t, dest, nil, "config", "extensions.worktreeConfig", "true")
			case "changed":
				b = []byte(strings.Replace(string(b), "false", "true", 1))
			case "extra":
				b = append(b, []byte("[http \"https://github.com/\"]\n\textraheader = synthetic-marker\n")...)
			case "empty":
				b = nil
			case "hardlink":
				if err = os.Link(file, filepath.Join(filepath.Dir(dest), "external-identical-config")); err != nil {
					t.Fatal(err)
				}
			case "directory", "symlink":
				if err = os.Remove(file); err != nil {
					t.Fatal(err)
				}
				if mode == "directory" {
					err = os.Mkdir(file, 0700)
				} else {
					external := filepath.Join(filepath.Dir(dest), "external-identical-config")
					if err = os.WriteFile(external, b, 0600); err != nil {
						t.Fatal(err)
					}
					err = os.Symlink(external, file)
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			if mode == "changed" || mode == "extra" || mode == "empty" {
				if err = os.WriteFile(file, b, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if _, err = VerifyCheckout(context.Background(), p.Pin, p.TreeEntries, dest); err == nil {
				t.Fatal("active, altered or externally shared configuration admitted")
			}
		})
	}
}
