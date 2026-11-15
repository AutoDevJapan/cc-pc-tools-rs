//go:build windows

package main

import (
    "context"
    "encoding/json"
    "errors"
    "fmt"
    "net/http"
    "sync"
    "strings"
    "time"
)

// This is a bounded observation of current public source, not an update permit.
type ghaFreshMainSample struct { head, tree string }

// Fixed repository tree/blob reads use the supplied authenticated transport.
// Other observations use the supplied public transport, which may be the same
// owned session when the caller requires authenticated discovery.
type ghaFreshSplitSourceTransport struct { public,authenticated http.RoundTripper }
func (t *ghaFreshSplitSourceTransport) RoundTrip(req *http.Request) (*http.Response,error) {
    if t==nil||t.public==nil||t.authenticated==nil||req==nil||req.URL==nil||req.Method!=http.MethodGet||
        req.URL.Scheme!="https"||req.URL.Host!="api.github.com"||req.URL.User!=nil||req.URL.RawPath!=""||
        req.URL.Fragment!=""||req.URL.Opaque!=""||req.URL.ForceQuery{return nil,errGHAGuardUnqualified}
    const base="/repos/AutoDevJapan/cc-pc-tools-rs"
    if req.URL.Path!=base&&!strings.HasPrefix(req.URL.Path,base+"/"){return nil,errGHAGuardUnqualified}
    for _,prefix:=range []string{base+"/git/trees/",base+"/git/blobs/"} {
        if strings.HasPrefix(req.URL.Path,prefix) {
            if req.URL.RawQuery!=""||!ghaGuardHex(strings.TrimPrefix(req.URL.Path,prefix),40){return nil,errGHAGuardUnqualified}
            return t.authenticated.RoundTrip(req)
        }
    }
    copy:=req.Clone(req.Context());copy.Header=req.Header.Clone()
    copy.Header.Del("Authorization");copy.Header.Del("Cookie");copy.Header.Del("Proxy-Authorization")
    return t.public.RoundTrip(copy)
}

func ghaFreshSplitSourceClient(public,authenticated *http.Client) (*http.Client,error) {
    if public==nil||authenticated==nil||public.Transport==nil||authenticated.Transport==nil{return nil,errGHAGuardUnqualified}
    return &http.Client{Transport:&ghaFreshSplitSourceTransport{public:public.Transport,authenticated:authenticated.Transport},Timeout:30*time.Second},nil
}

func ghaFreshCurrentObject(ctx context.Context, client *http.Client, endpoint string) (map[string]json.RawMessage,error) {
    raw,err:=ghaGuardGitGet(ctx,client,endpoint,"application/vnd.github+json",ghaFreshListingLimit)
    if err!=nil {return nil,errGHAGuardUnqualified}
    defer clear(raw)
    return ghaFreshObject(raw)
}

func ghaFreshCurrentNestedSHA(obj map[string]json.RawMessage,key string) (string,bool) {
    nested,err:=ghaFreshObject(obj[key]);if err!=nil{return "",false}
    value,ok:=ghaFreshString(nested,"sha")
    return value,ok&&ghaGuardHex(value,40)
}

// The head comes only from the fixed repository main ref. The compare response
// must prove the reviewed H1 producer is its ancestor; no CLI SHA is accepted.
func ghaFreshRequireAncestor(ctx context.Context,client *http.Client,base,head string) error {
    if ctx==nil||ctx.Err()!=nil||client==nil||!ghaGuardHex(base,40)||!ghaGuardHex(head,40){return errGHAGuardUnqualified}
    endpoint:=fmt.Sprintf("%s/compare/%s...%s",ghaGuardGitAPI,base,head)
    cmp,err:=ghaFreshCurrentObject(ctx,client,endpoint);if err!=nil{return errGHAGuardUnqualified}
    status,sok:=ghaFreshString(cmp,"status");ahead,aok:=ghaFreshInt(cmp,"ahead_by");behind,bok:=ghaFreshInt(cmp,"behind_by")
    reported,bbase:=ghaFreshCurrentNestedSHA(cmp,"base_commit");merge,bmerge:=ghaFreshCurrentNestedSHA(cmp,"merge_base_commit")
    if !sok||!aok||!bok||behind!=0||!bbase||reported!=base||!bmerge||merge!=base{return errGHAGuardUnqualified}
    if (head==base&&(status!="identical"||ahead!=0))||(head!=base&&(status!="ahead"||ahead<=0))||ctx.Err()!=nil{return errGHAGuardUnqualified}
    return nil
}

// The H1 producer revision comes from server metadata and signed provenance;
// it is not a caller argument or a replacement for the fixed source contract.
func ghaFreshReadCurrentMain(ctx context.Context,client *http.Client,producer string) (ghaFreshMainSample,error) {
    var none ghaFreshMainSample
    if ctx==nil||ctx.Err()!=nil||client==nil||!ghaGuardHex(producer,40){return none,errGHAGuardUnqualified}
    ref,err:=ghaFreshCurrentObject(ctx,client,ghaGuardGitAPI+"/git/ref/heads/main")
    if err!=nil{return none,errGHAGuardUnqualified}
    name,ok:=ghaFreshString(ref,"ref");if !ok||name!="refs/heads/main"{return none,errGHAGuardUnqualified}
    object,err:=ghaFreshObject(ref["object"]);if err!=nil{return none,errGHAGuardUnqualified}
    kind,kok:=ghaFreshString(object,"type");head,hok:=ghaFreshString(object,"sha")
    if !kok||kind!="commit"||!hok||!ghaGuardHex(head,40){return none,errGHAGuardUnqualified}
    commit,err:=ghaFreshCurrentObject(ctx,client,ghaGuardGitAPI+"/git/commits/"+head)
    if err!=nil{return none,errGHAGuardUnqualified}
    actual,aok:=ghaFreshString(commit,"sha");tree,tok:=ghaFreshCurrentNestedSHA(commit,"tree")
    if !aok||actual!=head||!tok||ghaFreshRequireAncestor(ctx,client,producer,head)!=nil{return none,errGHAGuardUnqualified}
    return ghaFreshMainSample{head:head,tree:tree},nil
}


