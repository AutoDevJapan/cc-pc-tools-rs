//go:build windows

package main

import (
    "archive/zip"
    "bytes"
    "context"
    "crypto/sha1"
    "encoding/hex"
    "debug/buildinfo"
    "encoding/binary"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "strings"
    "sync"
)

const dsRunnerSource = "ea56c94fc2820dc349316d15bea38b7f1ae85595"
const dsRunnerSourceTree = "f9c9fce316d22f86517b7d766b7056ea0516fa29"
const dsRunnerModuleTree = "d0d012463d11faec2b57efffc80e8c13366dd3fd"
// The reviewed main commit that the producer revision of a capture must descend
// from. The producer files are checked at the producer revision itself (see
// dsRunnerProducerFiles), not at this base. The workflow and README pins name the
// seven-day retention text, which lands after this base, so the producer revision
// of a capture is a main commit that contains that change. The compare answer is
// read whole. Its size depends on how much content changed between the base and
// the producer revision, not on the number of commits (measurements: 38 commits and
// about 1.08 MB, then 18 commits and 423,765 bytes for the producer revision of
// generation 3), and the limit is 2 MiB, so move the base forward in a reviewed
// change before the answer nears the limit.
const dsRunnerApprovedProducer = "95c1004e0017cb9d8d3b16f93daa72706eaccca1"
const dsRunnerWorkflow = ".github/workflows/task-runner-capture.yml"
const dsRunnerModule = "github.com/AutoDevJapan/cc-pc-tools-rs/unattended-runner"
var errDSRunnerEvidence = errors.New("ds_runner_capture_unqualified")
var errDSRunnerCleanup = errors.New("ds_runner_cleanup_failed")

