//go:build windows

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestOwnedCargoTwoLinksSameObjectHeldBeforeConsume(t *testing.T) {
	root := physicalSoftwareFixture(t)
	a := filepath.Join(root, "out.exe")
	b := filepath.Join(root, "dependency.exe")
	if e := os.WriteFile(a, []byte("owned compiled fixture"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.Link(a, b); e != nil {
		t.Fatal(e)
	}
	calls := 0
	if !withPinnedCargoOutput(a, b, softwareIdentity{}, func(raw []byte, id softwareIdentity) bool {
		calls++
		if !bytes.Equal(raw, []byte("owned compiled fixture")) || id.FileID == "" {
			t.Fatal("wrong pinned object")
		}
		for _, p := range []string{a, b} {
			if os.WriteFile(p, []byte("mutate"), 0600) == nil {
				t.Fatal("alias writer not denied")
			}
			if os.Rename(p, p+".moved") == nil {
				t.Fatal("alias rename not denied")
			}
		}
		return true
	}) || calls != 1 {
		t.Fatal("owned same producer output rejected")
	}
	if e := os.WriteFile(a, []byte("after release"), 0600); e != nil {
		t.Fatal("lease leaked")
	}
}

func TestCargoLinkWrongIdentityExtraAliasAndMissingNeverRead(t *testing.T) {
	for _, fault := range []string{"wrong_id", "extra", "missing", "different"} {
		t.Run(fault, func(t *testing.T) {
			root := physicalSoftwareFixture(t)
			a := filepath.Join(root, "out.exe")
			b := filepath.Join(root, "alias.exe")
			if e := os.WriteFile(a, []byte("owned"), 0600); e != nil {
				t.Fatal(e)
			}
			if e := os.Link(a, b); e != nil {
				t.Fatal(e)
			}
			expected := softwareIdentity{}
			switch fault {
			case "wrong_id":
				expected = softwareIdentity{Volume: "1", FileID: "other"}
			case "extra":
				if e := os.Link(a, a+".third"); e != nil {
					t.Fatal(e)
				}
			case "missing":
				b += "missing"
			case "different":
				if e := os.Remove(b); e != nil {
					t.Fatal(e)
				}
				if e := os.WriteFile(b, []byte("other"), 0600); e != nil {
					t.Fatal(e)
				}
			}
			if withPinnedCargoOutput(a, b, expected, func([]byte, softwareIdentity) bool { t.Fatal("invalid build output consumed"); return true }) {
				t.Fatal("invalid link set admitted")
			}
		})
	}
}

func TestAskArtifactCopyCreateNewAndSameHeldReadback(t *testing.T) {
	root := physicalSoftwareFixture(t)
	path := filepath.Join(root, "candidate.exe")
	raw := []byte("owned software artifact")
	id, e := createAskArtifactCopy(path, raw)
	if e != nil || id.FileID == "" {
		t.Fatal(e, id)
	}
	if _, e = createAskArtifactCopy(path, []byte("overwrite")); e == nil {
		t.Fatal("existing candidate changed")
	}
	got, e := os.ReadFile(path)
	if e != nil || !bytes.Equal(got, raw) {
		t.Fatal("readback mismatch")
	}
	var out bytes.Buffer
	if runAskBuildArtifact([]string{"--export-ask-cargo-output", "other"}, &out) != 2 || out.Len() != 0 {
		t.Fatal("caller changed fixed purpose")
	}
}