type ghaFreshContractFile struct { name,blob,sha string; size int64 }
type ghaFreshProducerContract struct { workflow,main,mod ghaFreshContractFile }

// Fixed source contract, not a producer run identity and not a caller input.
func ghaFreshFixedProducerContract() ghaFreshProducerContract {
    return ghaFreshProducerContract{
        workflow:ghaFreshContractFile{"unattended-readiness.yml","cf563394fa8d291bd5a62ae7838548bd025923cc","dd6ffc0d0fe3044f4e9769de8486b2d498c77ffaf43373ef8993b2a07034dac8",33582},
        main:ghaFreshContractFile{"main.go","d12328bd71bffded1119fdafe1e47d268ca25f5c","3cce4269015ba2d1506655ac06f287ce43b3eaac6bb4c85275c4830147bc728b",28426},
        mod:ghaFreshContractFile{"go.mod","7cbca21b453b71813b1c2062c88c3a100b4712ed","7ac39ab780837918502a138688161e032874992abf4287399269c40881fe595d",79},
    }
}

func ghaFreshFindTreeFile(entries []ghaGuardGitTreeEntry,want ghaFreshContractFile) bool {
    for _,e:=range entries {
        if e.Path==want.name {
            return e.Type=="blob"&&e.Mode=="100644"&&e.SHA==want.blob&&e.Size!=nil&&*e.Size==want.size
        }
    }
    return false
}

// Only exact fixed path components can name executable or reviewed inputs on
// the Windows checkout; an unrelated sibling remains acceptable.
func ghaFreshNoCaseAlias(entries []ghaGuardGitTreeEntry,names ...string) bool {
    for _,entry:=range entries {
        for _,name:=range names {if strings.EqualFold(entry.Path,name)&&entry.Path!=name{return false}}
    }
    return true
}

func ghaFreshNoWorkspace(entries []ghaGuardGitTreeEntry) bool {
    for _,e:=range entries {
        if strings.EqualFold(e.Path,"go.work")||strings.EqualFold(e.Path,"go.work.sum"){return false}
    }
    return true
}

// Go run ignores tests and inert documentation, but any additional package
// source, object, workspace or vendor input requires a new reviewed contract.
func ghaFreshCaptureTreeMatches(entries []ghaGuardGitTreeEntry,p ghaFreshProducerContract) bool {
    if !ghaFreshFindTreeFile(entries,p.main)||!ghaFreshFindTreeFile(entries,p.mod)||!ghaFreshNoWorkspace(entries){return false}
    seen:=map[string]bool{}
    for _,e:=range entries {
        folded:=strings.ToLower(e.Path);if seen[folded]{return false};seen[folded]=true
        if e.Path==p.main.name||e.Path==p.mod.name {continue}
        if strings.EqualFold(e.Path,"vendor"){return false}
        if e.Type=="tree"{continue}
        if e.Type!="blob"||e.Mode!="100644"{return false}
        if strings.HasPrefix(e.Path,".")||strings.HasPrefix(e.Path,"_"){continue}
        name:=strings.ToLower(e.Path)
        if strings.HasSuffix(name,"_test.go"){continue}
        if name=="go.sum"||strings.HasSuffix(name,".go")||strings.HasSuffix(name,".s")||strings.HasSuffix(name,".syso")||
            strings.HasSuffix(name,".c")||strings.HasSuffix(name,".cc")||strings.HasSuffix(name,".cpp")||
            strings.HasSuffix(name,".cxx")||strings.HasSuffix(name,".m")||strings.HasSuffix(name,".f")||
            strings.HasSuffix(name,".for")||strings.HasSuffix(name,".f90")||strings.HasSuffix(name,".h")||
            strings.HasSuffix(name,".hh")||strings.HasSuffix(name,".hpp")||strings.HasSuffix(name,".hxx")||
            strings.HasSuffix(name,".swig")||strings.HasSuffix(name,".swigcxx"){return false}
    }
    return true
}

func ghaFreshCheckContractBlob(ctx context.Context,client *http.Client,w ghaFreshContractFile) error {
    if !ghaGuardHex(w.blob,40)||!ghaGuardHex(w.sha,64)||w.size<=0||w.size>ghaGuardGitObjectLimit{return errGHAGuardUnqualified}
    raw,err:=ghaGuardGitGet(ctx,client,ghaGuardGitAPI+"/git/blobs/"+w.blob,"application/vnd.github.raw+json",w.size)
    if err!=nil{return errGHAGuardUnqualified};defer clear(raw)
    if int64(len(raw))!=w.size||digest(raw)!=w.sha||ghaGuardGitObjectID("blob",raw)!=w.blob||ctx.Err()!=nil{return errGHAGuardUnqualified}
    return nil
}

