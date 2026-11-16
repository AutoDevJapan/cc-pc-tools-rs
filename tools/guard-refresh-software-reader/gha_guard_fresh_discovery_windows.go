//go:build windows

package main

import (
    "bytes"
    "context"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "net/http"
    "strconv"
    "strings"
    "time"
)

// This fixed revision is the reviewed producer checkpoint for this proposal.
// If main moves before dispatch, this source must be requalified.
const ghaFreshProducerRevision = "5a28f6fa6d9845bc1cd6fe5352abbcda275bd099"
const ghaFreshWorkflowID int64 = 382798887
const ghaFreshOwnerID int64 = 88025750
const ghaFreshListingURL = "https://api.github.com/repos/AutoDevJapan/cc-pc-tools-rs/actions/workflows/382798887/runs?branch=main&event=workflow_dispatch&head_sha=5a28f6fa6d9845bc1cd6fe5352abbcda275bd099&per_page=100"
const ghaFreshListingLimit = 2 << 20

// A bounded public HTTP status is an observation, never an authority.
type ghaFreshHTTPStatusError struct { phase string; status int }
func (e *ghaFreshHTTPStatusError) Error() string {
    if e==nil {return "guard_fresh_http_unavailable"}
    return fmt.Sprintf("guard_fresh_%s_http_%d",e.phase,e.status)
}

// A hint locates public server metadata only. It is not an issuer, receipt,
// retained candidate, credential permission, target purpose or update grant.
type ghaFreshRunHint struct {
    runID int64
    attempt int64
    producerRevision string
}

func ghaFreshObject(raw []byte) (map[string]json.RawMessage,error) {
    if len(raw)==0 || len(raw)>ghaFreshListingLimit {return nil,errGHAGuardUnqualified}
    dec:=json.NewDecoder(bytes.NewReader(raw))
    token,err:=dec.Token();if err!=nil||token!=json.Delim('{'){return nil,errGHAGuardUnqualified}
    result:=make(map[string]json.RawMessage);folded:=make(map[string]bool)
    for dec.More() {
        keyToken,e:=dec.Token();key,ok:=keyToken.(string)
        if e!=nil||!ok||key==""||len(key)>256||len(result)>=256||folded[strings.ToLower(key)] {return nil,errGHAGuardUnqualified}
        var value json.RawMessage
        if dec.Decode(&value)!=nil{return nil,errGHAGuardUnqualified}
        result[key]=value;folded[strings.ToLower(key)]=true
    }
    end,err:=dec.Token();if err!=nil||end!=json.Delim('}')||dec.Decode(new(any))!=io.EOF{return nil,errGHAGuardUnqualified}
    return result,nil
}

func ghaFreshInt(m map[string]json.RawMessage,key string) (int64,bool) {
    var value int64
    raw,ok:=m[key]
    if !ok||len(raw)==0||json.Unmarshal(raw,&value)!=nil{return 0,false}
    return value,true
}

func ghaFreshString(m map[string]json.RawMessage,key string) (string,bool) {
    var value string
    raw,ok:=m[key]
    if !ok||len(raw)==0||json.Unmarshal(raw,&value)!=nil{return "",false}
    return value,true
}

func ghaFreshNestedID(m map[string]json.RawMessage,key string,expected int64) bool {
    raw,ok:=m[key];if !ok{return false}
    nested,err:=ghaFreshObject(raw);if err!=nil{return false}
    id,ok:=ghaFreshInt(nested,"id")
    return ok&&id==expected
}

