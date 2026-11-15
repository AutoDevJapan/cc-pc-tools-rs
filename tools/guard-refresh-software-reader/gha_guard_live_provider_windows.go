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
    "net/url"
    "strings"
    "time"
)

const ghaGuardFixedAttestationID = "67491128"

// The redirect comes only from an authenticated request to the fixed GitHub
// endpoint. Credentials are never sent to the returned object-storage host.
func ghaGuardLiveRedirectURL(raw string) (*url.URL,error) {
    if len(raw)==0||len(raw)>8192 {return nil,errGHAGuardUnqualified}
    u,err:=url.Parse(raw)
    if err!=nil||u.Scheme!="https"||u.User!=nil||u.Fragment!=""||(u.Port()!=""&&u.Port()!="443") {return nil,errGHAGuardUnqualified}
    host:=strings.ToLower(u.Hostname())
    if !strings.HasSuffix(host,".blob.core.windows.net")&&!strings.HasSuffix(host,".actions.githubusercontent.com") {return nil,errGHAGuardUnqualified}
    return u,nil
}

// api is the same private authenticated authority used for metadata and Git
// source verification; object is a separate credential-free downloader.
func ghaGuardLiveRedirected(ctx context.Context,api,object *http.Client,endpoint string,limit int64) ([]byte,error) {
    if ctx==nil||ctx.Err()!=nil||api==nil||object==nil||limit<1||limit>ghaGuardArchiveLimit||!strings.HasPrefix(endpoint,"https://api.github.com/repos/AutoDevJapan/cc-pc-tools-rs/actions/") {return nil,errGHAGuardUnqualified}
    req,err:=http.NewRequestWithContext(ctx,http.MethodGet,endpoint,nil);if err!=nil{return nil,errGHAGuardUnqualified}
    req.Header.Set("Accept","application/vnd.github+json")
    req.Header.Set("X-GitHub-Api-Version","2022-11-28")
    once:=*api;once.CheckRedirect=func(*http.Request,[]*http.Request)error{return http.ErrUseLastResponse}
    response,err:=once.Do(req)
    if err!=nil||response==nil||response.Body==nil {return nil,errGHAGuardUnqualified}
    location:=response.Header.Get("Location");closeErr:=response.Body.Close()
    if response.StatusCode!=http.StatusFound||closeErr!=nil||ctx.Err()!=nil{return nil,errGHAGuardUnqualified}
    target,err:=ghaGuardLiveRedirectURL(location);if err!=nil{return nil,errGHAGuardUnqualified}
    download,err:=http.NewRequestWithContext(ctx,http.MethodGet,target.String(),nil);if err!=nil{return nil,errGHAGuardUnqualified}
    noRedirect:=*object;noRedirect.Jar=nil;noRedirect.CheckRedirect=func(*http.Request,[]*http.Request)error{return http.ErrUseLastResponse}
    response,err=noRedirect.Do(download)
    if err!=nil||response==nil||response.Body==nil {return nil,errGHAGuardUnqualified}
    if response.StatusCode!=http.StatusOK {_=response.Body.Close();return nil,errGHAGuardUnqualified}
    raw,readErr:=io.ReadAll(io.LimitReader(response.Body,limit+1));closeErr=response.Body.Close()
    if readErr!=nil||closeErr!=nil||int64(len(raw))>limit||ctx.Err()!=nil {clear(raw);return nil,errGHAGuardUnqualified}
    return raw,nil
}

