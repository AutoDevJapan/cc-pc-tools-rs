//go:build windows

package main

import (
    "context"
    "encoding/base64"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "net/http"
    "strconv"
    "strings"
    "sync"
    "testing"
    "time"
)

// All traffic is synthetic: the owned session attaches only the fictional
// credential to fixed-repository requests in its validated listing scope.
func TestGHAFreshDiscoveryManyOldRunsUsesOwnedScope(t *testing.T) {
    for _,mode:=range []string{"normal","old-rerun","failed-rerun","wrong-retention","old-live-rerun","two-live"} {t.Run(mode,func(t *testing.T){
        var envelope struct{Runs []map[string]any `json:"workflow_runs"`}
        if json.Unmarshal([]byte(ghaFreshTestGood),&envelope)!=nil||len(envelope.Runs)!=1{t.Fatal("fixture")}
        runs:=make([]map[string]any,0,19)
        for i:=0;i<19;i++ {
            raw,_:=json.Marshal(envelope.Runs[0]);var run map[string]any;if json.Unmarshal(raw,&run)!=nil{t.Fatal("clone")}
            run["id"]=int64(50000+i);if i==0&&(mode=="old-rerun"||mode=="old-live-rerun"||mode=="failed-rerun"){run["run_attempt"]=2};if i==0&&mode=="failed-rerun"{run["conclusion"]="failure"};runs=append(runs,run)
        }
        listing,_:=json.Marshal(map[string]any{"total_count":len(runs),"workflow_runs":runs})
        workflow,jobs,artifacts:=ghaFreshLocatorFixture()
        oldWorkflow:=[]byte("synthetic reviewed workflow with one-day retention\n")
        policy:=ghaFreshContractFile{name:"unattended-readiness.yml",blob:ghaGuardGitObjectID("blob",oldWorkflow),sha:digest(oldWorkflow),size:int64(len(oldWorkflow))}
        count:=0
        base:=ghaFreshTestTransport(func(r *http.Request)(*http.Response,error){
            count++
            if r.URL.Host!="api.github.com"||r.Header.Get("Authorization")!="Bearer synthetic-discovery-only"{t.Fatal("discovery credential escaped its fixed destination")}
            body:=""
            switch {
            case r.URL.String()==ghaFreshListingURL:body=string(listing)
            case strings.HasSuffix(r.URL.Path,"/actions/workflows/unattended-readiness.yml"):body=workflow
            case strings.HasSuffix(r.URL.Path,"/contents/.github/workflows/unattended-readiness.yml"):
                content:=oldWorkflow;if mode=="wrong-retention"{content=[]byte("different workflow")}
                raw,_:=json.Marshal(map[string]any{"name":policy.name,"path":".github/workflows/unattended-readiness.yml","type":"file","sha":policy.blob,"size":policy.size,"encoding":"base64","content":base64.StdEncoding.EncodeToString(content)});body=string(raw)
            default:
                for i:=0;i<19;i++ {
                    id:=int64(50000+i)
                    if r.URL.Path==fmt.Sprintf("/repos/AutoDevJapan/cc-pc-tools-rs/actions/runs/%d/attempts/1/jobs",id){body=strings.ReplaceAll(jobs,"12345",fmt.Sprint(id));if i==0&&mode=="failed-rerun" {body=strings.Replace(body,`"conclusion":"success"`,`"conclusion":"failure"`,1)};break}
                    if r.URL.Path==fmt.Sprintf("/repos/AutoDevJapan/cc-pc-tools-rs/actions/runs/%d/artifacts",id){
                        live:=i==18||mode=="two-live"&&i==17||mode=="old-live-rerun"&&i==0
                        if live {body=strings.ReplaceAll(strings.ReplaceAll(artifacts,"12345",fmt.Sprint(id)),"2026-11-16T12:00:00Z","2099-01-01T00:00:00Z")} else {body=`{"total_count":0,"artifacts":[]}`}
                        break
                    }
                }
            }
            if body=="" {t.Fatal("unexpected fixed request")}
            return &http.Response{StatusCode:http.StatusOK,Header:make(http.Header),Body:io.NopCloser(strings.NewReader(body)),Request:r},nil
        })
        root,cancel:=context.WithCancel(context.Background());defer cancel()
        initial,err:=ghaFreshDiscoveryURLs(nil);if err!=nil{t.Fatal(err)}
        session:=&ghaGuardCredentialSession{discovery:initial,secret:[]byte("synthetic-discovery-only"),base:base,root:root,cancel:cancel,ready:true,drain:make(chan struct{})}
        t.Cleanup(func(){c,done:=context.WithTimeout(context.Background(),time.Second);defer done();if session.close(c)!=nil{t.Error("fictional credential cleanup")}})
        client:=&http.Client{Transport:session};now:=time.Date(2026,11,17,20,0,0,0,time.UTC)
        verify:=func(ctx context.Context,c *http.Client,h ghaFreshRunHint)error{return ghaFreshVerifiedRetentionSourceWith(ctx,c,h,policy)}
        authority:=&ghaGuardEvidenceAuthority{client:client,credential:session}
        loc,err:=ghaFreshDiscoverOwnedWith(context.Background(),authority,now,verify)
        if mode=="normal"||mode=="old-rerun"||mode=="failed-rerun" {
            if err!=nil||loc.hint.runID!=50018||loc.hint.superseded||count<=60{t.Fatal("valid generation was not selected through authenticated discovery",mode,err,count)}
            if session.fresh==nil||*session.fresh!=loc {t.Fatal("same-session binding absent")}
            foreign:=loc;foreign.hint.runID=999999;if ghaFreshBindDiscoveryLocator(authority,foreign)==nil{t.Fatal("unlisted generation bound")}
        } else if err==nil||loc.hint.runID!=0 {t.Fatal("unknown or multiple live generation accepted",mode)}
        cleanup,done:=context.WithTimeout(context.Background(),time.Second);defer done();if session.close(cleanup)!=nil{t.Fatal("fictional credential was not drained")}
    })}
}

