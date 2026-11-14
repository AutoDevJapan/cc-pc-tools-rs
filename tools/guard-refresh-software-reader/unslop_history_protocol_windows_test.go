//go:build windows

package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func historyProtocolFixture(t *testing.T, ctx context.Context, count int) (unslopHistoryProtocol, *unslopHistoryExchange) {
	t.Helper()
	requests := make([]unslopHistoryRequest, count)
	for i := range requests { requests[i] = unslopHistoryRequest{"thread/read", map[string]any{"threadId":"synthetic-owned", "includeTurns":false}} }
	plan, err := buildUnslopHistoryProtocol(requests)
	if err != nil { t.Fatal(err) }
	t.Cleanup(plan.erase)
	x, err := newUnslopHistoryExchange(ctx, plan)
	if err != nil { t.Fatal(err) }
	t.Cleanup(func(){ _ = x.Close() })
	return plan, x
}

func historyReadStep(t *testing.T, x *unslopHistoryExchange, want []byte) {
	t.Helper()
	actual := make([]byte, len(want))
	if _, err := io.ReadFull(x, actual); err != nil || !bytes.Equal(actual, want) { t.Fatal("fixed step was not delivered exactly", err) }
	clear(actual)
}


type historyReadTestResult struct { data []byte; err error }

type historyReadTestOwner struct {
	cancel context.CancelFunc
	done chan historyReadTestResult
	result historyReadTestResult
	received bool
}

func (o *historyReadTestOwner) receive(limit time.Duration) bool {
	if o.received { return true }
	timer:=time.NewTimer(limit);defer timer.Stop()
	select {case o.result=<-o.done:o.received=true;return true;case <-timer.C:return false}
}

// The cleanup is registered before the goroutine starts. A failed assertion
// cannot abandon its result or make a canceled read count as a failure signal.
func historyStartBlockedRead(t *testing.T, x *unslopHistoryExchange, cancel context.CancelFunc, step int) *historyReadTestOwner {
	t.Helper()
	o:=&historyReadTestOwner{cancel:cancel,done:make(chan historyReadTestResult,1)}
	t.Cleanup(func(){
		o.cancel()
		if !o.receive(3*time.Second) {t.Error("history read cleanup unknown: result was not received");return}
		clear(o.result.data)
	})
	go func(){buffer:=make([]byte,1<<20);n,err:=x.Read(buffer);o.done<-historyReadTestResult{buffer[:n],err}}()
	timer:=time.NewTimer(time.Second);defer timer.Stop()
	select {
	case waiting:=<-x.waiting:if waiting!=step {t.Fatal("reader witness belongs to a different step",waiting,step)}
	case o.result=<-o.done:o.received=true;t.Fatal("reader returned before the failure boundary",o.result.err)
	case <-timer.C:t.Fatal("reader did not reach the response boundary")
	}
	return o
}

func historyRequireFailureWake(t *testing.T, o *historyReadTestOwner) {
	t.Helper()
	// The caller has not canceled, closed stdout, or supplied another response.
	// Removing failLocked's signal must fail here, before cleanup can cancel.
	if !o.receive(time.Second) {t.Fatal("failure did not wake the already blocked reader")}
	if len(o.result.data)!=0||o.result.err!=errUnslopHistoryProtocol {t.Fatal("failure woke the reader with input or the wrong error",o.result.err)}
}

type historyChildTestOwner struct {
	ctx context.Context
	cancel context.CancelFunc
	done chan askChildResult
	result askChildResult
	plan unslopHistoryProtocol
	root string
	launched,received,retained bool
}

// Unknown cleanup keeps the exact test owner, result channel and private fixture.
// It never means an actual native process or Job was observed as collected.
var historyChildTestUncollected=struct{sync.Mutex;owners []*historyChildTestOwner}{}

func (o *historyChildTestOwner) receive(limit time.Duration) bool {
	if o.received {return true}
	timer:=time.NewTimer(limit);defer timer.Stop()
	select {case o.result=<-o.done:o.received=true;return true;case <-timer.C:return false}
}

