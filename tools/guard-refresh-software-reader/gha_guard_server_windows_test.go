//go:build windows

package main

import (
    "context"
    "bytes"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "os"
    "strings"
    "sync"
    "testing"
    "time"

    "github.com/sigstore/sigstore-go/pkg/root"
    "github.com/sigstore/sigstore-go/pkg/tuf"
)

type ghaServerTransport func(*http.Request)(*http.Response,error)
func (f ghaServerTransport) RoundTrip(r *http.Request)(*http.Response,error){return f(r)}

func TestGHAServerCollectsExactRunJobArtifact(t *testing.T) {
    now:=time.Unix(1800000000,0).UTC();head:=strings.Repeat("b",40)
    for _,fault:=range []string{"none","wrong-head","wrong-job","wrong-check","wrong-artifact","bad-binding","duplicate-job","duplicate-artifact","reversed-steps","duplicate-step","failure","expired","pagination","redirect","body-too-large"} {
        t.Run(fault,func(t *testing.T){
            expect:=ghaGuardMetadataExpectation{RepositoryID:1406818713,HeadRepositoryID:1406818713,Source:ghaGuardSource,SourceTree:ghaGuardSourceTree,ProducerRevision:head,WorkflowPath:".github/workflows/unattended-readiness.yml",Event:"workflow_dispatch",Ref:"refs/heads/main",WorkflowID:7,RunID:2,RunAttempt:1,JobID:3,CheckRunID:9,JobName:"Guard bootstrap artifact",ArtifactID:4,ArtifactName:"guard-bootstrap-2-1-3"}
            client:=&http.Client{Transport:ghaServerTransport(func(req *http.Request)(*http.Response,error){
                var body any;status:=200;header:=make(http.Header)
                switch {
                case strings.HasSuffix(req.URL.Path,"/runs/2"):
                    sha:=head;if fault=="wrong-head" {sha=ghaGuardSource};body=map[string]any{"id":2,"run_attempt":1,"head_sha":sha,"head_branch":"main","event":"workflow_dispatch","path":expect.WorkflowPath,"workflow_id":7,"status":"completed","conclusion":"success","repository":map[string]any{"id":1406818713},"head_repository":map[string]any{"id":1406818713}}
                case strings.HasSuffix(req.URL.Path,"/attempts/1/jobs"):
                    id:=3;if fault=="wrong-job" {id=5};check:=9;if fault=="wrong-check" {check=10};conclusion:="success";if fault=="failure" {conclusion="failure"};job:=map[string]any{"id":id,"run_id":2,"name":expect.JobName,"status":"completed","conclusion":conclusion,"check_run_url":"https://api.github.com/repos/AutoDevJapan/cc-pc-tools-rs/check-runs/"+fmt.Sprint(check),"steps":[]any{map[string]any{"name":"Capture the fixed guard build","number":1,"status":"completed","conclusion":"success"},map[string]any{"name":"Check the exact captured files","number":2,"status":"completed","conclusion":"success"},map[string]any{"name":"Attest the guard and source evidence","number":3,"status":"completed","conclusion":"success"},map[string]any{"name":"Upload the fixed guard capture","number":4,"status":"completed","conclusion":"success"},map[string]any{"name":"Record the server-assigned capture artifact","number":5,"status":"completed","conclusion":"success"}}};if fault=="reversed-steps" {steps:=job["steps"].([]any);steps[0].(map[string]any)["name"],steps[1].(map[string]any)["name"]=steps[1].(map[string]any)["name"],steps[0].(map[string]any)["name"]};if fault=="duplicate-step" {steps:=job["steps"].([]any);steps=append(steps,map[string]any{"name":"Upload the fixed guard capture","number":6,"status":"completed","conclusion":"success"});job["steps"]=steps};jobs:=[]any{job};if fault=="duplicate-job" {jobs=append(jobs,job)};body=map[string]any{"total_count":len(jobs),"jobs":jobs}
                case strings.HasSuffix(req.URL.Path,"/runs/2/artifacts"):
                    id:=4;if fault=="wrong-artifact" {id=6};expires:=now.Add(time.Hour);if fault=="expired" {expires=now};artifact:=map[string]any{"id":id,"name":expect.ArtifactName,"size_in_bytes":100,"digest":"sha256:"+strings.Repeat("a",64),"expired":false,"expires_at":expires,"workflow_run":map[string]any{"id":2,"repository_id":1406818713,"head_repository_id":1406818713,"head_sha":head}};artifacts:=[]any{artifact};if fault=="duplicate-artifact" {artifacts=append(artifacts,artifact)};body=map[string]any{"total_count":len(artifacts),"artifacts":artifacts}
                default:t.Fatal("unexpected endpoint",req.URL);return nil,nil
                }
                raw,_:=json.Marshal(body);if fault=="body-too-large" {raw=make([]byte,2<<20+1)};if fault=="pagination" {header.Set("Link","<next>; rel=next")};if fault=="redirect" {status=302}
                return &http.Response{StatusCode:status,Header:header,Body:io.NopCloser(strings.NewReader(string(raw)))},nil
            })}
            binding:=ghaGuardCaptureBinding{RepositoryID:expect.RepositoryID,Source:expect.Source,ProducerRevision:expect.ProducerRevision,RunID:expect.RunID,RunAttempt:expect.RunAttempt,JobID:expect.JobID,CheckRunID:expect.CheckRunID,ArtifactID:expect.ArtifactID};if fault=="bad-binding" {binding.ArtifactID++}
            got,err:=ghaGuardFetchServerEvidence(context.Background(),client,expect,binding,now)
            if fault=="none" {if err!=nil||got==nil {t.Fatal("valid metadata rejected",err)}} else if err==nil||got!=nil {t.Fatal("invalid metadata accepted",fault)}
        })
    }
}

