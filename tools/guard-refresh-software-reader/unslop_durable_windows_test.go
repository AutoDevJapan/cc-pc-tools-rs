//go:build windows

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func newDurableTestStore(t *testing.T) *unslopDurableStore {
	t.Helper()
	root:=t.TempDir()
	h,ok:=openPlain(root,true);if !ok {t.Fatal("cannot hold test directory")}
	f:=os.NewFile(uintptr(h),root)
	t.Cleanup(func(){_ = f.Close()})
	store,err:=newUnslopDurableStore(f);if err!=nil {t.Fatal(err)}
	t.Cleanup(func(){_ = store.Close()})
	return store
}

func newDurableTestCapture(t *testing.T,store *unslopDurableStore,mode string) (*unslopDurableController,*unslopSyntheticCapture,*syntheticUnslopPending,*softwareRustSyntheticAccess) {
	t.Helper()
	provider,access:=syntheticSoftwareRustProvider()
	if mode=="late-provider-close"||mode=="cancel-close" {access.session.closeError=true}
	session:=syntheticUnslopCapture()
	pending:=&syntheticUnslopPending{}
	if mode=="capture-close" {pending.fault="export_close_failed"}
	controller,err:=newUnslopDurableController(store,provider,func(context.Context,rustToolchainHeld,unslopRustToolchainBinding)(unslopCaptureAccess,error){
		return unslopCaptureAccess{begin:func(context.Context)(unslopProducerSession,error){return session,nil},stage:func(context.Context,unslopCaptureReceipt,[]byte)(unslopPendingCapture,error){return pending,nil}},nil
	})
	if err!=nil {t.Fatal(err)}
	return controller,session,pending,access
}

func durableTestFailPhase(store *unslopDurableStore,phase string) {
	write:=store.write
	store.write=func(f *os.File,raw []byte)(int,error){if bytes.Contains(raw,[]byte(`"kind":"`+phase+`"`)){return 0,errors.New("private storage failure path")};return write(f,raw)}
}

func TestUnslopDurableCapturePersistsLateProviderAndPublishedReceipt(t *testing.T) {
	for _,mode:=range []string{"normal","late-provider-close","capture-close"} {
		t.Run(mode,func(t *testing.T){
			store:=newDurableTestStore(t)
			c,session,pending,access:=newDurableTestCapture(t,store,mode)
			r:=c.Capture(context.Background())
			if !r.ResultSaved||r.Qualified!=(mode=="normal")||!r.ReceiptPublished||session.builds!=1||pending.commits!=1||access.session.closed!=1 {t.Fatal("final result did not preserve actual capture",mode,r)}
			reopened,err:=newUnslopDurableStore(store.root);if err!=nil {t.Fatal(err)}
			t.Cleanup(func(){_ = reopened.Close()})
			recovered,err:=reopened.Recover(context.Background(),r.Generation)
			if err!=nil||recovered.State!="durable_capture_result_saved"||!recovered.ReceiptPublished||recovered.ReceiptSHA==""||recovered.Capture==nil||recovered.Capture.CleanupFailed!=(mode=="late-provider-close")||recovered.Qualified||recovered.DataAccessGranted {t.Fatal("recovery lost final provider result",mode,recovered,err)}
			if mode=="capture-close"&&recovered.Capture.CaptureReason!="capture_export_close_failed" {t.Fatal("late export failure lost")}
			encoded,err:=json.Marshal(recovered)
			if err!=nil||bytes.Contains(encoded,[]byte(store.path))||bytes.Contains(encoded,[]byte("private"))||bytes.Contains(encoded,[]byte("InstallPath"))||bytes.Contains(encoded,[]byte("owned-cargo")) {t.Fatal("recovery exposed raw path or provider bytes")}
			if again:=c.Capture(context.Background());again.Qualified||again.DurableReason!="durable_generation_already_used"||session.builds!=1||pending.commits!=1 {t.Fatal("same generation captured or published twice",again)}
		})
	}
}

func TestUnslopDurableFinalSaveRetryDoesNotRecapture(t *testing.T) {
	for _,mode:=range []string{"write","partial","after-sync","after-publish","cancel-after-publication"} {
		t.Run(mode,func(t *testing.T){
			store:=newDurableTestStore(t)
			c,session,pending,_:=newDurableTestCapture(t,store,"normal")
			ctx,cancel:=context.WithCancel(context.Background());defer cancel()
			var failOnce bool
			write:=store.write
			store.write=func(f *os.File,raw []byte)(int,error){
				if !failOnce&&bytes.Contains(raw,[]byte(`"kind":"final"`)) {
					if mode=="write" {failOnce=true;return 0,errors.New("private disk error")}
					if mode=="partial" {failOnce=true;n,e:=f.Write(raw[:len(raw)/2]);if e!=nil{return n,e};return n,io.ErrShortWrite}
				}
				return write(f,raw)
			}
			store.hook=func(phase string)error {
				if failOnce{return nil}
				if c.result!=nil&&(mode=="after-sync"&&phase=="after-sync"||mode=="after-publish"&&phase=="after-publish") {failOnce=true;return errors.New("private sync error")}
				return nil
			}
			if mode=="cancel-after-publication" {c.provider=&unslopCleanupFactProvider{base:c.provider,after:func(error){cancel()}}}
			r:=c.Capture(ctx)
			if r.ResultSaved||r.Qualified||r.Receipt!=nil||!r.ReceiptPublished||session.builds!=1||pending.commits!=1 {t.Fatal("save failure returned final success or repeated capture",mode,r)}
			store.write=write;store.hook=nil
			r=c.RetryFinal(context.Background())
			if !r.ResultSaved||!r.ReceiptPublished||session.builds!=1||pending.commits!=1 {t.Fatal("same-result retry did not complete",mode,r)}
			if mode=="cancel-after-publication"&&r.ToolchainReason!="toolchain_cancelled" {t.Fatal("retry erased cancellation",r)}
		})
	}
}

