//go:build windows

package main

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "strings"
    "sync"
    "time"
)

// The server reader is not a verifier or update authority. It is used only by
// a future fixed provider after independently authenticating its publication
// binding and validating the actual attestation. No normal runtime wires it.
type ghaGuardServerEvidence struct {
    run ghaGuardRun
    job ghaGuardJob
    artifact ghaGuardArtifact
    binding ghaGuardCaptureBinding
}

func ghaGuardServerJSON(ctx context.Context,client *http.Client,endpoint string,out any) error {
    if ctx==nil||ctx.Err()!=nil||client==nil||!strings.HasPrefix(endpoint,"https://api.github.com/repos/AutoDevJapan/cc-pc-tools-rs/actions/") {return errGHAGuardUnqualified}
    bounded,cancel:=context.WithTimeout(ctx,15*time.Second);defer cancel();ctx=bounded
    req,err:=http.NewRequestWithContext(ctx,http.MethodGet,endpoint,nil);if err!=nil{return errGHAGuardUnqualified}
    req.Header.Set("Accept","application/vnd.github+json");req.Header.Set("X-GitHub-Api-Version","2022-11-28")
    noRedirect:=*client;noRedirect.CheckRedirect=func(*http.Request,[]*http.Request)error{return errGHAGuardUnqualified}
    response,err:=noRedirect.Do(req)
    if err!=nil||response==nil||response.Body==nil{return errGHAGuardUnqualified}
    if response.StatusCode!=http.StatusOK||response.Header.Get("Link")!="" {_=response.Body.Close();return errGHAGuardUnqualified}
    raw,readErr:=io.ReadAll(io.LimitReader(response.Body,2<<20+1));closeErr:=response.Body.Close()
    if readErr!=nil||closeErr!=nil||len(raw)>2<<20||ctx.Err()!=nil {clear(raw);return errGHAGuardUnqualified}
    defer clear(raw)
    dec:=json.NewDecoder(bytes.NewReader(raw))
    if dec.Decode(out)!=nil||dec.Decode(new(any))!=io.EOF{return errGHAGuardUnqualified}
    return nil
}

