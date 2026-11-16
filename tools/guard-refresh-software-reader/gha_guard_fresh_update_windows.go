//go:build windows

package main

import (
    "context"
    "encoding/json"
    "errors"
    "io"
    "net/http"
    "sync"
    "time"
)

const ghaFreshUpdateFlag = "--apply-fresh-guard-update"
const ghaFreshUpdateDomain = "owner-guard-fresh-update-v1"
var errGHAFreshUpdateIssuerUnavailable = errors.New("guard_update_issuer_unavailable")

type ghaFreshUpdateUnavailable struct {
    Domain string `json:"domain"`
    Reason string `json:"reason"`
    Qualified bool `json:"qualified"`
    UpdatePerformed bool `json:"update_performed"`
    DataAccessGranted bool `json:"data_access_granted"`
}

// This callback is an in-package observation seam. It cannot issue a target
// permit or prove a replacement. The real native issuer does not yet exist.
type ghaFreshHeldUpdateObservation func(*guardTargetCandidate,*ghaFreshCurrentInputs,[]byte) error

// The candidate and its source proof remain live during the same pinned borrow.
// The only current endpoint is an observation followed by fixed failure. A real
// update requires a separately issued fixed-purpose native lease and must record
// post-commit/cleanup outcomes without treating an error as proof of no write.
func ghaFreshHandoffWithoutIssuer(ctx context.Context,a *guardTargetCandidate,p *ghaFreshCurrentInputs,public *http.Client,authority *ghaGuardEvidenceAuthority,observe ghaFreshHeldUpdateObservation) error {
    return ghaFreshHandoffWithoutIssuerWith(ctx,a,p,public,authority,ghaFreshFixedProducerContract(),observe)
}

// The policy argument is a package-test seam; neither path issues an update.
func ghaFreshHandoffWithoutIssuerWith(ctx context.Context,a *guardTargetCandidate,p *ghaFreshCurrentInputs,public *http.Client,authority *ghaGuardEvidenceAuthority,policy ghaFreshProducerContract,observe ghaFreshHeldUpdateObservation) (result error) {
    if ctx==nil{return errGHAGuardUnqualified}
    if ctx.Err()!=nil||a==nil||p==nil||public==nil||authority==nil{return errors.Join(errGHAGuardUnqualified,ctx.Err())}
    if err:=p.beginUse();err!=nil{return err}
    defer func(){result=errors.Join(result,p.endUse())}()
    return a.WithPinnedPayload(func(raw []byte) error {
        if len(raw)==0||!a.borrowing()||!p.useLive()||ctx.Err()!=nil{return errGHAGuardUnqualified}
        if err:=p.recheckBorrowed(ctx,a,public,authority,policy);err!=nil{return err}
        if !p.useLive()||ctx.Err()!=nil{return errGHAGuardUnqualified}
        if observe!=nil {
            if err:=observe(a,p,raw);err!=nil{return err}
            if !a.borrowing()||!p.useLive()||ctx.Err()!=nil{return errGHAGuardUnqualified}
        }
        return errGHAFreshUpdateIssuerUnavailable
    })
}

// This entry fails before acquiring a credential, candidate or target. It must
// not be upgraded to success until the real issuer and supervised transition
// exist and have independent source/OS acceptance.
func runGHAFreshUpdate(args []string,out io.Writer) int {
    if len(args)!=1||args[0]!=ghaFreshUpdateFlag||out==nil{return 2}
    result:=ghaFreshUpdateUnavailable{Domain:ghaFreshUpdateDomain,Reason:"guard_update_issuer_unavailable"}
    if json.NewEncoder(out).Encode(result)!=nil{return 1}
    return 1
}

// This in-process owner ties one verified candidate to the source records
// retained from that same H1 archive. It is not a target-update issuer.
type ghaFreshHeldUpdate struct {
    mu sync.Mutex
    self *ghaFreshHeldUpdate
    artifact *guardTargetCandidate
    proof *ghaFreshCurrentInputs
    public *http.Client
    closeIdle func()
    active,revoked,closing,closed bool
}

type ghaFreshHeldAcquisition func(context.Context,*ghaFreshUpdateArchiveCapture) (guardTargetArtifact,error)

// Production calls this only from a future whole-flight supervised worker.
// It is currently unreachable from the fixed-failure CLI entry.
func ghaFreshOpenHeldUpdate(ctx context.Context,audit *ghaFreshCredentialAudit) (*ghaFreshHeldUpdate,error) {
    tr:=&http.Transport{Proxy:nil,TLSHandshakeTimeout:10*time.Second,ResponseHeaderTimeout:15*time.Second,MaxResponseHeaderBytes:64<<10}
    client:=&http.Client{Transport:tr,Timeout:30*time.Second}
    return ghaFreshOpenHeldUpdateWith(ctx,client,tr.CloseIdleConnections,func(inner context.Context,capture *ghaFreshUpdateArchiveCapture)(guardTargetArtifact,error){
        return guardTargetArtifactFromServer(inner,func(c context.Context)(*ghaGuardVerifiedCandidate,error){
            return ghaFreshOpenVerifiedCandidateCaptured(c,audit,capture)
        })
    })
}

