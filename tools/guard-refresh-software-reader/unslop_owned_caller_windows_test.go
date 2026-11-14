//go:build windows

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"syscall"
	"io"
	"os"
	"strings"
	"sync"
	"time"
	"testing"
)

func ownedCallerFixture(t *testing.T, collected bool) (*unslopOwnedCaller, *normalOwnedSyntheticCapture, *syntheticUnslopPending, *softwareRustSyntheticAccess, chan askProducerFlightResult) {
	t.Helper()
	root, err := newUnslopCallerHeldRoot(newDurableTestStore(t))
	if err != nil { t.Fatal(err) }
	provider, access := syntheticSoftwareRustProvider()
	done := make(chan askProducerFlightResult, 1)
	flight := &askOwnedProducerFlight{Done:done, Abort:func(){}}
	live := &unslopLiveSession{unslopNativeSession:&unslopNativeSession{}, start:func(context.Context,string,string,string)(unslopLiveFlight,error){ return &unslopOwnedFlight{flight:flight},nil }}
	capture := &normalOwnedSyntheticCapture{unslopSyntheticCapture:syntheticUnslopCapture(),live:live,collected:collected}
	pending := &syntheticUnslopPending{}
	if collected { done <- retainedTestCompletion() }
	caller, err := newUnslopOwnedCaller(root, provider, func(context.Context,rustToolchainHeld,unslopRustToolchainBinding)(unslopCaptureAccess,error){
		return unslopCaptureAccess{begin:func(context.Context)(unslopProducerSession,error){ return &unslopOwnedCaptureSubject{unslopProducerSession:capture,live:live},nil },stage:func(context.Context,unslopCaptureReceipt,[]byte)(unslopPendingCapture,error){ return pending,nil }},nil
	})
	if err != nil { t.Fatal(err) }
	t.Cleanup(func(){unslopCallerRoots.Lock();defer unslopCallerRoots.Unlock();delete(unslopCallerRoots.stores,root.store);delete(unslopCallerRoots.identities,root.identity)})
	return caller,capture,pending,access,done
}

func TestUnslopOwnedCallerNormalRouteFailsBeforeAcquisition(t *testing.T) {
	var out bytes.Buffer
	if exit := runNormalUnslopOwnedCapture(context.Background(), &out); exit != 1 { t.Fatal("unavailable owner was accepted",exit) }
	var result unslopOwnedCallerResult
	if err:=json.Unmarshal(out.Bytes(),&result);err!=nil { t.Fatal(err) }
	if result.CallerReason!="capture_owner_unavailable"||result.Qualified||result.ResultSaved||result.OwnedInputsRetained||result.CleanupComplete||result.DataAccessGranted||result.UpdatePerformed { t.Fatal("normal unconnected owner became authority",result) }
	if strings.Contains(out.String(),"C:")||strings.Contains(out.String(),"D:") { t.Fatal("fixed failure disclosed a path") }
}

func TestUnslopOwnedCallerStartsOnlyOnceAndRetriesSameFinal(t *testing.T) {
	caller,capture,pending,access,_:=ownedCallerFixture(t,true)
	store:=caller.root.store
	write:=store.write
	durableTestFailPhase(store,"final")
	first:=caller.Begin(context.Background())
	if first.ResultSaved||!first.ReceiptPublished||first.CleanupComplete||capture.builds!=1||pending.commits!=1||access.opens!=1 { t.Fatal("fixture did not fail after one publication",first) }
	store.write=write
	second:=caller.Begin(context.Background())
	if second.ResultSaved||capture.builds!=1||pending.commits!=1||access.opens!=1 { t.Fatal("Begin restarted an existing capture",second) }
	final:=caller.Resume(context.Background())
	if !final.ResultSaved||!final.CleanupComplete||final.CleanupPending||!final.Qualified||final.OwnedInputsRetained||caller.root.store!=nil||capture.builds!=1||pending.commits!=1||access.opens!=1||access.session.closed!=1 { t.Fatal("same final retry did not complete exactly once",final) }
	if again:=caller.Resume(context.Background());!again.CleanupComplete||capture.builds!=1||access.session.closed!=1 { t.Fatal("completed caller repeated work",again) }
}