func TestGHAFreshDiscoveryScopeRejectsExternalOrUnlistedRequests(t *testing.T) {
    h:=ghaFreshRunHint{runID:12345,attempt:1,producerRevision:ghaFreshTestRevision,createdAt:time.Date(2026,11,15,17,21,18,0,time.UTC)}
    scope,err:=ghaFreshDiscoveryURLs([]ghaFreshRunHint{h});if err!=nil{t.Fatal(err)}
    allowed,_:=ghaFreshResourceURL(ghaFreshJobsResource,h);req,_:=http.NewRequest(http.MethodGet,allowed,nil)
    if !scope.allows(req){t.Fatal("validated fixed request refused")}
    for _,bad:=range []string{strings.Replace(allowed,"api.github.com","other.invalid",1),strings.Replace(allowed,"12345","12346",1),strings.Replace(allowed,"attempts/1","attempts/2",1),allowed+"&x=1"} {req,_:=http.NewRequest(http.MethodGet,bad,nil);if scope.allows(req){t.Fatal("unlisted destination accepted")}}
    req,_=http.NewRequest(http.MethodGet,allowed,nil);req.Header.Set("Authorization","caller-value");if scope.allows(req){t.Fatal("caller credential accepted")}
    req.Header.Del("Authorization");req.Method=http.MethodPost;if scope.allows(req){t.Fatal("write method accepted")}
}

func TestGHAFreshSupersededAttemptIsNeverLocator(t *testing.T) {
    loc:=ghaFreshTestLocator();loc.hint.superseded=true
    if ghaFreshLocatorValid(loc,time.Now().UTC()){t.Fatal("rerun attempt admitted as a candidate locator")}
}

