//go:build windows

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const guardTargetSoftwarePath = "D:/CC/Tools/client-records/scrub391.exe"
const guardTargetUpdateDomain = "owner-guard-target-metadata-v1"

var errGuardTargetMetadata = errors.New("guard_target_metadata_unavailable")
var errGuardTargetCleanup = errors.New("guard_target_cleanup_failed")
var errGuardTargetArtifactUnavailable = errors.New("guard_target_artifact_unavailable")
var errGuardTargetPurposeUnavailable = errors.New("guard_target_purpose_unavailable")

// Only the fixed normal factory supplies the concrete verified candidate.
// This private seam exists for owned synthetic controls, not CLI/JSON input.
type guardTargetArtifact interface {
	VerifySourceGeneration() bool
	WithPinnedPayload(func([]byte) error) error
	Close() error
}

type guardTargetMetadata struct {
	identity softwareIdentity
	acl      string
	parent   string
	size     int64
}

type guardTargetMetadataLease interface {
	Observe() (guardTargetMetadata, error)
	Close() error
}

type guardTargetMetadataResult struct {
	Qualified              bool              `json:"qualified"`
	Reason                 string            `json:"reason"`
	Domain                 string            `json:"domain"`
	MetadataObserved       bool              `json:"metadata_observed"`
	CurrentIdentity        *softwareIdentity `json:"current_identity,omitempty"`
	ACLHash                string            `json:"acl_sha256,omitempty"`
	ParentProof            string            `json:"parent_proof,omitempty"`
	Bytes                  int64             `json:"bytes,omitempty"`
	Cancelled              bool              `json:"cancelled"`
	CleanupFailed          bool              `json:"cleanup_failed"`
	SoftwareReplaceAllowed bool              `json:"software_replace_allowed"`
	DataAccessGranted      bool              `json:"data_access_granted"`
	UpdatePerformed        bool              `json:"update_performed"`
}

func guardTargetMetadataShape(v guardTargetMetadata) bool {
	positive := func(s string) bool { n, err := strconv.ParseUint(s, 10, 64); return err == nil && n > 0 && strconv.FormatUint(n, 10) == s }
	return positive(v.identity.Volume) && positive(v.identity.FileID) && positive(v.identity.Created) &&
		v.acl != "" && len(v.parent) == 64 && v.size > 0 && v.size <= 64<<20
}

func guardTargetErrorReason(err error, fallback string) string {
	if err == context.Canceled || err == context.DeadlineExceeded {
		return "guard_target_cancelled"
	}
	return fallback
}

type guardTargetPurposeLease interface {
	Recheck(context.Context, guardTargetArtifact) error
	OpenMetadata(context.Context) (guardTargetMetadataLease, error)
	Close() error
}

// These two normal factories are intentionally not created from JSON, hashes,
// old installer pins, a caller boolean, or a claimed owner identity. Their
// source adapters need separate review with the genuine producer capability.
func guardTargetOpenNormalArtifact(ctx context.Context) (guardTargetArtifact, error) {
	return guardTargetNormalArtifactFromServer(ctx)
}
func guardTargetOpenNormalPurpose(ctx context.Context, artifact guardTargetArtifact) (guardTargetPurposeLease, error) {
	return guardTargetNormalPurposeForArtifact(ctx, artifact)
}

func guardTargetObserveError(r *guardTargetMetadataResult, err error) {
	r.Cancelled = r.Cancelled || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
	r.CleanupFailed = r.CleanupFailed || errors.Is(err, errGuardTargetCleanup)
}

func guardTargetCloseResult(r *guardTargetMetadataResult, err error) {
	if err != nil { r.CleanupFailed = true; guardTargetObserveError(r, err) }
}

func guardTargetReleaseMetadataResult(r *guardTargetMetadataResult, ctx context.Context, artifact guardTargetArtifact, purpose guardTargetPurposeLease) {
	if purpose != nil { guardTargetCloseResult(r, purpose.Close()) }
	if artifact != nil { guardTargetCloseResult(r, artifact.Close()) }
	if ctx != nil { guardTargetObserveError(r, ctx.Err()) }
	if r.Cancelled || r.CleanupFailed {
		r.Qualified = false
		if r.Reason == "verified_guard_target_metadata" {
			if r.CleanupFailed { r.Reason = "guard_target_cleanup_failed" } else { r.Reason = "guard_target_cancelled" }
		}
	}
}

