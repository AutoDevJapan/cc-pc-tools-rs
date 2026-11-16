//go:build windows

package main

import (
    "archive/tar"
    "bytes"
    "compress/gzip"
    "context"
    "errors"
    "io"
    "net/http"
    "net/http/httptrace"
    "net/url"
    "os"
    "strconv"
    "strings"
    "sync/atomic"
    "testing"
    "time"
)

type ghaGuardSourceTestEntry struct {name string; body []byte; mode int64; kind byte; link string}

func ghaGuardSourceTestRecords() []ghaGuardSourceRecord {
    files:=[]ghaGuardSourceTestEntry{{name:"repair-guard/a.go",body:[]byte("alpha")},{name:"tools/civ6-ci-contract/b.go",body:[]byte("bravo")}}
    out:=make([]ghaGuardSourceRecord,0,len(files))
    for _,f:=range files {out=append(out,ghaGuardSourceRecord{Path:f.name,Size:int64(len(f.body)),SHA:digest(f.body),Blob:ghaGuardGitObjectID("blob",f.body)})}
    return out
}

func ghaGuardSourceTestArchive(t *testing.T,entries []ghaGuardSourceTestEntry,global string,extraTail bool) []byte {
    t.Helper();var out bytes.Buffer
    zw:=gzip.NewWriter(&out);tw:=tar.NewWriter(zw)
    if global!="" {if err:=tw.WriteHeader(&tar.Header{Name:"pax_global_header",Typeflag:tar.TypeXGlobalHeader,PAXRecords:map[string]string{"comment":global}});err!=nil {t.Fatal(err)}}
    for _,e:=range entries {
        kind:=e.kind;if kind==0 {kind=tar.TypeReg};mode:=e.mode;if mode==0 {mode=0664}
        size:=int64(len(e.body));if kind!=tar.TypeReg {size=0}
        h:=&tar.Header{Name:ghaGuardSourceArchivePrefix+e.name,Typeflag:kind,Mode:mode,Size:size,Linkname:e.link}
        if err:=tw.WriteHeader(h);err!=nil {t.Fatal(err)}
        if size!=0 {if _,err:=tw.Write(e.body);err!=nil {t.Fatal(err)}}
    }
    if err:=tw.Close();err!=nil {t.Fatal(err)}
    if extraTail {if _,err:=zw.Write([]byte{1});err!=nil {t.Fatal(err)}}
    if err:=zw.Close();err!=nil {t.Fatal(err)}
    return out.Bytes()
}

