package main

import (
    "archive/tar"
    "bytes"
    "context"
    "encoding/json"
    "os/exec"
    "runtime"
    "io"
    "encoding/binary"
    "fmt"
    "os"
    "net/http"
    "path/filepath"
    "strings"
    "testing"
)


func TestSourceInventoryTracksEverySelectedByte(t *testing.T) {
    root:=t.TempDir()
    for name,body:=range map[string]string{"repair-guard/go.mod":"module example","repair-guard/a.go":"package main","repair-guard/owner_catalog.json":"{}","tools/civ6-ci-contract/go.mod":"module local"} {
        full:=filepath.Join(root,filepath.FromSlash(name))
        if err:=os.MkdirAll(filepath.Dir(full),0700);err!=nil {t.Fatal(err)}
        if err:=os.WriteFile(full,[]byte(body),0600);err!=nil {t.Fatal(err)}
    }
    first,err:=captureInventory(root);if err!=nil||len(first)!=4 {t.Fatal(first,err)}
    before:=inventoryDigest(first)
    if err=os.WriteFile(filepath.Join(root,"repair-guard","a.go"),[]byte("changed"),0600);err!=nil {t.Fatal(err)}
    second,err:=captureInventory(root);if err!=nil||inventoryDigest(second)==before {t.Fatal("changed source not detected",err)}
    if err=os.WriteFile(filepath.Join(root,"repair-guard","extra.go"),[]byte("package main"),0600);err!=nil {t.Fatal(err)}
    third,err:=captureInventory(root);if err!=nil||len(third)!=5||inventoryDigest(third)==inventoryDigest(second) {t.Fatal("new source not detected",err)}
}

func TestFixedEnvironmentRejectsMissingOrUntrustedOrigin(t *testing.T) {
    for _,tc:=range []struct{name string;env map[string]string;valid bool}{
        {"valid", map[string]string{"GITHUB_ACTIONS":"true","GITHUB_REPOSITORY_ID":"1406818713","GITHUB_RUN_ID":"7","GITHUB_RUN_ATTEMPT":"1","GITHUB_JOB":"guard-bootstrap","GITHUB_EVENT_NAME":"workflow_dispatch","GITHUB_REF":"refs/heads/main","GITHUB_SHA":strings.Repeat("a",40),"RUNNER_OS":"Windows","RUNNER_ARCH":"X64","GITHUB_WORKSPACE":filepath.Join(t.TempDir(),"owned"),"GUARD_CHECK_RUN_ID":"8","RUNNER_NAME":"owned-runner","RUNNER_ENVIRONMENT":"github-hosted","RUNNER_TEMP":t.TempDir()},true},
        {"wrong-repository",map[string]string{"GITHUB_ACTIONS":"true","GITHUB_REPOSITORY_ID":"9"},false},
        {"empty",map[string]string{},false},
    } {
        t.Run(tc.name,func(t *testing.T){_,err:=parseCaptureEnvironment(func(k string)string{return tc.env[k]});if (err==nil)!=tc.valid {t.Fatal("unexpected admission",err)}})
    }
}

func TestCaptureFileCreateNewPreservesExisting(t *testing.T) {
    path:=filepath.Join(t.TempDir(),"capture.json")
    if err:=writeExclusive(path,[]byte("first"));err!=nil {t.Fatal(err)}
    if err:=writeExclusive(path,[]byte("second"));err==nil {t.Fatal("collision accepted")}
    b,err:=os.ReadFile(path);if err!=nil||string(b)!="first" {t.Fatal("existing file changed",err)}
}


