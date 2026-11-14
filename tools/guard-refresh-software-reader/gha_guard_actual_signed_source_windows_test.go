//go:build windows

package main

import (
    "archive/zip"
    "bytes"
    "compress/gzip"
    "context"
    "crypto/sha256"
    "crypto/x509"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "net"
    "net/http"
    "net/url"
    "os"
    "strings"
    "sync/atomic"
    "testing"
    "time"

    "github.com/sigstore/sigstore-go/pkg/root"
    "github.com/sigstore/sigstore-go/pkg/tuf"
)

var ghaGuardActualWindowClosed = errors.New("guard_actual_window_closed")

const ghaGuardActualEntryReserve = 8*time.Minute + 30*time.Second
const ghaGuardActualPhaseReserve = 2*time.Minute + 30*time.Second
const ghaGuardActualNetworkReserve = 3*time.Minute + 30*time.Second

type ghaGuardActualFailureStage uint8
const (
    ghaGuardActualAcquisition ghaGuardActualFailureStage = iota
    ghaGuardActualVerification
)

// Only an unbroken single-error chain ending in the fixed cause or the
// context deadline is eligible. Joined errors may contain a hard failure.
func ghaGuardActualExpiryCauseOnly(err error) bool {
    for depth:=0;depth<32&&err!=nil;depth++ {
        if err==ghaGuardActualWindowClosed||err==context.DeadlineExceeded {return true}
        err=errors.Unwrap(err)
    }
    return false
}

type ghaGuardActualWindowTrace struct { deadline atomic.Bool; hard atomic.Bool }
func (x *ghaGuardActualWindowTrace) observeError(ctx context.Context,err error) {
    if x==nil||err==nil||err==io.EOF{return}
    if ctx!=nil&&ctx.Err()==context.DeadlineExceeded&&context.Cause(ctx)==ghaGuardActualWindowClosed&&ghaGuardActualExpiryCauseOnly(err) {x.deadline.Store(true);return}
    x.hard.Store(true)
}
func (x *ghaGuardActualWindowTrace) observeStatus(status,expected int) {if x!=nil&&status!=expected{x.hard.Store(true)}}
func ghaGuardActualNotExecuted(stage ghaGuardActualFailureStage,err error,x *ghaGuardActualWindowTrace) bool {
    return stage==ghaGuardActualAcquisition&&err!=nil&&x!=nil&&x.deadline.Load()&&!x.hard.Load()
}
func ghaGuardActualHasWindow(now,expiry time.Time,reserve time.Duration) bool {
    return reserve>0&&now.Before(expiry)&&expiry.Sub(now)>=reserve
}
func ghaGuardActualSkip(t *testing.T,reason string) {t.Helper();t.Skip("GUARD_ACTUAL_NOT_EXECUTED="+reason)}

// Diagnostics contain fixed classifications, counts and SHA-256 values only.
// Response bytes, URLs, credentials and lower-level error text are not printed.
type ghaGuardActualBundleDiagnostic struct {
    stage,linkMarker,context,transport string
    status,linkKeys,linkValues,linkBytes,bodyBytes,entries,matches,bundleBytes int
    linkKnown,linkHasSHA,bundleHasSHA,bundleDigestMatch,readFailed,closeFailed,hard bool
    linkSHA,bundleSHA [32]byte
}
func ghaGuardDiagnosticEnum(value string,allowed ...string) string {
    for _,v:=range allowed {if value==v{return v}}
    return "unknown"
}
func ghaGuardBundleTransportKind(err error) string {
    if err==nil{return "none"}
    if ghaGuardActualExpiryCauseOnly(err){return "deadline"}
    if errors.Is(err,context.Canceled){return "cancelled"}
    var dns *net.DNSError;if errors.As(err,&dns){return "dns"}
    var unknown x509.UnknownAuthorityError;if errors.As(err,&unknown){return "tls_certificate"}
    var hostname x509.HostnameError;if errors.As(err,&hostname){return "tls_certificate"}
    var invalid x509.CertificateInvalidError;if errors.As(err,&invalid){return "tls_certificate"}
    var network net.Error
    if errors.As(err,&network){if network.Timeout(){return "timeout"};return "network"}
    return "other"
}
func (d *ghaGuardActualBundleDiagnostic) noteLink(headers http.Header) {
    if d==nil{return}
    d.linkMarker="no_link"
    var first string
    for name,values:=range headers {
        if !strings.EqualFold(name,"Link") {continue}
        d.linkKeys++
        for _,value:=range values {
            d.linkValues++
            if d.linkValues==1 {first=value;d.linkBytes=len(value)}
        }
    }
    if d.linkKeys==0{return}
    if d.linkKeys!=1||d.linkValues!=1 {d.linkMarker="non_single_field";return}
    if len(first)>8192 {d.linkMarker="oversize";return}
    d.linkSHA=sha256.Sum256([]byte(first));d.linkHasSHA=true
    if ghaGuardSinglePageLink(headers) {d.linkKnown=true;d.linkMarker="known_deprecation";return}
    if strings.Trim(first," \t")=="" {d.linkMarker="empty";return}
    // These are case-insensitive substring markers, not parsed RFC Link relations.
    lower:=strings.ToLower(first)
    markers:=[]struct{marker,name string}{{`rel="next"`,"contains_next_marker"},{`rel="prev"`,"contains_prev_marker"},{`rel="first"`,"contains_first_marker"},{`rel="last"`,"contains_last_marker"}}
    found:=0
    for _,r:=range markers {if strings.Contains(lower,r.marker){d.linkMarker=r.name;found++}}
    if found>1||found>0&&strings.Contains(lower,`rel="deprecation"`){d.linkMarker="contains_multiple_markers";return}
    if found==0 {d.linkMarker="other"}
}
func (d *ghaGuardActualBundleDiagnostic) finish(ctx context.Context,trace *ghaGuardActualWindowTrace) {
    if d==nil{return}
    if trace!=nil {d.hard=trace.hard.Load();if trace.deadline.Load(){d.context="window_deadline";return}}
    if ctx==nil {d.context="unknown";return}
    switch ctx.Err() {
    case nil:d.context="active"
    case context.DeadlineExceeded:d.context="deadline"
    case context.Canceled:d.context="cancelled"
    default:d.context="other"
    }
}
func (d ghaGuardActualBundleDiagnostic) safeString() string {
    stage:=ghaGuardDiagnosticEnum(d.stage,"init","request","transport","status","link","read","close","read_close","bound","context","select_json","select_match","select_compact","pinned_length","pinned_digest","ok")
    marker:=ghaGuardDiagnosticEnum(d.linkMarker,"unobserved","no_link","known_deprecation","contains_next_marker","contains_prev_marker","contains_first_marker","contains_last_marker","contains_multiple_markers","non_single_field","empty","other","oversize")
    ctx:=ghaGuardDiagnosticEnum(d.context,"unknown","active","window_deadline","deadline","cancelled","other")
    transport:=ghaGuardDiagnosticEnum(d.transport,"none","deadline","cancelled","dns","tls_certificate","timeout","network","other")
    linkSHA,bundleSHA:="-","-"
    if d.linkHasSHA {linkSHA=fmt.Sprintf("%x",d.linkSHA)}
    if d.bundleHasSHA {bundleSHA=fmt.Sprintf("%x",d.bundleSHA)}
    return fmt.Sprintf("stage=%s status=%d link_keys=%d link_values=%d link_known=%t link_marker=%s link_first_bytes=%d link_sha256=%s body_bytes=%d entries=%d matches=%d bundle_bytes=%d bundle_sha256=%s bundle_digest_match=%t context=%s transport=%s hard=%t read_error=%t close_error=%t",stage,d.status,d.linkKeys,d.linkValues,d.linkKnown,marker,d.linkBytes,linkSHA,d.bodyBytes,d.entries,d.matches,d.bundleBytes,bundleSHA,d.bundleDigestMatch,ctx,transport,d.hard,d.readFailed,d.closeFailed)
}

