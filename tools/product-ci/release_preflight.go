package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// This is a compatibility preflight, never an Actions runner, status writer or
// deployment. Its only input is the original audited merged snapshot.
type releasePreflightReceipt struct {
	RepoID                                                          int64
	SourceSHA, Tree                                                 string
	PhaseCPUNano, PhaseMemoryBytes, PhasePids                       int64
	SourceVerifiedBefore, SourceVerifiedAfter                       bool
	OriginalCodeQLSucceeded, NoRunnerRegistered, NoProductContainer bool
	BuildAttempted, ArtifactVerified, LockReleased                  bool
	RecoveryRequired, Succeeded, ExecutionAuthorized                bool
	Build                                                           releaseBuildReceipt
	Failure                                                         string
}

type releasePhaseRecord struct {
	Kind, Nonce, DirectorySHA256, SourceSHA, Tree string
	CPUNano, MemoryBytes, Pids                    int64
	BuildAttempted                                bool
}

type releasePhaseLease struct {
	file   *os.File
	path   string
	record releasePhaseRecord
}

func releasePhasePath() string {
	// Exactly the existing per-product exclusion. Old entry points' O_EXCL
	// acquisition rejects this lease without any change to their admission.
	return filepath.Join(os.TempDir(), "todo2648-repo-1285649124.lock")
}

func newReleasePhaseRecord(dir string) (releasePhaseRecord, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return releasePhaseRecord{}, errors.New("release phase nonce unavailable")
	}
	return releasePhaseRecord{Kind: "fixed-release-build-preflight", Nonce: hex.EncodeToString(b), DirectorySHA256: digest([]byte(dir)), SourceSHA: mergedSHA, Tree: mergedTree,
		CPUNano: releasePhaseCPUNano, MemoryBytes: releasePhaseMemory, Pids: releasePhasePids}, nil
}

func validReleasePhase(r releasePhaseRecord, dir string) bool {
	return r.Kind == "fixed-release-build-preflight" && releaseNoncePattern.MatchString(r.Nonce) && r.DirectorySHA256 == digest([]byte(dir)) && r.SourceSHA == mergedSHA && r.Tree == mergedTree &&
		r.CPUNano == releasePhaseCPUNano && r.MemoryBytes == releasePhaseMemory && r.Pids == releasePhasePids
}

func acquireReleasePhase(path, dir string, recovery bool) (*releasePhaseLease, error) {
	if validateOwnedDir(dir, recovery) != "" {
		return nil, errors.New("release phase owned directory rejected")
	}
	var f *os.File
	var err error
	if recovery {
		f, err = openAdmissionForRecovery(path)
	} else {
		f, err = createReleaseAdmission(path)
	}
	if err != nil {
		return nil, errors.New("release phase unavailable")
	}
	l := &releasePhaseLease{file: f, path: path}
	if err = lockAdmission(f); err != nil {
		f.Close()
		return nil, errors.New("release phase owner active")
	}
	info, err := f.Stat()
	entry, entryErr := os.Lstat(path)
	if err != nil || entryErr != nil || !entry.Mode().IsRegular() || !os.SameFile(info, entry) {
		f.Close()
		return nil, errors.New("release phase identity rejected")
	}
	if recovery {
		if info.Size() < 2 || info.Size() > 4096 {
			f.Close()
			return nil, errors.New("release phase record rejected")
		}
		b := make([]byte, info.Size()-1)
		_, err = f.ReadAt(b, 1)
		if err != nil || json.Unmarshal(b, &l.record) != nil || !validReleasePhase(l.record, dir) {
			f.Close()
			return nil, errors.New("release phase record rejected")
		}
	} else {
		l.record, err = newReleasePhaseRecord(dir)
		if err == nil {
			err = l.persist()
		}
		if err != nil {
			// No source or Docker operation has been called. Remove only this new
			// inode while its kernel lock still excludes recovery.
			_ = l.release()
			return nil, errors.New("release phase reservation failed")
		}
	}
	return l, nil
}

func (l *releasePhaseLease) persist() error {
	b, err := json.Marshal(l.record)
	if err != nil {
		return err
	}
	if err = l.file.Truncate(int64(len(b) + 1)); err != nil {
		return err
	}
	if _, err = l.file.WriteAt(b, 1); err != nil {
		return err
	}
	return l.file.Sync()
}

