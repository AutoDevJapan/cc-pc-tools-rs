//go:build windows

package main

import (
    "archive/zip"
    "bytes"
    "context"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "net/http"
    "strings"
    "sync"
    "testing"
    "time"
)

type ghaCurrentTestFixture struct {
    client *http.Client
    endpoints map[string][]byte
    records []ghaGuardSourceRecord
    policy ghaFreshProducerContract
    producer,head,tree string
    refReads,listReads int
    moveAtSecondRead,changeListing bool
}

func ghaCurrentTestFixtureFor(t *testing.T,mode string) *ghaCurrentTestFixture {
    t.Helper()
    f:=&ghaCurrentTestFixture{endpoints:map[string][]byte{},producer:ghaFreshTestRevision,head:ghaFreshTestRevision}
    encode:=func(value any) []byte {raw,err:=json.Marshal(value);if err!=nil{t.Fatal(err)};return raw}
    mkTree:=func(entries []ghaGuardGitTreeEntry) string {
        id,raw:=ghaGuardTreeTestMakeTree(t,entries);f.endpoints["/git/trees/"+id]=raw;return id
    }
    mkFile:=func(name string,raw []byte) (ghaFreshContractFile,ghaGuardGitTreeEntry) {
        n:=int64(len(raw));id:=ghaGuardGitObjectID("blob",raw)
        f.endpoints["/git/blobs/"+id]=append([]byte(nil),raw...)
        return ghaFreshContractFile{name:name,blob:id,sha:digest(raw),size:n},ghaGuardGitTreeEntry{Path:name,Mode:"100644",Type:"blob",SHA:id,Size:&n}
    }
    wf,wfEntry:=mkFile("unattended-readiness.yml",[]byte("owned workflow"))
    main,mainEntry:=mkFile("main.go",[]byte("owned capture main"))
    mod,modEntry:=mkFile("go.mod",[]byte("owned capture module"))
    f.policy=ghaFreshProducerContract{workflow:wf,main:main,mod:mod}
    if mode=="wrong-workflow" {_,wfEntry=mkFile("unattended-readiness.yml",[]byte("wrong workflow"))}
    if mode=="wrong-capture" {_,mainEntry=mkFile("main.go",[]byte("wrong capture"))}
    capture:=[]ghaGuardGitTreeEntry{mainEntry,modEntry}
    if mode=="capture-extra" {_,entry:=mkFile("extra.go",[]byte("extra package input"));capture=append(capture,entry)}
    if mode=="inert-test" {_,entry:=mkFile("extra_test.go",[]byte("test only"));capture=append(capture,entry)}
    captureID:=mkTree(capture)
    workflowID:=mkTree([]ghaGuardGitTreeEntry{wfEntry})
    githubID:=mkTree([]ghaGuardGitTreeEntry{{Path:"workflows",Mode:"040000",Type:"tree",SHA:workflowID}})
    guard:=make([]ghaGuardGitTreeEntry,0,248)
    for i:=0;i<248;i++ {
        name:=fmt.Sprintf("input-%03d.go",i);raw:=[]byte(fmt.Sprintf("owned-input-%03d",i))
        size:=int64(len(raw));id:=ghaGuardGitObjectID("blob",raw);f.endpoints["/git/blobs/"+id]=append([]byte(nil),raw...)
        fileMode:="100644";if mode=="wrong-mode"&&i==0{fileMode="100755"}
        guard=append(guard,ghaGuardGitTreeEntry{Path:name,Mode:fileMode,Type:"blob",SHA:id,Size:&size})
        f.records=append(f.records,ghaGuardSourceRecord{Path:"repair-guard/"+name,Size:size,SHA:digest(raw),Blob:id})
    }
    raw:=[]byte("owned-replace-module");size:=int64(len(raw));id:=ghaGuardGitObjectID("blob",raw)
    f.endpoints["/git/blobs/"+id]=append([]byte(nil),raw...)
    f.records=append(f.records,ghaGuardSourceRecord{Path:"tools/civ6-ci-contract/go.mod",Size:size,SHA:digest(raw),Blob:id})
    contractID:=mkTree([]ghaGuardGitTreeEntry{{Path:"go.mod",Mode:"100644",Type:"blob",SHA:id,Size:&size}})
    toolsID:=mkTree([]ghaGuardGitTreeEntry{{Path:"civ6-ci-contract",Mode:"040000",Type:"tree",SHA:contractID},{Path:"guard-gha-capture",Mode:"040000",Type:"tree",SHA:captureID}})
    root:=[]ghaGuardGitTreeEntry{{Path:".github",Mode:"040000",Type:"tree",SHA:githubID},{Path:"repair-guard",Mode:"040000",Type:"tree",SHA:mkTree(guard)},{Path:"tools",Mode:"040000",Type:"tree",SHA:toolsID}}
    if mode=="workspace" {_,entry:=mkFile("go.work",[]byte("go 1.27"));root=append(root,entry)}
    // Valid Git trees can contain case-fold aliases which collide on Windows.
    // Keep the reviewed lowercase tree unchanged in each negative control.
    switch mode {
    case "alias-root-tools":root=append(root,ghaGuardGitTreeEntry{Path:"Tools",Mode:"040000",Type:"tree",SHA:toolsID})
    case "alias-root-github":root=append(root,ghaGuardGitTreeEntry{Path:".GitHub",Mode:"040000",Type:"tree",SHA:githubID})
    case "alias-root-guard":root=append(root,ghaGuardGitTreeEntry{Path:"Repair-Guard",Mode:"040000",Type:"tree",SHA:root[1].SHA})
    case "alias-workflows":root[0].SHA=mkTree([]ghaGuardGitTreeEntry{{Path:"workflows",Mode:"040000",Type:"tree",SHA:workflowID},{Path:"Workflows",Mode:"040000",Type:"tree",SHA:workflowID}})
    case "alias-capture":root[2].SHA=mkTree([]ghaGuardGitTreeEntry{{Path:"civ6-ci-contract",Mode:"040000",Type:"tree",SHA:contractID},{Path:"guard-gha-capture",Mode:"040000",Type:"tree",SHA:captureID},{Path:"Guard-Gha-Capture",Mode:"040000",Type:"tree",SHA:captureID}})
    case "alias-contract":root[2].SHA=mkTree([]ghaGuardGitTreeEntry{{Path:"civ6-ci-contract",Mode:"040000",Type:"tree",SHA:contractID},{Path:"Civ6-Ci-Contract",Mode:"040000",Type:"tree",SHA:contractID},{Path:"guard-gha-capture",Mode:"040000",Type:"tree",SHA:captureID}})
    case "alias-workflow-file":
        alias:=wfEntry;alias.Path="Unattended-Readiness.yml"
        other:=mkTree([]ghaGuardGitTreeEntry{wfEntry,alias})
        root[0].SHA=mkTree([]ghaGuardGitTreeEntry{{Path:"workflows",Mode:"040000",Type:"tree",SHA:other}})
    case "unrelated-sibling":_,entry:=mkFile("tools-extra.txt",[]byte("inert"));root=append(root,entry)
    }
    producerTree:=mkTree(root);f.tree=producerTree
    if mode=="current-changed"||mode=="current-workflow"||mode=="unrelated-main" {
        current:=append([]ghaGuardGitTreeEntry(nil),root...)
        switch mode {
        case "current-changed":
            changed:=append([]ghaGuardGitTreeEntry(nil),guard...)
            _,entry:=mkFile("input-000.go",[]byte("changed"));changed[0]=entry;current[1].SHA=mkTree(changed)
        case "current-workflow":
            _,entry:=mkFile("unattended-readiness.yml",[]byte("changed workflow"))
            changedWorkflow:=mkTree([]ghaGuardGitTreeEntry{entry})
            current[0].SHA=mkTree([]ghaGuardGitTreeEntry{{Path:"workflows",Mode:"040000",Type:"tree",SHA:changedWorkflow}})
        case "unrelated-main":
            _,entry:=mkFile("outside.txt",[]byte("unrelated project change"));current=append(current,entry)
        }
        f.tree=mkTree(current);f.head=strings.Repeat("d",40)
    }
    f.endpoints[""]=encode(map[string]any{"id":int64(1406818713)})
    f.endpoints["/git/ref/heads/main"]=encode(map[string]any{"ref":"refs/heads/main","object":map[string]any{"type":"commit","sha":f.head}})
    f.endpoints["/git/commits/"+f.producer]=encode(map[string]any{"sha":f.producer,"tree":map[string]any{"sha":producerTree}})
    if f.head!=f.producer {f.endpoints["/git/commits/"+f.head]=encode(map[string]any{"sha":f.head,"tree":map[string]any{"sha":f.tree}})}
    compare:=func(base,head string) {
        status:="ahead";ahead:=1
        if base==head {status="identical";ahead=0}
        f.endpoints["/compare/"+base+"..."+head]=encode(map[string]any{"status":status,"ahead_by":ahead,"behind_by":0,"base_commit":map[string]any{"sha":base},"merge_base_commit":map[string]any{"sha":base}})
    }
    compare(ghaGuardSource,f.producer);compare(ghaFreshProducerAnchor,f.producer);compare(f.producer,f.head)
    if f.head!=f.producer {compare(ghaGuardSource,f.head);compare(ghaFreshProducerAnchor,f.head)}
    alternate:=strings.Repeat("f",40)
    f.endpoints["/git/commits/"+alternate]=encode(map[string]any{"sha":alternate,"tree":map[string]any{"sha":f.tree}})
    compare(f.producer,alternate)
    run:=map[string]any{"id":int64(2),"workflow_id":ghaFreshWorkflowID,"run_attempt":1,"head_sha":f.producer,
        "created_at":"2026-11-15T17:21:18Z","head_branch":"main","path":".github/workflows/unattended-readiness.yml",
        "event":"workflow_dispatch","status":"completed","conclusion":"success",
        "repository":map[string]any{"id":int64(1406818713)},"head_repository":map[string]any{"id":int64(1406818713)},
        "actor":map[string]any{"id":ghaFreshOwnerID},"triggering_actor":map[string]any{"id":ghaFreshOwnerID}}
    f.endpoints["/actions/workflows/382798887/runs"]=encode(map[string]any{"total_count":1,"workflow_runs":[]any{run}})
    f.client=&http.Client{Transport:ghaGuardTreeTestTransport(func(r *http.Request)(*http.Response,error){
        const prefix="/repos/AutoDevJapan/cc-pc-tools-rs"
        if r.Method!=http.MethodGet||r.URL.Scheme!="https"||r.URL.Host!="api.github.com"||!strings.HasPrefix(r.URL.Path,prefix){return nil,errors.New("unexpected endpoint")}
        key:=strings.TrimPrefix(r.URL.Path,prefix)
        if key=="/actions/workflows/382798887/runs" {if r.URL.RawQuery!=ghaFreshListingQuery{return nil,errors.New("unexpected listing")}} else if strings.HasPrefix(key,"/actions/runs/") {if r.URL.RawQuery!="per_page=100"{return nil,errors.New("unexpected action query")}} else if r.URL.RawQuery!="" {return nil,errors.New("unexpected query")}
        body,ok:=f.endpoints[key]
        if key=="/git/ref/heads/main" {
            f.refReads++
            if f.moveAtSecondRead&&f.refReads==2 {body=encode(map[string]any{"ref":"refs/heads/main","object":map[string]any{"type":"commit","sha":alternate}});ok=true}
        }
        if key=="/actions/workflows/382798887/runs" {
            f.listReads++
            if f.changeListing&&f.listReads==2 {other:=make(map[string]any,len(run));for k,v:=range run{other[k]=v};other["id"]=int64(3);body=encode(map[string]any{"total_count":2,"workflow_runs":[]any{run,other}});ok=true}
        }
        status:=http.StatusOK;if !ok{status=http.StatusNotFound}
        return &http.Response{StatusCode:status,Header:make(http.Header),Body:io.NopCloser(bytes.NewReader(body)),Request:r},nil
    })}
    return f
}

