//go:build windows

package main

import (
    "bytes"
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

const ghaFreshTestRevision = "cccccccccccccccccccccccccccccccccccccccc"
const ghaFreshTestGood = "{\"total_count\":1,\"workflow_runs\":[{\"id\":12345,\"created_at\":\"2026-11-15T17:21:18Z\",\"run_attempt\":1,\"workflow_id\":382798887,\"head_sha\":\"cccccccccccccccccccccccccccccccccccccccc\",\"head_branch\":\"main\",\"path\":\".github/workflows/unattended-readiness.yml\",\"event\":\"workflow_dispatch\",\"status\":\"completed\",\"conclusion\":\"success\",\"repository\":{\"id\":1406818713},\"head_repository\":{\"id\":1406818713},\"actor\":{\"id\":88025750},\"triggering_actor\":{\"id\":88025750}}]}"

func TestGHAFreshRunHintRejectsAmbiguity(t *testing.T) {
    good,err:=ghaFreshSelectRunHint([]byte(ghaFreshTestGood))
    if err!=nil||good.runID!=12345||good.attempt!=1||good.producerRevision!=ghaFreshTestRevision {t.Fatal("fixed hint rejected")}
    cases:=[]struct{name,raw string}{
        {"zero",strings.Replace(ghaFreshTestGood,"\"total_count\":1","\"total_count\":0",1)},
        {"multiple",strings.Replace(ghaFreshTestGood,"\"total_count\":1","\"total_count\":2",1)},
        {"duplicate",strings.Replace(ghaFreshTestGood,"\"id\":12345","\"id\":12345,\"id\":12345",1)},
        {"case-fold",strings.Replace(ghaFreshTestGood,"\"id\":12345","\"id\":12345,\"ID\":12345",1)},
        {"wrong-attempt",strings.Replace(ghaFreshTestGood,"\"run_attempt\":1","\"run_attempt\":2",1)},
        {"wrong-workflow",strings.Replace(ghaFreshTestGood,"382798887","382798888",1)},
        {"null-workflow",strings.Replace(ghaFreshTestGood,"\"workflow_id\":382798887","\"workflow_id\":null",1)},
        {"invalid-revision",strings.Replace(ghaFreshTestGood,ghaFreshTestRevision,strings.Repeat("z",40),1)},
        {"wrong-branch",strings.Replace(ghaFreshTestGood,"\"head_branch\":\"main\"","\"head_branch\":\"other\"",1)},
        {"wrong-event",strings.Replace(ghaFreshTestGood,"workflow_dispatch","push",1)},
        {"not-done",strings.Replace(ghaFreshTestGood,"completed","in_progress",1)},
        {"failed",strings.Replace(ghaFreshTestGood,"success","failure",1)},
        {"wrong-repo",strings.Replace(ghaFreshTestGood,"1406818713","1406818714",1)},
        {"wrong-actor",strings.Replace(ghaFreshTestGood,"88025750","88025751",1)},
        {"trailing",ghaFreshTestGood+" {}"},
    }
    for _,tc:=range cases {t.Run(tc.name,func(t *testing.T){if _,err:=ghaFreshSelectRunHint([]byte(tc.raw));err==nil{t.Fatal("invalid hint accepted")}})}
}

type ghaFreshTestTransport func(*http.Request)(*http.Response,error)
func (f ghaFreshTestTransport) RoundTrip(r *http.Request)(*http.Response,error){return f(r)}
type ghaFreshTestBody struct { io.Reader; closed bool }
func (b *ghaFreshTestBody) Close() error {b.closed=true;return nil}

func TestGHAFreshRunHintBoundedTransport(t *testing.T) {
    for _,status:=range []int{200,302,403,429} {t.Run(fmt.Sprint(status),func(t *testing.T){
        body:=&ghaFreshTestBody{Reader:bytes.NewReader([]byte(ghaFreshTestGood))}
        calls:=0
        client:=&http.Client{Transport:ghaFreshTestTransport(func(r *http.Request)(*http.Response,error){
            calls++
            if r.Method!=http.MethodGet||r.URL.String()!=ghaFreshListingURL||r.Header.Get("Authorization")!=""||r.Header.Get("Cookie")!="" {t.Fatal("unexpected request")}
            return &http.Response{StatusCode:status,Header:http.Header{},Body:body,Request:r},nil
        })}
        hint,err:=ghaFreshReadRunHint(context.Background(),client)
        if calls!=1||!body.closed {t.Fatal("request/body cleanup mismatch")}
        if status==200 {
            if err!=nil||hint.runID!=12345 {t.Fatal("valid public hint failed")}
        } else {
            if err==nil||hint.runID!=0 {t.Fatal("non-200 response accepted")}
            var statusErr *ghaFreshHTTPStatusError
            if !errors.As(err,&statusErr)||statusErr==nil||statusErr.phase!="runs"||statusErr.status!=status {t.Fatal("public HTTP status lost")}
        }
    })}
    ctx,cancel:=context.WithCancel(context.Background());cancel()
    client:=&http.Client{Transport:ghaFreshTestTransport(func(*http.Request)(*http.Response,error){t.Fatal("cancelled call reached transport");return nil,io.EOF})}
    if _,err:=ghaFreshReadRunHint(ctx,client);err==nil {t.Fatal("cancel accepted")}
}


func ghaFreshLocatorFixture() (string,string,string) {
    workflow:="{\"id\":382798887,\"path\":\".github/workflows/unattended-readiness.yml\",\"state\":\"active\"}"
    names:=[]string{"Capture the fixed guard build","Check the exact captured files","Attest the guard and source evidence","Upload the fixed guard capture","Record the server-assigned capture artifact"}
    steps:=make([]map[string]any,0,len(names))
    for i,name:=range names {steps=append(steps,map[string]any{"name":name,"status":"completed","conclusion":"success","number":i+1})}
    jobs,_:=json.Marshal(map[string]any{"total_count":1,"jobs":[]any{map[string]any{"id":23456,"run_id":12345,"name":"Guard bootstrap artifact","status":"completed","conclusion":"success","started_at":"2026-11-15T17:30:00Z","completed_at":"2026-11-15T18:00:00Z","check_run_url":"https://api.github.com/repos/AutoDevJapan/cc-pc-tools-rs/check-runs/34567","steps":steps}}})
    artifacts,_:=json.Marshal(map[string]any{"total_count":1,"artifacts":[]any{map[string]any{"id":45678,"name":"guard-bootstrap-12345-1-23456","size_in_bytes":10,"digest":"sha256:"+strings.Repeat("a",64),"expired":false,"expires_at":"2026-11-16T12:00:00Z","workflow_run":map[string]any{"id":12345,"repository_id":1406818713,"head_repository_id":1406818713,"head_sha":ghaFreshTestRevision}}}})
    return workflow,string(jobs),string(artifacts)
}

func TestGHAFreshPublicPreflightIsOnlyLocator(t *testing.T) {
    workflow,jobs,artifacts:=ghaFreshLocatorFixture()
    hint,err:=ghaFreshSelectRunHint([]byte(ghaFreshTestGood));if err!=nil{t.Fatal(err)}
    now:=time.Date(2026,11,15,12,0,0,0,time.UTC)
    cases:=[]struct{name,workflow,jobs,artifacts string;valid bool}{
        {"valid",workflow,jobs,artifacts,true},
        {"workflow-inactive",strings.Replace(workflow,"active","disabled",1),jobs,artifacts,false},
        {"wrong-job",workflow,strings.Replace(jobs,"Guard bootstrap artifact","other",1),artifacts,false},
        {"missing-step",workflow,strings.Replace(jobs,"Upload the fixed guard capture","other",1),artifacts,false},
        {"wrong-run",workflow,strings.Replace(jobs,"12345","12346",1),artifacts,false},
        {"wrong-check",workflow,strings.Replace(jobs,"check-runs/34567","check-runs/0",1),artifacts,false},
        {"wrong-artifact",workflow,jobs,strings.Replace(artifacts,"guard-bootstrap-12345","guard-bootstrap-12346",1),false},
        {"expired",workflow,jobs,strings.Replace(artifacts,"\"expired\":false","\"expired\":true",1),false},
        {"old-expiry",workflow,jobs,strings.Replace(artifacts,"2026-11-16T12:00:00Z","2026-11-14T12:00:00Z",1),false},
        {"wrong-digest",workflow,jobs,strings.Replace(artifacts,"sha256:","sha1:",1),false},
        {"wrong-head",workflow,jobs,strings.Replace(artifacts,ghaFreshTestRevision,strings.Repeat("b",40),1),false},
    }
    for _,tc:=range cases {t.Run(tc.name,func(t *testing.T){
        bodies:=[]string{tc.workflow,tc.jobs,tc.artifacts};calls:=0;var bodiesOpened []*ghaFreshTestBody
        client:=&http.Client{Transport:ghaFreshTestTransport(func(r *http.Request)(*http.Response,error){
            if calls>=len(bodies){t.Fatal("unexpected extra request")}
            expected,e:=ghaFreshResourceURL(ghaFreshResource(calls+1),hint);if e!=nil||r.URL.String()!=expected||r.Header.Get("Authorization")!="" {t.Fatal("unexpected metadata request")}
            body:=&ghaFreshTestBody{Reader:strings.NewReader(bodies[calls])};bodiesOpened=append(bodiesOpened,body);calls++
            return &http.Response{StatusCode:200,Header:http.Header{},Body:body,Request:r},nil
        })}
        loc,err:=ghaFreshPublicPreflight(context.Background(),client,hint,now)
        for _,body:=range bodiesOpened {if !body.closed{t.Fatal("metadata response not closed")}}
        if (err==nil)!=tc.valid {t.Fatalf("unexpected preflight result: %v",err)}
        if tc.valid&&(loc.jobID!=23456||loc.checkRunID!=34567||loc.artifactID!=45678||loc.hint.runID!=hint.runID){t.Fatal("locator mismatched")}
        if !tc.valid&&loc.artifactID!=0 {t.Fatal("partial locator escaped")}
    })}
}

func TestGHAFreshRunHintRejectsAdditionalPages(t *testing.T) {
    body:=&ghaFreshTestBody{Reader:strings.NewReader(ghaFreshTestGood)}
    client:=&http.Client{Transport:ghaFreshTestTransport(func(*http.Request)(*http.Response,error){
        header:=make(http.Header);header.Set("Link",`<https://api.github.com/example?page=2>; rel="next"`)
        return &http.Response{StatusCode:http.StatusOK,Header:header,Body:body},nil
    })}
    hint,err:=ghaFreshReadRunHint(context.Background(),client)
    var statusErr *ghaFreshHTTPStatusError
    if err==nil||hint.runID!=0||!body.closed||errors.As(err,&statusErr){t.Fatal("pagination mismatch misclassified")}
}

func TestGHAFreshHintUsesActualRevisionAndFiltersWorkflow(t *testing.T) {
    var envelope struct { Runs []map[string]any `json:"workflow_runs"` }
    if json.Unmarshal([]byte(ghaFreshTestGood),&envelope)!=nil||len(envelope.Runs)!=1 {t.Fatal("fixture")}
    good:=envelope.Runs[0]
    // This server value is a lookup hint, not a candidate or update permit.
    actual:=strings.Repeat("d",40);good["head_sha"]=actual
    other:=map[string]any{"id":23456,"workflow_id":999999}
    old:=map[string]any{"id":34567,"workflow_id":ghaFreshWorkflowID,"created_at":"2026-11-14T08:07:58Z"}
    raw,err:=json.Marshal(map[string]any{"total_count":3,"workflow_runs":[]any{other,old,good}})
    if err!=nil {t.Fatal(err)}
    now:=time.Date(2026,11,16,0,0,0,0,time.UTC)
    hint,err:=ghaFreshSelectRunHintAt(raw,now)
    if err!=nil||hint.producerRevision!=actual||hint.runID!=12345 {t.Fatal("actual revision hint lost",err)}
    // An additional eligible success is ambiguous; do not choose a newest run.
    var clone map[string]any
    b,_:=json.Marshal(good);if json.Unmarshal(b,&clone)!=nil{t.Fatal("clone")};clone["id"]=45678
    raw,_=json.Marshal(map[string]any{"total_count":4,"workflow_runs":[]any{other,old,good,clone}})
    if _,err=ghaFreshSelectRunHintAt(raw,now);err==nil {t.Fatal("ambiguous H1 accepted")}
    raw,_=json.Marshal(map[string]any{"total_count":4,"workflow_runs":[]any{other,old,good}})
    if _,err=ghaFreshSelectRunHintAt(raw,now);err==nil {t.Fatal("incomplete page accepted")}
}

func TestGHAFreshRunHintUsesExplicitArtifactExpiry(t *testing.T) {
    var envelope struct { Runs []map[string]any `json:"workflow_runs"` }
    if json.Unmarshal([]byte(ghaFreshTestGood),&envelope)!=nil||len(envelope.Runs)!=1 {t.Fatal("fixture")}
    first:=envelope.Runs[0]
    rawFirst,_:=json.Marshal(first);var second map[string]any
    if json.Unmarshal(rawFirst,&second)!=nil {t.Fatal("clone")};second["id"]=float64(12346)
    listing,_:=json.Marshal(map[string]any{"total_count":2,"workflow_runs":[]any{first,second}})
    workflow,jobs,artifacts:=ghaFreshLocatorFixture()
    artifacts=strings.ReplaceAll(artifacts,"2026-11-16T12:00:00Z","2099-01-01T00:00:00Z")
    jobs2:=strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(jobs,"12345","12346"),"23456","23457"),"34567","34568")
    artifacts2:=strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(artifacts,"12345","12346"),"23456","23457"),"45678","45679")
    for _,name:=range []string{"expired-and-live","two-live","missing-first","missing-past","missing-unknown","duplicate-first","all-expired"} {
        t.Run(name,func(t *testing.T){
            firstArtifact:=artifacts;secondArtifact:=artifacts2
            if name=="expired-and-live"||name=="all-expired" {firstArtifact=strings.Replace(firstArtifact,"\"expired\":false","\"expired\":true",1)}
            if name=="all-expired" {secondArtifact=strings.Replace(secondArtifact,"\"expired\":false","\"expired\":true",1)}
            if name=="missing-first"||name=="missing-past"||name=="missing-unknown" {firstArtifact="{\"total_count\":0,\"artifacts\":[]}"}
            if name=="duplicate-first" {
                var obj map[string]json.RawMessage;var items []json.RawMessage
                if json.Unmarshal([]byte(firstArtifact),&obj)!=nil||json.Unmarshal(obj["artifacts"],&items)!=nil||len(items)!=1{t.Fatal("artifact fixture")}
                items=append(items,items[0]);encoded,err:=json.Marshal(items);if err!=nil{t.Fatal(err)}
                obj["total_count"]=json.RawMessage("2");obj["artifacts"]=encoded;encoded,err=json.Marshal(obj);if err!=nil{t.Fatal(err)};firstArtifact=string(encoded)
            }
            firstJobs:=jobs
            if name=="missing-unknown" {firstJobs=strings.Replace(firstJobs,"2026-11-15T18:00:00Z","not-a-time",1)}
            client:=&http.Client{Transport:ghaFreshTestTransport(func(r *http.Request)(*http.Response,error){
                body:=""
                switch {
                case r.URL.String()==ghaFreshListingURL:body=string(listing)
                case strings.HasSuffix(r.URL.Path,"/actions/workflows/unattended-readiness.yml"):body=workflow
                case strings.HasSuffix(r.URL.Path,"/contents/.github/workflows/unattended-readiness.yml"):body=`{}`
                case strings.HasSuffix(r.URL.Path,"/actions/runs/12345/attempts/1/jobs"):body=firstJobs
                case strings.HasSuffix(r.URL.Path,"/actions/runs/12346/attempts/1/jobs"):body=jobs2
                case strings.HasSuffix(r.URL.Path,"/actions/runs/12345/artifacts"):body=firstArtifact
                case strings.HasSuffix(r.URL.Path,"/actions/runs/12346/artifacts"):body=secondArtifact
                default:t.Fatal("unexpected request");return nil,io.EOF
                }
                return &http.Response{StatusCode:http.StatusOK,Header:make(http.Header),Body:io.NopCloser(strings.NewReader(body)),Request:r},nil
            })}
            now:=time.Date(2026,11,16,12,0,0,0,time.UTC)
            if name=="missing-past"||name=="missing-unknown" {now=time.Date(2026,11,17,12,0,0,0,time.UTC)}
            hint,err:=ghaFreshReadRunHintWith(context.Background(),client,client,now)
            if name=="expired-and-live" {if err!=nil||hint.runID!=12346 {t.Fatal("expired candidate not separated",name,err)}} else if err==nil||hint.runID!=0 {t.Fatal("unproved multiple candidates accepted",name)}
        })
    }
}

