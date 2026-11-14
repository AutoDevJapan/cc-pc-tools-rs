//go:build windows

package main

import (
    "archive/zip"
    "bytes"
    "context"
    "crypto/sha1"
    "encoding/hex"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "sort"
    "strings"
    "sync"
    "unicode/utf8"
)

// This verifier only checks the selected fixed source against Git objects. It
// does not grant access to the installed target or replace run/attestation checks.
const ghaGuardGitAPI = "https://api.github.com/repos/AutoDevJapan/cc-pc-tools-rs"
const ghaGuardGitTreeResponseLimit = 4 << 20
const ghaGuardGitObjectLimit = 8 << 20
const ghaGuardGitTotalLimit = 64 << 20

type ghaGuardGitTreeEntry struct {
    Path string
    Mode string
    Type string
    SHA string
    Size *int64
}

type ghaGuardGitTreeReply struct {
    SHA string
    Truncated *bool
    Tree *[]ghaGuardGitTreeEntry
}

// client is supplied only by the future code-owned authenticated transport.
// Endpoint paths are constructed here from fixed repository and object IDs.
func ghaGuardGitGet(ctx context.Context, client *http.Client, endpoint, accept string, limit int64) ([]byte, error) {
    if ctx == nil || ctx.Err() != nil || client == nil || limit < 0 || endpoint != ghaGuardGitAPI && !strings.HasPrefix(endpoint, ghaGuardGitAPI+"/") {
        return nil, errGHAGuardUnqualified
    }
    request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
    if err != nil { return nil, errGHAGuardUnqualified }
    request.Header.Set("Accept", accept)
    request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
    bounded := *client
    bounded.CheckRedirect = func(*http.Request, []*http.Request) error { return errGHAGuardUnqualified }
    response, err := bounded.Do(request)
    if err != nil || response == nil || response.Body == nil {
        if response != nil && response.Body != nil { _ = response.Body.Close() }
        return nil, errGHAGuardUnqualified
    }
    if response.StatusCode != http.StatusOK || response.Header.Get("Link") != "" {
        _ = response.Body.Close()
        return nil, errGHAGuardUnqualified
    }
    raw, readErr := io.ReadAll(io.LimitReader(response.Body, limit+1))
    closeErr := response.Body.Close()
    if readErr != nil || closeErr != nil || int64(len(raw)) > limit || ctx.Err() != nil {
        clear(raw)
        return nil, errGHAGuardUnqualified
    }
    return raw, nil
}

func ghaGuardGitJSON(ctx context.Context, client *http.Client, endpoint string, out any) error {
    raw, err := ghaGuardGitGet(ctx, client, endpoint, "application/vnd.github+json", ghaGuardGitTreeResponseLimit)
    if err != nil { return errGHAGuardUnqualified }
    defer clear(raw)
    dec := json.NewDecoder(bytes.NewReader(raw))
    if dec.Decode(out) != nil || dec.Decode(new(any)) != io.EOF || ctx.Err() != nil { return errGHAGuardUnqualified }
    return nil
}

func ghaGuardGitObjectID(kind string, raw []byte) string {
    h := sha1.New()
    _, _ = fmt.Fprintf(h, "%s %d%c", kind, len(raw), byte(0))
    _, _ = h.Write(raw)
    return hex.EncodeToString(h.Sum(nil))
}

