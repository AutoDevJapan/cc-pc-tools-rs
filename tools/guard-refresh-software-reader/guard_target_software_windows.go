//go:build windows

package main

import (
	"bytes"
	"context"
	"debug/buildinfo"
	"encoding/json"
	"errors"
	"io"
	"os"
	"runtime/debug"
	"strconv"
	"sync"
	"syscall"
	"time"
)

const guardSoftwareInspectionFlag = "--inspect-fresh-guard-software"
const guardSoftwareWorkerFlag = "--fresh-guard-software-worker"
const guardSoftwareDomain = "owner-guard-target-software-v1"

type guardSoftwareSnapshot struct {
	metadata guardTargetMetadata
	sha      string
	revision string
}

type guardSoftwareLease interface {
	Snapshot(context.Context) (guardSoftwareSnapshot, error)
	Observe() (guardTargetMetadata, error)
	Close() error
}

type guardSoftwarePurposeLease interface {
	Recheck(context.Context, guardTargetArtifact) error
	OpenSoftware(context.Context) (guardSoftwareLease, error)
	Close() error
}

// This is an observation after the owned read has closed, never a serialized
// capability for later preparation, replacement or adjacent data access.
type guardSoftwareResult struct {
	guardTargetMetadataResult
	SoftwareObserved       bool   `json:"software_observed"`
	SHA256                 string `json:"sha256,omitempty"`
	SourceRevision         string `json:"source_revision,omitempty"`
	CredentialCleanupState string `json:"credential_cleanup_state"`
}

type guardSoftwareInspectionResult struct {
	guardSoftwareResult
	WorkerResultObserved bool `json:"worker_result_observed"`
	WorkerStarted        bool `json:"worker_started"`
	WorkerAssigned       bool `json:"worker_assigned"`
	WorkerWaitVerified   bool `json:"worker_wait_verified"`
	WorkerReaped         bool `json:"worker_reaped"`
	WorkerTimedOut       bool `json:"worker_timed_out"`
}

// A distinct purpose: the existing metadata lease is not widened into a
// file-data permit. Only the genuine private artifact adapter can own this
// one fixed software-read purpose during the same candidate borrow.
type guardSoftwarePurpose struct {
	mu         sync.Mutex
	self       *guardSoftwarePurpose
	artifact   *guardTargetCandidate
	candidate  *ghaGuardOwnedCandidate
	generation ghaGuardGeneration
	ctx        context.Context
	closed     bool
	opened     bool
	open       func(context.Context) (guardSoftwareLease, error)
}

func guardSoftwarePurposeForArtifact(ctx context.Context, value guardTargetArtifact) (guardSoftwarePurposeLease, error) {
	if ctx == nil || ctx.Err() != nil {
		return nil, guardTargetCandidateFailure(ctx, errGuardTargetPurposeUnavailable)
	}
	artifact, ok := value.(*guardTargetCandidate)
	if !ok {
		return nil, errGuardTargetPurposeUnavailable
	}
	source, generation, valid := artifact.binding()
	if !valid {
		return nil, errGuardTargetPurposeUnavailable
	}
	p := &guardSoftwarePurpose{artifact: artifact, candidate: source, generation: generation, ctx: ctx, open: openGuardTargetSoftware}
	p.self = p
	return p, nil
}

func (p *guardSoftwarePurpose) Recheck(ctx context.Context, value guardTargetArtifact) error {
	if p == nil || ctx == nil || ctx.Err() != nil {
		return guardTargetCandidateFailure(ctx, errGuardTargetPurposeUnavailable)
	}
	p.mu.Lock()
	if p.self != p || p.closed || p.ctx == nil || p.ctx.Err() != nil || value != p.artifact {
		p.mu.Unlock()
		return errGuardTargetPurposeUnavailable
	}
	artifact, source, generation := p.artifact, p.candidate, p.generation
	p.mu.Unlock()
	actual, current, valid := artifact.binding()
	if !valid || actual != source || current != generation {
		return errGuardTargetPurposeUnavailable
	}
	return nil
}