func ghaGuardFetchServerEvidence(ctx context.Context,client *http.Client,e ghaGuardMetadataExpectation,binding ghaGuardCaptureBinding,now time.Time) (*ghaGuardServerEvidence,error) {
    if e.RepositoryID!=1406818713||e.RunID<=0||e.RunAttempt<=0||e.JobID<=0||e.ArtifactID<=0 {return nil,errGHAGuardUnqualified}
    base:="https://api.github.com/repos/AutoDevJapan/cc-pc-tools-rs/actions"
    var runRaw struct {
        ID int64 `json:"id"`
        Attempt int `json:"run_attempt"`
        Event,Path,Status,Conclusion string
        WorkflowID int64 `json:"workflow_id"`
        Repository struct{ID int64 `json:"id"`} `json:"repository"`
        HeadRepository struct{ID int64 `json:"id"`} `json:"head_repository"`
        HeadSHA string `json:"head_sha"`
        HeadBranch string `json:"head_branch"`
    }
    if ghaGuardServerJSON(ctx,client,fmt.Sprintf("%s/runs/%d",base,e.RunID),&runRaw)!=nil{return nil,errGHAGuardUnqualified}
    run:=ghaGuardRun{RepositoryID:runRaw.Repository.ID,HeadRepositoryID:runRaw.HeadRepository.ID,HeadSHA:runRaw.HeadSHA,HeadBranch:runRaw.HeadBranch,WorkflowPath:runRaw.Path,WorkflowID:runRaw.WorkflowID,Event:runRaw.Event,RunID:runRaw.ID,RunAttempt:runRaw.Attempt,Status:runRaw.Status,Conclusion:runRaw.Conclusion}
    var jobs struct {
        Total int `json:"total_count"`
        Jobs []struct {
            ID int64 `json:"id"`
            RunID int64 `json:"run_id"`
            Name,Status,Conclusion string
            CheckRunURL string `json:"check_run_url"`
            Steps []struct{Name,Status,Conclusion string;Number int `json:"number"`}
        } `json:"jobs"`
    }
    if ghaGuardServerJSON(ctx,client,fmt.Sprintf("%s/runs/%d/attempts/%d/jobs?per_page=100",base,e.RunID,e.RunAttempt),&jobs)!=nil||jobs.Total!=len(jobs.Jobs)||len(jobs.Jobs)>100{return nil,errGHAGuardUnqualified}
    var job ghaGuardJob;matches:=0
    expectedCheck:=fmt.Sprintf("https://api.github.com/repos/AutoDevJapan/cc-pc-tools-rs/check-runs/%d",e.CheckRunID)
    for _,v:=range jobs.Jobs {
        if v.Name!=e.JobName {continue}
        matches++
        if v.ID!=e.JobID||v.RunID!=e.RunID||v.CheckRunURL!=expectedCheck {continue}
        order:=[]string{"Capture the fixed guard build","Check the exact captured files","Attest the guard and source evidence","Upload the fixed guard capture","Record the server-assigned capture artifact"}
        required:=map[string]bool{};for _,name:=range order {required[name]=true}
        next,last:=0,0;validSteps:=true
        for _,step:=range v.Steps {
            if !required[step.Name]{continue}
            if next>=len(order)||step.Name!=order[next]||step.Status!="completed"||step.Conclusion!="success"||step.Number<=last {validSteps=false;break}
            last=step.Number;next++
        }
        job=ghaGuardJob{RunID:v.RunID,JobID:v.ID,CheckRunID:e.CheckRunID,RunAttempt:e.RunAttempt,Name:v.Name,Status:v.Status,Conclusion:v.Conclusion,UploadStepCompleted:validSteps&&next==len(order)}
    }
    if matches!=1{return nil,errGHAGuardUnqualified}
    var artifacts struct {
        Total int `json:"total_count"`
        Artifacts []struct {
            ID int64 `json:"id"`
            Name,Digest string
            Expired bool
            ExpiresAt time.Time `json:"expires_at"`
            SizeInBytes int64 `json:"size_in_bytes"`
            WorkflowRun struct {
                ID int64 `json:"id"`
                RepositoryID int64 `json:"repository_id"`
                HeadRepositoryID int64 `json:"head_repository_id"`
                HeadSHA string `json:"head_sha"`
            } `json:"workflow_run"`
        } `json:"artifacts"`
    }
    if ghaGuardServerJSON(ctx,client,fmt.Sprintf("%s/runs/%d/artifacts?per_page=100",base,e.RunID),&artifacts)!=nil||artifacts.Total!=len(artifacts.Artifacts)||len(artifacts.Artifacts)>100{return nil,errGHAGuardUnqualified}
    var artifact ghaGuardArtifact;matches=0
    for _,v:=range artifacts.Artifacts {
        if v.Name!=e.ArtifactName {continue}
        matches++
        if v.ID!=e.ArtifactID {continue}
        artifact=ghaGuardArtifact{RepositoryID:v.WorkflowRun.RepositoryID,HeadRepositoryID:v.WorkflowRun.HeadRepositoryID,HeadSHA:v.WorkflowRun.HeadSHA,RunID:v.WorkflowRun.ID,ID:v.ID,Name:v.Name,Size:v.SizeInBytes,Digest:v.Digest,Expired:v.Expired,ExpiresAt:v.ExpiresAt}
    }
    if matches!=1||!ghaGuardMetadataMatches(e,run,job,artifact,binding,now){return nil,errGHAGuardUnqualified}
    return &ghaGuardServerEvidence{run:run,job:job,artifact:artifact,binding:binding},nil
}

