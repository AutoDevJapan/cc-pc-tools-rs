//go:build windows

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func historyOutcomePositive() unslopHistoryFacts {
	code:=0
	return unslopHistoryFacts{StdinCopyStarted:true,StdinCopyFinished:true,StdinCloseAttempted:true,StdinCloseCompleted:true,ChildObserved:true,Started:true,WaitVerified:true,Reaped:true,Assigned:true,CollectionReason:"history_job_collected",CollectionObserved:1,CollectionSignalled:1,CollectionTotal:1,CollectionTotalKnown:true,CollectionRootMatched:true,ExitCode:&code,ProtocolReason:"history_protocol_complete",WritesComplete:true}
}

func TestUnslopHistoryCompositeOutcomeRetainsNativeAndCleanupFacts(t *testing.T) {
	for _,mode:=range []string{"unreaped-protocol","unreaped-protocol-cleanup","not-started","collected-protocol","success"} {
		t.Run(mode,func(t *testing.T){
			facts:=historyOutcomePositive()
			var previous error
			if mode!="success" {previous=errors.New("history_protocol_unavailable");facts.ProtocolReason="history_protocol_response_incomplete"}
			if strings.HasPrefix(mode,"unreaped") {facts.Reaped=false}
			if mode=="not-started" {facts.Started=false;facts.Reaped=false;facts.WaitVerified=false;facts.ExitCode=nil}
			order:=[]int{}
			resource:=&historyCleanupTestCloser{order:&order,id:1,fail:mode=="unreaped-protocol-cleanup"}
			body:=[]byte("candidate response")
			answers,err:=finishUnslopHistoryOutcome([]json.RawMessage{body},previous,facts,[]io.Closer{resource})
			if resource.calls!=1 {t.Fatal("cleanup not attempted")}
			if mode=="success" {if err!=nil||len(answers)!=1 {t.Fatal("positive rejected")};return}
			if err==nil||answers!=nil||!bytes.Equal(body,make([]byte,len(body))) {t.Fatal("failed outcome returned bytes")}
			result:=unslopOriginInspection{Reason:"normal_history_provider_unavailable"}
			projectUnslopHistoryFailure(&result,err)
			if result.History==nil||result.History.Started!=facts.Started||result.History.Reaped!=facts.Reaped||result.History.ProtocolReason!=facts.ProtocolReason {t.Fatal("independent facts lost")}
			if strings.HasPrefix(mode,"unreaped")&&result.Reason!="history_process_collection_unavailable" {t.Fatal("unreaped fact lost")}
			if mode=="not-started"&&result.Reason=="history_process_collection_unavailable" {t.Fatal("not started became unreaped")}
			if mode=="unreaped-protocol-cleanup"&&!result.History.ResourceCleanupFailed {t.Fatal("cleanup fact lost")}
			raw,_:=json.Marshal(result)
			if bytes.Contains(raw,[]byte("candidate response"))||bytes.Contains(raw,[]byte("private")) {t.Fatal("raw detail escaped")}
		})
	}
}

func TestUnslopHistoryNativeExitAndPipeFailureRemainSeparate(t *testing.T) {
	for _,tc:=range []struct{name string;wait error;write,stdout,stderr,want bool}{
		{"complete",nil,false,true,true,false},
		{"exit-only",&exec.ExitError{},false,true,true,false},
		{"pipe",errors.New("private pipe"),false,true,true,true},
		{"exit-and-write",&exec.ExitError{},true,true,true,true},
		{"exit-and-missing-eof",&exec.ExitError{},false,true,false,true},
	} {t.Run(tc.name,func(t *testing.T){
		if unslopHistoryIOFailed(tc.wait,tc.write,tc.stdout,tc.stderr,historyCompletedStdinFacts())!=tc.want {t.Fatal("I/O classification changed")}
	})}
	facts:=historyOutcomePositive();code:=3;facts.ExitCode=&code
	_,err:=finishUnslopHistoryOutcome(nil,errors.New("history_process_collection_unavailable"),facts,nil)
	result:=unslopOriginInspection{};projectUnslopHistoryFailure(&result,err)
	if result.Reason!="history_child_exit_failed"||result.History==nil||*result.History.ExitCode!=3||result.History.IOFailed {t.Fatal("exit-only was reported as I/O")}
}

