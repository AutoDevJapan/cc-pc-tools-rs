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
    provenExpired map[int64]bool
    replacement map[int64]bool
    digestChange map[int64]bool
    sizeChange map[int64]bool
    expiryChange map[int64]bool
    unexpire map[int64]bool
    jobReplacement map[int64]bool
    duplicate map[int64]bool
    futureExpiry map[int64]bool
    otherArtifact map[int64]string
    artifactPage map[int64]string
    uniqueIDs bool
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
    f:=&ghaF1Fixture{calls:map[string]int{},live:map[int64]bool{},expired:map[int64]bool{},provenExpired:map[int64]bool{},replacement:map[int64]bool{},digestChange:map[int64]bool{},sizeChange:map[int64]bool{},expiryChange:map[int64]bool{},unexpire:map[int64]bool{},jobReplacement:map[int64]bool{},duplicate:map[int64]bool{},futureExpiry:map[int64]bool{},otherArtifact:map[int64]string{},artifactPage:map[int64]string{},failedJob:map[int64]bool{},workflow:w,jobs:j,artifacts:a,proof:p,policy:ghaFreshContractFile{name:"unattended-readiness.yml",blob:ghaGuardGitObjectID("blob",p),sha:digest(p),size:int64(len(p))}}
    for i:=0;i<=oldCount;i++ {raw,_:=json.Marshal(envelope.Runs[0]);var run map[string]any;if json.Unmarshal(raw,&run)!=nil{t.Fatal("clone")};run["id"]=int64(50000+i);f.runs=append(f.runs,run)}
    if oldCount>0{f.runs[0]["run_attempt"]=2};f.live[int64(50000+oldCount)]=true
    return f
}
// Add a second row before or after the selected artifact. Malformed JSON is
// returned verbatim only in the explicit negative fixture.
func ghaF1ArtifactPageCase(body,mode string)(string,error) {
    var page struct{Artifacts []json.RawMessage `json:"artifacts"`}
    if json.Unmarshal([]byte(body),&page)!=nil||len(page.Artifacts)!=1{return "",errors.New("fixture page")}
    var selected struct{ID int64 `json:"id"`;Name string `json:"name"`}
    if json.Unmarshal(page.Artifacts[0],&selected)!=nil||selected.ID<=0||selected.Name==""{return "",errors.New("fixture selected")}
    before:=strings.HasSuffix(mode,"-before");if !before&&!strings.HasSuffix(mode,"-after"){return "",errors.New("fixture order")}
    kind:=strings.TrimSuffix(strings.TrimSuffix(mode,"-before"),"-after")
    var other map[string]any;if json.Unmarshal(page.Artifacts[0],&other)!=nil{return "",errors.New("fixture copy")}
    other["name"]="unrelated-artifact";other["id"]=selected.ID;var raw []byte
    switch kind {
    case "duplicate-id":
    case "zero-id":other["id"]=int64(0)
    case "missing-id":delete(other,"id")
    case "same-name":other["name"]=selected.Name;other["id"]=selected.ID+1
    case "distinct":other["id"]=selected.ID+1
    case "bad-object":raw=[]byte("17")
    case "bad-json":raw=[]byte(`{"name":`)
    default:return "",errors.New("fixture mode")
    }
    if raw==nil {var err error;raw,err=json.Marshal(other);if err!=nil{return "",err}}
    first,second:=page.Artifacts[0],json.RawMessage(raw);if before{first,second=second,first}
    return `{"total_count":2,"artifacts":[`+string(first)+","+string(second)+`]}`,nil
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
        rest:=strings.TrimPrefix(path,prefix);parts:=strings.Split(rest,"/")
        id,err:=strconv.ParseInt(parts[0],10,64);if err!=nil{return nil,err}
        if len(parts)==1 {index:=int(id-50000);if index<0||index>=len(f.runs){return nil,errors.New("fixture run")};f.calls["run"]++;raw,_:=json.Marshal(f.runs[index]);body=string(raw)
        } else if len(parts)==4&&parts[1]=="attempts"&&parts[2]=="1"&&parts[3]=="jobs" {f.calls["jobs"]++;f.calls["job/"+parts[0]]++;body=strings.ReplaceAll(f.jobs,"12345",parts[0]);if f.uniqueIDs{jobID:=id+1000000;if f.jobReplacement[id]{jobID+=500000};body=strings.ReplaceAll(body,"23456",fmt.Sprint(jobID));body=strings.ReplaceAll(body,"34567",fmt.Sprint(id+2000000))};if f.failedJob[id]{body=strings.Replace(body,`"conclusion":"success"`,`"conclusion":"failure"`,1)}
        } else if len(parts)==2&&parts[1]=="artifacts" {f.calls["artifacts"]++;f.calls["artifact/"+parts[0]]++;body=`{"total_count":0,"artifacts":[]}`
            if f.live[id]||f.expired[id]||f.provenExpired[id] {
                body=strings.ReplaceAll(f.artifacts,"12345",parts[0])
                artifactID:=int64(45678);if f.uniqueIDs{jobID:=id+1000000;if f.jobReplacement[id]{jobID+=500000};body=strings.ReplaceAll(body,"23456",fmt.Sprint(jobID));artifactID=id+3000000};if f.replacement[id]{artifactID++}
                body=strings.Replace(body,`"id":45678`,fmt.Sprintf(`"id":%d`,artifactID),1)
                index:=int(id-50000);if index>=0&&index<len(f.runs){if revision,ok:=f.runs[index]["head_sha"].(string);ok{body=strings.ReplaceAll(body,ghaFreshTestRevision,revision)}}
                if f.provenExpired[id]&&!f.live[id]&&!f.unexpire[id]{body=strings.Replace(body,`"expired":false`,`"expired":true`,1)}
                if f.digestChange[id]{body=strings.Replace(body,strings.Repeat("a",64),strings.Repeat("b",64),1)}
                if f.sizeChange[id]{body=strings.Replace(body,`"size_in_bytes":10`,`"size_in_bytes":11`,1)}
                if f.expiryChange[id]{body=strings.Replace(body,"2026-11-16T12:00:00Z","2026-11-16T11:00:00Z",1)}
                if (f.live[id]&&!f.expiry)||f.futureExpiry[id] {body=strings.Replace(body,"2026-11-16T12:00:00Z","2099-01-01T00:00:00Z",1)}
                if f.duplicate[id] {var page struct{Artifacts []json.RawMessage `json:"artifacts"`};if json.Unmarshal([]byte(body),&page)!=nil||len(page.Artifacts)!=1{return nil,errors.New("fixture duplicate")};page.Artifacts=append(page.Artifacts,page.Artifacts[0]);raw,_:=json.Marshal(map[string]any{"total_count":len(page.Artifacts),"artifacts":page.Artifacts});body=string(raw)}
                if mode:=f.artifactPage[id];mode!="" {var err error;body,err=ghaF1ArtifactPageCase(body,mode);if err!=nil{return nil,err}}
            }
            if mode:=f.otherArtifact[id];mode!="" {
                index:=int(id-50000);if index<0||index>=len(f.runs){return nil,errors.New("fixture run")};revision,_:=f.runs[index]["head_sha"].(string)
                name:="unrelated";if mode=="other-job"{name=fmt.Sprintf("guard-bootstrap-%d-1-%d",id,id+1500000)};if mode=="other-attempt"{name=fmt.Sprintf("guard-bootstrap-%d-2-%d",id,id+1500000)}
                row:=map[string]any{"id":id+9000000,"name":name,"size_in_bytes":10,"expired":false,"expires_at":"2099-01-01T00:00:00Z","digest":"sha256:"+strings.Repeat("e",64),"workflow_run":map[string]any{"id":id,"repository_id":1406818713,"head_repository_id":1406818713,"head_sha":revision}}
                raw,_:=json.Marshal(map[string]any{"total_count":1,"artifacts":[]any{row}});body=string(raw)
            }
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

// Elapsed-expiry evidence is provisional until the final boundary; a failed
// first job is not cached and is reobserved on the next intermediate scan.
func TestGHAFreshUnprovedRetiredOutcomesAreReobserved(t *testing.T) {
    for _,mode:=range []string{"explicit-expired","failed-job"} {t.Run(mode,func(t *testing.T){
        f:=ghaF1NewFixture(t,2);if mode=="explicit-expired"{f.expired[50000]=true}else{f.failedJob[50000]=true}
        session,client,closeOwned:=ghaF1Session(t,f);defer closeOwned();now:=time.Date(2026,11,17,20,0,0,0,time.UTC);policy:=&ghaFreshRetentionIdentity{marker:4}
        scan:=func()(ghaFreshRunHint,error){return ghaFreshReadRunHintCore(context.Background(),client,client,now,f.verify,policy)}
        if h,err:=scan();err!=nil||h.runID!=50002{t.Fatal("initial scan",err)}
        session.mu.Lock();cached:=len(session.retired);session.mu.Unlock();wantCache:=1;if mode=="explicit-expired"{wantCache=2};if cached!=wantCache{t.Fatal("unexpected provisional evidence",cached)}
        f.mu.Lock();delete(f.expired,50000);delete(f.failedJob,50000);f.live[50000]=true;f.mu.Unlock()
        boundary:=ghaFreshObserveBoundary;if mode=="explicit-expired"{boundary=ghaFreshFinalBoundary}
        if _,err:=ghaFreshReadRunHintAt(context.Background(),client,client,now,f.verify,policy,boundary);err==nil||f.count("job/50000")!=2{t.Fatal("changed retired outcome was hidden")}
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
    ctx,cancel:=context.WithTimeout(context.Background(),5*time.Second);defer cancel()
    var barrierMu sync.Mutex;arrived:=0;bothStarted:=make(chan struct{})
    verify:=func(ctx context.Context,_ *http.Client,h ghaFreshRunHint)error {
        barrierMu.Lock();arrived++;if arrived==2{close(bothStarted)};barrierMu.Unlock()
        select {case <-bothStarted:case <-ctx.Done():return ctx.Err()}
        if h.runID==50000{return errors.New("private-first-failure")}
        return errors.Join(context.Canceled,errGHAGuardResponseCleanup)
    }
    _,err:=ghaFreshReadRunHintCore(ctx,client,client,now,verify,&ghaFreshRetentionIdentity{marker:6})
    if err==nil||!errors.Is(err,context.Canceled)||!errors.Is(err,errGHAGuardResponseCleanup){t.Fatal("sibling cleanup or cancellation lost")}
    parent:=ghaV4ProjectFailure(t,ghaFreshSafeCurrentFailure(context.Background(),err))
    if !parent.Cancelled||!parent.CleanupFailed {t.Fatal("parent lost sibling result",parent)}
    session.mu.Lock();cached,active:=len(session.retired),session.active;session.mu.Unlock()
    if cached!=0||active!=0 {t.Fatal("failed parallel flight not drained",cached,active)}
}

func TestGHAFreshPreCancelledEntryForgetsRetiredEvidence(t *testing.T) {
    f:=ghaF1NewFixture(t,2);session,client,closeOwned:=ghaF1Session(t,f);defer closeOwned()
    now:=time.Date(2026,11,17,20,0,0,0,time.UTC);policy:=&ghaFreshRetentionIdentity{marker:7}
    if _,err:=ghaFreshReadRunHintCore(context.Background(),client,client,now,f.verify,policy);err!=nil{t.Fatal("initial scan",err)}
    session.mu.Lock();cached:=len(session.retired);session.mu.Unlock();if cached!=2{t.Fatal("precondition: no retained evidence",cached)}
    f.mu.Lock();before:=0;for _,n:=range f.calls{before+=n};f.mu.Unlock()
    ctx,cancel:=context.WithCancel(context.Background());cancel()
    if _,err:=ghaFreshReadRunHintCore(ctx,client,client,now,f.verify,policy);!errors.Is(err,context.Canceled){t.Fatal("pre-cancel lost",err)}
    session.mu.Lock();cached,active:=len(session.retired),session.active;session.mu.Unlock()
    f.mu.Lock();after:=0;for _,n:=range f.calls{after+=n};f.mu.Unlock()
    if cached!=0||active!=0||after!=before{t.Fatal("pre-cancel retained cache or started HTTP",cached,active,before,after)}
    old:=f.count("job/50000")
    if _,err:=ghaFreshReadRunHintCore(context.Background(),client,client,now,f.verify,policy);err!=nil||f.count("job/50000")!=old+1{t.Fatal("discarded evidence reused",err)}
}

// The first fatal response deliberately blocks in Close. No later hint may
// start, while already-started siblings still report their cleanup outcomes.
type ghaF1BlockingCloseBody struct {ctx context.Context;entered chan<- struct{};release <-chan struct{}}
func (*ghaF1BlockingCloseBody) Read([]byte)(int,error){return 0,io.EOF}
func (b *ghaF1BlockingCloseBody) Close()error {
    select{case b.entered<-struct{}{}:default:}
    select{case <-b.release:return nil;case <-b.ctx.Done():return b.ctx.Err()}
}
type ghaF1NotifyingCloseBody struct {closed chan<- struct{};fail bool}
func (*ghaF1NotifyingCloseBody) Read([]byte)(int,error){return 0,io.EOF}
func (b *ghaF1NotifyingCloseBody) Close()error {
    select{case b.closed<-struct{}{}:default:};if b.fail{return errors.New("private-cleanup-detail")};return nil
}

func TestGHAFreshFatalHTTPStopsUnstartedAndDrains(t *testing.T) {
    for _,status:=range []int{http.StatusForbidden,http.StatusTooManyRequests} {t.Run(strconv.Itoa(status),func(t *testing.T){
        ctx,cancel:=context.WithTimeout(context.Background(),5*time.Second)
        firstRelease:=make(chan struct{});siblingsRelease:=make(chan struct{});closeRelease:=make(chan struct{})
        started:=make(chan int64,32);closeEntered:=make(chan struct{},1);siblingsClosed:=make(chan struct{},8);done:=make(chan struct{})
        var firstOnce,siblingsOnce,closeOnce sync.Once
        releaseAll:=func(){firstOnce.Do(func(){close(firstRelease)});siblingsOnce.Do(func(){close(siblingsRelease)});closeOnce.Do(func(){close(closeRelease)})}
        now:=time.Date(2026,11,17,20,0,0,0,time.UTC);hints:=make([]ghaFreshRunHint,32);work:=make([]int,len(hints))
        for i:=range hints {hints[i]=ghaFreshRunHint{runID:int64(50000+i),attempt:1,producerRevision:ghaFreshTestRevision,createdAt:time.Date(2026,11,15,17,21,18,0,time.UTC)};work[i]=i}
        scope,err:=ghaFreshDiscoveryURLs(hints);if err!=nil{cancel();t.Fatal(err)}
        root,cancelRoot:=context.WithCancel(context.Background())
        base:=ghaFreshTestTransport(func(r *http.Request)(*http.Response,error){
            if r.URL.Host!="api.github.com"||r.Header.Get("Authorization")!="Bearer fictional"{return nil,errors.New("fixture destination")}
            const prefix="/repos/AutoDevJapan/cc-pc-tools-rs/actions/runs/"
            if !strings.HasPrefix(r.URL.Path,prefix){return nil,errors.New("fixture path")}
            parts:=strings.Split(strings.TrimPrefix(r.URL.Path,prefix),"/")
            if len(parts)!=4||parts[1]!="attempts"||parts[2]!="1"||parts[3]!="jobs"{return nil,errors.New("fixture path")}
            id,e:=strconv.ParseInt(parts[0],10,64);if e!=nil{return nil,e}
            select{case started<-id:case <-ctx.Done():return nil,ctx.Err()}
            if id==50000 {
                select{case <-firstRelease:case <-ctx.Done():return nil,ctx.Err()}
                return &http.Response{StatusCode:status,Header:make(http.Header),Body:&ghaF1BlockingCloseBody{ctx:ctx,entered:closeEntered,release:closeRelease},Request:r},nil
            }
            select{case <-siblingsRelease:case <-ctx.Done():return nil,ctx.Err()}
            return &http.Response{StatusCode:http.StatusForbidden,Header:make(http.Header),Body:&ghaF1NotifyingCloseBody{closed:siblingsClosed,fail:id==50001},Request:r},nil
        })
        session:=&ghaGuardCredentialSession{discovery:scope,secret:[]byte("fictional"),base:base,root:root,cancel:cancelRoot,ready:true,drain:make(chan struct{})}
        queue:=&ghaFreshScanQueue{work:work,outcomes:make([]ghaFreshScanResult,len(hints)),stopped:make(chan struct{})}
        client:=&http.Client{Transport:&ghaFreshScanTransport{base:session,queue:queue}}
        go func(){defer close(done);ghaFreshRunScanWorkers(ctx,queue,func(index int)ghaFreshScanResult{
            _,err:=ghaFreshPreflightAfterWorkflow(ctx,client,hints[index],now,nil);return ghaFreshScanResult{checked:true,err:err}
        })}()
        defer func(){releaseAll();cancel();select{case <-done:case <-time.After(time.Second):t.Error("scan workers not joined")};c,stop:=context.WithTimeout(context.Background(),time.Second);defer stop();if session.close(c)!=nil{t.Error("session not drained")}}()
        seen:=map[int64]bool{}
        for i:=0;i<8;i++ {select{case id:=<-started:seen[id]=true;case <-ctx.Done():t.Fatal("first wave did not start")}}
        if len(seen)!=8||!seen[50000]||!seen[50001]{t.Fatal("unexpected first wave",seen)}
        firstOnce.Do(func(){close(firstRelease)})
        select{case <-queue.stopped:case <-ctx.Done():t.Fatal("fatal status not observed")}
        select{case <-closeEntered:case <-ctx.Done():t.Fatal("fatal response Close not reached")}
        siblingsOnce.Do(func(){close(siblingsRelease)})
        for i:=0;i<7;i++ {select{case <-siblingsClosed:case <-ctx.Done():t.Fatal("started sibling not closed")}}
        closeOnce.Do(func(){close(closeRelease)})
        select{case <-done:case <-ctx.Done():t.Fatal("workers not joined")}
        queue.mu.Lock();claimed:=queue.cursor;first:=queue.fatal;queue.mu.Unlock()
        if claimed!=8||len(started)!=0{t.Fatal("hint started after fatal observation",claimed,len(started))}
        var statusErr *ghaGuardHTTPStatusError
        if !errors.As(first,&statusErr)||statusErr.status!=status||statusErr.phase!="authenticated_metadata"{t.Fatal("initial HTTP status lost",first)}
        failures:=first;checked:=0
        for _,out:=range queue.outcomes {if out.checked{checked++;failures=errors.Join(failures,out.err)}}
        if checked!=8||!errors.Is(failures,errGHAGuardResponseCleanup){t.Fatal("started sibling cleanup lost",checked)}
        parent:=ghaV4ProjectFailure(t,ghaFreshSafeCurrentFailure(context.Background(),failures))
        if parent.Reason!=fmt.Sprintf("guard_github_authenticated_metadata_http_%d",status)||!parent.CleanupFailed||parent.Qualified{t.Fatal("parent facts lost",parent)}
        session.mu.Lock();active:=session.active;session.mu.Unlock();if active!=0{t.Fatal("response handles not drained",active)}
    })}
}

// Exercise all four discovery boundaries with the maximum permitted listing.
// Requests are fictional; current source/signature traffic is counted separately.
func TestGHAFreshExplicitExpiredFourScanRequestBudget(t *testing.T) {
    cases:=[]struct{name string;distinct bool;mode string}{{"expired-flag",false,"flag"},{"distinct-revisions",true,"flag"},{"elapsed-only",false,"elapsed"},{"expired-before-deadline",false,"future"}}
    for _,tc:=range cases {t.Run(tc.name,func(t *testing.T){
        f:=ghaF1NewFixture(t,998);f.uniqueIDs=true
        for i:=0;i<998;i++ {id:=int64(50000+i);if tc.mode=="elapsed"{f.expired[id]=true}else{f.provenExpired[id]=true};if tc.mode=="future"{f.futureExpiry[id]=true};if tc.distinct{f.runs[i]["head_sha"]=fmt.Sprintf("%040x",i+1)}}
        session,client,closeOwned:=ghaF1Session(t,f);defer closeOwned()
        ctx,cancel:=context.WithTimeout(context.Background(),3*time.Minute);defer cancel()
        now:=time.Date(2026,11,17,20,0,0,0,time.UTC);policy:=&ghaFreshRetentionIdentity{marker:8}
        authority:=&ghaGuardEvidenceAuthority{client:client,credential:session}
        loc,err:=ghaFreshDiscoverOwnedCore(ctx,authority,now,f.verify,policy);if err!=nil||loc.hint.runID!=50998{t.Fatal("initial discovery",err)}
        for pass:=0;pass<3;pass++ {
            boundary:=ghaFreshObserveBoundary;if pass==2{boundary=ghaFreshFinalBoundary}
            h,err:=ghaFreshReadRunHintAt(ctx,client,client,now,f.verify,policy,boundary);if err!=nil||h.runID!=loc.hint.runID{t.Fatal("generation scan",pass,err)}
            current,err:=ghaFreshPublicPreflightWithRetention(ctx,client,h,now,f.verify);if err!=nil||!ghaFreshSameLocator(current,loc){t.Fatal("live preflight",pass,err)}
        }
        jobs,artifacts,listing,workflow,proof:=f.count("jobs"),f.count("artifacts"),f.count("listing"),f.count("workflow"),f.count("proof")
        if jobs!=2004||artifacts!=2004||listing!=80||workflow!=8||proof!=0{t.Fatalf("wrong discovery request plan: jobs=%d artifacts=%d listing=%d workflow=%d proof=%d",jobs,artifacts,listing,workflow,proof)}
        if jobs+artifacts+listing+workflow+proof!=4096{t.Fatal("unexpected four-scan GET count")}
        session.mu.Lock();cached,active:=len(session.retired),session.active;session.mu.Unlock()
        if cached!=998||active!=0{t.Fatal("retired evidence or cleanup lost",cached,active)}
    })}
}

// The cache is provisional: the final boundary must reject changes to an old
// artifact even if the workflow-run listing still reports the same attempt.
func TestGHAFreshExpiredEvidenceFinalRevalidation(t *testing.T) {
    for _,mode:=range []string{"replacement-id","digest-change","size-change","expiry-change","job-replacement","job-failure","new-live","missing","duplicate","future-expiry","flag-reversed","flag-became-true","stable-future","unchanged","new-attempt"} {t.Run(mode,func(t *testing.T){
        f:=ghaF1NewFixture(t,3);f.uniqueIDs=true;for i:=0;i<3;i++{f.provenExpired[int64(50000+i)]=true}
        if mode=="flag-became-true"{delete(f.provenExpired,50000);f.expired[50000]=true};if mode=="stable-future"{f.futureExpiry[50000]=true}
        session,client,closeOwned:=ghaF1Session(t,f);defer closeOwned()
        ctx,cancel:=context.WithTimeout(context.Background(),10*time.Second);defer cancel();now:=time.Date(2026,11,17,20,0,0,0,time.UTC);policy:=&ghaFreshRetentionIdentity{marker:9}
        if _,err:=ghaFreshReadRunHintAt(ctx,client,client,now,f.verify,policy,ghaFreshObserveBoundary);err!=nil{t.Fatal("initial scan",err)}
        session.mu.Lock();cached:=len(session.retired);session.mu.Unlock();if cached!=3{t.Fatal("expired proof not recorded",cached)}
        oldJobs:=f.count("job/50000");oldArtifacts:=f.count("artifact/50000")
        if _,err:=ghaFreshReadRunHintAt(ctx,client,client,now,f.verify,policy,ghaFreshObserveBoundary);err!=nil{t.Fatal("middle scan",err)}
        if f.count("job/50000")!=oldJobs||f.count("artifact/50000")!=oldArtifacts{t.Fatal("middle scan did not reuse proof")}
        f.mu.Lock();switch mode {
        case "replacement-id":f.replacement[50000]=true
        case "digest-change":f.digestChange[50000]=true
        case "size-change":f.sizeChange[50000]=true
        case "expiry-change":f.expiryChange[50000]=true
        case "job-replacement":f.jobReplacement[50000]=true
        case "job-failure":f.failedJob[50000]=true
        case "new-live":f.live[50000]=true
        case "missing":delete(f.provenExpired,50000)
        case "duplicate":f.duplicate[50000]=true
        case "future-expiry":f.futureExpiry[50000]=true
        case "flag-reversed":f.unexpire[50000]=true
        case "flag-became-true":f.provenExpired[50000]=true
        case "new-attempt":f.runs[0]["run_attempt"]=3
        };f.mu.Unlock()
        _,err:=ghaFreshReadRunHintAt(ctx,client,client,now,f.verify,policy,ghaFreshFinalBoundary)
        if mode=="unchanged"||mode=="new-attempt"||mode=="flag-became-true"||mode=="stable-future" {
            if err!=nil||f.count("job/50000")!=oldJobs+1||f.count("artifact/50000")!=oldArtifacts+1{t.Fatal("final revalidation rejected valid new observation",mode,err)}
        } else {
            if err==nil{t.Fatal("changed retired evidence admitted",mode)}
            session.mu.Lock();cached,active:=len(session.retired),session.active;session.mu.Unlock();if cached!=0||active!=0{t.Fatal("failure retained evidence or handles",mode,cached,active)}
        }
    })}
}

// Counts all requests made by the exercised synthetic discovery, server and
// 249-record source checks and fixed redirect requests; signature is not simulated.
type ghaF1BudgetTransport struct {
    mu sync.Mutex
    calls,sourceCalls,limit int
    discovery *ghaF1Fixture
    source http.RoundTripper
}
func (b *ghaF1BudgetTransport) RoundTrip(r *http.Request)(*http.Response,error) {
    b.mu.Lock();b.calls++;n,limit:=b.calls,b.limit;isSource:=strings.Contains(r.URL.Path,"/git/")||strings.Contains(r.URL.Path,"/compare/")||r.URL.Path=="/repos/AutoDevJapan/cc-pc-tools-rs";if isSource{b.sourceCalls++};b.mu.Unlock()
    if n>limit{return &http.Response{StatusCode:http.StatusTooManyRequests,Header:make(http.Header),Body:io.NopCloser(strings.NewReader("fictional quota")),Request:r},nil}
    if r.URL.Path=="/user" {return &http.Response{StatusCode:http.StatusOK,Header:make(http.Header),Body:io.NopCloser(strings.NewReader(`{"id":88025750,"login":"mashfromband"}`)),Request:r},nil}
    if strings.HasPrefix(r.URL.Path,"/repos/AutoDevJapan/cc-pc-tools-rs/actions/jobs/")||strings.HasPrefix(r.URL.Path,"/repos/AutoDevJapan/cc-pc-tools-rs/actions/artifacts/") {
        header:=make(http.Header);header.Set("Location","https://fixture.blob.core.windows.net/object")
        return &http.Response{StatusCode:http.StatusFound,Header:header,Body:io.NopCloser(strings.NewReader("")),Request:r},nil
    }
    if isSource{return b.source.RoundTrip(r)}
    return b.discovery.roundTrip(r)
}
type ghaF1PublicBudget struct {mu sync.Mutex;calls int}
func (b *ghaF1PublicBudget) RoundTrip(r *http.Request)(*http.Response,error) {
    if r.Header.Get("Authorization")!=""||r.Header.Get("Cookie")!=""{return nil,errors.New("credential reached public fixture")}
    b.mu.Lock();b.calls++;b.mu.Unlock();body:="1234567890"
    if r.URL.Host=="api.github.com"&&strings.HasPrefix(r.URL.Path,"/repos/AutoDevJapan/cc-pc-tools-rs/attestations/") {body=`{"attestations":[{"repository_id":1406818713,"bundle_url":"https://example.invalid/77.json.sn","bundle":{}}]}`
    } else if r.URL.Host!="fixture.blob.core.windows.net"&&r.URL.String()!=ghaGuardPublicSourceURL{return nil,errors.New("unexpected public fixture")}
    return &http.Response{StatusCode:http.StatusOK,Header:make(http.Header),Body:io.NopCloser(strings.NewReader(body)),Request:r},nil
}
func TestGHAFreshExplicitExpiredAcquisitionSourceBudget(t *testing.T) {
    for _,injectQuota:=range []bool{false,true} {t.Run(fmt.Sprintf("inject-quota-%t",injectQuota),func(t *testing.T){
        f:=ghaF1NewFixture(t,998);f.uniqueIDs=true;for i:=0;i<998;i++{f.provenExpired[int64(50000+i)]=true}
        source:=ghaCurrentTestFixtureFor(t,"unrelated-main")
        session,client,closeOwned:=ghaF1Session(t,f);defer closeOwned()
        budget:=&ghaF1BudgetTransport{limit:5000,discovery:f,source:source.client.Transport};session.base=budget;publicBudget:=&ghaF1PublicBudget{};public:=&http.Client{Transport:publicBudget};injectedAt:=0
        ctx,cancel:=context.WithTimeout(context.Background(),3*time.Minute);defer cancel();now:=time.Date(2026,11,17,20,0,0,0,time.UTC);policy:=ghaFreshFixedRetentionIdentity
        run:=func()error {
            authority:=&ghaGuardEvidenceAuthority{client:client,credential:session}
            req,e:=http.NewRequestWithContext(ctx,http.MethodGet,"https://api.github.com/user",nil);if e!=nil{return e};req.Header.Set("Accept","application/vnd.github+json");req.Header.Set("X-GitHub-Api-Version","2022-11-28")
            resp,e:=client.Do(req);if e!=nil{return e};raw,e:=io.ReadAll(resp.Body);closeErr:=resp.Body.Close();if e!=nil||closeErr!=nil||!ghaGuardCredentialOwnerJSON(raw){return errGHAGuardUnqualified}
            noProof:=func(context.Context,*http.Client,ghaFreshRunHint)error{return errors.New("unexpected explicit proof callback")}
            loc,err:=ghaFreshDiscoverOwnedCore(ctx,authority,now,noProof,policy);if err!=nil{return err}
            if err=ghaFreshAuthenticatedRunMatches(ctx,client,loc.hint);err!=nil{return err}
            expected,err:=ghaFreshExpectation(loc,now);if err!=nil{return err}
            binding:=ghaGuardCaptureBinding{RepositoryID:expected.RepositoryID,Source:expected.Source,ProducerRevision:expected.ProducerRevision,RunID:expected.RunID,RunAttempt:expected.RunAttempt,JobID:expected.JobID,CheckRunID:expected.CheckRunID,ArtifactID:expected.ArtifactID}
            logURL:=fmt.Sprintf("https://api.github.com/repos/AutoDevJapan/cc-pc-tools-rs/actions/jobs/%d/logs",loc.jobID)
            if _,err=ghaGuardLiveRedirectedDetailed(ctx,client,public,logURL,8<<20);err!=nil{return err}
            if _,err=ghaGuardFetchServerEvidenceDetailed(ctx,client,expected,binding,now);err!=nil{return err}
            pub:=ghaGuardPublicationRecord{OutputSHA:strings.Repeat("b",64),AttestationID:"77"}
            bundle,e:=ghaFreshReadPublicBundle(ctx,public,pub);if e!=nil{return e};clear(bundle)
            if err=ghaFreshCheckWorkflow(ctx,client,loc.hint);err!=nil{return err}
            if _,err=ghaGuardLiveRedirectedDetailed(ctx,client,public,logURL,8<<20);err!=nil{return err}
            if _,err=ghaGuardFetchServerEvidenceDetailed(ctx,client,expected,binding,now);err!=nil{return err}
            archiveURL:=fmt.Sprintf("https://api.github.com/repos/AutoDevJapan/cc-pc-tools-rs/actions/artifacts/%d/zip",loc.artifactID)
            if _,err=ghaGuardLiveRedirectedDetailed(ctx,client,public,archiveURL,loc.archiveBytes);err!=nil{return err}
            manifest,capture:=ghaCurrentTestArchive(t,source.records)
            if records,e:=ghaGuardArchiveSourceRecords(ctx,capture,manifest,client);e!=nil||len(records)!=249{return errGHAGuardUnqualified}
            var commit struct{Tree struct{SHA string `json:"sha"`} `json:"tree"`}
            if json.Unmarshal(source.endpoints["/git/commits/"+source.producer],&commit)!=nil{return errGHAGuardUnqualified}
            if err=ghaGuardVerifyGitTreeSnapshot(ctx,client,source.producer,commit.Tree.SHA,source.records);err!=nil{return err}
            archive,e:=ghaGuardReadPinnedSourceArchive(ctx,publicBudget);if e!=nil{return e};clear(archive)
            generation:=ghaGuardGeneration{Source:ghaGuardSource,SourceTree:ghaGuardSourceTree,ProducerRevision:loc.hint.producerRevision,RunID:loc.hint.runID,RunAttempt:int(loc.hint.attempt),JobID:loc.jobID,CheckRunID:loc.checkRunID,ArtifactID:loc.artifactID,OutputSHA:strings.Repeat("b",64),ProofSHA:strings.Repeat("d",64)}
            scan:=func(boundary ghaFreshDiscoveryBoundary)error {
                return ghaFreshCheckHintGenerationAtClock(ctx,client,client,generation,loc,boundary,func()time.Time{return now})
            }
            if err=scan(ghaFreshObserveBoundary);err!=nil{return err}
            if err=ghaFreshVerifyCurrentRecordsWith(ctx,client,client,source.records,source.producer,source.policy);err!=nil{return err}
            if err=scan(ghaFreshObserveBoundary);err!=nil{return err}
            if injectQuota{budget.mu.Lock();injectedAt=budget.calls+50;budget.limit=injectedAt;budget.mu.Unlock()}
            return scan(ghaFreshFinalBoundary)
        }
        err:=run();budget.mu.Lock();calls,sourceCalls:=budget.calls,budget.sourceCalls;budget.mu.Unlock()
        session.mu.Lock();active,cached:=session.active,len(session.retired);session.mu.Unlock()
        if active!=0{t.Fatal("responses not drained",active)}
        publicBudget.mu.Lock();publicCalls:=publicBudget.calls;publicBudget.mu.Unlock()
        t.Logf("synthetic GET count: authenticated=%d source=%d public=%d injected=%t cutoff=%d (TUF signature fetch excluded)",calls,sourceCalls,publicCalls,injectQuota,injectedAt)
        if !injectQuota {
            if err!=nil||calls>=5000||sourceCalls<249||publicCalls!=5||cached!=998{t.Fatal("synthetic acquisition/source budget",calls,sourceCalls,publicCalls,cached,err)}
        } else {
            var status *ghaGuardHTTPStatusError
            if err==nil||injectedAt<=0||!errors.As(err,&status)||status.status!=http.StatusTooManyRequests||calls>injectedAt+16||cached!=0{t.Fatal("synthetic quota failure was not stopped",calls,cached,err)}
        }
    })}
}

// Unique source revisions are independently verified once; the final
// boundary rechecks absence without re-downloading the same proof per run.
func TestGHAFreshMissingFourScanRequestBudget(t *testing.T) {
    for _,distinct:=range []bool{false,true} {t.Run(fmt.Sprintf("distinct-revisions-%t",distinct),func(t *testing.T){
        f:=ghaF1NewFixture(t,998);f.uniqueIDs=true
        for i:=0;i<998;i++{if distinct{f.runs[i]["head_sha"]=fmt.Sprintf("%040x",i+1)}}
        session,client,closeOwned:=ghaF1Session(t,f);defer closeOwned()
        ctx,cancel:=context.WithTimeout(context.Background(),3*time.Minute);defer cancel();now:=time.Date(2026,11,17,20,0,0,0,time.UTC);policy:=&ghaFreshRetentionIdentity{marker:11}
        authority:=&ghaGuardEvidenceAuthority{client:client,credential:session}
        loc,err:=ghaFreshDiscoverOwnedCore(ctx,authority,now,f.verify,policy);if err!=nil||loc.hint.runID!=50998{t.Fatal("initial missing scan",err)}
        for pass:=0;pass<3;pass++ {
            boundary:=ghaFreshObserveBoundary;if pass==2{boundary=ghaFreshFinalBoundary}
            h,err:=ghaFreshReadRunHintAt(ctx,client,client,now,f.verify,policy,boundary);if err!=nil{t.Fatal("missing scan",pass,err)}
            current,err:=ghaFreshPublicPreflightWithRetention(ctx,client,h,now,f.verify);if err!=nil||!ghaFreshSameLocator(current,loc){t.Fatal("live preflight",pass,err)}
        }
        jobs,artifacts,listing,workflow,proof:=f.count("jobs"),f.count("artifacts"),f.count("listing"),f.count("workflow"),f.count("proof")
        expectedProof:=1;if distinct{expectedProof=998}
        if jobs!=1006||artifacts!=2004||listing!=80||workflow!=8||proof!=expectedProof{t.Fatalf("wrong missing plan: jobs=%d artifacts=%d listing=%d workflow=%d proof=%d",jobs,artifacts,listing,workflow,proof)}
        if jobs+artifacts+listing+workflow+proof>4096{t.Fatal("missing plan exceeded budget")}
        session.mu.Lock();cached,active:=len(session.retired),session.active;session.mu.Unlock();if cached!=998||active!=0{t.Fatal("missing proof/cleanup lost",cached,active)}
    })}
}

func TestGHAFreshMissingFinalRejectsOtherJobArtifact(t *testing.T) {
    f:=ghaF1NewFixture(t,2);f.uniqueIDs=true;session,client,closeOwned:=ghaF1Session(t,f);defer closeOwned()
    ctx,cancel:=context.WithTimeout(context.Background(),10*time.Second);defer cancel();now:=time.Date(2026,11,17,20,0,0,0,time.UTC);policy:=&ghaFreshRetentionIdentity{marker:12}
    if _,err:=ghaFreshReadRunHintAt(ctx,client,client,now,f.verify,policy,ghaFreshObserveBoundary);err!=nil{t.Fatal("initial missing proof",err)}
    session.mu.Lock();cached:=len(session.retired);session.mu.Unlock();if cached!=2{t.Fatal("missing proof absent",cached)}
    f.mu.Lock();f.live[50000]=true;f.jobReplacement[50000]=true;f.mu.Unlock()
    if _,err:=ghaFreshReadRunHintAt(ctx,client,client,now,f.verify,policy,ghaFreshFinalBoundary);err==nil{t.Fatal("other job artifact hidden by missing cache")}
    session.mu.Lock();cached,active:=len(session.retired),session.active;session.mu.Unlock();if cached!=0||active!=0{t.Fatal("failed final retained evidence or handles",cached,active)}
}

// Exercise the same generation-check function used immediately before an
// acquisition/handoff result; the fake verifier cannot mint a missing proof.
func TestGHAFreshFinalGenerationRejectsRetiredChanges(t *testing.T) {
    for _,mode:=range []string{"unchanged","replacement-id","new-live","missing"} {t.Run(mode,func(t *testing.T){
        f:=ghaF1NewFixture(t,3);f.uniqueIDs=true;for i:=0;i<3;i++{f.provenExpired[int64(50000+i)]=true}
        session,client,closeOwned:=ghaF1Session(t,f);defer closeOwned();ctx,cancel:=context.WithTimeout(context.Background(),10*time.Second);defer cancel()
        now:=time.Date(2026,11,17,20,0,0,0,time.UTC);authority:=&ghaGuardEvidenceAuthority{client:client,credential:session}
        noProof:=func(context.Context,*http.Client,ghaFreshRunHint)error{return errors.New("unexpected initial proof callback")}
        loc,err:=ghaFreshDiscoverOwnedCore(ctx,authority,now,noProof,ghaFreshFixedRetentionIdentity);if err!=nil{t.Fatal("initial discovery",err)}
        g:=ghaGuardGeneration{Source:ghaGuardSource,SourceTree:ghaGuardSourceTree,ProducerRevision:loc.hint.producerRevision,RunID:loc.hint.runID,RunAttempt:int(loc.hint.attempt),JobID:loc.jobID,CheckRunID:loc.checkRunID,ArtifactID:loc.artifactID,OutputSHA:strings.Repeat("b",64),ProofSHA:strings.Repeat("d",64)}
        clock:=func()time.Time{return now}
        for i:=0;i<2;i++ {if err=ghaFreshCheckHintGenerationAtClock(ctx,client,client,g,loc,ghaFreshObserveBoundary,clock);err!=nil{t.Fatal("intermediate generation",err)}}
        f.mu.Lock();switch mode {case "replacement-id":f.replacement[50000]=true;case "new-live":f.live[50000]=true;case "missing":delete(f.provenExpired,50000)};f.mu.Unlock()
        err=ghaFreshCheckHintGenerationAtClock(ctx,client,client,g,loc,ghaFreshFinalBoundary,clock)
        if mode=="unchanged" {if err!=nil{t.Fatal("unchanged final rejected",err)}} else {
            if err==nil{t.Fatal("final generation admitted changed retired evidence",mode)}
            session.mu.Lock();cached,active:=len(session.retired),session.active;session.mu.Unlock();if cached!=0||active!=0{t.Fatal("failed final retained evidence or handles",cached,active)}
        }
    })}
}

// A final-boundary cache miss must inspect the same reserved name space as a
// cached missing proof, including a new session, run and attempt.
func TestGHAFreshNewFinalMissingChecksProducerNamespace(t *testing.T) {
    for _,scenario:=range []string{"fresh-final","new-session","new-attempt","new-run"} {
        for _,kind:=range []string{"clean","other-job","other-attempt","unrelated"} {t.Run(scenario+"/"+kind,func(t *testing.T){
            f:=ghaF1NewFixture(t,1);f.uniqueIDs=true
            session,client,closeOwned:=ghaF1Session(t,f);defer closeOwned()
            ctx,cancel:=context.WithTimeout(context.Background(),10*time.Second);defer cancel()
            now:=time.Date(2026,11,17,20,0,0,0,time.UTC);policy:=&ghaFreshRetentionIdentity{marker:20};target:=int64(50000)
            if scenario!="fresh-final" {
                if _,err:=ghaFreshReadRunHintAt(ctx,client,client,now,f.verify,policy,ghaFreshObserveBoundary);err!=nil{t.Fatal("initial observation",err)}
            }
            switch scenario {
            case "new-session":
                closeOwned();session,client,closeOwned=ghaF1Session(t,f);defer closeOwned()
                session.mu.Lock();cached:=len(session.retired);session.mu.Unlock();if cached!=0{t.Fatal("new session inherited cache")}
            case "new-attempt":f.mu.Lock();f.runs[0]["run_attempt"]=3;f.mu.Unlock()
            case "new-run":
                f.mu.Lock();raw,_:=json.Marshal(f.runs[0]);var extra map[string]any;if json.Unmarshal(raw,&extra)!=nil{f.mu.Unlock();t.Fatal("clone")};target=50002;extra["id"]=target;extra["run_attempt"]=1;f.runs=append(f.runs,extra);f.mu.Unlock()
            }
            if kind!="clean"{f.mu.Lock();f.otherArtifact[target]=kind;f.mu.Unlock()}
            jobKey:="job/"+strconv.FormatInt(target,10);artifactKey:="artifact/"+strconv.FormatInt(target,10);jobsBefore,artBefore:=f.count(jobKey),f.count(artifactKey)
            h,err:=ghaFreshReadRunHintAt(ctx,client,client,now,f.verify,policy,ghaFreshFinalBoundary)
            if f.count(jobKey)!=jobsBefore+1||f.count(artifactKey)!=artBefore+1{t.Fatal("final cache miss was not examined",scenario,kind)}
            if kind=="other-job" {if err==nil{t.Fatal("other producer accepted",scenario)}} else if err!=nil||h.runID!=50001{t.Fatal("valid non-conflict rejected",scenario,kind,err)}
            session.mu.Lock();cached,active:=len(session.retired),session.active;session.mu.Unlock()
            if active!=0||(kind=="other-job"&&cached!=0){t.Fatal("failure left cache or active requests",cached,active)}
        })}
    }
}

func TestGHAFreshInvalidExpiredEvidenceReturnsZero(t *testing.T) {
    _,_,body:=ghaFreshLocatorFixture();now:=time.Date(2026,11,17,20,0,0,0,time.UTC)
    h:=ghaFreshRunHint{runID:12345,attempt:1,producerRevision:ghaFreshTestRevision,createdAt:time.Date(2026,11,15,17,21,18,0,time.UTC)}
    for _,kind:=range []string{"duplicate-id","zero-id","missing-id","bad-object","bad-json","same-name","distinct"} {for _,order:=range []string{"before","after"} {t.Run(kind+"/"+order,func(t *testing.T){
        raw,err:=ghaF1ArtifactPageCase(body,kind+"-"+order);if err!=nil{t.Fatal(err)}
        page,parseErr:=ghaFreshObject([]byte(raw));if kind=="bad-json" {if parseErr==nil{t.Fatal("broken JSON parsed")};return};if parseErr!=nil{t.Fatal(parseErr)}
        evidence,ok:=ghaFreshExplicitRetiredEvidence(page,h,23456,34567,now)
        if kind=="distinct" {if !ok||!evidence.valid(now){t.Fatal("valid distinct row rejected")}} else if ok||evidence!=(ghaFreshRetiredEvidence{}){t.Fatal("invalid page returned evidence",kind,order)}
    })}}
}

// Exercise malformed or contradictory pages through the actual initial and
// final preflight, including a cached final observation.
func TestGHAFreshExpiredPageConsistencyAcrossBoundaries(t *testing.T) {
    for _,phase:=range []string{"observe-first","final-first","final-cached"} {
        for _,kind:=range []string{"duplicate-id","zero-id","missing-id","bad-object","bad-json","same-name","distinct"} {
            for _,order:=range []string{"before","after"} {t.Run(phase+"/"+kind+"/"+order,func(t *testing.T){
                f:=ghaF1NewFixture(t,1);f.uniqueIDs=true;f.provenExpired[50000]=true
                session,client,closeOwned:=ghaF1Session(t,f);defer closeOwned();ctx,cancel:=context.WithTimeout(context.Background(),10*time.Second);defer cancel()
                now:=time.Date(2026,11,17,20,0,0,0,time.UTC);policy:=&ghaFreshRetentionIdentity{marker:21}
                if phase=="final-cached" {if _,err:=ghaFreshReadRunHintAt(ctx,client,client,now,f.verify,policy,ghaFreshObserveBoundary);err!=nil{t.Fatal("initial proof",err)}}
                f.mu.Lock();f.artifactPage[50000]=kind+"-"+order;f.mu.Unlock()
                boundary:=ghaFreshObserveBoundary;if phase!="observe-first"{boundary=ghaFreshFinalBoundary}
                h,err:=ghaFreshReadRunHintAt(ctx,client,client,now,f.verify,policy,boundary)
                if kind=="distinct" {if err!=nil||h.runID!=50001{t.Fatal("valid distinct row rejected",err)}} else if err==nil {t.Fatal("contradictory page accepted",phase,kind,order)}
                session.mu.Lock();cached,active:=len(session.retired),session.active;session.mu.Unlock()
                if active!=0||(kind!="distinct"&&cached!=0){t.Fatal("failed scan retained state",cached,active)}
            })}
        }
    }
}