// Both resources are transferred into this controller and closed once. A
// verified producer alone never supplies the independent target purpose lease.
func inspectGuardTargetMetadata(ctx context.Context, artifact guardTargetArtifact, purpose guardTargetPurposeLease) (r guardTargetMetadataResult) {
	r.Domain = guardTargetUpdateDomain
	r.Reason = "guard_target_artifact_unavailable"
	defer func() { guardTargetReleaseMetadataResult(&r, ctx, artifact, purpose) }()
	if ctx == nil || artifact == nil { return }
	if ctx.Err() != nil { r.Reason = "guard_target_cancelled"; return }
	if !artifact.VerifySourceGeneration() { return }
	if purpose == nil { r.Reason = "guard_target_purpose_unavailable"; return }
	if err := purpose.Recheck(ctx, artifact); err != nil {
		guardTargetObserveError(&r, err)
		r.Reason = guardTargetErrorReason(err, "guard_target_purpose_unavailable")
		return
	}
	calls := 0
	callbackSucceeded := false
	err := artifact.WithPinnedPayload(func(payload []byte) error {
		calls++
		if calls != 1 || len(payload) == 0 || ctx.Err() != nil {
			r.Reason = guardTargetErrorReason(ctx.Err(), "guard_target_artifact_unavailable")
			return errGuardTargetMetadata
		}
		if err := purpose.Recheck(ctx, artifact); err != nil {
			guardTargetObserveError(&r, err)
			r.Reason = guardTargetErrorReason(err, "guard_target_purpose_unavailable")
			return errGuardTargetMetadata
		}
		r.Reason = "guard_target_metadata_unavailable"
		lease, openErr := purpose.OpenMetadata(ctx)
		if lease != nil {
			defer func() { guardTargetCloseResult(&r, lease.Close()) }()
		}
		if openErr != nil || lease == nil {
			guardTargetObserveError(&r, openErr)
			r.Reason = guardTargetErrorReason(openErr, "guard_target_metadata_unavailable")
			return errGuardTargetMetadata
		}
		first, observeErr := lease.Observe()
		if observeErr != nil || !guardTargetMetadataShape(first) {
			guardTargetObserveError(&r, observeErr)
			r.Reason = guardTargetErrorReason(observeErr, "guard_target_metadata_unavailable")
			return errGuardTargetMetadata
		}
		id := first.identity
		r.MetadataObserved = true
		r.CurrentIdentity = &id
		r.ACLHash = digest([]byte(first.acl))
		r.ParentProof = first.parent
		r.Bytes = first.size
		if ctx.Err() != nil { r.Reason = "guard_target_cancelled"; return ctx.Err() }
		after, observeErr := lease.Observe()
		if observeErr != nil {
			guardTargetObserveError(&r, observeErr)
			r.Reason = guardTargetErrorReason(observeErr, "guard_target_metadata_unavailable")
			return errGuardTargetMetadata
		}
		if after != first { r.Reason = "guard_target_generation_changed"; return errGuardTargetMetadata }
		r.Reason = "verified_guard_target_metadata"
		callbackSucceeded = true
		return nil
	})
	guardTargetObserveError(&r, err)
	if err != nil || calls != 1 || !callbackSucceeded {
		if r.Reason == "verified_guard_target_metadata" { r.Reason = guardTargetErrorReason(err, "guard_target_artifact_unavailable") }
		return
	}
	if !artifact.VerifySourceGeneration() { r.Reason = "guard_target_artifact_unavailable"; return }
	if err := purpose.Recheck(ctx, artifact); err != nil {
		guardTargetObserveError(&r, err)
		r.Reason = guardTargetErrorReason(err, "guard_target_purpose_unavailable")
		return
	}
	if ctx.Err() != nil { r.Reason = "guard_target_cancelled"; return }
	if r.CleanupFailed { r.Reason = "guard_target_cleanup_failed"; return }
	r.Qualified = true
	return
}