func (p *guardSoftwarePurpose) OpenSoftware(ctx context.Context) (guardSoftwareLease, error) {
	if p == nil || ctx == nil {
		return nil, errGuardTargetPurposeUnavailable
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.self != p || p.closed || p.opened || p.open == nil || p.ctx == nil || p.ctx.Err() != nil || ctx.Err() != nil {
		return nil, guardTargetCandidateFailure(ctx, errGuardTargetPurposeUnavailable)
	}
	source, generation, valid := p.artifact.binding()
	if !valid || source != p.candidate || generation != p.generation || !p.artifact.borrowing() {
		return nil, errGuardTargetPurposeUnavailable
	}
	p.opened = true
	lease, err := p.open(ctx)
	if err != nil {
		return lease, err
	}
	source, generation, valid = p.artifact.binding()
	if !valid || source != p.candidate || generation != p.generation || !p.artifact.borrowing() || ctx.Err() != nil {
		return lease, guardTargetCandidateFailure(ctx, errGuardTargetPurposeUnavailable)
	}
	return lease, nil
}

func (p *guardSoftwarePurpose) Close() error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.self != p {
		return errGuardTargetCleanup
	}
	p.closed = true
	p.artifact, p.candidate, p.open = nil, nil, nil
	return nil
}

func guardSoftwareSnapshotShape(v guardSoftwareSnapshot) bool {
	return guardTargetMetadataShape(v.metadata) && v.sha == installedGuardSHA && v.revision == installedGuardRevision
}

func releaseGuardSoftwareResult(r *guardSoftwareResult, ctx context.Context, artifact guardTargetArtifact, purpose guardSoftwarePurposeLease) {
	if purpose != nil && purpose.Close() != nil {
		r.CleanupFailed = true
	}
	if artifact != nil && artifact.Close() != nil {
		r.CleanupFailed = true
	}
	if ctx != nil && ctx.Err() != nil {
		r.Cancelled = true
	}
	if r.CleanupFailed || r.Cancelled {
		wasQualified := r.Qualified || r.Reason == "verified_guard_target_software"
		r.Qualified = false
		if wasQualified {
			if r.CleanupFailed {
				r.Reason = "guard_target_cleanup_failed"
			} else {
				r.Reason = "guard_target_cancelled"
			}
		}
	}
	if !r.Qualified {
		r.SoftwareObserved = false
		r.SHA256, r.SourceRevision = "", ""
	}
}

