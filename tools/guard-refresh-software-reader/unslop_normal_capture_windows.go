//go:build windows

package main

import (
	"context"
	"sync"
)

// A subject binds the concrete live owner to the session used by capture.
// Production uses the live session itself. The separate delegate also lets
// internal synthetic tests exercise this route without opening fixed inputs.
type unslopOwnedCaptureSubject struct {
	unslopProducerSession
	live *unslopLiveSession
}

// An observation is not an authority. No completed actual Close remains the
// zero state, including while a native Close call is still in progress.
type unslopCaptureCloseObservation struct {
	mu sync.Mutex
	attempted,failed bool
}

type unslopOwnedCaptureProof struct {
	controller *unslopDurableController
	generation string
	capability *softwareRustOwnedCapability
	lease softwareRustHeldSession
	live *unslopLiveSession
	flight *unslopOwnedFlight
	rawFlight *askOwnedProducerFlight
	unverifiedStart unslopLiveFlight
	rejectedSession unslopProducerSession
	done <-chan askProducerFlightResult
	nonce,manifest,drive string
	inputManifest string
	native *askNativeProcess
	moved,adopted bool
	captureClose *unslopCaptureCloseObservation
}

func (p *unslopOwnedCaptureProof) observe(session unslopProducerSession,held rustToolchainHeld,binding unslopRustToolchainBinding) (unslopProducerSession,error) {
	if p==nil||p.capability==nil||!p.capability.current()||p.controller==nil||p.generation!=p.controller.journal.generation||p.live!=nil {return nil,errUnslopDurableConflict}
	p.capability.mu.Lock();issued:=p.capability.proof==p&&p.capability.claimed;p.capability.mu.Unlock();if !issued{return nil,errUnslopDurableConflict}
	owned,ok:=p.lease.(*softwareRustOwnedSession);if !ok||owned.capability!=p.capability||!owned.current(){return nil,errUnslopDurableConflict}
	var live *unslopLiveSession
	switch s:=session.(type) {
	case *unslopLiveSession:live=s
	case *unslopOwnedCaptureSubject:live=s.live
	}
	if live==nil||live.unslopNativeSession==nil||live.closed||live.start==nil||live.flight!=nil {return nil,errUnslopDurableConflict}
	inputManifest,valid:=unslopCapturedManifest(session.Before());if !valid{return nil,errUnslopDurableConflict}
	actual:=p.lease.Held()
	if len(actual.Cargo.Raw)==0||len(actual.Rustc.Raw)==0||len(held.Cargo.Raw)==0||len(held.Rustc.Raw)==0||&actual.Cargo.Raw[0]!=&held.Cargo.Raw[0]||&actual.Rustc.Raw[0]!=&held.Rustc.Raw[0] {return nil,errUnslopDurableConflict}
	bound,valid:=bindUnslopRustToolchain(fixedUnslopToolchainPurpose(),actual)
	if !valid||bound!=binding {return nil,errUnslopDurableConflict}
	p.captureClose=&unslopCaptureCloseObservation{}
	p.live=live;p.inputManifest=inputManifest
	p.controller.observedLive=live;p.controller.observedHeld=held;p.controller.observedBinding=binding
	start:=live.start
	live.start=func(ctx context.Context,nonce,manifest,drive string)(unslopLiveFlight,error){
		if p.flight!=nil||!p.capability.current()||!unslopHex(nonce,32)||manifest!=p.inputManifest||len(drive)!=2||drive[1]!=':'||drive[0]<'P'||drive[0]>'Z'{return nil,errUnslopDurableConflict}
		flight,err:=start(ctx,nonce,manifest,drive)
		if flight==nil{return nil,err}
		owned,ok:=flight.(*unslopOwnedFlight)
		if !ok||owned==nil||owned.flight==nil||owned.flight.Done==nil||owned.flight.Abort==nil {p.unverifiedStart=flight;return flight,errUnslopDurableConflict}
		p.flight=owned;p.rawFlight=owned.flight;p.done=owned.flight.Done
		p.nonce=nonce;p.manifest=manifest;p.drive=drive
		return flight,err
	}
	return &unslopOwnedCaptureSession{unslopProducerSession:session,proof:p},nil
}

