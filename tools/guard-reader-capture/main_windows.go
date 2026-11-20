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
    "runtime"
    "runtime/debug"
    "sort"
    "strconv"
    "strings"
    "time"
)

const repositoryID = "1406818713"
const jobKey = "guard-reader-capture"
const jobName = "Guard reader software artifact"

// Source pin 0 (rehearsal): the reader module as it exists on main 95c1004e.
// A later pin replaces only the next four values, the two pin constants below,
// the checkout ref, the source and pin literals of the publication record in the
// workflow, and the README paragraph; the module path, directory and limits stay
// fixed. A source pin names a version of the fixed source. It is not the
// generation of any consumer's formal capture.
const readerSource = "95c1004e0017cb9d8d3b16f93daa72706eaccca1"
const readerTree = "047674bc84f483d12702ebd0d3332f45e2984390"
const readerModuleTree = "4f188fbc69396af166f06a74af03df9d502afc3f"
const readerInputCount = 111

// Both values are written into capture.json, source-proof.json and the
// publication record. They are constants of the reviewed source and no input
// can change them. Pin 0 is a rehearsal whose result is never the reader.
const sourcePin = 0
const sourcePinRehearsal = true

const readerModulePath = "github.com/AutoDevJapan/cc-pc-tools-rs/tools/guard-refresh-software-reader"
const readerModuleDirectory = "tools/guard-refresh-software-reader"
const executableName = "guard-refresh-software-reader.exe"
const executableLimit = 96 << 20
const sourceLimit = 8 << 20
const totalLimit = 64 << 20

var errCapture = errors.New("guard_reader_capture_unavailable")

// fail marks the call site so a hosted failure can be located without
// publishing tool output, paths or response bodies.
func fail() error {
    _,_,line,ok:=runtime.Caller(1)
    if !ok{return errCapture}
    return fmt.Errorf("%w@%d",errCapture,line)
}

// inner keeps the call site recorded by a deeper check; any other error
// collapses to the fixed sentinel.
func inner(err error) error {
    if errors.Is(err,errCapture){return err}
    return errCapture
}

// failureTail holds a bounded, printable remark about the last failed child
// command. The child streams themselves are cleared; the hosted inputs are
// public source and module names, and the runner masks its own secrets.
var failureTail string

func printable(raw []byte) string {
    out:=make([]byte,len(raw))
    for i,b:=range raw {
        if b=='\n'||(b>=0x20&&b<0x7f){out[i]=b}else{out[i]='?'}
    }
    return string(out)
}

func tailOf(err error,diagnostic []byte) string {
    note:=""
    if err!=nil {
        raw:=[]byte(err.Error())
        if len(raw)>200{raw=raw[:200]}
        note=printable(raw)
    }
    if len(diagnostic)>1024{diagnostic=diagnostic[len(diagnostic)-1024:]}
    return strings.TrimSpace(note+"\n"+printable(diagnostic))
}

// A hosted runner may treat a line of step output that starts with its command
// marker (possibly after blanks) as a workflow command. Whether it does so for
// an error stream was not measured; every printed line of the tail therefore
// gets a fixed prefix, so no line of child output can start such a command.
const tailLinePrefix = "tail| "

func prefixedTail(tail string) string {
    lines:=strings.Split(tail,"\n")
    for i,line:=range lines{lines[i]=tailLinePrefix+line}
    return strings.Join(lines,"\n")
}

// reportFailure is what the command prints on failure: the fixed marker and,
// for a failed child command, the prefixed tail.
func reportFailure(w io.Writer,err error) {
    fmt.Fprintln(w,err)
    if failureTail!=""{fmt.Fprintln(w,prefixedTail(failureTail))}
}

type captureEnv struct {
    workspace, readerRoot, workflowSource, event string
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
    if get("GITHUB_REPOSITORY")!="AutoDevJapan/cc-pc-tools-rs"||get("GITHUB_SERVER_URL")!="https://github.com"||get("GITHUB_API_URL")!="https://api.github.com"||get("GITHUB_WORKFLOW_REF")!="AutoDevJapan/cc-pc-tools-rs/.github/workflows/guard-reader-capture.yml@refs/heads/main"||get("GITHUB_ACTIONS")!="true"||get("GITHUB_REPOSITORY_ID")!=repositoryID||get("GITHUB_JOB")!=jobKey||get("RUNNER_OS")!="Windows"||get("RUNNER_ARCH")!="X64" {return result,fail()}
    event:=get("GITHUB_EVENT_NAME")
    if event!="workflow_dispatch"||get("GITHUB_REF")!="refs/heads/main"||get("RUNNER_ENVIRONMENT")!="github-hosted"||get("RUNNER_NAME")==""||get("RUNNER_TEMP")==""||!filepath.IsAbs(get("RUNNER_TEMP")) {return result,fail()}
    source:=get("GITHUB_SHA")
    run,err:=strconv.ParseInt(get("GITHUB_RUN_ID"),10,64);if err!=nil||run<1{return result,fail()}
    attempt,err:=strconv.Atoi(get("GITHUB_RUN_ATTEMPT"));if err!=nil||attempt!=1{return result,fail()}
    checkRun,err:=strconv.ParseInt(get("READER_CAPTURE_CHECK_RUN_ID"),10,64);if err!=nil||checkRun<1{return result,fail()}
    root:=get("GITHUB_WORKSPACE")
    if get("GITHUB_WORKFLOW_SHA")!=source||!lowerHex(source,40)||root==""||!filepath.IsAbs(root) {return result,fail()}
    return captureEnv{workspace:filepath.Join(filepath.Clean(root),"producer"),readerRoot:filepath.Join(filepath.Clean(root),"reader-source"),workflowSource:source,event:event,runID:run,checkRunID:checkRun,attempt:attempt},nil
}

