package main

import (
    "archive/tar"
    "bytes"
    "context"
    "crypto/sha1"
    "crypto/sha256"
    "encoding/binary"
    "encoding/hex"
    "encoding/json"
    "debug/buildinfo"
    "errors"
    "fmt"
    "io"
    "io/fs"
    "debug/pe"
    "net/http"
    "os"
    "os/exec"
    "os/signal"
    "path"
    "path/filepath"
    "reflect"
    "sort"
    "strconv"
    "strings"
    "time"
)

const repositoryID = "1406818713"
const jobKey = "guard-bootstrap"
const guardSource = "80c4ed5cbd864585f4894c6286d3d5c92194c1f5"
const guardTree = "84437789dfbe1bf1d56e4b96698b5c8da4a3dfb7"
const sourceLimit = 8 << 20
const totalLimit = 64 << 20

var errCapture = errors.New("guard_capture_unavailable")

type captureEnv struct {
    workspace, guardRoot, workflowSource, event string
    runID int64
    checkRunID int64
    attempt int
}

type sourceRecord struct {
    Path string `json:"path"`
    Size int64 `json:"size"`
    SHA string `json:"sha256"`
    Blob string `json:"git_blob"`
}

type runnerObservation struct {OS string `json:"os"`;Version string `json:"version"`;RunnerOS string `json:"runner_os"`;RunnerArch string `json:"runner_arch"`}

type captureCommand struct {
    Directory string `json:"directory"`
    Executable string `json:"executable"`
    Arguments []string `json:"arguments"`
}

type captureBefore struct {
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
    CompileCommand captureCommand `json:"compile_command"`
    RunnerImageSHA string `json:"runner_image_sha256"`
    RunnerImage runnerObservation `json:"runner_image"`
    BuildGraphSHA string `json:"build_graph_sha256"`
    CompileCommandSHA string `json:"compile_command_sha256"`
    Inputs []sourceRecord `json:"inputs"`
}

func lowerHex(s string,n int) bool {
    if len(s)!=n {return false}
    for _,r:=range s {if !((r>='0'&&r<='9')||(r>='a'&&r<='f')) {return false}}
    return true
}
func hashBytes(raw []byte) string {h:=sha256.Sum256(raw);return hex.EncodeToString(h[:])}

func parseCaptureEnvironment(get func(string)string) (captureEnv,error) {
    var result captureEnv
    if get("GITHUB_ACTIONS")!="true"||get("GITHUB_REPOSITORY_ID")!=repositoryID||get("GITHUB_JOB")!=jobKey||get("RUNNER_OS")!="Windows"||get("RUNNER_ARCH")!="X64" {return result,errCapture}
    event:=get("GITHUB_EVENT_NAME")
    if event!="workflow_dispatch"||get("GITHUB_REF")!="refs/heads/main"||get("RUNNER_ENVIRONMENT")!="github-hosted"||get("RUNNER_NAME")==""||get("RUNNER_TEMP")==""||!filepath.IsAbs(get("RUNNER_TEMP")) {return result,errCapture}
    source:=get("GITHUB_SHA")
    run,err:=strconv.ParseInt(get("GITHUB_RUN_ID"),10,64);if err!=nil||run<1{return result,errCapture}
    attempt,err:=strconv.Atoi(get("GITHUB_RUN_ATTEMPT"));if err!=nil||attempt<1{return result,errCapture}
    checkRun,err:=strconv.ParseInt(get("GUARD_CHECK_RUN_ID"),10,64);if err!=nil||checkRun<1{return result,errCapture}
    root:=get("GITHUB_WORKSPACE")
    if !lowerHex(source,40)||root==""||!filepath.IsAbs(root) {return result,errCapture}
    return captureEnv{workspace:filepath.Join(filepath.Clean(root),"producer"),guardRoot:filepath.Join(filepath.Clean(root),"guard-source"),workflowSource:source,event:event,runID:run,checkRunID:checkRun,attempt:attempt},nil
}

