//go:build windows

package main

import (
    "bytes"
    "context"
    "encoding/json"
    "errors"
    "net/http"
    "strings"
    "testing"
    "time"
)

func TestDSRunnerFailureReasonVocabularyIsClosed(t *testing.T){
    for _,reason:=range []string{
        "ds_runner_capture_not_bound","ds_runner_credential_not_found","ds_runner_credential_access_denied","ds_runner_credential_unavailable",
        "ds_runner_cancelled","ds_runner_deadline_exceeded",
        "ds_runner_owner_http_401","ds_runner_owner_http_403","ds_runner_owner_mismatch",
        "ds_runner_repository_malformed","ds_runner_producer_compare_too_large","ds_runner_producer_compare_paginated",
        "ds_runner_job_log_object_http_404","ds_runner_artifact_object_http_410","ds_runner_artifacts_expired","ds_runner_artifacts_absent",
        "ds_runner_archive_mismatch","ds_runner_trust_root_unavailable","ds_runner_signature_mismatch","ds_runner_recheck_mismatch",
        "ds_runner_target_open_failed","ds_runner_target_predecessor_mismatch","ds_runner_target_postcheck_mismatch",
    }{if !dsRunnerFailureReasonAllowed(reason){t.Fatalf("fixed reason rejected: %s",reason)}}
    for _,reason:=range []string{
        "","free text","ds_runner_","ds_runner_owner","ds_runner_owner_http_","ds_runner_owner_http_99","ds_runner_owner_http_600",
        "ds_runner_owner_http_0401","ds_runner_owner_http_+401","ds_runner_owner_http_-401","ds_runner_owner_http_401x",
        "ds_runner_unknown_mismatch","ds_runner_owner_unknown","ds_runner_job_log_object_object_http_404","DS_RUNNER_OWNER_MISMATCH",
        "ds_runner_owner_mismatch ","ds_runner_owner_mismatch\n","https://api.github.com/user","C:/Users/owner/token.txt",
        "ds_runner_target_other","ds_runner_worker_timed_out","guard_github_credential_forbidden",
    }{if dsRunnerFailureReasonAllowed(reason){t.Fatalf("a token outside the vocabulary was accepted: %q",reason)}}
}

func TestDSRunnerFailureReasonsComeOnlyFromTypedErrors(t *testing.T){
    if dsRunnerFailure("free text")!=nil||dsRunnerPhaseFailure("owner","other")!=nil||dsRunnerHTTPFailure("owner",99)!=nil||dsRunnerHTTPFailure("unknown",404)!=nil{t.Fatal("a token outside the vocabulary made an error")}
    if got:=dsRunnerFailureReason(dsRunnerHTTPFailure("owner",403));got!="ds_runner_owner_http_403"{t.Fatalf("typed status reason %q",got)}
    if got:=dsRunnerFailureReason(errors.Join(errDSRunnerEvidence,dsRunnerPhaseFailure("run","mismatch")));got!="ds_runner_run_mismatch"{t.Fatalf("typed stage reason %q",got)}
    if dsRunnerFailureReason(errors.New("ds_runner_run_mismatch"))!=""{t.Fatal("an untyped error whose text matches became a reason")}
    if dsRunnerFailureReason(nil)!=""||dsRunnerFailureReason(errDSRunnerEvidence)!=""{t.Fatal("no stage was named, yet a reason appeared")}
    if dsRunnerFailureReason(&dsRunnerFailureError{reason:"C:/Users/owner/secret.txt"})!=""{t.Fatal("a typed error outside the vocabulary became a reason")}
    if got:=dsRunnerFailureReason(errors.Join(errDSRunnerEvidence,context.DeadlineExceeded,dsRunnerPhaseFailure("run","mismatch")));got!="ds_runner_deadline_exceeded"{t.Fatalf("deadline reason %q",got)}
    if got:=dsRunnerFailureReason(errors.Join(errDSRunnerEvidence,context.Canceled));got!="ds_runner_cancelled"{t.Fatalf("cancelled reason %q",got)}
    ended,stop:=context.WithCancel(context.Background());stop()
    if got:=dsRunnerFailureReasonFor(ended,dsRunnerEvidenceReason("run","mismatch"));got!="ds_runner_cancelled"{t.Fatalf("an ended context must be the reason: %q",got)}
    if got:=dsRunnerFailureReasonFor(context.Background(),dsRunnerEvidenceReason("run","mismatch"));got!="ds_runner_run_mismatch"{t.Fatalf("live context hid the stage: %q",got)}
    if dsRunnerFailureReasonFor(context.Background(),nil)!=""{t.Fatal("success carried a reason")}
}

