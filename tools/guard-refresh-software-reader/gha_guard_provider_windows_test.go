//go:build windows

package main

import (
    "archive/zip"
    "bytes"
    "context"
    "encoding/binary"
    "encoding/json"
    "errors"
    "io"
    "net/http"
    "net/http/httptest"
    "os"
    "strings"
    "sync"
    "testing"
    "time"
)

func ghaGuardTestPE() []byte {
    raw:=make([]byte,256)
    raw[0]='M';raw[1]='Z'
    binary.LittleEndian.PutUint32(raw[0x3c:],128)
    copy(raw[128:],[]byte{'P','E',0,0})
    binary.LittleEndian.PutUint16(raw[132:],0x8664)
    return raw
}

func ghaGuardTestProof(manifest ghaGuardManifest) []byte {
    observation:=[]byte(`{"version":"fixture","env":{"CGO_ENABLED":"0","GOARCH":"amd64","GOFLAGS":"","GOOS":"windows","GOTOOLCHAIN":"local","GOWORK":"off"}}`)
    graph:=[]byte(`[{"ImportPath":"archive/zip"}]`)
    command:=[]byte(`{"directory":"repair-guard","executable":"go","arguments":["build","-mod=readonly","-pgo=off","-trimpath","-o","\u003cfixed-attempt-directory\u003e/guard-build.exe","."]}`)
    image:=[]byte(`{"os":"fixture","version":"owned","runner_os":"Windows","runner_arch":"X64"}`)
    inputs:=[]ghaGuardSourceRecord{{Path:"repair-guard/go.mod",Size:1,SHA:strings.Repeat("a",64),Blob:strings.Repeat("b",40)}}
    inventory,_:=json.Marshal(inputs)
    before:=ghaGuardProofSnapshot{Domain:"guard-gha-source-before-v1",RepositoryID:manifest.RepositoryID,Source:manifest.Source,RunID:manifest.RunID,RunAttempt:manifest.RunAttempt,Event:"workflow_dispatch",SourceTree:manifest.SourceTree,ProducerRevision:manifest.ProducerRevision,CheckRunID:manifest.CheckRunID,InventorySHA:digest(inventory),RunnerToolchainObservationSHA:manifest.RunnerToolchainSHA,RunnerToolchainObservation:observation,BuildGraph:graph,SourceTreeBefore:manifest.SourceTree,GitTreeSHA:strings.Repeat("c",64),GitArchiveSHA:strings.Repeat("d",64),CompileCommand:command,RunnerImageSHA:manifest.RunnerImageSHA,RunnerImage:image,BuildGraphSHA:digest(graph),CompileCommandSHA:manifest.CompileCommandSHA,Inputs:inputs}
    snapshot,_:=json.Marshal(before);exit:=0
    raw,_:=json.Marshal(ghaGuardSourceProof{Domain:"guard-gha-source-proof-v1",Before:snapshot,After:snapshot,CompilerExit:&exit})
    return raw
}

func ghaGuardTestManifest() ghaGuardManifest {
    observation:=[]byte(`{"version":"fixture","env":{"CGO_ENABLED":"0","GOARCH":"amd64","GOFLAGS":"","GOOS":"windows","GOTOOLCHAIN":"local","GOWORK":"off"}}`)
    command:=[]byte(`{"directory":"repair-guard","executable":"go","arguments":["build","-mod=readonly","-pgo=off","-trimpath","-o","\u003cfixed-attempt-directory\u003e/guard-build.exe","."]}`)
    image:=[]byte(`{"os":"fixture","version":"owned","runner_os":"Windows","runner_arch":"X64"}`)
    m:=ghaGuardManifest{Domain:"guard-gha-capture-v1",RepositoryID:1406818713,Source:ghaGuardSource,SourceTree:ghaGuardSourceTree,ProducerRevision:strings.Repeat("b",40),RunID:2,RunAttempt:1,JobID:3,CheckRunID:9,RunnerToolchainSHA:digest(observation),CompileCommandSHA:digest(command),RunnerImageSHA:digest(image),OutputName:"guard.exe",OutputBytes:int64(len(ghaGuardTestPE())),OutputSHA:digest(ghaGuardTestPE())}
    m.SourceManifestSHA=digest(ghaGuardTestProof(m))
    return m
}