// Capture generations. A generation is one formal capture of the fixed runner source.
// Its hosted artifact has a fixed retention, so a generation is admitted for a limited time.
// Generation 1 expired at 2026-11-17T00:12:39Z and generation 2 at 2026-11-18T03:16:58Z.
// Both are retired: their tuples stay below as a record only, and every consumer rejects
// them. Generation 3 is the capture admitted now; the next capture becomes generation 4.
// The artifact of generation 3 is retained until 2026-11-27T03:34:21Z. After that every
// maintenance command fails closed until the reviewed pins of generation 4 replace it.
// Its producer revision descends from the approved producer, so a run passes the producer
// comparison and, while the other checks agree, reaches the run's artifact list: GitHub
// drops an expired artifact from that list, so the reason is then ds_runner_artifacts_absent,
// and a list that still shows it as expired gives ds_runner_artifacts_expired. A run without
// the owner credential, or whose owner check fails, stops earlier. Generation 2 stopped at the
// producer comparison instead (ds_runner_producer_compare_mismatch, before any request about
// the run or its artifacts): its producer revision is an ancestor of the approved producer,
// and any capture whose producer revision is not ahead of the approved producer stops there.
// These are expected capture bytes, not an issuer or an installed grant.
// The authenticated/cryptographic constructor still verifies all material.
// No CLI/JSON/env can replace this tuple or select another receipt.
const dsRunnerCaptureGeneration = 3
type dsRunnerCapturePin struct {
    Producer string
    WorkflowID, RunID, JobID, CheckRunID, ArtifactID int64
    Attempt int
    ArchiveSHA, OutputSHA, CaptureSHA, ProofSHA, BundleSHA, AttestationID string
}
type dsRunnerRetiredCapture struct {
    Generation int
    ExpiredAt string
    Pin dsRunnerCapturePin
}
var dsRunnerRetiredCaptures = []dsRunnerRetiredCapture{
    {Generation:1,ExpiredAt:"2026-11-17T00:12:39Z",Pin:dsRunnerCapturePin{
        Producer:"1d5c06f36e63f723ed0ad099efbbeff8f27b2b28",
        WorkflowID:419451709,RunID:43778902807,JobID:133011394642,CheckRunID:133011394642,ArtifactID:14187089175,Attempt:1,
        ArchiveSHA:"f831289951ee48c5c98fe1d7559d1c879bb2b0e6e6295bfb786890449e069a8d",
        OutputSHA:"3b64239e3c154187a1f81e2afbff70ce6ca041d7b54532d3f09a81f3983fad95",
        CaptureSHA:"7c0f585c3b5b3482e960914363d2407f23792cbfbe035b5be95a074ed3d841ed",
        ProofSHA:"550176dcbf25f5b407e1bf0b05068350fc25feba2ac1acdc734ae91fc0c5f17a",
        BundleSHA:"5192d66e050efdbf33078587b91fd69a3f07868ef8d63fc5cd8d77ff7d63e5f3",AttestationID:"67916323",
    }},
    {Generation:2,ExpiredAt:"2026-11-18T03:16:58Z",Pin:dsRunnerCapturePin{
        Producer:"800f4c82d68394c1d844a35ad3c1c49133c7bbe8",
        WorkflowID:419451709,RunID:44008886976,JobID:133790798206,CheckRunID:133790798206,ArtifactID:14296275534,Attempt:1,
        ArchiveSHA:"9a075932fd9197157bc0bede94704a93b0ac902dfbf596e7d81b4d14c2a477df",
        OutputSHA:"3b64239e3c154187a1f81e2afbff70ce6ca041d7b54532d3f09a81f3983fad95",
        CaptureSHA:"ad88a46f131a01f50671fa11100b784f63425f1b2ccbb02020ae4f9187a8647c",
        ProofSHA:"8b45c02298028a114876d34c2ef6d1f33f578dbead3f2cfa272d3ad1c7cb8a4c",
        BundleSHA:"38470f1a09aa3f33466554e6ae42c8a18eddbb79b3b01739187c472de50bfe64",AttestationID:"68439001",
    }},
}
var dsRunnerAdmittedCapture = dsRunnerCapturePin{
    Producer:"2afd2669e43d356d997e027ffdeb8a7d63d2b6b2",
    WorkflowID:419451709,RunID:44668468992,JobID:136063453021,CheckRunID:136063453021,ArtifactID:14598363233,Attempt:1,
    ArchiveSHA:"27b901cdf27d8a59f276524471f1a099a1ea345eb1e6f7a153f1676efc1dfa62",
    OutputSHA:"3b64239e3c154187a1f81e2afbff70ce6ca041d7b54532d3f09a81f3983fad95",
    CaptureSHA:"9e6c7edb820ccc0e67199e6a18ca143167a91b3b25cdd6679b70e9b751d6e6ed",
    ProofSHA:"572d33dbe62bc1d4e956d357d671366a614e69380743e14ca96d7c21f17bc0d0",
    BundleSHA:"28b23c7086e6cbd3b3a37b424ba0fe7c25efdf69fbb35607dc080100ce53e66f",AttestationID:"69973705",
}
// A pin is retired when it reuses any run-specific identity of a retired capture, even if its
// other fields were changed. The output digest is not compared: every generation so far built
// the same executable from the same fixed source, so a new capture may legitimately carry a
// retired output digest.
func dsRunnerPinRetired(p dsRunnerCapturePin) bool {
    for _,r:=range dsRunnerRetiredCaptures{
        q:=r.Pin
        if p.RunID==q.RunID||p.JobID==q.JobID||p.CheckRunID==q.CheckRunID||p.ArtifactID==q.ArtifactID||p.ArchiveSHA==q.ArchiveSHA||p.AttestationID==q.AttestationID{return true}
    }
    return false
}
func dsRunnerPinFormed(p dsRunnerCapturePin) bool {
    if !ghaGuardHex(p.Producer,40)||p.WorkflowID<1||p.RunID<1||p.JobID<1||p.CheckRunID<1||p.ArtifactID<1||p.Attempt!=1||p.AttestationID==""||len(p.AttestationID)>128{return false}
    for _,v:=range []string{p.ArchiveSHA,p.OutputSHA,p.CaptureSHA,p.ProofSHA,p.BundleSHA}{if !ghaGuardHex(v,64){return false}}
    for _,r:=range p.AttestationID{if !(r>='0'&&r<='9'||r>='A'&&r<='Z'||r>='a'&&r<='z'||r=='_'||r=='-'){return false}}
    return true
}
func dsRunnerPinValid(p dsRunnerCapturePin) bool {return dsRunnerPinFormed(p)&&!dsRunnerPinRetired(p)}

