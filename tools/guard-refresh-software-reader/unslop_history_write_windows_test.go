//go:build windows

package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

func historyTestResponse(id int) []byte {
	raw,_:=json.Marshal(map[string]any{"id":id,"result":map[string]any{}})
	return append(raw,10)
}

func historyWaitWritingStep(t *testing.T, x *unslopHistoryExchange, step int) {
	t.Helper()
	deadline := time.Now().Add(2*time.Second)
	for time.Now().Before(deadline) {
		x.mu.Lock()
		writing, current, reason := x.writing, x.step, x.reason
		x.mu.Unlock()
		if writing && current == step { return }
		if reason != "" { t.Fatal("writer failed before expected step",reason) }
		runtime.Gosched()
	}
	t.Fatal("writer did not begin expected fixed step")
}

func historyWaitWriterStopped(t *testing.T, o *historyPipeTestOwner) error {
	t.Helper()
	select {
	case err := <-o.done:
		o.done <- err
		return err
	case <-time.After(2*time.Second): t.Fatal("same writer did not terminate"); return nil
	}
}

func TestUnslopHistoryUntrackedReadAndPrematureResponseCannotComplete(t *testing.T) {
	for _, mode := range []string{"read-without-write", "predicted-before-write"} {
		t.Run(mode,func(t *testing.T){
			plan,err:=buildUnslopHistoryProtocol([]unslopHistoryRequest{{"thread/read",map[string]any{"threadId":"synthetic-owned"}}})
			if err!=nil {t.Fatal(err)}
			defer plan.erase()
			x,err:=newUnslopHistoryExchange(context.Background(),plan)
			if err!=nil {t.Fatal(err)}
			defer x.Close()
			if mode=="read-without-write" {
				buffer:=make([]byte,1<<20)
				if n,err:=x.Read(buffer);n!=0||err==nil {t.Fatal("Reader supplied untracked input")}
			} else {x.observe(historyTestResponse(1))}
			x.endOutput(true)
			if x.resultReason()=="history_protocol_complete"||x.writtenSteps!=0 {t.Fatal("unsent response completed protocol")}
		})
	}
}

func TestUnslopHistoryRejectsArbitraryWriter(t *testing.T) {
	plan,err:=buildUnslopHistoryProtocol([]unslopHistoryRequest{{"thread/read",map[string]any{"threadId":"synthetic-owned"}}})
	if err!=nil {t.Fatal(err)}
	defer plan.erase()
	x,err:=newUnslopHistoryExchange(context.Background(),plan)
	if err!=nil {t.Fatal(err)}
	defer x.Close()
	var output bytes.Buffer
	if n,err:=x.WriteTo(&output);n!=0||err==nil||output.Len()!=0||x.writesComplete() {t.Fatal("arbitrary writer accepted")}
}

func TestUnslopHistoryPendingReplyCannotHideMiddleOrFinalPipeFailure(t *testing.T) {
	for _, mode := range []string{"middle", "final", "partial-final"} {
		t.Run(mode,func(t *testing.T){
			requests:=[]unslopHistoryRequest{{"thread/read",map[string]any{"threadId":"synthetic-owned","padding":strings.Repeat("x",256<<10)}}}
			if mode=="middle" {requests=append(requests,unslopHistoryRequest{"thread/read",map[string]any{"threadId":"synthetic-owned"}})}
			plan,err:=buildUnslopHistoryProtocol(requests)
			if err!=nil {t.Fatal(err)}
			defer plan.erase()
			x,err:=newUnslopHistoryExchange(context.Background(),plan)
			if err!=nil {t.Fatal(err)}
			defer x.Close()
			o:=historyAttachPipe(t,x)
			historyReadStep(t,x,plan.Steps[0].Input)
			x.observe(historyTestResponse(1))
			historyWaitWritingStep(t,x,1)
			if mode=="partial-final" {
				part:=make([]byte,4096)
				if _,err:=io.ReadFull(o.reader,part);err!=nil {t.Fatal(err)}
				if !bytes.Equal(part,plan.Steps[1].Input[:len(part)]) {t.Fatal("wrong fixed step reached pipe")}
			}
			x.observe(historyTestResponse(2))
			x.endOutput(true)
			if err:=o.reader.Close();err!=nil {t.Fatal(err)}
			if err:=historyWaitWriterStopped(t,o);err==nil {t.Fatal("incomplete pipe write was suppressed")}
			if !x.writeFailed||x.writerFinished||x.writtenSteps!=1||x.resultReason()=="history_protocol_complete" {t.Fatal("pending response replaced failed write",x.resultReason())}
		})
	}
}

