//go:build windows

package main

import (
    "bytes"
    "context"
    "crypto/tls"
    "encoding/json"
    "errors"
    "io"
    "net/http"
    "runtime"
    "strings"
    "sync"
    "time"
    "unsafe"

    "golang.org/x/sys/windows"
)

const ghaGuardFixedCredentialTarget = "gh:github.com:mashfromband"
var errGHAGuardCredentialUnavailable = errors.New("guard_github_credential_unavailable")
var errGHAGuardCredentialIdentity = errors.New("guard_github_credential_identity_mismatch")
var errGHAGuardCredentialExpired = errors.New("guard_github_artifact_expired")
var errGHAGuardCredentialNotFound = errors.New("guard_github_credential_not_found")
var errGHAGuardCredentialAccessDenied = errors.New("guard_github_credential_access_denied")
var errGHAGuardCredentialUnauthorized = errors.New("guard_github_credential_unauthorized")
var errGHAGuardCredentialForbidden = errors.New("guard_github_credential_forbidden")
var errGHAGuardCredentialStatus = errors.New("guard_github_credential_http_status")
// The secret is revoked before waiting; timeout still means cleanup is unverified.
var errGHAGuardCredentialCleanup = errors.New("guard_github_credential_cleanup_failed")

// Fixed expiry only rejects. Live GitHub metadata remains mandatory for admission.
func ghaGuardFixedArtifactWindow(now time.Time) bool {
    return !now.IsZero() && now.UTC().Before(time.Date(2026,11,15,8,9,29,0,time.UTC))
}

type ghaGuardWinCredential struct {
    Flags uint32
    Type uint32
    TargetName *uint16
    Comment *uint16
    LastWritten [2]uint32
    CredentialBlobSize uint32
    CredentialBlob *byte
    Persist uint32
    AttributeCount uint32
    Attributes uintptr
    TargetAlias *uint16
    UserName *uint16
}

func ghaGuardCredentialBytesValid(raw []byte) bool {
    if len(raw)==0 || len(raw)>2560 {return false}
    padding:=false;haveValue:=false
    for _,b:=range raw {
        if b=='=' {padding=true;continue}
        if padding {return false}
        if !(b>='A'&&b<='Z'||b>='a'&&b<='z'||b>='0'&&b<='9'||b=='-'||b=='.'||b=='_'||b=='~'||b=='+'||b=='/') {return false}
        haveValue=true
    }
    return haveValue
}

// Read exactly one user-scoped Windows Credential Manager item. The OS call
// is synchronous and is not interrupted by context cancellation.
func ghaGuardReadFixedWinCredential() ([]byte,error) {
    dll:=windows.NewLazySystemDLL("advapi32.dll")
    read:=dll.NewProc("CredReadW");free:=dll.NewProc("CredFree")
    if read.Find()!=nil || free.Find()!=nil {return nil,errGHAGuardCredentialUnavailable}
    target,err:=windows.UTF16PtrFromString(ghaGuardFixedCredentialTarget)
    if err!=nil {return nil,errGHAGuardCredentialUnavailable}
    var c *ghaGuardWinCredential
    ok,_,callErr:=read.Call(uintptr(unsafe.Pointer(target)),uintptr(1),0,uintptr(unsafe.Pointer(&c)))
    runtime.KeepAlive(target)
    if ok==0 {
        if errors.Is(callErr,windows.ERROR_NOT_FOUND) {return nil,errGHAGuardCredentialNotFound}
        if errors.Is(callErr,windows.ERROR_ACCESS_DENIED) {return nil,errGHAGuardCredentialAccessDenied}
        return nil,errGHAGuardCredentialUnavailable
    }
    if c==nil {return nil,errGHAGuardCredentialUnavailable}
    defer func(){
        if c.CredentialBlob!=nil && c.CredentialBlobSize>0 && c.CredentialBlobSize<=2560 {clear(unsafe.Slice(c.CredentialBlob,int(c.CredentialBlobSize)))}
        free.Call(uintptr(unsafe.Pointer(c)))
    }()
    if c.Type!=1 || c.TargetName==nil || c.UserName==nil || !strings.EqualFold(windows.UTF16PtrToString(c.TargetName),ghaGuardFixedCredentialTarget) || !strings.EqualFold(windows.UTF16PtrToString(c.UserName),"mashfromband") || c.CredentialBlob==nil || c.CredentialBlobSize==0 || c.CredentialBlobSize>2560 {return nil,errGHAGuardCredentialUnavailable}
    secret:=append([]byte(nil),unsafe.Slice(c.CredentialBlob,int(c.CredentialBlobSize))...)
    if !ghaGuardCredentialBytesValid(secret) {clear(secret);return nil,errGHAGuardCredentialUnavailable}
    return secret,nil
}

