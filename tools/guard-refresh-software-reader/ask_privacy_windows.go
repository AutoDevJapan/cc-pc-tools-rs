//go:build windows

package main

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// A software-maintenance purpose, separate from data and image qualification.
// The final reviewed distribution pins are compiled in, never caller supplied.
const askTarget = "D:/CC/Tools/discord-ask/discord_ask.exe"
const askStage = "D:/CC/Tools/discord-ask/discord_ask.privacy4431.stage.exe"
const askBackup = "D:/CC/Tools/discord-ask/discord_ask.privacy4431.before.exe"
const askOldSHA = "f5dfeaea2013d83f165da7801595a795d71aedba4c93ed9cc24e8492bdf67ad7"
const askOldBytes = 13171200
const askOldVolume = "1215965535"
const askOldID = "844424946112908"
const askOldCreated = "134386801684208828"

type askSoftwarePlan struct {
	Candidate, Proof, Source, SHA, ProofSHA string
	Bytes                                   int
}

// These identify the separately reviewed source and compiled distribution.
var compiledAskPlan = askSoftwarePlan{
	Candidate: "C:/Users/makhmeto/Documents/Codex/2026-11-07/ika-x20/work/4431-ui-tools/codex-owned-f60f3742a1933562ec75aa5c8eb05d72/discord_ask-privacy-f102.exe",
	Proof:     "D:/CC/Tools/codex-jobs/fourbros4431-ask-privacy-f102-candidate-proof.json",
	Source:    "f102a759524ab2cbefc690fbaf4f9710cc1ddee0",
	SHA:       "d0b6e7db813211ae078f403cb6a9743e696b8f53397bf34d9c8d86be54f1f96d",
	ProofSHA:  "7b218213add39363696283065dc07187d17b08e99f37bc4e22359fa7bcfb677e",
	Bytes:     13400576,
}

type askSoftwareProof struct {
	Head, Candidate, SHA string
	Bytes                int
	Sources              []buildSourceRecord
}