func TestDSRunnerEvidenceFailureKeepsOnlyFixedFacts(t *testing.T){
    raw:=errors.Join(errors.New("synthetic raw secret"),dsRunnerHTTPFailure("run",404),context.DeadlineExceeded,errGHAGuardResponseCleanup)
    got:=dsRunnerEvidenceFailure(raw)
    if !errors.Is(got,errDSRunnerEvidence)||!errors.Is(got,errDSRunnerCleanup)||!errors.Is(got,context.DeadlineExceeded){t.Fatal("primary evidence, cleanup or deadline fact lost")}
    if dsRunnerFailureReason(got)!="ds_runner_deadline_exceeded"{t.Fatal("the deadline must be reported first")}
    if strings.Contains(got.Error(),"secret"){t.Fatal("raw error text survived the evidence failure")}
    typed:=dsRunnerEvidenceFailure(dsRunnerHTTPEvidence("run",404))
    if dsRunnerFailureReason(typed)!="ds_runner_run_http_404"||errors.Is(typed,errDSRunnerCleanup){t.Fatal("typed status lost or invented a cleanup fact")}
}

func TestDSRunnerEndpointPhaseIsDerivedOnlyFromFixedEndpoints(t *testing.T){
    for _,test:=range []struct{endpoint,phase string}{
        {"https://api.github.com/user","owner"},
        {dsRunnerAPI,"repository"},
        {dsRunnerAPI+"/actions/workflows/task-runner-capture.yml","workflow"},
        {dsRunnerAPI+"/actions/runs/17","run"},
        {dsRunnerAPI+"/actions/runs/17/attempts/1/jobs?per_page=100","jobs"},
        {dsRunnerAPI+"/actions/runs/17/artifacts?per_page=100","artifacts"},
        {dsRunnerAPI+"/actions/jobs/23/logs","job_log"},
        {dsRunnerAPI+"/actions/artifacts/41/zip","artifact"},
        {dsRunnerAPI+"/compare/"+dsRunnerApprovedProducer+"..."+dsRunnerApprovedProducer,"producer_compare"},
        {dsRunnerAPI+"/contents/tools/task-runner-capture/go.mod?ref="+dsRunnerApprovedProducer,"producer_file"},
        {dsRunnerAPI+"/git/commits/"+dsRunnerSource,"source_commit"},
        {dsRunnerAPI+"/git/trees/"+dsRunnerModuleTree,"source_tree"},
        {dsRunnerAPI+"/attestations/sha256:"+digest([]byte("subject"))+"?per_page=100","attestation"},
        {"https://example.invalid/",""},
        {"https://api.github.com/repos/other/repository",""},
        {dsRunnerAPI+"x",""},
        {"",""},
    }{if got:=dsRunnerEndpointPhase(test.endpoint);got!=test.phase{t.Fatalf("%q: phase %q, want %q",test.endpoint,got,test.phase)}}
}

func TestDSRunnerHTTPBytesNamesTheStoppedPhaseWithoutRawText(t *testing.T){
    link:=http.Header{"Link":{`<https://api.github.com/page?page=2>; rel="next"`}}
    for _,test:=range []struct{name,endpoint string;limit int64;status int;header http.Header;body string;transportErr error;want string}{
        {"owner-unauthorized","https://api.github.com/user",1024,401,nil,"secret body",nil,"ds_runner_owner_http_401"},
        {"owner-forbidden","https://api.github.com/user",1024,403,nil,"secret body",nil,"ds_runner_owner_http_403"},
        {"run-missing",dsRunnerAPI+"/actions/runs/17",1024,404,nil,"secret body",nil,"ds_runner_run_http_404"},
        {"jobs-server-error",dsRunnerAPI+"/actions/runs/17/attempts/1/jobs?per_page=100",1024,500,nil,"secret body",nil,"ds_runner_jobs_http_500"},
        {"compare-paginated",dsRunnerAPI+"/compare/"+dsRunnerApprovedProducer+"..."+dsRunnerApprovedProducer,1024,200,link,"{}",nil,"ds_runner_producer_compare_paginated"},
        {"attestation-too-large",dsRunnerAPI+"/attestations/sha256:"+digest([]byte("subject"))+"?per_page=100",8,200,nil,"0123456789",nil,"ds_runner_attestation_too_large"},
        {"repository-unreachable",dsRunnerAPI,1024,0,nil,"",errors.New("synthetic transport secret"),"ds_runner_repository_unavailable"},
    }{t.Run(test.name,func(t *testing.T){
        body:=&dsRunnerFixtureBody{Reader:bytes.NewReader([]byte(test.body))}
        client:=&http.Client{Transport:dsRunnerFixtureTransport(func(*http.Request)(*http.Response,error){
            if test.transportErr!=nil{return nil,test.transportErr}
            header:=test.header;if header==nil{header=make(http.Header)}
            return &http.Response{StatusCode:test.status,Header:header,Body:body},nil
        })}
        raw,err:=dsRunnerHTTPBytes(context.Background(),client,test.endpoint,test.limit)
        if raw!=nil||err==nil||!errors.Is(err,errDSRunnerEvidence){t.Fatal("a failed answer became evidence")}
        if got:=dsRunnerFailureReason(err);got!=test.want{t.Fatalf("reason %q, want %q",got,test.want)}
        if strings.Contains(err.Error(),"secret"){t.Fatal("response or transport text reached the failure")}
        if test.transportErr==nil&&body.closed!=1{t.Fatal("the response body was not closed exactly once")}
    })}
}