// Only a network operation cancelled by the fixed evidence-window deadline
// can be classified as not executed. Other HTTP, read and close failures win.
type ghaGuardActualTrackedBody struct { io.ReadCloser; ctx context.Context; cancel context.CancelFunc; trace *ghaGuardActualWindowTrace }
func (b *ghaGuardActualTrackedBody) Read(p []byte)(int,error) {
    n,err:=b.ReadCloser.Read(p);b.trace.observeError(b.ctx,err);return n,err
}
func (b *ghaGuardActualTrackedBody) Close() error {
    err:=b.ReadCloser.Close();if err!=nil{b.trace.hard.Store(true)};b.cancel();return err
}
func ghaGuardActualTimedRoundTrip(r *http.Request,base http.RoundTripper,cutoff time.Time,trace *ghaGuardActualWindowTrace) (*http.Response,error) {
    if r==nil||base==nil||trace==nil||cutoff.IsZero(){return nil,errGHAGuardUnqualified}
    ctx,cancel:=context.WithDeadlineCause(r.Context(),cutoff,ghaGuardActualWindowClosed)
    next:=r.Clone(ctx)
    response,err:=base.RoundTrip(next)
    if err!=nil {
        trace.observeError(ctx,err)
        if response!=nil&&response.Body!=nil {if response.Body.Close()!=nil{trace.hard.Store(true)}}
        cancel();return nil,err
    }
    if response==nil||response.Body==nil {trace.hard.Store(true);cancel();return nil,errGHAGuardUnqualified}
    response.Body=&ghaGuardActualTrackedBody{ReadCloser:response.Body,ctx:ctx,cancel:cancel,trace:trace}
    return response,nil
}

type ghaGuardActualTestRT func(*http.Request)(*http.Response,error)
func (f ghaGuardActualTestRT) RoundTrip(r *http.Request)(*http.Response,error){return f(r)}

// This is a hosted-test transport only. The job token is never persisted or
// printed, and is attached exclusively to fixed GitHub Actions API requests.
type ghaGuardActualTestAuth struct { base http.RoundTripper; token string; stage string; status int; cutoff time.Time; trace *ghaGuardActualWindowTrace }
func (a *ghaGuardActualTestAuth) RoundTrip(r *http.Request)(*http.Response,error) {
    if a==nil||a.base==nil||a.token==""||r==nil||r.URL==nil||r.URL.Scheme!="https"||r.URL.Host!="api.github.com"||!strings.HasPrefix(r.URL.Path,"/repos/AutoDevJapan/cc-pc-tools-rs/actions/") {
        if a!=nil&&a.trace!=nil {a.trace.hard.Store(true)}
        return nil,errGHAGuardUnqualified
    }
    a.stage="actions-other"
    switch {
    case strings.Contains(r.URL.Path,"/actions/workflows/"):a.stage="workflow"
    case strings.HasSuffix(r.URL.Path,"/logs"):a.stage="job-log"
    case strings.HasSuffix(r.URL.Path,"/zip"):a.stage="artifact-zip"
    case strings.HasSuffix(r.URL.Path,"/jobs"):a.stage="jobs"
    case strings.HasSuffix(r.URL.Path,"/artifacts"):a.stage="artifacts"
    case strings.Contains(r.URL.Path,"/actions/runs/"):a.stage="run"
    }
    next:=r.Clone(r.Context());next.Header=r.Header.Clone();next.Header.Set("Authorization","Bearer "+a.token)
    response,err:=ghaGuardActualTimedRoundTrip(next,a.base,a.cutoff,a.trace)
    a.status=0
    if response!=nil {
        a.status=response.StatusCode
        expected:=http.StatusOK
        if a.stage=="job-log"||a.stage=="artifact-zip" {expected=http.StatusFound}
        a.trace.observeStatus(a.status,expected)
    }
    return response,err
}