func TestGHAFreshCurrentSourceCompleteInputBoundary(t *testing.T) {
    for _,name:=range []string{"complete","inert-test","missing","extra","changed-blob","duplicate","wrong-mode","tree-tamper","wrong-ancestor","wrong-workflow","wrong-capture","capture-extra","workspace","current-changed","current-workflow","unrelated-main","main-moved","cancelled","alias-root-tools","alias-root-github","alias-root-guard","alias-workflows","alias-capture","alias-contract","alias-workflow-file","unrelated-sibling"} {
        t.Run(name,func(t *testing.T){
            f:=ghaCurrentTestFixtureFor(t,name)
            records:=append([]ghaGuardSourceRecord(nil),f.records...)
            switch name {
            case "missing":records=records[:len(records)-1]
            case "extra":records=append(records,records[len(records)-1])
            case "changed-blob":records[0].Blob=strings.Repeat("a",40)
            case "duplicate":records[1]=records[0]
            case "tree-tamper":f.endpoints["/git/trees/"+f.tree]=bytes.Replace(f.endpoints["/git/trees/"+f.tree],[]byte("repair-guard"),[]byte("repair-guarb"),1)
            case "wrong-ancestor":
                key:="/compare/"+ghaFreshProducerAnchor+"..."+f.producer
                f.endpoints[key]=bytes.Replace(f.endpoints[key],[]byte(ghaFreshProducerAnchor),[]byte(strings.Repeat("f",40)),2)
            case "main-moved":f.moveAtSecondRead=true
            }
            ctx:=context.Background();if name=="cancelled" {var cancel context.CancelFunc;ctx,cancel=context.WithCancel(ctx);cancel()}
            err:=ghaFreshVerifyCurrentRecordsWith(ctx,f.client,f.client,records,f.producer,f.policy)
            if name=="complete"||name=="inert-test"||name=="unrelated-main"||name=="unrelated-sibling" {if err!=nil||f.refReads!=2 {t.Fatal("complete current inventory rejected",name,err)}} else if err==nil {t.Fatal("unbound current inventory accepted",name)}
        })
    }
}