func askSourcePath(path string) bool {
	if path == "Cargo.toml" || path == "Cargo.lock" || path == "repair-guard/owner_catalog.json" {
		return true
	}
	if strings.Contains(path, "\\") || strings.Contains(path, ":") {
		return false
	}
	parts := strings.Split(path, "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return len(parts) > 1 && map[string]bool{"cc-ask": true, "cc-chrome": true, "cc-job-policy": true, "cc-live": true, "cc-local-llm": true, "cc-task-title": true}[parts[0]]
}

func askCandidateMatches(raw, proofRaw []byte, plan askSoftwarePlan, git func(...string) ([]byte, bool)) bool {
	return askCandidateMatchesContext(context.Background(), raw, proofRaw, plan, git)
}

func askCandidateMatchesContext(ctx context.Context, raw, proofRaw []byte, plan askSoftwarePlan, git func(...string) ([]byte, bool)) bool {
	if ctx.Err() != nil {
		return false
	}
	if plan.Bytes <= 0 || len(plan.SHA) != 64 || len(plan.ProofSHA) != 64 || len(plan.Source) != 40 || len(raw) != plan.Bytes || digest(raw) != plan.SHA || digest(proofRaw) != plan.ProofSHA {
		return false
	}
	var proof askSoftwareProof
	if json.Unmarshal(proofRaw, &proof) != nil || proof.Head != plan.Source || proof.Candidate != plan.Candidate || proof.SHA != plan.SHA || proof.Bytes != len(raw) {
		return false
	}
	if _, ok := git("merge-base", "--is-ancestor", plan.Source, "refs/remotes/origin/main"); !ok {
		return false
	}
	treeArgs := []string{"ls-tree", "-r", "-z", plan.Source, "--", "Cargo.toml", "Cargo.lock", "cc-ask", "cc-chrome", "cc-job-policy", "cc-live", "cc-local-llm", "cc-task-title", "repair-guard/owner_catalog.json"}
	tree, ok := git(treeArgs...)
	if !ok {
		return false
	}
	treeArgs[3] = "refs/remotes/origin/main"
	merged, ok := git(treeArgs...)
	if !ok || !bytes.Equal(tree, merged) {
		return false
	}
	entries := map[string]string{}
	for _, row := range bytes.Split(tree, []byte{0}) {
		if len(row) == 0 {
			continue
		}
		pair := bytes.SplitN(row, []byte{'\t'}, 2)
		if len(pair) != 2 {
			return false
		}
		fields := strings.Fields(string(pair[0]))
		if len(fields) != 3 || fields[1] != "blob" || !askSourcePath(string(pair[1])) {
			return false
		}
		entries[string(pair[1])] = fields[2]
	}
	if len(entries) == 0 || len(entries) != len(proof.Sources) {
		return false
	}
	seen := map[string]bool{}
	for _, source := range proof.Sources {
		if ctx.Err() != nil {
			return false
		}
		if seen[source.Path] || !askSourcePath(source.Path) || entries[source.Path] != source.Blob || len(source.SHA) != 64 {
			return false
		}
		seen[source.Path] = true
	}
	archiveArgs := append([]string{"archive", "--format=tar", plan.Source, "--"}, treeArgs[5:]...)
	archive, ok := git(archiveArgs...)
	if !ok || len(archive) > 64<<20 {
		return false
	}
	reader := tar.NewReader(bytes.NewReader(archive))
	verified := map[string]bool{}
	byPath := map[string]buildSourceRecord{}
	for _, source := range proof.Sources {
		byPath[source.Path] = source
	}
	for {
		if ctx.Err() != nil {
			return false
		}
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return false
		}
		if header.Typeflag == tar.TypeXGlobalHeader {
			if header.PAXRecords["comment"] != plan.Source || len(header.PAXRecords) != 1 {
				return false
			}
			continue
		}
		if header.Typeflag == tar.TypeDir {
			continue
		}
		source, exists := byPath[header.Name]
		if !exists || verified[header.Name] || header.Typeflag != tar.TypeReg || header.Size < 0 || header.Size > 8<<20 {
			return false
		}
		blob, err := io.ReadAll(io.LimitReader(reader, header.Size+1))
		if err != nil || int64(len(blob)) != header.Size {
			return false
		}
		if digest(blob) != source.SHA && digest(bytes.ReplaceAll(blob, []byte("\n"), []byte("\r\n"))) != source.SHA {
			return false
		}
		h := sha1.New()
		fmt.Fprintf(h, "blob %d\x00", len(blob))
		h.Write(blob)
		if hex.EncodeToString(h.Sum(nil)) != source.Blob {
			return false
		}
		verified[header.Name] = true
	}
	return ctx.Err() == nil && len(verified) == len(proof.Sources)
}

func executeAskSoftware(prepare bool, plan askSoftwarePlan, b updateBoundary) (r updateResult) {
	return executeAskSoftwareContext(context.Background(), prepare, plan, b)
}