func ghaGuardTestArchive(t *testing.T, manifest ghaGuardManifest, payload []byte, extra string) []byte {
    t.Helper()
    var out bytes.Buffer
    z:=zip.NewWriter(&out)
    m,err:=z.Create("capture.json");if err!=nil {t.Fatal(err)}
    raw,err:=json.Marshal(manifest);if err!=nil {t.Fatal(err)}
    if _,err=m.Write(raw);err!=nil {t.Fatal(err)}
    exe,err:=z.Create("guard.exe");if err!=nil {t.Fatal(err)}
    if _,err=exe.Write(payload);err!=nil {t.Fatal(err)}
    proof,err:=z.Create("source-proof.json");if err!=nil {t.Fatal(err)};if _,err=proof.Write(ghaGuardTestProof(manifest));err!=nil {t.Fatal(err)}
    if extra!="" {x,err:=z.Create(extra);if err!=nil {t.Fatal(err)};if _,err=x.Write([]byte("x"));err!=nil {t.Fatal(err)}}
    if err=z.Close();err!=nil {t.Fatal(err)}
    return out.Bytes()
}

func ghaGuardTestArchiveRaw(t *testing.T, manifest []byte, payload []byte, proofManifest ghaGuardManifest) []byte {
    t.Helper();var out bytes.Buffer;z:=zip.NewWriter(&out)
    m,err:=z.Create("capture.json");if err!=nil {t.Fatal(err)};if _,err=m.Write(manifest);err!=nil {t.Fatal(err)}
    x,err:=z.Create("guard.exe");if err!=nil {t.Fatal(err)};if _,err=x.Write(payload);err!=nil {t.Fatal(err)}
    proof,err:=z.Create("source-proof.json");if err!=nil {t.Fatal(err)};if _,err=proof.Write(ghaGuardTestProof(proofManifest));err!=nil {t.Fatal(err)}
    if err=z.Close();err!=nil {t.Fatal(err)};return out.Bytes()
}

func TestGHAGuardArchiveSameBuffer(t *testing.T) {
    base:=ghaGuardTestManifest()
    for _,fault:=range []string{"none","archive-sha","source","producer-revision","job","pe-bytes","extra","duplicate","traversal","corrupt","noncanonical","duplicate-key","cancelled"} {
        t.Run(fault,func(t *testing.T){
            manifest:=base
            raw:=ghaGuardTestArchive(t,manifest,ghaGuardTestPE(),map[string]string{"extra":"extra.txt","duplicate":"guard.exe","traversal":"../guard.exe"}[fault])
            if fault=="noncanonical"||fault=="duplicate-key" {
                original,_:=json.Marshal(manifest);if fault=="noncanonical" {original=append([]byte(" "),original...)} else {original=append(original[:len(original)-1],[]byte(`,"domain":"guard-gha-capture-v1"}`)...)}
                raw=ghaGuardTestArchiveRaw(t,original,ghaGuardTestPE(),manifest)
            }
            expect:=ghaGuardArchiveExpectation{Manifest:base,ArchiveSHA:digest(raw),ArchiveBytes:int64(len(raw))}
            ctx:=context.Background()
            switch fault {
            case "archive-sha": expect.ArchiveSHA=strings.Repeat("0",64)
            case "source": expect.Manifest.Source=strings.Repeat("b",40)
            case "producer-revision": expect.Manifest.ProducerRevision=expect.Manifest.Source
            case "job": expect.Manifest.JobID++
            case "pe-bytes": expect.Manifest.OutputSHA=strings.Repeat("1",64)
            case "corrupt": raw=raw[:len(raw)-1];expect.ArchiveBytes=int64(len(raw));expect.ArchiveSHA=digest(raw)
            case "cancelled": var cancel context.CancelFunc;ctx,cancel=context.WithCancel(ctx);cancel()
            }
            got,err:=ghaGuardReadArchive(ctx,raw,expect)
            if fault=="none" {if err!=nil||!bytes.Equal(got,ghaGuardTestPE()){t.Fatal("valid archive rejected",err)}} else if err==nil||got!=nil {t.Fatal("invalid archive accepted",fault)}
        })
    }
}