func TestGHAGuardPinnedSourceArchive(t *testing.T) {
    records:=ghaGuardSourceTestRecords()
    a:=ghaGuardSourceTestEntry{name:records[0].Path,body:[]byte("alpha")}
    b:=ghaGuardSourceTestEntry{name:records[1].Path,body:[]byte("bravo")}
    cases:=[]struct{name string; entries []ghaGuardSourceTestEntry; global string; extraTail,valid bool}{
        {name:"valid",entries:[]ghaGuardSourceTestEntry{a,b},global:ghaGuardSource,valid:true},
        {name:"missing",entries:[]ghaGuardSourceTestEntry{a},global:ghaGuardSource},
        {name:"duplicate",entries:[]ghaGuardSourceTestEntry{a,a,b},global:ghaGuardSource},
        {name:"extra-target",entries:[]ghaGuardSourceTestEntry{a,b,{name:"repair-guard/extra.go",body:[]byte("x")}},global:ghaGuardSource},
        {name:"changed-bytes",entries:[]ghaGuardSourceTestEntry{{name:a.name,body:[]byte("alphb")},b},global:ghaGuardSource},
        {name:"executable-mode",entries:[]ghaGuardSourceTestEntry{{name:a.name,body:a.body,mode:0775},b},global:ghaGuardSource},
        {name:"wrong-read-mode",entries:[]ghaGuardSourceTestEntry{{name:a.name,body:a.body,mode:0444},b},global:ghaGuardSource},
        {name:"symlink",entries:[]ghaGuardSourceTestEntry{{name:a.name,kind:tar.TypeSymlink,link:"b"},b},global:ghaGuardSource},
        {name:"hardlink",entries:[]ghaGuardSourceTestEntry{{name:a.name,kind:tar.TypeLink,link:"b"},b},global:ghaGuardSource},
        {name:"missing-global",entries:[]ghaGuardSourceTestEntry{a,b}},
        {name:"wrong-global",entries:[]ghaGuardSourceTestEntry{a,b},global:"other"},
        {name:"nonzero-tail",entries:[]ghaGuardSourceTestEntry{a,b},global:ghaGuardSource,extraTail:true},
    }
    for _,tc:=range cases {t.Run(tc.name,func(t *testing.T){
        archive:=ghaGuardSourceTestArchive(t,tc.entries,tc.global,tc.extraTail)
        err:=ghaGuardValidatePinnedSourceArchive(context.Background(),archive,records)
        if (err==nil)!=tc.valid {t.Fatalf("unexpected result: %v",err)}
    })}
    valid:=ghaGuardSourceTestArchive(t,[]ghaGuardSourceTestEntry{a,b},ghaGuardSource,false)
    t.Run("compressed-trailing",func(t *testing.T){bad:=append(append([]byte(nil),valid...),1);if ghaGuardValidatePinnedSourceArchive(context.Background(),bad,records)==nil {t.Fatal("trailing compressed data accepted")}})
    t.Run("truncated",func(t *testing.T){if ghaGuardValidatePinnedSourceArchive(context.Background(),valid[:len(valid)-1],records)==nil {t.Fatal("truncated archive accepted")}})
    t.Run("cancelled",func(t *testing.T){ctx,cancel:=context.WithCancel(context.Background());cancel();if ghaGuardValidatePinnedSourceArchive(ctx,valid,records)==nil {t.Fatal("cancel accepted")}})
    t.Run("wrong-record-hash",func(t *testing.T){bad:=append([]ghaGuardSourceRecord(nil),records...);bad[0].SHA=digest([]byte("wrong"));if ghaGuardValidatePinnedSourceArchive(context.Background(),valid,bad)==nil {t.Fatal("wrong SHA accepted")}})
    t.Run("wrong-record-blob",func(t *testing.T){bad:=append([]ghaGuardSourceRecord(nil),records...);bad[0].Blob=ghaGuardGitObjectID("blob",[]byte("wrong"));if ghaGuardValidatePinnedSourceArchive(context.Background(),valid,bad)==nil {t.Fatal("wrong Git blob accepted")}})
}

// Test-only authentication is confined to the fixed read-only source endpoints.
// The codeload archive uses a separate unauthenticated transport.
type ghaGuardArchiveCIAuth struct { base http.RoundTripper; token string }

func (a *ghaGuardArchiveCIAuth) RoundTrip(req *http.Request) (*http.Response,error) {
    if a==nil || a.base==nil || !ghaGuardCredentialBytesValid([]byte(a.token)) || req==nil || req.URL==nil || req.Method!=http.MethodGet || (req.Body!=nil && req.Body!=http.NoBody) {return nil,errGHAGuardUnqualified}
    u:=req.URL
    if u.Scheme!="https" || u.Host!="api.github.com" || u.User!=nil || u.Opaque!="" || u.Fragment!="" || u.RawPath!="" || u.ForceQuery || u.RawQuery!="" || (req.Host!="" && req.Host!="api.github.com") {return nil,errGHAGuardUnqualified}
    for name,values:=range req.Header {
        if len(values)!=1 {return nil,errGHAGuardUnqualified}
        switch strings.ToLower(name) {
        case "accept":
            if values[0]!="application/vnd.github+json" {return nil,errGHAGuardUnqualified}
        case "x-github-api-version":
            if values[0]!="2022-11-28" {return nil,errGHAGuardUnqualified}
        default:return nil,errGHAGuardUnqualified
        }
    }
    const root="/repos/AutoDevJapan/cc-pc-tools-rs"
    valid:=u.Path==root || u.Path==root+"/git/commits/"+ghaGuardSource
    if strings.HasPrefix(u.Path,root+"/git/trees/") {valid=ghaGuardHex(strings.TrimPrefix(u.Path,root+"/git/trees/"),40)}
    if !valid {return nil,errGHAGuardUnqualified}
    if req.Header.Get("Accept")!="application/vnd.github+json" || req.Header.Get("X-GitHub-Api-Version")!="2022-11-28" {return nil,errGHAGuardUnqualified}
    next:=req.Clone(req.Context());next.Header=req.Header.Clone();if next.Header==nil {next.Header=make(http.Header)}
    next.Header.Set("Authorization","Bearer "+a.token)
    response,err:=a.base.RoundTrip(next)
    if response!=nil {response.Request=req}
    return response,err
}