func TestUnslopPartialAcquisitionCleanupPreservesErrorAndOwnership(t *testing.T) {
	for _,fail:=range []bool{false,true} {t.Run(map[bool]string{false:"closed",true:"unconfirmed"}[fail],func(t *testing.T){
		order:=[]int{}
		parent:=&historyCleanupTestCloser{order:&order,id:1,fail:fail}
		leaf:=&historyCleanupTestCloser{order:&order,id:2}
		err:=finishSoftwareAcquisition(errUpdateObject,parent,leaf)
		if !errors.Is(err,errUpdateObject)||errors.Is(err,errSoftwareCleanupUnconfirmed)!=fail {t.Fatal("error compatibility lost")}
		report:=softwareCleanupFacts(err)
		if report.Attempted!=2||report.Failed!=map[bool]int{false:0,true:1}[fail]||parent.calls!=1||leaf.calls!=1 {t.Fatal("close attempts lost")}
		facts:=unslopHistoryFacts{};noteUnslopHistoryAcquisition(&facts,err)
		_,outcome:=finishUnslopHistoryOutcome(nil,err,facts,nil)
		result:=unslopOriginInspection{};projectUnslopHistoryFailure(&result,outcome)
		if result.History==nil||!result.History.AcquisitionFailed||!result.History.PartialAcquisition||result.History.PartialCleanupFailed!=fail||result.History.ChildObserved||!errors.Is(outcome,errUpdateObject)||errors.Is(outcome,errSoftwareCleanupUnconfirmed)!=fail {t.Fatal("partial acquisition facts lost")}
	})}
}

func TestUnslopOpenPlainDetailedKeepsLegacySuccessAndRejectsMissing(t *testing.T) {
	file,err:=os.CreateTemp(physicalSoftwareFixture(t),"history-open-control-")
	if err!=nil {t.Fatal(err)}
	path:=file.Name();defer os.Remove(path)
	if _,err=file.Write([]byte("fixed"));err!=nil {t.Fatal(err)}
	if err=file.Close();err!=nil {t.Fatal(err)}
	handle,err:=openPlainDetailed(path,false)
	if err!=nil||handle==0 {t.Fatal("detailed positive failed")}
	if err=(&softwareOwnedHandle{handle:handle}).Close();err!=nil {t.Fatal(err)}
	legacy,ok:=openPlain(path,false)
	if !ok||legacy==0 {t.Fatal("legacy positive failed")}
	if err=(&softwareOwnedHandle{handle:legacy}).Close();err!=nil {t.Fatal(err)}
	_,err=openUpdateObject(path+"-missing")
	if err==nil||!errors.Is(err,errUpdateObject) {t.Fatal("missing object accepted")}
	report:=softwareCleanupFacts(err)
	if report.Attempted==0||report.Failed!=0 {t.Fatal("partial acquisition success not recorded")}
}

type historyOutcomeTestRPC struct { failure error; calls, failureAt int }
func (r *historyOutcomeTestRPC) Call(context.Context,string,any) (json.RawMessage,error) {
	r.calls++
	if r.failureAt==2 && r.calls==1 {return json.RawMessage("{\"thread\":{\"id\":\"synthetic-one\",\"source\":\"cli\",\"turns\":[]}}"),nil}
	return nil,r.failure
}
func TestUnslopHistoryNormalInspectionPreservesCompositeFailure(t *testing.T) {
	facts:=historyOutcomePositive();facts.Reaped=false;facts.WriteFailed=true;facts.ProtocolReason="history_protocol_stdin_write_failed"
	order:=[]int{}
	resource:=&historyCleanupTestCloser{order:&order,id:1,fail:true}
	_,err:=finishUnslopHistoryOutcome(nil,errors.New("private token path"),facts,[]io.Closer{resource})
	rpc:=&historyOutcomeTestRPC{failure:err}
	result:=inspectUnslopEvaluationOrigin(context.Background(),rpc,[2]string{"synthetic-one","synthetic-two"})
	if rpc.calls!=1||result.History==nil||!result.History.Started||result.History.Reaped||!result.History.WriteFailed||!result.History.ResourceCleanupFailed||result.Reason!="history_process_collection_unavailable" {t.Fatal("normal inspection collapsed failure")}
	if result.OriginalFilesQualified||result.BodyReads!=0 {t.Fatal("failure minted authority")}
	raw,_:=json.Marshal(result)
	if bytes.Contains(raw,[]byte("private token path"))||bytes.Contains(raw,[]byte("private cleanup detail")) {t.Fatal("failure leaked original detail")}
	legacy:=inspectUnslopEvaluationOrigin(context.Background(),nil,[2]string{})
	legacyRaw,_:=json.Marshal(legacy)
	if legacy.History!=nil||bytes.Contains(legacyRaw,[]byte("\"history\":")) {t.Fatal("legacy result unexpectedly has facts")}
}