func (o *historyChildTestOwner) retain() {
	if o.retained {return}
	o.retained=true
	historyChildTestUncollected.Lock();defer historyChildTestUncollected.Unlock()
	historyChildTestUncollected.owners=append(historyChildTestUncollected.owners,o)
}

func historyNewChildTestOwner(t *testing.T, cancel context.CancelFunc) *historyChildTestOwner {
	t.Helper()
	o:=&historyChildTestOwner{cancel:cancel,done:make(chan askChildResult,1)}
	// Register before fixture acquisition or launch. This is the only result
	// receiver and remains responsible after Fatal, setup errors and timeouts.
	t.Cleanup(func(){
		o.cancel()
		if o.launched&&!o.receive(3*time.Second) {
			o.retain();t.Error("history child cleanup unknown: same result not received; owner and fixture retained");return
		}
		if o.received {
			r:=&o.result
			clear(r.Raw)
			if r.Capture!=nil {clear(r.Capture.Stdout)}
			if r.Started&&(!r.WaitVerified||!r.Reaped) {
				o.retain();t.Error("history child cleanup incomplete; fixture retained",r.Started,r.Assigned,r.WaitVerified,r.Reaped,r.TimedOut,r.CollectionReason,r.CollectionObserved,r.CollectionTotal,r.CollectionTotalKnown);return
			}
		}
		o.plan.erase()
		if o.root!="" {if err:=os.RemoveAll(o.root);err!=nil {o.retain();t.Error("history test fixture cleanup failed; owner retained")}}
	})
	return o
}

func historyStartNativeTestChild(t *testing.T, mode string) *historyChildTestOwner {
	t.Helper()
	ctx,cancel:=context.WithTimeout(context.Background(),5*time.Second)
	o:=historyNewChildTestOwner(t,cancel)
	o.ctx=ctx
	var err error
	o.root,err=os.MkdirTemp("","unslop-history-")
	if err!=nil {t.Fatal("history private fixture unavailable",err)}
	ctx=context.WithValue(ctx,askChildEnvironment{},map[string]string{"UNSLOP_PROTOCOL_CHILD":mode,"UNSLOP_PROTOCOL_ROOT":o.root})
	o.plan,err=buildUnslopHistoryProtocol([]unslopHistoryRequest{{"thread/read",map[string]any{"threadId":"synthetic-owned"}},{"thread/read",map[string]any{"threadId":"synthetic-owned"}}})
	if err!=nil {t.Fatal(err)}
	ctx=context.WithValue(ctx,askChildCaptureKey{},askChildCapture{})
	ctx=context.WithValue(ctx,unslopHistoryProtocolKey{},o.plan)
	o.launched=true
	go func(){o.done<-askOwnedChild(ctx,os.Args[0],"-test.run=^TestUnslopHistoryProtocolChild$")}()
	return o
}

func historyWaitNativeBoundary(o *historyChildTestOwner, name string) bool {
	deadline:=time.NewTimer(3*time.Second);ticker:=time.NewTicker(time.Millisecond)
	defer deadline.Stop();defer ticker.Stop()
	for {select {
	case <-ticker.C:if _,err:=os.Stat(filepath.Join(o.root,name));err==nil {return true}
	case <-deadline.C:return false
	}}
}

func TestUnslopHistoryExchangeWaitsForActualResponseAndFinalEOF(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	plan, x := historyProtocolFixture(t, ctx, 2)
	historyReadStep(t, x, plan.Steps[0].Input)
	for i := range plan.Steps {
		o:=historyStartBlockedRead(t,x,cancel,i)
		frame := []byte("{\"id\":"+strconv.Itoa(i+1)+",\"result\":{}}\n")
		x.observe(frame[:len(frame)-1])
		select {case o.result=<-o.done:o.received=true;t.Fatal("partial JSONL frame advanced input",o.result.err);default:}
		x.observe(frame[len(frame)-1:])
		if !o.receive(time.Second) {t.Fatal("matching response did not wake the reader")}
		if i+1==len(plan.Steps) {
			if o.result.err!=io.EOF||len(o.result.data)!=0 {t.Fatal("final response did not close stdin")}
		} else if o.result.err!=nil||!bytes.Equal(o.result.data,plan.Steps[i+1].Input) {t.Fatal("matching response did not release the exact next input",o.result.err)}
		clear(o.result.data)
	}
	x.endOutput(true)
	if reason:=x.resultReason();reason!="history_protocol_complete" {t.Fatal("complete sequence was rejected",reason)}
}

