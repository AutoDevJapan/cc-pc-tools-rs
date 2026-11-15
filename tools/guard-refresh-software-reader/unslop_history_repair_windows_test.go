//go:build windows

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

func historyOwnedHandleFlags(handle syscall.Handle) (uint32,bool) {
	var flags uint32
	ok,_,_:=syscall.NewLazyDLL("kernel32.dll").NewProc("GetHandleInformation").Call(uintptr(handle),uintptr(unsafe.Pointer(&flags)))
	return flags,ok!=0
}

func historyOwnedLeafMatches(handle syscall.Handle, id softwareIdentity) bool {
	var info syscall.ByHandleFileInformation
	if syscall.GetFileInformationByHandle(handle,&info)!=nil {return false}
	return id==(softwareIdentity{fmt.Sprint(info.VolumeSerialNumber),fmt.Sprint(uint64(info.FileIndexHigh)<<32|uint64(info.FileIndexLow)),fmt.Sprint(uint64(info.CreationTime.HighDateTime)<<32|uint64(info.CreationTime.LowDateTime))})
}

func historyCleanupRegistryContains(owner io.Closer) bool {
	softwareUnconfirmedCleanup.Lock()
	defer softwareUnconfirmedCleanup.Unlock()
	for _,group:=range softwareUnconfirmedCleanup.owners {for _,held:=range group {if held==owner {return true}}}
	return false
}

func TestUnslopNativeLeafCloseFailureKeepsExactHandleOwner(t *testing.T) {
	path:=filepath.Join(physicalSoftwareFixture(t),"owned-leaf")
	if err:=os.WriteFile(path,[]byte("owned leaf"),0600);err!=nil {t.Fatal(err)}
	value,err:=openUpdateObject(path)
	if err!=nil {t.Fatal("owned leaf acquisition failed")}
	object:=value.(*nativeUpdateObject)
	owner:=object.leaf
	handle,id,generation:=owner.handle,owner.identity,owner.generation
	protected:=false
	t.Cleanup(func(){
		_ = object.Close()
		if !protected {return}
		flags,ok:=historyOwnedHandleFlags(handle)
		if !ok||flags&2==0||!historyOwnedLeafMatches(handle,id) {t.Error("original protected leaf not identified");return}
		if err:=syscall.SetHandleInformation(handle,2,0);err!=nil {t.Error("owned protect reset failed");return}
		if err:=syscall.CloseHandle(handle);err!=nil {t.Error("owned native cleanup failed");return}
		protected=false
	})
	if !owner.identityKnown||generation==0||!historyOwnedLeafMatches(handle,id) {t.Fatal("owner identity missing")}
	if err:=syscall.SetHandleInformation(handle,2,2);err!=nil {t.Fatal("owned protect setup failed")}
	protected=true
	if err=object.Close();err==nil||!errors.Is(err,errSoftwareCleanupUnconfirmed) {t.Fatal("protected leaf close was not recorded")}
	if owner.file.Fd()!=uintptr(syscall.InvalidHandle) {t.Fatal("expected Go descriptor invalidation not reached")}
	if owner.handle!=handle||owner.identity!=id||owner.generation!=generation||!historyOwnedLeafMatches(handle,id)||!historyCleanupRegistryContains(owner) {t.Fatal("native leaf owner was lost")}
	if err=object.Close();err==nil||owner.closeCalls!=1 {t.Fatal("failed native close retried")}
	if err=owner.Close();err==nil||owner.closeCalls!=1 {t.Fatal("direct owner close retried")}
}

func historyCompletedStdinFacts() unslopHistoryStdinFacts {
	return unslopHistoryStdinFacts{CopyStarted:true,CopyFinished:true,CloseAttempted:true,CloseCompleted:true}
}