func TestSourceTreeArchiveAndInventoryMatch(t *testing.T) {
    names:=[]string{"repair-guard/go.mod","repair-guard/main_windows.go","tools/civ6-ci-contract/go.mod"}
    records:=make([]sourceRecord,0,len(names));var tree bytes.Buffer;var archive bytes.Buffer;writer:=tar.NewWriter(&archive)
    for _,name:=range names {
        raw:=[]byte("owned "+name+"\n");records=append(records,sourceRecord{Path:name,Size:int64(len(raw)),SHA:hashBytes(raw),Blob:gitBlob(raw)})
        fmt.Fprintf(&tree,"100644 blob %s\t%s\x00",gitBlob(raw),name)
        if err:=writer.WriteHeader(&tar.Header{Name:name,Mode:0644,Size:int64(len(raw))});err!=nil{t.Fatal(err)}
        if _,err:=writer.Write(raw);err!=nil{t.Fatal(err)}
    }
    if err:=writer.Close();err!=nil{t.Fatal(err)}
    if err:=verifySourceTree(records,tree.Bytes(),archive.Bytes(),strings.Repeat("a",40));err!=nil {t.Fatal("valid fixture rejected",err)}
    changed:=append([]sourceRecord(nil),records...);changed[0].SHA=strings.Repeat("a",64)
    if verifySourceTree(changed,tree.Bytes(),archive.Bytes(),strings.Repeat("a",40))==nil {t.Fatal("changed checkout accepted")}
    extra:=append(append([]byte(nil),tree.Bytes()...),tree.Bytes()...)
    if verifySourceTree(records,extra,archive.Bytes(),strings.Repeat("a",40))==nil {t.Fatal("duplicate tree entry accepted")}
    if verifySourceTree(records,tree.Bytes(),archive.Bytes()[:20],strings.Repeat("a",40))==nil {t.Fatal("truncated archive accepted")}
    linked:=bytes.Replace(tree.Bytes(),[]byte("100644 blob"),[]byte("120000 blob"),1)
    if verifySourceTree(records,linked,archive.Bytes(),strings.Repeat("a",40))==nil {t.Fatal("symlink mode accepted")}
}

func TestFixedCaptureCommand(t *testing.T) {
    output:=filepath.Join(t.TempDir(),"guard-build.exe");command,err:=captureCommandFor(output)
    if err!=nil||command.Directory!="repair-guard"||command.Executable!="go" {t.Fatal(command,err)}
    joined:=strings.Join(command.Arguments," ")
    if !strings.Contains(joined,"-mod=readonly")||!strings.Contains(joined,"-pgo=off")||!strings.Contains(joined,"-trimpath")||!strings.Contains(joined,output) {t.Fatal("fixed build flags missing",joined)}
    canonical,err:=canonicalCaptureCommand(command,output);if err!=nil||canonical.Arguments[5]!="<fixed-attempt-directory>/guard-build.exe" {t.Fatal("canonical command differs",err)}
    command.Arguments[5]=filepath.Join(t.TempDir(),"other.exe");if _,err=canonicalCaptureCommand(command,output);err==nil {t.Fatal("changed output accepted")}
    if _,err=captureCommandFor("relative.exe");err==nil {t.Fatal("relative output accepted")}
}

func TestGuardOutputPEHeader(t *testing.T) {
    raw:=make([]byte,256);raw[0]='M';raw[1]='Z';binary.LittleEndian.PutUint32(raw[0x3c:],128);copy(raw[128:],[]byte{'P','E',0,0});binary.LittleEndian.PutUint16(raw[132:],0x8664)
    if !validGuardPE(raw){t.Fatal("valid AMD64 header rejected")}
    raw[132]=0;if validGuardPE(raw){t.Fatal("wrong machine accepted")}
}

func TestGraphProjectionIsStableAndRejectsErrors(t *testing.T) {
    one:=[]byte(`{"ImportPath":"example/main","GoFiles":["main.go"],"Imports":["fmt"]}`)
    out,err:=canonicalGraph(one,"C:/owned");if err!=nil||len(out)==0 {t.Fatal("valid graph",err)}
    if _,err=canonicalGraph(append(one,one...),"C:/owned");err==nil {t.Fatal("duplicate package accepted")}
    if _,err=canonicalGraph([]byte(`{"ImportPath":"example/main","Error":{"Err":"failure"}}`),"C:/owned");err==nil {t.Fatal("package error accepted")}
}

type captureTestRoundTrip func(*http.Request)(*http.Response,error)
func (f captureTestRoundTrip) RoundTrip(r *http.Request)(*http.Response,error){return f(r)}

