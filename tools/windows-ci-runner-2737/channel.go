package windowsrunner

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	sdk "github.com/autodevjapan/cc-pc-tools-rs/tools/windows-ci-sdk-2737"
	"net"
	"net/http"
	"sync"
	"time"
)

const channelPath = "/v1/windows-runner"
const upgrade = "cc-windows-runner-2737-v1"

type bufferedConn struct {
	net.Conn
	r *bufio.Reader
}

func (c *bufferedConn) Read(b []byte) (int, error) { return c.r.Read(b) }

// Dial accepts only the fresh SDK COM result and RAM identity. It never uses
// an HTTP proxy, bearer header, redirect, request-selected URL or shell.
func Dial(ctx context.Context, boot sdk.BootstrapResult, identity sdk.Identity, seed Seed) (net.Conn, error) {
	if seed.Validate() != nil || sdk.CertificateHash(identity.Leaf) != seed.ClientSHA256 || identity.Leaf.Subject.CommonName != "windows-ci-sdk-2737/"+seed.Plan.Instance {
		return nil, errors.New("channel identity rejected")
	}
	cfg, e := sdk.ClientTLSConfig(identity, boot.IPv4, boot.ServerSHA256)
	if e != nil {
		return nil, e
	}
	d := tls.Dialer{Config: cfg, NetDialer: &net.Dialer{Timeout: 15 * time.Second}}
	c, e := d.DialContext(ctx, "tcp4", net.JoinHostPort(boot.IPv4.To4().String(), fmt.Sprint(sdk.ServicePort)))
	if e != nil {
		return nil, errors.New("Windows channel connection failed")
	}
	_ = c.SetDeadline(time.Now().Add(15 * time.Second))
	request, _ := http.NewRequest(http.MethodPost, "https://"+c.RemoteAddr().String()+channelPath, nil)
	request.Header.Set("Connection", "Upgrade")
	request.Header.Set("Upgrade", upgrade)
	if e = request.Write(c); e != nil {
		c.Close()
		return nil, errors.New("Windows channel upgrade failed")
	}
	r := bufio.NewReader(c)
	response, e := http.ReadResponse(r, request)
	if e != nil || response.StatusCode != http.StatusSwitchingProtocols || response.Header.Get("Upgrade") != upgrade {
		c.Close()
		return nil, errors.New("Windows channel rejected")
	}
	_ = c.SetDeadline(time.Time{})
	return &bufferedConn{c, r}, nil
}

// channelServer permits one TLS-authorized session per clone, including failed
// sessions. Stop closes the channel and waits for the entire guest operation.
type channelServer struct {
	mu            sync.Mutex
	used, stopped bool
	conn          net.Conn
	done          chan struct{}
	run           func(context.Context, net.Conn) error
	cancel        context.CancelFunc
	result        error
}

func (s *channelServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.URL.Path != channelPath || r.Header.Get("Upgrade") != upgrade || r.TLS == nil || len(r.TLS.PeerCertificates) != 1 || r.ContentLength != 0 {
		http.Error(w, "rejected", 400)
		return
	}
	s.mu.Lock()
	if s.used || s.stopped {
		s.mu.Unlock()
		http.Error(w, "consumed", 409)
		return
	}
	h, ok := w.(http.Hijacker)
	if !ok {
		s.mu.Unlock()
		http.Error(w, "unavailable", 500)
		return
	}
	c, rw, e := h.Hijack()
	if e != nil {
		s.mu.Unlock()
		return
	}
	s.used = true
	s.conn = c
	s.done = make(chan struct{})
	deadline, end := context.WithTimeout(context.Background(), 26*time.Minute)
	ctx, cancel := context.WithCancelCause(deadline)
	s.cancel = func() { cancel(ErrServiceStop) }
	s.result = errors.New("Windows channel upgrade did not enter session")
	s.mu.Unlock()
	defer close(s.done)
	defer end()
	defer cancel(context.Canceled)
	defer c.Close()
	_ = c.SetDeadline(time.Time{})
	if _, e = fmt.Fprintf(rw, "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: %s\r\n\r\n", upgrade); e != nil {
		return
	}
	if rw.Flush() != nil {
		return
	}
	err := s.run(ctx, &bufferedConn{c, rw.Reader})
	s.mu.Lock()
	s.result = err
	s.mu.Unlock()
}
func (s *channelServer) stop(ctx context.Context) error {
	s.mu.Lock()
	s.stopped = true
	c, done, cancel := s.conn, s.done, s.cancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if c != nil {
		c.Close()
	}
	if done == nil {
		return nil
	}
	select {
	case <-done:
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.result
	case <-ctx.Done():
		return errors.New("Windows session drain not confirmed")
	}
}
