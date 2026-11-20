package main

import (
    "archive/tar"
    "bytes"
    "context"
    "encoding/binary"
    "encoding/json"
    "errors"
    "io"
    "net/http"
    "os"
    "path"
    "path/filepath"
    "regexp"
    "runtime/debug"
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
        "GITHUB_WORKFLOW_REF":"AutoDevJapan/cc-pc-tools-rs/.github/workflows/guard-reader-capture.yml@refs/heads/main",
        "GITHUB_WORKFLOW_SHA":strings.Repeat("a",40), "GITHUB_SHA":strings.Repeat("a",40),
        "GITHUB_JOB":jobKey, "GITHUB_EVENT_NAME":"workflow_dispatch",
        "GITHUB_REF":"refs/heads/main", "RUNNER_OS":"Windows", "RUNNER_ARCH":"X64",
        "RUNNER_ENVIRONMENT":"github-hosted", "RUNNER_NAME":"owned-hosted-fixture",
        "RUNNER_TEMP":t.TempDir(), "GITHUB_WORKSPACE":t.TempDir(),
        "GITHUB_RUN_ID":"17", "GITHUB_RUN_ATTEMPT":"1", "READER_CAPTURE_CHECK_RUN_ID":"31",
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
        "READER_CAPTURE_CHECK_RUN_ID":"0",
    }
    for key,value:=range cases {t.Run(key,func(t *testing.T) {
        old:=values[key];values[key]=value
        defer func(){values[key]=old}()
        if _,err:=parseCaptureEnvironment(func(k string)string{return values[k]});err==nil {t.Fatal("changed purpose accepted")}
    })}
}

func readerGraph() []graphPackage {
    return []graphPackage{
        {ImportPath:"fmt",Standard:true,GoFiles:[]string{"print.go"}},
        {ImportPath:"example.test/dependency/pkg",
         Module:&graphModule{Path:"example.test/dependency",Version:"v1.2.3",Sum:"h1:zip",GoModSum:"h1:mod"},
         GoFiles:[]string{"pkg.go"},SFiles:[]string{"asm.s"},EmbedFiles:[]string{"data.bin"}},
        {ImportPath:readerModulePath,
         Module:&graphModule{Path:readerModulePath,Main:true},
         GoFiles:[]string{"main_windows.go"}},
    }
}
func readerLock() map[string]string {
    return map[string]string{
        "example.test/dependency v1.2.3":"h1:zip",
        "example.test/dependency v1.2.3/go.mod":"h1:mod",
    }
}
func graphRecords() []sourceRecord {
    return []sourceRecord{{Path:readerModuleDirectory+"/go.mod"},{Path:readerModuleDirectory+"/go.sum"},{Path:readerModuleDirectory+"/main_windows.go"}}
}
func graphBytes(t *testing.T,g []graphPackage) []byte {t.Helper();raw,err:=json.Marshal(g);if err!=nil{t.Fatal(err)};return raw}

