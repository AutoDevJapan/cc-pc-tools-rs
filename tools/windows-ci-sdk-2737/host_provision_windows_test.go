//go:build windows && amd64

package sdkprep

import (
	"errors"
	"net"
	"net/url"
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