// Reconstruct and hash the actual Git tree object rather than trusting a JSON
// sha field. Directory names sort as if followed by slash in the Git format.
func ghaGuardGitTree(ctx context.Context, client *http.Client, objectID string) ([]ghaGuardGitTreeEntry, error) {
    if !ghaGuardHex(objectID, 40) { return nil, errGHAGuardUnqualified }
    var reply ghaGuardGitTreeReply
    if ghaGuardGitJSON(ctx, client, ghaGuardGitAPI+"/git/trees/"+objectID, &reply) != nil || reply.SHA != objectID || reply.Truncated == nil || *reply.Truncated || reply.Tree == nil || len(*reply.Tree) > 4096 {
        return nil, errGHAGuardUnqualified
    }
    entries := append([]ghaGuardGitTreeEntry(nil), (*reply.Tree)...)
    names := make(map[string]bool, len(entries))
    for _, entry := range entries {
        if ctx.Err() != nil || entry.Path == "" || entry.Path == "." || entry.Path == ".." || !utf8.ValidString(entry.Path) || strings.ContainsAny(entry.Path, "/\\\x00") || names[entry.Path] || !ghaGuardHex(entry.SHA, 40) {
            return nil, errGHAGuardUnqualified
        }
        names[entry.Path] = true
        valid := (entry.Mode == "040000" && entry.Type == "tree") || (entry.Mode == "100644" && entry.Type == "blob") || (entry.Mode == "100755" && entry.Type == "blob") || (entry.Mode == "120000" && entry.Type == "blob") || (entry.Mode == "160000" && entry.Type == "commit")
        if !valid { return nil, errGHAGuardUnqualified }
    }
    sort.Slice(entries, func(i, j int) bool {
        a, b := entries[i].Path, entries[j].Path
        if entries[i].Type == "tree" { a += "/" }
        if entries[j].Type == "tree" { b += "/" }
        return a < b
    })
    var wire bytes.Buffer
    for _, entry := range entries {
        mode := entry.Mode
        if mode == "040000" { mode = "40000" }
        object, err := hex.DecodeString(entry.SHA)
        if err != nil || len(object) != sha1.Size { return nil, errGHAGuardUnqualified }
        _, _ = fmt.Fprintf(&wire, "%s %s%c", mode, entry.Path, byte(0))
        _, _ = wire.Write(object)
    }
    if ghaGuardGitObjectID("tree", wire.Bytes()) != objectID || ctx.Err() != nil { return nil, errGHAGuardUnqualified }
    return entries, nil
}

func ghaGuardGitSubtree(entries []ghaGuardGitTreeEntry, name string) (string, error) {
    for _, entry := range entries {
        if entry.Path == name && entry.Mode == "040000" && entry.Type == "tree" { return entry.SHA, nil }
    }
    return "", errGHAGuardUnqualified
}

func ghaGuardVerifyGitSnapshot(ctx context.Context, client *http.Client, commitID, treeID string, records []ghaGuardSourceRecord) error {
    if ctx == nil || ctx.Err() != nil || client == nil || !ghaGuardHex(commitID, 40) || !ghaGuardHex(treeID, 40) || len(records) == 0 || len(records) > 4096 { return errGHAGuardUnqualified }
    var repo struct { ID int64 }
    if ghaGuardGitJSON(ctx, client, ghaGuardGitAPI, &repo) != nil || repo.ID != 1406818713 { return errGHAGuardUnqualified }
    var commit struct { SHA string; Tree struct { SHA string } }
    if ghaGuardGitJSON(ctx, client, ghaGuardGitAPI+"/git/commits/"+commitID, &commit) != nil || commit.SHA != commitID || commit.Tree.SHA != treeID { return errGHAGuardUnqualified }
    expected := make(map[string]ghaGuardSourceRecord, len(records))
    folded := make(map[string]bool, len(records))
    var total int64
    prior := ""
    for _, record := range records {
        if ctx.Err() != nil || !ghaGuardValidInputPath(record.Path) || strings.ContainsRune(record.Path, ':') || !utf8.ValidString(record.Path) || record.Path <= prior || record.Size < 0 || record.Size > ghaGuardGitObjectLimit || !ghaGuardHex(record.Blob, 40) || !ghaGuardHex(record.SHA, 64) || folded[strings.ToLower(record.Path)] {
            return errGHAGuardUnqualified
        }
        total += record.Size
        if total > ghaGuardGitTotalLimit { return errGHAGuardUnqualified }
        expected[record.Path] = record
        folded[strings.ToLower(record.Path)] = true
        prior = record.Path
    }
    root, err := ghaGuardGitTree(ctx, client, treeID)
    if err != nil { return errGHAGuardUnqualified }
    guardTree, err := ghaGuardGitSubtree(root, "repair-guard")
    if err != nil { return errGHAGuardUnqualified }
    toolsTree, err := ghaGuardGitSubtree(root, "tools")
    if err != nil { return errGHAGuardUnqualified }
    tools, err := ghaGuardGitTree(ctx, client, toolsTree)
    if err != nil { return errGHAGuardUnqualified }
    contractTree, err := ghaGuardGitSubtree(tools, "civ6-ci-contract")
    if err != nil { return errGHAGuardUnqualified }
    found := make(map[string]bool, len(records))
    var trees int
    var walk func(string, string, int) error
    walk = func(id, prefix string, depth int) error {
        trees++
        if ctx.Err() != nil || depth > 32 || trees > 4096 { return errGHAGuardUnqualified }
        entries, err := ghaGuardGitTree(ctx, client, id)
        if err != nil { return errGHAGuardUnqualified }
        for _, entry := range entries {
            name := prefix + "/" + entry.Path
            if entry.Type == "tree" {
                if walk(entry.SHA, name, depth+1) != nil { return errGHAGuardUnqualified }
                continue
            }
            record, ok := expected[name]
            if ctx.Err() != nil || !ok || found[name] || entry.Type != "blob" || entry.Mode != "100644" || entry.Size == nil || *entry.Size != record.Size || entry.SHA != record.Blob { return errGHAGuardUnqualified }
            found[name] = true
        }
        return nil
    }
    if walk(guardTree, "repair-guard", 0) != nil || walk(contractTree, "tools/civ6-ci-contract", 0) != nil || len(found) != len(expected) { return errGHAGuardUnqualified }
    return ghaGuardVerifyGitBlobs(ctx, client, records)
}

