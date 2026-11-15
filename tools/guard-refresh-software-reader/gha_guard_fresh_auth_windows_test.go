//go:build windows

package main

import (
    "context"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "net/http"
    "strings"
    "testing"
    "time"
)

func ghaFreshTestLocator() ghaFreshPublicLocator {
    return ghaFreshPublicLocator{
        hint:ghaFreshRunHint{runID:1201,attempt:1,producerRevision:ghaFreshProducerRevision},
        jobID:2202,checkRunID:3303,artifactID:4404,
        archiveBytes:90,archiveSHA:strings.Repeat("a",64),
        expiresAt:time.Date(2099,1,1,0,0,0,0,time.UTC),
    }
}

func TestGHAFreshCredentialBoundToExactGeneration(t *testing.T) {
    now:=time.Date(2026,1,1,0,0,0,0,time.UTC)
    loc:=ghaFreshTestLocator()
    base:="https://api.github.com/repos/AutoDevJapan/cc-pc-tools-rs"
    for _,endpoint:=range []string{
        "https://api.github.com/user",
        base,base+"/actions/workflows/unattended-readiness.yml",
        base+"/actions/runs/1201",
        base+"/actions/runs/1201/attempts/1/jobs?per_page=100",
        base+"/actions/runs/1201/artifacts?per_page=100",
        base+"/actions/jobs/2202/logs",
        base+"/actions/artifacts/4404/zip",
        base+"/git/commits/"+ghaGuardSource,
        base+"/git/trees/"+strings.Repeat("a",40),
        base+"/git/blobs/"+strings.Repeat("b",40),
    } {
        req,err:=http.NewRequest(http.MethodGet,endpoint,nil)
        if err!=nil||!ghaFreshCredentialAllowed(req,loc,now){t.Fatalf("fixed request rejected %s",endpoint)}
    }
    for _,endpoint:=range []string{
        base+"/actions/runs/1202",
        base+"/actions/runs/1201/attempts/2/jobs?per_page=100",
        base+"/actions/runs/1201/artifacts?per_page=10",
        base+"/actions/jobs/2203/logs",
        base+"/actions/artifacts/4405/zip",
        base+"/actions/artifacts/4404/zip?token=bad",
        "https://other.invalid/repos/AutoDevJapan/cc-pc-tools-rs/actions/runs/1201",
        base+"/git/trees/../../secrets",
        base+"/git/blobs/"+strings.Repeat("z",40),
    } {
        req,err:=http.NewRequest(http.MethodGet,endpoint,nil)
        if err==nil&&ghaFreshCredentialAllowed(req,loc,now){t.Fatalf("foreign request accepted %s",endpoint)}
    }
    req,_:=http.NewRequest(http.MethodGet,base+"/actions/runs/1201",nil)
    req.Header.Set("Authorization","caller-value")
    if ghaFreshCredentialAllowed(req,loc,now){t.Fatal("caller authorization accepted")}
    req.Header.Del("Authorization");req.Method=http.MethodPost
    if ghaFreshCredentialAllowed(req,loc,now){t.Fatal("non-GET accepted")}
    req.Method=http.MethodGet
    if ghaFreshCredentialAllowed(req,loc,loc.expiresAt){t.Fatal("expired generation accepted")}
    other:=loc;other.hint.producerRevision=strings.Repeat("a",40)
    if ghaFreshCredentialAllowed(req,other,now){t.Fatal("foreign producer accepted")}
}

func TestGHAFreshExpiredPreflightDoesNotReadCredential(t *testing.T) {
    loc:=ghaFreshTestLocator();loc.expiresAt=time.Now().Add(-time.Second)
    if got,err:=ghaFreshOpenWinCredAuthority(context.Background(),loc);got!=nil||err==nil {
        t.Fatal("expired locator admitted")
    }
}

