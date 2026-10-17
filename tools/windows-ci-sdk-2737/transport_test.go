package sdkprep

import (
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/http/httptest"
	"strings"
	"testing"
)

type testOperations struct{}

func (testOperations) Status(instance string) Result {
	return Result{Instance: instance, Stage: "ready", OK: true}
}
func (testOperations) Prepare(instance string, id PackageID) Result {
	return Result{Instance: instance, Package: id, Stage: "prepared", OK: true}
}
func (testOperations) Verify(instance string, id PackageID) Result {
	return Result{Instance: instance, Package: id, Stage: "verified", OK: true}
}

func TestEphemeralMTLSIdentityPins(t *testing.T) {
	ip := net.ParseIP("192.168.77.20")
	server, err := NewIdentity("nonce-1", ip, x509.ExtKeyUsageServerAuth)
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewIdentity("nonce-1", nil, x509.ExtKeyUsageClientAuth)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ServerTLSConfig(server, client); err != nil {
		t.Fatal(err)
	}
	if _, err := ClientTLSConfig(client, ip, CertificateHash(server.Leaf)); err != nil {
		t.Fatal(err)
	}
	if _, err := ClientTLSConfig(client, ip, strings.Repeat("0", 64)); err != nil {
		t.Fatalf("well-formed pin rejected before handshake: %v", err)
	}
	serverTLS, err := ServerTLSConfig(server, client)
	if err != nil {
		t.Fatal(err)
	}
	clientTLS, err := ClientTLSConfig(client, ip, CertificateHash(server.Leaf))
	if err != nil {
		t.Fatal(err)
	}
	clientConn, serverConn := net.Pipe()
	serverSide := tls.Server(serverConn, serverTLS)
	clientSide := tls.Client(clientConn, clientTLS)
	serverHandshake := make(chan error, 1)
	go func() { serverHandshake <- serverSide.Handshake() }()
	if err := clientSide.Handshake(); err != nil {
		t.Fatalf("pinned mTLS handshake failed: %v", err)
	}
	if err := <-serverHandshake; err != nil {
		t.Fatalf("server mTLS handshake failed: %v", err)
	}
	_ = clientSide.Close()
	_ = serverSide.Close()
	if _, err := NewIdentity("nonce-1", net.ParseIP("8.8.8.8"), x509.ExtKeyUsageServerAuth); err == nil {
		t.Fatal("public listener address accepted")
	}
	other, err := NewIdentity("nonce-2", nil, x509.ExtKeyUsageClientAuth)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ServerTLSConfig(server, other); err == nil {
		t.Fatal("cross-instance client certificate accepted")
	}
}

func TestFixedHTTPHandler(t *testing.T) {
	handler, err := NewHandler("nonce-1", testOperations{})
	if err != nil {
		t.Fatal(err)
	}
	good := httptest.NewRequest("POST", "/v1/request", strings.NewReader(`{"instance":"nonce-1","operation":"verify-versions","package":"rust-1.97.1"}`))
	good.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, good)
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"stage":"verified"`) {
		t.Fatalf("fixed request failed: status=%d", response.Code)
	}
	for _, raw := range []string{
		`{"instance":"nonce-1","operation":"exec","command":"whoami"}`,
		`{"instance":"nonce-2","operation":"status"}`,
	} {
		req := httptest.NewRequest("POST", "/v1/request", strings.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		got := httptest.NewRecorder()
		handler.ServeHTTP(got, req)
		if got.Code != 400 {
			t.Errorf("invalid request accepted with status %d", got.Code)
		}
	}
	wrongPath := httptest.NewRequest("POST", "/anything", strings.NewReader(`{}`))
	got := httptest.NewRecorder()
	handler.ServeHTTP(got, wrongPath)
	if got.Code != 404 {
		t.Fatalf("unexpected endpoint accepted: status=%d", got.Code)
	}
}