func inspectGuardSoftware(ctx context.Context, artifact guardTargetArtifact, purpose guardSoftwarePurposeLease) (r guardSoftwareResult) {
	r.Domain, r.Reason = guardSoftwareDomain, "guard_target_artifact_unavailable"
	r.CredentialCleanupState = "unknown"
	defer func() { releaseGuardSoftwareResult(&r, ctx, artifact, purpose) }()
	if ctx == nil || artifact == nil || !artifact.VerifySourceGeneration() {
		return
	}
	if ctx.Err() != nil {
		r.Cancelled, r.Reason = true, "guard_target_cancelled"
		return
	}
	if purpose == nil {
		r.Reason = "guard_target_purpose_unavailable"
		return
	}
	if err := purpose.Recheck(ctx, artifact); err != nil {
		guardTargetObserveError(&r.guardTargetMetadataResult, err)
		r.Reason = "guard_target_purpose_unavailable"
		return
	}
	calls, callbackSucceeded := 0, false
	err := artifact.WithPinnedPayload(func(payload []byte) (callbackErr error) {
		calls++
		if calls != 1 || len(payload) == 0 || ctx.Err() != nil {
			return errGuardTargetArtifactUnavailable
		}
		if err := purpose.Recheck(ctx, artifact); err != nil {
			r.Reason = "guard_target_purpose_unavailable"
			return err
		}
		r.Reason = "guard_target_metadata_unavailable"
		lease, openErr := purpose.OpenSoftware(ctx)
		if lease != nil {
			defer func() {
				if closeErr := lease.Close(); closeErr != nil {
					r.CleanupFailed = true
					callbackErr = errors.Join(callbackErr, errGuardTargetCleanup, closeErr)
				}
			}()
		}
		if openErr != nil || lease == nil {
			return errors.Join(errGuardTargetMetadata, openErr)
		}
		snapshot, err := lease.Snapshot(ctx)
		if err != nil || !guardSoftwareSnapshotShape(snapshot) {
			r.Reason = "guard_target_software_unverified"
			return errors.Join(errGuardTargetMetadata, err)
		}
		after, err := lease.Observe()
		if err != nil || after != snapshot.metadata {
			r.Reason = "guard_target_generation_changed"
			return errors.Join(errGuardTargetMetadata, err)
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := purpose.Recheck(ctx, artifact); err != nil {
			r.Reason = "guard_target_purpose_unavailable"
			return err
		}
		id := snapshot.metadata.identity
		r.MetadataObserved, r.SoftwareObserved = true, true
		r.CurrentIdentity, r.Bytes = &id, snapshot.metadata.size
		r.ACLHash, r.ParentProof = digest([]byte(snapshot.metadata.acl)), snapshot.metadata.parent
		r.SHA256, r.SourceRevision = snapshot.sha, snapshot.revision
		callbackSucceeded = true
		return nil
	})
	guardTargetObserveError(&r.guardTargetMetadataResult, err)
	generationValid := artifact.VerifySourceGeneration()
	if err != nil || calls != 1 || !callbackSucceeded || r.CleanupFailed || !generationValid {
		// A completed metadata/snapshot observation is not "unavailable"
		// when the enclosing artifact borrow or source generation then fails.
		// Keep an earlier read, purpose, cancellation or cleanup cause intact.
		if r.Reason == "guard_target_metadata_unavailable" && callbackSucceeded && !r.CleanupFailed && ctx.Err() == nil {
			if !generationValid {
				r.Reason = "guard_target_generation_changed"
			} else if err != nil || calls != 1 {
				r.Reason = "guard_target_artifact_unavailable"
			}
		}
		if r.Reason == "guard_target_metadata_unavailable" && (calls != 1 || !callbackSucceeded) {
			r.Reason = "guard_target_software_unverified"
		}
		return
	}
	if err := purpose.Recheck(ctx, artifact); err != nil {
		guardTargetObserveError(&r.guardTargetMetadataResult, err)
		r.Reason = "guard_target_purpose_unavailable"
		return
	}
	if ctx.Err() != nil {
		return
	}
	r.Qualified, r.Reason = true, "verified_guard_target_software"
	return
}

type guardSoftwareNativeLease struct {
	mu         sync.Mutex
	meta       *guardTargetNativeMetadata
	data       *os.File
	verify     func([]byte) bool
	closed     bool
	closeError error
	pending    []io.Closer
}

func verifyInstalledGuardSoftware(raw []byte) bool {
	return pinnedBuildMatches(raw, installedGuardSHA, func(b []byte) (*debug.BuildInfo, error) {
		return buildinfo.Read(bytes.NewReader(b))
	})
}

func openGuardTargetSoftware(ctx context.Context) (guardSoftwareLease, error) {
	return openGuardTargetSoftwareAt(ctx, guardTargetSoftwarePath, verifyInstalledGuardSoftware)
}

// Owned-fixture seam only: production fixes the path and verifier above.
func openGuardTargetSoftwareAt(ctx context.Context, path string, verify func([]byte) bool) (guardSoftwareLease, error) {
	lease := &guardSoftwareNativeLease{meta: &guardTargetNativeMetadata{}, verify: verify}
	if ctx == nil || verify == nil {
		return lease, errGuardTargetMetadata
	}
	if err := ctx.Err(); err != nil {
		return lease, err
	}
	meta, err := openGuardTargetMetadataAt(ctx, path)
	if meta != nil {
		native, ok := meta.(*guardTargetNativeMetadata)
		if !ok {
			return lease, errors.Join(errGuardTargetMetadata, err)
		}
		lease.meta = native
	}
	if err != nil {
		// Transfer partial parent/leaf ownership to the caller's one lease
		// cleanup, including when native acquisition returned an error.
		return lease, err
	}
	// Metadata and body handles coexist. Read sharing is compatible and
	// neither permits writes/deletes; no release/reopen namespace gap exists.
	native := lease.meta
	before, err := native.Observe()
	if err != nil {
		return lease, err
	}
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return lease, errGuardTargetMetadata
	}
	h, err := syscall.CreateFile(p, syscall.GENERIC_READ|0x20000, syscall.FILE_SHARE_READ, nil, syscall.OPEN_EXISTING, 0x00200000, 0)
	if err != nil {
		return lease, errGuardTargetMetadata
	}
	lease.data = os.NewFile(uintptr(h), path)
	dataID, dataErr := fileSoftwareIdentity(lease.data)
	actual, plain := handlePath(h)
	dataACL, aclErr := updateFileACL(lease.data)
	dataInfo, statErr := lease.data.Stat()
	after, err := native.Observe()
	if err != nil || dataErr != nil || aclErr != nil || statErr != nil || !plain ||
		!sameCleanPath(actual, path) || dataID != before.identity || dataACL != before.acl ||
		dataInfo.Size() != before.size || after != before || !guardTargetMetadataShape(after) {
		return lease, errors.Join(errGuardTargetMetadata, err)
	}
	return lease, nil
}