func TestGHAServerCannotMintAuthorityWithoutVerifier(t *testing.T) {
    candidate,err:=ghaGuardOpenVerifiedCandidate(context.Background())
    if err==nil||candidate!=nil {t.Fatal("metadata minted update authority")}
    empty:=&ghaGuardVerifiedCandidate{};if empty.VerifySourceGeneration()||empty.WithPinnedPayload(func([]byte)error{return nil})==nil {t.Fatal("empty candidate gained authority")}
    _=empty.Close();if empty.VerifySourceGeneration()||empty.WithPinnedPayload(func([]byte)error{return nil})==nil {t.Fatal("closed candidate gained authority")}
}

func TestGHAPublicationRecordBindsArtifactToJob(t *testing.T) {
    record:=ghaGuardPublicationRecord{Domain:"guard-gha-publication-v1",RepositoryID:1406818713,Source:ghaGuardSource,SourceTree:ghaGuardSourceTree,ProducerRevision:strings.Repeat("b",40),RunID:2,RunAttempt:1,JobID:3,CheckRunID:9,ArtifactID:4,ArtifactName:"guard-bootstrap-2-1-3",ArchiveSHA:strings.Repeat("a",64),OutputSHA:strings.Repeat("b",64),CaptureSHA:strings.Repeat("c",64),SourceManifestSHA:strings.Repeat("d",64),AttestationID:"attest-1"}
    raw,err:=json.Marshal(record);if err!=nil {t.Fatal(err)}
    got,err:=ghaGuardParsePublication(raw);if err!=nil||got!=record {t.Fatal("valid publication rejected",err)}
    line:=append([]byte("2026-11-14T08:00:01.0000000Z guard_gha_publication_v1="),raw...);line=append(line,'\n')
    if parsed,err:=ghaGuardPublicationFromLog(line);err!=nil||parsed!=record {t.Fatal("job log record rejected",err)}
    if _,err:=ghaGuardPublicationFromLog(append(append([]byte(nil),line...),line...));err==nil {t.Fatal("duplicate log record accepted")}
    echoed:=[]byte("2026-11-14T08:00:00.0000000Z \x1b[36;1mWrite-Output ('guard_gha_publication_v1=' + $json)\x1b[0m\n2026-11-14T08:00:00.0000001Z \x1b[36;1m[IO.File]::AppendAllText($env:GITHUB_STEP_SUMMARY, 'guard_gha_publication_v1=' + $json + [Environment]::NewLine, [Text.UTF8Encoding]::new($false))\x1b[0m\n")
    actual:=append([]byte("2026-11-14T08:00:01.0000000Z guard_gha_publication_v1="),raw...)
    actual=append(actual,'\n')
    if parsed,err:=ghaGuardPublicationFromLog(append(echoed,actual...));err!=nil||parsed!=record {t.Fatal("echo plus one record rejected",err)}
    if _,err:=ghaGuardPublicationFromLog(append(append([]byte(nil),echoed...),echoed...));err==nil {t.Fatal("echo without record accepted")}
    duplicateBodies:=append(append(append([]byte(nil),echoed...),actual...),actual...)
    if _,err:=ghaGuardPublicationFromLog(duplicateBodies);err==nil {t.Fatal("two record bodies accepted")}
    malformed:=append([]byte("2026-11-14T08:00:02.0000000Z guard_gha_publication_v1="),[]byte("{broken}\n")...)
    if _,err:=ghaGuardPublicationFromLog(append(append([]byte(nil),actual...),malformed...));err==nil {t.Fatal("malformed record body ignored")}
    inside:=append([]byte("2026-11-14T08:00:03.0000000Z incidental guard_gha_publication_v1="),raw...)
    inside=append(inside,'\n')
    if _,err:=ghaGuardPublicationFromLog(inside);err==nil {t.Fatal("embedded marker treated as record")}
    inCombined:=append(append([]byte(nil),inside...),actual...)
    if parsed,err:=ghaGuardPublicationFromLog(inCombined);err!=nil||parsed!=record {t.Fatal("embedded non-record blocked real record",err)}
    untimed:=append([]byte("guard_gha_publication_v1="),raw...)
    if _,err:=ghaGuardPublicationFromLog(untimed);err==nil {t.Fatal("untimed record accepted")}


    e:=ghaGuardMetadataExpectation{RepositoryID:record.RepositoryID,Source:record.Source,SourceTree:record.SourceTree,ProducerRevision:record.ProducerRevision,RunID:record.RunID,RunAttempt:record.RunAttempt,JobID:record.JobID,CheckRunID:record.CheckRunID,ArtifactID:record.ArtifactID,ArtifactName:record.ArtifactName}
    a:=ghaGuardArtifact{ID:record.ArtifactID,Digest:"sha256:"+record.ArchiveSHA}
    if !ghaGuardPublicationMatches(e,a,record){t.Fatal("same artifact did not bind")}
    a.ID++;if ghaGuardPublicationMatches(e,a,record){t.Fatal("other artifact accepted")}
    duplicate:=append(raw[:len(raw)-1],[]byte(",\"artifact_id\":4}")...);if _,err=ghaGuardParsePublication(duplicate);err==nil {t.Fatal("duplicate key accepted")}
    changed:=record;changed.ArtifactName="guard-bootstrap-2-1-4";raw,_=json.Marshal(changed);if _,err=ghaGuardParsePublication(raw);err==nil {t.Fatal("different job name accepted")}
    changed=record;changed.Source=changed.ProducerRevision;raw,_=json.Marshal(changed);if _,err=ghaGuardParsePublication(raw);err==nil {t.Fatal("producer source substituted")}
}