func captureInventory(root string) ([]sourceRecord,error) {
    var result []sourceRecord
    seen:=map[string]bool{}
    total:=int64(0)
    for _,base:=range []string{readerModuleDirectory} {
        rootPath:=filepath.Join(root,filepath.FromSlash(base))
        err:=filepath.WalkDir(rootPath,func(name string,d fs.DirEntry,walkErr error) error {
            if walkErr!=nil{return walkErr}
            if d.Type()&os.ModeSymlink!=0 {return fail()}
            if d.IsDir(){return nil}
            if !d.Type().IsRegular(){return fail()}
            relative,err:=filepath.Rel(root,name);if err!=nil{return err}
            relative=filepath.ToSlash(relative)
            if path.Clean(relative)!=relative||strings.HasPrefix(relative,"../")||strings.ContainsAny(relative,"\\:") {return fail()}
            folded:=strings.ToLower(relative);if seen[folded]{return fail()};seen[folded]=true
            file,err:=os.Open(name);if err!=nil{return err}
            info,statErr:=file.Stat();if statErr!=nil||!info.Mode().IsRegular()||info.Size()<0||info.Size()>sourceLimit {_=file.Close();return fail()}
            raw,readErr:=io.ReadAll(io.LimitReader(file,sourceLimit+1));closeErr:=file.Close()
            if readErr!=nil||closeErr!=nil||int64(len(raw))!=info.Size() {return fail()}
            total+=int64(len(raw));if total>totalLimit{return fail()}
            result=append(result,sourceRecord{Path:relative,Size:int64(len(raw)),SHA:hashBytes(raw),Blob:gitBlob(raw)})
            return nil
        })
        if err!=nil{return nil,err}
    }
    if len(result)!=readerInputCount{return nil,fail()}
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

// childParentNames are the names, besides PATH, whose value a git or go child
// takes from the parent environment. Nothing else of the parent reaches a child:
// not the job token, not the OIDC request variables, not any GITHUB_ or ACTIONS_
// name, and not a Go or Git setting that the fixed values below replace.
var childParentNames = []string{"SYSTEMROOT","WINDIR","TEMP","TMP","USERPROFILE","HOME","PROGRAMDATA","PROGRAMFILES","PROGRAMFILES(X86)","LOCALAPPDATA","APPDATA"}

// childEnvironment is the whole environment of every git and go child. The
// parent is a lookup so that a test can run this same code against a parent
// that holds tokens.
func childEnvironment(parent func(string)string) []string {
    env:=[]string{"GIT_TERMINAL_PROMPT=0","GOOS=windows","GOARCH=amd64","GOPROXY=https://proxy.golang.org","GOSUMDB=sum.golang.org"}
    if value:=parent("PATH");value!="" {env=append(env,"PATH="+value)}
    for _,key:=range childParentNames {if value:=parent(key);value!="" {env=append(env,key+"="+value)}}
    return append(env,"GOWORK=off","GOTOOLCHAIN=local","GOFLAGS=","GOENV=off","CGO_ENABLED=0","GIT_CONFIG_NOSYSTEM=1","GIT_CONFIG_GLOBAL=NUL","GIT_NO_REPLACE_OBJECTS=1")
}

// fixedCommand is the only place that builds a git or go child command.
func fixedCommand(ctx context.Context,dir,exe string,args ...string) *exec.Cmd {
    cmd:=exec.CommandContext(ctx,exe,args...);cmd.Dir=dir
    cmd.Env=childEnvironment(os.Getenv)
    cmd.WaitDelay=2*time.Second
    return cmd
}

func runFixed(ctx context.Context,dir,exe string,args ...string)([]byte,error) {
    cmd:=fixedCommand(ctx,dir,exe,args...)
    var out,diagnostic boundedOutput;out.limit=128<<20;diagnostic.limit=1<<20
    cmd.Stdout=&out;cmd.Stderr=&diagnostic
    err:=cmd.Run()
    failed:=err!=nil||out.overflow||diagnostic.overflow||ctx.Err()!=nil
    if failed{failureTail=tailOf(err,diagnostic.data)}
    clear(diagnostic.data)
    if failed{clear(out.data);return nil,fail()}
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
    ModuleTree string `json:"module_tree"`
    SourcePin int `json:"source_pin"`
    SourcePinRehearsal bool `json:"source_pin_rehearsal"`
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
type sourceProof struct {
    Domain string `json:"domain"`
    SourcePin int `json:"source_pin"`
    SourcePinRehearsal bool `json:"source_pin_rehearsal"`
    Before captureBefore `json:"before"`
    After captureBefore `json:"after"`
    CompilerExit int `json:"compiler_exit"`
}

func newSourceProof(first,second captureBefore) sourceProof {
    return sourceProof{Domain:"guard-reader-gha-source-proof-v1",SourcePin:sourcePin,SourcePinRehearsal:sourcePinRehearsal,Before:first,After:second,CompilerExit:0}
}

func newFinalManifest(e captureEnv,jobID int64,first captureBefore,proofSHA,outputSHA string,outputBytes int64) finalManifest {
    return finalManifest{Domain:"guard-reader-gha-capture-v1",RepositoryID:1406818713,Source:readerSource,SourceTree:readerTree,ModuleTree:readerModuleTree,SourcePin:sourcePin,SourcePinRehearsal:sourcePinRehearsal,ProducerRevision:e.workflowSource,RunID:e.runID,RunAttempt:e.attempt,JobID:jobID,CheckRunID:e.checkRunID,SourceManifestSHA:proofSHA,RunnerToolchainSHA:first.RunnerToolchainObservationSHA,CompileCommandSHA:first.CompileCommandSHA,RunnerImageSHA:first.RunnerImageSHA,OutputName:executableName,OutputBytes:outputBytes,OutputSHA:outputSHA}
}

func before(ctx context.Context,e captureEnv,out io.Writer) error {
    token:=os.Getenv("GH_TOKEN");if token==""{return fail()}
    client:=&http.Client{Timeout:20*time.Second,CheckRedirect:func(*http.Request,[]*http.Request)error{return fail()}}
    jobID,err:=resolveJobID(ctx,e,token,client);if err!=nil{return inner(err)}
    first,err:=collectBefore(ctx,e);if err!=nil{return inner(err)}
    lock,err:=readLock(e.readerRoot,first.Inputs);if err!=nil{return inner(err)}
    modules,err:=checkGraphInputs(first.BuildGraph,first.Inputs,lock);if err!=nil{return inner(err)}
    base:=filepath.Join(os.Getenv("RUNNER_TEMP"),"guard-reader-capture")
    if err=os.Mkdir(base,0700);err!=nil{return fail()}
    runDir:=filepath.Join(base,strconv.FormatInt(e.runID,10));attemptDir:=filepath.Join(runDir,strconv.Itoa(e.attempt));dir:=filepath.Join(attemptDir,strconv.FormatInt(jobID,10))
    for _,name:=range []string{runDir,attemptDir,dir} {if err=os.Mkdir(name,0700);err!=nil{return fail()}}
    beforeRaw,err:=json.Marshal(first);if err!=nil{return fail()}
    if err=writeExclusive(filepath.Join(attemptDir,"source-before.json"),beforeRaw);err!=nil{return fail()}
    tempOutput:=filepath.Join(attemptDir,"reader-build.exe");if _,err=os.Lstat(tempOutput);!errors.Is(err,os.ErrNotExist){return fail()}
    command,err:=captureCommandFor(tempOutput);if err!=nil{return fail()}
    canonicalCommand,err:=canonicalCaptureCommand(command,tempOutput);if err!=nil||!reflect.DeepEqual(canonicalCommand,first.CompileCommand){return fail()}
    // Before/after source checks are observations, not proof of an immutable
    // toolchain or a substitute for the later attestation and review gates.
    buildOut,err:=runFixed(ctx,filepath.Join(e.readerRoot,filepath.FromSlash(command.Directory)),command.Executable,command.Arguments...);clear(buildOut);if err!=nil{return fail()}
    file,err:=os.Open(tempOutput);if err!=nil{return fail()}
    info,statErr:=file.Stat();if statErr!=nil||!info.Mode().IsRegular()||info.Size()<1||info.Size()>executableLimit {_=file.Close();return fail()}
    raw,readErr:=io.ReadAll(io.LimitReader(file,executableLimit+1));if readErr!=nil||int64(len(raw))!=info.Size()||!validReaderPE(raw){_=file.Close();clear(raw);return fail()}
    peFile,peErr:=pe.NewFile(bytes.NewReader(raw));if peErr!=nil||peFile.FileHeader.Machine!=pe.IMAGE_FILE_MACHINE_AMD64 {clear(raw);_=file.Close();return fail()};_ = peFile.Close()
    build,buildErr:=buildinfo.Read(bytes.NewReader(raw));if buildErr!=nil||build.Path!=readerModulePath {clear(raw);_=file.Close();return fail()}
    if err=checkBinaryDependencies(build.Deps,modules,lock);err!=nil {clear(raw);_=file.Close();return inner(err)}
    settings:=map[string]string{};for _,v:=range build.Settings {if _,exists:=settings[v.Key];exists {clear(raw);_=file.Close();return fail()};settings[v.Key]=v.Value}
    if settings["vcs.revision"]!=readerSource||settings["vcs.modified"]!="false"||settings["GOOS"]!="windows"||settings["GOARCH"]!="amd64"||settings["-trimpath"]!="true" {clear(raw);_=file.Close();return fail()}
    outputSHA:=hashBytes(raw);outputBytes:=int64(len(raw));clear(raw)
    second,err:=collectBefore(ctx,e);if err!=nil{_=file.Close();return inner(err)}
    if !reflect.DeepEqual(first,second){_=file.Close();return fail()}
    if _,err=file.Seek(0,io.SeekStart);err!=nil {_=file.Close();return fail()}
    again,err:=io.ReadAll(io.LimitReader(file,executableLimit+1));if err!=nil||int64(len(again))!=outputBytes||hashBytes(again)!=outputSHA {clear(again);_=file.Close();return fail()};clear(again)
    current,pathErr:=os.Lstat(tempOutput);if pathErr!=nil||!current.Mode().IsRegular()||!os.SameFile(info,current) {_=file.Close();return fail()}
    if _,err=file.Seek(0,io.SeekStart);err!=nil {_=file.Close();return fail()}
    export,err:=io.ReadAll(io.LimitReader(file,executableLimit+1));if err!=nil||int64(len(export))!=outputBytes||hashBytes(export)!=outputSHA {clear(export);_=file.Close();return fail()}
    if err=writeExclusive(filepath.Join(dir,executableName),export);err!=nil {clear(export);_=file.Close();return fail()}
    copied,copyErr:=os.ReadFile(filepath.Join(dir,executableName));if copyErr!=nil||int64(len(copied))!=outputBytes||hashBytes(copied)!=outputSHA {clear(copied);clear(export);_=file.Close();return fail()};clear(copied);clear(export)
    if err=file.Close();err!=nil{return fail()}
    afterJob,err:=resolveJobID(ctx,e,token,client);if err!=nil{return inner(err)}
    if afterJob!=jobID{return fail()}
    afterInventory,err:=captureInventory(e.readerRoot);if err!=nil{return inner(err)}
    if !reflect.DeepEqual(first.Inputs,afterInventory){return fail()}
    proof,err:=json.Marshal(newSourceProof(first,second));if err!=nil||len(proof)>8<<20{return fail()}
    proofSHA:=hashBytes(proof);if err=writeExclusive(filepath.Join(dir,"source-proof.json"),proof);err!=nil{return fail()}
    checkProof,err:=os.ReadFile(filepath.Join(dir,"source-proof.json"));if err!=nil||!bytes.Equal(checkProof,proof){return fail()}
    manifest:=newFinalManifest(e,jobID,first,proofSHA,outputSHA,outputBytes)
    capture,err:=json.Marshal(manifest);if err!=nil||len(capture)>64<<10{return fail()}
    if ctx.Err()!=nil{return fail()}
    if err=writeExclusive(filepath.Join(dir,"capture.json"),capture);err!=nil{return fail()}
    checkCapture,err:=os.ReadFile(filepath.Join(dir,"capture.json"));if err!=nil||!bytes.Equal(checkCapture,capture){return fail()}
    artifactName:=fmt.Sprintf("guard-reader-software-%d-%d-%d",e.runID,e.attempt,jobID)
    projection:=fmt.Sprintf("artifact-directory=%s\nartifact-name=%s\njob-id=%d\nreader-sha256=%s\ncapture-sha256=%s\nsource-manifest-sha256=%s\n",dir,artifactName,jobID,outputSHA,hashBytes(capture),proofSHA)
    if ctx.Err()!=nil{return fail()}
    outputPath:=os.Getenv("GITHUB_OUTPUT");if outputPath==""||!filepath.IsAbs(outputPath)||strings.ContainsAny(dir,"\r\n") {return fail()}
    outputFile,openErr:=os.OpenFile(outputPath,os.O_WRONLY|os.O_APPEND,0);if openErr!=nil{return fail()}
    n,writeErr:=io.WriteString(outputFile,projection);closeErr:=outputFile.Close();if writeErr!=nil||closeErr!=nil||n!=len(projection){return fail()}
    message:="guard reader capture completed\n"
    n,err=io.WriteString(out,message);if err!=nil||n!=len(message)||ctx.Err()!=nil{return fail()};return nil
}

func bytesTrimSpace(v []byte) []byte {return []byte(strings.TrimSpace(string(v)))}

func run(args []string,out io.Writer) int {
    if len(args)!=0 {return 2}
    env,err:=parseCaptureEnvironment(os.Getenv);if err!=nil{fmt.Fprintln(os.Stderr,err);return 1}
    parent,stop:=signal.NotifyContext(context.Background(),os.Interrupt);defer stop()
    ctx,cancel:=context.WithTimeout(parent,14*time.Minute);defer cancel()
    if err=before(ctx,env,out);err!=nil{reportFailure(os.Stderr,err);return 1}
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
    if !lowerHex(revision,40)||len(records)==0||len(records)>4096||len(tree)>8<<20||len(archive)>128<<20{return fail()}
    expected:=make(map[string]sourceRecord,len(records));for _,r:=range records {if expected[r.Path].Path!="" {return fail()};expected[r.Path]=r}
    blobs:=make(map[string]string,len(records));folded:=map[string]bool{}
    if len(tree)==0||tree[len(tree)-1]!=0{return fail()}
    for _,entry:=range bytes.Split(tree,[]byte{0}) {
        if len(entry)==0 {continue}
        pair:=bytes.SplitN(entry,[]byte{'\t'},2);if len(pair)!=2{return fail()}
        fields:=strings.Fields(string(pair[0]));name:=string(pair[1])
        if len(fields)!=3||fields[0]!="100644"||fields[1]!="blob"||!lowerHex(fields[2],40)||expected[name].Path==""||expected[name].Blob!=fields[2]||folded[strings.ToLower(name)] {return fail()}
        folded[strings.ToLower(name)]=true;blobs[name]=fields[2]
    }
    if len(blobs)!=len(expected){return fail()}
    seen:=map[string]bool{};reader:=tar.NewReader(bytes.NewReader(archive))
    for {
        hdr,err:=reader.Next();if err==io.EOF{break};if err!=nil{return fail()}
        if hdr.Typeflag==tar.TypeDir {
            dir:=strings.TrimSuffix(hdr.Name,"/")+"/";matched:=false;for name:=range expected {if strings.HasPrefix(name,dir){matched=true;break}};if !matched{return fail()};continue
        }
        if hdr.Typeflag==tar.TypeXGlobalHeader {if len(hdr.PAXRecords)!=1||hdr.PAXRecords["comment"]!=revision{return fail()};continue}
        record,ok:=expected[hdr.Name];if !ok||seen[hdr.Name]||hdr.Typeflag!=tar.TypeReg||hdr.Size<0||hdr.Size>sourceLimit{return fail()}
        raw,err:=io.ReadAll(io.LimitReader(reader,sourceLimit+1));if err!=nil||int64(len(raw))!=hdr.Size||gitBlob(raw)!=blobs[hdr.Name] {return fail()}
        if int64(len(raw))!=record.Size||hashBytes(raw)!=record.SHA {return fail()}
        seen[hdr.Name]=true
    }
    if len(seen)!=len(expected){return fail()};return nil
}

// This known API-version notice form does not imply another response page.
// Header names are case-insensitive; repeated fields, pagination and other
// link relations are not an authority to combine or select response pages.
func captureJobsSinglePageHeaders(headers http.Header) bool {
	links := 0
	for name, values := range headers {
		if !strings.EqualFold(name, "Link") {
			continue
		}
		links++
		if links != 1 || len(values) != 1 {
			return false
		}
		parts := strings.Split(values[0], ";")
		if len(parts) != 3 {
			return false
		}
		if strings.Trim(parts[0], " \t") != "<https://docs.github.com/en/rest/about-the-rest-api/api-versions>" ||
			strings.Trim(parts[1], " \t") != "rel=\"deprecation\"" ||
			strings.Trim(parts[2], " \t") != "type=\"text/html\"" {
			return false
		}
	}
	return true
}

type captureJobEntry struct {
	ID          int64
	RunID       int64
	HeadSHA     string
	Name        string
	Status      string
	CheckRunURL string
}

type captureJobsPage struct {
	Total int
	Jobs  []captureJobEntry
}

// Check critical names before projecting them into typed values. Unknown API
// additions remain opaque JSON and cannot alter the selected critical fields.
func captureJobCriticalFields(raw []byte, names []string) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	open, err := decoder.Token()
	if err != nil || open != json.Delim('{') {
		return nil, fail()
	}
	fields := make(map[string]json.RawMessage, len(names))
	for decoder.More() {
		token, err := decoder.Token()
		name, ok := token.(string)
		if err != nil || !ok {
			return nil, fail()
		}
		var value json.RawMessage
		if decoder.Decode(&value) != nil {
			return nil, fail()
		}
		for _, critical := range names {
			if !strings.EqualFold(name, critical) {
				continue
			}
			if name != critical {
				return nil, fail()
			}
			if _, duplicate := fields[critical]; duplicate {
				return nil, fail()
			}
			if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
				return nil, fail()
			}
			fields[critical] = value
		}
	}
	close, err := decoder.Token()
	if err != nil || close != json.Delim('}') ||
		decoder.Decode(new(any)) != io.EOF || len(fields) != len(names) {
		return nil, fail()
	}
	return fields, nil
}

