package windowsrunner

import (
	sourcepacket "cc/ci-source-2737"
	"cc/product-ci/normalci"
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"
)

type deadlineHost struct {
	fakeHost
	remaining time.Duration
}

func (h *deadlineHost) Queued(ctx context.Context, _ Plan) (normalci.Binding, error) {
	d, _ := ctx.Deadline()
	h.remaining = time.Until(d)
	return normalci.Binding{}, errors.New("stop before source/token transfer")
}

type deadlineConn struct {
	net.Conn
	deadline time.Time
}

func (c *deadlineConn) SetDeadline(d time.Time) error { c.deadline = d; return nil }

func pressDeadlinePlan(workflow, role string) Plan {
	p := testSeed().Plan
	p.Source.Product, p.Source.Workflow = sourcepacket.PressProduct, workflow
	p.Source.PullHeadSHA, p.Source.MergeBaseSHA = strings.Repeat("d", 40), strings.Repeat("e", 40)
	p.Role = role
	p.Label = "cc-product-2766-1386702183-windows-" + role + "-123-2"
	return p
}

func TestRealHostSessionDeadlineCoversOriginalWindowsRoles(t *testing.T) {
	for _, p := range []Plan{testSeed().Plan, pressDeadlinePlan("rust-v2-https.yml", "rust-v2-https-windows-2022"), pressDeadlinePlan("windows-hidden-fixtures.yml", "junction-fixtures")} {
		t.Run(p.Role, func(t *testing.T) {
			a, b := net.Pipe()
			defer b.Close()
			conn := &deadlineConn{Conn: a}
			seed := testSeed()
			seed.Plan = p
			h := &deadlineHost{}
			_, err := HostSession(context.Background(), conn, seed, sourcepacket.Packet{}, h)
			if err == nil {
				t.Fatal("pre-transfer stop lost")
			}
			want := 26 * time.Minute
			if p.Source.Product == sourcepacket.PressProduct {
				want = 130 * time.Minute
			}
			for _, got := range []time.Duration{h.remaining, time.Until(conn.deadline)} {
				if got < want-time.Second || got > want {
					t.Fatalf("effective session deadline %s, want %s", got, want)
				}
			}
		})
	}
}