func (l *guardSoftwareNativeLease) Observe() (guardTargetMetadata, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed || l.meta == nil {
		return guardTargetMetadata{}, errGuardTargetMetadata
	}
	return l.meta.Observe()
}

func (l *guardSoftwareNativeLease) Snapshot(ctx context.Context) (guardSoftwareSnapshot, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed || l.meta == nil || l.meta.leaf == nil || l.data == nil || ctx == nil || ctx.Err() != nil {
		return guardSoftwareSnapshot{}, guardTargetCandidateFailure(ctx, errGuardTargetMetadata)
	}
	first, err := l.meta.Observe()
	if err != nil || !guardTargetMetadataShape(first) {
		return guardSoftwareSnapshot{}, errors.Join(errGuardTargetMetadata, err)
	}
	dataID, idErr := fileSoftwareIdentity(l.data)
	dataACL, aclErr := updateFileACL(l.data)
	actual, plain := handlePath(syscall.Handle(l.data.Fd()))
	if idErr != nil || aclErr != nil || !plain || !sameCleanPath(actual, l.meta.leaf.Name()) || dataID != first.identity || dataACL != first.acl {
		return guardSoftwareSnapshot{}, errGuardTargetMetadata
	}
	raw, err := io.ReadAll(io.NewSectionReader(l.data, 0, first.size+1))
	defer clear(raw)
	if err != nil || int64(len(raw)) != first.size || ctx.Err() != nil {
		return guardSoftwareSnapshot{}, errors.Join(errGuardTargetMetadata, err, ctx.Err())
	}
	after, err := l.meta.Observe()
	afterID, idErr := fileSoftwareIdentity(l.data)
	afterACL, aclErr := updateFileACL(l.data)
	if err != nil || idErr != nil || aclErr != nil || after != first ||
		afterID != first.identity || afterACL != first.acl || l.verify == nil || !l.verify(raw) || ctx.Err() != nil {
		return guardSoftwareSnapshot{}, errors.Join(errGuardTargetMetadata, err, ctx.Err())
	}
	return guardSoftwareSnapshot{metadata: first, sha: digest(raw), revision: installedGuardRevision}, nil
}

// The ordinary file identity verifier deliberately rejects directories.
// Record an ancestor's identity from its same held directory handle without
// widening that verifier or treating this cleanup record as read authority.
func guardSoftwareOwnedParent(file *os.File) *softwareOwnedFile {
	owner := newSoftwareOwnedFile(file)
	owner.identity, owner.identityKnown = softwareIdentity{}, false
	if file == nil {
		return owner
	}
	var info syscall.ByHandleFileInformation
	if syscall.GetFileInformationByHandle(owner.handle, &info) == nil &&
		info.FileAttributes&syscall.FILE_ATTRIBUTE_DIRECTORY != 0 &&
		info.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT == 0 {
		owner.identity = softwareIdentity{
			Volume: strconv.FormatUint(uint64(info.VolumeSerialNumber), 10),
			FileID: strconv.FormatUint(uint64(info.FileIndexHigh)<<32|uint64(info.FileIndexLow), 10),
			Created: strconv.FormatUint(uint64(info.CreationTime.HighDateTime)<<32|uint64(info.CreationTime.LowDateTime), 10),
		}
		owner.identityKnown = true
	}
	return owner
}

func (l *guardSoftwareNativeLease) Close() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return l.closeError
	}
	l.closed = true
	var resources []io.Closer
	if l.meta != nil {
		for _, parent := range l.meta.parents {
			resources = append(resources, guardSoftwareOwnedParent(parent))
		}
		if l.meta.leaf != nil {
			resources = append(resources, newSoftwareOwnedFile(l.meta.leaf))
		}
	}
	if l.data != nil {
		resources = append(resources, newSoftwareOwnedFile(l.data))
	}
	report := closeSoftwareOwnedResources(resources)
	if report.Failed != 0 {
		l.pending = resources
		l.closeError = errors.Join(l.closeError, errGuardTargetCleanup, &softwareCleanupError{report: report})
	}
	if l.meta != nil {
		l.meta.closed = true
		l.meta.leaf, l.meta.parents, l.meta.paths = nil, nil, nil
	}
	l.data = nil
	return l.closeError
}