func decodeCaptureJobsPage(raw []byte) (captureJobsPage, error) {
	var page captureJobsPage
	if len(raw) > 2<<20 {
		return page, fail()
	}
	envelope, err := captureJobCriticalFields(raw, []string{"total_count", "jobs"})
	if err != nil || json.Unmarshal(envelope["total_count"], &page.Total) != nil ||
		page.Total < 0 || page.Total > 100 {
		return captureJobsPage{}, fail()
	}
	var entries []json.RawMessage
	if json.Unmarshal(envelope["jobs"], &entries) != nil ||
		page.Total != len(entries) || len(entries) > 100 {
		return captureJobsPage{}, fail()
	}
	// Every row belongs to one normalized page, including rows not selected below.
	seenIDs := make(map[int64]struct{}, len(entries))
	for _, entry := range entries {
		fields, err := captureJobCriticalFields(entry, []string{
			"id", "run_id", "head_sha", "name", "status", "check_run_url",
		})
		if err != nil {
			return captureJobsPage{}, fail()
		}
		var job captureJobEntry
		if json.Unmarshal(fields["id"], &job.ID) != nil ||
			json.Unmarshal(fields["run_id"], &job.RunID) != nil ||
			json.Unmarshal(fields["head_sha"], &job.HeadSHA) != nil ||
			json.Unmarshal(fields["name"], &job.Name) != nil ||
			json.Unmarshal(fields["status"], &job.Status) != nil ||
			json.Unmarshal(fields["check_run_url"], &job.CheckRunURL) != nil {
			return captureJobsPage{}, fail()
		}
		if job.ID <= 0 {
			return captureJobsPage{}, fail()
		}
		if _, duplicate := seenIDs[job.ID]; duplicate {
			return captureJobsPage{}, fail()
		}
		seenIDs[job.ID] = struct{}{}
		page.Jobs = append(page.Jobs, job)
	}
	return page, nil
}

