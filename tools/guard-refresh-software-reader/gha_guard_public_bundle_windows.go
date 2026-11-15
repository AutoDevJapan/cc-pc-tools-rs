//go:build windows

package main

import (
    "bytes"
    "context"
    "encoding/json"
    "io"
    "net/http"
    "net/url"
    "strings"
    "time"
)

const ghaGuardPublicAttestationURL = "https://api.github.com/repos/AutoDevJapan/cc-pc-tools-rs/attestations/sha256:a61e25065ae35d51f1ac8133959176b21e2c3740c1516b76f799371f1c27f9a2?per_page=100"
const ghaGuardPublicBundleSize = 11619
const ghaGuardPublicBundleSHA = "c7c63a9adc5d342674e61116f732f4fd6dd1b82a8a039228ce80e9ff5fee85e1"

// The returned bytes are untrusted signature input, never an issuer or grant.
// Production owns the transport: no caller URL, token, trust root or redirects.
func ghaGuardFetchPinnedPublicBundle(ctx context.Context) ([]byte, error) {
    if ctx == nil || ctx.Err() != nil { return nil, errGHAGuardUnqualified }
    tr := &http.Transport{Proxy:nil, TLSHandshakeTimeout:10*time.Second, ResponseHeaderTimeout:15*time.Second, MaxResponseHeaderBytes:64<<10}
    defer tr.CloseIdleConnections()
    return ghaGuardReadPinnedPublicBundle(ctx, tr)
}

// The transport seam supplies bytes only; it cannot construct a candidate.
func ghaGuardReadPinnedPublicBundle(ctx context.Context, tr http.RoundTripper) ([]byte, error) {
    if ctx == nil || ctx.Err() != nil || tr == nil { return nil, errGHAGuardUnqualified }
    bounded, cancel := context.WithTimeout(ctx, 30*time.Second)
    defer cancel()
    req, err := http.NewRequestWithContext(bounded, http.MethodGet, ghaGuardPublicAttestationURL, nil)
    if err != nil { return nil, errGHAGuardUnqualified }
    req.Header.Set("Accept", "application/vnd.github+json")
    req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
    client := &http.Client{Transport:tr, Timeout:30*time.Second, CheckRedirect:func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
    resp, err := client.Do(req)
    if err != nil {
        if resp != nil && resp.Body != nil { _ = resp.Body.Close() }
        return nil, errGHAGuardUnqualified
    }
    if resp == nil || resp.Body == nil { return nil, errGHAGuardUnqualified }
    if resp.StatusCode != http.StatusOK || !ghaGuardSinglePageLink(resp.Header) {
        _ = resp.Body.Close()
        return nil, errGHAGuardUnqualified
    }
    raw, readErr := io.ReadAll(io.LimitReader(resp.Body, (4<<20)+1))
    closeErr := resp.Body.Close()
    defer clear(raw)
    if readErr != nil || closeErr != nil || len(raw) > 4<<20 || bounded.Err() != nil { return nil, errGHAGuardUnqualified }
    var envelope struct {
        Attestations []struct {
            RepositoryID int64 `json:"repository_id"`
            BundleURL string `json:"bundle_url"`
            Bundle json.RawMessage `json:"bundle"`
        } `json:"attestations"`
    }
    dec := json.NewDecoder(bytes.NewReader(raw))
    if dec.Decode(&envelope) != nil || dec.Decode(new(any)) != io.EOF { return nil, errGHAGuardUnqualified }
    var found json.RawMessage
    matches := 0
    for _, item := range envelope.Attestations {
        u, parseErr := url.Parse(item.BundleURL)
        if parseErr != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || item.RepositoryID != 1406818713 || !strings.HasSuffix(u.Path, "/"+ghaGuardFixedAttestationID+".json.sn") { continue }
        found = item.Bundle
        matches++
    }
    if matches != 1 || len(found) == 0 { return nil, errGHAGuardUnqualified }
    var compact bytes.Buffer
    if json.Compact(&compact, found) != nil { return nil, errGHAGuardUnqualified }
    defer clear(compact.Bytes())
    bundle := append([]byte(nil), compact.Bytes()...)
    if len(bundle) != ghaGuardPublicBundleSize || digest(bundle) != ghaGuardPublicBundleSHA || bounded.Err() != nil {
        clear(bundle)
        return nil, errGHAGuardUnqualified
    }
    return bundle, nil
}
