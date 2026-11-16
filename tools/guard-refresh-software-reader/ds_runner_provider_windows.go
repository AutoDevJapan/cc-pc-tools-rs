//go:build windows

package main

import (
    "bytes"
    "context"
    "crypto/tls"
    "encoding/base64"
    "encoding/hex"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "net/http"
    "reflect"
    "unicode"
    "strings"
    "sync"
    "time"

    "github.com/sigstore/sigstore-go/pkg/bundle"
    "github.com/sigstore/sigstore-go/pkg/fulcio/certificate"
    "github.com/sigstore/sigstore-go/pkg/root"
    "github.com/sigstore/sigstore-go/pkg/tuf"
    "github.com/sigstore/sigstore-go/pkg/verify"
)

// Exact original producer files must remain unchanged at the admitted run
// revision; being a descendant of a reviewed commit alone is insufficient.
type dsRunnerProducerFile struct{Path,Blob string}
var dsRunnerProducerFiles = []dsRunnerProducerFile{
    {".github/workflows/task-runner-capture.yml","dbdb8e6b569ac8503801103e10d115523577aa8e"},
    {"tools/task-runner-capture/README.md","2b684c79ddfb6ab019399a7673c55965ffa9fe31"},
    {"tools/task-runner-capture/go.mod","d1acf495f8a291b2d8f84708e17082ace7c14671"},
    {"tools/task-runner-capture/main_windows.go","aac49589adaefd3a8f8269950e341b41ff67d9fa"},
    {"tools/task-runner-capture/main_windows_test.go","d502c679cb98644e0ee1e02a12403de2858ff294"},
}
const dsRunnerAPI = "https://api.github.com/repos/AutoDevJapan/cc-pc-tools-rs"
func dsRunnerAllowed(req *http.Request,p dsRunnerCapturePin)bool{
    if req==nil||req.URL==nil||req.Method!=http.MethodGet||req.Body!=nil&&req.Body!=http.NoBody||!dsRunnerPinValid(p){return false}
    u:=req.URL
    if u.Scheme!="https"||u.Host!="api.github.com"||u.User!=nil||u.Opaque!=""||u.Fragment!=""||u.RawPath!=""||u.ForceQuery||req.Host!=""&&req.Host!="api.github.com"{return false}
    for name,values:=range req.Header{
        if len(values)!=1{return false}
        switch strings.ToLower(name){case "accept":if values[0]!="application/vnd.github+json"{return false};case "x-github-api-version":if values[0]!="2022-11-28"{return false};default:return false}
    }
    if u.Path=="/user"{return u.RawQuery==""}
    prefix:="/repos/AutoDevJapan/cc-pc-tools-rs"
    if !strings.HasPrefix(u.Path,prefix){return false}
    path:=strings.TrimPrefix(u.Path,prefix)
    singles:=[]string{"","/actions/workflows/task-runner-capture.yml",
        fmt.Sprintf("/actions/runs/%d",p.RunID),fmt.Sprintf("/actions/jobs/%d/logs",p.JobID),
        fmt.Sprintf("/actions/artifacts/%d/zip",p.ArtifactID),
        "/compare/"+dsRunnerApprovedProducer+"..."+p.Producer,
        "/git/commits/"+dsRunnerSource,"/git/trees/"+dsRunnerModuleTree}
    for _,v:=range singles{if path==v{return u.RawQuery==""}}
    for _,v:=range []string{fmt.Sprintf("/actions/runs/%d/attempts/1/jobs",p.RunID),fmt.Sprintf("/actions/runs/%d/artifacts",p.RunID),"/attestations/sha256:"+p.OutputSHA}{if path==v{return u.RawQuery=="per_page=100"}}
    for _,file:=range dsRunnerProducerFiles{if path=="/contents/"+file.Path{return u.RawQuery=="ref="+p.Producer}}
    return false
}
type dsRunnerCredential struct{mu sync.Mutex;secret []byte;base http.RoundTripper;pin dsRunnerCapturePin;root context.Context;cancel context.CancelFunc;ready,closed bool;active int;done chan struct{}}
func(s *dsRunnerCredential)finish(){s.mu.Lock();s.active--;if s.closed&&s.active==0&&s.done!=nil{close(s.done);s.done=nil};s.mu.Unlock()}
func(s *dsRunnerCredential)RoundTrip(req *http.Request)(*http.Response,error){
    if s==nil||!dsRunnerAllowed(req,s.pin)||req.Context().Err()!=nil{return nil,errDSRunnerEvidence}
    s.mu.Lock();if s.closed||s.base==nil||s.root==nil||s.cancel==nil||len(s.secret)==0||!s.ready&&req.URL.Path!="/user"{s.mu.Unlock();return nil,errDSRunnerEvidence}
    s.active++;header:="Bearer "+string(s.secret);ownedRoot:=s.root;base:=s.base;s.mu.Unlock()
    clean,releaseCtx:=ghaGuardCredentialContext(req.Context(),ownedRoot)
    release:=func(){releaseCtx();s.finish()}
    if clean.Err()!=nil{release();return nil,errors.Join(errDSRunnerEvidence,clean.Err())}
    clone:=req.Clone(clean);clone.Header=req.Header.Clone();clone.Header.Set("Authorization",header);header=""
    owned:=true;defer func(){if owned{release()}}()
    response,err:=base.RoundTrip(clone)
    if err!=nil||response==nil||response.Body==nil{
        var closeErr error;if response!=nil&&response.Body!=nil{closeErr=response.Body.Close()}
        return nil,dsRunnerResponseError(errDSRunnerEvidence,closeErr,clean.Err())
    }
    response.Request=req;response.Body=&ghaGuardCredentialBody{body:response.Body,release:release};owned=false
    return response,nil
}
func(s *dsRunnerCredential)Close(ctx context.Context)error{
    if s==nil||ctx==nil{return errDSRunnerCleanup}
    s.mu.Lock();if !s.closed{s.closed=true;clear(s.secret);s.secret=nil;s.cancel();if s.active==0&&s.done!=nil{close(s.done);s.done=nil}}
    done:=s.done;s.mu.Unlock();if closer,ok:=s.base.(interface{CloseIdleConnections()});ok{closer.CloseIdleConnections()}
    if done==nil{return nil};select{case<-done:return nil;case<-ctx.Done():return errors.Join(errDSRunnerCleanup,ctx.Err())}
}
func dsRunnerResponseError(primary,closeErr,contextErr error)error{
    if closeErr!=nil{return errors.Join(primary,errDSRunnerCleanup,contextErr)}
    return errors.Join(primary,contextErr)
}
// A failed response-body close is kept as one fixed cleanup fact next to the
// primary evidence failure. Raw transport errors never leave this layer.
func dsRunnerCleanupFact(err error)bool{return errors.Is(err,errDSRunnerCleanup)||errors.Is(err,errGHAGuardResponseCleanup)}
func dsRunnerEvidenceFailure(err error)error{
    if dsRunnerCleanupFact(err){return errors.Join(errDSRunnerEvidence,errDSRunnerCleanup)}
    return errDSRunnerEvidence
}
func dsRunnerFailureOr(failure error)error{
    if failure!=nil{return failure}
    return errDSRunnerEvidence
}
func dsRunnerServerFetch(ctx context.Context,client *http.Client,endpoint string,out any,failure *error)bool{
    err:=dsRunnerServerJSON(ctx,client,endpoint,out);if err==nil{return true}
    *failure=dsRunnerEvidenceFailure(err);return false
}
type dsRunnerAuthority struct{session *dsRunnerCredential;client *http.Client;pin dsRunnerCapturePin}
func openDSRunnerAuthority(ctx context.Context,p dsRunnerCapturePin)(result *dsRunnerAuthority,err error){
    if ctx==nil||ctx.Err()!=nil||!dsRunnerPinValid(p){return nil,errDSRunnerEvidence}
    secret,err:=ghaGuardReadFixedWinCredential();if err!=nil{return nil,errors.Join(errDSRunnerEvidence,err)}
    owned,cancel:=context.WithCancel(context.Background())
    transport:=&http.Transport{Proxy:nil,TLSHandshakeTimeout:10*time.Second,ResponseHeaderTimeout:15*time.Second,MaxResponseHeaderBytes:64<<10,TLSNextProto:make(map[string]func(string,*tls.Conn)http.RoundTripper)}
    session:=&dsRunnerCredential{secret:secret,base:transport,pin:p,root:owned,cancel:cancel,done:make(chan struct{})}
    client:=&http.Client{Transport:session,Timeout:30*time.Second,CheckRedirect:func(*http.Request,[]*http.Request)error{return http.ErrUseLastResponse}}
    issued:=false;defer func(){if !issued{cleanup,c:=context.WithTimeout(context.Background(),5*time.Second);defer c();err=errors.Join(err,session.Close(cleanup))}}()
    raw,err:=dsRunnerHTTPBytes(ctx,client,"https://api.github.com/user",64<<10)
    if err!=nil{return nil,err};valid:=ghaGuardCredentialOwnerJSON(raw);clear(raw)
    if !valid||ctx.Err()!=nil{return nil,errDSRunnerEvidence}
    session.mu.Lock();session.ready=true;session.mu.Unlock()
    result=&dsRunnerAuthority{session:session,client:client,pin:p};issued=true;return result,nil
}
func dsRunnerHTTPBytes(ctx context.Context,client *http.Client,endpoint string,limit int64)([]byte,error){
    if ctx==nil||ctx.Err()!=nil||client==nil||limit<1||limit>64<<20{return nil,errDSRunnerEvidence}
    request,err:=http.NewRequestWithContext(ctx,http.MethodGet,endpoint,nil);if err!=nil{return nil,errDSRunnerEvidence}
    request.Header.Set("Accept","application/vnd.github+json");request.Header.Set("X-GitHub-Api-Version","2022-11-28")
    isolated:=*client;isolated.CheckRedirect=func(*http.Request,[]*http.Request)error{return http.ErrUseLastResponse}
    response,err:=isolated.Do(request)
    if err!=nil||response==nil||response.Body==nil{var closeErr error;if response!=nil&&response.Body!=nil{closeErr=response.Body.Close()};return nil,dsRunnerResponseError(errDSRunnerEvidence,closeErr,ctx.Err())}
    if response.StatusCode!=http.StatusOK||!ghaGuardSinglePageLink(response.Header){return nil,dsRunnerResponseError(errDSRunnerEvidence,response.Body.Close(),ctx.Err())}
    raw,readErr:=io.ReadAll(io.LimitReader(response.Body,limit+1));closeErr:=response.Body.Close()
    if readErr!=nil||closeErr!=nil||int64(len(raw))>limit||ctx.Err()!=nil{clear(raw);return nil,dsRunnerResponseError(errDSRunnerEvidence,closeErr,ctx.Err())};return raw,nil
}
func dsRunnerServerJSON(ctx context.Context,client *http.Client,endpoint string,out any)error{
    raw,err:=dsRunnerHTTPBytes(ctx,client,endpoint,2<<20);if err!=nil{return err};defer clear(raw)
    return dsRunnerDecodeServerJSON(raw,out)
}
// Server documents allow extra API fields, but never duplicate/case-folded keys
// in any nested object. Producer-owned JSON uses stricter canonical decoding.
func dsRunnerUniqueKeys(raw []byte)error{
    if len(raw)==0||len(raw)>8<<20{return errDSRunnerEvidence}
    d:=json.NewDecoder(bytes.NewReader(raw));d.UseNumber();tokens:=0
    var walk func(int)error
    walk=func(depth int)error{
        tokens++;if tokens>200000||depth>64{return errDSRunnerEvidence};token,err:=d.Token();if err!=nil{return errDSRunnerEvidence}
        delim,ok:=token.(json.Delim);if !ok{return nil}
        if delim=='{'{seen:=map[string]bool{};for d.More(){key,e:=d.Token();name,ok:=key.(string);if e!=nil||!ok||seen[dsRunnerFoldJSONKey(name)]{return errDSRunnerEvidence};seen[dsRunnerFoldJSONKey(name)]=true;if walk(depth+1)!=nil{return errDSRunnerEvidence}};end,e:=d.Token();if e!=nil||end!=json.Delim('}'){return errDSRunnerEvidence};return nil}
        if delim=='['{for d.More(){if walk(depth+1)!=nil{return errDSRunnerEvidence}};end,e:=d.Token();if e!=nil||end!=json.Delim(']'){return errDSRunnerEvidence};return nil};return errDSRunnerEvidence
    }
    if walk(0)!=nil||d.Decode(new(any))!=io.EOF{return errDSRunnerEvidence};return nil
}
func dsRunnerVerifyProducerSource(ctx context.Context,client *http.Client,p dsRunnerCapturePin)error{
    var failure error
    var comparison struct{Status string;Base struct{SHA string} `json:"base_commit"`;MergeBase struct{SHA string} `json:"merge_base_commit"`}
    if !dsRunnerServerFetch(ctx,client,dsRunnerAPI+"/compare/"+dsRunnerApprovedProducer+"..."+p.Producer,&comparison,&failure)||(comparison.Status!="ahead"&&comparison.Status!="identical")||comparison.Base.SHA!=dsRunnerApprovedProducer||comparison.MergeBase.SHA!=dsRunnerApprovedProducer{return dsRunnerFailureOr(failure)}
    for _,file:=range dsRunnerProducerFiles{
        var content struct{Type,SHA,Encoding,Content string;Size int64}
        if !dsRunnerServerFetch(ctx,client,dsRunnerAPI+"/contents/"+file.Path+"?ref="+p.Producer,&content,&failure)||content.Type!="file"||content.SHA!=file.Blob||content.Encoding!="base64"||content.Size<1||content.Size>64<<10{return dsRunnerFailureOr(failure)}
        raw,err:=base64.StdEncoding.DecodeString(strings.ReplaceAll(content.Content,"\n",""));if err!=nil{return errDSRunnerEvidence}
        valid:=int64(len(raw))==content.Size&&dsRunnerGitBlob(raw)==file.Blob;clear(raw);if !valid{return errDSRunnerEvidence}
    }
    var commit struct{SHA string;Tree struct{SHA string}}
    if !dsRunnerServerFetch(ctx,client,dsRunnerAPI+"/git/commits/"+dsRunnerSource,&commit,&failure)||commit.SHA!=dsRunnerSource||commit.Tree.SHA!=dsRunnerSourceTree{return dsRunnerFailureOr(failure)}
    var tree struct{SHA string;Truncated bool;Tree []struct{Path,Mode,Type,SHA string;Size int64}}
    if !dsRunnerServerFetch(ctx,client,dsRunnerAPI+"/git/trees/"+dsRunnerModuleTree,&tree,&failure)||tree.SHA!=dsRunnerModuleTree||tree.Truncated||len(tree.Tree)!=len(dsRunnerInputPins){return dsRunnerFailureOr(failure)}
    expected:=map[string]dsRunnerInputPin{};for _,pin:=range dsRunnerInputPins{expected[strings.TrimPrefix(pin.Path,"unattended-runner/")]=pin}
    seen:=map[string]bool{};for _,file:=range tree.Tree{pin,ok:=expected[file.Path];if !ok||seen[file.Path]||file.Type!="blob"||file.Mode!="100644"||file.SHA!=pin.Blob||file.Size!=pin.Size{return errDSRunnerEvidence};seen[file.Path]=true}
    return nil
}
type dsRunnerPublication struct {
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
    DataAccessGranted bool `json:"data_access_granted"`
    UpdatePerformed bool `json:"update_performed"`
    SendAllowed bool `json:"send_allowed"`
    SourceQuiescenceProved bool `json:"source_quiescence_proved"`
}
func dsRunnerPublicationFromLog(raw []byte,p dsRunnerCapturePin)error{
    if len(raw)==0||len(raw)>8<<20{return errDSRunnerEvidence};found:=0
    marker:=[]byte("task_runner_gha_publication_v1=")
    for _,line:=range bytes.Split(raw,[]byte{'\n'}){
        line=bytes.TrimSuffix(line,[]byte{'\r'});split:=bytes.IndexByte(line,' ');if split<1{continue}
        if _,err:=time.Parse(time.RFC3339Nano,string(line[:split]));err!=nil{continue}
        body:=line[split+1:];if !bytes.HasPrefix(body,marker){continue};body=body[len(marker):]
        var pub dsRunnerPublication
        if dsRunnerCanonical(body,&pub,8192)!=nil||pub.Domain!="task-runner-gha-publication-v1"||pub.RepositoryID!=1406818713||pub.Source!=dsRunnerSource||pub.SourceTree!=dsRunnerSourceTree||pub.ProducerRevision!=p.Producer||pub.RunID!=p.RunID||pub.RunAttempt!=1||pub.JobID!=p.JobID||pub.CheckRunID!=p.CheckRunID||pub.ArtifactID!=p.ArtifactID||pub.ArtifactName!=dsRunnerArtifactName(p)||pub.ArchiveSHA!=p.ArchiveSHA||pub.OutputSHA!=p.OutputSHA||pub.CaptureSHA!=p.CaptureSHA||pub.SourceManifestSHA!=p.ProofSHA||pub.AttestationID!=p.AttestationID||pub.DataAccessGranted||pub.UpdatePerformed||pub.SendAllowed||pub.SourceQuiescenceProved{return errDSRunnerEvidence};found++
    }
    if found!=1{return errDSRunnerEvidence};return nil
}
func dsRunnerServerMatches(ctx context.Context,client *http.Client,p dsRunnerCapturePin,now time.Time)(int64,error){
    var failure error
    if !dsRunnerPinValid(p)||ctx==nil||ctx.Err()!=nil||now.IsZero(){return 0,errDSRunnerEvidence}
    var repo struct{ID int64;FullName string `json:"full_name"`;Private,Archived bool}
    if !dsRunnerServerFetch(ctx,client,dsRunnerAPI,&repo,&failure)||repo.ID!=1406818713||repo.FullName!="AutoDevJapan/cc-pc-tools-rs"||repo.Private||repo.Archived{return 0,dsRunnerFailureOr(failure)}
    var wf struct{ID int64;Path,State string}
    if !dsRunnerServerFetch(ctx,client,dsRunnerAPI+"/actions/workflows/task-runner-capture.yml",&wf,&failure)||wf.ID!=p.WorkflowID||wf.Path!=dsRunnerWorkflow||wf.State!="active"{return 0,dsRunnerFailureOr(failure)}
    // Head repository and actor keys are explicit; API snake_case is not inferred.
    var head struct{ID int64;Attempt int `json:"run_attempt"`;Event,Path,Status,Conclusion string;WorkflowID int64 `json:"workflow_id"`;HeadSHA string `json:"head_sha"`;HeadBranch string `json:"head_branch"`;Repository struct{ID int64} `json:"repository"`;HeadRepository struct{ID int64} `json:"head_repository"`;Actor struct{ID int64;Login string} `json:"actor"`;TriggeringActor struct{ID int64;Login string} `json:"triggering_actor"`}
    if !dsRunnerServerFetch(ctx,client,fmt.Sprintf("%s/actions/runs/%d",dsRunnerAPI,p.RunID),&head,&failure)||head.ID!=p.RunID||head.Attempt!=1||head.Event!="workflow_dispatch"||head.Path!=dsRunnerWorkflow||head.WorkflowID!=p.WorkflowID||head.HeadSHA!=p.Producer||head.HeadBranch!="main"||head.Status!="completed"||head.Conclusion!="success"||head.Repository.ID!=1406818713||head.HeadRepository.ID!=1406818713||head.Actor.ID!=88025750||!strings.EqualFold(head.Actor.Login,"mashfromband")||head.TriggeringActor.ID!=88025750||!strings.EqualFold(head.TriggeringActor.Login,"mashfromband"){return 0,dsRunnerFailureOr(failure)}
    var jobs struct{Total int `json:"total_count"`;Jobs []struct{ID int64;RunID int64 `json:"run_id"`;HeadSHA string `json:"head_sha"`;CheckRunURL string `json:"check_run_url"`;Name,Status,Conclusion string;Steps []struct{Name,Status,Conclusion string;Number int}}}
    if !dsRunnerServerFetch(ctx,client,fmt.Sprintf("%s/actions/runs/%d/attempts/1/jobs?per_page=100",dsRunnerAPI,p.RunID),&jobs,&failure)||jobs.Total!=len(jobs.Jobs)||jobs.Total>100{return 0,dsRunnerFailureOr(failure)}
    matches:=0
    order:=[]string{"Capture the fixed runner build","Check the exact captured files","Attest the runner and source evidence","Upload the fixed runner capture","Record the server-assigned capture artifact"}
    jobIDs:=map[int64]bool{}
    for _,job:=range jobs.Jobs{if job.ID<1||jobIDs[job.ID]{return 0,errDSRunnerEvidence};jobIDs[job.ID]=true;if job.Name!="Task runner software artifact"{continue};matches++;if job.ID!=p.JobID||job.RunID!=p.RunID||job.HeadSHA!=p.Producer||job.CheckRunURL!=fmt.Sprintf("%s/check-runs/%d",dsRunnerAPI,p.CheckRunID)||job.Status!="completed"||job.Conclusion!="success"{return 0,errDSRunnerEvidence}
        next,last:=0,0;for _,step:=range job.Steps{needed:=false;for _,name:=range order{if name==step.Name{needed=true}};if !needed{continue};if next>=len(order)||step.Name!=order[next]||step.Number<=last||step.Status!="completed"||step.Conclusion!="success"{return 0,errDSRunnerEvidence};last=step.Number;next++};if next!=len(order){return 0,errDSRunnerEvidence}}
    if matches!=1{return 0,errDSRunnerEvidence}
    var artifacts struct{Total int `json:"total_count"`;Artifacts []struct{ID int64;Name,Digest string;Expired bool;Size int64 `json:"size_in_bytes"`;Expires time.Time `json:"expires_at"`;WorkflowRun struct{ID int64;RepositoryID int64 `json:"repository_id"`;HeadRepositoryID int64 `json:"head_repository_id"`;HeadSHA string `json:"head_sha"`} `json:"workflow_run"`}}
    if !dsRunnerServerFetch(ctx,client,fmt.Sprintf("%s/actions/runs/%d/artifacts?per_page=100",dsRunnerAPI,p.RunID),&artifacts,&failure)||artifacts.Total!=len(artifacts.Artifacts)||artifacts.Total>100{return 0,dsRunnerFailureOr(failure)}
    matches=0;var size int64
    artifactIDs:=map[int64]bool{}
    for _,a:=range artifacts.Artifacts{if a.ID<1||artifactIDs[a.ID]{return 0,errDSRunnerEvidence};artifactIDs[a.ID]=true;if a.Name!=dsRunnerArtifactName(p){continue};matches++;if a.ID!=p.ArtifactID||a.Digest!="sha256:"+p.ArchiveSHA||a.Expired||!a.Expires.After(now)||a.Size<1||a.Size>64<<20||a.WorkflowRun.ID!=p.RunID||a.WorkflowRun.RepositoryID!=1406818713||a.WorkflowRun.HeadRepositoryID!=1406818713||a.WorkflowRun.HeadSHA!=p.Producer{return 0,errDSRunnerEvidence};size=a.Size}
    if matches!=1{return 0,errDSRunnerEvidence};return size,nil
}
func dsRunnerVerifySignedSubjects(ctx context.Context,trusted root.TrustedMaterial,raw []byte,p dsRunnerCapturePin)error{
    if ctx==nil||ctx.Err()!=nil||trusted==nil||!dsRunnerPinValid(p)||len(raw)==0||len(raw)>4<<20||digest(raw)!=p.BundleSHA{return errDSRunnerEvidence}
    var signed bundle.Bundle
    if signed.UnmarshalJSON(raw)!=nil||signed.Bundle==nil||signed.Bundle.GetDsseEnvelope()==nil{return errDSRunnerEvidence}
    signer:="https://github.com/AutoDevJapan/cc-pc-tools-rs/"+dsRunnerWorkflow+"@refs/heads/main"
    san,err:=verify.NewSANMatcher(signer,"");if err!=nil{return errDSRunnerEvidence};issuer,err:=verify.NewIssuerMatcher("https://token.actions.githubusercontent.com","");if err!=nil{return errDSRunnerEvidence}
    ext:=certificate.Extensions{BuildSignerURI:signer,BuildSignerDigest:p.Producer,BuildConfigURI:signer,BuildConfigDigest:p.Producer,RunnerEnvironment:"github-hosted",SourceRepositoryURI:"https://github.com/AutoDevJapan/cc-pc-tools-rs",SourceRepositoryIdentifier:"1406818713",SourceRepositoryDigest:p.Producer,SourceRepositoryRef:"refs/heads/main",RunInvocationURI:fmt.Sprintf("https://github.com/AutoDevJapan/cc-pc-tools-rs/actions/runs/%d/attempts/1",p.RunID),BuildTrigger:"workflow_dispatch",SourceRepositoryVisibilityAtSigning:"public"}
    identity,err:=verify.NewCertificateIdentity(san,issuer,ext);if err!=nil{return errDSRunnerEvidence}
    verifier,err:=verify.NewVerifier(trusted,verify.WithSignedCertificateTimestamps(1),verify.WithTransparencyLog(1),verify.WithObserverTimestamps(1));if err!=nil{return errDSRunnerEvidence}
    expected:=map[string]string{"unattended-runner.exe":p.OutputSHA,"source-proof.json":p.ProofSHA,"capture.json":p.CaptureSHA}
    for _,sha:=range []string{p.OutputSHA,p.ProofSHA,p.CaptureSHA}{value,e:=hex.DecodeString(sha);if e!=nil{return errDSRunnerEvidence};result,e:=verifier.Verify(&signed,verify.NewPolicy(verify.WithArtifactDigest("sha256",value),verify.WithCertificateIdentity(identity)));if e!=nil{return errors.Join(errDSRunnerEvidence,e)};if result==nil||result.Statement==nil||result.Signature==nil||result.Signature.Certificate==nil||result.Statement.GetType()!="https://in-toto.io/Statement/v1"||result.Statement.GetPredicateType()!="https://slsa.dev/provenance/v1"{return errDSRunnerEvidence};subjects:=result.Statement.GetSubject();if len(subjects)!=3{return errDSRunnerEvidence};seen:=map[string]bool{};for _,subject:=range subjects{if subject==nil||seen[subject.GetName()]||expected[subject.GetName()]==""||len(subject.GetDigest())!=1||subject.GetDigest()["sha256"]!=expected[subject.GetName()]{return errDSRunnerEvidence};seen[subject.GetName()]=true}}
    if ctx.Err()!=nil{return errors.Join(errDSRunnerEvidence,ctx.Err())};return nil
}
func openNormalDSRunnerCandidate(ctx context.Context)(candidate *dsRunnerCandidate,resultErr error){
    if ctx==nil||ctx.Err()!=nil{return nil,errDSRunnerEvidence}
    p:=dsRunnerAdmittedCapture
    if !dsRunnerPinValid(p){return nil,errors.New("ds_runner_capture_not_bound")}
    bounded,cancel:=context.WithTimeout(ctx,2*time.Minute);defer cancel();ctx=bounded
    authority,err:=openDSRunnerAuthority(ctx,p);if err!=nil{return nil,err}
    defer func(){cleanup,c:=context.WithTimeout(context.Background(),5*time.Second);defer c();if e:=authority.session.Close(cleanup);e!=nil{if candidate!=nil{_=candidate.Close();candidate=nil};resultErr=errors.Join(resultErr,errDSRunnerCleanup,e)}}()
    client:=authority.client
    if err=dsRunnerVerifyProducerSource(ctx,client,p);err!=nil{return nil,err}
    size,err:=dsRunnerServerMatches(ctx,client,p,time.Now().UTC());if err!=nil{return nil,err}
    objectTransport:=&http.Transport{Proxy:nil,TLSHandshakeTimeout:10*time.Second,ResponseHeaderTimeout:15*time.Second};defer objectTransport.CloseIdleConnections()
    object:=&http.Client{Transport:objectTransport,Timeout:30*time.Second}
    log,err:=ghaGuardLiveRedirectedDetailed(ctx,client,object,fmt.Sprintf("%s/actions/jobs/%d/logs",dsRunnerAPI,p.JobID),8<<20);if err!=nil{return nil,err};defer clear(log)
    if dsRunnerPublicationFromLog(log,p)!=nil{return nil,errDSRunnerEvidence}
    archive,err:=ghaGuardLiveRedirectedDetailed(ctx,client,object,fmt.Sprintf("%s/actions/artifacts/%d/zip",dsRunnerAPI,p.ArtifactID),size);if err!=nil{return nil,err};defer clear(archive)
    if int64(len(archive))!=size{return nil,errDSRunnerEvidence}
    held,err:=dsRunnerReadArchive(ctx,archive,p);if err!=nil{return nil,err};defer held.erase()
    attestation,err:=dsRunnerHTTPBytes(ctx,client,dsRunnerAPI+"/attestations/sha256:"+p.OutputSHA+"?per_page=100",8<<20);if err!=nil{return nil,err};defer clear(attestation)
    rawBundle,err:=ghaFreshBundleFromJSON(attestation,ghaGuardPublicationRecord{OutputSHA:p.OutputSHA,AttestationID:p.AttestationID});if err!=nil{return nil,errDSRunnerEvidence};defer clear(rawBundle)
    opts:=tuf.DefaultOptions().WithDisableLocalCache().WithContext(ctx).WithFetcher(ghaGuardNewTUFRequestFetcher(ctx,object))
    trusted,err:=root.FetchTrustedRootWithOptions(opts);if err!=nil||dsRunnerVerifySignedSubjects(ctx,trusted,rawBundle,p)!=nil{return nil,errDSRunnerEvidence}
    afterSize,err:=dsRunnerServerMatches(ctx,client,p,time.Now().UTC());if err!=nil{return nil,err};if afterSize!=size||ctx.Err()!=nil||authority.client!=client||digest(archive)!=p.ArchiveSHA||digest(held.payload)!=p.OutputSHA{return nil,errDSRunnerEvidence}
    candidate=&dsRunnerCandidate{issuer:&dsRunnerIssuer{token:1},pin:p,raw:held.payload};candidate.self=candidate;held.payload=nil
    return candidate,nil
}

