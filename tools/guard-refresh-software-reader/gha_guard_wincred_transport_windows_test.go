//go:build windows

package main

import (
    "bytes"
    "context"
    "errors"
    "io"
    "net/http"
    "net/http/httptrace"
    "strings"
    "sync/atomic"
    "testing"
    "time"
)

type ghaGuardCredentialTestRT func(*http.Request)(*http.Response,error)
func (f ghaGuardCredentialTestRT) RoundTrip(r *http.Request)(*http.Response,error){return f(r)}

func ghaGuardCredentialTestSession(base http.RoundTripper) *ghaGuardCredentialSession {
    root,cancel:=context.WithCancel(context.Background())
    return &ghaGuardCredentialSession{secret:[]byte("unit_only_token"),base:base,root:root,cancel:cancel,drain:make(chan struct{})}
}

func TestGHAGuardCredentialExactPolicy(t *testing.T) {
    tests:=[]struct{method,url string;valid bool}{
        {"GET","https://api.github.com/user",true},
        {"GET","https://api.github.com/repos/AutoDevJapan/cc-pc-tools-rs",true},
        {"GET","https://api.github.com/repos/AutoDevJapan/cc-pc-tools-rs/actions/runs/43574333045",true},
        {"GET","https://api.github.com/repos/AutoDevJapan/cc-pc-tools-rs/actions/runs/43574333045/attempts/1/jobs?per_page=100",true},
        {"GET","https://api.github.com/repos/AutoDevJapan/cc-pc-tools-rs/actions/jobs/132385166063/logs",true},
        {"GET","https://api.github.com/repos/AutoDevJapan/cc-pc-tools-rs/actions/artifacts/14105209772/zip",true},
        {"GET","https://api.github.com/repos/AutoDevJapan/cc-pc-tools-rs/git/blobs/44c449b0d19a86de14cacb8d0c799fa96f16dc8b",true},
        {"POST","https://api.github.com/user",false},
        {"GET","http://api.github.com/user",false},
        {"GET","https://api.github.com.evil.invalid/user",false},
        {"GET","https://api.github.com:443/user",false},
        {"GET","https://user@api.github.com/user",false},
        {"GET","https://api.github.com/user?x=1",false},
        {"GET","https://api.github.com/user?",false},
        {"GET","https://api.github.com/repos/Other/cc-pc-tools-rs",false},
        {"GET","https://api.github.com/repos/AutoDevJapan/cc-pc-tools-rs/actions/jobs/999/logs",false},
        {"GET","https://api.github.com/repos/AutoDevJapan/cc-pc-tools-rs/actions/runs/43574333045/artifacts?per_page=100&x=1",false},
        {"GET","https://api.github.com/repos/AutoDevJapan/cc-pc-tools-rs/git/blobs/NOTHEX",false},
    }
    for _,tc:=range tests {req,err:=http.NewRequest(tc.method,tc.url,nil);if err!=nil{t.Fatal(err)};if got:=ghaGuardCredentialAllowed(req);got!=tc.valid{t.Fatalf("policy mismatch for %s",tc.url)}}
    req,_:=http.NewRequest("GET","https://api.github.com/user",nil)
    req.Header["authorization"]=[]string{"caller_value"}
    if ghaGuardCredentialAllowed(req){t.Fatal("caller authorization accepted")}
    if ghaGuardFixedCredentialTarget!="gh:github.com:mashfromband"{t.Fatal("credential target changed")}
}

func TestGHAGuardCredentialOwnerIdentity(t *testing.T) {
    tests:=[]struct{raw string;valid bool}{
        {`{"id":88025750,"login":"mashfromband"}`,true},
        {`{"login":"MashFromBand","id":88025750,"ignored":"x"}`,true},
        {`{"id":1,"login":"mashfromband"}`,false},
        {`{"id":88025750,"login":"other"}`,false},
        {`{"id":"88025750","login":"mashfromband"}`,false},
        {`{"id":88025750,"id":88025750,"login":"mashfromband"}`,false},
        {`{"id":88025750,"Login":"mashfromband"}`,false},
        {`{"id":88025750,"login":"mashfromband"} {}`,false},
    }
    for _,tc:=range tests {if ghaGuardCredentialOwnerJSON([]byte(tc.raw))!=tc.valid {t.Fatalf("identity mismatch for fixture")}}
}

func TestGHAGuardCredentialValueAndExpiry(t *testing.T) {
    if !ghaGuardCredentialBytesValid([]byte("unit_only_token")) || ghaGuardCredentialBytesValid([]byte("bad\nvalue")) || ghaGuardCredentialBytesValid([]byte("===")) || ghaGuardCredentialBytesValid([]byte("{\"not\":\"token\"}")) || ghaGuardCredentialBytesValid(make([]byte,2561)) {t.Fatal("credential format")}
    expiry:=time.Date(2026,11,15,8,9,29,0,time.UTC)
    if !ghaGuardFixedArtifactWindow(expiry.Add(-time.Nanosecond)) || ghaGuardFixedArtifactWindow(expiry) || ghaGuardFixedArtifactWindow(time.Time{}) {t.Fatal("expiry boundary")}
}