// Fixed bounded fields only; never log remote text, URL, token, or error body.
func ghaGuardArchiveRateRemaining(h http.Header) string {
    values:=h.Values("X-RateLimit-Remaining");if len(values)!=1{return "unknown"}
    s:=values[0];if len(s)==0 || len(s)>9{return "unknown"}
    allZero:=true;for _,r:=range s {if r<'0'||r>'9' {return "unknown"};if r!='0' {allZero=false}}
    if allZero {return "zero"};return "nonzero"
}

func TestGHAGuardArchiveCIAuthBoundary(t *testing.T) {
    const secret="synthetic-token"
    requests:=[]string{ghaGuardGitAPI,ghaGuardGitAPI+"/git/commits/"+ghaGuardSource,ghaGuardGitAPI+"/git/trees/"+strings.Repeat("a",40)}
    calls:=0
    base:=ghaGuardTreeTestTransport(func(r *http.Request)(*http.Response,error){
        calls++
        if r.Header.Get("Authorization")!="Bearer "+secret {t.Fatal("expected scoped authorization")}
        return &http.Response{StatusCode:http.StatusOK,Header:http.Header{},Body:io.NopCloser(strings.NewReader("{}")),Request:r},nil
    })
    tr:=&ghaGuardArchiveCIAuth{base:base,token:secret}
    for _,raw:=range requests {
        req,err:=http.NewRequest(http.MethodGet,raw,nil);if err!=nil {t.Fatal(err)}
        req.Header.Set("Accept","application/vnd.github+json");req.Header.Set("X-GitHub-Api-Version","2022-11-28")
        resp,err:=tr.RoundTrip(req);if err!=nil||resp==nil {t.Fatal("eligible request failed")}
        if resp.Request.Header.Get("Authorization")!="" {t.Fatal("response retained authorization")}
        if resp.Body.Close()!=nil {t.Fatal("body close failed")}
    }
    for _,raw:=range []string{"https://codeload.github.com/AutoDevJapan/cc-pc-tools-rs/tar.gz/"+ghaGuardSource,ghaGuardGitAPI+"/git/trees/"+strings.Repeat("g",40),ghaGuardGitAPI+"/git/blobs/"+strings.Repeat("a",40),ghaGuardGitAPI+"?x=1",ghaGuardGitAPI+"/git/commits/"+strings.Repeat("a",40),"http://api.github.com/repos/AutoDevJapan/cc-pc-tools-rs", "https://api.github.com:443/repos/AutoDevJapan/cc-pc-tools-rs", "https://user@api.github.com/repos/AutoDevJapan/cc-pc-tools-rs",ghaGuardGitAPI+"#fragment","https://example.test/",ghaGuardGitAPI+"/actions/runs/1"} {
        req,err:=http.NewRequest(http.MethodGet,raw,nil);if err!=nil {t.Fatal(err)}
        req.Header.Set("Accept","application/vnd.github+json");req.Header.Set("X-GitHub-Api-Version","2022-11-28")
        if resp,err:=tr.RoundTrip(req);err==nil||resp!=nil {t.Fatal("unexpected destination accepted")}
    }
    for _,name:=range []string{"Authorization","Cookie","Proxy-Authorization","Cookie2","Host","X-Forwarded-Host","X-HTTP-Method-Override"} {
        req,_:=http.NewRequest(http.MethodGet,ghaGuardGitAPI,nil);req.Header.Set("Accept","application/vnd.github+json");req.Header.Set("X-GitHub-Api-Version","2022-11-28");req.Header.Set(name,"synthetic")
        if resp,err:=tr.RoundTrip(req);err==nil||resp!=nil {t.Fatal("extra header accepted")}
    }
    duplicateHeader,_:=http.NewRequest(http.MethodGet,ghaGuardGitAPI,nil);duplicateHeader.Header.Add("Accept","application/vnd.github+json");duplicateHeader.Header.Add("Accept","application/vnd.github+json");duplicateHeader.Header.Set("X-GitHub-Api-Version","2022-11-28")
    if resp,err:=tr.RoundTrip(duplicateHeader);err==nil||resp!=nil {t.Fatal("duplicate header accepted")}
    wrongHost,_:=http.NewRequest(http.MethodGet,ghaGuardGitAPI,nil);wrongHost.Header.Set("Accept","application/vnd.github+json");wrongHost.Header.Set("X-GitHub-Api-Version","2022-11-28");wrongHost.Host="example.test"
    if resp,err:=tr.RoundTrip(wrongHost);err==nil||resp!=nil {t.Fatal("host override accepted")}
    withBody,_:=http.NewRequest(http.MethodGet,ghaGuardGitAPI,strings.NewReader("x"));withBody.Header.Set("Accept","application/vnd.github+json");withBody.Header.Set("X-GitHub-Api-Version","2022-11-28")
    if resp,err:=tr.RoundTrip(withBody);err==nil||resp!=nil {t.Fatal("body accepted")}
    post,_:=http.NewRequest(http.MethodPost,ghaGuardGitAPI,nil);post.Header.Set("Accept","application/vnd.github+json");post.Header.Set("X-GitHub-Api-Version","2022-11-28");if _,err:=tr.RoundTrip(post);err==nil {t.Fatal("write method accepted")}
    forged,_:=http.NewRequest(http.MethodGet,ghaGuardGitAPI,nil);forged.Header.Set("Accept","application/vnd.github+json");forged.Header.Set("X-GitHub-Api-Version","2022-11-28");forged.Header.Set("Authorization","Bearer another");if _,err:=tr.RoundTrip(forged);err==nil {t.Fatal("caller authorization accepted")}
    noToken:=&ghaGuardArchiveCIAuth{base:base};valid,_:=http.NewRequest(http.MethodGet,ghaGuardGitAPI,nil);valid.Header.Set("Accept","application/vnd.github+json");valid.Header.Set("X-GitHub-Api-Version","2022-11-28");if _,err:=noToken.RoundTrip(valid);err==nil {t.Fatal("missing token accepted")}
    dirty,_:=url.Parse(ghaGuardGitAPI);dirty.ForceQuery=true;dirtyHeader:=make(http.Header);dirtyHeader.Set("Accept","application/vnd.github+json");dirtyHeader.Set("X-GitHub-Api-Version","2022-11-28");if _,err:=tr.RoundTrip(&http.Request{Method:http.MethodGet,URL:dirty,Header:dirtyHeader});err==nil {t.Fatal("empty query accepted")}
    if calls!=len(requests) {t.Fatal("invalid request reached transport")}
    client:=&http.Client{Transport:tr,CheckRedirect:func(*http.Request,[]*http.Request) error{return http.ErrUseLastResponse}}
    redirectBase:=ghaGuardTreeTestTransport(func(r *http.Request)(*http.Response,error){
        calls++;return &http.Response{StatusCode:http.StatusFound,Header:http.Header{"Location":[]string{"https://codeload.github.com/should-not-receive-token"}},Body:io.NopCloser(strings.NewReader("")),Request:r},nil
    })
    tr.base=redirectBase
    redirectReq,_:=http.NewRequest(http.MethodGet,ghaGuardGitAPI,nil);redirectReq.Header.Set("Accept","application/vnd.github+json");redirectReq.Header.Set("X-GitHub-Api-Version","2022-11-28")
    response,err:=client.Do(redirectReq);if err!=nil||response==nil||response.StatusCode!=http.StatusFound||calls!=len(requests)+1 {t.Fatal("redirect was followed or hidden")}
    if response.Body.Close()!=nil {t.Fatal("redirect close failed")}
    tr.base=base
    for _,tc:=range []struct{value,want string}{{"0","zero"},{"000","zero"},{"12","nonzero"},{"","unknown"},{"1\nsecret","unknown"},{"1234567890","unknown"}} {
        h:=http.Header{};if tc.value!="" {h.Set("X-RateLimit-Remaining",tc.value)}
        if got:=ghaGuardArchiveRateRemaining(h);got!=tc.want {t.Fatal("unexpected rate category")}
    }
    duplicate:=http.Header{};duplicate.Add("X-RateLimit-Remaining","0");duplicate.Add("X-RateLimit-Remaining","1");if ghaGuardArchiveRateRemaining(duplicate)!="unknown" {t.Fatal("ambiguous rate category")}
}

