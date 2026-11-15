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
    "strings"
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
