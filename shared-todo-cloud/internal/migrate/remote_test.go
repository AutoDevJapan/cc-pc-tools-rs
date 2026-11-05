package migrate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"autodevjapan/shared-todo-cloud/internal/httpapi"
	"autodevjapan/shared-todo-cloud/internal/store"
	"autodevjapan/shared-todo-cloud/internal/transport"
)

func fixtureCloud(t *testing.T, f *fixture, wrap func(http.Handler) http.Handler) (*store.Store, transport.Config) {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "fiction-ledger.db"))
	if err != nil {
		t.Fatal("fixture store open failed")
	}
	t.Cleanup(func() { s.Close() })
	syncToken, adminToken := strings.Repeat("fiction-sync-", 4), strings.Repeat("fiction-admin-", 4)
	h, err := httpapi.New(s, httpapi.Config{SyncToken: syncToken, AdminToken: adminToken})
	if err != nil {
		t.Fatal("fixture HTTP setup failed")
	}
	var handler http.Handler = h
	if wrap != nil {
		handler = wrap(handler)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return s, transport.Config{BaseURL: server.URL, SyncToken: syncToken, ClientID: "fiction-migrator", CachePath: filepath.Join(f.home, "todo.json")}
}

func TestUploadChunksAliasesExactBytesAndRetryWithoutSourceLock(t *testing.T) {
	f := newFixture(t, true)
	exported := mustExport(t, f)
	var mu sync.Mutex
	var sizes []int
	var sequence []string
	var failedChunk, failedFinal atomic.Bool
	var lockFailures atomic.Int32
	var checkLock sync.Once
	var finalBodies [][]byte
	s, cfg := fixtureCloud(t, f, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			checkLock.Do(func() {
				l, err := os.OpenFile(filepath.Join(f.home, "todo.lock"), os.O_RDWR, 0600)
				if err != nil {
					lockFailures.Add(1)
				} else {
					if lockFile(l) != nil {
						lockFailures.Add(1)
					} else {
						unlockFile(l)
					}
					l.Close()
				}
			})
			if r.Method == "PUT" && strings.Contains(r.URL.Path, "/chunks/") {
				b, _ := io.ReadAll(r.Body)
				r.Body = io.NopCloser(bytes.NewReader(b))
				mu.Lock()
				sizes = append(sizes, len(b))
				sequence = append(sequence, "chunk")
				mu.Unlock()
				if failedChunk.CompareAndSwap(false, true) {
					http.Error(w, "fiction-secret-response", 503)
					return
				}
			}
			if r.Method == "PUT" && strings.HasPrefix(r.URL.Path, "/api/v1/assets/") {
				mu.Lock()
				sequence = append(sequence, "asset")
				mu.Unlock()
			}
			if r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/finalize") {
				b, _ := io.ReadAll(r.Body)
				r.Body = io.NopCloser(bytes.NewReader(b))
				mu.Lock()
				finalBodies = append(finalBodies, b)
				sequence = append(sequence, "finalize")
				mu.Unlock()
				if failedFinal.CompareAndSwap(false, true) {
					// The server commits, then the caller loses the successful reply.
					next.ServeHTTP(httptest.NewRecorder(), r)
					http.Error(w, "fiction-secret-after-commit", 503)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	})
	got, err := Upload(context.Background(), cfg, f.out)
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	if !got.Finalized || got.Revision != 1 || got.ChunkCount != 2 || got.VerifiedBlobs != exported.AssetFiles+1 || lockFailures.Load() != 0 {
		t.Fatalf("upload verification incomplete: %+v; lock failures %d", got, lockFailures.Load())
	}
	mu.Lock()
	if len(sizes) != 3 || sizes[0] != ChunkBytes || sizes[1] != ChunkBytes || sizes[2] <= 0 || sizes[2] >= ChunkBytes {
		t.Errorf("chunk sizes not bounded: %v", sizes)
	}
	if len(finalBodies) != 2 || !bytes.Equal(finalBodies[0], finalBodies[1]) {
		t.Error("finalize retry changed its fingerprint")
	}
	firstChunk, lastAsset := -1, -1
	for i, v := range sequence {
		if v == "chunk" && firstChunk < 0 {
			firstChunk = i
		}
		if v == "asset" {
			lastAsset = i
		}
	}
	if firstChunk <= lastAsset {
		t.Error("root started before asset staging completed")
	}
	mu.Unlock()
	asset, err := s.Asset(got.RootSHA256)
	if err != nil || !bytes.Equal(asset.Bytes, f.raw) {
		t.Fatal("server lost original snapshot bytes")
	}
	state, err := s.Snapshot()
	if err != nil || !equalJSON(f.raw, state.Document) {
		t.Fatal("server lost keys, types or case")
	}
	m := readManifest(t, f.out)
	for _, a := range m.Assets {
		meta, err := s.ResolveAsset(a.OriginalPath)
		if err != nil || meta.SHA256 != a.SHA256 || meta.Kind != a.Kind || meta.Bytes != a.Bytes {
			t.Fatal("server lost a path alias")
		}
	}
	mu.Lock()
	oldChunks := len(sizes)
	mu.Unlock()
	if _, err := Upload(context.Background(), cfg, f.out); err != nil {
		t.Fatalf("retry: %v", err)
	}
	mu.Lock()
	newChunks := len(sizes)
	mu.Unlock()
	if oldChunks != newChunks {
		t.Fatal("finalized import resent chunks")
	}
	if _, err := Verify(context.Background(), cfg, f.out); err != nil {
		t.Fatalf("verify: %v", err)
	}
}

