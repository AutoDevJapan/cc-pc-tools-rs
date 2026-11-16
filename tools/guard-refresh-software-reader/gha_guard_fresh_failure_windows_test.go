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

// A source body can report an independent read error together with a control
// or cleanup error. Project both fixed facts through the worker to its parent.
func TestGHAFreshSourceReadJoinedFailuresReachParent(t *testing.T) {
    tests:=[]struct{name string;err error;read,cancel,cleanup bool}{
        {"read-and-deadline",errors.Join(io.ErrUnexpectedEOF,context.DeadlineExceeded),true,true,false},
        {"read-and-cancel",fmt.Errorf("private-wrapper: %w",errors.Join(io.ErrUnexpectedEOF,context.Canceled)),true,true,false},
        {"read-and-cleanup",errors.Join(io.ErrUnexpectedEOF,errGHAGuardResponseCleanup),true,false,true},
        {"deadline-only",context.DeadlineExceeded,false,true,false},
        {"cancel-only",context.Canceled,false,true,false},
        {"eof-wrapped",fmt.Errorf("private-wrapper: %w",io.EOF),false,false,false},
        {"eof-and-deadline",errors.Join(io.EOF,context.DeadlineExceeded),false,true,false},
    }
    for _,tc:=range tests {t.Run(tc.name,func(t *testing.T){
        root,cancel:=context.WithCancel(context.Background());defer cancel();loc:=ghaFreshTestLocator()
        base:=ghaFreshTestRoundTrip(func(*http.Request)(*http.Response,error){
            return &http.Response{StatusCode:http.StatusOK,Header:make(http.Header),Body:&ghaV4SourceReadBody{failure:tc.err}},nil
        })
        session:=&ghaGuardCredentialSession{fresh:&loc,secret:[]byte("fictional"),base:base,root:root,cancel:cancel,ready:true,drain:make(chan struct{})}
        t.Cleanup(func(){c,done:=context.WithTimeout(context.Background(),time.Second);defer done();if session.close(c)!=nil{t.Error("session cleanup")}})
        tracked:=&ghaFreshSourceStatusTransport{base:session};client:=&http.Client{Transport:tracked}
        response,err:=client.Get("https://api.github.com/repos/AutoDevJapan/cc-pc-tools-rs/git/trees/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa");if err!=nil{t.Fatal(err)}
        _,_=io.ReadAll(response.Body);_=response.Body.Close()
        if !tc.read&&!tc.cancel&&!tc.cleanup {if tracked.observedFailure(){t.Fatal("EOF marked as failure")};return}
        failure:=ghaGuardSafeSourceFailure(tracked.sourceFailure(context.Background()),nil)
        if errors.Is(failure,errGHAFreshSourceRead)!=tc.read {t.Fatal("source read fact lost")}
        parent:=ghaV4ProjectFailure(t,failure)
        if parent.Qualified||parent.Cancelled!=tc.cancel||parent.CleanupFailed!=tc.cleanup||parent.SourceReadFailed!=tc.read {t.Fatal("parent lost a failure fact",parent)}
        if tc.read&&parent.Reason!="guard_github_source_read_failed" {t.Fatal("read cause lost",parent)}
        if !tc.read&&tc.cancel&&parent.Reason!="guard_target_cancelled" {t.Fatal("control cause mislabeled",parent)}
    })}
}

