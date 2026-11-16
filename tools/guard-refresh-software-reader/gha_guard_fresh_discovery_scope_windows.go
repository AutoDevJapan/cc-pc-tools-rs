//go:build windows

package main

import (
    "context"
    "encoding/base64"
    "errors"
    "fmt"
    "io"
    "net/http"
    "strings"
    "time"
)

// This session can read only the fixed owner, repository and workflow.
// Its run-specific URLs are constructed from a validated fixed listing.
// It is discovery permission, not a candidate or installation authority.
type ghaFreshDiscoveryScope struct { allowed map[string]bool }

func ghaFreshDiscoveryURLs(hints []ghaFreshRunHint) (*ghaFreshDiscoveryScope,error) {
    if len(hints)>ghaFreshListingMax{return nil,errGHAGuardUnqualified}
    scope:=&ghaFreshDiscoveryScope{allowed:map[string]bool{
        "https://api.github.com/user":true,
        "https://api.github.com/repos/AutoDevJapan/cc-pc-tools-rs/actions/workflows/unattended-readiness.yml":true,
        ghaFreshListingURL:true,
    }}
    for p:=2;p<=10;p++ {scope.allowed[fmt.Sprintf("%s&page=%d",ghaFreshListingURL,p)]=true}
    seen:=map[int64]bool{}
    const base="https://api.github.com/repos/AutoDevJapan/cc-pc-tools-rs"
    for _,h:=range hints {
        if !ghaFreshHintShape(h)||seen[h.runID]||h.createdAt.IsZero(){return nil,errGHAGuardUnqualified};seen[h.runID]=true
        scope.allowed[fmt.Sprintf("%s/actions/runs/%d/attempts/1/jobs?per_page=100",base,h.runID)]=true
        scope.allowed[fmt.Sprintf("%s/actions/runs/%d/artifacts?per_page=100",base,h.runID)]=true
        scope.allowed[base+"/contents/.github/workflows/unattended-readiness.yml?ref="+h.producerRevision]=true
    }
    return scope,nil
}

func (s *ghaFreshDiscoveryScope) allows(req *http.Request) bool {
    if s==nil||req==nil||req.URL==nil||req.Method!=http.MethodGet||(req.Body!=nil&&req.Body!=http.NoBody){return false}
    u:=req.URL
    if u.Scheme!="https"||u.Host!="api.github.com"||u.User!=nil||u.Opaque!=""||u.Fragment!=""||u.RawPath!=""||u.ForceQuery||(req.Host!=""&&req.Host!="api.github.com"){return false}
    for name,values:=range req.Header {
        if len(values)!=1{return false}
        switch strings.ToLower(name) {
        case "accept":if values[0]!="application/vnd.github+json"{return false}
        case "x-github-api-version":if values[0]!="2022-11-28"{return false}
        default:return false
        }
    }
    return s.allowed[u.String()]
}

// Register only results of the fixed, bounded listing already parsed by the
// caller in this package. There is no CLI path, token or URL input.
func ghaFreshInstallDiscoveryScope(client *http.Client,hints []ghaFreshRunHint) error {
    if client==nil{return errGHAGuardUnqualified}
    session,owned:=client.Transport.(*ghaGuardCredentialSession)
    if !owned{return nil} // package-test transports do not own credentials
    scope,err:=ghaFreshDiscoveryURLs(hints);if err!=nil{return err}
    session.mu.Lock();defer session.mu.Unlock()
    if session.closed||!session.ready||session.active!=0{return errGHAGuardUnqualified}
    session.discovery=scope
    return nil
}

