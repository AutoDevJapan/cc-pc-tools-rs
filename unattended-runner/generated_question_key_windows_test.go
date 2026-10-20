//go:build windows

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestGeneratedQuestionWindowsPurposeKeyRequiresValidUserProtection(t *testing.T) {
	plain := bytes.Repeat([]byte{0x45}, 32)
	sealed, err := generatedQuestionProtect(plain)
	if err != nil || bytes.Equal(sealed, plain) {
		t.Fatal("purpose key was not encrypted", err)
	}
	opened, err := generatedQuestionUnprotect(sealed)
	if err != nil || !bytes.Equal(opened, plain) {
		t.Fatal("purpose key cannot round trip under current user", err)
	}
	eraseGeneratedKey(opened)
	sealed[len(sealed)/2] ^= 0x80
	if _, err = generatedQuestionUnprotect(sealed); err == nil {
		t.Fatal("tampered user-bound key accepted")
	}
}

func TestGeneratedQuestionRustVerbatimPathUsesSameOriginalReceipt(t *testing.T) {
	s, image := generatedFixture(t)
	r := generatedRegistryFixture(t, s)
	manifest := filepath.Join(filepath.Dir(s.Proof), "rust-path-evidence.json")
	writeGeneratedBundle(t, manifest, generatedBundle(s))
	if _, err := r.register(context.Background(), manifest); err != nil {
		t.Fatal(err)
	}
	verbatim := `\\?\` + image
	proof, err := qualifyGeneratedQuestionImage(context.Background(), verbatim, s.Scope, generatedQuestionSpec{Load: r.load})
	if err != nil || !proof.Qualified || proof.SHA256 != s.Images[0].SHA256 {
		t.Fatal("Rust canonical source did not find its original receipt", err)
	}
	if _, err = qualifyGeneratedQuestionImage(context.Background(), `\\?\Volume{not-a-drive}\image.png`, s.Scope, generatedQuestionSpec{Load: r.load}); err == nil {
		t.Fatal("unknown namespace manufactured qualification")
	}
}

// Opt-in acceptance uses actual native evidence with an isolated bookkeeping
// store. Images remain at their original paths and no request is sent.
func TestGeneratedQuestionActualNativeProofRoundTripsWithoutSourceCopy(t *testing.T) {
	evidence := os.Getenv("CC_GENERATED_QUESTION_EVIDENCE")
	if evidence == "" {
		t.Skip("no live native evidence supplied")
	}
	r := generatedQuestionRegistryProduction()
	ctx := context.Background()
	b, err := r.check(ctx, evidence)
	if err != nil {
		t.Fatal("actual native evidence check failed", err)
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r.Home = root
	if count, err := r.register(ctx, evidence); err != nil || count != len(b.Images) {
		t.Fatal("actual evidence registration failed", err)
	}
	for _, image := range b.Images {
		for _, path := range []string{image.Path, `\\?\` + filepath.FromSlash(image.Path)} {
			proof, err := qualifyGeneratedQuestionImage(ctx, path, b.Scope, generatedQuestionSpec{Load: r.load})
			if err != nil || !proof.Qualified || proof.SHA256 != image.SHA256 || proof.Size != image.Size {
				t.Fatal("actual registered native source did not round trip", err)
			}
		}
	}
	t.Logf("%d actual native images registered and consumed with ordinary and Rust canonical paths", len(b.Images))
}