func TestGraphAcceptsOnlyLockedThirdPartyModules(t *testing.T) {
    modules,err:=checkGraphInputs(graphBytes(t,readerGraph()),graphRecords(),readerLock())
    if err!=nil||len(modules)!=1||modules["example.test/dependency"]!="v1.2.3" {t.Fatalf("locked graph rejected: %v",err)}
    cases:=map[string]func([]graphPackage)[]graphPackage{
        "unlocked version":func(g []graphPackage)[]graphPackage{g[1].Module.Version="v1.2.4";return g},
        "unlocked module":func(g []graphPackage)[]graphPackage{g[1].Module.Path="example.test/other";g[1].ImportPath="example.test/other/pkg";return g},
        "zip checksum differs":func(g []graphPackage)[]graphPackage{g[1].Module.Sum="h1:other";return g},
        "go.mod checksum differs":func(g []graphPackage)[]graphPackage{g[1].Module.GoModSum="h1:other";return g},
        "unversioned dependency":func(g []graphPackage)[]graphPackage{g[1].Module.Version="";return g},
        "replacement":func(g []graphPackage)[]graphPackage{g[1].Module.Replace=&graphModule{Path:"example.test/replacement",Version:"v1.0.0"};return g},
        "dependency marked main":func(g []graphPackage)[]graphPackage{g[1].Module.Main=true;return g},
        "package outside its module":func(g []graphPackage)[]graphPackage{g[1].ImportPath="example.test/elsewhere";return g},
        "standard package with module":func(g []graphPackage)[]graphPackage{g[0].Module=&graphModule{Path:"std",Version:"v1.0.0"};return g},
        "package without module":func(g []graphPackage)[]graphPackage{g[1].Module=nil;return g},
        "conflicting dependency version":func(g []graphPackage)[]graphPackage{return append(g,graphPackage{ImportPath:"example.test/dependency/other",Module:&graphModule{Path:"example.test/dependency",Version:"v1.2.4"}})},
        "versioned application":func(g []graphPackage)[]graphPackage{g[2].Module.Version="v1.0.0";return g},
        "replaced application":func(g []graphPackage)[]graphPackage{g[2].Module.Replace=&graphModule{Path:"other"};return g},
        "application subpackage":func(g []graphPackage)[]graphPackage{g[2].ImportPath=readerModulePath+"/sub";return g},
        "missing application":func(g []graphPackage)[]graphPackage{return g[:2]},
        "duplicate application":func(g []graphPackage)[]graphPackage{return append(g,g[2])},
        "no dependency":func(g []graphPackage)[]graphPackage{return []graphPackage{g[0],g[2]}},
        "unknown source":func(g []graphPackage)[]graphPackage{g[2].GoFiles=[]string{"late.go"};return g},
        "traversal":func(g []graphPackage)[]graphPackage{g[2].GoFiles=[]string{"../main_windows.go"};return g},
        "case duplicate":func(g []graphPackage)[]graphPackage{g[2].GoFiles=[]string{"main_windows.go","MAIN_WINDOWS.go"};return g},
        "test file":func(g []graphPackage)[]graphPackage{g[2].GoFiles=[]string{"main_windows_test.go"};return g},
        "no application source":func(g []graphPackage)[]graphPackage{g[2].GoFiles=nil;return g},
        "application cgo":func(g []graphPackage)[]graphPackage{g[2].CgoFiles=[]string{"extra.go"};return g},
        "application assembly":func(g []graphPackage)[]graphPackage{g[2].SFiles=[]string{"extra.s"};return g},
        "application syso":func(g []graphPackage)[]graphPackage{g[2].SysoFiles=[]string{"extra.syso"};return g},
        "application embed":func(g []graphPackage)[]graphPackage{g[2].EmbedFiles=[]string{"extra.bin"};return g},
    }
    for name,change:=range cases {t.Run(name,func(t *testing.T) {
        if _,err:=checkGraphInputs(graphBytes(t,change(readerGraph())),graphRecords(),readerLock());err==nil {t.Fatal("uncaptured or unlocked input accepted")}
    })}
    // Optional listing checksums may be absent; the binary check then binds them.
    omitted:=readerGraph();omitted[1].Module.Sum="";omitted[1].Module.GoModSum=""
    if _,err:=checkGraphInputs(graphBytes(t,omitted),graphRecords(),readerLock());err!=nil {t.Fatalf("absent listing checksums rejected: %v",err)}
    if _,err:=checkGraphInputs(graphBytes(t,readerGraph()),graphRecords()[1:],readerLock());err==nil {t.Fatal("missing go.mod accepted")}
    if _,err:=checkGraphInputs(graphBytes(t,readerGraph()),append([]sourceRecord(nil),graphRecords()[0],graphRecords()[2]),readerLock());err==nil {t.Fatal("missing go.sum accepted")}
    if _,err:=checkGraphInputs(graphBytes(t,readerGraph()),graphRecords(),nil);err==nil {t.Fatal("empty lock accepted")}
    if _,err:=checkGraphInputs(graphBytes(t,readerGraph()),append(graphRecords(),graphRecords()[0]),readerLock());err==nil {t.Fatal("duplicate input record accepted")}
}