// A normal provider must be created by the existing authenticated GitHub
// transport owner. This package does not parse caller-supplied credentials,
// paths, reports or hashes into that capability.
// The normal acquisition must come from an authenticated owner of the
// existing GitHub connection. A caller-supplied observation is not authority.
type ghaGuardEvidenceAuthority struct {
    client *http.Client
}

func ghaGuardNormalEvidenceAuthority() *ghaGuardEvidenceAuthority { return nil }

func ghaGuardActualExpectation() ghaGuardMetadataExpectation {
    return ghaGuardMetadataExpectation{
        RepositoryID:1406818713,HeadRepositoryID:1406818713,
        Source:ghaGuardSource,SourceTree:ghaGuardSourceTree,
        ProducerRevision:"f2f9e4c4cddba70240004e7084443c27c51baee9",
        WorkflowPath:".github/workflows/unattended-readiness.yml",
        Event:"workflow_dispatch",Ref:"refs/heads/main",
        WorkflowID:0,RunID:43574333045,RunAttempt:1,JobID:132385166063,
        CheckRunID:132385166063,JobName:"Guard bootstrap artifact",
        ArtifactID:14105209772,ArtifactName:"guard-bootstrap-43574333045-1-132385166063",
    }
}

// The production entry may call this constructor only with a code-owned,
// authenticated transport. The normal authority factory below is not wired.
type ghaGuardAcquiredEvidence struct {
    run ghaGuardRun
    job ghaGuardJob
    artifact ghaGuardArtifact
    jobLog []byte
    workflowID int64
    workflowPath string
    workflowActive bool
    manifest ghaGuardManifest
    archive []byte
    bundle []byte
}

func ghaGuardConstructVerifiedCandidate(ctx context.Context,e ghaGuardMetadataExpectation,material *ghaGuardAcquiredEvidence) (*ghaGuardVerifiedCandidate,error) {
    return ghaGuardConstructVerifiedCandidateWith(ctx,e,material,ghaGuardVerifyActualSignature,ghaGuardVerifyArchiveGitSource)
}