// Only fixed read-only API paths are eligible for the owned token.
func ghaGuardCredentialAllowed(req *http.Request) bool {
    if req==nil || req.URL==nil || req.Method!=http.MethodGet || req.Body!=nil && req.Body!=http.NoBody {return false}
    u:=req.URL
    if u.Scheme!="https" || u.Host!="api.github.com" || u.User!=nil || u.Opaque!="" || u.Fragment!="" || u.RawPath!="" || u.ForceQuery || (req.Host!=""&&req.Host!="api.github.com") {return false}
    for name,values:=range req.Header {
        if len(values)!=1 {return false}
        switch strings.ToLower(name) {
        case "accept":
            if values[0]!="application/vnd.github+json" && values[0]!="application/vnd.github.raw+json" {return false}
        case "x-github-api-version":
            if values[0]!="2022-11-28" {return false}
        default:return false
        }
    }
    if u.Path=="/user" {return u.RawQuery==""}
    const base="/repos/AutoDevJapan/cc-pc-tools-rs"
    p:=strings.TrimPrefix(u.Path,base)
    if p==u.Path {return false}
    switch p {
    case "", "/actions/workflows/unattended-readiness.yml", "/actions/runs/43574333045",
        "/actions/jobs/132385166063/logs", "/actions/artifacts/14105209772/zip",
        "/git/commits/80c4ed5cbd864585f4894c6286d3d5c92194c1f5":
        return u.RawQuery==""
    case "/actions/runs/43574333045/attempts/1/jobs", "/actions/runs/43574333045/artifacts":
        return u.RawQuery=="per_page=100"
    }
    for _,prefix:=range []string{"/git/trees/","/git/blobs/"} {
        if strings.HasPrefix(p,prefix) {return u.RawQuery=="" && ghaGuardHex(strings.TrimPrefix(p,prefix),40)}
    }
    return false
}

// Only fixed status categories are exposed; response bodies are not returned.
func ghaGuardCredentialStatusError(status int) error {
    switch status {
    case http.StatusOK:return nil
    case http.StatusUnauthorized:return errGHAGuardCredentialUnauthorized
    case http.StatusForbidden:return errGHAGuardCredentialForbidden
    default:return errGHAGuardCredentialStatus
    }
}

func ghaGuardCredentialOwnerJSON(raw []byte) bool {
    if len(raw)==0 || len(raw)>64<<10 {return false}
    d:=json.NewDecoder(bytes.NewReader(raw));d.UseNumber()
    first,err:=d.Token();if err!=nil || first!=json.Delim('{') {return false}
    idSeen,loginSeen:=false,false
    for d.More() {
        keyToken,e:=d.Token();if e!=nil{return false};key,ok:=keyToken.(string);if !ok{return false}
        switch {
        case strings.EqualFold(key,"id"):
            if key!="id"||idSeen{return false};idSeen=true
            v,e:=d.Token();n,ok:=v.(json.Number);if e!=nil||!ok||n.String()!="88025750"{return false}
        case strings.EqualFold(key,"login"):
            if key!="login"||loginSeen{return false};loginSeen=true
            v,e:=d.Token();name,ok:=v.(string);if e!=nil||!ok||!strings.EqualFold(name,"mashfromband"){return false}
        default:
            var skip json.RawMessage;if d.Decode(&skip)!=nil{return false};clear(skip)
        }
    }
    end,err:=d.Token();if err!=nil||end!=json.Delim('}')||!idSeen||!loginSeen{return false}
    return d.Decode(new(any))==io.EOF
}