// Pages are constructed from the fixed workflow, never from a server Link.
// More than 100 unrelated repository runs are excluded by the request itself.
func TestGHAFreshWorkflowPaginationRequiresCompleteStablePages(t *testing.T) {
    for _,mode:=range []string{"normal","missing","duplicate","changed","pending","foreign-workflow","too-many"} {t.Run(mode,func(t *testing.T){
        var envelope struct{Runs []map[string]any `json:"workflow_runs"`}
        if json.Unmarshal([]byte(ghaFreshTestGood),&envelope)!=nil||len(envelope.Runs)!=1 {t.Fatal("fixture")}
        good:=envelope.Runs[0];noise:=make([]map[string]any,100)
        for i:=range noise {noise[i]=map[string]any{"id":int64(50000+i),"workflow_id":ghaFreshWorkflowID,"created_at":"2026-11-15T17:21:18Z","status":"completed","conclusion":"failure","run_attempt":1}}
        calls:=0
        client:=&http.Client{Transport:ghaFreshTestTransport(func(r *http.Request)(*http.Response,error){
            calls++;if r.Method!=http.MethodGet||r.URL.Path!="/repos/AutoDevJapan/cc-pc-tools-rs/actions/workflows/382798887/runs" {t.Fatal("wrong fixed workflow request")}
            page:=1;if r.URL.RawQuery==ghaFreshListingQuery+"&page=2" {page=2} else if r.URL.RawQuery!=ghaFreshListingQuery {t.Fatal("wrong query")}
            total:=101;var runs []map[string]any;header:=make(http.Header)
            if page==1 {runs=append(runs,noise...);header.Set("Link",`<https://api.github.com/ignored?page=2>; rel="next"`)} else {runs=[]map[string]any{good};header.Set("Link",`<https://api.github.com/ignored?page=1>; rel="prev"`)}
            if mode=="missing"&&page==2 {runs=nil}
            if mode=="duplicate"&&page==2 {dup:=map[string]any{};for k,v:=range good{dup[k]=v};dup["id"]=int64(50000);runs=[]map[string]any{dup}}
            if mode=="pending"&&page==1 {bad:=map[string]any{};for k,v:=range noise[0]{bad[k]=v};bad["status"]="in_progress";runs[0]=bad}
            if mode=="foreign-workflow"&&page==1 {bad:=map[string]any{};for k,v:=range noise[0]{bad[k]=v};bad["workflow_id"]=int64(999);runs[0]=bad}
            if mode=="changed"&&page==2&&calls>2 {other:=map[string]any{};for k,v:=range good{other[k]=v};other["id"]=int64(12346);runs=[]map[string]any{other}}
            if mode=="too-many" {total=1000}
            raw,err:=json.Marshal(map[string]any{"total_count":total,"workflow_runs":runs});if err!=nil{t.Fatal(err)}
            return &http.Response{StatusCode:http.StatusOK,Header:header,Body:io.NopCloser(bytes.NewReader(raw)),Request:r},nil
        })}
        hint,err:=ghaFreshReadRunHintWith(context.Background(),client,client,time.Date(2026,11,16,0,0,0,0,time.UTC))
        if mode=="normal" {if err!=nil||hint.runID!=12345||calls!=4{t.Fatal("complete pages rejected",err,calls)}} else if err==nil||hint.runID!=0 {t.Fatal("incomplete or unstable pages admitted",mode)}
    })}
}

