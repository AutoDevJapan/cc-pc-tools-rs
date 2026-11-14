//go:build windows

package main

import (
    "bytes"
    "context"
    "encoding/hex"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "net/http"
    "sort"
    "strings"
    "sync/atomic"
    "testing"
)

type ghaGuardTreeTestTransport func(*http.Request) (*http.Response, error)
func (f ghaGuardTreeTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type ghaGuardTreeTestFixture struct {
    client *http.Client
    records []ghaGuardSourceRecord
    commit, tree string
    requests atomic.Int64
    blobRequests atomic.Int64
    endpoints map[string][]byte
}

func ghaGuardTreeTestMakeTree(t *testing.T, entries []ghaGuardGitTreeEntry) (string, []byte) {
    t.Helper()
    items := append([]ghaGuardGitTreeEntry(nil), entries...)
    sort.Slice(items, func(i, j int) bool {
        a, b := items[i].Path, items[j].Path
        if items[i].Type == "tree" { a += "/" }
        if items[j].Type == "tree" { b += "/" }
        return a < b
    })
    var raw bytes.Buffer
    for _, item := range items {
        mode := item.Mode
        if mode == "040000" { mode = "40000" }
        object, err := hex.DecodeString(item.SHA)
        if err != nil { t.Fatal(err) }
        _, _ = fmt.Fprintf(&raw, "%s %s%c", mode, item.Path, byte(0))
        _, _ = raw.Write(object)
    }
    id := ghaGuardGitObjectID("tree", raw.Bytes())
    reply, err := json.Marshal(struct { SHA string; Truncated bool; Tree []ghaGuardGitTreeEntry }{id, false, items})
    if err != nil { t.Fatal(err) }
    return id, reply
}

func ghaGuardTreeTestFixtureFor(t *testing.T, mode string) *ghaGuardTreeTestFixture {
    t.Helper()
    f := &ghaGuardTreeTestFixture{commit: strings.Repeat("c", 40), endpoints: make(map[string][]byte)}
    blob1, blob2, blob3 := []byte("module guard"), []byte("catalog data"), []byte("module dependency")
    id1, id2, id3 := ghaGuardGitObjectID("blob", blob1), ghaGuardGitObjectID("blob", blob2), ghaGuardGitObjectID("blob", blob3)
    n1,n2,n3:=int64(len(blob1)),int64(len(blob2)),int64(len(blob3))
    f.records = []ghaGuardSourceRecord{
        {Path:"repair-guard/go.mod",Size:n1,SHA:digest(blob1),Blob:id1},
        {Path:"repair-guard/owner_catalog.json",Size:n2,SHA:digest(blob2),Blob:id2},
        {Path:"tools/civ6-ci-contract/go.mod",Size:n3,SHA:digest(blob3),Blob:id3},
    }
    if mode == "bad-input-mode" { mode = "100755" } else { mode = "100644" }
    mk := func(entries []ghaGuardGitTreeEntry) string {
        id, body := ghaGuardTreeTestMakeTree(t, entries)
        f.endpoints["/git/trees/"+id] = body
        return id
    }
    guardID:=mk([]ghaGuardGitTreeEntry{{Path:"go.mod",Mode:mode,Type:"blob",SHA:id1,Size:&n1},{Path:"owner_catalog.json",Mode:"100644",Type:"blob",SHA:id2,Size:&n2}})
    depID:=mk([]ghaGuardGitTreeEntry{{Path:"go.mod",Mode:"100644",Type:"blob",SHA:id3,Size:&n3}})
    toolsID:=mk([]ghaGuardGitTreeEntry{{Path:"civ6-ci-contract",Mode:"040000",Type:"tree",SHA:depID}})
    f.tree=mk([]ghaGuardGitTreeEntry{{Path:"repair-guard",Mode:"040000",Type:"tree",SHA:guardID},{Path:"tools",Mode:"040000",Type:"tree",SHA:toolsID}})
    f.endpoints[""]=[]byte(`{"id":1406818713}`)
    f.endpoints["/git/commits/"+f.commit]=[]byte(fmt.Sprintf(`{"sha":%q,"tree":{"sha":%q}}`,f.commit,f.tree))
    f.endpoints["/git/blobs/"+id1]=blob1
    f.endpoints["/git/blobs/"+id2]=blob2
    f.endpoints["/git/blobs/"+id3]=blob3
    f.client=&http.Client{Transport: ghaGuardTreeTestTransport(func(r *http.Request)(*http.Response,error){
        f.requests.Add(1)
        if strings.HasPrefix(r.URL.Path,"/repos/AutoDevJapan/cc-pc-tools-rs/git/blobs/") { f.blobRequests.Add(1) }
        if r.URL.Scheme!="https"||r.URL.Host!="api.github.com"||!strings.HasPrefix(r.URL.Path,"/repos/AutoDevJapan/cc-pc-tools-rs")||r.URL.RawQuery!="" { return nil,errors.New("bad endpoint") }
        key:=strings.TrimPrefix(r.URL.Path,"/repos/AutoDevJapan/cc-pc-tools-rs")
        body,ok:=f.endpoints[key]
        status:=http.StatusOK;if !ok {status=http.StatusNotFound}
        return &http.Response{StatusCode:status,Header:make(http.Header),Body:io.NopCloser(bytes.NewReader(body)),Request:r},nil
    })}
    return f
}

func TestGHAGuardGitSnapshotRejectsUnboundInputs(t *testing.T) {
    for _, fault := range []string{"none","wrong-repo","wrong-commit","wrong-tree","tree-tamper","truncated","missing-tree-list","missing","extra","duplicate","wrong-mode","wrong-blob","wrong-size","wrong-sha256","bad-blob-body","oversize","cancelled"} {
        t.Run(fault,func(t *testing.T){
            fixtureMode:="";if fault=="wrong-mode" {fixtureMode="bad-input-mode"}
            f:=ghaGuardTreeTestFixtureFor(t,fixtureMode)
            records:=append([]ghaGuardSourceRecord(nil),f.records...)
            switch fault {
            case "wrong-repo":f.endpoints[""]=[]byte(`{"id":1}`)
            case "wrong-commit":f.endpoints["/git/commits/"+f.commit]=[]byte(fmt.Sprintf(`{"sha":%q,"tree":{"sha":%q}}`,strings.Repeat("f",40),f.tree))
            case "wrong-tree":f.endpoints["/git/commits/"+f.commit]=[]byte(fmt.Sprintf(`{"sha":%q,"tree":{"sha":%q}}`,f.commit,strings.Repeat("f",40)))
            case "tree-tamper":f.endpoints["/git/trees/"+f.tree]=bytes.Replace(f.endpoints["/git/trees/"+f.tree],[]byte("repair-guard"),[]byte("repair-guarb"),1)
            case "truncated":f.endpoints["/git/trees/"+f.tree]=bytes.Replace(f.endpoints["/git/trees/"+f.tree],[]byte(`"Truncated":false`),[]byte(`"Truncated":true`),1)
            case "missing-tree-list":f.endpoints["/git/trees/"+f.tree]=[]byte(fmt.Sprintf(`{"sha":%q,"truncated":false}`,f.tree))
            case "missing":records=records[:len(records)-1]
            case "extra":records=append(records,ghaGuardSourceRecord{Path:"tools/civ6-ci-contract/missing.go",Size:1,SHA:strings.Repeat("a",64),Blob:strings.Repeat("b",40)})
            case "duplicate":records=append(records,records[len(records)-1])
            case "wrong-blob":records[0].Blob=strings.Repeat("b",40)
            case "wrong-size":records[0].Size++
            case "wrong-sha256":records[0].SHA=strings.Repeat("a",64)
            case "bad-blob-body":f.endpoints["/git/blobs/"+records[0].Blob]=[]byte(strings.Repeat("x",int(records[0].Size)))
            case "oversize":records[0].Size=ghaGuardGitObjectLimit+1
            }
            ctx:=context.Background();if fault=="cancelled" {var cancel context.CancelFunc;ctx,cancel=context.WithCancel(ctx);cancel()}
            err:=ghaGuardVerifyGitSnapshot(ctx,f.client,f.commit,f.tree,records)
            if fault=="none" {
                if err!=nil||f.blobRequests.Load()!=int64(len(records)) {t.Fatal("valid tree/bytes rejected",err,f.blobRequests.Load())}
            } else if err==nil {t.Fatal("unbound input accepted",fault)}
        })
    }
}

func TestGHAGuardGitSnapshotMidReadCancellation(t *testing.T) {
    f:=ghaGuardTreeTestFixtureFor(t,"")
    base:=f.client.Transport
    ctx,cancel:=context.WithCancel(context.Background())
    f.client.Transport=ghaGuardTreeTestTransport(func(r *http.Request)(*http.Response,error){
        if strings.Contains(r.URL.Path,"/git/blobs/") {cancel();return nil,context.Canceled}
        return base.RoundTrip(r)
    })
    defer cancel()
    if ghaGuardVerifyGitSnapshot(ctx,f.client,f.commit,f.tree,f.records)==nil {t.Fatal("cancelled blob fetch accepted")}
}