func guardSoftwareInspectWith(ctx context.Context, audit *ghaFreshCredentialAudit, acquire func(context.Context) (guardTargetArtifact, error)) guardSoftwareResult {
	failure := guardSoftwareResult{guardTargetMetadataResult: guardTargetMetadataResult{Domain: guardSoftwareDomain, Reason: "guard_target_artifact_unavailable"}, CredentialCleanupState: "unknown"}
	if ctx == nil || acquire == nil {
		return failure
	}
	artifact, err := acquire(ctx)
	if err != nil || artifact == nil {
		if artifact != nil && artifact.Close() != nil {
			failure.CleanupFailed = true
		}
		failure.Reason = ghaFreshAcquisitionReason(err)
		guardTargetObserveError(&failure.guardTargetMetadataResult, err)
		if audit != nil && (audit.state == "completed" || audit.state == "failed") {
			failure.CredentialCleanupState = audit.state
		}
		if errors.Is(err, errGHAGuardCredentialCleanup) || errors.Is(err, errGHAGuardResponseCleanup) || failure.CredentialCleanupState == "failed" {
			failure.CleanupFailed = true
		}
		return failure
	}
	purpose, err := guardSoftwarePurposeForArtifact(ctx, artifact)
	if err != nil || purpose == nil {
		failure.Reason = "guard_target_purpose_unavailable"
		guardTargetObserveError(&failure.guardTargetMetadataResult, err)
		releaseGuardSoftwareResult(&failure, ctx, artifact, purpose)
		return failure
	}
	result := inspectGuardSoftware(ctx, artifact, purpose)
	if audit != nil && (audit.state == "completed" || audit.state == "failed") {
		result.CredentialCleanupState = audit.state
	}
	if result.CredentialCleanupState != "completed" {
		wasQualified := result.Qualified
		result.Qualified, result.SoftwareObserved = false, false
		result.SHA256, result.SourceRevision = "", ""
		if wasQualified {
			result.Reason = "guard_github_credential_cleanup_unobserved"
		}
		if result.CredentialCleanupState == "failed" {
			result.CleanupFailed = true
			if wasQualified {
				result.Reason = "guard_github_credential_cleanup_failed"
			}
		}
	}
	return result
}