func TestUnslopOwnedCallerRetainsAfterCancelledRequest(t *testing.T) {
	caller,capture,pending,access,done:=ownedCallerFixture(t,false)
	first:=caller.Begin(context.Background())
	if !first.ResultSaved||!first.CleanupPending||!first.OwnedInputsRetained||first.CaptureCleanupFailed||access.opens!=1||capture.builds!=1||pending.commits!=0 { t.Fatal("fixture did not hold an uncollected flight",first) }
	transferred:=caller.Resume(context.Background())
	if caller.handoff==nil||caller.handoff.ticket==nil||!transferred.OwnedInputsRetained||access.session.closed!=0 { t.Fatal("caller did not transfer its acquired session",transferred) }
	aborts:=0
	caller.controller.ownedCapture.rawFlight.Abort=func(){aborts++}
	ctx,cancel:=context.WithCancel(context.Background());cancel()
	cancelled:=caller.Resume(ctx)
	if aborts!=1||!cancelled.CleanupPending||!cancelled.OwnedInputsRetained||access.session.closed!=0||len(done)!=0 { t.Fatal("request cancellation discarded the actual owner",cancelled,aborts) }
	done<-retainedTestCompletion()
	final:=caller.Resume(context.Background())
	if !final.CleanupComplete||final.CleanupPending||final.OwnedInputsRetained||final.CaptureCleanupFailed||final.Qualified||aborts!=1||access.session.closed!=1||capture.builds!=1||access.opens!=1 { t.Fatal("same failed capture did not finish its own cleanup",final) }
}

func TestUnslopOwnedCallerPreservesRootAndReportOnTransferSaveFailure(t *testing.T) {
	caller,_,_,access,done:=ownedCallerFixture(t,false)
	caller.Begin(context.Background())
	store:=caller.root.store;write:=store.write
	durableTestFailPhase(store,"owner-intent")
	failed:=caller.Resume(context.Background())
	if !failed.CleanupPending||!failed.OwnedInputsRetained||caller.handoff==nil||caller.handoff.ticket!=nil||caller.root.store!=store||access.session.closed!=0 { t.Fatal("pre-transfer save failure lost original inputs",failed) }
	store.write=write
	caller.Resume(context.Background())
	if caller.handoff.ticket==nil {t.Fatal("retry did not move the original flight")}
	done<-retainedTestCompletion()
	final:=caller.Resume(context.Background())
	if !final.CleanupComplete||access.session.closed!=1||access.opens!=1 { t.Fatal("transfer retry reacquired or leaked the lease",final) }
}

func TestUnslopOwnedCallerCloseFailureKeepsInvalidCapabilityAndReferences(t *testing.T) {
	caller,_,_,access,done:=ownedCallerFixture(t,false)
	caller.Begin(context.Background())
	caller.Resume(context.Background())
	access.session.closeError=true
	done<-retainedTestCompletion()
	failed:=caller.Resume(context.Background())
	proof:=caller.controller.ownedCapture
	if !failed.CleanupPending||!failed.OwnedInputsRetained||failed.CleanupComplete||proof.capability.current()||caller.root.store==nil||caller.handoff==nil||access.session.closed!=1 { t.Fatal("invalid capability was treated as completed cleanup",failed) }
	again:=caller.Resume(context.Background())
	if !again.CleanupPending||access.session.closed!=1||proof.capability.session!=access.session||len(caller.owner.Tickets())!=1 { t.Fatal("failed Close was retried or its report forgotten",again) }
}

func TestUnslopOwnedCallerPartialErrorKeepsExactSession(t *testing.T) {
	root,err:=newUnslopCallerHeldRoot(newDurableTestStore(t));if err!=nil {t.Fatal(err)}
	provider,access:=syntheticSoftwareRustProvider()
	partial:=syntheticUnslopCapture()
	caller,err:=newUnslopOwnedCaller(root,provider,func(context.Context,rustToolchainHeld,unslopRustToolchainBinding)(unslopCaptureAccess,error){return unslopCaptureAccess{begin:func(context.Context)(unslopProducerSession,error){return partial,errors.New("private failure C:/private")},stage:func(context.Context,unslopCaptureReceipt,[]byte)(unslopPendingCapture,error){t.Fatal("partial begin reached stage");return nil,errUnslopDurable}},nil})
	if err!=nil {t.Fatal(err)}
	first:=caller.Begin(context.Background())
	for i:=0;i<2;i++ {
		r:=caller.Resume(context.Background())
		if !r.CleanupPending||!r.OwnedInputsRetained||r.CallerReason!="capture_partial_cleanup_pending"||r.CleanupComplete||caller.controller.ownedCapture.rejectedSession!=partial||caller.root.store==nil||access.session.closed!=0||partial.closed { t.Fatal("partial failure lost its original owner",first,r) }
		b,err:=json.Marshal(r);if err!=nil {t.Fatal(err)}
		if strings.Contains(string(b),"private")||strings.Contains(string(b),"C:/") {t.Fatal("raw error reached caller output")}
	}
}