func TestUnslopHistoryExchangeRejectsInvalidFramesAndEndsBlockedReads(t *testing.T) {
	cases:=[]struct{name,wire string;end bool}{
		{"wrong-id", "{\"id\":9,\"result\":{}}\n",false},
		{"duplicate-field", "{\"id\":9,\"id\":1,\"result\":{}}\n",false},
		{"case-alias", "{\"ID\":1,\"result\":{}}\n",false},
		{"notification-with-result", "{\"method\":\"warning\",\"result\":{}}\n",false},
		{"server-request", "{\"id\":1,\"method\":\"item/requestApproval\",\"params\":{}}\n",false},
		{"error", "{\"id\":1,\"error\":{\"code\":9,\"message\":\"private-token\"}}\n",false},
		{"null", "{\"id\":1,\"result\":null}\n",false},
		{"non-object", "{\"id\":1,\"result\":true}\n",false},
		{"extra-member", "{\"id\":1,\"result\":{},\"unknown\":\"private-token\"}\n",false},
		{"partial-eof", "{\"id\":1,\"result\":{}",true},
		{"empty-eof", "",true},
	}
	for _,c:=range cases { t.Run(c.name,func(t *testing.T){
		ctx,cancel:=context.WithCancel(context.Background());defer cancel()
		plan,x:=historyProtocolFixture(t,ctx,1)
		historyReadStep(t,x,plan.Steps[0].Input)
		o:=historyStartBlockedRead(t,x,cancel,0)
		x.observe([]byte(c.wire));if c.end {x.endOutput(true)}
		historyRequireFailureWake(t,o)
		if reason:=x.resultReason();!strings.HasPrefix(reason,"history_protocol_")||strings.Contains(reason,"private")||strings.Contains(reason,"token") { t.Fatal("raw response reached reason",reason) }
	}) }
}

func TestUnslopHistoryExchangeChunkingLimitsAndDuplicateResponse(t *testing.T) {
	wire:=[]byte("{\"id\":1,\"result\":{\"text\":\"日本語\\n引用\"}}\r\n")
	for split:=0;split<=len(wire);split++ {
		ctx,cancel:=context.WithCancel(context.Background())
		plan,x:=historyProtocolFixture(t,ctx,1)
		historyReadStep(t,x,plan.Steps[0].Input)
		x.observe(wire[:split]);x.observe(wire[split:])
		historyReadStep(t,x,plan.Steps[1].Input)
		x.observe([]byte("{\"id\":2,\"result\":{}}\n"));x.endOutput(true)
		if x.resultReason()!="history_protocol_complete" { t.Fatal("JSONL chunk boundary changed the result",split,x.resultReason()) }
		cancel()
	}
	for _,mode:=range []string{"duplicate-response","frame-limit","total-limit","stdout-error","close","cancel"} {t.Run(mode,func(t *testing.T){
		ctx,cancel:=context.WithCancel(context.Background());defer cancel()
		plan,x:=historyProtocolFixture(t,ctx,1)
		historyReadStep(t,x,plan.Steps[0].Input)
		o:=historyStartBlockedRead(t,x,cancel,0)
		switch mode {
		case "duplicate-response": x.observe([]byte("{\"id\":1,\"result\":{}}\n{\"id\":1,\"result\":{}}\n"))
		case "frame-limit": x.observe(bytes.Repeat([]byte{'x'},unslopHistoryFrameLimit+1))
		case "total-limit": x.observe(make([]byte,unslopHistoryOutputLimit+1))
		case "stdout-error": x.endOutput(false)
		case "close": _=x.Close()
		case "cancel": cancel()
		}
		if mode=="cancel" {
			if !o.receive(time.Second)||o.result.err!=errUnslopHistoryProtocol||len(o.result.data)!=0 {t.Fatal("cancel did not terminate the blocked reader")}
		} else {historyRequireFailureWake(t,o)}
	})}
}

