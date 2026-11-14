//go:build windows

package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

type retainedTestResource struct { fakeUpdateObject; calls int; name string; order *[]string; fail bool; duringClose func() }
func (r *retainedTestResource) Close() error { r.calls++; if r.order!=nil {*r.order=append(*r.order,r.name)};if r.duringClose!=nil {r.duringClose()};if r.fail {return errors.New("private resource failure")};return nil }
type retainedTestLease struct { held rustToolchainHeld; calls int; order *[]string; fail bool; onClose func() }
func (l *retainedTestLease) Held() rustToolchainHeld {return l.held}
func (l *retainedTestLease) Close() error {l.calls++;if l.order!=nil {*l.order=append(*l.order,"toolchain")};if l.onClose!=nil {l.onClose()};clear(l.held.Cargo.Raw);clear(l.held.Rustc.Raw);if l.fail{return errors.New("private toolchain failure")};return nil}

type retainedTestFixture struct {
	owner *unslopRetainedOwner
	ticket *unslopRetainedTicket
	live *unslopLiveSession
	oldLive *unslopLiveSession
	lease softwareRustHeldSession
	oldLease *retainedTestLease
	done chan askProducerFlightResult
	resource *retainedTestResource
	aborts int
	order []string
}
func newRetainedTestFixture(t *testing.T, cached ...askProducerFlightResult) *retainedTestFixture {
	t.Helper()
	owner,err:=newUnslopRetainedOwner(4)
	if err!=nil {t.Fatal(err)}
	f:=&retainedTestFixture{owner:owner,done:make(chan askProducerFlightResult,1)}
	base:=newSyntheticUnslopToolchainProvider()
	f.oldLease=&retainedTestLease{held:base.held,order:&f.order}
	f.lease=f.oldLease
	f.resource=&retainedTestResource{name:"input",order:&f.order}
	flight:=&askOwnedProducerFlight{Done:f.done,Abort:func(){f.aborts++}}
	f.live=&unslopLiveSession{unslopNativeSession:&unslopNativeSession{},evidence:f.resource,flight:&unslopOwnedFlight{flight:flight}}
	f.oldLive=f.live
	if len(cached)>0 {value:=cached[0];f.live.flight.(*unslopOwnedFlight).result=&value}
	outcome:=unslopToolchainCaptureResult{unslopCaptureResult:unslopCaptureResult{Reason:"toolchain_cancelled",ReceiptPublished:true},CaptureAttempted:true,CaptureReason:"capture_cleanup_failed_preserved",ToolchainReason:"toolchain_cancelled"}
	f.ticket,err=owner.Retain(&f.live,&f.lease,nil,outcome)
	if err!=nil {t.Fatal(err)}
	return f
}
func retainedTestCompletion() askProducerFlightResult {
	return askProducerFlightResult{NativeProcess:&askNativeProcess{PID:17,Creation:29},Child:askChildResult{Started:true,Assigned:true,WaitVerified:true,Reaped:true,Exit:0},ProtocolComplete:true,ACKSent:true,ACKReceived:true}
}