// Test-only policy injection is not a CLI input or candidate issuer.
func ghaFreshVerifyProducerContractWith(ctx context.Context,public,authenticated *http.Client,producer string,records []ghaGuardSourceRecord,p ghaFreshProducerContract) error {
    if ctx==nil||ctx.Err()!=nil||public==nil||authenticated==nil||!ghaGuardHex(producer,40)||len(records)!=249{return errGHAGuardUnqualified}
    sourceClient,err:=ghaFreshSplitSourceClient(public,authenticated);if err!=nil{return errGHAGuardUnqualified}
    if ghaFreshRequireAncestor(ctx,public,ghaGuardSource,producer)!=nil||ghaFreshRequireAncestor(ctx,public,ghaFreshProducerAnchor,producer)!=nil{return errGHAGuardUnqualified}
    commit,err:=ghaFreshCurrentObject(ctx,public,ghaGuardGitAPI+"/git/commits/"+producer);if err!=nil{return errGHAGuardUnqualified}
    got,ok:=ghaFreshString(commit,"sha");tree,tok:=ghaFreshCurrentNestedSHA(commit,"tree")
    if !ok||got!=producer||!tok||ghaGuardVerifyGitTreeSnapshot(ctx,sourceClient,producer,tree,records)!=nil{return errGHAGuardUnqualified}
    root,err:=ghaGuardGitTree(ctx,sourceClient,tree);if err!=nil||!ghaFreshNoWorkspace(root)||!ghaFreshNoCaseAlias(root,".github","tools","repair-guard"){return errGHAGuardUnqualified}
    gh,err:=ghaGuardGitSubtree(root,".github");if err!=nil{return errGHAGuardUnqualified}
    ghEntries,err:=ghaGuardGitTree(ctx,sourceClient,gh);if err!=nil||!ghaFreshNoCaseAlias(ghEntries,"workflows"){return errGHAGuardUnqualified}
    workflows,err:=ghaGuardGitSubtree(ghEntries,"workflows");if err!=nil{return errGHAGuardUnqualified}
    workflowEntries,err:=ghaGuardGitTree(ctx,sourceClient,workflows)
    if err!=nil||!ghaFreshNoCaseAlias(workflowEntries,p.workflow.name)||!ghaFreshFindTreeFile(workflowEntries,p.workflow){return errGHAGuardUnqualified}
    toolsID,err:=ghaGuardGitSubtree(root,"tools");if err!=nil{return errGHAGuardUnqualified}
    tools,err:=ghaGuardGitTree(ctx,sourceClient,toolsID);if err!=nil||!ghaFreshNoWorkspace(tools)||!ghaFreshNoCaseAlias(tools,"guard-gha-capture","civ6-ci-contract"){return errGHAGuardUnqualified}
    captureID,err:=ghaGuardGitSubtree(tools,"guard-gha-capture");if err!=nil{return errGHAGuardUnqualified}
    capture,err:=ghaGuardGitTree(ctx,sourceClient,captureID);if err!=nil||!ghaFreshCaptureTreeMatches(capture,p){return errGHAGuardUnqualified}
    for _,item:=range []ghaFreshContractFile{p.workflow,p.main,p.mod} {
        if ghaFreshCheckContractBlob(ctx,authenticated,item)!=nil{return errGHAGuardUnqualified}
    }
    if ctx.Err()!=nil{return errGHAGuardUnqualified}
    return nil
}

func ghaFreshVerifyProducerContract(ctx context.Context,public,authenticated *http.Client,producer string,records []ghaGuardSourceRecord) error {
    return ghaFreshVerifyProducerContractWith(ctx,public,authenticated,producer,records,ghaFreshFixedProducerContract())
}

// Validate the complete source inventory against one main revision and repeat
// the main sample. This observes a boundary; it does not lock remote main.
func ghaFreshVerifyCurrentRecords(ctx context.Context,public,authenticated *http.Client,records []ghaGuardSourceRecord,producer string) error {
    return ghaFreshVerifyCurrentRecordsWith(ctx,public,authenticated,records,producer,ghaFreshFixedProducerContract())
}

// The policy argument is a package-test seam and never supplied by the CLI.
func ghaFreshVerifyCurrentRecordsWith(ctx context.Context,public,authenticated *http.Client,records []ghaGuardSourceRecord,producer string,policy ghaFreshProducerContract) (result error) {
    if ctx==nil{return errGHAGuardUnqualified}
    if ctx.Err()!=nil||public==nil||authenticated==nil||public.Transport==nil||authenticated.Transport==nil||len(records)!=249||!ghaGuardHex(producer,40){return errors.Join(errGHAGuardUnqualified,ctx.Err())}
    publicStatus:=&ghaFreshSourceStatusTransport{base:public.Transport}
    authenticatedStatus:=&ghaFreshSourceStatusTransport{base:authenticated.Transport}
    pub:=*public;pub.Transport=publicStatus
    auth:=*authenticated;auth.Transport=authenticatedStatus
    public=&pub;authenticated=&auth
    defer func(){
        if publicStatus.observedFailure(){result=errors.Join(result,publicStatus.sourceFailure(ctx))}
        if authenticatedStatus.observedFailure(){result=errors.Join(result,authenticatedStatus.sourceFailure(ctx))}
        if ctx.Err()!=nil{result=errors.Join(errGHAGuardUnqualified,result,ctx.Err())}
    }()
    sourceClient,err:=ghaFreshSplitSourceClient(public,authenticated);if err!=nil{return errGHAGuardUnqualified}
    first,err:=ghaFreshReadCurrentMain(ctx,public,producer);if err!=nil{return errGHAGuardUnqualified}
    if ghaFreshVerifyProducerContractWith(ctx,public,authenticated,producer,records,policy)!=nil{return errGHAGuardUnqualified}
    if first.head!=producer&&ghaFreshVerifyProducerContractWith(ctx,public,authenticated,first.head,records,policy)!=nil{return errGHAGuardUnqualified}
    if ghaGuardVerifyGitTreeSnapshot(ctx,sourceClient,first.head,first.tree,records)!=nil{return errGHAGuardUnqualified}
    if ghaGuardVerifyGitBlobs(ctx,authenticated,records)!=nil{return errGHAGuardUnqualified}
    last,err:=ghaFreshReadCurrentMain(ctx,public,producer)
    if err!=nil||last!=first||ctx.Err()!=nil{return errGHAGuardUnqualified}
    return nil
}

