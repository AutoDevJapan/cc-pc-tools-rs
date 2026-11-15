package main

import (
    "archive/tar"
    "bytes"
    "context"
    "encoding/binary"
    "encoding/json"
    "io"
    "net/http"
    "os"
    "path/filepath"
    "strconv"
    "strings"
    "testing"
)

func captureValues(t *testing.T) map[string]string {
    t.Helper()
    return map[string]string{
        "GITHUB_ACTIONS":"true", "GITHUB_REPOSITORY":"AutoDevJapan/cc-pc-tools-rs",
        "GITHUB_REPOSITORY_ID":repositoryID, "GITHUB_SERVER_URL":"https://github.com",
        "GITHUB_API_URL":"https://api.github.com",
        "GITHUB_WORKFLOW_REF":"AutoDevJapan/cc-pc-tools-rs/.github/workflows/task-runner-capture.yml@refs/heads/main",
        "GITHUB_WORKFLOW_SHA":strings.Repeat("a",40), "GITHUB_SHA":strings.Repeat("a",40),
        "GITHUB_JOB":jobKey, "GITHUB_EVENT_NAME":"workflow_dispatch",
        "GITHUB_REF":"refs/heads/main", "RUNNER_OS":"Windows", "RUNNER_ARCH":"X64",
        "RUNNER_ENVIRONMENT":"github-hosted", "RUNNER_NAME":"owned-hosted-fixture",
        "RUNNER_TEMP":t.TempDir(), "GITHUB_WORKSPACE":t.TempDir(),
        "GITHUB_RUN_ID":"17", "GITHUB_RUN_ATTEMPT":"1", "RUNNER_CAPTURE_CHECK_RUN_ID":"31",
    }
}

func TestCaptureEnvironmentIsOneFixedHostedPurpose(t *testing.T) {
    values:=captureValues(t)
    if _,err:=parseCaptureEnvironment(func(k string)string{return values[k]});err!=nil {t.Fatal(err)}
    cases:=map[string]string{
        "GITHUB_ACTIONS":"false", "GITHUB_REPOSITORY":"other/repository",
        "GITHUB_REPOSITORY_ID":"1", "GITHUB_SERVER_URL":"https://example.invalid",
        "GITHUB_API_URL":"https://example.invalid",
        "GITHUB_WORKFLOW_REF":"AutoDevJapan/cc-pc-tools-rs/.github/workflows/other.yml@refs/heads/main",
        "GITHUB_WORKFLOW_SHA":strings.Repeat("b",40), "GITHUB_SHA":"bad",
        "GITHUB_JOB":"guard-bootstrap", "GITHUB_EVENT_NAME":"pull_request",
        "GITHUB_REF":"refs/heads/other", "RUNNER_OS":"Linux", "RUNNER_ARCH":"ARM64",
        "RUNNER_ENVIRONMENT":"self-hosted", "RUNNER_NAME":"", "RUNNER_TEMP":"",
        "GITHUB_WORKSPACE":"", "GITHUB_RUN_ID":"0", "GITHUB_RUN_ATTEMPT":"2",
        "RUNNER_CAPTURE_CHECK_RUN_ID":"0",
    }
    for key,value:=range cases {t.Run(key,func(t *testing.T) {
        old:=values[key];values[key]=value
        defer func(){values[key]=old}()
        if _,err:=parseCaptureEnvironment(func(k string)string{return values[k]});err==nil {t.Fatal("changed purpose accepted")}
    })}
}

func mainGraph() []graphPackage {
    return []graphPackage{
        {ImportPath:"fmt",Standard:true,GoFiles:[]string{"print.go"}},
        {ImportPath:"github.com/AutoDevJapan/cc-pc-tools-rs/unattended-runner",
         Module:&graphModule{Path:"github.com/AutoDevJapan/cc-pc-tools-rs/unattended-runner",Main:true},
         GoFiles:[]string{"runner.go"}},
    }
}
func graphRecords() []sourceRecord {
    return []sourceRecord{{Path:"unattended-runner/go.mod"},{Path:"unattended-runner/runner.go"}}
}
func graphBytes(t *testing.T,g []graphPackage) []byte {t.Helper();raw,err:=json.Marshal(g);if err!=nil{t.Fatal(err)};return raw}