// verify is injected by package tests only. The normal constructor above fixes
// this to in-process Sigstore verification, never a report or a caller bool.
func ghaGuardConstructVerifiedCandidateWith(ctx context.Context,e ghaGuardMetadataExpectation,material *ghaGuardAcquiredEvidence,verifySignature func(context.Context,[]byte,[]byte,ghaGuardManifest)error,verifySource func(context.Context,[]byte,ghaGuardManifest)error) (*ghaGuardVerifiedCandidate,error) {
    if material!=nil {defer clear(material.archive);defer clear(material.bundle);defer clear(material.jobLog)}
    if ctx==nil||ctx.Err()!=nil||material==nil||verifySignature==nil||verifySource==nil{return nil,errGHAGuardUnqualified}
    bounded,cancel:=context.WithTimeout(ctx,2*time.Minute);defer cancel();ctx=bounded
    if len(material.jobLog)==0||len(material.jobLog)>8<<20||len(material.archive)==0||len(material.archive)>ghaGuardArchiveLimit||len(material.bundle)==0||len(material.bundle)>4<<20{return nil,errGHAGuardUnqualified}
    if material.workflowID<=0||!material.workflowActive||material.workflowPath!=e.WorkflowPath{return nil,errGHAGuardUnqualified}
    if e.WorkflowID==0 {e.WorkflowID=material.workflowID}else if e.WorkflowID!=material.workflowID{return nil,errGHAGuardUnqualified}
    pub,err:=ghaGuardPublicationFromLog(material.jobLog)
    if err!=nil{return nil,errGHAGuardUnqualified}
    binding:=ghaGuardCaptureBinding{RepositoryID:pub.RepositoryID,Source:pub.Source,ProducerRevision:pub.ProducerRevision,RunID:pub.RunID,RunAttempt:pub.RunAttempt,JobID:pub.JobID,CheckRunID:pub.CheckRunID,ArtifactID:pub.ArtifactID}
    logSHA:=digest(material.jobLog)
    if !ghaGuardMetadataMatches(e,material.run,material.job,material.artifact,binding,time.Now().UTC())||!ghaGuardPublicationMatches(e,material.artifact,pub){return nil,errGHAGuardUnqualified}
    m:=material.manifest
    if m.RepositoryID!=e.RepositoryID||m.Source!=e.Source||m.SourceTree!=e.SourceTree||m.ProducerRevision!=e.ProducerRevision||m.RunID!=e.RunID||m.RunAttempt!=e.RunAttempt||m.JobID!=e.JobID||m.CheckRunID!=e.CheckRunID||m.OutputSHA!=pub.OutputSHA||m.SourceManifestSHA!=pub.SourceManifestSHA{return nil,errGHAGuardUnqualified}
    encoded,err:=json.Marshal(m)
    if err!=nil||digest(encoded)!=pub.CaptureSHA{return nil,errGHAGuardUnqualified}
    archiveSHA:=pub.ArchiveSHA;archiveBytes:=material.artifact.Size;bundleSHA:=digest(material.bundle)
    payload,err:=ghaGuardReadArchive(ctx,material.archive,ghaGuardArchiveExpectation{Manifest:m,ArchiveSHA:archiveSHA,ArchiveBytes:archiveBytes})
    if err!=nil{return nil,errGHAGuardUnqualified}
    transferred:=false
    defer func(){if !transferred {clear(payload)}}()
    if verifySource(ctx,material.archive,m)!=nil||ctx.Err()!=nil||digest(material.archive)!=archiveSHA||digest(payload)!=m.OutputSHA{return nil,errGHAGuardUnqualified}
    if verifySignature(ctx,material.bundle,payload,m)!=nil||ctx.Err()!=nil||digest(material.jobLog)!=logSHA||digest(material.bundle)!=bundleSHA||digest(material.archive)!=archiveSHA||digest(payload)!=m.OutputSHA{return nil,errGHAGuardUnqualified}
    afterPub,afterErr:=ghaGuardPublicationFromLog(material.jobLog)
    if afterErr!=nil||afterPub!=pub||!ghaGuardMetadataMatches(e,material.run,material.job,material.artifact,binding,time.Now().UTC())||!ghaGuardPublicationMatches(e,material.artifact,pub){return nil,errGHAGuardUnqualified}
    generation:=ghaGuardGeneration{Source:e.Source,SourceTree:e.SourceTree,ProducerRevision:e.ProducerRevision,RunID:e.RunID,RunAttempt:e.RunAttempt,JobID:e.JobID,CheckRunID:e.CheckRunID,ArtifactID:e.ArtifactID,OutputSHA:m.OutputSHA,ProofSHA:m.SourceManifestSHA}
    if !ghaGuardGenerationValid(generation){return nil,errGHAGuardUnqualified}
    candidate:=&ghaGuardVerifiedCandidate{issuer:&ghaGuardIssuer{token:1},generation:generation,payload:payload}
    candidate.self=candidate
    transferred=true
    return candidate,nil
}

// The normal GitHub transport and download-to-held-buffer operation remain
// unconnected; absence is a fixed failure and not a new authorization grant.
func ghaGuardOpenVerifiedCandidate(ctx context.Context) (*ghaGuardVerifiedCandidate,error) {
    if ctx==nil||ctx.Err()!=nil||ghaGuardNormalEvidenceAuthority()==nil{return nil,errGHAGuardUnqualified}
    return nil,errGHAGuardUnqualified
}

// The issuer is constructed only after the bound acquisition and cryptographic
// checks. It is never parsed from JSON or constructed from a report.
type ghaGuardIssuer struct { token byte }

type ghaGuardGeneration struct {
    Source,SourceTree,ProducerRevision string
    RunID int64
    RunAttempt int
    JobID,CheckRunID,ArtifactID int64
    OutputSHA,ProofSHA string
}

