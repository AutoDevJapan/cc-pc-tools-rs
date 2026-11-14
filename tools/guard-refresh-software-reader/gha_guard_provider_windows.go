//go:build windows

package main

import (
    "archive/zip"
    "bytes"
    "context"
    "encoding/binary"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "strings"
    "time"
)

var errGHAGuardUnqualified = errors.New("guard_gha_unqualified")

const ghaGuardArchiveLimit = 64 << 20
const ghaGuardPayloadLimit = 32 << 20
const ghaGuardManifestLimit = 64 << 10
const ghaGuardSource = "80c4ed5cbd864585f4894c6286d3d5c92194c1f5"
const ghaGuardSourceTree = "84437789dfbe1bf1d56e4b96698b5c8da4a3dfb7"

// These values are observations, not authority. In production each must come
// from a separately authenticated GitHub metadata/provenance provider.
type ghaGuardRun struct {
    RepositoryID,HeadRepositoryID int64
    HeadSHA,WorkflowPath,Event,HeadBranch string
    WorkflowID int64
    RunID int64
    RunAttempt int
    Status,Conclusion string
}
type ghaGuardJob struct {
    RunID,JobID,CheckRunID int64
    RunAttempt int
    Name,Status,Conclusion string
    UploadStepCompleted bool
}
type ghaGuardArtifact struct {
    RepositoryID,HeadRepositoryID int64
    HeadSHA string
    RunID,ID int64
    Name string
    Size int64
    Digest string
    ExpiresAt time.Time
    Expired bool
}
type ghaGuardCaptureBinding struct {
    RepositoryID int64
    Source,ProducerRevision string
    RunID int64
    RunAttempt int
    JobID,CheckRunID,ArtifactID int64
}
type ghaGuardMetadataExpectation struct {
    RepositoryID,HeadRepositoryID int64
    Source,SourceTree,ProducerRevision,WorkflowPath,Event,Ref string
    WorkflowID int64
    RunID int64
    RunAttempt int
    JobID int64
    CheckRunID int64
    JobName string
    ArtifactID int64
    ArtifactName string
}

// The current fixed provider has no admitted run, old target or source
// generation. No caller-selected path/hash can make this plan available.
func ghaGuardFixedExpectation() (*ghaGuardMetadataExpectation,error) {
    return nil,errGHAGuardUnqualified
}

// This compares observations only. An authenticated provider must establish
// that run/job/step and capture binding came from the same server execution.
// JSON, archive content, artifact name and caller-provided values are not proof.
func ghaGuardMetadataMatches(e ghaGuardMetadataExpectation,r ghaGuardRun,j ghaGuardJob,a ghaGuardArtifact,c ghaGuardCaptureBinding,now time.Time) bool {
    if e.RepositoryID!=1406818713||e.HeadRepositoryID!=e.RepositoryID||e.Source!=ghaGuardSource||e.SourceTree!=ghaGuardSourceTree||!ghaGuardHex(e.ProducerRevision,40)||e.WorkflowPath!=".github/workflows/unattended-readiness.yml"||e.Event!="workflow_dispatch"||e.Ref!="refs/heads/main"||e.WorkflowID<=0||e.RunID<=0||e.RunAttempt<=0||e.JobID<=0||e.CheckRunID<=0||e.JobName!="Guard bootstrap artifact"||e.ArtifactID<=0||e.ArtifactName!=fmt.Sprintf("guard-bootstrap-%d-%d-%d",e.RunID,e.RunAttempt,e.JobID)||now.IsZero() {return false}
    if r.RepositoryID!=e.RepositoryID||r.HeadRepositoryID!=e.HeadRepositoryID||r.HeadSHA!=e.ProducerRevision||r.WorkflowPath!=e.WorkflowPath||r.WorkflowID!=e.WorkflowID||r.Event!=e.Event||r.HeadBranch!="main"||r.RunID!=e.RunID||r.RunAttempt!=e.RunAttempt||r.Status!="completed"||r.Conclusion!="success" {return false}
    if j.RunID!=e.RunID||j.RunAttempt!=e.RunAttempt||j.JobID!=e.JobID||j.CheckRunID!=e.CheckRunID||j.Name!=e.JobName||j.Status!="completed"||j.Conclusion!="success"||!j.UploadStepCompleted {return false}
    if a.RepositoryID!=e.RepositoryID||a.HeadRepositoryID!=e.HeadRepositoryID||a.HeadSHA!=e.ProducerRevision||a.RunID!=e.RunID||a.ID!=e.ArtifactID||a.Name!=e.ArtifactName||a.Size<1||a.Size>ghaGuardArchiveLimit||a.Expired||!a.ExpiresAt.After(now)||!strings.HasPrefix(a.Digest,"sha256:")||!ghaGuardHex(strings.TrimPrefix(a.Digest,"sha256:"),64) {return false}
    return c.RepositoryID==e.RepositoryID&&c.Source==e.Source&&c.ProducerRevision==e.ProducerRevision&&c.RunID==e.RunID&&c.RunAttempt==e.RunAttempt&&c.JobID==e.JobID&&c.CheckRunID==e.CheckRunID&&c.ArtifactID==e.ArtifactID
}

