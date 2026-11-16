//go:build windows

package main

import (
    "bytes"
    "context"
    "encoding/hex"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "net/http"
    "net/url"
    "strconv"
    "strings"
    "sync"
    "time"

    "github.com/sigstore/sigstore-go/pkg/bundle"
    "github.com/sigstore/sigstore-go/pkg/fulcio/certificate"
    "github.com/sigstore/sigstore-go/pkg/root"
    "github.com/sigstore/sigstore-go/pkg/tuf"
    "github.com/sigstore/sigstore-go/pkg/verify"
)

// A server locator is only a bounded lookup hint. A real candidate is issued
// only after authenticated metadata, source bytes and signatures agree.
func ghaFreshLocatorValid(l ghaFreshPublicLocator, now time.Time) bool {
    return ghaFreshHintShape(l.hint) && !l.hint.superseded && l.jobID>0 && l.checkRunID>0 &&
        l.artifactID>0 && l.archiveBytes>0 && l.archiveBytes<=ghaGuardArchiveLimit &&
        ghaGuardHex(l.archiveSHA,64) && !now.IsZero() && !l.expiresAt.IsZero() &&
        l.expiresAt.After(now)
}

func ghaFreshExpectation(l ghaFreshPublicLocator, now time.Time) (ghaGuardMetadataExpectation,error) {
    if !ghaFreshLocatorValid(l,now) {return ghaGuardMetadataExpectation{},errGHAGuardUnqualified}
    return ghaGuardMetadataExpectation{
        RepositoryID:1406818713,HeadRepositoryID:1406818713,
        Source:ghaGuardSource,SourceTree:ghaGuardSourceTree,
        ProducerRevision:l.hint.producerRevision,
        WorkflowPath:".github/workflows/unattended-readiness.yml",
        Event:"workflow_dispatch",Ref:"refs/heads/main",WorkflowID:ghaFreshWorkflowID,
        RunID:l.hint.runID,RunAttempt:int(l.hint.attempt),JobID:l.jobID,
        CheckRunID:l.checkRunID,JobName:"Guard bootstrap artifact",
        ArtifactID:l.artifactID,
        ArtifactName:fmt.Sprintf("guard-bootstrap-%d-%d-%d",l.hint.runID,l.hint.attempt,l.jobID),
    },nil
}

// Only the fixed repository and exact code-owned generation may receive a
// credential. Callers cannot supply a URL, token, output hash or target path.
func ghaFreshCredentialAllowed(req *http.Request,l ghaFreshPublicLocator,now time.Time) bool {
    if !ghaFreshLocatorValid(l,now)||req==nil||req.URL==nil||req.Method!=http.MethodGet||
        (req.Body!=nil&&req.Body!=http.NoBody) {return false}
    u:=req.URL
    if u.Scheme!="https"||u.Host!="api.github.com"||u.User!=nil||u.Opaque!=""||
        u.Fragment!=""||u.RawPath!=""||u.ForceQuery||
        (req.Host!=""&&req.Host!="api.github.com") {return false}
    for name,values:=range req.Header {
        if len(values)!=1{return false}
        switch strings.ToLower(name) {
        case "accept":
            if values[0]!="application/vnd.github+json"&&values[0]!="application/vnd.github.raw+json"{return false}
        case "x-github-api-version":
            if values[0]!="2022-11-28"{return false}
        default:return false
        }
    }
    if u.Path=="/user" {return u.RawQuery==""}
    const base="/repos/AutoDevJapan/cc-pc-tools-rs"
    p:=strings.TrimPrefix(u.Path,base)
    if p==u.Path{return false}
    if p=="/actions/workflows/382798887/runs" {
        if u.RawQuery==ghaFreshListingQuery{return true}
        suffix:=strings.TrimPrefix(u.RawQuery,ghaFreshListingQuery+"&page=")
        if suffix==u.RawQuery{return false};page,err:=strconv.Atoi(suffix)
        return err==nil&&page>=2&&page<=10&&strconv.Itoa(page)==suffix
    }
    if strings.HasPrefix(p,"/git/commits/") {return u.RawQuery==""&&ghaGuardHex(strings.TrimPrefix(p,"/git/commits/"),40)}
    if strings.HasPrefix(p,"/compare/") {
        pair:=strings.Split(strings.TrimPrefix(p,"/compare/"),"...")
        return u.RawQuery==""&&len(pair)==2&&ghaGuardHex(pair[0],40)&&ghaGuardHex(pair[1],40)
    }
    switch p {
    case "", "/git/ref/heads/main", "/actions/workflows/unattended-readiness.yml",
         fmt.Sprintf("/actions/runs/%d",l.hint.runID),
         fmt.Sprintf("/actions/jobs/%d/logs",l.jobID),
         fmt.Sprintf("/actions/artifacts/%d/zip",l.artifactID):
        return u.RawQuery==""
    case fmt.Sprintf("/actions/runs/%d/attempts/%d/jobs",l.hint.runID,l.hint.attempt),
         fmt.Sprintf("/actions/runs/%d/artifacts",l.hint.runID):
        return u.RawQuery=="per_page=100"
    }
    for _,prefix:=range []string{"/git/trees/","/git/blobs/"} {
        if strings.HasPrefix(p,prefix) {
            return u.RawQuery==""&&ghaGuardHex(strings.TrimPrefix(p,prefix),40)
        }
    }
    return false
}

