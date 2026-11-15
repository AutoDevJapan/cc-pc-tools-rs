//go:build windows

package main

import (
    "bytes"
    "context"
    "crypto/sha256"
    "encoding/hex"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "net/http"
    "os"
    "path/filepath"
    "testing"
)

type ghaGuardPublicTestRT func(*http.Request) (*http.Response, error)
func (f ghaGuardPublicTestRT) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type ghaGuardPublicTestBody struct {
    io.Reader
    closeErr error
    closed int
}
func (b *ghaGuardPublicTestBody) Close() error { b.closed++; return b.closeErr }

type ghaGuardPublicTestReadError struct{}
func (ghaGuardPublicTestReadError) Read([]byte) (int, error) { return 0, errors.New("read failed") }

func ghaGuardPublicTestFixture(t *testing.T) []byte {
    t.Helper()
    b, err := os.ReadFile(filepath.Join("..", "guard-attestation-verifier", "actual-attestation.sigstore.json"))
    if err != nil { t.Fatal(err) }
    h := sha256.Sum256(b)
    if len(b) != 11619 || hex.EncodeToString(h[:]) != "c7c63a9adc5d342674e61116f732f4fd6dd1b82a8a039228ce80e9ff5fee85e1" { t.Fatal("fixture mismatch") }
    return b
}

func ghaGuardPublicTestEnvelope(repo int64, location string, bundle []byte, count int) []byte {
    one := fmt.Sprintf("{\"repository_id\":%d,\"bundle_url\":%q,\"bundle\":%s}", repo, location, bundle)
    var out bytes.Buffer
    out.WriteString("{\"attestations\":[")
    for i:=0; i<count; i++ { if i>0 { out.WriteByte(',') }; out.WriteString(one) }
    out.WriteString("]}")
    return out.Bytes()
}

func TestGHAGuardPublicBundleFixedSource(t *testing.T) {
    fixture := ghaGuardPublicTestFixture(t)
    raw := ghaGuardPublicTestEnvelope(1406818713, "https://example.invalid/67491128.json.sn", fixture, 1)
    body := &ghaGuardPublicTestBody{Reader:bytes.NewReader(raw)}
    calls := 0
    rt := ghaGuardPublicTestRT(func(req *http.Request) (*http.Response,error) {
        calls++
        if req.Method != http.MethodGet || req.URL.String() != ghaGuardPublicAttestationURL || req.Header.Get("Authorization") != "" || req.Header.Get("Cookie") != "" { t.Fatal("unexpected request authority") }
        if _, ok := req.Context().Deadline(); !ok { t.Fatal("missing deadline") }
        h := http.Header{}
        h.Set("Link", ghaGuardVersionDeprecationLink)
        return &http.Response{StatusCode:http.StatusOK, Header:h, Body:body, Request:req}, nil
    })
    got, err := ghaGuardReadPinnedPublicBundle(context.Background(),rt)
    if err != nil || !bytes.Equal(got,fixture) || calls != 1 || body.closed != 1 { t.Fatalf("public bundle not retained: err=%v calls=%d closed=%d",err,calls,body.closed) }
    clear(got)
}

