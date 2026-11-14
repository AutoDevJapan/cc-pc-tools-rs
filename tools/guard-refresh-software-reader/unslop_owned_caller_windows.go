//go:build windows

package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
)

// This object belongs inside a continuing native owner process. A short-lived
// request CLI must not use a package map as a substitute for that process.
// It accepts actual acquired objects; JSON cannot construct one.
type unslopOwnedCaller struct {
	mu sync.Mutex
	root *unslopCallerHeldRoot
	provider *softwareRustOriginProvider
	controller *unslopDurableController
	owner *unslopRetainedOwner
	handoff *unslopNormalCaptureHandoff
	capture unslopDurableCaptureResult
	captureCleanupRefs []any
	captureCleanupFailed bool
	pendingCleanup *unslopCallerPending
	started,inputsReleased,complete bool
	reason string
}

type unslopOwnedCallerResult struct {
	unslopDurableCaptureResult
	CallerReason string `json:"caller_reason"`
	CleanupPending bool `json:"cleanup_pending"`
	CleanupComplete bool `json:"cleanup_complete"`
	CaptureCleanupFailed bool `json:"capture_cleanup_failed,omitempty"`
	CaptureCleanupRefsRetained bool `json:"capture_cleanup_refs_retained,omitempty"`
	RootCleanupFailed bool `json:"root_cleanup_failed,omitempty"`
	PendingCloseFailed bool `json:"pending_close_failed,omitempty"`
	PendingAbortFailed bool `json:"pending_abort_failed,omitempty"`
	PendingCleanupRefsRetained bool `json:"pending_cleanup_refs_retained,omitempty"`
}

var errUnslopCallerRootCleanup=errors.New("capture_root_cleanup_failed")

// Keep the primary constructor failure and its original root alongside the
// independent cleanup fact. Native error text is never part of this error.
type unslopCallerInitializationError struct {
	primary error
	root *unslopCallerHeldRoot
	CleanupFailed bool
}
func (e *unslopCallerInitializationError) Error() string {return e.primary.Error()}
func (e *unslopCallerInitializationError) Unwrap() []error {return []error{e.primary,errUnslopCallerRootCleanup}}

func unslopCallerAbandonInitialization(root *unslopCallerHeldRoot,primary error) error {
	_ = root.abandon()
	unslopCallerRoots.Lock();failed:=root.caller==nil&&root.closeAttempted&&root.closeError!=nil;unslopCallerRoots.Unlock()
	if failed {return &unslopCallerInitializationError{primary:primary,root:root,CleanupFailed:true}}
	return primary
}

func (c *unslopOwnedCaller) captureFactory(factory unslopToolchainCaptureFactory) unslopToolchainCaptureFactory {
	return func(ctx context.Context,held rustToolchainHeld,binding unslopRustToolchainBinding)(unslopCaptureAccess,error){
		access,err:=factory(ctx,held,binding)
		if err!=nil||access.begin==nil {return access,err}
		stage:=access.stage
		if stage!=nil {
			access.stage=func(ctx context.Context,receipt unslopCaptureReceipt,raw []byte)(unslopPendingCapture,error){
				pending,err:=stage(ctx,receipt,raw)
				if pending==nil {return nil,err}
				c.pendingCleanup=newUnslopCallerPending(pending)
				return c.pendingCleanup,err
			}
		}
		begin:=access.begin
		access.begin=func(ctx context.Context)(unslopProducerSession,error){
			session,err:=begin(ctx)
			if session!=nil {
				c.rememberCaptureInputs(session)
				if err==nil {
					var live *unslopLiveSession
					switch value:=session.(type) {case *unslopLiveSession:live=value;case *unslopOwnedCaptureSubject:live=value.live}
					if live!=nil {session=&unslopOwnedCaptureSubject{unslopProducerSession:&unslopCallerCaptureSession{unslopProducerSession:session,caller:c,live:live},live:live}}
				}
			}
			return session,err
		}
		return access,nil
	}
}