func TestGHAFreshFixedProducerContractDoesNotTrustSyntheticPolicy(t *testing.T) {
    f:=ghaCurrentTestFixtureFor(t,"")
    if ghaFreshVerifyCurrentRecords(context.Background(),f.client,f.client,f.records,f.producer)==nil {t.Fatal("synthetic contract became fixed authority")}
}

func ghaCurrentTestArchive(t *testing.T,records []ghaGuardSourceRecord) (ghaGuardManifest,[]byte) {
    t.Helper()
    m:=ghaGuardTestManifest();m.ProducerRevision=ghaFreshTestRevision
    var proof ghaGuardSourceProof
    if json.Unmarshal(ghaGuardTestProof(m),&proof)!=nil {t.Fatal("proof fixture")}
    var snapshot ghaGuardProofSnapshot
    if json.Unmarshal(proof.Before,&snapshot)!=nil {t.Fatal("snapshot fixture")}
    snapshot.Inputs=append([]ghaGuardSourceRecord(nil),records...)
    inventory,err:=json.Marshal(snapshot.Inputs);if err!=nil{t.Fatal(err)}
    snapshot.InventorySHA=digest(inventory)
    raw,err:=json.Marshal(snapshot);if err!=nil{t.Fatal(err)}
    proof.Before=raw;proof.After=raw
    proofRaw,err:=json.Marshal(proof);if err!=nil{t.Fatal(err)}
    m.SourceManifestSHA=digest(proofRaw)
    var out bytes.Buffer
    z:=zip.NewWriter(&out)
    for _,item:=range []struct{name string;raw []byte}{{"capture.json",[]byte("{}")},{"guard.exe",ghaGuardTestPE()},{"source-proof.json",proofRaw}} {
        w,err:=z.Create(item.name);if err!=nil{t.Fatal(err)}
        if _,err=w.Write(item.raw);err!=nil{t.Fatal(err)}
    }
    if z.Close()!=nil{t.Fatal("archive fixture close")}
    return m,out.Bytes()
}

func ghaCurrentTestCandidate(t *testing.T,ctx context.Context,m ghaGuardManifest) *guardTargetCandidate {
    t.Helper()
    generation:=ghaGuardGeneration{Source:m.Source,SourceTree:m.SourceTree,ProducerRevision:m.ProducerRevision,
        RunID:m.RunID,RunAttempt:m.RunAttempt,JobID:m.JobID,CheckRunID:m.CheckRunID,
        ArtifactID:5,OutputSHA:m.OutputSHA,ProofSHA:m.SourceManifestSHA}
    source:=&ghaGuardVerifiedCandidate{issuer:&ghaGuardIssuer{token:1},generation:generation,payload:ghaGuardTestPE()}
    source.self=source
    value,err:=guardTargetArtifactFromServer(ctx,func(context.Context)(*ghaGuardVerifiedCandidate,error){return source,nil})
    if err!=nil {t.Fatal("owned candidate fixture",err)}
    artifact,ok:=value.(*guardTargetCandidate);if !ok{t.Fatal("candidate type")}
    t.Cleanup(func(){_ = artifact.Close()})
    return artifact
}