// This positive uses the production public fetch and fixed source fixture.
// It proves archive input equivalence, not issuer or installed authority.
func TestGHAGuardOfficialSourceArchiveReal(t *testing.T) {
    if os.Getenv("GUARD_GHA_ACTUAL_SIGNED_SOURCE")!="1" {t.Skip("actual public archive not requested")}
    fixture:=ghaGuardActualTestSourceFixture(t)
    records:=make([]ghaGuardSourceRecord,len(fixture.Inputs))
    for i,x:=range fixture.Inputs {records[i]=ghaGuardSourceRecord{Path:x.Path,Size:x.Size,SHA:x.SHA,Blob:x.Blob}}
    if len(records)!=249 {t.Fatal("wrong fixed input count")}
    ctx,cancel:=context.WithTimeout(context.Background(),110*time.Second);defer cancel()
    tr:=&http.Transport{Proxy:nil};defer tr.CloseIdleConnections()
    token:=os.Getenv("GH_TOKEN");if token=="" {t.Fatal("Actions source read token unavailable")}
    auth:=&ghaGuardArchiveCIAuth{base:tr,token:token}
    var apiRequests,apiNon200,apiTransportErrors,apiStatus atomic.Int64
    var apiRate atomic.Int64
    var apiFirstFailure atomic.Bool
    var archiveDial atomic.Bool
    trace:=&httptrace.ClientTrace{GetConn:func(hostPort string){if hostPort=="codeload.github.com:443" {archiveDial.Store(true)}}}
    ctx=httptrace.WithClientTrace(ctx,trace)
    observed:=ghaGuardTreeTestTransport(func(req *http.Request)(*http.Response,error){
        apiRequests.Add(1)
        response,err:=auth.RoundTrip(req)
        if err!=nil {apiTransportErrors.Add(1)}
        if response!=nil && response.StatusCode!=http.StatusOK {
            apiNon200.Add(1)
            if apiFirstFailure.CompareAndSwap(false,true) {apiStatus.Store(int64(response.StatusCode));switch ghaGuardArchiveRateRemaining(response.Header) {case "zero":apiRate.Store(1);case "nonzero":apiRate.Store(2);default:apiRate.Store(3)}}
        }
        return response,err
    })
    client:=&http.Client{Transport:observed,Timeout:30*time.Second,CheckRedirect:func(*http.Request,[]*http.Request)error{return http.ErrUseLastResponse}}
    if err:=ghaGuardVerifyGitSnapshotViaPublicArchive(ctx,client,ghaGuardSource,ghaGuardSourceTree,records);err!=nil {
        if apiNon200.Load()>0 {rate:="unknown";if apiRate.Load()==1 {rate="zero"} else if apiRate.Load()==2 {rate="nonzero"};t.Fatalf("official root or archive inputs mismatch (api_requests=%d api_non_200=%d api_first_failure_status=%d api_first_failure_rate_remaining=%s api_transport_errors=%d archive_dial=%t)",apiRequests.Load(),apiNon200.Load(),apiStatus.Load(),rate,apiTransportErrors.Load(),archiveDial.Load())}
        t.Fatalf("official root or archive inputs mismatch (api_requests=%d api_non_200=0 api_transport_errors=%d archive_dial=%t)",apiRequests.Load(),apiTransportErrors.Load(),archiveDial.Load())
    }
}

