package transport

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCapacityVersionServesOnlyOneBoundMetadataRead(t *testing.T) {
	state := strings.Repeat("a", 64)
	good := fmt.Sprintf("{\"revision\":9007199254740993,\"stateId\":%q}", state)
	pair := fmt.Sprintf("{\"ok\":true,\"version\":%s}\n", good)
	type testCase struct {
		name          string
		body          string
		req           string
		status        int
		response      string
		calls         int32
		usesOtherPath bool
	}
	tests := []testCase{
		{name: "matching", body: good, response: pair, calls: 1},
		{
			name: "two requests yield one reply", body: good, response: pair, calls: 1,
			req: "{\"op\":\"capacity-version\",\"cachePath\":%q}\n{\"op\":\"capacity-version\",\"cachePath\":%q}\n",
		},
		{
			name: "zero", body: strings.Replace(good, "9007199254740993", "0", 1), calls: 1,
			response: strings.Replace(pair, "9007199254740993", "0", 1),
		},
		{
			name: "401 without private body", status: 401, body: "fixture-private-response", calls: 1,
			response: "{\"ok\":false,\"error\":\"todo_remote_http_401\"}\n",
		},
		{
			name: "old server without version", status: 404, body: "fixture-private-response", calls: 1,
			response: "{\"ok\":false,\"error\":\"todo_remote_http_404\"}\n",
		},
		{
			name: "different cache", usesOtherPath: true,
			response: "{\"ok\":false,\"error\":\"todo_remote_invalid_control_frame\"}\n",
		},
		{
			name: "write arguments rejected", req: "{\"op\":\"capacity-version\",\"cachePath\":%q,\"documentPath\":\"other\"}\n",
			response: "{\"ok\":false,\"error\":\"todo_remote_invalid_control_frame\"}\n",
		},
		{
			name: "unknown field", req: "{\"op\":\"capacity-version\",\"cachePath\":%q,\"override\":true}\n",
			response: "{\"ok\":false,\"error\":\"todo_remote_invalid_control_frame\"}\n",
		},
		{
			name: "duplicate cache path", req: "{\"op\":\"capacity-version\",\"cachePath\":\"other\",\"cachePath\":%q}\n",
			response: "{\"ok\":false,\"error\":\"todo_remote_invalid_control_frame\"}\n",
		},
	}
	invalid := map[string]string{
		"missing revision": fmt.Sprintf("{\"stateId\":%q}", state),
		"null revision": strings.Replace(good, "9007199254740993", "null", 1),
		"string revision": strings.Replace(good, "9007199254740993", "\"9007199254740993\"", 1),
		"wrong case": strings.Replace(good, "\"revision\"", "\"Revision\"", 1),
		"duplicate revision": strings.Replace(good, "\"revision\":", "\"revision\":0,\"revision\":", 1),
		"wide revision": strings.Replace(good, "9007199254740993", "18446744073709551616", 1),
		"old version without state identity": "{\"revision\":1}",
		"mangled state identity": strings.Replace(good, state, "not-a-digest", 1),
		"additional object": good + "{}",
	}
	for name, body := range invalid {
		tests = append(tests, testCase{name: name, body: body, response: "{\"ok\":false,\"error\":\"todo_remote_invalid_response\"}\n", calls: 1})
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Method != http.MethodGet || r.URL.RequestURI() != "/api/v1/version" ||
					r.Header.Get("Authorization") != "Bearer fixture-token" {
					t.Error("only the canonical authenticated version GET is allowed")
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				if test.status != 0 {
					w.WriteHeader(test.status)
				}
				_, _ = w.Write([]byte(test.body))
			}))
			t.Cleanup(server.Close)
			cache := filepath.Join(t.TempDir(), "todo.json")
			c, err := NewClient(server.URL, "fixture-token")
			if err != nil {
				t.Fatal(err)
			}
			cfg := activateFixture(t, c, cache)
			before := []byte("{\"items\":[],\"fixture\":\"should remain unchanged\"}")
			if err := os.WriteFile(cache, before, 0600); err != nil {
				t.Fatal(err)
			}
			filesBefore := capacityFixtureFiles(t, filepath.Dir(cache))
			if test.usesOtherPath {
				cache = filepath.Join(filepath.Dir(cache), "different.json")
			}
			var request string
			switch strings.Count(test.req, "%q") {
			case 0:
				request = fmt.Sprintf("{\"op\":\"capacity-version\",\"cachePath\":%q}\n", cache)
			case 1:
				request = fmt.Sprintf(test.req, cache)
			default:
				request = fmt.Sprintf(test.req, cache, cache)
			}
			var output bytes.Buffer
			if err := Serve(context.Background(), cfg, strings.NewReader(request), &output); err != nil {
				t.Fatal(err)
			}
			if output.String() != test.response || calls.Load() != test.calls {
				t.Fatalf("response = %q, calls = %d", output.String(), calls.Load())
			}
			if !reflect.DeepEqual(capacityFixtureFiles(t, filepath.Dir(cache)), filesBefore) {
				t.Fatal("version read wrote fixture files")
			}
		})
	}
}