// The object transport observes only a status code and never carries a token.
type ghaGuardActualTestObject struct { base http.RoundTripper; status int; cutoff time.Time; trace *ghaGuardActualWindowTrace }
func (o *ghaGuardActualTestObject) RoundTrip(r *http.Request)(*http.Response,error) {
    if o==nil||o.base==nil||r==nil||r.Header.Get("Authorization")!=""||r.Header.Get("Cookie")!="" {
        if o!=nil&&o.trace!=nil {o.trace.hard.Store(true)}
        return nil,errGHAGuardUnqualified
    }
    response,err:=ghaGuardActualTimedRoundTrip(r,o.base,o.cutoff,o.trace)
    o.status=0;if response!=nil {o.status=response.StatusCode;o.trace.observeStatus(o.status,http.StatusOK)}
    return response,err
}

// Read the public API response without authentication. The signed URL is used
// only to check the server-provided identifier; it is never followed or logged.
func ghaGuardActualTestBundle(ctx context.Context,cutoff time.Time,trace *ghaGuardActualWindowTrace,diag *ghaGuardActualBundleDiagnostic) ([]byte,error) {
    transport:=&http.Transport{Proxy:nil};defer transport.CloseIdleConnections()
    return ghaGuardActualTestBundleWithTransport(ctx,cutoff,trace,transport,diag)
}

// The test-only seam injects only the RoundTripper. The URL, headers, checks,
// response bounds, digest and caller-owned result remain fixed below.
func ghaGuardActualTestBundleWithTransport(ctx context.Context,cutoff time.Time,trace *ghaGuardActualWindowTrace,transport http.RoundTripper,diag *ghaGuardActualBundleDiagnostic) ([]byte,error) {
    if diag==nil {diag=&ghaGuardActualBundleDiagnostic{}}
    *diag=ghaGuardActualBundleDiagnostic{stage:"request",linkMarker:"unobserved",context:"unknown",transport:"none",bodyBytes:-1,entries:-1,matches:-1,bundleBytes:-1}
    defer diag.finish(ctx,trace)
    if ctx==nil||trace==nil||transport==nil{return nil,errGHAGuardUnqualified}
    endpoint:="https://api.github.com/repos/AutoDevJapan/cc-pc-tools-rs/attestations/sha256:a61e25065ae35d51f1ac8133959176b21e2c3740c1516b76f799371f1c27f9a2?per_page=100"
    req,err:=http.NewRequestWithContext(ctx,http.MethodGet,endpoint,nil);if err!=nil{return nil,errGHAGuardUnqualified}
    req.Header.Set("Accept","application/vnd.github+json");req.Header.Set("X-GitHub-Api-Version","2022-11-28")
    guarded:=ghaGuardActualTestRT(func(r *http.Request)(*http.Response,error){
        if r==nil||r.URL==nil||r.URL.Scheme!="https"||r.URL.Host!="api.github.com"||r.URL.Path!="/repos/AutoDevJapan/cc-pc-tools-rs/attestations/sha256:a61e25065ae35d51f1ac8133959176b21e2c3740c1516b76f799371f1c27f9a2"||r.Header.Get("Authorization")!=""||r.Header.Get("Cookie")!="" {
            if trace!=nil {trace.hard.Store(true)};return nil,errGHAGuardUnqualified
        }
        response,err:=ghaGuardActualTimedRoundTrip(r,transport,cutoff,trace)
        if response!=nil {trace.observeStatus(response.StatusCode,http.StatusOK)}
        return response,err
    })
    client:=&http.Client{Transport:guarded,Timeout:30*time.Second,CheckRedirect:func(*http.Request,[]*http.Request)error{return http.ErrUseLastResponse}}
    diag.stage="transport"
    response,err:=client.Do(req)
    diag.transport=ghaGuardBundleTransportKind(err)
    if response!=nil {
        if response.StatusCode>=100&&response.StatusCode<=599 {diag.status=response.StatusCode}
        diag.noteLink(response.Header)
    }
    if err!=nil||response==nil||response.Body==nil{return nil,errGHAGuardUnqualified}
    if response.StatusCode!=http.StatusOK {
        diag.stage="status";diag.closeFailed=response.Body.Close()!=nil;return nil,errGHAGuardUnqualified
    }
    if !ghaGuardSinglePageLink(response.Header) {
        diag.stage="link";diag.closeFailed=response.Body.Close()!=nil;return nil,errGHAGuardUnqualified
    }
    raw,readErr:=io.ReadAll(io.LimitReader(response.Body,4<<20+1));closeErr:=response.Body.Close()
    diag.bodyBytes=len(raw);diag.readFailed=readErr!=nil;diag.closeFailed=closeErr!=nil
    if readErr!=nil||closeErr!=nil {
        switch {case readErr!=nil&&closeErr!=nil:diag.stage="read_close";case readErr!=nil:diag.stage="read";default:diag.stage="close"}
        clear(raw);return nil,errGHAGuardUnqualified
    }
    if len(raw)>4<<20 {diag.stage="bound";clear(raw);return nil,errGHAGuardUnqualified}
    if ctx.Err()!=nil {diag.stage="context";clear(raw);return nil,errGHAGuardUnqualified}
    defer clear(raw)
    bundle,selectErr:=ghaGuardActualSelectBundleWithDiagnostic(raw,diag)
    if selectErr!=nil{return nil,errGHAGuardUnqualified}
    diag.bundleBytes=len(bundle);diag.bundleSHA=sha256.Sum256(bundle);diag.bundleHasSHA=true
    diag.bundleDigestMatch=digest(bundle)=="c7c63a9adc5d342674e61116f732f4fd6dd1b82a8a039228ce80e9ff5fee85e1"
    if len(bundle)!=11619 {diag.stage="pinned_length";clear(bundle);return nil,errGHAGuardUnqualified}
    if !diag.bundleDigestMatch {diag.stage="pinned_digest";clear(bundle);return nil,errGHAGuardUnqualified}
    diag.stage="ok"
    return bundle,nil
}