func TestBinaryDependenciesMatchTheLockedGraph(t *testing.T) {
    modules:=map[string]string{"example.test/dependency":"v1.2.3"}
    good:=func() []*debug.Module {return []*debug.Module{{Path:"example.test/dependency",Version:"v1.2.3",Sum:"h1:zip"}}}
    if err:=checkBinaryDependencies(good(),modules,readerLock());err!=nil {t.Fatalf("matching binary rejected: %v",err)}
    cases:=map[string]func([]*debug.Module)[]*debug.Module{
        "no modules":func(d []*debug.Module)[]*debug.Module{return nil},
        "missing module":func(d []*debug.Module)[]*debug.Module{return []*debug.Module{}},
        "extra module":func(d []*debug.Module)[]*debug.Module{return append(d,&debug.Module{Path:"example.test/extra",Version:"v1.0.0",Sum:"h1:zip"})},
        "other version":func(d []*debug.Module)[]*debug.Module{d[0].Version="v1.2.4";return d},
        "other checksum":func(d []*debug.Module)[]*debug.Module{d[0].Sum="h1:other";return d},
        "missing checksum":func(d []*debug.Module)[]*debug.Module{d[0].Sum="";return d},
        "replacement":func(d []*debug.Module)[]*debug.Module{d[0].Replace=&debug.Module{Path:"example.test/replacement"};return d},
        "nil entry":func(d []*debug.Module)[]*debug.Module{return []*debug.Module{nil}},
        "duplicate entry":func(d []*debug.Module)[]*debug.Module{return append(d,d[0])},
    }
    for name,change:=range cases {t.Run(name,func(t *testing.T) {
        if err:=checkBinaryDependencies(change(good()),modules,readerLock());err==nil {t.Fatal("binary with another module set accepted")}
    })}
}

func TestCanonicalGraphRejectsAmbiguityAndErrors(t *testing.T) {
    var joined bytes.Buffer
    for _,pkg:=range readerGraph(){raw,_:=json.Marshal(pkg);joined.Write(raw);joined.WriteByte('\n')}
    valid,err:=canonicalGraph(joined.Bytes(),"fixed-root");if err!=nil {t.Fatal(err)}
    if _,err=checkGraphInputs(valid,graphRecords(),readerLock());err!=nil{t.Fatal(err)}
    for _,raw:=range [][]byte{
        nil, []byte("not JSON"), append(append([]byte(nil),joined.Bytes()...),[]byte("{} trailing")...),
        []byte("{\"ImportPath\":\"fmt\"}\n{\"ImportPath\":\"fmt\"}"),
        []byte("{\"ImportPath\":\"fmt\",\"Error\":{}}"),
        []byte("{\"ImportPath\":\"fmt\",\"DepsErrors\":[{}]}"),
    } {if _,err=canonicalGraph(raw,"fixed-root");err==nil{t.Fatal("ambiguous graph accepted")}}
}

func TestGoSumLockParsing(t *testing.T) {
    lock,err:=parseGoSum([]byte("example.test/a v1.0.0 h1:one\nexample.test/a v1.0.0/go.mod h1:two\r\n\n"))
    if err!=nil||len(lock)!=2||lock["example.test/a v1.0.0"]!="h1:one"||lock["example.test/a v1.0.0/go.mod"]!="h1:two" {t.Fatalf("valid lock rejected: %v",err)}
    if repeat,err:=parseGoSum([]byte("a v1.0.0 h1:x\na v1.0.0 h1:x\n"));err!=nil||len(repeat)!=1 {t.Fatal("identical repeat rejected")}
    for name,raw:=range map[string][]byte{
        "empty":nil,
        "blank lines only":[]byte("\n\n"),
        "two fields":[]byte("example.test/a v1.0.0\n"),
        "four fields":[]byte("a v1.0.0 h1:x extra\n"),
        "other hash kind":[]byte("a v1.0.0 sha256:x\n"),
        "bare hash prefix":[]byte("a v1.0.0 h1:\n"),
        "conflicting repeat":[]byte("a v1.0.0 h1:x\na v1.0.0 h1:y\n"),
        "NUL byte":[]byte("a v1.0.0 h1:x\x00\n"),
    } {t.Run(name,func(t *testing.T) {if _,err:=parseGoSum(raw);err==nil {t.Fatal("malformed lock accepted")}})}
}

func TestLockIsReadFromTheInventoriedFile(t *testing.T) {
    root:=t.TempDir();dir:=filepath.Join(root,filepath.FromSlash(readerModuleDirectory))
    if err:=os.MkdirAll(dir,0700);err!=nil{t.Fatal(err)}
    raw:=[]byte("example.test/a v1.0.0 h1:one\nexample.test/a v1.0.0/go.mod h1:two\n")
    if err:=os.WriteFile(filepath.Join(dir,"go.sum"),raw,0600);err!=nil{t.Fatal(err)}
    record:=sourceRecord{Path:readerModuleDirectory+"/go.sum",Size:int64(len(raw)),SHA:hashBytes(raw),Blob:gitBlob(raw)}
    if lock,err:=readLock(root,[]sourceRecord{record});err!=nil||len(lock)!=2 {t.Fatalf("inventoried lock rejected: %v",err)}
    for name,records:=range map[string][]sourceRecord{
        "no record":nil,
        "duplicate record":{record,record},
        "other SHA-256":{{Path:record.Path,Size:record.Size,SHA:hashBytes([]byte("other")),Blob:record.Blob}},
        "other blob":{{Path:record.Path,Size:record.Size,SHA:record.SHA,Blob:gitBlob([]byte("other"))}},
        "other size":{{Path:record.Path,Size:record.Size+1,SHA:record.SHA,Blob:record.Blob}},
    } {t.Run(name,func(t *testing.T) {if _,err:=readLock(root,records);err==nil {t.Fatal("unbound lock accepted")}})}
    if err:=os.WriteFile(filepath.Join(dir,"go.sum"),append(append([]byte(nil),raw...),'x'),0600);err!=nil{t.Fatal(err)}
    if _,err:=readLock(root,[]sourceRecord{record});err==nil {t.Fatal("changed lock accepted")}
}