func TestGHACandidateBorrowCloseInvalidatesWithoutDeadlock(t *testing.T) {
    raw:=[]byte("owned-synthetic-payload");candidate:=&ghaGuardVerifiedCandidate{issuer:&ghaGuardIssuer{token:1},payload:raw,generation:ghaGuardGeneration{Source:ghaGuardSource,SourceTree:ghaGuardSourceTree,ProducerRevision:strings.Repeat("a",40),RunID:1,RunAttempt:1,JobID:2,CheckRunID:3,ArtifactID:4,OutputSHA:digest(raw),ProofSHA:strings.Repeat("b",64)}};candidate.self=candidate
    started,release,done:=make(chan struct{}),make(chan struct{}),make(chan error,1)
    var releaseOnce sync.Once
    releaseBorrow:=func(){releaseOnce.Do(func(){close(release)})}
    joined:=false
    t.Cleanup(func(){releaseBorrow();if !joined {select{case <-done:joined=true;case <-time.After(3*time.Second):t.Error("owned borrow did not finish")}}})
    go func(){done<-candidate.WithPinnedPayload(func(got []byte)error {
        if !bytes.Equal(got,raw){t.Error("wrong held bytes")};if _,ok:=candidate.Generation();!ok {t.Error("live generation unavailable")}
        close(started);<-release;if !bytes.Equal(got,[]byte("owned-synthetic-payload")){t.Error("payload erased before borrow returned")};return nil
    })}()
    select {case <-started:case <-time.After(3*time.Second):t.Fatal("borrow did not start")}
    if candidate.WithPinnedPayload(func([]byte)error{return nil})==nil {t.Fatal("concurrent borrow accepted")}
    if err:=candidate.Close();err!=nil {t.Fatal(err)}
    if candidate.VerifySourceGeneration(){t.Fatal("Close did not invalidate immediately")}
    if _,ok:=candidate.Generation();ok {t.Fatal("closed generation available")}
    if candidate.WithPinnedPayload(func([]byte)error{return nil})==nil {t.Fatal("new borrow after Close")}
    short,cancel:=context.WithTimeout(context.Background(),time.Millisecond);defer cancel();if candidate.WaitClosed(short)==nil {t.Fatal("live borrow reported drained")}
    releaseBorrow()
    select {case err:=<-done:joined=true;if err==nil{t.Fatal("closed borrow reported success")};case <-time.After(3*time.Second):t.Fatal("borrow did not finish")}
    closed,stop:=context.WithTimeout(context.Background(),3*time.Second);defer stop()
    if err:=candidate.WaitClosed(closed);err!=nil {t.Fatal("released borrow not drained",err)}
    for _,b:=range raw {if b!=0 {t.Fatal("payload not cleared after last borrower")}}
}

func TestGHACandidateMutationInvalidatesGeneration(t *testing.T) {
    raw:=[]byte("owned-synthetic-payload");candidate:=&ghaGuardVerifiedCandidate{issuer:&ghaGuardIssuer{token:1},payload:raw,generation:ghaGuardGeneration{Source:ghaGuardSource,SourceTree:ghaGuardSourceTree,ProducerRevision:strings.Repeat("a",40),RunID:1,RunAttempt:1,JobID:2,CheckRunID:3,ArtifactID:4,OutputSHA:digest(raw),ProofSHA:strings.Repeat("b",64)}};candidate.self=candidate
    if err:=candidate.WithPinnedPayload(func(value []byte)error {value[0]^=1;return nil});err==nil {t.Fatal("mutated payload accepted")}
    if candidate.VerifySourceGeneration(){t.Fatal("mutated candidate remained valid")}
    if err:=candidate.WaitClosed(context.Background());err!=nil {t.Fatal(err)}
}