func (p *unslopOwnedCaptureProof) matches(c *unslopDurableController,live *unslopLiveSession,lease softwareRustHeldSession) bool {
	if p==nil||p.controller!=c||p.generation!=c.journal.generation||p.moved||p.capability==nil||!p.capability.current()||p.lease!=lease||p.live!=live||p.flight==nil||live.flight!=p.flight||p.flight.flight!=p.rawFlight||p.rawFlight==nil||p.rawFlight.Done!=p.done {return false}
	p.capability.mu.Lock();issued:=p.capability.proof==p&&p.capability.claimed;p.capability.mu.Unlock();if !issued{return false}
	owned,ok:=lease.(*softwareRustOwnedSession);if !ok||owned.capability!=p.capability||!owned.current(){return false}
	if p.native!=nil&&(p.flight.result==nil||p.flight.result.NativeProcess==nil||*p.native!=*p.flight.result.NativeProcess){return false}
	return p.done!=nil&&p.rawFlight.Abort!=nil&&unslopHex(p.nonce,32)&&p.manifest==p.inputManifest&&len(p.drive)==2&&p.drive[1]==':'&&p.drive[0]>='P'&&p.drive[0]<='Z'
}

func (p *unslopOwnedCaptureProof) observeNative() bool {
	if p==nil||p.flight==nil||p.flight.result==nil{return false}
	r:=p.flight.result
	if r.NativeProcess==nil||r.NativeProcess.PID==0||r.NativeProcess.Creation==0{return false}
	if p.native==nil {native:=*r.NativeProcess;p.native=&native}
	return *p.native==*r.NativeProcess
}

func (p *unslopOwnedCaptureProof) canCloseLease() bool {
	if p==nil{return true}
	if p.rejectedSession!=nil||p.unverifiedStart!=nil{return false}
	if p.flight==nil{return p.live==nil||p.live.flight==nil}
	if p.live==nil||p.live.flight!=p.flight||p.flight.flight!=p.rawFlight||p.rawFlight==nil||p.rawFlight.Done!=p.done||p.flight.result==nil{return false}
	return p.observeNative()&&unslopRetainedCollectionVerified(true,*p.flight.result)
}

func (p *unslopOwnedCaptureProof) captureCloseResult() (bool,bool) {
	if p==nil||p.captureClose==nil {return false,false}
	state:=p.captureClose;state.mu.Lock();defer state.mu.Unlock()
	return state.attempted,state.failed
}

type unslopOwnedCaptureSession struct {
	unslopProducerSession
	proof *unslopOwnedCaptureProof
	closeOnce sync.Once
	closeErr error
}

func (s *unslopOwnedCaptureSession) AwaitCompletion(ctx context.Context) (unslopProducerCompletion,error) {
	r,err:=s.unslopProducerSession.AwaitCompletion(ctx)
	if s.proof.flight!=nil&&s.proof.flight.result!=nil&&!s.proof.observeNative(){return unslopProducerCompletion{},errUnslopDurableConflict}
	return r,err
}

func (s *unslopOwnedCaptureSession) Close() error {
	p:=s.proof
	if p==nil||p.live==nil{return errUnslopDurableConflict}
	if p.unverifiedStart!=nil||p.flight==nil&&p.live.flight!=nil {return errUnslopDurableConflict}
	if p.flight!=nil {
		if p.live.flight!=p.flight||p.flight.flight!=p.rawFlight||p.rawFlight.Done!=p.done||p.flight.result==nil||!p.observeNative()||!unslopRetainedCollectionVerified(true,*p.flight.result) {return errUnslopDurable}
		// Only this same flight's full collection record permits old live.Close.
		p.live.completed=true
	}
	s.closeOnce.Do(func(){
		s.closeErr=p.live.Close()
		if state:=p.captureClose;state!=nil {
			state.mu.Lock();state.attempted=true;state.failed=s.closeErr!=nil;state.mu.Unlock()
		}
	})
	return s.closeErr
}

type unslopOwnedCaptureLeaseProvider struct {proof *unslopOwnedCaptureProof}