func ghaFreshArtifactMatches(l ghaFreshPublicLocator,a ghaGuardArtifact,now time.Time) bool {
    return ghaFreshLocatorValid(l,now)&&a.ID==l.artifactID&&a.RunID==l.hint.runID&&
        a.RepositoryID==1406818713&&a.HeadRepositoryID==1406818713&&
        a.HeadSHA==l.hint.producerRevision&&a.Size==l.archiveBytes&&
        a.Digest=="sha256:"+l.archiveSHA&&!a.Expired&&a.ExpiresAt.Equal(l.expiresAt)&&
        a.ExpiresAt.After(now)
}

// Select a bounded untrusted signed bundle by the publication's ID. Both
// subjects, the certificate identity and the trusted transparency evidence are
// checked later before this data can contribute to any issued candidate.
func ghaFreshBundleFromJSON(raw []byte,pub ghaGuardPublicationRecord) ([]byte,error) {
    if !ghaGuardHex(pub.OutputSHA,64)||len(pub.AttestationID)==0||len(pub.AttestationID)>128 {
        return nil,errGHAGuardUnqualified
    }
    for _,r:=range pub.AttestationID {
        if !((r>='a'&&r<='z')||(r>='A'&&r<='Z')||(r>='0'&&r<='9')||r=='_'||r=='-'){
            return nil,errGHAGuardUnqualified
        }
    }
    outer,err:=ghaFreshObject(raw);if err!=nil{return nil,errGHAGuardUnqualified}
    items,ok:=ghaFreshArray(outer,"attestations");if !ok{return nil,errGHAGuardUnqualified}
    var selected json.RawMessage
    matches:=0
    for _,item:=range items {
        obj,err:=ghaFreshObject(item);if err!=nil{return nil,errGHAGuardUnqualified}
        id,ok:=ghaFreshInt(obj,"repository_id");if !ok{return nil,errGHAGuardUnqualified}
        location,ok:=ghaFreshString(obj,"bundle_url");if !ok{return nil,errGHAGuardUnqualified}
        u,err:=url.Parse(location)
        if err!=nil||u.Scheme!="https"||u.Host==""||u.User!=nil||u.Fragment!="" {return nil,errGHAGuardUnqualified}
        if id!=1406818713||!strings.HasSuffix(u.Path,"/"+pub.AttestationID+".json.sn"){continue}
        data,exists:=obj["bundle"]
        if !exists||len(data)==0||bytes.Equal(data,[]byte("null")){return nil,errGHAGuardUnqualified}
        selected=data;matches++
    }
    if matches!=1{return nil,errGHAGuardUnqualified}
    var compact bytes.Buffer
    if json.Compact(&compact,selected)!=nil||compact.Len()==0||compact.Len()>4<<20{return nil,errGHAGuardUnqualified}
    result:=append([]byte(nil),compact.Bytes()...)
    clear(compact.Bytes())
    return result,nil
}