func TestGHACandidateRejectsIncompleteGeneration(t *testing.T) {
    raw:=[]byte("owned");candidate:=&ghaGuardVerifiedCandidate{issuer:&ghaGuardIssuer{token:1},payload:raw,generation:ghaGuardGeneration{Source:ghaGuardSource,OutputSHA:digest(raw)}};candidate.self=candidate
    if candidate.VerifySourceGeneration(){t.Fatal("incomplete generation accepted")}
    called:=false
    if candidate.WithPinnedPayload(func([]byte)error{called=true;return nil})==nil||called {t.Fatal("incomplete generation reached callback")}
}

func TestGHACandidateCallbackPanicInvalidates(t *testing.T) {
    raw:=[]byte("owned");candidate:=&ghaGuardVerifiedCandidate{issuer:&ghaGuardIssuer{token:1},payload:raw,generation:ghaGuardGeneration{Source:ghaGuardSource,SourceTree:ghaGuardSourceTree,ProducerRevision:strings.Repeat("a",40),RunID:1,RunAttempt:1,JobID:2,CheckRunID:3,ArtifactID:4,OutputSHA:digest(raw),ProofSHA:strings.Repeat("b",64)}};candidate.self=candidate
    func(){defer func(){_ = recover()}();_ = candidate.WithPinnedPayload(func([]byte)error{panic("owned fault")})}()
    if candidate.VerifySourceGeneration(){t.Fatal("panic left candidate valid")}
    if err:=candidate.WaitClosed(context.Background());err!=nil {t.Fatal(err)}
}




func TestGHAGuardConstructorSyntheticStages(t *testing.T) {
    manifest:=ghaGuardTestManifest()
    archive:=ghaGuardTestArchive(t,manifest,ghaGuardTestPE(),"")
    now:=time.Now().UTC()
    e:=ghaGuardMetadataExpectation{RepositoryID:1406818713,HeadRepositoryID:1406818713,Source:manifest.Source,SourceTree:manifest.SourceTree,ProducerRevision:manifest.ProducerRevision,WorkflowPath:".github/workflows/unattended-readiness.yml",Event:"workflow_dispatch",Ref:"refs/heads/main",WorkflowID:7,RunID:manifest.RunID,RunAttempt:manifest.RunAttempt,JobID:manifest.JobID,CheckRunID:manifest.CheckRunID,JobName:"Guard bootstrap artifact",ArtifactID:4,ArtifactName:"guard-bootstrap-2-1-3"}
    capture,_:=json.Marshal(manifest)
    pub:=ghaGuardPublicationRecord{Domain:"guard-gha-publication-v1",RepositoryID:e.RepositoryID,Source:e.Source,SourceTree:e.SourceTree,ProducerRevision:e.ProducerRevision,RunID:e.RunID,RunAttempt:e.RunAttempt,JobID:e.JobID,CheckRunID:e.CheckRunID,ArtifactID:e.ArtifactID,ArtifactName:e.ArtifactName,ArchiveSHA:digest(archive),OutputSHA:manifest.OutputSHA,CaptureSHA:digest(capture),SourceManifestSHA:manifest.SourceManifestSHA,AttestationID:"fixture"}
    mk:=func()*ghaGuardAcquiredEvidence{
        record,_:=json.Marshal(pub)
        return &ghaGuardAcquiredEvidence{
            run:ghaGuardRun{RepositoryID:e.RepositoryID,HeadRepositoryID:e.HeadRepositoryID,HeadSHA:e.ProducerRevision,WorkflowPath:e.WorkflowPath,Event:e.Event,HeadBranch:"main",WorkflowID:e.WorkflowID,RunID:e.RunID,RunAttempt:e.RunAttempt,Status:"completed",Conclusion:"success"},
            job:ghaGuardJob{RunID:e.RunID,JobID:e.JobID,CheckRunID:e.CheckRunID,RunAttempt:e.RunAttempt,Name:e.JobName,Status:"completed",Conclusion:"success",UploadStepCompleted:true},
            artifact:ghaGuardArtifact{RepositoryID:e.RepositoryID,HeadRepositoryID:e.HeadRepositoryID,HeadSHA:e.ProducerRevision,RunID:e.RunID,ID:e.ArtifactID,Name:e.ArtifactName,Size:int64(len(archive)),Digest:"sha256:"+pub.ArchiveSHA,ExpiresAt:now.Add(time.Hour)},
            workflowID:e.WorkflowID,workflowPath:e.WorkflowPath,workflowActive:true,jobLog:append([]byte("2026-11-14T08:09:31.3203023Z guard_gha_publication_v1="),record...),manifest:manifest,archive:append([]byte(nil),archive...),bundle:[]byte("synthetic-signature"),
        }
    }
    for _,fault:=range []string{"none","wrong-job","wrong-artifact","bad-publication","archive-corrupt","signature-error","payload-change","cancelled"} {
        t.Run(fault,func(t *testing.T) {
            m:=mk();local:=context.Background();calls:=0
            switch fault {
            case "wrong-job":m.job.JobID++
            case "wrong-artifact":m.artifact.ID++
            case "bad-publication":m.jobLog=append(m.jobLog,append([]byte("\n"),m.jobLog...)...)
            case "archive-corrupt":m.archive[len(m.archive)-1]^=1
            case "cancelled":var cancel context.CancelFunc;local,cancel=context.WithCancel(local);cancel()
            }
            verifier:=func(ctx context.Context,bundle,payload []byte,got ghaGuardManifest)error{
                calls++
                if !bytes.Equal(bundle,[]byte("synthetic-signature"))||!bytes.Equal(payload,ghaGuardTestPE())||got!=manifest{return errGHAGuardUnqualified}
                if fault=="signature-error"{return errGHAGuardUnqualified}
                if fault=="payload-change"{payload[0]^=1}
                return nil
            }
            candidate,err:=ghaGuardConstructVerifiedCandidateWith(local,e,m,verifier)
            if fault=="none" {
                if err!=nil||candidate==nil||calls!=1||!candidate.VerifySourceGeneration(){t.Fatal("valid synthetic stages rejected",err)}
                if err=candidate.Close();err!=nil{t.Fatal(err)}
            }else if err==nil||candidate!=nil {t.Fatal("bad stage issued candidate",fault)}
        })
    }
    if candidate,err:=ghaGuardOpenVerifiedCandidate(context.Background());err==nil||candidate!=nil{t.Fatal("normal transport absence issued a candidate")}
}