func TestGHAFreshBundleSelectionIsNotAnAuthority(t *testing.T) {
    pub:=ghaGuardPublicationRecord{OutputSHA:strings.Repeat("a",64),AttestationID:"77"}
    item:=func(repo int64,id string,raw string)string {
        return fmt.Sprintf("{\"repository_id\":%d,\"bundle_url\":%q,\"bundle\":%s}",repo,"https://example.invalid/"+id+".json.sn",raw)
    }
    valid:=[]byte("{\"attestations\":["+item(1406818713,"77",`{"a":1}`)+"]}")
    out,err:=ghaFreshBundleFromJSON(valid,pub)
    if err!=nil||string(out)!=`{"a":1}`{t.Fatalf("expected bytes, got %q / %v",out,err)}
    defer clear(out)
    cases:=[][]byte{
        []byte("{\"attestations\":["+item(1406818713,"77",`{"a":1}`)+","+item(1406818713,"77",`{"a":1}`)+"]}"),
        []byte("{\"attestations\":["+item(1,"77",`{"a":1}`)+"]}"),
        []byte("{\"attestations\":["+item(1406818713,"78",`{"a":1}`)+"]}"),
        []byte("{\"attestations\":["+item(1406818713,"77","null")+"]}"),
        []byte(`{"attestations":[],"attestations":[]}`),
        []byte(`{"attestations":null}`),
    }
    for i,input:=range cases {
        if result,err:=ghaFreshBundleFromJSON(input,pub);err==nil||result!=nil{t.Fatalf("invalid bundle %d accepted",i)}
    }
    // Selector output by itself is not an issued candidate or signature proof.
    if err:=ghaFreshVerifySignatureWithTrusted(context.Background(),nil,ghaFreshTestLocator(),out,[]byte("x"),ghaGuardManifest{});err==nil{
        t.Fatal("bundle selection bypassed signature validation")
    }
}

func TestGHAFreshManifestAndArtifactGenerationBinding(t *testing.T) {
    now:=time.Date(2026,1,1,0,0,0,0,time.UTC)
    loc:=ghaFreshTestLocator()
    payload:=[]byte("synthetic")
    manifest:=ghaGuardManifest{
        RepositoryID:1406818713,Source:ghaGuardSource,SourceTree:ghaGuardSourceTree,
        ProducerRevision:ghaFreshProducerRevision,RunID:loc.hint.runID,RunAttempt:1,
        JobID:loc.jobID,CheckRunID:loc.checkRunID,OutputName:"guard.exe",
        OutputBytes:int64(len(payload)),OutputSHA:digest(payload),SourceManifestSHA:strings.Repeat("b",64),
    }
    if !ghaFreshManifestMatches(loc,manifest,payload,now){t.Fatal("consistent observation rejected")}
    for _,mutate:=range []func(*ghaGuardManifest){
        func(m *ghaGuardManifest){m.RunID++},func(m *ghaGuardManifest){m.JobID++},
        func(m *ghaGuardManifest){m.CheckRunID++},func(m *ghaGuardManifest){m.RunAttempt++},
        func(m *ghaGuardManifest){m.ProducerRevision=strings.Repeat("f",40)},
        func(m *ghaGuardManifest){m.Source=strings.Repeat("e",40)},
        func(m *ghaGuardManifest){m.OutputSHA=strings.Repeat("c",64)},
        func(m *ghaGuardManifest){m.OutputBytes++},
    } {
        v:=manifest;mutate(&v)
        if ghaFreshManifestMatches(loc,v,payload,now){t.Fatal("mismatched manifest accepted")}
    }
    a:=ghaGuardArtifact{RepositoryID:1406818713,HeadRepositoryID:1406818713,
        HeadSHA:ghaFreshProducerRevision,RunID:loc.hint.runID,ID:loc.artifactID,
        Size:loc.archiveBytes,Digest:"sha256:"+loc.archiveSHA,ExpiresAt:loc.expiresAt}
    if !ghaFreshArtifactMatches(loc,a,now){t.Fatal("consistent artifact rejected")}
    a.Digest="sha256:"+strings.Repeat("b",64)
    if ghaFreshArtifactMatches(loc,a,now){t.Fatal("different archive accepted")}
}

// Test transport never handles a real token or accesses the network.
type ghaFreshTestRoundTrip func(*http.Request)(*http.Response,error)
func (f ghaFreshTestRoundTrip) RoundTrip(r *http.Request)(*http.Response,error){return f(r)}