func ghaFreshReadPublicBundle(ctx context.Context,client *http.Client,pub ghaGuardPublicationRecord) ([]byte,error) {
    if ctx==nil||ctx.Err()!=nil||client==nil||!ghaGuardHex(pub.OutputSHA,64){return nil,errGHAGuardUnqualified}
    endpoint:="https://api.github.com/repos/AutoDevJapan/cc-pc-tools-rs/attestations/sha256:"+pub.OutputSHA+"?per_page=100"
    bounded,cancel:=context.WithTimeout(ctx,30*time.Second);defer cancel()
    request,err:=http.NewRequestWithContext(bounded,http.MethodGet,endpoint,nil)
    if err!=nil{return nil,errGHAGuardUnqualified}
    request.Header.Set("Accept","application/vnd.github+json")
    request.Header.Set("X-GitHub-Api-Version","2022-11-28")
    isolated:=*client;isolated.Jar=nil
    isolated.CheckRedirect=func(*http.Request,[]*http.Request)error{return http.ErrUseLastResponse}
    response,err:=isolated.Do(request)
    if err!=nil||response==nil||response.Body==nil {
        var closeErr error;if response!=nil&&response.Body!=nil{closeErr=response.Body.Close()}
        return nil,errors.Join(ghaGuardResponseFailure(errGHAGuardUnqualified,closeErr,bounded.Err()),ghaFreshContextFailure(bounded,err))
    }
    if response.StatusCode!=http.StatusOK {
        primary:=errors.Join(errGHAGuardUnqualified,&ghaFreshHTTPStatusError{phase:"attestation",status:response.StatusCode})
        return nil,ghaGuardResponseFailure(primary,response.Body.Close(),bounded.Err())
    }
    if !ghaGuardSinglePageLink(response.Header){return nil,ghaGuardResponseFailure(errGHAGuardUnqualified,response.Body.Close(),bounded.Err())}
    raw,readErr:=io.ReadAll(io.LimitReader(response.Body,ghaFreshListingLimit+1))
    closeErr:=response.Body.Close();defer clear(raw)
    if readErr!=nil||closeErr!=nil||len(raw)>ghaFreshListingLimit||bounded.Err()!=nil{return nil,errors.Join(ghaGuardResponseFailure(errGHAGuardUnqualified,closeErr,bounded.Err()),ghaFreshContextFailure(bounded,readErr))}
    return ghaFreshBundleFromJSON(raw,pub)
}

func ghaFreshManifestMatches(l ghaFreshPublicLocator,m ghaGuardManifest,payload []byte,now time.Time) bool {
    return ghaFreshLocatorValid(l,now)&&m.RepositoryID==1406818713&&m.Source==ghaGuardSource&&
        m.SourceTree==ghaGuardSourceTree&&m.ProducerRevision==l.hint.producerRevision&&
        m.RunID==l.hint.runID&&m.RunAttempt==int(l.hint.attempt)&&m.JobID==l.jobID&&
        m.CheckRunID==l.checkRunID&&m.OutputName=="guard.exe"&&m.OutputBytes==int64(len(payload))&&
        len(payload)>0&&len(payload)<=ghaGuardPayloadLimit&&ghaGuardHex(m.SourceManifestSHA,64)&&
        ghaGuardHex(m.OutputSHA,64)&&digest(payload)==m.OutputSHA
}

