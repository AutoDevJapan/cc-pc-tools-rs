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
    "sort"
    "strconv"
    "strings"
    "time"
)

// The reviewed anchor is a minimum ancestor, not a fabricated run revision.
const ghaFreshProducerAnchor = "22ea37a850c6c2c85acbbc1f16fee449534caa65"
const ghaFreshAnchorNotBefore = "2026-11-15T17:21:17Z"
const ghaFreshWorkflowID int64 = 382798887
const ghaFreshOwnerID int64 = 88025750
const ghaFreshListingQuery = "branch=main&event=workflow_dispatch&created=%3E%3D2026-11-15&per_page=100"
const ghaFreshListingURL = "https://api.github.com/repos/AutoDevJapan/cc-pc-tools-rs/actions/workflows/382798887/runs?"+ghaFreshListingQuery
// GitHub caps filtered workflow-run searches at 1000 results. Reaching the
// cap cannot establish completeness, so it remains an explicit failure.
const ghaFreshListingMax = 999
const ghaFreshListingLimit = 2 << 20
var errGHAFreshExpiredArtifact = errors.New("guard_fresh_expired_artifact")

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
    createdAt time.Time
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
    if !ok||len(raw)==0||bytes.Equal(bytes.TrimSpace(raw),[]byte("null"))||json.Unmarshal(raw,&value)!=nil{return 0,false}
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

// Preserve only fixed context sentinels, never transport error text.
func ghaFreshContextFailure(ctx context.Context,failure error) error {
    var result error
    if ctx!=nil {result=ctx.Err()}
    if errors.Is(failure,context.Canceled){result=errors.Join(result,context.Canceled)}
    if errors.Is(failure,context.DeadlineExceeded){result=errors.Join(result,context.DeadlineExceeded)}
    if errors.Is(failure,errGHAGuardResponseCleanup){result=errors.Join(result,errGHAGuardResponseCleanup)}
    return result
}

// A unique matching successful run is a lookup hint only. Later code must
// independently obtain the authenticated job, artifact, expiry and signature.
func ghaFreshSelectRunHint(raw []byte) (ghaFreshRunHint,error) {
    return ghaFreshSelectRunHintAt(raw,time.Now().UTC())
}

func ghaFreshSelectRunHintAt(raw []byte,now time.Time) (ghaFreshRunHint,error) {
    values,err:=ghaFreshSelectRunHintsAt(raw,now)
    if err!=nil||len(values)!=1{return ghaFreshRunHint{},errGHAGuardUnqualified}
    return values[0],nil
}

// A single-page fixture remains bounded. The live reader below constructs all
// fixed-workflow pages itself and never follows an untrusted Link target.
func ghaFreshSelectRunHintsAt(raw []byte,now time.Time) ([]ghaFreshRunHint,error) {
    envelope,err:=ghaFreshObject(raw);if err!=nil{return nil,errGHAGuardUnqualified}
    total,ok:=ghaFreshInt(envelope,"total_count");if !ok||total<0||total>100{return nil,errGHAGuardUnqualified}
    var runs []json.RawMessage
    data,ok:=envelope["workflow_runs"]
    if !ok||json.Unmarshal(data,&runs)!=nil||int64(len(runs))!=total{return nil,errGHAGuardUnqualified}
    return ghaFreshSelectRunsAt(runs,now,false)
}