// Both normal selection and the old-attempt fallback use the same job-page
// identity contract through the actual preflight path.
func TestGHAFreshPreflightRejectsConflictingJobIdentity(t *testing.T) {
    cases:=[]struct{name string;failed bool;others []map[string]any;want string}{
        {"failed-distinct-other",true,[]map[string]any{{"name":"Other job","id":24,"run_id":19}},"noncandidate"},
        {"failed-duplicate-guard-other",true,[]map[string]any{{"name":"Other job","id":23,"run_id":19}},"reject"},
        {"failed-duplicate-other-rows",true,[]map[string]any{{"name":"Other A","id":24,"run_id":19},{"name":"Other B","id":24,"run_id":19}},"reject"},
        {"failed-zero-other",true,[]map[string]any{{"name":"Other job","id":0,"run_id":19}},"reject"},
        {"failed-missing-other-id",true,[]map[string]any{{"name":"Other job","run_id":19}},"reject"},
        {"failed-wrong-other-run",true,[]map[string]any{{"name":"Other job","id":24,"run_id":20}},"reject"},
        {"selected-distinct-other",false,[]map[string]any{{"name":"Other job","id":24,"run_id":19}},"selected"},
        {"selected-duplicate-guard-other",false,[]map[string]any{{"name":"Other job","id":23,"run_id":19}},"reject"},
        {"selected-negative-other",false,[]map[string]any{{"name":"Other job","id":-1,"run_id":19}},"reject"},
        {"selected-missing-other-run",false,[]map[string]any{{"name":"Other job","id":24}},"reject"},
    }
    for _,tc:=range cases {t.Run(tc.name,func(t *testing.T){
        workflow,jobsRaw,artifacts:=ghaFreshLocatorFixture()
        var fixture struct{Jobs []map[string]any `json:"jobs"`};if json.Unmarshal([]byte(jobsRaw),&fixture)!=nil||len(fixture.Jobs)!=1{t.Fatal("fixture")}
        guard:=fixture.Jobs[0];guard["id"]=23;guard["run_id"]=19;if tc.failed{guard["conclusion"]="failure"}
        rows:=[]map[string]any{guard};rows=append(rows,tc.others...);jobs,err:=json.Marshal(map[string]any{"total_count":len(rows),"jobs":rows});if err!=nil{t.Fatal(err)}
        artifacts=strings.ReplaceAll(strings.ReplaceAll(artifacts,"12345","19"),"23456","23")
        artifacts=strings.Replace(artifacts,"2026-11-16T12:00:00Z","2099-01-01T00:00:00Z",1)
        artifactCalls:=0
        client:=&http.Client{Transport:ghaFreshTestTransport(func(r *http.Request)(*http.Response,error){
            body:=""
            switch r.URL.Path {
            case "/repos/AutoDevJapan/cc-pc-tools-rs/actions/workflows/unattended-readiness.yml":body=workflow
            case "/repos/AutoDevJapan/cc-pc-tools-rs/actions/runs/19/attempts/1/jobs":body=string(jobs)
            case "/repos/AutoDevJapan/cc-pc-tools-rs/actions/runs/19/artifacts":artifactCalls++;body=artifacts
            default:t.Fatal("unexpected endpoint");return nil,errGHAGuardUnqualified
            }
            return &http.Response{StatusCode:http.StatusOK,Header:make(http.Header),Body:io.NopCloser(strings.NewReader(body)),Request:r},nil
        })}
        hint:=ghaFreshRunHint{runID:19,attempt:1,producerRevision:ghaFreshTestRevision,createdAt:time.Date(2026,11,15,17,21,18,0,time.UTC),superseded:tc.failed}
        verifyCalls:=0;verify:=func(context.Context,*http.Client,ghaFreshRunHint)error{verifyCalls++;return nil}
        loc,err:=ghaFreshPublicPreflightWithRetention(context.Background(),client,hint,time.Date(2026,11,17,20,0,0,0,time.UTC),verify)
        switch tc.want {
        case "noncandidate":
            if !errors.Is(err,errGHAFreshNonCandidate)||loc.jobID!=0||artifactCalls!=0||verifyCalls!=0{t.Fatal("valid failed attempt not recognized",err)}
        case "selected":
            if err!=nil||loc.jobID!=23||loc.artifactID!=45678||artifactCalls!=1||verifyCalls!=0{t.Fatal("valid distinct job page rejected",err)}
        default:
            if err==nil||!errors.Is(err,errGHAGuardUnqualified)||errors.Is(err,errGHAFreshNonCandidate)||errors.Is(err,errGHAFreshExpiredArtifact)||loc.jobID!=0||artifactCalls!=0||verifyCalls!=0{t.Fatal("contradictory page accepted or mislabeled",tc.name,err)}
        }
    })}
}