func ghaCurrentTestLocator(m ghaGuardManifest,archive []byte) ghaFreshPublicLocator {
    l:=ghaFreshTestLocator()
    l.hint.runID=m.RunID;l.hint.attempt=int64(m.RunAttempt);l.hint.producerRevision=m.ProducerRevision
    l.jobID=m.JobID;l.checkRunID=m.CheckRunID;l.artifactID=5
    l.archiveBytes=int64(len(archive));l.archiveSHA=digest(archive)
    return l
}

func ghaCurrentTestInstallLocator(t *testing.T,f *ghaCurrentTestFixture,l ghaFreshPublicLocator) {
    t.Helper()
    encode:=func(v any) []byte {b,err:=json.Marshal(v);if err!=nil{t.Fatal(err)};return b}
    names:=[]string{"Capture the fixed guard build","Check the exact captured files","Attest the guard and source evidence","Upload the fixed guard capture","Record the server-assigned capture artifact"}
    steps:=make([]map[string]any,0,len(names))
    for i,name:=range names {steps=append(steps,map[string]any{"name":name,"status":"completed","conclusion":"success","number":i+1})}
    f.endpoints["/actions/workflows/unattended-readiness.yml"]=encode(map[string]any{"id":ghaFreshWorkflowID,"path":".github/workflows/unattended-readiness.yml","state":"active"})
    jobKey:=fmt.Sprintf("/actions/runs/%d/attempts/%d/jobs",l.hint.runID,l.hint.attempt)
    f.endpoints[jobKey]=encode(map[string]any{"total_count":1,"jobs":[]any{map[string]any{"id":l.jobID,"run_id":l.hint.runID,"name":"Guard bootstrap artifact","status":"completed","conclusion":"success","check_run_url":fmt.Sprintf("https://api.github.com/repos/AutoDevJapan/cc-pc-tools-rs/check-runs/%d",l.checkRunID),"steps":steps}}})
    artifactKey:=fmt.Sprintf("/actions/runs/%d/artifacts",l.hint.runID)
    f.endpoints[artifactKey]=encode(map[string]any{"total_count":1,"artifacts":[]any{map[string]any{
        "id":l.artifactID,"name":fmt.Sprintf("guard-bootstrap-%d-%d-%d",l.hint.runID,l.hint.attempt,l.jobID),
        "size_in_bytes":l.archiveBytes,"digest":"sha256:"+l.archiveSHA,"expired":false,"expires_at":l.expiresAt,
        "workflow_run":map[string]any{"id":l.hint.runID,"repository_id":int64(1406818713),"head_repository_id":int64(1406818713),"head_sha":l.hint.producerRevision},
    }}})
}

func ghaCurrentTestAuthority(t *testing.T,f *ghaCurrentTestFixture,m ghaGuardManifest,archive []byte) *ghaGuardEvidenceAuthority {
    t.Helper()
    loc:=ghaCurrentTestLocator(m,archive);ghaCurrentTestInstallLocator(t,f,loc)
    root,cancel:=context.WithCancel(context.Background())
    session:=&ghaGuardCredentialSession{fresh:&loc,secret:[]byte("fictional"),base:f.client.Transport,root:root,cancel:cancel,ready:true,drain:make(chan struct{})}
    authority:=&ghaGuardEvidenceAuthority{client:&http.Client{Transport:session},credential:session}
    t.Cleanup(func(){ctx,done:=context.WithTimeout(context.Background(),time.Second);defer done();if session.close(ctx)!=nil{t.Error("credential fixture cleanup")}})
    return authority
}

func ghaCurrentTestBoundProof(t *testing.T,ctx context.Context,f *ghaCurrentTestFixture,m ghaGuardManifest,archive []byte,artifact *guardTargetCandidate) (*ghaFreshCurrentInputs,error) {
    t.Helper()
    capture:=ghaFreshNewUpdateArchiveCapture();defer capture.Close()
    if capture.Start(ghaCurrentTestLocator(m,archive))!=nil||capture.Hold(archive,m)!=nil{return nil,errGHAGuardUnqualified}
    proof,err:=capture.Bind(ctx,artifact,f.client)
    if err!=nil||proof==nil{return proof,err}
    if err=capture.ClaimBound(proof);err!=nil {_=capture.DiscardBound(proof);return nil,err}
    return proof,nil
}

func TestGHAFreshCurrentInputsAreBoundToOwnedCandidate(t *testing.T) {
    for _,name:=range []string{"normal","wrong-proof","wrong-generation","different-owner","tampered-record","closed","cancelled"} {
        t.Run(name,func(t *testing.T){
            ctx:=context.Background();f:=ghaCurrentTestFixtureFor(t,"")
            manifest,archive:=ghaCurrentTestArchive(t,f.records);artifact:=ghaCurrentTestCandidate(t,ctx,manifest)
            authority:=ghaCurrentTestAuthority(t,f,manifest,archive);request:=manifest
            if name=="wrong-proof"{request.SourceManifestSHA=strings.Repeat("f",64)}
            if name=="wrong-generation"{request.RunID++}
            var proof *ghaFreshCurrentInputs;var err error
            if name=="wrong-proof"||name=="wrong-generation" {
                proof,err=ghaFreshBindCurrentInputs(ctx,artifact,archive,request,f.client)
                if err==nil||proof!=nil{t.Fatal("wrong proof or generation accepted")};return
            }
            proof,err=ghaCurrentTestBoundProof(t,ctx,f,manifest,archive,artifact)
            if err!=nil||proof==nil{t.Fatal("owned proof rejected",err)}
            defer proof.Close()
            target:=artifact
            if name=="different-owner"{target=ghaCurrentTestCandidate(t,ctx,manifest)}
            if name=="tampered-record"{proof.records[0].SHA=strings.Repeat("f",64)}
            if name=="closed"{_ = proof.Close()}
            if name=="cancelled"{var cancel context.CancelFunc;ctx,cancel=context.WithCancel(ctx);cancel()}
            err=proof.recheckWith(ctx,target,f.client,authority,f.policy)
            if name=="normal" {if err!=nil{t.Fatal("same owned source rejected",err)}} else if err==nil {t.Fatal("invalid proof accepted",name)}
        })
    }
}

