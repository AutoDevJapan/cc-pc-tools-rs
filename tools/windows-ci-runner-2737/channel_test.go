package windowsrunner

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	sdk "github.com/autodevjapan/cc-pc-tools-rs/tools/windows-ci-sdk-2737"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRealMTLSChannelSingleUseAndWrongClientPin(t *testing.T) {
	instance := testSeed().Plan.Instance
	ip := net.ParseIP("192.168.77.20")
	server, e := sdk.NewIdentity(instance, ip, x509.ExtKeyUsageServerAuth)
	if e != nil {
		t.Fatal(e)
	}
	client, e := sdk.NewHostIdentity(instance)
	if e != nil {
		t.Fatal(e)
	}
	serverConfig, e := sdk.ServerTLSConfig(server, sdk.CertificateHash(client.Leaf))
	if e != nil {
		t.Fatal(e)
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	s := &channelServer{run: func(ctx context.Context, c net.Conn) error {
		close(entered)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}}
	web := httptest.NewUnstartedServer(s)
	web.TLS = serverConfig
	web.StartTLS()
	defer web.Close()
	connect := func(identity sdk.Identity) (net.Conn, *http.Response, error) {
		config, e := sdk.ClientTLSConfig(identity, ip, sdk.CertificateHash(server.Leaf))
		if e != nil {
			return nil, nil, e
		}
		c, e := tls.Dial("tcp4", web.Listener.Addr().String(), config)
		if e != nil {
			return nil, nil, e
		}
		c.SetDeadline(time.Now().Add(3 * time.Second))
		r, _ := http.NewRequest("POST", web.URL+channelPath, nil)
		r.Header.Set("Connection", "Upgrade")
		r.Header.Set("Upgrade", upgrade)
		if e = r.Write(c); e != nil {
			c.Close()
			return nil, nil, e
		}
		reply, e := http.ReadResponse(bufio.NewReader(c), r)
		return c, reply, e
	}
	wrong, e := sdk.NewHostIdentity(instance)
	if e != nil {
		t.Fatal(e)
	}
	c, r, e := connect(wrong)
	if c != nil {
		c.Close()
	}
	if e == nil && r.StatusCode == 101 {
		t.Fatal("wrong client admitted")
	}
	c, r, e = connect(client)
	if e != nil || r.StatusCode != 101 {
		t.Fatalf("valid client: %v", e)
	}
	defer c.Close()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("session did not enter")
	}
	second, r, e := connect(client)
	if second != nil {
		defer second.Close()
	}
	if e != nil || r.StatusCode != 409 {
		t.Fatal("reentry accepted", e)
	}
	close(release)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if e = s.stop(ctx); e != nil {
		t.Fatal(e)
	}
}

func TestServiceStopAwaitsHijackedSessionDrain(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	started := make(chan struct{})
	release := make(chan struct{})
	s := &channelServer{used: true, done: done, cancel: cancel}
	go func() { <-ctx.Done(); close(started); <-release; close(done) }()
	stopped := make(chan error, 1)
	budget, end := context.WithTimeout(context.Background(), 2*time.Second)
	defer end()
	go func() { stopped <- s.stop(budget) }()
	<-started
	select {
	case <-stopped:
		t.Fatal("service stop preceded drain")
	default:
	}
	close(release)
	if e := <-stopped; e != nil {
		t.Fatal(fmt.Sprint(e))
	}
}
