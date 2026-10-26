package officialci3893

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

func TestResumeRefusalBoundariesPreserveReceipt(t *testing.T) {
	for _, change := range []string{"journal", "binding", "asset", "state", "future", "expiry", "lifetime", "manifest", "parent", "physical", "evidence-stale", "evidence-future", "evidence-binding"} {
		t.Run(change, func(t *testing.T) {
			f := newFixture(t)
			r := f.create(t)
			want := map[string]string{
				"journal": "receipt_load_failed", "binding": "receipt_binding_changed", "asset": "receipt_asset_changed",
				"state": "receipt_not_complete", "future": "receipt_created_in_future", "expiry": "receipt_expired",
				"lifetime": "receipt_lifetime_changed", "manifest": "receipt_manifest_missing", "parent": "receipt_parent_changed",
				"physical":       "physical_tree_not_verified",
				"evidence-stale": "authority_evidence_stale", "evidence-future": "authority_evidence_in_future", "evidence-binding": "authority_evidence_changed",
			}[change]
			switch change {
			case "journal":
				delete(f.journal.records, r.ID)
			case "binding":
				r.Binding.PR++
			case "asset":
				r.Asset.ID++
			case "state":
				r.State = Consumed
			case "future":
				r.Created = f.now.Add(1)
			case "expiry":
				f.now = r.Expires
				f.authority.evidence.Observed = f.now
			case "lifetime":
				r.Expires = r.Expires.Add(-1)
			case "manifest":
				r.Files = nil
			case "parent":
				r.Parent.FileID++
			case "physical":
				if err := os.Remove(f.path(r, "readme.txt")); err != nil {
					t.Fatal(err)
				}
			case "evidence-stale":
				f.authority.evidence.Observed = f.now.Add(-time.Minute - time.Nanosecond)
			case "evidence-future":
				f.authority.evidence.Observed = f.now.Add(time.Nanosecond)
			case "evidence-binding":
				f.authority.evidence.Binding.RunID++
			}
			if change != "journal" {
				f.journal.records[r.ID] = seal(r)
			}
			before := f.journal.records[r.ID]
			err := f.engine.Resume(context.Background(), r.ID)
			if err == nil || err.Error() != "official CI: "+want || !errors.Is(err, ErrDenied) {
				t.Fatalf("refusal boundary lost: change=%s err=%v", change, err)
			}
			after := f.journal.records[r.ID]
			if string(before.bytes) != string(after.bytes) || string(before.mac) != string(after.mac) {
				t.Fatal("read-only refusal modified the original receipt")
			}
		})
	}
}
