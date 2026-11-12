//go:build windows

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"
)

// This input-free software operation implements the requested installed-program
// repair. Its namespace is two compiled CreateNew leaves, never guard data.
const updateCandidatePath = "C:/Users/makhmeto/Documents/Codex/2026-11-07/ika-x20/work/4431-ui-tools/codex-owned-f60f3742a1933562ec75aa5c8eb05d72/guard-refresh-diagnostic-4ede.exe"
const updateCandidateSHA = "edd86d52143b469b4d08493563dfab66db382f278de15791f8a97cc80a143900"
const updateBuildProofPath = "D:/CC/Tools/codex-jobs/fourbros4431-diagnostic-4ede-build-proof.json"
const updateBuildProofSHA = "5e8fd9232cdafa35d784394dc2509529c2c9445439154303435b0b543e0d13d6"
const updateSourceRoot = "C:/Users/makhmeto/Documents/Codex/2026-11-07/ika-x20/work/4431-ui-tools"
const updateSourceHead = "4ede82ea7d6e274c518f2e23a3ade685e8c4c3a2"
const updateMergedHead = "aca933238d13d10649e2e563f8c4f32fcf3a46e1"
const updateStagePath = "D:/CC/Tools/client-records/scrub391.refresh4431.stage.exe"
const updateBackupPath = "D:/CC/Tools/client-records/scrub391.refresh4431.before-f136.exe"
const updateCandidateBytes = 14056960
const updateOldVolume = "1215965535"
const updateOldFileID = "2814749781084419"
const updateOldCreated = "134385555870411097"

type updateResult struct {
	Qualified              bool              `json:"qualified"`
	Prepared               bool              `json:"prepared"`
	Reason                 string            `json:"reason"`
	Domain                 string            `json:"domain,omitempty"`
	Target                 string            `json:"target,omitempty"`
	Stage                  string            `json:"stage,omitempty"`
	Backup                 string            `json:"backup,omitempty"`
	CurrentSHA             string            `json:"current_sha256,omitempty"`
	CandidateSHA           string            `json:"candidate_sha256,omitempty"`
	CurrentIdentity        *softwareIdentity `json:"current_identity,omitempty"`
	StageIdentity          *softwareIdentity `json:"stage_identity,omitempty"`
	BackupIdentity         *softwareIdentity `json:"backup_identity,omitempty"`
	ACLHash                string            `json:"acl_sha256,omitempty"`
	SourceHead             string            `json:"source_head,omitempty"`
	SourceMerged           string            `json:"source_merged,omitempty"`
	Created                []string          `json:"created,omitempty"`
	SoftwareReplaceAllowed bool              `json:"software_replace_allowed"`
	DataAccessGranted      bool              `json:"data_access_granted"`
	UpdatePerformed        bool              `json:"update_performed"`
}

type updateOperations struct{ verify, prepare func() updateResult }
type buildSourceRecord struct {
	Path string `json:"path"`
	Blob string `json:"blob"`
	SHA  string `json:"sha256"`
}
type updateBuildProof struct {
	Head      string              `json:"head"`
	Merge     string              `json:"mainMerge"`
	Candidate string              `json:"candidate"`
	Bytes     int                 `json:"bytes"`
	SHA       string              `json:"sha256"`
	Sources   []buildSourceRecord `json:"compiledPathBlobEvidence"`
}

func digest(raw []byte) string { h := sha256.Sum256(raw); return hex.EncodeToString(h[:]) }

func candidateMatches(raw, proofRaw []byte, git func(...string) ([]byte, bool)) bool {
	if len(raw) != updateCandidateBytes || digest(raw) != updateCandidateSHA || digest(proofRaw) != updateBuildProofSHA {
		return false
	}
	var proof updateBuildProof
	if json.Unmarshal(proofRaw, &proof) != nil || proof.Head != updateSourceHead || proof.Merge != updateMergedHead || proof.Candidate != updateCandidatePath || proof.Bytes != len(raw) || !strings.EqualFold(proof.SHA, updateCandidateSHA) || len(proof.Sources) != 227 {
		return false
	}
	info, err := buildinfo.Read(bytes.NewReader(raw))
	if err != nil || info.Path != installedGuardModule {
		return false
	}
	settings := map[string]string{}
	for _, s := range info.Settings {
		if _, exists := settings[s.Key]; exists {
			return false
		}
		settings[s.Key] = s.Value
	}
	// The dirty build is honestly recorded. The independent pinned input proof
	// and exact PE digest anchor it; this does not assert a clean source build.
	if settings["vcs"] != "git" || settings["vcs.revision"] != updateSourceHead || settings["vcs.modified"] != "true" || settings["GOOS"] != "windows" || settings["GOARCH"] != "amd64" || settings["-trimpath"] != "true" {
		return false
	}
	if _, ok := git("merge-base", "--is-ancestor", updateSourceHead, updateMergedHead); !ok {
		return false
	}
	tree, ok := git("ls-tree", "-r", "-z", updateMergedHead, "--", "repair-guard", "tools/civ6-ci-contract")
	if !ok {
		return false
	}
	blobs := map[string]string{}
	for _, entry := range bytes.Split(tree, []byte{0}) {
		if len(entry) == 0 {
			continue
		}
		pair := bytes.SplitN(entry, []byte{'\t'}, 2)
		if len(pair) != 2 {
			return false
		}
		fields := strings.Fields(string(pair[0]))
		if len(fields) != 3 || fields[1] != "blob" {
			return false
		}
		blobs[string(pair[1])] = fields[2]
	}
	seen := map[string]bool{}
	for _, source := range proof.Sources {
		if seen[source.Path] || (!strings.HasPrefix(source.Path, "repair-guard/") && !strings.HasPrefix(source.Path, "tools/civ6-ci-contract/")) || strings.Contains(source.Path, "..") || strings.Contains(source.Path, "\\") || len(source.SHA) != 64 || blobs[source.Path] != source.Blob {
			return false
		}
		seen[source.Path] = true
	}
	return true
}