// Capture metadata is read from the same bounded archive whose digest was
// bound to the server publication. It is not itself an update authorization.
func ghaGuardLiveCapture(archive []byte,pub ghaGuardPublicationRecord) (ghaGuardManifest,error) {
    var manifest ghaGuardManifest
    reader,err:=zip.NewReader(bytes.NewReader(archive),int64(len(archive)))
    if err!=nil||len(reader.File)!=3{return manifest,errGHAGuardUnqualified}
    var capture *zip.File;seen:=map[string]bool{}
    for _,f:=range reader.File {
        if f==nil||f.Flags&1!=0||!f.Mode().IsRegular()||seen[f.Name]||(f.Name!="capture.json"&&f.Name!="source-proof.json"&&f.Name!="guard.exe") {return manifest,errGHAGuardUnqualified}
        seen[f.Name]=true
        if f.Name=="capture.json" {capture=f}
    }
    if capture==nil||capture.UncompressedSize64>ghaGuardManifestLimit{return manifest,errGHAGuardUnqualified}
    entry,err:=capture.Open();if err!=nil{return manifest,errGHAGuardUnqualified}
    raw,readErr:=io.ReadAll(io.LimitReader(entry,ghaGuardManifestLimit+1));closeErr:=entry.Close()
    if readErr!=nil||closeErr!=nil||len(raw)>ghaGuardManifestLimit||uint64(len(raw))!=capture.UncompressedSize64||digest(raw)!=pub.CaptureSHA {clear(raw);return manifest,errGHAGuardUnqualified}
    defer clear(raw)
    decoder:=json.NewDecoder(bytes.NewReader(raw));decoder.DisallowUnknownFields()
    if decoder.Decode(&manifest)!=nil||decoder.Decode(new(any))!=io.EOF{return ghaGuardManifest{},errGHAGuardUnqualified}
    canonical,err:=json.Marshal(manifest)
    if err!=nil||!bytes.Equal(canonical,raw)||manifest.RepositoryID!=pub.RepositoryID||manifest.Source!=pub.Source||manifest.SourceTree!=pub.SourceTree||manifest.ProducerRevision!=pub.ProducerRevision||manifest.RunID!=pub.RunID||manifest.RunAttempt!=pub.RunAttempt||manifest.JobID!=pub.JobID||manifest.CheckRunID!=pub.CheckRunID||manifest.SourceManifestSHA!=pub.SourceManifestSHA||manifest.OutputSHA!=pub.OutputSHA {return ghaGuardManifest{},errGHAGuardUnqualified}
    return manifest,nil
}

// Bundle bytes are evidence, not a grant. This routine never issues a
// candidate. The caller must be the code-owned transport and retain the
// authenticated bundle; all partial buffers are erased on failure.
func ghaGuardLiveAcquireWith(ctx context.Context,authority *ghaGuardEvidenceAuthority,object *http.Client,e ghaGuardMetadataExpectation,bundle []byte,attestationID string) (*ghaGuardAcquiredEvidence,error) {
    if ctx==nil||ctx.Err()!=nil||authority==nil||authority.client==nil||object==nil||len(bundle)==0||len(bundle)>4<<20||attestationID=="" {return nil,errGHAGuardUnqualified}
    client:=authority.client
    bounded,cancel:=context.WithTimeout(ctx,2*time.Minute);defer cancel();ctx=bounded
    var workflow struct {ID int64;Path,State string}
    if ghaGuardServerJSON(ctx,client,"https://api.github.com/repos/AutoDevJapan/cc-pc-tools-rs/actions/workflows/unattended-readiness.yml",&workflow)!=nil||workflow.ID<=0||workflow.Path!=e.WorkflowPath||workflow.State!="active"||(e.WorkflowID!=0&&e.WorkflowID!=workflow.ID){return nil,errGHAGuardUnqualified}
    e.WorkflowID=workflow.ID
    jobURL:=fmt.Sprintf("https://api.github.com/repos/AutoDevJapan/cc-pc-tools-rs/actions/jobs/%d/logs",e.JobID)
    log,err:=ghaGuardLiveRedirected(ctx,client,object,jobURL,8<<20);if err!=nil{return nil,errGHAGuardUnqualified}
    success:=false
    defer func(){if !success{clear(log)}}()
    pub,err:=ghaGuardPublicationFromLog(log)
    if err!=nil||pub.AttestationID!=attestationID{return nil,errGHAGuardUnqualified}
    binding:=ghaGuardCaptureBinding{RepositoryID:pub.RepositoryID,Source:pub.Source,ProducerRevision:pub.ProducerRevision,RunID:pub.RunID,RunAttempt:pub.RunAttempt,JobID:pub.JobID,CheckRunID:pub.CheckRunID,ArtifactID:pub.ArtifactID}
    evidence,err:=ghaGuardFetchServerEvidence(ctx,client,e,binding,time.Now().UTC())
    if err!=nil||!ghaGuardPublicationMatches(e,evidence.artifact,pub){return nil,errGHAGuardUnqualified}
    archiveURL:=fmt.Sprintf("https://api.github.com/repos/AutoDevJapan/cc-pc-tools-rs/actions/artifacts/%d/zip",e.ArtifactID)
    archive,err:=ghaGuardLiveRedirected(ctx,client,object,archiveURL,evidence.artifact.Size)
    if err!=nil{return nil,errGHAGuardUnqualified}
    defer func(){if !success{clear(archive)}}()
    if int64(len(archive))!=evidence.artifact.Size||digest(archive)!=pub.ArchiveSHA||ctx.Err()!=nil{return nil,errGHAGuardUnqualified}
    manifest,err:=ghaGuardLiveCapture(archive,pub);if err!=nil{return nil,errGHAGuardUnqualified}
    payload,err:=ghaGuardReadArchive(ctx,archive,ghaGuardArchiveExpectation{Manifest:manifest,ArchiveSHA:pub.ArchiveSHA,ArchiveBytes:evidence.artifact.Size})
    if err!=nil{return nil,errGHAGuardUnqualified};clear(payload)
    ownedBundle:=append([]byte(nil),bundle...)
    if ctx.Err()!=nil||authority.client!=client {clear(ownedBundle);return nil,errGHAGuardUnqualified}
    material:=&ghaGuardAcquiredEvidence{run:evidence.run,job:evidence.job,artifact:evidence.artifact,jobLog:log,workflowID:workflow.ID,workflowPath:workflow.Path,workflowActive:true,manifest:manifest,archive:archive,bundle:ownedBundle}
    success=true
    return material,nil
}