func TestGraphRejectsInputsOutsideTheCapturedApplication(t *testing.T) {
    if err:=checkGraphInputs(graphBytes(t,mainGraph()),graphRecords());err!=nil {t.Fatal(err)}
    cases:=map[string]func([]graphPackage)[]graphPackage{
        "unknown dependency":func(g []graphPackage)[]graphPackage{return append(g,graphPackage{ImportPath:"example.invalid/extra"})},
        "replacement":func(g []graphPackage)[]graphPackage{g[1].Module.Replace=&graphModule{Path:"other"};return g},
        "versioned application":func(g []graphPackage)[]graphPackage{g[1].Module.Version="v1.0.0";return g},
        "non-main module":func(g []graphPackage)[]graphPackage{g[1].Module.Main=false;return g},
        "missing main":func(g []graphPackage)[]graphPackage{return g[:1]},
        "duplicate main":func(g []graphPackage)[]graphPackage{return append(g,g[1])},
        "unknown source":func(g []graphPackage)[]graphPackage{g[1].GoFiles=[]string{"late.go"};return g},
        "traversal":func(g []graphPackage)[]graphPackage{g[1].GoFiles=[]string{"../runner.go"};return g},
        "case duplicate":func(g []graphPackage)[]graphPackage{g[1].GoFiles=[]string{"runner.go","RUNNER.go"};return g},
        "test file":func(g []graphPackage)[]graphPackage{g[1].GoFiles=[]string{"runner_test.go"};return g},
        "cgo":func(g []graphPackage)[]graphPackage{g[1].CgoFiles=[]string{"extra.go"};return g},
        "syso":func(g []graphPackage)[]graphPackage{g[1].SysoFiles=[]string{"extra.syso"};return g},
        "embed":func(g []graphPackage)[]graphPackage{g[1].EmbedFiles=[]string{"extra.bin"};return g},
    }
    for name,change:=range cases {t.Run(name,func(t *testing.T) {
        if err:=checkGraphInputs(graphBytes(t,change(mainGraph())),graphRecords());err==nil {t.Fatal("uncaptured input accepted")}
    })}
    if err:=checkGraphInputs(graphBytes(t,mainGraph()),graphRecords()[1:]);err==nil {t.Fatal("missing go.mod accepted")}
}

func TestCanonicalGraphRejectsAmbiguityAndErrors(t *testing.T) {
    var joined bytes.Buffer
    for _,pkg:=range mainGraph(){raw,_:=json.Marshal(pkg);joined.Write(raw);joined.WriteByte('\n')}
    valid,err:=canonicalGraph(joined.Bytes(),"fixed-root");if err!=nil {t.Fatal(err)}
    if err=checkGraphInputs(valid,graphRecords());err!=nil{t.Fatal(err)}
    for _,raw:=range [][]byte{
        nil, []byte("not JSON"), append(append([]byte(nil),joined.Bytes()...),[]byte("{} trailing")...),
        []byte("{\"ImportPath\":\"fmt\"}\n{\"ImportPath\":\"fmt\"}"),
        []byte("{\"ImportPath\":\"fmt\",\"Error\":{}}"),
        []byte("{\"ImportPath\":\"fmt\",\"DepsErrors\":[{}]}"),
    } {if _,err=canonicalGraph(raw,"fixed-root");err==nil{t.Fatal("ambiguous graph accepted")}}
}

func sourceFixture(t *testing.T,files map[string][]byte)([]sourceRecord,[]byte,[]byte) {
    t.Helper()
    names:=[]string{"unattended-runner/go.mod","unattended-runner/runner.go"}
    records:=make([]sourceRecord,0,len(names));var tree,archive bytes.Buffer;tw:=tar.NewWriter(&archive)
    for _,name:=range names {
        raw:=files[name]
        records=append(records,sourceRecord{Path:name,Size:int64(len(raw)),SHA:hashBytes(raw),Blob:gitBlob(raw)})
        tree.WriteString("100644 blob "+gitBlob(raw)+"\t"+name+"\x00")
        if err:=tw.WriteHeader(&tar.Header{Name:name,Mode:0600,Size:int64(len(raw)),Typeflag:tar.TypeReg});err!=nil{t.Fatal(err)}
        if _,err:=tw.Write(raw);err!=nil{t.Fatal(err)}
    }
    if err:=tw.Close();err!=nil{t.Fatal(err)}
    return records,tree.Bytes(),archive.Bytes()
}