// The active callback pins the buffer until it returns. Close invalidates
// admission immediately and defers erasure until the last borrower returns.
// No current factory can mark this structure issued or fill it from JSON.
type ghaGuardVerifiedCandidate struct {
    self *ghaGuardVerifiedCandidate
    mu sync.Mutex
    closed bool
    issuer *ghaGuardIssuer
    owner *ghaGuardOwnedCandidate
    borrowers int
    drain chan struct{}
    generation ghaGuardGeneration
    payload []byte
}
type ghaGuardOwnedCandidate struct { source *ghaGuardVerifiedCandidate }

func ghaGuardGenerationValid(g ghaGuardGeneration) bool {
    return g.Source==ghaGuardSource&&g.SourceTree==ghaGuardSourceTree&&ghaGuardHex(g.ProducerRevision,40)&&g.RunID>0&&g.RunAttempt>0&&g.JobID>0&&g.CheckRunID>0&&g.ArtifactID>0&&ghaGuardHex(g.OutputSHA,64)&&ghaGuardHex(g.ProofSHA,64)
}

// Claim transfers the exact candidate once, including an invalid partial
// candidate that still needs a unique owner for Close.
func (c *ghaGuardVerifiedCandidate) Claim() (*ghaGuardOwnedCandidate,error) {
    if c==nil||c.self!=c{return nil,errGHAGuardUnqualified}
    c.mu.Lock();defer c.mu.Unlock()
    if c.closed||c.issuer==nil||c.owner!=nil||c.borrowers!=0{return nil,errGHAGuardUnqualified}
    owner:=&ghaGuardOwnedCandidate{source:c};c.owner=owner
    return owner,nil
}
func (c *ghaGuardVerifiedCandidate) authorized(owner *ghaGuardOwnedCandidate) bool {
    if owner==nil{return c.owner==nil}
    return c.owner==owner&&owner.source==c
}
func (c *ghaGuardVerifiedCandidate) verify(owner *ghaGuardOwnedCandidate) bool {
    if c==nil||c.self!=c{return false};c.mu.Lock();defer c.mu.Unlock()
    return c.authorized(owner)&&c.issuer!=nil&&!c.closed&&ghaGuardGenerationValid(c.generation)&&len(c.payload)>0
}
func (c *ghaGuardVerifiedCandidate) generationFor(owner *ghaGuardOwnedCandidate) (ghaGuardGeneration,bool) {
    if c==nil||c.self!=c{return ghaGuardGeneration{},false};c.mu.Lock();defer c.mu.Unlock()
    if !c.authorized(owner)||c.issuer==nil||c.closed||!ghaGuardGenerationValid(c.generation)||len(c.payload)==0{return ghaGuardGeneration{},false}
    return c.generation,true
}
func (c *ghaGuardVerifiedCandidate) borrow(owner *ghaGuardOwnedCandidate,use func([]byte)error) (result error) {
    if c==nil||c.self!=c||use==nil{return errGHAGuardUnqualified}
    c.mu.Lock()
    if !c.authorized(owner)||c.issuer==nil||c.closed||!ghaGuardGenerationValid(c.generation)||len(c.payload)==0||digest(c.payload)!=c.generation.OutputSHA||c.borrowers!=0 {c.mu.Unlock();return errGHAGuardUnqualified}
    c.borrowers++;raw:=c.payload;c.mu.Unlock()
    completed:=false
    defer func(){c.mu.Lock();c.borrowers--;if !completed {c.closed=true;c.issuer=nil};if c.closed&&c.borrowers==0 {clear(c.payload);c.payload=nil;if c.drain!=nil {close(c.drain);c.drain=nil}};c.mu.Unlock()}()
    result=use(raw)
    c.mu.Lock();valid:=c.authorized(owner)&&!c.closed&&c.issuer!=nil&&ghaGuardGenerationValid(c.generation)&&digest(c.payload)==c.generation.OutputSHA;if !valid {c.closed=true;c.issuer=nil};c.mu.Unlock()
    completed=true
    if result!=nil{return result};if !valid{return errGHAGuardUnqualified};return nil
}
func (c *ghaGuardVerifiedCandidate) closeFor(owner *ghaGuardOwnedCandidate) error {
    if c==nil{return nil};if c.self!=c{return errGHAGuardUnqualified};c.mu.Lock();defer c.mu.Unlock()
    if !c.authorized(owner){return errGHAGuardUnqualified}
    c.closed=true;c.issuer=nil
    if c.borrowers==0 {clear(c.payload);c.payload=nil};return nil
}
func (c *ghaGuardVerifiedCandidate) waitFor(ctx context.Context,owner *ghaGuardOwnedCandidate) error {
    if c==nil{return nil};if c.self!=c||ctx==nil{return errGHAGuardUnqualified}
    c.mu.Lock();if !c.authorized(owner)||!c.closed {c.mu.Unlock();return errGHAGuardUnqualified};if c.borrowers==0 {c.mu.Unlock();return nil}
    if c.drain==nil {c.drain=make(chan struct{})};done:=c.drain;c.mu.Unlock()
    select {case <-done:return nil;case <-ctx.Done():return errGHAGuardUnqualified}
}
func (c *ghaGuardVerifiedCandidate) VerifySourceGeneration() bool {return c.verify(nil)}
func (c *ghaGuardVerifiedCandidate) Generation() (ghaGuardGeneration,bool) {return c.generationFor(nil)}
func (c *ghaGuardVerifiedCandidate) WithPinnedPayload(use func([]byte)error) error {return c.borrow(nil,use)}
func (c *ghaGuardVerifiedCandidate) Close() error {return c.closeFor(nil)}
func (c *ghaGuardVerifiedCandidate) WaitClosed(ctx context.Context) error {return c.waitFor(ctx,nil)}
func (o *ghaGuardOwnedCandidate) VerifySourceGeneration() bool {return o!=nil&&o.source.verify(o)}
func (o *ghaGuardOwnedCandidate) Generation() (ghaGuardGeneration,bool) {if o==nil{return ghaGuardGeneration{},false};return o.source.generationFor(o)}
func (o *ghaGuardOwnedCandidate) WithPinnedPayload(use func([]byte)error) error {if o==nil{return errGHAGuardUnqualified};return o.source.borrow(o,use)}
func (o *ghaGuardOwnedCandidate) Close() error {if o==nil||o.source==nil{return errGHAGuardUnqualified};return o.source.closeFor(o)}
func (o *ghaGuardOwnedCandidate) WaitClosed(ctx context.Context) error {if o==nil||o.source==nil{return errGHAGuardUnqualified};return o.source.waitFor(ctx,o)}