func TestUnslopHistoryProtocolRejectsMutatedOrUnboundedPlans(t *testing.T) {
	for _,mode:=range []string{"method","response-id","initialize","initialized","oversize","no-requests"} {t.Run(mode,func(t *testing.T){
		plan,err:=buildUnslopHistoryProtocol([]unslopHistoryRequest{{"thread/read",map[string]any{"threadId":"synthetic-owned"}}});if err!=nil {t.Fatal(err)}
		defer plan.erase()
		switch mode {
		case "method": plan.Steps[1].Input=bytes.ReplaceAll(plan.Steps[1].Input,[]byte("thread/read"),[]byte("turn/start"))
		case "response-id": plan.Steps[1].ResponseID=9
		case "initialize": plan.Steps[0].Input=[]byte("arbitrary-stdin\n")
		case "initialized": plan.Steps[1].Input=bytes.ReplaceAll(plan.Steps[1].Input,[]byte("initialized"),[]byte("turn/start"))
		case "oversize": plan.Steps[0].Input=make([]byte,(1<<20)+1)
		case "no-requests": plan.Steps=plan.Steps[:1]
		}
		x,err:=newUnslopHistoryExchange(context.Background(),plan)
		if x!=nil||err==nil { if x!=nil {_=x.Close()}; t.Fatal("caller values became an executable history plan",mode) }
	})}
}

func TestUnslopHistoryProtocolChild(t *testing.T) {
	mode:=os.Getenv("UNSLOP_PROTOCOL_CHILD")
	if mode=="" {return}
	if !askWorkerParent() {os.Exit(6)}
	root:=os.Getenv("UNSLOP_PROTOCOL_ROOT")
	input:=bufio.NewReader(os.Stdin)
	read:=func() map[string]json.RawMessage {
		line,err:=input.ReadBytes('\n');if err!=nil {os.Exit(10)}
		var frame map[string]json.RawMessage
		if json.Unmarshal(line,&frame)!=nil {os.Exit(11)}
		clear(line);return frame
	}
	mark:=func(name string){if os.WriteFile(filepath.Join(root,name),[]byte("synthetic"),0600)!=nil {os.Exit(12)}}
	method:=func(frame map[string]json.RawMessage)string{var value string;_ = json.Unmarshal(frame["method"],&value);return value}
	first:=read()
	if method(first)!="initialize"||string(first["id"])!="1" {os.Exit(13)}
	if input.Buffered()!=0 {mark("unexpected-input");os.Exit(18)}
	mark("initialize-received")
	if mode=="early-exit" {os.Exit(3)}
	if mode=="timeout"||mode=="cancel" {time.Sleep(10*time.Second);os.Exit(0)}
	if mode=="delayed" {
		for {if _,err:=os.Stat(filepath.Join(root,"release"));err==nil {break};time.Sleep(time.Millisecond)}
	}
	if mode=="partial" {_,_=os.Stdout.Write([]byte("{\"id\":1,\"result\":{}"));os.Exit(0)}
	if mode=="held-output" {
		child:=exec.Command(os.Args[0],"-test.run=^TestUnslopHistoryProtocolHeldOutputChild$")
		child.Env=append(os.Environ(),"UNSLOP_PROTOCOL_HELD_OUTPUT=1")
		child.Stdout,child.Stderr=os.Stdout,os.Stderr
		if child.Start()!=nil {os.Exit(14)}
		_,_=os.Stdout.Write([]byte("{\"id\":1,\"result\":{}"));os.Exit(3)
	}
	wire:="{\"id\":1,\"result\":{}}\n"
	switch mode {
	case "wrong-id":wire="{\"id\":8,\"result\":{}}\n"
	case "error":wire="{\"id\":1,\"error\":{\"message\":\"private-body\"}}\n"
	case "notification":wire="{\"method\":\"warning\",\"params\":{\"message\":\"private-body\"}}\n"+wire
	case "duplicate":wire+=wire
	case "overflow":wire=strings.Repeat("x",unslopHistoryFrameLimit+1)+"\n"
	}
	_,_=io.WriteString(os.Stdout,wire)
	if mode=="wrong-id"||mode=="error"||mode=="duplicate"||mode=="overflow" {
		if _,err:=input.ReadByte();err!=io.EOF {mark("unexpected-input")}
		os.Exit(0)
	}
	if notification:=read();method(notification)!="initialized"||notification["id"]!=nil {os.Exit(15)}
	for id:=2;id<=3;id++ {
		request:=read()
		if method(request)!="thread/read"||string(request["id"])!=strconv.Itoa(id) {os.Exit(16)}
		mark("request-"+strconv.Itoa(id))
		if mode=="request-early-exit" {os.Exit(3)}
		if mode=="request-timeout"||mode=="request-cancel" {time.Sleep(10*time.Second);os.Exit(0)}
		if mode=="request-error" {_,_=io.WriteString(os.Stdout,"{\"id\":"+strconv.Itoa(id)+",\"error\":{\"code\":1}}\n");if _,err:=input.ReadByte();err!=io.EOF {mark("unexpected-input")};os.Exit(0)}
		if mode=="request-wrong-id" {_,_=io.WriteString(os.Stdout,"{\"id\":9,\"result\":{}}\n");if _,err:=input.ReadByte();err!=io.EOF {mark("unexpected-input")};os.Exit(0)}
		_,_=io.WriteString(os.Stdout,"{\"id\":"+strconv.Itoa(id)+",\"result\":{\"thread\":{\"id\":\"synthetic-owned\"}}}\n")
	}
	if _,err:=input.ReadByte();err!=io.EOF {mark("unexpected-input");os.Exit(17)}
	mark("stdin-eof")
	os.Exit(0)
}