func TestUnslopOwnedCallerRejectsCopiedAndRepeatedRootOwnership(t *testing.T) {
	caller,_,_,access,_:=ownedCallerFixture(t,false)
	copyRoot:=*caller.root
	provider,_:=syntheticSoftwareRustProvider()
	if other,err:=newUnslopOwnedCaller(&copyRoot,provider,caller.controller.factory);err==nil||other!=nil {t.Fatal("copied root became an owned acquisition")}
	if other,err:=newUnslopOwnedCaller(caller.root,provider,caller.controller.factory);err==nil||other!=nil {t.Fatal("same root acquired another caller")}
	if access.opens!=0||caller.started {t.Fatal("rejected root started capture")}
}

func TestUnslopOwnedCallerReleasedReportMustSaveBeforeRootRelease(t *testing.T) {
	caller,_,_,access,done:=ownedCallerFixture(t,false)
	caller.Begin(context.Background());caller.Resume(context.Background())
	store:=caller.root.store;write:=store.write
	store.write=func(f *os.File,b []byte)(int,error){if bytes.Contains(b,[]byte("\"state\":\"released\"")){return 0,io.ErrShortWrite};return write(f,b)}
	done<-retainedTestCompletion()
	failed:=caller.Resume(context.Background())
	if !failed.CleanupPending||failed.CleanupComplete||failed.OwnedInputsRetained||caller.root.store!=store||access.session.closed!=1||caller.handoff.final!=nil||len(caller.owner.Tickets())!=1 {t.Fatal("unsaved release report lost the owning root",failed)}
	store.write=write
	final:=caller.Resume(context.Background())
	if !final.CleanupComplete||caller.root.store!=nil||access.session.closed!=1||len(caller.owner.Tickets())!=0 {t.Fatal("same report retry did not release root exactly once",final)}
}

func TestUnslopOwnedCallerStartSaveFailureRetriesBeforeOnlyCapture(t *testing.T) {
	caller,capture,pending,access,_:=ownedCallerFixture(t,true)
	store:=caller.root.store;write:=store.write
	durableTestFailPhase(store,"started")
	first:=caller.Begin(context.Background())
	if first.ResultSaved||caller.controller.attempted||access.opens!=0||capture.builds!=0||pending.commits!=0 {t.Fatal("start failure reached capture",first)}
	store.write=write
	final:=caller.Resume(context.Background())
	if !final.CleanupComplete||!final.Qualified||access.opens!=1||capture.builds!=1||pending.commits!=1 {t.Fatal("start retry did not run exactly one capture",final)}
}

func TestUnslopOwnedCallerRejectsCopiedControllerOwner(t *testing.T) {
	caller,capture,pending,access,_:=ownedCallerFixture(t,false)
	copyCaller:=&unslopOwnedCaller{root:caller.root,provider:caller.provider,controller:caller.controller,owner:caller.owner}
	for _,result:=range []unslopOwnedCallerResult{copyCaller.Begin(context.Background()),copyCaller.Resume(context.Background())} {
		if result.CallerReason!="capture_owner_mismatch"||result.Qualified||result.CleanupComplete {t.Fatal("copied caller became owner",result)}
	}
	if caller.started||capture.builds!=0||pending.commits!=0||access.opens!=0||access.session.closed!=0 {t.Fatal("copied caller affected original capture")}
}