// A single matching public attestation is data; the caller still checks its
// pinned bytes and verifies its real signature before issuing a candidate.
func ghaGuardActualSelectBundle(raw []byte) ([]byte,error) {
    return ghaGuardActualSelectBundleWithDiagnostic(raw,nil)
}
func ghaGuardActualSelectBundleWithDiagnostic(raw []byte,diag *ghaGuardActualBundleDiagnostic) ([]byte,error) {
    if diag!=nil {diag.stage="select_json"}
    var envelope struct {Attestations []struct{RepositoryID int64 `json:"repository_id"`;BundleURL string `json:"bundle_url"`;Bundle json.RawMessage `json:"bundle"`} `json:"attestations"`}
    dec:=json.NewDecoder(bytes.NewReader(raw));if dec.Decode(&envelope)!=nil||dec.Decode(new(any))!=io.EOF{return nil,errGHAGuardUnqualified}
    if diag!=nil {diag.entries=len(envelope.Attestations)}
    var found json.RawMessage;matches:=0
    for _,item:=range envelope.Attestations {
        u,err:=url.Parse(item.BundleURL)
        if err!=nil||u.Scheme!="https"||item.RepositoryID!=1406818713||!strings.HasSuffix(u.Path,"/67491128.json.sn") {continue}
        found=item.Bundle;matches++
    }
    if diag!=nil {diag.matches=matches;diag.stage="select_match"}
    if matches!=1||len(found)==0{return nil,errGHAGuardUnqualified}
    if diag!=nil {diag.stage="select_compact"}
    var compact bytes.Buffer
    if json.Compact(&compact,found)!=nil{return nil,errGHAGuardUnqualified}
    bundle:=append([]byte(nil),compact.Bytes()...)
    return bundle,nil
}

func ghaGuardActualTestSourceFixture(t *testing.T) ghaGuardFixedSourceFixture {
    t.Helper()
    var fixture ghaGuardFixedSourceFixture
    file,err:=os.Open("testdata/guard-source-80c4.json.gz");if err!=nil{t.Fatal("fixed source fixture unavailable")}
    compressed,readErr:=io.ReadAll(io.LimitReader(file,973011));closeErr:=file.Close()
    if readErr!=nil||closeErr!=nil||len(compressed)!=973010||digest(compressed)!="f9c2c9550eb5c7c8f93e76d62a755521b5c9074d87b741e71b880f565a013482" {t.Fatal("fixed source fixture mismatch")}
    zr,err:=gzip.NewReader(bytes.NewReader(compressed));if err!=nil{t.Fatal("invalid source fixture")}
    raw,readErr:=io.ReadAll(io.LimitReader(zr,4<<20+1));closeErr=zr.Close()
    if readErr!=nil||closeErr!=nil||len(raw)>4<<20 {t.Fatal("invalid source fixture")}
    dec:=json.NewDecoder(bytes.NewReader(raw));if dec.Decode(&fixture)!=nil||dec.Decode(new(any))!=io.EOF {t.Fatal("invalid source fixture")}
    if fixture.Repository.ID!=1406818713||fixture.Repository.FullName!="AutoDevJapan/cc-pc-tools-rs"||fixture.Source!=ghaGuardSource||fixture.Tree!=ghaGuardSourceTree||len(fixture.Trees)!=10||len(fixture.Blobs)!=248||len(fixture.Inputs)!=249 {t.Fatal("source fixture generation mismatch")}
    return fixture
}

// Construct a negative archive with unchanged manifest and a changed proof.
// Its revised ZIP digest is bound to a test-only publication record.
func ghaGuardActualTestMutateProof(archive []byte) ([]byte,error) {
    reader,err:=zip.NewReader(bytes.NewReader(archive),int64(len(archive)));if err!=nil||len(reader.File)!=3{return nil,errGHAGuardUnqualified}
    var output bytes.Buffer;writer:=zip.NewWriter(&output);seen:=map[string]bool{}
    for _,item:=range reader.File {
        if item==nil||seen[item.Name]||(item.Name!="capture.json"&&item.Name!="source-proof.json"&&item.Name!="guard.exe")||item.UncompressedSize64>ghaGuardPayloadLimit{return nil,errGHAGuardUnqualified}
        seen[item.Name]=true
        input,err:=item.Open();if err!=nil{return nil,errGHAGuardUnqualified}
        raw,readErr:=io.ReadAll(io.LimitReader(input,ghaGuardPayloadLimit+1));closeErr:=input.Close()
        if readErr!=nil||closeErr!=nil||uint64(len(raw))!=item.UncompressedSize64 {clear(raw);return nil,errGHAGuardUnqualified}
        if item.Name=="source-proof.json" {if len(raw)==0{return nil,errGHAGuardUnqualified};raw[len(raw)/2]^=1}
        entry,err:=writer.Create(item.Name);if err!=nil{clear(raw);return nil,errGHAGuardUnqualified}
        if _,err=entry.Write(raw);err!=nil{clear(raw);return nil,errGHAGuardUnqualified};clear(raw)
    }
    if !seen["source-proof.json"]||writer.Close()!=nil{return nil,errGHAGuardUnqualified}
    return output.Bytes(),nil
}

