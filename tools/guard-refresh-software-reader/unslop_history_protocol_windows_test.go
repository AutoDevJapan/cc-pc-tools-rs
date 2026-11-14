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

func TestUnslopHistoryExchangeWaitsForActualResponseAndFinalEOF(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	plan, x := historyProtocolFixture(t, ctx, 2)
	historyReadStep(t, x, plan.Steps[0].Input)
	for i := range plan.Steps {
		type readResult struct{ data []byte; err error }
		done := make(chan readResult, 1)
		go func(){ buffer:=make([]byte,1<<20); n,err:=x.Read(buffer); done<-readResult{buffer[:n],err} }()
		select { case waiting:=<-x.waiting: if waiting!=i {cancel();r:=<-done;clear(r.data);t.Fatal("reader witness belongs to a different step",waiting,i)}; case <-time.After(time.Second): cancel();r:=<-done;clear(r.data); t.Fatal("reader did not reach the response boundary") }
		select { case r:=<-done: clear(r.data); t.Fatal("reader advanced or returned EOF before matching response",r.err); default: }
		frame := []byte("{\"id\":"+strconv.Itoa(i+1)+",\"result\":{}}\n")
		x.observe(frame[:len(frame)-1])
		select { case r:=<-done: clear(r.data); t.Fatal("partial JSONL frame advanced input",r.err); default: }
		x.observe(frame[len(frame)-1:])
		select {
		case r:=<-done:
			if i+1 == len(plan.Steps) {
				if r.err != io.EOF || len(r.data)!=0 { clear(r.data); t.Fatal("final response did not close stdin") }
			} else if r.err != nil || !bytes.Equal(r.data, plan.Steps[i+1].Input) {
				clear(r.data); t.Fatal("matching response did not release the exact next input",r.err)
			}
			clear(r.data)
		case <-time.After(time.Second): cancel();r:=<-done;clear(r.data); t.Fatal("matching response did not wake the reader")
		}
	}
	x.endOutput(true)
	if reason:=x.resultReason(); reason!="history_protocol_complete" { t.Fatal("complete sequence was rejected",reason) }
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
		x.observe([]byte(c.wire));if c.end {x.endOutput(true)}
		var p [16]byte
		if n,err:=x.Read(p[:]);n!=0||err==nil||err==io.EOF { t.Fatal("invalid response supplied next request",n,err) }
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
		switch mode {
		case "duplicate-response": x.observe([]byte("{\"id\":1,\"result\":{}}\n{\"id\":1,\"result\":{}}\n"))
		case "frame-limit": x.observe(bytes.Repeat([]byte{'x'},unslopHistoryFrameLimit+1))
		case "total-limit": x.observe(make([]byte,unslopHistoryOutputLimit+1))
		case "stdout-error": x.endOutput(false)
		case "close": _=x.Close()
		case "cancel": cancel()
		}
		var p [16]byte
		if n,err:=x.Read(p[:]);n!=0||err==nil||err==io.EOF { t.Fatal("failure did not terminate stdin reader",n,err) }
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
		root:=physicalSoftwareFixture(t)
		t.Setenv("UNSLOP_PROTOCOL_CHILD",mode);t.Setenv("UNSLOP_PROTOCOL_ROOT",root)
		ctx,cancel:=context.WithTimeout(context.Background(),5*time.Second)
		defer cancel()
		plan,err:=buildUnslopHistoryProtocol([]unslopHistoryRequest{{"thread/read",map[string]any{"threadId":"synthetic-owned"}},{"thread/read",map[string]any{"threadId":"synthetic-owned"}}})
		if err!=nil {t.Fatal(err)}
		defer plan.erase()
		ctx=context.WithValue(ctx,askChildCaptureKey{},askChildCapture{})
		ctx=context.WithValue(ctx,unslopHistoryProtocolKey{},plan)
		done:=make(chan askChildResult,1)
		go func(){done<-askOwnedChild(ctx,os.Args[0],"-test.run=^TestUnslopHistoryProtocolChild$")}()
		// The marker confirms the child reached the intended response boundary;
		// it is not a sleep-based assumption that native startup already finished.
		deadline:=time.NewTimer(3*time.Second);ticker:=time.NewTicker(time.Millisecond)
		ready:=false
		boundary:="initialize-received"
		if mode=="request-cancel"||mode=="request-timeout"||mode=="request-early-exit" {boundary="request-2"}
	wait:
		for {select {
		case <-ticker.C:
			if _,err:=os.Stat(filepath.Join(root,boundary));err==nil {ready=true;break wait}
		case <-deadline.C:break wait
		}}
		deadline.Stop();ticker.Stop()
		if mode=="delayed" {if err:=os.WriteFile(filepath.Join(root,"release"),[]byte("synthetic"),0600);err!=nil {cancel();r:=<-done;if r.Capture!=nil {clear(r.Capture.Stdout)};t.Fatal(err)}}
		if mode=="cancel"||mode=="request-cancel"||!ready {cancel()}
		var r askChildResult
		select {case r=<-done:case <-time.After(8*time.Second):cancel();t.Fatal("same child copy/Wait/collection did not terminate")}
		defer clear(r.Raw)
		if r.Capture!=nil {defer clear(r.Capture.Stdout)}
		if !ready||!r.Started||!r.Assigned||!r.WaitVerified||!r.Reaped||r.Capture==nil||r.Capture.NativeProcess==nil {t.Fatal("original native child was not collected",r.CollectionReason)}
		if len(r.Raw)!=0||strings.Contains(r.Capture.protocolReason,"private") {t.Fatal("raw response reached ordinary output")}
		valid:=mode=="normal"||mode=="delayed"||mode=="notification"
		if valid {
			if !r.Capture.Complete||r.Capture.protocolReason!="history_protocol_complete"||r.Exit!=0||r.TimedOut {t.Fatal("valid fixed step exchange failed",r.Capture.protocolReason,r.Exit)}
			for _,name:=range []string{"request-2","request-3","stdin-eof"} {if _,err:=os.Stat(filepath.Join(root,name));err!=nil {t.Fatal("fixed request/final EOF was not consumed",name)}}
			answers,err:=parseUnslopHistoryBatch(r.Capture.Stdout,2);if err!=nil||len(answers)!=2 {t.Fatal("same child responses were not usable",err)}
			for _,answer:=range answers {clear(answer)}
		} else if r.Capture.Complete||r.Capture.protocolReason=="history_protocol_complete" {t.Fatal("invalid exchange became a successful capture",mode)}
		if mode=="cancel"||mode=="timeout"||mode=="request-cancel"||mode=="request-timeout" {if !r.TimedOut {t.Fatal("deadline/cancel was not preserved")}}
		if mode!="duplicate" {if _,err:=os.Stat(filepath.Join(root,"unexpected-input"));err==nil {t.Fatal("invalid response supplied a later request")}}
	})}
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
	for n:=0;n<33;n++ {x.observe([]byte("{\"method\":\"warning\",\"params\":{}}\n"))}
	if x.resultReason()!="history_protocol_notification_limit" {t.Fatal("notification flood did not terminate at its fixed bound")}
	var p [1]byte
	if n,err:=x.Read(p[:]);n!=0||err==nil||err==io.EOF {t.Fatal("notification overflow released later input")}
}

func TestUnslopHistoryNotificationPayloadIsNotAnAuthorityOrResponse(t *testing.T) {
	for _,params:=range []string{"null","[]","{}","\"opaque\""} {
		raw:=[]byte("{\"method\":\"future/serverNotification\",\"params\":"+params+"}")
		result,notification,err:=classifyUnslopHistoryFrame(raw,1)
		if err!=nil||!notification||result!=nil {clear(result);t.Fatal("notification payload was mistaken for a response")}
	}
}
