package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// Opt-in acceptance in our source-free SDK container, using the same extracted
// SDK location and user as the live guest. No runner registration or Cargo.
func TestLiveRunnerSDKNodePath(t *testing.T) {
	if os.Getenv("CC2737_LIVE_NODE_PROBE") != "1" {
		t.Skip("public SDK at live guest path not supplied")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, "/bin/sh", "-c", "command -v node; node --version; command -v npm; npm --version")
	c.Env = []string{"HOME=/tmp", "LANG=C.UTF-8", "PATH=" + liveRunnerPath}
	c.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: 1001, Gid: 1001}}
	b, e := c.CombinedOutput()
	lines := strings.Fields(string(b))
	if e != nil || len(lines) != 4 || lines[0] != "/opt/ci2737-runner/externals/node24/bin/node" || !strings.HasPrefix(lines[1], "v24.") || lines[2] != "/opt/ci2737-runner/externals/node24/bin/npm" {
		t.Fatal("live child selected an unexpected SDK node/npm", e, string(b))
	}
	t.Logf("public SDK child: node=%s npm=%s", lines[1], lines[3])
}

// Advance only the wrapper's clock: a regression must not wait for the real
// admission timeout, or depend on how fast the runner finishes its job.
type deadlineClockConn struct {
	net.Conn
	mu                 sync.Mutex
	now, writeDeadline time.Time
	setErr, clearErr   error
}

func (c *deadlineClockConn) SetWriteDeadline(d time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if d.IsZero() && c.clearErr != nil {
		return c.clearErr
	}
	if !d.IsZero() && c.setErr != nil {
		return c.setErr
	}
	c.writeDeadline = d
	return nil
}

func TestLiveEventRejectsDeadlineSetupAndCleanupErrors(t *testing.T) {
	for _, setting := range []bool{true, false} {
		c := &deadlineClockConn{}
		want := errors.New("injected deadline failure")
		if setting {
			c.setErr = want
		} else {
			c.clearErr = want
		}
		var sent bytes.Buffer
		err := writeLiveEvent(c, json.NewEncoder(&sent), liveJobEvent{ID: "fixed", Stage: "failed"})
		if !errors.Is(err, want) {
			t.Fatal("deadline failure reported as success", err)
		}
		if setting && sent.Len() != 0 {
			t.Fatal("event sent without a bounded deadline")
		}
		if !setting && sent.Len() == 0 {
			t.Fatal("cleanup failure fixture never sent an event")
		}
	}
}

func (c *deadlineClockConn) Write(b []byte) (int, error) {
	c.mu.Lock()
	expired := !c.writeDeadline.IsZero() && !c.now.Before(c.writeDeadline)
	c.mu.Unlock()
	if expired {
		return 0, os.ErrDeadlineExceeded
	}
	return c.Conn.Write(b)
}

func TestLivePermitDoesNotExpireLaterTerminalEvent(t *testing.T) {
	for _, stage := range []string{"failed", "runner-exited"} {
		t.Run(stage, func(t *testing.T) {
			a, b := net.Pipe()
			defer a.Close()
			defer b.Close()
			_ = b.SetDeadline(time.Now().Add(2 * time.Second))
			c := &deadlineClockConn{Conn: a, now: time.Now()}
			acks := make(chan liveJobAck, 1)
			acks <- liveJobAck{ID: "fixed", Stage: "start", Accepted: true}
			w := liveWire{out: json.NewEncoder(c), conn: c, acks: acks}
			done := make(chan error, 1)
			go func() { done <- w.permit(context.Background(), "fixed", liveJobEvent{ID: "fixed", Stage: "start"}) }()
			d := json.NewDecoder(b)
			var event liveJobEvent
			if e := d.Decode(&event); e != nil || event.Stage != "start" {
				t.Fatal("start event not delivered", e)
			}
			if e := <-done; e != nil {
				t.Fatal(e)
			}
			c.mu.Lock()
			c.now = c.now.Add(time.Minute)
			c.mu.Unlock()
			go func() { done <- w.out.Encode(liveJobEvent{ID: "fixed", Stage: stage}) }()
			if e := d.Decode(&event); e != nil || event.Stage != stage {
				t.Fatal("later terminal event not delivered", e)
			}
			if e := <-done; e != nil {
				t.Fatal(e)
			}
		})
	}
}

func TestLivePermitBindsAcknowledgementAndDrainsCancellation(t *testing.T) {
	for _, kind := range []string{"accepted", "wrong-stage", "closed", "cancelled"} {
		t.Run(kind, func(t *testing.T) {
			a, b := net.Pipe()
			defer a.Close()
			defer b.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			acks := make(chan liveJobAck, 1)
			w := liveWire{out: json.NewEncoder(a), conn: a, acks: acks, ctx: ctx}
			done := make(chan error, 1)
			go func() { done <- w.permit(ctx, "fixed", liveJobEvent{ID: "fixed", Stage: "start"}) }()
			var observed liveJobEvent
			if json.NewDecoder(b).Decode(&observed) != nil || observed.ID != "fixed" || observed.Stage != "start" {
				t.Fatal("actual event not delivered")
			}
			switch kind {
			case "accepted":
				acks <- liveJobAck{ID: "fixed", Stage: "start", Accepted: true}
			case "wrong-stage":
				acks <- liveJobAck{ID: "fixed", Stage: "complete", Accepted: true}
			case "closed":
				close(acks)
			case "cancelled":
				cancel()
			}
			select {
			case e := <-done:
				if (e == nil) != (kind == "accepted") {
					t.Fatal("unexpected admission result")
				}
			case <-time.After(2 * time.Second):
				t.Fatal("permit failed to drain")
			}
		})
	}
}
