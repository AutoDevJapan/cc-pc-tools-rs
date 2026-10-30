package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

// Padding exercises the JSON document budget without manufacturing a native
// sender's approval or loading any live receipt, body, credential, or ledger.
func recoveryBudgetFixture(t *testing.T, size int, domain string) (*Runner, string, []byte) {
	t.Helper()
	r := &Runner{
		Config: Config{Home: filepath.Join(t.TempDir(), "home")},
		State: &State{Since: "2026-10-13T00:00:00Z", Destination: Destination{Guild: "100", Forum: "200", Owner: "300", Bot: "400"},
			Threads: map[string]Thread{}, Scans: map[string]*Scan{}, Requests: map[string]*Request{}},
		Now: func() time.Time { return time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC) },
	}
	doc, err := json.Marshal(map[string]any{"domain": domain, "version": 1, "events": []any{
		map[string]any{"event": "verified", "request_id": "synthetic-question", "phase": "question:0", "receipt": map[string]any{
			"thread_id": "500", "message_id": "600", "author_id": "400", "timestamp": "2026-10-20T00:00:00Z", "flags": 4100}},
	}})
	if err != nil || len(doc) > size {
		t.Fatal("invalid synthetic fixture size", err)
	}
	doc = append(doc, bytes.Repeat([]byte(" "), size-len(doc))...)
	path := filepath.Join(filepath.Dir(r.Config.Home), "recovery-messages.json")
	if err := os.WriteFile(path, doc, 0600); err != nil {
		t.Fatal(err)
	}
	return r, path, doc
}

func TestRecoveryReceiptDocumentBudgetMatchesSender(t *testing.T) {
	for _, size := range []int{8<<20 + 128, 32 << 20} {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			r, path, before := recoveryBudgetFixture(t, size, "new-owner-recovery-messages")
			known, err := r.knownBotThreads()
			if err != nil || len(known) != 1 || !known["500"] || !r.KnownQuestionThreads["500"] {
				t.Fatalf("valid receipt projection inside sender budget was unavailable: %v", err)
			}
			// Another consumer must distinguish an absent exact receipt from an
			// unreadable document, while making no request or new origin claim.
			_, err = r.recoverProductNotice(context.Background(), "absent-request", "", "")
			if !errors.Is(err, errNoticeMissing) {
				t.Fatalf("missing receipt was confused with unreadable input: %v", err)
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(before, after) || len(r.State.Requests) != 0 {
				t.Fatal("read-only receipt inspection changed input or registered work")
			}
		})
	}
}

func TestRecoveryReceiptOversizeMalformedAndForeignRemainUnavailable(t *testing.T) {
	for _, tc := range []struct {
		name   string
		size   int
		domain string
		broken bool
	}{
		{"over-sender-budget", 32<<20 + 1, "new-owner-recovery-messages", false},
		{"foreign-domain", 1024, "foreign-ledger", false},
		{"malformed", 1024, "new-owner-recovery-messages", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, path, before := recoveryBudgetFixture(t, tc.size, tc.domain)
			if tc.broken {
				before = []byte(`{"domain":`)
				if err := os.WriteFile(path, before, 0600); err != nil {
					t.Fatal(err)
				}
			}
			r.KnownQuestionThreads = map[string]bool{"prior": true}
			known, err := r.knownBotThreads()
			if err == nil || len(known) != 0 || !r.KnownQuestionThreads["prior"] {
				t.Fatal("unavailable receipts were accepted or replaced prior evidence")
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(before, after) || len(r.State.Requests) != 0 {
				t.Fatal("unavailable input was rewritten or treated as newly registered work")
			}
		})
	}
}

func TestRecoveryReceiptBudgetDoesNotIncreaseGeneralStateBudget(t *testing.T) {
	r, path, _ := recoveryBudgetFixture(t, 8<<20+128, "unattended-owner-v1")
	var state State
	if maxState != 8<<20 || readJSON(path, maxState, &state) == nil || len(r.State.Requests) != 0 {
		t.Fatal("receipt compatibility loosened the independent state budget")
	}
}

func TestDiscoveryReportsReceiptInputFailureWithoutFurtherHTTP(t *testing.T) {
	r, path, before := recoveryBudgetFixture(t, 1024, "foreign-ledger")
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		calls.Add(1)
		switch req.URL.Path {
		case "/users/@me":
			_, _ = w.Write([]byte(`{"id":"400","bot":true}`))
		case "/channels/" + requestForum:
			_, _ = w.Write([]byte(`{"id":"` + requestForum + `","type":15,"guild_id":"100"}`))
		default:
			w.WriteHeader(http.StatusTeapot)
		}
	}))
	defer server.Close()
	r.Base, r.Client, r.Token = server.URL, server.Client(), "synthetic-token"
	err := r.discover(context.Background())
	var failure monthlyFailure
	if err == nil || !errors.As(err, &failure) || failure.Stage != "receipt_registry" || failure.Status != 0 {
		t.Fatalf("receipt failure lost its safe discovery stage: %v", err)
	}
	after, readErr := os.ReadFile(path)
	if calls.Load() != 2 || readErr != nil || !bytes.Equal(before, after) || len(r.State.Requests) != 0 || len(r.State.Discovery) != 0 {
		t.Fatal("failed prerequisite advanced discovery, rewrote evidence, or sent extra HTTP")
	}
}
