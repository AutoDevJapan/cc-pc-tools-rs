package officialci3893

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPurposeProtocolHasNoCallerSelectedGrant(t *testing.T) {
	f := newFixture(t)
	create, err := ParseCall([]string{"--construct-official-ci3893"})
	if err != nil {
		t.Fatal(err)
	}
	out, err := Execute(context.Background(), f.engine, create, nil)
	if err != nil || !out.PurposeVerified || out.OriginalRegistryRecovered || out.GeneralAccessGranted || out.LaunchPerformed {
		t.Fatalf("bad constructor result: %+v %v", out, err)
	}
	resume, err := ParseCall([]string{"--resume-official-ci3893", out.ReceiptID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = Execute(context.Background(), f.engine, resume, nil); err != nil {
		t.Fatal(err)
	}
	consume, err := ParseCall([]string{"--consume-official-ci3893", out.ReceiptID})
	if err != nil {
		t.Fatal(err)
	}
	used := 0
	out, err = Execute(context.Background(), f.engine, consume, func(p []Payload) error { used = len(p); return nil })
	if err != nil || used != 2 || out.State != Consumed || out.GeneralAccessGranted || out.OriginalRegistryRecovered || out.LaunchPerformed {
		t.Fatalf("bad consumer result: %+v %v", out, err)
	}
	for _, args := range [][]string{
		nil, {"--construct-official-ci3893", "D:/unknown"},
		{"--construct-official-ci3893", "--hash", strings.Repeat("a", 64)},
		{"--consume-official-ci3893", "../receipt"},
		{"--consume-official-ci3893", strings.Repeat("a", 32), "--root", "D:/unknown"},
		{"--consume-official-ci3893", strings.Repeat("a", 32), "--repo", "other"},
		{"--resume-official-ci3893"}, {"--guard-path", "D:/unknown"},
	} {
		if _, err := ParseCall(args); err == nil {
			t.Fatalf("accepted caller authority: %q", args)
		}
	}
}

func TestFreshIndependentAnchorRequiredBeforeIO(t *testing.T) {
	for _, kind := range []string{"unverified", "different-volume", "different-file", "different-birth", "different-receipt"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t)
			switch kind {
			case "unverified":
				f.authority.evidence.FreshOwnedAnchor = false
			case "different-volume":
				f.authority.evidence.Parent.Volume++
			case "different-file":
				f.authority.evidence.Parent.FileID++
			case "different-birth":
				f.authority.evidence.Parent.Birth++
			case "different-receipt":
				f.authority.evidence.Binding.AnchorReceipt = strings.Repeat("e", 64)
			}
			if _, err := f.engine.Construct(context.Background()); err == nil || f.archive.calls != 0 || len(f.journal.records) != 0 {
				t.Fatal("caller-only anchor admitted")
			}
		})
	}
}

func TestProductionFactoryCannotConsumeFixturePinOrAlternateScope(t *testing.T) {
	f := newFixture(t)
	e, err := New(f.engine.binding, f.authority, f.archive, f.journal, f.fs)
	if err != nil || e.asset != OfficialAsset() {
		t.Fatal("production asset is caller-selected")
	}
	e.now = f.engine.now
	if _, err = e.Construct(context.Background()); err == nil || f.archive.calls != 0 {
		t.Fatal("fixture pin admitted by production factory")
	}
	f.authority.evidence.Asset = OfficialAsset()
	if _, err = e.Construct(context.Background()); err == nil || len(f.journal.records) != 0 {
		t.Fatal("small fixture bytes admitted as official asset")
	}
	for _, kind := range []string{"repo", "run", "source", "anchor"} {
		b := f.engine.binding
		switch kind {
		case "repo":
			b.RepoID++
		case "run":
			b.RunID++
		case "source":
			b.SourceRegistration = "caller-claim"
		case "anchor":
			b.AnchorReceipt = "caller-claim"
		}
		if _, err = New(b, f.authority, f.archive, f.journal, f.fs); err == nil {
			t.Fatalf("alternate constructor scope: %s", kind)
		}
	}
}

func TestConcurrentConsumerOnlyOneCanUseReceipt(t *testing.T) {
	f := newFixture(t)
	r := f.create(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	var calls atomic.Int64
	go func() {
		done <- f.engine.Consume(context.Background(), r.ID, func([]Payload) error { calls.Add(1); close(entered); <-release; return nil })
	}()
	select {
	case <-entered:
	case err := <-done:
		t.Fatalf("first consumer failed: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("consumer did not enter")
	}
	err := f.engine.Consume(context.Background(), r.ID, func([]Payload) error { calls.Add(1); return nil })
	close(release)
	firstErr := <-done
	if err == nil || firstErr != nil || calls.Load() != 1 {
		t.Fatalf("non-atomic consumption: first=%v second=%v calls=%d", firstErr, err, calls.Load())
	}
}

func TestReceiptDeadlineUsesOneCreationClockSample(t *testing.T) {
	f := newFixture(t)
	var ticks int64
	f.engine.now = func() time.Time { ticks++; return f.now.Add(time.Duration(ticks) * time.Nanosecond) }
	r := f.create(t)
	if r.Expires.Sub(r.Created) != 30*time.Minute {
		t.Fatal("deadline sampled a second clock")
	}
	// A new engine may resume the same complete authenticated receipt. It cannot
	// manufacture a new expiry from the current time or recover a Pending record.
	e, err := newEngine(f.engine.binding, f.engine.asset, f.authority, f.archive, f.journal, f.fs)
	if err != nil {
		t.Fatal(err)
	}
	e.now = f.engine.now
	if err = e.Resume(context.Background(), r.ID); err != nil {
		t.Fatal(err)
	}
	saved, err := f.journal.Load(r.ID)
	if err != nil || !saved.Expires.Equal(r.Expires) {
		t.Fatal("resume extended expiry")
	}
}

type blockedArchive struct{ reader *blockedReader }

func (b blockedArchive) Open(context.Context, Asset) (io.ReadCloser, error) { return b.reader, nil }

type blockedReader struct {
	entered, closed      chan struct{}
	enterOnce, closeOnce sync.Once
}

func (b *blockedReader) Read([]byte) (int, error) {
	b.enterOnce.Do(func() { close(b.entered) })
	<-b.closed
	return 0, io.ErrClosedPipe
}
func (b *blockedReader) Close() error { b.closeOnce.Do(func() { close(b.closed) }); return nil }

func TestCancellationClosesArchiveAndLeavesNoReceipt(t *testing.T) {
	f := newFixture(t)
	r := &blockedReader{entered: make(chan struct{}), closed: make(chan struct{})}
	f.engine.archive = blockedArchive{r}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := f.engine.Construct(ctx); done <- err }()
	select {
	case <-r.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("archive did not enter read")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation lost: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled reader was not closed")
	}
	if len(f.journal.records) != 0 {
		t.Fatal("cancelled download created a receipt")
	}
}

func TestExpiredAfterConsumerIsNeverCommittedAsSuccess(t *testing.T) {
	f := newFixture(t)
	r := f.create(t)
	err := f.engine.Consume(context.Background(), r.ID, func([]Payload) error { f.now = r.Expires; return nil })
	saved, loadErr := f.journal.Load(r.ID)
	if err == nil || loadErr != nil || saved.State != Failed {
		t.Fatal("expired completion reported success")
	}
}