func TestGHAGuardOfficialSourceArchiveTransport(t *testing.T) {
    body:=&ghaGuardPublicTestBody{Reader:bytes.NewReader([]byte("archive"))}
    calls:=0
    rt:=ghaGuardPublicTestRT(func(req *http.Request)(*http.Response,error){calls++;if req.Method!=http.MethodGet||req.URL.String()!=ghaGuardPublicSourceURL||req.Header.Get("Authorization")!=""||req.Header.Get("Cookie")!="" {t.Fatal("unexpected source request")};return &http.Response{StatusCode:200,Header:http.Header{},Body:body,Request:req},nil})
    got,err:=ghaGuardReadPinnedSourceArchive(context.Background(),rt)
    if err!=nil||!bytes.Equal(got,[]byte("archive"))||body.closed!=1||calls!=1 {t.Fatalf("transport failed: %v",err)}
    clear(got)
    ctx,cancel:=context.WithCancel(context.Background());cancel()
    if got,err=ghaGuardReadPinnedSourceArchive(ctx,rt);got!=nil||!errors.Is(err,errGHAGuardUnqualified)||calls!=1 {t.Fatal("cancelled fetch accepted")}
}

func TestGHAGuardPinnedSourceArchiveRejectsDamagedEnd(t *testing.T) {
    records:=ghaGuardSourceTestRecords()
    entries:=[]ghaGuardSourceTestEntry{{name:records[0].Path,body:[]byte("alpha")},{name:records[1].Path,body:[]byte("bravo")}}
    valid:=ghaGuardSourceTestArchive(t,entries,ghaGuardSource,false)
    t.Run("gzip-checksum",func(t *testing.T){
        bad:=append([]byte(nil),valid...);bad[len(bad)-8]^=1
        if ghaGuardValidatePinnedSourceArchive(context.Background(),bad,records)==nil {t.Fatal("damaged gzip checksum accepted")}
    })
    t.Run("missing-tar-end-blocks",func(t *testing.T){
        zr,err:=gzip.NewReader(bytes.NewReader(valid));if err!=nil {t.Fatal(err)}
        plain,readErr:=io.ReadAll(zr);closeErr:=zr.Close();if readErr!=nil||closeErr!=nil||len(plain)<1024 {t.Fatal("fixture read failed")}
        var out bytes.Buffer;zw:=gzip.NewWriter(&out)
        if _,err=zw.Write(plain[:len(plain)-1024]);err!=nil {t.Fatal(err)}
        if err=zw.Close();err!=nil {t.Fatal(err)}
        if ghaGuardValidatePinnedSourceArchive(context.Background(),out.Bytes(),records)==nil {t.Fatal("tar without end blocks accepted")}
    })
}

