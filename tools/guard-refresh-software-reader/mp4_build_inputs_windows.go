//go:build windows

package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
)

type softwareGoPackage struct {
	Dir                                                                        string
	GoFiles, CgoFiles, CFiles, CXXFiles, HFiles, SFiles, SysoFiles, EmbedFiles []string
	Module                                                                     struct {
		Path, Dir, GoMod string
		Main             bool
	}
}

// Build only the actual selected, already-held source names. A new file added
// after go list cannot silently join the compiler inputs. The current fixed
// verifier has no cgo, assembly, embedded data or third-party Go dependencies.
func softwareGoBuildFiles(raw []byte, root string, proof askBuildEvidence) ([]string, bool) {
	var p softwareGoPackage
	if len(raw) > 1<<20 || json.Unmarshal(raw, &p) != nil || !sameCleanPath(p.Dir, filepath.Join(root, "unattended-runner")) || p.Module.Path != "github.com/AutoDevJapan/cc-pc-tools-rs/unattended-runner" || !p.Module.Main || !sameCleanPath(p.Module.Dir, p.Dir) || !sameCleanPath(p.Module.GoMod, filepath.Join(p.Dir, "go.mod")) {
		return nil, false
	}
	if len(p.CgoFiles)+len(p.CFiles)+len(p.CXXFiles)+len(p.HFiles)+len(p.SFiles)+len(p.SysoFiles)+len(p.EmbedFiles) != 0 || len(p.GoFiles) == 0 {
		return nil, false
	}
	known := map[string]bool{}
	for _, s := range proof.Inputs {
		known[s.Path] = true
	}
	if !known["unattended-runner/go.mod"] {
		return nil, false
	}
	files := make([]string, 0, len(p.GoFiles))
	seen := map[string]bool{}
	for _, name := range p.GoFiles {
		if filepath.Base(name) != name || strings.ContainsAny(name, "/\\:") || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || seen[name] || !known["unattended-runner/"+name] {
			return nil, false
		}
		seen[name] = true
		files = append(files, filepath.Join(p.Dir, name))
	}
	return files, true
}

func softwareProducerGoFiles(ctx context.Context, proof askBuildEvidence) ([]string, bool) {
	env, ok := askGitCommand(ctx, "C:/Program Files/Go/bin/go.exe", "-C", updateSourceRoot+"/unattended-runner", "env", "-json", "GOFLAGS", "GOWORK", "GOMOD", "GOOS", "GOARCH")
	if !ok {
		return nil, false
	}
	defer clear(env)
	var settings map[string]string
	if json.Unmarshal(env, &settings) != nil || settings["GOFLAGS"] != "" || settings["GOWORK"] != "" && settings["GOWORK"] != "off" || settings["GOOS"] != "windows" || settings["GOARCH"] != "amd64" || !sameCleanPath(settings["GOMOD"], filepath.Join(updateSourceRoot, "unattended-runner", "go.mod")) {
		return nil, false
	}
	raw, ok := askGitCommand(ctx, "C:/Program Files/Go/bin/go.exe", "-C", updateSourceRoot+"/unattended-runner", "list", "-json", ".")
	if !ok {
		return nil, false
	}
	defer clear(raw)
	return softwareGoBuildFiles(raw, updateSourceRoot, proof)
}