func TestFailureMarksTheCallSiteOnly(t *testing.T) {
    err:=fail()
    if !errors.Is(err,errCapture)||!regexp.MustCompile("^guard_reader_capture_unavailable@[1-9][0-9]*$").MatchString(err.Error()) {t.Fatalf("failure marker changed: %v",err)}
}

func TestFailureTailIsBoundedAndPrintable(t *testing.T) {
    raw:=append(bytes.Repeat([]byte("a"),2000),[]byte("\x00\xff\tend")...)
    got:=tailOf(errors.New("exit status 1"),raw)
    if len(got)>1300||strings.ContainsAny(got,"\x00\xff\t\r")||!strings.HasPrefix(got,"exit status 1\n")||!strings.HasSuffix(got,"???end") {t.Fatalf("failure tail not bounded or printable: %d",len(got))}
    if tailOf(nil,nil)!="" {t.Fatal("empty failure produced a remark")}
    if inner(errCapture)!=errCapture||!errors.Is(inner(fail()),errCapture)||inner(errors.New("other"))!=errCapture {t.Fatal("inner changed the failure vocabulary")}
}

func TestFixedSourceIdentityIsWellFormed(t *testing.T) {
    for _,value:=range []string{readerSource,readerTree,readerModuleTree} {if !lowerHex(value,40) {t.Fatal("source identity is not a full Git object ID")}}
    if readerInputCount<1||readerInputCount>4096||!strings.HasPrefix(readerModulePath,"github.com/AutoDevJapan/cc-pc-tools-rs/")||!strings.HasSuffix(readerModulePath,"/"+readerModuleDirectory)||executableName!=path.Base(readerModuleDirectory)+".exe"||executableLimit<=0 {t.Fatal("fixed module identity is inconsistent")}
}

func sourceFixture(t *testing.T,files map[string][]byte)([]sourceRecord,[]byte,[]byte) {
    t.Helper()
    names:=[]string{readerModuleDirectory+"/go.mod",readerModuleDirectory+"/main_windows.go"}
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
    original:=map[string][]byte{readerModuleDirectory+"/go.mod":[]byte("module fixture\n"),readerModuleDirectory+"/main_windows.go":[]byte("package main\n")}
    records,tree,archive:=sourceFixture(t,original)
    if err:=verifySourceTree(records,tree,archive,readerSource);err!=nil{t.Fatal(err)}
    changed:=map[string][]byte{readerModuleDirectory+"/go.mod":original[readerModuleDirectory+"/go.mod"],readerModuleDirectory+"/main_windows.go":append([]byte(nil),original[readerModuleDirectory+"/main_windows.go"]...)}
    changed[readerModuleDirectory+"/main_windows.go"][0]^=1
    if bytes.Equal(original[readerModuleDirectory+"/main_windows.go"],changed[readerModuleDirectory+"/main_windows.go"])||hashBytes(original[readerModuleDirectory+"/main_windows.go"])==hashBytes(changed[readerModuleDirectory+"/main_windows.go"]){t.Fatal("mutation was vacuous")}
    _,_,validChangedArchive:=sourceFixture(t,changed)
    if err:=verifySourceTree(records,tree,validChangedArchive,readerSource);err==nil{t.Fatal("changed source accepted")}
    if err:=verifySourceTree(records,tree[:len(tree)-1],archive,readerSource);err==nil{t.Fatal("truncated tree accepted")}
    duplicate:=append(append([]byte(nil),tree...),tree...)
    if err:=verifySourceTree(records,duplicate,archive,readerSource);err==nil{t.Fatal("duplicate tree accepted")}
    linkTree:=bytes.Replace(tree,[]byte("100644"),[]byte("120000"),1)
    if err:=verifySourceTree(records,linkTree,archive,readerSource);err==nil{t.Fatal("link mode accepted")}
}

