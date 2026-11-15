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

type ghaV4SourceReadBody struct{failure error;closeFailure bool}
func (b *ghaV4SourceReadBody) Read([]byte)(int,error){return 0,b.failure}
func (b *ghaV4SourceReadBody) Close() error {if b.closeFailure{return errors.New("private-close-detail")};return nil}

func ghaV4ProjectFailure(t *testing.T,err error) ghaFreshInspectionResult {
    t.Helper();audit:=&ghaFreshCredentialAudit{state:"completed"}
    worker:=ghaFreshInspectWith(context.Background(),audit,func(context.Context)(guardTargetArtifact,error){return nil,err})
    raw,e:=json.Marshal(worker);if e!=nil{t.Fatal(e)}
    if strings.Contains(string(raw),"private-"){t.Fatal("raw error detail escaped")}
    return ghaFreshInspectionFromChild(askChildResult{Raw:raw,Exit:1,Started:true,Assigned:true,WaitVerified:true,Reaped:true})
}

func TestGHAFreshOwnedHTTPConsumersPreservePartialFailure(t *testing.T) {
    for _,consumer:=range []string{"run","metadata"} {for _,mode:=range []string{"normal","partial-close","nested-cancel","nested-deadline"} {t.Run(consumer+"/"+mode,func(t *testing.T){
        loc:=ghaFreshTestLocator();run:=strings.ReplaceAll(ghaFreshTestGood,"12345","1201")
        run=strings.TrimSuffix(strings.TrimPrefix(run,"{\"total_count\":1,\"workflow_runs\":["),"]}")
        root,cancel:=context.WithCancel(context.Background());defer cancel();closed:=0
        base:=ghaFreshTestRoundTrip(func(req *http.Request)(*http.Response,error){
            if req.URL.Host!="api.github.com"||req.Header.Get("Authorization")!="Bearer fictional"{t.Fatal("credential destination")}
            switch mode {
            case "partial-close":return &http.Response{StatusCode:200,Header:make(http.Header),Body:&ghaFreshFlowBody{Reader:strings.NewReader("x"),closes:&closed,fail:true}},errors.New("private-transport")
            case "nested-cancel":return nil,errors.Join(context.Canceled,errors.New("private-transport"))
            case "nested-deadline":return nil,errors.Join(context.DeadlineExceeded,errors.New("private-transport"))
            }
            body:="{}";if consumer=="run"{body=run}
            return &http.Response{StatusCode:200,Header:make(http.Header),Body:&ghaFreshFlowBody{Reader:strings.NewReader(body),closes:&closed}},nil
        })
        session:=&ghaGuardCredentialSession{fresh:&loc,secret:[]byte("fictional"),base:base,root:root,cancel:cancel,ready:true,drain:make(chan struct{})}
        client:=&http.Client{Transport:session};ctx:=context.Background();var err error
        if consumer=="run" {err=ghaFreshAuthenticatedRunMatches(ctx,client,loc.hint)} else {var out map[string]any;err=ghaGuardServerJSONDetailed(ctx,client,fmt.Sprintf("https://api.github.com/repos/AutoDevJapan/cc-pc-tools-rs/actions/runs/%d/artifacts?per_page=100",loc.hint.runID),&out)}
        if mode=="normal" {if err!=nil{t.Fatal("normal request failed",err)}} else {
            if err==nil||strings.Contains(err.Error(),"private-"){t.Fatal("error lost or leaked")}
            parent:=ghaV4ProjectFailure(t,err)
            if mode=="partial-close"&&(!parent.CleanupFailed||closed!=1){t.Fatal("partial response cleanup lost")}
            if (mode=="nested-cancel"||mode=="nested-deadline")&&!parent.Cancelled{t.Fatal("nested cancellation lost")}
        }
        cleanup,done:=context.WithTimeout(context.Background(),time.Second);defer done();if session.close(cleanup)!=nil{t.Fatal("session not drained")}
    })}}
}

func TestGHAFreshSourceReadFailureReachesParent(t *testing.T) {
    for _,mode:=range []string{"normal-eof","eof-close","read-failure","read-and-close"} {t.Run(mode,func(t *testing.T){
        root,cancel:=context.WithCancel(context.Background());defer cancel();loc:=ghaFreshTestLocator()
        base:=ghaFreshTestRoundTrip(func(*http.Request)(*http.Response,error){
            var body io.ReadCloser=io.NopCloser(strings.NewReader("{}"))
            if mode=="eof-close" {body=&ghaV4SourceReadBody{failure:io.EOF,closeFailure:true}}
            if mode=="read-failure"||mode=="read-and-close" {body=&ghaV4SourceReadBody{failure:io.ErrUnexpectedEOF,closeFailure:mode=="read-and-close"}}
            return &http.Response{StatusCode:200,Header:make(http.Header),Body:body},nil
        })
        session:=&ghaGuardCredentialSession{fresh:&loc,secret:[]byte("fictional"),base:base,root:root,cancel:cancel,ready:true,drain:make(chan struct{})}
        t.Cleanup(func(){c,done:=context.WithTimeout(context.Background(),time.Second);defer done();if session.close(c)!=nil{t.Error("session cleanup")}})
        tracked:=&ghaFreshSourceStatusTransport{base:session};client:=&http.Client{Transport:tracked}
        resp,err:=client.Get("https://api.github.com/repos/AutoDevJapan/cc-pc-tools-rs/git/trees/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa");if err!=nil{t.Fatal(err)}
        _,_=io.ReadAll(resp.Body);_=resp.Body.Close()
        if mode=="normal-eof" {if tracked.observedFailure(){t.Fatal("EOF treated as read failure")};return}
        failure:=ghaGuardSafeSourceFailure(tracked.sourceFailure(context.Background()),nil)
        parent:=ghaV4ProjectFailure(t,failure)
        if mode=="eof-close" {if errors.Is(failure,errGHAFreshSourceRead)||!parent.CleanupFailed||parent.Reason!="guard_github_response_cleanup_failed"{t.Fatal("EOF close mislabeled as read failure")};return}
        if !errors.Is(failure,errGHAFreshSourceRead)||parent.Qualified||parent.Reason!="guard_github_source_read_failed"||parent.CleanupFailed!=(mode=="read-and-close"){t.Fatal("source read result lost",parent)}
    })}
}