func TestTreeAndArchiveAreBoundToTheSameInputBytes(t *testing.T) {
    original:=map[string][]byte{"unattended-runner/go.mod":[]byte("module fixture\n"),"unattended-runner/runner.go":[]byte("package main\n")}
    records,tree,archive:=sourceFixture(t,original)
    if err:=verifySourceTree(records,tree,archive,runnerSource);err!=nil{t.Fatal(err)}
    changed:=map[string][]byte{"unattended-runner/go.mod":original["unattended-runner/go.mod"],"unattended-runner/runner.go":append([]byte(nil),original["unattended-runner/runner.go"]...)}
    changed["unattended-runner/runner.go"][0]^=1
    if bytes.Equal(original["unattended-runner/runner.go"],changed["unattended-runner/runner.go"])||hashBytes(original["unattended-runner/runner.go"])==hashBytes(changed["unattended-runner/runner.go"]){t.Fatal("mutation was vacuous")}
    _,_,validChangedArchive:=sourceFixture(t,changed)
    if err:=verifySourceTree(records,tree,validChangedArchive,runnerSource);err==nil{t.Fatal("changed source accepted")}
    if err:=verifySourceTree(records,tree[:len(tree)-1],archive,runnerSource);err==nil{t.Fatal("truncated tree accepted")}
    duplicate:=append(append([]byte(nil),tree...),tree...)
    if err:=verifySourceTree(records,duplicate,archive,runnerSource);err==nil{t.Fatal("duplicate tree accepted")}
    linkTree:=bytes.Replace(tree,[]byte("100644"),[]byte("120000"),1)
    if err:=verifySourceTree(records,linkTree,archive,runnerSource);err==nil{t.Fatal("link mode accepted")}
}

func TestInventoryRequiresCompleteFixedNamespace(t *testing.T) {
    root:=t.TempDir();dir:=filepath.Join(root,"unattended-runner")
    if err:=os.Mkdir(dir,0700);err!=nil{t.Fatal(err)}
    for i:=0;i<185;i++ {if err:=os.WriteFile(filepath.Join(dir,"fixture-"+strconv.Itoa(i)+".go"),[]byte("package main\n"),0600);err!=nil{t.Fatal(err)}}
    records,err:=captureInventory(root);if err!=nil||len(records)!=185{t.Fatalf("complete fixture: %v",err)}
    if err=os.WriteFile(filepath.Join(dir,"late.go"),[]byte("package main\n"),0600);err!=nil{t.Fatal(err)}
    if _,err=captureInventory(root);err==nil{t.Fatal("added input accepted")}
}

func TestExclusiveReceiptsDoNotOverwrite(t *testing.T) {
    p:=filepath.Join(t.TempDir(),"capture.json");original:=[]byte("original")
    if err:=writeExclusive(p,original);err!=nil{t.Fatal(err)}
    if err:=writeExclusive(p,[]byte("replacement"));err==nil{t.Fatal("existing receipt overwritten")}
    again,err:=os.ReadFile(p);if err!=nil||!bytes.Equal(again,original){t.Fatal("original receipt changed")}
}

func TestBoundedChildCaptureDoesNotHideOverflow(t *testing.T) {
    out:=boundedOutput{limit:3}
    if n,err:=out.Write([]byte("12345"));n!=5||err!=nil||!out.overflow||string(out.data)!="123"{t.Fatal("output limit not recorded")}
}

type roundTripFunc func(*http.Request)(*http.Response,error)
func (f roundTripFunc)RoundTrip(r *http.Request)(*http.Response,error){return f(r)}