// Used inside the existing verified-candidate constructor, while the same
// archive and manifest are still held. This does not issue a capability.
func ghaFreshLocatorMatchesGeneration(l ghaFreshPublicLocator,g ghaGuardGeneration) bool {
    return ghaGuardGenerationValid(g)&&ghaFreshLocatorValid(l,time.Now().UTC())&&l.hint.runID==g.RunID&&l.hint.attempt==int64(g.RunAttempt)&&
        l.hint.producerRevision==g.ProducerRevision&&l.jobID==g.JobID&&l.checkRunID==g.CheckRunID&&l.artifactID==g.ArtifactID
}

func ghaFreshAuthorityMatches(a *ghaGuardEvidenceAuthority,g ghaGuardGeneration) bool {
    if a==nil||a.client==nil||a.credential==nil||a.client.Transport!=a.credential||!ghaGuardGenerationValid(g){return false}
    session:=a.credential
    session.mu.Lock();defer session.mu.Unlock()
    if !session.ready||session.closed||session.root==nil||session.root.Err()!=nil||len(session.secret)==0||session.fresh==nil{return false}
    l:=*session.fresh
    return ghaFreshLocatorMatchesGeneration(l,g)
}

func ghaFreshAuthorityHasLocator(a *ghaGuardEvidenceAuthority,l ghaFreshPublicLocator) bool {
    if a==nil||a.credential==nil{return false}
    a.credential.mu.Lock();defer a.credential.mu.Unlock()
    return a.credential.fresh!=nil&&*a.credential.fresh==l
}

func ghaFreshLocatorSHA(l ghaFreshPublicLocator) string {
    v:=struct { RunID,Attempt,JobID,CheckID,ArtifactID,ArchiveBytes int64; Revision,Created,ArchiveSHA,Expires string }{
        l.hint.runID,l.hint.attempt,l.jobID,l.checkRunID,l.artifactID,l.archiveBytes,l.hint.producerRevision,l.hint.createdAt.UTC().Format(time.RFC3339Nano),l.archiveSHA,l.expiresAt.UTC().Format(time.RFC3339Nano)}
    b,err:=json.Marshal(v);if err!=nil{return ""};defer clear(b)
    return digest(b)
}

func ghaFreshSameLocator(a,b ghaFreshPublicLocator) bool {
    return a.hint==b.hint&&a.jobID==b.jobID&&a.checkRunID==b.checkRunID&&a.artifactID==b.artifactID&&
        a.archiveBytes==b.archiveBytes&&a.archiveSHA==b.archiveSHA&&a.expiresAt.Equal(b.expiresAt)
}

func ghaFreshCheckHintGeneration(ctx context.Context,public,authenticated *http.Client,g ghaGuardGeneration,expected ghaFreshPublicLocator) error {
    if ctx==nil{return errGHAGuardUnqualified}
    if ctx.Err()!=nil||public==nil||authenticated==nil{return errors.Join(errGHAGuardUnqualified,ctx.Err())}
    h,err:=ghaFreshReadRunHintWith(ctx,authenticated,authenticated,time.Now().UTC())
    if err!=nil{return ghaFreshSafeCurrentFailure(ctx,err)}
    if h.runID!=g.RunID||h.attempt!=int64(g.RunAttempt)||h.producerRevision!=g.ProducerRevision{return errGHAGuardUnqualified}
    observed,err:=ghaFreshPublicPreflight(ctx,authenticated,h,time.Now().UTC())
    if err!=nil{return ghaFreshSafeCurrentFailure(ctx,err)}
    if !ghaFreshSameLocator(observed,expected)||observed.jobID!=g.JobID||observed.checkRunID!=g.CheckRunID||observed.artifactID!=g.ArtifactID||ctx.Err()!=nil{return errors.Join(errGHAGuardUnqualified,ctx.Err())}
    return nil
}