// A unique matching successful run is a lookup hint only. Later code must
// independently obtain the authenticated job, artifact, expiry and signature.
func ghaFreshSelectRunHint(raw []byte) (ghaFreshRunHint,error) {
    var none ghaFreshRunHint
    envelope,err:=ghaFreshObject(raw);if err!=nil{return none,err}
    total,ok:=ghaFreshInt(envelope,"total_count");if !ok||total!=1{return none,errGHAGuardUnqualified}
    var runs []json.RawMessage
    data,ok:=envelope["workflow_runs"]
    if !ok||json.Unmarshal(data,&runs)!=nil||len(runs)!=1{return none,errGHAGuardUnqualified}
    run,err:=ghaFreshObject(runs[0]);if err!=nil{return none,err}
    runID,ok:=ghaFreshInt(run,"id");if !ok||runID<=0{return none,errGHAGuardUnqualified}
    attempt,ok:=ghaFreshInt(run,"run_attempt");if !ok||attempt!=1{return none,errGHAGuardUnqualified}
    workflow,ok:=ghaFreshInt(run,"workflow_id");if !ok||workflow!=ghaFreshWorkflowID{return none,errGHAGuardUnqualified}
    for _,item:=range []struct{key,want string}{
        {"head_sha",ghaFreshProducerRevision},
        {"head_branch","main"},
        {"path",".github/workflows/unattended-readiness.yml"},
        {"event","workflow_dispatch"},
        {"status","completed"},
        {"conclusion","success"},
    } {
        value,ok:=ghaFreshString(run,item.key)
        if !ok||value!=item.want{return none,errGHAGuardUnqualified}
    }
    for _,item:=range []struct{key string;id int64}{
        {"repository",1406818713},{"head_repository",1406818713},
        {"actor",ghaFreshOwnerID},{"triggering_actor",ghaFreshOwnerID},
    } {
        if !ghaFreshNestedID(run,item.key,item.id){return none,errGHAGuardUnqualified}
    }
    return ghaFreshRunHint{runID:runID,attempt:attempt,producerRevision:ghaFreshProducerRevision},nil
}

// This bounded public lookup acquires only an untrusted hint. It does not read
// WinCred, download a private job log or artifact, or create any authority.
func ghaFreshReadRunHint(ctx context.Context,client *http.Client) (ghaFreshRunHint,error) {
    var none ghaFreshRunHint
    if ctx==nil||ctx.Err()!=nil||client==nil{return none,errGHAGuardUnqualified}
    bounded,cancel:=context.WithTimeout(ctx,15*time.Second);defer cancel()
    request,err:=http.NewRequestWithContext(bounded,http.MethodGet,ghaFreshListingURL,nil)
    if err!=nil{return none,errGHAGuardUnqualified}
    request.Header.Set("Accept","application/vnd.github+json")
    request.Header.Set("X-GitHub-Api-Version","2022-11-28")
    noRedirect:=*client
    noRedirect.CheckRedirect=func(*http.Request,[]*http.Request)error{return http.ErrUseLastResponse}
    response,err:=noRedirect.Do(request)
    if err!=nil||response==nil||response.Body==nil {
        var closeErr error;if response!=nil&&response.Body!=nil {closeErr=response.Body.Close()}
        return none,ghaGuardResponseFailure(errGHAGuardUnqualified,closeErr,bounded.Err())
    }
    if response.StatusCode!=http.StatusOK {
        closeErr:=response.Body.Close()
        if response.StatusCode>=100&&response.StatusCode<=599 {
            return none,ghaGuardResponseFailure(errors.Join(errGHAGuardUnqualified,&ghaFreshHTTPStatusError{phase:"runs",status:response.StatusCode}),closeErr,bounded.Err())
        }
        return none,ghaGuardResponseFailure(errGHAGuardUnqualified,closeErr,bounded.Err())
    }
    if !ghaGuardSinglePageLink(response.Header) {
        return none,ghaGuardResponseFailure(errGHAGuardUnqualified,response.Body.Close(),bounded.Err())
    }
    raw,readErr:=io.ReadAll(io.LimitReader(response.Body,ghaFreshListingLimit+1))
    closeErr:=response.Body.Close()
    defer clear(raw)
    if readErr!=nil||closeErr!=nil||len(raw)>ghaFreshListingLimit||bounded.Err()!=nil{return none,ghaGuardResponseFailure(errGHAGuardUnqualified,closeErr,bounded.Err())}
    return ghaFreshSelectRunHint(raw)
}