func TestGHAFreshCurrentArchiveRequiresMatchingAuthority(t *testing.T) {
    for _,name:=range []string{"normal","wrong-run","closed","wrong-transport","blob-tamper","listing-changed"} {
        t.Run(name,func(t *testing.T){
            f:=ghaCurrentTestFixtureFor(t,"");m,archive:=ghaCurrentTestArchive(t,f.records);authority:=ghaCurrentTestAuthority(t,f,m,archive)
            switch name {
            case "wrong-run":authority.credential.fresh.hint.runID++
            case "closed":if authority.credential.close(context.Background())!=nil {t.Fatal("fixture close")}
            case "wrong-transport":authority.client=&http.Client{Transport:f.client.Transport}
            case "blob-tamper":f.endpoints["/git/blobs/"+f.records[0].Blob]=[]byte("tampered")
            case "listing-changed":f.changeListing=true
            }
            err:=ghaFreshVerifyCurrentArchiveInputsWith(context.Background(),f.client,authority,archive,m,f.policy)
            if name=="normal" {if err!=nil{t.Fatal("bound archive rejected",err)}} else if err==nil {t.Fatal("invalid authority, blob or listing accepted",name)}
        })
    }
}

func TestGHAFreshUpdateCaptureBindsOneCandidate(t *testing.T) {
    for _,name:=range []string{"normal","wrong-archive","duplicate-hold","tampered-held","wrong-generation","closed","repeat-bind"} {
        t.Run(name,func(t *testing.T){
            ctx:=context.Background();f:=ghaCurrentTestFixtureFor(t,"");m,archive:=ghaCurrentTestArchive(t,f.records)
            l:=ghaCurrentTestLocator(m,archive);capture:=ghaFreshNewUpdateArchiveCapture();defer capture.Close()
            if capture.Start(l)!=nil {t.Fatal("capture start")}
            if name=="wrong-archive" {bad:=append([]byte(nil),archive...);bad[0]^=1;if capture.Hold(bad,m)==nil {t.Fatal("wrong archive accepted")};return}
            if capture.Hold(archive,m)!=nil {t.Fatal("capture hold")}
            if name=="duplicate-hold" {if capture.Hold(archive,m)==nil {t.Fatal("duplicate hold accepted")};return}
            if name=="tampered-held"{capture.archive[0]^=1}
            candidateManifest:=m;if name=="wrong-generation"{candidateManifest.RunID++}
            artifact:=ghaCurrentTestCandidate(t,ctx,candidateManifest)
            if name=="closed"{_ = capture.Close()}
            proof,err:=capture.Bind(ctx,artifact,f.client)
            if name=="wrong-generation"||name=="closed"||name=="tampered-held" {if err==nil||proof!=nil{t.Fatal("unbound candidate accepted")};return}
            if err!=nil||proof==nil {t.Fatal("same candidate binding rejected",err)}
            if err=capture.ClaimBound(proof);err!=nil{t.Fatal("pending proof transfer rejected",err)}
            defer proof.Close()
            if proof.locator!=l||proof.artifact!=artifact {t.Fatal("wrong bound source/locator")}
            if name=="repeat-bind" {if again,e:=capture.Bind(ctx,artifact,f.client);e==nil||again!=nil{t.Fatal("capture reused")}}
        })
    }
}

func TestGHAFreshSourceTransportSeparatesCredentials(t *testing.T) {
    publicCalls,authenticatedCalls:=0,0
    public:=&http.Client{Transport:ghaGuardTreeTestTransport(func(r *http.Request)(*http.Response,error){
        publicCalls++
        if r.Header.Get("Authorization")!=""||r.Header.Get("Cookie")!=""||r.Header.Get("Proxy-Authorization")!="" {t.Fatal("credential reached public transport")}
        return &http.Response{StatusCode:http.StatusOK,Header:make(http.Header),Body:io.NopCloser(strings.NewReader("{}")),Request:r},nil
    })}
    authenticated:=&http.Client{Transport:ghaGuardTreeTestTransport(func(r *http.Request)(*http.Response,error){
        authenticatedCalls++
        return &http.Response{StatusCode:http.StatusOK,Header:make(http.Header),Body:io.NopCloser(strings.NewReader("{}")),Request:r},nil
    })}
    client,err:=ghaFreshSplitSourceClient(public,authenticated);if err!=nil{t.Fatal(err)}
    ref,_:=http.NewRequest(http.MethodGet,ghaGuardGitAPI+"/git/ref/heads/main",nil)
    ref.Header.Set("Authorization","fictional");ref.Header.Set("Cookie","fictional");ref.Header.Set("Proxy-Authorization","fictional")
    response,err:=client.Do(ref);if err!=nil{t.Fatal(err)};_ = response.Body.Close()
    tree,_:=http.NewRequest(http.MethodGet,ghaGuardGitAPI+"/git/trees/"+strings.Repeat("a",40),nil)
    response,err=client.Do(tree);if err!=nil{t.Fatal(err)};_ = response.Body.Close()
    foreign,_:=http.NewRequest(http.MethodGet,"https://example.invalid/git/trees/"+strings.Repeat("a",40),nil)
    if response,err=client.Do(foreign);err==nil||response!=nil {t.Fatal("foreign source accepted")}
    if publicCalls!=1||authenticatedCalls!=1 {t.Fatal("wrong transport selection")}
}