func TestDSRunnerServerMatchesNamesEachStoppedPhase(t *testing.T){
    pin:=dsRunnerFixturePin([]byte("payload"))
    for _,test:=range []struct{name,document,from,to,want string}{
        {"private-repository","repository",`"private":false`,`"private":true`,"ds_runner_repository_mismatch"},
        {"missing-repository-field","repository",`,"archived":false`,"","ds_runner_repository_malformed"},
        {"inactive-workflow","workflow",`"state":"active"`,`"state":"disabled_manually"`,"ds_runner_workflow_mismatch"},
        {"failed-run","run",`"conclusion":"success"`,`"conclusion":"failure"`,"ds_runner_run_mismatch"},
        {"other-actor","run",`"actor":{"id":88025750`,`"actor":{"id":1`,"ds_runner_run_mismatch"},
        {"failed-job","jobs",`"conclusion":"success"`,`"conclusion":"failure"`,"ds_runner_jobs_mismatch"},
        {"missing-step","jobs",`,{"name":"Record the server-assigned capture artifact","status":"completed","conclusion":"success","number":5}`,"","ds_runner_jobs_mismatch"},
        {"expired-artifact","artifacts",`"expired":false`,`"expired":true`,"ds_runner_artifacts_expired"},
        {"other-digest","artifacts",`"digest":"sha256:`,`"digest":"sha256:0`,"ds_runner_artifacts_mismatch"},
    }{t.Run(test.name,func(t *testing.T){
        documents:=dsRunnerFixtureServerDocuments(pin)
        if !strings.Contains(documents[test.document],test.from){t.Fatal("fixture change does not apply")}
        documents[test.document]=strings.Replace(documents[test.document],test.from,test.to,1)
        size,err:=dsRunnerServerMatches(context.Background(),dsRunnerFixtureServerClient(documents),pin,time.Now().UTC())
        if size!=0||!errors.Is(err,errDSRunnerEvidence)||errors.Is(err,errDSRunnerCleanup){t.Fatal("a changed server document was accepted or reported a cleanup fact")}
        if got:=dsRunnerFailureReason(err);got!=test.want{t.Fatalf("reason %q, want %q",got,test.want)}
    })}
    t.Run("artifact-dropped-from-the-list",func(t *testing.T){
        documents:=dsRunnerFixtureServerDocuments(pin)
        documents["artifacts"]=`{"total_count":0,"artifacts":[]}`
        size,err:=dsRunnerServerMatches(context.Background(),dsRunnerFixtureServerClient(documents),pin,time.Now().UTC())
        if size!=0||!errors.Is(err,errDSRunnerEvidence)||errors.Is(err,errDSRunnerCleanup){t.Fatal("an empty artifact list was accepted or reported a cleanup fact")}
        if got:=dsRunnerFailureReason(err);got!="ds_runner_artifacts_absent"{t.Fatalf("an artifact dropped from the list was reported as %q",got)}
    })
    t.Run("answer-missing",func(t *testing.T){
        documents:=dsRunnerFixtureServerDocuments(pin)
        documents["path:workflow"]+="-moved"
        _,err:=dsRunnerServerMatches(context.Background(),dsRunnerFixtureServerClient(documents),pin,time.Now().UTC())
        if got:=dsRunnerFailureReason(err);got!="ds_runner_workflow_http_404"{t.Fatalf("a missing answer was reported as %q",got)}
    })
    t.Run("complete",func(t *testing.T){
        size,err:=dsRunnerServerMatches(context.Background(),dsRunnerFixtureServerClient(dsRunnerFixtureServerDocuments(pin)),pin,time.Now().UTC())
        if err!=nil||size!=1234||dsRunnerFailureReason(err)!=""{t.Fatal("complete synthetic documents carried a failure")}
    })
}

