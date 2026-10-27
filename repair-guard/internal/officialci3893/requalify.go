package officialci3893

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

const RequalificationDomain = "official-ci3387-expired-original-consumption-v1"

// Requalification records the new live evidence for one in-flight consumption.
// It has no expiry or reusable grant. The original receipt is retained verbatim
// apart from its normal Complete -> Consuming -> Consumed/Failed state changes.
type Requalification struct {
	Domain, OriginalReceiptSHA256 string
	Evidence                      Evidence
	AcceptedAt                    time.Time
}

// RequalificationJournal must atomically retain this record and Consuming,
// after its independent CreateNew replay marker, before any public handoff.
// An old journal without this additional capability cannot requalify anything.
type RequalificationJournal interface {
	RequalifyForConsumption(Receipt, Requalification) error
}

func originalReceiptDigest(r Receipt) string {
	r.State = Complete
	b, err := json.Marshal(r)
	if err != nil {
		return ""
	}
	return digest(b)
}

func (q Requalification) EvidenceSHA256() string {
	b, _ := json.Marshal(q.Evidence)
	return digest(b)
}

// ValidFor also runs when authenticated journal frames are read after restart.
// It never qualifies a path, observes live authority or changes a deadline.
func (q Requalification) ValidFor(r Receipt) bool {
	o := q.Evidence
	age := q.AcceptedAt.Sub(o.Observed)
	return q.Domain == RequalificationDomain && r.Domain == Domain && hexN(r.ID, 32) && hexN(q.OriginalReceiptSHA256, 64) &&
		q.OriginalReceiptSHA256 == originalReceiptDigest(r) &&
		(r.State == Complete || r.State == Consuming || r.State == Consumed || r.State == Failed) &&
		r.Expires.Sub(r.Created) == ReceiptLifetime && !q.AcceptedAt.Before(r.Expires) &&
		!o.Observed.IsZero() && age >= 0 && age <= time.Minute &&
		o.Binding == r.Binding && o.Asset == r.Asset && o.Parent == r.Parent &&
		o.FreshOwnedAnchor && o.SourceVerified && o.JobVerified && o.Active && o.OfficialPublicAsset &&
		o.ActorJob != "" && len(o.ActorJob) <= 256
}

// RequalifyConsume is an explicit one-shot operation on the original expired
// receipt. It does not establish a new reusable deadline or create software.
func (e *Engine) RequalifyConsume(ctx context.Context, id string, use func([]Payload) error) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	j, ok := e.journal.(RequalificationJournal)
	if use == nil || !ok {
		return ErrDenied
	}
	r, err := e.loadReceipt(ctx, id, true)
	if err != nil {
		return err
	}
	entered, claimed := false, false
	err = e.fs.WithVerified(r, func(p []Payload) error {
		entered = true
		// Recheck live authority after all physical objects and their snapshots
		// are verified and pinned; the old receipt itself supplies no permission.
		o, err := e.observe(ctx)
		if err != nil {
			return err
		}
		q := Requalification{RequalificationDomain, originalReceiptDigest(r), o, e.now().UTC()}
		if !q.ValidFor(r) {
			return ErrDenied
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		if err = j.RequalifyForConsumption(r, q); err != nil {
			return err
		}
		claimed = true
		r.State = Consuming
		if err = ctx.Err(); err != nil {
			return err
		}
		return use(p)
	})
	if err != nil {
		if claimed {
			_, err = e.fail(r, err)
		} else if !entered {
			err = validationFailure{validationPhysical, err}
		}
		return err
	}
	if !claimed {
		return ErrDenied
	}
	if err = ctx.Err(); err != nil {
		_, err = e.fail(r, err)
		return err
	}
	next := r
	next.State = Consumed
	if err = e.journal.Replace(r, next); err != nil {
		return fmt.Errorf("consumer finished; receipt commit failed: %w", err)
	}
	return nil
}
