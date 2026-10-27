package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
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
	onRead   func()
	failure  error
}

func (f *diagnosticFS) ParentIdentity() oc.Identity          { return f.identity }
func (*diagnosticFS) CreateRoot(string) (oc.Identity, error) { return oc.Identity{}, oc.ErrDenied }
func (*diagnosticFS) Install(string, oc.Identity, []oc.Payload) ([]oc.FileRecord, error) {
	return nil, oc.ErrDenied
}
func (f *diagnosticFS) WithVerified(oc.Receipt, func([]oc.Payload) error) error {
	f.reads++
	if f.onRead != nil {
		f.onRead()
	}
	if f.failure != nil {
		return f.failure
	}
	return errors.New("fixture-private-physical-path")
}

func TestOfficialCI3387SDKRefusalSurvivesActualCLI(t *testing.T) {
	for _, change := range []string{"journal", "expiry", "state", "physical", "evidence-stale", "physical-and-cancelled"} {
		t.Run(change, func(t *testing.T) {
			now := time.Now().UTC()
			binding := oc.FixedBinding(strings.Repeat("a", 40), strings.Repeat("b", 64), strings.Repeat("c", 64), "synthetic-creator")
			asset := oc.OfficialAsset()
			fs := &diagnosticFS{identity: oc.Identity{Volume: 1, FileID: 2, Birth: 3}}
			journal := &diagnosticJournal{receipt: oc.Receipt{Domain: oc.Domain, ID: strings.Repeat("d", 32), Binding: binding, Asset: asset, State: oc.Complete, Created: now.Add(-time.Hour), Expires: now.Add(23 * time.Hour), Parent: fs.identity, Files: []oc.FileRecord{{Name: "synthetic-public.txt"}}}}
			want := map[string]string{"journal": "sdk_receipt_load_failed", "expiry": "sdk_receipt_expired", "state": "sdk_receipt_not_complete", "physical": "sdk_physical_tree_not_verified", "evidence-stale": "sdk_authority_evidence_stale", "physical-and-cancelled": "sdk_physical_tree_not_verified"}[change]
			switch change {
			case "journal":
				journal.fail = true
			case "expiry":
				journal.receipt.Expires = now.Add(-time.Second)
			case "state":
				journal.receipt.State = oc.Consumed
			}
			authority := diagnosticAuthority{oc.Evidence{Binding: binding, Asset: asset, Parent: fs.identity, Observed: now, SourceVerified: true, JobVerified: true, Active: true, FreshOwnedAnchor: true, OfficialPublicAsset: true}}
			if change == "evidence-stale" {
				authority.evidence.Observed = now.Add(-61 * time.Second)
			}
			engine, err := oc.New(binding, authority, diagnosticArchive{}, journal, fs)
			if err != nil {
				t.Fatal(err)
			}
			var out bytes.Buffer
			var originalPhysicalError error
			var preservedPhysicalError bool
			code := runOfficial3894CLI([]string{"--verify-official-ci3387", journal.receipt.ID}, &out, func(ctx context.Context, _, id string, _ func(object) error) (object, error) {
				if change == "physical-and-cancelled" {
					var cancel context.CancelFunc
					ctx, cancel = context.WithCancel(ctx)
					defer cancel()
					fs.onRead = cancel
					originalPhysicalError = errors.New("fixture-private-physical-detail")
					fs.failure = originalPhysicalError
				}
				err := engine.Resume(ctx, id)
				preservedPhysicalError = originalPhysicalError != nil && errors.Is(err, originalPhysicalError)
				return object{"state": oc.Complete, "reason": "purpose_or_io", "files": "must-not-escape"}, err
			})
			var result object
			if code != 1 || json.Unmarshal(out.Bytes(), &result) != nil || result["reason"] != want || result["purpose_verified"] != false || result["files"] != nil || journal.writes != 0 || strings.Contains(out.String(), "fixture-private") {
				t.Fatalf("diagnostic lost, leaked, or mutated receipt: change=%s code=%d result=%s", change, code, out.String())
			}
			if change == "physical-and-cancelled" && (!preservedPhysicalError || fs.reads != 1) {
				t.Fatal("physical error was replaced by cancellation")
			}
			if change != "physical" && change != "physical-and-cancelled" && fs.reads != 0 {
				t.Fatal("invalid receipt reached physical payload")
			}
		})
	}
}