func TestUnslopOwnedCallerFinalRetryAfterForgetDoesNotTransferAgain(t *testing.T) {
	caller,capture,pending,access,done:=ownedCallerFixture(t,false)
	caller.Begin(context.Background());caller.Resume(context.Background())
	store:=caller.root.store
	done<-retainedTestCompletion()
	store.readHook=func()error {if len(caller.owner.Tickets())==0 {return errUnslopDurable};return nil}
	first:=caller.Resume(context.Background())
	if first.CleanupComplete||first.ResultSaved||caller.root.store==nil||access.session.closed!=1||caller.handoff.final==nil||!caller.handoff.final.Forgotten {t.Fatal("fixture did not fail final saving after Forget",first)}
	store.hook=nil;store.readHook=nil
	final:=caller.Resume(context.Background())
	if !final.CleanupComplete||caller.handoff.final==nil||!caller.handoff.final.Forgotten||access.session.closed!=1||capture.builds!=1||pending.commits!=0 {t.Fatal("same final retry repeated transfer or cleanup",final)}
}


func TestUnslopOwnedCallerCaptureCloseFailureKeepsRootAndFixedFacts(t *testing.T) {
	for _,stage:=range []string{"before-flight","after-collection"} {
		for _,fail:=range []bool{false,true} {
			t.Run(fmt.Sprintf("%s/failure-%t",stage,fail),func(t *testing.T){
				root,err:=newUnslopCallerHeldRoot(newDurableTestStore(t));if err!=nil {t.Fatal(err)}
				provider,access:=syntheticSoftwareRustProvider()
				resource:=&retainedTestResource{fail:fail}
				done:=make(chan askProducerFlightResult,1);done<-retainedTestCompletion()
				live:=&unslopLiveSession{unslopNativeSession:&unslopNativeSession{},start:func(context.Context,string,string,string)(unslopLiveFlight,error){return &unslopOwnedFlight{flight:&askOwnedProducerFlight{Done:done,Abort:func(){}}},nil}}
				capture:=&ownedCallerCloseSession{normalOwnedSyntheticCapture:&normalOwnedSyntheticCapture{unslopSyntheticCapture:syntheticUnslopCapture(),live:live,collected:true},beforeFlight:stage=="before-flight",lateResource:resource}
				pending:=&syntheticUnslopPending{}
				caller,err:=newUnslopOwnedCaller(root,provider,func(context.Context,rustToolchainHeld,unslopRustToolchainBinding)(unslopCaptureAccess,error){return unslopCaptureAccess{begin:func(context.Context)(unslopProducerSession,error){return &unslopOwnedCaptureSubject{unslopProducerSession:capture,live:live},nil},stage:func(context.Context,unslopCaptureReceipt,[]byte)(unslopPendingCapture,error){return pending,nil}},nil})
				if err!=nil {t.Fatal(err)}
				if len(caller.captureCleanupRefs)!=0||live.evidence!=nil||resource.calls!=0 {t.Fatal("fixture did not defer resource acquisition until Build")}
				result:=caller.Begin(context.Background())
				if !result.ResultSaved||result.CaptureCleanupFailed!=fail||result.RootCleanupFailed||access.session.closed!=1||result.ToolchainCleanupFailed||resource.calls!=1 {t.Fatal("capture and toolchain cleanup facts were conflated",result,resource.calls)}
				if fail {
					if result.CleanupComplete||!result.CleanupPending||!result.CaptureCleanupRefsRetained||root.closeAttempted||root.store==nil||result.CaptureReason!="capture_cleanup_failed_preserved" {t.Fatal("capture Close failure completed cleanup",result)}
					if stage=="before-flight"&&caller.controller.ownedCapture.flight!=nil||stage=="after-collection"&&(caller.controller.ownedCapture.flight==nil||!live.completed) {t.Fatal("fixture did not reach the selected collection boundary")}
					found:=false;for _,held:=range caller.captureCleanupRefs {found=found||held==resource};if !found {t.Fatal("failed actual resource reference was discarded")}
					beforeReason,beforeCapture,beforeToolchain,published:=result.Reason,result.CaptureReason,result.ToolchainReason,result.ReceiptPublished
					again:=caller.Resume(context.Background())
					if !again.CaptureCleanupFailed||again.CleanupComplete||again.Reason!=beforeReason||again.CaptureReason!=beforeCapture||again.ToolchainReason!=beforeToolchain||again.ReceiptPublished!=published||resource.calls!=1||access.session.closed!=1 {t.Fatal("cleanup retry changed primary result or repeated Close",again)}
					encoded,_:=json.Marshal(again);var fields map[string]any
					if json.Unmarshal(encoded,&fields)!=nil||fields["capture_cleanup_failed"]!=true||fields["capture_cleanup_refs_retained"]!=true||strings.Contains(string(encoded),"private") {t.Fatal("capture cleanup fact did not reach safe output")}
				} else if !result.CleanupComplete||result.CleanupPending||result.CaptureCleanupRefsRetained {t.Fatal("successful close was held as a cleanup failure",result)}
			})
		}
	}
}