func TestUnslopHistoryProtocolHeldOutputChild(t *testing.T) {
	if os.Getenv("UNSLOP_PROTOCOL_HELD_OUTPUT")!="1" {return}
	time.Sleep(10*time.Second)
	_,_=io.WriteString(os.Stdout,"late-output")
	os.Exit(0)
}

func TestUnslopHistoryProtocolNativeChildConsumesFixedStepsAndCollects(t *testing.T) {
	for _,mode:=range []string{"normal","delayed","early-exit","partial","wrong-id","duplicate","notification","error","request-error","request-wrong-id","request-early-exit","request-timeout","request-cancel","overflow","held-output","timeout","cancel"} {t.Run(mode,func(t *testing.T){
		o:=historyStartNativeTestChild(t,mode)
		boundary:="initialize-received"
		if mode=="request-cancel"||mode=="request-timeout"||mode=="request-early-exit" {boundary="request-2"}
		ready:=historyWaitNativeBoundary(o,boundary)
		if mode=="delayed" {if err:=os.WriteFile(filepath.Join(o.root,"release"),[]byte("synthetic"),0600);err!=nil {t.Fatal(err)}}
		if mode=="cancel"||mode=="request-cancel"||!ready {o.cancel()}
		if !o.receive(8*time.Second) {t.Fatal("same child copy/Wait/collection did not terminate")}
		r:=&o.result
		if !ready||!r.Started||!r.Assigned||!r.WaitVerified||!r.Reaped||r.Capture==nil||r.Capture.NativeProcess==nil {t.Fatal("original native child was not collected",r.CollectionReason)}
		if len(r.Raw)!=0||strings.Contains(r.Capture.protocolReason,"private") {t.Fatal("raw response reached ordinary output")}
		valid:=mode=="normal"||mode=="delayed"||mode=="notification"
		if valid {
			if !r.Capture.Complete||r.Capture.protocolReason!="history_protocol_complete"||r.Exit!=0||r.TimedOut {t.Fatal("valid fixed step exchange failed",r.Capture.protocolReason,r.Exit)}
			for _,name:=range []string{"request-2","request-3","stdin-eof"} {if _,err:=os.Stat(filepath.Join(o.root,name));err!=nil {t.Fatal("fixed request/final EOF was not consumed",name)}}
			answers,err:=parseUnslopHistoryBatch(r.Capture.Stdout,2);if err!=nil||len(answers)!=2 {t.Fatal("same child responses were not usable",err)}
			for _,answer:=range answers {clear(answer)}
		} else if r.Capture.Complete||r.Capture.protocolReason=="history_protocol_complete" {t.Fatal("invalid exchange became a successful capture",mode)}
		if mode=="cancel"||mode=="timeout"||mode=="request-cancel"||mode=="request-timeout" {if !r.TimedOut {t.Fatal("deadline/cancel was not preserved")}}
		if mode!="duplicate" {if _,err:=os.Stat(filepath.Join(o.root,"unexpected-input"));err==nil {t.Fatal("invalid response supplied a later request")}}
	})}
}

