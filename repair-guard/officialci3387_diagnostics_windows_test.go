package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	oc "github.com/AutoDevJapan/cc-pc-tools-rs/repair-guard/internal/officialci3893"
)

type diagnosticAuthority struct{ evidence oc.Evidence }

func (a diagnosticAuthority) Observe(context.Context) (oc.Evidence, error) { return a.evidence, nil }

type diagnosticArchive struct{}

func (diagnosticArchive) Open(context.Context, oc.Asset) (io.ReadCloser, error) {
	return nil, errors.New("archive must not open")
}

type diagnosticJournal struct {
	receipt oc.Receipt
	fail    bool
	writes  int
}

func (j *diagnosticJournal) Reserve(oc.Receipt) error             { j.writes++; return oc.ErrDenied }
func (j *diagnosticJournal) Replace(oc.Receipt, oc.Receipt) error { j.writes++; return oc.ErrDenied }
func (j *diagnosticJournal) Load(string) (oc.Receipt, error) {
	if j.fail {
		return oc.Receipt{}, errors.New("fixture-private-journal-detail")
	}
	return j.receipt, nil
}

type diagnosticFS struct {
	identity oc.Identity
	reads    int
}

func (f *diagnosticFS) ParentIdentity() oc.Identity          { return f.identity }
func (*diagnosticFS) CreateRoot(string) (oc.Identity, error) { return oc.Identity{}, oc.ErrDenied }
func (*diagnosticFS) Install(string, oc.Identity, []oc.Payload) ([]oc.FileRecord, error) {
	return nil, oc.ErrDenied
}
func (f *diagnosticFS) WithVerified(oc.Receipt, func([]oc.Payload) error) error {
	f.reads++
	return errors.New("fixture-private-physical-path")
}

func TestOfficialCI3387SDKRefusalSurvivesActualCLI(t *testing.T) {
	for _, change := range []string{"journal", "expiry", "state", "physical"} {
		t.Run(change, func(t *testing.T) {
			now := time.Now().UTC()
			binding := oc.FixedBinding(strings.Repeat("a", 40), strings.Repeat("b", 64), strings.Repeat("c", 64), "synthetic-creator")
			asset := oc.OfficialAsset()
			fs := &diagnosticFS{identity: oc.Identity{Volume: 1, FileID: 2, Birth: 3}}
			journal := &diagnosticJournal{receipt: oc.Receipt{Domain: oc.Domain, ID: strings.Repeat("d", 32), Binding: binding, Asset: asset, State: oc.Complete, Created: now.Add(-time.Hour), Expires: now.Add(23 * time.Hour), Parent: fs.identity, Files: []oc.FileRecord{{Name: "synthetic-public.txt"}}}}
			want := map[string]string{"journal": "sdk_receipt_load_failed", "expiry": "sdk_receipt_expired", "state": "sdk_receipt_not_complete", "physical": "sdk_physical_tree_not_verified"}[change]
			switch change {
			case "journal":
				journal.fail = true
			case "expiry":
				journal.receipt.Expires = now.Add(-time.Second)
			case "state":
				journal.receipt.State = oc.Consumed
			}
			authority := diagnosticAuthority{oc.Evidence{Binding: binding, Asset: asset, Parent: fs.identity, Observed: now, SourceVerified: true, JobVerified: true, Active: true, FreshOwnedAnchor: true, OfficialPublicAsset: true}}
			engine, err := oc.New(binding, authority, diagnosticArchive{}, journal, fs)
			if err != nil {
				t.Fatal(err)
			}
			var out bytes.Buffer
			code := runOfficial3894CLI([]string{"--verify-official-ci3387", journal.receipt.ID}, &out, func(ctx context.Context, _, id string, _ func(object) error) (object, error) {
				return object{"state": oc.Complete, "reason": "purpose_or_io", "files": "must-not-escape"}, engine.Resume(ctx, id)
			})
			var result object
			if code != 1 || json.Unmarshal(out.Bytes(), &result) != nil || result["reason"] != want || result["purpose_verified"] != false || result["files"] != nil || journal.writes != 0 || strings.Contains(out.String(), "fixture-private") {
				t.Fatalf("diagnostic lost, leaked, or mutated receipt: change=%s code=%d result=%s", change, code, out.String())
			}
			if change != "physical" && fs.reads != 0 {
				t.Fatal("invalid receipt reached physical payload")
			}
		})
	}
}

func TestOfficialCI3387UpdaterRecognizesPreviousNormalRepair(t *testing.T) {
	if !official3894InstalledBaseline("434832534d43cbe954aa1c2e573879556bb840818e830a489b744e6e2744740f", "521c4c4dddd7abbb0135d13bffb380ec984374b8") {
		t.Fatal("the normally merged and installed PR567 software cannot be compared")
	}
	if official3894InstalledBaseline("434832534d43cbe954aa1c2e573879556bb840818e830a489b744e6e2744740f", strings.Repeat("a", 40)) {
		t.Fatal("observed hash accepted with unrelated source")
	}
}
