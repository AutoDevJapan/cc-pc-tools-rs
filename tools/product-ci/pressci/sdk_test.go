package pressci

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func TestPublicSDKSelectorsRejectRequestsAndChangedBytes(t *testing.T) {
	for _, name := range []string{"rustup-windows", "chrome-windows", "chrome-linux-key"} {
		a, ok := PublicSDKAsset(name)
		u, e := url.Parse(a.URL)
		if !ok || e != nil || u.Scheme != "https" || u.User != nil || a.Bytes < 1 || len(a.SHA256) != 64 {
			t.Fatal("public SDK pin missing")
		}
		if VerifySDKAsset(name, []byte("changed public artifact")) == nil {
			t.Fatal("modified artifact accepted")
		}
	}
	if _, ok := PublicSDKAsset("https://untrusted.invalid/tool"); ok {
		t.Fatal("URL used as selector")
	}
	if _, e := FetchSDKAsset(context.Background(), "unknown"); e == nil {
		t.Fatal("unknown selector reached downloader")
	}
}

// Opt-in input is restricted to the fixed public files downloaded for this
// task, never credentials or product data. Also proves the fixed pins are usable.
func TestDownloadedPublicSDKBytesMatchCompiledPins(t *testing.T) {
	root := os.Getenv("CC2766_PUBLIC_SDK_ROOT")
	if root == "" {
		t.Skip("public SDK files not supplied")
	}
	for name, file := range map[string]string{"rustup-windows": "rustup-init.exe", "chrome-windows": "googlechromestandaloneenterprise64.msi", "chrome-linux-key": "google-signing-key.pub"} {
		b, e := os.ReadFile(filepath.Join(root, file))
		if e != nil {
			t.Fatal(e)
		}
		if e = VerifySDKAsset(name, b); e != nil {
			t.Fatal(name, e)
		}
		b[len(b)-1] ^= 1
		if VerifySDKAsset(name, b) == nil {
			t.Fatal("last-byte corruption accepted")
		}
	}
}