// Only bounded source HTTP status, cleanup and context sentinels survive the
// fixed verifier boundary; raw endpoint/body/errors are never returned.
func ghaFreshSafeCurrentFailure(ctx context.Context,failure error) error {
    primary:=error(errGHAGuardUnqualified)
    var status *ghaGuardHTTPStatusError
    var public *ghaFreshHTTPStatusError
    if errors.As(failure,&status)&&status!=nil {
        primary=ghaGuardHTTPFailure(status.phase,status.status)
    } else if errors.As(failure,&public)&&public!=nil&&public.status>=100&&public.status<=599 {
        switch public.phase {case "runs","metadata","attestation":primary=errors.Join(primary,&ghaFreshHTTPStatusError{phase:public.phase,status:public.status})}
    }
    if errors.Is(failure,errGHAFreshSourceRead){primary=errors.Join(primary,errGHAFreshSourceRead)}
    var closeErr,cancelErr error
    if errors.Is(failure,errGHAGuardResponseCleanup){closeErr=errGHAGuardResponseCleanup}
    if errors.Is(failure,context.Canceled){cancelErr=context.Canceled}
    if errors.Is(failure,context.DeadlineExceeded){cancelErr=errors.Join(cancelErr,context.DeadlineExceeded)}
    if ctx!=nil {cancelErr=errors.Join(cancelErr,ctx.Err())}
    return errors.Join(primary,closeErr,cancelErr)
}

func ghaFreshVerifyCurrentArchiveInputs(ctx context.Context,public *http.Client,a *ghaGuardEvidenceAuthority,archive []byte,m ghaGuardManifest) error {
    return ghaFreshVerifyCurrentArchiveInputsWith(ctx,public,a,archive,m,ghaFreshFixedProducerContract())
}

func ghaFreshVerifyCurrentArchiveInputsWith(ctx context.Context,public *http.Client,a *ghaGuardEvidenceAuthority,archive []byte,m ghaGuardManifest,policy ghaFreshProducerContract) error {
    if ctx==nil{return errGHAGuardUnqualified}
    if ctx.Err()!=nil||public==nil||a==nil||a.credential==nil{return errors.Join(errGHAGuardUnqualified,ctx.Err())}
    a.credential.mu.Lock();var locator ghaFreshPublicLocator;if a.credential.fresh!=nil{locator=*a.credential.fresh};a.credential.mu.Unlock()
    g:=ghaGuardGeneration{Source:m.Source,SourceTree:m.SourceTree,ProducerRevision:m.ProducerRevision,RunID:m.RunID,RunAttempt:m.RunAttempt,JobID:m.JobID,CheckRunID:m.CheckRunID,ArtifactID:locator.artifactID,OutputSHA:m.OutputSHA,ProofSHA:m.SourceManifestSHA}
    if m.Domain!="guard-gha-capture-v1"||m.RepositoryID!=1406818713||m.OutputName!="guard.exe"||m.OutputBytes<=0||m.OutputBytes>ghaGuardPayloadLimit||!ghaFreshAuthorityMatches(a,g){return errGHAGuardUnqualified}
    if err:=ghaFreshCheckHintGeneration(ctx,public,a.client,g,locator);err!=nil{return ghaFreshSafeCurrentFailure(ctx,err)}
    records,err:=ghaGuardArchiveSourceRecords(ctx,archive,m,public)
    if err!=nil{return ghaFreshSafeCurrentFailure(ctx,err)}
    defer clear(records)
    if err=ghaFreshVerifyCurrentRecordsWith(ctx,a.client,a.client,records,g.ProducerRevision,policy);err!=nil{return ghaFreshSafeCurrentFailure(ctx,err)}
    if !ghaFreshAuthorityMatches(a,g){return errGHAGuardUnqualified}
    if err=ghaFreshCheckHintGeneration(ctx,public,a.client,g,locator);err!=nil{return ghaFreshSafeCurrentFailure(ctx,err)}
    return nil
}

// This record is bound to the already-issued candidate and its proof digest.
// It cannot open the target, authorize update, or be recovered from JSON.
type ghaFreshCurrentInputs struct {
    mu sync.Mutex
    self *ghaFreshCurrentInputs
    artifact *guardTargetCandidate
    owner *ghaGuardOwnedCandidate
    generation ghaGuardGeneration
    locator ghaFreshPublicLocator
    locatorSHA string
    records []ghaGuardSourceRecord
    recordSHA string
    closed,active,revoked,committed bool
}

// A live use is retained across network waits and the observation callback.
// Close revokes admission and reports incomplete while that use is active.
func (p *ghaFreshCurrentInputs) beginUse() error {
    if p==nil{return errGHAGuardUnqualified}
    p.mu.Lock();defer p.mu.Unlock()
    if p.self!=p||p.closed||p.revoked||!p.committed||p.active{return errGHAGuardUnqualified}
    p.active=true;return nil
}
func (p *ghaFreshCurrentInputs) useLive() bool {
    if p==nil{return false}
    p.mu.Lock();defer p.mu.Unlock()
    return p.self==p&&p.committed&&p.active&&!p.closed&&!p.revoked
}
func (p *ghaFreshCurrentInputs) endUse() error {
    p.mu.Lock();defer p.mu.Unlock()
    if p.self!=p||!p.active{return errGuardTargetCleanup}
    p.active=false
    if p.revoked{return errGuardTargetCleanup}
    return nil
}

func ghaFreshRecordsSHA(records []ghaGuardSourceRecord) (string,error) {
    raw,err:=json.Marshal(records);if err!=nil{return "",errGHAGuardUnqualified}
    defer clear(raw)
    return digest(raw),nil
}

