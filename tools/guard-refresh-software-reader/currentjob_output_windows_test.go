//go:build windows

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCurrentJobOutputUsesSameTwoAliasObjectBeforeExport(t *testing.T) {
	for _, mode := range []string{"normal", "wrong_generation", "wrong_created", "wrong_size", "extra_alias", "wrong_source", "collision"} {
		t.Run(mode, func(t *testing.T) {
			root := physicalSoftwareFixture(t)
			a, b, dest := filepath.Join(root, "out.exe"), filepath.Join(root, "alias.exe"), filepath.Join(root, "export.exe")
			if os.WriteFile(a, []byte("owned compiled software"), 0600) != nil || os.Link(a, b) != nil {
				t.Fatal("fixture")
			}
			expected := softwareIdentity{}
			if mode == "wrong_created" {
				if !withPinnedCargoOutput(a, b, softwareIdentity{}, func(_ []byte, id softwareIdentity) bool { expected = id; return true }) {
					t.Fatal("fixture metadata")
				}
				expected.Created = "other"
			}
			if mode == "wrong_generation" {
				expected = softwareIdentity{Volume: "other", FileID: "other"}
			}
			if mode == "extra_alias" && os.Link(a, filepath.Join(root, "third.exe")) != nil {
				t.Fatal("third")
			}
			if mode == "collision" && os.WriteFile(dest, []byte("retained"), 0600) != nil {
				t.Fatal("collision")
			}
			var output bytes.Buffer
			size := len("owned compiled software")
			if mode == "wrong_size" {
				size++
			}
			exit := exportCurrentJobOutput(&output, mode != "wrong_source", a, b, dest, expected, size)
			var result struct {
				Qualified     bool `json:"qualified"`
				ExportCreated bool `json:"export_created"`
			}
			if json.Unmarshal(output.Bytes(), &result) != nil {
				t.Fatal("result")
			}
			if mode == "normal" {
				got, e := os.ReadFile(dest)
				if exit != 0 || !result.Qualified || !result.ExportCreated || e != nil || !bytes.Equal(got, []byte("owned compiled software")) {
					t.Fatal("normal export")
				}
			} else if exit == 0 || result.Qualified {
				t.Fatal("unqualified export")
			}
		})
	}
}

func TestCurrentJobOutputSourceAndGenerationBindBeforeAliasRead(t *testing.T) {
	for _, mode := range []string{"normal", "source", "output", "generation", "build_failure", "duplicate_input", "other_path", "tree", "changed_bytes", "proof_digest"} {
		t.Run(mode, func(t *testing.T) {
			var proof currentJobOutputEvidence
			proof.Source = currentJobSource
			proof.Merged = "42bc2f0b5965626e4ba47daaa3624a8317dbb322"
			proof.Artifact.Path = currentJobOutput
			proof.Artifact.Alias = currentJobAlias
			proof.Artifact.Bytes = 9862656
			proof.Artifact.Identity.Volume = currentJobOutputIdentity.Volume
			proof.Artifact.Identity.FileID = currentJobOutputIdentity.FileID
			proof.Artifact.Identity.Created = currentJobOutputIdentity.Created
			proof.Build.Exit = 0
			proof.Build.Handle = 48512
			proof.Build.CargoJSONArtifact = true
			proof.Build.ExpectedInputCount = 108
			var tree bytes.Buffer
			for i := 0; i < 108; i++ {
				path := fmt.Sprintf("cc-jobs/src/owned%d.rs", i)
				blob := strings.Repeat("a", 40)
				proof.Inputs = append(proof.Inputs, buildSourceRecord{Path: path, Blob: blob, SHA: digest([]byte("owned source"))})
				fmt.Fprintf(&tree, "100644 blob %s\t%s%c", blob, path, byte(0))
			}
			switch mode {
			case "source":
				proof.Source = "other"
			case "output":
				proof.Artifact.Path = "D:/unrelated.exe"
			case "generation":
				proof.Artifact.Identity.Created = "other"
			case "build_failure":
				proof.Build.Exit = 1
			case "duplicate_input":
				proof.Inputs[1] = proof.Inputs[0]
			case "other_path":
				proof.Inputs[0].Path = "../private"
			}
			raw, _ := json.Marshal(proof)
			sha := digest(raw)
			if mode == "proof_digest" {
				sha = strings.Repeat("0", 64)
			}
			opened := 0
			got := currentJobOutputSourceMatchesProof(raw, sha, func(args ...string) ([]byte, bool) {
				if args[0] == "merge-base" {
					return nil, true
				}
				if mode == "tree" {
					return nil, false
				}
				return tree.Bytes(), true
			}, func(path string) (updateObject, error) {
				opened++
				if !strings.Contains(path, "/cc-jobs/src/") && !strings.Contains(path, "\\cc-jobs\\src\\") {
					return nil, errors.New("outside")
				}
				raw := []byte("owned source")
				if mode == "changed_bytes" {
					raw = []byte("changed")
				}
				return &fakeUpdateObject{raw: raw}, nil
			})
			if got != (mode == "normal") {
				t.Fatal("wrong source outcome", mode, got)
			}
			if mode == "normal" && opened != 108 {
				t.Fatal("missing input verification")
			}
			if (mode == "source" || mode == "output" || mode == "generation" || mode == "build_failure" || mode == "proof_digest") && opened != 0 {
				t.Fatal("invalid provenance opened source")
			}
		})
	}
}