func TestUnslopHistoryRealPipeWritesEveryStepBeforeEOF(t *testing.T) {
	ctx,cancel:=context.WithCancel(context.Background());defer cancel()
	plan,x:=historyProtocolFixture(t,ctx,2)
	o:=historyPipeFor(t,x)
	for i,step:=range plan.Steps {
		historyReadStep(t,x,step.Input)
		x.observe(historyTestResponse(i+1))
	}
	var extra [1]byte
	if n,err:=o.reader.Read(extra[:]);n!=0||err!=io.EOF {t.Fatal("final reply did not close stdin",n,err)}
	x.endOutput(true)
	if err:=historyWaitWriterStopped(t,o);err!=nil {t.Fatal(err)}
	if x.resultReason()!="history_protocol_complete"||x.writtenSteps!=len(plan.Steps)||!x.writerFinished {t.Fatal("actual complete write sequence rejected")}
}

type historyCleanupTestCloser struct { calls int; fail bool; order *[]int; id int }
func (c *historyCleanupTestCloser) Close() error {
	c.calls++;*c.order=append(*c.order,c.id)
	if c.fail {return errors.New("private cleanup detail")};return nil
}
func TestUnslopHistoryCleanupFailureErasesAnswersAndClosesEveryResource(t *testing.T) {
	for _,mode:=range []string{"success","launcher","token","both"} {
		t.Run(mode,func(t *testing.T){
			order:=[]int{}
			launcher:=&historyCleanupTestCloser{fail:mode=="launcher"||mode=="both",order:&order,id:1}
			token:=&historyCleanupTestCloser{fail:mode=="token"||mode=="both",order:&order,id:2}
			backing,_:=json.Marshal(map[string]bool{"safe":true})
			answers,err:=finishUnslopHistoryOutcome([]json.RawMessage{backing},nil,unslopHistoryFacts{},[]io.Closer{launcher,token})
			if launcher.calls!=1||token.calls!=1||len(order)!=2||order[0]!=2||order[1]!=1 {t.Fatal("cleanup did not try both in reverse order")}
			if mode=="success" {
				if err!=nil||len(answers)!=1||!bytes.Equal(answers[0],backing) {t.Fatal("cleanup success lost answers")}
			} else {
				if answers!=nil||err==nil||err.Error()!="history_resource_cleanup_failed"||!bytes.Equal(backing,make([]byte,len(backing))) {t.Fatal("cleanup failure returned successful answers or raw detail")}
			}
		})
	}
}

func historyNativeWriteFaultObserved(result askChildResult, root string) bool {
	if !result.Started || !result.Assigned || !result.WaitVerified || !result.Reaped || result.TimedOut || (result.Exit>=6 && result.Exit<=15) || result.Capture==nil || !result.Capture.protocolWriteFailed || result.Capture.protocolWritesComplete || result.Capture.Complete {return false}
	if !bytes.Contains(result.Capture.Stdout,historyTestResponse(1)) || !bytes.Contains(result.Capture.Stdout,historyTestResponse(2)) {return false}
	for _,name:=range []string{"initialized","partial-step","predicted-reply","stdin-closed"} {
		value,err:=os.ReadFile(filepath.Join(root,name))
		if err!=nil || string(value)!="same-owned-child" {return false}
	}
	return true
}