func executeAskSoftwareContext(ctx context.Context, prepare bool, plan askSoftwarePlan, b updateBoundary) (r updateResult) {
	r.Reason = "qualification_unavailable"
	defer func() {
		if ctx.Err() != nil {
			r.Qualified, r.Prepared = false, false
			if len(r.Created) != 0 {
				r.Reason = "prepare_incomplete_preserved"
			} else {
				r = updateResult{Reason: "qualification_unavailable"}
			}
		}
	}()
	if ctx.Err() != nil {
		return
	}
	old, err := b.open(askTarget)
	if err != nil {
		return
	}
	defer old.Close()
	if len(old.Raw()) != askOldBytes || !b.oldMatches(old.Raw()) || old.ID() != (softwareIdentity{askOldVolume, askOldID, askOldCreated}) {
		return
	}
	proof, err := b.open(plan.Proof)
	if err != nil {
		return
	}
	defer proof.Close()
	candidate, err := b.open(plan.Candidate)
	if err != nil {
		return
	}
	defer candidate.Close()
	if !b.candidateMatches(candidate.Raw(), proof.Raw()) {
		return
	}
	if !b.absent(askStage) || !b.absent(askBackup) {
		r.Reason = "software_leaf_collision"
		return
	}
	acl, err := old.ACL()
	if err != nil || acl == "" || ctx.Err() != nil {
		return
	}
	id := old.ID()
	r = updateResult{Qualified: true, Reason: "verified_no_write", Domain: "owner-refresh-fixed-software-prepare-v1", Target: askTarget, Stage: askStage, Backup: askBackup, CurrentSHA: askOldSHA, CandidateSHA: plan.SHA, CurrentIdentity: &id, ACLHash: digest([]byte(acl)), SourceHead: plan.Source, SourceMerged: plan.Source}
	if !prepare {
		return
	}
	for _, item := range []struct {
		path string
		raw  []byte
		dest **softwareIdentity
	}{{askBackup, old.Raw(), &r.BackupIdentity}, {askStage, candidate.Raw(), &r.StageIdentity}} {
		if ctx.Err() != nil {
			return
		}
		newID, e := old.CreateNew(item.path, item.raw, acl)
		if newID.FileID != "" {
			r.Created = append(r.Created, item.path)
			*item.dest = &newID
		}
		if e != nil {
			r.Qualified = false
			// Before CREATE_NEW succeeds there is no returned created identity.
			// Preserve the failure as unqualified rather than claiming a partial
			// protocol that requires at least one created leaf. The supervisor
			// separately inspects collisions/unknown state without granting use.
			if len(r.Created) == 0 {
				r = updateResult{Reason: "qualification_unavailable"}
				return
			}
			r.Reason = "prepare_incomplete_preserved"
			return
		}
	}
	r.Prepared = true
	r.Reason = "prepared_two_fixed_software_leaves"
	return
}

type askNativeObject struct {
	updateObject
	ctx context.Context
}

func (o askNativeObject) CreateNew(path string, raw []byte, acl string) (softwareIdentity, error) {
	if o.ctx == nil || o.ctx.Err() != nil {
		return softwareIdentity{}, errUpdateObject
	}
	if path != askStage && path != askBackup || !sameParentPath(path, askTarget) {
		return softwareIdentity{}, errUpdateObject
	}
	native, ok := o.updateObject.(*nativeUpdateObject)
	if !ok {
		return softwareIdentity{}, errUpdateObject
	}
	return native.createNewSibling(path, raw, acl)
}

func performAskSoftware(prepare bool) updateResult {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	return executeAskSoftwareContext(ctx, prepare, compiledAskPlan, askSoftwareBoundaryContext(ctx, compiledAskPlan))
}

// Reconciliation never grants a switch. It observes only these three fixed
// software names after the caller has terminated and waited for the producer.
// An interrupted CreateNew may exist without a returned identity.
type askReconciledObject struct {
	State    string            `json:"state"`
	Identity *softwareIdentity `json:"identity,omitempty"`
	SHA      string            `json:"sha256,omitempty"`
	ACL      string            `json:"acl_sha256,omitempty"`
}
type askReconciliation struct {
	Domain                string `json:"domain"`
	Target, Stage, Backup askReconciledObject
	SwitchAllowed         bool `json:"switch_allowed"`
}