// The test seam supplies only trust roots. It cannot issue an update grant.
// The production caller fetches the trusted root itself.
func ghaFreshVerifySignatureWithTrusted(ctx context.Context,trusted root.TrustedMaterial,l ghaFreshPublicLocator,rawBundle,payload []byte,m ghaGuardManifest) error {
    if ctx==nil||ctx.Err()!=nil||trusted==nil||len(rawBundle)==0||len(rawBundle)>4<<20||
        !ghaFreshManifestMatches(l,m,payload,time.Now().UTC()) {return errGHAGuardUnqualified}
    var signed bundle.Bundle
    if signed.UnmarshalJSON(rawBundle)!=nil||signed.Bundle==nil||signed.Bundle.GetDsseEnvelope()==nil{return errGHAGuardUnqualified}
    signer:="https://github.com/AutoDevJapan/cc-pc-tools-rs/.github/workflows/unattended-readiness.yml@refs/heads/main"
    san,err:=verify.NewSANMatcher(signer,"");if err!=nil{return errGHAGuardUnqualified}
    issuer,err:=verify.NewIssuerMatcher("https://token.actions.githubusercontent.com","");if err!=nil{return errGHAGuardUnqualified}
    invocation:="https://github.com/AutoDevJapan/cc-pc-tools-rs/actions/runs/"+strconv.FormatInt(l.hint.runID,10)+"/attempts/"+strconv.FormatInt(l.hint.attempt,10)
    extensions:=certificate.Extensions{
        BuildSignerURI:signer,BuildSignerDigest:l.hint.producerRevision,
        BuildConfigURI:signer,BuildConfigDigest:l.hint.producerRevision,
        RunnerEnvironment:"github-hosted",
        SourceRepositoryURI:"https://github.com/AutoDevJapan/cc-pc-tools-rs",
        SourceRepositoryIdentifier:"1406818713",SourceRepositoryDigest:l.hint.producerRevision,
        SourceRepositoryRef:"refs/heads/main",RunInvocationURI:invocation,
        BuildTrigger:"workflow_dispatch",SourceRepositoryVisibilityAtSigning:"public",
    }
    identity,err:=verify.NewCertificateIdentity(san,issuer,extensions);if err!=nil{return errGHAGuardUnqualified}
    verifier,err:=verify.NewVerifier(trusted,verify.WithSignedCertificateTimestamps(1),verify.WithTransparencyLog(1),verify.WithObserverTimestamps(1))
    if err!=nil{return errGHAGuardUnqualified}
    for _,expected:=range []string{m.OutputSHA,m.SourceManifestSHA} {
        value,err:=hex.DecodeString(expected);if err!=nil{return errGHAGuardUnqualified}
        result,err:=verifier.Verify(&signed,verify.NewPolicy(verify.WithArtifactDigest("sha256",value),verify.WithCertificateIdentity(identity)))
        if err!=nil||result==nil||result.Statement==nil||result.Signature==nil||result.Signature.Certificate==nil||
            result.Statement.GetType()!="https://in-toto.io/Statement/v1"||result.Statement.GetPredicateType()!="https://slsa.dev/provenance/v1"||ctx.Err()!=nil{return errGHAGuardUnqualified}
        subjects:=result.Statement.GetSubject()
        if len(subjects)!=2||subjects[0]==nil||subjects[1]==nil||
            subjects[0].GetName()!="guard.exe"||subjects[0].GetDigest()["sha256"]!=m.OutputSHA||
            subjects[1].GetName()!="source-proof.json"||subjects[1].GetDigest()["sha256"]!=m.SourceManifestSHA{return errGHAGuardUnqualified}
    }
    if !ghaFreshLocatorValid(l,time.Now().UTC())||ctx.Err()!=nil{return errGHAGuardUnqualified}
    return nil
}

func ghaFreshVerifyActualSignature(ctx context.Context,l ghaFreshPublicLocator,rawBundle,payload []byte,m ghaGuardManifest) error {
    if ctx==nil||ctx.Err()!=nil||!ghaFreshLocatorValid(l,time.Now().UTC()){return errGHAGuardUnqualified}
    opts:=tuf.DefaultOptions().WithDisableLocalCache().WithContext(ctx).WithFetcher(ghaGuardNewTUFRequestFetcher(ctx,&http.Client{}))
    trusted,err:=root.FetchTrustedRootWithOptions(opts)
    if err!=nil||trusted==nil||ctx.Err()!=nil{return errGHAGuardUnqualified}
    return ghaFreshVerifySignatureWithTrusted(ctx,trusted,l,rawBundle,payload,m)
}