func TestUnslopHistoryNativeClosedStdinCannotAuthorizePredictedReplies(t *testing.T) {
	if os.Getenv("UNSLOP_WRITE_FAILURE_CHILD")=="1" {
		if !askWorkerParent() {os.Exit(6)}
		root:=os.Getenv("UNSLOP_WRITE_FAILURE_ROOT")
		mark:=func(name string){if os.WriteFile(filepath.Join(root,name),[]byte("same-owned-child"),0600)!=nil {os.Exit(7)}}
		input:=bufio.NewReader(os.Stdin)
		line,err:=input.ReadBytes('\n')
		var frame struct { ID int; Method string }
		if err!=nil || json.Unmarshal(line,&frame)!=nil || frame.ID!=1 || frame.Method!="initialize" {os.Exit(10)}
		mark("initialized")
		if _,err=os.Stdout.Write(historyTestResponse(1));err!=nil {os.Exit(12)}
		line,err=input.ReadBytes('\n')
		frame=struct { ID int; Method string }{}
		if err!=nil || json.Unmarshal(line,&frame)!=nil || frame.Method!="initialized" || frame.ID!=0 {os.Exit(11)}
		if first,err:=input.ReadByte();err!=nil || first!='{' {os.Exit(11)}
		mark("partial-step")
		if _,err:=os.Stdout.Write(historyTestResponse(2));err!=nil {os.Exit(12)}
		mark("predicted-reply")
		process,err:=syscall.GetCurrentProcess()
		if err!=nil {os.Exit(14)}
		var retained syscall.Handle
		if err=syscall.DuplicateHandle(process,syscall.Handle(os.Stdin.Fd()),process,&retained,0,false,syscall.DUPLICATE_SAME_ACCESS);err!=nil {os.Exit(15)}
		// Keep a second read handle until the successful stdin-close witness
		// is saved. This prevents the supervisor killing the child first.
		if err=os.Stdin.Close();err!=nil {_ = syscall.CloseHandle(retained);os.Exit(13)}
		mark("stdin-closed")
		if err=syscall.CloseHandle(retained);err!=nil {os.Exit(13)}
		os.Exit(0)
	}
	for _,mode:=range []string{"middle","final"} {t.Run(mode,func(t *testing.T){
		ctx,cancel:=context.WithTimeout(context.Background(),5*time.Second)
		owner:=historyNewChildTestOwner(t,cancel)
		owner.ctx=ctx
		var err error
		owner.root,err=os.MkdirTemp("","unslop-write-fault-")
		if err!=nil {t.Fatal(err)}
		owner.cleanupRoot=owner.root
		ctx=context.WithValue(ctx,askChildEnvironment{},map[string]string{"UNSLOP_WRITE_FAILURE_CHILD":"1","UNSLOP_WRITE_FAILURE_ROOT":owner.root})
		requests:=[]unslopHistoryRequest{{"thread/read",map[string]any{"threadId":"synthetic-owned","padding":strings.Repeat("x",256<<10)}}}
		if mode=="middle" {requests=append(requests,unslopHistoryRequest{"thread/read",map[string]any{"threadId":"synthetic-owned"}})}
		plan,err:=buildUnslopHistoryProtocol(requests)
		if err!=nil {t.Fatal(err)}
		owner.plan=plan
		ctx=context.WithValue(ctx,askChildCaptureKey{},askChildCapture{})
		ctx=context.WithValue(ctx,unslopHistoryProtocolKey{},plan)
		owner.launched=true
		go func(){owner.done<-askOwnedChild(ctx,os.Args[0],"-test.run=^TestUnslopHistoryNativeClosedStdinCannotAuthorizePredictedReplies$")}()
		if !owner.receive(8*time.Second) {t.Fatal("same native child was not collected")}
		if !historyNativeWriteFaultObserved(owner.result,owner.root) {t.Fatal("target write-failure boundary was not established")}
		if unslopHistoryChildComplete(owner.result) {t.Fatal("closed stdin predictions became successful history")}
	})}
}