// The fixed source fixture drives both validators. No issuer is created.
func TestGHAGuardArchiveAndBlobSourceEquivalence(t *testing.T) {
    fixture:=ghaGuardActualTestSourceFixture(t)
    records:=make([]ghaGuardSourceRecord,len(fixture.Inputs))
    entries:=make([]ghaGuardSourceTestEntry,0,len(fixture.Inputs))
    for i,x:=range fixture.Inputs {
        records[i]=ghaGuardSourceRecord{Path:x.Path,Size:x.Size,SHA:x.SHA,Blob:x.Blob}
        body,ok:=fixture.Blobs[x.Blob];if !ok {t.Fatal("fixture blob missing")}
        entries=append(entries,ghaGuardSourceTestEntry{name:x.Path,body:body})
    }
    if len(records)!=249 {t.Fatal("wrong fixture count")}
    archive:=ghaGuardSourceTestArchive(t,entries,ghaGuardSource,false)
    var blobCalls atomic.Int64
    client:=&http.Client{Transport:ghaGuardTreeTestTransport(func(req *http.Request)(*http.Response,error){
        if req.URL.Scheme!="https"||req.URL.Host!="api.github.com" {return nil,errGHAGuardUnqualified}
        key:=strings.TrimPrefix(req.URL.Path,"/repos/AutoDevJapan/cc-pc-tools-rs")
        var body []byte;status:=http.StatusOK
        switch {
        case key=="":body=fixture.RepoResponse
        case key=="/git/commits/"+ghaGuardSource:body=fixture.CommitResponse
        case strings.HasPrefix(key,"/git/trees/"):body=fixture.Trees[strings.TrimPrefix(key,"/git/trees/")]
        case strings.HasPrefix(key,"/git/blobs/"):blobCalls.Add(1);body=fixture.Blobs[strings.TrimPrefix(key,"/git/blobs/")]
        default:status=http.StatusNotFound
        }
        if body==nil {status=http.StatusNotFound}
        return &http.Response{StatusCode:status,Header:http.Header{},Body:io.NopCloser(bytes.NewReader(body)),Request:req},nil
    })}
    if ghaGuardVerifyGitSnapshot(context.Background(),client,ghaGuardSource,ghaGuardSourceTree,records)!=nil || blobCalls.Load()!=249 {t.Fatal("old validator failed fixture")}
    blobCalls.Store(0)
    if ghaGuardVerifyGitTreeSnapshot(context.Background(),client,ghaGuardSource,ghaGuardSourceTree,records)!=nil || blobCalls.Load()!=0 {t.Fatal("tree validation mismatch")}
    if ghaGuardValidatePinnedSourceArchive(context.Background(),archive,records)!=nil {t.Fatal("archive validator rejected same inputs")}
}


