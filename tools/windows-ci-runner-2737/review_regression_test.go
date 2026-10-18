package windowsrunner

import (
	sourcepacket "cc/ci-source-2737"
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"
)

func TestFailureTelemetryIsBoundToAdmittedListenerBeforePersistence(t *testing.T) {
	for _, name := range []string{"timeout", "unconfirmed", "wrong-runner", "before-listen", "timeout-not-drained", "unconfirmed-drained", "missing-exit", "invalid-hash"} {
		t.Run(name, func(t *testing.T) {
			packet := fixturePacket()
			seed := testSeed()
			seed.Plan.Source = packet.Pin
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			host, guest := net.Pipe()
			h := &fakeHost{seed: seed}
			sent := make(chan error, 1)
			go func() {
				defer guest.Close()
				var p Plan
				if e := receive(guest, &p); e != nil {
					sent <- e
					return
				}
				raw, e := readFrame(guest, maxSource)
				if e != nil {
					sent <- e
					return
				}
				if _, e = sourcepacket.Decode(raw, p.Source); e != nil {
					sent <- e
					return
				}
				code := uint32(125)
				ev := Event{Instance: p.Instance, Stage: Failed, RunnerID: 789, Exit: &code, Drained: true, LogSHA256: strings.Repeat("f", 64), FailureCode: "timeout"}
				if name == "before-listen" {
					ev.RunnerID = 0
					sent <- send(guest, ev)
					return
				}
				if e = send(guest, Event{Instance: p.Instance, Stage: SourceReady, Proof: receipt(p)}); e != nil {
					sent <- e
					return
				}
				var m message
				if e = receive(guest, &m); e != nil {
					sent <- e
					return
				}
				if e = receive(guest, &m); e != nil {
					sent <- e
					return
				}
				clear(m.Token)
				if e = send(guest, Event{Instance: p.Instance, Stage: Configured}); e != nil {
					sent <- e
					return
				}
				if e = receive(guest, &m); e != nil {
					sent <- e
					return
				}
				switch name {
				case "unconfirmed":
					ev.FailureCode = "drain-unconfirmed"
					ev.Drained = false
					ev.Exit = nil
					ev.LogSHA256 = ""
				case "wrong-runner":
					ev.RunnerID++
				case "timeout-not-drained":
					ev.Drained = false
				case "unconfirmed-drained":
					ev.FailureCode = "drain-unconfirmed"
				case "missing-exit":
					ev.Exit = nil
				case "invalid-hash":
					ev.LogSHA256 = "not-a-hash"
				}
				sent <- send(guest, ev)
			}()
			ev, e := HostSession(ctx, host, seed, packet, h)
			if se := <-sent; se != nil {
				t.Fatal(se)
			}
			switch name {
			case "timeout":
				if !errors.Is(e, context.DeadlineExceeded) || ev.Stage != Failed || len(h.events) != 3 {
					t.Fatal("coherent timeout not recorded", e, h.events)
				}
			case "unconfirmed":
				if e == nil || errors.Is(e, context.DeadlineExceeded) || errors.Is(e, ErrProtocol) || ev.Drained || len(h.events) != 3 {
					t.Fatal("unconfirmed drain misclassified", e, h.events)
				}
			default:
				if !errors.Is(e, ErrProtocol) {
					t.Fatal("inconsistent failure admitted", e)
				}
				want := 2
				if name == "before-listen" {
					want = 0
				}
				if len(h.events) != want {
					t.Fatal("untrusted failure persisted", h.events)
				}
			}
		})
	}
}
