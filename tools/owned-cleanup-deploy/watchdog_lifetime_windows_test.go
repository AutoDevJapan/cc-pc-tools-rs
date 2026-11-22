//go:build windows

package main

import (
	"runtime"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type watchdogLifetimeFixture struct {
	beforeErr,confirmErr,publishedErr,holdErr,closeErr error
	hold watchdogLifetimeHold
	beforeCalls,confirmCalls,publishedCalls,holdCalls,closeCalls int
}
func (f *watchdogLifetimeFixture) BeforeStop(context.Context,watchdogTaskSnapshot,[]*watchdogProcess)error{f.beforeCalls++;return f.beforeErr}
func (f *watchdogLifetimeFixture) ConfirmStopped(context.Context,watchdogTaskSnapshot)error{f.confirmCalls++;return f.confirmErr}
func (f *watchdogLifetimeFixture) BeforeRelease(context.Context,watchdogTaskSnapshot,watchdogFileIdentity,time.Time)error{return f.beforeErr}
func (f *watchdogLifetimeFixture) ConfirmPublishedStopped(context.Context,watchdogTaskSnapshot,watchdogFileIdentity,time.Time)error{f.publishedCalls++;return f.publishedErr}
func (f *watchdogLifetimeFixture) AcquireQuiescentHold(context.Context,watchdogTaskSnapshot,watchdogFileIdentity,time.Time)(watchdogLifetimeHold,error){f.holdCalls++;return f.hold,f.holdErr}
func (f *watchdogLifetimeFixture) Close()error{f.closeCalls++;return f.closeErr}
type watchdogLifetimeHoldFixture struct{releaseCalls,closeCalls int;releaseErr,closeErr error}
func(f *watchdogLifetimeHoldFixture) Release(context.Context)error{f.releaseCalls++;return f.releaseErr}
func(f *watchdogLifetimeHoldFixture) Close()error{f.closeCalls++;return f.closeErr}
func watchdogLifetimeTask() watchdogTaskSnapshot {
	return watchdogTaskSnapshot{View:watchdogTaskView{Name:watchdogTask,XMLSHA256:strings.Repeat("a",64),S4U:true,Limited:true,
		Executable:watchdogLauncher,Arguments:"--launch-service opencodex_watchdog",WorkingDirectory:"D:/CC/Tools/resident-guard",
		ResidentGuardDisabled:true,CreditWatchdogDisabled:true},Principal:"S-1-5-21-1"}
}
func watchdogLifetimeIdentity()watchdogFileIdentity{
	return watchdogFileIdentity{Volume:1,File:1,Created:1,Bytes:100,Links:1,SHA256:strings.Repeat("a",64),SecuritySHA256:strings.Repeat("b",64)}
}
func TestWatchdogCollectionRequiresSameGenerationLifetimeProof(t *testing.T){
	for _,kind:=range []string{"normal","no-owner","late-child","query-unknown","cancelled"}{
		t.Run(kind,func(t *testing.T){
			f:=&watchdogLifetimeFixture{};r:=&watchdogResidentNative{task:watchdogLifetimeTask(),lifetime:f};r.collection.begin(true)
			ctx,cancel:=context.WithCancel(context.Background());defer cancel()
			switch kind{case "no-owner":r.lifetime=nil;case "late-child","query-unknown":f.confirmErr=errWatchdogCollectionUnknown;case "cancelled":cancel()}
			e:=r.confirmCollection(ctx,watchdogLifetimeTask(),false)
			if (e==nil)!=(kind=="normal")||r.collection.confirmed!=(kind=="normal"){t.Fatal("snapshot/unknown became complete",e)}
			if kind!="normal"&&!errors.Is(r.collection.cleanupError(),errWatchdogCollectionUnknown){t.Fatal("unreaped fact lost")}
		})
	}
}
func TestWatchdogNeverStartedAndExitedGenerationsUseOwnedQuiescentHold(t *testing.T){
	for _,kind:=range []string{"never-started","already-exited","unknown","foreign-task","no-hold","partial-hold","read-changed","cancelled"}{
		t.Run(kind,func(t *testing.T){
			ctx,cancel:=context.WithCancel(context.Background());defer cancel()
			snapshot:=watchdogLifetimeTask();id:=watchdogLifetimeIdentity();hold:=&watchdogLifetimeHoldFixture{}
			f:=&watchdogLifetimeFixture{hold:hold}
			r:=&watchdogResidentNative{task:snapshot,lifetime:f,published:time.Now(),candidate:func()watchdogFileIdentity{return id}}
			r.taskRead=func(context.Context,string,string,string,uint32)(watchdogTaskSnapshot,error){if kind=="read-changed"{return watchdogTaskSnapshot{},errors.New("changed")};return snapshot,nil}
			switch kind{case "unknown":f.publishedErr=errWatchdogCollectionUnknown;case "foreign-task":snapshot.Principal="foreign";case "no-hold":f.hold=nil;case "partial-hold":f.holdErr=errors.New("partial");case "cancelled":cancel()}
			proof,e:=r.holdQuiescentPublished(ctx,snapshot,id)
			good:=kind=="never-started"||kind=="already-exited"
			if (e==nil)!=good||proof.valid()!=good{t.Fatal("invalid no-instance proof",e,proof)}
			if good&&!proof.OwnedGeneration{t.Fatal("missing quiescent generation")}
			if good&&r.recoveryHold.release(ctx)!=nil{t.Fatal("release")}
			_ = r.closeCollectionResources()
			if len(r.retainedHolds)>0&&hold.closeCalls!=1{t.Fatal("partial hold lost",hold.closeCalls)}
			if !good&&kind!="foreign-task"&&kind!="cancelled"&&r.collection.confirmed{t.Fatal("unknown collection accepted")}
		})
	}
}
func TestWatchdogCollectionCleanupDistinguishesCheckOnlyAndStopAttempted(t *testing.T){
	for _,kind:=range []string{"check-only","stop-unknown","stop-collected","hold-unknown"}{
		t.Run(kind,func(t *testing.T){
			r:=&watchdogResidentNative{pins:newWatchdogPinScope()}
			if kind!="check-only"{r.collection.begin(kind!="hold-unknown")}
			if kind=="stop-collected"{r.collection.confirm()}
			e:=r.Close();again:=r.Close();bad:=kind=="stop-unknown"||kind=="hold-unknown"
			if errors.Is(e,errWatchdogCollectionUnknown)!=bad||errors.Is(again,errWatchdogCollectionUnknown)!=bad{t.Fatal("close erased unreaped fact",e,again)}
		})
	}
}
func TestWatchdogGenerationProofStillRequiresCollectionAndHold(t *testing.T){
	for _,p:=range []watchdogStopped{{false,true,true},{true,false,true},{true,true,false}}{if p.valid(){t.Fatal("incomplete generation proof")}}
	if !(watchdogStopped{true,true,true}).valid(){t.Fatal("complete generation rejected")}
}
func TestWatchdogLifetimeIssuerUnavailableBeforeAnyCollectionOrStop(t *testing.T){
	c,e:=openNormalWatchdogLifetime(context.Background(),watchdogLifetimeTask(),nil)
	if c!=nil||e==nil{t.Fatal("normal lifetime proof fabricated")}
}
func TestWatchdogRecoveryHoldReleaseAndCloseFailuresStaySeparateAndOnce(t *testing.T){
	release:=errors.New("release-failed");cleanup:=errors.New("close-failed")
	f:=&watchdogLifetimeHoldFixture{releaseErr:release,closeErr:cleanup};s:=&watchdogLifetimeHoldState{hold:f}
	for n:=0;n<2;n++{if !errors.Is(s.release(context.Background()),release){t.Fatal("release lost")};e:=s.close();if !errors.Is(e,release)||!errors.Is(e,cleanup){t.Fatal("cleanup facts lost")}}
	if f.releaseCalls!=1||f.closeCalls!=1{t.Fatal("retried hold",f.releaseCalls,f.closeCalls)}
}

func TestWatchdogStopUnknownPreservesPrimaryAndUnreapedControllerFacts(t *testing.T){
	r:=&watchdogResidentNative{pins:newWatchdogPinScope()};r.collection.begin(true)
	result:=runWatchdogUpdate(context.Background(),true,func(context.Context)(*watchdogUpdateResources,error){
		return &watchdogUpdateResources{Resident:r},errors.New("stop-primary")
	})
	if result.Qualified||!result.Unreaped||!result.CleanupFailed||!result.ReconciliationRequired||result.retained==nil||
		result.Reason!="watchdog_distribution_issuer_unavailable" {t.Fatalf("primary/collection/cleanup facts conflated: %+v",result)}
}

// This is the real update consumer with a synthetic lifetime owner; no native
// Task/Job is created. Its accepted proof cannot be substituted by a snapshot.
type watchdogCollectionConsumerFixture struct{
	*watchdogTestResident
	native *watchdogResidentNative
}
func(f *watchdogCollectionConsumerFixture) StopAndHold(ctx context.Context)(watchdogStopped,error){
	f.native.collection.begin(true)
	if e:=f.native.confirmCollection(ctx,f.native.task,false);e!=nil{return watchdogStopped{},e}
	return watchdogStopped{true,true,true},nil
}
func(f *watchdogCollectionConsumerFixture) CollectionPending()bool{return f.native.CollectionPending()}
func(f *watchdogCollectionConsumerFixture) StopAttempted()bool{return f.native.StopAttempted()}
func(f *watchdogCollectionConsumerFixture) Close()error{return errors.Join(f.watchdogTestResident.Close(),f.native.Close())}
func TestWatchdogUnprovenLifetimeCannotReachStageButNormalProofCan(t *testing.T){
	for _,kind:=range []string{"normal","late-child","unknown"}{
		t.Run(kind,func(t *testing.T){
			all,files,resident:=watchdogTestResources();owner:=&watchdogLifetimeFixture{}
			if kind!="normal"{owner.confirmErr=errWatchdogCollectionUnknown}
			all.Resident=&watchdogCollectionConsumerFixture{watchdogTestResident:resident,native:&watchdogResidentNative{
				task:watchdogLifetimeTask(),lifetime:owner,pins:newWatchdogPinScope()}}
			result:=runWatchdogUpdate(context.Background(),true,func(context.Context)(*watchdogUpdateResources,error){return all,nil})
			good:=kind=="normal"
			if result.Qualified!=good||result.Steps.StageCreated!=good||!result.StopAttempted||result.Unreaped==good{
				t.Fatalf("collection gate lost: %+v calls=%v",result,files.calls)
			}
			if !good&&(result.Reason!="watchdog_owned_stop_unconfirmed"||!result.CleanupFailed||!result.ReconciliationRequired){t.Fatal("primary or unreaped lost",result)}
		})
	}
}

// This fixture runs the real update recovery consumer and the real resident
// ReleaseHold/StopPublishedAndHold/Close methods. Its lifetime owner is synthetic;
// no Task stop, process open, mutex operation or installed write is performed.
type watchdogRecoveryReleaseFixture struct{
	*watchdogTestResident
	native *watchdogResidentNative
}
func(f *watchdogRecoveryReleaseFixture) ReleaseHold(ctx context.Context)error{return f.native.ReleaseHold(ctx)}
func(f *watchdogRecoveryReleaseFixture) StopPublishedAndHold(ctx context.Context,id watchdogFileIdentity)(watchdogStopped,error){return f.native.StopPublishedAndHold(ctx,id)}
func(f *watchdogRecoveryReleaseFixture) StopAttempted()bool{return f.native.StopAttempted()}
func(f *watchdogRecoveryReleaseFixture) CollectionPending()bool{return f.native.CollectionPending()}
func(f *watchdogRecoveryReleaseFixture) Close()error{return errors.Join(f.native.Close(),f.watchdogTestResident.Close())}
func TestWatchdogRecoveryConsumerReleasesNewHoldDespitePriorFailure(t *testing.T){
	for _,kind:=range []string{"normal","prior-release-failure","new-release-failure","new-close-failure","compound","partial-hold"}{
		t.Run(kind,func(t *testing.T){
			ctx:=context.Background();all,files,resident:=watchdogTestResources();resident.fail="observe"
			prior:=errors.New("prior-release-failure");release:=errors.New("new-release-failure");cleanup:=errors.New("new-close-failure")
			hold:=&watchdogLifetimeHoldFixture{}
			owner:=&watchdogLifetimeFixture{hold:hold}
			snapshot:=watchdogLifetimeTask();snapshot.View=resident.task
			native:=&watchdogResidentNative{task:snapshot,lifetime:owner,published:time.Now(),pins:newWatchdogPinScope(),candidate:func()watchdogFileIdentity{return files.Candidate()}}
			if kind!="normal"{native.releaseErr=prior}
			if kind=="new-release-failure"||kind=="compound"{hold.releaseErr=release}
			if kind=="new-close-failure"||kind=="compound"{hold.closeErr=cleanup}
			if kind=="partial-hold"{owner.holdErr=errors.New("partial-hold")}
			native.taskRead=func(context.Context,string,string,string,uint32)(watchdogTaskSnapshot,error){return snapshot,nil}
			// ReleaseHold enforces its actual pinned-thread identity even though
			// this fixture acquires no native mutex or process handles.
			runtime.LockOSThread();native.threadPinned=true
			id,_,_:=kernel.NewProc("GetCurrentThreadId").Call();native.threadID=uint32(id)
			t.Cleanup(func(){_ = native.Close()})
			all.Resident=&watchdogRecoveryReleaseFixture{watchdogTestResident:resident,native:native}
			result:=runWatchdogUpdate(ctx,true,func(context.Context)(*watchdogUpdateResources,error){return all,nil})
			partial:=kind=="partial-hold"
			if result.Qualified||!result.Updated||!result.RollbackAttempted||result.RollbackSucceeded==partial||hold.closeCalls!=1||owner.closeCalls!=1||files.closeCount!=1||resident.closeCount!=1{
				t.Fatalf("same consumer recovery facts: %+v release=%d close=%d",result,hold.releaseCalls,hold.closeCalls)
			}
			wantRelease:=1;if partial{wantRelease=0}
			if hold.releaseCalls!=wantRelease{t.Fatal("new hold release skipped/retried",kind,hold.releaseCalls)}
			if kind=="normal"{if result.CleanupFailed||result.RecoveryCleanupFailed{t.Fatal("normal recovery failed",result)}}else{
				if result.Reason!="watchdog_hold_release_failed"||!result.CleanupFailed||!result.ReconciliationRequired||!errors.Is(native.closeErr,prior){t.Fatal("prior failure lost",result,native.closeErr)}
			}
			if !partial&&kind!="normal"&&result.RecoveryReason!="watchdog_recovery_hold_release_failed"{t.Fatal("recovery failure lost",result)}
			if partial&&result.RecoveryReason!="watchdog_candidate_stop_unconfirmed"{t.Fatal("partial hold permitted rollback",result)}
			if hold.releaseErr!=nil&&!errors.Is(native.closeErr,release){t.Fatal("new release failure lost")}
			if hold.closeErr!=nil&&!errors.Is(native.closeErr,cleanup){t.Fatal("new close failure lost")}
			_ = all.close();_ = native.Close()
			if hold.releaseCalls!=wantRelease||hold.closeCalls!=1||owner.closeCalls!=1{t.Fatal("cleanup retried a hold")}
		})
	}
}