func TestUnslopHistoryCancelBeforeGuardDoesNotStartChild(t *testing.T) {
	ctx,cancel:=context.WithCancel(context.Background());cancel()
	answers,facts,err:=runUnslopHistoryBatchObserved(ctx,[]unslopHistoryRequest{{"thread/read",map[string]any{"threadId":"synthetic-owned"}}})
	if answers!=nil||err==nil||!errors.Is(err,context.Canceled)||!facts.Cancelled||facts.Started||facts.ChildObserved||facts.Reaped||facts.CleanupAttempted!=0 {t.Fatal("pre-guard cancellation was changed")}
	var result unslopOriginInspection;projectUnslopHistoryFailure(&result,err)
	if result.Reason!="history_cancelled" {t.Fatal("cancel reason hidden")}
}

func TestUnslopHistoryGuardCancellationAndQualificationStayDistinct(t *testing.T) {
	for _,mode:=range []string{"cancelled","deadline","unqualified","qualified","cancelled-cleanup"} {t.Run(mode,func(t *testing.T){
		ctx,cancel:=context.WithCancel(context.Background())
		defer cancel()
		if mode=="deadline" {var c context.CancelFunc;ctx,c=context.WithDeadline(ctx,time.Now().Add(-time.Second));defer c()}
		if mode=="cancelled"||mode=="cancelled-cleanup" {cancel()}
		ok:=mode=="qualified"
		err:=unslopHistoryGuardResult(ctx,[]byte("{\"access_allowed\":true,\"client_resource\":false}"),ok)
		if mode=="qualified" {if err!=nil {t.Fatal("qualified guard failed")};return}
		order:=[]int{}
		var resources []io.Closer
		if mode=="cancelled-cleanup" {resources=[]io.Closer{&historyCleanupTestCloser{order:&order,id:1,fail:true}}}
		_,facts,outcome:=finishUnslopHistoryOutcomeObserved(nil,err,unslopHistoryFacts{},resources)
		result:=unslopOriginInspection{};projectUnslopHistoryFailure(&result,outcome)
		if facts.ChildObserved||facts.Started||facts.Reaped {t.Fatal("guard cancellation became an uncollected child")}
		if mode=="unqualified" {if facts.Cancelled||result.Reason!="history_launcher_unqualified" {t.Fatal("real qualification failure changed")};return}
		if !facts.Cancelled||!errors.Is(outcome,ctx.Err()) {t.Fatal("guard context cancellation lost")}
		if mode=="cancelled-cleanup" {if !facts.ResourceCleanupFailed||result.Reason!="history_resource_cleanup_failed" {t.Fatal("cleanup overwritten")}}else if result.Reason!="history_cancelled" {t.Fatal("cancel reason changed")}
	})}
}

func historyPositiveWriteFaultFixture(t *testing.T) (askChildResult,string) {
	t.Helper()
	root:=t.TempDir()
	for _,name:=range []string{"initialized","partial-step","predicted-reply","stdin-closed"} {
		if err:=os.WriteFile(filepath.Join(root,name),[]byte("same-owned-child"),0600);err!=nil {t.Fatal(err)}
	}
	result:=historyCollectedTestResult()
	result.Capture.Complete=false
	result.Capture.protocolWritesComplete=false
	result.Capture.protocolWriteFailed=true
	result.Capture.protocolIOFailed=true
	result.Capture.Stdout=append(historyTestResponse(1),historyTestResponse(2)...)
	return result,root
}

func TestUnslopHistoryWriteFaultControlsAreIndividuallyDiscriminating(t *testing.T) {
	for _,mode:=range []string{"positive","exit6","write-not-failed","missing-first-response","initialized","partial-step","predicted-reply","stdin-closed"} {t.Run(mode,func(t *testing.T){
		result,root:=historyPositiveWriteFaultFixture(t)
		if !historyNativeWriteFaultObserved(result,root) {t.Fatal("positive fault fixture is incomplete")}
		switch mode {
		case "positive":return
		case "exit6":result.Exit=6
		case "write-not-failed":result.Capture.protocolWriteFailed=false
		case "missing-first-response":result.Capture.Stdout=historyTestResponse(2)
		default:if err:=os.Remove(filepath.Join(root,mode));err!=nil {t.Fatal(err)}
		}
		if historyNativeWriteFaultObserved(result,root) {t.Fatal("independent fault condition was ignored")}
	})}
}