func TestGHAGuardMetadataRequiresBoundRunJobAndArtifact(t *testing.T) {
    now:=time.Unix(1800000000,0).UTC()
    expectation:=ghaGuardMetadataExpectation{RepositoryID:1406818713,HeadRepositoryID:1406818713,Source:ghaGuardSource,SourceTree:ghaGuardSourceTree,ProducerRevision:strings.Repeat("b",40),WorkflowPath:".github/workflows/unattended-readiness.yml",Event:"workflow_dispatch",Ref:"refs/heads/main",WorkflowID:7,RunID:2,RunAttempt:1,JobID:3,CheckRunID:9,JobName:"Guard bootstrap artifact",ArtifactID:4,ArtifactName:"guard-bootstrap-2-1-3"}
    run:=ghaGuardRun{RepositoryID:1406818713,HeadRepositoryID:1406818713,HeadSHA:expectation.ProducerRevision,WorkflowPath:expectation.WorkflowPath,Event:"workflow_dispatch",HeadBranch:"main",WorkflowID:7,RunID:2,RunAttempt:1,Status:"completed",Conclusion:"success"}
    job:=ghaGuardJob{RunID:2,JobID:3,CheckRunID:9,RunAttempt:1,Name:"Guard bootstrap artifact",Status:"completed",Conclusion:"success",UploadStepCompleted:true}
    artifact:=ghaGuardArtifact{RepositoryID:1406818713,HeadRepositoryID:1406818713,HeadSHA:expectation.ProducerRevision,RunID:2,ID:4,Name:"guard-bootstrap-2-1-3",Size:100,Digest:"sha256:"+strings.Repeat("a",64),ExpiresAt:now.Add(time.Hour)}
    capture:=ghaGuardCaptureBinding{RepositoryID:1406818713,Source:expectation.Source,ProducerRevision:expectation.ProducerRevision,RunID:2,RunAttempt:1,JobID:3,CheckRunID:9,ArtifactID:4}
    for _,fault:=range []string{"none","run-head","run-event","workflow-id","attempt","job","check-run","job-failed","step","artifact-run","artifact-head","expired","digest","binding","unknown-expectation"} {
        t.Run(fault,func(t *testing.T){
            r,j,a,c,e:=run,job,artifact,capture,expectation
            switch fault {
            case "run-head":r.HeadSHA=e.Source
            case "run-event":r.Event="push"
            case "workflow-id":r.WorkflowID++
            case "attempt":j.RunAttempt++
            case "job":j.JobID++
            case "check-run":j.CheckRunID++
            case "job-failed":j.Conclusion="failure"
            case "step":j.UploadStepCompleted=false
            case "artifact-run":a.RunID++
            case "artifact-head":a.HeadSHA=e.Source
            case "expired":a.ExpiresAt=now
            case "digest":a.Digest="sha256:bad"
            case "binding":c.ArtifactID++
            case "unknown-expectation":e.JobID=0
            }
            if ghaGuardMetadataMatches(e,r,j,a,c,now)!=(fault=="none") {t.Fatal("metadata mismatch",fault)}
        })
    }
}

func TestGHAGuardFixedExpectationUnavailable(t *testing.T) {
    if value,err:=ghaGuardFixedExpectation();err==nil||value!=nil {t.Fatal("unmeasured run became authority")}
}

type ghaGuardFailClose struct {io.Reader}
func (ghaGuardFailClose) Close() error {return errors.New("owned_close_failure")}

