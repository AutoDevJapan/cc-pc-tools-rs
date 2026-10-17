//go:build windows && amd64

package sdkprep

import (
	"errors"
	"net"
	"net/url"
	"strings"
	"testing"
)

func TestClassifyNetworkFailureDoesNotExposeRawTransportDetails(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"dns", &net.DNSError{Err: "no such host", IsNotFound: true}, "name-resolution"},
		{"proxy", &url.Error{Op: "Get", URL: "https://aka.ms/", Err: errors.New("proxyconnect tcp: private proxy endpoint")}, "proxy-connect"},
		{"connection", &url.Error{Op: "Get", URL: "https://aka.ms/", Err: errors.New("dial tcp: connection refused")}, "connection-refused"},
		{"unknown", &url.Error{Op: "Get", URL: "https://aka.ms/", Err: errors.New("unexpected transport detail")}, "network-or-protocol"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyNetworkFailure(tc.err); got != tc.want {
				t.Fatalf("classification=%q want=%q", got, tc.want)
			}
		})
	}
}

func TestAdminBrokerScriptsEmbedTheOwnedRoot(t *testing.T) {
	root := `D:\CC\owned root's`
	for name, script := range map[string][]byte{"provision": provisionVMPS, "recovery": removeVMKeepVHDPS} {
		t.Run(name, func(t *testing.T) {
			rendered, err := renderAdminPowerShellScript(script, root)
			if err != nil {
				t.Fatal(err)
			}
			text := string(rendered)
			if strings.Contains(text, ownedRootPlaceholder) || strings.Contains(text, "$PSScriptRoot") || !strings.Contains(text, `[IO.Path]::GetFullPath('D:\CC\owned root''s')`) {
				t.Fatal("admin script did not embed the fixed owned root")
			}
		})
	}
}