func (p *unslopOwnedCaptureLeaseProvider) WithHeld(ctx context.Context,visit func(rustToolchainHeld)bool) (result error) {
	proof:=p.proof
	if ctx==nil||ctx.Err()!=nil||visit==nil||proof==nil||proof.capability==nil||!proof.capability.current(){return errSoftwareRustUnavailable}
	accepted:=visit(proof.lease.Held())
	held:=proof.lease.Held()
	checkErr:=errSoftwareRustMismatch
	if held.Recheck!=nil&&rustToolchainInputsMatch(held) {checkErr=held.Recheck(ctx)}
	if ctx.Err()!=nil {result=ctx.Err()}else if checkErr!=nil {result=softwareRustFixedError(ctx,checkErr)}else if !rustToolchainInputsMatch(held){result=errSoftwareRustMismatch}else if !accepted{result=errSoftwareRustConsumer}
	// An uncollected flight keeps the actual session and capability alive.
	if proof.rejectedSession!=nil||proof.live!=nil&&!proof.live.closed&&(proof.flight!=nil||proof.unverifiedStart!=nil||proof.live.flight!=nil) {return result}
	if err:=proof.lease.Close();err!=nil {return softwareRustWithCleanupFailure(result)}
	return result
}

// This receiver owns caller slots at an exclusive transfer boundary. It does
// not open a provider, start capture, or turn stored metadata into authority.
type unslopNormalCaptureHandoff struct {
	mu sync.Mutex
	controller *unslopDurableController
	owner *unslopRetainedOwner
	live *unslopLiveSession
	lease softwareRustHeldSession
	ticket *unslopRetainedTicket
	final *unslopDurableDrainResult
}

type unslopNormalCaptureHandoffStatus struct {
	Generation string `json:"generation,omitempty"`
	State string `json:"state"`
	InputsHeld bool `json:"inputs_held"`
	Transferred bool `json:"transferred"`
	Report *unslopRetainedReport `json:"retained_owner,omitempty"`
	Qualified bool `json:"qualified"`
	DataAccessGranted bool `json:"data_access_granted"`
	UpdatePerformed bool `json:"update_performed"`
}

// The caller already owns an acquired lease, not a borrowed WithHeld tuple.
// It must not retain extra aliases or use either source slot after this move.
func newUnslopNormalCaptureHandoff(c *unslopDurableController,owner *unslopRetainedOwner,source **unslopLiveSession,lease *softwareRustHeldSession) (*unslopNormalCaptureHandoff,error) {
	if c==nil||owner==nil||source==nil||*source==nil||lease==nil||!unslopRetainedPointer(*lease) {return nil,errUnslopDurableConflict}
	c.mu.Lock();defer c.mu.Unlock()
	live:=*source
	if c.recovered||!c.started||!c.attempted||c.result==nil||c.journal==nil||c.owner!=nil||c.ticket!=nil||c.pendingSource!=nil||c.pendingReservation!=nil||live!=c.observedLive||live.unslopNativeSession==nil||live.closed {return nil,errUnslopDurableConflict}
	if c.ownedCapture==nil||c.ownedCapture.adopted||!c.ownedCapture.matches(c,live,*lease){return nil,errUnslopDurableConflict}
	flight,ok:=live.flight.(*unslopOwnedFlight)
	if !ok||flight==nil||flight.flight==nil||flight.flight.Done==nil||flight.flight.Abort==nil {return nil,errUnslopDurableConflict}
	held:=(*lease).Held()
	if len(held.Cargo.Raw)==0||len(held.Rustc.Raw)==0||len(c.observedHeld.Cargo.Raw)==0||len(c.observedHeld.Rustc.Raw)==0||&held.Cargo.Raw[0]!=&c.observedHeld.Cargo.Raw[0]||&held.Rustc.Raw[0]!=&c.observedHeld.Rustc.Raw[0] {return nil,errUnslopDurableConflict}
	binding,valid:=bindUnslopRustToolchain(fixedUnslopToolchainPurpose(),held)
	if !valid||binding.SHA!=c.observedBinding.SHA {return nil,errUnslopDurableConflict}
	owner.mu.Lock()
	known:=len(owner.identity)==32&&owner.entries!=nil&&owner.claims!=nil&&owner.handles!=nil&&owner.flights!=nil&&owner.done!=nil&&owner.reservations!=nil
	owner.mu.Unlock()
	if !known {return nil,errUnslopDurableConflict}
	nextLease,err:=(*lease).(*softwareRustOwnedSession).move();if err!=nil{return nil,err}
	h:=&unslopNormalCaptureHandoff{controller:c,owner:owner,live:live,lease:nextLease}
	c.ownedCapture.adopted=true
	c.ownedCapture.lease=nextLease
	c.pendingSource=live;c.pendingLease=nextLease
	*source=nil;*lease=nil
	return h,nil
}