// Candidate is private source-only evidence, not an update authority. The
// metadata observations and source/build proof must be independently acquired
// by a trusted fixed provider before this helper may be used in a runtime.
type ghaGuardCandidate struct {
    raw []byte
    archiveSHA,payloadSHA string
    manifest ghaGuardManifest
}
func ghaGuardReadCandidateForTest(ctx context.Context,e ghaGuardMetadataExpectation,run ghaGuardRun,job ghaGuardJob,artifact ghaGuardArtifact,binding ghaGuardCaptureBinding,now time.Time,body io.ReadCloser,manifest ghaGuardManifest) (candidate *ghaGuardCandidate,err error) {
    if body==nil{return nil,errGHAGuardUnqualified}
    defer func(){if closeErr:=body.Close();closeErr!=nil {if candidate!=nil {clear(candidate.raw);candidate=nil};err=errors.Join(err,errGHAGuardUnqualified)}}()
    if ctx==nil||ctx.Err()!=nil||!ghaGuardMetadataMatches(e,run,job,artifact,binding,now) {return nil,errGHAGuardUnqualified}
    if manifest.RepositoryID!=e.RepositoryID||manifest.Source!=e.Source||manifest.SourceTree!=e.SourceTree||manifest.CheckRunID!=e.CheckRunID||manifest.ProducerRevision!=e.ProducerRevision||manifest.RunID!=e.RunID||manifest.RunAttempt!=e.RunAttempt||manifest.JobID!=e.JobID {return nil,errGHAGuardUnqualified}
    raw,err:=io.ReadAll(io.LimitReader(body,artifact.Size+1));if err!=nil||ctx.Err()!=nil||int64(len(raw))!=artifact.Size {clear(raw);return nil,errGHAGuardUnqualified}
    defer clear(raw)
    archiveSHA:=strings.TrimPrefix(artifact.Digest,"sha256:")
    payload,err:=ghaGuardReadArchive(ctx,raw,ghaGuardArchiveExpectation{Manifest:manifest,ArchiveSHA:archiveSHA,ArchiveBytes:artifact.Size});if err!=nil{return nil,err}
    if ctx.Err()!=nil {clear(payload);return nil,errGHAGuardUnqualified}
    return &ghaGuardCandidate{raw:payload,archiveSHA:archiveSHA,payloadSHA:digest(payload),manifest:manifest},nil
}

func TestGHAGuardBoundedDownloadAndMetadataBinding(t *testing.T) {
    pe:=ghaGuardTestPE();manifest:=ghaGuardTestManifest()
    archive:=ghaGuardTestArchive(t,manifest,pe,"");now:=time.Unix(1800000000,0).UTC()
    e:=ghaGuardMetadataExpectation{RepositoryID:1406818713,HeadRepositoryID:1406818713,Source:manifest.Source,SourceTree:manifest.SourceTree,ProducerRevision:manifest.ProducerRevision,WorkflowPath:".github/workflows/unattended-readiness.yml",Event:"workflow_dispatch",Ref:"refs/heads/main",WorkflowID:7,RunID:2,RunAttempt:1,JobID:3,CheckRunID:9,JobName:"Guard bootstrap artifact",ArtifactID:4,ArtifactName:"guard-bootstrap-2-1-3"}
    run:=ghaGuardRun{RepositoryID:1406818713,HeadRepositoryID:1406818713,HeadSHA:e.ProducerRevision,WorkflowPath:e.WorkflowPath,Event:e.Event,HeadBranch:"main",WorkflowID:7,RunID:2,RunAttempt:1,Status:"completed",Conclusion:"success"}
    job:=ghaGuardJob{RunID:2,JobID:3,CheckRunID:9,RunAttempt:1,Name:e.JobName,Status:"completed",Conclusion:"success",UploadStepCompleted:true}
    artifact:=ghaGuardArtifact{RepositoryID:1406818713,HeadRepositoryID:1406818713,HeadSHA:e.ProducerRevision,RunID:2,ID:4,Name:e.ArtifactName,Size:int64(len(archive)),Digest:"sha256:"+digest(archive),ExpiresAt:now.Add(time.Hour)}
    binding:=ghaGuardCaptureBinding{RepositoryID:1406818713,Source:e.Source,ProducerRevision:e.ProducerRevision,RunID:2,RunAttempt:1,JobID:3,CheckRunID:9,ArtifactID:4}
    for _,fault:=range []string{"none","wrong-metadata","short","extra","cancelled","close-failure"} {
        t.Run(fault,func(t *testing.T){
            j:=job;data:=append([]byte(nil),archive...);ctx:=context.Background()
            switch fault {case "wrong-metadata":j.JobID++;case "short":data=data[:len(data)-1];case "extra":data=append(data,0);case "cancelled":var cancel context.CancelFunc;ctx,cancel=context.WithCancel(ctx);cancel()}
            var body io.ReadCloser=io.NopCloser(bytes.NewReader(data));if fault=="close-failure" {body=ghaGuardFailClose{bytes.NewReader(data)}}
            result,err:=ghaGuardReadCandidateForTest(ctx,e,run,j,artifact,binding,now,body,manifest)
            if fault=="none" {if err!=nil||result==nil||!bytes.Equal(result.raw,pe)||result.archiveSHA!=digest(archive)||result.payloadSHA!=digest(pe){t.Fatal("valid download rejected",err)}} else if err==nil||result!=nil {t.Fatal("invalid download accepted",fault)}
        })
    }
}