func TestUnslopRetainedOwnerKeepsInputsUntilNativeCollection(t *testing.T) {
	f:=newRetainedTestFixture(t)
	if f.live!=nil||f.lease!=nil {t.Fatal("ownership slots not moved")}
	if err:=f.oldLive.Close();err!=nil||f.resource.calls!=0||f.oldLease.calls!=0 {t.Fatal("moved session still closed retained input")}
	ctx,cancel:=context.WithCancel(context.Background());cancel()
	r:=f.owner.Close(ctx,f.ticket)
	if r.State!="retained"||r.Collection!="pending"||r.CloseAttempts!=0||r.WaitReason!="retained_owner_wait_cancelled"||!r.AbortRequested||f.resource.calls!=0||f.oldLease.calls!=0||f.aborts!=1||!r.Capture.ReceiptPublished||r.Capture.Qualified {t.Fatal("cancellation released input or lost capture",r)}
	f.owner.Close(ctx,f.ticket)
	if f.aborts!=1 {t.Fatal("abort repeated")}
	oldSlot:=f.oldLive
	var oldLease softwareRustHeldSession=f.oldLease
	if _,err:=f.owner.Retain(&oldSlot,&oldLease,nil,unslopToolchainCaptureResult{});err==nil||oldSlot==nil||oldLease==nil {t.Fatal("repeated callback adopted a moved session")}
	f.done<-retainedTestCompletion()
	r=f.owner.Drain(context.Background(),f.ticket)
	if r.State!="released"||r.Collection!="verified"||r.CloseAttempts!=1||f.resource.calls!=1||f.oldLease.calls!=1||strings.Join(f.order,",")!="input,toolchain" {t.Fatal("collected input not released once",r,f.order)}
	if r.Capture.CaptureReason!="capture_cleanup_failed_preserved"||r.Capture.ToolchainReason!="toolchain_cancelled"||!r.Capture.ReceiptPublished||r.Capture.Qualified {t.Fatal("release rewrote prior failure",r)}
	f.owner.Drain(context.Background(),f.ticket)
	f.owner.Close(context.Background(),f.ticket)
	if f.resource.calls!=1||f.oldLease.calls!=1 {t.Fatal("terminal Close repeated")}
	if !f.owner.Forget(f.ticket)||f.owner.Forget(f.ticket) {t.Fatal("released record forget contract")}
}

func TestUnslopRetainedOwnerRejectsIncompleteCollectionProof(t *testing.T) {
	for _,mode:=range []string{"not-started","not-assigned","no-wait","not-reaped","no-native","no-pid","no-created","closed-channel"} {
		t.Run(mode,func(t *testing.T){
			f:=newRetainedTestFixture(t)
			done:=retainedTestCompletion()
			switch mode {case "not-started":done.Child.Started=false;case "not-assigned":done.Child.Assigned=false;case "no-wait":done.Child.WaitVerified=false;case "not-reaped":done.Child.Reaped=false;case "no-native":done.NativeProcess=nil;case "no-pid":done.NativeProcess.PID=0;case "no-created":done.NativeProcess.Creation=0}
			if mode=="closed-channel" {close(f.done)}else{f.done<-done}
			r:=f.owner.Drain(context.Background(),f.ticket)
			if r.State!="retained"||r.Collection!="unverified"||r.CloseAttempts!=0||f.resource.calls!=0||f.oldLease.calls!=0||f.owner.Forget(f.ticket) {t.Fatal("unproven worker released input",mode,r)}
			r=f.owner.Drain(context.Background(),f.ticket)
			if r.CloseAttempts!=0 {t.Fatal("cached unverified result became proof")}
		})
	}
}

func TestUnslopRetainedOwnerReleasesFailedProtocolOnlyAfterReap(t *testing.T) {
	f:=newRetainedTestFixture(t)
	r:=retainedTestCompletion();r.Child.Exit=7;r.Child.TimedOut=true;r.ProtocolComplete=false;r.ACKSent=false;r.ACKReceived=false
	f.done<-r
	out:=f.owner.Drain(context.Background(),f.ticket)
	if out.State!="released"||out.Collection!="verified"||out.ProtocolComplete||!out.WorkerTimedOut||out.WorkerExit==nil||*out.WorkerExit!=7||out.Capture.Qualified||f.oldLease.calls!=1 {t.Fatal("collection was confused with successful protocol",out)}
}

