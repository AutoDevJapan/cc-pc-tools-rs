package main

import (
	"bufio"
	"bytes"
	sourcepacket "cc/ci-source-2737"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"
)

type guest struct {
	mu                   sync.Mutex
	active               bool
	results              map[string]fixtureResult
	instance             string
	prepared, configured bool
	sourceTouched        bool
	sdkAttempted         bool
	source               sourcepacket.Pin
}

func (g *guest) result(r fixtureResult) { g.mu.Lock(); g.results[r.ID] = r; g.mu.Unlock() }
func groupGone(p int) bool              { return p > 1 && errors.Is(syscall.Kill(-p, 0), syscall.ESRCH) }

// Caller holds mu. An attempted source upload is conservatively source-bearing.
func (g *guest) sdkRejectionLocked() sdkResult {
	return sdkResult{ID: g.instance, Failure: "sdk_guest_state_rejected", ProductSource: g.sourceTouched || g.prepared, RunnerRegistered: g.configured}
}
func (g *guest) serve(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(time.Minute))
	reader := bufio.NewReaderSize(conn, 4096)
	header, e := reader.ReadSlice('\n')
	if e != nil {
		return
	}
	d := json.NewDecoder(bytes.NewReader(header))
	q, e := decodeRequest(d)
	if e != nil || d.Decode(new(any)) != io.EOF {
		return
	}
	// Admission and each operation's state transition share one critical
	// section. Preparing an SDK consumes this guest, even on success: only a
	// fresh cold clone may subsequently probe it or receive product work.
	g.mu.Lock()
	if g.sdkAttempted {
		rejected := g.sdkRejectionLocked()
		g.mu.Unlock()
		if q.Kind == "prepare-sdk" {
			_ = json.NewEncoder(conn).Encode(rejected)
		}
		return
	}
	if q.Kind == "live-job" {
		if g.active || g.prepared || g.configured || q.ID != g.instance {
			g.mu.Unlock()
			return
		}
		g.active = true
		g.prepared = true
		g.configured = true
		g.sourceTouched = true
		g.mu.Unlock()
		defer func() { g.mu.Lock(); g.active = false; g.mu.Unlock() }()
		_ = conn.SetDeadline(time.Now().Add(40 * time.Minute))
		serveLiveJob(conn, reader, q.ID, g.source)
		return
	}
	if q.Kind == "probe-public-sdk" {
		if g.active || g.prepared || g.configured || g.sourceTouched || q.ID != g.instance {
			g.mu.Unlock()
			return
		}
		g.active = true
		g.mu.Unlock()
		defer func() { g.mu.Lock(); g.active = false; g.mu.Unlock() }()
		_ = conn.SetDeadline(time.Now().Add(90 * time.Second))
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		_ = json.NewEncoder(conn).Encode(publicSDKProbe{ID: q.ID, Rust: probeRustAt(ctx, "/home/ci2737"), Driver: probeSDKDriver(ctx)})
		return
	}
	if q.Kind == "materialize-source" {
		if g.active || q.ID != g.instance {
			g.mu.Unlock()
			return
		}
		g.active = true
		g.sourceTouched = true
		g.mu.Unlock()
		defer func() { g.mu.Lock(); g.active = false; g.mu.Unlock() }()
		_ = conn.SetDeadline(time.Now().Add(10 * time.Minute))
		result := materializeSource(reader, q.ID)
		if result.Failure == "" {
			g.mu.Lock()
			g.prepared = true
			g.mu.Unlock()
		}
		_ = json.NewEncoder(conn).Encode(result)
		return
	}
	if q.Kind == "configure-offline" {
		if g.active || !g.prepared || g.configured || q.ID != g.instance {
			g.mu.Unlock()
			return
		}
		g.active = true
		g.configured = true
		g.mu.Unlock()
		defer func() { g.mu.Lock(); g.active = false; g.mu.Unlock() }()
		_ = conn.SetDeadline(time.Now().Add(3 * time.Minute))
		_ = json.NewEncoder(conn).Encode(configureOffline(conn, reader, q.ID))
		return
	}
	if q.Kind == "prepare-sdk" {
		rejected := g.sdkRejectionLocked()
		if g.active || q.ID != g.instance || g.prepared || g.configured || g.sourceTouched {
			g.mu.Unlock()
			_ = json.NewEncoder(conn).Encode(rejected)
			return
		}
		g.active = true
		g.sdkAttempted = true
		g.mu.Unlock()
		defer func() { g.mu.Lock(); g.active = false; g.mu.Unlock() }()
		_ = conn.SetDeadline(time.Now().Add(15 * time.Minute))
		result := prepareSDK(conn, reader, q.ID)
		g.mu.Lock()
		result.ProductSource = g.sourceTouched || g.prepared
		result.RunnerRegistered = g.configured
		g.mu.Unlock()
		_ = json.NewEncoder(conn).Encode(result)
		return
	}
	d = json.NewDecoder(io.LimitReader(reader, 4096))
	if q.Kind == "status" {
		r, ok := g.results[q.ID]
		g.mu.Unlock()
		if ok {
			_ = json.NewEncoder(conn).Encode(r)
		}
		return
	}
	if q.Kind != "normal" && q.Kind != "cancel" {
		g.mu.Unlock()
		return
	}
	_, used := g.results[q.ID]
	if g.active || used || len(g.results) >= 2 {
		g.mu.Unlock()
		return
	}
	g.active = true
	g.results[q.ID] = fixtureResult{ID: q.ID}
	g.mu.Unlock()
	defer func() { g.mu.Lock(); g.active = false; g.mu.Unlock() }()
	r := fixtureResult{ID: q.ID}
	defer func() { r.Done = true; g.result(r) }()
	exe, e := os.Executable()
	if e != nil {
		r.Failure = "guest executable unavailable"
		return
	}
	c := exec.Command(exe, "fixture-parent")
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	c.WaitDelay = 5 * time.Second
	out, e := c.StdoutPipe()
	if e != nil {
		r.Failure = "fixture output unavailable"
		return
	}
	in, e := c.StdinPipe()
	if e != nil {
		r.Failure = "fixture input unavailable"
		return
	}
	if c.Start() != nil {
		r.Failure = "fixture spawn failed"
		return
	}
	r.Parent = c.Process.Pid
	r.Group = r.Parent
	defer func() {
		_ = in.Close()
		_ = syscall.Kill(-r.Group, syscall.SIGKILL)
		_ = c.Wait()
		deadline := time.Now().Add(5 * time.Second)
		for !groupGone(r.Group) && time.Now().Before(deadline) {
			time.Sleep(20 * time.Millisecond)
		}
		r.Gone = groupGone(r.Group)
	}()
	s := bufio.NewScanner(out)
	s.Buffer(make([]byte, 128), 256)
	if !s.Scan() {
		r.Failure = "fixture child not observed"
		return
	}
	r.Child, e = strconv.Atoi(s.Text())
	pg, pe := syscall.Getpgid(r.Child)
	if e != nil || pe != nil || pg != r.Group || r.Child == r.Parent || syscall.Kill(r.Child, 0) != nil {
		r.Failure = "fixture child identity unverified"
		return
	}
	r.Observed = true
	g.result(r)
	if json.NewEncoder(conn).Encode(r) != nil {
		r.Cancelled = true
		_ = syscall.Kill(-r.Group, syscall.SIGTERM)
		return
	}
	ack, ae := decodeRequest(d)
	if ae != nil || ack.Kind != "continue" || ack.ID != q.ID || q.Kind == "cancel" {
		r.Cancelled = true
		_ = syscall.Kill(-r.Group, syscall.SIGTERM)
	} else {
		_, _ = in.Write([]byte("done\n"))
	}
	// Reap the parent after it reaps its child. SIGKILL is a bounded fallback;
	// group absence is checked independently and is never inferred from Wait.
	done := make(chan error, 1)
	go func() { done <- c.Wait() }()
	select {
	case e = <-done:
	case <-time.After(10 * time.Second):
		_ = syscall.Kill(-r.Group, syscall.SIGKILL)
		e = <-done
	}
	if e != nil {
		r.Failure = "fixture exit failed"
	}
	// The deferred reaping call is harmless after Wait; it still checks group 0.
}
func fixtureParent() int {
	exe, e := os.Executable()
	if e != nil {
		return 1
	}
	child := exec.Command(exe, "fixture-child")
	out, e := child.StdoutPipe()
	if e != nil {
		return 1
	}
	if child.Start() != nil {
		return 1
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()
	s := bufio.NewScanner(out)
	if !s.Scan() || s.Text() != "ready" {
		_ = child.Process.Kill()
		_ = child.Wait()
		return 1
	}
	fmt.Println(child.Process.Pid)
	ready := make(chan struct{})
	go func() {
		s := bufio.NewScanner(os.Stdin)
		if s.Scan() && s.Text() == "done" {
			close(ready)
		}
	}()
	select {
	case <-ctx.Done():
	case <-ready:
	}
	_ = child.Process.Signal(syscall.SIGTERM)
	if child.Wait() != nil {
		return 1
	}
	return 0
}

func fixtureChild() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()
	fmt.Println("ready")
	<-ctx.Done()
}
func runGuest() error {
	b, e := os.ReadFile("/etc/ci2737-public.json")
	if e != nil {
		return errors.New("public guest config missing")
	}
	var p publicConfig
	if json.Unmarshal(b, &p) != nil || !validInstance(p.Instance) || !validHash(p.ClientCertificate) {
		return errors.New("public guest config rejected")
	}
	if p.Source.HeadSHA == "" {
		p.Source = fixedSourcePin()
	}
	if !validSourcePin(p.Source) {
		return errors.New("public source expectation rejected")
	}
	i, e := newIdentity()
	if e != nil {
		return e
	}
	l, e := tls.Listen("tcp4", fmt.Sprintf("0.0.0.0:%d", guestPort), serverTLS(i, p.ClientCertificate))
	if e != nil {
		return e
	}
	defer l.Close()
	var ip string
	until := time.Now().Add(2 * time.Minute)
	for ip == "" && time.Now().Before(until) {
		addrs, _ := net.InterfaceAddrs()
		for _, a := range addrs {
			v, _, _ := net.ParseCIDR(a.String())
			if v != nil && v.To4() != nil && v.IsPrivate() && !v.IsLoopback() {
				ip = v.String()
				break
			}
		}
		if ip == "" {
			time.Sleep(time.Second)
		}
	}
	if ip == "" {
		return errors.New("private guest network unavailable")
	}
	public, _ := json.Marshal(ready{p.Instance, ip, guestPort, i.Digest})
	serial, e := os.OpenFile("/dev/ttyS0", os.O_WRONLY, 0)
	if e != nil {
		return errors.New("owned guest console unavailable")
	}
	defer serial.Close()
	// Only public readiness data is emitted. No key or token is written to disk.
	announce := func() { _, _ = fmt.Fprintln(serial, "CI2737-READY "+base64.StdEncoding.EncodeToString(public)) }
	announce()
	g := guest{results: map[string]fixtureResult{}, instance: p.Instance, source: p.Source}
	deadline := time.AfterFunc(45*time.Minute, func() { _ = l.Close() })
	defer deadline.Stop()
	go func() {
		for n := 0; n < 120; n++ {
			time.Sleep(5 * time.Second)
			announce()
		}
	}()
	for {
		c, e := l.Accept()
		if e != nil {
			return e
		}
		go g.serve(c)
	}
}
func main() {
	if len(os.Args) == 3 && os.Args[1] == "job-hook" {
		if liveJobHook(os.Args[2]) == nil {
			return
		}
		os.Exit(1)
	}
	if len(os.Args) == 2 {
		switch os.Args[1] {
		case "guest":
			if runGuest() == nil {
				return
			}
		case "fixture-parent":
			os.Exit(fixtureParent())
		case "fixture-child":
			fixtureChild()
			return
		}
	}
	os.Exit(1)
}