func TestInventoryRequiresCompleteFixedNamespace(t *testing.T) {
    root:=t.TempDir();dir:=filepath.Join(root,filepath.FromSlash(readerModuleDirectory))
    if err:=os.MkdirAll(dir,0700);err!=nil{t.Fatal(err)}
    for i:=0;i<readerInputCount;i++ {if err:=os.WriteFile(filepath.Join(dir,"fixture-"+strconv.Itoa(i)+".go"),[]byte("package main\n"),0600);err!=nil{t.Fatal(err)}}
    records,err:=captureInventory(root);if err!=nil||len(records)!=readerInputCount{t.Fatalf("complete fixture: %v",err)}
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
    good:=job{ID:23,RunID:17,HeadSHA:env.workflowSource,Name:"Guard reader software artifact",Status:"in_progress",CheckRunURL:"https://api.github.com/repos/AutoDevJapan/cc-pc-tools-rs/check-runs/31"}
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
    if !validReaderPE(raw){t.Fatal("AMD64 fixture rejected")}
    binary.LittleEndian.PutUint16(raw[68:],0xaa64)
    if validReaderPE(raw){t.Fatal("wrong architecture accepted")}
    binary.LittleEndian.PutUint32(raw[0x3c:],0xffffffff)
    if validReaderPE(raw)||validReaderPE(nil){t.Fatal("invalid PE bounds accepted")}
}

func TestArtifactResultDoesNotIssueLocalAuthority(t *testing.T) {
    raw,err:=json.Marshal(finalManifest{Domain:"guard-reader-gha-capture-v1"});if err!=nil{t.Fatal(err)}
    var result map[string]any;if json.Unmarshal(raw,&result)!=nil{t.Fatal("manifest decode")}
    for _,key:=range []string{"data_access_granted","update_performed","send_allowed","source_quiescence_proved"} {if value,ok:=result[key].(bool);!ok||value{t.Fatalf("authority flag %s",key)}}
}

func TestCommandAcceptsNoCallerControlledInputs(t *testing.T) {
    if code:=run([]string{"--source","other"},io.Discard);code!=2{t.Fatal("caller source accepted")}
}

type captureJobsTrackedBody struct {
	*bytes.Reader
	closed bool
}

func (body *captureJobsTrackedBody) Close() error {
	body.closed = true
	return nil
}

func captureJobsConsumerFixture(t *testing.T, headers http.Header, document string) (int64, error) {
	t.Helper()
	body := &captureJobsTrackedBody{Reader: bytes.NewReader([]byte(document))}
	calls := 0
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		if request.Method != http.MethodGet ||
			request.URL.String() != "https://api.github.com/repos/AutoDevJapan/cc-pc-tools-rs/actions/runs/17/attempts/1/jobs?per_page=100" ||
			request.Header.Get("Authorization") != "Bearer fixture-token" {
			t.Fatal("consumer changed the fixed authenticated request")
		}
		return &http.Response{StatusCode: http.StatusOK, Header: headers, Body: body}, nil
	})}
	environment := captureEnv{workflowSource: strings.Repeat("a", 40), runID: 17, attempt: 1, checkRunID: 31}
	id, err := resolveJobID(context.Background(), environment, "fixture-token", client)
	if calls != 1 || !body.closed {
		t.Fatal("consumer retried a page or did not close its response")
	}
	return id, err
}

func captureJobsOriginalDocument() (string, string) {
	job := "{\"id\":23,\"run_id\":17,\"head_sha\":\"" + strings.Repeat("a", 40) +
		"\",\"name\":\"Guard reader software artifact\",\"status\":\"in_progress\"," +
		"\"check_run_url\":\"https://api.github.com/repos/AutoDevJapan/cc-pc-tools-rs/check-runs/31\"}"
	return "{\"total_count\":1,\"jobs\":[" + job + "]}", job
}