func TestGHAFreshBundleRequestClosesAndBounds(t *testing.T) {
    pub:=ghaGuardPublicationRecord{OutputSHA:strings.Repeat("a",64),AttestationID:"77"}
    body:=`{"attestations":[{"repository_id":1406818713,"bundle_url":"https://example.invalid/77.json.sn","bundle":{"a":1}}]}`
    closed:=false
    client:=&http.Client{Transport:ghaFreshTestRoundTrip(func(req *http.Request)(*http.Response,error){
        if req.URL.Host!="api.github.com"||!strings.Contains(req.URL.Path,pub.OutputSHA)||req.Header.Get("Authorization")!=""{
            t.Fatal("unexpected endpoint or credential")
        }
        return &http.Response{StatusCode:200,Header:make(http.Header),Body:&ghaFreshAuthTestBody{Reader:strings.NewReader(body),closed:&closed}},nil
    })}
    out,err:=ghaFreshReadPublicBundle(context.Background(),client,pub)
    if err!=nil||string(out)!=`{"a":1}`||!closed{t.Fatalf("bundle read %q err=%v closed=%v",out,err,closed)}
    clear(out)
    cancelled,cancel:=context.WithCancel(context.Background());cancel()
    if out,err:=ghaFreshReadPublicBundle(cancelled,client,pub);out!=nil||err==nil{t.Fatal("cancelled request accepted")}
}

type ghaFreshAuthTestBody struct{io.Reader;closed *bool}
func (b *ghaFreshAuthTestBody) Close() error {*b.closed=true;return nil}


func TestGHAFreshSessionKeepsTokenOnClonedFixedRequest(t *testing.T) {
    loc:=ghaFreshTestLocator()
    root,cancel:=context.WithCancel(context.Background())
    seen:=0
    base:=ghaFreshTestRoundTrip(func(req *http.Request)(*http.Response,error){
        seen++
        if req.Header.Get("Authorization")!="Bearer synthetic-token"||req.URL.Path!="/repos/AutoDevJapan/cc-pc-tools-rs/actions/runs/1201"{
            t.Fatal("wrong fake transport request")
        }
        return &http.Response{StatusCode:200,Header:make(http.Header),Body:io.NopCloser(strings.NewReader("{}"))},nil
    })
    session:=&ghaGuardCredentialSession{fresh:&loc,secret:[]byte("synthetic-token"),base:base,root:root,cancel:cancel,ready:true,drain:make(chan struct{})}
    req,_:=http.NewRequest(http.MethodGet,"https://api.github.com/repos/AutoDevJapan/cc-pc-tools-rs/actions/runs/1201",nil)
    response,err:=session.RoundTrip(req)
    if err!=nil||response==nil||seen!=1||req.Header.Get("Authorization")!="" {t.Fatal("fake request failed or caller was modified")}
    if response.Body.Close()!=nil{t.Fatal("body close")}
    foreign,_:=http.NewRequest(http.MethodGet,"https://api.github.com/repos/AutoDevJapan/cc-pc-tools-rs/actions/runs/1202",nil)
    if response,err:=session.RoundTrip(foreign);response!=nil||err==nil||seen!=1{t.Fatal("foreign request reached fake transport")}
    if session.close(context.Background())!=nil{t.Fatal("fake session did not drain")}
}

func TestGHAFreshAuthenticatedRunChecksActorAndGeneration(t *testing.T) {
    hint,err:=ghaFreshSelectRunHint([]byte(ghaFreshTestGood));if err!=nil{t.Fatal(err)}
    run:=strings.TrimSuffix(strings.TrimPrefix(ghaFreshTestGood,`{"total_count":1,"workflow_runs":[`),`]}`)
    tests:=[]struct{name,body string;status int;valid bool}{
        {"valid",run,http.StatusOK,true},
        {"foreign-actor",strings.Replace(run,"88025750","88025751",1),http.StatusOK,false},
        {"foreign-trigger",strings.Replace(run,`"triggering_actor":{"id":88025750}`,`"triggering_actor":{"id":88025751}`,1),http.StatusOK,false},
        {"foreign-run",strings.Replace(run,"12345","12346",1),http.StatusOK,false},
        {"duplicate",strings.Replace(run,`"id":12345`,`"id":12345,"id":12345`,1),http.StatusOK,false},
        {"forbidden",run,http.StatusForbidden,false},
    }
    for _,tc:=range tests {t.Run(tc.name,func(t *testing.T){
        closed:=false
        client:=&http.Client{Transport:ghaFreshTestRoundTrip(func(req *http.Request)(*http.Response,error){
            if req.URL.String()!="https://api.github.com/repos/AutoDevJapan/cc-pc-tools-rs/actions/runs/12345"{t.Fatal("wrong authenticated endpoint")}
            return &http.Response{StatusCode:tc.status,Header:make(http.Header),Body:&ghaFreshAuthTestBody{Reader:strings.NewReader(tc.body),closed:&closed}},nil
        })}
        err:=ghaFreshAuthenticatedRunMatches(context.Background(),client,hint)
        if (err==nil)!=tc.valid||!closed{t.Fatalf("valid=%v err=%v closed=%v",tc.valid,err,closed)}
        if tc.status==http.StatusForbidden&&!errors.Is(err,errGHAGuardCredentialForbidden){t.Fatal("forbidden status lost")}
    })}
}

