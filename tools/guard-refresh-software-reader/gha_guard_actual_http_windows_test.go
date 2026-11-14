//go:build windows

package main

import (
    "bytes"
    "context"
    "crypto/x509"
    "errors"
    "fmt"
    "io"
    "net"
    "net/http"
    "os"
    "strings"
    "testing"
    "time"
)

type ghaGuardActualHTTPTestBody struct {
    reader io.Reader
    closes *int
    readBytes *int
    readFailure bool
    closeFailure bool
}
func (b *ghaGuardActualHTTPTestBody) Read(p []byte)(int,error) {
    if b.readFailure {return 0,errors.New("synthetic read failure")}
    n,err:=b.reader.Read(p);*b.readBytes+=n;return n,err
}
func (b *ghaGuardActualHTTPTestBody) Close() error {
    (*b.closes)++
    if b.closeFailure {return errors.New("synthetic close failure")}
    return nil
}

func TestGHAGuardActualBundleHTTPConsumer(t *testing.T) {
    fixture,err:=os.ReadFile("testdata/guard-attestation-67491128.json")
    if err!=nil||len(fixture)!=11619||digest(fixture)!="c7c63a9adc5d342674e61116f732f4fd6dd1b82a8a039228ce80e9ff5fee85e1" {t.Fatal("fixed public bundle fixture mismatch")}
    defer clear(fixture)
    entry:=func(repo int64,loc string,raw []byte) string {
        return fmt.Sprintf(`{"repository_id":%d,"bundle_url":%q,"bundle":%s}`,repo,loc,string(raw))
    }
    wrap:=func(entries ...string) []byte {return []byte(`{"attestations":[`+strings.Join(entries,",")+`]}`)}
    fixtureURL:="https://example.invalid/67491128.json.sn"
    correct:=entry(1406818713,fixtureURL,fixture)
    good:=wrap(correct)
    changed:=append([]byte(nil),fixture...)
    marker:=[]byte(`"mediaType":"`);index:=bytes.Index(changed,marker)
    if index<0||index+len(marker)>=len(changed) {t.Fatal("fixture mutation setup failed")}
    index+=len(marker);if changed[index]=='a'{changed[index]='b'}else{changed[index]='a'}
    longer:=append([]byte(nil),fixture[:len(fixture)-1]...);longer=append(longer,[]byte(`,"fixture_extra":1}`)...)
    oversized:=bytes.Repeat([]byte{' '},(4<<20)+100)
    next:=`<https://api.github.com/repos/example?page=2>; rel="next"`
    cases:=[]struct{name string;status int;links []string;raw []byte;readFailure,closeFailure,transportFailure,dnsFailure,certFailure,joinedFailure,deadlineFailure,want bool}{
        {name:"no-link",raw:good,want:true},
        {name:"deprecation",links:[]string{ghaGuardVersionDeprecationLink},raw:good,want:true},
        {name:"next",links:[]string{next},raw:good},
        {name:"prev",links:[]string{`<https://api.github.com/repos/example?page=1>; rel="prev"`},raw:good},
        {name:"first",links:[]string{`<https://api.github.com/repos/example?page=1>; rel="first"`},raw:good},
        {name:"last",links:[]string{`<https://api.github.com/repos/example?page=3>; rel="last"`},raw:good},
        {name:"mixed",links:[]string{ghaGuardVersionDeprecationLink+", "+next},raw:good},
        {name:"multiple",links:[]string{ghaGuardVersionDeprecationLink,next},raw:good},
        {name:"unknown",links:[]string{`<https://example.invalid/>; rel="unknown"`},raw:good},
        {name:"alternate",links:[]string{`<https://example.invalid/>; rel="alternate"`},raw:good},
        {name:"foo-rel-next",links:[]string{`<https://example.invalid/>; rel="alternate"; foo-rel="next"`},raw:good},
        {name:"malformed",links:[]string{`not-a-link`},raw:good},
        {name:"redacted-link",links:[]string{`<https://private.invalid/?opaque=do-not-print>; rel="next"`},raw:good},
        {name:"next-close-error",links:[]string{next},raw:good,closeFailure:true},
        {name:"unauthorized",status:http.StatusUnauthorized,raw:good},
        {name:"forbidden",status:http.StatusForbidden,raw:good},
        {name:"rate-limit",status:http.StatusTooManyRequests,raw:good},
        {name:"redirect",status:http.StatusFound,raw:good},
        {name:"transport-error",transportFailure:true},
        {name:"transport-dns",dnsFailure:true},
        {name:"transport-certificate",certFailure:true},
        {name:"transport-joined",joinedFailure:true},
        {name:"transport-deadline",deadlineFailure:true},
        {name:"duplicate-id",raw:wrap(correct,correct)},
        {name:"wrong-repository",raw:wrap(entry(1,fixtureURL,fixture))},
        {name:"wrong-id",raw:wrap(entry(1406818713,"https://example.invalid/other.json.sn",fixture))},
        {name:"non-https",raw:wrap(entry(1406818713,"http://example.invalid/67491128.json.sn",fixture))},
        {name:"wrong-digest",raw:wrap(entry(1406818713,fixtureURL,changed))},
        {name:"wrong-length",raw:wrap(entry(1406818713,fixtureURL,longer))},
        {name:"bad-json",raw:[]byte("{")},
        {name:"trailing-json",raw:append(append([]byte(nil),good...),[]byte("{}")...)},
        {name:"oversized",raw:oversized},
        {name:"read-error",raw:good,readFailure:true},
        {name:"close-error",raw:good,closeFailure:true},
        {name:"read-close-error",raw:good,readFailure:true,closeFailure:true},
    }
    for _,tc:=range cases {t.Run(tc.name,func(t *testing.T){
        calls,closes,readBytes:=0,0,0
        transport:=ghaGuardActualTestRT(func(req *http.Request)(*http.Response,error){
            calls++
            if req==nil||req.URL==nil||req.Method!=http.MethodGet||req.URL.Scheme!="https"||req.URL.Host!="api.github.com"||req.URL.Path!="/repos/AutoDevJapan/cc-pc-tools-rs/attestations/sha256:a61e25065ae35d51f1ac8133959176b21e2c3740c1516b76f799371f1c27f9a2"||req.URL.RawQuery!="per_page=100"||req.URL.User!=nil||req.Header.Get("Authorization")!=""||req.Header.Get("Cookie")!=""||req.Header.Get("Accept")!="application/vnd.github+json"||req.Header.Get("X-GitHub-Api-Version")!="2022-11-28" {t.Error("fixed unauthenticated request changed")}
            if tc.transportFailure {return nil,errors.New("do-not-print-transport-secret")}
            if tc.dnsFailure {return nil,&net.DNSError{Err:"do-not-print-dns-secret",Name:"do-not-print-host.invalid",IsNotFound:true}}
            if tc.certFailure {return nil,x509.UnknownAuthorityError{}}
            if tc.joinedFailure {return nil,errors.Join(context.DeadlineExceeded,errors.New("do-not-print-joined-secret"))}
            if tc.deadlineFailure {return nil,context.DeadlineExceeded}
            header:=make(http.Header);for _,value:=range tc.links {header.Add("Link",value)}
            status:=tc.status;if status==0 {status=http.StatusOK}
            if status==http.StatusFound {header.Set("Location","https://example.invalid/not-followed")}
            body:=&ghaGuardActualHTTPTestBody{reader:bytes.NewReader(tc.raw),closes:&closes,readBytes:&readBytes,readFailure:tc.readFailure,closeFailure:tc.closeFailure}
            return &http.Response{StatusCode:status,Header:header,Body:body,Request:req},nil
        })
        trace:=&ghaGuardActualWindowTrace{}
        var diag ghaGuardActualBundleDiagnostic
        got,err:=ghaGuardActualTestBundleWithTransport(context.Background(),time.Now().Add(time.Minute),trace,transport,&diag)
        wantCloses:=1;if tc.transportFailure||tc.dnsFailure||tc.certFailure||tc.joinedFailure||tc.deadlineFailure {wantCloses=0}
        if calls!=1||closes!=wantCloses {t.Errorf("request or close count: %d/%d",calls,closes)}
        if tc.want {
            if err!=nil||!bytes.Equal(got,fixture)||len(got)!=11619||digest(got)!="c7c63a9adc5d342674e61116f732f4fd6dd1b82a8a039228ce80e9ff5fee85e1" {t.Error("valid fixed bundle was not returned")}
        } else if err==nil||got!=nil {t.Error("invalid HTTP evidence was accepted")}
        if tc.name=="oversized"&&readBytes!=(4<<20)+1 {t.Error("response read bound changed")}
        if (tc.name=="unauthorized"||tc.name=="forbidden"||tc.name=="rate-limit"||tc.name=="read-error"||tc.name=="close-error"||tc.name=="read-close-error"||tc.name=="transport-error"||tc.name=="transport-dns"||tc.name=="transport-certificate"||tc.name=="transport-joined"||tc.name=="transport-deadline")&&!trace.hard.Load() {t.Error("hard failure observation lost")}
        stages:=map[string]string{
            "no-link":"ok","deprecation":"ok","next":"link","prev":"link","first":"link","last":"link","mixed":"link","multiple":"link","unknown":"link","alternate":"link","foo-rel-next":"link","malformed":"link","redacted-link":"link","next-close-error":"link",
            "unauthorized":"status","forbidden":"status","rate-limit":"status","redirect":"status","transport-error":"transport","transport-dns":"transport","transport-certificate":"transport","transport-joined":"transport","transport-deadline":"transport",
            "duplicate-id":"select_match","wrong-repository":"select_match","wrong-id":"select_match","non-https":"select_match","wrong-digest":"pinned_digest","wrong-length":"pinned_length",
            "bad-json":"select_json","trailing-json":"select_json","oversized":"bound","read-error":"read","close-error":"close","read-close-error":"read_close",
        }
        if wantStage,ok:=stages[tc.name];!ok||diag.stage!=wantStage {t.Errorf("diagnostic stage=%s",diag.stage)}
        if tc.status!=0&&diag.status!=tc.status {t.Errorf("diagnostic status=%d",diag.status)}
        if tc.name=="deprecation"&&(!diag.linkKnown||diag.linkKeys!=1||diag.linkValues!=1||diag.linkMarker!="known_deprecation"||!diag.linkHasSHA) {t.Error("deprecation diagnostic mismatch")}
        if tc.name=="next"&&(diag.linkMarker!="contains_next_marker"||diag.linkKeys!=1||diag.linkValues!=1||!diag.linkHasSHA) {t.Error("next diagnostic mismatch")}
        if tc.name=="mixed"&&diag.linkMarker!="contains_multiple_markers" {t.Error("mixed Link diagnostic mismatch")}
        if tc.name=="multiple"&&(diag.linkMarker!="non_single_field"||diag.linkValues!=2||diag.linkHasSHA) {t.Error("multiple Link diagnostic mismatch")}
        if tc.name=="alternate"&&diag.linkMarker!="other" {t.Error("alternate Link marker mismatch")}
        if tc.name=="foo-rel-next"&&diag.linkMarker!="contains_next_marker" {t.Error("substring marker diagnostic mismatch")}
        if tc.name=="duplicate-id"&&(diag.entries!=2||diag.matches!=2) {t.Error("selection diagnostic mismatch")}
        if (tc.name=="wrong-repository"||tc.name=="wrong-id"||tc.name=="non-https")&&(diag.entries!=1||diag.matches!=0) {t.Error("zero-match diagnostic mismatch")}
        if tc.want&&(diag.entries!=1||diag.matches!=1||diag.bundleBytes!=11619||!diag.bundleHasSHA||!diag.bundleDigestMatch||fmt.Sprintf("%x",diag.bundleSHA)!="c7c63a9adc5d342674e61116f732f4fd6dd1b82a8a039228ce80e9ff5fee85e1") {t.Error("successful selection diagnostic mismatch")}
        if tc.name=="wrong-digest"&&(diag.bundleBytes!=11619||!diag.bundleHasSHA||diag.bundleDigestMatch) {t.Error("digest diagnostic missing")}
        if tc.name=="wrong-length"&&(diag.bundleBytes<=11619||!diag.bundleHasSHA||diag.bundleDigestMatch) {t.Error("length diagnostic missing")}
        if tc.name=="oversized"&&diag.bodyBytes!=(4<<20)+1 {t.Error("body bound diagnostic missing")}
        if tc.name=="read-error"&&(!diag.readFailed||diag.closeFailed) {t.Error("read error diagnostic mismatch")}
        if tc.name=="close-error"&&(diag.readFailed||!diag.closeFailed) {t.Error("close error diagnostic mismatch")}
        if tc.name=="read-close-error"&&(!diag.readFailed||!diag.closeFailed) {t.Error("combined I/O diagnostic mismatch")}
        if tc.name=="next-close-error"&&!diag.closeFailed {t.Error("close error diagnostic missing")}
        if tc.name=="transport-dns"&&diag.transport!="dns" {t.Error("DNS diagnostic mismatch")}
        if tc.name=="transport-certificate"&&diag.transport!="tls_certificate" {t.Error("certificate diagnostic mismatch")}
        if tc.name=="transport-joined"&&diag.transport=="deadline" {t.Error("combined error misclassified as deadline")}
        if tc.name=="transport-deadline"&&(diag.transport!="deadline"||diag.context!="active"||!diag.hard||ghaGuardActualNotExecuted(ghaGuardActualAcquisition,err,trace)) {t.Error("unrelated deadline was hidden")}
        output:=diag.safeString()
        if tc.name=="foo-rel-next"&&(!strings.Contains(output,"link_marker=contains_next_marker")||strings.Contains(output,"link_rel=")) {t.Error("substring marker was reported as a relation")}
        for _,private:=range []string{"do-not-print","private.invalid","example.invalid","token=","api.github.com"} {if strings.Contains(output,private) {t.Error("diagnostic leaked private material")}}
        clear(got)
    })}
}