func captureInventory(root string) ([]sourceRecord,error) {
    var result []sourceRecord
    seen:=map[string]bool{}
    total:=int64(0)
    for _,base:=range []string{"repair-guard","tools/civ6-ci-contract"} {
        rootPath:=filepath.Join(root,filepath.FromSlash(base))
        err:=filepath.WalkDir(rootPath,func(name string,d fs.DirEntry,walkErr error) error {
            if walkErr!=nil{return walkErr}
            if d.Type()&os.ModeSymlink!=0 {return errCapture}
            if d.IsDir(){return nil}
            if !d.Type().IsRegular(){return errCapture}
            relative,err:=filepath.Rel(root,name);if err!=nil{return err}
            relative=filepath.ToSlash(relative)
            if path.Clean(relative)!=relative||strings.HasPrefix(relative,"../")||strings.ContainsAny(relative,"\\:") {return errCapture}
            folded:=strings.ToLower(relative);if seen[folded]{return errCapture};seen[folded]=true
            file,err:=os.Open(name);if err!=nil{return err}
            info,statErr:=file.Stat();if statErr!=nil||!info.Mode().IsRegular()||info.Size()<0||info.Size()>sourceLimit {_=file.Close();return errCapture}
            raw,readErr:=io.ReadAll(io.LimitReader(file,sourceLimit+1));closeErr:=file.Close()
            if readErr!=nil||closeErr!=nil||int64(len(raw))!=info.Size() {return errCapture}
            total+=int64(len(raw));if total>totalLimit{return errCapture}
            result=append(result,sourceRecord{Path:relative,Size:int64(len(raw)),SHA:hashBytes(raw),Blob:gitBlob(raw)})
        })
        if err!=nil{return nil,err}
    }
    if len(result)<1||len(result)>4096{return nil,errCapture}
    sort.Slice(result,func(i,j int)bool{return result[i].Path<result[j].Path})
    return result,nil
}
func inventoryDigest(in []sourceRecord) string {raw,_:=json.Marshal(in);return hashBytes(raw)}

func writeExclusive(name string,raw []byte) error {
    f,err:=os.OpenFile(name,os.O_WRONLY|os.O_CREATE|os.O_EXCL,0600);if err!=nil{return err}
    n,writeErr:=f.Write(raw)
    if writeErr==nil&&n!=len(raw){writeErr=io.ErrShortWrite}
    if writeErr==nil{writeErr=f.Sync()}
    closeErr:=f.Close()
    return errors.Join(writeErr,closeErr)
}

func runFixed(ctx context.Context,dir,exe string,args ...string)([]byte,error) {
    cmd:=exec.CommandContext(ctx,exe,args...);cmd.Dir=dir
    cmd.Env=append(cmd.Env,"GIT_TERMINAL_PROMPT=0","GOOS=windows","GOARCH=amd64","GOPROXY=https://proxy.golang.org","GOSUMDB=sum.golang.org")
    if path:=os.Getenv("PATH");path!="" {cmd.Env=append(cmd.Env,"PATH="+path)}
    for _,key:=range []string{"SYSTEMROOT","WINDIR","TEMP","TMP","USERPROFILE","HOME","PROGRAMDATA","PROGRAMFILES","PROGRAMFILES(X86)","LOCALAPPDATA","APPDATA"} {if value:=os.Getenv(key);value!="" {cmd.Env=append(cmd.Env,key+"="+value)}}
    cmd.Env=append(cmd.Env,"GOWORK=off","GOTOOLCHAIN=local","GOFLAGS=","GOENV=off","CGO_ENABLED=0","GIT_CONFIG_NOSYSTEM=1","GIT_CONFIG_GLOBAL=NUL","GIT_NO_REPLACE_OBJECTS=1")
    cmd.WaitDelay=2*time.Second
    var out,diagnostic boundedOutput;out.limit=128<<20;diagnostic.limit=1<<20
    cmd.Stdout=&out;cmd.Stderr=&diagnostic
    err:=cmd.Run()
    clear(diagnostic.data)
    if err!=nil||out.overflow||diagnostic.overflow||ctx.Err()!=nil{clear(out.data);return nil,errCapture}
    return out.data,nil
}
type boundedOutput struct {data []byte;limit int;overflow bool}
func (o *boundedOutput) Write(p []byte)(int,error) {
    n:=len(p);remaining:=o.limit-len(o.data)
    if remaining<n{o.overflow=true;if remaining<0{remaining=0};p=p[:remaining]}
    o.data=append(o.data,p...)
    return n,nil
}