func TestGHAFreshPublicBundlePreservesForbiddenStatus(t *testing.T) {
    pub:=ghaGuardPublicationRecord{OutputSHA:strings.Repeat("a",64),AttestationID:"77"}
    closed:=false
    client:=&http.Client{Transport:ghaFreshTestRoundTrip(func(*http.Request)(*http.Response,error){
        return &http.Response{StatusCode:http.StatusForbidden,Header:make(http.Header),Body:&ghaFreshAuthTestBody{Reader:strings.NewReader("{}"),closed:&closed}},nil
    })}
    raw,err:=ghaFreshReadPublicBundle(context.Background(),client,pub)
    var statusErr *ghaFreshHTTPStatusError
    if raw!=nil||!closed||!errors.As(err,&statusErr)||statusErr==nil||
        statusErr.phase!="attestation"||statusErr.status!=http.StatusForbidden {
        t.Fatal("forbidden response was not preserved")
    }
}

func TestGHAFreshCredentialCloseAuditDistinguishesOutcomes(t *testing.T) {
    root,cancel:=context.WithCancel(context.Background())
    bodyClosed:=false
    base:=ghaFreshTestRoundTrip(func(*http.Request)(*http.Response,error){
        return &http.Response{StatusCode:http.StatusOK,Header:make(http.Header),
            Body:&ghaFreshAuthTestBody{Reader:strings.NewReader("{}"),closed:&bodyClosed}},nil
    })
    session:=&ghaGuardCredentialSession{secret:[]byte("synthetic-token"),base:base,root:root,cancel:cancel,ready:true,drain:make(chan struct{})}
    req,err:=http.NewRequest(http.MethodGet,"https://api.github.com/user",nil);if err!=nil{t.Fatal(err)}
    response,err:=session.RoundTrip(req);if err!=nil||response==nil||response.Body==nil{t.Fatal("synthetic body was not acquired")}
    authority:=&ghaGuardEvidenceAuthority{credential:session}
    audit:=&ghaFreshCredentialAudit{state:"unknown"}
    expired,stop:=context.WithCancel(context.Background());stop()
    err=ghaGuardCloseCredentialAuthorityObserved(authority,nil,expired,audit)
    if !errors.Is(err,errGHAGuardCredentialCleanup)||!errors.Is(err,errGHAGuardUnqualified)||
        audit.state!="failed"||session.secret!=nil||root.Err()==nil||bodyClosed {t.Fatal("pending-body close failure was not reported")}
    if response.Body.Close()!=nil||!bodyClosed{t.Fatal("synthetic body was not released")}
    select {case <-session.drain:default:t.Fatal("synthetic active request did not drain")}
    root2,cancel2:=context.WithCancel(context.Background())
    session2:=&ghaGuardCredentialSession{secret:[]byte("synthetic-token"),root:root2,cancel:cancel2,drain:make(chan struct{})}
    audit2:=&ghaFreshCredentialAudit{state:"unknown"}
    if err:=ghaGuardCloseCredentialAuthorityObserved(&ghaGuardEvidenceAuthority{credential:session2},nil,context.Background(),audit2);err!=nil||audit2.state!="completed"||root2.Err()==nil {t.Fatal("completed close not observed")}
    audit3:=&ghaFreshCredentialAudit{state:"unknown"}
    if err:=ghaGuardCloseCredentialAuthorityObserved(nil,nil,context.Background(),audit3);err==nil||audit3.state!="unknown"{t.Fatal("unacquired credential claimed complete")}
}

type ghaFreshFlowBody struct { io.Reader; closes *int; fail bool }
func (b *ghaFreshFlowBody) Close() error {*b.closes=*b.closes+1;if b.fail{return errors.New("DO_NOT_RETURN")};return nil}