func TestUnslopDurableStartAndPublicationSaveFailuresKeepBoundary(t *testing.T) {
	for _,phase:=range []string{"started","binding","staged","sealed","publish-intent","publication"} {
		t.Run(phase,func(t *testing.T){
			store:=newDurableTestStore(t)
			c,session,pending,access:=newDurableTestCapture(t,store,"normal")
			durableTestFailPhase(store,phase)
			r:=c.Capture(context.Background())
			if r.Qualified||r.Receipt!=nil {t.Fatal("save failure became success",phase,r)}
			if phase=="started"&&(session.builds!=0||access.opens!=0) {t.Fatal("capture started before generation save")}
			if phase=="publish-intent"&&pending.commits!=0 {t.Fatal("published before durable intent")}
			if phase=="publication"&&(!r.ReceiptPublished||pending.commits!=1||r.ResultSaved) {t.Fatal("post-publication failure hid publication",r)}
			if phase=="publication" {
				store.write=func(f *os.File,b []byte)(int,error){return f.Write(b)}
				if retry:=c.RetryFinal(context.Background());!retry.ResultSaved||!retry.ReceiptPublished||pending.commits!=1 {t.Fatal("publication record retry republished",retry)}
			}
		})
	}
}

func TestUnslopDurableJournalRejectsGenerationAndDuplicateMutation(t *testing.T) {
	store:=newDurableTestStore(t)
	c,_,_,_:=newDurableTestCapture(t,store,"normal")
	if r:=c.Capture(context.Background());!r.ResultSaved {t.Fatal(r)}
	for _,fault:=range []string{"generation","sequence","unknown-key","different-final","gap","same-sequence-different-bytes","previous-sha","noncanonical-valid-json"} {
		t.Run(fault,func(t *testing.T){
			copyStore:=newDurableTestStore(t)
			if err:=copyStore.reserve(c.journal.generation);err!=nil {t.Fatal(err)}
			for n:=1;n<=len(c.journal.events);n++ {raw,err:=store.read(filepath.Join(store.path,unslopDurableName(c.journal.generation,n)));if err!=nil {t.Fatal(err)};if err=os.WriteFile(filepath.Join(copyStore.path,unslopDurableName(c.journal.generation,n)),raw,0600);err!=nil {t.Fatal(err)}}
			path:=filepath.Join(copyStore.path,unslopDurableName(c.journal.generation,1))
			raw,_:=os.ReadFile(path)
			switch fault {
			case "generation":raw=bytes.Replace(raw,[]byte(c.journal.generation),[]byte(strings.Repeat("a",32)),1)
			case "sequence":raw=bytes.Replace(raw,[]byte(`"sequence":1`),[]byte(`"sequence":2`),1)
			case "unknown-key":raw=append(raw[:len(raw)-1],[]byte(`,"secret":"private"}`)...)
			case "gap":_ = os.Remove(filepath.Join(copyStore.path,unslopDurableName(c.journal.generation,2)))
			case "previous-sha":
				path=filepath.Join(copyStore.path,unslopDurableName(c.journal.generation,len(c.journal.events)))
				raw,_=os.ReadFile(path)
				var event unslopDurableEvent
				if json.Unmarshal(raw,&event)!=nil {t.Fatal("invalid source fixture")}
				event.PreviousSHA=strings.Repeat("a",64)
				raw,_=json.Marshal(event)
			case "noncanonical-valid-json":
				path=filepath.Join(copyStore.path,unslopDurableName(c.journal.generation,len(c.journal.events)))
				raw,_=os.ReadFile(path)
				raw=append([]byte(" \n"),raw...)
			case "different-final":
				j:=&unslopDurableJournal{store:copyStore,generation:c.journal.generation}
				changed:=*c.journal.last("final").Capture;changed.Reason="toolchain_cancelled"
				if _,err:=j.append(context.Background(),unslopDurableEvent{Kind:"final",Capture:&changed});err==nil {t.Fatal("different generation result overwrote final")};return
			case "same-sequence-different-bytes":
				copyStore.mu.Lock();unlock,err:=copyStore.lock(c.journal.generation);if err!=nil {t.Fatal(err)}
				err=copyStore.put(context.Background(),c.journal.generation,1,append(raw,' '));unlock();copyStore.mu.Unlock()
				if err==nil {t.Fatal("different bytes replaced saved generation")};return
			}
			if fault!="gap" {if err:=os.WriteFile(path,raw,0600);err!=nil {t.Fatal(err)}}
			if _,err:=copyStore.Recover(context.Background(),c.journal.generation);err==nil {t.Fatal("corrupt generation recovered",fault)}
		})
	}
}

func TestUnslopDurableRestartKeepsPublicationUncertainAfterIntent(t *testing.T) {
	store:=newDurableTestStore(t)
	c,_,pending,_:=newDurableTestCapture(t,store,"normal")
	durableTestFailPhase(store,"publication")
	r:=c.Capture(context.Background())
	if r.ResultSaved||!r.ReceiptPublished||pending.commits!=1 {t.Fatal("fixture did not stop after real publication",r)}
	recovery,err:=store.Recover(context.Background(),r.Generation)
	if err!=nil||recovery.Publication!="unknown"||recovery.State!="durable_recovery_incomplete"||recovery.Capture!=nil||recovery.Qualified {t.Fatal("restart guessed whether receipt was published",recovery,err)}
	store.write=func(f *os.File,b []byte)(int,error){return f.Write(b)}
	if retry:=c.RetryFinal(context.Background());!retry.ResultSaved||!retry.ReceiptPublished||pending.commits!=1 {t.Fatal("same-process retry republished receipt",retry)}
}