func TestAuthenticatedJobTupleAndCardinality(t *testing.T) {
    env:=captureEnv{workflowSource:strings.Repeat("a",40),runID:17,attempt:1,checkRunID:31}
    type job struct{ID int64 `json:"id"`;RunID int64 `json:"run_id"`;HeadSHA string `json:"head_sha"`;Name string `json:"name"`;Status string `json:"status"`;CheckRunURL string `json:"check_run_url"`}
    good:=job{ID:23,RunID:17,HeadSHA:env.workflowSource,Name:"Task runner software artifact",Status:"in_progress",CheckRunURL:"https://api.github.com/repos/AutoDevJapan/cc-pc-tools-rs/check-runs/31"}
    calls:=0;jobs:=[]job{good};total:=1
    client:=&http.Client{Transport:roundTripFunc(func(r *http.Request)(*http.Response,error) {
        calls++
        if r.URL.String()!="https://api.github.com/repos/AutoDevJapan/cc-pc-tools-rs/actions/runs/17/attempts/1/jobs?per_page=100"||r.Header.Get("Authorization")!="Bearer fixture-token"{t.Fatal("unbound job request")}
        raw,_:=json.Marshal(struct{Total int `json:"total_count"`;Jobs []job `json:"jobs"`}{total,jobs})
        return &http.Response{StatusCode:200,Header:make(http.Header),Body:io.NopCloser(bytes.NewReader(raw))},nil
    })}
    if id,err:=resolveJobID(context.Background(),env,"fixture-token",client);err!=nil||id!=23{t.Fatal("normal tuple not selected")}
    for _,bad:=range []job{
        {ID:23,RunID:18,HeadSHA:good.HeadSHA,Name:good.Name,Status:good.Status,CheckRunURL:good.CheckRunURL},
        {ID:23,RunID:17,HeadSHA:strings.Repeat("b",40),Name:good.Name,Status:good.Status,CheckRunURL:good.CheckRunURL},
        {ID:23,RunID:17,HeadSHA:good.HeadSHA,Name:"Other job",Status:good.Status,CheckRunURL:good.CheckRunURL},
        {ID:23,RunID:17,HeadSHA:good.HeadSHA,Name:good.Name,Status:"completed",CheckRunURL:good.CheckRunURL},
        {ID:23,RunID:17,HeadSHA:good.HeadSHA,Name:good.Name,Status:good.Status,CheckRunURL:good.CheckRunURL+"0"},
    } {jobs=[]job{bad};if _,err:=resolveJobID(context.Background(),env,"fixture-token",client);err==nil{t.Fatal("changed job accepted")}}
    jobs=[]job{good,good};total=2
    if _,err:=resolveJobID(context.Background(),env,"fixture-token",client);err==nil{t.Fatal("duplicate job accepted")}
    total=1
    if _,err:=resolveJobID(context.Background(),env,"fixture-token",client);err==nil{t.Fatal("partial listing accepted")}
    ctx,cancel:=context.WithCancel(context.Background());cancel();before:=calls
    if _,err:=resolveJobID(ctx,env,"fixture-token",client);err==nil||calls!=before{t.Fatal("cancel did not stop request")}
}

func TestPEArchitectureIsChecked(t *testing.T) {
    raw:=make([]byte,80);copy(raw,"MZ");binary.LittleEndian.PutUint32(raw[0x3c:],64);copy(raw[64:],"PE\x00\x00");binary.LittleEndian.PutUint16(raw[68:],0x8664)
    if !validRunnerPE(raw){t.Fatal("AMD64 fixture rejected")}
    binary.LittleEndian.PutUint16(raw[68:],0xaa64)
    if validRunnerPE(raw){t.Fatal("wrong architecture accepted")}
    binary.LittleEndian.PutUint32(raw[0x3c:],0xffffffff)
    if validRunnerPE(raw)||validRunnerPE(nil){t.Fatal("invalid PE bounds accepted")}
}

func TestArtifactResultDoesNotIssueLocalAuthority(t *testing.T) {
    raw,err:=json.Marshal(finalManifest{Domain:"task-runner-gha-capture-v1"});if err!=nil{t.Fatal(err)}
    var result map[string]any;if json.Unmarshal(raw,&result)!=nil{t.Fatal("manifest decode")}
    for _,key:=range []string{"data_access_granted","update_performed","send_allowed","source_quiescence_proved"} {if value,ok:=result[key].(bool);!ok||value{t.Fatalf("authority flag %s",key)}}
}

func TestCommandAcceptsNoCallerControlledInputs(t *testing.T) {
    if code:=run([]string{"--source","other"},io.Discard);code!=2{t.Fatal("caller source accepted")}
}