// Repeat the run/actor check using the same owned authenticated client.
// The run hint is never promoted into an authority by itself.
func ghaFreshAuthenticatedRunMatches(ctx context.Context,client *http.Client,h ghaFreshRunHint) error {
    if ctx==nil||ctx.Err()!=nil||client==nil||!ghaFreshHintShape(h){return errGHAGuardUnqualified}
    endpoint:=fmt.Sprintf("https://api.github.com/repos/AutoDevJapan/cc-pc-tools-rs/actions/runs/%d",h.runID)
    req,err:=http.NewRequestWithContext(ctx,http.MethodGet,endpoint,nil);if err!=nil{return errGHAGuardUnqualified}
    req.Header.Set("Accept","application/vnd.github+json")
    req.Header.Set("X-GitHub-Api-Version","2022-11-28")
    isolated:=*client;isolated.Jar=nil
    isolated.CheckRedirect=func(*http.Request,[]*http.Request)error{return http.ErrUseLastResponse}
    response,err:=isolated.Do(req)
    if err!=nil||response==nil||response.Body==nil {
        var closeErr error;if response!=nil&&response.Body!=nil{closeErr=response.Body.Close()}
        return errors.Join(ghaGuardResponseFailure(errGHAGuardUnqualified,closeErr,ctx.Err()),ghaFreshContextFailure(ctx,err))
    }
    if response.StatusCode!=http.StatusOK {
        primary:=errors.Join(ghaGuardHTTPFailure("authenticated_run",response.StatusCode),ghaGuardCredentialStatusError(response.StatusCode))
        return ghaGuardResponseFailure(primary,response.Body.Close(),ctx.Err())
    }
    if !ghaGuardSinglePageLink(response.Header) {return ghaGuardResponseFailure(errGHAGuardUnqualified,response.Body.Close(),ctx.Err())}
    raw,readErr:=io.ReadAll(io.LimitReader(response.Body,ghaFreshListingLimit+1))
    closeErr:=response.Body.Close();defer clear(raw)
    if readErr!=nil||closeErr!=nil||len(raw)>ghaFreshListingLimit||ctx.Err()!=nil{return errors.Join(ghaGuardResponseFailure(errGHAGuardUnqualified,closeErr,ctx.Err()),ghaFreshContextFailure(ctx,readErr))}
    var envelope bytes.Buffer
    envelope.WriteString("{\"total_count\":1,\"workflow_runs\":[")
    envelope.Write(raw)
    envelope.WriteString("]}")
    defer clear(envelope.Bytes())
    actual,err:=ghaFreshSelectRunHint(envelope.Bytes())
    if err!=nil||actual!=h||ctx.Err()!=nil{return errGHAGuardUnqualified}
    return nil
}


// This wrapper observes only the fixed source API's HTTP status/close result;
// it delegates every request to the same code-owned credential transport.
var errGHAFreshSourceRead = errors.New("guard_github_source_read_failed")

type ghaFreshSourceStatusTransport struct {
    base http.RoundTripper
    mu sync.Mutex
    status int
    conflicting,closeFailed,canceled,deadline,readFailed bool
}

type ghaFreshSourceStatusBody struct {
    body io.ReadCloser
    owner *ghaFreshSourceStatusTransport
    ctx context.Context
    once sync.Once
    closeErr error
}

// Classify bounded error leaves separately. A joined context/cleanup marker
// does not erase a distinct I/O failure, and pure control errors are not reads.
// Only fixed facts escape; neither wrappers nor their messages are retained.
type ghaFreshReadErrorFacts struct {deadline,canceled,cleanup,read bool}
func ghaFreshClassifyReadError(err error) ghaFreshReadErrorFacts {
    var facts ghaFreshReadErrorFacts
    if err==nil{return facts}
    pending:=[]error{err};visited:=0
    for len(pending)>0 {
        if visited>=128 {facts.read=true;break};visited++
        item:=pending[len(pending)-1];pending=pending[:len(pending)-1]
        if item==nil{continue}
        if many,ok:=item.(interface{Unwrap() []error});ok {
            children:=many.Unwrap();added:=false
            for _,child:=range children {
                if child==nil{continue}
                if len(pending)>=128-visited {facts.read=true;break}
                pending=append(pending,child);added=true
            }
            if !added{facts.read=true};continue
        }
        if one,ok:=item.(interface{Unwrap() error});ok {if child:=one.Unwrap();child!=nil {pending=append(pending,child);continue}}
        switch {
        case errors.Is(item,io.EOF):
        case errors.Is(item,context.DeadlineExceeded):facts.deadline=true
        case errors.Is(item,context.Canceled):facts.canceled=true
        case errors.Is(item,errGHAGuardResponseCleanup):facts.cleanup=true
        default:facts.read=true
        }
    }
    return facts
}

