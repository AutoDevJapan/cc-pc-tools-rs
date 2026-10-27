package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	oc "github.com/AutoDevJapan/cc-pc-tools-rs/repair-guard/internal/officialci3893"
)

func expiredJournalFixture(t *testing.T) (*official3894Journal, oc.Receipt, oc.Requalification) {
	t.Helper()
	s, pending := official3894JournalFixture(t)
	r := pending
	r.Created = time.Now().Add(-25 * time.Hour).UTC()
	r.Expires = r.Created.Add(oc.ReceiptLifetime)
	r.State = oc.Complete
	r.Binding = oc.FixedBinding(strings.Repeat("a", 40), strings.Repeat("b", 64), strings.Repeat("d", 64), "synthetic-creator")
	r.Asset = oc.OfficialAsset()
	r.Parent = oc.Identity{Volume: 1, FileID: 2, Birth: 3}
	r.Root = oc.Identity{Volume: 1, FileID: 4, Birth: 5}
	r.Files = []oc.FileRecord{{Name: "synthetic.txt", Size: 1, SHA256: strings.Repeat("e", 64), Identity: oc.Identity{Volume: 1, FileID: 6, Birth: 7}}}
	if err := s.Replace(pending, r); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(r)
	now := time.Now().UTC()
	q := oc.Requalification{Domain: oc.RequalificationDomain, OriginalReceiptSHA256: official3894Hash(b), AcceptedAt: now, Evidence: oc.Evidence{
		Parent: r.Parent, Binding: r.Binding, Asset: r.Asset, Observed: now, FreshOwnedAnchor: true, SourceVerified: true, JobVerified: true, Active: true, OfficialPublicAsset: true, ActorJob: "synthetic-current-3888-job",
	}}
	return s, r, q
}

