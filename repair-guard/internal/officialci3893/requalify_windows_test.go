package officialci3893

import (
	"context"
	"errors"
	"os"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type requalificationFixtureJournal struct {
	*fixtureJournal
	qualification *Requalification
}

func (j *requalificationFixtureJournal) RequalifyForConsumption(old Receipt, q Requalification) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	r, err := j.read(old.ID)
	if err != nil || !reflect.DeepEqual(old, r) || r.State != Complete || j.qualification != nil || !q.ValidFor(r) {
		return ErrDenied
	}
	next := r
	next.State = Consuming
	j.records[r.ID] = seal(next)
	j.qualification = &q
	return nil
}

func expiredFixture(t *testing.T) (*fixture, Receipt, *requalificationFixtureJournal) {
	t.Helper()
	f := newFixture(t)
	r := f.create(t)
	f.now = r.Expires.Add(time.Hour)
	f.authority.evidence.Observed = f.now
	f.authority.evidence.ActorJob = "synthetic-current-3888-job"
	j := &requalificationFixtureJournal{fixtureJournal: f.journal}
	f.engine.journal = j
	return f, r, j
}

func TestExpiredOriginalRequalificationConsumesSameSnapshotsOnce(t *testing.T) {
	f, r, j := expiredFixture(t)
	before := f.journal.records[r.ID]
	if err := f.engine.Resume(context.Background(), r.ID); ValidationReason(err) != "receipt_expired" {
		t.Fatalf("ordinary verification changed: %v", err)
	}
	if err := f.engine.Consume(context.Background(), r.ID, func([]Payload) error {
		t.Fatal("ordinary consumption of an expired receipt")
		return nil
	}); ValidationReason(err) != "receipt_expired" {
		t.Fatalf("ordinary consumption changed: %v", err)
	}
	if !reflect.DeepEqual(before, f.journal.records[r.ID]) || f.fs.reads.Load() != 0 {
		t.Fatal("ordinary expiry refusal changed or read the original software")
	}
	calls := 0
	if err := f.engine.RequalifyConsume(context.Background(), r.ID, func(p []Payload) error {
		calls++
		if len(p) != 2 || p[0].Name != "bin/example.txt" || string(p[0].Bytes) != "synthetic public bytes" {
			t.Error("different public snapshots")
		}
		return nil
	}); err != nil {
		t.Fatalf("fresh qualification could not hand off the original expired software: %v", err)
	}
	saved, err := f.journal.Load(r.ID)
	if err != nil || saved.State != Consumed || calls != 1 || f.archive.calls != 1 {
		t.Fatal("one-shot consumption was not durably completed")
	}
	saved.State = Complete
	if !reflect.DeepEqual(r, saved) {
		t.Fatal("original expiry, manifest, binding or physical identities changed")
	}
	if j.qualification == nil || !j.qualification.ValidFor(saved) || !j.qualification.AcceptedAt.Equal(f.now) {
		t.Fatal("current qualification was not bound to the original receipt")
	}
	if err := f.engine.RequalifyConsume(context.Background(), r.ID, func([]Payload) error { calls++; return nil }); err == nil || calls != 1 {
		t.Fatal("repeated requalification replayed software")
	}
}

