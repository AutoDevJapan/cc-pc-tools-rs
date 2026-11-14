//go:build windows

package main

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "strings"
    "sync"
    "sync/atomic"
    "testing"
    "time"
)

type ghaGuardLiveTestRT func(*http.Request)(*http.Response,error)
func (f ghaGuardLiveTestRT) RoundTrip(r *http.Request)(*http.Response,error){return f(r)}

type ghaGuardLiveTestReader struct { io.Reader; closeErr error; closeCount *atomic.Int64 }
func (r *ghaGuardLiveTestReader) Close() error {if r.closeCount!=nil{r.closeCount.Add(1)};return r.closeErr}

func TestGHAGuardLiveRedirectBoundary(t *testing.T) {
    for _,item:=range []struct{url string;ok bool}{
        {"https://example.blob.core.windows.net/path?sig=fixture",true},
        {"https://pipelines.actions.githubusercontent.com/path?sig=fixture",true},
        {"http://example.blob.core.windows.net/path",false},
        {"https://example.blob.core.windows.net.evil.invalid/path",false},
        {"https://user@example.blob.core.windows.net/path",false},
        {"https://example.blob.core.windows.net:444/path",false},
        {"https://example.blob.core.windows.net/path#fragment",false},
    } {_,err:=ghaGuardLiveRedirectURL(item.url);if (err==nil)!=item.ok{t.Fatal("redirect boundary mismatch",item.ok)}}
}