type ownedCallerCloseSession struct {
	*normalOwnedSyntheticCapture
	beforeFlight bool
	lateResource *retainedTestResource
}
func (s *ownedCallerCloseSession) Build(ctx context.Context) (unslopBuildInvocation,error) {
	s.live.evidence=s.lateResource
	if s.beforeFlight {s.builds++;return unslopBuildInvocation{},errors.New("synthetic preflight failure")}
	return s.normalOwnedSyntheticCapture.Build(ctx)
}

func TestUnslopOwnedCallerConstructorReturnsUnusedRootCleanupFailure(t *testing.T) {
	for _,fail:=range []bool{false,true} {
		t.Run(fmt.Sprintf("close-failure-%t",fail),func(t *testing.T){
			root,err:=newUnslopCallerHeldRoot(newDurableTestStore(t));if err!=nil {t.Fatal(err)}
			var closed syscall.Handle
			if fail {
				if len(root.heldParents)==0 {t.Fatal("fixture has no owned ancestor")}
				closed=root.heldParents[0];if err=syscall.CloseHandle(closed);err!=nil {t.Fatal(err)}
			}
			caller,err:=newUnslopOwnedCaller(root,nil,nil)
			if caller!=nil||!errors.Is(err,errUnslopCallerOwnerUnavailable)||errors.Is(err,errUnslopCallerRootCleanup)!=fail||!root.closeAttempted {t.Fatal("unused-root primary and cleanup failure were not both returned",err)}
			var cleanup *unslopCallerInitializationError
			if fail {
				if !errors.As(err,&cleanup)||!cleanup.CleanupFailed||cleanup.root!=root||root.store==nil||len(root.heldParents)==0||root.heldParents[0]!=closed {t.Fatal("cleanup error lost the actual root references")}
				firstErr:=root.closeError
				if repeated,repeatErr:=newUnslopOwnedCaller(root,nil,nil);repeated!=nil||!errors.Is(repeatErr,errUnslopCallerRootCleanup)||root.closeError!=firstErr {t.Fatal("same failed root lost its cleanup result")}
				if root.abandon()==nil||root.closeError!=firstErr||err.Error()!="capture_owner_unavailable" {t.Fatal("constructor error retried Close or leaked native text")}
			} else if errors.As(err,&cleanup)||root.store!=nil {t.Fatal("successful unused-root cleanup was misclassified")}
		})
	}
	active,_,_,access,_:=ownedCallerFixture(t,false)
	_,err:=newUnslopOwnedCaller(active.root,nil,nil)
	if !errors.Is(err,errUnslopCallerOwnerUnavailable)||errors.Is(err,errUnslopCallerRootCleanup)||active.root.closeAttempted||access.session.closed!=0 {t.Fatal("foreign constructor closed the active caller or claimed cleanup failed")}
}