func TestUnslopRetainedOwnerCloseFailureAndReentryAreNotRetried(t *testing.T) {
	f:=newRetainedTestFixture(t)
	f.resource.fail=true
	f.oldLease.fail=true
	var nested unslopRetainedReport
	f.resource.duringClose=func(){nested=f.owner.Close(context.Background(),f.ticket)}
	f.done<-retainedTestCompletion()
	r:=f.owner.Drain(context.Background(),f.ticket)
	if nested.State!="release_in_progress"||r.State!="release_failed"||r.CloseAttempts!=1||r.CloseFailures!=2||f.oldLease.calls!=1||f.resource.calls!=1||f.owner.Forget(f.ticket) {t.Fatal("failure/reentrant close contract",r,nested)}
	raw,err:=json.Marshal(r)
	if err!=nil||strings.Contains(string(raw),"private")||strings.Contains(string(raw),"owned-cargo") {t.Fatal("close error or tool bytes leaked",string(raw))}
	f.owner.Close(context.Background(),f.ticket)
	if f.oldLease.calls!=1||f.resource.calls!=1 {t.Fatal("failed close retried")}
}

func TestUnslopRetainedOwnerSingleDrainWhileWaiting(t *testing.T) {
	f:=newRetainedTestFixture(t)
	ctx,cancel:=context.WithTimeout(context.Background(),5*time.Second);defer cancel()
	entered,release:=make(chan struct{}),make(chan struct{})
	var once sync.Once
	unblock:=func(){once.Do(func(){close(release)})};defer unblock()
	f.oldLease.held.Recheck=func(ctx context.Context)error {close(entered);select {case <-release:return nil;case <-ctx.Done():return ctx.Err()}}
	finished:=make(chan unslopRetainedReport,1)
	f.done<-retainedTestCompletion()
	go func(){finished<-f.owner.Drain(ctx,f.ticket)}()
	select {case <-entered:case <-ctx.Done():t.Fatal("drain did not reach recheck")}
	r2:=f.owner.Drain(context.Background(),f.ticket)
	if r2.State!="release_in_progress"||f.resource.calls!=0||f.oldLease.calls!=0 {t.Fatal("second drain released during first recheck",r2)}
	unblock()
	select {
	case r1:=<-finished:
		if r1.State!="released"||f.resource.calls!=1||f.oldLease.calls!=1 {t.Fatal("parallel drain did not share one release",r1)}
	case <-ctx.Done():t.Fatal("drain did not finish release")
	}
}

func TestUnslopRetainedOwnerCancelledDrainKeepsRegistryReference(t *testing.T) {
	f:=newRetainedTestFixture(t)
	deadline,stop:=context.WithTimeout(context.Background(),5*time.Second);defer stop()
	ctx,cancel:=context.WithCancel(deadline);defer cancel()
	finished:=make(chan struct{})
	go func(){defer close(finished);f.owner.Drain(ctx,f.ticket)}()
	cancel()
	select {case <-finished:case <-deadline.Done():t.Fatal("cancelled drain did not return")}
	if len(f.owner.Tickets())!=1||f.oldLease.calls!=0||f.resource.calls!=0 {t.Fatal("cancel lost retained owner")}
	f.done<-retainedTestCompletion()
	if f.owner.Drain(context.Background(),f.ticket).State!="released" {t.Fatal("normal cleanup could not resume")}
}

func TestUnslopRetainedOwnerCachedTerminalHasSameCollectionRules(t *testing.T) {
	for _,collected:=range []bool{false,true} {
		t.Run(map[bool]string{false:"unreaped",true:"reaped"}[collected],func(t *testing.T){
			result:=retainedTestCompletion();result.Child.Reaped=collected
			f:=newRetainedTestFixture(t,result)
			r:=f.owner.Drain(context.Background(),f.ticket)
			if (r.State=="released")!=collected||f.oldLease.calls!=map[bool]int{false:0,true:1}[collected] {t.Fatal("cached result bypassed native rule",r)}
			if !collected {
				// An unrelated later value cannot revise the already consumed
				// native flight result. A real reaper adapter is still required.
				f.done<-retainedTestCompletion()
				if f.owner.Drain(context.Background(),f.ticket).State!="retained"||f.oldLease.calls!=0 {t.Fatal("caller result replaced unverified terminal")}
			}
		})
	}
}