func TestGHAGuardLiveAcquisition(t *testing.T) {
    for _,fault:=range []string{"none","wrong-workflow","wrong-workflow-id","bad-log","wrong-attestation","metadata-mismatch","archive-mutated","archive-overlong","authority-swap","api-unauthorized","api-denied","api-malformed-redirect","api-redirect-unapproved","api-redirect-close-error","object-redirect","object-close-error","cancelled","empty-bundle"} {
        t.Run(fault,func(t *testing.T){
            manifest:=ghaGuardTestManifest()
            archive:=ghaGuardTestArchive(t,manifest,ghaGuardTestPE(),"")
            e:=ghaGuardMetadataExpectation{RepositoryID:1406818713,HeadRepositoryID:1406818713,Source:manifest.Source,SourceTree:manifest.SourceTree,ProducerRevision:manifest.ProducerRevision,WorkflowPath:".github/workflows/unattended-readiness.yml",Event:"workflow_dispatch",Ref:"refs/heads/main",WorkflowID:7,RunID:manifest.RunID,RunAttempt:manifest.RunAttempt,JobID:manifest.JobID,CheckRunID:manifest.CheckRunID,JobName:"Guard bootstrap artifact",ArtifactID:4,ArtifactName:"guard-bootstrap-2-1-3"}
            capture,_:=json.Marshal(manifest)
            publication:=ghaGuardPublicationRecord{Domain:"guard-gha-publication-v1",RepositoryID:e.RepositoryID,Source:e.Source,SourceTree:e.SourceTree,ProducerRevision:e.ProducerRevision,RunID:e.RunID,RunAttempt:e.RunAttempt,JobID:e.JobID,CheckRunID:e.CheckRunID,ArtifactID:e.ArtifactID,ArtifactName:e.ArtifactName,ArchiveSHA:digest(archive),OutputSHA:manifest.OutputSHA,CaptureSHA:digest(capture),SourceManifestSHA:manifest.SourceManifestSHA,AttestationID:"fixture"}
            published,_:=json.Marshal(publication)
            jobLog:=append([]byte("2026-01-02T03:04:05.000Z guard_gha_publication_v1="),published...)
            if fault=="bad-log" {jobLog=[]byte("no publication")}
            names:=[]string{"Capture the fixed guard build","Check the exact captured files","Attest the guard and source evidence","Upload the fixed guard capture","Record the server-assigned capture artifact"}
            steps:=make([]map[string]any,0,len(names));for i,name:=range names{steps=append(steps,map[string]any{"name":name,"status":"completed","conclusion":"success","number":i+1})}
            run:=map[string]any{"id":e.RunID,"run_attempt":e.RunAttempt,"event":e.Event,"path":e.WorkflowPath,"status":"completed","conclusion":"success","workflow_id":e.WorkflowID,"repository":map[string]any{"id":e.RepositoryID},"head_repository":map[string]any{"id":e.HeadRepositoryID},"head_sha":e.ProducerRevision,"head_branch":"main"}
            jobs:=map[string]any{"total_count":1,"jobs":[]any{map[string]any{"id":e.JobID,"run_id":e.RunID,"name":e.JobName,"status":"completed","conclusion":"success","check_run_url":fmt.Sprintf("https://api.github.com/repos/AutoDevJapan/cc-pc-tools-rs/check-runs/%d",e.CheckRunID),"steps":steps}}}
            artifactID:=e.ArtifactID;if fault=="metadata-mismatch" {artifactID++}
            artifacts:=map[string]any{"total_count":1,"artifacts":[]any{map[string]any{"id":artifactID,"name":e.ArtifactName,"digest":"sha256:"+publication.ArchiveSHA,"expired":false,"expires_at":time.Now().UTC().Add(time.Hour),"size_in_bytes":len(archive),"workflow_run":map[string]any{"id":e.RunID,"repository_id":e.RepositoryID,"head_repository_id":e.HeadRepositoryID,"head_sha":e.ProducerRevision}}}}
            workflowState:="active";if fault=="wrong-workflow" {workflowState="disabled_manually"}
            encode:=func(v any)[]byte{b,err:=json.Marshal(v);if err!=nil{t.Fatal(err)};return b}
            var objectCalls,leaked,apiRedirectCloses,objectCloses atomic.Int64
            var authority *ghaGuardEvidenceAuthority
            api:=&http.Client{Transport:ghaGuardLiveTestRT(func(req *http.Request)(*http.Response,error){
                if req.URL.Host!="api.github.com" {leaked.Add(1);return nil,fmt.Errorf("wrong api host")}
                req.Header.Set("Authorization","Bearer test-only")
                key:=req.URL.Path
                status:=http.StatusOK;header:=make(http.Header);var body []byte
                switch {
                case strings.HasSuffix(key,"/actions/workflows/unattended-readiness.yml"):body=encode(map[string]any{"id":e.WorkflowID,"path":e.WorkflowPath,"state":workflowState})
                case strings.HasSuffix(key,"/actions/jobs/3/logs"):
                    status=http.StatusFound;header.Set("Location","https://fixture.blob.core.windows.net/log?sig=fixture")
                    if fault=="api-denied" {status=http.StatusForbidden}
                    if fault=="api-unauthorized" {status=http.StatusUnauthorized}
                    if fault=="api-malformed-redirect" {header.Set("Location","https://[")}
                    if fault=="api-redirect-unapproved" {header.Set("Location","https://invalid.example.test/path")}
                case strings.HasSuffix(key,"/actions/runs/2"):body=encode(run)
                case strings.HasSuffix(key,"/actions/runs/2/attempts/1/jobs"):body=encode(jobs)
                case strings.HasSuffix(key,"/actions/runs/2/artifacts"):body=encode(artifacts)
                case strings.HasSuffix(key,"/actions/artifacts/4/zip"):status=http.StatusFound;header.Set("Location","https://fixture.blob.core.windows.net/archive?sig=fixture")
                default:status=http.StatusNotFound
                }
                var responseBody io.ReadCloser=io.NopCloser(bytes.NewReader(body))
                if status==http.StatusFound {
                    var closeErr error;if fault=="api-redirect-close-error" {closeErr=io.ErrUnexpectedEOF}
                    responseBody=&ghaGuardLiveTestReader{Reader:bytes.NewReader(body),closeErr:closeErr,closeCount:&apiRedirectCloses}
                }
                return &http.Response{StatusCode:status,Header:header,Body:responseBody,Request:req},nil
            })}
            authority=&ghaGuardEvidenceAuthority{client:api}
            object:=&http.Client{Transport:ghaGuardLiveTestRT(func(req *http.Request)(*http.Response,error){
                objectCalls.Add(1)
                if apiRedirectCloses.Load()!=objectCalls.Load(){leaked.Add(1)}
                if req.Header.Get("Authorization")!=""||req.Header.Get("Cookie")!="" {leaked.Add(1)}
                var body []byte
                switch req.URL.Path {case "/log":body=jobLog;case "/archive":body=append([]byte(nil),archive...);if fault=="archive-mutated"{body[0]^=1};if fault=="archive-overlong"{body=append(body,0)};if fault=="authority-swap"{authority.client=&http.Client{}};default:return nil,fmt.Errorf("unexpected object")}
                status:=http.StatusOK;header:=make(http.Header)
                if fault=="object-redirect"{status=http.StatusFound;header.Set("Location","https://fixture.blob.core.windows.net/unexpected?sig=fixture")}
                var closeErr error;if fault=="object-close-error"{closeErr=io.ErrUnexpectedEOF}
                return &http.Response{StatusCode:status,Header:header,Body:&ghaGuardLiveTestReader{Reader:bytes.NewReader(body),closeErr:closeErr,closeCount:&objectCloses},Request:req},nil
            })}
            ctx:=context.Background();if fault=="cancelled"{var cancel context.CancelFunc;ctx,cancel=context.WithCancel(ctx);cancel()}
            bundle:=[]byte("synthetic-bundle");if fault=="empty-bundle"{bundle=nil}
            attestation:="fixture";if fault=="wrong-attestation"{attestation="other"}
            expected:=e;expected.WorkflowID=0
            if fault=="wrong-workflow-id" {expected.WorkflowID=e.WorkflowID+1}
            material,err:=ghaGuardLiveAcquireWith(ctx,authority,object,expected,bundle,attestation)
            if fault!="none" {
                if err==nil||material!=nil||leaked.Load()!=0 {t.Fatal("invalid acquisition succeeded",fault,err)}
                if (fault=="api-denied"||fault=="api-unauthorized")&&objectCalls.Load()!=0 {t.Fatal("denied endpoint was retried through object client")}
                if (fault=="api-malformed-redirect"||fault=="api-redirect-unapproved"||fault=="api-redirect-close-error")&&(apiRedirectCloses.Load()!=1||objectCalls.Load()!=0){t.Fatal("invalid api redirect was followed or not closed exactly once")}
                if fault=="object-redirect"&&(objectCalls.Load()!=1||objectCloses.Load()!=1||apiRedirectCloses.Load()!=1){t.Fatal("unexpected redirect was followed or not closed exactly once")}
                return
            }
            if err!=nil||material==nil||leaked.Load()!=0||objectCalls.Load()!=2||objectCloses.Load()!=2||apiRedirectCloses.Load()!=2||!bytes.Equal(material.archive,archive)||!bytes.Equal(material.bundle,bundle)||material.manifest!=manifest {t.Fatal("valid acquisition failed",err)}
            bundle[0]^=1;if bytes.Equal(material.bundle,bundle){t.Fatal("bundle was not independently retained")}
            clear(material.archive);clear(material.bundle);clear(material.jobLog)
        })
    }
}