// These controls exercise the deadline classification without any network.
func TestGHAGuardActualWindowDecision(t *testing.T) {
    expiry:=time.Date(2026,time.November,15,8,9,29,0,time.UTC)
    cases:=[]struct{name string;now time.Time;reserve time.Duration;want bool}{
        {"workflow-entry",expiry.Add(-10*time.Minute),ghaGuardActualEntryReserve,true},
        {"test-entry-exact",expiry.Add(-ghaGuardActualEntryReserve),ghaGuardActualEntryReserve,true},
        {"compile-delay",expiry.Add(-ghaGuardActualEntryReserve+time.Nanosecond),ghaGuardActualEntryReserve,false},
        {"expired",expiry,ghaGuardActualEntryReserve,false},
        {"invalid-reserve",expiry.Add(-time.Minute),0,false},
    }
    for _,tc:=range cases {t.Run(tc.name,func(t *testing.T){
        if got:=ghaGuardActualHasWindow(tc.now,expiry,tc.reserve);got!=tc.want {t.Fatalf("window got %v; want %v",got,tc.want)}
    })}
}

type ghaGuardActualTestReadCloser struct { read func([]byte)(int,error); close func()error }
func (b *ghaGuardActualTestReadCloser) Read(p []byte)(int,error) {return b.read(p)}
func (b *ghaGuardActualTestReadCloser) Close() error {return b.close()}

func ghaGuardActualTestMarkDeadline(trace *ghaGuardActualWindowTrace) {
    ctx,cancel:=context.WithDeadlineCause(context.Background(),time.Now().Add(-time.Second),ghaGuardActualWindowClosed)
    defer cancel();trace.observeError(ctx,ctx.Err())
}