// Skip an absent old artifact only after the exact old run's revision names
// the reviewed workflow blob and the actual bytes match the fixed contract.
func ghaFreshVerifiedRetentionSource(ctx context.Context,client *http.Client,h ghaFreshRunHint) error {
    return ghaFreshVerifiedRetentionSourceWith(ctx,client,h,ghaFreshFixedProducerContract().workflow)
}
func ghaFreshVerifiedRetentionSourceWith(ctx context.Context,client *http.Client,h ghaFreshRunHint,w ghaFreshContractFile) error {
    if ctx==nil||ctx.Err()!=nil||client==nil||!ghaFreshHintShape(h)||w.name!="unattended-readiness.yml"||!ghaGuardHex(w.blob,40)||!ghaGuardHex(w.sha,64)||w.size<=0||w.size>ghaGuardGitObjectLimit {return errGHAGuardUnqualified}
    endpoint:="https://api.github.com/repos/AutoDevJapan/cc-pc-tools-rs/contents/.github/workflows/unattended-readiness.yml?ref="+h.producerRevision
    req,err:=http.NewRequestWithContext(ctx,http.MethodGet,endpoint,nil);if err!=nil{return errGHAGuardUnqualified}
    req.Header.Set("Accept","application/vnd.github+json");req.Header.Set("X-GitHub-Api-Version","2022-11-28")
    noRedirect:=*client;noRedirect.Jar=nil;noRedirect.CheckRedirect=func(*http.Request,[]*http.Request)error{return http.ErrUseLastResponse}
    response,err:=noRedirect.Do(req)
    if err!=nil||response==nil||response.Body==nil {var closeErr error;if response!=nil&&response.Body!=nil{closeErr=response.Body.Close()};return errors.Join(ghaGuardResponseFailure(errGHAGuardUnqualified,closeErr,ctx.Err()),ghaFreshContextFailure(ctx,err))}
    if response.StatusCode!=http.StatusOK {return ghaGuardResponseFailure(ghaFreshHTTPObservedFailure(client,"metadata",response.StatusCode),response.Body.Close(),ctx.Err())}
    if !ghaGuardSinglePageLink(response.Header) {return ghaGuardResponseFailure(errGHAGuardUnqualified,response.Body.Close(),ctx.Err())}
    raw,readErr:=io.ReadAll(io.LimitReader(response.Body,ghaFreshListingLimit+1));closeErr:=response.Body.Close();defer clear(raw)
    if readErr!=nil||closeErr!=nil||len(raw)>ghaFreshListingLimit||ctx.Err()!=nil{return errors.Join(ghaGuardResponseFailure(errGHAGuardUnqualified,closeErr,ctx.Err()),ghaFreshContextFailure(ctx,readErr))}
    obj,err:=ghaFreshObject(raw);if err!=nil{return errGHAGuardUnqualified}
    name,nok:=ghaFreshString(obj,"name");path,pok:=ghaFreshString(obj,"path");kind,kok:=ghaFreshString(obj,"type");sha,sok:=ghaFreshString(obj,"sha");size,zok:=ghaFreshInt(obj,"size");enc,eok:=ghaFreshString(obj,"encoding");content,cok:=ghaFreshString(obj,"content")
    if !nok||name!=w.name||!pok||path!=".github/workflows/unattended-readiness.yml"||!kok||kind!="file"||!sok||sha!=w.blob||!zok||size!=w.size||!eok||enc!="base64"||!cok{return errGHAGuardUnqualified}
    body,decodeErr:=base64.StdEncoding.DecodeString(content);defer clear(body)
    if decodeErr!=nil||int64(len(body))!=w.size||digest(body)!=w.sha||ghaGuardGitObjectID("blob",body)!=w.blob||ctx.Err()!=nil{return errors.Join(errGHAGuardUnqualified,ctx.Err())}
    return nil
}

// Tighten the same owned session to the selected live generation. The
// preflight result remains an untrusted hint until metadata and signature agree.
func ghaFreshBindDiscoveryLocator(a *ghaGuardEvidenceAuthority,l ghaFreshPublicLocator) error {
    if a==nil||a.credential==nil||a.client==nil||a.client.Transport!=a.credential||!ghaFreshLocatorValid(l,time.Now().UTC())||l.hint.superseded{return errGHAGuardUnqualified}
    session:=a.credential;session.mu.Lock();defer session.mu.Unlock()
    if session.closed||!session.ready||session.active!=0||session.discovery==nil{return errGHAGuardUnqualified}
    jobs,err:=ghaFreshResourceURL(ghaFreshJobsResource,l.hint);if err!=nil||!session.discovery.allowed[jobs]{return errGHAGuardUnqualified}
    artifact,err:=ghaFreshResourceURL(ghaFreshArtifactsResource,l.hint);if err!=nil||!session.discovery.allowed[artifact]{return errGHAGuardUnqualified}
    revision:="https://api.github.com/repos/AutoDevJapan/cc-pc-tools-rs/contents/.github/workflows/unattended-readiness.yml?ref="+l.hint.producerRevision
    if !session.discovery.allowed[revision]{return errGHAGuardUnqualified}
    selected:=l;session.fresh=&selected
    return nil
}

// Discovery, preflight and binding share the one owned credential session.
func ghaFreshDiscoverOwned(ctx context.Context,a *ghaGuardEvidenceAuthority,now time.Time) (ghaFreshPublicLocator,error) {
    return ghaFreshDiscoverOwnedCore(ctx,a,now,ghaFreshVerifiedRetentionSource,ghaFreshFixedRetentionIdentity)
}

// The verifier is a package-test seam; production passes the fixed contract.
func ghaFreshDiscoverOwnedWith(ctx context.Context,a *ghaGuardEvidenceAuthority,now time.Time,verify func(context.Context,*http.Client,ghaFreshRunHint)error) (ghaFreshPublicLocator,error) {
    return ghaFreshDiscoverOwnedCore(ctx,a,now,verify,nil)
}
func ghaFreshDiscoverOwnedCore(ctx context.Context,a *ghaGuardEvidenceAuthority,now time.Time,verify func(context.Context,*http.Client,ghaFreshRunHint)error,policy *ghaFreshRetentionIdentity) (ghaFreshPublicLocator,error) {
    var none ghaFreshPublicLocator
    if ctx==nil||ctx.Err()!=nil||a==nil||a.client==nil||a.credential==nil||a.client.Transport!=a.credential||verify==nil||now.IsZero(){return none,errGHAGuardUnqualified}
    hint,err:=ghaFreshReadRunHintCore(ctx,a.client,a.client,now,verify,policy);if err!=nil{return none,err}
    loc,err:=ghaFreshPublicPreflightWithRetention(ctx,a.client,hint,now,verify);if err!=nil{return none,err}
    if err=ghaFreshBindDiscoveryLocator(a,loc);err!=nil{return none,err}
    return loc,nil
}