func TestAssetsCanBeStagedBeforeExportingLatestRoot(t *testing.T) {
	f := newFixture(t, false)
	exported := mustExport(t, f)
	var roots atomic.Int32
	s, cfg := fixtureCloud(t, f, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/api/v1/imports/") {
				roots.Add(1)
			}
			next.ServeHTTP(w, r)
		})
	})
	for range 2 {
		got, err := UploadAssets(context.Background(), cfg, f.out)
		if err != nil || got.Finalized || got.Revision != 0 || got.VerifiedBlobs != exported.AssetFiles {
			t.Fatalf("asset pre-stage: %v", err)
		}
	}
	state, err := s.Snapshot()
	if err != nil || state.Revision != 0 || roots.Load() != 0 {
		t.Fatal("asset staging initialized ledger")
	}
	if _, err := s.Asset(exported.RootSHA256); !errorsIsNotFound(err) {
		t.Fatal("asset-only upload stored a root")
	}
	f.document["items"].([]any)[0].(map[string]any)["note"] = "latest fiction note"
	f.save(t)
	f.out = filepath.Join(filepath.Dir(f.out), "latest-export")
	mustExport(t, f)
	if _, err := Upload(context.Background(), cfg, f.out); err != nil {
		t.Fatalf("latest root: %v", err)
	}
	state, err = s.Snapshot()
	if err != nil || !equalJSON(state.Document, f.raw) {
		t.Fatal("latest root was not imported")
	}
}

func TestRootLargerThan16MiBUsesBoundedChunks(t *testing.T) {
	f := newFixture(t, false)
	// The server escapes '<' to six wire bytes: a 17 MiB source exceeds the
	// old 64 MiB HTTP response ceiling without exceeding the source ceiling.
	f.document["items"].([]any)[0].(map[string]any)["note"] = strings.Repeat("<", 17<<20)
	f.save(t)
	exported := mustExport(t, f)
	if exported.RootBytes <= 16<<20 || exported.ChunkCount != 5 {
		t.Fatal("fixture did not exceed old upload ceiling")
	}
	var chunks atomic.Int32
	_, cfg := fixtureCloud(t, f, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.Contains(r.URL.Path, "/chunks/") {
				if r.ContentLength <= 0 || r.ContentLength > ChunkBytes {
					t.Error("chunk exceeded protocol limit")
				}
				chunks.Add(1)
			}
			next.ServeHTTP(w, r)
		})
	})
	if _, err := Upload(context.Background(), cfg, f.out); err != nil {
		t.Fatalf("large root upload: %v", err)
	}
	if chunks.Load() != 5 {
		t.Fatal("large root was not split into all chunks")
	}
}

func errorsIsNotFound(err error) bool {
	var se *store.Error
	return errors.As(err, &se) && se.Code == store.CodeNotFound
}

func TestRelativeAndEmptySpecificationAliasesRoundTrip(t *testing.T) {
	f := newFixture(t, false)
	item := f.document["items"].([]any)[0].(map[string]any)
	item["spec"], item["spec_history"] = "specs/empty.md", []any{}
	writeFixture(t, filepath.Join(f.home, "specs", "empty.md"), nil)
	f.save(t)
	mustExport(t, f)
	s, cfg := fixtureCloud(t, f, nil)
	if _, err := Upload(context.Background(), cfg, f.out); err != nil {
		t.Fatalf("empty spec upload: %v", err)
	}
	for _, path := range []string{"specs/empty.md", filepath.Join(f.home, "specs", "empty.md")} {
		meta, err := s.ResolveAsset(path)
		if err != nil || meta.Bytes != 0 || meta.SHA256 != digest(nil) || meta.Kind != "spec" {
			t.Fatal("empty file or relative alias was lost")
		}
	}
}