type dsRunnerManifest struct {
    Domain string `json:"domain"`
    RepositoryID int64 `json:"repository_id"`
    Source string `json:"source"`
    SourceTree string `json:"source_tree"`
    ModuleTree string `json:"module_tree"`
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
    DataAccessGranted bool `json:"data_access_granted"`
    UpdatePerformed bool `json:"update_performed"`
    SendAllowed bool `json:"send_allowed"`
    SourceQuiescenceProved bool `json:"source_quiescence_proved"`
}
type dsRunnerSourceRecord struct {Path string `json:"path"`; Size int64 `json:"size"`; SHA string `json:"sha256"`; Blob string `json:"git_blob"`}
type dsRunnerInputPin struct{Path string; Size int64; Blob string}
type dsRunnerCommand struct{Directory string `json:"directory"`; Executable string `json:"executable"`; Arguments []string `json:"arguments"`}
type dsRunnerImage struct{OS string `json:"os"`; Version string `json:"version"`; RunnerOS string `json:"runner_os"`; RunnerArch string `json:"runner_arch"`}
type dsRunnerBefore struct {
    Domain string `json:"domain"`
    RepositoryID int64 `json:"repository_id"`
    Source string `json:"source"`
    RunID int64 `json:"run_id"`
    RunAttempt int `json:"run_attempt"`
    Event string `json:"event"`
    SourceTree string `json:"source_tree"`
    ModuleTree string `json:"module_tree"`
    ProducerRevision string `json:"producer_revision"`
    CheckRunID int64 `json:"check_run_id"`
    InventorySHA string `json:"inventory_sha256"`
    RunnerToolchainObservationSHA string `json:"runner_toolchain_observation_sha256"`
    RunnerToolchainObservation json.RawMessage `json:"runner_toolchain_observation"`
    BuildGraph json.RawMessage `json:"build_graph"`
    SourceTreeBefore string `json:"source_tree_before"`
    GitTreeSHA string `json:"git_tree_sha256"`
    GitArchiveSHA string `json:"git_archive_sha256"`
    CompileCommand dsRunnerCommand `json:"compile_command"`
    RunnerImageSHA string `json:"runner_image_sha256"`
    RunnerImage dsRunnerImage `json:"runner_image"`
    BuildGraphSHA string `json:"build_graph_sha256"`
    CompileCommandSHA string `json:"compile_command_sha256"`
    Inputs []dsRunnerSourceRecord `json:"inputs"`
}
type dsRunnerSourceProof struct{Domain string `json:"domain"`; Before json.RawMessage `json:"before"`; After json.RawMessage `json:"after"`; CompilerExit *int `json:"compiler_exit"`}
func dsRunnerCanonical(raw []byte,out any,limit int) error {
    if len(raw)==0||len(raw)>limit{return errDSRunnerEvidence}
    dec:=json.NewDecoder(bytes.NewReader(raw));dec.DisallowUnknownFields()
    if dec.Decode(out)!=nil||dec.Decode(new(any))!=io.EOF{return errDSRunnerEvidence}
    canonical,err:=json.Marshal(out);defer clear(canonical)
    if err!=nil||!bytes.Equal(canonical,raw){return errDSRunnerEvidence};return nil
}
func dsRunnerManifestMatches(m dsRunnerManifest,p dsRunnerCapturePin) bool {
    return dsRunnerPinValid(p)&&m.Domain=="task-runner-gha-capture-v1"&&m.RepositoryID==1406818713&&m.Source==dsRunnerSource&&m.SourceTree==dsRunnerSourceTree&&m.ModuleTree==dsRunnerModuleTree&&m.ProducerRevision==p.Producer&&m.RunID==p.RunID&&m.RunAttempt==p.Attempt&&m.JobID==p.JobID&&m.CheckRunID==p.CheckRunID&&m.OutputName=="unattended-runner.exe"&&m.OutputBytes>0&&m.OutputBytes<=32<<20&&m.OutputSHA==p.OutputSHA&&m.SourceManifestSHA==p.ProofSHA&&ghaGuardHex(m.RunnerToolchainSHA,64)&&ghaGuardHex(m.CompileCommandSHA,64)&&ghaGuardHex(m.RunnerImageSHA,64)&&!m.DataAccessGranted&&!m.UpdatePerformed&&!m.SendAllowed&&!m.SourceQuiescenceProved
}
func dsRunnerProofMatches(ctx context.Context,raw []byte,m dsRunnerManifest) error {
    var p dsRunnerSourceProof
    if ctx==nil||ctx.Err()!=nil||digest(raw)!=m.SourceManifestSHA||dsRunnerCanonical(raw,&p,8<<20)!=nil||p.Domain!="task-runner-gha-source-proof-v1"||p.CompilerExit==nil||*p.CompilerExit!=0||!bytes.Equal(p.Before,p.After){return errDSRunnerEvidence}
    var b dsRunnerBefore
    if dsRunnerCanonical(p.Before,&b,8<<20)!=nil||b.Domain!="task-runner-gha-source-before-v1"||b.RepositoryID!=m.RepositoryID||b.Source!=m.Source||b.SourceTree!=m.SourceTree||b.ModuleTree!=dsRunnerModuleTree||b.SourceTreeBefore!=m.SourceTree||b.ProducerRevision!=m.ProducerRevision||b.RunID!=m.RunID||b.RunAttempt!=1||b.CheckRunID!=m.CheckRunID||b.Event!="workflow_dispatch"||len(b.Inputs)!=len(dsRunnerInputPins){return errDSRunnerEvidence}
    for i,item:=range b.Inputs{pin:=dsRunnerInputPins[i];if item.Path!=pin.Path||item.Size!=pin.Size||item.Blob!=pin.Blob||!ghaGuardHex(item.SHA,64){return errDSRunnerEvidence}}
    encoded,_:=json.Marshal(b.Inputs);defer clear(encoded)
    command,_:=json.Marshal(b.CompileCommand);defer clear(command)
    image,_:=json.Marshal(b.RunnerImage);defer clear(image)
    expected:=dsRunnerCommand{Directory:"unattended-runner",Executable:"go",Arguments:[]string{"build","-mod=readonly","-pgo=off","-trimpath","-o","<fixed-attempt-directory>/runner-build.exe","."}}
    want,_:=json.Marshal(expected);defer clear(want)
    if !bytes.Equal(command,want)||digest(encoded)!=b.InventorySHA||digest(b.BuildGraph)!=b.BuildGraphSHA||digest(b.RunnerToolchainObservation)!=b.RunnerToolchainObservationSHA||digest(command)!=b.CompileCommandSHA||digest(image)!=b.RunnerImageSHA||b.CompileCommandSHA!=m.CompileCommandSHA||b.RunnerImageSHA!=m.RunnerImageSHA||b.RunnerToolchainObservationSHA!=m.RunnerToolchainSHA||!ghaGuardHex(b.GitTreeSHA,64)||!ghaGuardHex(b.GitArchiveSHA,64)||b.RunnerImage.OS==""||b.RunnerImage.Version==""||b.RunnerImage.RunnerOS!="Windows"||b.RunnerImage.RunnerArch!="X64"{return errDSRunnerEvidence}
    if dsRunnerGraphMatches(b.BuildGraph,b.Inputs)!=nil||ctx.Err()!=nil{return errDSRunnerEvidence};return nil
}
type dsRunnerGraphModule struct{Path,Version,Sum,GoModSum string;Main bool;Replace *dsRunnerGraphModule}
type dsRunnerGraphPackage struct{ImportPath string;Standard bool;Module *dsRunnerGraphModule;Error *json.RawMessage;DepsErrors []json.RawMessage;GoFiles,CgoFiles,CFiles,CXXFiles,HFiles,SFiles,SysoFiles,EmbedFiles []string;Imports []string}
func dsRunnerGraphMatches(raw []byte,inputs []dsRunnerSourceRecord) error {
    var packages []dsRunnerGraphPackage
    if dsRunnerCanonical(raw,&packages,8<<20)!=nil||len(packages)==0||len(packages)>4096{return errDSRunnerEvidence}
    known:=map[string]bool{};for _,v:=range inputs{known[v.Path]=true}
    prior:="";main:=0
    for _,v:=range packages{
        if v.ImportPath==""||v.ImportPath<=prior||v.Error!=nil||len(v.DepsErrors)!=0{return errDSRunnerEvidence};prior=v.ImportPath
        if v.Standard{continue};main++
        if v.ImportPath!=dsRunnerModule||v.Module==nil||!v.Module.Main||v.Module.Path!=dsRunnerModule||v.Module.Version!=""||v.Module.Replace!=nil||len(v.GoFiles)==0||len(v.CgoFiles)+len(v.CFiles)+len(v.CXXFiles)+len(v.HFiles)+len(v.SFiles)+len(v.SysoFiles)+len(v.EmbedFiles)!=0{return errDSRunnerEvidence}
        seen:=map[string]bool{};for _,name:=range v.GoFiles{if name==""||strings.ContainsAny(name,"/\\:")||!strings.HasSuffix(name,".go")||strings.HasSuffix(name,"_test.go")||seen[strings.ToLower(name)]||!known["unattended-runner/"+name]{return errDSRunnerEvidence};seen[strings.ToLower(name)]=true}
    }
    if main!=1{return errDSRunnerEvidence};return nil
}
func dsRunnerPEMatches(raw []byte,m dsRunnerManifest) bool {
    if len(raw)<64||int64(len(raw))!=m.OutputBytes||digest(raw)!=m.OutputSHA||raw[0]!='M'||raw[1]!='Z'{return false}
    off:=uint64(binary.LittleEndian.Uint32(raw[0x3c:]));if off+6>uint64(len(raw))||!bytes.Equal(raw[off:off+4],[]byte{'P','E',0,0})||binary.LittleEndian.Uint16(raw[off+4:off+6])!=0x8664{return false}
    info,err:=buildinfo.Read(bytes.NewReader(raw));if err!=nil||info.Path!=dsRunnerModule{return false}
    settings:=map[string]string{};for _,v:=range info.Settings{if _,ok:=settings[v.Key];ok{return false};settings[v.Key]=v.Value}
    return settings["vcs"]=="git"&&settings["vcs.revision"]==dsRunnerSource&&settings["vcs.modified"]=="false"&&settings["GOOS"]=="windows"&&settings["GOARCH"]=="amd64"&&settings["-trimpath"]=="true"
}
type dsRunnerArchive struct{manifest dsRunnerManifest;payload,proof,capture []byte}
func(a *dsRunnerArchive)erase(){if a!=nil{clear(a.payload);clear(a.proof);clear(a.capture)}}
func dsRunnerReadArchive(ctx context.Context,raw []byte,p dsRunnerCapturePin)(result *dsRunnerArchive,err error){
    if ctx==nil||ctx.Err()!=nil||!dsRunnerPinValid(p)||len(raw)==0||len(raw)>64<<20||digest(raw)!=p.ArchiveSHA{return nil,errDSRunnerEvidence}
    z,e:=zip.NewReader(bytes.NewReader(raw),int64(len(raw)));if e!=nil||len(z.File)!=3{return nil,errDSRunnerEvidence}
    result=&dsRunnerArchive{};defer func(){if err!=nil{result.erase();result=nil}}()
    limits:=map[string]int64{"unattended-runner.exe":32<<20,"source-proof.json":8<<20,"capture.json":64<<10};seen:=map[string]bool{}
    for _,f:=range z.File{
        limit,ok:=limits[f.Name];if !ok||seen[f.Name]||f.Flags&1!=0||!f.Mode().IsRegular()||f.UncompressedSize64>uint64(limit)||f.CompressedSize64>64<<20{return result,errDSRunnerEvidence};seen[f.Name]=true
        reader,e:=f.Open();if e!=nil{return result,errDSRunnerEvidence};data,readErr:=io.ReadAll(io.LimitReader(reader,limit+1));closeErr:=reader.Close()
        if readErr!=nil||closeErr!=nil||int64(len(data))>limit||uint64(len(data))!=f.UncompressedSize64{clear(data);return result,errors.Join(errDSRunnerEvidence,closeErr)}
        switch f.Name{case "unattended-runner.exe":result.payload=data;case "source-proof.json":result.proof=data;case "capture.json":result.capture=data}
    }
    if digest(result.capture)!=p.CaptureSHA||digest(result.proof)!=p.ProofSHA||digest(result.payload)!=p.OutputSHA||dsRunnerCanonical(result.capture,&result.manifest,64<<10)!=nil||!dsRunnerManifestMatches(result.manifest,p)||dsRunnerProofMatches(ctx,result.proof,result.manifest)!=nil||!dsRunnerPEMatches(result.payload,result.manifest)||ctx.Err()!=nil{return result,errDSRunnerEvidence}
    return result,nil
}