func ghaFreshSelectRunsAt(runs []json.RawMessage,now time.Time,strictWorkflow bool) ([]ghaFreshRunHint,error) {
    anchor,err:=time.Parse(time.RFC3339,ghaFreshAnchorNotBefore)
    if err!=nil||now.IsZero()||len(runs)>ghaFreshListingMax{return nil,errGHAGuardUnqualified}
    seen:=make(map[int64]bool,len(runs));var hints []ghaFreshRunHint
    for _,rawRun:=range runs {
        run,err:=ghaFreshObject(rawRun);if err!=nil{return nil,errGHAGuardUnqualified}
        id,iok:=ghaFreshInt(run,"id");workflow,wok:=ghaFreshInt(run,"workflow_id")
        if !iok||id<=0||seen[id]||!wok||workflow<=0{return nil,errGHAGuardUnqualified};seen[id]=true
        if workflow!=ghaFreshWorkflowID{if strictWorkflow{return nil,errGHAGuardUnqualified};continue}
        created,cok:=ghaFreshString(run,"created_at");when,terr:=time.Parse(time.RFC3339,created)
        if !cok||terr!=nil||when.After(now){return nil,errGHAGuardUnqualified}
        if when.Before(anchor){continue}
        status,sok:=ghaFreshString(run,"status");if !sok||status!="completed"{return nil,errGHAGuardUnqualified}
        conclusion,cok:=ghaFreshString(run,"conclusion");if !cok{return nil,errGHAGuardUnqualified}
        if conclusion!="success"{continue}
        attempt,aok:=ghaFreshInt(run,"run_attempt");head,hok:=ghaFreshString(run,"head_sha")
        if !aok||attempt!=1||!hok||!ghaGuardHex(head,40){return nil,errGHAGuardUnqualified}
        for _,item:=range []struct{key,want string}{
            {"head_branch","main"},{"path",".github/workflows/unattended-readiness.yml"},{"event","workflow_dispatch"},
        } {
            value,ok:=ghaFreshString(run,item.key);if !ok||value!=item.want{return nil,errGHAGuardUnqualified}
        }
        for _,item:=range []struct{key string;id int64}{
            {"repository",1406818713},{"head_repository",1406818713},{"actor",ghaFreshOwnerID},{"triggering_actor",ghaFreshOwnerID},
        } {
            if !ghaFreshNestedID(run,item.key,item.id){return nil,errGHAGuardUnqualified}
        }
        hints=append(hints,ghaFreshRunHint{runID:id,attempt:attempt,producerRevision:head,createdAt:when.UTC()})
    }
    sort.Slice(hints,func(i,j int)bool{return hints[i].runID<hints[j].runID})
    return hints,nil
}

// Fetch one explicitly constructed page. A complete response count and Link
// relationship are checked; server-provided URLs are never followed.
func ghaFreshReadRunsPage(ctx context.Context,client *http.Client,page int) ([]json.RawMessage,int64,error) {
    if ctx==nil{return nil,0,errGHAGuardUnqualified}
    if ctx.Err()!=nil||client==nil||page<1||page>10{return nil,0,errors.Join(errGHAGuardUnqualified,ctx.Err())}
    endpoint:=ghaFreshListingURL;if page>1{endpoint+=fmt.Sprintf("&page=%d",page)}
    request,err:=http.NewRequestWithContext(ctx,http.MethodGet,endpoint,nil);if err!=nil{return nil,0,errGHAGuardUnqualified}
    request.Header.Set("Accept","application/vnd.github+json");request.Header.Set("X-GitHub-Api-Version","2022-11-28")
    isolated:=*client;isolated.Jar=nil;isolated.CheckRedirect=func(*http.Request,[]*http.Request)error{return http.ErrUseLastResponse}
    response,err:=isolated.Do(request)
    if err!=nil||response==nil||response.Body==nil {var closeErr error;if response!=nil&&response.Body!=nil{closeErr=response.Body.Close()};return nil,0,errors.Join(ghaGuardResponseFailure(errGHAGuardUnqualified,closeErr,ctx.Err()),ghaFreshContextFailure(ctx,err))}
    if response.StatusCode!=http.StatusOK {
        closeErr:=response.Body.Close();primary:=error(errGHAGuardUnqualified)
        if response.StatusCode>=100&&response.StatusCode<=599{primary=errors.Join(primary,&ghaFreshHTTPStatusError{phase:"runs",status:response.StatusCode})}
        return nil,0,ghaGuardResponseFailure(primary,closeErr,ctx.Err())
    }
    raw,readErr:=io.ReadAll(io.LimitReader(response.Body,ghaFreshListingLimit+1));closeErr:=response.Body.Close();defer clear(raw)
    if readErr!=nil||closeErr!=nil||len(raw)>ghaFreshListingLimit||ctx.Err()!=nil{return nil,0,errors.Join(ghaGuardResponseFailure(errGHAGuardUnqualified,closeErr,ctx.Err()),ghaFreshContextFailure(ctx,readErr))}
    envelope,err:=ghaFreshObject(raw);if err!=nil{return nil,0,errGHAGuardUnqualified}
    total,ok:=ghaFreshInt(envelope,"total_count");if !ok||total<0||total>ghaFreshListingMax{return nil,0,errGHAGuardUnqualified}
    var runs []json.RawMessage;if json.Unmarshal(envelope["workflow_runs"],&runs)!=nil||len(runs)>100{return nil,0,errGHAGuardUnqualified}
    pages:=int((total+99)/100);if pages==0{pages=1};if page>pages{return nil,0,errGHAGuardUnqualified}
    expected:=int(total)-100*(page-1);if expected>100{expected=100};if expected<0||len(runs)!=expected{return nil,0,errGHAGuardUnqualified}
    link:=response.Header.Get("Link");hasNext:=strings.Contains(link,`rel="next"`)
    if (page<pages&&(!hasNext||link==""))||(page==pages&&hasNext)||(pages==1&&link!=""){return nil,0,errGHAGuardUnqualified}
    return runs,total,nil
}