func TestUnslopDurableRecoveryDoesNotInventFinalOrNativeOwner(t *testing.T) {
	store:=newDurableTestStore(t)
	c,_,_,_:=newDurableTestCapture(t,store,"normal")
	if err:=c.begin(context.Background());err!=nil {t.Fatal(err)}
	r,err:=store.Recover(context.Background(),c.journal.generation)
	if err!=nil||r.State!="durable_recovery_incomplete"||r.Capture!=nil||r.Owner!=nil||r.Qualified||r.DataAccessGranted {t.Fatal("restart invented outcome",r,err)}
	other:=strings.Repeat("b",32)
	if _,err:=store.Recover(context.Background(),other);err==nil {t.Fatal("different generation adopted")}
	f:=newRetainedTestFixture(t)
	f.owner.mu.Lock();f.owner.entries[f.ticket].report.Capture=unslopToolchainCaptureResult{};f.owner.mu.Unlock()
	if err:=c.SaveRetained(context.Background(),f.owner,f.ticket);err!=nil {t.Fatal(err)}
	r,err=store.Recover(context.Background(),c.journal.generation)
	if err!=nil||r.State!="durable_owner_reconciliation_required"||r.Owner==nil||r.Owner.OwnerID!=f.owner.Snapshot(f.ticket).OwnerID||r.Qualified {t.Fatal("unreaped restart became a released owner",r,err)}
	restored,recovery,err:=restoreUnslopDurableController(store,c.journal.generation)
	if err!=nil||recovery.State!="durable_owner_reconciliation_required"||restored.owner!=nil||restored.ticket!=nil||restored.Capture(context.Background()).Qualified||restored.RetryFinal(context.Background()).ResultSaved {t.Fatal("restart reconstructed a live OS owner",recovery,err)}
	all,err:=store.RecoverAll(context.Background())
	if err!=nil||len(all)!=1||all[0].Generation!=c.journal.generation {t.Fatal("bounded normal recovery lost saved generation",all,err)}
}

func TestUnslopDurableDrainRequiresSameReportSaveBeforeForget(t *testing.T) {
	for _,mode:=range []string{"normal","save-failure","unverified","wrong-ticket","tampered-proof"} {
		t.Run(mode,func(t *testing.T){
			store:=newDurableTestStore(t)
			c,err:=newUnslopDurableController(store,nil,nil);if err!=nil {t.Fatal(err)}
			f:=newRetainedTestFixture(t)
			f.owner.mu.Lock();f.owner.entries[f.ticket].report.Capture=unslopToolchainCaptureResult{};f.owner.mu.Unlock()
			if mode=="unverified" {r:=retainedTestCompletion();r.Child.Reaped=false;f.done<-r}else{f.done<-retainedTestCompletion()}
			if mode=="save-failure" {durableTestFailPhase(store,"owner")}
			if mode=="wrong-ticket" {
				forged:=&unslopRetainedTicket{sequence:f.ticket.sequence}
				if err:=c.SaveRetained(context.Background(),f.owner,forged);err==nil {t.Fatal("forged ticket saved")};return
			}
			if mode=="tampered-proof" {
				f.owner.Drain(context.Background(),f.ticket)
				if err:=c.SaveRetained(context.Background(),f.owner,f.ticket);err!=nil {t.Fatal(err)}
				f.owner.mu.Lock();f.owner.entries[f.ticket].report.Capture.Reason="toolchain_origin_unavailable";f.owner.mu.Unlock()
				if f.owner.Forget(f.ticket) {t.Fatal("proof for different report erased owner")};return
			}
			r:=c.DrainAndSave(context.Background(),f.owner,f.ticket)
			if mode=="normal"&&(!r.Saved||!r.Forgotten||len(f.owner.Tickets())!=0) {t.Fatal("saved release was not forgotten",r)}
			if mode=="save-failure" {
				if r.Saved||r.Forgotten||f.owner.Forget(f.ticket)||len(f.owner.Tickets())!=1 {t.Fatal("save failure lost retained report",r)}
				if f.resource.calls!=0||f.oldLease.calls!=0 {t.Fatal("drain began before initial owner state was saved")}
				store.write=func(f *os.File,b []byte)(int,error){return f.Write(b)}
				r=c.DrainAndSave(context.Background(),f.owner,f.ticket)
				if !r.Saved||!r.Forgotten||f.resource.calls!=1||f.oldLease.calls!=1 {t.Fatal("owner save retry repeated Close or failed",r)}
			}
			if mode=="unverified"&&(!r.Saved||r.Forgotten||f.resource.calls!=0||f.oldLease.calls!=0) {t.Fatal("saved unknown result released ownership",r)}
		})
	}
}

func TestUnslopDurableFinalOwnerSaveFailureRetainsReleasedReport(t *testing.T) {
	store:=newDurableTestStore(t)
	c,err:=newUnslopDurableController(store,nil,nil);if err!=nil {t.Fatal(err)}
	f:=newRetainedTestFixture(t)
	f.done<-retainedTestCompletion()
	write:=store.write
	store.write=func(file *os.File,b []byte)(int,error){if bytes.Contains(b,[]byte(`"state":"released"`)){return 0,io.ErrShortWrite};return write(file,b)}
	r:=c.DrainAndSave(context.Background(),f.owner,f.ticket)
	if r.Saved||r.Forgotten||r.Report.State!="released"||f.owner.Forget(f.ticket)||f.resource.calls!=1||f.oldLease.calls!=1 {t.Fatal("unsaved final release lost report or repeated Close",r)}
	recovery,err:=store.Recover(context.Background(),c.journal.generation)
	if err!=nil||recovery.State!="durable_owner_reconciliation_required" {t.Fatal("restart invented the missing release result",recovery,err)}
	store.write=write
	r=c.DrainAndSave(context.Background(),f.owner,f.ticket)
	if !r.Saved||!r.Forgotten||f.resource.calls!=1||f.oldLease.calls!=1 {t.Fatal("final owner save retry repeated release",r)}
}