func TestUnslopCleanupFailureRetainsExactOwnerReference(t *testing.T) {
	order:=[]int{}
	resource:=&historyCleanupTestCloser{order:&order,id:1,fail:true}
	_ = finishSoftwareAcquisition(errUpdateObject,resource)
	softwareUnconfirmedCleanup.Lock()
	found:=false
	for _,group:=range softwareUnconfirmedCleanup.owners {for _,owner:=range group {if owner==resource {found=true}}}
	softwareUnconfirmedCleanup.Unlock()
	if !found||resource.calls!=1 {t.Fatal("unconfirmed original owner lost or retried")}
}

func TestUnslopHistorySuccessfulObservedOutcomeIncludesCleanupFacts(t *testing.T) {
	facts:=historyOutcomePositive()
	order:=[]int{}
	resource:=&historyCleanupTestCloser{order:&order,id:1}
	body:=[]byte("answer")
	answers,observed,err:=finishUnslopHistoryOutcomeObserved([]json.RawMessage{body},nil,facts,[]io.Closer{resource})
	if err!=nil||len(answers)!=1||observed.CleanupAttempted!=1||observed.CleanupUnconfirmed!=0||!observed.Started||!observed.Reaped {t.Fatal("successful observed facts lost")}
}

func TestUnslopNativePartialCleanupReportsActualFailedHandle(t *testing.T) {
	for _,protect:=range []bool{false,true} {t.Run(map[bool]string{false:"closed",true:"protected"}[protect],func(t *testing.T){
		path:=filepath.Join(physicalSoftwareFixture(t),"partial-owned-leaf")
		if err:=os.WriteFile(path,[]byte("owned partial acquisition"),0600);err!=nil {t.Fatal("partial fixture creation failed")}
		handle,err:=openPlainDetailed(path,false)
		if err!=nil {t.Fatal("partial fixture acquisition failed")}
		file:=os.NewFile(uintptr(handle),path)
		if file==nil {_ = syscall.CloseHandle(handle);t.Fatal("partial fixture file unavailable")}
		owner:=newSoftwareOwnedFile(file)
		object:=&nativeUpdateObject{f:file,leaf:owner,raw:[]byte("private buffered bytes")}
		id,generation:=owner.identity,owner.generation
		protected:=false
		t.Cleanup(func(){
			_ = object.Close()
			if !protected {return}
			flags,known:=historyOwnedHandleFlags(handle)
			if !known||flags&2==0||!owner.identityKnown||!historyOwnedLeafMatches(handle,id) {t.Error("partial cleanup owner not identified");return}
			owner.mu.Lock()
			failedClose:=owner.closeCompleted&&owner.closeErr!=nil
			owner.mu.Unlock()
			if !failedClose {t.Error("partial cleanup close result unavailable");return}
			if err:=syscall.SetHandleInformation(handle,2,0);err!=nil {t.Error("partial cleanup protection reset failed");return}
			if err:=syscall.CloseHandle(handle);err!=nil {t.Error("partial cleanup native release failed");return}
			protected=false
		})
		if !owner.identityKnown||generation==0||!historyOwnedLeafMatches(handle,id) {t.Fatal("partial fixture native identity unavailable")}
		if protect {
			if err:=syscall.SetHandleInformation(handle,2,2);err!=nil {t.Fatal("partial fixture protection failed")}
			protected=true
		}
		err=finishSoftwareAcquisition(errUpdateObject,object)
		primary,unconfirmed:=errors.Is(err,errUpdateObject),errors.Is(err,errSoftwareCleanupUnconfirmed)
		report:=softwareCleanupFacts(err)
		wantFailed:=0;if protect {wantFailed=1}
		if !primary||unconfirmed!=protect||report.Attempted!=1||report.Failed!=wantFailed||len(object.raw)!=0||owner.closeCalls!=1 {
			t.Fatalf("native_partial_cleanup primary=%t unconfirmed=%t protected=%t attempted=%d failed=%d raw_cleared=%t close_calls=%d",primary,unconfirmed,protect,report.Attempted,report.Failed,len(object.raw)==0,owner.closeCalls)
		}
		if protect {
			sameOwner:=owner.handle==handle&&owner.identity==id&&owner.generation==generation&&historyOwnedLeafMatches(handle,id)&&historyCleanupRegistryContains(owner)
			if !sameOwner||len(object.unconfirmedResources)!=1||owner.file.Fd()!=uintptr(syscall.InvalidHandle) {t.Fatal("partial cleanup lost original native owner")}
			if object.Close()==nil||owner.Close()==nil||owner.closeCalls!=1 {t.Fatal("partial cleanup retried failed close")}
		} else if object.Close()!=nil||owner.closeCalls!=1 {t.Fatal("partial cleanup repeated native close")}
	})}
}