func TestGHAFreshMissingExpiryRequiresCompleteTimedEvidence(t *testing.T) {
    _,jobsRaw,_:=ghaFreshLocatorFixture()
    jobs,err:=ghaFreshObject([]byte(jobsRaw));if err!=nil{t.Fatal(err)}
    artifacts,err:=ghaFreshObject([]byte(`{"total_count":0,"artifacts":[]}`));if err!=nil{t.Fatal(err)}
    hint:=ghaFreshRunHint{runID:12345,attempt:1,producerRevision:ghaFreshTestRevision,createdAt:time.Date(2026,11,15,17,21,18,0,time.UTC)}
    justBefore:=time.Date(2026,11,16,18,0,59,0,time.UTC)
    boundary:=time.Date(2026,11,16,18,1,0,0,time.UTC)
    if ghaFreshMissingPastRetention(jobs,artifacts,hint,23456,justBefore){t.Fatal("premature expiry")}
    if !ghaFreshMissingPastRetention(jobs,artifacts,hint,23456,boundary){t.Fatal("completed one-day retention not recognized")}
    missingTime:=hint;missingTime.createdAt=time.Time{}
    if ghaFreshMissingPastRetention(jobs,artifacts,missingTime,23456,boundary){t.Fatal("unknown run creation accepted")}
    lateCreation:=hint;lateCreation.createdAt=time.Date(2026,11,15,17,40,0,0,time.UTC)
    if ghaFreshMissingPastRetention(jobs,artifacts,lateCreation,23456,boundary){t.Fatal("job preceding run creation accepted")}
    for _,bad:=range []string{
        strings.Replace(jobsRaw,"2026-11-15T18:00:00Z","not-a-time",1),
        strings.Replace(jobsRaw,"2026-11-15T17:30:00Z","2026-11-15T16:00:00Z",1),
        strings.Replace(jobsRaw,"2026-11-15T17:30:00Z","2026-11-15T19:00:00Z",1),
        strings.Replace(jobsRaw,"\"total_count\":1","\"total_count\":2",1),
    } {item,e:=ghaFreshObject([]byte(bad));if e!=nil{t.Fatal(e)};if ghaFreshMissingPastRetention(item,artifacts,hint,23456,boundary){t.Fatal("unknown completion accepted")}}
    incomplete,err:=ghaFreshObject([]byte(`{"total_count":1,"artifacts":[]}`));if err!=nil{t.Fatal(err)}
    if ghaFreshMissingPastRetention(jobs,incomplete,hint,23456,boundary){t.Fatal("incomplete artifact listing accepted")}
    duplicate,err:=ghaFreshObject([]byte(`{"total_count":2,"artifacts":[{"id":9,"name":"other"},{"id":9,"name":"other2"}]}`));if err!=nil{t.Fatal(err)}
    if ghaFreshMissingPastRetention(jobs,duplicate,hint,23456,boundary){t.Fatal("duplicate artifact ID accepted as missing")}
}