func TestExpiredOriginalRequalificationRejectsBeforeWriting(t *testing.T) {
	for _, change := range []string{"not-expired", "pending", "consuming", "consumed", "failed", "binding", "asset", "parent", "lifetime", "manifest", "evidence-stale", "evidence-future", "evidence-binding", "stopped", "actor-missing", "physical", "cancelled", "no-journal-capability", "corrupt"} {
		t.Run(change, func(t *testing.T) {
			f, r, j := expiredFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch change {
			case "not-expired":
				f.now = r.Expires.Add(-time.Second)
				f.authority.evidence.Observed = f.now
			case "pending":
				r.State = Pending
			case "consuming":
				r.State = Consuming
			case "consumed":
				r.State = Consumed
			case "failed":
				r.State = Failed
			case "binding":
				r.Binding.RunID++
			case "asset":
				r.Asset.ID++
			case "parent":
				r.Parent.FileID++
			case "lifetime":
				r.Expires = r.Expires.Add(time.Second)
			case "manifest":
				r.Files = nil
			case "evidence-stale":
				f.authority.evidence.Observed = f.now.Add(-time.Minute - time.Nanosecond)
			case "evidence-future":
				f.authority.evidence.Observed = f.now.Add(time.Nanosecond)
			case "evidence-binding":
				f.authority.evidence.Binding.JobID++
			case "stopped":
				f.authority.evidence.Active = false
			case "actor-missing":
				f.authority.evidence.ActorJob = ""
			case "physical":
				if err := os.Remove(f.path(r, "readme.txt")); err != nil {
					t.Fatal(err)
				}
			case "cancelled":
				cancel()
			case "no-journal-capability":
				f.engine.journal = f.journal
			}
			f.journal.records[r.ID] = seal(r)
			if change == "corrupt" {
				v := f.journal.records[r.ID]
				v.bytes[0] ^= 1
				f.journal.records[r.ID] = v
			}
			before := f.journal.records[r.ID]
			called := false
			err := f.engine.RequalifyConsume(ctx, r.ID, func([]Payload) error { called = true; return nil })
			if err == nil || called || j.qualification != nil || !reflect.DeepEqual(before, f.journal.records[r.ID]) || f.archive.calls != 1 {
				t.Fatalf("invalid requalification changed or consumed software: %s %v", change, err)
			}
		})
	}
}

func TestExpiredOriginalRequalificationFailureIsNotReplayable(t *testing.T) {
	for _, failure := range []string{"callback", "terminal-save", "context"} {
		t.Run(failure, func(t *testing.T) {
			f, r, j := expiredFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			if failure == "terminal-save" {
				f.journal.failState = Consumed
			}
			err := f.engine.RequalifyConsume(ctx, r.ID, func([]Payload) error {
				calls++
				if failure == "callback" {
					return errors.New("synthetic handoff failure")
				}
				if failure == "context" {
					cancel()
				}
				return nil
			})
			want := Failed
			if failure == "terminal-save" {
				want = Consuming
			}
			saved, e := f.journal.Load(r.ID)
			if err == nil || e != nil || calls != 1 || saved.State != want || j.qualification == nil {
				t.Fatal("failure was not retained", err, e, saved.State)
			}
			if e = f.engine.RequalifyConsume(context.Background(), r.ID, func([]Payload) error { calls++; return nil }); e == nil || calls != 1 {
				t.Fatal("failed handoff replayed")
			}
		})
	}
}

type requalificationCheckFS struct {
	CreatedFS
	check func()
}

func (f requalificationCheckFS) WithVerified(r Receipt, use func([]Payload) error) error {
	return f.CreatedFS.WithVerified(r, func(p []Payload) error { f.check(); return use(p) })
}

func TestExpiredOriginalRequalificationRechecksAuthorityAfterPhysicalVerification(t *testing.T) {
	f, r, j := expiredFixture(t)
	before := f.journal.records[r.ID]
	checked := false
	f.engine.fs = requalificationCheckFS{f.fs, func() { checked = true; f.authority.evidence.Active = false }}
	if err := f.engine.RequalifyConsume(context.Background(), r.ID, func([]Payload) error { t.Error("stopped actor received snapshots"); return nil }); err == nil {
		t.Fatal("used pre-physical authority")
	}
	if !checked || f.fs.reads.Load() != 1 || j.qualification != nil || !reflect.DeepEqual(before, f.journal.records[r.ID]) {
		t.Fatal("did not reject after actual physical verification and before claiming consumption")
	}
}

func TestExpiredOriginalRequalificationConcurrentConsumers(t *testing.T) {
	f, r, j := expiredFixture(t)
	var calls, success atomic.Int64
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if err := f.engine.RequalifyConsume(context.Background(), r.ID, func([]Payload) error { calls.Add(1); return nil }); err == nil {
				success.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()
	saved, err := f.journal.Load(r.ID)
	if err != nil || saved.State != Consumed || calls.Load() != 1 || success.Load() != 1 || j.qualification == nil {
		t.Fatal("concurrent consumers did not share one durable claim")
	}
}