// This exercises the same constructor as production with the retained public
// artifact and real cryptography; GitHub metadata is synthetic. The binary
// fixture is not committed, and absence never counts as a positive run.
func TestGHAGuardActualArtifactConstructor(t *testing.T) {
    if os.Getenv("GUARD_GHA_ACTUAL_FIXTURE")!="1" {t.Skip("actual retained public artifact not supplied")}
    archive,err:=os.ReadFile("testdata/actual-guard-artifact.zip");if err!=nil{t.Fatal(err)}
    signed,err:=os.ReadFile("testdata/actual-guard-attestation.sigstore.json");if err!=nil{t.Fatal(err)}
    now:=time.Now().UTC()
    expiry:=time.Date(2026,time.November,15,8,9,29,0,time.UTC)
    if !now.Before(expiry){t.Skip("actual GitHub artifact expired; historical evidence is not a fresh acquisition")}
    ctx,cancel:=context.WithTimeout(context.Background(),2*time.Minute);defer cancel()
    trusted,err:=root.FetchTrustedRootWithOptions(tuf.DefaultOptions().WithDisableLocalCache().WithContext(ctx).WithFetcher(ghaGuardNewTUFRequestFetcher(ctx,&http.Client{})));if err!=nil{t.Fatal(err)}
    verifyReal:=func(ctx context.Context,bundle,payload []byte,m ghaGuardManifest)error{return ghaGuardVerifySignatureWithTrusted(ctx,trusted,bundle,payload,m)}
    e:=ghaGuardActualExpectation();e.WorkflowID=7
    manifest:=ghaGuardManifest{Domain:"guard-gha-capture-v1",RepositoryID:1406818713,Source:ghaGuardSource,SourceTree:ghaGuardSourceTree,ProducerRevision:e.ProducerRevision,RunID:e.RunID,RunAttempt:e.RunAttempt,JobID:e.JobID,CheckRunID:e.CheckRunID,SourceManifestSHA:"aeeefe291c6a10a0f8d1bf1ee1a0952daf29dba87046596980c7c509c690f8c8",RunnerToolchainSHA:"0b2c36009670818ff928fee28a95e61590f930041d2fcec9624ec5798964a8f8",CompileCommandSHA:"616dbd64b1f6a3fe614b367a4d4f998dc14ac388a5cced808cc123a1d12b6892",RunnerImageSHA:"3e1e398a479c225605dcfeb8ca3f0b45b86f7b02a1f3eec3c1ce8eb4efff4aa9",OutputName:"guard.exe",OutputBytes:14299136,OutputSHA:"a61e25065ae35d51f1ac8133959176b21e2c3740c1516b76f799371f1c27f9a2"}
    capture,_:=json.Marshal(manifest)
    publication:=ghaGuardPublicationRecord{Domain:"guard-gha-publication-v1",RepositoryID:e.RepositoryID,Source:e.Source,SourceTree:e.SourceTree,ProducerRevision:e.ProducerRevision,RunID:e.RunID,RunAttempt:e.RunAttempt,JobID:e.JobID,CheckRunID:e.CheckRunID,ArtifactID:e.ArtifactID,ArtifactName:e.ArtifactName,ArchiveSHA:"681ae0e1356755a752addf0776ffdab48432fcb850f243d3989e7466e3076452",OutputSHA:manifest.OutputSHA,CaptureSHA:digest(capture),SourceManifestSHA:manifest.SourceManifestSHA,AttestationID:"67491128"}
    mk:=func()*ghaGuardAcquiredEvidence{
        record,_:=json.Marshal(publication)
        return &ghaGuardAcquiredEvidence{
            run:ghaGuardRun{RepositoryID:e.RepositoryID,HeadRepositoryID:e.HeadRepositoryID,HeadSHA:e.ProducerRevision,WorkflowPath:e.WorkflowPath,Event:e.Event,HeadBranch:"main",WorkflowID:e.WorkflowID,RunID:e.RunID,RunAttempt:e.RunAttempt,Status:"completed",Conclusion:"success"},
            job:ghaGuardJob{RunID:e.RunID,JobID:e.JobID,CheckRunID:e.CheckRunID,RunAttempt:e.RunAttempt,Name:e.JobName,Status:"completed",Conclusion:"success",UploadStepCompleted:true},
            artifact:ghaGuardArtifact{RepositoryID:e.RepositoryID,HeadRepositoryID:e.HeadRepositoryID,HeadSHA:e.ProducerRevision,RunID:e.RunID,ID:e.ArtifactID,Name:e.ArtifactName,Size:int64(len(archive)),Digest:"sha256:"+publication.ArchiveSHA,ExpiresAt:expiry},
            workflowID:e.WorkflowID,workflowPath:e.WorkflowPath,workflowActive:true,jobLog:append([]byte("2026-11-14T08:09:31.3203023Z guard_gha_publication_v1="),record...),
            manifest:manifest,archive:append([]byte(nil),archive...),bundle:append([]byte(nil),signed...),
        }
    }
    for _,fault:=range []string{"none","wrong-job","wrong-artifact","wrong-publication","expired","archive-mutated","bundle-mutated","payload-mutated-during-verification","cancelled"} {
        t.Run(fault,func(t *testing.T) {
            material:=mk();localCtx:=ctx
            switch fault {
            case "wrong-job":material.job.JobID++
            case "wrong-artifact":material.artifact.ID++
            case "wrong-publication":material.jobLog=append(material.jobLog,append([]byte("\n"),material.jobLog...)...)
            case "expired":material.artifact.ExpiresAt=time.Now().Add(-time.Second)
            case "archive-mutated":material.archive[len(material.archive)-1]^=1
            case "bundle-mutated":material.bundle[0]^=1
            case "cancelled":var stop context.CancelFunc;localCtx,stop=context.WithCancel(ctx);stop()
            }
            verifyFn:=verifyReal
            if fault=="payload-mutated-during-verification" {verifyFn=func(ctx context.Context,bundle,payload []byte,m ghaGuardManifest)error{err:=verifyReal(ctx,bundle,payload,m);payload[0]^=1;return err}}
            candidate,err:=ghaGuardConstructVerifiedCandidateWith(localCtx,e,material,verifyFn)
            if fault=="none" {
                if err!=nil||candidate==nil||!candidate.VerifySourceGeneration(){t.Fatal("valid material rejected",err)}
                if err=candidate.WithPinnedPayload(func(b []byte)error{if digest(b)!=manifest.OutputSHA{t.Fatal("wrong payload")};return nil});err!=nil{t.Fatal(err)}
                if err=candidate.Close();err!=nil{t.Fatal(err)}
                if err=candidate.WaitClosed(context.Background());err!=nil{t.Fatal(err)}
            }else if err==nil||candidate!=nil {t.Fatal("invalid material issued candidate",fault)}
        })
    }
}