// This starts at the post-owner-authentication driver seam. All transports,
// log bytes and token-like strings are synthetic; no candidate is issued.
func TestGHAFreshPostAuthStatusReachesParent(t *testing.T) {
    cases:=[]struct{name,fail string;status int;phase string}{
        {"job401","first-log",401,"job_log"},
        {"job403","first-log",403,"job_log"},
        {"job403-closefail","first-log-close",403,"job_log"},
        {"job-object429","job-object",429,"job_log_object"},
        {"metadata429","metadata",429,"metadata"},
        {"second-job403","second-log",403,"job_log"},
        {"zip403","artifact",403,"artifact"},
        {"zip429","artifact",429,"artifact"},
        {"zip-object403","artifact-object",403,"artifact_object"},
    }
    for _,tc:=range cases {t.Run(tc.name,func(t *testing.T){
        hint,err:=ghaFreshSelectRunHint([]byte(ghaFreshTestGood));if err!=nil{t.Fatal(err)}
        expiry:=time.Date(2099,1,1,0,0,0,0,time.UTC)
        loc:=ghaFreshPublicLocator{hint:hint,jobID:23456,checkRunID:34567,artifactID:45678,
            archiveBytes:10,archiveSHA:strings.Repeat("a",64),expiresAt:expiry}
        workflow,jobs,artifacts:=ghaFreshLocatorFixture()
        artifacts=strings.Replace(artifacts,"2026-11-16T12:00:00Z",expiry.Format(time.RFC3339),1)
        run:=strings.TrimSuffix(strings.TrimPrefix(ghaFreshTestGood,"{\"total_count\":1,\"workflow_runs\":["),"]}")
        pub:=ghaGuardPublicationRecord{Domain:"guard-gha-publication-v1",RepositoryID:1406818713,
            Source:ghaGuardSource,SourceTree:ghaGuardSourceTree,ProducerRevision:ghaFreshProducerRevision,
            RunID:hint.runID,RunAttempt:1,JobID:loc.jobID,CheckRunID:loc.checkRunID,ArtifactID:loc.artifactID,
            ArtifactName:"guard-bootstrap-12345-1-23456",ArchiveSHA:loc.archiveSHA,
            OutputSHA:strings.Repeat("b",64),CaptureSHA:strings.Repeat("c",64),
            SourceManifestSHA:strings.Repeat("d",64),AttestationID:"77"}
        encoded,err:=json.Marshal(pub);if err!=nil{t.Fatal(err)}
        log:="2026-01-02T03:04:05.000Z guard_gha_publication_v1="+string(encoded)+"\n"
        bundle:="{\"attestations\":[{\"repository_id\":1406818713,\"bundle_url\":\"https://example.invalid/77.json.sn\",\"bundle\":{}}]}"
        apiResponses,objectResponses,closed,logCalls:=0,0,0,0
        api:=&http.Client{Transport:ghaFreshTestRoundTrip(func(req *http.Request)(*http.Response,error){
            if req.URL.Host!="api.github.com"{t.Fatal("wrong API host")}
            req.Header.Set("Authorization","Bearer synthetic-only")
            status:=http.StatusOK;header:=make(http.Header);body:=""
            switch {
            case strings.HasSuffix(req.URL.Path,"/actions/runs/12345"):
                body=run
            case strings.HasSuffix(req.URL.Path,"/actions/jobs/23456/logs"):
                logCalls++;status=http.StatusFound;header.Set("Location","https://fixture.blob.core.windows.net/log?sig=DO_NOT_RETURN")
                if (tc.fail=="first-log"||tc.fail=="first-log-close")&&logCalls==1||tc.fail=="second-log"&&logCalls==2{status=tc.status;body="DO_NOT_RETURN"}
            case strings.HasSuffix(req.URL.Path,"/actions/runs/12345/attempts/1/jobs"):
                body=jobs;if tc.fail=="metadata"{status=tc.status;body="DO_NOT_RETURN"}
            case strings.HasSuffix(req.URL.Path,"/actions/runs/12345/artifacts"):
                body=artifacts
            case strings.HasSuffix(req.URL.Path,"/actions/workflows/unattended-readiness.yml"):
                body=workflow
            case strings.HasSuffix(req.URL.Path,"/actions/artifacts/45678/zip"):
                if tc.fail=="artifact" {status=tc.status;body="DO_NOT_RETURN";header.Set("Location","https://secret.invalid/DO_NOT_RETURN")
                } else if tc.fail=="artifact-object" {status=http.StatusFound;header.Set("Location","https://fixture.blob.core.windows.net/archive?sig=DO_NOT_RETURN")
                } else {t.Fatal("unexpected artifact request")}
            default:t.Fatal("unexpected API endpoint");return nil,nil
            }
            apiResponses++
            return &http.Response{StatusCode:status,Header:header,Body:&ghaFreshFlowBody{Reader:strings.NewReader(body),closes:&closed,fail:tc.fail=="first-log-close"&&logCalls==1&&strings.HasSuffix(req.URL.Path,"/actions/jobs/23456/logs")}},nil
        })}
        public:=&http.Client{Transport:ghaFreshTestRoundTrip(func(req *http.Request)(*http.Response,error){
            if req.Header.Get("Authorization")!=""||req.Header.Get("Cookie")!=""{t.Fatal("credential forwarded")}
            body:="";status:=http.StatusOK
            switch {
            case req.URL.Host=="fixture.blob.core.windows.net"&&req.URL.Path=="/log":
                body=log;if tc.fail=="job-object"{status=tc.status;body="DO_NOT_RETURN"}
            case req.URL.Host=="fixture.blob.core.windows.net"&&req.URL.Path=="/archive":
                if tc.fail!="artifact-object"{t.Fatal("unexpected object")};status=tc.status;body="DO_NOT_RETURN"
            case req.URL.Host=="api.github.com"&&strings.Contains(req.URL.Path,"/attestations/sha256:"):body=bundle
            default:t.Fatal("unexpected public endpoint");return nil,nil
            }
            objectResponses++
            return &http.Response{StatusCode:status,Header:make(http.Header),Body:&ghaFreshFlowBody{Reader:strings.NewReader(body),closes:&closed}},nil
        })}
        candidate,err:=ghaFreshAcquireFromAuthority(context.Background(),&ghaGuardEvidenceAuthority{client:api},public,loc)
        var statusErr *ghaGuardHTTPStatusError
        if candidate!=nil||!errors.Is(err,errGHAGuardUnqualified)||!errors.As(err,&statusErr)||
            statusErr.phase!=tc.phase||statusErr.status!=tc.status||closed!=apiResponses+objectResponses {
            t.Fatalf("status/close mismatch phase=%s status=%d closed=%d responses=%d err=%v",tc.phase,tc.status,closed,apiResponses+objectResponses,err)
        }
        if strings.Contains(err.Error(),"DO_NOT_RETURN")||strings.Contains(err.Error(),"https://"){t.Fatal("HTTP detail escaped")}
        if errors.Is(err,errGHAGuardResponseCleanup)!=(tc.fail=="first-log-close"){t.Fatal("response close failure lost")}
        audit:=&ghaFreshCredentialAudit{state:"unknown"}
        worker:=ghaFreshInspectWith(context.Background(),audit,func(context.Context)(guardTargetArtifact,error){return nil,err})
        raw,marshalErr:=json.Marshal(worker);if marshalErr!=nil{t.Fatal(marshalErr)}
        parent:=ghaFreshInspectionFromChild(askChildResult{Raw:raw,Exit:1,Started:true,Assigned:true,WaitVerified:true,Reaped:true})
        expected:=fmt.Sprintf("guard_github_%s_http_%d",tc.phase,tc.status)
        if parent.Qualified||parent.Reason!=expected||!parent.WorkerReaped||!parent.WorkerResultObserved||
            parent.CleanupFailed!=(tc.fail=="first-log-close")||parent.CredentialCleanupState!="unknown"||strings.Contains(string(raw),"DO_NOT_RETURN")||strings.Contains(string(raw),"https://"){
            t.Fatalf("parent did not preserve bounded status: %+v",parent)
        }
    })}
}