// This locator is an untrusted public observation. It cannot issue an owned
// candidate, authorize credential access by itself, or open a target.
type ghaFreshPublicLocator struct {
    hint ghaFreshRunHint
    jobID,checkRunID,artifactID int64
    archiveBytes int64
    archiveSHA string
    expiresAt time.Time
}

type ghaFreshResource uint8
const (
    ghaFreshWorkflowResource ghaFreshResource = iota + 1
    ghaFreshJobsResource
    ghaFreshArtifactsResource
)

func ghaFreshHintShape(h ghaFreshRunHint) bool {
    return h.runID>0&&h.attempt==1&&h.producerRevision==ghaFreshProducerRevision
}

// The endpoint is constructed only from one of three fixed operations and a
// bounded numeric server hint; no URL, path, hash or credential is caller input.
func ghaFreshResourceURL(kind ghaFreshResource,h ghaFreshRunHint) (string,error) {
    if !ghaFreshHintShape(h){return "",errGHAGuardUnqualified}
    const base="https://api.github.com/repos/AutoDevJapan/cc-pc-tools-rs/actions"
    switch kind {
    case ghaFreshWorkflowResource:return base+"/workflows/unattended-readiness.yml",nil
    case ghaFreshJobsResource:return fmt.Sprintf("%s/runs/%d/attempts/%d/jobs?per_page=100",base,h.runID,h.attempt),nil
    case ghaFreshArtifactsResource:return fmt.Sprintf("%s/runs/%d/artifacts?per_page=100",base,h.runID),nil
    default:return "",errGHAGuardUnqualified
    }
}

func ghaFreshReadResource(ctx context.Context,client *http.Client,kind ghaFreshResource,h ghaFreshRunHint) (map[string]json.RawMessage,error) {
    if ctx==nil||ctx.Err()!=nil||client==nil{return nil,errGHAGuardUnqualified}
    endpoint,err:=ghaFreshResourceURL(kind,h);if err!=nil{return nil,err}
    bounded,cancel:=context.WithTimeout(ctx,15*time.Second);defer cancel()
    request,err:=http.NewRequestWithContext(bounded,http.MethodGet,endpoint,nil);if err!=nil{return nil,errGHAGuardUnqualified}
    request.Header.Set("Accept","application/vnd.github+json");request.Header.Set("X-GitHub-Api-Version","2022-11-28")
    noRedirect:=*client;noRedirect.CheckRedirect=func(*http.Request,[]*http.Request)error{return http.ErrUseLastResponse}
    response,err:=noRedirect.Do(request)
    if err!=nil||response==nil||response.Body==nil {
        var closeErr error;if response!=nil&&response.Body!=nil {closeErr=response.Body.Close()}
        return nil,ghaGuardResponseFailure(errGHAGuardUnqualified,closeErr,bounded.Err())
    }
    if response.StatusCode!=http.StatusOK {
        closeErr:=response.Body.Close()
        if response.StatusCode>=100&&response.StatusCode<=599{return nil,ghaGuardResponseFailure(errors.Join(errGHAGuardUnqualified,&ghaFreshHTTPStatusError{phase:"metadata",status:response.StatusCode}),closeErr,bounded.Err())}
        return nil,ghaGuardResponseFailure(errGHAGuardUnqualified,closeErr,bounded.Err())
    }
    if !ghaGuardSinglePageLink(response.Header) {
        return nil,ghaGuardResponseFailure(errGHAGuardUnqualified,response.Body.Close(),bounded.Err())
    }
    raw,readErr:=io.ReadAll(io.LimitReader(response.Body,ghaFreshListingLimit+1));closeErr:=response.Body.Close()
    defer clear(raw)
    if readErr!=nil||closeErr!=nil||len(raw)>ghaFreshListingLimit||bounded.Err()!=nil{return nil,ghaGuardResponseFailure(errGHAGuardUnqualified,closeErr,bounded.Err())}
    return ghaFreshObject(raw)
}