func TestGHAGuardCredentialTransportScopeAndCleanup(t *testing.T) {
    var calls atomic.Int64
    base:=ghaGuardCredentialTestRT(func(req *http.Request)(*http.Response,error){
        calls.Add(1)
        if req.Header.Get("Authorization")!="Bearer unit_only_token" || httptrace.ContextClientTrace(req.Context())!=nil {t.Error("credential context was not isolated")}
        return &http.Response{StatusCode:200,Header:http.Header{},Body:io.NopCloser(bytes.NewReader([]byte("{}"))),Request:req},nil
    })
    s:=ghaGuardCredentialTestSession(base)
    req,_:=http.NewRequest("GET","https://api.github.com/repos/AutoDevJapan/cc-pc-tools-rs/actions/runs/43574333045",nil)
    if _,err:=s.RoundTrip(req);err==nil||calls.Load()!=0 {t.Fatal("unvalidated session used")}
    s.ready=true
    ctx:=httptrace.WithClientTrace(context.Background(),&httptrace.ClientTrace{})
    req=req.WithContext(ctx)
    response,err:=s.RoundTrip(req)
    if err!=nil||response==nil||req.Header.Get("Authorization")!="" {t.Fatal("credential route failed")}
    if _,err=io.ReadAll(response.Body);err!=nil {t.Fatal(err)}
    if err=response.Body.Close();err!=nil {t.Fatal(err)}
    if err=s.close(context.Background());err!=nil||len(s.secret)!=0 {t.Fatal("credential cleanup failed")}
    if _,err=s.RoundTrip(req);err==nil||calls.Load()!=1 {t.Fatal("closed session reused")}
}

func TestGHAGuardCredentialTransportRedactionAndPendingClose(t *testing.T) {
    fail:=ghaGuardCredentialTestSession(ghaGuardCredentialTestRT(func(*http.Request)(*http.Response,error){return nil,errors.New("unit_only_token") }))
    fail.ready=true;req,_:=http.NewRequest("GET","https://api.github.com/user",nil)
    if _,err:=fail.RoundTrip(req);err==nil||strings.Contains(err.Error(),"unit_only_token"){t.Fatal("transport error leaked")}
    if err:=fail.close(context.Background());err!=nil {t.Fatal(err)}
    pending:=ghaGuardCredentialTestSession(ghaGuardCredentialTestRT(func(r *http.Request)(*http.Response,error){return &http.Response{StatusCode:200,Header:http.Header{},Body:io.NopCloser(bytes.NewReader([]byte("x"))),Request:r},nil}))
    pending.ready=true;response,err:=pending.RoundTrip(req);if err!=nil {t.Fatal(err)}
    ctx,cancel:=context.WithCancel(context.Background());cancel()
    if pending.close(ctx)==nil||len(pending.secret)!=0 {t.Fatal("pending close falsely completed")}
    if err=response.Body.Close();err!=nil {t.Fatal(err)}
    if err=pending.close(context.Background());err!=nil {t.Fatal(err)}
}

func TestGHAGuardCredentialStatusCategories(t *testing.T) {
    if ghaGuardCredentialStatusError(http.StatusOK)!=nil {t.Fatal("successful status rejected")}
    if !errors.Is(ghaGuardCredentialStatusError(http.StatusUnauthorized),errGHAGuardCredentialUnauthorized) {t.Fatal("401 not preserved")}
    if !errors.Is(ghaGuardCredentialStatusError(http.StatusForbidden),errGHAGuardCredentialForbidden) {t.Fatal("403 not preserved")}
    if !errors.Is(ghaGuardCredentialStatusError(http.StatusBadGateway),errGHAGuardCredentialStatus) {t.Fatal("other status not classified")}
}


func TestGHAGuardCredentialRequestLifetime(t *testing.T) {
    for _,scenario:=range []string{"success","caller-cancel","close-timeout","transport-error"} {
        t.Run(scenario,func(t *testing.T){
            gate:=make(chan struct{});observed:=make(chan string,1);released:=false
            defer func(){if !released {close(gate)}}()
            base:=ghaGuardCredentialTestRT(func(r *http.Request)(*http.Response,error){
                go func(){
                    select {
                    case <-gate: observed<-r.Header.Get("Authorization")
                    case <-time.After(2*time.Second): observed<-"observation-timeout"
                    }
                }()
                if scenario=="transport-error" {return nil,errors.New("unit_only_token")}
                return &http.Response{StatusCode:http.StatusOK,Header:http.Header{},Body:io.NopCloser(bytes.NewReader([]byte("ok"))),Request:r},nil
            })
            s:=ghaGuardCredentialTestSession(base);s.ready=true
            ctx,cancel:=context.WithCancel(context.Background());defer cancel()
            req,_:=http.NewRequestWithContext(ctx,http.MethodGet,"https://api.github.com/user",nil)
            response,err:=s.RoundTrip(req)
            if scenario=="transport-error" {
                if err==nil||strings.Contains(err.Error(),"unit_only_token")||response!=nil {t.Fatal("transport error not sanitized")}
            } else if err!=nil||response==nil||response.Request!=req||req.Header.Get("Authorization")!="" {
                t.Fatal("unexpected transport result")
            }
            if scenario=="caller-cancel" {cancel()}
            if scenario=="close-timeout" {
                closedCtx,stop:=context.WithCancel(context.Background());stop()
                if s.close(closedCtx)==nil {t.Fatal("active body falsely drained")}
            }
            close(gate);released=true
            select {
            case value:=<-observed:
                if value!="Bearer unit_only_token" {t.Fatal("transport request mutated before its final use")}
            case <-time.After(3*time.Second):t.Fatal("late request read did not complete")
            }
            if response!=nil {if err:=response.Body.Close();err!=nil {t.Fatal(err)}}
            endCtx,end:=context.WithTimeout(context.Background(),time.Second);defer end()
            if err:=s.close(endCtx);err!=nil||len(s.secret)!=0 {t.Fatal("credential session not drained")}
        })
    }
}