func TestResolveJobIDSinglePageNoticeAndLinkFailures(t *testing.T) {
	document, _ := captureJobsOriginalDocument()
	notice := "<https://docs.github.com/en/rest/about-the-rest-api/api-versions>; rel=\"deprecation\"; type=\"text/html\""
	next := "<https://api.github.com/repos/AutoDevJapan/cc-pc-tools-rs/actions/runs/17/attempts/1/jobs?page=2>; rel=\"next\""
	last := strings.Replace(next, "rel=\"next\"", "rel=\"last\"", 1)
	for _, test := range []struct {
		name    string
		headers http.Header
		allowed bool
	}{
		{"no-link", nil, true},
		{"other-additive-header", http.Header{"X-Api-Notice": {"kept"}}, true},
		{"deprecation", http.Header{"Link": {notice}}, true},
		{"lowercase-header", http.Header{"link": {notice}}, true},
		{"notice-whitespace", http.Header{"Link": {" \t" + notice + "\t "}}, true},
		{"next", http.Header{"Link": {next}}, false},
		{"last", http.Header{"Link": {last}}, false},
		{"unknown-relation", http.Header{"Link": {strings.Replace(notice, "deprecation", "other", 1)}}, false},
		{"unknown-host", http.Header{"Link": {strings.Replace(notice, "docs.github.com", "example.invalid", 1)}}, false},
		{"other-media-type", http.Header{"Link": {strings.Replace(notice, "text/html", "application/json", 1)}}, false},
		{"multiple-header-values", http.Header{"Link": {notice, notice}}, false},
		{"case-alias-headers", http.Header{"Link": {notice}, "link": {notice}}, false},
		{"mixed-pagination-values", http.Header{"Link": {notice, next}}, false},
		{"mixed-pagination-comma", http.Header{"Link": {notice + ", " + next}}, false},
		{"multiple-comma", http.Header{"Link": {notice + ", " + notice}}, false},
		{"empty-field", http.Header{"Link": {""}}, false},
		{"empty-header-values", http.Header{"Link": {}}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			id, err := captureJobsConsumerFixture(t, test.headers, document)
			if test.allowed {
				if err != nil || id != 23 {
					t.Fatal("normal single-page response was rejected")
				}
			} else if err == nil || id != 0 {
				t.Fatal("ambiguous or paginated response was accepted")
			}
		})
	}
}

func TestResolveJobIDCriticalJSONNamesAndCardinality(t *testing.T) {
	document, job := captureJobsOriginalDocument()
	for _, positive := range []struct {
		name string
		raw  string
	}{
		{"normal", document},
		{"additive-fields", "{\"future\":{\"nested\":[{\"id\":999,\"id\":998}],\"name\":null},\"total_count\":1,\"jobs\":[" +
			strings.TrimSuffix(job, "}") + ",\"labels\":[\"hosted\"],\"runner_id\":42,\"extra\":{\"ID\":7}}]}"},
		{"escaped-exact-critical-name", strings.Replace(document, "\"id\":", "\"\\u0069d\":", 1)},
	} {
		t.Run(positive.name, func(t *testing.T) {
			if id, err := captureJobsConsumerFixture(t, nil, positive.raw); err != nil || id != 23 {
				t.Fatal("normal critical fields with compatible additions were rejected")
			}
		})
	}
	for _, field := range []struct {
		name, value string
		envelope    bool
	}{
		{"total_count", "1", true},
		{"jobs", "[" + job + "]", true},
		{"id", "23", false},
		{"run_id", "17", false},
		{"head_sha", "\"" + strings.Repeat("a", 40) + "\"", false},
		{"name", "\"Guard reader software artifact\"", false},
		{"status", "\"in_progress\"", false},
		{"check_run_url", "\"https://api.github.com/repos/AutoDevJapan/cc-pc-tools-rs/check-runs/31\"", false},
	} {
		pair := "\"" + field.name + "\":" + field.value
		container := job
		if field.envelope {
			container = document
		}
		if strings.Count(container, pair) != 1 {
			t.Fatal("critical-field fixture is not an exact mutation")
		}
		early := "999"
		if field.name == "jobs" {
			early = "[]"
		} else if field.name != "id" && field.name != "run_id" && field.name != "total_count" {
			early = "\"earlier\""
		}
		escapedName := "\"\\u00" + strconv.FormatInt(int64(field.name[0]), 16) + field.name[1:] + "\""
		for _, mutation := range []struct {
			name, replacement string
		}{
			{"duplicate-different-first", "\"" + field.name + "\":" + early + "," + pair},
			{"duplicate-identical", pair + "," + pair},
			{"case-alias-only", "\"" + strings.ToUpper(field.name) + "\":" + field.value},
			{"case-alias-before", "\"" + strings.ToUpper(field.name) + "\":" + early + "," + pair},
			{"case-alias-after", pair + ",\"" + strings.ToUpper(field.name) + "\":" + early},
			{"escaped-duplicate", escapedName + ":" + early + "," + pair},
			{"null", "\"" + field.name + "\":null"},
			{"wrong-type", "\"" + field.name + "\":{}"},
		} {
			t.Run(field.name+"/"+mutation.name, func(t *testing.T) {
				changed := strings.Replace(container, pair, mutation.replacement, 1)
				if changed == container {
					t.Fatal("critical-field mutation did not change bytes")
				}
				input := changed
				if !field.envelope {
					input = "{\"total_count\":1,\"jobs\":[" + changed + "]}"
				}
				if id, err := captureJobsConsumerFixture(t, nil, input); err == nil || id != 0 {
					t.Fatal("ambiguous critical response was accepted")
				}
			})
		}
		t.Run(field.name+"/missing", func(t *testing.T) {
			var fields map[string]json.RawMessage
			if json.Unmarshal([]byte(container), &fields) != nil {
				t.Fatal("invalid fixture")
			}
			delete(fields, field.name)
			changed, err := json.Marshal(fields)
			if err != nil {
				t.Fatal("fixture encode")
			}
			input := string(changed)
			if !field.envelope {
				input = "{\"total_count\":1,\"jobs\":[" + input + "]}"
			}
			if id, err := captureJobsConsumerFixture(t, nil, input); err == nil || id != 0 {
				t.Fatal("missing mandatory field was accepted")
			}
		})
	}
	for _, bad := range []struct {
		name, raw string
	}{
		{"duplicate-selected-job", "{\"total_count\":2,\"jobs\":[" + job + "," + job + "]}"},
		{"trailing-document", document + "{}"},
		{"null-envelope", "null"},
		{"array-envelope", "[" + document + "]"},
		{"null-job", "{\"total_count\":1,\"jobs\":[null]}"},
		{"object-jobs", "{\"total_count\":1,\"jobs\":{}}"},
		{"count-mismatch", strings.Replace(document, "\"total_count\":1", "\"total_count\":2", 1)},
		{"too-many-jobs", "{\"total_count\":101,\"jobs\":[" + strings.TrimSuffix(strings.Repeat(job+",", 101), ",") + "]}"},
	} {
		t.Run(bad.name, func(t *testing.T) {
			if id, err := captureJobsConsumerFixture(t, nil, bad.raw); err == nil || id != 0 {
				t.Fatal("incomplete or ambiguous job list was accepted")
			}
		})
	}
}