func TestUnslopOwnedCallerKeepsIndependentSessionAndPendingFailures(t *testing.T) {
	for _,mode:=range []string{"normal","session-only","pending-only","both","pending-abort","published-pending-close","uncollected"} {
		t.Run(mode,func(t *testing.T){
			collected:=mode!="uncollected"
			caller,capture,_,access,done:=ownedCallerFixture(t,collected)
			resource:=&retainedTestResource{fail:mode=="session-only"||mode=="both"}
			resource.duringClose=func(){if attempted,failed:=caller.controller.ownedCapture.captureCloseResult();attempted||failed {t.Fatal("in-progress Close was reported completed")}}
			capture.live.evidence=resource
			pending:=&ownedCallerCountedPending{syntheticUnslopPending:&syntheticUnslopPending{},closeFail:mode=="pending-only"||mode=="both"||mode=="published-pending-close",abortFail:mode=="pending-abort"}
			if mode=="pending-abort"||mode=="pending-only" {pending.syntheticUnslopPending.fault="seal_failed"}
			originalFactory:=caller.controller.factory
			caller.controller.factory=caller.captureFactory(func(ctx context.Context,held rustToolchainHeld,binding unslopRustToolchainBinding)(unslopCaptureAccess,error){
				access,err:=originalFactory(ctx,held,binding);if err!=nil {return access,err}
				access.stage=func(context.Context,unslopCaptureReceipt,[]byte)(unslopPendingCapture,error){return pending,nil}
				return access,nil
			})
			result:=caller.Begin(context.Background())
			wantSession:=mode=="session-only"||mode=="both"
			wantPending:=pending.closeFail||pending.abortFail
			if !result.ResultSaved||result.CaptureCleanupFailed!=wantSession||result.PendingCloseFailed!=pending.closeFail||result.PendingAbortFailed!=pending.abortFail||result.ToolchainCleanupFailed||pending.closes!=1||access.opens!=1||capture.builds!=1 {t.Fatal("compound cleanup facts were collapsed",mode,result,pending.closes)}
			if mode=="uncollected" {
				if !result.CleanupPending||result.CaptureCleanupFailed||access.session.closed!=0 {t.Fatal("uncollected Close refusal became an actual failure",result)}
				caller.Resume(context.Background());done<-retainedTestCompletion()
				final:=caller.Resume(context.Background())
				if !final.CleanupComplete||final.CaptureCleanupFailed||final.PendingCloseFailed||final.PendingAbortFailed||pending.closes!=1||resource.calls!=1||access.session.closed!=1 {t.Fatal("normal same-flight recovery was blocked",final)}
				return
			}
			if attempted,failed:=caller.controller.ownedCapture.captureCloseResult();!attempted||failed!=wantSession {t.Fatal("actual session Close result was not observed",mode,attempted,failed)}
			if result.CleanupComplete==(wantSession||wantPending)||caller.root.closeAttempted==(wantSession||wantPending)||resource.calls!=1||access.session.closed!=1 {t.Fatal("compound failure released root or repeated cleanup",mode,result)}
			if mode=="pending-only"&&(result.ReceiptPublished||pending.commits!=0||pending.abortCalls!=1) {t.Fatal("pre-publication pending failure crossed Commit",result)}
			if mode=="both"&&(result.CaptureReason!="capture_export_close_failed"||!result.CaptureCleanupFailed||!result.PendingCloseFailed) {t.Fatal("later export reason erased earlier actual Close failure",result)}
			if mode=="published-pending-close"&&(!result.ReceiptPublished||pending.commits!=1) {t.Fatal("cleanup failure erased committed publication",result)}
			if mode=="normal"&&(!result.Qualified||!result.ReceiptPublished||pending.commits!=1) {t.Fatal("normal capture did not use the successful commit",result)}
			beforeReason,beforeCapture,beforeToolchain,published:=result.Reason,result.CaptureReason,result.ToolchainReason,result.ReceiptPublished
			again:=caller.Resume(context.Background())
			if again.Reason!=beforeReason||again.CaptureReason!=beforeCapture||again.ToolchainReason!=beforeToolchain||again.ReceiptPublished!=published||again.CaptureCleanupFailed!=wantSession||again.PendingCloseFailed!=pending.closeFail||again.PendingAbortFailed!=pending.abortFail||pending.closes!=1||resource.calls!=1||access.session.closed!=1 {t.Fatal("retry changed original outcome or repeated Close",again)}
			if wantPending {
				if !again.PendingCleanupRefsRetained||caller.pendingCleanup==nil {t.Fatal("pending failure lost original owner")}
				encoded,_:=json.Marshal(again);var fields map[string]any
				if json.Unmarshal(encoded,&fields)!=nil||fields["pending_cleanup_refs_retained"]!=true||pending.closeFail&&fields["pending_close_failed"]!=true||pending.abortFail&&fields["pending_abort_failed"]!=true||strings.Contains(string(encoded),"private") {t.Fatal("pending cleanup facts did not reach safe output")}
			}
		})
	}
}

