package main

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"
)

func TestSDKFramesKeepBinaryBytesAndRejectOversizeAndTruncation(t *testing.T) {
	want := []byte{0, 10, 13, 255, 1, 2}
	var b bytes.Buffer
	if e := writeBlob(&b, want); e != nil {
		t.Fatal(e)
	}
	got, e := readBlob(&b, 32)
	if e != nil || !bytes.Equal(want, got) {
		t.Fatal("SDK binary frame changed")
	}
	var oversized bytes.Buffer
	_ = binary.Write(&oversized, binary.BigEndian, uint64(33))
	if _, e = readBlob(&oversized, 32); e == nil {
		t.Fatal("SDK oversized frame accepted")
	}
	var truncated bytes.Buffer
	_ = binary.Write(&truncated, binary.BigEndian, uint64(4))
	truncated.WriteByte(1)
	if _, e = readBlob(&truncated, 32); e == nil {
		t.Fatal("SDK truncated frame accepted")
	}
}

func TestPinnedMutualTLSRejectsAnUnrelatedClientAndServer(t *testing.T) {
	server, e := newIdentity()
	if e != nil {
		t.Fatal(e)
	}
	client, e := newIdentity()
	if e != nil {
		t.Fatal(e)
	}
	foreign, e := newIdentity()
	if e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		name      string
		client    identity
		serverPin string
		ok        bool
	}{{"same-owned-session", client, server.Digest, true}, {"foreign-client", foreign, server.Digest, false}, {"foreign-server", client, foreign.Digest, false}} {
		t.Run(tc.name, func(t *testing.T) {
			a, b := net.Pipe()
			defer a.Close()
			defer b.Close()
			_ = a.SetDeadline(time.Now().Add(2 * time.Second))
			_ = b.SetDeadline(time.Now().Add(2 * time.Second))
			srv := tls.Server(a, serverTLS(server, client.Digest))
			cli := tls.Client(b, clientTLS(tc.client, tc.serverPin))
			done := make(chan error, 1)
			go func() { done <- srv.Handshake() }()
			ce := cli.Handshake()
			se := <-done
			if (ce == nil && se == nil) != tc.ok {
				t.Fatal("TLS identity boundary failed")
			}
		})
	}
	cert, e := x509.ParseCertificate(server.Certificate.Certificate[0])
	if e != nil {
		t.Fatal(e)
	}
	if verifyPeer(server.Digest)(tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert, cert}}) == nil {
		t.Fatal("extra certificate accepted")
	}
}

func TestReadinessBindsTheSpecificVMAndPrivateIPv4(t *testing.T) {
	instance := strings.Repeat("a", 32)
	r := ready{instance, "172.20.5.3", guestPort, strings.Repeat("b", 64)}
	if validateReady(r, instance) != nil {
		t.Fatal("owned readiness rejected")
	}
	for _, fault := range []string{"instance", "public-ip", "loopback", "port", "certificate"} {
		t.Run(fault, func(t *testing.T) {
			v := r
			switch fault {
			case "instance":
				v.Instance = strings.Repeat("c", 32)
			case "public-ip":
				v.IP = "1.1.1.1"
			case "loopback":
				v.IP = "127.0.0.1"
			case "port":
				v.Port++
			case "certificate":
				v.Certificate = "invalid"
			}
			if validateReady(v, instance) == nil {
				t.Fatal("unrelated readiness accepted")
			}
		})
	}
}

func TestRequestsAdmitOnlyFixtureOperations(t *testing.T) {
	id := strings.Repeat("c", 32)
	for _, raw := range []string{`{"kind":"shell","id":"` + id + `"}`, `{"kind":"normal","id":"` + id + `","command":"not allowed"}`, `{"kind":"normal","id":"../escape"}`} {
		if _, e := decodeRequest(json.NewDecoder(strings.NewReader(raw))); e == nil {
			t.Fatal("request-selected command or path accepted")
		}
	}
}