func TestGHAGuardSourceProofBoundaries(t *testing.T) {
    observation:=[]byte(`{"version":"go version go1.27.4 windows/amd64","env":{"CGO_ENABLED":"0","GOARCH":"amd64","GOFLAGS":"","GOOS":"windows","GOTOOLCHAIN":"local","GOWORK":"off"}}`)
    graph:=[]byte(`[{"ImportPath":"archive/zip"}]`)
    command:=[]byte(`{"directory":"repair-guard","executable":"go","arguments":["build","-mod=readonly","-pgo=off","-trimpath","-o","\u003cfixed-attempt-directory\u003e/guard-build.exe","."]}`)
    image:=[]byte(`{"os":"windows-2025","version":"owned","runner_os":"Windows","runner_arch":"X64"}`)
    inputs:=[]ghaGuardSourceRecord{{Path:"repair-guard/go.mod",Size:1,SHA:strings.Repeat("a",64),Blob:strings.Repeat("b",40)}}
    encodedInputs,err:=json.Marshal(inputs);if err!=nil{t.Fatal(err)}
    base:=ghaGuardProofSnapshot{Domain:"guard-gha-source-before-v1",RepositoryID:1406818713,Source:ghaGuardSource,RunID:2,RunAttempt:1,Event:"workflow_dispatch",SourceTree:ghaGuardSourceTree,ProducerRevision:strings.Repeat("b",40),CheckRunID:9,InventorySHA:digest(encodedInputs),RunnerToolchainObservationSHA:digest(observation),RunnerToolchainObservation:observation,BuildGraph:graph,SourceTreeBefore:ghaGuardSourceTree,GitTreeSHA:strings.Repeat("c",64),GitArchiveSHA:strings.Repeat("d",64),CompileCommand:command,RunnerImageSHA:digest(image),RunnerImage:image,BuildGraphSHA:digest(graph),CompileCommandSHA:digest(command),Inputs:inputs}
    for _,fault:=range []string{"none","wrong-proof-hash","different-after","compiler-failed","source-mismatch","run-mismatch","inventory-mismatch","unsafe-input","command-mismatch","trailing","cancelled"} {
        t.Run(fault,func(t *testing.T) {
            before:=base;after:=base;exit:=0
            switch fault {
            case "different-after":after.CheckRunID++
            case "compiler-failed":exit=1
            case "source-mismatch":before.Source=strings.Repeat("f",40);after=before
            case "run-mismatch":before.RunID++;after=before
            case "inventory-mismatch":before.InventorySHA=strings.Repeat("f",64);after=before
            case "unsafe-input":before.Inputs=[]ghaGuardSourceRecord{{Path:"repair-guard/../escape",Size:1,SHA:strings.Repeat("a",64),Blob:strings.Repeat("b",40)}};v,_:=json.Marshal(before.Inputs);before.InventorySHA=digest(v);after=before
            case "command-mismatch":before.CompileCommand=[]byte(`{"directory":"repair-guard","executable":"go","arguments":["test"]}`);before.CompileCommandSHA=digest(before.CompileCommand);after=before
            }
            first,err:=json.Marshal(before);if err!=nil{t.Fatal(err)}
            second,err:=json.Marshal(after);if err!=nil{t.Fatal(err)}
            raw,err:=json.Marshal(ghaGuardSourceProof{Domain:"guard-gha-source-proof-v1",Before:first,After:second,CompilerExit:&exit});if err!=nil{t.Fatal(err)}
            if fault=="trailing" {raw=append(raw,[]byte("{}")...)}
            m:=ghaGuardManifest{RepositoryID:1406818713,Source:ghaGuardSource,SourceTree:ghaGuardSourceTree,ProducerRevision:strings.Repeat("b",40),RunID:2,RunAttempt:1,JobID:3,CheckRunID:9,SourceManifestSHA:digest(raw),RunnerToolchainSHA:base.RunnerToolchainObservationSHA,CompileCommandSHA:base.CompileCommandSHA,RunnerImageSHA:base.RunnerImageSHA}
            if fault=="command-mismatch" {m.CompileCommandSHA=before.CompileCommandSHA}
            if fault=="wrong-proof-hash" {m.SourceManifestSHA=strings.Repeat("0",64)}
            ctx:=context.Background();if fault=="cancelled" {var cancel context.CancelFunc;ctx,cancel=context.WithCancel(ctx);cancel()}
            err=ghaGuardValidateSourceProof(ctx,raw,m)
            if fault=="none"&&err!=nil {t.Fatal("valid proof rejected",err)}
            if fault!="none"&&err==nil {t.Fatal("invalid proof accepted",fault)}
        })
    }
}