type ghaGuardCredentialSession struct {
    mu sync.Mutex
    // Non-nil only for the code-owned fresh-generation acquisition.
    fresh *ghaFreshPublicLocator
    discovery *ghaFreshDiscoveryScope
    retired map[ghaFreshRetiredKey]time.Time
    secret []byte
    base http.RoundTripper
    root context.Context
    cancel context.CancelFunc
    ready,closed,drained bool
    active int
    drain chan struct{}
}

func (s *ghaGuardCredentialSession) finish() {
    s.mu.Lock();defer s.mu.Unlock()
    s.active--
    if s.closed && s.active==0 && !s.drained {s.drained=true;close(s.drain)}
}

func (s *ghaGuardCredentialSession) close(ctx context.Context) error {
    if s==nil||ctx==nil{return errGHAGuardUnqualified}
    s.mu.Lock()
    if !s.closed {
        s.closed=true;clear(s.secret);s.secret=nil;clear(s.retired);s.retired=nil;s.cancel()
        if s.active==0 && !s.drained {s.drained=true;close(s.drain)}
    }
    done:=s.drain;s.mu.Unlock()
    if closer,ok:=s.base.(interface{CloseIdleConnections()});ok {closer.CloseIdleConnections()}
    select {case <-done:return nil;case <-ctx.Done():return errGHAGuardUnqualified}
}

type ghaGuardCredentialBody struct {
    body io.ReadCloser
    once sync.Once
    err error
    release func()
}
func (b *ghaGuardCredentialBody) Close() error {
    b.once.Do(func(){b.err=b.body.Close();b.release()})
    return b.err
}
func (b *ghaGuardCredentialBody) Read(p []byte) (int,error) {
    n,err:=b.body.Read(p)
    if err!=nil {if closeErr:=b.Close();err==io.EOF && closeErr!=nil {return n,errGHAGuardResponseCleanup}}
    return n,err
}

// Strip caller context values including HTTP tracing; retain only deadline and
// cancellation. The owned credential never enters the caller request headers.
func ghaGuardCredentialContext(original,root context.Context) (context.Context,func()) {
    clean,cancel:=context.WithCancel(context.Background())
    var deadlineCancel context.CancelFunc
    if d,ok:=original.Deadline();ok {clean,deadlineCancel=context.WithDeadline(clean,d)}
    stopCaller:=context.AfterFunc(original,cancel);stopRoot:=context.AfterFunc(root,cancel)
    release:=func(){stopCaller();stopRoot();if deadlineCancel!=nil {deadlineCancel()};cancel()}
    return clean,release
}

func (s *ghaGuardCredentialSession) RoundTrip(req *http.Request) (*http.Response,error) {
    if s==nil||req==nil||req.URL==nil{return nil,errGHAGuardUnqualified}
    if req.Context().Err()!=nil{return nil,errors.Join(errGHAGuardUnqualified,req.Context().Err())}
    s.mu.Lock()
    allowed:=false
    if s.fresh!=nil {allowed=ghaFreshCredentialAllowed(req,*s.fresh,time.Now().UTC())}
    if !allowed&&s.discovery!=nil {allowed=s.discovery.allows(req)}
    if s.fresh==nil&&s.discovery==nil {allowed=ghaGuardCredentialAllowed(req)}
    if !allowed||s.closed||len(s.secret)==0||(!s.ready&&req.URL.Path!="/user") {s.mu.Unlock();return nil,errGHAGuardUnqualified}
    s.active++;value:="Bearer "+string(s.secret);root:=s.root;base:=s.base;s.mu.Unlock()
    clean,releaseContext:=ghaGuardCredentialContext(req.Context(),root)
    release:=func(){releaseContext();s.finish()}
    if req.Context().Err()!=nil || root.Err()!=nil {release();return nil,errors.Join(errGHAGuardUnqualified,req.Context().Err(),root.Err())}
    clone:=req.Clone(clean);clone.Header=req.Header.Clone();if clone.Header==nil {clone.Header=make(http.Header)}
    clone.Header.Set("Authorization",value)
    resp,err:=base.RoundTrip(clone)
    // The underlying transport may read clone until the response body closes.
    // Do not mutate the request; its header is reclaimed with the request.
    value=""
    if err!=nil || resp==nil || resp.Body==nil {
        var closeErr,contextErr error
        if resp!=nil&&resp.Body!=nil {closeErr=resp.Body.Close()}
        if errors.Is(err,errGHAGuardResponseCleanup) {closeErr=errors.Join(closeErr,errGHAGuardResponseCleanup)}
        if errors.Is(err,context.Canceled) {contextErr=errors.Join(contextErr,context.Canceled)}
        if errors.Is(err,context.DeadlineExceeded) {contextErr=errors.Join(contextErr,context.DeadlineExceeded)}
        if req.Context().Err()!=nil {contextErr=errors.Join(contextErr,req.Context().Err())}
        if root.Err()!=nil {contextErr=errors.Join(contextErr,root.Err())}
        release()
        if closeErr==nil&&contextErr==nil{return nil,errGHAGuardUnqualified}
        return nil,ghaGuardResponseFailure(errGHAGuardUnqualified,closeErr,contextErr)
    }
    resp.Request=req;resp.Body=&ghaGuardCredentialBody{body:resp.Body,release:release}
    return resp,nil
}