func TestGHACandidateClaimMovesExactlyOnce(t *testing.T) {
    raw:=[]byte("owned-synthetic-payload")
    candidate:=&ghaGuardVerifiedCandidate{issuer:&ghaGuardIssuer{token:1},payload:raw,generation:ghaGuardGeneration{Source:ghaGuardSource,SourceTree:ghaGuardSourceTree,ProducerRevision:strings.Repeat("a",40),RunID:1,RunAttempt:1,JobID:2,CheckRunID:3,ArtifactID:4,OutputSHA:digest(raw),ProofSHA:strings.Repeat("b",64)}};candidate.self=candidate
    owner,err:=candidate.Claim();if err!=nil||owner==nil {t.Fatal("initial claim failed",err)}
    if again,err:=candidate.Claim();err==nil||again!=nil {t.Fatal("second claim accepted")}
    if candidate.VerifySourceGeneration(){t.Fatal("old alias still valid")}
    if _,ok:=candidate.Generation();ok {t.Fatal("old alias retained generation")}
    if candidate.WithPinnedPayload(func([]byte)error{return nil})==nil {t.Fatal("old alias borrowed")}
    if err:=candidate.WaitClosed(context.Background());err==nil {t.Fatal("old alias observed closure")}
    if err:=candidate.Close();err==nil {t.Fatal("old alias closed moved candidate")}
    if !owner.VerifySourceGeneration(){t.Fatal("owner lost candidate")}
    copyOwner:=*owner
    if copyOwner.VerifySourceGeneration(){t.Fatal("copied owner gained authority")}
    if err:=copyOwner.Close();err==nil {t.Fatal("copied owner closed candidate")}
    if err:=owner.WithPinnedPayload(func(got []byte)error {if !bytes.Equal(got,raw){t.Fatal("wrong payload")};return nil});err!=nil {t.Fatal(err)}
    if err:=owner.Close();err!=nil {t.Fatal(err)}
    if err:=owner.WaitClosed(context.Background());err!=nil {t.Fatal(err)}
}