func runGuardSoftwareWorker(args []string, out io.Writer) int {
	if len(args) != 1 || args[0] != guardSoftwareWorkerFlag || out == nil {
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	audit := &ghaFreshCredentialAudit{state: "unknown"}
	result := guardSoftwareInspectWith(ctx, audit, func(c context.Context) (guardTargetArtifact, error) {
		return guardTargetArtifactFromServer(c, func(inner context.Context) (*ghaGuardVerifiedCandidate, error) {
			return ghaFreshOpenVerifiedCandidateObserved(inner, audit)
		})
	})
	if json.NewEncoder(out).Encode(result) != nil || !result.Qualified {
		return 1
	}
	return 0
}

func guardSoftwareInspectionFromChild(child askChildResult) guardSoftwareInspectionResult {
	r := guardSoftwareInspectionResult{guardSoftwareResult: guardSoftwareResult{guardTargetMetadataResult: guardTargetMetadataResult{Domain: guardSoftwareDomain, Reason: "guard_target_worker_unqualified"}, CredentialCleanupState: "unknown"},
		WorkerStarted: child.Started, WorkerAssigned: child.Assigned, WorkerWaitVerified: child.WaitVerified, WorkerReaped: child.Reaped, WorkerTimedOut: child.TimedOut}
	if !child.Started || !child.Assigned || !child.WaitVerified || !child.Reaped || child.TimedOut ||
		!child.ResultStdoutComplete || !child.ResultStderrComplete ||
		(child.Exit != 0 && child.Exit != 1) || len(child.Raw) == 0 || len(child.Raw) > 65536 {
		r.Cancelled = child.TimedOut
		r.CleanupFailed = child.Started && !child.Reaped
		return r
	}
	raw := bytes.TrimSpace(child.Raw)
	if _, err := ghaFreshObject(raw); err != nil {
		return r
	}
	var value guardSoftwareResult
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if dec.Decode(&value) != nil || dec.Decode(new(any)) != io.EOF {
		return r
	}
	canonical, err := json.Marshal(value)
	if err != nil || !bytes.Equal(raw, canonical) || value.Domain != guardSoftwareDomain ||
		value.SoftwareReplaceAllowed || value.DataAccessGranted || value.UpdatePerformed {
		return r
	}
	if child.Exit == 1 {
		if value.Qualified || value.SoftwareObserved || value.SHA256 != "" || value.SourceRevision != "" {
			return r
		}
		if value.CredentialCleanupState != "unknown" && value.CredentialCleanupState != "completed" && value.CredentialCleanupState != "failed" {
			return r
		}
		switch value.Reason {
		case "guard_target_artifact_unavailable", "guard_target_purpose_unavailable",
			"guard_target_metadata_unavailable", "guard_target_software_unverified",
			"guard_target_generation_changed", "guard_target_cancelled", "guard_target_cleanup_failed",
			"guard_github_credential_cleanup_unobserved", "guard_github_credential_cleanup_failed",
			"guard_github_credential_not_found", "guard_github_credential_access_denied",
			"guard_github_credential_identity_mismatch", "guard_github_credential_unauthorized",
			"guard_github_credential_forbidden", "guard_github_artifact_expired",
			"guard_github_source_read_failed", "guard_github_response_cleanup_failed":
			r.Reason = value.Reason
		default:
			if !ghaFreshHTTPReasonAllowed(value.Reason) {
				return r
			}
			r.Reason = value.Reason
		}
		r.CredentialCleanupState = value.CredentialCleanupState
		r.Cancelled, r.CleanupFailed = value.Cancelled, value.CleanupFailed
		r.WorkerResultObserved = true
		return r
	}
	id := value.CurrentIdentity
	if err != nil || !bytes.Equal(raw, canonical) || !value.Qualified || !value.MetadataObserved || !value.SoftwareObserved ||
		value.Domain != guardSoftwareDomain || value.Reason != "verified_guard_target_software" ||
		id == nil || !ghaFreshPositiveDecimal(id.Volume) || !ghaFreshPositiveDecimal(id.FileID) || !ghaFreshPositiveDecimal(id.Created) ||
		!ghaGuardHex(value.ACLHash, 64) || !ghaGuardHex(value.ParentProof, 64) || value.Bytes <= 0 || value.Bytes > 64<<20 ||
		value.SHA256 != installedGuardSHA || value.SourceRevision != installedGuardRevision ||
		value.CredentialCleanupState != "completed" || value.Cancelled || value.CleanupFailed ||
		value.SoftwareReplaceAllowed || value.DataAccessGranted || value.UpdatePerformed {
		return r
	}
	r.guardSoftwareResult, r.WorkerResultObserved = value, true
	return r
}

func guardSoftwareSupervisedInspection() guardSoftwareInspectionResult {
	return guardSoftwareSupervisedInspectionWith(askOwnedChild)
}

func guardSoftwareSupervisedInspectionWith(runChild func(context.Context, string, ...string) askChildResult) guardSoftwareInspectionResult {
	executable, err := os.Executable()
	if err != nil || runChild == nil {
		return guardSoftwareInspectionResult{guardSoftwareResult: guardSoftwareResult{guardTargetMetadataResult: guardTargetMetadataResult{Domain: guardSoftwareDomain, Reason: "guard_target_worker_unqualified"}, CredentialCleanupState: "unknown"}}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute+30*time.Second)
	ctx = context.WithValue(ctx, askStrictJobKey{}, true)
	ctx = context.WithValue(ctx, askResultCompleteKey{}, true)
	child := runChild(ctx, executable, guardSoftwareWorkerFlag)
	cancel()
	defer clear(child.Raw)
	return guardSoftwareInspectionFromChild(child)
}

func runGuardSoftwareInspectionWith(args []string, out io.Writer, inspect func() guardSoftwareInspectionResult) int {
	if len(args) != 1 || args[0] != guardSoftwareInspectionFlag || out == nil || inspect == nil {
		return 2
	}
	result := inspect()
	if json.NewEncoder(out).Encode(result) != nil || !result.Qualified {
		return 1
	}
	return 0
}

func runGuardSoftwareInspection(args []string, out io.Writer) int {
	return runGuardSoftwareInspectionWith(args, out, guardSoftwareSupervisedInspection)
}