func capacityFixtureFiles(t *testing.T, home string) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(home)
	if err != nil {
		t.Fatal(err)
	}
	files := make(map[string]string, len(entries))
	for _, entry := range entries {
		data, err := os.ReadFile(filepath.Join(home, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		files[entry.Name()] = digest(data)
	}
	return files
}

func TestCapacityVersionCancellationDoesNotBeginOrRefresh(t *testing.T) {
	entered := make(chan struct{})
	httpDone := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/version" {
			t.Error("unexpected request")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		close(entered)
		<-r.Context().Done()
		close(httpDone)
	}))
	t.Cleanup(server.Close)
	cache := filepath.Join(t.TempDir(), "todo.json")
	client, err := NewClient(server.URL, "fixture-token")
	if err != nil {
		t.Fatal(err)
	}
	cfg := activateFixture(t, client, cache)
	filesBefore := capacityFixtureFiles(t, filepath.Dir(cache))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var out bytes.Buffer
	result := make(chan error, 1)
	req := fmt.Sprintf("{\"op\":\"capacity-version\",\"cachePath\":%q}\n", cache)
	go func() { result <- Serve(ctx, cfg, strings.NewReader(req), &out) }()
	select {
	case <-entered:
		cancel()
	case <-ctx.Done():
		t.Error("version GET never reached the test server")
	}
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	select {
	case <-httpDone:
	case <-time.After(5 * time.Second):
		t.Fatal("version request did not cancel")
	}
	if out.String() != "{\"ok\":false,\"error\":\"todo_remote_network_failed\"}\n" ||
		!reflect.DeepEqual(capacityFixtureFiles(t, filepath.Dir(cache)), filesBefore) {
		t.Fatal("canceled inspection reported success or wrote fixture files")
	}
}

func TestBridgeRequiresAnUnambiguousOperationBeforeDispatch(t *testing.T) {
	for _, fixture := range []struct {
		name string
		keys string
	}{
		{"read then write", `"op":"capacity-version","op":"begin"`},
		{"write then read", `"op":"begin","op":"capacity-version"`},
		{"case alias", `"op":"capacity-version","Op":"begin"`},
		{"escaped duplicate", `"op":"capacity-version","\u006fp":"begin"`},
		{"case alias alone", `"OP":"begin"`},
		{"wrong type", `"op":7`},
		{"null", `"op":null`},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			l, c := newLedger(t)
			cache := cachePath(t)
			cfg := activateFixture(t, c, cache)
			before := capacityFixtureFiles(t, filepath.Dir(cache))
			request := fmt.Sprintf("{%s,\"cachePath\":%q}\n", fixture.keys, cache)
			var output bytes.Buffer
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			err := Serve(ctx, cfg, strings.NewReader(request), &output)
			if err == nil || err.Error() != "todo_remote_invalid_control_frame" || output.Len() != 0 {
				t.Fatal("ambiguous operation was not rejected before dispatch")
			}
			if !reflect.DeepEqual(capacityFixtureFiles(t, filepath.Dir(cache)), before) {
				t.Fatal("ambiguous operation changed the fixture")
			}
			l.mu.Lock()
			defer l.mu.Unlock()
			if l.acquires != 0 || l.versionReads != 0 || l.stateReads != 0 || l.changeReads != 0 || l.commits != 0 || l.releases != 0 {
				t.Fatal("ambiguous operation reached the fixture service")
			}
		})
	}
}