type updateObject interface {
	Raw() []byte
	ID() softwareIdentity
	ACL() (string, error)
	CreateNew(string, []byte, string) (softwareIdentity, error)
	Close() error
}
type updateBoundary struct {
	open             func(string) (updateObject, error)
	absent           func(string) bool
	oldMatches       func([]byte) bool
	candidateMatches func([]byte, []byte) bool
}

// Validate and write share the same held old/candidate buffers. Callers cannot
// provide paths, digests, receipts or a claim that protected leaves are clear.
func executeFixedUpdate(prepare bool, b updateBoundary) (r updateResult) {
	r.Reason = "qualification_unavailable"
	old, e := b.open(installedGuardPath)
	if e != nil {
		return
	}
	defer old.Close()
	if !b.oldMatches(old.Raw()) {
		return
	}
	proof, e := b.open(updateBuildProofPath)
	if e != nil {
		return
	}
	defer proof.Close()
	candidate, e := b.open(updateCandidatePath)
	if e != nil {
		return
	}
	defer candidate.Close()
	if !b.candidateMatches(candidate.Raw(), proof.Raw()) {
		return
	}
	if !b.absent(updateStagePath) || !b.absent(updateBackupPath) {
		r.Reason = "software_leaf_collision"
		return
	}
	acl, e := old.ACL()
	if e != nil || acl == "" {
		return
	}
	id := old.ID()
	if id != (softwareIdentity{updateOldVolume, updateOldFileID, updateOldCreated}) {
		return
	}
	r = updateResult{Qualified: true, Reason: "verified_no_write", Domain: "owner-refresh-fixed-software-prepare-v1", Target: installedGuardPath, Stage: updateStagePath, Backup: updateBackupPath, CurrentSHA: installedGuardSHA, CandidateSHA: updateCandidateSHA, CurrentIdentity: &id, ACLHash: digest([]byte(acl)), SourceHead: updateSourceHead, SourceMerged: updateMergedHead}
	if !prepare {
		return
	}
	// Partial creation is preserved and reported, never overwritten or deleted.
	for _, item := range []struct {
		path string
		raw  []byte
		dest **softwareIdentity
	}{{updateBackupPath, old.Raw(), &r.BackupIdentity}, {updateStagePath, candidate.Raw(), &r.StageIdentity}} {
		newID, err := old.CreateNew(item.path, item.raw, acl)
		if newID.FileID != "" {
			r.Created = append(r.Created, item.path)
			*item.dest = &newID
		}
		if err != nil {
			r.Qualified = false
			r.Reason = "prepare_incomplete_preserved"
			return
		}
	}
	r.Prepared = true
	r.Reason = "prepared_two_fixed_software_leaves"
	return
}

func fixedUpdateBoundary() updateBoundary {
	git := func(args ...string) ([]byte, bool) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "C:/Program Files/Git/cmd/git.exe", append([]string{"-C", updateSourceRoot}, args...)...)
		cmd.Stderr = io.Discard
		cmd.WaitDelay = 100 * time.Millisecond
		cmd.Env = append(os.Environ(), "GIT_NO_REPLACE_OBJECTS=1", "GIT_CONFIG_COUNT=0")
		out, err := cmd.Output()
		return out, err == nil
	}
	return updateBoundary{open: openUpdateObject, absent: func(path string) bool { _, err := os.Lstat(path); return os.IsNotExist(err) }, oldMatches: func(raw []byte) bool {
		return pinnedBuildMatches(raw, installedGuardSHA, func(b []byte) (*debug.BuildInfo, error) { return buildinfo.Read(bytes.NewReader(b)) })
	}, candidateMatches: func(raw, proof []byte) bool { return candidateMatches(raw, proof, git) }}
}
func observeFixedUpdate() updateResult { return executeFixedUpdate(false, fixedUpdateBoundary()) }
func prepareFixedUpdate() updateResult { return executeFixedUpdate(true, fixedUpdateBoundary()) }
func runUpdate(args []string, out io.Writer, operations updateOperations) int {
	if len(args) != 1 {
		return 2
	}
	var run func() updateResult
	switch args[0] {
	case "--verify-refresh-guard-update":
		run = operations.verify
	case "--prepare-refresh-guard-update":
		run = operations.prepare
	default:
		return 2
	}
	if run == nil {
		return 2
	}
	r := run()
	if json.NewEncoder(out).Encode(r) != nil || !r.Qualified || args[0] == "--prepare-refresh-guard-update" && !r.Prepared {
		return 1
	}
	return 0
}
func sameSoftwareParent(path string) bool {
	return (path == updateStagePath || path == updateBackupPath) && strings.EqualFold(filepath.Dir(filepath.Clean(path)), filepath.Dir(filepath.Clean(installedGuardPath)))
}