func TestGHAGuardActualBundleDiagnosticContextAndRedaction(t *testing.T) {
    t.Run("cancelled",func(t *testing.T){
        ctx,cancel:=context.WithCancel(context.Background());cancel()
        var d ghaGuardActualBundleDiagnostic;d.finish(ctx,&ghaGuardActualWindowTrace{})
        if d.context!="cancelled" {t.Fatal("cancelled context diagnostic mismatch")}
    })
    t.Run("window-deadline",func(t *testing.T){
        ctx,cancel:=context.WithDeadlineCause(context.Background(),time.Now().Add(-time.Second),ghaGuardActualWindowClosed);defer cancel()
        trace:=&ghaGuardActualWindowTrace{};trace.observeError(ctx,ctx.Err())
        var d ghaGuardActualBundleDiagnostic;d.finish(ctx,trace)
        if d.context!="window_deadline" {t.Fatal("window deadline diagnostic mismatch")}
    })
    t.Run("foreign-deadline",func(t *testing.T){
        ctx,cancel:=context.WithDeadline(context.Background(),time.Now().Add(-time.Second));defer cancel()
        var d ghaGuardActualBundleDiagnostic;d.finish(ctx,&ghaGuardActualWindowTrace{})
        if d.context!="deadline" {t.Fatal("other deadline diagnostic mismatch")}
    })
    t.Run("enum-redaction",func(t *testing.T){
        d:=ghaGuardActualBundleDiagnostic{stage:"do-not-print-stage",linkMarker:"do-not-print-link",context:"do-not-print-context",transport:"do-not-print-transport"}
        output:=d.safeString()
        if strings.Contains(output,"do-not-print")||!strings.Contains(output,"stage=unknown")||!strings.Contains(output,"link_marker=unknown") {t.Fatal("diagnostic enum was not redacted")}
    })
}
