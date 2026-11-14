//go:build windows

package main

import (
    "archive/zip"
    "bytes"
    "context"
    "encoding/binary"
    "encoding/hex"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "net/http"
    "strings"
    "time"

    "github.com/sigstore/sigstore-go/pkg/bundle"
    "github.com/sigstore/sigstore-go/pkg/fulcio/certificate"
    "github.com/sigstore/sigstore-go/pkg/root"
    "github.com/sigstore/sigstore-go/pkg/tuf"
    "github.com/sigstore/sigstore-go/pkg/verify"
    "github.com/theupdateframework/go-tuf/v2/metadata/fetcher"
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
    if ghaGuardValidateSourceProof(ctx,proof,m)!=nil {clear(proof);return fail()};clear(proof)
    payload,err:=read(entries["guard.exe"],ghaGuardPayloadLimit);if err!=nil{return fail()}
    if ctx.Err()!=nil||int64(len(payload))!=m.OutputBytes||digest(payload)!=m.OutputSHA||!ghaGuardPEHeader(payload) {clear(payload);return fail()}
    return payload,nil
}

// These structures describe the signed producer observation. They do not
// establish compiler or runner authority; caller must verify Sigstore and
// authenticate the server/run binding before issuing any candidate.
type ghaGuardSourceRecord struct {
    Path string `json:"path"`
    Size int64 `json:"size"`
    SHA string `json:"sha256"`
    Blob string `json:"git_blob"`
}
type ghaGuardProofSnapshot struct {
    Domain string `json:"domain"`
    RepositoryID int64 `json:"repository_id"`
    Source string `json:"source"`
    RunID int64 `json:"run_id"`
    RunAttempt int `json:"run_attempt"`
    Event string `json:"event"`
    SourceTree string `json:"source_tree"`
    ProducerRevision string `json:"producer_revision"`
    CheckRunID int64 `json:"check_run_id"`
    InventorySHA string `json:"inventory_sha256"`
    RunnerToolchainObservationSHA string `json:"runner_toolchain_observation_sha256"`
    RunnerToolchainObservation json.RawMessage `json:"runner_toolchain_observation"`
    BuildGraph json.RawMessage `json:"build_graph"`
    SourceTreeBefore string `json:"source_tree_before"`
    GitTreeSHA string `json:"git_tree_sha256"`
    GitArchiveSHA string `json:"git_archive_sha256"`
    CompileCommand json.RawMessage `json:"compile_command"`
    RunnerImageSHA string `json:"runner_image_sha256"`
    RunnerImage json.RawMessage `json:"runner_image"`
    BuildGraphSHA string `json:"build_graph_sha256"`
    CompileCommandSHA string `json:"compile_command_sha256"`
    Inputs []ghaGuardSourceRecord `json:"inputs"`
}
type ghaGuardSourceProof struct {
    Domain string `json:"domain"`
    Before json.RawMessage `json:"before"`
    After json.RawMessage `json:"after"`
    CompilerExit *int `json:"compiler_exit"`
}

func ghaGuardValidInputPath(path string) bool {
    if path==""||strings.Contains(path,"\\")||strings.ContainsRune(path,0) {return false}
    if !strings.HasPrefix(path,"repair-guard/")&&!strings.HasPrefix(path,"tools/civ6-ci-contract/"){return false}
    for _,part:=range strings.Split(path,"/"){if part==""||part=="."||part==".."{return false}}
    return true
}

