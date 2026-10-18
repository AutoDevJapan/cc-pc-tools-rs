package main

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"sync"
	"testing"
	"time"
)

// Advance only the wrapper's clock: a regression must not wait for the real
// admission timeout, or depend on how fast the runner finishes its job.
type deadlineClockConn struct {
	net.Conn
	mu                 sync.Mutex
	now, writeDeadline time.Time
}

func (c *deadlineClockConn) SetWriteDeadline(d time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.writeDeadline = d
	return nil
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
