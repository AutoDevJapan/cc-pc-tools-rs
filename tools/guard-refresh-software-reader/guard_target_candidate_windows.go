//go:build windows

package main

import (
	"context"
	"errors"
	"sync"
)

// This object owns the exact private candidate returned by the server issuer.
// It never constructs an issuer from a report, digest, or caller assertion.
type guardTargetCandidate struct {
	mu sync.Mutex
	self *guardTargetCandidate
	candidate *ghaGuardOwnedCandidate
	generation ghaGuardGeneration
	ctx context.Context
	payloadBytes int
	admitted,active,closed,closing bool
	closeErr error
}

func guardTargetCandidateFailure(ctx context.Context, fallback error) error {
	if ctx != nil && ctx.Err() != nil { return errors.Join(fallback,ctx.Err()) }
	return fallback
}

// A nonnil partial resource is returned with its error so that the admission
// controller closes that exact candidate once. Only the normal factory below
// selects the production issuer; the private loader seam is for owned tests.
func guardTargetArtifactFromServer(ctx context.Context, load func(context.Context) (*ghaGuardVerifiedCandidate,error)) (guardTargetArtifact,error) {
	if ctx == nil || load == nil { return nil,errGuardTargetArtifactUnavailable }
	if err := ctx.Err(); err != nil { return nil,err }
	source,openErr := load(ctx)
	if source == nil { return nil,guardTargetCandidateFailure(ctx,errors.Join(errGuardTargetArtifactUnavailable,openErr)) }
	// Claim before inspecting partial errors so exactly one cleanup owner exists.
	// Failure must not Close the raw alias: it may already belong to another owner.
	owner,claimErr := source.Claim()
	if claimErr != nil || owner == nil {
		return nil,guardTargetCandidateFailure(ctx,errors.Join(errGuardTargetArtifactUnavailable,openErr,claimErr))
	}
	a := &guardTargetCandidate{candidate:owner,ctx:ctx}
	a.self = a
	if openErr != nil { return a,guardTargetCandidateFailure(ctx,errors.Join(errGuardTargetArtifactUnavailable,openErr)) }
	generation,ok := owner.Generation()
	if !ok || !owner.VerifySourceGeneration() { return a,errGuardTargetArtifactUnavailable }
	a.generation = generation
	calls := 0
	bytes := 0
	err := owner.WithPinnedPayload(func(raw []byte) error {
		calls++
		current,valid := owner.Generation()
		if calls != 1 || !valid || current != generation || ctx.Err() != nil ||
			len(raw) == 0 || len(raw) > ghaGuardPayloadLimit || digest(raw) != generation.OutputSHA || !ghaGuardPEHeader(raw) {
			return errGuardTargetArtifactUnavailable
		}
		bytes = len(raw)
		return nil
	})
	after,valid := owner.Generation()
	if err != nil || calls != 1 || bytes == 0 || !valid || after != generation || ctx.Err() != nil {
		return a,guardTargetCandidateFailure(ctx,errGuardTargetArtifactUnavailable)
	}
	a.payloadBytes = bytes
	a.admitted = true
	return a,nil
}

func guardTargetNormalArtifactFromServer(ctx context.Context) (guardTargetArtifact,error) {
	return guardTargetArtifactFromServer(ctx,ghaGuardOpenVerifiedCandidate)
}

func (a *guardTargetCandidate) binding() (*ghaGuardOwnedCandidate,ghaGuardGeneration,bool) {
	if a == nil { return nil,ghaGuardGeneration{},false }
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.self != a || !a.admitted || a.closed || a.closing || a.candidate == nil || a.ctx == nil || a.ctx.Err() != nil || a.payloadBytes < 1 {
		return nil,ghaGuardGeneration{},false
	}
	generation,ok := a.candidate.Generation()
	if !ok || generation != a.generation || !a.candidate.VerifySourceGeneration() { return nil,ghaGuardGeneration{},false }
	return a.candidate,generation,true
}

func (a *guardTargetCandidate) VerifySourceGeneration() bool {
	_,_,ok := a.binding()
	return ok
}

func (a *guardTargetCandidate) borrowing() bool {
	if a == nil { return false }
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.self == a && a.admitted && a.active && !a.closed && !a.closing
}

func (a *guardTargetCandidate) WithPinnedPayload(use func([]byte) error) (result error) {
	if use == nil { return errGuardTargetArtifactUnavailable }
	source,generation,ok := a.binding()
	if !ok { return errGuardTargetArtifactUnavailable }
	a.mu.Lock()
	if a.self != a || a.closed || a.closing || !a.admitted || a.active || a.candidate != source || a.generation != generation {
		a.mu.Unlock()
		return errGuardTargetArtifactUnavailable
	}
	a.active = true
	size := a.payloadBytes
	a.mu.Unlock()
	completed := false
	defer func() {
		a.mu.Lock()
		a.active = false
		if !completed || result != nil { a.admitted = false }
		a.mu.Unlock()
	}()
	calls := 0
	callbackSucceeded := false
	result = source.WithPinnedPayload(func(raw []byte) error {
		calls++
		actual,current,valid := a.binding()
		if calls != 1 || !valid || actual != source || current != generation || len(raw) != size || digest(raw) != generation.OutputSHA {
			return errGuardTargetArtifactUnavailable
		}
		if err := use(raw); err != nil { return err }
		actual,current,valid = a.binding()
		if !valid || actual != source || current != generation || len(raw) != size || digest(raw) != generation.OutputSHA {
			return errGuardTargetArtifactUnavailable
		}
		callbackSucceeded = true
		return nil
	})
	if result == nil && (calls != 1 || !callbackSucceeded) { result = errGuardTargetArtifactUnavailable }
	if result == nil {
		actual,current,valid := a.binding()
		if !valid || actual != source || current != generation { result = errGuardTargetArtifactUnavailable }
	}
	completed = true
	return result
}