// Fictional multi-page listing. The owned transport still enforces the fixed
// URL and credential scope; no external request is made by this test.
type ghaF1Fixture struct {
    mu sync.Mutex
    runs []map[string]any
    calls map[string]int
    live map[int64]bool
    expired map[int64]bool
    failedJob map[int64]bool
    sourceStatus int
    changeOnListing int
    expiry bool
    workflow,jobs,artifacts string
    proof []byte
    policy ghaFreshContractFile
}
func ghaF1NewFixture(t *testing.T,oldCount int) *ghaF1Fixture {
    t.Helper();var envelope struct{Runs []map[string]any `json:"workflow_runs"`}
    if json.Unmarshal([]byte(ghaFreshTestGood),&envelope)!=nil||len(envelope.Runs)!=1{t.Fatal("fixture")}
    w,j,a:=ghaFreshLocatorFixture();p:=[]byte("synthetic reviewed workflow with one-day retention\n")
    f:=&ghaF1Fixture{calls:map[string]int{},live:map[int64]bool{},expired:map[int64]bool{},failedJob:map[int64]bool{},workflow:w,jobs:j,artifacts:a,proof:p,policy:ghaFreshContractFile{name:"unattended-readiness.yml",blob:ghaGuardGitObjectID("blob",p),sha:digest(p),size:int64(len(p))}}
    for i:=0;i<=oldCount;i++ {raw,_:=json.Marshal(envelope.Runs[0]);var run map[string]any;if json.Unmarshal(raw,&run)!=nil{t.Fatal("clone")};run["id"]=int64(50000+i);f.runs=append(f.runs,run)}
    if oldCount>0{f.runs[0]["run_attempt"]=2};f.live[int64(50000+oldCount)]=true
    return f
}
func (f *ghaF1Fixture) roundTrip(r *http.Request)(*http.Response,error){
    f.mu.Lock();defer f.mu.Unlock()
    if r.URL.Host!="api.github.com"||r.Header.Get("Authorization")!="Bearer fictional"{return nil,errors.New("fixture destination")}
    path:=r.URL.Path;body:="";status:=http.StatusOK;header:=make(http.Header)
    switch {
    case path=="/repos/AutoDevJapan/cc-pc-tools-rs/actions/workflows/382798887/runs":
        f.calls["listing"]++;if f.changeOnListing==f.calls["listing"] {f.runs[0]["run_attempt"]=3};page:=1;if p:=r.URL.Query().Get("page");p!=""{var err error;page,err=strconv.Atoi(p);if err!=nil{return nil,err}}
        start:=(page-1)*100;end:=start+100;if end>len(f.runs){end=len(f.runs)};if start<0||start>=len(f.runs){return nil,errors.New("fixture page")}
        raw,_:=json.Marshal(map[string]any{"total_count":len(f.runs),"workflow_runs":f.runs[start:end]});body=string(raw)
        if end<len(f.runs){header.Set("Link",`<https://api.github.com/ignored>; rel="next"`)}
    case path=="/repos/AutoDevJapan/cc-pc-tools-rs/actions/workflows/unattended-readiness.yml":f.calls["workflow"]++;body=f.workflow
    case path=="/repos/AutoDevJapan/cc-pc-tools-rs/contents/.github/workflows/unattended-readiness.yml":
        f.calls["proof"]++;if f.sourceStatus!=0{status=f.sourceStatus;body="{}";break}
        raw,_:=json.Marshal(map[string]any{"name":f.policy.name,"path":".github/workflows/unattended-readiness.yml","type":"file","sha":f.policy.blob,"size":f.policy.size,"encoding":"base64","content":base64.StdEncoding.EncodeToString(f.proof)});body=string(raw)
    default:
        const prefix="/repos/AutoDevJapan/cc-pc-tools-rs/actions/runs/"
        if !strings.HasPrefix(path,prefix){return nil,errors.New("fixture path")}
        rest:=strings.TrimPrefix(path,prefix);parts:=strings.Split(rest,"/");if len(parts)<2{return nil,errors.New("fixture path")}
        id,err:=strconv.ParseInt(parts[0],10,64);if err!=nil{return nil,err}
        if len(parts)==4&&parts[1]=="attempts"&&parts[2]=="1"&&parts[3]=="jobs" {f.calls["jobs"]++;f.calls["job/"+parts[0]]++;body=strings.ReplaceAll(f.jobs,"12345",parts[0]);if f.failedJob[id]{body=strings.Replace(body,`"conclusion":"success"`,`"conclusion":"failure"`,1)}
        } else if len(parts)==2&&parts[1]=="artifacts" {f.calls["artifacts"]++;f.calls["artifact/"+parts[0]]++;body=`{"total_count":0,"artifacts":[]}`
            if f.live[id]||f.expired[id] {body=strings.ReplaceAll(f.artifacts,"12345",parts[0]);if f.live[id]&&!f.expiry {body=strings.Replace(body,"2026-11-16T12:00:00Z","2099-01-01T00:00:00Z",1)}}
        } else {return nil,errors.New("fixture path")}
    }
    return &http.Response{StatusCode:status,Header:header,Body:io.NopCloser(strings.NewReader(body)),Request:r},nil
}
func ghaF1Session(t *testing.T,f *ghaF1Fixture)(*ghaGuardCredentialSession,*http.Client,func()){
    t.Helper();root,cancel:=context.WithCancel(context.Background());initial,err:=ghaFreshDiscoveryURLs(nil);if err!=nil{t.Fatal(err)}
    s:=&ghaGuardCredentialSession{discovery:initial,secret:[]byte("fictional"),base:ghaFreshTestTransport(f.roundTrip),root:root,cancel:cancel,ready:true,drain:make(chan struct{})}
    closeOwned:=func(){ctx,done:=context.WithTimeout(context.Background(),time.Second);defer done();if s.close(ctx)!=nil{t.Error("session not drained")}}
    return s,&http.Client{Transport:s},closeOwned
}
func (f *ghaF1Fixture) count(name string)int {f.mu.Lock();defer f.mu.Unlock();return f.calls[name]}
func (f *ghaF1Fixture) verify(ctx context.Context,c *http.Client,h ghaFreshRunHint)error{return ghaFreshVerifiedRetentionSourceWith(ctx,c,h,f.policy)}