func TestGHAFreshProofChecksBeforeAndAfterCredentialOpen(t *testing.T) {
    for _,name:=range []string{"normal","tampered-record","tampered-locator","expired","different-owner","closed","closed-during-open"} {
        t.Run(name,func(t *testing.T){
            ctx:=context.Background();f:=ghaCurrentTestFixtureFor(t,"");m,archive:=ghaCurrentTestArchive(t,f.records)
            artifact:=ghaCurrentTestCandidate(t,ctx,m);proof,err:=ghaCurrentTestBoundProof(t,ctx,f,m,archive,artifact)
            if err!=nil||proof==nil{t.Fatal("proof fixture",err)};defer proof.Close()
            authority:=ghaCurrentTestAuthority(t,f,m,archive);target:=artifact
            switch name {
            case "tampered-record":proof.records[0].SHA=strings.Repeat("f",64)
            case "tampered-locator":proof.locator.archiveSHA=strings.Repeat("f",64)
            case "expired":proof.locator.expiresAt=time.Date(2000,1,1,0,0,0,0,time.UTC);proof.locatorSHA=ghaFreshLocatorSHA(proof.locator)
            case "different-owner":target=ghaCurrentTestCandidate(t,ctx,m)
            case "closed":_ = proof.Close()
            }
            calls:=0
            open:=func(_ context.Context,l ghaFreshPublicLocator,_ *ghaFreshCredentialAudit)(*ghaGuardEvidenceAuthority,error){
                calls++
                if name=="closed-during-open" {if err:=proof.Close();!errors.Is(err,errGuardTargetCleanup){t.Fatal("active proof falsely closed",err)}}
                return authority,nil
            }
            got,openErr:=proof.openAuthorityWith(ctx,target,nil,open)
            if name=="normal" {
                if openErr!=nil||got!=authority||calls!=1{t.Fatal("same proof authority rejected",openErr)}
            } else {
                if openErr==nil||got!=nil{t.Fatal("invalid proof obtained authority",name)}
                if name=="closed-during-open" {if calls!=1||!authority.credential.closed{t.Fatal("partial authority not closed")}} else if calls!=0 {t.Fatal("invalid proof reached opener",name)}
            }
        })
    }
}

type ghaCurrentStatusCloseBody struct {io.Reader}
func (b *ghaCurrentStatusCloseBody) Close() error {return errors.New("sensitive-marker-must-not-escape")}
type ghaCurrentStatusErrorReadBody struct {err error}
func (b *ghaCurrentStatusErrorReadBody) Read([]byte)(int,error){return 0,b.err}
func (b *ghaCurrentStatusErrorReadBody) Close() error{return nil}

// Exercise new current-source HTTP/cleanup/cancel facts through the existing
// bounded worker and parent projection, not as an authority or live request.
func TestGHAFreshCurrentSourceFailureReachesParent(t *testing.T) {
    for _,mode:=range []string{"http-403","http-429","http-403-close","close","cancel","inner-deadline","inner-cancel","read-deadline","read-cancel","blob-403","blob-403-close","blob-close"} {t.Run(mode,func(t *testing.T){
        f:=ghaCurrentTestFixtureFor(t,"unrelated-main");base:=f.client.Transport
        ctx,cancel:=context.WithCancel(context.Background());defer cancel()
        client:=&http.Client{Transport:ghaGuardTreeTestTransport(func(r *http.Request)(*http.Response,error){
            ref:="/repos/AutoDevJapan/cc-pc-tools-rs/git/ref/heads/main"
            blob:="/repos/AutoDevJapan/cc-pc-tools-rs/git/blobs/"+f.records[0].Blob
            isBlob:=mode=="blob-403"||mode=="blob-403-close"||mode=="blob-close"
            if (!isBlob&&r.URL.Path==ref)||(isBlob&&r.URL.Path==blob) {
                if mode=="cancel" {cancel();return nil,context.Canceled}
                if mode=="inner-deadline" {return nil,errors.Join(context.DeadlineExceeded,errors.New("sensitive-marker-must-not-escape"))}
                if mode=="inner-cancel" {return nil,errors.Join(context.Canceled,errors.New("sensitive-marker-must-not-escape"))}
                if mode=="read-deadline"||mode=="read-cancel" {cause:=error(context.DeadlineExceeded);if mode=="read-cancel"{cause=context.Canceled};return &http.Response{StatusCode:200,Header:make(http.Header),Body:&ghaCurrentStatusErrorReadBody{err:errors.Join(cause,errors.New("sensitive-marker-must-not-escape"))},Request:r},nil}
                if mode=="http-403"||mode=="http-429"||mode=="http-403-close"||mode=="blob-403"||mode=="blob-403-close" {
                    status:=403;if mode=="http-429"{status=429}
                    var body io.ReadCloser=io.NopCloser(strings.NewReader("sensitive-marker-must-not-escape"))
                    if mode=="http-403-close"||mode=="blob-403-close" {body=&ghaCurrentStatusCloseBody{Reader:strings.NewReader("sensitive-marker-must-not-escape")}}
                    return &http.Response{StatusCode:status,Header:make(http.Header),Body:body,Request:r},nil
                }
                key:="/git/ref/heads/main";if isBlob{key="/git/blobs/"+f.records[0].Blob}
                body:=f.endpoints[key]
                return &http.Response{StatusCode:200,Header:make(http.Header),Body:&ghaCurrentStatusCloseBody{Reader:bytes.NewReader(body)},Request:r},nil
            }
            return base.RoundTrip(r)
        })}
        err:=ghaFreshVerifyCurrentRecordsWith(ctx,client,client,f.records,f.producer,f.policy)
        if err==nil {t.Fatal("failure admitted")}
        expected:="guard_target_cancelled"
        if mode=="http-403"||mode=="http-403-close"||mode=="blob-403"||mode=="blob-403-close"{expected="guard_github_source_metadata_http_403"}
        if mode=="http-429"{expected="guard_github_source_metadata_http_429"}
        if mode=="close"||mode=="blob-close"{expected="guard_github_response_cleanup_failed"}
        safe:=ghaGuardSafeSourceFailure(ghaFreshSafeCurrentFailure(ctx,err),ctx.Err())
        if got:=ghaFreshAcquisitionReason(safe);got!=expected{t.Fatalf("status lost: %s / %v",got,safe)}
        audit:=&ghaFreshCredentialAudit{state:"completed"}
        worker:=ghaFreshInspectWith(context.Background(),audit,func(context.Context)(guardTargetArtifact,error){return nil,safe})
        raw,marshalErr:=json.Marshal(worker);if marshalErr!=nil{t.Fatal(marshalErr)}
        child:=askChildResult{Started:true,Assigned:true,WaitVerified:true,Reaped:true,Exit:1,Raw:raw}
        parent:=ghaFreshInspectionFromChild(child);encoded,_:=json.Marshal(parent)
        if parent.Reason!=expected||parent.Qualified||!parent.WorkerResultObserved||strings.Contains(string(encoded),"sensitive-marker") {t.Fatal("parent projection lost or leaked evidence",string(encoded))}
        if (mode=="close"||mode=="blob-close"||mode=="http-403-close"||mode=="blob-403-close")&&!parent.CleanupFailed {t.Fatal("close failure lost")}
        if (mode=="cancel"||mode=="inner-deadline"||mode=="inner-cancel"||mode=="read-deadline"||mode=="read-cancel")&&!parent.Cancelled {t.Fatal("cancel lost")}
    })}
}