func validAskReconciliation(r askReconciliation) bool {
	if r.Domain != "owner-question-fixed-software-reconcile-v1" || r.SwitchAllowed {
		return false
	}
	for _, o := range []askReconciledObject{r.Target, r.Stage, r.Backup} {
		switch o.State {
		case "absent", "unknown":
			if o.Identity != nil || o.SHA != "" || o.ACL != "" {
				return false
			}
		case "present":
			if o.Identity == nil || o.Identity.FileID == "" || o.Identity.Volume == "" || o.Identity.Created == "" || len(o.SHA) != 64 || len(o.ACL) != 64 {
				return false
			}
			if _, e := hex.DecodeString(o.SHA); e != nil {
				return false
			}
			if _, e := hex.DecodeString(o.ACL); e != nil {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func reconcileAskObjects(ctx context.Context, open func(string) (updateObject, error), stat func(string) (os.FileInfo, error)) askReconciliation {
	r := askReconciliation{Domain: "owner-question-fixed-software-reconcile-v1"}
	observe := func(path string) askReconciledObject {
		unknown := askReconciledObject{State: "unknown"}
		if ctx.Err() != nil {
			return unknown
		}
		_, err := stat(path)
		if os.IsNotExist(err) {
			return askReconciledObject{State: "absent"}
		}
		if err != nil {
			return unknown
		}
		o, err := open(path)
		if err != nil {
			return unknown
		}
		defer o.Close()
		acl, err := o.ACL()
		if err != nil || ctx.Err() != nil {
			return unknown
		}
		id := o.ID()
		if id.FileID == "" {
			return unknown
		}
		return askReconciledObject{State: "present", Identity: &id, SHA: digest(o.Raw()), ACL: digest([]byte(acl))}
	}
	r.Target = observe(askTarget)
	r.Stage = observe(askStage)
	r.Backup = observe(askBackup)
	return r
}

func runAskReconciliation(args []string, out io.Writer) int {
	if len(args) != 1 || args[0] != "--inspect-ask-privacy-update" {
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	// Only the input-free compiled software names can be inspected. No record,
	// proof supplied by a caller or image path is accepted here.
	r := reconcileAskObjects(ctx, func(path string) (updateObject, error) {
		if path != askTarget && path != askStage && path != askBackup {
			return nil, errUpdateObject
		}
		return openUpdateObject(path)
	}, os.Lstat)
	if json.NewEncoder(out).Encode(r) != nil {
		return 1
	}
	return 0
}

// Return only bounded stdout and a completed wait. All Git calls share the
// operation context; a fresh per-child timeout would permit an unbounded sum.
type askGitOutput struct {
	bytes.Buffer
	limit int
}

func (b *askGitOutput) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.Len() {
		return 0, errors.New("software_source_output_limit")
	}
	return b.Buffer.Write(p)
}

func askGitCommand(ctx context.Context, executable string, args ...string) ([]byte, bool) {
	if executable == "C:/Program Files/Git/cmd/git.exe" {
		args = append([]string{"-c", "core.autocrlf=false", "-c", "core.eol=lf"}, args...)
	}
	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.Stderr = io.Discard
	cmd.WaitDelay = 100 * time.Millisecond
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	cmd.Env = append(os.Environ(), "GIT_NO_REPLACE_OBJECTS=1", "GIT_CONFIG_COUNT=0")
	out := askGitOutput{limit: 64 << 20}
	cmd.Stdout = &out
	err := cmd.Run()
	if err != nil || ctx.Err() != nil || cmd.ProcessState == nil || !cmd.ProcessState.Exited() {
		return nil, false
	}
	return out.Bytes(), true
}

func askSoftwareBoundaryContext(ctx context.Context, plan askSoftwarePlan) updateBoundary {
	git := func(args ...string) ([]byte, bool) {
		return askGitCommand(ctx, "C:/Program Files/Git/cmd/git.exe", append([]string{"-C", updateSourceRoot}, args...)...)
	}
	return updateBoundary{open: func(path string) (updateObject, error) {
		if ctx.Err() != nil {
			return nil, errUpdateObject
		}
		if path != askTarget && path != plan.Candidate && path != plan.Proof {
			return nil, errUpdateObject
		}
		o, e := openUpdateObject(path)
		if e != nil {
			return nil, e
		}
		return askNativeObject{o, ctx}, nil
	}, absent: func(path string) bool {
		if ctx.Err() != nil {
			return false
		}
		_, err := os.Lstat(path)
		return os.IsNotExist(err)
	}, oldMatches: func(raw []byte) bool { return digest(raw) == askOldSHA }, candidateMatches: func(raw, proof []byte) bool {
		return askCandidateMatchesContext(ctx, raw, proof, plan, git)
	}}
}

func runAskSoftware(args []string, out io.Writer, ops updateOperations) int {
	if len(args) != 1 {
		return 2
	}
	operation := ops.verify
	if args[0] == "--prepare-ask-privacy-update" {
		operation = ops.prepare
	} else if args[0] != "--verify-ask-privacy-update" {
		return 2
	}
	if operation == nil {
		return 2
	}
	r := operation()
	if json.NewEncoder(out).Encode(r) != nil || !r.Qualified || args[0] == "--prepare-ask-privacy-update" && !r.Prepared {
		return 1
	}
	return 0
}