func TestResolveJobIDRejectsInvalidFieldsInNonselectedJobs(t *testing.T) {
	_, selected := captureJobsOriginalDocument()
	other := strings.Replace(selected, "\"id\":23", "\"id\":24", 1)
	other = strings.Replace(other, "\"name\":\"Guard reader software artifact\"", "\"name\":\"Other job\"", 1)
	baseline := "{\"total_count\":2,\"jobs\":[" + other + "," + selected + "]}"
	if id, err := captureJobsConsumerFixture(t, nil, baseline); err != nil || id != 23 {
		t.Fatal("valid nonselected row changed the selected job")
	}
	for _, key := range []string{"id", "run_id", "head_sha", "name", "status", "check_run_url"} {
		for _, remove := range []bool{false, true} {
			name := key + "/null"
			if remove {
				name = key + "/missing"
			}
			t.Run(name, func(t *testing.T) {
				var fields map[string]json.RawMessage
				if json.Unmarshal([]byte(other), &fields) != nil {
					t.Fatal("invalid nonselected-row fixture")
				}
				if remove {
					delete(fields, key)
				} else {
					fields[key] = json.RawMessage("null")
				}
				changed, err := json.Marshal(fields)
				if err != nil {
					t.Fatal("nonselected-row encode")
				}
				document := "{\"total_count\":2,\"jobs\":[" + string(changed) + "," + selected + "]}"
				if document == baseline || !json.Valid([]byte(document)) {
					t.Fatal("nonselected-row mutation was vacuous or invalid JSON")
				}
				if id, err := captureJobsConsumerFixture(t, nil, document); err == nil || id != 0 {
					t.Fatal("invalid critical field was hidden in an unselected row")
				}
			})
		}
	}
}