func TestUnslopRetainedOwnerReportAndCapabilityCannotReleaseAnotherEntry(t *testing.T) {
	f:=newRetainedTestFixture(t)
	forged:=&unslopRetainedTicket{sequence:f.ticket.sequence}
	other,err:=newUnslopRetainedOwner(1);if err!=nil {t.Fatal(err)}
	if f.owner.Drain(context.Background(),forged).State!="unknown"||other.Close(context.Background(),f.ticket).State!="unknown"||f.oldLease.calls!=0 {t.Fatal("printable identity became release authority")}
	if f.owner.Snapshot(f.ticket).OwnerID=="" {t.Fatal("owner identity not retained")}
	f.done<-retainedTestCompletion()
	r:=f.owner.Drain(context.Background(),f.ticket)
	*r.WorkerExit=99;r.NativeProcess.PID=99;r.Capture.Reason="changed caller copy"
	again:=f.owner.Snapshot(f.ticket)
	if *again.WorkerExit!=0||again.NativeProcess.PID!=17||again.Capture.Reason=="changed caller copy" {t.Fatal("report mutation changed owner facts",again)}
}

func TestUnslopRetainedOwnerFailedAdoptionPreservesOriginalSlots(t *testing.T) {
	for _,mode:=range []string{"capacity","duplicate-lease","duplicate-input","duplicate-flight","duplicate-done","nil-input","duplicate-internal","missing-abort"} {
		t.Run(mode,func(t *testing.T){
			first:=newRetainedTestFixture(t)
			if mode=="capacity" {first.owner.limit=1}
			leaseObj:=&retainedTestLease{held:newSyntheticUnslopToolchainProvider().held}
			var lease softwareRustHeldSession=leaseObj
			done:=make(chan askProducerFlightResult,1)
			flight:=&askOwnedProducerFlight{Done:done,Abort:func(){}}
			resource:=&retainedTestResource{}
			live:=&unslopLiveSession{unslopNativeSession:&unslopNativeSession{},flight:&unslopOwnedFlight{flight:flight}}
			var extra []io.Closer
			if mode=="duplicate-lease" {lease=first.oldLease}
			if mode=="duplicate-input" {live.evidence=first.resource}
			if mode=="duplicate-flight" {live.flight=&unslopOwnedFlight{flight:first.owner.entries[first.ticket].flight}}
			if mode=="duplicate-done" {flight.Done=first.owner.entries[first.ticket].flight.Done}
			if mode=="nil-input" {extra=[]io.Closer{(*retainedTestResource)(nil)}}
			if mode=="missing-abort" {flight.Abort=nil}
			if mode=="duplicate-internal" {
				input:=&retainedTestUpdateObject{}
				live.evidence=input;live.tools=[]updateObject{input}
			}
			beforeLive,beforeLease:=live,lease
			if ticket,err:=first.owner.Retain(&live,&lease,extra,unslopToolchainCaptureResult{});err==nil||ticket!=nil||live!=beforeLive||lease!=beforeLease||resource.calls!=0||leaseObj.calls!=0 {t.Fatal("failed transfer changed ownership",mode,err)}
			first.done<-retainedTestCompletion()
			first.owner.Drain(context.Background(),first.ticket)
		})
	}
	for _,limit:=range []int{0,-1,129} {if _,err:=newUnslopRetainedOwner(limit);err==nil {t.Fatal("unbounded registry accepted",limit)}}
}

type retainedTestUpdateObject struct {fakeUpdateObject;calls int}
func (o *retainedTestUpdateObject) Close()error {o.calls++;return o.fakeUpdateObject.Close()}

