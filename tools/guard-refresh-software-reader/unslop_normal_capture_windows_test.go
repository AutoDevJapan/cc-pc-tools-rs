//go:build windows

package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
)

type normalOwnedSyntheticCapture struct {
	*unslopSyntheticCapture
	live *unslopLiveSession
	collected bool
}

func (s *normalOwnedSyntheticCapture) Build(ctx context.Context) (unslopBuildInvocation,error) {
	manifest,valid:=unslopCapturedManifest(s.before);if !valid{return unslopBuildInvocation{},errUnslopOrigin}
	flight,err:=s.live.start(ctx,strings.Repeat("c",32),manifest,"P:");if err!=nil{return unslopBuildInvocation{},err}
	s.live.flight=flight
	return s.unslopSyntheticCapture.Build(ctx)
}

func (s *normalOwnedSyntheticCapture) AwaitCompletion(ctx context.Context) (unslopProducerCompletion,error) {
	if !s.collected{return unslopProducerCompletion{ACKReceived:true,WaitVerified:true},nil}
	_,result,err:=s.live.flight.Done(ctx)
	return result,err
}

func normalOwnedCaptureFixture(t *testing.T,collected bool) (*unslopDurableController,*unslopRetainedOwner,*unslopLiveSession,softwareRustHeldSession,chan askProducerFlightResult,*softwareRustSyntheticSession) {
	t.Helper()
	provider,access:=syntheticSoftwareRustProvider()
	done:=make(chan askProducerFlightResult,1)
	flight:=&askOwnedProducerFlight{Done:done,Abort:func(){}}
	live:=&unslopLiveSession{unslopNativeSession:&unslopNativeSession{},start:func(context.Context,string,string,string)(unslopLiveFlight,error){return &unslopOwnedFlight{flight:flight},nil}}
	fixture:=&normalOwnedSyntheticCapture{unslopSyntheticCapture:syntheticUnslopCapture(),live:live,collected:collected}
	if collected {done<-retainedTestCompletion()}
	c,err:=newUnslopDurableController(newDurableTestStore(t),nil,func(context.Context,rustToolchainHeld,unslopRustToolchainBinding)(unslopCaptureAccess,error){
		return unslopCaptureAccess{begin:func(context.Context)(unslopProducerSession,error){return &unslopOwnedCaptureSubject{unslopProducerSession:fixture,live:live},nil},stage:func(context.Context,unslopCaptureReceipt,[]byte)(unslopPendingCapture,error){return &syntheticUnslopPending{},nil}},nil
	});if err!=nil{t.Fatal(err)}
	r:=c.CaptureOwned(context.Background(),provider)
	if !r.ResultSaved||r.Qualified!=collected||c.ownedCapture==nil||c.ownedCapture.capability.session!=access.session||c.ownedCapture.flight==nil||access.opens!=1||access.session.closed!=map[bool]int{false:0,true:1}[collected] {t.Fatal("owned capture fixture did not use the acquired session and started flight",r)}
	owner,err:=newUnslopRetainedOwner(2);if err!=nil {t.Fatal(err)}
	return c,owner,live,c.ownedCapture.lease,done,access.session
}

func normalHandoffFixture(t *testing.T) (*unslopDurableController,*unslopRetainedOwner,*unslopLiveSession,softwareRustHeldSession,chan askProducerFlightResult) {
	t.Helper()
	c,owner,live,lease,done,_:=normalOwnedCaptureFixture(t,false)
	return c,owner,live,lease,done
}

func TestUnslopOwnedCaptureSuccessClosesOriginalCapability(t *testing.T) {
	c,owner,live,lease,_,actual:=normalOwnedCaptureFixture(t,true)
	if c.ownedCapture.capability.current()||actual.closed!=1||!live.closed {t.Fatal("successful owned capture did not invalidate original session")}
	if h,err:=newUnslopNormalCaptureHandoff(c,owner,&live,&lease);err==nil||h!=nil {t.Fatal("closed capture capability was reused")}
	if r:=c.CaptureOwned(context.Background(),c.ownedCapture.capability.issuer);r.DurableReason!="durable_generation_already_used"||actual.closed!=1 {t.Fatal("same generation acquired or closed another session",r)}
}

