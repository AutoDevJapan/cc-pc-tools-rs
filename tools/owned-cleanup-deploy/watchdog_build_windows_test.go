//go:build windows

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
)

func watchdogBuildFixturePE() []byte {
	b:=make([]byte,256)
	b[0],b[1]='M','Z';b[60]=64
	copy(b[64:],"PE\x00\x00");b[68],b[69]=0x64,0x86
	b[88],b[89]=0x0b,0x02;b[156]=2
	return b
}
func watchdogFixtureCargo() []byte {
	return []byte("{\"reason\":\"compiler-artifact\",\"target\":{\"name\":\"ocx_health_watchdog\",\"kind\":[\"bin\"],\"crate_types\":[\"bin\"]},\"executable\":\"held-output.exe\",\"fresh\":false}\n{\"reason\":\"build-finished\",\"success\":true}\n")
}
func watchdogBuildSnapshot() watchdogSourceSnapshot {
	return watchdogSourceSnapshot{
		Commit:watchdogApprovedCommit,Tree:"d980b091aa58e7aef75cba040e9f852ad1e942fc",
		CrateTree:watchdogApprovedCrate,WorkspaceManifestBlob:watchdogCargoManifest,
		WorkspaceLockBlob:watchdogCargoLock,CompiledInputsSHA256:strings.Repeat("1",64),
		ConfigurationSHA256:strings.Repeat("2",64),ToolchainSHA256:strings.Repeat("3",64),
	}
}
type watchdogBuildTestArtifact struct {
	body []byte
	id watchdogFileIdentity
	closes int
	failClose bool
	duplicate bool
	suppressFailure bool
}
func (a *watchdogBuildTestArtifact) Identity(context.Context)(watchdogFileIdentity,error){return a.id,nil}
func (a *watchdogBuildTestArtifact) WithHeld(_ context.Context, callback func(io.ReadSeeker)error)error{
	e:=callback(bytes.NewReader(a.body))
	if a.duplicate {e=errors.Join(e,callback(bytes.NewReader(a.body)))}
	if a.suppressFailure {return nil}
	return e
}
func (a *watchdogBuildTestArtifact) Close()error{
	a.closes++
	if a.failClose{return errors.New("artifact-close")}
	return nil
}
type watchdogBuildTestLease struct{
	before,after watchdogSourceSnapshot
	output *watchdogBuildOutput
	calls []string
	buildError bool
	failClose bool
	cancel context.CancelFunc
	closes int
}
func (l *watchdogBuildTestLease) Claim(context.Context)error{
	l.calls=append(l.calls,"claim")
	if l.cancel!=nil{l.cancel()}
	return nil
}
func (l *watchdogBuildTestLease) Before(context.Context)(watchdogSourceSnapshot,error){l.calls=append(l.calls,"before");return l.before,nil}
func (l *watchdogBuildTestLease) Build(_ context.Context,args []string)(*watchdogBuildOutput,error){
	l.calls=append(l.calls,"build")
	if !reflect.DeepEqual(args,watchdogBuildArgs){return nil,errors.New("command-changed")}
	if l.buildError{return l.output,errors.New("partial-build")}
	return l.output,nil
}
func (l *watchdogBuildTestLease) After(context.Context)(watchdogSourceSnapshot,error){l.calls=append(l.calls,"after");return l.after,nil}
func (l *watchdogBuildTestLease) Recheck(context.Context)error{return nil}
func (l *watchdogBuildTestLease) Close()error{l.closes++;if l.failClose{return errors.New("lease-close")};return nil}

func watchdogBuildTestFixture()(*watchdogBuildTestLease,*watchdogBuildTestArtifact){
	body:=watchdogBuildFixturePE();hash:=sha256.Sum256(body);id:=watchdogTestID()
	id.Bytes=int64(len(body));id.SHA256=hex.EncodeToString(hash[:])
	a:=&watchdogBuildTestArtifact{body:body,id:id}
	l:=&watchdogBuildTestLease{
		before:watchdogBuildSnapshot(),after:watchdogBuildSnapshot(),
		output:&watchdogBuildOutput{
			ChildReaped:true,StdoutComplete:true,StderrComplete:true,
			CargoJSON:watchdogFixtureCargo(),Artifact:a,
		},
	}
	return l,a
}

func TestWatchdogBuildProducerOwnsSameProductAndClosesOnce(t *testing.T){
	l,a:=watchdogBuildTestFixture()
	p,e:=produceWatchdogBuild(context.Background(),func(context.Context)(watchdogBuildLease,error){return l,nil},"held-output.exe")
	if e!=nil||p==nil||p.Artifact!=a||a.closes!=0||l.closes!=0||
		strings.Join(l.calls,",")!="before,claim,build,after"{
		t.Fatalf("owned product: %v %+v",e,l)
	}
	if p.Close()!=nil||p.Close()!=nil||a.closes!=1||l.closes!=1{t.Fatal("close ownership")}
}