func durableTestSaveBeforeForget(t *testing.T,owner *unslopRetainedOwner,ticket *unslopRetainedTicket) {
	t.Helper()
	store:=newDurableTestStore(t)
	c,err:=newUnslopDurableController(store,nil,nil);if err!=nil {t.Fatal(err)}
	if err=c.SaveRetained(context.Background(),owner,ticket);err!=nil {t.Fatal(err)}
}

func TestUnslopDurableStoreRejectsInvalidRootAndConcurrentGenerationWriter(t *testing.T) {
	store:=newDurableTestStore(t)
	c,_,_,_:=newDurableTestCapture(t,store,"normal")
	unlock,err:=store.lock(c.journal.generation);if err!=nil {t.Fatal(err)}
	if release,err:=store.lock(c.journal.generation);err==nil {release();unlock();t.Fatal("same generation obtained two writer locks")}
	unlock()
	c.reserved=true // the lock fixture reserved this exact owned generation
	if c.begin(context.Background())!=nil {t.Fatal("released writer lock did not recover")}
	if _,err:=newUnslopDurableStore(nil);err==nil {t.Fatal("caller supplied no root capability")}
	file:=retainedTestOpenFile(t)
	if _,err:=newUnslopDurableStore(file);err==nil {t.Fatal("file became directory authority")}
	var unavailable *unslopDurableController
	if r:=unavailable.Capture(context.Background());r.Qualified||r.ResultSaved {t.Fatal("nil controller became available")}
	if r:=(&unslopDurableController{}).Capture(context.Background());r.Qualified||r.ResultSaved {t.Fatal("zero controller became available")}
}

func TestUnslopDurableReceiptAndGenerationNeverAcceptAnotherCapture(t *testing.T) {
	store:=newDurableTestStore(t)
	c,_,_,_:=newDurableTestCapture(t,store,"normal")
	first:=c.Capture(context.Background())
	if !first.ResultSaved {t.Fatal(first)}
	old:=c.journal.last("sealed")
	if old==nil {t.Fatal("receipt was not bound before publication")}
	changed:=*old.Receipt;changed.CandidateSHA=digest([]byte("different capture"))
	if _,err:=c.journal.append(context.Background(),unslopDurableEvent{Kind:"sealed",Receipt:&changed});err==nil {t.Fatal("same generation accepted another receipt")}
	copyOfResult:=c.currentResult(true,"durable_final_saved")
	copyOfResult.Receipt.CandidateSHA="changed caller copy"
	if c.result.Receipt.CandidateSHA=="changed caller copy" {t.Fatal("caller mutated pending final result")}
	other,_,_,_:=newDurableTestCapture(t,store,"normal")
	if other.journal.generation==c.journal.generation {t.Fatal("two generations reused one identity")}
	if second:=other.Capture(context.Background());!second.ResultSaved {t.Fatal(second)}
	all,err:=store.RecoverAll(context.Background())
	if err!=nil||len(all)!=2 {t.Fatal("different generations overwrote each other",all,err)}
	collision,_,_,access:=newDurableTestCapture(t,store,"normal")
	collision.journal.generation=c.journal.generation
	if result:=collision.Capture(context.Background());result.ResultSaved||result.Qualified||access.opens!=0 {t.Fatal("duplicate generation replayed another capture",result)}
}

func TestUnslopDurableRetainRejectsUnrelatedLiveSessionAndLease(t *testing.T) {
	store:=newDurableTestStore(t)
	c,_,_,_:=newDurableTestCapture(t,store,"normal")
	if r:=c.Capture(context.Background());!r.ResultSaved {t.Fatal(r)}
	owner,err:=newUnslopRetainedOwner(1);if err!=nil {t.Fatal(err)}
	live,lease,_:=retainedTestInputSession(t)
	beforeLive,beforeLease:=live,lease
	if ticket,err:=c.Retain(context.Background(),owner,&live,&lease,nil);err==nil||ticket!=nil||live!=beforeLive||lease!=beforeLease {t.Fatal("another live session or lease adopted the generation")}
}

func TestUnslopDurableRetainMovesOnlyObservedSessionAndHeldBuffers(t *testing.T) {
	for _,fault:=range []string{"none","session","buffer"} {
		t.Run(fault,func(t *testing.T){
			store:=newDurableTestStore(t)
			controller,err:=newUnslopDurableController(store,nil,nil);if err!=nil {t.Fatal(err)}
			if controller.begin(context.Background())!=nil {t.Fatal("generation start")}
			live,lease,done:=retainedTestInputSession(t)
			held:=lease.Held()
			binding,valid:=bindUnslopRustToolchain(fixedUnslopToolchainPurpose(),held);if !valid {t.Fatal("invalid original held tuple")}
			if _,err:=controller.journal.append(context.Background(),unslopDurableEvent{Kind:"binding",BindingSHA:binding.SHA});err!=nil {t.Fatal(err)}
			// This is the exact state Capture records from its concrete live
			// session. It is an internal test seam, not external authority.
			controller.observedLive=live;controller.observedHeld=held;controller.observedBinding=binding;controller.attempted=true
			controller.result=&unslopToolchainCaptureResult{unslopCaptureResult:unslopCaptureResult{Reason:"capture_cleanup_failed_preserved"},CaptureAttempted:true,CaptureReason:"capture_cleanup_failed_preserved",ToolchainReason:"toolchain_inputs_consumed"}
			if fault=="session" {live,_,_=retainedTestInputSession(t)}
			if fault=="buffer" {_,lease,_=retainedTestInputSession(t)}
			beforeLive,beforeLease:=live,lease
			owner,err:=newUnslopRetainedOwner(1);if err!=nil {t.Fatal(err)}
			ticket,err:=controller.Retain(context.Background(),owner,&live,&lease,nil)
			if fault!="none" {if err==nil||ticket!=nil||live!=beforeLive||lease!=beforeLease {t.Fatal("unobserved live input adopted durable generation")};return}
			if err!=nil||ticket==nil||live!=nil||lease!=nil {t.Fatal("observed live input not moved",err)}
			done<-retainedTestCompletion()
			if result:=controller.RetryRetained(context.Background());!result.Saved||!result.Forgotten {t.Fatal("observed retained result not saved",result)}
			recovery,err:=store.Recover(context.Background(),controller.journal.generation)
			if err!=nil||recovery.Capture==nil||recovery.Owner==nil||recovery.Owner.State!="released"||recovery.Capture.CaptureReason!=controller.result.CaptureReason {t.Fatal("capture and lease result not bound to one generation",recovery,err)}
		})
	}
}