func TestUnslopOwnedCaptureBorrowedRouteCannotAuthorizeHandoff(t *testing.T) {
	c,live,lease,_:=durableTestObservedRetain(t,newDurableTestStore(t))
	owner,err:=newUnslopRetainedOwner(1);if err!=nil {t.Fatal(err)}
	beforeLive,beforeLease:=live,lease
	if h,err:=newUnslopNormalCaptureHandoff(c,owner,&live,&lease);err==nil||h!=nil||live!=beforeLive||lease!=beforeLease {t.Fatal("borrowed observation minted an owned handoff")}
}

func TestUnslopOwnedCaptureRechecksFlightBeforeTransfer(t *testing.T) {
	for _,mode:=range []string{"flight","done","generation"} {
		t.Run(mode,func(t *testing.T){
			c,owner,live,lease,_:=normalHandoffFixture(t)
			actual:=normalActualLease(lease)
			h,err:=newUnslopNormalCaptureHandoff(c,owner,&live,&lease);if err!=nil {t.Fatal(err)}
			beforeLive,beforeLease:=h.live,h.lease
			original,done:=c.ownedCapture.flight,c.ownedCapture.done
			originalAborts,otherAborts:=0,0;c.ownedCapture.rawFlight.Abort=func(){originalAborts++}
			switch mode {
			case "flight":h.live.flight=&unslopOwnedFlight{flight:&askOwnedProducerFlight{Done:make(chan askProducerFlightResult),Abort:func(){otherAborts++}}}
			case "done":original.flight.Done=make(chan askProducerFlightResult)
			case "generation":c.ownedCapture.generation=strings.Repeat("f",32)
			}
			if err=h.Transfer(context.Background());err==nil||h.live!=beforeLive||h.lease!=beforeLease||h.ticket!=nil||len(owner.Tickets())!=0||actual.closed!=0||originalAborts!=0||otherAborts!=0||len(done)!=0 {t.Fatal("changed owned flight transferred or released original inputs",mode)}
		})
	}
}

func TestUnslopOwnedCaptureNewSessionCannotUseOldProof(t *testing.T) {
	old,_,_,_,_,_:=normalOwnedCaptureFixture(t,true)
	c,owner,live,lease,_:=normalHandoffFixture(t)
	beforeLive,beforeLease:=live,lease
	c.ownedCapture=old.ownedCapture
	if h,err:=newUnslopNormalCaptureHandoff(c,owner,&live,&lease);err==nil||h!=nil||live!=beforeLive||lease!=beforeLease||normalActualLease(lease).closed!=0 {t.Fatal("old closed proof authorized a new session")}
}

func TestUnslopOwnedCaptureCloseRequiresExactCollection(t *testing.T) {
	for _,mode:=range []string{"not-started","not-assigned","no-wait","not-reaped","no-native","other-native"} {
		t.Run(mode,func(t *testing.T){
			c,_,live,lease,_:=normalHandoffFixture(t)
			actual:=normalActualLease(lease)
			completion:=retainedTestCompletion()
			switch mode {
			case "not-started":completion.Child.Started=false
			case "not-assigned":completion.Child.Assigned=false
			case "no-wait":completion.Child.WaitVerified=false
			case "not-reaped":completion.Child.Reaped=false
			case "no-native":completion.NativeProcess=nil
			case "other-native":
				c.ownedCapture.flight.result=&completion
				if !c.ownedCapture.observeNative(){t.Fatal("fixture could not record original native identity")}
				completion.NativeProcess=&askNativeProcess{PID:18,Creation:30}
			}
			c.ownedCapture.flight.result=&completion
			session:=&unslopOwnedCaptureSession{unslopProducerSession:live,proof:c.ownedCapture}
			if err:=session.Close();err==nil||live.closed||live.completed||actual.closed!=0||!c.ownedCapture.capability.current(){t.Fatal("incomplete or different collection released owned inputs",mode)}
		})
	}
}