// This subprocess deliberately calls Fatal after a receive deadline. Its marker
// is written only after the registered owner has consumed and erased the result.
func TestUnslopHistoryNativeFatalCleanupChild(t *testing.T) {
	if os.Getenv("UNSLOP_HISTORY_FATAL_CLEANUP_CHILD")!="1" {return}
	if !askWorkerParent() {os.Exit(6)}
	outerRoot:=os.Getenv("UNSLOP_HISTORY_FATAL_CLEANUP_ROOT")
	var owner *historyChildTestOwner
	t.Cleanup(func(){
		if owner==nil||!owner.received||owner.retained||owner.ctx.Err()==nil {return}
		r:=&owner.result
		if !r.Started||!r.Assigned||!r.WaitVerified||!r.Reaped||!r.TimedOut||r.Capture==nil||r.Capture.NativeProcess==nil||r.Capture.Complete {return}
		for _,b:=range r.Raw {if b!=0 {return}}
		for _,b:=range r.Capture.Stdout {if b!=0 {return}}
		if _,err:=os.Stat(owner.root);!os.IsNotExist(err) {return}
		_ = os.WriteFile(filepath.Join(outerRoot,"fatal-cleanup-collected"),[]byte("same-result-collected"),0600)
	})
	owner=historyStartNativeTestChild(t,"request-cancel")
	if !historyWaitNativeBoundary(owner,"request-2") {t.Fatal("child did not reach delayed response boundary")}
	if owner.receive(time.Millisecond) {t.Fatal("delayed native child completed before cancel")}
	if owner.ctx.Err()!=nil||owner.received {t.Fatal("receive deadline was replaced by context cancellation")}
	t.Fatal("synthetic receive deadline: exercise registered cleanup")
}

func TestUnslopHistoryNativeReceiveDeadlineCollectsOriginalChild(t *testing.T) {
	ctx,cancel:=context.WithTimeout(context.Background(),5*time.Second)
	o:=historyNewChildTestOwner(t,cancel)
	o.ctx=ctx
	var err error
	o.root,err=os.MkdirTemp("","unslop-history-fatal-")
	if err!=nil {t.Fatal(err)}
	ctx=context.WithValue(ctx,askChildEnvironment{},map[string]string{"UNSLOP_HISTORY_FATAL_CLEANUP_CHILD":"1","UNSLOP_HISTORY_FATAL_CLEANUP_ROOT":o.root})
	// Plain finite capture observes the expected testing failure, rather than
	// parsing testing's output as a history response. It uses the same Job owner.
	ctx=context.WithValue(ctx,askChildCaptureKey{},askChildCapture{})
	o.launched=true
	go func(){o.done<-askOwnedChild(ctx,os.Args[0],"-test.run=^TestUnslopHistoryNativeFatalCleanupChild$")}()
	if !o.receive(8*time.Second) {t.Fatal("Fatal cleanup subprocess did not finish")}
	r:=&o.result
	if !r.Started||!r.Assigned||!r.WaitVerified||!r.Reaped||r.TimedOut||r.Exit!=1||r.Capture==nil||!r.Capture.Complete {t.Fatal("expected test failure was not collected",r.CollectionReason)}
	if !bytes.Contains(r.Capture.Stdout,[]byte("synthetic receive deadline: exercise registered cleanup")) {t.Fatal("test did not reach the intended Fatal branch")}
	marker,err:=os.ReadFile(filepath.Join(o.root,"fatal-cleanup-collected"))
	if err!=nil||string(marker)!="same-result-collected" {t.Fatal("Fatal cleanup dropped the original result or buffers")}
}