// Capture may acquire evidence during Build or ACK. Record those exact objects
// before the existing owned session performs Close; never close them twice.
type unslopCallerCaptureSession struct {
	unslopProducerSession
	caller *unslopOwnedCaller
	live *unslopLiveSession
}
func (s *unslopCallerCaptureSession) remember() {s.caller.rememberCaptureInputs(&unslopOwnedCaptureSubject{unslopProducerSession:s.unslopProducerSession,live:s.live})}
func (s *unslopCallerCaptureSession) Build(ctx context.Context) (unslopBuildInvocation,error) {defer s.remember();return s.unslopProducerSession.Build(ctx)}
func (s *unslopCallerCaptureSession) After(ctx context.Context) (unslopCapturedInputs,error) {defer s.remember();return s.unslopProducerSession.After(ctx)}
func (s *unslopCallerCaptureSession) Acknowledge(ctx context.Context) error {defer s.remember();return s.unslopProducerSession.Acknowledge(ctx)}
func (s *unslopCallerCaptureSession) AwaitCompletion(ctx context.Context) (unslopProducerCompletion,error) {defer s.remember();return s.unslopProducerSession.AwaitCompletion(ctx)}

// Snapshot references before live.Close can clear fields. These are cleanup
// references only; no unknown Closer is unwrapped and none is closed here.
func (c *unslopOwnedCaller) rememberCaptureInputs(session unslopProducerSession) {
	var live *unslopLiveSession
	switch value:=session.(type) {case *unslopLiveSession:live=value;case *unslopOwnedCaptureSubject:live=value.live}
	if live==nil||live.unslopNativeSession==nil {return}
	c.captureCleanupRefs=append(c.captureCleanupRefs,session,live,live.unslopNativeSession)
	rememberObject:=func(object updateObject){
		if object==nil {return}
		c.captureCleanupRefs=append(c.captureCleanupRefs,object)
		if native,ok:=object.(*nativeUpdateObject);ok&&native!=nil {
			if native.f!=nil {c.captureCleanupRefs=append(c.captureCleanupRefs,native.f)}
			for _,parent:=range native.parents {c.captureCleanupRefs=append(c.captureCleanupRefs,parent)}
		}
	}
	rememberObject(live.evidence);rememberObject(live.manifest)
	for _,tool:=range live.tools {rememberObject(tool)}
	for _,parent:=range live.parents {c.captureCleanupRefs=append(c.captureCleanupRefs,parent)}
	for _,snapshot:=range []*softwareBuildSnapshot{live.source,live.configuration} {
		if snapshot==nil {continue}
		c.captureCleanupRefs=append(c.captureCleanupRefs,snapshot)
		for _,file:=range snapshot.files {c.captureCleanupRefs=append(c.captureCleanupRefs,file)}
		for _,directory:=range snapshot.dirs {c.captureCleanupRefs=append(c.captureCleanupRefs,directory)}
		for _,sealed:=range snapshot.sealed {c.captureCleanupRefs=append(c.captureCleanupRefs,sealed.handle)}
		for _,acl:=range snapshot.handoffACLs {c.captureCleanupRefs=append(c.captureCleanupRefs,acl.handle)}
	}
}

func (c *unslopOwnedCaller) observeCaptureCleanupLocked() {
	proof:=c.controller.ownedCapture
	// Refused Close of an uncollected flight does not mean an actual Close
	// failed. The later retained owner may still collect and close it normally.
	if attempted,failed:=proof.captureCloseResult();attempted&&failed {c.captureCleanupFailed=true}
}

// The pending object is acquired by the same stage call. Facts come from that
// object's actual Close/Abort return values, independently of later reason text.
type unslopCallerPending struct {
	unslopPendingCapture
	mu sync.Mutex
	closeOnce,abortOnce sync.Once
	closeErr,abortErr error
	refs []any
}
func newUnslopCallerPending(actual unslopPendingCapture) *unslopCallerPending {
	p:=&unslopCallerPending{unslopPendingCapture:actual,refs:[]any{actual}}
	if native,ok:=actual.(*unslopNativePending);ok&&native!=nil&&native.export!=nil {
		p.refs=append(p.refs,native.export)
		if object,ok:=native.export.(*nativeUpdateObject);ok&&object!=nil {
			if object.f!=nil {p.refs=append(p.refs,object.f)}
			for _,parent:=range object.parents {p.refs=append(p.refs,parent)}
		}
	}
	return p
}
func (p *unslopCallerPending) Close() error {
	p.closeOnce.Do(func(){err:=p.unslopPendingCapture.Close();p.mu.Lock();p.closeErr=err;p.mu.Unlock()})
	p.mu.Lock();defer p.mu.Unlock();return p.closeErr
}
func (p *unslopCallerPending) Abort() error {
	p.abortOnce.Do(func(){err:=p.unslopPendingCapture.Abort();p.mu.Lock();p.abortErr=err;p.mu.Unlock()})
	p.mu.Lock();defer p.mu.Unlock();return p.abortErr
}
func (p *unslopCallerPending) cleanupFacts() (bool,bool,[]any) {
	if p==nil {return false,false,nil}
	p.mu.Lock();defer p.mu.Unlock()
	return p.closeErr!=nil,p.abortErr!=nil,append([]any(nil),p.refs...)
}

