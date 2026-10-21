package windowsrunner

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

type clockTokenHost struct {
	fakeHost
	tokens int
}

func TestGuestClockReadbackCorrectsSixteenHourSkewAndRefusesFailures(t *testing.T) {
	target := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	for _, mode := range []string{"correct", "set-error", "readback-skew", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			current := target.Add(16 * time.Hour)
			calls := 0
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "cancelled" {
				cancel()
			}
			before, after, e := updateGuestClock(ctx, target, func() time.Time { return current }, func(at time.Time) error {
				calls++
				if mode == "set-error" {
					return errors.New("fixture failure")
				}
				current = at
				if mode == "readback-skew" {
					current = current.Add(16 * time.Hour)
				}
				return nil
			})
			if mode == "correct" {
				if e != nil || calls != 1 || before-target.UnixMilli() != int64((16*time.Hour)/time.Millisecond) || after != target.UnixMilli() {
					t.Fatal("UTC correction/readback failed", before, after, e, calls)
				}
			} else if e == nil {
				t.Fatal("failed update accepted")
			}
			if mode == "cancelled" && calls != 0 {
				t.Fatal("clock changed after cancellation")
			}
		})
	}
}

func TestClockReceiptBindsEchoInstancePrecisionAndRoundTrip(t *testing.T) {
	start := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	q := clockRequest{testSeed().Plan.Instance, start.UnixMilli()}
	base := ClockReceipt{q.Instance, q.HostUnixMillis, q.HostUnixMillis + 57600000, q.HostUnixMillis}
	for _, mode := range []string{"correct", "instance", "echo", "still-ahead", "old-before", "slow", "host-jump"} {
		t.Run(mode, func(t *testing.T) {
			r := base
			end := start.Add(10 * time.Millisecond)
			switch mode {
			case "instance":
				r.Instance = "wrong"
			case "echo":
				r.HostUnixMillis++
			case "still-ahead":
				r.AfterUnixMillis += 57600000
			case "old-before":
				r.BeforeUnixMillis = 1
			case "slow":
				end = start.Add(6 * time.Second)
			case "host-jump":
				end = start.Add(-time.Second)
			}
			e := validateClockReceipt(q, r, start, end)
			if (e == nil) != (mode == "correct") {
				t.Fatal("clock receipt decision", mode, e)
			}
		})
	}
}

func TestSessionRequiresClockUpdateAndPersistenceBeforeSourceAndToken(t *testing.T) {
	for _, mode := range []string{"correct", "guest-error", "persistence-error"} {
		t.Run(mode, func(t *testing.T) {
			packet := fixturePacket()
			seed := testSeed()
			seed.Plan.Source = packet.Pin
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			host, guest := net.Pipe()
			defer host.Close()
			ops := fake(seed.Plan)
			if mode == "guest-error" {
				ops.clockError = errors.New("fixture clock failure")
			}
			h := &clockTokenHost{fakeHost: fakeHost{seed: seed, clockRejected: mode == "persistence-error"}}
			done := make(chan error, 1)
			go func() { defer guest.Close(); done <- guestSession(ctx, guest, seed, ops) }()
			_, e := HostSession(ctx, host, seed, packet, h)
			host.Close()
			ge := <-done
			if mode == "correct" {
				if e != nil || ge != nil || h.tokens != 1 || !ops.configured || !ops.listened {
					t.Fatal("positive clock admission failed", e, ge, h.tokens)
				}
			} else if e == nil || h.tokens != 0 || ops.configured || ops.listened {
				t.Fatal("authority before successful clock admission", e, h.tokens)
			}
		})
	}
}

func (h *clockTokenHost) Token(ctx context.Context, p Plan) ([]byte, error) {
	h.tokens++
	return h.fakeHost.Token(ctx, p)
}

// A valid source proof alone says nothing about the guest wall clock. The
// pre-fix implementation issues registration authority without a clock ack.
func TestHostRejectsClocklessGuestBeforeRegistrationToken(t *testing.T) {
	packet := fixturePacket()
	seed := testSeed()
	seed.Plan.Source = packet.Pin
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	host, guest := net.Pipe()
	host.SetDeadline(time.Now().Add(3 * time.Second))
	guest.SetDeadline(time.Now().Add(3 * time.Second))
	h := &clockTokenHost{fakeHost: fakeHost{seed: seed}}
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer guest.Close()
		var p Plan
		if receive(guest, &p) != nil {
			return
		}
		if _, e := readFrame(guest, maxSource); e != nil {
			return
		}
		if send(guest, Event{Instance: p.Instance, Stage: SourceReady, Proof: receipt(p)}) != nil {
			return
		}
		var m message
		if receive(guest, &m) == nil {
			receive(guest, &m)
		}
	}()
	_, e := HostSession(ctx, host, seed, packet, h)
	host.Close()
	<-done
	if e == nil || h.tokens != 0 {
		t.Fatalf("clockless guest received registration authority: error=%v tokens=%d", e, h.tokens)
	}
}