type ghaV4HTTPStatusBody struct {cancel context.CancelFunc;fail bool}
func (*ghaV4HTTPStatusBody) Read([]byte)(int,error){return 0,io.EOF}
func (b *ghaV4HTTPStatusBody) Close() error {if b.cancel!=nil{b.cancel()};if b.fail{return errors.New("private-close-detail")};return nil}

func TestGHAFreshCurrentFailureKeepsActualPhase(t *testing.T) {
    for _,mode:=range []string{"public-runs-429","authenticated-runs-429-close-cancel","authenticated-metadata-403","source-tree-429"} {t.Run(mode,func(t *testing.T){
        ctx,cancel:=context.WithCancel(context.Background());defer cancel()
        loc:=ghaFreshTestLocator();loc.hint.runID=12345;loc.jobID=23456;loc.checkRunID=34567;loc.artifactID=45678
        base:=ghaFreshTestRoundTrip(func(r *http.Request)(*http.Response,error){
            if mode=="source-tree-429" {return &http.Response{StatusCode:429,Header:make(http.Header),Body:io.NopCloser(strings.NewReader("")),Request:r},nil}
            if r.URL.String()==ghaFreshListingURL {
                if mode=="public-runs-429" {return &http.Response{StatusCode:429,Header:make(http.Header),Body:io.NopCloser(strings.NewReader("")),Request:r},nil}
                if mode=="authenticated-runs-429-close-cancel" {return &http.Response{StatusCode:429,Header:make(http.Header),Body:&ghaV4HTTPStatusBody{cancel:cancel,fail:true},Request:r},nil}
                return &http.Response{StatusCode:200,Header:make(http.Header),Body:io.NopCloser(strings.NewReader(ghaFreshTestGood)),Request:r},nil
            }
            if strings.HasSuffix(r.URL.Path,"/actions/workflows/unattended-readiness.yml") {return &http.Response{StatusCode:403,Header:make(http.Header),Body:io.NopCloser(strings.NewReader("")),Request:r},nil}
            t.Fatal("unexpected phase request");return nil,errGHAGuardUnqualified
        })
        var failure error;expected:="";wantClose,wantCancel:=false,false
        if mode=="source-tree-429" {
            tracked:=&ghaFreshSourceStatusTransport{base:base};client:=&http.Client{Transport:tracked}
            response,err:=client.Get("https://api.github.com/repos/AutoDevJapan/cc-pc-tools-rs/git/trees/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa");if err!=nil{t.Fatal(err)};_=response.Body.Close()
            failure=tracked.sourceFailure(ctx);expected="guard_github_source_metadata_http_429"
        } else if mode=="public-runs-429" {
            public:=&http.Client{Transport:base};_,failure=ghaFreshReadRunHintWith(ctx,public,public,time.Now().UTC());expected="guard_fresh_public_runs_http_429"
        } else {
            root,stop:=context.WithCancel(context.Background());defer stop()
            session:=&ghaGuardCredentialSession{fresh:&loc,secret:[]byte("fictional"),base:base,root:root,cancel:stop,ready:true,drain:make(chan struct{})}
            t.Cleanup(func(){c,done:=context.WithTimeout(context.Background(),time.Second);defer done();if session.close(c)!=nil{t.Error("session cleanup")}})
            client:=&http.Client{Transport:session};g:=ghaGuardGeneration{RunID:loc.hint.runID,RunAttempt:1,ProducerRevision:loc.hint.producerRevision}
            failure=ghaFreshCheckHintGeneration(ctx,client,client,g,loc)
            if mode=="authenticated-runs-429-close-cancel" {expected="guard_github_authenticated_runs_http_429";wantClose=true;wantCancel=true} else {expected="guard_github_authenticated_metadata_http_403"}
        }
        if failure==nil {t.Fatal("phase failure accepted")}
        parent:=ghaV4ProjectFailure(t,ghaGuardSafeSourceFailure(ghaFreshSafeCurrentFailure(ctx,failure),ctx.Err()))
        if parent.Reason!=expected||parent.CleanupFailed!=wantClose||parent.Cancelled!=wantCancel {t.Fatal("phase or cleanup/cancel lost",parent)}
    })}
}