func (l *releasePhaseLease) release() error {
	info, err := l.file.Stat()
	now, ne := os.Lstat(l.path)
	if err != nil || ne != nil || !os.SameFile(info, now) {
		return errors.New("release phase identity changed")
	}
	// Windows handles explicitly share DELETE. Unlink while still holding the
	// kernel lock, as in the existing recovery path; never close then unlink.
	if os.Remove(l.path) != nil {
		return errors.New("release phase removal failed")
	}
	return nil
}

func verifyReleasePreflightIdle(ctx context.Context, r repository, read api) error {
	return verifyReleasePreflightIdleWith(ctx, r, read, func(ctx context.Context) ([]byte, error) {
		return boundedOutput(cmd(ctx, docker, "container", "ls", "--all", "--quiet", "--filter", "label=cc.todo=2648"), 1<<20)
	})
}

func verifyReleasePreflightIdleWith(ctx context.Context, r repository, read api, inventory func(context.Context) ([]byte, error)) error {
	var runners struct {
		Total   int `json:"total_count"`
		Runners []json.RawMessage
	}
	if err := read(ctx, "repos/"+r.FullName+"/actions/runners?per_page=100", &runners); err != nil {
		return mergedReadFailure("preflight runners", err)
	}
	// No registration is needed by this host-only preflight. Fail closed even
	// for an unrelated registration instead of altering or deleting it.
	if runners.Total != 0 || len(runners.Runners) != 0 {
		return errors.New("release preflight runner registered")
	}
	out, err := inventory(ctx)
	if err != nil {
		return errors.New("release preflight container inventory unavailable")
	}
	if strings.TrimSpace(string(out)) != "" {
		return errors.New("release preflight product container present")
	}
	return nil
}

func verifyOriginalReleaseCodeQL(ctx context.Context, r repository, read api) error {
	var run struct {
		codeQLRun
		WorkflowID int64 `json:"workflow_id"`
		Conclusion string
	}
	if err := read(ctx, "repos/"+r.FullName+"/actions/runs/38941323954", &run); err != nil {
		return mergedReadFailure("preflight CodeQL run", err)
	}
	if run.ID != mergedCodeQLRun || run.WorkflowID != mergedCodeQLWorkflow || run.RunAttempt != 1 || run.HeadSHA != mergedSHA || run.HeadBranch != "main" || run.Event != "dynamic" || run.Path != "dynamic/github-code-scanning/codeql" || run.Status != "completed" || run.Conclusion != "success" || !sameMergedRepo(run.Repository, r) || !sameMergedRepo(run.HeadRepo, r) {
		return errors.New("release preflight original CodeQL run rejected")
	}
	var jobs struct {
		Total int `json:"total_count"`
		Jobs  []struct {
			ID                 int64
			RunID              int64 `json:"run_id"`
			Status, Conclusion string
			Labels             []string
		}
	}
	if err := read(ctx, "repos/"+r.FullName+"/actions/runs/38941323954/attempts/1/jobs?per_page=100", &jobs); err != nil {
		return mergedReadFailure("preflight CodeQL jobs", err)
	}
	seen := map[int64]bool{117109408190: false, 117109408508: false, 117109408625: false, 117109408843: false}
	if jobs.Total != 4 || len(jobs.Jobs) != 4 {
		return errors.New("release preflight original CodeQL matrix rejected")
	}
	for _, j := range jobs.Jobs {
		used, ok := seen[j.ID]
		if !ok || used || j.RunID != mergedCodeQLRun || j.Status != "completed" || j.Conclusion != "success" || len(j.Labels) != 1 || j.Labels[0] != codeQLLabel(r.ID) {
			return errors.New("release preflight original CodeQL matrix rejected")
		}
		seen[j.ID] = true
	}
	return nil
}

func readSealedReleaseSource(ctx context.Context, r repository, read api, archiveRead func(context.Context, string) ([]byte, error)) (sealedReleaseContext, error) {
	var tree releaseTreeReply
	if err := read(ctx, "repos/"+r.FullName+"/git/trees/"+mergedTree+"?recursive=1", &tree); err != nil {
		return sealedReleaseContext{}, mergedReadFailure("release tree", err)
	}
	if _, err := verifyReleaseTree(tree, mergedTree); err != nil {
		return sealedReleaseContext{}, err
	}
	b, err := archiveRead(ctx, "repos/"+r.FullName+"/tarball/"+mergedSHA)
	if err != nil {
		return sealedReleaseContext{}, mergedReadFailure("release archive", err)
	}
	return sealReleaseContext(tree, b)
}