func TestGHACandidateClaimRejectsBorrowAndConcurrentDuplicate(t *testing.T) {
    makeCandidate:=func()*ghaGuardVerifiedCandidate{raw:=[]byte("held");candidate:=&ghaGuardVerifiedCandidate{issuer:&ghaGuardIssuer{token:1},payload:raw,generation:ghaGuardGeneration{Source:ghaGuardSource,SourceTree:ghaGuardSourceTree,ProducerRevision:strings.Repeat("a",40),RunID:1,RunAttempt:1,JobID:2,CheckRunID:3,ArtifactID:4,OutputSHA:digest(raw),ProofSHA:strings.Repeat("b",64)}};candidate.self=candidate;return candidate}
    candidate:=makeCandidate()
    entered,release,done:=make(chan struct{}),make(chan struct{}),make(chan error,1)
    var releaseOnce sync.Once
    releaseBorrow:=func(){releaseOnce.Do(func(){close(release)})}
    joined:=false
    t.Cleanup(func(){releaseBorrow();if !joined {select{case <-done:joined=true;case <-time.After(3*time.Second):t.Error("owned borrow did not finish")}}})
    go func(){done<-candidate.WithPinnedPayload(func([]byte)error{close(entered);<-release;return nil})}()
    select {case <-entered:case <-time.After(3*time.Second):t.Fatal("borrow did not start")}
    if owner,err:=candidate.Claim();err==nil||owner!=nil {t.Fatal("claim during borrow accepted")}
    releaseBorrow()
    select {case err:=<-done:joined=true;if err!=nil{t.Fatal(err)};case <-time.After(3*time.Second):t.Fatal("borrow did not finish")}
    owners:=make(chan *ghaGuardOwnedCandidate,2)
    for i:=0;i<2;i++ {go func(){owner,_:=candidate.Claim();owners<-owner}()}
    count:=0;var winner *ghaGuardOwnedCandidate
    for i:=0;i<2;i++ {select {case owner:=<-owners:if owner!=nil {count++;winner=owner};case <-time.After(3*time.Second):t.Fatal("concurrent claims did not finish")}}
    if count!=1 {t.Fatal("concurrent claims did not produce exactly one owner",count)}
    if err:=winner.Close();err!=nil{t.Fatal(err)}
}

func TestGHACandidateClaimAllowsInvalidPartialCleanup(t *testing.T) {
    candidate:=&ghaGuardVerifiedCandidate{issuer:&ghaGuardIssuer{token:1},payload:[]byte("partial")};candidate.self=candidate
    owner,err:=candidate.Claim();if err!=nil||owner==nil {t.Fatal("partial owner could not be claimed",err)}
    if owner.VerifySourceGeneration(){t.Fatal("partial generation accepted")}
    if err:=owner.Close();err!=nil{t.Fatal(err)}
    if err:=owner.WaitClosed(context.Background());err!=nil{t.Fatal(err)}
    if owner2,err:=candidate.Claim();err==nil||owner2!=nil {t.Fatal("closed candidate was claimed")}
}


