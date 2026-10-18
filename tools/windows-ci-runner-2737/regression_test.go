package windowsrunner

import (
	"archive/zip"
	"bytes"
	sourcepacket "cc/ci-source-2737"
	"context"
	"errors"
	sdk "github.com/autodevjapan/cc-pc-tools-rs/tools/windows-ci-sdk-2737"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStockArchiveRejectsReplacementTraversalAndCaseCollision(t *testing.T) {
	if unpackRunner([]byte("replacement"), filepath.Join(t.TempDir(), "stock")) == nil {
		t.Fatal("unhashed stock archive accepted")
	}
	for _, names := range [][]string{{"../outside"}, {`dir\bad`}, {"C:/bad"}, {"file", "FILE"}, {".hidden/../bad"}, {"bad."}} {
		var b bytes.Buffer
		z := zip.NewWriter(&b)
		for _, name := range names {
			f, e := z.Create(name)
			if e != nil {
				t.Fatal(e)
			}
			f.Write([]byte("synthetic"))
		}
		z.Close()
		if unpackCheckedZip(b.Bytes(), filepath.Join(t.TempDir(), "stock")) == nil {
			t.Fatal("unsafe stock member accepted")
		}
	}
}
func TestPublicSeedsCannotSerializeIdentityPrivateKey(t *testing.T) {
	s := testSeed()
	id, e := sdk.NewHostIdentity(s.Plan.Instance)
	if e != nil {
		t.Fatal(e)
	}
	s.ClientSHA256 = sdk.CertificateHash(id.Leaf)
	a, b, e := PublicSeeds(s, id)
	if e != nil {
		t.Fatal(e)
	}
	if bytes.Contains(a, []byte("PrivateKey")) || bytes.Contains(b, []byte("Certificate")) || !bytes.Contains(a, []byte(s.ClientSHA256)) {
		t.Fatal("seed content rejected")
	}
	var got Seed
	if decode(b, &got) != nil || !samePlan(got.Plan, s.Plan) {
		t.Fatal("seed roundtrip failed")
	}
	s.ClientSHA256 = strings.Repeat("f", 64)
	if _, _, e = PublicSeeds(s, id); e == nil {
		t.Fatal("wrong seed client accepted")
	}
}
func TestReplayedAndWrongContextHookFailsBeforeOfficialSecondClaim(t *testing.T) {
	for _, badContext := range []bool{false, true} {
		packet := fixturePacket()
		seed := testSeed()
		seed.Plan.Source = packet.Pin
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		host, guest := net.Pipe()
		ops := fake(seed.Plan)
		ops.badContext = badContext
		ops.stages = []Stage{Start, Start}
		done := make(chan error, 1)
		go func() { defer guest.Close(); done <- guestSession(ctx, guest, seed, ops) }()
		h := &fakeHost{seed: seed}
		_, e := HostSession(ctx, host, seed, packet, h)
		if e == nil {
			t.Fatal("invalid hook accepted")
		}
		if <-done == nil {
			t.Fatal("guest reported invalid hook success")
		}
		cancel()
	}
}
func TestEOFDuringConfigurationCancelsWholeFlight(t *testing.T) {
	packet := fixturePacket()
	seed := testSeed()
	seed.Plan.Source = packet.Pin
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	host, guest := net.Pipe()
	defer host.Close()
	ops := fake(seed.Plan)
	ops.configureEntered = make(chan struct{})
	done := make(chan error, 1)
	go func() { defer guest.Close(); done <- guestSession(ctx, guest, seed, ops) }()
	send(host, seed.Plan)
	raw, _ := sourcepacket.Encode(packet)
	writeFrame(host, raw)
	var ev Event
	receive(host, &ev)
	send(host, message{Ack: ack{seed.Plan.Instance, SourceReady, true, 0}})
	send(host, message{Token: []byte("synthetic-registration-token")})
	<-ops.configureEntered
	host.Close()
	select {
	case e := <-done:
		if !errors.Is(e, ErrHostEOF) {
			t.Fatalf("EOF not distinguished: %v", e)
		}
	case <-ctx.Done():
		t.Fatal("configuration survived EOF")
	}
	if ops.listened {
		t.Fatal("listener started after configuration EOF")
	}
}