func TestUnslopRetainedOwnerMovesCaptureInputsAndClearsOldAlias(t *testing.T) {
	owner,err:=newUnslopRetainedOwner(1);if err!=nil {t.Fatal(err)}
	input:=&retainedTestUpdateObject{}
	manifest:=&retainedTestUpdateObject{}
	leaseObj:=&retainedTestLease{held:newSyntheticUnslopToolchainProvider().held}
	var lease softwareRustHeldSession=leaseObj
	done:=make(chan askProducerFlightResult,1)
	live:=&unslopLiveSession{unslopNativeSession:&unslopNativeSession{manifest:manifest},evidence:input,flight:&unslopOwnedFlight{flight:&askOwnedProducerFlight{Done:done,Abort:func(){}}}}
	old,oldBase:=live,live.unslopNativeSession
	ticket,err:=owner.Retain(&live,&lease,nil,unslopToolchainCaptureResult{})
	if err!=nil {t.Fatal(err)}
	if err:=old.Close();err!=nil {t.Fatal(err)}
	if err:=oldBase.Close();err!=nil {t.Fatal(err)}
	if input.calls!=0||manifest.calls!=0||leaseObj.calls!=0 {t.Fatal("old aliases released transferred inputs")}
	done<-retainedTestCompletion()
	if owner.Drain(context.Background(),ticket).State!="released"||input.calls!=1||manifest.calls!=1||leaseObj.calls!=1 {t.Fatal("capture inputs not released once")}
}

func TestUnslopRetainedOwnerCancelDuringReleasePreservesSingleClose(t *testing.T) {
	for _,returnsError:=range []bool{false,true} {
	t.Run(map[bool]string{false:"cancel-with-nil-error",true:"cancel-with-error"}[returnsError],func(t *testing.T){
	f:=newRetainedTestFixture(t)
	ctx,cancel:=context.WithCancel(context.Background());defer cancel()
	f.oldLease.held.Recheck=func(context.Context)error {cancel();if returnsError {return context.Canceled};return nil}
	f.done<-retainedTestCompletion()
	r:=f.owner.Drain(ctx,f.ticket)
	if r.Collection!="verified"||r.State!="release_failed"||r.CloseAttempts!=1||f.oldLease.calls!=1||f.resource.calls!=1||!r.Capture.ReceiptPublished {t.Fatal("cancel during release abandoned or retried cleanup",r)}
	f.owner.Close(context.Background(),f.ticket)
	if f.oldLease.calls!=1||f.resource.calls!=1 {t.Fatal("cancelled release retried Close")}
	})
	}
}

func TestUnslopRetainedOwnerDoesNotSynthesizeExitWithoutWait(t *testing.T) {
	f:=newRetainedTestFixture(t)
	done:=retainedTestCompletion();done.Child.WaitVerified=false;done.Child.Exit=0
	f.done<-done
	r:=f.owner.Drain(context.Background(),f.ticket)
	if r.WorkerExit!=nil||r.Collection!="unverified"||r.CloseAttempts!=0 {t.Fatal("unwaited worker reported success",r)}
}

func TestUnslopRetainedOwnerCancelledUnverifiedResultRequestsAbortOnce(t *testing.T) {
	f:=newRetainedTestFixture(t)
	done:=retainedTestCompletion();done.Child.Reaped=false
	f.done<-done
	if r:=f.owner.Drain(context.Background(),f.ticket);r.Collection!="unverified" {t.Fatal("missing unverified result",r)}
	ctx,cancel:=context.WithCancel(context.Background());cancel()
	for i:=0;i<2;i++ {
		r:=f.owner.Drain(ctx,f.ticket)
		if r.State!="retained"||r.Collection!="unverified"||r.CloseAttempts!=0||!r.AbortRequested||r.WaitReason!="retained_owner_wait_cancelled" {t.Fatal("unverified cancellation lost ownership",r)}
	}
	if f.aborts!=1||f.resource.calls!=0||f.oldLease.calls!=0||f.owner.Forget(f.ticket) {t.Fatal("unverified worker released or aborted twice")}
	// No second value can replace this flight's terminal result. Native reaping
	// needs its own owned adapter and must not be fabricated from this channel.
	f.done<-retainedTestCompletion()
	if r:=f.owner.Drain(context.Background(),f.ticket);r.Collection!="unverified"||len(f.done)!=1 {t.Fatal("second result replaced native terminal",r)}
}

