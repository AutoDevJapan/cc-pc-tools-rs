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
		"\",\"name\":\"Task runner software artifact\",\"status\":\"in_progress\"," +
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
		{"name", "\"Task runner software artifact\"", false},
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
	other = strings.Replace(other, "\"name\":\"Task runner software artifact\"", "\"name\":\"Other job\"", 1)
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