func TestGHAGuardLegacyHTTPErrorEqualityIsUnchanged(t *testing.T) {
    api:=&http.Client{Transport:ghaFreshTestRoundTrip(func(*http.Request)(*http.Response,error){
        return &http.Response{StatusCode:http.StatusForbidden,Header:make(http.Header),Body:io.NopCloser(strings.NewReader("opaque"))},nil
    })}
    called:=false
    object:=&http.Client{Transport:ghaFreshTestRoundTrip(func(*http.Request)(*http.Response,error){called=true;return nil,io.EOF})}
    endpoint:="https://api.github.com/repos/AutoDevJapan/cc-pc-tools-rs/actions/jobs/3/logs"
    if raw,err:=ghaGuardLiveRedirected(context.Background(),api,object,endpoint,1024);raw!=nil||err!=errGHAGuardUnqualified||called{t.Fatal("legacy redirect error changed")}
    var out any
    if err:=ghaGuardServerJSON(context.Background(),api,"https://api.github.com/repos/AutoDevJapan/cc-pc-tools-rs/actions/runs/2",&out);err!=errGHAGuardUnqualified{t.Fatal("legacy metadata error changed")}
    e:=ghaGuardMetadataExpectation{RepositoryID:1406818713,RunID:2,RunAttempt:1,JobID:3,ArtifactID:4}
    if result,err:=ghaGuardFetchServerEvidence(context.Background(),api,e,ghaGuardCaptureBinding{},time.Now());result!=nil||err!=errGHAGuardUnqualified{t.Fatal("legacy evidence error changed")}
    if result,err:=ghaGuardLiveAcquireWith(context.Background(),&ghaGuardEvidenceAuthority{client:api},object,e,[]byte("fixture"),"fixture");result!=nil||err!=errGHAGuardUnqualified{t.Fatal("legacy acquisition error changed")}
}

