package migrate

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type assetRunResult struct {
	summary Summary
	err     error
}

func testGroups(sizes ...int64) []assetGroup {
	groups := make([]assetGroup, len(sizes))
	for i, size := range sizes {
		groups[i].aliases = []Asset{{File: File{OriginalPath: strconv.Itoa(i), Bytes: size}}}
	}
	return groups
}

func receive[T any](t *testing.T, ctx context.Context, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-ctx.Done():
		t.Fatal("fixture synchronization timed out")
		var zero T
		return zero
	}
}

func TestGroupingPreservesManifestAndPerDigestAliasOrder(t *testing.T) {
	assets := []Asset{
		{File: File{SHA256: "a", OriginalPath: "first"}},
		{File: File{SHA256: "b", OriginalPath: "second"}},
		{File: File{SHA256: "a", OriginalPath: "third"}},
		{File: File{SHA256: "b", OriginalPath: "fourth"}},
	}
	before := append([]Asset(nil), assets...)
	groups := groupAssets(assets)
	if !reflect.DeepEqual(before, assets) || len(groups) != 2 || !reflect.DeepEqual(groups[0].aliases, []Asset{assets[0], assets[2]}) || !reflect.DeepEqual(groups[1].aliases, []Asset{assets[1], assets[3]}) {
		t.Fatal("manifest order or shared digest ownership changed")
	}
}

func TestWorkersCancelOnFirstErrorAndJoinBeforeReturning(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	groups := testGroups(make([]int64, 2*assetWorkerLimit)...)
	entered := make(chan int, len(groups))
	cancelledWorkers := make(chan struct{}, assetWorkerLimit)
	start, cleanup := make(chan struct{}), make(chan struct{})
	var startOnce, cleanupOnce sync.Once
	releaseStart := func() { startOnce.Do(func() { close(start) }) }
	releaseCleanup := func() { cleanupOnce.Do(func() { close(cleanup) }) }
	defer releaseStart()
	defer releaseCleanup()
	var active atomic.Int32
	done := make(chan assetRunResult, 1)
	go func() {
		count, err := runAssetGroups(ctx, groups, func(ctx context.Context, group assetGroup) error {
			index, _ := strconv.Atoi(group.aliases[0].OriginalPath)
			active.Add(1)
			defer active.Add(-1)
			entered <- index
			select {
			case <-start:
			case <-ctx.Done():
				return failure("cancelled")
			}
			if index == 0 {
				return failure("fixture_first_error")
			}
			<-ctx.Done()
			cancelledWorkers <- struct{}{}
			// Deliberately delay worker cleanup after it observes cancellation.
			<-cleanup
			return failure("cancelled")
		})
		done <- assetRunResult{summary: Summary{VerifiedBlobs: count}, err: err}
	}()
	seen := map[int]bool{}
	for range assetWorkerLimit {
		seen[receive(t, ctx, entered)] = true
	}
	if len(seen) != assetWorkerLimit || !seen[0] {
		t.Fatal("workers did not start distinct jobs")
	}
	releaseStart()
	for range assetWorkerLimit - 1 {
		receive(t, ctx, cancelledWorkers)
	}
	select {
	case <-done:
		t.Fatal("pool returned before cancelled workers completed cleanup")
	default:
	}
	releaseCleanup()
	result := receive(t, ctx, done)
	requireCode(t, result.err, "fixture_first_error")
	if result.summary.VerifiedBlobs != 0 || active.Load() != 0 || len(entered) != 0 {
		t.Fatal("unfinished jobs were counted or workers survived the return")
	}
}

func TestAssetMemoryAdmissionKeepsLargeFilesExclusive(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	groups := testGroups(32<<20, 32<<20, MaxAssetBytes, 1<<20)
	entered := make(chan int, len(groups))
	permits := make([]chan struct{}, len(groups))
	for i := range permits {
		permits[i] = make(chan struct{})
	}
	defer func() {
		for _, permit := range permits {
			select {
			case <-permit:
			default:
				close(permit)
			}
		}
	}()
	var mu sync.Mutex
	var activeBytes int64
	var activeFiles int
	var invalidAdmission bool
	done := make(chan assetRunResult, 1)
	go func() {
		count, err := runAssetGroups(ctx, groups, func(ctx context.Context, group assetGroup) error {
			index, _ := strconv.Atoi(group.aliases[0].OriginalPath)
			size := group.aliases[0].Bytes
			mu.Lock()
			activeFiles++
			activeBytes += size
			if activeBytes > assetMemoryBudget && !(activeFiles == 1 && size == MaxAssetBytes) {
				invalidAdmission = true
			}
			mu.Unlock()
			entered <- index
			defer func() { mu.Lock(); activeFiles--; activeBytes -= size; mu.Unlock() }()
			select {
			case <-permits[index]:
				return nil
			case <-ctx.Done():
				return failure("cancelled")
			}
		})
		done <- assetRunResult{summary: Summary{VerifiedBlobs: count}, err: err}
	}()
	first, second := receive(t, ctx, entered), receive(t, ctx, entered)
	if first+second != 1 {
		t.Fatal("small-file admission order differs")
	}
	close(permits[0])
	close(permits[1])
	if receive(t, ctx, entered) != 2 {
		t.Fatal("large file did not run next")
	}
	close(permits[2])
	if receive(t, ctx, entered) != 3 {
		t.Fatal("file after exclusive work did not resume")
	}
	close(permits[3])
	result := receive(t, ctx, done)
	mu.Lock()
	defer mu.Unlock()
	if result.err != nil || result.summary.VerifiedBlobs != len(groups) || invalidAdmission || activeFiles != 0 || activeBytes != 0 {
		t.Fatal("memory budget, exclusive admission or completion count differs")
	}
}

