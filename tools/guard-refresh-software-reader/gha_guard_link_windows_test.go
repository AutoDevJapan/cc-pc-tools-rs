//go:build windows

package main

import (
    "bytes"
    "context"
    "fmt"
    "io"
    "net/http"
    "strings"
    "testing"
)

type ghaGuardLinkTestTransport func(*http.Request)(*http.Response,error)
func (f ghaGuardLinkTestTransport) RoundTrip(r *http.Request)(*http.Response,error) {return f(r)}

func TestGHAGuardSinglePageLink(t *testing.T) {
    next:=`<https://api.github.com/repos/example?page=2>; rel="next"`
    cases:=[]struct{name string; header http.Header; want bool}{
        {"no-link",nil,true},
        {"known-deprecation",http.Header{"Link":[]string{ghaGuardVersionDeprecationLink}},true},
        {"surrounding-ows",http.Header{"Link":[]string{" \t"+ghaGuardVersionDeprecationLink+"\t "}},true},
        {"empty-link",http.Header{"Link":[]string{""}},false},
        {"empty-values",http.Header{"Link":nil},false},
        {"next",http.Header{"Link":[]string{next}},false},
        {"prev",http.Header{"Link":[]string{`<https://api.github.com/repos/example?page=1>; rel="prev"`}},false},
        {"first",http.Header{"Link":[]string{`<https://api.github.com/repos/example?page=1>; rel="first"`}},false},
        {"last",http.Header{"Link":[]string{`<https://api.github.com/repos/example?page=3>; rel="last"`}},false},
        {"combined",http.Header{"Link":[]string{ghaGuardVersionDeprecationLink+", "+next}},false},
        {"multiple-values",http.Header{"Link":[]string{ghaGuardVersionDeprecationLink,next}},false},
        {"case-variant-duplicate",http.Header{"Link":[]string{ghaGuardVersionDeprecationLink},"link":[]string{next}},false},
        {"unknown-relation",http.Header{"Link":[]string{`<https://docs.github.com/en/rest/about-the-rest-api/api-versions>; rel="unknown"`}},false},
        {"malformed",http.Header{"Link":[]string{`<https://docs.github.com/en/rest/about-the-rest-api/api-versions>; rel=deprecation`}},false},
        {"control-character",http.Header{"Link":[]string{ghaGuardVersionDeprecationLink+"\r\n"}},false},
    }
    for _,tc:=range cases {t.Run(tc.name,func(t *testing.T){
        if got:=ghaGuardSinglePageLink(tc.header);got!=tc.want {t.Fatalf("got %v; want %v",got,tc.want)}
    })}
}

func TestGHAGuardLinkSharedConsumers(t *testing.T) {
    next:=`<https://api.github.com/repos/example?page=2>; rel="next"`
    cases:=[]struct{name string; header http.Header; want bool}{
        {"no-link",nil,true},
        {"known-deprecation",http.Header{"Link":[]string{ghaGuardVersionDeprecationLink}},true},
        {"next",http.Header{"Link":[]string{next}},false},
        {"combined",http.Header{"Link":[]string{ghaGuardVersionDeprecationLink+", "+next}},false},
        {"multiple",http.Header{"Link":[]string{ghaGuardVersionDeprecationLink,next}},false},
        {"unknown",http.Header{"Link":[]string{`<https://example.invalid/>; rel="unknown"`}},false},
    }
    for _,tc:=range cases {t.Run(tc.name,func(t *testing.T){
        body:=[]byte(`{"value":1}`)
        client:=&http.Client{Transport:ghaGuardLinkTestTransport(func(r *http.Request)(*http.Response,error){
            return &http.Response{StatusCode:http.StatusOK,Header:tc.header.Clone(),Body:io.NopCloser(bytes.NewReader(body)),Request:r},nil
        })}
        var output struct{Value int `json:"value"`}
        serverErr:=ghaGuardServerJSON(context.Background(),client,"https://api.github.com/repos/AutoDevJapan/cc-pc-tools-rs/actions/runs/1",&output)
        treeRaw,treeErr:=ghaGuardGitGet(context.Background(),client,ghaGuardGitAPI+"/git/blobs/"+strings.Repeat("a",40),"application/vnd.github.raw+json",128)
        if (serverErr==nil)!=tc.want||(treeErr==nil)!=tc.want {t.Fatalf("server success=%v tree success=%v",serverErr==nil,treeErr==nil)}
        if tc.want&&(output.Value!=1||!bytes.Equal(treeRaw,body)) {t.Fatal("accepted body was not preserved")}
    })}
}

func TestGHAGuardActualBundleSelection(t *testing.T) {
    entry:=func(repo int64,loc string)string {return fmt.Sprintf(`{"repository_id":%d,"bundle_url":%q,"bundle":{"value":1}}`,repo,loc)}
    good:=entry(1406818713,"https://example.invalid/67491128.json.sn")
    wrongID:=entry(1406818713,"https://example.invalid/other.json.sn")
    wrap:=func(rows string)string{return `{"attestations":[`+rows+`]}`}
    cases:=[]struct{name,raw string; want bool}{
        {"one-fixed-id",wrap(good),true},
        {"fixed-and-other",wrap(good+","+wrongID),true},
        {"duplicate-fixed",wrap(good+","+good),false},
        {"wrong-repository",wrap(entry(1,"https://example.invalid/67491128.json.sn")),false},
        {"wrong-id",wrap(wrongID),false},
        {"non-https",wrap(entry(1406818713,"http://example.invalid/67491128.json.sn")),false},
        {"missing-bundle",wrap(`{"repository_id":1406818713,"bundle_url":"https://example.invalid/67491128.json.sn"}`),false},
        {"trailing-json",wrap(good)+`{}`,false},
    }
    for _,tc:=range cases {t.Run(tc.name,func(t *testing.T){
        selected,err:=ghaGuardActualSelectBundle([]byte(tc.raw))
        if (err==nil)!=tc.want {t.Fatalf("selection success=%v",err==nil)}
        if tc.want&&!bytes.Equal(selected,[]byte(`{"value":1}`)) {t.Fatal("wrong bundle selected")}
    })}
}
