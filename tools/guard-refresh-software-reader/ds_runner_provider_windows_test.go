//go:build windows

package main

import (
    "bytes"
    "context"
    "errors"
    "fmt"
    "io"
    "net/http"
    "strings"
    "testing"
    "time"
)

type dsRunnerFixtureTransport func(*http.Request)(*http.Response,error)
func(f dsRunnerFixtureTransport)RoundTrip(r *http.Request)(*http.Response,error){return f(r)}
type dsRunnerFixtureBody struct{*bytes.Reader;closed int;closeErr error}
func(b *dsRunnerFixtureBody)Close()error{b.closed++;return b.closeErr}
func TestDSRunnerServerJSONRequiresExactCompleteCriticalFields(t *testing.T){
    type item struct{ID int64;Status string}
    type document struct{ID int64;Expired bool;Items []item;Expires time.Time}
    for _,test:=range []struct{name,raw string;valid bool}{
        {"complete","{\"id\":23,\"expired\":false,\"items\":[{\"id\":31,\"status\":\"ok\"}],\"expires\":\"2030-01-01T00:00:00Z\"}",true},
        {"unknown-addition","{\"id\":23,\"expired\":false,\"items\":[{\"id\":31,\"status\":\"ok\",\"future\":null}],\"expires\":\"2030-01-01T00:00:00Z\",\"future\":{\"value\":[1,2]}}",true},
        {"missing-false-flag","{\"id\":23,\"items\":[],\"expires\":\"2030-01-01T00:00:00Z\"}",false},
        {"null-false-flag","{\"id\":23,\"expired\":null,\"items\":[],\"expires\":\"2030-01-01T00:00:00Z\"}",false},
        {"alias-only","{\"ID\":23,\"expired\":false,\"items\":[],\"expires\":\"2030-01-01T00:00:00Z\"}",false},
        {"alias-duplicate","{\"id\":23,\"ID\":23,\"expired\":false,\"items\":[],\"expires\":\"2030-01-01T00:00:00Z\"}",false},
        {"nested-missing","{\"id\":23,\"expired\":false,\"items\":[{\"id\":31}],\"expires\":\"2030-01-01T00:00:00Z\"}",false},
        {"nested-unicode-alias","{\"id\":23,\"expired\":false,\"items\":[{\"id\":31,\"status\":\"ok\",\"\u017ftatus\":\"ok\"}],\"expires\":\"2030-01-01T00:00:00Z\"}",false},
        {"null-row","{\"id\":23,\"expired\":false,\"items\":[null],\"expires\":\"2030-01-01T00:00:00Z\"}",false},
        {"null-array","{\"id\":23,\"expired\":false,\"items\":null,\"expires\":\"2030-01-01T00:00:00Z\"}",false},
        {"wrong-number","{\"id\":\"23\",\"expired\":false,\"items\":[],\"expires\":\"2030-01-01T00:00:00Z\"}",false},
        {"wrong-time","{\"id\":23,\"expired\":false,\"items\":[],\"expires\":7}",false},
        {"trailing","{\"id\":23,\"expired\":false,\"items\":[],\"expires\":\"2030-01-01T00:00:00Z\"}{}",false},
    }{t.Run(test.name,func(t *testing.T){out:=document{ID:999,Expired:true};err:=dsRunnerDecodeServerJSON([]byte(test.raw),&out);if test.valid{if err!=nil||out.ID!=23||out.Expired{t.Fatal("valid server document rejected")}}else if err==nil||out.ID!=0{t.Fatal("invalid document was accepted or stale output retained")}})}
}
func TestDSRunnerCredentialRequestAndBodyOwnership(t *testing.T){
    pin:=dsRunnerFixturePin([]byte("payload"));root,cancel:=context.WithCancel(context.Background());defer cancel();body:=&dsRunnerFixtureBody{Reader:bytes.NewReader([]byte("ok"))};calls:=0
    s:=&dsRunnerCredential{secret:[]byte("synthetic"),pin:pin,root:root,cancel:cancel,ready:true,done:make(chan struct{})}
    s.base=dsRunnerFixtureTransport(func(r *http.Request)(*http.Response,error){calls++;if r.Header.Get("Authorization")!="Bearer synthetic"{t.Fatal("credential not limited to internal request")};return &http.Response{StatusCode:200,Body:body,Header:make(http.Header)},nil})
    cleanup,c:=context.WithTimeout(context.Background(),time.Second);defer c();defer s.Close(cleanup)
    request,_:=http.NewRequestWithContext(context.Background(),http.MethodGet,dsRunnerAPI,nil);request.Header.Set("Accept","application/vnd.github+json");request.Header.Set("X-GitHub-Api-Version","2022-11-28")
    response,err:=s.RoundTrip(request);if err!=nil{t.Fatal(err)};if request.Header.Get("Authorization")!=""{t.Fatal("caller request acquired credentials")};s.mu.Lock();active:=s.active;s.mu.Unlock();if active!=1{t.Fatal("response body did not retain request ownership")}
    raw,err:=io.ReadAll(response.Body);if err!=nil||string(raw)!="ok"{t.Fatal("body read")};if response.Body.Close()!=nil||response.Body.Close()!=nil{t.Fatal("close")};s.mu.Lock();active=s.active;s.mu.Unlock();if active!=0||body.closed!=1||calls!=1{t.Fatal("response was not released exactly once")}
    forbidden,_:=http.NewRequest(http.MethodGet,"https://example.invalid/",nil);if _,err=s.RoundTrip(forbidden);err==nil||calls!=1{t.Fatal("credential reached another destination")}
    if s.Close(cleanup)!=nil{t.Fatal("credential close")};if _,err=s.RoundTrip(request);err==nil||calls!=1{t.Fatal("closed credential reused")}
}
func TestDSRunnerCredentialRoundTripPanicDoesNotLeakActive(t *testing.T){
    root,cancel:=context.WithCancel(context.Background());defer cancel();s:=&dsRunnerCredential{secret:[]byte("synthetic"),pin:dsRunnerFixturePin([]byte("payload")),root:root,cancel:cancel,ready:true,done:make(chan struct{}),base:dsRunnerFixtureTransport(func(*http.Request)(*http.Response,error){panic("synthetic transport")})}
    request,_:=http.NewRequest(http.MethodGet,dsRunnerAPI,nil);panicked:=false;func(){defer func(){panicked=recover()!=nil}();_,_=s.RoundTrip(request)}();if !panicked{t.Fatal("fixture missed transport")};s.mu.Lock();n:=s.active;s.mu.Unlock();if n!=0{t.Fatal("transport panic leaked active request")};ctx,c:=context.WithTimeout(context.Background(),time.Second);defer c();if s.Close(ctx)!=nil{t.Fatal("session did not close after panic")}
}
func TestDSRunnerHTTPConsumerPreservesCleanupFailure(t *testing.T){
    body:=&dsRunnerFixtureBody{Reader:bytes.NewReader([]byte("ok")),closeErr:errors.New("synthetic close")};client:=&http.Client{Transport:dsRunnerFixtureTransport(func(*http.Request)(*http.Response,error){return &http.Response{StatusCode:200,Header:make(http.Header),Body:body},nil})}
    raw,err:=dsRunnerHTTPBytes(context.Background(),client,dsRunnerAPI,64);if raw!=nil||!errors.Is(err,errDSRunnerCleanup)||body.closed!=1{t.Fatal("cleanup failure became successful evidence")}
}
func TestDSRunnerUnboundOrCancelledAuthorityDoesNotReadCredentials(t *testing.T){
    if _,err:=openDSRunnerAuthority(context.Background(),dsRunnerCapturePin{});err==nil{t.Fatal("unbound pin accepted")};if _,err:=openNormalDSRunnerCandidate(nil);err==nil{t.Fatal("nil context accepted")}
    ctx,cancel:=context.WithCancel(context.Background());cancel();if _,err:=openNormalDSRunnerCandidate(ctx);err==nil{t.Fatal("cancelled normal factory accepted")}
}
func TestDSRunnerServerEvidenceKeepsCleanupFactWithPrimaryFailure(t *testing.T){
    pin:=dsRunnerFixturePin([]byte("payload"))
    for _,test:=range []struct{name string;status int;closeErr error;cleanup bool}{
        {"body-close-failure",200,errors.New("synthetic close"),true},
        {"status-without-close-failure",500,nil,false},
    }{t.Run(test.name,func(t *testing.T){
        client:=&http.Client{Transport:dsRunnerFixtureTransport(func(*http.Request)(*http.Response,error){return &http.Response{StatusCode:test.status,Header:make(http.Header),Body:&dsRunnerFixtureBody{Reader:bytes.NewReader([]byte("{}")),closeErr:test.closeErr}},nil})}
        _,matchErr:=dsRunnerServerMatches(context.Background(),client,pin,time.Now().UTC())
        sourceErr:=dsRunnerVerifyProducerSource(context.Background(),client,pin)
        for _,err:=range []error{matchErr,sourceErr}{if !errors.Is(err,errDSRunnerEvidence)||errors.Is(err,errDSRunnerCleanup)!=test.cleanup{t.Fatal("primary evidence failure or cleanup fact was lost")}}
    })}
}
func TestDSRunnerCleanupFactRecognizesBothResponseMarkers(t *testing.T){
    for _,test:=range []struct{name string;err error;fact bool}{
        {"nil",nil,false},
        {"plain",errors.New("synthetic failure"),false},
        {"evidence-only",errDSRunnerEvidence,false},
        {"runner-marker",errors.Join(errDSRunnerEvidence,errDSRunnerCleanup),true},
        {"guard-response-marker",errors.Join(errGHAGuardUnqualified,errGHAGuardResponseCleanup),true},
    }{
        got:=dsRunnerEvidenceFailure(test.err)
        if dsRunnerCleanupFact(test.err)!=test.fact||!errors.Is(got,errDSRunnerEvidence)||errors.Is(got,errDSRunnerCleanup)!=test.fact{t.Fatalf("%s: cleanup fact classification is wrong",test.name)}
    }
}
func dsRunnerFixtureServerDocuments(p dsRunnerCapturePin)map[string]string{
    steps:=`[{"name":"Capture the fixed runner build","status":"completed","conclusion":"success","number":1},{"name":"Check the exact captured files","status":"completed","conclusion":"success","number":2},{"name":"Attest the runner and source evidence","status":"completed","conclusion":"success","number":3},{"name":"Upload the fixed runner capture","status":"completed","conclusion":"success","number":4},{"name":"Record the server-assigned capture artifact","status":"completed","conclusion":"success","number":5}]`
    prefix:="/repos/AutoDevJapan/cc-pc-tools-rs"
    return map[string]string{
        "repository":`{"id":1406818713,"full_name":"AutoDevJapan/cc-pc-tools-rs","private":false,"archived":false}`,
        "workflow":fmt.Sprintf(`{"id":%d,"path":%q,"state":"active"}`,p.WorkflowID,dsRunnerWorkflow),
        "run":fmt.Sprintf(`{"id":%d,"run_attempt":1,"event":"workflow_dispatch","path":%q,"status":"completed","conclusion":"success","workflow_id":%d,"head_sha":%q,"head_branch":"main","repository":{"id":1406818713},"head_repository":{"id":1406818713},"actor":{"id":88025750,"login":"mashfromband"},"triggering_actor":{"id":88025750,"login":"mashfromband"}}`,p.RunID,dsRunnerWorkflow,p.WorkflowID,p.Producer),
        "jobs":fmt.Sprintf(`{"total_count":1,"jobs":[{"id":%d,"run_id":%d,"head_sha":%q,"check_run_url":"%s/check-runs/%d","name":"Task runner software artifact","status":"completed","conclusion":"success","steps":%s}]}`,p.JobID,p.RunID,p.Producer,dsRunnerAPI,p.CheckRunID,steps),
        "artifacts":fmt.Sprintf(`{"total_count":1,"artifacts":[{"id":%d,"name":%q,"digest":"sha256:%s","expired":false,"size_in_bytes":1234,"expires_at":"2099-01-01T00:00:00Z","workflow_run":{"id":%d,"repository_id":1406818713,"head_repository_id":1406818713,"head_sha":%q}}]}`,p.ArtifactID,dsRunnerArtifactName(p),p.ArchiveSHA,p.RunID,p.Producer),
        "path:repository":prefix,
        "path:workflow":prefix+"/actions/workflows/task-runner-capture.yml",
        "path:run":fmt.Sprintf("%s/actions/runs/%d",prefix,p.RunID),
        "path:jobs":fmt.Sprintf("%s/actions/runs/%d/attempts/1/jobs",prefix,p.RunID),
        "path:artifacts":fmt.Sprintf("%s/actions/runs/%d/artifacts",prefix,p.RunID),
    }
}
func dsRunnerFixtureServerClient(documents map[string]string)*http.Client{
    byPath:=map[string]string{}
    for _,name:=range []string{"repository","workflow","run","jobs","artifacts"}{byPath[documents["path:"+name]]=documents[name]}
    return &http.Client{Transport:dsRunnerFixtureTransport(func(r *http.Request)(*http.Response,error){
        document,ok:=byPath[r.URL.Path];status:=200;if !ok{status=404}
        return &http.Response{StatusCode:status,Header:make(http.Header),Body:&dsRunnerFixtureBody{Reader:bytes.NewReader([]byte(document))}},nil
    })}
}
func TestDSRunnerServerMatchesAcceptsCompleteDocumentsAndRejectsEachChange(t *testing.T){
    pin:=dsRunnerFixturePin([]byte("payload"))
    size,err:=dsRunnerServerMatches(context.Background(),dsRunnerFixtureServerClient(dsRunnerFixtureServerDocuments(pin)),pin,time.Now().UTC())
    if err!=nil||size!=1234{t.Fatal("complete synthetic server documents were rejected")}
    for _,test:=range []struct{name,document,from,to string}{
        {"private-repository","repository",`"private":false`,`"private":true`},
        {"missing-repository-field","repository",`,"archived":false`,""},
        {"inactive-workflow","workflow",`"state":"active"`,`"state":"disabled_manually"`},
        {"failed-run","run",`"conclusion":"success"`,`"conclusion":"failure"`},
        {"other-actor","run",`"actor":{"id":88025750`,`"actor":{"id":1`},
        {"failed-job","jobs",`"conclusion":"success"`,`"conclusion":"failure"`},
        {"missing-step","jobs",`,{"name":"Record the server-assigned capture artifact","status":"completed","conclusion":"success","number":5}`,""},
        {"expired-artifact","artifacts",`"expired":false`,`"expired":true`},
        {"other-digest","artifacts",`"digest":"sha256:`,`"digest":"sha256:0`},
    }{t.Run(test.name,func(t *testing.T){
        documents:=dsRunnerFixtureServerDocuments(pin)
        if !strings.Contains(documents[test.document],test.from){t.Fatal("fixture change does not apply")}
        documents[test.document]=strings.Replace(documents[test.document],test.from,test.to,1)
        size,err:=dsRunnerServerMatches(context.Background(),dsRunnerFixtureServerClient(documents),pin,time.Now().UTC())
        if size!=0||!errors.Is(err,errDSRunnerEvidence)||errors.Is(err,errDSRunnerCleanup){t.Fatal("a changed server document was accepted or reported a cleanup fact")}
    })}
}