func (h *unslopNormalCaptureHandoff) Transfer(ctx context.Context) error {
	if h==nil||ctx==nil {return errUnslopDurable}
	h.mu.Lock();defer h.mu.Unlock()
	if h.controller==nil||h.owner==nil {return errUnslopDurable}
	if h.final!=nil {return nil}
	if h.ticket!=nil {return h.controller.SaveRetained(ctx,h.owner,h.ticket)}
	ticket,err:=h.controller.Retain(ctx,h.owner,&h.live,&h.lease,nil)
	// A ticket may be returned with a post-transfer save error. Keep it and
	// retry the save; only a pre-transfer failure leaves these input slots held.
	if ticket!=nil {h.ticket=ticket}
	return err
}

func (h *unslopNormalCaptureHandoff) statusLocked() unslopNormalCaptureHandoffStatus {
	r:=unslopNormalCaptureHandoffStatus{State:"unknown"}
	if h.controller==nil||h.owner==nil {return r}
	if h.final!=nil {
		r.State="result_saved";r.Transferred=true
		report:=unslopRetainedCopyReport(h.final.Report);r.Report=&report
		h.controller.mu.Lock();if h.controller.journal!=nil {r.Generation=h.controller.journal.generation};h.controller.mu.Unlock()
		return r
	}
	h.controller.mu.Lock()
	if h.controller.journal!=nil {r.Generation=h.controller.journal.generation}
	r.InputsHeld=h.live!=nil&&unslopRetainedPointer(h.lease)
	if h.ticket!=nil {
		r.Transferred=true
		report:=h.owner.Snapshot(h.ticket)
		r.State=report.State
		if unslopDurableOwnerValid(report) {r.Report=&report}
	} else {
		r.State="inputs_owned"
		if reservation:=h.controller.pendingReservation;reservation!=nil&&reservation.owner==h.owner&&reservation.source==h.live&&reservation.lease==h.lease {
			r.State="transfer_pending"
			report:=unslopRetainedCopyReport(reservation.report)
			if unslopDurableOwnerValid(report) {r.Report=&report}
		}
	}
	h.controller.mu.Unlock()
	return r
}

func (h *unslopNormalCaptureHandoff) Status() unslopNormalCaptureHandoffStatus {
	if h==nil {return unslopNormalCaptureHandoffStatus{State:"unknown"}}
	h.mu.Lock();defer h.mu.Unlock()
	return h.statusLocked()
}

func (h *unslopNormalCaptureHandoff) DrainAndSave(ctx context.Context) unslopDurableDrainResult {
	if h==nil||ctx==nil {return unslopDurableDrainResult{Reason:"durable_controller_unavailable"}}
	h.mu.Lock()
	if h.final!=nil {r:=*h.final;r.Report=unslopRetainedCopyReport(r.Report);h.mu.Unlock();return r}
	controller,owner,ticket:=h.controller,h.owner,h.ticket
	if ticket==nil {
		status:=h.statusLocked()
		r:=unslopDurableDrainResult{Reason:"durable_owner_transfer_pending"}
		if status.Report!=nil {r.Report=*status.Report}
		h.mu.Unlock()
		return r
	}
	h.mu.Unlock()
	r:=controller.DrainAndSave(ctx,owner,ticket)
	if r.Saved&&r.Forgotten {
		h.mu.Lock();saved:=r;saved.Report=unslopRetainedCopyReport(r.Report);h.final=&saved;h.mu.Unlock()
	}
	return r
}
