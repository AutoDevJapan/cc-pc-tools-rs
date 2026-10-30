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
	"slices"
	"strconv"
	"strings"
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
	r, _ := fixture(t)
	r.Config.Since = r.State.Since
	statePath := filepath.Join(r.Config.Home, "unattended-state.json")
	doc, err := json.Marshal(r.State)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, doc, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadIndependentState(r.Config, r.State.Destination); err != nil {
		t.Fatal("state fixture failed before applying size constraint", err)
	}
	doc = append(doc, bytes.Repeat([]byte(" "), (8<<20)+128-len(doc))...)
	if err := os.WriteFile(statePath, doc, 0600); err != nil {
		t.Fatal(err)
	}
	state, err := loadIndependentState(r.Config, r.State.Destination)
	if err == nil || state != nil {
		t.Fatal("receipt compatibility loosened the independent state budget")
	}
	if after, err := os.ReadFile(statePath); err != nil || !bytes.Equal(doc, after) {
		t.Fatal("rejecting oversized state changed the input")
	}
}

func TestDiscoveryReportsReceiptInputFailureWithoutFurtherHTTP(t *testing.T) {
	for _, tc := range []struct {
		name, stage string
	}{
		{"foreign", "receipt_registry_domain"},
		{"missing", "receipt_registry_missing"},
		{"oversize", "receipt_registry_limit"},
		{"malformed", "receipt_registry_malformed"},
		{"wrong-type", "receipt_registry_malformed"},
		{"directory", "receipt_registry_unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) { testDiscoveryReceiptFailure(t, tc.name, tc.stage) })
	}
}

func testDiscoveryReceiptFailure(t *testing.T, input, wantStage string) {
	t.Helper()
	r, path, before := recoveryBudgetFixture(t, 1024, "foreign-ledger")
	switch input {
	case "missing", "directory":
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if input == "directory" {
			if err := os.Mkdir(path, 0700); err != nil {
				t.Fatal(err)
			}
		}
	case "oversize":
		before = bytes.Repeat([]byte(" "), 32<<20+1)
	case "malformed":
		before = []byte(`{"domain":`)
	case "wrong-type":
		before = []byte(`{"domain":"new-owner-recovery-messages","events":"synthetic-private-input"}`)
	}
	if input != "missing" && input != "directory" {
		if err := os.WriteFile(path, before, 0600); err != nil {
			t.Fatal(err)
		}
	}
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
	if err == nil || !errors.As(err, &failure) || failure.Stage != wantStage || failure.Code != "invalid_or_unavailable" || failure.Status != 0 {
		t.Fatalf("receipt failure lost its safe discovery stage: %v", err)
	}
	if input != "missing" && input != "directory" {
		after, readErr := os.ReadFile(path)
		if readErr != nil || !bytes.Equal(before, after) {
			t.Fatal("failed discovery rewrote its input")
		}
	}
	if calls.Load() != 2 || len(r.State.Requests) != 0 || len(r.State.Discovery) != 0 || r.State.ReceiptCursor != "" {
		t.Fatal("failed prerequisite advanced discovery, rewrote evidence, or sent extra HTTP")
	}
	if _, err := os.Stat(filepath.Join(r.Config.Home, "unattended-state.json")); !os.IsNotExist(err) {
		t.Fatal("failed prerequisite published intake state", err)
	}
}

func TestDiscoveryTickDiagnosticsExcludePrivateInput(t *testing.T) {
	for _, tc := range []struct {
		name, stage  string
		status       int
		botID, guild string
	}{
		{"authentication", "bot_identity_source_unavailable", 401, "400", "100"},
		{"bot-identity", "bot_identity_invalid_or_unavailable", 0, "499", "100"},
		{"forum-source", "request_forum_source_unavailable", 403, "400", "100"},
		{"forum-guild", "request_forum_invalid_or_unavailable", 0, "400", "999"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, _ := fixture(t)
			const privateMarker = "synthetic-private-marker-not-for-health"
			r.Token = privateMarker
			var calls atomic.Int32
			server(t, r, func(w http.ResponseWriter, q *http.Request) {
				calls.Add(1)
				switch q.URL.Path {
				case "/users/@me":
					if tc.name == "authentication" {
						w.WriteHeader(tc.status)
						_, _ = w.Write([]byte(privateMarker))
						return
					}
					respond(w, map[string]any{"id": tc.botID, "bot": true})
				case "/channels/" + requestForum:
					if tc.name == "forum-source" {
						w.WriteHeader(tc.status)
						_, _ = w.Write([]byte(privateMarker))
						return
					}
					respond(w, Thread{ID: requestForum, Type: 15, Guild: tc.guild})
				default:
					t.Errorf("unexpected discovery request: %s", q.URL.Path)
					w.WriteHeader(http.StatusTeapot)
				}
			})
			r.Run = func(context.Context, string, ...string) (string, error) {
				return `{"repair_mode":true,"error":null}`, nil
			}
			health := r.tick(context.Background())
			var got []string
			for _, s := range health.Errors {
				if strings.HasPrefix(s, "discovery_") || s == "discord_discovery_failed" {
					got = append(got, s)
				}
			}
			want := []string{"discord_discovery_failed", "discovery_" + tc.stage}
			if tc.status != 0 {
				want = append(want, "discovery_http_status_"+strconv.Itoa(tc.status))
			}
			encoded, err := json.Marshal(health)
			wantCalls := int32(2)
			if tc.name == "authentication" || tc.name == "bot-identity" {
				wantCalls = 1
			}
			if !slices.Equal(got, want) || err != nil || strings.Contains(string(encoded), privateMarker) || strings.Contains(string(encoded), r.Config.Home) || calls.Load() != wantCalls || health.API {
				t.Fatalf("discovery did not retain only safe diagnostics: %v, requests=%d", got, calls.Load())
			}
		})
	}
}
