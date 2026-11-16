//go:build windows

package main

import (
    "context"
    "errors"
    "testing"
    "time"
)

func dsRunnerFixturePin(raw []byte)dsRunnerCapturePin{
    return dsRunnerCapturePin{Producer:dsRunnerApprovedProducer,WorkflowID:11,RunID:17,JobID:23,CheckRunID:31,ArtifactID:41,Attempt:1,ArchiveSHA:digest([]byte("archive")),OutputSHA:digest(raw),CaptureSHA:digest([]byte("capture")),ProofSHA:digest([]byte("proof")),BundleSHA:digest([]byte("bundle")),AttestationID:"fixture41"}
}
func dsRunnerFixtureCandidate(raw []byte)*dsRunnerCandidate{
    c:=&dsRunnerCandidate{issuer:&dsRunnerIssuer{token:1},pin:dsRunnerFixturePin(raw),raw:append([]byte(nil),raw...)};c.self=c;return c
}
func TestDSRunnerCandidateRejectsDetachedOrMutatedState(t *testing.T){
    for _,kind:=range []string{"detached","issuer","pin","payload","cancel"}{t.Run(kind,func(t *testing.T){
        c:=dsRunnerFixtureCandidate([]byte("candidate"));ctx,cancel:=context.WithCancel(context.Background());defer cancel();defer c.Close()
        switch kind{case "detached":c.self=nil;case "issuer":c.issuer=nil;case "pin":c.pin.RunID=0;case "payload":c.raw[0]^=1;case "cancel":cancel()}
        called:=false;if c.with(ctx,func([]byte)error{called=true;return nil})==nil||called{t.Fatal("invalid candidate entered a consumer")}
    })}
}
func TestDSRunnerCandidateFailureAndPanicRevokeBorrow(t *testing.T){
    for _,kind:=range []string{"error","panic","cancel","mutation","pin"}{t.Run(kind,func(t *testing.T){
        c:=dsRunnerFixtureCandidate([]byte("candidate"));ctx,cancel:=context.WithCancel(context.Background());defer cancel();defer c.Close();entered:=false;panicked:=false
        var err error
        func(){defer func(){if recover()!=nil{panicked=true}}();err=c.with(ctx,func(raw []byte)error{entered=true;switch kind{case "error":return errors.New("primary");case "panic":panic("synthetic");case "cancel":cancel();case "mutation":raw[0]^=1;case "pin":c.mu.Lock();c.pin.JobID++;c.mu.Unlock()};return nil})}()
        if !entered||kind=="panic"&&!panicked||kind!="panic"&&err==nil{t.Fatal("failure fixture missed the borrowed callback")}
        c.mu.Lock();closed,issuer,n:=c.closed,c.issuer,len(c.raw);c.mu.Unlock();if !closed||issuer!=nil||n!=0{t.Fatal("failed borrowed callback retained authority or payload")}
        if c.with(context.Background(),func([]byte)error{return nil})==nil{t.Fatal("revoked candidate was reused")}
    })}
}
func TestDSRunnerCandidateCloseWaitsForActiveBorrow(t *testing.T){
    c:=dsRunnerFixtureCandidate([]byte("candidate"));entered:=make(chan struct{});release:=make(chan struct{});done:=make(chan error,1);released:=false;collected:=false
    defer func(){if !released{close(release)};_ = c.Close();ctx,cancel:=context.WithTimeout(context.Background(),time.Second);defer cancel();_ = c.waitClosed(ctx);if !collected{select{case<-done:case<-ctx.Done():t.Error("owned callback was not reaped")}}}()
    go func(){done<-c.with(context.Background(),func(raw []byte)error{close(entered);<-release;if string(raw)!="candidate"{return errors.New("premature clear")};return nil})}()
    select{case<-entered:case<-time.After(time.Second):t.Fatal("callback did not enter")}
    if c.with(context.Background(),func([]byte)error{return nil})==nil{t.Fatal("concurrent borrower accepted")}
    if c.Close()!=nil{t.Fatal("close")};ctx,cancel:=context.WithCancel(context.Background());cancel();if c.waitClosed(ctx)==nil{t.Fatal("wait reported completion while borrower held bytes")}
    c.mu.Lock();n:=len(c.raw);c.mu.Unlock();if n==0{t.Fatal("close cleared an active consumer's bytes")}
    close(release);released=true;ctx2,cancel2:=context.WithTimeout(context.Background(),time.Second);defer cancel2();if c.waitClosed(ctx2)!=nil{t.Fatal("borrower did not finish")}
    select{case err:=<-done:collected=true;if err==nil{t.Fatal("concurrent close was reported as a successful borrow")};case<-ctx2.Done():t.Fatal("callback result missing")}
    c.mu.Lock();n=len(c.raw);c.mu.Unlock();if n!=0{t.Fatal("closed payload not erased")}
}
func TestDSRunnerCandidateSuccessAndCloseAreDistinct(t *testing.T){
    c:=dsRunnerFixtureCandidate([]byte("candidate"));if c.with(context.Background(),func(raw []byte)error{if string(raw)!="candidate"{t.Fatal("different buffer")};return nil})!=nil{t.Fatal("normal borrow")}
    if c.waitClosed(context.Background())==nil{t.Fatal("borrow completion was treated as close")};if c.Close()!=nil||c.Close()!=nil||c.waitClosed(context.Background())!=nil{t.Fatal("idempotent close")}
}
func TestDSRunnerGitBlobUsesGitObjectHeader(t *testing.T){if dsRunnerGitBlob([]byte("test\n"))!="9daeafb9864cf43055ae93beb0afd6c7d144bfa4"{t.Fatal("Git blob object header is not exact")}}