func TestUnslopDurableMoveIntentIsSavedBeforeSlotsChange(t *testing.T) {
	for _,fail:=range []bool{false,true} {
		t.Run(map[bool]string{false:"saved",true:"save-failure"}[fail],func(t *testing.T){
			store:=newDurableTestStore(t)
			controller,err:=newUnslopDurableController(store,nil,nil);if err!=nil {t.Fatal(err)}
			if err=controller.begin(context.Background());err!=nil {t.Fatal(err)}
			if fail {durableTestFailPhase(store,"owner-intent")}
			owner,err:=newUnslopRetainedOwner(1);if err!=nil {t.Fatal(err)}
			live,lease,done:=retainedTestInputSession(t)
			beforeLive,beforeLease:=live,lease
			ticket,err:=owner.retainBeforeMove(&live,&lease,nil,unslopToolchainCaptureResult{},func(report unslopRetainedReport)error{
				if live!=beforeLive||lease!=beforeLease {t.Fatal("slots moved before durable intent")}
				_,err:=controller.journal.append(context.Background(),unslopDurableEvent{Kind:"owner-intent",Owner:&report});return err
			})
			if fail {if err==nil||ticket!=nil||live!=beforeLive||lease!=beforeLease {t.Fatal("failed intent lost original ownership")};return}
			if err!=nil||ticket==nil||live!=nil||lease!=nil {t.Fatal("saved intent did not move slots",err)}
			if recovery,err:=store.Recover(context.Background(),controller.journal.generation);err!=nil||recovery.State!="durable_owner_transfer_unconfirmed" {t.Fatal("restart invented transfer success",recovery,err)}
			done<-retainedTestCompletion()
			if result:=controller.DrainAndSave(context.Background(),owner,ticket);!result.Saved||!result.Forgotten {t.Fatal("saved release not consumed",result)}
		})
	}
}

func TestUnslopDurablePublicationPendingSurvivesRetryEarlyFailure(t *testing.T) {
	for _,first:=range []string{"partial","after-sync","after-publish"} {
		for _,retryFault:=range []string{"lock","read"} {
			t.Run(first+"/"+retryFault,func(t *testing.T){
				store:=newDurableTestStore(t)
				c,session,published,_:=newDurableTestCapture(t,store,"normal")
				write:=store.write
				publicationPath:=""
				store.write=func(f *os.File,raw []byte)(int,error){
					if bytes.Contains(raw,[]byte(`"kind":"publication"`)){publicationPath=f.Name()}
					if f.Name()==publicationPath&&first=="partial" {
						n,err:=f.Write(raw[:len(raw)/2]);if err!=nil{return n,err}
						store.readHook=func()error{return errUnslopDurable}
						return n,io.ErrShortWrite
					}
					return write(f,raw)
				}
				store.hook=func(step string)error {
					if publicationPath!=""&&(first=="after-sync"&&step=="after-sync"||first=="after-publish"&&step=="after-publish") {
						store.readHook=func()error{return errUnslopDurable}
						return errUnslopDurable
					}
					return nil
				}
				initial:=c.Capture(context.Background())
				if initial.ResultSaved||!initial.ReceiptPublished||published.commits!=1||c.journal.pending==nil||c.journal.pending.Kind!="publication" {t.Fatal("publication event lost before next retry",initial)}
				before,_:=json.Marshal(c.journal.pending)
				store.write=write;store.hook=nil;store.readHook=nil
				var unlock func()
				if retryFault=="lock" {var err error;unlock,err=store.lock(c.journal.generation);if err!=nil{t.Fatal(err)}}else{store.readHook=func()error{return errUnslopDurable}}
				retry:=c.RetryFinal(context.Background())
				if unlock!=nil{unlock()};store.readHook=nil
				after,_:=json.Marshal(c.journal.pending)
				if retry.ResultSaved||c.journal.pending==nil||!bytes.Equal(before,after) {t.Fatal("early retry failure discarded or changed pending publication",retry)}
				final:=c.RetryFinal(context.Background())
				if !final.ResultSaved||!final.ReceiptPublished||published.commits!=1||session.builds!=1||c.journal.pending!=nil {t.Fatal("recovered pending event did not save exact result",final)}
				changed:=false
				if _,err:=c.journal.append(context.Background(),unslopDurableEvent{Kind:"publication",Published:&changed});err==nil {t.Fatal("different publication bytes replaced recovered event")}
			})
		}
	}
}

func durableTestObservedRetain(t *testing.T,store *unslopDurableStore) (*unslopDurableController,*unslopLiveSession,softwareRustHeldSession,chan askProducerFlightResult) {
	t.Helper()
	c,err:=newUnslopDurableController(store,nil,nil);if err!=nil {t.Fatal(err)}
	if c.begin(context.Background())!=nil {t.Fatal("generation start")}
	live,lease,done:=retainedTestInputSession(t)
	held:=lease.Held()
	binding,valid:=bindUnslopRustToolchain(fixedUnslopToolchainPurpose(),held);if !valid {t.Fatal("held fixture invalid")}
	if _,err:=c.journal.append(context.Background(),unslopDurableEvent{Kind:"binding",BindingSHA:binding.SHA});err!=nil {t.Fatal(err)}
	c.observedLive=live;c.observedHeld=held;c.observedBinding=binding;c.attempted=true
	c.result=&unslopToolchainCaptureResult{unslopCaptureResult:unslopCaptureResult{Reason:"capture_cleanup_failed_preserved"},CaptureAttempted:true,CaptureReason:"capture_cleanup_failed_preserved",ToolchainReason:"toolchain_inputs_consumed"}
	return c,live,lease,done
}