func TestGHAFreshSameFlightRetiredEvidenceAndLiveRecheck(t *testing.T) {
    f:=ghaF1NewFixture(t,400);f.runs[1]["head_sha"]=strings.Repeat("d",40)
    session,client,closeOwned:=ghaF1Session(t,f);defer closeOwned()
    policy:=&ghaFreshRetentionIdentity{marker:1};now:=time.Date(2026,11,17,20,0,0,0,time.UTC)
    scan:=func()(ghaFreshRunHint,error){return ghaFreshReadRunHintCore(context.Background(),client,client,now,f.verify,policy)}
    authority:=&ghaGuardEvidenceAuthority{client:client,credential:session}
    loc,err:=ghaFreshDiscoverOwnedCore(context.Background(),authority,now,f.verify,policy)
    if err!=nil||loc.hint.runID!=50400 {t.Fatal("initial owned discovery",err)}
    for i:=0;i<3;i++ {
        h,err:=scan();if err!=nil||h.runID!=50400 {t.Fatal("valid same-flight scan",i,err)}
        current,err:=ghaFreshPublicPreflightWithRetention(context.Background(),client,h,now,f.verify)
        if err!=nil||current!=loc {t.Fatal("selected locator did not remain current",i,err)}
    }
    if f.count("listing")!=40||f.count("workflow")!=8||f.count("job/50000")!=1||f.count("job/50400")!=8||f.count("proof")!=2 {t.Fatal("retired proof reuse or live refresh missing",f.calls)}
    if f.count("jobs")+f.count("artifacts")+f.count("listing")+f.count("workflow")+f.count("proof")>900 {t.Fatal("synthetic request budget exceeded")}
    session.mu.Lock();cached:=len(session.retired);session.mu.Unlock();if cached!=400{t.Fatal("retired evidence not bound to session",cached)}

    // 2 -> 3, and a later contradictory 3 -> 2, both require new proof.
    beforeOld:=f.count("job/50000");f.mu.Lock();f.runs[0]["run_attempt"]=3;f.mu.Unlock()
    if h,err:=scan();err!=nil||h.runID!=50400||f.count("job/50000")!=beforeOld+1 {t.Fatal("new generation was not rechecked",err)}
    f.mu.Lock();f.runs[0]["run_attempt"]=2;f.mu.Unlock()
    if h,err:=scan();err!=nil||h.runID!=50400||f.count("job/50000")!=beforeOld+2 {t.Fatal("old cached generation was revived",err)}
    f.mu.Lock();f.runs[0]["run_attempt"]=3;f.sourceStatus=http.StatusForbidden;f.mu.Unlock()
    if _,err:=scan();err==nil||f.count("job/50000")!=beforeOld+3 {t.Fatal("failed proof accepted")}
    session.mu.Lock();afterFailure:=len(session.retired);session.mu.Unlock();if afterFailure!=0{t.Fatal("incomplete scan retained evidence")}
    f.mu.Lock();f.sourceStatus=0;f.mu.Unlock()
    if h,err:=scan();err!=nil||h.runID!=50400||f.count("job/50000")!=beforeOld+4 {t.Fatal("fresh successful proof not retried",err)}

    // The current artifact is never cached: loss of its validity fails closed
    // and an incomplete scan erases prior entries.
    f.mu.Lock();f.expiry=true;f.mu.Unlock();beforeLive:=f.count("job/50400")
    if _,err:=scan();err==nil||f.count("job/50400")!=beforeLive+1 {t.Fatal("expired selected artifact accepted")}
    session.mu.Lock();afterExpiry:=len(session.retired);session.mu.Unlock();if afterExpiry!=0{t.Fatal("failed expiry scan retained evidence")}
    f.mu.Lock();f.expiry=false;f.mu.Unlock()

    // A newly listed second live generation is examined and creates ambiguity.
    f.mu.Lock();raw,_:=json.Marshal(f.runs[0]);var fresh map[string]any;_ = json.Unmarshal(raw,&fresh);fresh["id"]=int64(50401);fresh["run_attempt"]=1;f.runs=append(f.runs,fresh);f.live[50401]=true;f.mu.Unlock()
    if _,err:=scan();err==nil||f.count("job/50401")!=1 {t.Fatal("new live generation was ignored")}
    f.mu.Lock();f.runs=f.runs[:len(f.runs)-1];delete(f.live,50401);f.mu.Unlock()
    if h,err:=scan();err!=nil||h.runID!=50400 {t.Fatal("valid listing did not recover",err)}

    // Different internal verifier identity and different credential session
    // cannot consume the previous flight's entries.
    beforeOld=f.count("job/50000");otherPolicy:=&ghaFreshRetentionIdentity{marker:2}
    if _,err:=ghaFreshReadRunHintCore(context.Background(),client,client,now,f.verify,otherPolicy);err!=nil||f.count("job/50000")!=beforeOld+1 {t.Fatal("other verifier reused evidence",err)}
    closeOwned();session.mu.Lock();left:=len(session.retired);session.mu.Unlock();if left!=0{t.Fatal("closed session retained evidence")}
    next,nextClient,closeNext:=ghaF1Session(t,f);defer closeNext();_ = next;beforeOld=f.count("job/50000")
    if _,err:=ghaFreshReadRunHintCore(context.Background(),nextClient,nextClient,now,f.verify,policy);err!=nil||f.count("job/50000")!=beforeOld+1 {t.Fatal("new session reused evidence",err)}
}