type releasePreflightDeps struct {
	read    api
	archive func(context.Context, string) ([]byte, error)
	source  func(context.Context, repository, api, func(context.Context, string) ([]byte, error)) (sealedReleaseContext, error)
	idle    func(context.Context, repository, api) error
	prepare func(sealedReleaseContext) (sealedReleaseBuildInput, error)
	build   func(context.Context, sealedReleaseBuildInput, string) (releaseBuildReceipt, error)
	recover func(context.Context, string) (releaseBuildReceipt, error)
}

func releaseBuildPreflight(ctx context.Context, dir string, recovery bool) (releasePreflightReceipt, error) {
	return runReleasePreflight(ctx, dir, releasePhasePath(), recovery, releasePreflightDeps{get, readReleaseSourceArchive, readSealedReleaseSource, verifyReleasePreflightIdle, prepareAuditedReleaseBuild, executeAuditedReleaseBuild, recoverAuditedReleaseBuild})
}

func runReleasePreflight(ctx context.Context, dir, lockPath string, recovery bool, d releasePreflightDeps) (report releasePreflightReceipt, ret error) {
	report = releasePreflightReceipt{RepoID: 1285649124, SourceSHA: mergedSHA, Tree: mergedTree, PhaseCPUNano: releasePhaseCPUNano, PhaseMemoryBytes: releasePhaseMemory, PhasePids: releasePhasePids}
	l, err := acquireReleasePhase(lockPath, dir, recovery)
	if err != nil {
		report.Failure = "phase-unavailable"
		return report, err
	}
	defer l.file.Close()
	clean := !l.record.BuildAttempted
	defer func() {
		if clean {
			if err := l.release(); err != nil {
				ret = errors.Join(ret, err)
			} else {
				report.LockReleased = true
			}
		}
		report.RecoveryRequired = !report.LockReleased
		if ret != nil {
			report.Succeeded = false
			report.Failure = "preflight-incomplete"
		}
	}()
	if recovery {
		if l.record.BuildAttempted {
			report.Build, ret = d.recover(ctx, dir)
			clean = report.Build.PhaseMayRelease && !report.Build.RecoveryRequired
		}
		if !clean && ret == nil {
			ret = errors.New("release preflight recovery incomplete")
		}
		return report, ret
	}
	r, err := verifyMerged(ctx, mergedCandidate(), d.read)
	if err != nil {
		return report, err
	}
	report.SourceVerifiedBefore = true
	if err := verifyOriginalReleaseCodeQL(ctx, r, d.read); err != nil {
		return report, err
	}
	report.OriginalCodeQLSucceeded = true
	if err := d.idle(ctx, r, d.read); err != nil {
		return report, err
	}
	report.NoRunnerRegistered, report.NoProductContainer = true, true
	snapshot, err := d.source(ctx, r, d.read, d.archive)
	if err != nil {
		return report, err
	}
	input, err := d.prepare(snapshot)
	if err != nil {
		return report, err
	}
	beforeBuild, err := verifyMerged(ctx, mergedCandidate(), d.read)
	if err != nil || beforeBuild != r {
		return report, errors.New("release preflight source changed or unavailable before build")
	}
	if err := d.idle(ctx, r, d.read); err != nil {
		return report, err
	}
	// Persist intent before entering code which may create an external resource.
	l.record.BuildAttempted = true
	if err := l.persist(); err != nil {
		return report, errors.New("release phase intent persistence failed")
	}
	clean = false
	report.BuildAttempted = true
	report.Build, ret = d.build(ctx, input, dir)
	clean = report.Build.PhaseMayRelease && !report.Build.RecoveryRequired
	if ret == nil {
		artifact, err := report.Build.verifiedArtifact()
		if err != nil {
			ret = err
		} else {
			// Verification only. Never load, publish or retain the built image.
			report.ArtifactVerified = len(artifact) > 0
			clear(artifact)
		}
	}
	after, err := verifyMerged(ctx, mergedCandidate(), d.read)
	if err != nil || after != r {
		ret = errors.Join(ret, errors.New("release preflight source changed or unavailable after build"))
	} else {
		report.SourceVerifiedAfter = true
	}
	report.Succeeded = ret == nil && report.Build.Succeeded && report.ArtifactVerified && clean
	if !report.Succeeded && ret == nil {
		ret = errors.New("release preflight build or cleanup incomplete")
	}
	return report, ret
}