func TestDSRunnerProducerSourceNamesTheStoppedPhase(t *testing.T){
    pin:=dsRunnerFixturePin([]byte("payload"))
    compare:=func(status,base string)string{return `{"status":"`+status+`","base_commit":{"sha":"`+base+`"},"merge_base_commit":{"sha":"`+dsRunnerApprovedProducer+`"}}`}
    file:=func(sha string)string{return `{"type":"file","sha":"`+sha+`","encoding":"base64","content":"","size":1}`}
    serve:=func(documents map[string]string)*http.Client{
        return &http.Client{Transport:dsRunnerFixtureTransport(func(r *http.Request)(*http.Response,error){
            document,ok:=documents[r.URL.Path];status:=200;if !ok{status=404}
            return &http.Response{StatusCode:status,Header:make(http.Header),Body:&dsRunnerFixtureBody{Reader:bytes.NewReader([]byte(document))}},nil
        })}
    }
    comparePath:="/repos/AutoDevJapan/cc-pc-tools-rs/compare/"+dsRunnerApprovedProducer+"..."+pin.Producer
    firstFile:="/repos/AutoDevJapan/cc-pc-tools-rs/contents/"+dsRunnerProducerFiles[0].Path
    for _,test:=range []struct{name string;documents map[string]string;want string}{
        {"compare-missing",map[string]string{},"ds_runner_producer_compare_http_404"},
        {"compare-malformed",map[string]string{comparePath:"{}"},"ds_runner_producer_compare_malformed"},
        {"compare-not-descended",map[string]string{comparePath:compare("diverged",dsRunnerApprovedProducer)},"ds_runner_producer_compare_mismatch"},
        {"compare-other-base",map[string]string{comparePath:compare("ahead","1111111111111111111111111111111111111111")},"ds_runner_producer_compare_mismatch"},
        {"first-file-missing",map[string]string{comparePath:compare("ahead",dsRunnerApprovedProducer)},"ds_runner_producer_file_http_404"},
        {"first-file-other-blob",map[string]string{comparePath:compare("ahead",dsRunnerApprovedProducer),firstFile:file("0000000000000000000000000000000000000000")},"ds_runner_producer_file_mismatch"},
    }{t.Run(test.name,func(t *testing.T){
        err:=dsRunnerVerifyProducerSource(context.Background(),serve(test.documents),pin)
        if err==nil||!errors.Is(err,errDSRunnerEvidence)||errors.Is(err,errDSRunnerCleanup){t.Fatal("an unmatched producer source was accepted or reported a cleanup fact")}
        if got:=dsRunnerFailureReason(err);got!=test.want{t.Fatalf("reason %q, want %q",got,test.want)}
    })}
}

func TestDSRunnerCandidateConstructorReportsTheStoppedStage(t *testing.T){
    pin:=dsRunnerFixturePin([]byte("payload"))
    authorityFor:=func(status int)dsRunnerAuthorityOpener{
        return func(_ context.Context,p dsRunnerCapturePin)(*dsRunnerAuthority,error){
            root,cancel:=context.WithCancel(context.Background())
            session:=&dsRunnerCredential{secret:[]byte("synthetic"),pin:p,root:root,cancel:cancel,ready:true,done:make(chan struct{})}
            client:=&http.Client{Transport:dsRunnerFixtureTransport(func(*http.Request)(*http.Response,error){
                return &http.Response{StatusCode:status,Header:make(http.Header),Body:&dsRunnerFixtureBody{Reader:bytes.NewReader([]byte("{}"))}},nil
            })}
            return &dsRunnerAuthority{session:session,client:client,pin:p},nil
        }
    }
    for _,test:=range []struct{name string;pin dsRunnerCapturePin;open dsRunnerAuthorityOpener;want string}{
        {"unbound-pin",dsRunnerCapturePin{},authorityFor(200),"ds_runner_capture_not_bound"},
        {"no-opener",pin,nil,"ds_runner_capture_not_bound"},
        {"opener-failed",pin,func(context.Context,dsRunnerCapturePin)(*dsRunnerAuthority,error){return nil,errors.Join(errDSRunnerEvidence,dsRunnerCredentialFailure(errGHAGuardCredentialNotFound))},"ds_runner_credential_not_found"},
        {"opener-without-authority",pin,func(context.Context,dsRunnerCapturePin)(*dsRunnerAuthority,error){return nil,nil},""},
        {"compare-missing",pin,authorityFor(404),"ds_runner_producer_compare_http_404"},
        {"compare-malformed",pin,authorityFor(200),"ds_runner_producer_compare_malformed"},
    }{t.Run(test.name,func(t *testing.T){
        candidate,err:=openDSRunnerCandidateWith(context.Background(),test.pin,test.open)
        if candidate!=nil||err==nil{t.Fatal("a stopped constructor issued a candidate or no error")}
        if got:=dsRunnerFailureReason(err);got!=test.want{t.Fatalf("reason %q, want %q",got,test.want)}
    })}
    if candidate,err:=openDSRunnerCandidateWith(nil,pin,authorityFor(200));candidate!=nil||err==nil{t.Fatal("nil context accepted")}
    ended,stop:=context.WithCancel(context.Background());stop()
    if candidate,err:=openDSRunnerCandidateWith(ended,pin,authorityFor(200));candidate!=nil||err==nil{t.Fatal("cancelled context accepted")}
}

