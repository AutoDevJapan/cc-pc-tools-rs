//go:build windows

package main

import (
    "archive/tar"
    "bytes"
    "compress/gzip"
    "context"
    "io"
    "net/http"
    "path"
    "strings"
    "time"
    "unicode/utf8"
)

const ghaGuardPublicSourceURL = "https://codeload.github.com/AutoDevJapan/cc-pc-tools-rs/tar.gz/80c4ed5cbd864585f4894c6286d3d5c92194c1f5"
const ghaGuardSourceArchivePrefix = "cc-pc-tools-rs-80c4ed5cbd864585f4894c6286d3d5c92194c1f5/"
const ghaGuardSourceCompressedLimit = 64 << 20
const ghaGuardSourceExpandedLimit = 128 << 20

// This obtains untrusted source bytes only; it does not issue a candidate.
func ghaGuardFetchPinnedSourceArchive(ctx context.Context) ([]byte,error) {
    if ctx == nil || ctx.Err() != nil { return nil,errGHAGuardUnqualified }
    tr := &http.Transport{Proxy:nil,TLSHandshakeTimeout:10*time.Second,ResponseHeaderTimeout:20*time.Second,MaxResponseHeaderBytes:64<<10}
    defer tr.CloseIdleConnections()
    return ghaGuardReadPinnedSourceArchive(ctx,tr)
}

// The seam can return bytes only. A candidate still needs the fixed issuer.
func ghaGuardReadPinnedSourceArchive(ctx context.Context,tr http.RoundTripper) ([]byte,error) {
    if ctx == nil || ctx.Err() != nil || tr == nil { return nil,errGHAGuardUnqualified }
    bounded,cancel := context.WithTimeout(ctx,60*time.Second); defer cancel()
    req,err := http.NewRequestWithContext(bounded,http.MethodGet,ghaGuardPublicSourceURL,nil)
    if err != nil { return nil,errGHAGuardUnqualified }
    req.Header.Set("Accept","application/octet-stream")
    client := &http.Client{Transport:tr,Timeout:60*time.Second,CheckRedirect:func(*http.Request,[]*http.Request) error {return http.ErrUseLastResponse}}
    response,err := client.Do(req)
    if err != nil { if response != nil && response.Body != nil { _ = response.Body.Close() }; return nil,errGHAGuardUnqualified }
    if response == nil || response.Body == nil { return nil,errGHAGuardUnqualified }
    if response.StatusCode != http.StatusOK { _ = response.Body.Close(); return nil,errGHAGuardUnqualified }
    raw,readErr := io.ReadAll(io.LimitReader(response.Body,ghaGuardSourceCompressedLimit+1))
    closeErr := response.Body.Close()
    if readErr != nil || closeErr != nil || len(raw)==0 || len(raw)>ghaGuardSourceCompressedLimit || bounded.Err()!=nil { clear(raw); return nil,errGHAGuardUnqualified }
    return raw,nil
}

type ghaGuardArchiveContextReader struct { ctx context.Context; reader io.Reader; count int64; emptyEOF bool }
func (r *ghaGuardArchiveContextReader) Read(p []byte) (int,error) {
    if r.ctx.Err()!=nil { return 0,errGHAGuardUnqualified }
    n,err:=r.reader.Read(p);r.count+=int64(n)
    if len(p)>0 && n==0 && err==io.EOF {r.emptyEOF=true}
    return n,err
}

type ghaGuardArchiveZeroWriter struct{}
func (ghaGuardArchiveZeroWriter) Write(p []byte) (int,error) {
    for _,b := range p { if b!=0 { return 0,errGHAGuardUnqualified } }
    return len(p),nil
}