func ghaGuardCloseCredentialAuthority(a *ghaGuardEvidenceAuthority,primary error) error {
    ctx,cancel:=context.WithTimeout(context.Background(),5*time.Second);defer cancel()
    return ghaGuardCloseCredentialAuthorityWithContext(a,primary,ctx)
}

// The explicit context is a cleanup test seam; it is never an authority input.
type ghaFreshCredentialAudit struct { state string }

func ghaGuardCloseCredentialAuthorityWithContext(a *ghaGuardEvidenceAuthority,primary error,ctx context.Context) error {
    return ghaGuardCloseCredentialAuthorityObserved(a,primary,ctx,nil)
}

// Audit is reporting only and is set exclusively by the real close/drain call.
func ghaGuardCloseCredentialAuthorityObserved(a *ghaGuardEvidenceAuthority,primary error,ctx context.Context,audit *ghaFreshCredentialAudit) error {
    if a==nil||a.credential==nil||ctx==nil{return errors.Join(primary,errGHAGuardUnqualified)}
    if err:=a.credential.close(ctx);err!=nil {
        if audit!=nil {audit.state="failed"}
        return errors.Join(primary,errGHAGuardUnqualified,errGHAGuardCredentialCleanup)
    }
    if audit!=nil {audit.state="completed"}
    return errors.Join(primary,nil)
}

func ghaFreshCloseCredentialAuthority(a *ghaGuardEvidenceAuthority,primary error,audit *ghaFreshCredentialAudit) error {
    ctx,cancel:=context.WithTimeout(context.Background(),5*time.Second);defer cancel()
    return ghaGuardCloseCredentialAuthorityObserved(a,primary,ctx,audit)
}

// No caller token, URL, identity boolean or callback can issue this authority.
// The uninterruptible Win32 call needs an outer process supervisor before use.
func ghaGuardOpenWinCredAuthority(ctx context.Context) (*ghaGuardEvidenceAuthority,error) {
    return ghaGuardOpenWinCredAuthorityWithLocator(ctx,nil,nil)
}

// The in-process fixed-purpose flow treats this locator as untrusted.
// Authentication, server metadata and signatures must still agree before use.
func ghaFreshOpenWinCredAuthority(ctx context.Context,locator ghaFreshPublicLocator) (*ghaGuardEvidenceAuthority,error) {
    return ghaFreshOpenWinCredAuthorityObserved(ctx,locator,nil)
}

func ghaFreshOpenWinCredAuthorityObserved(ctx context.Context,locator ghaFreshPublicLocator,audit *ghaFreshCredentialAudit) (*ghaGuardEvidenceAuthority,error) {
    if audit!=nil {audit.state="unknown"}
    if !ghaFreshLocatorValid(locator,time.Now().UTC()) {return nil,errors.Join(errGHAGuardUnqualified,errGHAGuardCredentialExpired)}
    owned:=locator
    return ghaGuardOpenWinCredAuthorityWithLocator(ctx,&owned,audit)
}

func ghaGuardOpenWinCredAuthorityWithLocator(ctx context.Context,locator *ghaFreshPublicLocator,audit *ghaFreshCredentialAudit) (*ghaGuardEvidenceAuthority,error) {
    return ghaGuardOpenWinCredAuthorityWithPurpose(ctx,locator,nil,audit)
}