func ghaFreshArray(m map[string]json.RawMessage,key string) ([]json.RawMessage,bool) {
    raw,ok:=m[key];if !ok||len(raw)==0||bytes.Equal(raw,[]byte("null")){return nil,false}
    var value []json.RawMessage
    if json.Unmarshal(raw,&value)!=nil||len(value)>100{return nil,false}
    return value,true
}

func ghaFreshBool(m map[string]json.RawMessage,key string) (bool,bool) {
    raw,ok:=m[key];if !ok||len(raw)==0||bytes.Equal(raw,[]byte("null")){return false,false}
    var value bool
    if json.Unmarshal(raw,&value)!=nil{return false,false}
    return value,true
}

func ghaFreshStepSequence(raw json.RawMessage) bool {
    var values []json.RawMessage
    if json.Unmarshal(raw,&values)!=nil||len(values)>100{return false}
    want:=[]string{"Capture the fixed guard build","Check the exact captured files","Attest the guard and source evidence","Upload the fixed guard capture","Record the server-assigned capture artifact"}
    required:=make(map[string]bool,len(want));for _,v:=range want {required[v]=true}
    index:=0;last:=int64(0)
    for _,value:=range values {
        step,err:=ghaFreshObject(value);if err!=nil{return false}
        name,ok:=ghaFreshString(step,"name");if !ok{return false}
        if !required[name]{continue}
        status,sok:=ghaFreshString(step,"status");conclusion,cok:=ghaFreshString(step,"conclusion");number,nok:=ghaFreshInt(step,"number")
        if index>=len(want)||name!=want[index]||!sok||status!="completed"||!cok||conclusion!="success"||!nok||number<=last{return false}
        index++;last=number
    }
    return index==len(want)
}

func ghaFreshSelectJob(m map[string]json.RawMessage,h ghaFreshRunHint) (int64,int64,error) {
    total,ok:=ghaFreshInt(m,"total_count");if !ok{return 0,0,errGHAGuardUnqualified}
    jobs,ok:=ghaFreshArray(m,"jobs");if !ok||total!=int64(len(jobs)){return 0,0,errGHAGuardUnqualified}
    const prefix="https://api.github.com/repos/AutoDevJapan/cc-pc-tools-rs/check-runs/"
    var jobID,checkID int64;matches:=0
    for _,raw:=range jobs {
        job,err:=ghaFreshObject(raw);if err!=nil{return 0,0,errGHAGuardUnqualified}
        name,ok:=ghaFreshString(job,"name");if !ok{return 0,0,errGHAGuardUnqualified}
        if name!="Guard bootstrap artifact"{continue}
        matches++
        id,iok:=ghaFreshInt(job,"id");run,rok:=ghaFreshInt(job,"run_id")
        status,sok:=ghaFreshString(job,"status");conclusion,cok:=ghaFreshString(job,"conclusion")
        check,uok:=ghaFreshString(job,"check_run_url")
        suffix:=strings.TrimPrefix(check,prefix)
        parsed,err:=strconv.ParseInt(suffix,10,64)
        steps,stepsOK:=job["steps"]
        if !iok||id<=0||!rok||run!=h.runID||!sok||status!="completed"||!cok||conclusion!="success"||!uok||suffix==check||err!=nil||parsed<=0||strconv.FormatInt(parsed,10)!=suffix||!stepsOK||!ghaFreshStepSequence(steps){return 0,0,errGHAGuardUnqualified}
        jobID,checkID=id,parsed
    }
    if matches!=1{return 0,0,errGHAGuardUnqualified}
    return jobID,checkID,nil
}

