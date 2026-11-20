//go:build windows

package main

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"syscall"
	"unsafe"
)

// A transaction keeps both executable objects and all ancestor handles until
// observation or rollback completes. Failed Close objects are not retried.
type watchdogNative struct {
	pins *watchdogPinScope
	target string
	source string
	origin watchdogOrigin
	expected watchdogFileIdentity
	candidateID watchdogFileIdentity
	old, candidate, stage *pin
	stageWriter *pin
	stageWriterCloseAttempted bool
	ancestorPins []*pin
	state watchdogSteps
	closed bool
	closeErr error
	retainedDescriptors []unsafe.Pointer
	// Synthetic fault seam; normal preparation always uses renameNoReplace.
	rename func(*pin, string) error
}

func watchdogSecurityHeld(p *pin, retained *[]unsafe.Pointer) (string, error) {
	var owner, group, dacl, sd unsafe.Pointer
	code, _, _ := advapi.NewProc("GetSecurityInfo").Call(p.f.Fd(), 1, 7,
		uintptr(unsafe.Pointer(&owner)), uintptr(unsafe.Pointer(&group)),
		uintptr(unsafe.Pointer(&dacl)), 0, uintptr(unsafe.Pointer(&sd)))
	if code != 0 {
		return "", errors.New("watchdog_security_read_failed")
	}
	var operationErr error
	digest := sha256.New()
	appendSID := func(v unsafe.Pointer) {
		if operationErr != nil {
			return
		}
		if v == nil {
			operationErr = errors.New("watchdog_security_sid_invalid")
			return
		}
		valid, _, _ := advapi.NewProc("IsValidSid").Call(uintptr(v))
		if valid == 0 {
			operationErr = errors.New("watchdog_security_sid_invalid")
			return
		}
		n, _, _ := advapi.NewProc("GetLengthSid").Call(uintptr(v))
		if n < 8 || n > 1024 {
			operationErr = errors.New("watchdog_security_sid_invalid")
			return
		}
		var size [8]byte
		binary.LittleEndian.PutUint64(size[:], uint64(n))
		digest.Write(size[:])
		digest.Write(unsafe.Slice((*byte)(v), int(n)))
	}
	appendSID(owner)
	appendSID(group)
	var control uint16
	var revision uint32
	ok, _, _ := advapi.NewProc("GetSecurityDescriptorControl").Call(uintptr(sd),
		uintptr(unsafe.Pointer(&control)), uintptr(unsafe.Pointer(&revision)))
	if ok == 0 {
		operationErr = errors.Join(operationErr, errors.New("watchdog_security_control_failed"))
	}
	var flags [2]byte
	binary.LittleEndian.PutUint16(flags[:], control&0x1000)
	digest.Write(flags[:])
	if dacl == nil {
		// A null DACL is distinct from an empty DACL.
		digest.Write([]byte{0})
	} else {
		digest.Write([]byte{1})
		n := *(*uint16)(unsafe.Add(dacl, 2))
		if n < 8 {
			operationErr = errors.Join(operationErr, errors.New("watchdog_security_acl_invalid"))
		} else {
			digest.Write(unsafe.Slice((*byte)(dacl), int(n)))
		}
	}
	freed, _, _ := kernel.NewProc("LocalFree").Call(uintptr(sd))
	if freed != 0 {
		if retained != nil {
			*retained = append(*retained, sd)
		}
		operationErr = errors.Join(operationErr, errors.New("watchdog_security_release_failed"))
	}
	if operationErr != nil {
		return "", operationErr
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func watchdogSecurity(p *pin) (string, error) {
	return watchdogSecurityHeld(p, nil)
}

func watchdogIdentity(p *pin) (watchdogFileIdentity, error) {
	return watchdogIdentityHeld(p, nil)
}

func watchdogIdentityHeld(p *pin, retained *[]unsafe.Pointer) (watchdogFileIdentity, error) {
	var i syscall.ByHandleFileInformation
	if p == nil || p.f == nil ||
		syscall.GetFileInformationByHandle(syscall.Handle(p.f.Fd()), &i) != nil ||
		i.FileAttributes&(0x10|0x400) != 0 || i.NumberOfLinks != 1 {
		return watchdogFileIdentity{}, errors.New("watchdog_identity_unavailable")
	}
	hash, err := p.hash()
	if err != nil {
		return watchdogFileIdentity{}, err
	}
	security, err := watchdogSecurityHeld(p, retained)
	if err != nil {
		return watchdogFileIdentity{}, err
	}
	v := watchdogFileIdentity{
		Volume: i.VolumeSerialNumber,
		File: uint64(i.FileIndexHigh)<<32 | uint64(i.FileIndexLow),
		Created: uint64(i.CreationTime.HighDateTime)<<32 | uint64(i.CreationTime.LowDateTime),
		Bytes: int64(i.FileSizeHigh)<<32 | int64(i.FileSizeLow),
		Links: i.NumberOfLinks, SHA256: hash, SecuritySHA256: security,
	}
	if !v.valid() {
		return watchdogFileIdentity{}, errors.New("watchdog_identity_invalid")
	}
	return v, nil
}

func (n *watchdogNative) identity(p *pin) (watchdogFileIdentity, error) {
	return watchdogIdentityHeld(p, &n.retainedDescriptors)
}
func (n *watchdogNative) copySecurity(from, to *pin) error {
	var owner, group, dacl, sd unsafe.Pointer
	code, _, _ := advapi.NewProc("GetSecurityInfo").Call(from.f.Fd(), 1, 7,
		uintptr(unsafe.Pointer(&owner)), uintptr(unsafe.Pointer(&group)),
		uintptr(unsafe.Pointer(&dacl)), 0, uintptr(unsafe.Pointer(&sd)))
	if code != 0 {
		return errors.New("watchdog_security_read_failed")
	}
	var control uint16
	var revision uint32
	ok, _, _ := advapi.NewProc("GetSecurityDescriptorControl").Call(uintptr(sd),
		uintptr(unsafe.Pointer(&control)), uintptr(unsafe.Pointer(&revision)))
	var operationErr error
	if ok == 0 {
		operationErr = errors.New("watchdog_security_control_failed")
	} else {
		flags := uintptr(7 | 0x20000000)
		if control&0x1000 != 0 {
			flags = 7 | 0x80000000
		}
		code, _, _ = advapi.NewProc("SetSecurityInfo").Call(to.f.Fd(), 1, flags,
			uintptr(owner), uintptr(group), uintptr(dacl), 0)
		if code != 0 {
			operationErr = errors.New("watchdog_security_copy_failed")
		}
	}
	freed, _, _ := kernel.NewProc("LocalFree").Call(uintptr(sd))
	if freed != 0 {
		n.retainedDescriptors = append(n.retainedDescriptors, sd)
		operationErr = errors.Join(operationErr, errors.New("watchdog_security_release_failed"))
	}
	return operationErr
}

// The fixed normal entry cannot use a caller-selected target. Issuer origin
// is required before even the first installed handle is opened.
func prepareWatchdogNative(ctx context.Context, origin watchdogOrigin, artifact watchdogFileIdentity, source string) (*watchdogNative, error) {
	if !origin.valid() || !artifact.valid() {
		return nil, errors.New("watchdog_origin_or_artifact_unbound")
	}
	return prepareWatchdogNativeAt(ctx, watchdogTarget, origin, artifact, source)
}

// Package-private owned fixture seam. Normal code above always uses the one
// fixed target. This function does not issue purpose or artifact authority.
func prepareWatchdogNativeAt(ctx context.Context, target string, origin watchdogOrigin, artifact watchdogFileIdentity, source string) (*watchdogNative, error) {
	if ctx == nil || ctx.Err() != nil || !origin.valid() || !artifact.valid() {
		return nil, errors.New("watchdog_inputs_unbound")
	}
	n := &watchdogNative{target: target, source: source, origin: origin, expected: artifact, rename: renameNoReplace, pins:newWatchdogPinScope()}
	fail := func(e error) (*watchdogNative, error) {
		// Return partial ownership for the controller's single Close path.
		return n, e
	}
	var err error
	n.ancestorPins, err = n.pins.parents(target)
	if err != nil {
		return fail(err)
	}
	n.old, err = n.pins.open(target, syscall.GENERIC_READ|0x20000|0x10000, 1, 3)
	if err != nil {
		return fail(err)
	}
	more, err := n.pins.parents(source)
	n.ancestorPins = append(n.ancestorPins, more...)
	if err != nil {
		return fail(err)
	}
	n.candidate, err = n.pins.open(source, syscall.GENERIC_READ|0x20000, 1, 3)
	if err != nil {
		return fail(err)
	}
	if err = n.Check(ctx); err != nil {
		return fail(err)
	}
	return n, nil
}

func (n *watchdogNative) backup() string { return n.target + ".before-" + watchdogGeneration }
func (n *watchdogNative) stageName() string { return n.target + ".stage-" + watchdogGeneration }
func (n *watchdogNative) failed() string { return n.target + ".failed-" + watchdogGeneration }

func (n *watchdogNative) Check(ctx context.Context) error {
	if err := n.checkInputs(ctx); err != nil {
		return err
	}
	if !missing(n.backup()) || !missing(n.stageName()) || !missing(n.failed()) {
		return errors.New("watchdog_previous_generation_retained")
	}
	return nil
}

func (n *watchdogNative) checkInputs(ctx context.Context) error {
	if n.closed || ctx.Err() != nil || n.old == nil || n.candidate == nil ||
		!n.old.at(n.target) || !n.candidate.at(n.source) {
		return errors.New("watchdog_held_inputs_changed")
	}
	old, err := n.identity(n.old)
	if err != nil || old != n.origin.Installed {
		return errors.New("watchdog_predecessor_origin_mismatch")
	}
	artifact, err := n.identity(n.candidate)
	if err != nil || artifact != n.expected {
		return errors.New("watchdog_artifact_changed")
	}
	return nil
}

func (n *watchdogNative) Stage(ctx context.Context) error {
	if err := n.Check(ctx); err != nil {
		return err
	}
	if n.stage != nil {
		return errors.New("watchdog_stage_already_owned")
	}
	stage, err := n.pins.open(n.stageName(), syscall.GENERIC_READ|syscall.GENERIC_WRITE|
		0x10000|0x20000|0x40000|0x80000, 1, 1)
	if stage != nil {
		n.stage = stage
		n.state.StageCreated = true
	}
	if err != nil {
		return err
	}
	if _, err = n.candidate.f.Seek(0, 0); err != nil {
		return err
	}
	written, err := io.Copy(n.stage.f, io.LimitReader(n.candidate.f, n.expected.Bytes+1))
	if err != nil || written != n.expected.Bytes {
		return errors.New("watchdog_stage_write_failed")
	}
	if err = n.stage.f.Sync(); err != nil {
		return err
	}
	if err = n.copySecurity(n.old, n.stage); err != nil {
		return err
	}
	id, err := n.identity(n.stage)
	if err != nil || id.Bytes != n.expected.Bytes || id.SHA256 != n.expected.SHA256 ||
		id.SecuritySHA256 != n.origin.Installed.SecuritySHA256 {
		return errors.New("watchdog_stage_readback_failed")
	}
	n.candidateID = id
	// An executable cannot be launched while its staging write handle remains
	// open. Close it once, reopen without write access, then require the same
	// full identity and bytes. This checked reopen interval is not an atomic
	// namespace lease against an uncooperative same-user writer.
	n.stageWriter = n.stage
	n.stageWriterCloseAttempted = true
	if err = n.pins.closePin(n.stageWriter); err != nil {
		n.closeErr = err
		n.stage = nil
		return err
	}
	n.stage = nil
	n.stage, err = n.pins.open(n.stageName(), syscall.GENERIC_READ|0x20000|0x10000, 1, 3)
	if err != nil {
		return err
	}
	sealed, err := n.identity(n.stage)
	if err != nil || sealed != id {
		return errors.New("watchdog_sealed_stage_changed")
	}
	return nil
}

func (n *watchdogNative) Candidate() watchdogFileIdentity { return n.candidateID }
func (n *watchdogNative) Steps() watchdogSteps { return n.state }

func watchdogSameHeld(a, b watchdogFileIdentity) bool {
	// Windows may change CreationTime when renaming this same held file.
	// The original object stays open, preventing file ID reuse.
	a.Created, b.Created = 0, 0
	return a == b
}

func (n *watchdogNative) Publish(ctx context.Context) error {
	if n.closed || ctx.Err() != nil || n.stage == nil || !n.candidateID.valid() ||
		n.state.OldRetired || n.state.Published {
		return errors.New("watchdog_publish_state_invalid")
	}
	if err := n.checkInputs(ctx); err != nil {
		return err
	}
	if !missing(n.backup()) || !missing(n.failed()) || n.pins.verifyName(n.stage, n.stageName()) != nil {
		return errors.New("watchdog_namespace_changed")
	}
	id, err := n.identity(n.stage)
	if err != nil || id != n.candidateID {
		return errors.New("watchdog_stage_changed")
	}
	if n.pins.verifyName(n.old, n.target) != nil || !n.old.at(n.target) {
		return errors.New("watchdog_predecessor_name_changed")
	}
	n.state.OldRetireAttempted = true
	if err = n.rename(n.old, n.backup()); err != nil {
		n.state.NamespaceUnknown = true
		return err
	}
	n.state.OldRetired = true
	n.state.PublicName = "absent"
	if ctx.Err() != nil {
		return ctx.Err()
	}
	n.state.PublishAttempted = true
	if err = n.rename(n.stage, n.target); err != nil {
		n.state.NamespaceUnknown = true
		n.state.PublicName = namespaceState(n.target)
		return err
	}
	n.state.Published = true
	n.state.PublicName = "present"
	if err = n.pins.verifyName(n.stage, n.target); err != nil {
		return err
	}
	id, err = n.identity(n.stage)
	if err != nil || !watchdogSameHeld(id, n.candidateID) {
		return errors.New("watchdog_installed_readback_failed")
	}
	// Observe uses the actual post-rename identity, including current creation.
	n.candidateID = id
	old, err := n.identity(n.old)
	if err != nil || !watchdogSameHeld(old, n.origin.Installed) || n.pins.verifyName(n.old, n.backup()) != nil {
		return errors.New("watchdog_backup_changed")
	}
	n.state.Verified = true
	return nil
}

func (n *watchdogNative) Rollback(ctx context.Context) error {
	n.state.RestoreAttempted = true
	if n.closed || ctx.Err() != nil || !n.state.OldRetired ||
		n.old == nil || n.pins.verifyName(n.old, n.backup()) != nil || !missing(n.failed()) {
		return errors.New("watchdog_rollback_precondition_failed")
	}
	old, err := n.identity(n.old)
	if err != nil || !watchdogSameHeld(old, n.origin.Installed) {
		return errors.New("watchdog_backup_changed")
	}
	if n.state.Published {
		if n.stage == nil || n.pins.verifyName(n.stage, n.target) != nil {
			return errors.New("watchdog_public_name_changed")
		}
		current, err := n.identity(n.stage)
		if err != nil || !watchdogSameHeld(current, n.candidateID) {
			return errors.New("watchdog_published_object_changed")
		}
		if err := n.rename(n.stage, n.failed()); err != nil {
			return err
		}
		n.state.CandidateRetired = true
	}
	if !missing(n.target) {
		return errors.New("watchdog_restore_name_not_absent")
	}
	if err := n.rename(n.old, n.target); err != nil {
		n.state.NamespaceUnknown = true
		n.state.PublicName = namespaceState(n.target)
		return err
	}
	n.state.Restored = true
	n.state.PublicName = "present"
	if n.pins.verifyName(n.old, n.target) != nil {
		return errors.New("watchdog_restore_unverified")
	}
	id, err := n.identity(n.old)
	if err != nil || !watchdogSameHeld(id, n.origin.Installed) {
		return errors.New("watchdog_restored_identity_changed")
	}
	n.state.NamespaceUnknown = false
	return nil
}

// The live baseline permits delete sharing so the resident can be stopped
// before DELETE-capable transaction handles are acquired. It still denies
// write sharing, retains the original object and prevents file-ID reuse.
type watchdogDeferredNative struct {
	pins *watchdogPinScope
	target, source string
	origin watchdogOrigin
	artifact watchdogFileIdentity
	baseline *pin
	baselineParents []*pin
	transaction *watchdogNative
	retainedDescriptors []unsafe.Pointer
	closed bool
	closeErr error
}

func newWatchdogDeferredNative(origin watchdogOrigin, artifact watchdogFileIdentity, source string) (*watchdogDeferredNative, error) {
	if !origin.valid() || !artifact.valid() {
		return nil, errors.New("watchdog_origin_or_artifact_unbound")
	}
	return &watchdogDeferredNative{target:watchdogTarget,source:source,origin:origin,artifact:artifact,pins:newWatchdogPinScope()},nil
}

func (d *watchdogDeferredNative) Check(ctx context.Context) error {
	if d.closed || ctx.Err()!=nil || !d.origin.valid() || !d.artifact.valid() {
		return errors.New("watchdog_deferred_inputs_unbound")
	}
	var err error
	if d.pins==nil {d.pins=newWatchdogPinScope()}
	if d.baseline==nil {
		d.baselineParents,err=d.pins.parents(d.target)
		if err!=nil{return err}
		d.baseline,err=d.pins.open(d.target,syscall.GENERIC_READ|0x20000,5,3)
		if err!=nil{return err}
	}
	id,err:=watchdogIdentityHeld(d.baseline,&d.retainedDescriptors)
	if err!=nil||id!=d.origin.Installed||d.pins.verifyName(d.baseline,d.target)!=nil {
		return errors.New("watchdog_predecessor_origin_mismatch")
	}
	if !missing(d.target+".before-"+watchdogGeneration)||
		!missing(d.target+".stage-"+watchdogGeneration)||
		!missing(d.target+".failed-"+watchdogGeneration) {
		return errors.New("watchdog_previous_generation_retained")
	}
	return nil
}

func (d *watchdogDeferredNative) Stage(ctx context.Context) error {
	if d.closed||d.transaction!=nil||d.baseline==nil {
		return errors.New("watchdog_deferred_stage_state")
	}
	if err:=d.Check(ctx);err!=nil{return err}
	// Controller invokes Stage only after its resident adapter proves owned
	// stop, collection and launch hold. No live-image DELETE open occurs in Check.
	var err error
	d.transaction,err=prepareWatchdogNativeAt(ctx,d.target,d.origin,d.artifact,d.source)
	if err!=nil{return err}
	id,err:=d.transaction.identity(d.transaction.old)
	if err!=nil||id!=d.origin.Installed{return errors.New("watchdog_stopped_target_changed")}
	return d.transaction.Stage(ctx)
}
func (d *watchdogDeferredNative) Publish(ctx context.Context) error {
	if d.closed||d.transaction==nil{return errors.New("watchdog_transaction_unavailable")}
	return d.transaction.Publish(ctx)
}
func (d *watchdogDeferredNative) Rollback(ctx context.Context) error {
	if d.closed||d.transaction==nil{return errors.New("watchdog_transaction_unavailable")}
	return d.transaction.Rollback(ctx)
}
func (d *watchdogDeferredNative) Candidate() watchdogFileIdentity {
	if d.transaction==nil{return watchdogFileIdentity{}}
	return d.transaction.Candidate()
}
func (d *watchdogDeferredNative) Steps() watchdogSteps {
	if d.transaction==nil{return watchdogSteps{}}
	return d.transaction.Steps()
}
func (d *watchdogDeferredNative) Close() error {
	if d.closed{return d.closeErr}
	d.closed=true
	var es []error
	if d.transaction!=nil{es=append(es,d.transaction.Close())}
	if d.pins!=nil{es=append(es,d.pins.Close())}
	if len(d.retainedDescriptors)!=0{es=append(es,errors.New("watchdog_security_release_failed"))}
	d.closeErr=errors.Join(es...)
	return d.closeErr
}

func (n *watchdogNative) Close() error {
	if n.closed {
		return n.closeErr
	}
	n.closed = true
	// Keep all pointers, including a failed-close handle. Never reopen, retry a
	// failed close, or delete a stage/backup in the cleanup path.
	es := []error{n.closeErr}
	if len(n.retainedDescriptors) != 0 {
		es = append(es, errors.New("watchdog_security_release_failed"))
	}
	if n.pins!=nil{es=append(es,n.pins.Close())}
	n.closeErr = errors.Join(es...)
	return n.closeErr
}