// The capture manifest is evidence inside the archive, never an authority.
// Artifact ID is not included because the server allocates it after upload.
type ghaGuardManifest struct {
    Domain string `json:"domain"`
    RepositoryID int64 `json:"repository_id"`
    Source string `json:"source"`
    SourceTree string `json:"source_tree"`
    ProducerRevision string `json:"producer_revision"`
    RunID int64 `json:"run_id"`
    RunAttempt int `json:"run_attempt"`
    JobID int64 `json:"job_id"`
    CheckRunID int64 `json:"check_run_id"`
    SourceManifestSHA string `json:"source_manifest_sha256"`
    RunnerToolchainSHA string `json:"runner_toolchain_observation_sha256"`
    CompileCommandSHA string `json:"compile_command_sha256"`
    RunnerImageSHA string `json:"runner_image_sha256"`
    OutputName string `json:"output_name"`
    OutputBytes int64 `json:"output_bytes"`
    OutputSHA string `json:"output_sha256"`
}
type ghaGuardArchiveExpectation struct {
    Manifest ghaGuardManifest
    ArchiveSHA string
    ArchiveBytes int64
}

// ReadArchive is a bounded pure comparison against values already supplied by
// the metadata/provenance provider. Returned bytes are never an update grant.
func ghaGuardReadArchive(ctx context.Context,archive []byte,e ghaGuardArchiveExpectation) ([]byte,error) {
    fail:=func()([]byte,error){return nil,errGHAGuardUnqualified}
    if ctx==nil||ctx.Err()!=nil||len(archive)==0||len(archive)>ghaGuardArchiveLimit||int64(len(archive))!=e.ArchiveBytes||!ghaGuardHex(e.ArchiveSHA,64)||digest(archive)!=e.ArchiveSHA {return fail()}
    m:=e.Manifest
    if m.Domain!="guard-gha-capture-v1"||m.RepositoryID!=1406818713||m.Source!=ghaGuardSource||m.SourceTree!=ghaGuardSourceTree||!ghaGuardHex(m.ProducerRevision,40)||m.RunID<=0||m.RunAttempt<=0||m.JobID<=0||m.CheckRunID<=0||!ghaGuardHex(m.SourceManifestSHA,64)||!ghaGuardHex(m.RunnerToolchainSHA,64)||!ghaGuardHex(m.CompileCommandSHA,64)||!ghaGuardHex(m.RunnerImageSHA,64)||m.OutputName!="guard.exe"||m.OutputBytes<1||m.OutputBytes>ghaGuardPayloadLimit||!ghaGuardHex(m.OutputSHA,64) {return fail()}
    z,err:=zip.NewReader(bytes.NewReader(archive),int64(len(archive)));if err!=nil||len(z.File)!=3 {return fail()}
    entries:=make(map[string]*zip.File,3)
    for _,f:=range z.File {
        if f==nil||f.Flags&1!=0||!f.Mode().IsRegular()||(f.Name!="capture.json"&&f.Name!="guard.exe"&&f.Name!="source-proof.json")||entries[f.Name]!=nil {return fail()}
        if f.UncompressedSize64>ghaGuardPayloadLimit||f.CompressedSize64>ghaGuardArchiveLimit {return fail()}
        entries[f.Name]=f
    }
    read:=func(f *zip.File,limit int64)([]byte,error){
        if f==nil||int64(f.UncompressedSize64)>limit {return nil,errGHAGuardUnqualified}
        reader,err:=f.Open();if err!=nil{return nil,err}
        raw,readErr:=io.ReadAll(io.LimitReader(reader,limit+1))
        closeErr:=reader.Close()
        if readErr!=nil||closeErr!=nil||int64(len(raw))>limit||uint64(len(raw))!=f.UncompressedSize64 {clear(raw);return nil,errGHAGuardUnqualified}
        return raw,nil
    }
    rawManifest,err:=read(entries["capture.json"],ghaGuardManifestLimit);if err!=nil{return fail()}
    defer clear(rawManifest)
    var actual ghaGuardManifest
    dec:=json.NewDecoder(bytes.NewReader(rawManifest));dec.DisallowUnknownFields()
    if dec.Decode(&actual)!=nil||dec.Decode(new(any))!=io.EOF||actual!=m {return fail()}
    canonical,err:=json.Marshal(actual);if err!=nil||!bytes.Equal(canonical,rawManifest){clear(canonical);return fail()};clear(canonical)
    proof,err:=read(entries["source-proof.json"],8<<20);if err!=nil{return fail()}
    if digest(proof)!=m.SourceManifestSHA {clear(proof);return fail()};clear(proof)
    payload,err:=read(entries["guard.exe"],ghaGuardPayloadLimit);if err!=nil{return fail()}
    if ctx.Err()!=nil||int64(len(payload))!=m.OutputBytes||digest(payload)!=m.OutputSHA||!ghaGuardPEHeader(payload) {clear(payload);return fail()}
    return payload,nil
}

// This bounds only the AMD64 PE signature. It is not a hardened PE parser,
// signature check, build provenance, compiler qualification or install grant.
func ghaGuardPEHeader(raw []byte) bool {
    if len(raw)<64||raw[0]!='M'||raw[1]!='Z' {return false}
    off:=uint64(binary.LittleEndian.Uint32(raw[0x3c:]))
    if off+6>uint64(len(raw)) {return false}
    return bytes.Equal(raw[off:off+4],[]byte{'P','E',0,0})&&binary.LittleEndian.Uint16(raw[off+4:off+6])==0x8664
}


func ghaGuardHex(s string,n int) bool {
    if len(s)!=n{return false}
    for _,r:=range s {if !((r>='0'&&r<='9')||(r>='a'&&r<='f')){return false}}
    return true
}