// resolveJobID uses the authenticated jobs API for this exact run attempt.
// The workflow supplies its own check_run_id; a job name/key alone is not ID.
func resolveJobID(ctx context.Context,e captureEnv,token string,client *http.Client) (int64,error) {
    if ctx==nil||ctx.Err()!=nil||token==""||client==nil||e.runID<1||e.attempt<1||e.checkRunID<1 {return 0,fail()}
    endpoint:=fmt.Sprintf("https://api.github.com/repos/AutoDevJapan/cc-pc-tools-rs/actions/runs/%d/attempts/%d/jobs?per_page=100",e.runID,e.attempt)
    req,err:=http.NewRequestWithContext(ctx,http.MethodGet,endpoint,nil);if err!=nil{return 0,fail()}
    req.Header.Set("Authorization","Bearer "+token);req.Header.Set("Accept","application/vnd.github+json");req.Header.Set("X-GitHub-Api-Version","2022-11-28")
    response,err:=client.Do(req);if err!=nil{return 0,fail()}
    if response.StatusCode!=http.StatusOK||!captureJobsSinglePageHeaders(response.Header) {_=response.Body.Close();return 0,fail()}
    raw,err:=io.ReadAll(io.LimitReader(response.Body,2<<20+1));closeErr:=response.Body.Close();if err!=nil||closeErr!=nil||len(raw)>2<<20{return 0,fail()};defer clear(raw)
    envelope,err:=decodeCaptureJobsPage(raw);if err!=nil{return 0,fail()}
    expected:=fmt.Sprintf("https://api.github.com/repos/AutoDevJapan/cc-pc-tools-rs/check-runs/%d",e.checkRunID)
    var id int64;matches:=0
    for _,job:=range envelope.Jobs {if job.RunID==e.runID&&job.HeadSHA==e.workflowSource&&job.Name==jobName&&job.Status=="in_progress"&&job.CheckRunURL==expected&&job.ID>0 {id=job.ID;matches++}}
    if matches!=1||ctx.Err()!=nil{return 0,fail()};return id,nil
}