func newUnslopOwnedCaller(root *unslopCallerHeldRoot,provider *softwareRustOriginProvider,factory unslopToolchainCaptureFactory) (*unslopOwnedCaller,error) {
	if root==nil {return nil,errUnslopCallerOwnerUnavailable}
	if !root.current() {return nil,unslopCallerAbandonInitialization(root,errUnslopCallerOwnerUnavailable)}
	if provider==nil||factory==nil {return nil,unslopCallerAbandonInitialization(root,errUnslopCallerOwnerUnavailable)}
	caller:=&unslopOwnedCaller{root:root,provider:provider,reason:"capture_not_started"}
	controller,err:=newUnslopDurableController(root.store,nil,caller.captureFactory(factory))
	if err!=nil {return nil,unslopCallerAbandonInitialization(root,errUnslopCallerRoot)}
	owner,err:=newUnslopRetainedOwner(1)
	if err!=nil {return nil,unslopCallerAbandonInitialization(root,errUnslopCallerOwnerUnavailable)}
	caller.controller=controller;caller.owner=owner
	if !root.claim(caller) {return nil,errUnslopCallerRoot}
	return caller,nil
}

func (c *unslopOwnedCaller) resultLocked() unslopOwnedCallerResult {
	r:=unslopOwnedCallerResult{unslopDurableCaptureResult:c.capture,CallerReason:c.reason,CleanupPending:c.started&&!c.complete,CleanupComplete:c.complete}
	r.unslopToolchainCaptureResult=unslopDurableCloneResult(c.capture.unslopToolchainCaptureResult)
	if c.capture.RetainedOwner!=nil {report:=unslopRetainedCopyReport(*c.capture.RetainedOwner);r.RetainedOwner=&report}
	r.CaptureCleanupFailed=c.captureCleanupFailed
	r.CaptureCleanupRefsRetained=c.captureCleanupFailed&&len(c.captureCleanupRefs)>0
	closeFailed,abortFailed,pendingRefs:=c.pendingCleanup.cleanupFacts()
	r.PendingCloseFailed=closeFailed;r.PendingAbortFailed=abortFailed
	r.PendingCleanupRefsRetained=(closeFailed||abortFailed)&&len(pendingRefs)>0
	if c.root!=nil {unslopCallerRoots.Lock();r.RootCleanupFailed=c.root.closeAttempted&&c.root.closeError!=nil;unslopCallerRoots.Unlock()}
	r.DataAccessGranted=false;r.UpdatePerformed=false
	if !c.complete {r.Qualified=false;r.Receipt=nil}
	return r
}

func (c *unslopOwnedCaller) Status() unslopOwnedCallerResult {
	if c==nil {return unslopOwnedCallerResult{CallerReason:"capture_owner_unavailable"}}
	c.mu.Lock();defer c.mu.Unlock()
	return c.resultLocked()
}

// Begin never repeats capture, even when final saving or transfer is pending.
// Resume operates on the same controller and uses a separately bounded context.
func (c *unslopOwnedCaller) Begin(ctx context.Context) unslopOwnedCallerResult {
	if c==nil {return unslopOwnedCallerResult{CallerReason:"capture_owner_unavailable"}}
	c.mu.Lock();defer c.mu.Unlock()
	if c.complete {return c.resultLocked()}
	if !c.root.owns(c) {return unslopOwnedCallerResult{CallerReason:"capture_owner_mismatch"}}
	if c.started {return c.resultLocked()}
	if ctx==nil||ctx.Err()!=nil||!c.root.current(){c.reason="capture_start_unavailable";return c.resultLocked()}
	c.started=true
	c.capture=c.controller.CaptureOwned(ctx,c.provider)
	c.reason="capture_cleanup_pending"
	c.finishLocked()
	return c.resultLocked()
}