type ownedCallerCountedPending struct {
	*syntheticUnslopPending
	closeFail,abortFail bool
	closes,abortCalls int
}
func (p *ownedCallerCountedPending) Close() error {p.closes++;if p.closeFail {return errors.New("private pending close failure")};return nil}
func (p *ownedCallerCountedPending) Abort() error {p.abortCalls++;p.syntheticUnslopPending.Abort();if p.abortFail {return errors.New("private pending abort failure")};return nil}

func TestUnslopOwnedCallerPendingRetainsActualNativeExportAfterCloseFailure(t *testing.T) {
	export:=&retainedTestResource{fail:true}
	native:=&unslopNativePending{export:export}
	pending:=newUnslopCallerPending(native)
	if pending.Close()==nil||pending.Close()==nil||export.calls!=1||native.export!=nil {t.Fatal("native pending Close did not fail exactly once")}
	closeFailed,abortFailed,refs:=pending.cleanupFacts()
	if !closeFailed||abortFailed||len(refs)==0 {t.Fatal("native pending failure was not observed")}
	found:=false;for _,ref:=range refs {found=found||ref==export};if !found {t.Fatal("actual export reference was lost when native Close cleared its field")}
}

func TestUnslopOwnedCaptureCloseObservationUsesSameActualCall(t *testing.T) {
	for _,fail:=range []bool{false,true} {
		t.Run(fmt.Sprintf("close-failure-%t",fail),func(t *testing.T){
			c,_,live,lease,done,actual:=normalOwnedCaptureFixture(t,false)
			entered,release,closeDone:=make(chan struct{}),make(chan struct{}),make(chan struct{})
			var releaseOnce sync.Once
			unblock:=func(){releaseOnce.Do(func(){close(release)})}
			resource:=&retainedTestResource{fail:fail,duringClose:func(){close(entered);<-release}}
			live.evidence=resource
			session:=&unslopOwnedCaptureSession{unslopProducerSession:live,proof:c.ownedCapture}
			queued,collected,started:=false,false,false
			var closeErr error
			collect:=func()error {
				if collected {return nil}
				if !queued {done<-retainedTestCompletion();queued=true}
				ctx,cancel:=context.WithTimeout(context.Background(),5*time.Second);defer cancel()
				if _,_,err:=live.flight.Done(ctx);err!=nil {return err}
				collected=true
				return nil
			}
			startClose:=func(){if !started {started=true;go func(){closeErr=session.Close();close(closeDone)}()}}
			t.Cleanup(func(){
				unblock()
				if err:=collect();err!=nil {t.Errorf("same-flight cleanup collection failed: %v",err);return}
				startClose()
				select {case <-closeDone:case <-time.After(5*time.Second):t.Error("same-session Close did not finish during cleanup");return}
				if !c.ownedCapture.canCloseLease() {t.Error("same issued lease did not reach its regular Close gate");return}
				if err:=lease.Close();err!=nil {t.Errorf("same issued lease cleanup failed: %v",err)}
				if actual.closed!=1||c.ownedCapture.capability.current()||lease.(*softwareRustOwnedSession).current() {t.Error("same issued lease was not closed once and invalidated",actual.closed)}
				if resource.calls!=1 {t.Error("same capture resource was not closed exactly once during cleanup",resource.calls)}
			})
			if err:=collect();err!=nil {t.Fatal(err)}
			if actual.closed!=0||!c.ownedCapture.capability.current()||c.ownedCapture.capability.session!=actual||c.ownedCapture.lease!=lease {t.Fatal("fixture did not retain the acquired toolchain lease")}
			startClose()
			select {case <-entered:case <-time.After(5*time.Second):t.Fatal("actual Close did not reach the observation barrier")}
			for i:=0;i<16;i++ {if attempted,failed:=c.ownedCapture.captureCloseResult();attempted||failed {t.Fatal("in-progress Close was treated as completed",attempted,failed)}}
			unblock()
			select {case <-closeDone:case <-time.After(5*time.Second):t.Fatal("actual Close did not finish")}
			if attempted,failed:=c.ownedCapture.captureCloseResult();!attempted||failed!=fail||(closeErr!=nil)!=fail {t.Fatal("same actual Close observation was lost",attempted,failed,closeErr)}
			session.Close()
			if resource.calls!=1||actual.closed!=0 {t.Fatal("session Close repeated cleanup or closed a different owner",resource.calls,actual.closed)}
		})
	}
}