func TestDSRunnerCredentialFailureNamesTheFixedCause(t *testing.T){
    for _,test:=range []struct{name string;err error;want string}{
        {"not-found",errGHAGuardCredentialNotFound,"ds_runner_credential_not_found"},
        {"access-denied",errors.Join(errGHAGuardUnqualified,errGHAGuardCredentialAccessDenied),"ds_runner_credential_access_denied"},
        {"unavailable",errGHAGuardCredentialUnavailable,"ds_runner_credential_unavailable"},
        {"unknown",errors.New("synthetic"),"ds_runner_credential_unavailable"},
    }{if got:=dsRunnerFailureReason(dsRunnerCredentialFailure(test.err));got!=test.want{t.Fatalf("%s: reason %q, want %q",test.name,got,test.want)}}
}

func TestDSRunnerLiveFailureKeepsGuardStatusAndPhase(t *testing.T){
    if got:=dsRunnerFailureReason(dsRunnerLiveFailure("job_log",ghaGuardHTTPFailure("job_log_object",404)));got!="ds_runner_job_log_object_http_404"{t.Fatalf("object status %q",got)}
    if got:=dsRunnerFailureReason(dsRunnerLiveFailure("artifact",ghaGuardHTTPFailure("artifact",410)));got!="ds_runner_artifact_http_410"{t.Fatalf("download status %q",got)}
    if got:=dsRunnerFailureReason(dsRunnerLiveFailure("artifact",ghaGuardHTTPFailure("source_metadata",500)));got!="ds_runner_artifact_unavailable"{t.Fatalf("a status of another phase was reported as %q",got)}
    cleanup:=dsRunnerLiveFailure("artifact",errors.Join(errGHAGuardUnqualified,errGHAGuardResponseCleanup))
    if !dsRunnerCleanupFact(cleanup)||dsRunnerFailureReason(cleanup)!="ds_runner_artifact_unavailable"{t.Fatal("cleanup fact or fixed phase lost")}
}

func TestDSRunnerMaintenanceCarriesOnlyTypedFailureReasons(t *testing.T){
    ctx:=context.Background()
    for _,test:=range []struct{name string;err error;want string}{
        {"typed",errors.Join(errDSRunnerEvidence,dsRunnerPhaseFailure("run","mismatch")),"ds_runner_run_mismatch"},
        {"typed-status",dsRunnerHTTPEvidence("artifact_object",410),"ds_runner_artifact_object_http_410"},
        {"plain",errors.New("ds_runner_run_mismatch"),""},
        {"evidence-only",errDSRunnerEvidence,""},
        {"forged",errors.Join(errDSRunnerEvidence,&dsRunnerFailureError{reason:"C:/Users/owner/secret.txt"}),""},
    }{t.Run(test.name,func(t *testing.T){
        s:=dsRunnerSyntheticInitial();access:=s.access()
        access.acquire=func(context.Context)(*dsRunnerCandidate,error){return nil,test.err}
        r:=dsRunnerSyntheticRun(ctx,"apply",access)
        if r.Qualified||r.Reason!="qualification_unavailable"||r.FailureReason!=test.want{t.Fatalf("reason %q, want %q",r.FailureReason,test.want)}
        if !dsRunnerWorkerResultValid("apply",r){t.Fatal("the failure record is not a valid worker record")}
    })}
    t.Run("cancelled",func(t *testing.T){
        flight,cancel:=context.WithCancel(context.Background());defer cancel()
        s:=dsRunnerSyntheticInitial();access:=s.access()
        access.acquire=func(context.Context)(*dsRunnerCandidate,error){cancel();return nil,errDSRunnerEvidence}
        r:=dsRunnerSyntheticRun(flight,"apply",access)
        if r.Qualified||r.FailureReason!="ds_runner_cancelled"||!dsRunnerWorkerResultValid("apply",r){t.Fatalf("a cancelled flight was reported as %q",r.FailureReason)}
    })
}