func TestGHAGuardLiveOpenRequiresNormalAuthority(t *testing.T) {
    if candidate,err:=ghaGuardLiveOpen(context.Background(),[]byte("untrusted-bundle"));err==nil||candidate!=nil {t.Fatal("unavailable authority issued a candidate")}
}


func TestGHAGuardLiveRedirectCancellationDrains(t *testing.T) {
    ctx,cancel:=context.WithCancel(context.Background())
    started:=make(chan struct{})
    done:=make(chan error,1)
    var startedOnce sync.Once
    joined:=false
    t.Cleanup(func(){
        cancel()
        if !joined {select{case <-done:joined=true;case <-time.After(3*time.Second):t.Error("download did not terminate")}}
    })
    api:=&http.Client{Transport:ghaGuardLiveTestRT(func(req *http.Request)(*http.Response,error){
        header:=make(http.Header);header.Set("Location","https://fixture.blob.core.windows.net/object?sig=fixture")
        return &http.Response{StatusCode:http.StatusFound,Header:header,Body:io.NopCloser(bytes.NewReader(nil)),Request:req},nil
    })}
    object:=&http.Client{Transport:ghaGuardLiveTestRT(func(req *http.Request)(*http.Response,error){
        startedOnce.Do(func(){close(started)})
        <-req.Context().Done()
        return nil,req.Context().Err()
    })}
    go func(){_,err:=ghaGuardLiveRedirected(ctx,api,object,"https://api.github.com/repos/AutoDevJapan/cc-pc-tools-rs/actions/jobs/3/logs",1024);done<-err}()
    select {case <-started:case <-time.After(3*time.Second):t.Fatal("download did not start")}
    cancel()
    select {
    case err:=<-done:joined=true;if err==nil {t.Fatal("cancelled download succeeded")}
    case <-time.After(3*time.Second):t.Fatal("cancelled download did not terminate")
    }
}