func ghaFreshBindCurrentInputs(ctx context.Context,a *guardTargetCandidate,archive []byte,m ghaGuardManifest,client *http.Client) (*ghaFreshCurrentInputs,error) {
    if ctx==nil||ctx.Err()!=nil||a==nil||client==nil{return nil,errGHAGuardUnqualified}
    owner,g,ok:=a.binding()
    if !ok||owner==nil||m.Domain!="guard-gha-capture-v1"||m.RepositoryID!=1406818713||
        m.Source!=g.Source||m.SourceTree!=g.SourceTree||m.ProducerRevision!=g.ProducerRevision||
        m.RunID!=g.RunID||m.RunAttempt!=g.RunAttempt||
        m.JobID!=g.JobID||m.CheckRunID!=g.CheckRunID||m.SourceManifestSHA!=g.ProofSHA||
        m.OutputName!="guard.exe"||m.OutputSHA!=g.OutputSHA {return nil,errGHAGuardUnqualified}
    a.mu.Lock();size:=a.payloadBytes;a.mu.Unlock()
    if m.OutputBytes!=int64(size){return nil,errGHAGuardUnqualified}
    records,err:=ghaGuardArchiveSourceRecords(ctx,archive,m,client)
    if err!=nil||len(records)!=249{return nil,errGHAGuardUnqualified}
    records=append([]ghaGuardSourceRecord(nil),records...)
    sum,err:=ghaFreshRecordsSHA(records);if err!=nil{clear(records);return nil,errGHAGuardUnqualified}
    after,again,valid:=a.binding()
    if !valid||after!=owner||again!=g||ctx.Err()!=nil{clear(records);return nil,errGHAGuardUnqualified}
    proof:=&ghaFreshCurrentInputs{artifact:a,owner:owner,generation:g,records:records,recordSHA:sum}
    proof.self=proof
    return proof,nil
}

func (p *ghaFreshCurrentInputs) Recheck(ctx context.Context,a *guardTargetCandidate,public *http.Client,authority *ghaGuardEvidenceAuthority) error {
    return p.recheckWith(ctx,a,public,authority,ghaFreshFixedProducerContract())
}

func (p *ghaFreshCurrentInputs) recheckWith(ctx context.Context,a *guardTargetCandidate,public *http.Client,authority *ghaGuardEvidenceAuthority,policy ghaFreshProducerContract) (result error) {
    if p==nil||ctx==nil||ctx.Err()!=nil||a==nil||public==nil||authority==nil{return errGHAGuardUnqualified}
    if err:=p.beginUse();err!=nil{return err}
    defer func(){result=errors.Join(result,p.endUse())}()
    return p.recheckBorrowed(ctx,a,public,authority,policy)
}

func (p *ghaFreshCurrentInputs) recheckBorrowed(ctx context.Context,a *guardTargetCandidate,public *http.Client,authority *ghaGuardEvidenceAuthority,policy ghaFreshProducerContract) error {
    if p==nil||ctx==nil||ctx.Err()!=nil||a==nil||public==nil||authority==nil||!p.useLive(){return errGHAGuardUnqualified}
    owner,g,valid:=a.binding();if !valid||!ghaFreshAuthorityMatches(authority,g){return errGHAGuardUnqualified}
    p.mu.Lock()
    if p.self!=p||p.closed||p.revoked||!p.active||!p.committed||p.artifact!=a||p.owner!=owner||p.generation!=g||len(p.records)!=249||!ghaFreshLocatorValid(p.locator,time.Now().UTC())||p.locatorSHA==""||ghaFreshLocatorSHA(p.locator)!=p.locatorSHA {
        p.mu.Unlock();return errGHAGuardUnqualified
    }
    records:=append([]ghaGuardSourceRecord(nil),p.records...);expected:=p.recordSHA;locator:=p.locator;locatorSHA:=p.locatorSHA
    p.mu.Unlock()
    defer clear(records)
    if !ghaFreshAuthorityHasLocator(authority,locator){return errGHAGuardUnqualified}
    if err:=ghaFreshCheckHintGeneration(ctx,public,authority.client,g,locator);err!=nil{return ghaFreshSafeCurrentFailure(ctx,err)}
    sum,err:=ghaFreshRecordsSHA(records)
    if err!=nil||sum!=expected{return errGHAGuardUnqualified}
    if err=ghaFreshVerifyCurrentRecordsWith(ctx,authority.client,authority.client,records,g.ProducerRevision,policy);err!=nil{return ghaFreshSafeCurrentFailure(ctx,err)}
    if !ghaFreshAuthorityMatches(authority,g){return errGHAGuardUnqualified}
    owner2,g2,valid:=a.binding()
    p.mu.Lock()
    sum2,hashErr:=ghaFreshRecordsSHA(p.records)
    alive:=p.self==p&&!p.closed&&!p.revoked&&p.active&&p.committed&&p.artifact==a&&p.owner==owner&&p.generation==g&&p.locator==locator&&p.locatorSHA==locatorSHA&&ghaFreshLocatorSHA(p.locator)==locatorSHA&&sum2==expected&&hashErr==nil
    p.mu.Unlock()
    if !valid||owner2!=owner||g2!=g||!alive||!ghaFreshAuthorityHasLocator(authority,locator)||ctx.Err()!=nil{return errors.Join(errGHAGuardUnqualified,ctx.Err())}
    if err:=ghaFreshCheckHintGeneration(ctx,public,authority.client,g,locator);err!=nil{return ghaFreshSafeCurrentFailure(ctx,err)}
    if !p.useLive(){return errGuardTargetCleanup}
    return nil
}

func (p *ghaFreshCurrentInputs) Close() error {
    if p==nil{return nil}
    p.mu.Lock();defer p.mu.Unlock()
    if p.self!=p{return errGHAGuardUnqualified}
    if p.closed{return nil}
    p.revoked=true
    if p.active{return errGuardTargetCleanup}
    p.closed=true;clear(p.records);p.records=nil;p.artifact=nil;p.owner=nil;p.recordSHA="";p.locator=ghaFreshPublicLocator{};p.locatorSHA=""
    return nil
}