func ghaGuardVerifyGitBlobs(ctx context.Context, client *http.Client, records []ghaGuardSourceRecord) error {
    local, cancel := context.WithCancel(ctx)
    defer cancel()
    jobs := make(chan ghaGuardSourceRecord)
    var wg sync.WaitGroup
    var once sync.Once
    var failed bool
    for i := 0; i < 8; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            for record := range jobs {
                if local.Err() != nil { return }
                raw, err := ghaGuardGitGet(local, client, ghaGuardGitAPI+"/git/blobs/"+record.Blob, "application/vnd.github.raw+json", record.Size)
                valid := err == nil && int64(len(raw)) == record.Size && ghaGuardGitObjectID("blob", raw) == record.Blob && digest(raw) == record.SHA && local.Err() == nil
                clear(raw)
                if !valid { once.Do(func(){ failed = true; cancel() }); return }
            }
        }()
    }
    send := true
    for _, record := range records {
        if !send { break }
        if local.Err() != nil { send = false; break }
        select {
        case jobs <- record:
        case <-local.Done(): send = false
        }
    }
    close(jobs)
    wg.Wait()
    if failed || ctx.Err() != nil || !send { return errGHAGuardUnqualified }
    return nil
}

// The archive is already checked by the caller; its proof is independently
// parsed and bound again before its selected inputs are compared with Git.
func ghaGuardVerifyArchiveGitSource(ctx context.Context, archive []byte, manifest ghaGuardManifest) error {
    if ctx == nil || ctx.Err() != nil { return errGHAGuardUnqualified }
    authority := ghaGuardNormalEvidenceAuthority()
    if authority == nil || authority.client == nil { return errGHAGuardUnqualified }
    return ghaGuardVerifyArchiveGitSourceWithClient(ctx, archive, manifest, authority.client)
}

// This explicit client form is a package-test seam, and does not mint authority.
func ghaGuardVerifyArchiveGitSourceWithClient(ctx context.Context, archive []byte, manifest ghaGuardManifest, client *http.Client) error {
    if ctx == nil || ctx.Err() != nil || client == nil || manifest.RepositoryID != 1406818713 || manifest.Source != ghaGuardSource || manifest.SourceTree != ghaGuardSourceTree || len(archive) == 0 || len(archive) > ghaGuardArchiveLimit { return errGHAGuardUnqualified }
    z, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
    if err != nil || len(z.File) != 3 { return errGHAGuardUnqualified }
    var target *zip.File
    for _, f := range z.File { if f.Name == "source-proof.json" { if target != nil { return errGHAGuardUnqualified }; target = f } }
    if target == nil || target.UncompressedSize64 > 8<<20 { return errGHAGuardUnqualified }
    file, err := target.Open()
    if err != nil { return errGHAGuardUnqualified }
    raw, readErr := io.ReadAll(io.LimitReader(file, (8<<20)+1))
    closeErr := file.Close()
    if readErr != nil || closeErr != nil || len(raw) > 8<<20 || uint64(len(raw)) != target.UncompressedSize64 { clear(raw); return errGHAGuardUnqualified }
    defer clear(raw)
    if ghaGuardValidateSourceProof(ctx, raw, manifest) != nil { return errGHAGuardUnqualified }
    var proof ghaGuardSourceProof
    if json.Unmarshal(raw, &proof) != nil { return errGHAGuardUnqualified }
    var snapshot ghaGuardProofSnapshot
    if json.Unmarshal(proof.Before, &snapshot) != nil || len(snapshot.Inputs) != 249 { return errGHAGuardUnqualified }
    return ghaGuardVerifyGitSnapshot(ctx, client, ghaGuardSource, ghaGuardSourceTree, snapshot.Inputs)
}