func parallelFixture(t *testing.T) (*fixture, Summary, Manifest) {
	t.Helper()
	f := newFixture(t, false)
	var history []any
	for i := range 4 * assetWorkerLimit {
		for _, alias := range []string{"a", "b"} {
			path := filepath.Join(f.home, "specs", fmt.Sprintf("parallel-%02d-%s.md", i, alias))
			writeFixture(t, path, []byte(fmt.Sprintf("fiction body %02d with distinct digest", i)))
			history = append(history, map[string]any{"spec": path})
		}
	}
	item := f.document["items"].([]any)[0].(map[string]any)
	item["spec"], item["spec_history"] = history[0].(map[string]any)["spec"], history
	f.save(t)
	summary := mustExport(t, f)
	return f, summary, readManifest(t, f.out)
}

type requestGate struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func newRequestGate() *requestGate {
	return &requestGate{entered: make(chan struct{}, assetWorkerLimit), release: make(chan struct{})}
}

func (g *requestGate) open() { g.once.Do(func() { close(g.release) }) }

func TestParallelUploadAndVerifyKeepAliasesCountsAndRootBarrier(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	f, exported, m := parallelFixture(t)
	before, _ := os.ReadFile(filepath.Join(f.out, "manifest.json"))
	aliases := map[string]string{}
	for _, a := range m.Assets {
		aliases[a.OriginalPath] = a.SHA256
	}
	var gate atomic.Pointer[requestGate]
	uploadGate, verifyGate := newRequestGate(), newRequestGate()
	defer uploadGate.open()
	defer verifyGate.open()
	gate.Store(uploadGate)
	var active, peak, writes atomic.Int32
	var violation atomic.Bool
	var mu sync.Mutex
	busy := map[string]bool{}
	s, cfg := fixtureCloud(t, f, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "GET" {
				writes.Add(1)
			}
			hash := strings.TrimPrefix(r.URL.Path, "/api/v1/assets/")
			if hash == "resolve" {
				hash = aliases[r.URL.Query().Get("path")]
			}
			isAsset := strings.HasPrefix(r.URL.Path, "/api/v1/assets/") && hash != "" && hash != exported.RootSHA256
			if !isAsset {
				if active.Load() != 0 {
					violation.Store(true)
				}
				next.ServeHTTP(w, r)
				return
			}
			inFlight := active.Add(1)
			for old := peak.Load(); inFlight > old && !peak.CompareAndSwap(old, inFlight); old = peak.Load() {
			}
			mu.Lock()
			if busy[hash] {
				violation.Store(true)
			}
			busy[hash] = true
			mu.Unlock()
			defer func() { mu.Lock(); delete(busy, hash); mu.Unlock(); active.Add(-1) }()
			if current := gate.Load(); current != nil {
				select {
				case current.entered <- struct{}{}:
				case <-current.release:
				case <-r.Context().Done():
					return
				}
				select {
				case <-current.release:
				case <-r.Context().Done():
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	})
	done := make(chan assetRunResult, 1)
	go func() { summary, err := Upload(ctx, cfg, f.out); done <- assetRunResult{summary, err} }()
	for range assetWorkerLimit {
		receive(t, ctx, uploadGate.entered)
	}
	if peak.Load() != assetWorkerLimit {
		t.Fatal("upload did not use all bounded workers")
	}
	uploadGate.open()
	result := receive(t, ctx, done)
	if result.err != nil || result.summary.Counts != exported.Counts || result.summary.VerifiedBlobs != exported.AssetFiles+1 || !result.summary.Finalized {
		t.Fatalf("parallel upload: %v", result.err)
	}
	oldWrites := writes.Load()
	peak.Store(0)
	gate.Store(verifyGate)
	go func() { summary, err := Verify(ctx, cfg, f.out); done <- assetRunResult{summary, err} }()
	for range assetWorkerLimit {
		receive(t, ctx, verifyGate.entered)
	}
	if peak.Load() != assetWorkerLimit {
		t.Fatal("verify did not use all bounded workers")
	}
	verifyGate.open()
	result = receive(t, ctx, done)
	if result.err != nil || result.summary.Counts != exported.Counts || result.summary.VerifiedBlobs != exported.AssetFiles+1 || !result.summary.Finalized || writes.Load() != oldWrites || peak.Load() > assetWorkerLimit || active.Load() != 0 || violation.Load() {
		t.Fatal("parallel verification, isolation or root barrier failed")
	}
	for _, a := range m.Assets {
		meta, err := s.ResolveAsset(a.OriginalPath)
		if err != nil || !matchesAsset(assetMetadata{SHA256: meta.SHA256, Bytes: meta.Bytes, OriginalPath: meta.OriginalPath, Kind: meta.Kind}, a.File, a.Kind) {
			t.Fatal("parallel work lost an exact path alias")
		}
	}
	after, _ := os.ReadFile(filepath.Join(f.out, "manifest.json"))
	if !bytes.Equal(before, after) {
		t.Fatal("parallel work modified the manifest")
	}
}

func TestParallelFailureCancelsPeersAndResumesPartlyStoredAliases(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	f, exported, m := parallelFixture(t)
	groups := groupAssets(m.Assets)
	completeHash, failHash := groups[0].aliases[0].SHA256, groups[1].aliases[0].SHA256
	if len(groups[1].aliases) < 2 {
		t.Fatal("fixture lacks shared digest aliases")
	}
	failedPath := groups[1].aliases[1].OriginalPath
	allHeld, allExited := make(chan struct{}), make(chan struct{})
	var arrived, held, rootRequests atomic.Int32
	var failMode atomic.Bool
	failMode.Store(true)
	s, cfg := fixtureCloud(t, f, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/api/v1/imports/") {
				rootRequests.Add(1)
			}
			if failMode.Load() && r.Method == "PUT" && strings.HasPrefix(r.URL.Path, "/api/v1/assets/") {
				hash := strings.TrimPrefix(r.URL.Path, "/api/v1/assets/")
				if hash == failHash && r.URL.Query().Get("path") == failedPath {
					select {
					case <-allHeld:
						http.Error(w, "fiction-server-private-text", http.StatusConflict)
					case <-r.Context().Done():
					}
					return
				}
				if hash != completeHash && hash != failHash {
					// Reading the small body permits the HTTP server to observe the
					// client disconnect while this handler waits for cancellation.
					_, _ = io.Copy(io.Discard, r.Body)
					held.Add(1)
					if arrived.Add(1) == assetWorkerLimit-1 {
						close(allHeld)
					}
					<-r.Context().Done()
					if held.Add(-1) == 0 {
						close(allExited)
					}
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	})
	partial, err := Upload(ctx, cfg, f.out)
	code, status := ErrorCode(err)
	if code != "remote_http_error" || status != http.StatusConflict || partial.VerifiedBlobs != 1 || partial.Finalized || rootRequests.Load() != 0 {
		t.Fatalf("first failure or completed count differs: %v, verified %d", err, partial.VerifiedBlobs)
	}
	receive(t, ctx, allExited)
	if held.Load() != 0 {
		t.Fatal("cancelled HTTP requests remained active")
	}
	if _, err := s.ResolveAsset(groups[1].aliases[0].OriginalPath); err != nil {
		t.Fatal("fixture did not retain its first successful alias")
	}
	if _, err := s.ResolveAsset(failedPath); !errorsIsNotFound(err) {
		t.Fatal("failed alias was unexpectedly stored")
	}
	state, err := s.Snapshot()
	if err != nil || state.Revision != 0 {
		t.Fatal("failed asset phase initialized the ledger")
	}
	failMode.Store(false)
	resumed, err := Upload(ctx, cfg, f.out)
	if err != nil || !resumed.Finalized || resumed.Counts != exported.Counts || resumed.VerifiedBlobs != exported.AssetFiles+1 {
		t.Fatalf("parallel resume: %v", err)
	}
	for _, a := range m.Assets {
		meta, err := s.ResolveAsset(a.OriginalPath)
		if err != nil || meta.SHA256 != a.SHA256 || meta.Bytes != a.Bytes || meta.Kind != a.Kind {
			t.Fatal("resumed upload lost an alias or its original digest")
		}
	}
	if _, err := Verify(ctx, cfg, f.out); err != nil {
		t.Fatalf("resumed verify: %v", err)
	}
}