// The capture retains only the exact server-bound archive until it is bound to
// the candidate that survived signature/source verification. It is not an
// issuer and never accepts a path or caller-supplied update decision.
type ghaFreshUpdateArchiveCapture struct {
    mu sync.Mutex
    self *ghaFreshUpdateArchiveCapture
    locator ghaFreshPublicLocator
    manifest ghaGuardManifest
    archive []byte
    pending *ghaFreshCurrentInputs
    started,held,used,active,revoked,closed bool
}

func ghaFreshNewUpdateArchiveCapture() *ghaFreshUpdateArchiveCapture {
    c:=&ghaFreshUpdateArchiveCapture{};c.self=c;return c
}

func (c *ghaFreshUpdateArchiveCapture) Start(l ghaFreshPublicLocator) error {
    if c==nil||!ghaFreshLocatorValid(l,time.Now().UTC()){return errGHAGuardUnqualified}
    c.mu.Lock();defer c.mu.Unlock()
    if c.self!=c||c.closed||c.started{return errGHAGuardUnqualified}
    c.locator=l;c.started=true;return nil
}

func (c *ghaFreshUpdateArchiveCapture) Hold(raw []byte,m ghaGuardManifest) error {
    if c==nil{return errGHAGuardUnqualified}
    c.mu.Lock();defer c.mu.Unlock()
    l:=c.locator
    if c.self!=c||c.closed||!c.started||c.held||c.used||!ghaFreshLocatorValid(l,time.Now().UTC())||
        int64(len(raw))!=l.archiveBytes||digest(raw)!=l.archiveSHA||
        m.Domain!="guard-gha-capture-v1"||m.RepositoryID!=1406818713||m.Source!=ghaGuardSource||m.SourceTree!=ghaGuardSourceTree||
        m.ProducerRevision!=l.hint.producerRevision||m.RunID!=l.hint.runID||int64(m.RunAttempt)!=l.hint.attempt||
        m.JobID!=l.jobID||m.CheckRunID!=l.checkRunID||m.OutputName!="guard.exe"||
        !ghaGuardHex(m.OutputSHA,64)||!ghaGuardHex(m.SourceManifestSHA,64){return errGHAGuardUnqualified}
    c.archive=append([]byte(nil),raw...);c.manifest=m;c.held=true
    return nil
}

// Binding consumes the capture once; only the same already-issued owned
// candidate whose proof digest matches these bytes can retain the record set.
func (c *ghaFreshUpdateArchiveCapture) Bind(ctx context.Context,a *guardTargetCandidate,client *http.Client) (*ghaFreshCurrentInputs,error) {
    return c.bindWith(ctx,a,client,nil)
}

// The hook is a package-test seam; production has no callback or CLI input.
func (c *ghaFreshUpdateArchiveCapture) bindWith(ctx context.Context,a *guardTargetCandidate,client *http.Client,hook func(string)) (out *ghaFreshCurrentInputs,resultErr error) {
    if c==nil||ctx==nil||ctx.Err()!=nil||a==nil||client==nil{return nil,errGHAGuardUnqualified}
    c.mu.Lock()
    if c.self!=c||c.closed||c.revoked||c.active||!c.started||!c.held||c.used||!ghaFreshLocatorValid(c.locator,time.Now().UTC()) {
        c.mu.Unlock();return nil,errGHAGuardUnqualified
    }
    raw,m,loc:=c.archive,c.manifest,c.locator
    c.archive=nil;c.used=true;c.held=false;c.active=true
    c.mu.Unlock()
    completed:=false
    defer func(){
        clear(raw)
        c.mu.Lock()
        if !completed||c.self!=c||c.revoked||c.closed||ctx.Err()!=nil {
            discard:=out;out=nil
            resultErr=errors.Join(errGHAGuardUnqualified,resultErr,ctx.Err())
            if c.revoked {resultErr=errors.Join(resultErr,errGuardTargetCleanup)}
            c.mu.Unlock()
            if discard!=nil {resultErr=errors.Join(resultErr,discard.Close())}
            c.mu.Lock();c.active=false;c.mu.Unlock()
            return
        }
        if out!=nil {c.pending=out}
        c.active=false;c.mu.Unlock()
    }()
    if hook!=nil {hook("active")}
    if int64(len(raw))!=loc.archiveBytes||digest(raw)!=loc.archiveSHA{return nil,errGHAGuardUnqualified}
    owner,g,ok:=a.binding()
    if !ok||owner==nil||g.RunID!=loc.hint.runID||int64(g.RunAttempt)!=loc.hint.attempt||g.ProducerRevision!=loc.hint.producerRevision||
        g.JobID!=loc.jobID||g.CheckRunID!=loc.checkRunID||g.ArtifactID!=loc.artifactID{return nil,errGHAGuardUnqualified}
    proof,err:=ghaFreshBindCurrentInputs(ctx,a,raw,m,client)
    if err!=nil||proof==nil{return nil,errGHAGuardUnqualified}
    if ctx.Err()!=nil||int64(len(raw))!=loc.archiveBytes||digest(raw)!=loc.archiveSHA {closeErr:=proof.Close();return nil,errors.Join(errGHAGuardUnqualified,closeErr,ctx.Err())}
    proof.mu.Lock();proof.locator=loc;proof.locatorSHA=ghaFreshLocatorSHA(loc);proof.mu.Unlock()
    out=proof
    if hook!=nil {hook("before-pending")}
    completed=true
    return out,nil
}