func TestGHAFreshListingAttemptChangeBetweenPagesFailsClosed(t *testing.T) {
    f:=ghaF1NewFixture(t,100);f.changeOnListing=3
    _,client,closeOwned:=ghaF1Session(t,f);defer closeOwned();now:=time.Date(2026,11,17,20,0,0,0,time.UTC)
    if _,err:=ghaFreshReadRunHintCore(context.Background(),client,client,now,f.verify,&ghaFreshRetentionIdentity{marker:3});err==nil {t.Fatal("attempt 2 -> 3 was not detected")}
    if f.count("jobs")!=0||f.count("workflow")!=0{t.Fatal("preflight ran on an unstable listing")}
}

// An explicit expired artifact or failed first job has no verified fixed
// retention proof. Reobserve both and reject a later live contradiction.
func TestGHAFreshUnprovedRetiredOutcomesAreReobserved(t *testing.T) {
    for _,mode:=range []string{"explicit-expired","failed-job"} {t.Run(mode,func(t *testing.T){
        f:=ghaF1NewFixture(t,2);if mode=="explicit-expired"{f.expired[50000]=true}else{f.failedJob[50000]=true}
        session,client,closeOwned:=ghaF1Session(t,f);defer closeOwned();now:=time.Date(2026,11,17,20,0,0,0,time.UTC);policy:=&ghaFreshRetentionIdentity{marker:4}
        scan:=func()(ghaFreshRunHint,error){return ghaFreshReadRunHintCore(context.Background(),client,client,now,f.verify,policy)}
        if h,err:=scan();err!=nil||h.runID!=50002{t.Fatal("initial scan",err)}
        session.mu.Lock();cached:=len(session.retired);session.mu.Unlock();if cached!=1{t.Fatal("unproved outcome cached",cached)}
        f.mu.Lock();delete(f.expired,50000);delete(f.failedJob,50000);f.live[50000]=true;f.mu.Unlock()
        if _,err:=scan();err==nil||f.count("job/50000")!=2{t.Fatal("changed unproved outcome was hidden")}
    })}
}