func TestDSRunnerMaintenanceNamesTheStoppedTargetStep(t *testing.T){
    ctx:=context.Background()
    for _,test:=range []struct{name,op,want string;setup func(*dsRunnerSyntheticStore,*dsRunnerMaintenanceAccess)}{
        {"open-failed","verify","ds_runner_target_open_failed",func(_ *dsRunnerSyntheticStore,a *dsRunnerMaintenanceAccess){a.open=func(context.Context,dsRunnerRepairPurpose)(dsRunnerTargetSession,error){return nil,errors.New("synthetic open")}}},
        {"read-failed","verify","ds_runner_target_read_failed",func(s *dsRunnerSyntheticStore,_ *dsRunnerMaintenanceAccess){delete(s.raw,dsRunnerCurrent)}},
        {"snapshot-invalid","verify","ds_runner_target_snapshot_invalid",func(s *dsRunnerSyntheticStore,_ *dsRunnerMaintenanceAccess){s.meta[dsRunnerCurrent]=dsRunnerSnapshot{}}},
        {"predecessor-mismatch","verify","ds_runner_target_predecessor_mismatch",func(s *dsRunnerSyntheticStore,_ *dsRunnerMaintenanceAccess){s.raw[dsRunnerCurrent]=[]byte("another installed");s.meta[dsRunnerCurrent]=dsRunnerSyntheticSnapshot("13",len("another installed"))}},
        {"target-changed","apply","ds_runner_target_changed",func(s *dsRunnerSyntheticStore,_ *dsRunnerMaintenanceAccess){s.mutateCurrent=true}},
        {"stage-failed","prepare","ds_runner_target_stage_failed",func(s *dsRunnerSyntheticStore,_ *dsRunnerMaintenanceAccess){s.failStage=true}},
        {"postcheck-failed","apply","ds_runner_target_postcheck_failed",func(s *dsRunnerSyntheticStore,_ *dsRunnerMaintenanceAccess){s.failAfterCommit=true}},
    }{t.Run(test.name,func(t *testing.T){
        s:=dsRunnerSyntheticInitial();access:=s.access()
        test.setup(s,&access)
        r:=dsRunnerSyntheticRun(ctx,test.op,access)
        if r.Qualified||r.FailureReason!=test.want{t.Fatalf("stopped reason %q, want %q",r.FailureReason,test.want)}
        if !dsRunnerWorkerResultValid(test.op,r){t.Fatal("the failure record is not a valid worker record")}
    })}
    s:=dsRunnerSyntheticInitial();s.raw[dsRunnerStaged]=[]byte("unrelated bytes");s.meta[dsRunnerStaged]=dsRunnerSyntheticSnapshot("77",len("unrelated bytes"))
    r:=dsRunnerSyntheticExecute(ctx,"apply",s)
    if r.Reason!="fixed_leaf_collision_preserved"||r.FailureReason!=""{t.Fatal("a collision gained a second reason")}
    cancelled,cancel:=context.WithCancel(context.Background());defer cancel()
    after:=dsRunnerSyntheticInitial();after.cancelAfterCommit=cancel
    r=dsRunnerSyntheticExecute(cancelled,"apply",after)
    if r.Reason!="commit_outcome_requires_reconciliation"||r.FailureReason!="ds_runner_cancelled"||!dsRunnerWorkerResultValid("apply",r){t.Fatal("a flight cancelled after the commit was not named")}
}