// Bind returns a pending proof. It is unusable until the same receiver claims
// it, so a concurrent Close cannot both report complete and release a proof.
func (c *ghaFreshUpdateArchiveCapture) ClaimBound(p *ghaFreshCurrentInputs) error {
    if c==nil||p==nil{return errGHAGuardUnqualified}
    c.mu.Lock();defer c.mu.Unlock()
    if c.self!=c||c.closed||c.revoked||c.active||c.pending!=p{return errGuardTargetCleanup}
    p.mu.Lock();defer p.mu.Unlock()
    if p.self!=p||p.closed||p.revoked||p.active||p.committed{return errGHAGuardUnqualified}
    p.committed=true;c.pending=nil;return nil
}

// A rejected transfer remains owned here until the exact pending proof closes.
func (c *ghaFreshUpdateArchiveCapture) DiscardBound(p *ghaFreshCurrentInputs) error {
    if c==nil||p==nil{return errGHAGuardUnqualified}
    c.mu.Lock();defer c.mu.Unlock()
    if c.self!=c||c.active||c.pending!=p{return errGuardTargetCleanup}
    if err:=p.Close();err!=nil{return errors.Join(errGuardTargetCleanup,err)}
    c.pending=nil;return nil
}

func (c *ghaFreshUpdateArchiveCapture) Close() error {
    if c==nil{return nil}
    c.mu.Lock();defer c.mu.Unlock()
    if c.self!=c{return errGHAGuardUnqualified}
    if c.closed{return nil}
    c.revoked=true
    if c.active||c.pending!=nil{return errGuardTargetCleanup}
    c.closed=true;clear(c.archive);c.archive=nil;c.locator=ghaFreshPublicLocator{};c.manifest=ghaGuardManifest{}
    return nil
}

// A fresh session can be opened only from the same still-live candidate and
// code-owned locator. CredReadW requires the existing whole-flight supervisor.
type ghaFreshAuthorityOpener func(context.Context,ghaFreshPublicLocator,*ghaFreshCredentialAudit) (*ghaGuardEvidenceAuthority,error)

func (p *ghaFreshCurrentInputs) OpenAuthority(ctx context.Context,a *guardTargetCandidate,audit *ghaFreshCredentialAudit) (*ghaGuardEvidenceAuthority,error) {
    return p.openAuthorityWith(ctx,a,audit,ghaFreshOpenWinCredAuthorityObserved)
}

// opener is a package-test seam. Production always uses the fixed WinCred
// function, and proof/locator checks precede that function.
func (p *ghaFreshCurrentInputs) openAuthorityWith(ctx context.Context,a *guardTargetCandidate,audit *ghaFreshCredentialAudit,open ghaFreshAuthorityOpener) (result *ghaGuardEvidenceAuthority,resultErr error) {
    if audit!=nil {audit.state="unknown"}
    if p==nil||ctx==nil||ctx.Err()!=nil||a==nil||open==nil{return nil,errGHAGuardUnqualified}
    if err:=p.beginUse();err!=nil{return nil,err}
    defer func(){
        if endErr:=p.endUse();endErr!=nil {
            if result!=nil {resultErr=ghaFreshCloseCredentialAuthority(result,errors.Join(resultErr,endErr),audit);result=nil} else {resultErr=errors.Join(resultErr,endErr)}
        }
    }()
    owner,g,valid:=a.binding();if !valid{return nil,errGHAGuardUnqualified}
    p.mu.Lock()
    l:=p.locator;sum,hashErr:=ghaFreshRecordsSHA(p.records);expected:=p.recordSHA;locatorSHA:=p.locatorSHA
    alive:=p.self==p&&!p.closed&&!p.revoked&&p.active&&p.committed&&p.artifact==a&&p.owner==owner&&p.generation==g&&len(p.records)==249&&
        hashErr==nil&&sum==expected&&locatorSHA!=""&&ghaFreshLocatorSHA(l)==locatorSHA&&ghaFreshLocatorMatchesGeneration(l,g)
    p.mu.Unlock()
    if !alive{return nil,errGHAGuardUnqualified}
    authority,err:=open(ctx,l,audit)
    if authority==nil{return nil,errors.Join(errGHAGuardUnqualified,err)}
    if err!=nil{return nil,ghaFreshCloseCredentialAuthority(authority,errors.Join(errGHAGuardUnqualified,err),audit)}
    owner2,g2,valid:=a.binding()
    p.mu.Lock()
    sum,hashErr=ghaFreshRecordsSHA(p.records)
    alive=p.self==p&&!p.closed&&!p.revoked&&p.active&&p.committed&&p.artifact==a&&p.owner==owner&&p.generation==g&&p.locator==l&&p.locatorSHA==locatorSHA&&
        ghaFreshLocatorSHA(p.locator)==locatorSHA&&len(p.records)==249&&hashErr==nil&&sum==expected
    p.mu.Unlock()
    if !valid||owner2!=owner||g2!=g||!alive||!ghaFreshAuthorityMatches(authority,g)||!ghaFreshAuthorityHasLocator(authority,l)||ctx.Err()!=nil {
        return nil,ghaFreshCloseCredentialAuthority(authority,errGHAGuardUnqualified,audit)
    }
    return authority,nil
}