// Only the authenticated/cryptographic constructor makes a self-bound issuer.
// A parsed report, pin, path or matching hash cannot construct this capability.
type dsRunnerIssuer struct{token byte}
type dsRunnerCandidate struct{self *dsRunnerCandidate;mu sync.Mutex;issuer *dsRunnerIssuer;pin dsRunnerCapturePin;raw []byte;active,closed bool;done chan struct{}}
func(c *dsRunnerCandidate)with(ctx context.Context,use func([]byte)error)(err error){
    if c==nil||ctx==nil||use==nil{return errDSRunnerEvidence};c.mu.Lock()
    if c.self!=c||c.closed||c.active||c.issuer==nil||ctx.Err()!=nil||!dsRunnerPinValid(c.pin)||digest(c.raw)!=c.pin.OutputSHA{c.mu.Unlock();return errDSRunnerEvidence}
    c.active=true;raw:=c.raw;pin:=c.pin;c.mu.Unlock();returned:=false
    defer func(){
        c.mu.Lock();defer c.mu.Unlock();c.active=false
        if !returned||err!=nil||ctx.Err()!=nil||c.pin!=pin||digest(c.raw)!=pin.OutputSHA{c.closed=true;c.issuer=nil}
        if c.closed{clear(c.raw);c.raw=nil};if c.done!=nil{close(c.done);c.done=nil}
    }()
    err=use(raw);if err!=nil{return err}
    c.mu.Lock();valid:=!c.closed&&c.issuer!=nil&&c.pin==pin&&digest(c.raw)==pin.OutputSHA;c.mu.Unlock()
    if !valid||ctx.Err()!=nil{return errDSRunnerEvidence};returned=true;return nil
}
func(c *dsRunnerCandidate)Close()error{if c==nil{return nil};c.mu.Lock();defer c.mu.Unlock();if c.self!=c{return errDSRunnerEvidence};c.closed=true;c.issuer=nil;if !c.active{clear(c.raw);c.raw=nil};return nil}
func(c *dsRunnerCandidate)waitClosed(ctx context.Context)error{if c==nil{return nil};if ctx==nil{return errDSRunnerCleanup};c.mu.Lock();if c.self!=c||!c.closed{c.mu.Unlock();return errDSRunnerEvidence};if !c.active{c.mu.Unlock();return nil};if c.done==nil{c.done=make(chan struct{})};done:=c.done;c.mu.Unlock();select{case<-done:return nil;case<-ctx.Done():return errDSRunnerCleanup}}
func dsRunnerArtifactName(p dsRunnerCapturePin)string{return fmt.Sprintf("task-runner-software-%d-%d-%d",p.RunID,p.Attempt,p.JobID)}

func dsRunnerGitBlob(raw []byte)string{h:=sha1.New();_,_=fmt.Fprintf(h,"blob %d\x00",len(raw));_,_=h.Write(raw);return hex.EncodeToString(h.Sum(nil))}
