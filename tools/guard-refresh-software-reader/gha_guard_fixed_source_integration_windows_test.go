//go:build windows

package main

import (
    "archive/zip"
    "bytes"
    "compress/gzip"
    "context"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "os"
    "strings"
    "sync/atomic"
    "testing"
    "time"
)

// The fixture contains only fixed public Git objects. The proof and signature
// in this test are synthetic and do not confer production authority.
type ghaGuardFixedSourceFixture struct {
    Repository struct { ID int64 `json:"id"`; FullName string `json:"fullName"` } `json:"repository"`
    Source string `json:"sourceCommit"`
    Tree string `json:"sourceTree"`
    RepoResponse json.RawMessage `json:"repoResponse"`
    CommitResponse json.RawMessage `json:"commitResponse"`
    Trees map[string]json.RawMessage `json:"trees"`
    Blobs map[string][]byte `json:"blobs"`
    Inputs []struct { Path string `json:"path"`; Size int64 `json:"size"`; SHA string `json:"sha256"`; Blob string `json:"gitBlob"` } `json:"inputs"`
}

func TestGHAGuardFixedSourceConstructor249(t *testing.T) {
    file,err:=os.Open("testdata/guard-source-80c4.json.gz");if err!=nil {t.Fatal(err)}
    compressed,readFileErr:=io.ReadAll(io.LimitReader(file,973011));closeFileErr:=file.Close()
    if readFileErr!=nil||closeFileErr!=nil||len(compressed)!=973010||digest(compressed)!="f9c2c9550eb5c7c8f93e76d62a755521b5c9074d87b741e71b880f565a013482" {t.Fatal("fixed fixture mismatch")}
    reader,err:=gzip.NewReader(bytes.NewReader(compressed));if err!=nil {t.Fatal(err)}
    raw,readErr:=io.ReadAll(io.LimitReader(reader,4<<20+1));closeErr:=reader.Close()
    if readErr!=nil||closeErr!=nil||len(raw)>4<<20 {t.Fatal("invalid compressed fixture")}
    var fixture ghaGuardFixedSourceFixture
    decoder:=json.NewDecoder(bytes.NewReader(raw))
    if decoder.Decode(&fixture)!=nil||decoder.Decode(new(any))!=io.EOF {t.Fatal("invalid fixture")}
    if fixture.Repository.ID!=1406818713||fixture.Repository.FullName!="AutoDevJapan/cc-pc-tools-rs"||fixture.Source!=ghaGuardSource||fixture.Tree!=ghaGuardSourceTree||len(fixture.Trees)!=10||len(fixture.Blobs)!=248||len(fixture.Inputs)!=249 {t.Fatal("fixture not bound to fixed source")}
    records:=make([]ghaGuardSourceRecord,0,len(fixture.Inputs))
    for _,item:=range fixture.Inputs {records=append(records,ghaGuardSourceRecord{Path:item.Path,Size:item.Size,SHA:item.SHA,Blob:item.Blob})}
    manifest:=ghaGuardTestManifest()
    var sourceProof ghaGuardSourceProof
    if json.Unmarshal(ghaGuardTestProof(manifest),&sourceProof)!=nil {t.Fatal("invalid synthetic proof")}
    var snapshot ghaGuardProofSnapshot
    if json.Unmarshal(sourceProof.Before,&snapshot)!=nil {t.Fatal("invalid synthetic snapshot")}
    snapshot.Inputs=records
    inventory,err:=json.Marshal(records);if err!=nil {t.Fatal(err)}
    snapshot.InventorySHA=digest(inventory)
    before,err:=json.Marshal(snapshot);if err!=nil {t.Fatal(err)}
    exit:=0
    proof,err:=json.Marshal(ghaGuardSourceProof{Domain:"guard-gha-source-proof-v1",Before:before,After:before,CompilerExit:&exit});if err!=nil {t.Fatal(err)}
    for _, item:=range snapshot.Inputs {
        body,ok:=fixture.Blobs[item.Blob]
        if !ok||int64(len(body))!=item.Size||digest(body)!=item.SHA||ghaGuardGitObjectID("blob",body)!=item.Blob {t.Fatal("fixture blob mismatch",item.Path)}
    }
    for _,fault:=range []string{"none","blob-mutated","blob-missing"} {
        t.Run(fault,func(t *testing.T){
            candidateManifest:=manifest
            candidateManifest.SourceManifestSHA=digest(proof)
            capture,err:=json.Marshal(candidateManifest);if err!=nil {t.Fatal(err)}
            var zipped bytes.Buffer;writer:=zip.NewWriter(&zipped)
            for _,item:=range []struct{name string;data []byte}{{"capture.json",capture},{"guard.exe",ghaGuardTestPE()},{"source-proof.json",proof}} {
                out,err:=writer.Create(item.name);if err!=nil {t.Fatal(err)}
                if _,err=out.Write(item.data);err!=nil {t.Fatal(err)}
            }
            if err=writer.Close();err!=nil {t.Fatal(err)}
            archive:=zipped.Bytes()
            e:=ghaGuardMetadataExpectation{RepositoryID:1406818713,HeadRepositoryID:1406818713,Source:candidateManifest.Source,SourceTree:candidateManifest.SourceTree,ProducerRevision:candidateManifest.ProducerRevision,WorkflowPath:".github/workflows/unattended-readiness.yml",Event:"workflow_dispatch",Ref:"refs/heads/main",WorkflowID:7,RunID:candidateManifest.RunID,RunAttempt:candidateManifest.RunAttempt,JobID:candidateManifest.JobID,CheckRunID:candidateManifest.CheckRunID,JobName:"Guard bootstrap artifact",ArtifactID:4,ArtifactName:fmt.Sprintf("guard-bootstrap-%d-%d-%d",candidateManifest.RunID,candidateManifest.RunAttempt,candidateManifest.JobID)}
            publication:=ghaGuardPublicationRecord{Domain:"guard-gha-publication-v1",RepositoryID:e.RepositoryID,Source:e.Source,SourceTree:e.SourceTree,ProducerRevision:e.ProducerRevision,RunID:e.RunID,RunAttempt:e.RunAttempt,JobID:e.JobID,CheckRunID:e.CheckRunID,ArtifactID:e.ArtifactID,ArtifactName:e.ArtifactName,ArchiveSHA:digest(archive),OutputSHA:candidateManifest.OutputSHA,CaptureSHA:digest(capture),SourceManifestSHA:candidateManifest.SourceManifestSHA,AttestationID:"fixture"}
            published,err:=json.Marshal(publication);if err!=nil {t.Fatal(err)}
            material:=&ghaGuardAcquiredEvidence{
                run:ghaGuardRun{RepositoryID:e.RepositoryID,HeadRepositoryID:e.HeadRepositoryID,HeadSHA:e.ProducerRevision,WorkflowPath:e.WorkflowPath,Event:e.Event,HeadBranch:"main",WorkflowID:e.WorkflowID,RunID:e.RunID,RunAttempt:e.RunAttempt,Status:"completed",Conclusion:"success"},
                job:ghaGuardJob{RunID:e.RunID,JobID:e.JobID,CheckRunID:e.CheckRunID,RunAttempt:e.RunAttempt,Name:e.JobName,Status:"completed",Conclusion:"success",UploadStepCompleted:true},
                artifact:ghaGuardArtifact{RepositoryID:e.RepositoryID,HeadRepositoryID:e.HeadRepositoryID,HeadSHA:e.ProducerRevision,RunID:e.RunID,ID:e.ArtifactID,Name:e.ArtifactName,Size:int64(len(archive)),Digest:"sha256:"+publication.ArchiveSHA,ExpiresAt:time.Now().UTC().Add(time.Hour)},
                workflowID:e.WorkflowID,workflowPath:e.WorkflowPath,workflowActive:true,jobLog:append([]byte("2026-01-02T03:04:05.000Z guard_gha_publication_v1="),published...),manifest:candidateManifest,archive:append([]byte(nil),archive...),bundle:[]byte("synthetic-signature"),
            }
            var blobCalls, targetCalls atomic.Int64
            client:=&http.Client{Transport:ghaGuardTreeTestTransport(func(req *http.Request)(*http.Response,error){
                key:=strings.TrimPrefix(req.URL.Path,"/repos/AutoDevJapan/cc-pc-tools-rs")
                var body []byte;status:=http.StatusOK
                switch {
                case key=="":body=fixture.RepoResponse
                case key=="/git/commits/"+ghaGuardSource:body=fixture.CommitResponse
                case strings.HasPrefix(key,"/git/trees/"):body=fixture.Trees[strings.TrimPrefix(key,"/git/trees/")];if body==nil {status=http.StatusNotFound}
                case strings.HasPrefix(key,"/git/blobs/"):
                    blobCalls.Add(1);id:=strings.TrimPrefix(key,"/git/blobs/");body=fixture.Blobs[id]
                    if id==snapshot.Inputs[0].Blob {targetCalls.Add(1)}
                    if id==snapshot.Inputs[0].Blob&&fault=="blob-missing" {status=http.StatusNotFound}
                    if id==snapshot.Inputs[0].Blob&&fault=="blob-mutated" {body=append([]byte(nil),body...);body[0]^=1}
                    if body==nil {status=http.StatusNotFound}
                default:status=http.StatusNotFound
                }
                return &http.Response{StatusCode:status,Header:make(http.Header),Body:io.NopCloser(bytes.NewReader(body)),Request:req},nil
            })}
            sourceCalls,signatureCalls:=0,0
            sourceCheck:=func(ctx context.Context,held []byte,m ghaGuardManifest)error {
                sourceCalls++
                if len(held)==0||len(material.archive)==0||&held[0]!=&material.archive[0] {return errGHAGuardUnqualified}
                return ghaGuardVerifyArchiveGitSourceWithClient(ctx,held,m,client)
            }
            signatureCheck:=func(ctx context.Context,bundle,payload []byte,m ghaGuardManifest)error {
                signatureCalls++
                if sourceCalls!=1||blobCalls.Load()!=249||!bytes.Equal(payload,ghaGuardTestPE())||m!=candidateManifest {return errGHAGuardUnqualified}
                return nil
            }
            candidate,err:=ghaGuardConstructVerifiedCandidateWith(context.Background(),e,material,signatureCheck,sourceCheck)
            if fault!="none" {if err==nil||candidate!=nil||sourceCalls!=1||signatureCalls!=0||targetCalls.Load()==0 {t.Fatal("source failure did not reach the intended blob boundary",err)};return}
            if err!=nil||candidate==nil||sourceCalls!=1||signatureCalls!=1||blobCalls.Load()!=249||!candidate.VerifySourceGeneration(){t.Fatal("fixed 249 source integration failed",err,blobCalls.Load())}
            owner,err:=candidate.Claim();if err!=nil||owner==nil {t.Fatal("candidate claim failed",err)}
            if err=owner.Close();err!=nil {t.Fatal(err)}
            if err=owner.WaitClosed(context.Background());err!=nil {t.Fatal(err)}
        })
    }
}