func TestGHAFreshMovingMainUsesBoundedPublicQuota(t *testing.T) {
    ctx:=context.Background();f:=ghaCurrentTestFixtureFor(t,"unrelated-main")
    m,archive:=ghaCurrentTestArchive(t,f.records);artifact:=ghaCurrentTestCandidate(t,ctx,m)
    authority:=ghaCurrentTestAuthority(t,f,m,archive);proof,err:=ghaCurrentTestBoundProof(t,ctx,f,m,archive,artifact)
    if err!=nil||proof==nil{t.Fatal("fixture proof",err)};defer proof.Close()
    base:=f.client.Transport;var mu sync.Mutex;publicCalls,authenticatedCalls:=0,0
    public:=&http.Client{Transport:ghaGuardTreeTestTransport(func(r *http.Request)(*http.Response,error){
        mu.Lock();publicCalls++;n:=publicCalls;mu.Unlock()
        if r.Header.Get("Authorization")!=""||r.Header.Get("Cookie")!=""||r.URL.Host!="api.github.com" {t.Error("credential reached public transport");return nil,errGHAGuardUnqualified}
        if n>60 {return &http.Response{StatusCode:429,Header:make(http.Header),Body:io.NopCloser(strings.NewReader("quota")),Request:r},nil}
        return base.RoundTrip(r)
    })}
    authority.credential.base=ghaGuardTreeTestTransport(func(r *http.Request)(*http.Response,error){
        mu.Lock();authenticatedCalls++;mu.Unlock()
        if r.Header.Get("Authorization")!="Bearer fictional"||r.URL.Host!="api.github.com"||r.URL.Scheme!="https" {t.Error("credential scope escaped");return nil,errGHAGuardUnqualified}
        return base.RoundTrip(r)
    })
    hint,err:=ghaFreshReadRunHint(ctx,public);if err!=nil{t.Fatal("public hint",err)}
    if _,err=ghaFreshPublicPreflight(ctx,public,hint,time.Now().UTC());err!=nil{t.Fatal("public preflight",err)}
    if err=ghaFreshVerifyCurrentArchiveInputsWith(ctx,public,authority,archive,m,f.policy);err!=nil{t.Fatal("current archive",err)}
    err=ghaFreshHandoffWithoutIssuerWith(ctx,artifact,proof,public,authority,f.policy,nil)
    if !errors.Is(err,errGHAFreshUpdateIssuerUnavailable){t.Fatal("handoff verification",err)}
    mu.Lock();p,a:=publicCalls,authenticatedCalls;mu.Unlock()
    if p>60||a<249{t.Fatalf("wrong public/authenticated split: public=%d auth=%d",p,a)}
}

func TestGHAFreshCaptureCloseDuringBindKeepsOwnership(t *testing.T) {
    for _,phase:=range []string{"active","before-pending"} {t.Run(phase,func(t *testing.T){
        ctx,cancel:=context.WithTimeout(context.Background(),5*time.Second);defer cancel()
        f:=ghaCurrentTestFixtureFor(t,"");m,archive:=ghaCurrentTestArchive(t,f.records);artifact:=ghaCurrentTestCandidate(t,ctx,m)
        capture:=ghaFreshNewUpdateArchiveCapture();if capture.Start(ghaCurrentTestLocator(m,archive))!=nil||capture.Hold(archive,m)!=nil{t.Fatal("capture fixture")}
        held:=capture.archive;entered:=make(chan struct{});release:=make(chan struct{});defer func(){select{case <-release:default:close(release)}}()
        type result struct{proof *ghaFreshCurrentInputs;err error};done:=make(chan result,1)
        go func(){p,e:=capture.bindWith(ctx,artifact,f.client,func(step string){if step==phase{close(entered);<-release}});done<-result{p,e}}()
        select{case <-entered:case <-ctx.Done():t.Fatal("bind did not reach barrier")}
        if err:=capture.Close();!errors.Is(err,errGuardTargetCleanup){t.Fatal("active close reported completion",err)}
        close(release)
        var got result;select{case got=<-done:case <-ctx.Done():t.Fatal("bind did not drain")}
        if got.err==nil||got.proof!=nil{t.Fatal("revoked bind returned proof")}
        for _,b:=range held {if b!=0 {t.Fatal("held archive not erased")}}
        if err:=capture.Close();err!=nil{t.Fatal("owner could not finish close",err)}
    })}
}