// Match encoding/json's Unicode simple-fold equivalence without a quadratic
// scan of a large API object. Unknown fields remain opaque to the schema.
func dsRunnerFoldJSONKey(name string)string{
    var out strings.Builder
    for _,r:=range name{small:=r;for next:=unicode.SimpleFold(r);next!=r;next=unicode.SimpleFold(next){if next<small{small=next}};out.WriteRune(small)}
    return out.String()
}
func dsRunnerDecodeServerJSON(raw []byte,out any)error{
    v:=reflect.ValueOf(out)
    if !v.IsValid()||v.Kind()!=reflect.Pointer||v.IsNil()||v.Elem().Kind()!=reflect.Struct{return errDSRunnerEvidence}
    v.Elem().Set(reflect.Zero(v.Elem().Type()))
    if dsRunnerUniqueKeys(raw)!=nil||dsRunnerRequireServerFields(raw,v.Elem().Type(),0)!=nil{return errDSRunnerEvidence}
    d:=json.NewDecoder(bytes.NewReader(raw));if d.Decode(out)!=nil||d.Decode(new(any))!=io.EOF{v.Elem().Set(reflect.Zero(v.Elem().Type()));return errDSRunnerEvidence};return nil
}
func dsRunnerRequireServerFields(raw []byte,t reflect.Type,depth int)error{
    if depth>64||len(raw)==0||bytes.Equal(bytes.TrimSpace(raw),[]byte("null")){return errDSRunnerEvidence}
    if t==reflect.TypeOf(time.Time{}){return nil}
    switch t.Kind(){
    case reflect.Pointer:return dsRunnerRequireServerFields(raw,t.Elem(),depth+1)
    case reflect.Struct:
        var fields map[string]json.RawMessage
        if json.Unmarshal(raw,&fields)!=nil||fields==nil{return errDSRunnerEvidence}
        for i:=0;i<t.NumField();i++{f:=t.Field(i);if f.PkgPath!=""{continue};name:=strings.Split(f.Tag.Get("json"),",")[0];if name=="-"{continue};if name==""{name=strings.ToLower(f.Name)};value,ok:=fields[name];if !ok||dsRunnerRequireServerFields(value,f.Type,depth+1)!=nil{return errDSRunnerEvidence}}
    case reflect.Slice:
        var values []json.RawMessage
        if json.Unmarshal(raw,&values)!=nil{return errDSRunnerEvidence}
        for _,v:=range values{if dsRunnerRequireServerFields(v,t.Elem(),depth+1)!=nil{return errDSRunnerEvidence}}
    case reflect.String,reflect.Bool,reflect.Int,reflect.Int64:
    default:return errDSRunnerEvidence
    }
    return nil
}