// Discovery starts with only the fixed owner and workflow-list endpoints.
func ghaFreshOpenWinCredDiscovery(ctx context.Context,audit *ghaFreshCredentialAudit) (*ghaGuardEvidenceAuthority,error) {
    scope,err:=ghaFreshDiscoveryURLs(nil);if err!=nil{return nil,err}
    return ghaGuardOpenWinCredAuthorityWithPurpose(ctx,nil,scope,audit)
}

func ghaGuardOpenWinCredAuthorityWithPurpose(ctx context.Context,locator *ghaFreshPublicLocator,discovery *ghaFreshDiscoveryScope,audit *ghaFreshCredentialAudit) (out *ghaGuardEvidenceAuthority,resultErr error) {
    if ctx==nil||ctx.Err()!=nil{return nil,errGHAGuardUnqualified}
    if locator==nil&&discovery==nil {
        if !ghaGuardFixedArtifactWindow(time.Now()) {return nil,errors.Join(errGHAGuardUnqualified,errGHAGuardCredentialExpired)}
    } else if locator!=nil&&!ghaFreshLocatorValid(*locator,time.Now().UTC()) {return nil,errors.Join(errGHAGuardUnqualified,errGHAGuardCredentialExpired)}
    secret,err:=ghaGuardReadFixedWinCredential()
    if err!=nil{return nil,errors.Join(errGHAGuardUnqualified,err)}
    root,cancel:=context.WithCancel(context.Background())
    transport:=&http.Transport{Proxy:nil,TLSHandshakeTimeout:10*time.Second,ResponseHeaderTimeout:15*time.Second,MaxResponseHeaderBytes:64<<10,TLSNextProto:make(map[string]func(string,*tls.Conn)http.RoundTripper)}
    session:=&ghaGuardCredentialSession{fresh:locator,discovery:discovery,secret:secret,base:transport,root:root,cancel:cancel,drain:make(chan struct{})}
    client:=&http.Client{Transport:session,Timeout:30*time.Second,CheckRedirect:func(*http.Request,[]*http.Request)error{return http.ErrUseLastResponse}}
    authority:=&ghaGuardEvidenceAuthority{client:client,credential:session}
    issued:=false
    defer func(){if !issued {resultErr=ghaFreshCloseCredentialAuthority(authority,resultErr,audit)}}()
    if ctx.Err()!=nil{return nil,errGHAGuardUnqualified}
    req,err:=http.NewRequestWithContext(ctx,http.MethodGet,"https://api.github.com/user",nil)
    if err!=nil{return nil,errGHAGuardUnqualified}
    req.Header.Set("Accept","application/vnd.github+json");req.Header.Set("X-GitHub-Api-Version","2022-11-28")
    response,err:=client.Do(req)
    if err!=nil||response==nil||response.Body==nil {
        var closeErr error;if response!=nil&&response.Body!=nil {closeErr=response.Body.Close()}
        return nil,errors.Join(ghaGuardResponseFailure(errGHAGuardUnqualified,closeErr,ctx.Err()),ghaFreshContextFailure(ctx,err))
    }
    if statusErr:=ghaGuardCredentialStatusError(response.StatusCode);statusErr!=nil {
        primary:=errors.Join(ghaGuardHTTPFailure("credential_owner",response.StatusCode),statusErr)
        return nil,ghaGuardResponseFailure(primary,response.Body.Close(),ctx.Err())
    }
    raw,readErr:=io.ReadAll(io.LimitReader(response.Body,(64<<10)+1));closeErr:=response.Body.Close()
    valid:=readErr==nil&&closeErr==nil&&ctx.Err()==nil&&ghaGuardCredentialOwnerJSON(raw)
    clear(raw)
    if !valid{return nil,errors.Join(ghaGuardResponseFailure(errors.Join(errGHAGuardUnqualified,errGHAGuardCredentialIdentity),closeErr,ctx.Err()),ghaFreshContextFailure(ctx,readErr))}
    session.mu.Lock();if session.closed {session.mu.Unlock();return nil,errGHAGuardUnqualified};session.ready=true;session.mu.Unlock()
    issued=true;return authority,nil
}