func TestUnslopRetainedOwnerDeadlineDoesNotImplyWorkerTimeout(t *testing.T) {
	f:=newRetainedTestFixture(t)
	ctx,cancel:=context.WithDeadline(context.Background(),time.Unix(0,0));defer cancel()
	r:=f.owner.Drain(ctx,f.ticket)
	if r.WaitReason!="retained_owner_wait_deadline"||r.WorkerTimedOut||!r.AbortRequested||r.CloseAttempts!=0||f.aborts!=1 {t.Fatal("caller deadline became worker timeout",r)}
	f.done<-retainedTestCompletion()
	if r=f.owner.Drain(context.Background(),f.ticket);r.State!="released"||r.WorkerTimedOut||f.oldLease.calls!=1 {t.Fatal("fresh cleanup context could not drain",r)}
}

func TestUnslopRetainedOwnerConcurrentDrainConsumesOneResult(t *testing.T) {
	f:=newRetainedTestFixture(t)
	ctx,cancel:=context.WithTimeout(context.Background(),5*time.Second);defer cancel()
	start:=make(chan struct{})
	results:=make(chan unslopRetainedReport,2)
	for i:=0;i<2;i++ {go func(){<-start;results<-f.owner.Drain(ctx,f.ticket)}()}
	close(start)
	f.done<-retainedTestCompletion()
	for i:=0;i<2;i++ {
		r:=<-results
		if r.State!="released"&&r.State!="release_in_progress" {t.Fatal("parallel drain tried to receive another terminal result",r)}
	}
	if f.owner.Snapshot(f.ticket).State!="released"||f.oldLease.calls!=1||f.resource.calls!=1||len(f.done)!=0 {t.Fatal("parallel drain duplicated Done or Close")}
}

func TestUnslopRetainedOwnerClearsTerminalBuffers(t *testing.T) {
	for _,reaped:=range []bool{false,true} {
		t.Run(map[bool]string{false:"retained",true:"released"}[reaped],func(t *testing.T){
			f:=newRetainedTestFixture(t)
			raw,stdout:=[]byte("private worker result"),[]byte("private captured output")
			done:=retainedTestCompletion();done.Child.Reaped=reaped
			done.Child.Raw=raw;done.Child.Capture=&askChildCaptureResult{Stdout:stdout}
			f.done<-done
			r:=f.owner.Drain(context.Background(),f.ticket)
			for _,buffer:=range [][]byte{raw,stdout} {for _,value:=range buffer {if value!=0 {t.Fatal("terminal buffer was retained")}}}
			encoded,err:=json.Marshal(r)
			if err!=nil||strings.Contains(string(encoded),"private") {t.Fatal("terminal buffer appeared in report")}
		})
	}
}

func retainedTestOpenFile(t *testing.T) *os.File {
	t.Helper()
	f,err:=os.CreateTemp(t.TempDir(),"input-")
	if err!=nil {t.Fatal(err)}
	t.Cleanup(func(){_ = f.Close()})
	return f
}

func retainedTestInputSession(t *testing.T) (*unslopLiveSession,softwareRustHeldSession,chan askProducerFlightResult) {
	t.Helper()
	done:=make(chan askProducerFlightResult,1)
	live:=&unslopLiveSession{unslopNativeSession:&unslopNativeSession{},flight:&unslopOwnedFlight{flight:&askOwnedProducerFlight{Done:done,Abort:func(){}}}}
	lease:=&retainedTestLease{held:newSyntheticUnslopToolchainProvider().held}
	return live,lease,done
}