type ghaGuardPublicationRecord struct {
    Domain string `json:"domain"`
    RepositoryID int64 `json:"repository_id"`
    Source string `json:"source"`
    SourceTree string `json:"source_tree"`
    ProducerRevision string `json:"producer_revision"`
    RunID int64 `json:"run_id"`
    RunAttempt int `json:"run_attempt"`
    JobID int64 `json:"job_id"`
    CheckRunID int64 `json:"check_run_id"`
    ArtifactID int64 `json:"artifact_id"`
    ArtifactName string `json:"artifact_name"`
    ArchiveSHA string `json:"archive_sha256"`
    OutputSHA string `json:"output_sha256"`
    CaptureSHA string `json:"capture_sha256"`
    SourceManifestSHA string `json:"source_manifest_sha256"`
    AttestationID string `json:"attestation_id"`
}

// The record is parsed from one authenticated job output, not a caller grant.
// Even a valid parse cannot mint a candidate or replace attestation validation.
func ghaGuardParsePublication(raw []byte) (ghaGuardPublicationRecord,error) {
    var record ghaGuardPublicationRecord
    if len(raw)==0||len(raw)>8192{return record,errGHAGuardUnqualified}
    dec:=json.NewDecoder(bytes.NewReader(raw));dec.DisallowUnknownFields()
    if dec.Decode(&record)!=nil||dec.Decode(new(any))!=io.EOF{return ghaGuardPublicationRecord{},errGHAGuardUnqualified}
    canonical,err:=json.Marshal(record);if err!=nil||!bytes.Equal(canonical,raw){return ghaGuardPublicationRecord{},errGHAGuardUnqualified}
    if record.Domain!="guard-gha-publication-v1"||record.RepositoryID!=1406818713||record.Source!=ghaGuardSource||record.SourceTree!=ghaGuardSourceTree||!ghaGuardHex(record.ProducerRevision,40)||record.RunID<1||record.RunAttempt<1||record.JobID<1||record.CheckRunID<1||record.ArtifactID<1||record.ArtifactName!=fmt.Sprintf("guard-bootstrap-%d-%d-%d",record.RunID,record.RunAttempt,record.JobID)||!ghaGuardHex(record.ArchiveSHA,64)||!ghaGuardHex(record.OutputSHA,64)||!ghaGuardHex(record.CaptureSHA,64)||!ghaGuardHex(record.SourceManifestSHA,64)||len(record.AttestationID)<1||len(record.AttestationID)>128{return ghaGuardPublicationRecord{},errGHAGuardUnqualified}
    for _,r:=range record.AttestationID {if !((r>='a'&&r<='z')||(r>='A'&&r<='Z')||(r>='0'&&r<='9')||r=='_'||r=='-'){return ghaGuardPublicationRecord{},errGHAGuardUnqualified}}
    return record,nil
}