func TestUnslopDurableIntentRetryKeepsOriginalOwnerAndTicket(t *testing.T) {
	for _,fault:=range []string{"partial","after-sync","after-publish"} {
		t.Run(fault,func(t *testing.T){
			store:=newDurableTestStore(t)
			c,live,lease,done:=durableTestObservedRetain(t,store)
			ownerA,err:=newUnslopRetainedOwner(3);if err!=nil {t.Fatal(err)}
			ownerB,err:=newUnslopRetainedOwner(3);if err!=nil {t.Fatal(err)}
			write:=store.write;intentPath:=""
			store.write=func(f *os.File,raw []byte)(int,error){
				if bytes.Contains(raw,[]byte(`"kind":"owner-intent"`)){intentPath=f.Name()}
				if f.Name()==intentPath&&fault=="partial" {n,err:=f.Write(raw[:len(raw)/2]);if err!=nil{return n,err};return n,io.ErrShortWrite}
				return write(f,raw)
			}
			store.hook=func(step string)error {if intentPath!=""&&(fault=="after-sync"&&step=="after-sync"||fault=="after-publish"&&step=="after-publish"){return errUnslopDurable};return nil}
			beforeLive,beforeLease:=live,lease
			if ticket,err:=c.Retain(context.Background(),ownerA,&live,&lease,nil);err==nil||ticket!=nil||live!=beforeLive||lease!=beforeLease||c.pendingReservation==nil {t.Fatal("failed intent did not keep original owner reservation")}
			reservation:=c.pendingReservation
			pendingStatus:=c.RetryRetained(context.Background())
			if pendingStatus.Saved||pendingStatus.Forgotten||pendingStatus.Reason!="durable_owner_transfer_pending"||pendingStatus.Report.OwnerID!=reservation.report.OwnerID {t.Fatal("pending owner reservation was hidden or activated",pendingStatus)}
			pendingBytes,_:=json.Marshal(c.journal.pending)
			if ticket,err:=c.Retain(context.Background(),ownerB,&live,&lease,nil);err==nil||ticket!=nil||live!=beforeLive||lease!=beforeLease||c.pendingReservation!=reservation||len(ownerB.reservations)!=0 {t.Fatal("different owner adopted a pending generation")}
			stillPending,_:=json.Marshal(c.journal.pending)
			if !bytes.Equal(pendingBytes,stillPending) {t.Fatal("different owner overwrote pending intent")}
			otherLive,otherLease,otherDone:=retainedTestInputSession(t)
			other,err:=ownerA.Retain(&otherLive,&otherLease,nil,unslopToolchainCaptureResult{});if err!=nil {t.Fatal("independent owner registration failed",err)}
			if other.sequence==reservation.ticket.sequence {t.Fatal("reservation ticket was reused by another registration")}
			store.write=write;store.hook=nil
			ticket,err:=c.Retain(context.Background(),ownerA,&live,&lease,nil)
			if err!=nil||ticket!=reservation.ticket||live!=nil||lease!=nil||c.pendingReservation!=nil {t.Fatal("original intent could not resume after another registration",err)}
			intent:=c.journal.last("owner-intent")
			if intent==nil||intent.Owner.OwnerID!=ownerA.Snapshot(ticket).OwnerID {t.Fatal("handoff changed its original identity")}
			done<-retainedTestCompletion()
			if result:=c.RetryRetained(context.Background());!result.Saved||!result.Forgotten {t.Fatal("resumed original handoff failed",result)}
			otherDone<-retainedTestCompletion()
			ownerA.Drain(context.Background(),other);durableTestSaveBeforeForget(t,ownerA,other)
			if !ownerA.Forget(other) {t.Fatal("independent registration lost ownership")}
		})
	}
}

func durableTestAssertSavedReport(t *testing.T,c *unslopDurableController,result unslopDurableDrainResult) {
	t.Helper()
	if !result.Saved||!unslopHex(result.RecordSHA,64) {t.Fatal("saved report has no record binding",result)}
	want,_:=json.Marshal(result.Report)
	found:=false
	for n,e:=range c.journal.events {
		if c.journal.hashes[n]!=result.RecordSHA {continue}
		if e.Kind!="owner"||e.Owner==nil {t.Fatal("saved report points to a non-owner record")}
		actual,_:=json.Marshal(e.Owner)
		if !bytes.Equal(want,actual) {t.Fatal("Saved and returned Report refer to different snapshots")}
		found=true
	}
	if !found {t.Fatal("saved report record was not read back")}
}