func (b *ghaFreshSourceStatusBody) Read(p []byte)(int,error){
    n,err:=b.body.Read(p)
    if err!=nil {
        facts:=ghaFreshClassifyReadError(err)
        b.owner.mu.Lock()
        if facts.deadline{b.owner.deadline=true}
        if facts.canceled&&(b.ctx==nil||b.ctx.Err()==nil){b.owner.canceled=true}
        if facts.cleanup{b.owner.closeFailed=true}
        if facts.read{b.owner.readFailed=true}
        b.owner.mu.Unlock()
    }
    return n,err
}
func (b *ghaFreshSourceStatusBody) Close() error {
    b.once.Do(func(){
        b.closeErr=b.body.Close()
        if b.closeErr!=nil {b.owner.mu.Lock();b.owner.closeFailed=true;b.owner.mu.Unlock()}
    })
    return b.closeErr
}

func (t *ghaFreshSourceStatusTransport) RoundTrip(req *http.Request)(*http.Response,error) {
    if t==nil||t.base==nil||req==nil||req.URL==nil{return nil,errGHAGuardUnqualified}
    response,err:=t.base.RoundTrip(req)
    const repo="/repos/AutoDevJapan/cc-pc-tools-rs"
    sourceEndpoint:=req.URL.Scheme=="https"&&req.URL.Host=="api.github.com"&&
        req.URL.RawQuery==""&&(req.URL.Path==repo||strings.HasPrefix(req.URL.Path,repo+"/git/")||strings.HasPrefix(req.URL.Path,repo+"/compare/"))
    if sourceEndpoint&&err!=nil {
        t.mu.Lock()
        if errors.Is(err,errGHAGuardResponseCleanup){t.closeFailed=true}
        if errors.Is(err,context.DeadlineExceeded){t.deadline=true}
        // Sibling requests canceled after a parallel failure are not a new
        // caller cancellation. A live request's own cancellation is retained.
        if errors.Is(err,context.Canceled)&&req.Context().Err()==nil {t.canceled=true}
        t.mu.Unlock()
    }
    if sourceEndpoint&&response!=nil {
        if response.StatusCode!=http.StatusOK&&response.StatusCode>=100&&response.StatusCode<=599 {
            t.mu.Lock()
            if t.status!=0&&t.status!=response.StatusCode {t.conflicting=true}
            if t.status==0 {t.status=response.StatusCode}
            t.mu.Unlock()
        }
        if response.Body!=nil {response.Body=&ghaFreshSourceStatusBody{body:response.Body,owner:t,ctx:req.Context()}}
    }
    return response,err
}

func (t *ghaFreshSourceStatusTransport) sourceFailure(ctx context.Context) error {
    if t==nil{return errGHAGuardUnqualified}
    t.mu.Lock();status,conflicting,closeFailed,canceled,deadline,readFailed:=t.status,t.conflicting,t.closeFailed,t.canceled,t.deadline,t.readFailed;t.mu.Unlock()
    primary:=error(errGHAGuardUnqualified)
    if status!=0&&!conflicting {primary=ghaGuardHTTPFailure("source_metadata",status)}
    if readFailed {primary=errors.Join(primary,errGHAFreshSourceRead)}
    var contextErr error;if ctx!=nil {contextErr=ctx.Err()}
    if canceled {contextErr=errors.Join(contextErr,context.Canceled)}
    if deadline {contextErr=errors.Join(contextErr,context.DeadlineExceeded)}
    if closeFailed{return errors.Join(primary,errGHAGuardResponseCleanup,contextErr)}
    return errors.Join(primary,contextErr)
}

func (t *ghaFreshSourceStatusTransport) observedFailure() bool {
    if t==nil{return true}
    t.mu.Lock();defer t.mu.Unlock()
    return t.status!=0||t.conflicting||t.closeFailed||t.canceled||t.deadline||t.readFailed
}