func TestDSRunnerWorkerRecordRejectsMisplacedFailureReasons(t *testing.T){
    valid:=dsRunnerSyntheticObservedBase("verify","qualification_unavailable");valid.FailureReason="ds_runner_target_predecessor_mismatch"
    if !dsRunnerWorkerResultValid("verify",valid){t.Fatal("a failed record with a fixed reason was rejected")}
    forwarded,observed:=dsRunnerObserveWorker("verify",dsRunnerObservedChild(t,valid,1))
    if !observed||forwarded.FailureReason!="ds_runner_target_predecessor_mismatch"||!forwarded.Reaped||forwarded.Qualified{t.Fatal("the worker's fixed reason was not forwarded")}
    capture:=dsRunnerMaintenanceResult{Domain:dsRunnerMaintenanceDomain,Operation:"capture",Reason:"qualification_unavailable",CommitOutcome:dsRunnerCommitNotAttempted,FailureReason:"ds_runner_run_mismatch"}
    if !dsRunnerWorkerResultValid("capture",capture){t.Fatal("a failed capture record with a fixed reason was rejected")}
    for _,test:=range []struct{name,op string;result func()dsRunnerMaintenanceResult}{
        {"free-text","verify",func()dsRunnerMaintenanceResult{r:=valid;r.FailureReason="free text";return r}},
        {"path","verify",func()dsRunnerMaintenanceResult{r:=valid;r.FailureReason="C:/Users/owner/token.txt";return r}},
        {"url","verify",func()dsRunnerMaintenanceResult{r:=valid;r.FailureReason="https://api.github.com/user";return r}},
        {"status-out-of-range","verify",func()dsRunnerMaintenanceResult{r:=valid;r.FailureReason="ds_runner_owner_http_99";return r}},
        {"caller-only-reason","verify",func()dsRunnerMaintenanceResult{r:=valid;r.FailureReason="ds_runner_worker_timed_out";return r}},
        {"qualified-record","verify",func()dsRunnerMaintenanceResult{r:=dsRunnerSyntheticObservedBase("verify","verified_no_write");r.Qualified=true;r.FailureReason="ds_runner_run_mismatch";return r}},
        {"target-reason-on-capture","capture",func()dsRunnerMaintenanceResult{r:=capture;r.FailureReason="ds_runner_target_changed";return r}},
    }{t.Run(test.name,func(t *testing.T){
        r:=test.result()
        if dsRunnerWorkerResultValid(test.op,r){t.Fatal("a misplaced failure reason was accepted")}
        exit:=1;if r.Qualified{exit=0}
        if _,ok:=dsRunnerObserveWorker(test.op,dsRunnerObservedChild(t,r,exit));ok{t.Fatal("a misplaced failure reason was forwarded")}
    })}
}

func TestDSRunnerOuterNamesWorkerLifecycleFailure(t *testing.T){
    complete:=askChildResult{Started:true,Assigned:true,WaitVerified:true,Reaped:true,ResultStdoutComplete:true,ResultStderrComplete:true,Exit:1}
    for _,test:=range []struct{name string;edit func(*askChildResult);want string}{
        {"not-started",func(c *askChildResult){c.Started=false},"ds_runner_worker_not_started"},
        {"timed-out",func(c *askChildResult){c.TimedOut=true},"ds_runner_worker_timed_out"},
        {"not-reaped",func(c *askChildResult){c.Reaped=false},"ds_runner_worker_not_confirmed"},
        {"not-assigned",func(c *askChildResult){c.Assigned=false},"ds_runner_worker_not_confirmed"},
        {"exit-two",func(c *askChildResult){c.Exit=2},"ds_runner_worker_exit_other"},
        {"stdout-incomplete",func(c *askChildResult){c.ResultStdoutComplete=false},"ds_runner_worker_output_incomplete"},
        {"stderr-incomplete",func(c *askChildResult){c.ResultStderrComplete=false},"ds_runner_worker_output_incomplete"},
        {"record-invalid",func(*askChildResult){},"ds_runner_worker_record_invalid"},
    }{
        child:=complete;test.edit(&child)
        if got:=dsRunnerOuterFailureReason(child);got!=test.want{t.Fatalf("%s: reason %q, want %q",test.name,got,test.want)}
        if dsRunnerFailureReasonAllowed(test.want){t.Fatalf("%s: a caller-only reason entered the worker vocabulary",test.name)}
    }
    var out bytes.Buffer
    code:=runDSRunnerMaintenanceWith([]string{"--verify-ds-runner-update"},&out,func(context.Context,string,...string)askChildResult{return askChildResult{}})
    var r dsRunnerMaintenanceResult
    if code!=1||json.Unmarshal(out.Bytes(),&r)!=nil||r.Reason!="worker_outcome_unknown"||r.FailureReason!="ds_runner_worker_not_started"{t.Fatal("the caller did not name why it had no worker record")}
}