func retainedTestSetInput(live *unslopLiveSession, lease *softwareRustHeldSession, mode string, f *os.File) []io.Closer {
	switch mode {
	case "native":live.evidence=&nativeUpdateObject{f:f}
	case "snapshot":live.source=&softwareBuildSnapshot{files:[]*os.File{f}}
	case "extra":return []io.Closer{&nativeUpdateObject{f:f}}
	case "lease":*lease=&softwareRustNativeSession{held:newSyntheticUnslopToolchainProvider().held,nodes:[]softwareRustNativeNode{{f:f}}}
	}
	return nil
}

func TestUnslopRetainedOwnerRejectsDistinctWrappersForSameOwnedFile(t *testing.T) {
	for _,ownerMode:=range []string{"native","snapshot","extra","lease"} {
		for _,extraMode:=range []string{"native","snapshot"} {
			t.Run(ownerMode+"/"+extraMode,func(t *testing.T){
				owner,err:=newUnslopRetainedOwner(2);if err!=nil {t.Fatal(err)}
				f:=retainedTestOpenFile(t)
				live,lease,_:=retainedTestInputSession(t)
				extra:=retainedTestSetInput(live,&lease,ownerMode,f)
				var alias io.Closer=&nativeUpdateObject{f:f}
				if extraMode=="snapshot" {alias=&softwareBuildSnapshot{files:[]*os.File{f}}}
				extra=append(extra,alias)
				oldLive,oldLease:=live,lease
				if ticket,err:=owner.Retain(&live,&lease,extra,unslopToolchainCaptureResult{});err==nil||ticket!=nil||live!=oldLive||lease!=oldLease||len(owner.Tickets())!=0 {t.Fatal("overlapping ownership was transferred")}
				if _,err:=f.Stat();err!=nil {t.Fatal("rejected ownership closed the original file")}
			})
		}
	}
}

func TestUnslopRetainedOwnerRejectsFileSharedWithAnotherEntry(t *testing.T) {
	for _,ownerMode:=range []string{"native","snapshot","extra","lease"} {
		for _,extraMode:=range []string{"file","native","snapshot"} {
			t.Run(ownerMode+"/"+extraMode,func(t *testing.T){
				owner,err:=newUnslopRetainedOwner(2);if err!=nil {t.Fatal(err)}
				f:=retainedTestOpenFile(t)
				first,firstLease,firstDone:=retainedTestInputSession(t)
				firstExtra:=retainedTestSetInput(first,&firstLease,ownerMode,f)
				ticket,err:=owner.Retain(&first,&firstLease,firstExtra,unslopToolchainCaptureResult{});if err!=nil {t.Fatal(err)}
				before:=owner.Snapshot(ticket)
				second,secondLease,_:=retainedTestInputSession(t)
				var alias io.Closer=f
				if extraMode=="native" {alias=&nativeUpdateObject{f:f}}
				if extraMode=="snapshot" {alias=&softwareBuildSnapshot{files:[]*os.File{f}}}
				oldSecond,oldLease:=second,secondLease
				if other,err:=owner.Retain(&second,&secondLease,[]io.Closer{alias},unslopToolchainCaptureResult{});err==nil||other!=nil||second!=oldSecond||secondLease!=oldLease||len(owner.Tickets())!=1 {t.Fatal("another entry adopted the original file")}
				after:=owner.Snapshot(ticket)
				if before.OwnerID!=after.OwnerID||after.State!="retained"||after.CloseAttempts!=0 {t.Fatal("rejection changed the existing entry",after)}
				if _,err:=f.Stat();err!=nil {t.Fatal("rejection closed an existing entry's file")}
				firstDone<-retainedTestCompletion()
				if r:=owner.Drain(context.Background(),ticket);r.State!="released" {t.Fatal("original entry could not release once",r)}
			})
		}
	}
}