func captureCommandFor(output string) (captureCommand,error) {
    if output==""||!filepath.IsAbs(output){return captureCommand{},fail()}
    return captureCommand{Directory:readerModuleDirectory,Executable:"go",Arguments:[]string{"build","-mod=readonly","-pgo=off","-trimpath","-o",output,"."}},nil
}

func canonicalCaptureCommand(command captureCommand,output string) (captureCommand,error) {
    if command.Directory!=readerModuleDirectory||command.Executable!="go"||len(command.Arguments)!=7||command.Arguments[0]!="build"||command.Arguments[1]!="-mod=readonly"||command.Arguments[2]!="-pgo=off"||command.Arguments[3]!="-trimpath"||command.Arguments[4]!="-o"||command.Arguments[5]!=output||command.Arguments[6]!="."||filepath.Base(output)!="reader-build.exe" {return captureCommand{},fail()}
    result:=command;result.Arguments=append([]string(nil),command.Arguments...);result.Arguments[5]="<fixed-attempt-directory>/reader-build.exe";return result,nil
}

// The standard toolchain capture is bounded and returns a digest of explicit
// observations, not a claim that every toolchain DLL/cache byte is proven.
func captureBuildGraph(ctx context.Context,root string) ([]byte,error) {
    return runFixed(ctx,filepath.Join(root,filepath.FromSlash(readerModuleDirectory)),"go","list","-deps","-json","-mod=readonly",".")
}