func ghaFreshVerifySourceWithStatus(ctx context.Context,authority *ghaGuardEvidenceAuthority,client *http.Client,archive []byte,m ghaGuardManifest) error {
    if ctx==nil||ctx.Err()!=nil||authority==nil||authority.client!=client||client==nil||client.Transport==nil{return errGHAGuardUnqualified}
    tracked:=&ghaFreshSourceStatusTransport{base:client.Transport}
    same:=*client;same.Transport=tracked
    err:=ghaGuardVerifyArchiveGitSourceViaPublicArchiveWithClient(ctx,archive,m,&same)
    if err!=nil||ctx.Err()!=nil||authority.client!=client||tracked.observedFailure(){return tracked.sourceFailure(ctx)}
    return nil
}

// This source-only candidate needs the existing job-object supervisor around
// the complete operation: Windows CredReadW is synchronous and not cancellable.
func ghaFreshOpenVerifiedCandidate(ctx context.Context) (*ghaGuardVerifiedCandidate,error) {
    return ghaFreshOpenVerifiedCandidateObserved(ctx,nil)
}

func ghaFreshOpenVerifiedCandidateObserved(ctx context.Context,audit *ghaFreshCredentialAudit) (*ghaGuardVerifiedCandidate,error) {
    return ghaFreshOpenVerifiedCandidateCaptured(ctx,audit,nil)
}

// The optional capture is private to the fixed update worker. It holds no
// authority and is erased if acquisition or credential cleanup fails.
func ghaFreshOpenVerifiedCandidateCaptured(ctx context.Context,audit *ghaFreshCredentialAudit,capture *ghaFreshUpdateArchiveCapture) (result *ghaGuardVerifiedCandidate,resultErr error) {
    if capture!=nil {defer func(){if result==nil||resultErr!=nil {resultErr=errors.Join(resultErr,capture.Close())}}()}
    if audit!=nil {audit.state="unknown"}
    if ctx==nil{return nil,errGHAGuardUnqualified}
    if ctx.Err()!=nil{return nil,errors.Join(errGHAGuardUnqualified,ctx.Err())}
    transport:=&http.Transport{Proxy:nil,TLSHandshakeTimeout:10*time.Second,ResponseHeaderTimeout:15*time.Second,MaxResponseHeaderBytes:64<<10}
    defer transport.CloseIdleConnections()
    public:=&http.Client{Transport:transport,Timeout:30*time.Second}
    authority,err:=ghaFreshOpenWinCredDiscovery(ctx,audit)
    if authority==nil{return nil,errors.Join(errGHAGuardUnqualified,err)}
    defer func(){
        if closeErr:=ghaFreshCloseCredentialAuthority(authority,nil,audit);closeErr!=nil {
            if result!=nil {
                cleanup,cancel:=context.WithTimeout(context.Background(),5*time.Second)
                resultErr=errors.Join(resultErr,result.Close(),result.WaitClosed(cleanup));cancel()
            }
            result=nil;resultErr=errors.Join(errGHAGuardUnqualified,resultErr,closeErr)
        }
    }()
    if err!=nil||authority.client==nil{return nil,errors.Join(errGHAGuardUnqualified,err)}
    loc,err:=ghaFreshDiscoverOwned(ctx,authority,time.Now().UTC());if err!=nil{return nil,err}
    if capture!=nil {if err=capture.Start(loc);err!=nil{return nil,err}}
    return ghaFreshAcquireFromAuthorityCaptured(ctx,authority,public,loc,capture)
}

// A test seam for fixed transports. Production obtains the authority above;
// no caller-supplied URL, token or result can issue a candidate through CLI.
func ghaFreshAcquireFromAuthority(ctx context.Context,authority *ghaGuardEvidenceAuthority,public *http.Client,loc ghaFreshPublicLocator) (*ghaGuardVerifiedCandidate,error) {
    return ghaFreshAcquireFromAuthorityCaptured(ctx,authority,public,loc,nil)
}