func (c *unslopOwnedCaller) finishLocked() {
	c.observeCaptureCleanupLocked()
	if c.captureCleanupFailed {c.reason="capture_input_cleanup_failed";return}
	if closeFailed,abortFailed,_:=c.pendingCleanup.cleanupFacts();closeFailed||abortFailed {c.reason="capture_pending_cleanup_failed";return}
	if !c.capture.ResultSaved||c.capture.OwnedInputsRetained {return}
	if c.handoff!=nil&&(c.handoff.final==nil||!c.handoff.final.Saved||!c.handoff.final.Forgotten) {return}
	proof:=c.controller.ownedCapture
	if proof!=nil {
		capability:=proof.capability
		if capability==nil {c.reason="capture_cleanup_pending";return}
		capability.mu.Lock();closed,closing,closeErr:=capability.closed,capability.closing,capability.closeErr;capability.mu.Unlock()
		if !closed||closing||closeErr!=nil {c.reason="capture_cleanup_pending";return}
	}
	if c.capture.ToolchainCleanupFailed {c.reason="capture_cleanup_pending";return}
	c.inputsReleased=true
	if err:=c.root.release(c);err!=nil {c.reason="capture_root_release_failed";return}
	c.captureCleanupRefs=nil;c.pendingCleanup=nil
	c.complete=true
	if c.capture.Qualified {c.reason="capture_complete"} else {c.reason="capture_failed_cleaned"}
}

func (c *unslopOwnedCaller) Resume(ctx context.Context) unslopOwnedCallerResult {
	if c==nil {return unslopOwnedCallerResult{CallerReason:"capture_owner_unavailable"}}
	c.mu.Lock();defer c.mu.Unlock()
	if c.complete {return c.resultLocked()}
	if !c.root.owns(c) {return unslopOwnedCallerResult{CallerReason:"capture_owner_mismatch"}}
	if !c.started {return c.resultLocked()}
	if ctx==nil {c.reason="capture_cleanup_context_unavailable";return c.resultLocked()}
	if !c.controller.attempted {
		// Only a failed pre-capture journal start can enter CaptureOwned again.
		c.capture=c.controller.CaptureOwned(ctx,c.provider)
	} else if !c.capture.ResultSaved {c.capture=c.controller.RetryFinal(ctx)}
	c.observeCaptureCleanupLocked()
	if c.captureCleanupFailed {c.reason="capture_input_cleanup_failed";return c.resultLocked()}
	proof:=c.controller.ownedCapture
	if proof!=nil&&proof.rejectedSession!=nil {c.reason="capture_partial_cleanup_pending";return c.resultLocked()}
	if c.capture.OwnedInputsRetained||c.handoff!=nil {
		if c.handoff==nil {
			if proof==nil||proof.live==nil||proof.unverifiedStart!=nil {c.reason="capture_partial_cleanup_pending";return c.resultLocked()}
			live,lease:=proof.live,proof.lease
			handoff,err:=newUnslopNormalCaptureHandoff(c.controller,c.owner,&live,&lease)
			if err!=nil {c.reason="capture_owner_transfer_pending";return c.resultLocked()}
			c.handoff=handoff
		}
		wasTransferred:=c.handoff.ticket!=nil
		if c.handoff.final==nil {
			if err:=c.handoff.Transfer(ctx);err!=nil {
				// Once moved, even a failed save may cancel only this same bound flight.
				if c.handoff.ticket!=nil&&ctx.Err()!=nil {_ = c.handoff.DrainAndSave(ctx)}
				c.reason="capture_owner_transfer_pending";return c.resultLocked()
			}
			if !wasTransferred {c.reason="capture_cleanup_pending";return c.resultLocked()}
		}
		drain:=c.handoff.DrainAndSave(ctx)
		if drain.Saved&&drain.Forgotten {c.inputsReleased=true}
		c.capture=c.controller.RetryFinal(ctx)
		if !drain.Saved||!drain.Forgotten {c.reason="capture_cleanup_pending";return c.resultLocked()}
	}
	c.finishLocked()
	return c.resultLocked()
}

func runNormalUnslopOwnedCapture(ctx context.Context,out io.Writer) int {
	// Until a continuing owner and normal issuer are connected, do not acquire
	// root/provider or begin a worker inside this short-lived process.
	_ = ctx
	result:=unslopOwnedCallerResult{
		unslopDurableCaptureResult:unslopDurableCaptureResult{
			unslopToolchainCaptureResult:unslopToolchainCaptureResult{unslopCaptureResult:unslopCaptureResult{Reason:"missing_producer_provenance"}},
			DurableReason:"durable_controller_unavailable",
		},
		CallerReason:"capture_owner_unavailable",
	}
	if out==nil {return 1}
	_ = json.NewEncoder(out).Encode(result)
	return 1
}
