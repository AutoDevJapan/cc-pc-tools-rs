//go:build windows

package main

import (
    "bytes"
    "context"
    "errors"
    "fmt"
    "io"
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
    cases:=[]struct{name string;status int;links []string;raw []byte;readFailure,closeFailure,want bool}{
        {name:"no-link",raw:good,want:true},
        {name:"deprecation",links:[]string{ghaGuardVersionDeprecationLink},raw:good,want:true},
        {name:"next",links:[]string{next},raw:good},
        {name:"prev",links:[]string{`<https://api.github.com/repos/example?page=1>; rel="prev"`},raw:good},
        {name:"first",links:[]string{`<https://api.github.com/repos/example?page=1>; rel="first"`},raw:good},
        {name:"last",links:[]string{`<https://api.github.com/repos/example?page=3>; rel="last"`},raw:good},
        {name:"mixed",links:[]string{ghaGuardVersionDeprecationLink+", "+next},raw:good},
        {name:"multiple",links:[]string{ghaGuardVersionDeprecationLink,next},raw:good},
        {name:"unknown",links:[]string{`<https://example.invalid/>; rel="unknown"`},raw:good},
        {name:"malformed",links:[]string{`not-a-link`},raw:good},
        {name:"unauthorized",status:http.StatusUnauthorized,raw:good},
        {name:"forbidden",status:http.StatusForbidden,raw:good},
        {name:"redirect",status:http.StatusFound,raw:good},
        {name:"duplicate-id",raw:wrap(correct,correct)},
        {name:"wrong-repository",raw:wrap(entry(1,fixtureURL,fixture))},
        {name:"wrong-id",raw:wrap(entry(1406818713,"https://example.invalid/other.json.sn",fixture))},
        {name:"non-https",raw:wrap(entry(1406818713,"http://example.invalid/67491128.json.sn",fixture))},
        {name:"wrong-digest",raw:wrap(entry(1406818713,fixtureURL,changed))},
        {name:"wrong-length",raw:wrap(entry(1406818713,fixtureURL,longer))},
        {name:"oversized",raw:oversized},
        {name:"read-error",raw:good,readFailure:true},
        {name:"close-error",raw:good,closeFailure:true},
    }
    for _,tc:=range cases {t.Run(tc.name,func(t *testing.T){
        calls,closes,readBytes:=0,0,0
        transport:=ghaGuardActualTestRT(func(req *http.Request)(*http.Response,error){
            calls++
            if req==nil||req.URL==nil||req.Method!=http.MethodGet||req.URL.Scheme!="https"||req.URL.Host!="api.github.com"||req.URL.Path!="/repos/AutoDevJapan/cc-pc-tools-rs/attestations/sha256:a61e25065ae35d51f1ac8133959176b21e2c3740c1516b76f799371f1c27f9a2"||req.URL.RawQuery!="per_page=100"||req.URL.User!=nil||req.Header.Get("Authorization")!=""||req.Header.Get("Cookie")!=""||req.Header.Get("Accept")!="application/vnd.github+json"||req.Header.Get("X-GitHub-Api-Version")!="2022-11-28" {t.Error("fixed unauthenticated request changed")}
            header:=make(http.Header);for _,value:=range tc.links {header.Add("Link",value)}
            status:=tc.status;if status==0 {status=http.StatusOK}
            if status==http.StatusFound {header.Set("Location","https://example.invalid/not-followed")}
            body:=&ghaGuardActualHTTPTestBody{reader:bytes.NewReader(tc.raw),closes:&closes,readBytes:&readBytes,readFailure:tc.readFailure,closeFailure:tc.closeFailure}
            return &http.Response{StatusCode:status,Header:header,Body:body,Request:req},nil
        })
        trace:=&ghaGuardActualWindowTrace{}
        got,err:=ghaGuardActualTestBundleWithTransport(context.Background(),time.Now().Add(time.Minute),trace,transport)
        if calls!=1||closes!=1 {t.Errorf("request or close count: %d/%d",calls,closes)}
        if tc.want {
            if err!=nil||!bytes.Equal(got,fixture)||len(got)!=11619||digest(got)!="c7c63a9adc5d342674e61116f732f4fd6dd1b82a8a039228ce80e9ff5fee85e1" {t.Error("valid fixed bundle was not returned")}
        } else if err==nil||got!=nil {t.Error("invalid HTTP evidence was accepted")}
        if tc.name=="oversized"&&readBytes!=(4<<20)+1 {t.Error("response read bound changed")}
        if (tc.name=="unauthorized"||tc.name=="forbidden"||tc.name=="read-error"||tc.name=="close-error")&&!trace.hard.Load() {t.Error("hard failure observation lost")}
        clear(got)
    })}
}