func TestUnslopHistoryFinalCollectionFactsSurviveProjection(t *testing.T) {
	for _,mode:=range []string{"complete","unreaped","unknown-count","wrong-root"} {t.Run(mode,func(t *testing.T){
		child:=historyCollectedTestResult()
		if mode=="unreaped" {child.Reaped=false}
		if mode=="unknown-count" {child.CollectionTotalKnown=false}
		if mode=="wrong-root" {native:=*child.Capture.NativeProcess;native.Creation++;child.Capture.NativeProcess=&native}
		facts:=unslopHistoryFacts{};observeUnslopHistoryChild(&facts,child)
		answers,observed,err:=finishUnslopHistoryOutcomeObserved([]json.RawMessage{[]byte("{}")},nil,facts,nil)
		if mode=="complete" {if err!=nil||len(answers)!=1||!observed.CollectionRootMatched {t.Fatal("complete collection rejected")};return}
		if err==nil||answers!=nil {t.Fatal("unconfirmed collection accepted")}
		result:=unslopOriginInspection{};projectUnslopHistoryFailure(&result,err)
		if result.History==nil||result.History.CollectionTotalKnown!=child.CollectionTotalKnown||result.History.CollectionObserved!=child.CollectionObserved||result.History.CollectionSignalled!=child.CollectionSignalled||result.History.CollectionTotal!=child.CollectionTotal||result.History.Reaped!=child.Reaped {t.Fatal("final native observation lost")}
		if mode=="wrong-root"&&result.History.CollectionRootMatched {t.Fatal("wrong root accepted")}
	})}
}

func TestUnslopHistoryProjectionDoesNotForwardUnknownReasons(t *testing.T) {
	facts:=historyOutcomePositive();facts.ProtocolReason="private protocol details";facts.CollectionReason="private collection details"
	failure:=&unslopHistoryFailure{reason:"private operation details",facts:facts}
	result:=unslopOriginInspection{};projectUnslopHistoryFailure(&result,failure)
	raw,err:=json.Marshal(result)
	if err!=nil||bytes.Contains(raw,[]byte("private"))||result.Reason!="history_operation_failed"||result.History.ProtocolReason!="history_protocol_unavailable"||result.History.CollectionReason!="" {t.Fatal("unknown detail escaped")}
}

func TestUnslopHistoryCLIEncodesCompositeFailureAtBothRPCStages(t *testing.T) {
	for _,failureAt:=range []int{1,2} {t.Run(map[int]string{1:"thread-read",2:"turns-list"}[failureAt],func(t *testing.T){
		facts:=historyOutcomePositive();facts.Reaped=false;facts.ProtocolReason="history_protocol_stdin_write_failed";facts.WriteFailed=true
		order:=[]int{}
		_,failure:=finishUnslopHistoryOutcome(nil,errors.New("private transport detail"),facts,[]io.Closer{&historyCleanupTestCloser{order:&order,id:1,fail:true}})
		rpc:=&historyOutcomeTestRPC{failure:failure,failureAt:failureAt}
		var output bytes.Buffer
		code:=runUnslopPurpose(context.Background(),[]string{"--inspect-unslop-evaluation-origin"},&output,unslopPurposeRuntime{history:rpc,historyThreads:[2]string{"synthetic-one","synthetic-two"}})
		var result unslopOriginInspection
		if code!=1||json.Unmarshal(output.Bytes(),&result)!=nil||rpc.calls!=failureAt {t.Fatal("CLI stage not reached")}
		if result.Reason!="history_process_collection_unavailable"||result.History==nil||!result.History.Started||result.History.Reaped||!result.History.WriteFailed||!result.History.ResourceCleanupFailed||result.OriginalFilesQualified||result.BodyReads!=0 {t.Fatal("CLI lost independent failure facts")}
		if bytes.Contains(output.Bytes(),[]byte("private")) {t.Fatal("CLI forwarded private failure detail")}
	})}
}