func TestGHAFreshCapturePendingRequiresClaimOrDiscard(t *testing.T) {
    for _,mode:=range []string{"claim","close-before-claim"} {t.Run(mode,func(t *testing.T){
        ctx:=context.Background();f:=ghaCurrentTestFixtureFor(t,"");m,archive:=ghaCurrentTestArchive(t,f.records);artifact:=ghaCurrentTestCandidate(t,ctx,m)
        capture:=ghaFreshNewUpdateArchiveCapture();if capture.Start(ghaCurrentTestLocator(m,archive))!=nil||capture.Hold(archive,m)!=nil{t.Fatal("capture fixture")}
        proof,err:=capture.Bind(ctx,artifact,f.client);if err!=nil||proof==nil{t.Fatal("pending bind",err)}
        if err=proof.beginUse();err==nil{_ = proof.endUse();t.Fatal("unclaimed proof usable")}
        if mode=="claim" {
            if err=capture.ClaimBound(proof);err!=nil{t.Fatal("claim",err)}
            if err=capture.Close();err!=nil{t.Fatal("capture close after transfer",err)}
            if err=proof.beginUse();err!=nil{t.Fatal("claimed proof unusable",err)};if err=proof.endUse();err!=nil{t.Fatal(err)}
            if err=proof.Close();err!=nil{t.Fatal(err)}
        } else {
            if err=capture.Close();!errors.Is(err,errGuardTargetCleanup){t.Fatal("pending close falsely completed",err)}
            if err=capture.ClaimBound(proof);err==nil{t.Fatal("revoked transfer accepted")}
            if err=capture.DiscardBound(proof);err!=nil{t.Fatal("discard pending proof",err)}
            if err=capture.Close();err!=nil{t.Fatal("close after discard",err)}
            if err=proof.beginUse();err==nil{t.Fatal("discarded proof usable")}
        }
    })}
}

func TestGHAFreshProofCloseDuringRecheckAndObservation(t *testing.T) {
    for _,phase:=range []string{"last-listing","standalone-recheck","observation"} {t.Run(phase,func(t *testing.T){
        ctx,cancel:=context.WithTimeout(context.Background(),10*time.Second);defer cancel()
        f:=ghaCurrentTestFixtureFor(t,"");m,archive:=ghaCurrentTestArchive(t,f.records);artifact:=ghaCurrentTestCandidate(t,ctx,m)
        entered:=make(chan struct{});release:=make(chan struct{});defer func(){select{case <-release:default:close(release)}}()
        base:=f.client.Transport;listCount:=0
        f.client=&http.Client{Transport:ghaGuardTreeTestTransport(func(r *http.Request)(*http.Response,error){
            if r.URL.Path=="/repos/AutoDevJapan/cc-pc-tools-rs/actions/workflows/382798887/runs" {
                listCount++;if (phase=="last-listing"||phase=="standalone-recheck")&&listCount==2 {close(entered);<-release}
            }
            return base.RoundTrip(r)
        })}
        authority:=ghaCurrentTestAuthority(t,f,m,archive);proof,err:=ghaCurrentTestBoundProof(t,ctx,f,m,archive,artifact)
        if err!=nil||proof==nil{t.Fatal("proof fixture",err)}
        observed:=false;done:=make(chan error,1)
        go func(){
            if phase=="standalone-recheck" {done<-proof.recheckWith(ctx,artifact,f.client,authority,f.policy);return}
            done<-ghaFreshHandoffWithoutIssuerWith(ctx,artifact,proof,f.client,authority,f.policy,func(*guardTargetCandidate,*ghaFreshCurrentInputs,[]byte)error{
                observed=true;if phase=="observation"{close(entered);<-release};return nil
            })
        }()
        select{case <-entered:case <-ctx.Done():t.Fatal("proof did not reach barrier")}
        if err=proof.Close();!errors.Is(err,errGuardTargetCleanup){t.Fatal("active proof close reported completion",err)}
        close(release)
        select{case err=<-done:case <-ctx.Done():t.Fatal("handoff did not drain")}
        if !errors.Is(err,errGuardTargetCleanup){t.Fatal("revocation not reported",err)}
        if (phase=="last-listing"||phase=="standalone-recheck")&&observed {t.Fatal("revoked proof reached observer")}
        if phase=="observation"&&!observed {t.Fatal("observation barrier not reached")}
        if err=proof.Close();err!=nil{t.Fatal("proof owner could not finish close",err)}
    })}
}

func TestGHAFreshSourceFailurePreservesAllFixedFacts(t *testing.T) {
    raw:=errors.Join(ghaGuardHTTPFailure("source_metadata",429),errGHAGuardResponseCleanup,context.DeadlineExceeded,errors.New("sensitive-marker"))
    safe:=ghaGuardSafeSourceFailure(ghaFreshSafeCurrentFailure(context.Background(),raw),nil)
    if !errors.Is(safe,errGHAGuardUnqualified)||!errors.Is(safe,errGHAGuardResponseCleanup)||!errors.Is(safe,context.DeadlineExceeded)||
        ghaFreshAcquisitionReason(safe)!="guard_github_source_metadata_http_429"||strings.Contains(safe.Error(),"sensitive-marker") {t.Fatal("fixed failure facts lost or raw error escaped",safe)}
    audit:=&ghaFreshCredentialAudit{state:"completed"}
    worker:=ghaFreshInspectWith(context.Background(),audit,func(context.Context)(guardTargetArtifact,error){return nil,safe})
    rawChild,_:=json.Marshal(worker)
    parent:=ghaFreshInspectionFromChild(askChildResult{Started:true,Assigned:true,WaitVerified:true,Reaped:true,Exit:1,Raw:rawChild})
    if parent.Qualified||!parent.Cancelled||!parent.CleanupFailed||parent.Reason!="guard_github_source_metadata_http_429"{t.Fatal("combined parent facts lost",parent)}
}