func validReaderPE(raw []byte) bool {
    if len(raw)<64||raw[0]!='M'||raw[1]!='Z'{return false}
    off:=uint64(binary.LittleEndian.Uint32(raw[0x3c:]));if off+6>uint64(len(raw)){return false}
    return bytes.Equal(raw[off:off+4],[]byte{'P','E',0,0})&&binary.LittleEndian.Uint16(raw[off+4:off+6])==0x8664
}

// Each phase is independently recaptured from the fixed checkout. Observed
// runner/toolchain values are evidence, not complete local execution authority.
func collectBefore(ctx context.Context,e captureEnv) (captureBefore,error) {
    var zero captureBefore
    producer,err:=runFixed(ctx,e.workspace,"git","rev-parse","HEAD");if err!=nil||strings.TrimSpace(string(producer))!=e.workflowSource{return zero,fail()}
    head,err:=runFixed(ctx,e.readerRoot,"git","rev-parse","HEAD");if err!=nil||strings.TrimSpace(string(head))!=readerSource{return zero,fail()}
    treeID,err:=runFixed(ctx,e.readerRoot,"git","rev-parse","HEAD^{tree}");if err!=nil||strings.TrimSpace(string(treeID))!=readerTree{return zero,fail()}
    if _,err=runFixed(ctx,e.workspace,"git","merge-base","--is-ancestor",readerSource,e.workflowSource);err!=nil{return zero,fail()}
    status,err:=runFixed(ctx,e.readerRoot,"git","status","--porcelain=v1","--untracked-files=all","--",readerModuleDirectory);if err!=nil||len(bytesTrimSpace(status))!=0{return zero,fail()}
    producerStatus,err:=runFixed(ctx,e.workspace,"git","status","--porcelain=v1","--untracked-files=all","--","tools/guard-reader-capture",".github/workflows/guard-reader-capture.yml");if err!=nil||len(bytesTrimSpace(producerStatus))!=0{return zero,fail()}
    moduleTree,err:=runFixed(ctx,e.readerRoot,"git","rev-parse","HEAD:"+readerModuleDirectory);if err!=nil||strings.TrimSpace(string(moduleTree))!=readerModuleTree{return zero,fail()}
    inventory,err:=captureInventory(e.readerRoot);if err!=nil{return zero,inner(err)}
    tree,err:=runFixed(ctx,e.readerRoot,"git","ls-tree","-r","-z",readerSource,"--",readerModuleDirectory);if err!=nil{return zero,fail()}
    archive,err:=runFixed(ctx,e.readerRoot,"git","archive","--format=tar",readerSource,"--",readerModuleDirectory);if err!=nil{return zero,fail()}
    if err=verifySourceTree(inventory,tree,archive,readerSource);err!=nil{return zero,inner(err)}
    // Preliminary package enumeration may populate the hosted Go cache; its
    // JSON projection, rather than its volatile build paths, is compared.
    graph,err:=captureBuildGraph(ctx,e.readerRoot);if err!=nil{return zero,fail()}
    graphCanonical,err:=canonicalGraph(graph,e.readerRoot);if err!=nil{return zero,inner(err)}
    lock,err:=readLock(e.readerRoot,inventory);if err!=nil{return zero,inner(err)}
    if _,err=checkGraphInputs(graphCanonical,inventory,lock);err!=nil{return zero,inner(err)}
    version,err:=runFixed(ctx,e.readerRoot,"go","version");if err!=nil{return zero,fail()}
    goEnv,err:=runFixed(ctx,e.readerRoot,"go","env","-json","GOOS","GOARCH","GOVERSION","GOROOT","GOTOOLCHAIN","GOWORK","GOFLAGS","CGO_ENABLED","GOMODCACHE","GOCACHE","GOPROXY","GOSUMDB");if err!=nil{return zero,fail()}
    var env map[string]string;if json.Unmarshal(goEnv,&env)!=nil||env["GOOS"]!="windows"||env["GOARCH"]!="amd64"||env["GOTOOLCHAIN"]!="local"||env["GOWORK"]!="off"||env["GOFLAGS"]!=""||env["CGO_ENABLED"]!="0" {return zero,fail()}
    env["GOROOT"]="<runner-goroot>";env["GOMODCACHE"]="<runner-module-cache>";env["GOCACHE"]="<runner-build-cache>"
    command:=captureCommand{Directory:readerModuleDirectory,Executable:"go",Arguments:[]string{"build","-mod=readonly","-pgo=off","-trimpath","-o","<fixed-attempt-directory>/reader-build.exe","."}};commandRaw,_:=json.Marshal(command)
    image:=runnerObservation{OS:os.Getenv("ImageOS"),Version:os.Getenv("ImageVersion"),RunnerOS:os.Getenv("RUNNER_OS"),RunnerArch:os.Getenv("RUNNER_ARCH")};if image.OS==""||image.Version=="" {return zero,fail()};imageRaw,err:=json.Marshal(image);if err!=nil{return zero,fail()}
    observation,err:=json.Marshal(struct{Version string `json:"version"`;Env map[string]string `json:"env"`}{strings.TrimSpace(string(version)),env});if err!=nil{return zero,fail()}
    return captureBefore{Domain:"guard-reader-gha-source-before-v1",RepositoryID:1406818713,Source:readerSource,SourceTree:readerTree,ModuleTree:readerModuleTree,ProducerRevision:e.workflowSource,RunID:e.runID,RunAttempt:e.attempt,CheckRunID:e.checkRunID,Event:e.event,InventorySHA:inventoryDigest(inventory),BuildGraphSHA:hashBytes(graphCanonical),BuildGraph:graphCanonical,SourceTreeBefore:strings.TrimSpace(string(treeID)),GitTreeSHA:hashBytes(tree),GitArchiveSHA:hashBytes(archive),RunnerToolchainObservationSHA:hashBytes(observation),RunnerToolchainObservation:observation,RunnerImageSHA:hashBytes(imageRaw),RunnerImage:image,CompileCommandSHA:hashBytes(commandRaw),CompileCommand:command,Inputs:inventory},nil
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
    for {var item graphPackage;err:=dec.Decode(&item);if err==io.EOF{break};if err!=nil||item.ImportPath==""||item.Error!=nil||len(item.DepsErrors)!=0||seen[item.ImportPath]{return nil,fail()};seen[item.ImportPath]=true;objects=append(objects,item);if len(objects)>4096{return nil,fail()}}
    if len(objects)==0||root==""{return nil,fail()};sort.Slice(objects,func(i,j int)bool{return objects[i].ImportPath<objects[j].ImportPath})
    encoded,err:=json.Marshal(objects);if err!=nil||len(encoded)>8<<20{return nil,fail()};return encoded,nil
}