func TestUnslopHistoryNativeExitAndStdinCloseFailureStaySeparate(t *testing.T) {
	for _,mode:=range []string{"complete","complete-exit3","complete-no-eof","complete-exit3-no-eof"} {t.Run(mode,func(t *testing.T){
		fault:=&unslopHistoryStdinFaults{protectWriter:mode=="complete-no-eof"||mode=="complete-exit3-no-eof"}
		owner:=historyStartNativeTestChildWithStdinFault(t,mode,fault)
		if !owner.receive(8*time.Second) {t.Fatal("same native child was not collected")}
		result:=owner.result
		if !result.Started||!result.Assigned||!result.WaitVerified||!result.Reaped||result.Capture==nil||result.Capture.protocolReason!="history_protocol_complete"||!result.Capture.protocolWritesComplete {t.Fatal("native complete protocol not reached")}
		wantExit:=0;if mode=="complete-exit3"||mode=="complete-exit3-no-eof" {wantExit=3}
		wantCloseFailure:=fault.protectWriter
		if result.Exit!=wantExit||result.Capture.historyStdin.CloseFailed!=wantCloseFailure||result.Capture.protocolIOFailed!=wantCloseFailure||!result.Capture.historyStdin.CopyFinished||result.Capture.historyStdin.CopyFailed||!result.Capture.historyStdin.CloseAttempted||!result.Capture.historyStdin.CloseCompleted {t.Fatal("native exit and same-pipe close were collapsed")}
		if fault.owner==nil||fault.owner.writer.closeCalls!=1 {t.Fatal("writer close owner not observed")}
		if wantCloseFailure {flags,ok:=historyOwnedHandleFlags(fault.owner.writer.handle);if !ok||flags&2==0||fault.owner.writer.generation==0 {t.Fatal("original protected writer missing")}}
		if wantCloseFailure&&!historyCleanupRegistryContains(fault.owner.writer) {t.Fatal("failed writer not retained")}
		facts:=unslopHistoryFacts{};observeUnslopHistoryChild(&facts,result)
		_,outcome:=finishUnslopHistoryOutcome(nil,func()error{if wantExit!=0||wantCloseFailure{return errors.New("history_capture_unavailable")};return nil}(),facts,nil)
		if wantExit==0&&!wantCloseFailure {if outcome!=nil {t.Fatal("positive rejected")};return}
		var inspection unslopOriginInspection;projectUnslopHistoryFailure(&inspection,outcome)
		raw,_:=json.Marshal(inspection)
		if inspection.History==nil||inspection.History.StdinCloseFailed!=wantCloseFailure||*inspection.History.ExitCode!=wantExit||bytes.Contains(raw,[]byte("private")) {t.Fatal("projection lost separate facts")}
	})}
}