func ghaFreshSelectArtifact(m map[string]json.RawMessage,h ghaFreshRunHint,jobID int64,now time.Time) (ghaFreshPublicLocator,error) {
    var none ghaFreshPublicLocator
    total,ok:=ghaFreshInt(m,"total_count");if !ok||now.IsZero()||jobID<=0{return none,errGHAGuardUnqualified}
    artifacts,ok:=ghaFreshArray(m,"artifacts");if !ok||total!=int64(len(artifacts)){return none,errGHAGuardUnqualified}
    expected:=fmt.Sprintf("guard-bootstrap-%d-%d-%d",h.runID,h.attempt,jobID)
    matches:=0
    for _,raw:=range artifacts {
        artifact,err:=ghaFreshObject(raw);if err!=nil{return none,err}
        name,ok:=ghaFreshString(artifact,"name");if !ok{return none,errGHAGuardUnqualified}
        if name!=expected{continue}
        matches++
        id,iok:=ghaFreshInt(artifact,"id");size,sok:=ghaFreshInt(artifact,"size_in_bytes")
        expired,eok:=ghaFreshBool(artifact,"expired");dig,dok:=ghaFreshString(artifact,"digest")
        var expiry time.Time
        if !iok||id<=0||!sok||size<=0||size>ghaGuardArchiveLimit||!eok||expired||!dok||!strings.HasPrefix(dig,"sha256:")||!ghaGuardHex(strings.TrimPrefix(dig,"sha256:"),64)||json.Unmarshal(artifact["expires_at"],&expiry)!=nil||expiry.IsZero()||!expiry.After(now){return none,errGHAGuardUnqualified}
        workflow,err:=ghaFreshObject(artifact["workflow_run"]);if err!=nil{return none,err}
        run,rok:=ghaFreshInt(workflow,"id");repo,pok:=ghaFreshInt(workflow,"repository_id");headRepo,hok:=ghaFreshInt(workflow,"head_repository_id");revision,vok:=ghaFreshString(workflow,"head_sha")
        if !rok||run!=h.runID||!pok||repo!=1406818713||!hok||headRepo!=1406818713||!vok||revision!=h.producerRevision{return none,errGHAGuardUnqualified}
        none=ghaFreshPublicLocator{hint:h,jobID:jobID,artifactID:id,archiveBytes:size,archiveSHA:strings.TrimPrefix(dig,"sha256:"),expiresAt:expiry}
    }
    if matches!=1{return ghaFreshPublicLocator{},errGHAGuardUnqualified}
    return none,nil
}

func ghaFreshPublicPreflight(ctx context.Context,client *http.Client,h ghaFreshRunHint,now time.Time) (ghaFreshPublicLocator,error) {
    var none ghaFreshPublicLocator
    if ctx==nil||ctx.Err()!=nil||client==nil||!ghaFreshHintShape(h)||now.IsZero(){return none,errGHAGuardUnqualified}
    workflow,err:=ghaFreshReadResource(ctx,client,ghaFreshWorkflowResource,h);if err!=nil{return none,err}
    id,iok:=ghaFreshInt(workflow,"id");path,pok:=ghaFreshString(workflow,"path");state,sok:=ghaFreshString(workflow,"state")
    if !iok||id!=ghaFreshWorkflowID||!pok||path!=".github/workflows/unattended-readiness.yml"||!sok||state!="active"{return none,errGHAGuardUnqualified}
    jobs,err:=ghaFreshReadResource(ctx,client,ghaFreshJobsResource,h);if err!=nil{return none,err}
    jobID,checkID,err:=ghaFreshSelectJob(jobs,h);if err!=nil{return none,err}
    artifacts,err:=ghaFreshReadResource(ctx,client,ghaFreshArtifactsResource,h);if err!=nil{return none,err}
    loc,err:=ghaFreshSelectArtifact(artifacts,h,jobID,now);if err!=nil{return none,err}
    loc.checkRunID=checkID
    if ctx.Err()!=nil{return none,errGHAGuardUnqualified}
    return loc,nil
}