func normalActualLease(lease softwareRustHeldSession) *softwareRustSyntheticSession {
	return lease.(*softwareRustOwnedSession).capability.session.(*softwareRustSyntheticSession)
}

func TestUnslopNormalHandoffAcceptsOnlyObservedOwnedSlots(t *testing.T) {
	for _,mode:=range []string{"nil-controller","nil-owner","other-session","copied-buffers","same-buffers-other-lease","other-issued-lease","changed-binding","recovered","closed-session","other-flight","other-done","other-generation","copied-proof","closed-capability"} {
		t.Run(mode,func(t *testing.T){
			c,owner,live,lease,_:=normalHandoffFixture(t)
			controller,target:=c,owner
			originalLease:=normalActualLease(lease)
			originalDone:=c.ownedCapture.done
			originalAborts,otherAborts:=0,0
			c.ownedCapture.rawFlight.Abort=func(){originalAborts++}
			switch mode {
			case "nil-controller":controller=nil
			case "nil-owner":target=nil
			case "other-session":live,_,_=retainedTestInputSession(t)
			case "copied-buffers":
				held:=lease.Held();held.Cargo.Raw=append([]byte(nil),held.Cargo.Raw...);held.Rustc.Raw=append([]byte(nil),held.Rustc.Raw...)
				lease=&retainedTestLease{held:held}
			case "same-buffers-other-lease":lease=&retainedTestLease{held:lease.Held()}
			case "other-issued-lease":
				provider,_:=syntheticSoftwareRustProvider();capability,err:=provider.openOwned(context.Background());if err!=nil {t.Fatal(err)}
				lease=capability.lease;t.Cleanup(func(){_ = capability.Close()})
			case "changed-binding":c.observedBinding.SHA=strings.Repeat("0",64)
			case "recovered":c.recovered=true
			case "closed-session":live.closed=true
			case "other-flight":live.flight=&unslopOwnedFlight{flight:&askOwnedProducerFlight{Done:make(chan askProducerFlightResult),Abort:func(){otherAborts++}}}
			case "other-done":live.flight.(*unslopOwnedFlight).flight.Done=make(chan askProducerFlightResult)
			case "other-generation":c.ownedCapture.generation=strings.Repeat("0",32)
			case "copied-proof":copied:=*c.ownedCapture;c.ownedCapture=&copied
			case "closed-capability":
				completion:=retainedTestCompletion();c.ownedCapture.flight.result=&completion
				if err:=lease.Close();err!=nil {t.Fatal(err)}
			}
			beforeLive,beforeLease:=live,lease
			if handoff,err:=newUnslopNormalCaptureHandoff(controller,target,&live,&lease);err==nil||handoff!=nil||live!=beforeLive||lease!=beforeLease {t.Fatal("unobserved ownership was accepted or caller slots changed",mode)}
			wantClosed:=0;if mode=="closed-capability" {wantClosed=1}
			if originalLease.closed!=wantClosed||len(owner.Tickets())!=0 {t.Fatal("rejected handoff closed or registered input",mode)}
			if originalAborts!=0||otherAborts!=0||len(originalDone)!=0 {t.Fatal("rejected handoff consumed or aborted an original flight",mode)}
			if other,ok:=beforeLease.(*retainedTestLease);ok&&other.calls!=0 {t.Fatal("rejected handoff closed the replacement lease")}
		})
	}
}