func TestGHAGuardActualDeadlineClassification(t *testing.T) {
    t.Run("deadline-during-body-read",func(t *testing.T){
        trace:=&ghaGuardActualWindowTrace{}
        request,err:=http.NewRequestWithContext(context.Background(),http.MethodGet,"https://api.github.com/",nil);if err!=nil {t.Fatal(err)}
        transport:=ghaGuardActualTestRT(func(r *http.Request)(*http.Response,error){
            body:=&ghaGuardActualTestReadCloser{read:func([]byte)(int,error){<-r.Context().Done();return 0,r.Context().Err()},close:func()error{return nil}}
            return &http.Response{StatusCode:http.StatusOK,Header:make(http.Header),Body:body,Request:r},nil
        })
        response,err:=ghaGuardActualTimedRoundTrip(request,transport,time.Now().Add(30*time.Millisecond),trace)
        if err!=nil||response==nil {t.Fatal("response setup failed")}
        _,readErr:=io.ReadAll(response.Body);closeErr:=response.Body.Close()
        if readErr==nil||closeErr!=nil||!ghaGuardActualNotExecuted(ghaGuardActualAcquisition,readErr,trace) {t.Fatal("causal deadline must be not executed")}
    })
    for _,wrapped:=range []bool{false,true} {
        name:="deadline-during-roundtrip-cause";if wrapped {name="deadline-during-roundtrip-wrapped-cause"}
        t.Run(name,func(t *testing.T){
            trace:=&ghaGuardActualWindowTrace{}
            request,err:=http.NewRequestWithContext(context.Background(),http.MethodGet,"https://api.github.com/",nil);if err!=nil {t.Fatal(err)}
            transport:=ghaGuardActualTestRT(func(r *http.Request)(*http.Response,error){
                <-r.Context().Done();cause:=context.Cause(r.Context())
                if wrapped {return nil,&url.Error{Op:"Get",URL:"https://api.github.com/",Err:cause}}
                return nil,cause
            })
            response,err:=ghaGuardActualTimedRoundTrip(request,transport,time.Now().Add(30*time.Millisecond),trace)
            if response!=nil||err==nil||!errors.Is(err,ghaGuardActualWindowClosed)||!ghaGuardActualNotExecuted(ghaGuardActualAcquisition,err,trace) {t.Fatal("fixed deadline cause must be not executed")}
        })
    }
    t.Run("deadline-during-body-read-cause",func(t *testing.T){
        trace:=&ghaGuardActualWindowTrace{}
        request,err:=http.NewRequestWithContext(context.Background(),http.MethodGet,"https://api.github.com/",nil);if err!=nil {t.Fatal(err)}
        transport:=ghaGuardActualTestRT(func(r *http.Request)(*http.Response,error){
            body:=&ghaGuardActualTestReadCloser{read:func([]byte)(int,error){<-r.Context().Done();return 0,context.Cause(r.Context())},close:func()error{return nil}}
            return &http.Response{StatusCode:http.StatusOK,Header:make(http.Header),Body:body,Request:r},nil
        })
        response,err:=ghaGuardActualTimedRoundTrip(request,transport,time.Now().Add(30*time.Millisecond),trace)
        if err!=nil||response==nil {t.Fatal("response setup failed")}
        _,readErr:=io.ReadAll(response.Body);closeErr:=response.Body.Close()
        if readErr==nil||closeErr!=nil||!errors.Is(readErr,ghaGuardActualWindowClosed)||!ghaGuardActualNotExecuted(ghaGuardActualAcquisition,readErr,trace) {t.Fatal("body deadline cause must be not executed")}
    })
    for _,fault:=range []string{"joined-cause","joined-deadline","joined-eof"} {t.Run(fault,func(t *testing.T){
        ctx,cancel:=context.WithDeadlineCause(context.Background(),time.Now().Add(-time.Second),ghaGuardActualWindowClosed);defer cancel()
        trace:=&ghaGuardActualWindowTrace{}
        hard:=errors.New("independent read failure")
        mixed:=errors.Join(ghaGuardActualWindowClosed,hard)
        if fault=="joined-deadline" {mixed=errors.Join(context.DeadlineExceeded,hard)}
        if fault=="joined-eof" {mixed=errors.Join(io.EOF,hard)}
        trace.observeError(ctx,mixed)
        if !trace.hard.Load()||ghaGuardActualNotExecuted(ghaGuardActualAcquisition,mixed,trace) {t.Fatal("combined hard failure was masked")}
    })}
    t.Run("different-deadline-cause",func(t *testing.T){
        ctx,cancel:=context.WithDeadlineCause(context.Background(),time.Now().Add(-time.Second),errors.New("other deadline"));defer cancel()
        trace:=&ghaGuardActualWindowTrace{};trace.observeError(ctx,ghaGuardActualWindowClosed)
        if !trace.hard.Load()||ghaGuardActualNotExecuted(ghaGuardActualAcquisition,ghaGuardActualWindowClosed,trace) {t.Fatal("foreign deadline was masked")}
    })
    for _,status:=range []int{http.StatusUnauthorized,http.StatusForbidden} {t.Run(http.StatusText(status),func(t *testing.T){
        trace:=&ghaGuardActualWindowTrace{}
        request,err:=http.NewRequestWithContext(context.Background(),http.MethodGet,"https://api.github.com/repos/AutoDevJapan/cc-pc-tools-rs/actions/runs/1",nil);if err!=nil {t.Fatal(err)}
        base:=ghaGuardActualTestRT(func(r *http.Request)(*http.Response,error){return &http.Response{StatusCode:status,Header:make(http.Header),Body:io.NopCloser(bytes.NewReader(nil)),Request:r},nil})
        auth:=&ghaGuardActualTestAuth{base:base,token:"synthetic",cutoff:time.Now().Add(time.Minute),trace:trace}
        response,err:=auth.RoundTrip(request);if err!=nil||response==nil {t.Fatal("status setup failed")}
        if err=response.Body.Close();err!=nil {t.Fatal("status cleanup failed")}
        ghaGuardActualTestMarkDeadline(trace)
        if !trace.hard.Load()||!trace.deadline.Load()||ghaGuardActualNotExecuted(ghaGuardActualAcquisition,errGHAGuardUnqualified,trace) {t.Fatal("HTTP error was masked by deadline")}
    })}
    for _,fault:=range []string{"read","close"} {t.Run(fault,func(t *testing.T){
        trace:=&ghaGuardActualWindowTrace{}
        request,err:=http.NewRequestWithContext(context.Background(),http.MethodGet,"https://api.github.com/",nil);if err!=nil {t.Fatal(err)}
        transport:=ghaGuardActualTestRT(func(r *http.Request)(*http.Response,error){
            body:=&ghaGuardActualTestReadCloser{read:func([]byte)(int,error){if fault=="read" {return 0,errors.New("read failed")};return 0,io.EOF},close:func()error{if fault=="close" {return errors.New("close failed")};return nil}}
            return &http.Response{StatusCode:http.StatusOK,Header:make(http.Header),Body:body,Request:r},nil
        })
        response,err:=ghaGuardActualTimedRoundTrip(request,transport,time.Now().Add(time.Minute),trace);if err!=nil||response==nil {t.Fatal("body setup failed")}
        _,readErr:=io.ReadAll(response.Body);closeErr:=response.Body.Close()
        if fault=="read"&&readErr==nil||fault=="close"&&closeErr==nil {t.Fatal("hard failure not exercised")}
        ghaGuardActualTestMarkDeadline(trace)
        if !trace.hard.Load()||!trace.deadline.Load()||ghaGuardActualNotExecuted(ghaGuardActualAcquisition,errGHAGuardUnqualified,trace) {t.Fatal("read or close error was masked by deadline")}
    })}
    t.Run("unrelated-cancellation-stays-failure",func(t *testing.T){
        ctx,cancel:=context.WithCancel(context.Background());cancel()
        trace:=&ghaGuardActualWindowTrace{};trace.observeError(ctx,ctx.Err());ghaGuardActualTestMarkDeadline(trace)
        if !trace.hard.Load()||ghaGuardActualNotExecuted(ghaGuardActualAcquisition,errGHAGuardUnqualified,trace) {t.Fatal("unrelated cancellation was masked")}
    })
    t.Run("signature-failure-is-not-deadline-skip",func(t *testing.T){
        trace:=&ghaGuardActualWindowTrace{};ghaGuardActualTestMarkDeadline(trace)
        if !trace.deadline.Load()||ghaGuardActualNotExecuted(ghaGuardActualVerification,errors.New("signature invalid"),trace) {t.Fatal("signature failure was masked by deadline")}
    })
}