func TestUnslopHistoryFastFinalReplyWaitsForSuccessfulPipeWrite(t *testing.T) {
	plan,err:=buildUnslopHistoryProtocol([]unslopHistoryRequest{{"thread/read",map[string]any{"threadId":"synthetic-owned","padding":strings.Repeat("x",256<<10)}}})
	if err!=nil {t.Fatal(err)}
	defer plan.erase()
	x,err:=newUnslopHistoryExchange(context.Background(),plan)
	if err!=nil {t.Fatal(err)}
	defer x.Close()
	o:=historyAttachPipe(t,x)
	historyReadStep(t,x,plan.Steps[0].Input)
	x.observe(historyTestResponse(1))
	historyWaitWritingStep(t,x,1)
	x.observe(historyTestResponse(2))
	x.endOutput(true)
	if x.resultReason()=="history_protocol_complete" {t.Fatal("pending write counted as complete")}
	payload:=make([]byte,len(plan.Steps[1].Input))
	if _,err:=io.ReadFull(o.reader,payload);err!=nil||!bytes.Equal(payload,plan.Steps[1].Input) {t.Fatal("fixed final request was not actually written",err)}
	if err:=historyWaitWriterStopped(t,o);err!=nil {t.Fatal(err)}
	if x.resultReason()!="history_protocol_complete"||!x.writesComplete() {t.Fatal("successful pending write did not release completion")}
}

func TestUnslopHistoryCleanupKeepsPriorFailureWithoutSkippingClose(t *testing.T) {
	order:=[]int{}
	prior:=errors.New("history_process_collection_unavailable")
	launcher:=&historyCleanupTestCloser{order:&order,id:1}
	token:=&historyCleanupTestCloser{order:&order,id:2}
	answers,err:=finishUnslopHistoryOutcome(nil,prior,unslopHistoryFacts{},[]io.Closer{launcher,token})
	if answers!=nil||err==nil||err.Error()!=prior.Error()||launcher.calls!=1||token.calls!=1 {t.Fatal("prior failure skipped cleanup or was replaced")}
}

func TestUnslopHistoryConcurrentDuplicateWriterCannotAdvanceInput(t *testing.T) {
	ctx,cancel:=context.WithCancel(context.Background());defer cancel()
	plan,x:=historyProtocolFixture(t,ctx,1)
	historyReadStep(t,x,plan.Steps[0].Input)
	read,write,err:=os.Pipe()
	if err!=nil {t.Fatal(err)}
	defer read.Close();defer write.Close()
	if n,err:=x.WriteTo(write);n!=0||err==nil {t.Fatal("second pipe writer accepted")}
	if x.resultReason()!="history_protocol_stdin_writer_invalid"||x.writesComplete() {t.Fatal("duplicate writer changed authority")}
}

func TestUnslopHistoryCompletedProtocolExitThreeIsNotIOFailure(t *testing.T) {
	owner:=historyStartNativeTestChild(t,"complete-exit3")
	if !owner.receive(8*time.Second) {t.Fatal("same child result not collected")}
	result:=owner.result
	if !result.Started||!result.WaitVerified||!result.Reaped||result.Exit!=3||result.Capture==nil {t.Fatal("exit-three target not reached")}
	if result.Capture.protocolReason!="history_protocol_complete"||!result.Capture.protocolWritesComplete||result.Capture.protocolIOFailed||result.Capture.Complete==false {t.Fatal("native exit misclassified as protocol I/O")}
	facts:=unslopHistoryFacts{};observeUnslopHistoryChild(&facts,result)
	_,err:=finishUnslopHistoryOutcome(nil,errors.New("history_process_collection_unavailable"),facts,nil)
	inspection:=unslopOriginInspection{};projectUnslopHistoryFailure(&inspection,err)
	if inspection.Reason!="history_child_exit_failed"||inspection.History==nil||inspection.History.IOFailed||*inspection.History.ExitCode!=3 {t.Fatal("exit fact lost in normal inspection")}
}