func TestUnslopNormalHandoffMovesOriginalSlotsWithoutClosing(t *testing.T) {
	c,owner,live,lease,done:=normalHandoffFixture(t)
	originalLive,originalLease:=live,lease
	h,err:=newUnslopNormalCaptureHandoff(c,owner,&live,&lease)
	if err!=nil||h==nil||live!=nil||lease!=nil||h.live!=originalLive||normalActualLease(h.lease)!=normalActualLease(originalLease) {t.Fatal("handoff did not move exact caller slots",err)}
	aliasLive,aliasLease:=originalLive,originalLease
	if duplicate,err:=newUnslopNormalCaptureHandoff(c,owner,&aliasLive,&aliasLease);err==nil||duplicate!=nil||aliasLive!=originalLive||aliasLease!=originalLease {t.Fatal("same controller adopted input twice")}
	status:=h.Status()
	actual:=normalActualLease(originalLease)
	if status.State!="inputs_owned"||!status.InputsHeld||status.Transferred||status.Qualified||actual.closed!=0 {t.Fatal("adoption began cleanup or implied runtime success",status)}
	if h.lease.Close()==nil||actual.closed!=0||!c.ownedCapture.capability.current(){t.Fatal("uncollected current lease could close capture inputs")}
	if originalLease.Held().Cargo.Raw!=nil||originalLease.Close()==nil||actual.closed!=0 {t.Fatal("moved caller lease alias remained usable")}
	if err=h.Transfer(context.Background());err!=nil {t.Fatal(err)}
	if h.live!=nil||h.lease!=nil||h.ticket==nil||len(owner.Tickets())!=1 {t.Fatal("transfer did not retain exact inputs")}
	ticket:=h.ticket
	if c.ownedCapture.lease.Close()==nil||actual.closed!=0 {t.Fatal("post-transfer caller alias could close owner lease")}
	if err=h.Transfer(context.Background());err!=nil||h.ticket!=ticket||len(owner.Tickets())!=1 {t.Fatal("repeat transfer registered another ticket",err)}
	done<-retainedTestCompletion()
	r:=h.DrainAndSave(context.Background())
	if !r.Saved||!r.Forgotten||actual.closed!=1 {t.Fatal("owned handoff did not reach saved release",r)}
	if again:=h.DrainAndSave(context.Background());!again.Saved||!again.Forgotten||again.RecordSHA!=r.RecordSHA||h.Status().State!="result_saved"||actual.closed!=1 {t.Fatal("saved handoff lost its final result or repeated cleanup",again)}
}

func TestUnslopNormalHandoffKeepsInputsAcrossTransferSaveFailure(t *testing.T) {
	for _,phase:=range []string{"owner-intent","owner"} {
		t.Run(phase,func(t *testing.T){
			c,owner,live,lease,done:=normalHandoffFixture(t)
			originalLive,originalLease:=live,lease
			actual:=normalActualLease(lease)
			h,err:=newUnslopNormalCaptureHandoff(c,owner,&live,&lease);if err!=nil {t.Fatal(err)}
			movedLease:=h.lease
			store:=c.journal.store;write:=store.write;durableTestFailPhase(store,phase)
			if err=h.Transfer(context.Background());err==nil {t.Fatal("fixture did not fail transfer save")}
			status:=h.Status()
			if status.Qualified||actual.closed!=0||live!=nil||lease!=nil||originalLease.Close()==nil {t.Fatal("failed save abandoned or closed owned inputs",status)}
			if phase=="owner-intent"&&(h.live!=originalLive||h.lease!=movedLease||h.ticket!=nil||c.pendingReservation==nil||status.Transferred||!status.InputsHeld) {t.Fatal("failed intent lost original slot ownership",status)}
			if phase=="owner"&&(h.ticket==nil||h.live!=nil||h.lease!=nil||!status.Transferred||status.InputsHeld) {t.Fatal("post-transfer save lost retained ticket",status)}
			store.write=write
			if err=h.Transfer(context.Background());err!=nil {t.Fatal("same handoff did not resume",err)}
			done<-retainedTestCompletion()
			if result:=h.DrainAndSave(context.Background());!result.Saved||!result.Forgotten||actual.closed!=1 {t.Fatal("resumed handoff did not save once",result)}
		})
	}
}