// The application is one flat main package of this module. Besides the
// standard library only third-party modules with an exact go.sum lock may
// appear; their package files are covered by the module checksums, not by this
// repository's inventory. Source and toolchain observations stay separate from
// update authority.
func checkGraphInputs(raw []byte,records []sourceRecord,lock map[string]string) (map[string]string,error) {
    var packages []graphPackage
    if json.Unmarshal(raw,&packages)!=nil||len(packages)==0||len(lock)==0{return nil,fail()}
    known:=map[string]bool{}
    for _,r:=range records {if r.Path==""||known[r.Path]{return nil,fail()};known[r.Path]=true}
    modules:=map[string]string{}
    mainCount:=0
    for _,pkg:=range packages {
        if pkg.Standard {
            if pkg.Module!=nil{return nil,fail()}
            continue
        }
        if pkg.Module==nil||pkg.Module.Replace!=nil{return nil,fail()}
        module:=pkg.Module
        if module.Main {
            if pkg.ImportPath!=readerModulePath||module.Path!=readerModulePath||module.Version!=""{return nil,fail()}
            mainCount++
            if len(pkg.GoFiles)==0||len(pkg.CgoFiles)+len(pkg.CFiles)+len(pkg.CXXFiles)+len(pkg.HFiles)+len(pkg.SFiles)+len(pkg.SysoFiles)+len(pkg.EmbedFiles)!=0 {return nil,fail()}
            seen:=map[string]bool{}
            for _,name:=range pkg.GoFiles {
                if path.Base(name)!=name||strings.ContainsAny(name,"\\:")||!strings.HasSuffix(name,".go")||strings.HasSuffix(name,"_test.go")||seen[strings.ToLower(name)]||!known[readerModuleDirectory+"/"+name] {return nil,fail()}
                seen[strings.ToLower(name)]=true
            }
            continue
        }
        if module.Path==""||module.Path==readerModulePath||module.Version==""||(pkg.ImportPath!=module.Path&&!strings.HasPrefix(pkg.ImportPath,module.Path+"/")) {return nil,fail()}
        if prior,found:=modules[module.Path];found&&prior!=module.Version{return nil,fail()}
        modules[module.Path]=module.Version
        zipSum,zipLocked:=lock[module.Path+" "+module.Version]
        modSum,modLocked:=lock[module.Path+" "+module.Version+"/go.mod"]
        if !zipLocked||!modLocked{return nil,fail()}
        // The listing's checksum fields are optional; when present they must be the locked ones.
        if (module.Sum!=""&&module.Sum!=zipSum)||(module.GoModSum!=""&&module.GoModSum!=modSum){return nil,fail()}
    }
    if mainCount!=1||len(modules)==0||!known[readerModuleDirectory+"/go.mod"]||!known[readerModuleDirectory+"/go.sum"]{return nil,fail()}
    return modules,nil
}