func TestResolveJobIDRequiresExactCheckRun(t *testing.T) {
    e:=captureEnv{runID:7,attempt:1,checkRunID:8,workflowSource:strings.Repeat("b",40)}
    for _,fault:=range []string{"none","wrong-check","wrong-run","wrong-head","wrong-name","wrong-status","duplicate","link","unauthorized"} {
        t.Run(fault,func(t *testing.T){
            rows:=[]map[string]any{{"id":int64(9),"run_id":int64(7),"head_sha":strings.Repeat("b",40),"name":"Guard bootstrap artifact","status":"in_progress","check_run_url":"https://api.github.com/repos/AutoDevJapan/cc-pc-tools-rs/check-runs/8"}}
            switch fault {case "wrong-check":rows[0]["check_run_url"]="https://api.github.com/repos/AutoDevJapan/cc-pc-tools-rs/check-runs/10";case "wrong-run":rows[0]["run_id"]=int64(8);case "wrong-head":rows[0]["head_sha"]=strings.Repeat("a",40);case "wrong-name":rows[0]["name"]="other";case "wrong-status":rows[0]["status"]="completed";case "duplicate":rows=append(rows,rows[0])}
            data,_:=json.Marshal(map[string]any{"total_count":len(rows),"jobs":rows})
            client:=&http.Client{Transport:captureTestRoundTrip(func(req *http.Request)(*http.Response,error){
                if req.URL.Host!="api.github.com"||req.Header.Get("Authorization")!="Bearer owned-test-token" {t.Fatal("wrong request")}
                status:=http.StatusOK;if fault=="unauthorized" {status=http.StatusUnauthorized}
                header:=make(http.Header);if fault=="link" {header.Set("Link","<next>; rel=next")}
                return &http.Response{StatusCode:status,Header:header,Body:io.NopCloser(bytes.NewReader(data))},nil
            })}
            id,err:=resolveJobID(context.Background(),e,"owned-test-token",client)
            if fault=="none" {if err!=nil||id!=9 {t.Fatal(id,err)}} else if err==nil||id!=0 {t.Fatal("bad job accepted",fault)}
        })
    }
}

func TestCaptureMainRejectsArgumentsAndNonRunner(t *testing.T) {
    var out bytes.Buffer
    if code:=run([]string{"--arbitrary"},&out);code!=2||out.Len()!=0 {t.Fatal("arbitrary arguments accepted",code)}
    t.Setenv("GITHUB_ACTIONS","")
    if code:=run(nil,&out);code!=1||out.Len()!=0 {t.Fatal("non-runner accepted",code)}
}

func TestFinalManifestShape(t *testing.T) {
    m:=finalManifest{Domain:"guard-gha-capture-v1",RepositoryID:1406818713,Source:guardSource,SourceTree:guardTree,ProducerRevision:strings.Repeat("b",40),RunID:1,RunAttempt:1,JobID:2,CheckRunID:3,SourceManifestSHA:strings.Repeat("c",64),RunnerToolchainSHA:strings.Repeat("d",64),CompileCommandSHA:strings.Repeat("e",64),RunnerImageSHA:strings.Repeat("f",64),OutputName:"guard.exe",OutputBytes:3,OutputSHA:strings.Repeat("1",64)}
    raw,err:=json.Marshal(m);if err!=nil {t.Fatal(err)}
    var got map[string]any;if json.Unmarshal(raw,&got)!=nil||got["source"]!=guardSource||got["producer_revision"]==guardSource||got["source_tree"]!=guardTree {t.Fatal("source and producer collapsed",string(raw))}
    if len(got)!=16 {t.Fatal("unexpected capture schema",len(got))}
}

func TestCaptureRejectsWrongMainSourceAndProducerRevision(t *testing.T) {
    env:=map[string]string{"GITHUB_ACTIONS":"true","GITHUB_REPOSITORY_ID":"1406818713","GITHUB_RUN_ID":"7","GITHUB_RUN_ATTEMPT":"1","GITHUB_JOB":"guard-bootstrap","GITHUB_EVENT_NAME":"workflow_dispatch","GITHUB_REF":"refs/heads/main","GITHUB_SHA":strings.Repeat("b",40),"RUNNER_OS":"Windows","RUNNER_ARCH":"X64","GITHUB_WORKSPACE":t.TempDir(),"GUARD_CHECK_RUN_ID":"8","RUNNER_NAME":"owned","RUNNER_ENVIRONMENT":"github-hosted","RUNNER_TEMP":t.TempDir()}
    got,err:=parseCaptureEnvironment(func(k string)string{return env[k]});if err!=nil||got.workflowSource!=strings.Repeat("b",40)||got.workflowSource==guardSource||!strings.HasSuffix(got.guardRoot,"guard-source") {t.Fatal("producer/source separation",got,err)}
    env["GITHUB_REF"]="refs/heads/other";if _,err=parseCaptureEnvironment(func(k string)string{return env[k]});err==nil {t.Fatal("other ref accepted")}
}