func TestDSRunnerOuterForwardsFailureReasonFromRealChild(t *testing.T){
    t.Setenv("DS_RUNNER_WORKER_CHILD","acquire-typed")
    var out bytes.Buffer
    code:=runDSRunnerMaintenanceWith([]string{"--apply-ds-runner-update"},&out,dsRunnerOwnedWorkerLauncher(20*time.Second))
    var r dsRunnerMaintenanceResult
    if code!=1||json.Unmarshal(out.Bytes(),&r)!=nil{t.Fatal("outer exit code or result record is wrong")}
    if !r.Reaped||r.Qualified||r.Reason!="qualification_unavailable"||r.FailureReason!="ds_runner_run_mismatch"||r.CommitOutcome!=dsRunnerCommitNotAttempted{t.Fatal("the worker's failure reason did not survive the owned child and the canonical record")}
}

// A producer that is not ahead of the approved base is refused at the producer
// comparison, before any request about the run, its jobs or its artifacts. The
// compiled pin is used as it is; only the answers of the server are synthetic.
func TestDSRunnerProducerBehindTheApprovedBaseStopsBeforeTheArtifactList(t *testing.T){
    pin:=dsRunnerAdmittedCapture
    comparePath:="/repos/AutoDevJapan/cc-pc-tools-rs/compare/"+dsRunnerApprovedProducer+"..."+pin.Producer
    firstFile:="/repos/AutoDevJapan/cc-pc-tools-rs/contents/"+dsRunnerProducerFiles[0].Path
    compare:=func(status,mergeBase string)string{return `{"status":"`+status+`","base_commit":{"sha":"`+dsRunnerApprovedProducer+`"},"merge_base_commit":{"sha":"`+mergeBase+`"}}`}
    run:=func(document string)(string,[]string){
        var paths []string
        open:=func(_ context.Context,p dsRunnerCapturePin)(*dsRunnerAuthority,error){
            root,cancel:=context.WithCancel(context.Background())
            session:=&dsRunnerCredential{secret:[]byte("synthetic"),pin:p,root:root,cancel:cancel,ready:true,done:make(chan struct{})}
            client:=&http.Client{Transport:dsRunnerFixtureTransport(func(r *http.Request)(*http.Response,error){
                paths=append(paths,r.URL.Path)
                status,body:=404,"{}"
                if r.URL.Path==comparePath{status,body=200,document}
                return &http.Response{StatusCode:status,Header:make(http.Header),Body:&dsRunnerFixtureBody{Reader:bytes.NewReader([]byte(body))}},nil
            })}
            return &dsRunnerAuthority{session:session,client:client,pin:p},nil
        }
        candidate,err:=openDSRunnerCandidateWith(context.Background(),pin,open)
        if candidate!=nil||err==nil||!errors.Is(err,errDSRunnerEvidence)||errors.Is(err,errDSRunnerCleanup){t.Fatal("a stopped constructor issued a candidate, no error or a cleanup fact")}
        return dsRunnerFailureReason(err),paths
    }
    reason,paths:=run(compare("behind",pin.Producer))
    if reason!="ds_runner_producer_compare_mismatch"{t.Fatalf("a producer behind the approved base was reported as %q",reason)}
    if len(paths)!=1||paths[0]!=comparePath{t.Fatalf("the constructor went past the producer comparison: %q",paths)}
    reason,paths=run(compare("ahead",dsRunnerApprovedProducer))
    if reason!="ds_runner_producer_file_http_404"{t.Fatalf("a producer ahead of the approved base was reported as %q",reason)}
    if len(paths)!=2||paths[0]!=comparePath||paths[1]!=firstFile{t.Fatalf("the constructor did not move on to the producer files: %q",paths)}
}

// The session of an authority is closed on every path out of the constructor,
// including an authority that carries no client.
func TestDSRunnerCandidateConstructorClosesTheSessionOfAnAuthorityWithoutClient(t *testing.T){
    owned,cancel:=context.WithCancel(context.Background());defer cancel()
    session:=&dsRunnerCredential{secret:[]byte("synthetic"),pin:dsRunnerAdmittedCapture,root:owned,cancel:cancel,ready:true,done:make(chan struct{})}
    open:=func(_ context.Context,p dsRunnerCapturePin)(*dsRunnerAuthority,error){return &dsRunnerAuthority{session:session,pin:p},nil}
    candidate,err:=openDSRunnerCandidateWith(context.Background(),dsRunnerAdmittedCapture,open)
    if candidate!=nil||err==nil||!errors.Is(err,errDSRunnerEvidence)||errors.Is(err,errDSRunnerCleanup){t.Fatal("an authority without a client issued a candidate, no error or a cleanup fact")}
    if owned.Err()==nil{t.Fatal("the session of an authority without a client was left open")}
}
