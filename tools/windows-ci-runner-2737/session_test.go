package windowsrunner

import (
	"archive/tar"
	"bytes"
	sourcepacket "cc/ci-source-2737"
	"cc/product-ci/normalci"
	"compress/gzip"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

func oid(kind string, b []byte) string {
	h := sha1.New()
	fmt.Fprintf(h, "%s %d%c", kind, len(b), 0)
	h.Write(b)
	return hex.EncodeToString(h.Sum(nil))
}
func sha(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func treeEntry(mode, name, id string) []byte {
	raw, _ := hex.DecodeString(id)
	return append(append([]byte(mode+" "+name), 0), raw...)
}

// Entirely synthetic Git objects/archive, no product source or GitHub request.
func fixturePacket() sourcepacket.Packet {
	wf := []byte("name: synthetic\r\non: [workflow_dispatch]\r\n")
	sentinel := []byte("original CRLF bytes\r\n")
	wid, sid := oid("blob", wf), oid("blob", sentinel)
	wTree := oid("tree", treeEntry("100644", "ci.yml", wid))
	gTree := oid("tree", treeEntry("40000", "workflows", wTree))
	root := append(treeEntry("40000", ".github", gTree), treeEntry("100644", "sentinel.txt", sid)...)
	tid := oid("tree", root)
	commit := []byte("tree " + tid + "\nauthor Fixture <fixture@example.invalid> 1 +0000\ncommitter Fixture <fixture@example.invalid> 1 +0000\n\nsynthetic\n")
	var ar bytes.Buffer
	gz := gzip.NewWriter(&ar)
	tw := tar.NewWriter(gz)
	for _, file := range []struct {
		name string
		b    []byte
	}{{".github/workflows/ci.yml", wf}, {"sentinel.txt", sentinel}} {
		tw.WriteHeader(&tar.Header{Name: "fixture/" + file.name, Mode: 0644, Size: int64(len(file.b)), Typeflag: tar.TypeReg})
		tw.Write(file.b)
	}
	tw.Close()
	gz.Close()
	return sourcepacket.Packet{Pin: sourcepacket.Pin{HeadSHA: oid("commit", commit), TreeSHA: tid, WorkflowSHA256: sha(wf)}, CommitPayload: commit, Archive: ar.Bytes(), ArchiveSHA256: sha(ar.Bytes()), TreeEntries: []sourcepacket.TreeEntry{{Path: ".github", Mode: "040000", Type: "tree", SHA: gTree}, {Path: ".github/workflows", Mode: "040000", Type: "tree", SHA: wTree}, {Path: ".github/workflows/ci.yml", Mode: "100644", Type: "blob", SHA: wid}, {Path: "sentinel.txt", Mode: "100644", Type: "blob", SHA: sid}}}
}

type fakeOps struct {
	p                    Plan
	calls                chan hookCall
	closed               chan struct{}
	configured, listened bool
	badContext           bool
	stages               []Stage
	configureEntered     chan struct{}
	processError         error
	clockError           error
	configureError       error
	configureToken       []byte
	mu                   sync.Mutex
}

func fake(p Plan) *fakeOps {
	return &fakeOps{p: p, calls: make(chan hookCall), closed: make(chan struct{})}
}
func (o *fakeOps) prepare(context.Context, sourcepacket.Packet) (sourcepacket.Receipt, error) {
	return receipt(o.p), nil
}
func (o *fakeOps) syncClock(_ context.Context, at time.Time) (int64, int64, error) {
	return at.Add(16 * time.Hour).UnixMilli(), at.UnixMilli(), o.clockError
}
func (o *fakeOps) configure(ctx context.Context, token []byte) error {
	if o.configureEntered != nil {
		close(o.configureEntered)
		<-ctx.Done()
		return context.Cause(ctx)
	}
	if string(token) != "synthetic-registration-token" {
		return errors.New("wrong token")
	}
	o.configureToken = token
	if o.configureError != nil {
		return o.configureError
	}
	o.mu.Lock()
	o.configured = true
	o.mu.Unlock()
	return nil
}
func (o *fakeOps) proof(context.Context) (sourcepacket.Receipt, error) { return receipt(o.p), nil }
func (o *fakeOps) hooks() <-chan hookCall                              { return o.calls }
func (o *fakeOps) close() error                                        { close(o.closed); return nil }
func (o *fakeOps) listen(ctx context.Context, id int64) <-chan processResult {
	o.mu.Lock()
	o.listened = true
	o.mu.Unlock()
	done := make(chan processResult, 1)
	go func() {
		if o.processError != nil {
			done <- processResult{drained: true, err: o.processError, logHash: strings.Repeat("f", 64)}
			return
		}
		stages := o.stages
		if stages == nil {
			stages = []Stage{Start, Complete}
		}
		for _, stage := range stages {
			c := hookContext(o.p)
			if o.badContext {
				c.RunID++
			}
			call := hookCall{stage, c, make(chan error, 1)}
			select {
			case o.calls <- call:
			case <-ctx.Done():
				done <- processResult{drained: true}
				return
			}
			select {
			case e := <-call.reply:
				if e != nil {
					done <- processResult{drained: true, err: e}
					return
				}
			case <-ctx.Done():
				done <- processResult{drained: true}
				return
			}
		}
		done <- processResult{drained: true, logHash: strings.Repeat("f", 64)}
	}()
	return done
}

type fakeHost struct {
	seed          Seed
	events        []Stage
	reject        Stage
	registered    bool
	clockRejected bool
	observations  []Event
	claims        int
}

func (h *fakeHost) ClockObserved(context.Context, Plan, ClockReceipt) error {
	if h.clockRejected {
		return errors.New("clock persistence refused")
	}
	return nil
}

func (h *fakeHost) Queued(_ context.Context, p Plan) (normalci.Binding, error) {
	return binding(p), nil
}
func (h *fakeHost) Token(context.Context, Plan) ([]byte, error) {
	return []byte("synthetic-registration-token"), nil
}
func (h *fakeHost) Registered(context.Context, Plan) (int64, error) {
	if h.reject == Configured {
		return 0, errors.New("reject registration")
	}
	h.registered = true
	return 789, nil
}
func (h *fakeHost) Claim(_ context.Context, p Plan, id int64, ev Event) error {
	h.claims++
	if !h.registered || id != 789 || h.reject == ev.Stage {
		return errors.New("official claim rejected")
	}
	return nil
}
func (h *fakeHost) Observe(_ context.Context, ev Event) error {
	h.events = append(h.events, ev.Stage)
	h.observations = append(h.observations, ev)
	return nil
}

func TestSyntheticHostGuestStagesAndPositiveClaims(t *testing.T) {
	packet := fixturePacket()
	seed := testSeed()
	seed.Plan.Source = packet.Pin
	for _, reject := range []Stage{"", Configured, Start, Complete} {
		t.Run(string(reject), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			host, guest := net.Pipe()
			ops := fake(seed.Plan)
			h := &fakeHost{seed: seed, reject: reject}
			done := make(chan error, 1)
			go func() { defer guest.Close(); done <- guestSession(ctx, guest, seed, ops) }()
			ev, e := HostSession(ctx, host, seed, packet, h)
			ge := <-done
			if reject == "" {
				if e != nil || ge != nil || ev.Stage != RunnerExited || !ev.Drained || len(h.events) != 5 {
					t.Fatalf("finite session failed: %v %v %#v", e, ge, h.events)
				}
			} else {
				if e == nil {
					t.Fatal("negative host claim accepted")
				}
				if reject == Configured {
					ops.mu.Lock()
					listened := ops.listened
					ops.mu.Unlock()
					if listened {
						t.Fatal("listener ran without registered identity")
					}
				}
			}
			select {
			case <-ops.closed:
			default:
				t.Fatal("operations not drained")
			}
		})
	}
}
func TestGuestRejectsPlanBeforeSourceAndDoesNotConfigure(t *testing.T) {
	seed := testSeed()
	host, guest := net.Pipe()
	defer host.Close()
	done := make(chan error, 1)
	ops := fake(seed.Plan)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go func() { defer guest.Close(); done <- guestSession(ctx, guest, seed, ops) }()
	p := seed.Plan
	p.Source.TreeSHA = strings.Repeat("f", 40)
	if send(host, p) != nil {
		t.Fatal("send failed")
	}
	if <-done == nil {
		t.Fatal("different source plan accepted")
	}
	if ops.configured || ops.listened {
		t.Fatal("execution preceded source admission")
	}
}
func TestGuestEOFAndCancelDrainDuringPendingHook(t *testing.T) {
	for _, cancelFrame := range []bool{false, true} {
		t.Run(fmt.Sprint(cancelFrame), func(t *testing.T) {
			packet := fixturePacket()
			seed := testSeed()
			seed.Plan.Source = packet.Pin
			host, guest := net.Pipe()
			defer host.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			ops := fake(seed.Plan)
			done := make(chan error, 1)
			go func() { defer guest.Close(); done <- guestSession(ctx, guest, seed, ops) }()
			send(host, seed.Plan)
			if _, e := hostClock(host, seed.Plan.Instance); e != nil {
				t.Fatal(e)
			}
			raw, _ := sourcepacket.Encode(packet)
			writeFrame(host, raw)
			var ev Event
			receive(host, &ev)
			send(host, message{Ack: ack{seed.Plan.Instance, SourceReady, true, 0}})
			send(host, message{Token: []byte("synthetic-registration-token")})
			receive(host, &ev)
			send(host, message{Ack: ack{seed.Plan.Instance, Configured, true, 789}})
			receive(host, &ev)
			if ev.Stage != Start {
				t.Fatal("start absent")
			}
			if cancelFrame {
				send(host, message{Cancel: true})
			} else {
				host.Close()
			}
			select {
			case e := <-done:
				if e == nil {
					t.Fatal("cancel/EOF became success")
				}
			case <-ctx.Done():
				t.Fatal("pending hook/child not drained")
			}
			select {
			case <-ops.closed:
			default:
				t.Fatal("operations left open")
			}
		})
	}
}