func ghaGuardSourceTestPlainTar(t *testing.T, archive []byte) []byte {
    t.Helper();zr,err:=gzip.NewReader(bytes.NewReader(archive));if err!=nil {t.Fatal(err)}
    plain,readErr:=io.ReadAll(zr);closeErr:=zr.Close()
    if readErr!=nil||closeErr!=nil||len(plain)<1024 {t.Fatal("invalid fixture")}
    return plain
}

func ghaGuardSourceTestGzipTar(t *testing.T, plain []byte) []byte {
    t.Helper();var out bytes.Buffer;zw:=gzip.NewWriter(&out)
    if _,err:=zw.Write(plain);err!=nil {t.Fatal(err)}
    if err:=zw.Close();err!=nil {t.Fatal(err)}
    return out.Bytes()
}

// Generate a real PAX or GNU extension and remove the following ordinary file.
func ghaGuardSourceTestSpecialPrefix(t *testing.T, format tar.Format, name string, pax map[string]string, kind byte) []byte {
    t.Helper();var out bytes.Buffer;tw:=tar.NewWriter(&out)
    h:=&tar.Header{Name:name,Typeflag:tar.TypeReg,Mode:0644,Format:format,PAXRecords:pax}
    if err:=tw.WriteHeader(h);err!=nil {t.Fatal(err)}
    if err:=tw.Close();err!=nil {t.Fatal(err)}
    raw:=out.Bytes()
    if len(raw)<512||raw[156]!=kind {t.Fatal("special fixture not generated")}
    size,err:=strconv.ParseInt(strings.Trim(string(raw[124:136])," \x00"),8,64)
    if err!=nil||size<1||size>1<<20 {t.Fatal("invalid special fixture size")}
    end:=512+int((size+511)/512)*512
    if end>len(raw) {t.Fatal("truncated special fixture")}
    return append([]byte(nil),raw[:end]...)
}