func TestOfficialCI3387UpdaterRecognizesPreviousNormalRepair(t *testing.T) {
	raw, err := os.ReadFile("../docs/evidence/official-ci3387-known-guard-baseline-521c4c4d.json")
	if err != nil {
		t.Fatal(err)
	}
	var published struct {
		Domain       string `json:"domain"`
		RepositoryID int64  `json:"repository_id"`
		Source       string `json:"source_head"`
		SHA          string `json:"software_sha256"`
		PR           int    `json:"pull_request"`
	}
	if json.Unmarshal(raw, &published) != nil || published.Domain != "official-ci3387-known-guard-baseline-v1" || published.RepositoryID != 1406818713 || published.PR != 567 || !official3894InstalledBaseline(published.SHA, published.Source) {
		t.Fatal("the normally merged and installed PR567 software cannot be compared")
	}
	if official3894InstalledBaseline("434832534d43cbe954aa1c2e573879556bb840818e830a489b744e6e2744740f", strings.Repeat("a", 40)) {
		t.Fatal("observed hash accepted with unrelated source")
	}
}

func TestOfficialCI3387UpdaterRecognizesDeployedDiagnostic(t *testing.T) {
	raw, err := os.ReadFile("../docs/evidence/official-ci3387-known-guard-baseline-7cea2e8f.json")
	if err != nil {
		t.Fatal(err)
	}
	var published struct {
		Domain       string `json:"domain"`
		RepositoryID int64  `json:"repository_id"`
		Source       string `json:"source_head"`
		SHA          string `json:"software_sha256"`
		Merge        string `json:"merge_commit"`
		PR           int    `json:"pull_request"`
	}
	if json.Unmarshal(raw, &published) != nil || published.Domain != "official-ci3387-known-guard-baseline-v1" || published.RepositoryID != 1406818713 || published.PR != 582 || published.Merge != "e9674d8abe4167058c67117183ce7b481a311f00" || published.SHA != official3894DiagnosticGuardSHA || published.Source != official3894DiagnosticGuardHead || !official3894InstalledBaseline(published.SHA, published.Source) {
		t.Fatal("the normally merged and deployed PR582 diagnostic cannot be compared")
	}
	if official3894InstalledBaseline(published.SHA, strings.Repeat("a", 40)) || official3894InstalledBaseline(strings.Repeat("b", 64), published.Source) || official3894InstalledBaseline(official3894RepairGuardSHA, published.Source) {
		t.Fatal("known diagnostic software accepted with another source or hash")
	}
}

func TestOfficialCI3387UpdaterRecognizesConcurrentPublishedSoftware(t *testing.T) {
	raw, err := os.ReadFile("../docs/evidence/official-ci3387-known-guard-baseline-81700412.json")
	if err != nil {
		t.Fatal(err)
	}
	var record struct {
		Domain       string `json:"domain"`
		RepositoryID int64  `json:"repository_id"`
		Source       string `json:"source_head"`
		SHA          string `json:"software_sha256"`
		PR           int    `json:"pull_request"`
		Evidence     string `json:"evidence_source"`
	}
	if json.Unmarshal(raw, &record) != nil || record.Domain != "official-ci3387-known-guard-baseline-v1" || record.RepositoryID != 1406818713 || record.PR != 585 || record.Evidence != "normal-todo-note-3686" || record.SHA != official3894ConcurrentGuardSHA || record.Source != official3894ConcurrentGuardHead || !official3894InstalledBaseline(record.SHA, record.Source) {
		t.Fatal("normal concurrent software distribution cannot be compared")
	}
	if official3894InstalledBaseline(record.SHA, official3894DiagnosticGuardHead) || official3894InstalledBaseline(official3894DiagnosticGuardSHA, record.Source) || official3894InstalledBaseline(strings.Repeat("c", 64), record.Source) {
		t.Fatal("unknown or mismatched concurrent software identity accepted")
	}
}
