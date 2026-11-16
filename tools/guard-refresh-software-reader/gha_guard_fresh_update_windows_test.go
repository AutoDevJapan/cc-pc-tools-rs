//go:build windows

package main

import (
    "bytes"
    "context"
    "encoding/json"
    "errors"
    "net/http"
    "testing"
)

func TestGHAFreshUpdateEntryIsFixedFailureWithoutIssuer(t *testing.T) {
    var out bytes.Buffer
    if code:=run([]string{ghaFreshUpdateFlag},&out,nil);code!=1 {t.Fatal("missing issuer was not a failure",code)}
    var result ghaFreshUpdateUnavailable
    if json.Unmarshal(out.Bytes(),&result)!=nil||result.Domain!=ghaFreshUpdateDomain||result.Reason!="guard_update_issuer_unavailable"||
        result.Qualified||result.UpdatePerformed||result.DataAccessGranted {t.Fatal("unconnected entry claimed authority")}
    out.Reset()
    if code:=runGHAFreshUpdate([]string{ghaFreshUpdateFlag,"extra"},&out);code!=2||out.Len()!=0 {t.Fatal("extra argument accepted")}
}

func TestGHAFreshHandoffUsesOneOwnedBorrowAndFailsWithoutIssuer(t *testing.T) {
    for _,name:=range []string{"normal","different-owner","closed-proof","tampered-proof","main-moved","closed-candidate","cancelled"} {
        t.Run(name,func(t *testing.T){
            ctx:=context.Background();fixture:=ghaCurrentTestFixtureFor(t,"")
            manifest,archive:=ghaCurrentTestArchive(t,fixture.records)
            artifact:=ghaCurrentTestCandidate(t,ctx,manifest)
            authority:=ghaCurrentTestAuthority(t,fixture,manifest,archive)
            proof,err:=ghaCurrentTestBoundProof(t,ctx,fixture,manifest,archive,artifact)
            if err!=nil||proof==nil {t.Fatal("fixture proof",err)}
            defer proof.Close()
            target:=artifact
            switch name {
            case "different-owner":target=ghaCurrentTestCandidate(t,ctx,manifest)
            case "closed-proof":_ = proof.Close()
            case "tampered-proof":proof.records[0].SHA="ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
            case "main-moved":fixture.moveAtSecondRead=true
            case "closed-candidate":_ = artifact.Close()
            case "cancelled":var cancel context.CancelFunc;ctx,cancel=context.WithCancel(ctx);cancel()
            }
            calls:=0
            observe:=func(got *guardTargetCandidate,held *ghaFreshCurrentInputs,raw []byte) error {
                calls++
                if got!=artifact||held!=proof||!got.borrowing()||len(raw)==0 {t.Fatal("not the same held candidate")}
                _,generation,ok:=got.binding()
                if !ok||digest(raw)!=generation.OutputSHA||held.generation!=generation {t.Fatal("generation changed during borrow")}
                return nil
            }
            err=ghaFreshHandoffWithoutIssuerWith(ctx,target,proof,fixture.client,authority,fixture.policy,observe)
            if name=="normal" {
                if !errors.Is(err,errGHAFreshUpdateIssuerUnavailable)||calls!=1 {t.Fatal("owned handoff did not end at missing issuer",err)}
                if artifact.VerifySourceGeneration(){t.Fatal("failed handoff left candidate admitted")}
            } else if err==nil||calls!=0 {t.Fatal("invalid source reached transition",name)}
        })
    }
}

func TestGHAFreshHandoffRequiresItsInputs(t *testing.T) {
    ctx:=context.Background()
    client:=&http.Client{}
    if ghaFreshHandoffWithoutIssuer(ctx,nil,nil,client,nil,nil)==nil {t.Fatal("nil objects accepted")}
    if ghaFreshHandoffWithoutIssuer(nil,nil,nil,client,nil,nil)==nil {t.Fatal("nil context accepted")}
}

func TestGHAFreshHeldOwnerRetainsOneCandidateAndCleansPartial(t *testing.T) {
    for _,name:=range []string{"normal","partial","missing"} {
        t.Run(name,func(t *testing.T){
            ctx:=context.Background();f:=ghaCurrentTestFixtureFor(t,"");m,archive:=ghaCurrentTestArchive(t,f.records)
            artifact:=ghaCurrentTestCandidate(t,ctx,m);idle:=0
            acquire:=func(_ context.Context,capture *ghaFreshUpdateArchiveCapture) (guardTargetArtifact,error) {
                if capture.Start(ghaCurrentTestLocator(m,archive))!=nil||capture.Hold(archive,m)!=nil {t.Fatal("capture fixture")}
                if name=="missing" {return nil,nil}
                if name=="partial" {return artifact,errors.New("synthetic partial")}
                return artifact,nil
            }
            held,err:=ghaFreshOpenHeldUpdateWith(ctx,f.client,func(){idle++},acquire)
            if name=="normal" {
                if err!=nil||held==nil||held.artifact!=artifact||held.proof==nil||held.proof.artifact!=artifact {t.Fatal("same candidate not retained",err)}
                if held.Close()!=nil||idle!=1||artifact.VerifySourceGeneration(){t.Fatal("normal owned cleanup failed")}
            } else {
                if err==nil||held!=nil||idle!=1 {t.Fatal("partial acquisition accepted or not cleaned",name)}
                if name=="partial"&&artifact.VerifySourceGeneration(){t.Fatal("partial candidate not closed")}
            }
        })
    }
}

func TestGHAFreshCaptureFailureAndActiveCloseDoNotLoseOwnership(t *testing.T) {
    ctx:=context.Background();f:=ghaCurrentTestFixtureFor(t,"");m,archive:=ghaCurrentTestArchive(t,f.records)
    loc:=ghaCurrentTestLocator(m,archive);capture:=ghaFreshNewUpdateArchiveCapture()
    if capture.Start(loc)!=nil||capture.Hold(archive,m)!=nil {t.Fatal("capture fixture")}
    cancelled,cancel:=context.WithCancel(ctx);cancel()
    if candidate,err:=ghaFreshAcquireFromAuthorityCaptured(cancelled,nil,nil,loc,capture);err==nil||candidate!=nil {t.Fatal("cancelled acquisition accepted")}
    if !capture.closed||len(capture.archive)!=0 {t.Fatal("failed capture not erased")}

    artifact:=ghaCurrentTestCandidate(t,ctx,m)
    acquire:=func(_ context.Context,c *ghaFreshUpdateArchiveCapture)(guardTargetArtifact,error){
        if c.Start(loc)!=nil||c.Hold(archive,m)!=nil{t.Fatal("capture fixture")}
        return artifact,nil
    }
    held,err:=ghaFreshOpenHeldUpdateWith(ctx,f.client,func(){},acquire)
    if err!=nil||held==nil {t.Fatal("held fixture",err)}
    held.mu.Lock();held.active=true;held.mu.Unlock()
    if held.Close()==nil {t.Fatal("active close claimed cleanup")}
    held.mu.Lock();preserved:=held.revoked&&!held.closed&&held.artifact==artifact&&held.proof!=nil;held.active=false;held.mu.Unlock()
    if !preserved {t.Fatal("active owner lost")}
    if held.Close()!=nil||artifact.VerifySourceGeneration(){t.Fatal("later cleanup failed")}
}