// Normal acquisition uses one fixed generation and a fresh credential-free
// object downloader. The bundle still must come from the normal host owner.
func ghaGuardLiveAcquire(ctx context.Context,authority *ghaGuardEvidenceAuthority,bundle []byte) (*ghaGuardAcquiredEvidence,error) {
    transport:=&http.Transport{Proxy:nil}
    defer transport.CloseIdleConnections()
    object:=&http.Client{Transport:transport,Timeout:30*time.Second}
    return ghaGuardLiveAcquireWith(ctx,authority,object,ghaGuardActualExpectation(),bundle,ghaGuardFixedAttestationID)
}

// This fixed production bridge cannot manufacture the authority or bundle.
// Only the actual Sigstore check and the same authority client can issue the
// private candidate; neither test hooks nor diagnostics are wired here.
func ghaGuardLiveOpen(ctx context.Context,bundle []byte) (result *ghaGuardVerifiedCandidate,resultErr error) {
    if ctx==nil||ctx.Err()!=nil{return nil,errGHAGuardUnqualified}
    authority,authErr:=ghaGuardNormalEvidenceAuthority(ctx)
    if authErr!=nil||authority==nil||authority.client==nil{return nil,errors.Join(errGHAGuardUnqualified,authErr)}
    defer func(){
        if closeErr:=ghaGuardCloseCredentialAuthority(authority,nil);closeErr!=nil {
            if result!=nil {
                cleanup,cancel:=context.WithTimeout(context.Background(),5*time.Second)
                resultErr=errors.Join(resultErr,result.Close(),result.WaitClosed(cleanup));cancel()
            }
            result=nil;resultErr=errors.Join(errGHAGuardUnqualified,resultErr,closeErr)
        }
    }()
    if bundle==nil {
        var err error
        bundle,err=ghaGuardFetchPinnedPublicBundle(ctx)
        if err!=nil{return nil,errGHAGuardUnqualified}
        defer clear(bundle)
    }
    client:=authority.client
    material,err:=ghaGuardLiveAcquire(ctx,authority,bundle)
    if err!=nil{return nil,errGHAGuardUnqualified}
    if authority.client!=client {clear(material.archive);clear(material.bundle);clear(material.jobLog);return nil,errGHAGuardUnqualified}
    candidate,err:=ghaGuardConstructVerifiedCandidateWith(ctx,ghaGuardActualExpectation(),material,ghaGuardVerifyActualSignature,func(ctx context.Context,archive []byte,m ghaGuardManifest)error{
        if authority.client!=client{return errGHAGuardUnqualified}
        err:=ghaGuardVerifyArchiveGitSourceViaPublicArchiveWithClient(ctx,archive,m,client)
        if err!=nil||authority.client!=client{return errGHAGuardUnqualified}
        return nil
    })
    if err!=nil||candidate==nil {
        if candidate!=nil {return nil,errors.Join(errGHAGuardUnqualified,err,candidate.Close())}
        return nil,errGHAGuardUnqualified
    }
    if ctx.Err()!=nil||authority.client!=client {
        if closeErr:=candidate.Close();closeErr!=nil{return nil,errors.Join(errGHAGuardUnqualified,closeErr)}
        return nil,errGHAGuardUnqualified
    }
    return candidate,nil
}