type finalManifest struct {
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
type sourceProof struct {
    Domain string `json:"domain"`
    Before captureBefore `json:"before"`
    After captureBefore `json:"after"`
    CompilerExit int `json:"compiler_exit"`
}

func before(ctx context.Context,e captureEnv,out io.Writer) error {
    first,err:=collectBefore(ctx,e);if err!=nil{return errCapture}
    token:=os.Getenv("GH_TOKEN");if token==""{return errCapture}
    client:=&http.Client{Timeout:20*time.Second,CheckRedirect:func(*http.Request,[]*http.Request)error{return errCapture}}
    jobID,err:=resolveJobID(ctx,e,token,client);if err!=nil{return errCapture}
    base:=filepath.Join(os.Getenv("RUNNER_TEMP"),"guard-gha-capture")
    if err=os.Mkdir(base,0700);err!=nil{return errCapture}
    runDir:=filepath.Join(base,strconv.FormatInt(e.runID,10));attemptDir:=filepath.Join(runDir,strconv.Itoa(e.attempt));dir:=filepath.Join(attemptDir,strconv.FormatInt(jobID,10))
    for _,name:=range []string{runDir,attemptDir,dir} {if err=os.Mkdir(name,0700);err!=nil{return errCapture}}
    beforeRaw,err:=json.Marshal(first);if err!=nil{return errCapture}
    if err=writeExclusive(filepath.Join(attemptDir,"source-before.json"),beforeRaw);err!=nil{return errCapture}
    tempOutput:=filepath.Join(attemptDir,"guard-build.exe");if _,err=os.Lstat(tempOutput);!errors.Is(err,os.ErrNotExist){return errCapture}
    command,err:=captureCommandFor(tempOutput);if err!=nil{return errCapture}
    canonicalCommand,err:=canonicalCaptureCommand(command,tempOutput);if err!=nil||!reflect.DeepEqual(canonicalCommand,first.CompileCommand){return errCapture}
    // Before/after source checks are observations, not proof of an immutable
    // toolchain or a substitute for the later attestation and review gates.
    buildOut,err:=runFixed(ctx,filepath.Join(e.guardRoot,command.Directory),command.Executable,command.Arguments...);clear(buildOut);if err!=nil{return errCapture}
    file,err:=os.Open(tempOutput);if err!=nil{return errCapture}
    info,statErr:=file.Stat();if statErr!=nil||!info.Mode().IsRegular()||info.Size()<1||info.Size()>32<<20 {_=file.Close();return errCapture}
    raw,readErr:=io.ReadAll(io.LimitReader(file,32<<20+1));if readErr!=nil||int64(len(raw))!=info.Size()||!validGuardPE(raw){_=file.Close();clear(raw);return errCapture}
    peFile,peErr:=pe.NewFile(bytes.NewReader(raw));if peErr!=nil||peFile.FileHeader.Machine!=pe.IMAGE_FILE_MACHINE_AMD64 {clear(raw);_=file.Close();return errCapture};_ = peFile.Close()
    build,buildErr:=buildinfo.Read(bytes.NewReader(raw));if buildErr!=nil||build.Path!="github.com/AutoDevJapan/cc-pc-tools-rs/repair-guard" {clear(raw);_=file.Close();return errCapture}
    settings:=map[string]string{};for _,v:=range build.Settings {if _,exists:=settings[v.Key];exists {clear(raw);_=file.Close();return errCapture};settings[v.Key]=v.Value}
    if settings["vcs.revision"]!=guardSource||settings["vcs.modified"]!="false"||settings["GOOS"]!="windows"||settings["GOARCH"]!="amd64"||settings["-trimpath"]!="true" {clear(raw);_=file.Close();return errCapture}
    outputSHA:=hashBytes(raw);outputBytes:=int64(len(raw));clear(raw)
    second,err:=collectBefore(ctx,e);if err!=nil||!reflect.DeepEqual(first,second){_=file.Close();return errCapture}
    if _,err=file.Seek(0,io.SeekStart);err!=nil {_=file.Close();return errCapture}
    again,err:=io.ReadAll(io.LimitReader(file,32<<20+1));if err!=nil||int64(len(again))!=outputBytes||hashBytes(again)!=outputSHA {clear(again);_=file.Close();return errCapture};clear(again)
    current,pathErr:=os.Lstat(tempOutput);if pathErr!=nil||!current.Mode().IsRegular()||!os.SameFile(info,current) {_=file.Close();return errCapture}
    if _,err=file.Seek(0,io.SeekStart);err!=nil {_=file.Close();return errCapture}
    export,err:=io.ReadAll(io.LimitReader(file,32<<20+1));if err!=nil||int64(len(export))!=outputBytes||hashBytes(export)!=outputSHA {clear(export);_=file.Close();return errCapture}
    if err=writeExclusive(filepath.Join(dir,"guard.exe"),export);err!=nil {clear(export);_=file.Close();return errCapture}
    copied,copyErr:=os.ReadFile(filepath.Join(dir,"guard.exe"));if copyErr!=nil||int64(len(copied))!=outputBytes||hashBytes(copied)!=outputSHA {clear(copied);clear(export);_=file.Close();return errCapture};clear(copied);clear(export)
    if err=file.Close();err!=nil{return errCapture}
    afterJob,err:=resolveJobID(ctx,e,token,client);if err!=nil||afterJob!=jobID{return errCapture}
    afterInventory,err:=captureInventory(e.guardRoot);if err!=nil||!reflect.DeepEqual(first.Inputs,afterInventory){return errCapture}
    proof,err:=json.Marshal(sourceProof{Domain:"guard-gha-source-proof-v1",Before:first,After:second,CompilerExit:0});if err!=nil||len(proof)>8<<20{return errCapture}
    proofSHA:=hashBytes(proof);if err=writeExclusive(filepath.Join(dir,"source-proof.json"),proof);err!=nil{return errCapture}
    checkProof,err:=os.ReadFile(filepath.Join(dir,"source-proof.json"));if err!=nil||!bytes.Equal(checkProof,proof){return errCapture}
    manifest:=finalManifest{Domain:"guard-gha-capture-v1",RepositoryID:1406818713,Source:guardSource,SourceTree:guardTree,ProducerRevision:e.workflowSource,RunID:e.runID,RunAttempt:e.attempt,JobID:jobID,CheckRunID:e.checkRunID,SourceManifestSHA:proofSHA,RunnerToolchainSHA:first.RunnerToolchainObservationSHA,CompileCommandSHA:first.CompileCommandSHA,RunnerImageSHA:first.RunnerImageSHA,OutputName:"guard.exe",OutputBytes:outputBytes,OutputSHA:outputSHA}
    capture,err:=json.Marshal(manifest);if err!=nil||len(capture)>64<<10{return errCapture}
    if ctx.Err()!=nil{return errCapture}
    if err=writeExclusive(filepath.Join(dir,"capture.json"),capture);err!=nil{return errCapture}
    checkCapture,err:=os.ReadFile(filepath.Join(dir,"capture.json"));if err!=nil||!bytes.Equal(checkCapture,capture){return errCapture}
    artifactName:=fmt.Sprintf("guard-bootstrap-%d-%d-%d",e.runID,e.attempt,jobID)
    projection:=fmt.Sprintf("artifact-directory=%s\nartifact-name=%s\njob-id=%d\nguard-sha256=%s\ncapture-sha256=%s\nsource-manifest-sha256=%s\n",dir,artifactName,jobID,outputSHA,hashBytes(capture),proofSHA)
    if ctx.Err()!=nil{return errCapture}
    outputPath:=os.Getenv("GITHUB_OUTPUT");if outputPath==""||!filepath.IsAbs(outputPath)||strings.ContainsAny(dir,"\r\n") {return errCapture}
    outputFile,openErr:=os.OpenFile(outputPath,os.O_WRONLY|os.O_APPEND,0);if openErr!=nil{return errCapture}
    n,writeErr:=io.WriteString(outputFile,projection);closeErr:=outputFile.Close();if writeErr!=nil||closeErr!=nil||n!=len(projection){return errCapture}
    _,err=fmt.Fprintln(out,"guard capture completed");if err!=nil{return errCapture};return nil
}

func bytesTrimSpace(v []byte) []byte {return []byte(strings.TrimSpace(string(v)))}

func run(args []string,out io.Writer) int {
    if len(args)!=0 {return 2}
    env,err:=parseCaptureEnvironment(os.Getenv);if err!=nil{return 1}
    parent,stop:=signal.NotifyContext(context.Background(),os.Interrupt);defer stop()
    ctx,cancel:=context.WithTimeout(parent,10*time.Minute);defer cancel()
    if err=before(ctx,env,out);err!=nil{return 1}
    return 0
}
func main(){os.Exit(run(os.Args[1:],os.Stdout))}

func gitBlob(raw []byte) string {
    h:=sha1.New();_,_=fmt.Fprintf(h,"blob %d\x00",len(raw));_,_=h.Write(raw);return hex.EncodeToString(h.Sum(nil))
}

// Compare all selected Git tree entries and archive bytes with the inventory
// captured from the checked-out build inputs. No file names from the archive
// are written to disk. A successful comparison is only source evidence.
func verifySourceTree(records []sourceRecord,tree,archive []byte,revision string) error {
    if !lowerHex(revision,40)||len(records)==0||len(records)>4096||len(tree)>8<<20||len(archive)>128<<20{return errCapture}
    expected:=make(map[string]sourceRecord,len(records));for _,r:=range records {if expected[r.Path].Path!="" {return errCapture};expected[r.Path]=r}
    blobs:=make(map[string]string,len(records));folded:=map[string]bool{}
    if len(tree)==0||tree[len(tree)-1]!=0{return errCapture}
    for _,entry:=range bytes.Split(tree,[]byte{0}) {
        if len(entry)==0 {continue}
        pair:=bytes.SplitN(entry,[]byte{'\t'},2);if len(pair)!=2{return errCapture}
        fields:=strings.Fields(string(pair[0]));name:=string(pair[1])
        if len(fields)!=3||fields[0]!="100644"||fields[1]!="blob"||!lowerHex(fields[2],40)||expected[name].Path==""||expected[name].Blob!=fields[2]||folded[strings.ToLower(name)] {return errCapture}
        folded[strings.ToLower(name)]=true;blobs[name]=fields[2]
    }
    if len(blobs)!=len(expected){return errCapture}
    seen:=map[string]bool{};reader:=tar.NewReader(bytes.NewReader(archive))
    for {
        hdr,err:=reader.Next();if err==io.EOF{break};if err!=nil{return errCapture}
        if hdr.Typeflag==tar.TypeDir {
            dir:=strings.TrimSuffix(hdr.Name,"/")+"/";matched:=false;for name:=range expected {if strings.HasPrefix(name,dir){matched=true;break}};if !matched{return errCapture};continue
        }
        if hdr.Typeflag==tar.TypeXGlobalHeader {if len(hdr.PAXRecords)!=1||hdr.PAXRecords["comment"]!=revision{return errCapture};continue}
        record,ok:=expected[hdr.Name];if !ok||seen[hdr.Name]||hdr.Typeflag!=tar.TypeReg||hdr.Size<0||hdr.Size>sourceLimit{return errCapture}
        raw,err:=io.ReadAll(io.LimitReader(reader,sourceLimit+1));if err!=nil||int64(len(raw))!=hdr.Size||gitBlob(raw)!=blobs[hdr.Name] {return errCapture}
        if int64(len(raw))!=record.Size||hashBytes(raw)!=record.SHA {return errCapture}
        seen[hdr.Name]=true
    }
    if len(seen)!=len(expected){return errCapture};return nil
}

// resolveJobID uses the authenticated jobs API for this exact run attempt.
// The workflow supplies its own check_run_id; a job name/key alone is not ID.
func resolveJobID(ctx context.Context,e captureEnv,token string,client *http.Client) (int64,error) {
    if ctx==nil||ctx.Err()!=nil||token==""||client==nil||e.runID<1||e.attempt<1||e.checkRunID<1 {return 0,errCapture}
    endpoint:=fmt.Sprintf("https://api.github.com/repos/AutoDevJapan/cc-pc-tools-rs/actions/runs/%d/attempts/%d/jobs?per_page=100",e.runID,e.attempt)
    req,err:=http.NewRequestWithContext(ctx,http.MethodGet,endpoint,nil);if err!=nil{return 0,errCapture}
    req.Header.Set("Authorization","Bearer "+token);req.Header.Set("Accept","application/vnd.github+json");req.Header.Set("X-GitHub-Api-Version","2022-11-28")
    response,err:=client.Do(req);if err!=nil{return 0,errCapture}
    if response.StatusCode!=http.StatusOK||response.Header.Get("Link")!="" {_=response.Body.Close();return 0,errCapture}
    raw,err:=io.ReadAll(io.LimitReader(response.Body,2<<20+1));closeErr:=response.Body.Close();if err!=nil||closeErr!=nil||len(raw)>2<<20{return 0,errCapture};defer clear(raw)
    var envelope struct{Total int `json:"total_count"`;Jobs []struct{ID int64 `json:"id"`;RunID int64 `json:"run_id"`;HeadSHA string `json:"head_sha"`;Name string `json:"name"`;Status string `json:"status"`;CheckRunURL string `json:"check_run_url"`} `json:"jobs"`}
    dec:=json.NewDecoder(bytes.NewReader(raw));if dec.Decode(&envelope)!=nil||dec.Decode(new(any))!=io.EOF||envelope.Total!=len(envelope.Jobs)||len(envelope.Jobs)>100{return 0,errCapture}
    expected:=fmt.Sprintf("https://api.github.com/repos/AutoDevJapan/cc-pc-tools-rs/check-runs/%d",e.checkRunID)
    var id int64;matches:=0
    for _,job:=range envelope.Jobs {if job.RunID==e.runID&&job.HeadSHA==e.workflowSource&&job.Name=="Guard bootstrap artifact"&&job.Status=="in_progress"&&job.CheckRunURL==expected&&job.ID>0 {id=job.ID;matches++}}
    if matches!=1||ctx.Err()!=nil{return 0,errCapture};return id,nil
}

func captureCommandFor(output string) (captureCommand,error) {
    if output==""||!filepath.IsAbs(output){return captureCommand{},errCapture}
    return captureCommand{Directory:"repair-guard",Executable:"go",Arguments:[]string{"build","-mod=readonly","-pgo=off","-trimpath","-o",output,"."}},nil
}

func canonicalCaptureCommand(command captureCommand,output string) (captureCommand,error) {
    if command.Directory!="repair-guard"||command.Executable!="go"||len(command.Arguments)!=7||command.Arguments[0]!="build"||command.Arguments[1]!="-mod=readonly"||command.Arguments[2]!="-pgo=off"||command.Arguments[3]!="-trimpath"||command.Arguments[4]!="-o"||command.Arguments[5]!=output||command.Arguments[6]!="."||filepath.Base(output)!="guard-build.exe" {return captureCommand{},errCapture}
    result:=command;result.Arguments=append([]string(nil),command.Arguments...);result.Arguments[5]="<fixed-attempt-directory>/guard-build.exe";return result,nil
}

// The standard toolchain capture is bounded and returns a digest of explicit
// observations, not a claim that every toolchain DLL/cache byte is proven.
func captureBuildGraph(ctx context.Context,root string) ([]byte,error) {
    return runFixed(ctx,filepath.Join(root,"repair-guard"),"go","list","-deps","-json","-mod=readonly",".")
}

func validGuardPE(raw []byte) bool {
    if len(raw)<64||raw[0]!='M'||raw[1]!='Z'{return false}
    off:=uint64(binary.LittleEndian.Uint32(raw[0x3c:]));if off+6>uint64(len(raw)){return false}
    return bytes.Equal(raw[off:off+4],[]byte{'P','E',0,0})&&binary.LittleEndian.Uint16(raw[off+4:off+6])==0x8664
}

// Each phase is independently recaptured from the fixed checkout. Observed
// runner/toolchain values are evidence, not complete local execution authority.
func collectBefore(ctx context.Context,e captureEnv) (captureBefore,error) {
    var zero captureBefore
    producer,err:=runFixed(ctx,e.workspace,"git","rev-parse","HEAD");if err!=nil||strings.TrimSpace(string(producer))!=e.workflowSource{return zero,errCapture}
    head,err:=runFixed(ctx,e.guardRoot,"git","rev-parse","HEAD");if err!=nil||strings.TrimSpace(string(head))!=guardSource{return zero,errCapture}
    treeID,err:=runFixed(ctx,e.guardRoot,"git","rev-parse","HEAD^{tree}");if err!=nil||strings.TrimSpace(string(treeID))!=guardTree{return zero,errCapture}
    if _,err=runFixed(ctx,e.workspace,"git","merge-base","--is-ancestor",guardSource,e.workflowSource);err!=nil{return zero,errCapture}
    status,err:=runFixed(ctx,e.guardRoot,"git","status","--porcelain=v1","--untracked-files=all","--","repair-guard","tools/civ6-ci-contract");if err!=nil||len(bytesTrimSpace(status))!=0{return zero,errCapture}
    producerStatus,err:=runFixed(ctx,e.workspace,"git","status","--porcelain=v1","--untracked-files=all","--","tools/guard-gha-capture",".github/workflows/unattended-readiness.yml");if err!=nil||len(bytesTrimSpace(producerStatus))!=0{return zero,errCapture}
    inventory,err:=captureInventory(e.guardRoot);if err!=nil{return zero,err}
    tree,err:=runFixed(ctx,e.guardRoot,"git","ls-tree","-r","-z",guardSource,"--","repair-guard","tools/civ6-ci-contract");if err!=nil{return zero,errCapture}
    archive,err:=runFixed(ctx,e.guardRoot,"git","archive","--format=tar",guardSource,"--","repair-guard","tools/civ6-ci-contract");if err!=nil{return zero,errCapture}
    if verifySourceTree(inventory,tree,archive,guardSource)!=nil{return zero,errCapture}
    // Preliminary package enumeration may populate the hosted Go cache; its
    // JSON projection, rather than its volatile build paths, is compared.
    graph,err:=captureBuildGraph(ctx,e.guardRoot);if err!=nil{return zero,errCapture}
    graphCanonical,err:=canonicalGraph(graph,e.guardRoot);if err!=nil{return zero,errCapture}
    if err=checkGraphInputs(graphCanonical,inventory);err!=nil{return zero,errCapture}
    version,err:=runFixed(ctx,e.guardRoot,"go","version");if err!=nil{return zero,errCapture}
    goEnv,err:=runFixed(ctx,e.guardRoot,"go","env","-json","GOOS","GOARCH","GOVERSION","GOROOT","GOTOOLCHAIN","GOWORK","GOFLAGS","CGO_ENABLED","GOMODCACHE","GOCACHE","GOPROXY","GOSUMDB");if err!=nil{return zero,errCapture}
    var env map[string]string;if json.Unmarshal(goEnv,&env)!=nil||env["GOOS"]!="windows"||env["GOARCH"]!="amd64"||env["GOTOOLCHAIN"]!="local"||env["GOWORK"]!="off"||env["GOFLAGS"]!=""||env["CGO_ENABLED"]!="0" {return zero,errCapture}
    env["GOROOT"]="<runner-goroot>";env["GOMODCACHE"]="<runner-module-cache>";env["GOCACHE"]="<runner-build-cache>"
    command:=captureCommand{Directory:"repair-guard",Executable:"go",Arguments:[]string{"build","-mod=readonly","-pgo=off","-trimpath","-o","<fixed-attempt-directory>/guard-build.exe","."}};commandRaw,_:=json.Marshal(command)
    image:=runnerObservation{OS:os.Getenv("ImageOS"),Version:os.Getenv("ImageVersion"),RunnerOS:os.Getenv("RUNNER_OS"),RunnerArch:os.Getenv("RUNNER_ARCH")};if image.OS==""||image.Version=="" {return zero,errCapture};imageRaw,err:=json.Marshal(image);if err!=nil{return zero,errCapture}
    observation,err:=json.Marshal(struct{Version string `json:"version"`;Env map[string]string `json:"env"`}{strings.TrimSpace(string(version)),env});if err!=nil{return zero,errCapture}
    return captureBefore{Domain:"guard-gha-source-before-v1",RepositoryID:1406818713,Source:guardSource,SourceTree:guardTree,ProducerRevision:e.workflowSource,RunID:e.runID,RunAttempt:e.attempt,CheckRunID:e.checkRunID,Event:e.event,InventorySHA:inventoryDigest(inventory),BuildGraphSHA:hashBytes(graphCanonical),BuildGraph:graphCanonical,SourceTreeBefore:strings.TrimSpace(string(treeID)),GitTreeSHA:hashBytes(tree),GitArchiveSHA:hashBytes(archive),RunnerToolchainObservationSHA:hashBytes(observation),RunnerToolchainObservation:observation,RunnerImageSHA:hashBytes(imageRaw),RunnerImage:image,CompileCommandSHA:hashBytes(commandRaw),CompileCommand:command,Inputs:inventory},nil
}

type graphModule struct {Path,Version,Sum,GoModSum string;Main bool;Replace *graphModule}
type graphPackage struct {
    ImportPath string
    Standard bool
    Module *graphModule
    Error *json.RawMessage
    DepsErrors []json.RawMessage
    GoFiles,CgoFiles,CFiles,CXXFiles,HFiles,SFiles,SysoFiles,EmbedFiles []string
    Imports []string
}
func canonicalGraph(raw []byte,root string) ([]byte,error) {
    dec:=json.NewDecoder(bytes.NewReader(raw));var objects []graphPackage;seen:=map[string]bool{}
    for {var item graphPackage;err:=dec.Decode(&item);if err==io.EOF{break};if err!=nil||item.ImportPath==""||item.Error!=nil||len(item.DepsErrors)!=0||seen[item.ImportPath]{return nil,errCapture};seen[item.ImportPath]=true;objects=append(objects,item);if len(objects)>4096{return nil,errCapture}}
    if len(objects)==0||root==""{return nil,errCapture};sort.Slice(objects,func(i,j int)bool{return objects[i].ImportPath<objects[j].ImportPath})
    encoded,err:=json.Marshal(objects);if err!=nil||len(encoded)>8<<20{return nil,errCapture};return encoded,nil
}


func checkGraphInputs(raw []byte,records []sourceRecord) error {
    var packages []graphPackage;if json.Unmarshal(raw,&packages)!=nil{return errCapture}
    known:=map[string]bool{};for _,r:=range records {known[r.Path]=true}
    seenMain,seenReplace:=false,false
    for _,pkg:=range packages {
        prefix:=""
        switch {
        case pkg.ImportPath=="github.com/AutoDevJapan/cc-pc-tools-rs/repair-guard":prefix="repair-guard";seenMain=true
        case strings.HasPrefix(pkg.ImportPath,"github.com/AutoDevJapan/cc-pc-tools-rs/repair-guard/"):prefix="repair-guard/"+strings.TrimPrefix(pkg.ImportPath,"github.com/AutoDevJapan/cc-pc-tools-rs/repair-guard/")
        case pkg.ImportPath=="cc/civ6-ci-contract":prefix="tools/civ6-ci-contract";seenReplace=true
        case strings.HasPrefix(pkg.ImportPath,"cc/civ6-ci-contract/"):prefix="tools/civ6-ci-contract/"+strings.TrimPrefix(pkg.ImportPath,"cc/civ6-ci-contract/")
        }
        if prefix==""{continue}
        for _,names:=range [][]string{pkg.GoFiles,pkg.CgoFiles,pkg.CFiles,pkg.CXXFiles,pkg.HFiles,pkg.SFiles,pkg.SysoFiles,pkg.EmbedFiles} {for _,name:=range names {candidate:=path.Join(prefix,filepath.ToSlash(name));if !known[candidate] {return errCapture}}}
    }
    if !seenMain||!seenReplace||!known["repair-guard/go.mod"]||!known["repair-guard/go.sum"]||!known["repair-guard/owner_catalog.json"]||!known["tools/civ6-ci-contract/go.mod"]||!known["tools/civ6-ci-contract/go.sum"] {return errCapture};return nil
}