func ghaFreshAcquireFromAuthorityCaptured(ctx context.Context,authority *ghaGuardEvidenceAuthority,public *http.Client,loc ghaFreshPublicLocator,capture *ghaFreshUpdateArchiveCapture) (result *ghaGuardVerifiedCandidate,resultErr error) {
    if capture!=nil {defer func(){if result==nil||resultErr!=nil {resultErr=errors.Join(resultErr,capture.Close())}}()}
    if ctx==nil{return nil,errGHAGuardUnqualified}
    if ctx.Err()!=nil||authority==nil||authority.client==nil||public==nil{return nil,errors.Join(errGHAGuardUnqualified,ctx.Err())}
    e,err:=ghaFreshExpectation(loc,time.Now().UTC());if err!=nil{return nil,err}
    client:=authority.client
    if err:=ghaFreshAuthenticatedRunMatches(ctx,client,loc.hint);err!=nil{return nil,err}
    endpoint:=fmt.Sprintf("https://api.github.com/repos/AutoDevJapan/cc-pc-tools-rs/actions/jobs/%d/logs",loc.jobID)
    log,err:=ghaGuardLiveRedirectedDetailed(ctx,client,public,endpoint,8<<20)
    if err!=nil{return nil,err}
    pub,err:=ghaGuardPublicationFromLog(log);clear(log)
    if err!=nil{return nil,errGHAGuardUnqualified}
    binding:=ghaGuardCaptureBinding{RepositoryID:pub.RepositoryID,Source:pub.Source,ProducerRevision:pub.ProducerRevision,RunID:pub.RunID,RunAttempt:pub.RunAttempt,JobID:pub.JobID,CheckRunID:pub.CheckRunID,ArtifactID:pub.ArtifactID}
    evidence,err:=ghaGuardFetchServerEvidenceDetailed(ctx,client,e,binding,time.Now().UTC())
    if err!=nil{return nil,err}
    if evidence==nil||!ghaFreshArtifactMatches(loc,evidence.artifact,time.Now().UTC())||
        !ghaGuardPublicationMatches(e,evidence.artifact,pub){return nil,errGHAGuardUnqualified}
    bundleBytes,err:=ghaFreshReadPublicBundle(ctx,public,pub)
    if err!=nil{return nil,err};defer clear(bundleBytes)
    material,err:=ghaGuardLiveAcquireWithDetailed(ctx,authority,public,e,bundleBytes,pub.AttestationID)
    if err!=nil{return nil,err}
    if material==nil{return nil,errGHAGuardUnqualified}
    second,secondErr:=ghaGuardPublicationFromLog(material.jobLog)
    if secondErr!=nil||second!=pub||authority.client!=client||
        !ghaFreshArtifactMatches(loc,material.artifact,time.Now().UTC()) {
        clear(material.archive);clear(material.bundle);clear(material.jobLog)
        return nil,errGHAGuardUnqualified
    }
    candidate,err:=ghaGuardConstructVerifiedCandidateWithDetailed(ctx,e,material,
        func(c context.Context,b,p []byte,m ghaGuardManifest)error{return ghaFreshVerifyActualSignature(c,loc,b,p,m)},
        func(c context.Context,a []byte,m ghaGuardManifest)error{
            if authority.client!=client{return errGHAGuardUnqualified}
            if err:=ghaFreshVerifySourceWithStatus(c,authority,client,a,m);err!=nil{return err}
            if err:=ghaFreshVerifyCurrentArchiveInputs(c,public,authority,a,m);err!=nil{return err}
            if capture!=nil {return capture.Hold(a,m)}
            return nil
        })
    if err!=nil {
        if candidate!=nil{return nil,errors.Join(err,candidate.Close())}
        return nil,err
    }
    if candidate==nil{return nil,errGHAGuardUnqualified}
    generation,valid:=candidate.Generation()
    if ctx.Err()!=nil||authority.client!=client||!ghaFreshLocatorValid(loc,time.Now().UTC())||!valid||!ghaFreshAuthorityMatches(authority,generation) {
        closeErr:=candidate.Close();return nil,errors.Join(errGHAGuardUnqualified,closeErr,ctx.Err())
    }
    if err:=ghaFreshCheckHintGeneration(ctx,public,client,generation,loc);err!=nil {
        closeErr:=candidate.Close();return nil,errors.Join(errGHAGuardUnqualified,err,closeErr)
    }
    return candidate,nil
}