func TestUnslopDurableConcurrentDrainReturnsActuallySavedReport(t *testing.T) {
	for _,mode:=range []string{"progress","released","save-failure"} {
		t.Run(mode,func(t *testing.T){
			store:=newDurableTestStore(t)
			c,err:=newUnslopDurableController(store,nil,nil);if err!=nil {t.Fatal(err)}
			f:=newRetainedTestFixture(t)
			if err:=c.SaveRetained(context.Background(),f.owner,f.ticket);err!=nil {t.Fatal(err)}
			ctx,cancel:=context.WithTimeout(context.Background(),10*time.Second);defer cancel()
			entered,release:=make(chan struct{}),make(chan struct{})
			var once sync.Once
			unblock:=func(){once.Do(func(){close(release)})};defer unblock()
			f.oldLease.held.Recheck=func(ctx context.Context)error {close(entered);select{case <-release:return nil;case <-ctx.Done():return ctx.Err()}}
			finished:=make(chan unslopRetainedReport,1)
			f.done<-retainedTestCompletion()
			go func(){finished<-f.owner.Drain(ctx,f.ticket)}()
			select{case <-entered:case <-ctx.Done():t.Fatal("owner did not reach release barrier")}
			stale:=f.owner.Drain(ctx,f.ticket)
			if stale.State!="release_in_progress" {t.Fatal("second drain did not observe a progressing owner",stale)}
			if mode!="progress" {unblock();select{case result:=<-finished:if result.State!="released"{t.Fatal(result)};case <-ctx.Done():t.Fatal("owner did not complete release")}}
			write:=store.write
			if mode=="save-failure" {store.write=func(file *os.File,raw []byte)(int,error){if bytes.Contains(raw,[]byte(`"state":"released"`)){return 0,io.ErrShortWrite};return write(file,raw)}}
			result:=c.saveDrainedReport(ctx,f.owner,f.ticket,stale)
			if mode=="save-failure" {
				if result.Saved||result.Forgotten||result.RecordSHA!=""||f.owner.Forget(f.ticket) {t.Fatal("failed save claimed the newer report was durable",result)}
				store.write=write
				result=c.saveDrainedReport(ctx,f.owner,f.ticket,stale)
			}
			durableTestAssertSavedReport(t,c,result)
			if mode=="progress" {
				if result.Report.State!="release_in_progress"||result.Forgotten {t.Fatal("progress report became a completed release",result)}
				unblock();select{case <-finished:case <-ctx.Done():t.Fatal("release did not complete")}
				result=c.DrainAndSave(ctx,f.owner,f.ticket);durableTestAssertSavedReport(t,c,result)
			}
			if result.Report.State!="released"||!result.Forgotten||f.resource.calls!=1||f.oldLease.calls!=1 {t.Fatal("returned report did not match durable release",result)}
		})
	}
}

func TestUnslopDurableCancelledSaveStillAbortsExactRetainedFlight(t *testing.T) {
	for _,mode:=range []string{"cancel-before","cancel-during-save","deadline-before","write-failure-and-cancel"} {
		t.Run(mode,func(t *testing.T){
			store:=newDurableTestStore(t)
			c,err:=newUnslopDurableController(store,nil,nil);if err!=nil{t.Fatal(err)}
			f:=newRetainedTestFixture(t)
			var ctx context.Context
			var cancel context.CancelFunc
			if mode=="deadline-before" {ctx,cancel=context.WithDeadline(context.Background(),time.Unix(0,0))}else{ctx,cancel=context.WithCancel(context.Background())}
			defer cancel()
			if mode=="cancel-before" {cancel()}
			write:=store.write
			if mode=="cancel-during-save" {store.hook=func(step string)error{if step=="after-sync"{cancel()};return nil}}
			if mode=="write-failure-and-cancel" {store.write=func(*os.File,[]byte)(int,error){cancel();return 0,io.ErrShortWrite}}
			for n:=0;n<2;n++ {
				result:=c.DrainAndSave(ctx,f.owner,f.ticket)
				if result.Saved||result.Forgotten||result.Report.State!="retained"||!result.Report.AbortRequested||f.aborts!=1||f.resource.calls!=0||f.oldLease.calls!=0||f.owner.Forget(f.ticket) {t.Fatal("unsaved cancellation lost Abort or released input",mode,result)}
				want:="retained_owner_wait_cancelled";if mode=="deadline-before" {want="retained_owner_wait_deadline"}
				if result.Report.WaitReason!=want {t.Fatal("save failure lost caller cancellation kind",result)}
			}
			store.write=write;store.hook=nil
			f.done<-retainedTestCompletion()
			result:=c.DrainAndSave(context.Background(),f.owner,f.ticket)
			durableTestAssertSavedReport(t,c,result)
			if !result.Forgotten||f.aborts!=1||f.resource.calls!=1||f.oldLease.calls!=1 {t.Fatal("fresh cleanup repeated Abort or Close",result)}
		})
	}
}

func TestUnslopDurableFinalReadFailureKeepsReceiptForRetry(t *testing.T) {
	for _,mode:=range []string{"first-record","before-sealed","after-final-readback","different-stored-bytes"} {
		t.Run(mode,func(t *testing.T){
			store:=newDurableTestStore(t)
			c,session,published,_:=newDurableTestCapture(t,store,"normal")
			var beforeEvents,beforeHashes []byte
			reads:=0
			c.provider=&unslopCleanupFactProvider{base:c.provider,after:func(error){
				sealed:=c.journal.last("sealed")
				if sealed==nil {t.Fatal("successful capture did not seal its receipt")}
				beforeEvents,_=json.Marshal(c.journal.events);beforeHashes,_=json.Marshal(c.journal.hashes)
				failAt:=1;if mode=="before-sealed" {failAt=sealed.Sequence}
				if mode=="after-final-readback" {
					store.hook=func(step string)error {if c.result!=nil&&step=="after-publish" {store.readHook=func()error{return errUnslopDurable}};return nil}
				} else {
					store.readHook=func()error {reads++;if reads>=failAt{return errUnslopDurable};return nil}
				}
			}}
			initial:=c.Capture(context.Background())
			if initial.ResultSaved||initial.Qualified||initial.Receipt!=nil||!initial.ReceiptPublished||c.result==nil||c.result.Receipt==nil||published.commits!=1||session.builds!=1 {t.Fatal("fixture did not keep a successfully published receipt after final read failure",initial)}
			afterEvents,_:=json.Marshal(c.journal.events);afterHashes,_:=json.Marshal(c.journal.hashes)
			if !bytes.Equal(beforeEvents,afterEvents)||!bytes.Equal(beforeHashes,afterHashes)||c.journal.last("sealed")==nil {t.Fatal("failed final load discarded the last verified journal")}
			if c.journal.pending==nil||c.journal.pending.Kind!="final" {t.Fatal("failed final read lost its pending event")}
			pendingBytes,_:=json.Marshal(c.journal.pending)
			if again:=c.RetryFinal(context.Background());again.ResultSaved {t.Fatal("unrecovered read failure reported saved",again)}
			stillPending,_:=json.Marshal(c.journal.pending)
			if !bytes.Equal(pendingBytes,stillPending) {t.Fatal("failed retry changed the pending final")}
			store.hook=nil;store.readHook=nil
			if mode=="different-stored-bytes" {
				path:=filepath.Join(store.path,unslopDurableName(c.journal.generation,1))
				original,err:=os.ReadFile(path);if err!=nil {t.Fatal(err)}
				if err=os.WriteFile(path,append(append([]byte(nil),original...),' '),0600);err!=nil {t.Fatal(err)}
				if altered:=c.RetryFinal(context.Background());altered.ResultSaved||altered.Qualified||altered.Receipt!=nil {t.Fatal("changed stored bytes were accepted after read recovery",altered)}
				if err=os.WriteFile(path,original,0600);err!=nil {t.Fatal(err)}
			}
			wantReceipt,_:=json.Marshal(c.result.Receipt)
			final:=c.RetryFinal(context.Background())
			gotReceipt,_:=json.Marshal(final.Receipt)
			if !final.ResultSaved||!final.Qualified||!final.ReceiptPublished||!bytes.Equal(wantReceipt,gotReceipt)||published.commits!=1||session.builds!=1||c.journal.pending!=nil {t.Fatal("read recovery did not save the original receipt once",final)}
		})
	}
}