func TestGHAGuardPublicBundleRejectsUntrustedInputs(t *testing.T) {
    fixture := ghaGuardPublicTestFixture(t)
    valid := ghaGuardPublicTestEnvelope(1406818713, "https://example.invalid/67491128.json.sn", fixture, 1)
    bad := append([]byte(nil),fixture...)
    needle := []byte("\"payload\":\"")
    at := bytes.Index(bad,needle)
    if at < 0 || at+len(needle) >= len(bad) { t.Fatal("missing signature payload") }
    at += len(needle)
    if bad[at] == 'A' { bad[at] = 'B' } else { bad[at] = 'A' }
    if !json.Valid(bad) || len(bad) != len(fixture) { t.Fatal("invalid mutation setup") }
    tests := []struct {
        name string
        status int
        data []byte
        links []string
        location string
        readFail,closeFail bool
    }{
        {name:"wrong-repository", status:200, data:ghaGuardPublicTestEnvelope(1,"https://example.invalid/67491128.json.sn",fixture,1)},
        {name:"wrong-id", status:200, data:ghaGuardPublicTestEnvelope(1406818713,"https://example.invalid/1.json.sn",fixture,1)},
        {name:"duplicate", status:200, data:ghaGuardPublicTestEnvelope(1406818713,"https://example.invalid/67491128.json.sn",fixture,2)},
        {name:"tampered-same-length", status:200, data:ghaGuardPublicTestEnvelope(1406818713,"https://example.invalid/67491128.json.sn",bad,1)},
        {name:"malformed", status:200, data:[]byte("{")},
        {name:"oversize", status:200, data:bytes.Repeat([]byte("x"),(4<<20)+1)},
        {name:"forbidden", status:403, data:valid},
        {name:"redirect", status:302, data:valid, location:"https://example.invalid/elsewhere"},
        {name:"pagination", status:200, data:valid, links:[]string{"<https://example.invalid/next>; rel=\"next\""}},
        {name:"duplicate-link", status:200, data:valid, links:[]string{ghaGuardVersionDeprecationLink,ghaGuardVersionDeprecationLink}},
        {name:"read-error", status:200, readFail:true},
        {name:"close-error", status:200, data:valid, closeFail:true},
    }
    for _, tc := range tests {
        t.Run(tc.name,func(t *testing.T){
            var reader io.Reader = bytes.NewReader(tc.data)
            if tc.readFail { reader = ghaGuardPublicTestReadError{} }
            body := &ghaGuardPublicTestBody{Reader:reader}
            if tc.closeFail { body.closeErr = errors.New("close failed") }
            calls := 0
            rt := ghaGuardPublicTestRT(func(req *http.Request) (*http.Response,error) {
                calls++
                h := http.Header{}
                for _, value := range tc.links { h.Add("Link",value) }
                if tc.location != "" { h.Set("Location",tc.location) }
                return &http.Response{StatusCode:tc.status,Header:h,Body:body,Request:req},nil
            })
            got,err := ghaGuardReadPinnedPublicBundle(context.Background(),rt)
            if got != nil || !errors.Is(err,errGHAGuardUnqualified) || calls != 1 || body.closed != 1 { t.Fatalf("accepted or leaked: err=%v calls=%d closed=%d",err,calls,body.closed) }
        })
    }
}

func TestGHAGuardPublicBundleCancellation(t *testing.T) {
    ctx,cancel := context.WithCancel(context.Background())
    cancel()
    calls := 0
    rt := ghaGuardPublicTestRT(func(*http.Request) (*http.Response,error) { calls++; return nil,errors.New("unexpected") })
    got,err := ghaGuardReadPinnedPublicBundle(ctx,rt)
    if got != nil || !errors.Is(err,errGHAGuardUnqualified) || calls != 0 { t.Fatal("cancel was not fail-closed") }
    if got,err = ghaGuardReadPinnedPublicBundle(nil,rt); got != nil || !errors.Is(err,errGHAGuardUnqualified) { t.Fatal("nil context accepted") }
    if got,err = ghaGuardReadPinnedPublicBundle(context.Background(),nil); got != nil || !errors.Is(err,errGHAGuardUnqualified) { t.Fatal("nil transport accepted") }
}
type ghaGuardPublicTestCancelReader struct {
    reader *bytes.Reader
    cancel context.CancelFunc
}
func (r *ghaGuardPublicTestCancelReader) Read(p []byte) (int,error) {
    n,err := r.reader.Read(p)
    if err == io.EOF { r.cancel() }
    return n,err
}

func TestGHAGuardPublicBundleCancellationDuringRead(t *testing.T) {
    fixture := ghaGuardPublicTestFixture(t)
    raw := ghaGuardPublicTestEnvelope(1406818713,"https://example.invalid/67491128.json.sn",fixture,1)
    ctx,cancel := context.WithCancel(context.Background())
    defer cancel()
    body := &ghaGuardPublicTestBody{Reader:&ghaGuardPublicTestCancelReader{reader:bytes.NewReader(raw),cancel:cancel}}
    rt := ghaGuardPublicTestRT(func(req *http.Request) (*http.Response,error) {
        return &http.Response{StatusCode:http.StatusOK,Header:http.Header{},Body:body,Request:req},nil
    })
    got,err := ghaGuardReadPinnedPublicBundle(ctx,rt)
    if got != nil || !errors.Is(err,errGHAGuardUnqualified) || body.closed != 1 { t.Fatalf("cancel after read accepted: err=%v closed=%d",err,body.closed) }
}

func TestGHAGuardPublicBundleTransportFailure(t *testing.T) {
    calls := 0
    rt := ghaGuardPublicTestRT(func(*http.Request) (*http.Response,error) { calls++; return nil,errors.New("transport failed") })
    got,err := ghaGuardReadPinnedPublicBundle(context.Background(),rt)
    if got != nil || !errors.Is(err,errGHAGuardUnqualified) || calls != 1 { t.Fatal("transport failure was retried or accepted") }
}