func TestGHAFreshSourceStatusIsPerRequestAndSanitized(t *testing.T) {
    cases:=[]struct{name,endpoint string;status int;closeFail,wantStatus bool}{
        {"source401","https://api.github.com/repos/AutoDevJapan/cc-pc-tools-rs/git/commits/80c4ed5cbd864585f4894c6286d3d5c92194c1f5",401,false,true},
        {"source429","https://api.github.com/repos/AutoDevJapan/cc-pc-tools-rs/git/trees/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",429,false,true},
        {"source200-invalid","https://api.github.com/repos/AutoDevJapan/cc-pc-tools-rs",200,false,false},
        {"source403-closefail","https://api.github.com/repos/AutoDevJapan/cc-pc-tools-rs",403,true,true},
        {"foreign","https://example.invalid/DO_NOT_RETURN",403,false,false},
    }
    for _,tc:=range cases {t.Run(tc.name,func(t *testing.T){
        closed:=0
        base:=ghaFreshTestRoundTrip(func(*http.Request)(*http.Response,error){
            return &http.Response{StatusCode:tc.status,Header:make(http.Header),
                Body:&ghaFreshFlowBody{Reader:strings.NewReader("DO_NOT_RETURN"),closes:&closed,fail:tc.closeFail}},nil
        })
        tracked:=&ghaFreshSourceStatusTransport{base:base}
        req,err:=http.NewRequest(http.MethodGet,tc.endpoint,nil);if err!=nil{t.Fatal(err)}
        response,err:=tracked.RoundTrip(req);if err!=nil||response==nil||response.Body==nil{t.Fatal("fake response missing")}
        _=response.Body.Close()
        failure:=tracked.sourceFailure(context.Background())
        var status *ghaGuardHTTPStatusError
        gotStatus:=errors.As(failure,&status)
        if gotStatus!=tc.wantStatus||tc.wantStatus&&(status.phase!="source_metadata"||status.status!=tc.status)||
            errors.Is(failure,errGHAGuardResponseCleanup)!=tc.closeFail||closed!=1||
            strings.Contains(failure.Error(),"DO_NOT_RETURN")||strings.Contains(failure.Error(),"https://"){t.Fatal("source status leaked or mismatched")}
    })}
}