func TestGHAGuardSignatureRejectsMissingMaterial(t *testing.T) {
    if ghaGuardVerifyActualSignature(context.Background(),nil,nil,ghaGuardManifest{})==nil {t.Fatal("missing signed material accepted")}
    cancelled,cancel:=context.WithCancel(context.Background());cancel()
    if ghaGuardVerifyActualSignature(cancelled,nil,nil,ghaGuardManifest{})==nil {t.Fatal("cancelled signature check accepted")}
}

func TestGHAGuardActualRetainedSignedArtifact(t *testing.T) {
    if os.Getenv("GUARD_GHA_ACTUAL_FIXTURE")!="1" {t.Skip("actual retained public artifact not supplied")}
    archive,err:=os.ReadFile("testdata/actual-guard-artifact.zip");if err!=nil{t.Fatal(err)}
    signed,err:=os.ReadFile("testdata/actual-guard-attestation.sigstore.json");if err!=nil{t.Fatal(err)}
    manifest:=ghaGuardManifest{Domain:"guard-gha-capture-v1",RepositoryID:1406818713,Source:ghaGuardSource,SourceTree:ghaGuardSourceTree,ProducerRevision:"f2f9e4c4cddba70240004e7084443c27c51baee9",RunID:43574333045,RunAttempt:1,JobID:132385166063,CheckRunID:132385166063,SourceManifestSHA:"aeeefe291c6a10a0f8d1bf1ee1a0952daf29dba87046596980c7c509c690f8c8",RunnerToolchainSHA:"0b2c36009670818ff928fee28a95e61590f930041d2fcec9624ec5798964a8f8",CompileCommandSHA:"616dbd64b1f6a3fe614b367a4d4f998dc14ac388a5cced808cc123a1d12b6892",RunnerImageSHA:"3e1e398a479c225605dcfeb8ca3f0b45b86f7b02a1f3eec3c1ce8eb4efff4aa9",OutputName:"guard.exe",OutputBytes:14299136,OutputSHA:"a61e25065ae35d51f1ac8133959176b21e2c3740c1516b76f799371f1c27f9a2"}
    exp:=ghaGuardArchiveExpectation{Manifest:manifest,ArchiveBytes:14597045,ArchiveSHA:"681ae0e1356755a752addf0776ffdab48432fcb850f243d3989e7466e3076452"}
    ctx,cancel:=context.WithTimeout(context.Background(),2*time.Minute);defer cancel()
    payload,err:=ghaGuardReadArchive(ctx,archive,exp);if err!=nil{t.Fatal("actual archive rejected",err)};defer clear(payload)
    if err=ghaGuardVerifyActualSignature(ctx,signed,payload,manifest);err!=nil{t.Fatal("actual signed artifact rejected",err)}
    before:=manifest;before.RunAttempt++
    if ghaGuardVerifyActualSignature(ctx,signed,payload,before)==nil{t.Fatal("wrong run attempt accepted")}
    payload[0]^=1
    if ghaGuardVerifyActualSignature(ctx,signed,payload,manifest)==nil{t.Fatal("modified payload accepted")}
    payload[0]^=1
    bad:=append([]byte(nil),signed...)
    index:=bytes.Index(bad,[]byte("\"sig\":\""));if index<0 {t.Fatal("signature field absent")}
    index+=len("\"sig\":\"")
    if index>=len(bad){t.Fatal("signature field empty")}
    if bad[index]=='A'{bad[index]='B'}else{bad[index]='A'}
    if ghaGuardVerifyActualSignature(ctx,bad,payload,manifest)==nil{t.Fatal("modified signature accepted")}
}