func TestUnslopRetainedOwnerAcceptsOnlyKnownIndependentExtraOwners(t *testing.T) {
	for _,mode:=range []string{"file","native","snapshot","same-outer","internal-wrapper","internal-file-leaf","independent-files"} {
		t.Run(mode,func(t *testing.T){
			owner,err:=newUnslopRetainedOwner(1);if err!=nil {t.Fatal(err)}
			live,lease,done:=retainedTestInputSession(t)
			f:=retainedTestOpenFile(t)
			var input io.Closer=f
			if mode=="native"||mode=="same-outer"||mode=="internal-wrapper" {input=&nativeUpdateObject{f:f}}
			if mode=="snapshot" {input=&softwareBuildSnapshot{files:[]*os.File{f}}}
			if mode=="internal-file-leaf" {live.evidence=&nativeUpdateObject{f:f}}
			if mode=="internal-wrapper" {live.evidence=input.(*nativeUpdateObject)}
			extra:=[]io.Closer{input,input}
			var other *os.File
			if mode=="independent-files" {other=retainedTestOpenFile(t);extra=append(extra,&nativeUpdateObject{f:other})}
			ticket,err:=owner.Retain(&live,&lease,extra,unslopToolchainCaptureResult{});if err!=nil {t.Fatal(err)}
			done<-retainedTestCompletion()
			if r:=owner.Drain(context.Background(),ticket);r.State!="released"||r.CloseFailures!=0 {t.Fatal("independent or identical input did not close once",r)}
			if _,err:=f.Stat();err==nil {t.Fatal("accepted input was not closed")}
			if other!=nil {if _,err:=other.Stat();err==nil {t.Fatal("independent second file was not closed")}}
		})
	}
	owner,err:=newUnslopRetainedOwner(1);if err!=nil {t.Fatal(err)}
	live,lease,_:=retainedTestInputSession(t)
	unknown:=&retainedTestResource{}
	oldLive,oldLease:=live,lease
	if ticket,err:=owner.Retain(&live,&lease,[]io.Closer{unknown},unslopToolchainCaptureResult{});err==nil||ticket!=nil||live!=oldLive||lease!=oldLease||unknown.calls!=0 {t.Fatal("unknown extra ownership was inferred")}
}

func TestUnslopRetainedOwnerRejectsExtraNativeHandleAlias(t *testing.T) {
	for _,crossEntry:=range []bool{false,true} {
	t.Run(map[bool]string{false:"same-entry",true:"another-entry"}[crossEntry],func(t *testing.T){
	owner,err:=newUnslopRetainedOwner(2);if err!=nil {t.Fatal(err)}
	live,lease,done:=retainedTestInputSession(t)
	f,other:=retainedTestOpenFile(t),retainedTestOpenFile(t)
	live.evidence=&nativeUpdateObject{f:f}
	var firstTicket *unslopRetainedTicket
	var firstDone chan askProducerFlightResult
	if crossEntry {
		firstDone=done
		firstTicket,err=owner.Retain(&live,&lease,nil,unslopToolchainCaptureResult{});if err!=nil {t.Fatal(err)}
		live,lease,_=retainedTestInputSession(t)
	}
	alias:=&nativeUpdateObject{f:other,parents:[]syscall.Handle{syscall.Handle(f.Fd())}}
	oldLive,oldLease:=live,lease
	if ticket,err:=owner.Retain(&live,&lease,[]io.Closer{alias},unslopToolchainCaptureResult{});err==nil||ticket!=nil||live!=oldLive||lease!=oldLease {t.Fatal("extra parent handle aliased an owned file")}
	for _,file:=range []*os.File{f,other} {if _,err:=file.Stat();err!=nil {t.Fatal("rejected alias closed a file")}}
	if crossEntry {firstDone<-retainedTestCompletion();if r:=owner.Drain(context.Background(),firstTicket);r.State!="released" {t.Fatal("first owner changed after handle rejection",r)}}
	})
	}
}