func TestGHAFreshDetailedSourceErrorPreservesLegacyEquality(t *testing.T) {
    manifest:=ghaGuardTestManifest()
    archive:=ghaGuardTestArchive(t,manifest,ghaGuardTestPE(),"")
    now:=time.Now().UTC()
    e:=ghaGuardMetadataExpectation{RepositoryID:1406818713,HeadRepositoryID:1406818713,
        Source:manifest.Source,SourceTree:manifest.SourceTree,ProducerRevision:manifest.ProducerRevision,
        WorkflowPath:".github/workflows/unattended-readiness.yml",Event:"workflow_dispatch",Ref:"refs/heads/main",
        WorkflowID:7,RunID:manifest.RunID,RunAttempt:manifest.RunAttempt,JobID:manifest.JobID,CheckRunID:manifest.CheckRunID,
        JobName:"Guard bootstrap artifact",ArtifactID:4,ArtifactName:"guard-bootstrap-2-1-3"}
    capture,_:=json.Marshal(manifest)
    pub:=ghaGuardPublicationRecord{Domain:"guard-gha-publication-v1",RepositoryID:e.RepositoryID,Source:e.Source,
        SourceTree:e.SourceTree,ProducerRevision:e.ProducerRevision,RunID:e.RunID,RunAttempt:e.RunAttempt,
        JobID:e.JobID,CheckRunID:e.CheckRunID,ArtifactID:e.ArtifactID,ArtifactName:e.ArtifactName,
        ArchiveSHA:digest(archive),OutputSHA:manifest.OutputSHA,CaptureSHA:digest(capture),
        SourceManifestSHA:manifest.SourceManifestSHA,AttestationID:"fixture"}
    makeMaterial:=func()*ghaGuardAcquiredEvidence{
        record,_:=json.Marshal(pub)
        return &ghaGuardAcquiredEvidence{
            run:ghaGuardRun{RepositoryID:e.RepositoryID,HeadRepositoryID:e.HeadRepositoryID,HeadSHA:e.ProducerRevision,
                WorkflowPath:e.WorkflowPath,Event:e.Event,HeadBranch:"main",WorkflowID:e.WorkflowID,RunID:e.RunID,
                RunAttempt:e.RunAttempt,Status:"completed",Conclusion:"success"},
            job:ghaGuardJob{RunID:e.RunID,JobID:e.JobID,CheckRunID:e.CheckRunID,RunAttempt:e.RunAttempt,
                Name:e.JobName,Status:"completed",Conclusion:"success",UploadStepCompleted:true},
            artifact:ghaGuardArtifact{RepositoryID:e.RepositoryID,HeadRepositoryID:e.HeadRepositoryID,HeadSHA:e.ProducerRevision,
                RunID:e.RunID,ID:e.ArtifactID,Name:e.ArtifactName,Size:int64(len(archive)),Digest:"sha256:"+pub.ArchiveSHA,ExpiresAt:now.Add(time.Hour)},
            workflowID:e.WorkflowID,workflowPath:e.WorkflowPath,workflowActive:true,
            jobLog:append([]byte("2026-11-14T08:09:31.3203023Z guard_gha_publication_v1="),record...),
            manifest:manifest,archive:append([]byte(nil),archive...),bundle:[]byte("synthetic-signature"),
        }
    }
    signatures:=0
    signature:=func(context.Context,[]byte,[]byte,ghaGuardManifest)error{signatures++;return nil}
    source:=func(context.Context,[]byte,ghaGuardManifest)error{
        return errors.Join(ghaGuardHTTPFailure("source_metadata",429),errGHAGuardResponseCleanup,errors.New("DO_NOT_RETURN"))
    }
    candidate,err:=ghaGuardConstructVerifiedCandidateWithDetailed(context.Background(),e,makeMaterial(),signature,source)
    var status *ghaGuardHTTPStatusError
    if candidate!=nil||!errors.Is(err,errGHAGuardUnqualified)||!errors.As(err,&status)||status.phase!="source_metadata"||status.status!=429||
        !errors.Is(err,errGHAGuardResponseCleanup)||signatures!=0||strings.Contains(err.Error(),"DO_NOT_RETURN"){t.Fatal("detailed source error lost or leaked")}
    legacy,legacyErr:=ghaGuardConstructVerifiedCandidateWith(context.Background(),e,makeMaterial(),signature,source)
    if legacy!=nil||legacyErr!=errGHAGuardUnqualified||signatures!=0{t.Fatal("legacy direct sentinel changed")}
    worker:=ghaFreshInspectWith(context.Background(),&ghaFreshCredentialAudit{state:"unknown"},func(context.Context)(guardTargetArtifact,error){return nil,err})
    raw,marshalErr:=json.Marshal(worker);if marshalErr!=nil{t.Fatal(marshalErr)}
    parent:=ghaFreshInspectionFromChild(askChildResult{Raw:raw,Exit:1,Started:true,Assigned:true,WaitVerified:true,Reaped:true})
    if parent.Qualified||parent.Reason!="guard_github_source_metadata_http_429"||!parent.CleanupFailed||
        !parent.WorkerReaped||parent.CredentialCleanupState!="unknown"||strings.Contains(string(raw),"DO_NOT_RETURN"){t.Fatal("parent source failure lost")}
}