func TestBuildGraphInputsAreInFixedInventory(t *testing.T) {
    records:=[]sourceRecord{{Path:"repair-guard/go.mod"},{Path:"repair-guard/go.sum"},{Path:"repair-guard/main_windows.go"},{Path:"repair-guard/owner_catalog.json"},{Path:"tools/civ6-ci-contract/go.mod"},{Path:"tools/civ6-ci-contract/go.sum"},{Path:"tools/civ6-ci-contract/plan.go"}}
    packages:=[]graphPackage{{ImportPath:"github.com/AutoDevJapan/cc-pc-tools-rs/repair-guard",GoFiles:[]string{"main_windows.go"},EmbedFiles:[]string{"owner_catalog.json"}},{ImportPath:"cc/civ6-ci-contract",Module:&graphModule{Path:"cc/civ6-ci-contract",Replace:&graphModule{Path:"../tools/civ6-ci-contract"}},GoFiles:[]string{"plan.go"}}}
    raw,_:=json.Marshal(packages);if err:=checkGraphInputs(raw,records);err!=nil{t.Fatal("valid graph rejected",err)}
    packages[0].EmbedFiles=[]string{"unproved.json"};raw,_=json.Marshal(packages);if checkGraphInputs(raw,records)==nil {t.Fatal("unproved embed accepted")}
    packages[0].EmbedFiles=nil;packages[1].GoFiles=[]string{"new.go"};raw,_=json.Marshal(packages);if checkGraphInputs(raw,records)==nil {t.Fatal("unproved replace input accepted")}
}

func TestCheckoutScopedConfigPreservesCanonicalLF(t *testing.T) {
    if runtime.GOOS!="windows" {t.Skip("Windows checkout conversion")}
    root:=t.TempDir()
    runGit:=func(env []string,args ...string) []byte {
        t.Helper();cmd:=exec.Command("git",args...);cmd.Dir=root
        for _,v:=range os.Environ() {k,_,_:=strings.Cut(v,"=");if strings.HasPrefix(strings.ToUpper(k),"GIT_") {continue};cmd.Env=append(cmd.Env,v)}
        cmd.Env=append(cmd.Env,"GIT_CONFIG_NOSYSTEM=1","GIT_CONFIG_GLOBAL=NUL", "GIT_NO_REPLACE_OBJECTS=1");cmd.Env=append(cmd.Env,env...)
        var out,diagnostic boundedOutput;out.limit=1<<20;diagnostic.limit=1<<20;cmd.Stdout=&out;cmd.Stderr=&diagnostic
        if err:=cmd.Run();err!=nil||out.overflow||diagnostic.overflow {t.Fatalf("fixture git failed: %v",err)};return out.data
    }
    runGit(nil,"init")
    if err:=os.WriteFile(filepath.Join(root,"fixture.txt"),[]byte("one\ntwo\n"),0600);err!=nil {t.Fatal(err)}
    runGit(nil,"-c","core.autocrlf=false","add","fixture.txt")
    runGit(nil,"config","--local","core.autocrlf","true")
    runGit(nil,"config","--local","core.eol","crlf")
    for _,dir:=range []string{"baseline","scoped"} {if err:=os.Mkdir(filepath.Join(root,dir),0700);err!=nil {t.Fatal(err)}}
    runGit(nil,"checkout-index","--prefix=baseline/","--","fixture.txt")
    scoped:=[]string{"GIT_CONFIG_COUNT=2","GIT_CONFIG_KEY_0=core.autocrlf","GIT_CONFIG_VALUE_0=false","GIT_CONFIG_KEY_1=core.eol","GIT_CONFIG_VALUE_1=lf"}
    runGit(scoped,"checkout-index","--prefix=scoped/","--","fixture.txt")
    base,err:=os.ReadFile(filepath.Join(root,"baseline","fixture.txt"));if err!=nil {t.Fatal(err)}
    fixed,err:=os.ReadFile(filepath.Join(root,"scoped","fixture.txt"));if err!=nil {t.Fatal(err)}
    canonical:=runGit(nil,"show",":fixture.txt")
    if !bytes.Contains(base,[]byte("\r\n"))||bytes.Equal(base,canonical) {t.Fatal("baseline did not demonstrate CRLF")}
    if !bytes.Equal(fixed,canonical)||hashBytes(fixed)!=hashBytes(canonical)||gitBlob(fixed)!=gitBlob(canonical) {t.Fatal("scoped checkout differed from canonical bytes")}
    if strings.TrimSpace(string(runGit(nil,"config","--local","--get","core.autocrlf")))!="true"||strings.TrimSpace(string(runGit(nil,"config","--local","--get","core.eol")))!="crlf" {t.Fatal("local Git configuration changed")}
}