func TestBridgeSingleBeginAndCloseStillUseTheWriterSession(t *testing.T) {
	l, c := newLedger(t)
	cache := cachePath(t)
	cfg := activateFixture(t, c, cache)
	lock, err := os.OpenFile(filepath.Join(filepath.Dir(cache), "todo.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := lockFile(lock); err != nil {
		t.Fatal(err)
	}
	defer unlockFile(lock)
	request := fmt.Sprintf("{\"op\":\"begin\",\"cachePath\":%q}\n{\"op\":\"close\"}\n", cache)
	var output bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := Serve(ctx, cfg, strings.NewReader(request), &output); err != nil {
		t.Fatal(err)
	}
	if output.String() != "{\"ok\":true,\"revision\":1}\n{\"ok\":true}\n" {
		t.Fatal("valid writer framing changed")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.acquires != 1 || l.stateReads != 1 || l.releases != 1 || l.versionReads != 0 || l.commits != 0 {
		t.Fatal("valid writer did not use one acquire, snapshot and release")
	}
}

func TestOrdinarySyncFillsTheLegacyCapacitySidecar(t *testing.T) {
	for _, legacy := range []string{"omitted", "zero"} {
		t.Run(legacy, func(t *testing.T) {
			l, c := newLedger(t)
			l.mu.Lock()
			l.stateID = strings.Repeat("a", 64)
			id := l.stateID
			l.mu.Unlock()
			cache := cachePath(t)
			cfg := activateFixture(t, c, cache)
			raw := append([]byte{0xef, 0xbb, 0xbf}, []byte(" \r\n"+fixture+"\r\n ")...)
			if err := os.WriteFile(cache, raw, 0600); err != nil {
				t.Fatal(err)
			}
			sidecar := map[string]any{"revision": 1, "stateId": id, "sha256": digest(raw)}
			if legacy == "zero" {
				sidecar["bytes"] = 0
			}
			if err := os.WriteFile(cache+".remote-revision", wire(sidecar), 0600); err != nil {
				t.Fatal(err)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := Sync(ctx, cfg); err != nil {
				t.Fatal(err)
			}
			complete := cacheState{Revision: 1, StateID: id, SHA256: digest(raw), Bytes: int64(len(raw))}
			if !bytes.Equal(read(t, cache), raw) ||
				!bytes.Equal(read(t, cache+".remote-revision"), wire(complete)) {
				t.Fatal("sync did not preserve the raw cache and upgrade the sidecar")
			}
			before := capacityFixtureFiles(t, filepath.Dir(cache))
			if err := syncIfChanged(ctx, cfg, c); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(capacityFixtureFiles(t, filepath.Dir(cache)), before) {
				t.Fatal("complete sidecar was rewritten on a stable watch poll")
			}
			l.mu.Lock()
			defer l.mu.Unlock()
			if l.acquires != 1 || l.releases != 1 || l.versionReads != 1 || l.stateReads != 0 ||
				l.changeReads != 0 || l.commits != 0 {
				t.Fatal("sidecar upgrade repeated the sync or fetched a document")
			}
		})
	}
}