func ghaFreshReadRunHint(ctx context.Context,client *http.Client) (ghaFreshRunHint,error) {
    return ghaFreshReadRunHintWith(ctx,client,client,time.Now().UTC())
}

// The listing client may be the existing scoped credential session; other
// run preflights stay on the public client and cannot receive its token.
func ghaFreshReadRunHintWith(ctx context.Context,listing,public *http.Client,now time.Time) (ghaFreshRunHint,error) {
    var none ghaFreshRunHint
    if ctx==nil{return none,errGHAGuardUnqualified}
    if ctx.Err()!=nil||listing==nil||public==nil||now.IsZero(){return none,errors.Join(errGHAGuardUnqualified,ctx.Err())}
    bounded,cancel:=context.WithTimeout(ctx,45*time.Second);defer cancel()
    first,total,err:=ghaFreshReadRunsPage(bounded,listing,1);if err!=nil{return none,err}
    pages:=int((total+99)/100);if pages==0{pages=1}
    runs:=append([]json.RawMessage(nil),first...)
    for page:=2;page<=pages;page++ {part,n,err:=ghaFreshReadRunsPage(bounded,listing,page);if err!=nil{return none,err};if n!=total{return none,errGHAGuardUnqualified};runs=append(runs,part...)}
    hints,err:=ghaFreshSelectRunsAt(runs,now,true);if err!=nil||len(hints)==0||bounded.Err()!=nil{return none,errors.Join(errGHAGuardUnqualified,bounded.Err())}
    if pages>1 {
        var repeat []json.RawMessage
        for page:=1;page<=pages;page++ {part,n,err:=ghaFreshReadRunsPage(bounded,listing,page);if err!=nil{return none,err};if n!=total{return none,errGHAGuardUnqualified};repeat=append(repeat,part...)}
        again,err:=ghaFreshSelectRunsAt(repeat,now,true);if err!=nil||len(again)!=len(hints){return none,errGHAGuardUnqualified}
        for i:=range hints {if hints[i]!=again[i]{return none,errGHAGuardUnqualified}}
    }
    if bounded.Err()!=nil{return none,errors.Join(errGHAGuardUnqualified,bounded.Err())}
    if len(hints)==1{return hints[0],nil}
    live:=0
    for _,hint:=range hints {
        _,preflightErr:=ghaFreshPublicPreflight(bounded,public,hint,now)
        if errors.Is(preflightErr,errGHAFreshExpiredArtifact){continue}
        if preflightErr!=nil{return none,errors.Join(preflightErr,bounded.Err())}
        none=hint;live++
    }
    if live!=1||bounded.Err()!=nil{return ghaFreshRunHint{},errors.Join(errGHAGuardUnqualified,bounded.Err())}
    return none,nil
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
    return h.runID>0&&h.attempt==1&&ghaGuardHex(h.producerRevision,40)
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
    if ctx==nil{return nil,errGHAGuardUnqualified}
    if ctx.Err()!=nil||client==nil{return nil,errors.Join(errGHAGuardUnqualified,ctx.Err())}
    endpoint,err:=ghaFreshResourceURL(kind,h);if err!=nil{return nil,err}
    bounded,cancel:=context.WithTimeout(ctx,15*time.Second);defer cancel()
    request,err:=http.NewRequestWithContext(bounded,http.MethodGet,endpoint,nil);if err!=nil{return nil,errGHAGuardUnqualified}
    request.Header.Set("Accept","application/vnd.github+json");request.Header.Set("X-GitHub-Api-Version","2022-11-28")
    noRedirect:=*client;noRedirect.CheckRedirect=func(*http.Request,[]*http.Request)error{return http.ErrUseLastResponse}
    response,err:=noRedirect.Do(request)
    if err!=nil||response==nil||response.Body==nil {
        var closeErr error;if response!=nil&&response.Body!=nil {closeErr=response.Body.Close()}
        return nil,errors.Join(ghaGuardResponseFailure(errGHAGuardUnqualified,closeErr,bounded.Err()),ghaFreshContextFailure(bounded,err))
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
    if readErr!=nil||closeErr!=nil||len(raw)>ghaFreshListingLimit||bounded.Err()!=nil{return nil,errors.Join(ghaGuardResponseFailure(errGHAGuardUnqualified,closeErr,bounded.Err()),ghaFreshContextFailure(bounded,readErr))}
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
    matches:=0;expiredMatch:=false
    for _,raw:=range artifacts {
        artifact,err:=ghaFreshObject(raw);if err!=nil{return none,err}
        name,ok:=ghaFreshString(artifact,"name");if !ok{return none,errGHAGuardUnqualified}
        if name!=expected{continue}
        matches++
        id,iok:=ghaFreshInt(artifact,"id");size,sok:=ghaFreshInt(artifact,"size_in_bytes")
        expired,eok:=ghaFreshBool(artifact,"expired");dig,dok:=ghaFreshString(artifact,"digest")
        var expiry time.Time
        if !iok||id<=0||!sok||size<=0||size>ghaGuardArchiveLimit||!eok||!dok||!strings.HasPrefix(dig,"sha256:")||!ghaGuardHex(strings.TrimPrefix(dig,"sha256:"),64)||json.Unmarshal(artifact["expires_at"],&expiry)!=nil||expiry.IsZero(){return none,errGHAGuardUnqualified}
        workflow,err:=ghaFreshObject(artifact["workflow_run"]);if err!=nil{return none,err}
        run,rok:=ghaFreshInt(workflow,"id");repo,pok:=ghaFreshInt(workflow,"repository_id");headRepo,hok:=ghaFreshInt(workflow,"head_repository_id");revision,vok:=ghaFreshString(workflow,"head_sha")
        if !rok||run!=h.runID||!pok||repo!=1406818713||!hok||headRepo!=1406818713||!vok||revision!=h.producerRevision{return none,errGHAGuardUnqualified}
        if expired||!expiry.After(now){expiredMatch=true;continue}
        none=ghaFreshPublicLocator{hint:h,jobID:jobID,artifactID:id,archiveBytes:size,archiveSHA:strings.TrimPrefix(dig,"sha256:"),expiresAt:expiry}
    }
    if matches!=1{return ghaFreshPublicLocator{},errGHAGuardUnqualified}
    if expiredMatch{return ghaFreshPublicLocator{},errGHAFreshExpiredArtifact}
    return none,nil
}

// For this fixed reviewed workflow, the upload's one-day retention starts
// no later than the successful job's authenticated completion. A completely
// enumerated missing name is skippable only after that bound plus one minute.
// An unknown, incomplete or newer missing result never becomes an expiry.
func ghaFreshMissingPastRetention(jobs,artifacts map[string]json.RawMessage,h ghaFreshRunHint,jobID int64,now time.Time) bool {
    if now.IsZero()||jobID<=0||h.createdAt.IsZero()||h.createdAt.After(now){return false}
    total,ok:=ghaFreshInt(artifacts,"total_count");values,valid:=ghaFreshArray(artifacts,"artifacts")
    if !ok||!valid||total!=int64(len(values)){return false}
    expected:=fmt.Sprintf("guard-bootstrap-%d-%d-%d",h.runID,h.attempt,jobID)
    seen:=map[int64]bool{}
    for _,raw:=range values {
        item,err:=ghaFreshObject(raw);if err!=nil{return false}
        name,nok:=ghaFreshString(item,"name");id,iok:=ghaFreshInt(item,"id")
        if !nok||name==""||name==expected||!iok||id<=0||seen[id]{return false};seen[id]=true
    }
    jobTotal,ok:=ghaFreshInt(jobs,"total_count");entries,valid:=ghaFreshArray(jobs,"jobs")
    if !ok||!valid||jobTotal!=int64(len(entries)){return false}
    found:=0;var completion time.Time
    for _,raw:=range entries {
        item,err:=ghaFreshObject(raw);if err!=nil{return false};id,ok:=ghaFreshInt(item,"id");if !ok{return false};if id!=jobID{continue}
        found++;run,rok:=ghaFreshInt(item,"run_id");status,sok:=ghaFreshString(item,"status");conclusion,cok:=ghaFreshString(item,"conclusion");value,tok:=ghaFreshString(item,"completed_at");startValue,startOK:=ghaFreshString(item,"started_at")
        if !rok||run!=h.runID||!sok||status!="completed"||!cok||conclusion!="success"||!tok||!startOK{return false}
        completion,err=time.Parse(time.RFC3339Nano,value);if err!=nil||completion.IsZero()||completion.After(now){return false}
        started,startErr:=time.Parse(time.RFC3339Nano,startValue);anchor,anchorErr:=time.Parse(time.RFC3339,ghaFreshAnchorNotBefore)
        if startErr!=nil||anchorErr!=nil||h.createdAt.Before(anchor)||started.Before(h.createdAt)||completion.Before(started){return false}
    }
    return found==1&&!now.Before(completion.Add(24*time.Hour+time.Minute))
}

func ghaFreshPublicPreflight(ctx context.Context,client *http.Client,h ghaFreshRunHint,now time.Time) (ghaFreshPublicLocator,error) {
    var none ghaFreshPublicLocator
    if ctx==nil{return none,errGHAGuardUnqualified}
    if ctx.Err()!=nil||client==nil||!ghaFreshHintShape(h)||now.IsZero(){return none,errors.Join(errGHAGuardUnqualified,ctx.Err())}
    workflow,err:=ghaFreshReadResource(ctx,client,ghaFreshWorkflowResource,h);if err!=nil{return none,err}
    id,iok:=ghaFreshInt(workflow,"id");path,pok:=ghaFreshString(workflow,"path");state,sok:=ghaFreshString(workflow,"state")
    if !iok||id!=ghaFreshWorkflowID||!pok||path!=".github/workflows/unattended-readiness.yml"||!sok||state!="active"{return none,errGHAGuardUnqualified}
    jobs,err:=ghaFreshReadResource(ctx,client,ghaFreshJobsResource,h);if err!=nil{return none,err}
    jobID,checkID,err:=ghaFreshSelectJob(jobs,h);if err!=nil{return none,err}
    artifacts,err:=ghaFreshReadResource(ctx,client,ghaFreshArtifactsResource,h);if err!=nil{return none,err}
    loc,err:=ghaFreshSelectArtifact(artifacts,h,jobID,now)
    if err!=nil {
        if errors.Is(err,errGHAFreshExpiredArtifact){return none,err}
        if ghaFreshMissingPastRetention(jobs,artifacts,h,jobID,now){return none,errGHAFreshExpiredArtifact}
        return none,err
    }
    loc.checkRunID=checkID
    if ctx.Err()!=nil{return none,errors.Join(errGHAGuardUnqualified,ctx.Err())}
    return loc,nil
}