func ghaGuardValidateSourceProof(ctx context.Context,raw []byte,m ghaGuardManifest) error {
    if ctx==nil||ctx.Err()!=nil||len(raw)==0||len(raw)>8<<20||!ghaGuardHex(m.SourceManifestSHA,64)||digest(raw)!=m.SourceManifestSHA{return errGHAGuardUnqualified}
    var proof ghaGuardSourceProof
    dec:=json.NewDecoder(bytes.NewReader(raw));dec.DisallowUnknownFields()
    if dec.Decode(&proof)!=nil||dec.Decode(new(any))!=io.EOF||proof.Domain!="guard-gha-source-proof-v1"||proof.CompilerExit==nil||*proof.CompilerExit!=0||len(proof.Before)==0||!bytes.Equal(proof.Before,proof.After){return errGHAGuardUnqualified}
    canonical,err:=json.Marshal(proof)
    if err!=nil||!bytes.Equal(canonical,raw){return errGHAGuardUnqualified}
    var snapshot ghaGuardProofSnapshot
    dec=json.NewDecoder(bytes.NewReader(proof.Before));dec.DisallowUnknownFields()
    if dec.Decode(&snapshot)!=nil||dec.Decode(new(any))!=io.EOF{return errGHAGuardUnqualified}
    canonical,err=json.Marshal(snapshot)
    if err!=nil||!bytes.Equal(canonical,proof.Before){return errGHAGuardUnqualified}
    if snapshot.Domain!="guard-gha-source-before-v1"||snapshot.RepositoryID!=m.RepositoryID||snapshot.Source!=m.Source||snapshot.SourceTree!=m.SourceTree||snapshot.SourceTreeBefore!=m.SourceTree||snapshot.ProducerRevision!=m.ProducerRevision||snapshot.RunID!=m.RunID||snapshot.RunAttempt!=m.RunAttempt||snapshot.CheckRunID!=m.CheckRunID||snapshot.Event!="workflow_dispatch"{return errGHAGuardUnqualified}
    if !ghaGuardHex(snapshot.GitTreeSHA,64)||!ghaGuardHex(snapshot.GitArchiveSHA,64)||!ghaGuardHex(snapshot.InventorySHA,64)||!ghaGuardHex(snapshot.BuildGraphSHA,64)||!ghaGuardHex(snapshot.RunnerToolchainObservationSHA,64)||!ghaGuardHex(snapshot.RunnerImageSHA,64)||!ghaGuardHex(snapshot.CompileCommandSHA,64){return errGHAGuardUnqualified}
    if len(snapshot.Inputs)<1||len(snapshot.Inputs)>4096||len(snapshot.BuildGraph)==0||snapshot.BuildGraph[0]!='['||len(snapshot.RunnerToolchainObservation)==0||snapshot.RunnerToolchainObservation[0]!='{'||len(snapshot.RunnerImage)==0||snapshot.RunnerImage[0]!='{'{return errGHAGuardUnqualified}
    var total int64
    prior:=""
    for _,item:=range snapshot.Inputs {
        if ctx.Err()!=nil||!ghaGuardValidInputPath(item.Path)||item.Path<=prior||item.Size<0||item.Size>8<<20||!ghaGuardHex(item.SHA,64)||!ghaGuardHex(item.Blob,40){return errGHAGuardUnqualified}
        total+=item.Size;if total>64<<20{return errGHAGuardUnqualified};prior=item.Path
    }
    inventory,err:=json.Marshal(snapshot.Inputs);if err!=nil||digest(inventory)!=snapshot.InventorySHA{return errGHAGuardUnqualified}
    if digest(snapshot.BuildGraph)!=snapshot.BuildGraphSHA||digest(snapshot.RunnerToolchainObservation)!=snapshot.RunnerToolchainObservationSHA||digest(snapshot.RunnerImage)!=snapshot.RunnerImageSHA||digest(snapshot.CompileCommand)!=snapshot.CompileCommandSHA||snapshot.RunnerToolchainObservationSHA!=m.RunnerToolchainSHA||snapshot.RunnerImageSHA!=m.RunnerImageSHA||snapshot.CompileCommandSHA!=m.CompileCommandSHA{return errGHAGuardUnqualified}
    expectedCommand:=[]byte(`{"directory":"repair-guard","executable":"go","arguments":["build","-mod=readonly","-pgo=off","-trimpath","-o","\u003cfixed-attempt-directory\u003e/guard-build.exe","."]}`)
    if !bytes.Equal(snapshot.CompileCommand,expectedCommand)||ctx.Err()!=nil{return errGHAGuardUnqualified}
    return nil
}