// The opt-in hosted acceptance uses live authenticated Actions metadata/log/ZIP
// and the public signed bundle. The Git tree transport is a fixed public-source
// fixture and the test authority is not a normal installed-owner capability.
func TestGHAGuardActualSignedSourceIntegration(t *testing.T) {
    if os.Getenv("GUARD_GHA_ACTUAL_SIGNED_SOURCE")!="1" {t.Skip("actual signed-source acceptance not requested")}
    expiry:=time.Date(2026,time.November,15,8,9,29,0,time.UTC)
    if !ghaGuardActualHasWindow(time.Now().UTC(),expiry,ghaGuardActualEntryReserve) {ghaGuardActualSkip(t,"entry_window")}
    token:=os.Getenv("GH_TOKEN");if token=="" {t.Fatal("existing Actions read token unavailable")}
    ctx,cancel:=context.WithTimeout(context.Background(),6*time.Minute);defer cancel()
    trace:=&ghaGuardActualWindowTrace{}
    cutoff:=expiry.Add(-ghaGuardActualNetworkReserve)
    var bundleDiag ghaGuardActualBundleDiagnostic
    bundle,err:=ghaGuardActualTestBundle(ctx,cutoff,trace,&bundleDiag)
    if err!=nil {
        if ghaGuardActualNotExecuted(ghaGuardActualAcquisition,err,trace) {ghaGuardActualSkip(t,"acquisition_deadline")}
        t.Fatalf("fixed public bundle acquisition failed: %s",bundleDiag.safeString())
    }
    defer clear(bundle)
    apiTransport:=&http.Transport{Proxy:nil};defer apiTransport.CloseIdleConnections()
    objectTransport:=&http.Transport{Proxy:nil};defer objectTransport.CloseIdleConnections()
    apiTrace:=&ghaGuardActualTestAuth{base:apiTransport,token:token,cutoff:cutoff,trace:trace}
    objectTrace:=&ghaGuardActualTestObject{base:objectTransport,cutoff:cutoff,trace:trace}
    api:=&http.Client{Transport:apiTrace,Timeout:30*time.Second}
    object:=&http.Client{Transport:objectTrace,Timeout:30*time.Second}
    authority:=&ghaGuardEvidenceAuthority{client:api}
    expectation:=ghaGuardActualExpectation()
    material,err:=ghaGuardLiveAcquireWith(ctx,authority,object,expectation,bundle,ghaGuardFixedAttestationID)
    if err!=nil||material==nil {
        if ghaGuardActualNotExecuted(ghaGuardActualAcquisition,err,trace) {ghaGuardActualSkip(t,"acquisition_deadline")}
        t.Fatalf("fixed acquisition failed: api_stage=%s api_status=%d object_status=%d",apiTrace.stage,apiTrace.status,objectTrace.status)
    }
    defer clear(material.archive);defer clear(material.bundle);defer clear(material.jobLog)
    if len(material.archive)!=14597045||digest(material.archive)!="681ae0e1356755a752addf0776ffdab48432fcb850f243d3989e7466e3076452"||len(material.jobLog)!=202632||digest(material.jobLog)!="9fb22330e10cb0586bd64af9ef8bea21ac613670ef1efc6fa0dbdfe5f32b2e19"||digest(material.bundle)!="c7c63a9adc5d342674e61116f732f4fd6dd1b82a8a039228ce80e9ff5fee85e1"||material.manifest.SourceManifestSHA!="aeeefe291c6a10a0f8d1bf1ee1a0952daf29dba87046596980c7c509c690f8c8" {t.Fatal("acquired bytes do not match fixed evidence")}
    fixture:=ghaGuardActualTestSourceFixture(t)
    if !ghaGuardActualHasWindow(time.Now().UTC(),expiry,ghaGuardActualPhaseReserve) {ghaGuardActualSkip(t,"phase_window")}
    rootCtx,rootCancel:=context.WithTimeout(ctx,2*time.Minute)
    opts:=tuf.DefaultOptions().WithDisableLocalCache().WithContext(rootCtx).WithFetcher(ghaGuardNewTUFRequestFetcher(rootCtx,&http.Client{}))
    trusted,err:=root.FetchTrustedRootWithOptions(opts);rootCancel()
    if err!=nil||trusted==nil {t.Fatal("trusted root unavailable")}
    for _,fault:=range []string{"none","blob-mutated","blob-missing","bundle-mutated","archive-mutated","proof-mutated"} {
        if !ghaGuardActualHasWindow(time.Now().UTC(),expiry,ghaGuardActualPhaseReserve) {ghaGuardActualSkip(t,"phase_window")}
        windowInterrupted:=false
        ok:=t.Run(fault,func(t *testing.T){
            current:=*material
            current.archive=append([]byte(nil),material.archive...);current.bundle=append([]byte(nil),material.bundle...);current.jobLog=append([]byte(nil),material.jobLog...)
            defer func(){clear(current.archive);clear(current.bundle);clear(current.jobLog)}()
            if fault=="bundle-mutated" {
                marker:=[]byte("\"sig\":\"");index:=bytes.Index(current.bundle,marker)
                if index<0||index+len(marker)>=len(current.bundle) {t.Fatal("signature field missing")}
                index+=len(marker);if current.bundle[index]=='A'{current.bundle[index]='B'}else{current.bundle[index]='A'}
            }
            if fault=="archive-mutated" {current.archive[len(current.archive)/2]^=1}
            if fault=="proof-mutated" {
                modified,err:=ghaGuardActualTestMutateProof(current.archive);if err!=nil {t.Fatal("proof mutation setup failed")}
                clear(current.archive);current.archive=modified
                publication,err:=ghaGuardPublicationFromLog(current.jobLog);if err!=nil {t.Fatal("publication setup failed")}
                publication.ArchiveSHA=digest(modified);current.artifact.Size=int64(len(modified));current.artifact.Digest="sha256:"+publication.ArchiveSHA
                encoded,err:=json.Marshal(publication);if err!=nil {t.Fatal("publication encoding failed")}
                clear(current.jobLog);current.jobLog=append([]byte("2026-01-02T03:04:05.000Z guard_gha_publication_v1="),encoded...)
            }
            if !ghaGuardActualHasWindow(time.Now().UTC(),expiry,ghaGuardActualPhaseReserve) {
                windowInterrupted=true;t.Skip("GUARD_ACTUAL_NOT_EXECUTED=phase_window")
            }
            var blobCalls,targetCalls atomic.Int64
            sourceClient:=&http.Client{Transport:ghaGuardActualTestRT(func(req *http.Request)(*http.Response,error){
                if req.URL.Scheme!="https"||req.URL.Host!="api.github.com" {return nil,errGHAGuardUnqualified}
                key:=strings.TrimPrefix(req.URL.Path,"/repos/AutoDevJapan/cc-pc-tools-rs")
                var body []byte;status:=http.StatusOK
                switch {
                case key=="":body=fixture.RepoResponse
                case key=="/git/commits/"+ghaGuardSource:body=fixture.CommitResponse
                case strings.HasPrefix(key,"/git/trees/"):body=fixture.Trees[strings.TrimPrefix(key,"/git/trees/")];if body==nil{status=http.StatusNotFound}
                case strings.HasPrefix(key,"/git/blobs/"):
                    blobCalls.Add(1);id:=strings.TrimPrefix(key,"/git/blobs/");body=fixture.Blobs[id]
                    if id==fixture.Inputs[0].Blob {
                        targetCalls.Add(1)
                        if fault=="blob-missing"{status=http.StatusNotFound}
                        if fault=="blob-mutated"{body=append([]byte(nil),body...);body[0]^=1}
                    }
                    if body==nil{status=http.StatusNotFound}
                default:status=http.StatusNotFound
                }
                return &http.Response{StatusCode:status,Header:make(http.Header),Body:io.NopCloser(bytes.NewReader(body)),Request:req},nil
            })}
            sourceCalls,signatureCalls:=0,0
            signatureControlPassed:=false
            sourceInterrupted,signatureInterrupted:=false,false
            sourceCheck:=func(ctx context.Context,held []byte,m ghaGuardManifest)error {
                sourceCalls++
                if len(held)==0||len(current.archive)==0||&held[0]!=&current.archive[0] {return errGHAGuardUnqualified}
                err:=ghaGuardVerifyArchiveGitSourceWithClient(ctx,held,m,sourceClient)
                if ctx.Err()!=nil {sourceInterrupted=true}
                return err
            }
            signatureCheck:=func(ctx context.Context,signed,payload []byte,m ghaGuardManifest)error {
                signatureCalls++
                defer func(){if ctx.Err()!=nil {signatureInterrupted=true}}()
                if fault=="bundle-mutated" {
                    if ghaGuardVerifySignatureWithTrusted(ctx,trusted,bundle,payload,m)!=nil{return errGHAGuardUnqualified}
                    signatureControlPassed=true
                    return ghaGuardVerifySignatureWithTrusted(ctx,trusted,signed,payload,m)
                }
                return ghaGuardVerifyActualSignature(ctx,signed,payload,m)
            }
            // The outer case deadline precedes the constructor's internal
            // two-minute deadline, so timeout cannot satisfy a negative case.
            caseCtx,caseCancel:=context.WithTimeout(ctx,110*time.Second)
            candidate,err:=ghaGuardConstructVerifiedCandidateWith(caseCtx,expectation,&current,signatureCheck,sourceCheck)
            caseErr:=caseCtx.Err();caseCancel()
            if caseErr!=nil||sourceInterrupted||signatureInterrupted {
                if candidate!=nil&&candidate.Close()!=nil {t.Error("interrupted candidate cleanup failed")}
                t.Fatal("verification was interrupted")
            }
            if fault!="none" {
                if candidate!=nil {closeErr:=candidate.Close();if closeErr!=nil {t.Error("unexpected candidate cleanup failed")};t.Fatal("invalid evidence issued a candidate")}
                if err==nil {t.Fatal("invalid evidence was accepted")}
                if (fault=="blob-mutated"||fault=="blob-missing")&&(sourceCalls!=1||targetCalls.Load()==0||signatureCalls!=0) {t.Fatal("source rejection ordering not observed")}
                if fault=="bundle-mutated"&&(sourceCalls!=1||blobCalls.Load()!=249||signatureCalls!=1||!signatureControlPassed) {t.Fatal("signature rejection ordering not observed")}
                if (fault=="archive-mutated"||fault=="proof-mutated")&&(sourceCalls!=0||signatureCalls!=0) {t.Fatal("archive or proof mutation reached later verification")}
                return
            }
            if err!=nil||candidate==nil||sourceCalls!=1||blobCalls.Load()!=249||signatureCalls!=1||!candidate.VerifySourceGeneration() {
                if candidate!=nil&&candidate.Close()!=nil {t.Error("partial candidate cleanup failed")}
                t.Fatal("actual signed source integration failed")
            }
            owner,err:=candidate.Claim()
            if err!=nil||owner==nil {
                if owner!=nil {if owner.Close()!=nil {t.Error("partial owner cleanup failed")}} else if candidate.Close()!=nil {t.Error("partial candidate cleanup failed")}
                t.Fatal("candidate claim failed")
            }
            closeAttempted:=false
            t.Cleanup(func(){if !closeAttempted {closeAttempted=true;if owner.Close()!=nil {t.Error("candidate cleanup failed")}}})
            if err=owner.WithPinnedPayload(func(raw []byte)error{if len(raw)!=14299136||digest(raw)!="a61e25065ae35d51f1ac8133959176b21e2c3740c1516b76f799371f1c27f9a2"{return errGHAGuardUnqualified};return nil});err!=nil {t.Fatal("payload binding failed")}
            closeAttempted=true
            if err=owner.Close();err!=nil {t.Fatal("candidate cleanup failed")}
            closed,stop:=context.WithTimeout(context.Background(),3*time.Second);defer stop()
            if err=owner.WaitClosed(closed);err!=nil {t.Fatal("candidate cleanup incomplete")}
        })
        if !ok {return}
        if windowInterrupted {ghaGuardActualSkip(t,"phase_window")}
    }
}