type guardTargetMetadataFactories struct {
	artifact func(context.Context) (guardTargetArtifact, error)
	purpose  func(context.Context, guardTargetArtifact) (guardTargetPurposeLease, error)
}

// The normal route always uses these fixed factories. The private factory
// seam lets tests exercise partial acquisition without constructing authority.
func observeNormalGuardTargetMetadata(ctx context.Context) guardTargetMetadataResult {
	return observeGuardTargetMetadataWith(ctx, guardTargetMetadataFactories{
		artifact: guardTargetOpenNormalArtifact,
		purpose: guardTargetOpenNormalPurpose,
	})
}

func observeGuardTargetMetadataWith(ctx context.Context, factories guardTargetMetadataFactories) (r guardTargetMetadataResult) {
	r.Domain = guardTargetUpdateDomain
	r.Reason = "guard_target_artifact_unavailable"
	var artifact guardTargetArtifact
	var purpose guardTargetPurposeLease
	transferred := false
	defer func() {
		if !transferred { guardTargetReleaseMetadataResult(&r, ctx, artifact, purpose) }
	}()
	if ctx == nil || factories.artifact == nil || factories.purpose == nil { return }
	if ctx.Err() != nil { r.Reason = "guard_target_cancelled"; return }
	var artifactErr error
	artifact, artifactErr = factories.artifact(ctx)
	guardTargetObserveError(&r, artifactErr)
	if artifactErr != nil || artifact == nil {
		r.Reason = guardTargetErrorReason(artifactErr, "guard_target_artifact_unavailable")
		return
	}
	if ctx.Err() != nil { r.Reason = "guard_target_cancelled"; return }
	if !artifact.VerifySourceGeneration() { return }
	var purposeErr error
	purpose, purposeErr = factories.purpose(ctx, artifact)
	guardTargetObserveError(&r, purposeErr)
	if purposeErr != nil || purpose == nil {
		r.Reason = guardTargetErrorReason(purposeErr, "guard_target_purpose_unavailable")
		return
	}
	transferred = true
	return inspectGuardTargetMetadata(ctx, artifact, purpose)
}

// Not routed by the public dispatcher until the concrete authority adapters
// and the exact software purpose have been reviewed and qualified.
func runGuardTargetMetadata(args []string, out io.Writer) int {
	if len(args) != 1 || args[0] != "--inspect-guard-target-update" { return 2 }
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r := observeNormalGuardTargetMetadata(ctx)
	if json.NewEncoder(out).Encode(r) != nil || !r.Qualified { return 1 }
	return 0
}

type guardTargetNativeMetadata struct {
	leaf    *os.File
	parents []*os.File
	paths   []string
	closed  bool
}

func openGuardTargetMetadata(ctx context.Context) (guardTargetMetadataLease, error) {
	return openGuardTargetMetadataAt(ctx, guardTargetSoftwarePath)
}

// Only the normal function above supplies a production path. Tests use an
// owned fixture. No public argument or receipt can select this path.
func openGuardTargetMetadataAt(ctx context.Context, fixedPath string) (guardTargetMetadataLease, error) {
	lease := &guardTargetNativeMetadata{}
	if ctx == nil { return lease, errGuardTargetMetadata }
	if err := ctx.Err(); err != nil { return lease, err }
	var paths []string
	for p := filepath.Dir(fixedPath); ; p = filepath.Dir(p) {
		paths = append(paths, p)
		if filepath.Dir(p) == p {
			break
		}
	}
	if len(paths) > 64 {
		return lease, errGuardTargetMetadata
	}
	for i := len(paths) - 1; i >= 0; i-- {
		file, err := guardTargetMetadataHandle(paths[i], true)
		if err != nil {
			return lease, err
		}
		lease.parents = append(lease.parents, file)
		lease.paths = append(lease.paths, paths[i])
		if ctx.Err() != nil {
			return lease, ctx.Err()
		}
	}
	file, err := guardTargetMetadataHandle(fixedPath, false)
	if err != nil {
		return lease, err
	}
	lease.leaf = file
	return lease, nil
}