func journalBytes(t *testing.T, s *official3894Journal) []byte {
	t.Helper()
	if _, err := s.file.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(s.file)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestOfficialCI3387RequalificationNativePersistenceAndReplay(t *testing.T) {
	for _, outcome := range []string{"consumed", "interrupted", "restored-prefix", "torn-tail"} {
		t.Run(outcome, func(t *testing.T) {
			s, r, q := expiredJournalFixture(t)
			original := journalBytes(t, s)
			if err := s.RequalifyForConsumption(r, q); err != nil {
				t.Fatal(err)
			}
			appended := journalBytes(t, s)
			if !bytes.HasPrefix(appended, original) || len(appended) == len(original) {
				t.Fatal("original authenticated frames changed")
			}
			if s.ticket.Receipt.State != oc.Consuming || s.ticket.Requalification == nil {
				t.Fatal("qualification and consuming were not committed together")
			}
			if _, err := official3894OpenStore(s.path, true); err == nil {
				t.Fatal("requalification enabled another preparation")
			}
			if _, err := official3894OpenStore(s.path, false); err == nil {
				t.Fatal("concurrent journal writer admitted")
			}
			if err := s.RequalifyForConsumption(r, q); err == nil {
				t.Fatal("second claim admitted")
			}
			if outcome == "consumed" {
				next := *s.ticket.Receipt
				next.State = oc.Consumed
				if err := s.Replace(*s.ticket.Receipt, next); err != nil {
					t.Fatal(err)
				}
			}
			path := s.path
			s.close()
			if outcome == "restored-prefix" {
				if err := os.WriteFile(path, original, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if outcome == "torn-tail" {
				if err := os.WriteFile(path, append(appended, 1, 2), 0600); err != nil {
					t.Fatal(err)
				}
			}
			reopened, err := official3894OpenStore(path, false)
			if outcome == "torn-tail" {
				if err == nil {
					reopened.close()
					t.Fatal("torn frame admitted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.close()
			loaded, err := reopened.Load(r.ID)
			if outcome == "restored-prefix" {
				if err == nil {
					t.Fatal("restored pre-consumption prefix replayed")
				}
				return
			}
			want := oc.Consuming
			if outcome == "consumed" {
				want = oc.Consumed
			}
			if err != nil || loaded.State != want || !reflect.DeepEqual(reopened.ticket.Requalification, &q) {
				t.Fatal("restart lost durable qualification", err)
			}
			loaded.State = oc.Complete
			if !reflect.DeepEqual(loaded, r) {
				t.Fatal("original receipt fields changed")
			}
		})
	}
}

func TestOfficialCI3387RequalificationRejectsChangedProofBeforeClaim(t *testing.T) {
	for _, kind := range []string{"digest", "id", "domain", "binding", "asset", "time", "stale", "future", "actor", "stopped", "original-lifetime"} {
		t.Run(kind, func(t *testing.T) {
			s, r, q := expiredJournalFixture(t)
			before := journalBytes(t, s)
			switch kind {
			case "digest":
				q.OriginalReceiptSHA256 = strings.Repeat("f", 64)
			case "id":
				r.ID = strings.Repeat("f", 32)
			case "domain":
				q.Domain = "other"
			case "binding":
				q.Evidence.Binding.Attempt++
			case "asset":
				q.Evidence.Asset.ID++
			case "time":
				q.AcceptedAt = r.Expires.Add(-time.Second)
				q.Evidence.Observed = q.AcceptedAt
			case "stale":
				q.Evidence.Observed = q.AcceptedAt.Add(-time.Minute - time.Nanosecond)
			case "future":
				q.Evidence.Observed = q.AcceptedAt.Add(time.Nanosecond)
			case "actor":
				q.Evidence.ActorJob = ""
			case "stopped":
				q.Evidence.Active = false
			case "original-lifetime":
				r.Expires = r.Expires.Add(time.Second)
			}
			if err := s.RequalifyForConsumption(r, q); err == nil {
				t.Fatal("invalid proof accepted")
			}
			if !bytes.Equal(before, journalBytes(t, s)) {
				t.Fatal("invalid proof wrote journal")
			}
			if _, err := os.Lstat(s.path + ".consuming"); !os.IsNotExist(err) {
				t.Fatal("invalid proof claimed consumption")
			}
		})
	}
}

func TestOfficialCI3387RequalificationRetainsProofAndInterruptedClaim(t *testing.T) {
	for _, kind := range []string{"proof-change", "proof-removal", "frame-limit"} {
		t.Run(kind, func(t *testing.T) {
			s, r, q := expiredJournalFixture(t)
			if kind == "frame-limit" {
				for s.sequence < 16 {
					if err := s.append(s.ticket); err != nil {
						t.Fatal(err)
					}
				}
				before := journalBytes(t, s)
				if err := s.RequalifyForConsumption(r, q); err == nil {
					t.Fatal("claim ignored frame limit")
				}
				if !bytes.Equal(before, journalBytes(t, s)) {
					t.Fatal("full journal was changed")
				}
				if _, err := s.Load(r.ID); err == nil {
					t.Fatal("interrupted claim replayed complete prefix")
				}
				return
			}
			if err := s.RequalifyForConsumption(r, q); err != nil {
				t.Fatal(err)
			}
			before := journalBytes(t, s)
			changed := s.ticket
			if kind == "proof-removal" {
				changed.Requalification = nil
			} else {
				copy := *changed.Requalification
				copy.Evidence.ActorJob = "other"
				changed.Requalification = &copy
			}
			if err := s.append(changed); err == nil {
				t.Fatal("accepted proof changed during consumption")
			}
			if !bytes.Equal(before, journalBytes(t, s)) {
				t.Fatal("changed proof was appended")
			}
		})
	}
}

func TestOfficialCI3387SourceBudgetKnownSoftwarePair(t *testing.T) {
	if !official3894InstalledBaseline("ad5d37c7b216264d2e99007a2e6f3468e7529803454d88f1954cc74a7beb4783", "0d97c1952ff6f8b47a3d40939941639b35309408") {
		t.Fatal("accepted PR586 software cannot be compared by its successor")
	}
	for _, p := range [][2]string{{official3894SharedGuardSHA, official3894ConcurrentGuardHead}, {official3894ConcurrentGuardSHA, official3894SharedGuardHead}, {strings.Repeat("0", 64), official3894SharedGuardHead}} {
		if official3894InstalledBaseline(p[0], p[1]) {
			t.Fatal("unknown or mixed software pair was adopted")
		}
	}
}

func TestOfficialCI3387RequalificationMetadataAndCLI(t *testing.T) {
	s, r, q := expiredJournalFixture(t)
	if err := s.RequalifyForConsumption(r, q); err != nil {
		t.Fatal(err)
	}
	summary := official3894Summary(s.ticket)
	if summary["expired_at_requalification"] != true || summary["receipt_deadline_extended"] != false || summary["original_receipt_expires_at"] != r.Expires || summary["requalification_actor_job"] != q.Evidence.ActorJob || summary["access_allowed"] != false || summary["launch_performed"] != false {
		t.Fatal("qualification concealed original expiry or granted access")
	}
	id := strings.Repeat("a", 32)
	flag := "--requalify-consume-official-ci3387"
	for _, args := range [][]string{{flag}, {flag, strings.ToUpper(id)}, {flag, id, "--expires", "later"}, {flag + "=" + id}, {flag, id, flag, id}} {
		var out bytes.Buffer
		calls := 0
		code := runOfficial3894CLI(args, &out, func(context.Context, string, string, func(object) error) (object, error) { calls++; return nil, nil })
		var got object
		if code != 2 || calls != 0 || json.Unmarshal(out.Bytes(), &got) != nil || got["reason"] != "arguments" {
			t.Fatal("invalid requalification arguments dispatched")
		}
	}
	for _, failure := range []string{"", "no-handoff", "terminal-save", "callback-failure"} {
		t.Run(failure, func(t *testing.T) {
			var out bytes.Buffer
			calls := 0
			code := runOfficial3894CLI([]string{flag, id}, &out, func(ctx context.Context, op, receipt string, handoff func(object) error) (object, error) {
				calls++
				if op != "requalify-consume" || receipt != id {
					t.Error("changed operation or receipt")
				}
				if _, ok := ctx.Deadline(); !ok {
					t.Error("unbounded operation")
				}
				if failure == "no-handoff" {
					return summary, nil
				}
				if failure == "callback-failure" {
					return summary, errors.New("synthetic failure")
				}
				body := object{"files": []oc.Payload{{Name: "synthetic.txt", Bytes: []byte("x")}}, "expired_at_requalification": true, "receipt_deadline_extended": false}
				if err := handoff(body); err != nil {
					return nil, err
				}
				if failure == "terminal-save" {
					return body, errors.New("synthetic terminal save failure")
				}
				return body, nil
			})
			decoder := json.NewDecoder(&out)
			var got object
			if calls != 1 || decoder.Decode(&got) != nil || decoder.Decode(&object{}) != io.EOF {
				t.Fatal("handoff was missing or repeated")
			}
			want := 0
			if failure != "" {
				want = 1
			}
			if code != want {
				t.Fatal("exit did not reflect durable consumption")
			}
			if (failure == "no-handoff" || failure == "callback-failure") && got["files"] != nil {
				t.Fatal("failed consumption leaked public payload")
			}
		})
	}
}