func TestUnslopDurableCancelledSaveRejectsUnboundFlight(t *testing.T) {
	for _,mode:=range []string{"same-owner-other-ticket","other-owner","recovered","unknown-ticket"} {
		t.Run(mode,func(t *testing.T){
			store:=newDurableTestStore(t)
			bound,err:=newUnslopDurableController(store,nil,nil);if err!=nil {t.Fatal(err)}
			first:=newRetainedTestFixture(t)
			if err=bound.SaveRetained(context.Background(),first.owner,first.ticket);err!=nil {t.Fatal(err)}
			otherLive,otherLease,otherDone:=retainedTestInputSession(t)
			otherResource:=&retainedTestResource{}
			otherLive.evidence=otherResource
			otherLeaseObject:=otherLease.(*retainedTestLease)
			otherAborts:=0
			otherLive.flight.(*unslopOwnedFlight).flight.Abort=func(){otherAborts++}
			otherTicket,err:=first.owner.Retain(&otherLive,&otherLease,nil,unslopToolchainCaptureResult{});if err!=nil {t.Fatal(err)}
			foreign:=newRetainedTestFixture(t)
			controller,owner,ticket:=bound,first.owner,otherTicket
			switch mode {
			case "other-owner":owner,ticket=foreign.owner,foreign.ticket
			case "recovered":
				controller,_,err=restoreUnslopDurableController(store,bound.journal.generation);if err!=nil {t.Fatal(err)}
				owner,ticket=first.owner,first.ticket
			case "unknown-ticket":ticket=&unslopRetainedTicket{sequence:otherTicket.sequence}
			}
			beforeFirst,_:=json.Marshal(first.owner.Snapshot(first.ticket))
			beforeOther,_:=json.Marshal(first.owner.Snapshot(otherTicket))
			beforeForeign,_:=json.Marshal(foreign.owner.Snapshot(foreign.ticket))
			ctx,cancel:=context.WithCancel(context.Background());cancel()
			for n:=0;n<2;n++ {
				result:=controller.DrainAndSave(ctx,owner,ticket)
				if first.aborts!=0||otherAborts!=0||foreign.aborts!=0 {t.Fatal("unbound cancellation aborted another flight",mode)}
				if result.Saved||result.Forgotten||result.Report.OwnerID!=""||result.Report.State!="unknown" {t.Fatal("unbound cancellation exposed or accepted another owner",mode,result)}
			}
			afterFirst,_:=json.Marshal(first.owner.Snapshot(first.ticket))
			afterOther,_:=json.Marshal(first.owner.Snapshot(otherTicket))
			afterForeign,_:=json.Marshal(foreign.owner.Snapshot(foreign.ticket))
			if !bytes.Equal(beforeFirst,afterFirst)||!bytes.Equal(beforeOther,afterOther)||!bytes.Equal(beforeForeign,afterForeign)||first.aborts!=0||otherAborts!=0||foreign.aborts!=0||first.resource.calls!=0||otherResource.calls!=0||foreign.resource.calls!=0||first.oldLease.calls!=0||otherLeaseObject.calls!=0||foreign.oldLease.calls!=0 {t.Fatal("unbound cancellation changed a real retained flight",mode)}
			if first.owner.Forget(first.ticket)||first.owner.Forget(otherTicket)||foreign.owner.Forget(foreign.ticket)||len(first.done)!=0||len(otherDone)!=0||len(foreign.done)!=0 {t.Fatal("unbound cancellation consumed or forgot retained input")}
			first.done<-retainedTestCompletion()
			if result:=bound.DrainAndSave(context.Background(),first.owner,first.ticket);!result.Saved||!result.Forgotten {t.Fatal("original binding did not remain usable",result)}
			otherDone<-retainedTestCompletion();first.owner.Drain(context.Background(),otherTicket);durableTestSaveBeforeForget(t,first.owner,otherTicket)
			if !first.owner.Forget(otherTicket) {t.Fatal("independent ticket cleanup failed")}
			foreign.done<-retainedTestCompletion();foreign.owner.Drain(context.Background(),foreign.ticket);durableTestSaveBeforeForget(t,foreign.owner,foreign.ticket)
			if !foreign.owner.Forget(foreign.ticket) {t.Fatal("independent owner cleanup failed")}
		})
	}
}