func historyStartNativeTestChildWithStdinFault(t *testing.T, mode string, fault *unslopHistoryStdinFaults) *historyChildTestOwner {
	t.Helper()
	ctx,cancel:=context.WithTimeout(context.Background(),5*time.Second)
	owner:=historyNewChildTestOwner(t,cancel)
	owner.ctx=ctx
	var err error
	owner.root,err=os.MkdirTemp("","unslop-history-stdin-")
	if err!=nil {t.Fatal("history fixture unavailable")}
	owner.cleanupRoot=owner.root
	ctx=context.WithValue(ctx,askChildEnvironment{},map[string]string{"UNSLOP_PROTOCOL_CHILD":mode,"UNSLOP_PROTOCOL_ROOT":owner.root})
	ctx=context.WithValue(ctx,unslopHistoryStdinFaultKey{},fault)
	owner.plan,err=buildUnslopHistoryProtocol([]unslopHistoryRequest{{"thread/read",map[string]any{"threadId":"synthetic-owned"}},{"thread/read",map[string]any{"threadId":"synthetic-owned"}}})
	if err!=nil {t.Fatal(err)}
	ctx=context.WithValue(ctx,askChildCaptureKey{},askChildCapture{})
	ctx=context.WithValue(ctx,unslopHistoryProtocolKey{},owner.plan)
	t.Cleanup(func(){
		owner.cancel()
		if owner.launched&&!owner.receive(3*time.Second) {owner.retain();t.Error("same child cleanup unavailable; pipe owner retained");return}
		if owner.received&&owner.result.Started&&(!owner.result.WaitVerified||!owner.result.Reaped) {owner.retain();t.Error("same child not collected; pipe owner retained");return}
		if fault.owner==nil||!fault.protectWriter {return}
		writer:=fault.owner.writer
		writer.mu.Lock()
		failedCloseKnown:=writer.closeCompleted&&writer.closeErr!=nil&&writer.generation!=0
		writer.mu.Unlock()
		if !failedCloseKnown {owner.retain();t.Error("writer close unfinished; original owner retained");return}
		flags,ok:=historyOwnedHandleFlags(writer.handle)
		if !ok||flags&2==0 {t.Error("original protected writer unavailable");return}
		if err:=syscall.SetHandleInformation(writer.handle,2,0);err!=nil {t.Error("writer protect reset failed");return}
		if err:=syscall.CloseHandle(writer.handle);err!=nil {t.Error("writer native cleanup failed")}
	})
	owner.launched=true
	go func(){owner.done<-askOwnedChild(ctx,os.Args[0],"-test.run=^TestUnslopHistoryProtocolChild$")}()
	return owner
}

func TestUnslopHistoryGuardReturnAfterCancellationKeepsCancellation(t *testing.T) {
	ctx,cancel:=context.WithCancel(context.Background())
	entered,release,done:=make(chan struct{}),make(chan struct{}),make(chan error,1)
	go func(){
		close(entered)
		<-release
		done<-unslopHistoryGuardResult(ctx,[]byte("{\"access_allowed\":false}"),false)
	}()
	<-entered
	cancel()
	close(release)
	select {
	case err:=<-done:
		_,facts,outcome:=finishUnslopHistoryOutcomeObserved(nil,err,unslopHistoryFacts{},nil)
		var result unslopOriginInspection;projectUnslopHistoryFailure(&result,outcome)
		if !errors.Is(err,context.Canceled)||!facts.Cancelled||facts.ChildObserved||result.Reason!="history_cancelled" {t.Fatal("guard return hid concurrent cancellation")}
	case <-time.After(time.Second):
		t.Fatal("same guard boundary did not finish")
	}
}

func TestUnslopClosedOwnerNeverClosesAnotherGeneration(t *testing.T) {
	root:=physicalSoftwareFixture(t)
	firstPath,secondPath:=filepath.Join(root,"first"),filepath.Join(root,"second")
	if err:=os.WriteFile(firstPath,[]byte("first"),0600);err!=nil {t.Fatal(err)}
	if err:=os.WriteFile(secondPath,[]byte("second"),0600);err!=nil {t.Fatal(err)}
	firstValue,err:=openUpdateObject(firstPath);if err!=nil {t.Fatal("first acquisition failed")}
	first:=firstValue.(*nativeUpdateObject)
	if err=first.Close();err!=nil {t.Fatal("first cleanup failed")}
	secondValue,err:=openUpdateObject(secondPath);if err!=nil {t.Fatal("second acquisition failed")}
	second:=secondValue.(*nativeUpdateObject);defer second.Close()
	if first.leaf.generation==second.leaf.generation {t.Fatal("owner generation reused")}
	// Exercise a stale raw value explicitly, independent of OS handle allocation.
	first.leaf.handle=second.leaf.handle
	if err=first.leaf.Close();err!=nil||first.leaf.closeCalls!=1 {t.Fatal("old owner retried close")}
	if !historyOwnedLeafMatches(second.leaf.handle,second.leaf.identity)||string(second.Raw())!="second" {t.Fatal("old owner affected new resource")}
}