// The callback is a package test seam. The production wrapper above fixes it
// to the authenticated candidate acquisition; no CLI field supplies it.
func ghaFreshOpenHeldUpdateWith(ctx context.Context,public *http.Client,closeIdle func(),acquire ghaFreshHeldAcquisition) (result *ghaFreshHeldUpdate,resultErr error) {
    if ctx==nil||ctx.Err()!=nil||public==nil||closeIdle==nil||acquire==nil{return nil,errGHAGuardUnqualified}
    capture:=ghaFreshNewUpdateArchiveCapture()
    defer func(){
        if closeErr:=capture.Close();closeErr!=nil {
            if result!=nil {resultErr=errors.Join(resultErr,result.Close());result=nil}
            resultErr=errors.Join(errGHAGuardUnqualified,resultErr,closeErr)
        }
    }()
    value,err:=acquire(ctx,capture)
    if err!=nil||value==nil {
        if value!=nil {err=errors.Join(err,value.Close())}
        closeIdle();return nil,errors.Join(errGHAGuardUnqualified,err)
    }
    artifact,ok:=value.(*guardTargetCandidate)
    if !ok {closeErr:=value.Close();closeIdle();return nil,errors.Join(errGHAGuardUnqualified,closeErr)}
    proof,err:=capture.Bind(ctx,artifact,public)
    claimed:=false
    if err==nil&&proof!=nil&&ctx.Err()==nil {err=capture.ClaimBound(proof);claimed=err==nil}
    if err!=nil||proof==nil||ctx.Err()!=nil {
        if proof!=nil {if claimed {err=errors.Join(err,proof.Close())} else {err=errors.Join(err,capture.DiscardBound(proof))}}
        err=errors.Join(err,artifact.Close());closeIdle()
        return nil,errors.Join(errGHAGuardUnqualified,err,ctx.Err())
    }
    result=&ghaFreshHeldUpdate{artifact:artifact,proof:proof,public:public,closeIdle:closeIdle}
    result.self=result
    return result,nil
}

// The retained locator opens only the same authenticated generation. This
// currently reaches the fixed missing-issuer failure, never a target write.
func (h *ghaFreshHeldUpdate) handoff(ctx context.Context,observe ghaFreshHeldUpdateObservation) (result error) {
    if h==nil||ctx==nil||ctx.Err()!=nil{return errGHAGuardUnqualified}
    h.mu.Lock()
    if h.self!=h||h.active||h.revoked||h.closing||h.closed||h.artifact==nil||h.proof==nil||h.public==nil {
        h.mu.Unlock();return errGHAGuardUnqualified
    }
    h.active=true;artifact,proof,public:=h.artifact,h.proof,h.public
    h.mu.Unlock()
    defer func(){h.mu.Lock();h.active=false;revoked:=h.revoked;h.mu.Unlock();if revoked{result=errors.Join(result,errGuardTargetCleanup)}}()
    audit:=&ghaFreshCredentialAudit{state:"unknown"}
    authority,err:=proof.OpenAuthority(ctx,artifact,audit)
    if authority==nil{return errors.Join(errGHAGuardUnqualified,err)}
    defer func(){result=ghaFreshCloseCredentialAuthority(authority,result,audit)}()
    if err!=nil{return errors.Join(errGHAGuardUnqualified,err)}
    return ghaFreshHandoffWithoutIssuer(ctx,artifact,proof,public,authority,observe)
}

// A concurrent Close revokes further admission but does not discard a live
// borrow; the owner must call Close again after the active handoff ends.
func (h *ghaFreshHeldUpdate) Close() error {
    if h==nil{return nil}
    h.mu.Lock()
    if h.self!=h {h.mu.Unlock();return errGuardTargetCleanup}
    if h.closed {h.mu.Unlock();return nil}
    h.revoked=true
    if h.active||h.closing {h.mu.Unlock();return errGuardTargetCleanup}
    h.closing=true;artifact,proof,closeIdle:=h.artifact,h.proof,h.closeIdle
    h.mu.Unlock()
    var err error
    if proof!=nil {err=errors.Join(err,proof.Close())}
    if artifact!=nil {err=errors.Join(err,artifact.Close())}
    if closeIdle!=nil {closeIdle()}
    h.mu.Lock();h.closing=false
    if err==nil {h.closed=true;h.artifact=nil;h.proof=nil;h.public=nil;h.closeIdle=nil}
    h.mu.Unlock()
    if err!=nil{return errors.Join(errGuardTargetCleanup,err)}
    return nil
}