// Close revokes admission once. A close during an active borrow is not reported
// as completed cleanup. The source candidate keeps bytes until its borrower ends.
func (a *guardTargetCandidate) Close() error {
	if a == nil { return nil }
	a.mu.Lock()
	if a.self != a { a.mu.Unlock(); return errGuardTargetCleanup }
	if a.closing { a.mu.Unlock(); return errGuardTargetCleanup }
	if a.closed { err := a.closeErr; a.mu.Unlock(); return err }
	a.closed,a.closing,a.admitted = true,true,false
	source,active,ctx := a.candidate,a.active,a.ctx
	a.mu.Unlock()
	var err error
	if source != nil {
		err = source.Close()
		if !active && err == nil { err = source.WaitClosed(ctx) }
	}
	if active { err = errors.Join(err,errGuardTargetCleanup) }
	a.mu.Lock()
	if err != nil { a.closeErr = errGuardTargetCleanup }
	a.closing = false
	a.candidate = nil
	result := a.closeErr
	a.mu.Unlock()
	return result
}

// The owner-requested operation is compiled for the one software target. This
// lease permits metadata inspection only and does not qualify adjacent data,
// candidate execution, preparation, replacement, or an ACL change.
type guardTargetOwnerPurpose struct {
	mu sync.Mutex
	self *guardTargetOwnerPurpose
	artifact *guardTargetCandidate
	candidate *ghaGuardOwnedCandidate
	generation ghaGuardGeneration
	ctx context.Context
	domain,target string
	closed,opened bool
	open func(context.Context) (guardTargetMetadataLease,error)
}

func guardTargetNormalPurposeForArtifact(ctx context.Context, value guardTargetArtifact) (guardTargetPurposeLease,error) {
	if ctx == nil { return nil,errGuardTargetPurposeUnavailable }
	if err := ctx.Err(); err != nil { return nil,err }
	artifact,ok := value.(*guardTargetCandidate)
	if !ok { return nil,errGuardTargetPurposeUnavailable }
	source,generation,valid := artifact.binding()
	if !valid { return nil,errGuardTargetPurposeUnavailable }
	purpose := &guardTargetOwnerPurpose{artifact:artifact,candidate:source,generation:generation,ctx:ctx,
		domain:guardTargetUpdateDomain,target:guardTargetSoftwarePath,open:openGuardTargetMetadata}
	purpose.self = purpose
	return purpose,nil
}

func (p *guardTargetOwnerPurpose) Recheck(ctx context.Context, value guardTargetArtifact) error {
	if p == nil || ctx == nil { return errGuardTargetPurposeUnavailable }
	if err := ctx.Err(); err != nil { return err }
	p.mu.Lock()
	if p.self != p || p.closed || p.ctx == nil || p.ctx.Err() != nil || p.domain != guardTargetUpdateDomain || p.target != guardTargetSoftwarePath || value != p.artifact {
		p.mu.Unlock()
		return errGuardTargetPurposeUnavailable
	}
	artifact,source,generation := p.artifact,p.candidate,p.generation
	p.mu.Unlock()
	actual,current,ok := artifact.binding()
	if !ok || actual != source || current != generation { return errGuardTargetPurposeUnavailable }
	return nil
}

func (p *guardTargetOwnerPurpose) OpenMetadata(ctx context.Context) (guardTargetMetadataLease,error) {
	if p == nil || ctx == nil { return nil,errGuardTargetPurposeUnavailable }
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.self != p || p.closed || p.opened || p.open == nil || p.ctx == nil || p.ctx.Err() != nil ||
		ctx.Err() != nil || p.domain != guardTargetUpdateDomain || p.target != guardTargetSoftwarePath {
		return nil,guardTargetCandidateFailure(ctx,errGuardTargetPurposeUnavailable)
	}
	artifact := p.artifact
	actual,generation,valid := artifact.binding()
	if !valid || actual != p.candidate || generation != p.generation || !artifact.borrowing() { return nil,errGuardTargetPurposeUnavailable }
	p.opened = true
	lease,err := p.open(ctx)
	if err != nil { return lease,err }
	actual,generation,valid = artifact.binding()
	if !valid || actual != p.candidate || generation != p.generation || !artifact.borrowing() || ctx.Err() != nil {
		return lease,guardTargetCandidateFailure(ctx,errGuardTargetPurposeUnavailable)
	}
	return lease,nil
}

// The admission controller owns metadata leases. This purpose only revokes its
// own permission; it never closes a candidate or metadata owned by another step.
func (p *guardTargetOwnerPurpose) Close() error {
	if p == nil { return nil }
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.self != p { return errGuardTargetCleanup }
	p.closed = true
	p.artifact = nil
	p.candidate = nil
	p.open = nil
	return nil
}