func TestGHACandidateOwnedBorrowCloseDrains(t *testing.T) {
    raw:=[]byte("owned-synthetic-payload")
    candidate:=&ghaGuardVerifiedCandidate{issuer:&ghaGuardIssuer{token:1},payload:raw,generation:ghaGuardGeneration{Source:ghaGuardSource,SourceTree:ghaGuardSourceTree,ProducerRevision:strings.Repeat("a",40),RunID:1,RunAttempt:1,JobID:2,CheckRunID:3,ArtifactID:4,OutputSHA:digest(raw),ProofSHA:strings.Repeat("b",64)}};candidate.self=candidate
    owner,err:=candidate.Claim();if err!=nil{t.Fatal(err)}
    entered,release,done:=make(chan struct{}),make(chan struct{}),make(chan error,1)
    var releaseOnce sync.Once
    releaseBorrow:=func(){releaseOnce.Do(func(){close(release)})}
    joined:=false
    t.Cleanup(func(){releaseBorrow();if !joined {select{case <-done:joined=true;case <-time.After(3*time.Second):t.Error("owned borrow did not finish")}}})
    go func(){done<-owner.WithPinnedPayload(func(got []byte)error{close(entered);<-release;if !bytes.Equal(got,raw){t.Error("payload cleared before borrow ended")};return nil})}()
    select {case <-entered:case <-time.After(3*time.Second):t.Fatal("owned borrow did not start")}
    if err:=owner.Close();err!=nil{t.Fatal(err)}
    if owner.VerifySourceGeneration(){t.Fatal("close did not invalidate")}
    if err:=candidate.Close();err==nil{t.Fatal("old alias close succeeded")}
    short,cancel:=context.WithTimeout(context.Background(),time.Millisecond);defer cancel()
    if owner.WaitClosed(short)==nil{t.Fatal("active borrow reported closed")}
    releaseBorrow()
    select {case err:=<-done:joined=true;if err==nil{t.Fatal("closed borrow reported success")};case <-time.After(3*time.Second):t.Fatal("owned borrow did not finish")}
    closed,stop:=context.WithTimeout(context.Background(),3*time.Second);defer stop()
    if err:=owner.WaitClosed(closed);err!=nil{t.Fatal(err)}
}


func TestGHACandidateRejectsRawCopyBeforeClaim(t *testing.T) {
    raw:=[]byte("unique-held-payload")
    original:=&ghaGuardVerifiedCandidate{issuer:&ghaGuardIssuer{token:1},payload:raw,generation:ghaGuardGeneration{Source:ghaGuardSource,SourceTree:ghaGuardSourceTree,ProducerRevision:strings.Repeat("a",40),RunID:1,RunAttempt:1,JobID:2,CheckRunID:3,ArtifactID:4,OutputSHA:digest(raw),ProofSHA:strings.Repeat("b",64)}}
    original.self=original
    // Model the fields of a pre-use value copy without copying sync.Mutex.
    clone:=&ghaGuardVerifiedCandidate{self:original.self,issuer:original.issuer,payload:original.payload,generation:original.generation}
    if owner,err:=clone.Claim();err==nil||owner!=nil {t.Fatal("raw copy claimed ownership")}
    if clone.VerifySourceGeneration(){t.Fatal("raw copy was valid")}
    if _,ok:=clone.Generation();ok {t.Fatal("raw copy exposed generation")}
    if err:=clone.Close();err==nil {t.Fatal("raw copy closed the original payload")}
    if string(raw)!="unique-held-payload" {t.Fatal("raw copy cleared shared bytes")}
    owner,err:=original.Claim();if err!=nil{t.Fatal(err)}
    if err:=owner.WithPinnedPayload(func(got []byte)error{if string(got)!="unique-held-payload"{t.Fatal("owner lost bytes")};return nil});err!=nil{t.Fatal(err)}
    if err:=owner.Close();err!=nil{t.Fatal(err)}
    if err:=owner.WaitClosed(context.Background());err!=nil{t.Fatal(err)}
}

func TestGHACandidateOriginalAndRawCopyConcurrentClaim(t *testing.T) {
    raw:=[]byte("held")
    original:=&ghaGuardVerifiedCandidate{issuer:&ghaGuardIssuer{token:1},payload:raw,generation:ghaGuardGeneration{Source:ghaGuardSource,SourceTree:ghaGuardSourceTree,ProducerRevision:strings.Repeat("a",40),RunID:1,RunAttempt:1,JobID:2,CheckRunID:3,ArtifactID:4,OutputSHA:digest(raw),ProofSHA:strings.Repeat("b",64)}}
    original.self=original
    clone:=&ghaGuardVerifiedCandidate{self:original.self,issuer:original.issuer,payload:original.payload,generation:original.generation}
    type claimResult struct {owner *ghaGuardOwnedCandidate; err error}
    results:=make(chan claimResult,2)
    go func(){owner,err:=original.Claim();results<-claimResult{owner,err}}()
    go func(){owner,err:=clone.Claim();results<-claimResult{owner,err}}()
    var owner *ghaGuardOwnedCandidate;success:=0
    for i:=0;i<2;i++ {
        select {
        case result:=<-results:
            if result.err==nil&&result.owner!=nil {success++;owner=result.owner}
        case <-time.After(3*time.Second):t.Fatal("claim worker did not finish")
        }
    }
    if success!=1||owner==nil {t.Fatal("claims did not yield exactly one owner",success)}
    if string(raw)!="held" {t.Fatal("copy or claim cleared shared bytes")}
    if err:=owner.Close();err!=nil{t.Fatal(err)}
}