func guardTargetMetadataHandle(path string, directory bool) (*os.File, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, errGuardTargetMetadata
	}
	flags := uint32(0x00200000)
	share := uint32(syscall.FILE_SHARE_READ)
	if directory {
		flags |= 0x02000000
		share |= syscall.FILE_SHARE_WRITE
	}
	// FILE_READ_ATTRIBUTES | READ_CONTROL; no FILE_READ_DATA or write access.
	handle, err := syscall.CreateFile(p, 0x20080, share, nil, syscall.OPEN_EXISTING, flags, 0)
	if err != nil {
		return nil, errGuardTargetMetadata
	}
	file := os.NewFile(uintptr(handle), path)
	var info syscall.ByHandleFileInformation
	actual, good := handlePath(handle)
	err = syscall.GetFileInformationByHandle(handle, &info)
	if err != nil || !good || !sameCleanPath(actual, path) ||
		info.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0 ||
		(info.FileAttributes&syscall.FILE_ATTRIBUTE_DIRECTORY != 0) != directory ||
		(!directory && info.NumberOfLinks != 1) {
		closeErr := file.Close()
		if closeErr != nil { return nil, errors.Join(errGuardTargetMetadata, errGuardTargetCleanup) }
		return nil, errGuardTargetMetadata
	}
	return file, nil
}

func (lease *guardTargetNativeMetadata) Observe() (guardTargetMetadata, error) {
	if lease == nil || lease.closed || lease.leaf == nil {
		return guardTargetMetadata{}, errGuardTargetMetadata
	}
	var proof strings.Builder
	for i, parent := range lease.parents {
		var info syscall.ByHandleFileInformation
		if syscall.GetFileInformationByHandle(syscall.Handle(parent.Fd()), &info) != nil {
			return guardTargetMetadata{}, errGuardTargetMetadata
		}
		actual, ok := handlePath(syscall.Handle(parent.Fd()))
		if !ok || !sameCleanPath(actual, lease.paths[i]) || info.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
			return guardTargetMetadata{}, errGuardTargetMetadata
		}
		acl, err := updateFileACL(parent)
		if err != nil {
			return guardTargetMetadata{}, errGuardTargetMetadata
		}
		fmt.Fprintf(&proof, "%d:%d:%d:%s;", info.VolumeSerialNumber,
			uint64(info.FileIndexHigh)<<32|uint64(info.FileIndexLow),
			uint64(info.CreationTime.HighDateTime)<<32|uint64(info.CreationTime.LowDateTime), digest([]byte(acl)))
	}
	id, err := fileSoftwareIdentity(lease.leaf)
	if err != nil {
		return guardTargetMetadata{}, errGuardTargetMetadata
	}
	actual, ok := handlePath(syscall.Handle(lease.leaf.Fd()))
	if !ok || !sameCleanPath(actual, lease.leaf.Name()) {
		return guardTargetMetadata{}, errGuardTargetMetadata
	}
	info, err := lease.leaf.Stat()
	if err != nil {
		return guardTargetMetadata{}, errGuardTargetMetadata
	}
	acl, err := updateFileACL(lease.leaf)
	if err != nil {
		return guardTargetMetadata{}, errGuardTargetMetadata
	}
	return guardTargetMetadata{identity: id, acl: acl, parent: digest([]byte(proof.String())), size: info.Size()}, nil
}

func (lease *guardTargetNativeMetadata) Close() error {
	if lease == nil || lease.closed {
		return nil
	}
	lease.closed = true
	failed := false
	if lease.leaf != nil {
		failed = lease.leaf.Close() != nil
		lease.leaf = nil
	}
	for i := len(lease.parents) - 1; i >= 0; i-- {
		if lease.parents[i].Close() != nil {
			failed = true
		}
	}
	lease.parents = nil
	lease.paths = nil
	if failed {
		return errGuardTargetCleanup
	}
	return nil
}