func TestResolveJobIDRejectsBeforeLossyStructProjection(t *testing.T) {
	document, _ := captureJobsOriginalDocument()
	for _, field := range []string{"id", "ID"} {
		t.Run(field, func(t *testing.T) {
			mutated := strings.Replace(document, "\"id\":23", "\""+field+"\":999,\"id\":23", 1)
			if mutated == document || !json.Valid([]byte(mutated)) {
				t.Fatal("projection counter was vacuous or invalid JSON")
			}
			var legacy struct {
				Jobs []struct {
					ID int64 `json:"id"`
				} `json:"jobs"`
			}
			if json.Unmarshal([]byte(mutated), &legacy) != nil ||
				len(legacy.Jobs) != 1 || legacy.Jobs[0].ID != 23 {
				t.Fatal("counter did not reach the standard struct-overwrite boundary")
			}
			if id, err := captureJobsConsumerFixture(t, nil, mutated); err == nil || id != 0 {
				t.Fatal("struct projection hid critical field ambiguity")
			}
		})
	}
}

func TestResolveJobIDChecksEveryRowIDBeforeSelection(t *testing.T) {
	_, selected := captureJobsOriginalDocument()
	change := func(row, old, replacement string) string {
		t.Helper()
		if old == replacement || strings.Count(row, old) != 1 {
			t.Fatal("row-identity fixture mutation was not exact")
		}
		changed := strings.Replace(row, old, replacement, 1)
		if changed == row || !json.Valid([]byte(changed)) {
			t.Fatal("row-identity fixture mutation was vacuous or invalid JSON")
		}
		return changed
	}
	otherSameID := change(selected, "\"name\":\"Guard reader software artifact\"", "\"name\":\"Other job\"")
	otherSameID = change(otherSameID, "check-runs/31", "check-runs/32")
	other := change(otherSameID, "\"id\":23", "\"id\":24")
	third := change(other, "\"id\":24", "\"id\":25")
	conflictingOther := change(other, "\"name\":\"Other job\"", "\"name\":\"Another job\"")
	conflictingOther = change(conflictingOther, "check-runs/32", "check-runs/33")
	withAdditions := strings.TrimSuffix(selected, "}") + ",\"future\":{\"id\":23,\"id\":23},\"labels\":[\"hosted\"]}"
	otherWithAdditions := strings.TrimSuffix(other, "}") + ",\"future\":{\"id\":23},\"runner_id\":null}"
	for _, test := range []struct {
		name    string
		rows    []string
		allowed bool
	}{
		{"one-selected-row", []string{selected}, true},
		{"distinct-selected-first", []string{selected, other}, true},
		{"distinct-selected-last", []string{other, selected}, true},
		{"distinct-three-rows", []string{other, selected, third}, true},
		{"positive-max-int64", []string{selected, change(other, "\"id\":24", "\"id\":9223372036854775807")}, true},
		{"unknown-additive-fields", []string{withAdditions, otherWithAdditions}, true},
		{"conflicting-selected-id-first", []string{selected, otherSameID}, false},
		{"conflicting-selected-id-last", []string{otherSameID, selected}, false},
		{"identical-selected-row", []string{selected, selected}, false},
		{"identical-nonselected-row", []string{selected, other, other}, false},
		{"conflicting-nonselected-id", []string{other, conflictingOther, selected}, false},
		{"zero-nonselected-first", []string{change(other, "\"id\":24", "\"id\":0"), selected}, false},
		{"zero-nonselected-last", []string{selected, change(other, "\"id\":24", "\"id\":0")}, false},
		{"negative-nonselected-first", []string{change(other, "\"id\":24", "\"id\":-1"), selected}, false},
		{"negative-nonselected-last", []string{selected, change(other, "\"id\":24", "\"id\":-1")}, false},
		{"minimum-int64-nonselected", []string{selected, change(other, "\"id\":24", "\"id\":-9223372036854775808")}, false},
		{"zero-selected", []string{change(selected, "\"id\":23", "\"id\":0"), other}, false},
		{"negative-selected", []string{change(selected, "\"id\":23", "\"id\":-1"), other}, false},
		{"overflow-nonselected", []string{selected, change(other, "\"id\":24", "\"id\":9223372036854775808")}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			document := "{\"total_count\":" + strconv.Itoa(len(test.rows)) + ",\"jobs\":[" + strings.Join(test.rows, ",") + "]}"
			if !json.Valid([]byte(document)) {
				t.Fatal("row-identity page fixture was invalid JSON")
			}
			id, err := captureJobsConsumerFixture(t, nil, document)
			if test.allowed {
				if err != nil || id != 23 {
					t.Fatal("valid distinct job identities were rejected")
				}
			} else if err == nil || id != 0 {
				t.Fatal("a nonpositive or reused page job identity was accepted")
			}
		})
	}
}