func TestGHAFreshWorkflowScopedListingIgnoresOtherRunVolume(t *testing.T) {
    var envelope struct{Runs []map[string]any `json:"workflow_runs"`}
    if json.Unmarshal([]byte(ghaFreshTestGood),&envelope)!=nil||len(envelope.Runs)!=1{t.Fatal("fixture")}
    all:=make([]map[string]any,0,102)
    for i:=0;i<101;i++ {all=append(all,map[string]any{"id":int64(60000+i),"workflow_id":int64(999)})}
    envelope.Runs[0]["workflow_id"]=ghaFreshWorkflowID
    all=append(all,envelope.Runs[0]);calls:=0
    client:=&http.Client{Transport:ghaFreshTestTransport(func(r *http.Request)(*http.Response,error){
        calls++;if r.URL.String()!=ghaFreshListingURL{t.Fatal("listing was not scoped to fixed workflow and date")}
        selected:=make([]map[string]any,0,1)
        for _,v:=range all {if fmt.Sprint(v["workflow_id"])==fmt.Sprint(ghaFreshWorkflowID){selected=append(selected,v)}}
        raw,_:=json.Marshal(map[string]any{"total_count":len(selected),"workflow_runs":selected})
        return &http.Response{StatusCode:200,Header:make(http.Header),Body:io.NopCloser(bytes.NewReader(raw)),Request:r},nil
    })}
    hint,err:=ghaFreshReadRunHintWith(context.Background(),client,client,time.Date(2026,11,16,0,0,0,0,time.UTC))
    if err!=nil||hint.runID!=12345||calls!=1{t.Fatal("unrelated runs blocked fixed workflow",err)}
}
