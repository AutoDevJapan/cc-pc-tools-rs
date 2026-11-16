//go:build windows

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"time"
)

const currentJobSource = "f95ba6fb5a9e8c137c466ddf3809174a27dc3219"
const currentJobOutputProof = "D:/CC/Tools/codex-jobs/fourbros4431-currentjob-release-output-proof.json"
const currentJobOutputProofSHA = "b9a590ebad32f4195dec8dc8fbaae5f2b63d3eb1f0c37fec28396030d52911f1"
const currentJobOutput = updateSourceRoot + "/target/release/codex_jobs.exe"
const currentJobAlias = updateSourceRoot + "/target/release/deps/codex_jobs.exe"
const currentJobExport = updateSourceRoot + "/codex-owned-ad4200a309dd3c4e9e8a6f4f2348e751/codex_jobs-current-f95.exe"

var currentJobOutputIdentity = softwareIdentity{Volume: "2229119083", FileID: "2814749784421620", Created: "134390608600569389"}

type currentJobOutputEvidence struct {
	Source   string `json:"source"`
	Merged   string `json:"merged"`
	Artifact struct {
		Path     string `json:"path"`
		Alias    string `json:"alias"`
		Bytes    int    `json:"bytes"`
		Identity struct {
			Volume  string `json:"volume"`
			FileID  string `json:"file_id"`
			Created string `json:"created"`
		} `json:"identity"`
	} `json:"artifact"`
	Inputs []buildSourceRecord `json:"inputs"`
	Build  struct {
		Exit               int  `json:"exit"`
		Handle             int  `json:"handle"`
		CargoJSONArtifact  bool `json:"cargoJSONArtifact"`
		ExpectedInputCount int  `json:"expectedInputCount"`
	} `json:"build"`
}

func currentJobSourcePath(path string) bool {
	if path == "Cargo.toml" || path == "Cargo.lock" {
		return true
	}
	if strings.Contains(path, "..") || strings.Contains(path, "\\") {
		return false
	}
	allowed := false
	for _, root := range []string{"cc-jobs", "cc-job-policy", "cc-chrome", "cc-task-title", "cc-redact", "cc-live", "cc-local-llm"} {
		if strings.HasPrefix(path, root+"/") {
			allowed = true
			break
		}
	}
	return allowed && (strings.HasSuffix(path, "/Cargo.toml") || strings.HasSuffix(path, "/build.rs") || strings.Contains(path, "/src/") || strings.HasPrefix(path, "cc-jobs/tests/fixtures/help-"))
}

func currentJobOutputSourceMatches(raw []byte, git func(...string) ([]byte, bool), open func(string) (updateObject, error)) bool {
	return currentJobOutputSourceMatchesProof(raw, currentJobOutputProofSHA, git, open)
}

func currentJobOutputSourceMatchesProof(raw []byte, expectedProofSHA string, git func(...string) ([]byte, bool), open func(string) (updateObject, error)) bool {
	if digest(raw) != expectedProofSHA {
		return false
	}
	var proof currentJobOutputEvidence
	if json.Unmarshal(raw, &proof) != nil || proof.Source != currentJobSource || proof.Merged != "42bc2f0b5965626e4ba47daaa3624a8317dbb322" || proof.Artifact.Path != currentJobOutput || proof.Artifact.Alias != currentJobAlias || proof.Artifact.Bytes != 9862656 || proof.Build.Exit != 0 || proof.Build.Handle != 48512 || !proof.Build.CargoJSONArtifact || proof.Build.ExpectedInputCount != 108 || len(proof.Inputs) != 108 {
		return false
	}
	id := softwareIdentity{proof.Artifact.Identity.Volume, proof.Artifact.Identity.FileID, proof.Artifact.Identity.Created}
	if id != currentJobOutputIdentity {
		return false
	}
	if _, ok := git("merge-base", "--is-ancestor", currentJobSource, "refs/remotes/origin/main"); !ok {
		return false
	}
	tree, ok := git("ls-tree", "-r", "-z", currentJobSource, "--", "Cargo.toml", "Cargo.lock", "cc-jobs", "cc-job-policy", "cc-chrome", "cc-task-title", "cc-redact", "cc-live", "cc-local-llm")
	if !ok || len(tree) > 1<<20 {
		return false
	}
	blobs := map[string]string{}
	for _, row := range bytes.Split(tree, []byte{0}) {
		if len(row) == 0 {
			continue
		}
		parts := bytes.SplitN(row, []byte{'\t'}, 2)
		if len(parts) != 2 {
			return false
		}
		path := string(parts[1])
		if !currentJobSourcePath(path) {
			continue
		}
		fields := strings.Fields(string(parts[0]))
		if len(fields) != 3 || fields[1] != "blob" {
			return false
		}
		blobs[path] = fields[2]
	}
	if len(blobs) != len(proof.Inputs) {
		return false
	}
	seen := map[string]bool{}
	for _, input := range proof.Inputs {
		if !currentJobSourcePath(input.Path) || seen[input.Path] || blobs[input.Path] != input.Blob || !vendorSHA(input.SHA) {
			return false
		}
		seen[input.Path] = true
		o, err := open(filepath.Join(updateSourceRoot, filepath.FromSlash(input.Path)))
		if err != nil {
			return false
		}
		same := digest(o.Raw()) == input.SHA
		closed := o.Close()
		if !same || closed != nil {
			return false
		}
	}
	return true
}

func runCurrentJobOutput(out io.Writer) int {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	proof, err := openUpdateObject(currentJobOutputProof)
	if err != nil {
		return exportCurrentJobOutput(out, false, "", "", "", softwareIdentity{}, 0)
	}
	valid := currentJobOutputSourceMatches(proof.Raw(), func(args ...string) ([]byte, bool) {
		return askGitCommand(ctx, "C:/Program Files/Git/cmd/git.exe", append([]string{"-C", updateSourceRoot}, args...)...)
	}, openUpdateObject)
	if proof.Close() != nil || ctx.Err() != nil {
		valid = false
	}
	return exportCurrentJobOutput(out, valid, currentJobOutput, currentJobAlias, currentJobExport, currentJobOutputIdentity, 9862656)
}

// The public entry will supply only fixed, independently bound producer paths.
// This controller does not broaden ordinary hardlink or source classification.
func exportCurrentJobOutput(out io.Writer, sourceQualified bool, a, b, destination string, expected softwareIdentity, expectedBytes int) int {
	r := struct {
		Domain        string            `json:"domain"`
		Qualified     bool              `json:"qualified"`
		ExportCreated bool              `json:"export_created"`
		SHA           string            `json:"sha256,omitempty"`
		Bytes         int               `json:"bytes,omitempty"`
		Identity      *softwareIdentity `json:"identity,omitempty"`
		Reason        string            `json:"reason"`
	}{Domain: "owner-current-job-software-output-v1", Reason: "source_or_output_unqualified"}
	if sourceQualified {
		withPinnedCargoOutput(a, b, expected, func(raw []byte, id softwareIdentity) bool {
			if expected.FileID != "" && expected != id || len(raw) != expectedBytes {
				return false
			}
			made, err := createAskArtifactCopy(destination, raw)
			if made.FileID != "" {
				r.ExportCreated = true
				r.Identity = &made
				r.Reason = "incomplete_export_preserved"
			}
			if err != nil {
				return false
			}
			r.Qualified = true
			r.SHA = digest(raw)
			r.Bytes = len(raw)
			r.Reason = "verified_same_handle_software_export"
			return true
		})
	}
	if json.NewEncoder(out).Encode(r) != nil || !r.Qualified {
		return 1
	}
	return 0
}