// Parse one bounded publication line from the independently retrieved log of
// the exact job; multiple lines and noncanonical records fail closed. The log
// source must still be authenticated by the caller.
func ghaGuardPublicationFromLog(raw []byte) (ghaGuardPublicationRecord,error) {
    if len(raw)==0||len(raw)>8<<20{return ghaGuardPublicationRecord{},errGHAGuardUnqualified}
    marker:=[]byte("guard_gha_publication_v1=");var record ghaGuardPublicationRecord;matches:=0
    for _,line:=range bytes.Split(raw,[]byte{'\n'}) {
        line=bytes.TrimSuffix(line,[]byte{'\r'})
        split:=bytes.IndexByte(line,' ');if split<=0 {continue}
        if _,err:=time.Parse(time.RFC3339Nano,string(line[:split]));err!=nil {continue}
        body:=line[split+1:]
        // Only an actual marker immediately after the timestamp is a record.
        // Source-command echoes and quoted or embedded markers are not bodies.
        if !bytes.HasPrefix(body,marker){continue}
        body=body[len(marker):]
        if bytes.Contains(body,marker){return ghaGuardPublicationRecord{},errGHAGuardUnqualified}
        parsed,err:=ghaGuardParsePublication(body);if err!=nil{return ghaGuardPublicationRecord{},err};record=parsed;matches++
    }
    if matches!=1{return ghaGuardPublicationRecord{},errGHAGuardUnqualified};return record,nil
}

func ghaGuardPublicationMatches(e ghaGuardMetadataExpectation,a ghaGuardArtifact,r ghaGuardPublicationRecord) bool {
    if r.RepositoryID!=e.RepositoryID||r.Source!=e.Source||r.SourceTree!=e.SourceTree||r.ProducerRevision!=e.ProducerRevision||r.RunID!=e.RunID||r.RunAttempt!=e.RunAttempt||r.JobID!=e.JobID||r.CheckRunID!=e.CheckRunID||r.ArtifactID!=e.ArtifactID||r.ArtifactName!=e.ArtifactName {return false}
    return a.ID==r.ArtifactID&&a.Digest=="sha256:"+r.ArchiveSHA
}