func TestGHAGuardPinnedSourceArchiveExactTarEnd(t *testing.T) {
    records:=ghaGuardSourceTestRecords()
    entries:=[]ghaGuardSourceTestEntry{{name:records[0].Path,body:[]byte("alpha")},{name:records[1].Path,body:[]byte("bravo")}}
    plain:=ghaGuardSourceTestPlainTar(t,ghaGuardSourceTestArchive(t,entries,ghaGuardSource,false))
    if !bytes.Equal(plain[len(plain)-1024:],make([]byte,1024)) {t.Fatal("fixture lacks end blocks")}
    prefix:=append([]byte(nil),plain[:len(plain)-1024]...)
    pax:=ghaGuardSourceTestSpecialPrefix(t,tar.FormatPAX,"fixture",map[string]string{"comment":"probe"},tar.TypeXHeader)
    gnu:=ghaGuardSourceTestSpecialPrefix(t,tar.FormatGNU,strings.Repeat("g",1300),nil,tar.TypeGNULongName)
    gnuZeros:=append([]byte(nil),gnu...)
    for i:=512;i<len(gnuZeros);i++ {gnuZeros[i]=0}
    makeEnd:=func(ext []byte, blocks int) []byte {
        raw:=append([]byte(nil),prefix...);raw=append(raw,ext...)
        return append(raw,make([]byte,512*blocks)...)
    }
    cases:=[]struct{name string; raw []byte; valid bool}{
        {"normal",plain,true},
        {"extra-zero-block",append(append([]byte(nil),plain...),make([]byte,512)...),true},
        {"no-end-block",makeEnd(nil,0),false},
        {"one-end-block",makeEnd(nil,1),false},
        {"dangling-pax-no-end",makeEnd(pax,0),false},
        {"dangling-pax-one-end",makeEnd(pax,1),false},
        {"dangling-pax-two-end",makeEnd(pax,2),false},
        {"dangling-gnu-no-end",makeEnd(gnu,0),false},
        {"dangling-gnu-one-end",makeEnd(gnu,1),false},
        {"dangling-gnu-two-end",makeEnd(gnu,2),false},
        {"dangling-gnu-all-zero-body",makeEnd(gnuZeros,0),false},
        {"nonzero-after-end",append(append([]byte(nil),plain...),1),false},
    }
    for _,tc:=range cases {t.Run(tc.name,func(t *testing.T){
        err:=ghaGuardValidatePinnedSourceArchive(context.Background(),ghaGuardSourceTestGzipTar(t,tc.raw),records)
        if (err==nil)!=tc.valid {t.Fatalf("unexpected archive result: %v",err)}
    })}
    // A local PAX header followed by its ordinary selected file remains valid.
    t.Run("attached-pax",func(t *testing.T){
        var out bytes.Buffer;zw:=gzip.NewWriter(&out);tw:=tar.NewWriter(zw)
        if err:=tw.WriteHeader(&tar.Header{Name:"pax_global_header",Typeflag:tar.TypeXGlobalHeader,PAXRecords:map[string]string{"comment":ghaGuardSource}});err!=nil {t.Fatal(err)}
        for i,e:=range entries {
            h:=&tar.Header{Name:ghaGuardSourceArchivePrefix+e.name,Typeflag:tar.TypeReg,Mode:0664,Size:int64(len(e.body))}
            if i==1 {h.Format=tar.FormatPAX;h.PAXRecords=map[string]string{"comment":"probe"}}
            if err:=tw.WriteHeader(h);err!=nil {t.Fatal(err)}
            if _,err:=tw.Write(e.body);err!=nil {t.Fatal(err)}
        }
        if err:=tw.Close();err!=nil {t.Fatal(err)}
        if err:=zw.Close();err!=nil {t.Fatal(err)}
        if err:=ghaGuardValidatePinnedSourceArchive(context.Background(),out.Bytes(),records);err!=nil {t.Fatalf("attached PAX rejected: %v",err)}
    })
}

type ghaGuardSourceTestLastEOF struct { raw []byte }
func (r *ghaGuardSourceTestLastEOF) Read(p []byte) (int,error) {
    if len(r.raw)==0 {return 0,io.EOF}
    n:=copy(p,r.raw);r.raw=r.raw[n:]
    if len(r.raw)==0 {return n,io.EOF}
    return n,nil
}

func TestGHAGuardArchiveReaderTracksEmptyEOF(t *testing.T) {
    r:=&ghaGuardArchiveContextReader{ctx:context.Background(),reader:&ghaGuardSourceTestLastEOF{raw:[]byte("abc")}}
    b:=make([]byte,3);n,err:=r.Read(b)
    if n!=3||err!=io.EOF||r.emptyEOF {t.Fatal("nonempty final read misclassified")}
    n,err=r.Read(b)
    if n!=0||err!=io.EOF||!r.emptyEOF {t.Fatal("empty EOF not observed")}
}