func TestGHAFreshRetiredProofFailureAndCancelAreNotCached(t *testing.T) {
    for _,mode:=range []string{"joined-cleanup","cancel"} {t.Run(mode,func(t *testing.T){
        f:=ghaF1NewFixture(t,1);session,client,closeOwned:=ghaF1Session(t,f);defer closeOwned()
        ctx,cancel:=context.WithCancel(context.Background());defer cancel();now:=time.Date(2026,11,17,20,0,0,0,time.UTC)
        verify:=func(context.Context,*http.Client,ghaFreshRunHint)error {
            if mode=="cancel"{cancel();return context.Canceled}
            return errors.Join(errGHAFreshExpiredArtifact,errGHAGuardResponseCleanup)
        }
        _,err:=ghaFreshReadRunHintCore(ctx,client,client,now,verify,&ghaFreshRetentionIdentity{marker:5})
        if err==nil {t.Fatal("failed proof accepted")}
        if mode=="joined-cleanup"&&!errors.Is(err,errGHAGuardResponseCleanup){t.Fatal("cleanup lost")}
        if mode=="cancel"&&!errors.Is(err,context.Canceled){t.Fatal("cancel lost")}
        session.mu.Lock();cached,active:=len(session.retired),session.active;session.mu.Unlock()
        if cached!=0||active!=0{t.Fatal("failed flight cached or undrained",cached,active)}
    })}
}

func TestGHAFreshParallelPreflightPreservesSiblingCleanup(t *testing.T) {
    f:=ghaF1NewFixture(t,2);f.runs[1]["head_sha"]=strings.Repeat("d",40)
    session,client,closeOwned:=ghaF1Session(t,f);defer closeOwned();now:=time.Date(2026,11,17,20,0,0,0,time.UTC)
    verify:=func(_ context.Context,_ *http.Client,h ghaFreshRunHint)error {
        if h.runID==50000{return errors.New("private-first-failure")}
        return errors.Join(context.Canceled,errGHAGuardResponseCleanup)
    }
    _,err:=ghaFreshReadRunHintCore(context.Background(),client,client,now,verify,&ghaFreshRetentionIdentity{marker:6})
    if err==nil||!errors.Is(err,context.Canceled)||!errors.Is(err,errGHAGuardResponseCleanup){t.Fatal("sibling cleanup or cancellation lost")}
    parent:=ghaV4ProjectFailure(t,ghaFreshSafeCurrentFailure(context.Background(),err))
    if !parent.Cancelled||!parent.CleanupFailed {t.Fatal("parent lost sibling result",parent)}
    session.mu.Lock();cached,active:=len(session.retired),session.active;session.mu.Unlock()
    if cached!=0||active!=0 {t.Fatal("failed parallel flight not drained",cached,active)}
}
