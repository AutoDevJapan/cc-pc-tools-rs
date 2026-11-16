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

const ghaFreshTestGood = "{\"total_count\":1,\"workflow_runs\":[{\"id\":12345,\"run_attempt\":1,\"workflow_id\":382798887,\"head_sha\":\"5a28f6fa6d9845bc1cd6fe5352abbcda275bd099\",\"head_branch\":\"main\",\"path\":\".github/workflows/unattended-readiness.yml\",\"event\":\"workflow_dispatch\",\"status\":\"completed\",\"conclusion\":\"success\",\"repository\":{\"id\":1406818713},\"head_repository\":{\"id\":1406818713},\"actor\":{\"id\":88025750},\"triggering_actor\":{\"id\":88025750}}]}"

func TestGHAFreshRunHintRejectsAmbiguity(t *testing.T) {
    good,err:=ghaFreshSelectRunHint([]byte(ghaFreshTestGood))
    if err!=nil||good.runID!=12345||good.attempt!=1||good.producerRevision!=ghaFreshProducerRevision {t.Fatal("fixed hint rejected")}
    cases:=[]struct{name,raw string}{
        {"zero",strings.Replace(ghaFreshTestGood,"\"total_count\":1","\"total_count\":0",1)},
        {"multiple",strings.Replace(ghaFreshTestGood,"\"total_count\":1","\"total_count\":2",1)},
        {"duplicate",strings.Replace(ghaFreshTestGood,"\"id\":12345","\"id\":12345,\"id\":12345",1)},
        {"case-fold",strings.Replace(ghaFreshTestGood,"\"id\":12345","\"id\":12345,\"ID\":12345",1)},
        {"wrong-attempt",strings.Replace(ghaFreshTestGood,"\"run_attempt\":1","\"run_attempt\":2",1)},
        {"wrong-workflow",strings.Replace(ghaFreshTestGood,"382798887","382798888",1)},
        {"wrong-revision",strings.Replace(ghaFreshTestGood,ghaFreshProducerRevision,strings.Repeat("a",40),1)},
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
    jobs,_:=json.Marshal(map[string]any{"total_count":1,"jobs":[]any{map[string]any{"id":23456,"run_id":12345,"name":"Guard bootstrap artifact","status":"completed","conclusion":"success","check_run_url":"https://api.github.com/repos/AutoDevJapan/cc-pc-tools-rs/check-runs/34567","steps":steps}}})
    artifacts,_:=json.Marshal(map[string]any{"total_count":1,"artifacts":[]any{map[string]any{"id":45678,"name":"guard-bootstrap-12345-1-23456","size_in_bytes":10,"digest":"sha256:"+strings.Repeat("a",64),"expired":false,"expires_at":"2026-11-16T12:00:00Z","workflow_run":map[string]any{"id":12345,"repository_id":1406818713,"head_repository_id":1406818713,"head_sha":ghaFreshProducerRevision}}}})
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
        {"wrong-head",workflow,jobs,strings.Replace(artifacts,ghaFreshProducerRevision,strings.Repeat("b",40),1),false},
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