// go.sum lines are "module version hash"; an entry seen twice must agree.
func parseGoSum(raw []byte) (map[string]string,error) {
    if len(raw)==0||len(raw)>4<<20||bytes.IndexByte(raw,0)>=0{return nil,fail()}
    lock:=map[string]string{}
    for _,line:=range strings.Split(string(raw),"\n") {
        fields:=strings.Fields(line)
        if len(fields)==0{continue}
        if len(fields)!=3||!strings.HasPrefix(fields[2],"h1:")||len(fields[2])<=3{return nil,fail()}
        key:=fields[0]+" "+fields[1]
        if prior,found:=lock[key];found&&prior!=fields[2]{return nil,fail()}
        lock[key]=fields[2]
    }
    if len(lock)==0{return nil,fail()}
    return lock,nil
}

// The lock is read from the checked-out file whose size and digests the source
// inventory already recorded; any difference rejects the capture.
func readLock(root string,records []sourceRecord) (map[string]string,error) {
    name:=readerModuleDirectory+"/go.sum"
    var record sourceRecord;found:=0
    for _,r:=range records {if r.Path==name{record=r;found++}}
    if found!=1||record.Size<1||record.Size>sourceLimit{return nil,fail()}
    file,err:=os.Open(filepath.Join(root,filepath.FromSlash(name)));if err!=nil{return nil,fail()}
    raw,readErr:=io.ReadAll(io.LimitReader(file,sourceLimit+1));closeErr:=file.Close()
    if readErr!=nil||closeErr!=nil||int64(len(raw))!=record.Size||hashBytes(raw)!=record.SHA||gitBlob(raw)!=record.Blob{clear(raw);return nil,fail()}
    lock,err:=parseGoSum(raw);clear(raw)
    if err!=nil{return nil,fail()}
    return lock,nil
}

// The linked binary must contain exactly the locked modules of the captured
// package graph: the same versions, the go.sum checksums and no replacement.
func checkBinaryDependencies(deps []*debug.Module,modules,lock map[string]string) error {
    if len(deps)==0||len(deps)!=len(modules){return fail()}
    seen:=map[string]bool{}
    for _,dep:=range deps {
        if dep==nil||dep.Replace!=nil||dep.Path==""||dep.Version==""||seen[dep.Path]{return fail()}
        seen[dep.Path]=true
        if modules[dep.Path]!=dep.Version||dep.Sum==""||dep.Sum!=lock[dep.Path+" "+dep.Version]{return fail()}
    }
    return nil
}