// The Sigstore check is a data-verification primitive, not an authority
// issuer. The production acquisition still must independently authenticate
// the exact GitHub run/job/upload binding before constructing any candidate.
func ghaGuardVerifySignatureWithTrusted(ctx context.Context,trusted root.TrustedMaterial,rawBundle,payload []byte,m ghaGuardManifest) error {
    if ctx==nil||ctx.Err()!=nil||len(rawBundle)==0||len(rawBundle)>4<<20||len(payload)!=14299136||digest(payload)!="a61e25065ae35d51f1ac8133959176b21e2c3740c1516b76f799371f1c27f9a2"||m.OutputSHA!=digest(payload)||m.SourceManifestSHA!="aeeefe291c6a10a0f8d1bf1ee1a0952daf29dba87046596980c7c509c690f8c8"||m.RepositoryID!=1406818713||m.Source!=ghaGuardSource||m.SourceTree!=ghaGuardSourceTree||m.ProducerRevision!="f2f9e4c4cddba70240004e7084443c27c51baee9"||m.RunID!=43574333045||m.RunAttempt!=1||m.JobID!=132385166063||m.CheckRunID!=132385166063{return errGHAGuardUnqualified}
    if trusted==nil||ctx.Err()!=nil{return errGHAGuardUnqualified}
    var signed bundle.Bundle
    var err error
    if err=signed.UnmarshalJSON(rawBundle);err!=nil||signed.Bundle==nil||signed.Bundle.GetDsseEnvelope()==nil{return errGHAGuardUnqualified}
    sanValue:="https://github.com/AutoDevJapan/cc-pc-tools-rs/.github/workflows/unattended-readiness.yml@refs/heads/main"
    san,err:=verify.NewSANMatcher(sanValue,"");if err!=nil{return errGHAGuardUnqualified}
    issuer,err:=verify.NewIssuerMatcher("https://token.actions.githubusercontent.com","");if err!=nil{return errGHAGuardUnqualified}
    extensions:=certificate.Extensions{BuildSignerURI:sanValue,BuildSignerDigest:m.ProducerRevision,BuildConfigURI:sanValue,BuildConfigDigest:m.ProducerRevision,RunnerEnvironment:"github-hosted",SourceRepositoryURI:"https://github.com/AutoDevJapan/cc-pc-tools-rs",SourceRepositoryIdentifier:"1406818713",SourceRepositoryDigest:m.ProducerRevision,SourceRepositoryRef:"refs/heads/main",RunInvocationURI:"https://github.com/AutoDevJapan/cc-pc-tools-rs/actions/runs/43574333045/attempts/1",BuildTrigger:"workflow_dispatch",SourceRepositoryVisibilityAtSigning:"public"}
    identity,err:=verify.NewCertificateIdentity(san,issuer,extensions);if err!=nil{return errGHAGuardUnqualified}
    verifier,err:=verify.NewVerifier(trusted,verify.WithSignedCertificateTimestamps(1),verify.WithTransparencyLog(1),verify.WithObserverTimestamps(1));if err!=nil{return errGHAGuardUnqualified}
    for _,expected:=range []string{m.OutputSHA,m.SourceManifestSHA}{
        d,err:=hex.DecodeString(expected);if err!=nil{return errGHAGuardUnqualified}
        result,err:=verifier.Verify(&signed,verify.NewPolicy(verify.WithArtifactDigest("sha256",d),verify.WithCertificateIdentity(identity)))
        if err!=nil||result==nil||result.Statement==nil||result.Signature==nil||result.Signature.Certificate==nil||result.Statement.GetType()!="https://in-toto.io/Statement/v1"||result.Statement.GetPredicateType()!="https://slsa.dev/provenance/v1"||ctx.Err()!=nil{return errGHAGuardUnqualified}
        subjects:=result.Statement.GetSubject()
        if len(subjects)!=2||subjects[0]==nil||subjects[1]==nil||subjects[0].GetName()!="guard.exe"||subjects[0].GetDigest()["sha256"]!=m.OutputSHA||subjects[1].GetName()!="source-proof.json"||subjects[1].GetDigest()["sha256"]!=m.SourceManifestSHA{return errGHAGuardUnqualified}
    }
    return nil
}

type ghaGuardTUFContextClient struct {
    ctx context.Context
    client *http.Client
}

func (c *ghaGuardTUFContextClient) Do(req *http.Request) (*http.Response,error) {
    if c==nil||c.ctx==nil||c.ctx.Err()!=nil||c.client==nil||req==nil{return nil,errGHAGuardUnqualified}
    return c.client.Do(req.Clone(c.ctx))
}

func ghaGuardNewTUFRequestFetcher(ctx context.Context,client *http.Client) *fetcher.DefaultFetcher {
    f:=fetcher.NewDefaultFetcher()
    f.SetHTTPClient(&ghaGuardTUFContextClient{ctx:ctx,client:client})
    return f
}

func ghaGuardVerifyActualSignature(ctx context.Context,rawBundle,payload []byte,m ghaGuardManifest) error {
    if ctx==nil||ctx.Err()!=nil{return errGHAGuardUnqualified}
    opts:=tuf.DefaultOptions().WithDisableLocalCache().WithContext(ctx).WithFetcher(ghaGuardNewTUFRequestFetcher(ctx,&http.Client{}))
    trusted,err:=root.FetchTrustedRootWithOptions(opts)
    if err!=nil||trusted==nil||ctx.Err()!=nil{return errGHAGuardUnqualified}
    return ghaGuardVerifySignatureWithTrusted(ctx,trusted,rawBundle,payload,m)
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