func TestUnslopHistoryExchangeCopiesPlanAndRejectsMixedCaptureBeforeSpawn(t *testing.T) {
	plan,err:=buildUnslopHistoryProtocol([]unslopHistoryRequest{{"thread/read",map[string]any{"threadId":"synthetic-owned","limit":json.Number("9007199254740993")}}})
	if err!=nil {t.Fatal(err)}
	defer plan.erase()
	x,err:=newUnslopHistoryExchange(context.Background(),plan)
	if err!=nil {t.Fatal("canonical numeric request was not preserved",err)}
	defer x.Close()
	original:=append([]byte(nil),plan.Steps[0].Input...)
	defer clear(original)
	clear(plan.Steps[0].Input)
	historyReadStep(t,x,original)
	for _,mode:=range []string{"mixed-stdin","no-capture","wrong-context-type"} {t.Run(mode,func(t *testing.T){
		valid,err:=buildUnslopHistoryProtocol([]unslopHistoryRequest{{"thread/read",map[string]any{"threadId":"synthetic-owned"}}});if err!=nil {t.Fatal(err)}
		defer valid.erase()
		ctx:=context.WithValue(context.Background(),unslopHistoryProtocolKey{},valid)
		if mode=="wrong-context-type" {ctx=context.WithValue(ctx,unslopHistoryProtocolKey{},"caller-supplied")}
		if mode!="no-capture" {
			capture:=askChildCapture{}
			if mode=="mixed-stdin" {capture.Stdin=[]byte("free-stdin")}
			ctx=context.WithValue(ctx,askChildCaptureKey{},capture)
		}
		r:=askOwnedChild(ctx,os.Args[0],"-test.run=^TestUnslopHistoryProtocolChild$")
		if r.Capture!=nil {clear(r.Capture.Stdout)}
		if r.Started||r.Assigned||r.WaitVerified||r.Reaped {t.Fatal("invalid history context started a child",mode)}
	})}
}

func TestUnslopHistoryNotificationsNeverAdvanceStepsAndRemainBounded(t *testing.T) {
	for _,method:=range []string{"configWarning","warning","account/updated","account/rateLimits/updated","future/serverNotification"} {t.Run(method,func(t *testing.T){
		ctx,cancel:=context.WithCancel(context.Background());defer cancel()
		plan,x:=historyProtocolFixture(t,ctx,1)
		wire:=[]byte("{\"method\":\""+method+"\",\"params\":{\"message\":\"private-notification\"}}\n")
		// Startup notification before stdin is consumed is not a response.
		x.observe(wire)
		historyReadStep(t,x,plan.Steps[0].Input)
		for n:=1;n<32;n++ {x.observe(wire)}
		if x.step!=0||!x.awaiting||x.notifications!=32||x.reason!="" {t.Fatal("notification advanced request state or failed inside its bound")}
		x.observe([]byte("{\"id\":1,\"result\":{}}\n"))
		historyReadStep(t,x,plan.Steps[1].Input)
		x.observe([]byte("{\"id\":2,\"result\":{}}\n"));x.endOutput(true)
		if x.resultReason()!="history_protocol_complete" {t.Fatal("bounded notifications prevented matching response",x.resultReason())}
	})}
	ctx,cancel:=context.WithCancel(context.Background());defer cancel()
	plan,x:=historyProtocolFixture(t,ctx,1)
	historyReadStep(t,x,plan.Steps[0].Input)
	o:=historyStartBlockedRead(t,x,cancel,0)
	for n:=0;n<33;n++ {x.observe([]byte("{\"method\":\"warning\",\"params\":{}}\n"))}
	if x.resultReason()!="history_protocol_notification_limit" {t.Fatal("notification flood did not terminate at its fixed bound")}
	historyRequireFailureWake(t,o)
}

func TestUnslopHistoryNotificationPayloadIsNotAnAuthorityOrResponse(t *testing.T) {
	for _,params:=range []string{"null","[]","{}","\"opaque\""} {
		raw:=[]byte("{\"method\":\"future/serverNotification\",\"params\":"+params+"}")
		result,notification,err:=classifyUnslopHistoryFrame(raw,1)
		if err!=nil||!notification||result!=nil {clear(result);t.Fatal("notification payload was mistaken for a response")}
	}
}