// A legacy consumer recognizes the unchanged worker shape when the optional
// read fact is absent. The fact never contains an error message or path.
type ghaLegacyInspectionWorker struct {
    guardTargetMetadataResult
    CredentialCleanupState string `json:"credential_cleanup_state"`
}
func TestGHAFreshWorkerParentIndependentReadFact(t *testing.T) {
    cases:=[]struct{name string;failure error;reason string;read,cancel,cleanup bool}{
        {"status-only",ghaGuardHTTPFailure("source_metadata",http.StatusForbidden),"guard_github_source_metadata_http_403",false,false,false},
        {"read-only",errors.Join(errGHAFreshSourceRead,errors.New("private-raw-detail")),"guard_github_source_read_failed",true,false,false},
        {"cancel-only",context.Canceled,"guard_target_cancelled",false,true,false},
        {"mixed",errors.Join(ghaGuardHTTPFailure("source_metadata",http.StatusForbidden),errGHAFreshSourceRead,context.Canceled,errGHAGuardResponseCleanup,errors.New("private-raw-detail")),"guard_github_source_metadata_http_403",true,true,true},
    }
    for _,tc:=range cases {t.Run(tc.name,func(t *testing.T){
        safe:=ghaGuardSafeSourceFailure(ghaFreshSafeCurrentFailure(context.Background(),tc.failure),nil)
        worker:=ghaFreshInspectWith(context.Background(),&ghaFreshCredentialAudit{state:"completed"},func(context.Context)(guardTargetArtifact,error){return nil,safe})
        raw,err:=json.Marshal(worker);if err!=nil{t.Fatal(err)};if strings.Contains(string(raw),"private-raw-detail"){t.Fatal("raw error leaked")}
        var fields map[string]json.RawMessage;if json.Unmarshal(raw,&fields)!=nil{t.Fatal("worker JSON")};_,present:=fields["source_read_failed"]
        if present!=tc.read||worker.SourceReadFailed!=tc.read{t.Fatal("worker read fact missing or fabricated")}
        var old ghaLegacyInspectionWorker;if json.Unmarshal(raw,&old)!=nil{t.Fatal("legacy permissive consumer rejected result")}
        if !tc.read {dec:=json.NewDecoder(bytes.NewReader(raw));dec.DisallowUnknownFields();if dec.Decode(&old)!=nil||dec.Decode(new(any))!=io.EOF{t.Fatal("old strict consumer rejected unchanged shape")}}
        parent:=ghaFreshInspectionFromChild(askChildResult{Started:true,Assigned:true,WaitVerified:true,Reaped:true,Exit:1,Raw:raw})
        if parent.Qualified||!parent.WorkerResultObserved||parent.Reason!=tc.reason||parent.SourceReadFailed!=tc.read||parent.Cancelled!=tc.cancel||parent.CleanupFailed!=tc.cleanup{t.Fatal("parent lost fixed facts",parent)}
        encoded,err:=json.Marshal(parent);if err!=nil||strings.Contains(string(encoded),"private-raw-detail"){t.Fatal("parent leaked raw error")}
        var projected map[string]json.RawMessage;if json.Unmarshal(encoded,&projected)!=nil{t.Fatal("parent JSON")};_,present=projected["source_read_failed"];if present!=tc.read{t.Fatal("parent optional fact mismatch")}
    })}
}

func TestGHAFreshLegacyReadReasonAndSuccessContradiction(t *testing.T) {
    legacy:=ghaLegacyInspectionWorker{guardTargetMetadataResult:guardTargetMetadataResult{Domain:guardTargetUpdateDomain,Reason:"guard_github_source_read_failed"},CredentialCleanupState:"completed"}
    raw,err:=json.Marshal(legacy);if err!=nil{t.Fatal(err)}
    parent:=ghaFreshInspectionFromChild(askChildResult{Started:true,Assigned:true,WaitVerified:true,Reaped:true,Exit:1,Raw:raw})
    if !parent.WorkerResultObserved||!parent.SourceReadFailed||parent.Qualified{t.Fatal("legacy fixed read reason lost")}
    good:=ghaFreshWorkerResult{guardTargetMetadataResult:guardTargetMetadataResult{Qualified:true,Reason:"verified_guard_target_metadata",Domain:guardTargetUpdateDomain,MetadataObserved:true,CurrentIdentity:&softwareIdentity{Volume:"1",FileID:"2",Created:"3"},ACLHash:strings.Repeat("a",64),ParentProof:strings.Repeat("b",64),Bytes:1},CredentialCleanupState:"completed"}
    raw,err=json.Marshal(good);if err!=nil{t.Fatal(err)}
    child:=askChildResult{Started:true,Assigned:true,WaitVerified:true,Reaped:true,Exit:0,Raw:raw}
    if _,ok:=ghaFreshDecodeInspection(child);!ok{t.Fatal("valid legacy-shaped success rejected")}
    if strings.Contains(string(raw),"source_read_failed"){t.Fatal("false fact changed legacy success shape")}
    good.SourceReadFailed=true;raw,err=json.Marshal(good);if err!=nil{t.Fatal(err)};child.Raw=raw
    if _,ok:=ghaFreshDecodeInspection(child);ok{t.Fatal("read failure accepted as success")}
    if got:=ghaFreshInspectionFromChild(child);got.Qualified{t.Fatal("contradictory worker granted success")}
}