func TestUnslopNormalHandoffCapacityKeepsOriginalInputs(t *testing.T) {
	c,owner,live,lease,done:=normalHandoffFixture(t)
	owner.limit=1
	otherLive,otherLease,otherDone:=retainedTestInputSession(t)
	other,err:=owner.Retain(&otherLive,&otherLease,nil,unslopToolchainCaptureResult{});if err!=nil {t.Fatal(err)}
	originalLease:=normalActualLease(lease)
	h,err:=newUnslopNormalCaptureHandoff(c,owner,&live,&lease);if err!=nil {t.Fatal(err)}
	if err=h.Transfer(context.Background());err==nil||!h.Status().InputsHeld||originalLease.closed!=0 {t.Fatal("capacity failure lost the handoff")}
	otherDone<-retainedTestCompletion();owner.Drain(context.Background(),other);durableTestSaveBeforeForget(t,owner,other)
	if !owner.Forget(other) {t.Fatal("independent capacity owner did not finish")}
	if err=h.Transfer(context.Background());err!=nil {t.Fatal("capacity recovery did not retry same input",err)}
	done<-retainedTestCompletion()
	if r:=h.DrainAndSave(context.Background());!r.Saved||!r.Forgotten||originalLease.closed!=1 {t.Fatal("capacity retry duplicated cleanup",r)}
}

func TestUnslopNormalHandoffCancelledDrainPreservesExactFlight(t *testing.T) {
	c,owner,live,lease,done:=normalHandoffFixture(t)
	originalLease:=normalActualLease(lease)
	aborts:=0;live.flight.(*unslopOwnedFlight).flight.Abort=func(){aborts++}
	h,err:=newUnslopNormalCaptureHandoff(c,owner,&live,&lease);if err!=nil {t.Fatal(err)}
	if err=h.Transfer(context.Background());err!=nil {t.Fatal(err)}
	ctx,cancel:=context.WithCancel(context.Background());cancel()
	for n:=0;n<2;n++ {
		r:=h.DrainAndSave(ctx)
		if r.Saved||r.Forgotten||aborts!=1||originalLease.closed!=0||r.Report.State!="retained"||owner.Forget(h.ticket) {t.Fatal("cancelled handoff released or changed its target",r)}
	}
	done<-retainedTestCompletion()
	if r:=h.DrainAndSave(context.Background());!r.Saved||!r.Forgotten||aborts!=1||originalLease.closed!=1 {t.Fatal("fresh cleanup repeated Abort or Close",r)}
}

func TestUnslopNormalHandoffKeepsUnverifiedAndFailedRelease(t *testing.T) {
	for _,mode:=range []string{"unverified","close-failure","save-failure"} {
		t.Run(mode,func(t *testing.T){
			c,owner,live,lease,done:=normalHandoffFixture(t)
			originalLease:=normalActualLease(lease)
			h,err:=newUnslopNormalCaptureHandoff(c,owner,&live,&lease);if err!=nil {t.Fatal(err)}
			if err=h.Transfer(context.Background());err!=nil {t.Fatal(err)}
			completion:=retainedTestCompletion();if mode=="unverified" {completion.Child.Reaped=false}
			if mode=="close-failure" {originalLease.closeError=true}
			store:=c.journal.store;write:=store.write
			if mode=="save-failure" {store.write=func(f *os.File,raw []byte)(int,error){if strings.Contains(string(raw),`"state":"released"`){return 0,io.ErrShortWrite};return write(f,raw)}}
			done<-completion
			r:=h.DrainAndSave(context.Background())
			if r.Forgotten||owner.Forget(h.ticket) {t.Fatal("unsaved or failed handoff was forgotten",r)}
			if mode=="unverified" {
				if !r.Saved||r.Report.State!="retained"||originalLease.closed!=0 {t.Fatal("unverified flight was closed",r)}
				done<-retainedTestCompletion()
				if again:=h.DrainAndSave(context.Background());again.Report.Collection!="unverified"||len(done)!=1||originalLease.closed!=0 {t.Fatal("later result replaced original flight",again)}
			} else if mode=="close-failure" {
				if !r.Saved||r.Report.State!="release_failed"||originalLease.closed!=1 {t.Fatal("close failure was not preserved",r)}
				if c.ownedCapture.capability.current(){t.Fatal("Close failure left capability reusable")}
				if again:=h.DrainAndSave(context.Background());again.Report.State!="release_failed"||originalLease.closed!=1 {t.Fatal("failed Close was repeated",again)}
			} else {
				if r.Saved||originalLease.closed!=1||r.Report.State!="released" {t.Fatal("failed save claimed success or lost release",r)}
				store.write=write
				if again:=h.DrainAndSave(context.Background());!again.Saved||!again.Forgotten||originalLease.closed!=1 {t.Fatal("report retry closed inputs twice",again)}
			}
			encoded,err:=json.Marshal(h.Status())
			if err!=nil||strings.Contains(string(encoded),store.path)||strings.Contains(string(encoded),"private") {t.Fatal("handoff status exposed source details")}
		})
	}
}

