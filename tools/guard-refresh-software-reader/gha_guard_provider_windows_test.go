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
    "strings"
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

func ghaGuardTestArchive(t *testing.T, manifest ghaGuardManifest, payload []byte, extra string) []byte {
    t.Helper()
    var out bytes.Buffer
    z:=zip.NewWriter(&out)
    m,err:=z.Create("capture.json");if err!=nil {t.Fatal(err)}
    raw,err:=json.Marshal(manifest);if err!=nil {t.Fatal(err)}
    if _,err=m.Write(raw);err!=nil {t.Fatal(err)}
    exe,err:=z.Create("guard.exe");if err!=nil {t.Fatal(err)}
    if _,err=exe.Write(payload);err!=nil {t.Fatal(err)}
    proof,err:=z.Create("source-proof.json");if err!=nil {t.Fatal(err)};if _,err=proof.Write([]byte("owned-source-proof"));err!=nil {t.Fatal(err)}
    if extra!="" {x,err:=z.Create(extra);if err!=nil {t.Fatal(err)};if _,err=x.Write([]byte("x"));err!=nil {t.Fatal(err)}}
    if err=z.Close();err!=nil {t.Fatal(err)}
    return out.Bytes()
}


func ghaGuardTestArchiveRaw(t *testing.T, manifest []byte, payload []byte) []byte {
    t.Helper();var out bytes.Buffer;z:=zip.NewWriter(&out)
    m,err:=z.Create("capture.json");if err!=nil {t.Fatal(err)};if _,err=m.Write(manifest);err!=nil {t.Fatal(err)}
    x,err:=z.Create("guard.exe");if err!=nil {t.Fatal(err)};if _,err=x.Write(payload);err!=nil {t.Fatal(err)}
    proof,err:=z.Create("source-proof.json");if err!=nil {t.Fatal(err)};if _,err=proof.Write([]byte("owned-source-proof"));err!=nil {t.Fatal(err)}
    if err=z.Close();err!=nil {t.Fatal(err)};return out.Bytes()
}

func TestGHAGuardArchiveSameBuffer(t *testing.T) {
    base:=ghaGuardManifest{Domain:"guard-gha-capture-v1",RepositoryID:1406818713,Source:ghaGuardSource,SourceTree:ghaGuardSourceTree,ProducerRevision:strings.Repeat("b",40),RunID:2,RunAttempt:1,JobID:3,CheckRunID:9,SourceManifestSHA:digest([]byte("owned-source-proof")),RunnerToolchainSHA:strings.Repeat("c",64),CompileCommandSHA:strings.Repeat("d",64),RunnerImageSHA:strings.Repeat("e",64),OutputName:"guard.exe",OutputBytes:int64(len(ghaGuardTestPE())),OutputSHA:digest(ghaGuardTestPE())}
    for _,fault:=range []string{"none","archive-sha","source","producer-revision","job","pe-bytes","extra","duplicate","traversal","corrupt","noncanonical","duplicate-key","cancelled"} {
        t.Run(fault,func(t *testing.T){
            manifest:=base
            raw:=ghaGuardTestArchive(t,manifest,ghaGuardTestPE(),map[string]string{"extra":"extra.txt","duplicate":"guard.exe","traversal":"../guard.exe"}[fault])
            if fault=="noncanonical"||fault=="duplicate-key" {
                original,_:=json.Marshal(manifest);if fault=="noncanonical" {original=append([]byte(" "),original...)} else {original=append(original[:len(original)-1],[]byte(`,"domain":"guard-gha-capture-v1"}`)...)}
                raw=ghaGuardTestArchiveRaw(t,original,ghaGuardTestPE())
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
    pe:=ghaGuardTestPE();manifest:=ghaGuardManifest{Domain:"guard-gha-capture-v1",RepositoryID:1406818713,Source:ghaGuardSource,SourceTree:ghaGuardSourceTree,ProducerRevision:strings.Repeat("b",40),RunID:2,RunAttempt:1,JobID:3,CheckRunID:9,SourceManifestSHA:digest([]byte("owned-source-proof")),RunnerToolchainSHA:strings.Repeat("c",64),CompileCommandSHA:strings.Repeat("d",64),RunnerImageSHA:strings.Repeat("e",64),OutputName:"guard.exe",OutputBytes:int64(len(pe)),OutputSHA:digest(pe)}
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