func TestUploadRefusesDifferentRootOnExistingLedger(t *testing.T) {
	f := newFixture(t, false)
	mustExport(t, f)
	var mutations atomic.Int32
	s, cfg := fixtureCloud(t, f, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "PUT" {
				mutations.Add(1)
			}
			next.ServeHTTP(w, r)
		})
	})
	if _, err := Upload(context.Background(), cfg, f.out); err != nil {
		t.Fatalf("initial upload: %v", err)
	}
	before, _ := s.Snapshot()
	oldMutations := mutations.Load()
	f.document["items"].([]any)[0].(map[string]any)["note"] = "changed root"
	f.save(t)
	f.out = filepath.Join(filepath.Dir(f.out), "different-export")
	mustExport(t, f)
	if _, err := Upload(context.Background(), cfg, f.out); err == nil {
		t.Fatal("existing ledger overwritten")
	}
	after, _ := s.Snapshot()
	if before.Revision != after.Revision || !bytes.Equal(before.Document, after.Document) || mutations.Load() != oldMutations {
		t.Fatal("rejected import changed server content")
	}
}

func TestVerifyDetectsChangedLedgerAndCorruptBlobUsingGETsOnly(t *testing.T) {
	f := newFixture(t, false)
	exported := mustExport(t, f)
	var corrupt atomic.Bool
	var writes atomic.Int32
	s, cfg := fixtureCloud(t, f, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "GET" {
				writes.Add(1)
			}
			if corrupt.Load() && r.Method == "GET" && r.URL.Path == "/api/v1/assets/"+exported.RootSHA256 {
				w.Header().Set("X-Content-SHA256", exported.RootSHA256)
				_, _ = w.Write(bytes.Repeat([]byte{'x'}, int(exported.RootBytes)))
				return
			}
			next.ServeHTTP(w, r)
		})
	})
	if _, err := Upload(context.Background(), cfg, f.out); err != nil {
		t.Fatalf("upload: %v", err)
	}
	oldWrites := writes.Load()
	corrupt.Store(true)
	_, err := Verify(context.Background(), cfg, f.out)
	requireCode(t, err, "remote_digest_mismatch")
	corrupt.Store(false)
	lease, err := s.Acquire("fiction-editor", "fiction-edit-request")
	if err != nil {
		t.Fatal("fixture lease failed")
	}
	changed := f.document["items"].([]any)[0].(map[string]any)
	changed["note"] = "modified after import"
	item, _ := json.Marshal(changed)
	if _, err := s.Commit(store.CommitRequest{RequestID: "fiction-edit", LeaseID: lease.ID, BaseRevision: lease.Revision, BaseStateID: lease.StateID, Upserts: []json.RawMessage{item}}); err != nil {
		t.Fatal("fixture commit failed")
	}
	_, err = Verify(context.Background(), cfg, f.out)
	requireCode(t, err, "remote_state_mismatch")
	if oldWrites != writes.Load() {
		t.Fatal("verify performed a write")
	}
}

func TestLocalPackageCorruptionIsRejectedBeforeAnyNetworkRequest(t *testing.T) {
	for _, name := range []string{"root", "asset", "manifest_path", "manifest_missing_alias", "manifest_missing_reference", "manifest_count"} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, false)
			mustExport(t, f)
			m := readManifest(t, f.out)
			switch name {
			case "root":
				writeFixture(t, filepath.Join(f.out, "snapshot.json"), []byte("changed"))
			case "asset":
				writeFixture(t, filepath.Join(f.out, filepath.FromSlash(m.Assets[0].File.File)), []byte("changed"))
			default:
				switch name {
				case "manifest_path":
					m.Assets[0].File.File = "../outside.md"
				case "manifest_missing_alias":
					m.Assets = m.Assets[1:]
				case "manifest_missing_reference":
					m.References = m.References[1:]
				case "manifest_count":
					m.Counts.Items++
				}
				b, _ := json.Marshal(m)
				writeFixture(t, filepath.Join(f.out, "manifest.json"), b)
			}
			var requests atomic.Int32
			_, cfg := fixtureCloud(t, f, func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); next.ServeHTTP(w, r) })
			})
			if _, err := Upload(context.Background(), cfg, f.out); err == nil || requests.Load() != 0 {
				t.Fatal("corrupt package reached the network")
			}
		})
	}
}

func TestWrongAliasReplyPreventsFinalizeAndRedirectsAreNotFollowed(t *testing.T) {
	for _, mode := range []string{"alias", "redirect"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t, false)
			mustExport(t, f)
			var calls, roots atomic.Int32
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
			defer target.Close()
			_, cfg := fixtureCloud(t, f, func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if strings.Contains(r.URL.Path, "/imports/") {
						roots.Add(1)
					}
					if mode == "redirect" {
						http.Redirect(w, r, target.URL+"/fiction-secret", http.StatusTemporaryRedirect)
						return
					}
					if r.URL.Path == "/api/v1/assets/resolve" {
						_ = json.NewEncoder(w).Encode(assetMetadata{OriginalPath: "wrong", Kind: "spec", SHA256: strings.Repeat("0", 64)})
						return
					}
					next.ServeHTTP(w, r)
				})
			})
			_, err := Upload(context.Background(), cfg, f.out)
			if err == nil || roots.Load() != 0 || calls.Load() != 0 {
				t.Fatal("invalid reply or redirect allowed an import")
			}
		})
	}
}
