//go:build windows

package main

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestProducerSelectedGoInputsMustBeHeldProofFiles(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "unattended-runner")
	base := softwareGoPackage{Dir: dir, GoFiles: []string{"main.go", "platform_windows.go"}}
	base.Module.Path = "github.com/AutoDevJapan/cc-pc-tools-rs/unattended-runner"
	base.Module.Main = true
	base.Module.Dir = dir
	base.Module.GoMod = filepath.Join(dir, "go.mod")
	proof := askBuildEvidence{Inputs: []buildSourceRecord{{Path: "unattended-runner/main.go"}, {Path: "unattended-runner/platform_windows.go"}, {Path: "unattended-runner/go.mod"}}}
	for _, fault := range []string{"normal", "untracked", "repeated", "parent", "embed", "cgo", "othermodule", "otherdir"} {
		t.Run(fault, func(t *testing.T) {
			p := base
			p.GoFiles = append([]string{}, base.GoFiles...)
			switch fault {
			case "untracked":
				p.GoFiles = append(p.GoFiles, "untracked.go")
			case "repeated":
				p.GoFiles = append(p.GoFiles, "main.go")
			case "parent":
				p.GoFiles[0] = "../main.go"
			case "embed":
				p.EmbedFiles = []string{"untracked.data"}
			case "cgo":
				p.CgoFiles = []string{"cgo.go"}
			case "othermodule":
				p.Module.Path = "other"
			case "otherdir":
				p.Dir = root
			}
			raw, _ := json.Marshal(p)
			files, ok := softwareGoBuildFiles(raw, root, proof)
			if ok != (fault == "normal") {
				t.Fatal("compiler input set mismatch admitted", fault)
			}
			if ok && (len(files) != 2 || files[0] != filepath.Join(dir, "main.go")) {
				t.Fatal("selected compiler paths differ")
			}
		})
	}
}
