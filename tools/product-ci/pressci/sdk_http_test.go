package pressci

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"strings"
	"testing"
)

type sdkRoundTripFunc func(*http.Request) (*http.Response, error)

func (f sdkRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestSDKDownloadRejectsRedirectStatusDigestAndOversizedBody(t *testing.T) {
	b := "artifact"
	h := sha256.Sum256([]byte(b))
	a := SDKAsset{URL: "https://dl.google.com/fixed", SHA256: hex.EncodeToString(h[:]), Bytes: int64(len(b))}
	transport := sdkTransport()
	defer transport.CloseIdleConnections()
	if transport.Proxy != nil {
		t.Fatal("environment proxy enabled")
	}
	for _, tc := range []struct {
		name, redirect, body string
		status               int
		wantOK               bool
		wantRequests         int
	}{
		{"fixed", "", b, 200, true, 1},
		{"same-host", "https://dl.google.com/next", b, 200, true, 2},
		{"different-host", "https://other.invalid/next", b, 200, false, 1},
		{"http", "http://dl.google.com/next", b, 200, false, 1},
		{"userinfo", "https://user@dl.google.com/next", b, 200, false, 1},
		{"non200", "", b, 403, false, 1},
		{"corruption", "", "artifacX", 200, false, 1},
		{"oversized", "", b + "extra", 200, false, 1},
		{"short", "", "short", 200, false, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := 0
			rt := sdkRoundTripFunc(func(r *http.Request) (*http.Response, error) {
				requests++
				if r.Header.Get("Authorization") != "" || r.Method != http.MethodGet {
					t.Fatal("credential or wrong method reached downloader")
				}
				resp := &http.Response{StatusCode: tc.status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(tc.body)), Request: r}
				if requests == 1 && tc.redirect != "" {
					resp.StatusCode = 302
					resp.Header.Set("Location", tc.redirect)
				}
				return resp, nil
			})
			out, e := fetchPinnedSDK(context.Background(), a, rt)
			if (e == nil) != tc.wantOK || requests != tc.wantRequests || tc.wantOK && string(out) != b {
				t.Fatalf("download policy: err=%v requests=%d", e, requests)
			}
		})
	}
	requests := 0
	_, e := fetchPinnedSDK(context.Background(), a, sdkRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		requests++
		resp := &http.Response{StatusCode: 302, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("")), Request: r}
		resp.Header.Set("Location", "https://dl.google.com/again")
		return resp, nil
	}))
	if e == nil || requests != 4 {
		t.Fatal("redirect loop escaped fixed cap")
	}
}