func TestWatchdogBuildProducerRejectsInputsCollectionAndPE(t *testing.T){
	for _,kind:=range []string{
		"before","after-input","after-config","exit","not-reaped","stdout",
		"stderr","partial","wrong-subsystem","wrong-hash","duplicate-callback",
		"suppressed-callback-error",
	}{
		t.Run(kind,func(t *testing.T){
			l,a:=watchdogBuildTestFixture()
			switch kind{
			case "before":l.before.CrateTree=strings.Repeat("0",40)
			case "after-input":l.after.CompiledInputsSHA256=strings.Repeat("0",64)
			case "after-config":l.after.ConfigurationSHA256=strings.Repeat("0",64)
			case "exit":l.output.ExitCode=1
			case "not-reaped":l.output.ChildReaped=false
			case "stdout":l.output.StdoutComplete=false
			case "stderr":l.output.StderrComplete=false
			case "partial":l.buildError=true
			case "wrong-subsystem":a.body[156]=3
			case "wrong-hash":a.id.SHA256=strings.Repeat("0",64)
			case "duplicate-callback":a.duplicate=true
			case "suppressed-callback-error":a.body[156]=3;a.suppressFailure=true
			}
			p,e:=produceWatchdogBuild(context.Background(),func(context.Context)(watchdogBuildLease,error){return l,nil},"held-output.exe")
			if e==nil||p!=nil{t.Fatalf("invalid qualified %v %v",p,e)}
			if l.closes!=1{t.Fatal("lease not closed")}
			if kind!="before"&&a.closes!=1{t.Fatal("artifact not closed")}
			if kind=="before"&&strings.Contains(strings.Join(l.calls,","),"build"){t.Fatal("invalid source built")}
		})
	}
}

func TestWatchdogBuildCancellationBeforeCommandHasBuildZero(t *testing.T){
	ctx,cancel:=context.WithCancel(context.Background());defer cancel()
	l,_:=watchdogBuildTestFixture();l.cancel=cancel
	p,e:=produceWatchdogBuild(ctx,func(context.Context)(watchdogBuildLease,error){return l,nil},"held-output.exe")
	if e==nil||p!=nil||strings.Contains(strings.Join(l.calls,","),"build")||l.closes!=1{
		t.Fatal("cancelled build spawned")
	}
}

func TestWatchdogBuildPartialFailureRetainsBothCleanupFacts(t *testing.T){
	l,a:=watchdogBuildTestFixture();l.buildError=true;l.failClose=true;a.failClose=true
	p,e:=produceWatchdogBuild(context.Background(),func(context.Context)(watchdogBuildLease,error){return l,nil},"held-output.exe")
	if e==nil||p==nil||!strings.Contains(e.Error(),"lease-close")||
		!strings.Contains(e.Error(),"artifact-close")||a.closes!=1||l.closes!=1{
		t.Fatal("lost partial close",e)
	}
	_=p.Close()
	if a.closes!=1||l.closes!=1{t.Fatal("failed close retried")}
}

func TestWatchdogCargoTranscriptRequiresRealBinAndFinalSuccess(t *testing.T){
	for _,kind:=range []string{"normal","fresh","missing-fresh","wrong-bin","fixture","wrong-path","duplicate","no-finish","failed-finish","after-finish","invalid"}{
		t.Run(kind,func(t *testing.T){
			raw:=string(watchdogFixtureCargo())
			switch kind{
			case "fresh":raw=strings.Replace(raw,"\"fresh\":false","\"fresh\":true",1)
			case "missing-fresh":raw=strings.Replace(raw,",\"fresh\":false","",1)
			case "wrong-bin":raw=strings.Replace(raw,"\"bin\"","\"lib\"",1)
			case "fixture":raw=strings.Replace(raw,"ocx_health_watchdog","ocx_watchdog_fixture",1)
			case "wrong-path":raw=strings.Replace(raw,"held-output.exe","other.exe",1)
			case "duplicate":raw=strings.Split(raw,"\n")[0]+"\n"+raw
			case "no-finish":raw=strings.Split(raw,"\n")[0]+"\n"
			case "failed-finish":raw=strings.Replace(raw,"\"success\":true","\"success\":false",1)
			case "after-finish":raw+="{\"reason\":\"compiler-message\"}\n"
			case "invalid":raw="{"
			}
			if (watchdogCargoEvidence([]byte(raw),"held-output.exe")==nil)!=(kind=="normal"){
				t.Fatal("transcript boundary")
			}
		})
	}
}
