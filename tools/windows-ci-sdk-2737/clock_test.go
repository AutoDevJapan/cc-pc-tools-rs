package sdkprep

import (
	"crypto/x509"
	"net"
	"testing"
	"time"
)

func TestFreshPinnedMTLSDoesNotDependOnSetupGuestClock(t *testing.T) {
	ip := net.IPv4(172, 25, 10, 2)
	client, err := newIdentityAt("clock-test", nil, x509.ExtKeyUsageClientAuth, time.Now().Add(-16*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	defer clearIdentity(&client)
	server, err := newIdentityAt("clock-test", ip, x509.ExtKeyUsageServerAuth, time.Now().Add(16*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	defer clearIdentity(&server)
	clientTLS, err := ClientTLSConfig(client, ip, CertificateHash(server.Leaf))
	if err != nil {
		t.Fatal(err)
	}
	if err := clientTLS.VerifyPeerCertificate(server.Certificate.Certificate, nil); err != nil {
		t.Fatal(err)
	}
	serverTLS, err := ServerTLSConfig(server, CertificateHash(client.Leaf))
	if err != nil {
		t.Fatal(err)
	}
	if err := serverTLS.VerifyPeerCertificate(client.Certificate.Certificate, nil); err != nil {
		t.Fatal(err)
	}
	other, err := NewHostIdentity("clock-test")
	if err != nil {
		t.Fatal(err)
	}
	defer clearIdentity(&other)
	if err := serverTLS.VerifyPeerCertificate(other.Certificate.Certificate, nil); err == nil {
		t.Fatal("a different fresh key must still be rejected")
	}
}
