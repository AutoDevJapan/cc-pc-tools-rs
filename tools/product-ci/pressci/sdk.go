package pressci

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/url"
	"time"
)

// Only public installers for the disposable Press guest. No URL, digest, path,
// credential or executable can be supplied by a workflow or a request frame.
type SDKAsset struct {
	URL, SHA256 string
	Bytes       int64
}

func PublicSDKAsset(name string) (SDKAsset, bool) {
	switch name {
	case "rustup-windows":
		return SDKAsset{"https://static.rust-lang.org/rustup/archive/1.28.2/x86_64-pc-windows-msvc/rustup-init.exe", "88d8258dcf6ae4f7a80c7d1088e1f36fa7025a1cfd1343731b4ee6f385121fc0", 13551616}, true
	case "chrome-windows":
		// Official Google download, Authenticode Valid / CN=Google LLC observed
		// on 2026-10-21. A changed upstream installer fails closed at this hash.
		return SDKAsset{"https://dl.google.com/dl/chrome/install/googlechromestandaloneenterprise64.msi", "a31bd8a4746069a128bf3beefdaa246c792af5f15bbf06af0259abc895c9609b", 170979328}, true
	case "chrome-linux-key":
		return SDKAsset{"https://dl.google.com/linux/linux_signing_key.pub", "54dea5f6c2a26091578cf52a999cebc6b64df478d37ad4dce96376b711e3b27c", 19640}, true
	}
	return SDKAsset{}, false
}

func VerifySDKAsset(name string, b []byte) error {
	a, ok := PublicSDKAsset(name)
	h := sha256.Sum256(b)
	if !ok || int64(len(b)) != a.Bytes || hex.EncodeToString(h[:]) != a.SHA256 {
		return errors.New("public Press SDK artifact rejected")
	}
	return nil
}

func FetchSDKAsset(ctx context.Context, name string) ([]byte, error) {
	a, ok := PublicSDKAsset(name)
	if !ok {
		return nil, errors.New("public Press SDK selector rejected")
	}
	transport := &http.Transport{Proxy: nil, TLSHandshakeTimeout: 15 * time.Second, ResponseHeaderTimeout: 30 * time.Second}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 3 * time.Minute, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		u, _ := url.Parse(a.URL)
		if len(via) > 3 || r.URL.Scheme != "https" || r.URL.Host != u.Host || r.URL.User != nil {
			return errors.New("public SDK redirect rejected")
		}
		return nil
	}}
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, a.URL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(r)
	if err != nil {
		return nil, errors.New("public Press SDK download failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("public Press SDK download status rejected")
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, a.Bytes+1))
	if err != nil || VerifySDKAsset(name, b) != nil {
		return nil, errors.New("public Press SDK bytes rejected")
	}
	return b, nil
}