// The caller must already validate the signed proof and the complete Git tree.
// This checks untrusted archive contents against every selected proof record.
// It neither authenticates a caller nor mints a candidate.
func ghaGuardValidatePinnedSourceArchive(ctx context.Context, raw []byte, records []ghaGuardSourceRecord) error {
    if ctx==nil || ctx.Err()!=nil || len(raw)==0 || len(raw)>ghaGuardSourceCompressedLimit || len(records)==0 || len(records)>4096 { return errGHAGuardUnqualified }
    expected:=make(map[string]ghaGuardSourceRecord,len(records))
    folded:=make(map[string]bool,len(records))
    dirs:=make(map[string]bool)
    var total int64
    prior:=""
    for _,rec:=range records {
        key:=strings.ToLower(rec.Path)
        if !ghaGuardValidInputPath(rec.Path) || strings.ContainsRune(rec.Path,':') || !utf8.ValidString(rec.Path) || rec.Path<=prior || folded[key] || rec.Size<0 || rec.Size>ghaGuardGitObjectLimit || !ghaGuardHex(rec.SHA,64) || !ghaGuardHex(rec.Blob,40) {return errGHAGuardUnqualified}
        total+=rec.Size; if total>ghaGuardGitTotalLimit {return errGHAGuardUnqualified}
        expected[rec.Path]=rec; folded[key]=true; prior=rec.Path
        for d:=path.Dir(rec.Path);d!=".";d=path.Dir(d) {dirs[d]=true}
    }
    input:=bytes.NewReader(raw)
    zr,err:=gzip.NewReader(input);if err!=nil {return errGHAGuardUnqualified}
    closed:=false;defer func(){if !closed {_=zr.Close()}}()
    zr.Multistream(false)
    counter:=&ghaGuardArchiveContextReader{ctx:ctx,reader:zr}
    limited:=&io.LimitedReader{R:counter,N:ghaGuardSourceExpandedLimit+1}
    tr:=tar.NewReader(limited)
    seen:=make(map[string]bool,len(records));seenNames:=make(map[string]bool)
    global:=false
    for {
        beforeNext:=counter.count;counter.emptyEOF=false
        h,nextErr:=tr.Next()
        if nextErr==io.EOF {
            // All previous bodies are drained. Final Next may consume only
            // previous body padding and exactly two zero headers.
            // A zero-byte EOF reveals missing headers, even after metadata.
            padding:=(512-beforeNext%512)%512
            if counter.emptyEOF || limited.N<=0 || counter.count-beforeNext!=padding+1024 {return errGHAGuardUnqualified}
            break
        }
        if nextErr!=nil || h==nil || ctx.Err()!=nil {return errGHAGuardUnqualified}
        if h.Typeflag==tar.TypeXGlobalHeader {
            if global || h.Name!="pax_global_header" || len(h.PAXRecords)!=1 || h.PAXRecords["comment"]!=ghaGuardSource {return errGHAGuardUnqualified}
            global=true;continue
        }
        if !global {return errGHAGuardUnqualified}
        if h.Name==strings.TrimSuffix(ghaGuardSourceArchivePrefix,"/") || h.Name==ghaGuardSourceArchivePrefix {
            if h.Typeflag!=tar.TypeDir || h.Size!=0 || h.Linkname!="" {return errGHAGuardUnqualified};continue
        }
        if !strings.HasPrefix(h.Name,ghaGuardSourceArchivePrefix) {return errGHAGuardUnqualified}
        rel:=strings.TrimPrefix(h.Name,ghaGuardSourceArchivePrefix)
        if h.Typeflag==tar.TypeDir {rel=strings.TrimSuffix(rel,"/")}
        if rel=="" || !utf8.ValidString(rel) || strings.Contains(rel,"\\") || strings.ContainsRune(rel,':') || path.Clean(rel)!=rel || strings.HasPrefix(rel,"/") {return errGHAGuardUnqualified}
        scoped:=rel=="repair-guard" || strings.HasPrefix(rel,"repair-guard/") || rel=="tools/civ6-ci-contract" || strings.HasPrefix(rel,"tools/civ6-ci-contract/")
        if !scoped {if _,discardErr:=io.Copy(io.Discard,tr);discardErr!=nil || ctx.Err()!=nil {return errGHAGuardUnqualified};continue}
        foldedName:=strings.ToLower(rel);if seenNames[foldedName] {return errGHAGuardUnqualified};seenNames[foldedName]=true
        if h.Typeflag==tar.TypeDir {if !dirs[rel] || h.Size!=0 || h.Linkname!="" {return errGHAGuardUnqualified};continue}
        if h.Typeflag!=tar.TypeReg || h.Linkname!="" || (h.Mode!=0644 && h.Mode!=0664) || len(h.Xattrs)!=0 {return errGHAGuardUnqualified}
        for key:=range h.PAXRecords {if strings.HasPrefix(key,"GNU.sparse") || key=="linkpath" {return errGHAGuardUnqualified}}
        rec,ok:=expected[rel];if !ok || seen[rel] || h.Size!=rec.Size {return errGHAGuardUnqualified}
        content,readErr:=io.ReadAll(io.LimitReader(tr,rec.Size+1))
        valid:=readErr==nil && int64(len(content))==rec.Size && digest(content)==rec.SHA && ghaGuardGitObjectID("blob",content)==rec.Blob && ctx.Err()==nil
        clear(content);if !valid {return errGHAGuardUnqualified};seen[rel]=true
    }
    if !global || len(seen)!=len(expected) || ctx.Err()!=nil {return errGHAGuardUnqualified}
    if _,err=io.Copy(ghaGuardArchiveZeroWriter{},limited);err!=nil || limited.N<=0 || ctx.Err()!=nil {return errGHAGuardUnqualified}
    closeErr:=zr.Close();closed=true
    if closeErr!=nil || input.Len()!=0 || ctx.Err()!=nil {return errGHAGuardUnqualified}
    return nil
}
