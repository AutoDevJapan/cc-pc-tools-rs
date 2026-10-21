package windowsrunner

import (
	sourcepacket "cc/ci-source-2737"
	"cc/product-ci/normalci"
	"context"
	"errors"
	"io"
	"net"
	"time"
)

// Controller is implemented by parent2737, inside its canonical Cargo lease.
// Queued calls normalci.Plan, Token persists an intent then requests the short
// registration token, Registered saves the numeric offline runner identity,
// Claim re-reads the official API claim, and Observe durably records metadata.
// None of these callbacks should expose tokens or raw runner/checkout logs.
type Controller interface {
	ClockObserved(context.Context, Plan, ClockReceipt) error
	Queued(context.Context, Plan) (normalci.Binding, error)
	Token(context.Context, Plan) ([]byte, error)
	Registered(context.Context, Plan) (int64, error)
	Claim(context.Context, Plan, int64, Event) error
	Observe(context.Context, Event) error
}
type message struct {
	Ack    ack
	Token  []byte
	Cancel bool
}

// HostSession closes the channel on cancellation/EOF. Its returned event is
// runner exit and drain metadata, NEVER official job completion. Parent must
// poll the official job and retain its VM/gate lease until cleanup is proven.
func HostSession(ctx context.Context, c net.Conn, seed Seed, packet sourcepacket.Packet, host Controller) (Event, error) {
	if c == nil || host == nil || seed.Validate() != nil {
		return Event{}, errors.New("host session rejected")
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(ctx, seed.Plan.sessionTimeout())
	defer cancel()
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			c.Close()
		case <-done:
		}
	}()
	sessionDeadline := time.Now().Add(seed.Plan.sessionTimeout())
	_ = c.SetDeadline(sessionDeadline)
	p := seed.Plan
	if b, e := host.Queued(ctx, p); e != nil || ValidateBinding(p, b) != nil {
		return Event{}, errors.New("initial queued job rejected")
	}
	raw, e := sourcepacket.Encode(packet)
	if e != nil {
		return Event{}, errors.New("source encoding rejected")
	}
	defer clear(raw)
	if packet.Pin != p.Source {
		return Event{}, errors.New("source pin changed")
	}
	_ = c.SetDeadline(time.Now().Add(10 * time.Second))
	if send(c, p) != nil {
		return Event{}, errors.New("initial transfer failed")
	}
	clock, e := hostClock(c, p.Instance)
	if e != nil {
		return Event{}, e
	}
	if e = host.ClockObserved(ctx, p, clock); e != nil {
		return Event{}, errors.New("guest UTC persistence failed")
	}
	_ = c.SetDeadline(sessionDeadline)
	if writeFrame(c, raw) != nil {
		return Event{}, errors.New("initial transfer failed")
	}
	var runnerID int64
	for _, stage := range []Stage{SourceReady, Configured, Start, Complete, RunnerExited} {
		var ev Event
		if e := receive(c, &ev); e != nil {
			if ctx.Err() != nil {
				return ev, ctx.Err()
			}
			if errors.Is(e, io.EOF) || errors.Is(e, io.ErrUnexpectedEOF) {
				return ev, ErrHostEOF
			}
			return ev, ErrProtocol
		}
		if ev.Instance == p.Instance && ev.Stage == Failed {
			if !validFailure(p, stage, runnerID, ev) {
				return ev, ErrProtocol
			}
			if host.Observe(ctx, ev) != nil {
				return ev, errors.New("failure persistence failed")
			}
			if ev.FailureCode == "timeout" {
				return ev, context.DeadlineExceeded
			}
			return ev, errors.New("Windows runner failed or drain unconfirmed")
		}
		if ev.Instance != p.Instance || ev.Stage != stage || ev.FailureCode != "" || ev.NativeError != 0 {
			return ev, errors.New("Windows event order rejected")
		}
		if stage == SourceReady || stage == Start || stage == Complete {
			if !validProof(p, ev.Proof) {
				return ev, errors.New("physical proof rejected")
			}
		}
		if stage == Start || stage == Complete {
			if ev.RunnerID != runnerID || ev.Context.Validate(p) != nil || host.Claim(ctx, p, runnerID, ev) != nil {
				return ev, errors.New("official hook claim rejected")
			}
		}
		if stage == SourceReady {
			if b, e := host.Queued(ctx, p); e != nil || ValidateBinding(p, b) != nil {
				return ev, errors.New("queued identity changed")
			}
		}
		if stage == Configured {
			// Registered is responsible for exact name/one label/offline/not busy
			// API verification and durable numeric identity BEFORE listen ack.
			runnerID, e = host.Registered(ctx, p)
			if e != nil || runnerID < 1 {
				return ev, errors.New("offline registration rejected")
			}
			if b, e := host.Queued(ctx, p); e != nil || ValidateBinding(p, b) != nil {
				return ev, errors.New("queued identity changed before listen")
			}
		}
		if stage == RunnerExited {
			if ev.RunnerID != runnerID || !ev.Drained || ev.Exit == nil || *ev.Exit != 0 || !validHex(ev.LogSHA256, 64) {
				return ev, errors.New("runner exit or drain rejected")
			}
		}
		if host.Observe(ctx, ev) != nil {
			return ev, errors.New("event persistence failed")
		}
		if stage == RunnerExited {
			return ev, nil
		}
		if send(c, message{Ack: ack{p.Instance, stage, true, runnerID}}) != nil {
			return ev, errors.New("stage acknowledgement failed")
		}
		if stage == SourceReady {
			token, e := host.Token(ctx, p)
			if e != nil || len(token) == 0 || len(token) > 4096 {
				clear(token)
				return ev, errors.New("registration token unavailable")
			}
			e = send(c, message{Token: token})
			clear(token)
			if e != nil {
				return ev, errors.New("configuration transfer failed")
			}
		}
	}
	return Event{}, errors.New("session incomplete")
}

// Failures are observations of this admitted configuration/listener, not
// cleanup permission. A listener timeout/process result must include a confirmed
// drain and measured exit/hash. A pre-launch configuration failure has no exit.
// An unconfirmed drain may lack an exit/hash but never masquerades as timeout.
func validFailure(p Plan, expected Stage, runnerID int64, ev Event) bool {
	// Registration has not been acknowledged here. This observation never
	// supplies a runner identity or authorizes listening/cleanup on its own.
	if expected == Configured && runnerID == 0 && ev.RunnerID == 0 && ev.Instance == p.Instance && ev.Stage == Failed {
		switch ev.FailureCode {
		case "configuration-create", "configuration-identity", "configuration-assign", "configuration-resume":
		case "configuration-setup", "configuration-execute":
			if ev.NativeError != 0 {
				return false
			}
		default:
			return false
		}
		if ev.Drained {
			return ev.Exit != nil && validHex(ev.LogSHA256, 64)
		}
		return ev.Exit == nil && (ev.LogSHA256 == "" || validHex(ev.LogSHA256, 64))
	}
	if ev.NativeError != 0 {
		return false
	}
	if runnerID < 1 || (expected != Start && expected != Complete && expected != RunnerExited) || ev.Instance != p.Instance || ev.Stage != Failed || ev.RunnerID != runnerID {
		return false
	}
	switch ev.FailureCode {
	case "process", "timeout":
		return ev.Drained && ev.Exit != nil && validHex(ev.LogSHA256, 64)
	case "drain-unconfirmed":
		return !ev.Drained && (ev.LogSHA256 == "" || validHex(ev.LogSHA256, 64))
	default:
		return false
	}
}