func TestGHAGuardTUFFetcherCancellation(t *testing.T) {
    started,flushed,release,result:=make(chan struct{}),make(chan struct{}),make(chan struct{}),make(chan error,1)
    handlerDone,workerDone:=make(chan struct{}),make(chan struct{})
    var releaseOnce,startedOnce sync.Once
    releaseHandler:=func(){releaseOnce.Do(func(){close(release)})}
    server:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
        defer close(handlerDone)
        startedOnce.Do(func(){close(started)})
        w.WriteHeader(http.StatusOK)
        if f,ok:=w.(http.Flusher);ok {f.Flush()}
        close(flushed)
        select {case <-r.Context().Done():case <-release:}
    }))
    ctx,cancel:=context.WithCancel(context.Background())
    client:=&http.Client{Timeout:5*time.Second}
    fetch:=ghaGuardNewTUFRequestFetcher(ctx,client)
    go func(){defer close(workerDone);_,err:=fetch.DownloadFile(server.URL,1024,time.Minute);result<-err}()
    t.Cleanup(func(){
        releaseHandler();cancel();client.CloseIdleConnections()
        select {case <-workerDone:case <-time.After(7*time.Second):t.Error("owned TUF fetch worker did not finish")}
        select {case <-handlerDone:case <-time.After(3*time.Second):t.Error("owned TUF handler did not finish")}
        server.CloseClientConnections()
        server.Close()
    })
    select {case <-started:case <-time.After(3*time.Second):t.Fatal("TUF request did not start")}
    select {case <-flushed:case <-time.After(3*time.Second):t.Fatal("TUF response did not flush")}
    cancel()
    select {
    case err:=<-result:
        if err==nil {t.Fatal("stalled TUF response accepted after cancellation")}
    case <-time.After(3*time.Second):
        t.Fatal("TUF fetch did not stop on cancellation")
    }
    select {case <-handlerDone:case <-time.After(3*time.Second):t.Fatal("TUF handler did not finish")}
}

func TestGHAGuardTUFFetcherPositiveAndHTTPError(t *testing.T) {
    server:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
        if r.URL.Path=="/missing" {w.WriteHeader(http.StatusNotFound);return}
        _,_=io.WriteString(w,"owned-tuf-metadata")
    }))
    defer server.Close()
    ctx,cancel:=context.WithTimeout(context.Background(),3*time.Second);defer cancel()
    fetch:=ghaGuardNewTUFRequestFetcher(ctx,&http.Client{})
    raw,err:=fetch.DownloadFile(server.URL+"/good",1024,time.Minute)
    if err!=nil||string(raw)!="owned-tuf-metadata" {t.Fatal("normal fetch failed",err)}
    if _,err=fetch.DownloadFile(server.URL+"/missing",1024,time.Minute);err==nil {t.Fatal("HTTP error accepted")}
    if _,err=fetch.DownloadFile(server.URL+"/good",2,time.Minute);err==nil {t.Fatal("oversized response accepted")}
}