func TestUnslopOwnedInputsRetainedFollowsOwnerMoveAndRelease(t *testing.T) {
	for _,mode:=range []string{"retained","released","release-failed","pending-intent"} {
		t.Run(mode,func(t *testing.T){
			c,owner,live,lease,done:=normalHandoffFixture(t)
			actual:=normalActualLease(lease)
			if before:=c.RetryFinal(context.Background());!before.OwnedInputsRetained {t.Fatal("owned capture lost original input status",before)}
			h,err:=newUnslopNormalCaptureHandoff(c,owner,&live,&lease);if err!=nil {t.Fatal(err)}
			store:=c.journal.store;write:=store.write
			if mode=="pending-intent" {durableTestFailPhase(store,"owner-intent")}
			err=h.Transfer(context.Background())
			if mode=="pending-intent" {
				if err==nil {t.Fatal("fixture did not fail pending intent")}
			} else if err!=nil {t.Fatal(err)}
			if moved:=c.RetryFinal(context.Background());!moved.OwnedInputsRetained||actual.closed!=0 {t.Fatal("handoff owner move hid held inputs",mode,moved)}
			store.write=write
			if mode=="pending-intent" {if err=h.Transfer(context.Background());err!=nil {t.Fatal(err)}}
			if mode=="retained" {return}
			if mode=="release-failed" {actual.closeError=true}
			check:=actual.held.Recheck
			actual.held.Recheck=func(ctx context.Context)error {
				during:=c.RetryFinal(ctx)
				if !during.OwnedInputsRetained||during.RetainedOwner==nil||during.RetainedOwner.State!="release_in_progress" {t.Fatal("release in progress hid input ownership",during)}
				return check(ctx)
			}
			done<-retainedTestCompletion()
			if mode=="released" {
				if released:=owner.Drain(context.Background(),h.ticket);released.State!="released"||owner.Forget(h.ticket) {t.Fatal("fixture did not keep released but unsaved owner",released)}
				if beforeSave:=c.RetryFinal(context.Background());beforeSave.OwnedInputsRetained||beforeSave.RetainedOwner==nil||beforeSave.RetainedOwner.State!="released" {t.Fatal("released inputs were reported held while report remained unsaved",beforeSave)}
			}
			result:=h.DrainAndSave(context.Background())
			wantHeld:=mode=="release-failed"
			if actual.closed!=1||result.Forgotten==wantHeld {t.Fatal("fixture did not reach expected owner release",mode,result)}
			final:=c.RetryFinal(context.Background())
			if final.OwnedInputsRetained!=wantHeld {t.Fatal("final ownership flag disagreed with retained report",mode,final)}
			if wantHeld&&(final.RetainedOwner==nil||final.RetainedOwner.State!="release_failed"||c.ownedCapture.capability.current()) {t.Fatal("cleanup references and invalid capability were conflated",final)}
			encoded,err:=json.Marshal(final);if err!=nil {t.Fatal(err)}
			var consumer map[string]any
			if json.Unmarshal(encoded,&consumer)!=nil||wantHeld&&consumer["owned_inputs_retained"]!=true||!wantHeld&&consumer["owned_inputs_retained"]==true {t.Fatal("ownership flag did not reach serialized consumer",mode)}
		})
	}
}
