package migrate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"autodevjapan/shared-todo-cloud/internal/transport"
)

type remote struct {
	base, token string
	http        *http.Client
}

const (
	assetWorkerLimit = 8
	// Budget source bytes before reading or sending them. A larger asset gets
	// the entire budget and runs alone, including the 128 MiB maximum asset.
	assetMemoryBudget   int64 = 64 << 20
	minAssetReservation int64 = 1 << 20
)

func newRemote(cfg transport.Config) (*remote, error) {
	if cfg.Validate() != nil {
		return nil, failure("invalid_configuration")
	}
	roundTripper := http.DefaultTransport
	if base, ok := roundTripper.(*http.Transport); ok {
		pool := base.Clone()
		pool.MaxIdleConnsPerHost = assetWorkerLimit
		pool.MaxConnsPerHost = assetWorkerLimit
		roundTripper = pool
	}
	return &remote{base: strings.TrimRight(cfg.BaseURL, "/"), token: cfg.SyncToken,
		http: &http.Client{Transport: roundTripper, Timeout: 60 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

func (c *remote) request(ctx context.Context, method, path string, body []byte, contentType string) (*http.Response, error) {
	if err := cancelled(ctx); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, bytes.NewReader(body))
	if err != nil {
		return nil, failure("request_failed")
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, failure("cancelled")
		}
		return nil, failure("network_failed")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Never include response text or URLs in an error. Even a proxy error
		// can contain task content, credentials or a private filesystem path.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		return nil, &Error{Code: "remote_http_error", HTTPStatus: resp.StatusCode}
	}
	return resp, nil
}

func (c *remote) call(ctx context.Context, method, path string, body []byte, contentType string, out any, limit int64) error {
	resp, err := c.request(ctx, method, path, body, contentType)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return failure("response_read_failed")
	}
	if int64(len(b)) > limit {
		return failure("invalid_remote_response")
	}
	if validateJSON(b) != nil || json.Unmarshal(b, out) != nil {
		return failure("invalid_remote_response")
	}
	return nil
}

// Mutations are replayable: immutable chunks/aliases or the identical Finalize
// fingerprint. Retry only transient failures, at most three times.
func (c *remote) mutate(ctx context.Context, method, path string, body []byte, contentType string, out any) error {
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		err = c.call(ctx, method, path, body, contentType, out, 1<<20)
		if err == nil {
			return nil
		}
		var e *Error
		if !errors.As(err, &e) || e.Code != "network_failed" && e.Code != "response_read_failed" && !(e.Code == "remote_http_error" && e.HTTPStatus >= 500) {
			return err
		}
		if attempt < 2 {
			timer := time.NewTimer(time.Duration(attempt+1) * 100 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return failure("cancelled")
			case <-timer.C:
			}
		}
	}
	return err
}

type assetMetadata struct {
	SHA256       string `json:"sha256"`
	OriginalPath string `json:"originalPath"`
	Kind         string `json:"kind"`
	Bytes        int64  `json:"bytes"`
}

func matchesAsset(got assetMetadata, file File, kind string) bool {
	return got.SHA256 == file.SHA256 && got.OriginalPath == file.OriginalPath && got.Kind == kind && got.Bytes == file.Bytes
}

func (c *remote) resolve(ctx context.Context, f File, kind string) error {
	var meta assetMetadata
	err := c.call(ctx, "GET", "/api/v1/assets/resolve?"+url.Values{"path": {f.OriginalPath}}.Encode(), nil, "", &meta, 1<<20)
	if err != nil {
		return err
	}
	if !matchesAsset(meta, f, kind) {
		return failure("remote_alias_mismatch")
	}
	return nil
}

func (c *remote) verifyBlob(ctx context.Context, f File) error {
	resp, err := c.request(ctx, "GET", "/api/v1/assets/"+f.SHA256, nil, "")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if header := resp.Header.Get("X-Content-SHA256"); header != "" && header != f.SHA256 {
		return failure("remote_digest_mismatch")
	}
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(resp.Body, f.Bytes+1))
	if err != nil || n != f.Bytes || hex.EncodeToString(h.Sum(nil)) != f.SHA256 {
		return failure("remote_digest_mismatch")
	}
	return nil
}

type assetGroup struct{ aliases []Asset }

func groupAssets(assets []Asset) []assetGroup {
	groups := make([]assetGroup, 0)
	indices := make(map[string]int)
	// Retain first appearance and alias order without sorting or modifying the
	// manifest. Each digest has one owner, even when several paths share it.
	for _, a := range assets {
		index, exists := indices[a.SHA256]
		if !exists {
			index = len(groups)
			indices[a.SHA256] = index
			groups = append(groups, assetGroup{})
		}
		groups[index].aliases = append(groups[index].aliases, a)
	}
	return groups
}

func assetReservation(group assetGroup) int64 {
	return min(assetMemoryBudget, max(minAssetReservation, group.aliases[0].Bytes))
}

type assetCompletion struct {
	index int
	err   error
}

// The dispatcher owns counts and memory reservations. Workers cancel their
// common context at the first failure; every dispatched job reports completion,
// and all workers are joined before the caller can start any root operation.
func runAssetGroups(ctx context.Context, groups []assetGroup, run func(context.Context, assetGroup) error) (int, error) {
	if err := cancelled(ctx); err != nil {
		return 0, err
	}
	if len(groups) == 0 {
		return 0, nil
	}
	workCtx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	workers := min(assetWorkerLimit, len(groups))
	jobs := make(chan int)
	completed := make(chan assetCompletion, workers)
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			for index := range jobs {
				err := cancelled(workCtx)
				if err == nil {
					err = run(workCtx, groups[index])
				}
				if err != nil {
					cancel(err)
				}
				completed <- assetCompletion{index: index, err: err}
			}
		})
	}
	var reserved int64
	next, running, verified := 0, 0, 0
	for next < len(groups) || running > 0 {
		if workCtx.Err() != nil && running == 0 {
			break
		}
		var dispatch chan int
		if workCtx.Err() == nil && next < len(groups) && running < workers && reserved+assetReservation(groups[next]) <= assetMemoryBudget {
			dispatch = jobs
		}
		done := workCtx.Done()
		if workCtx.Err() != nil {
			// Cancellation stops dispatch, but completion draining must continue.
			done = nil
		}
		select {
		case dispatch <- next:
			reserved += assetReservation(groups[next])
			running++
			next++
		case result := <-completed:
			reserved -= assetReservation(groups[result.index])
			running--
			if result.err == nil {
				verified++
			}
		case <-done:
		}
	}
	close(jobs)
	wg.Wait()
	if err := context.Cause(workCtx); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil && errors.Is(err, context.Cause(ctx)) {
			return verified, failure("cancelled")
		}
		return verified, err
	}
	return verified, nil
}

func (c *remote) asset(ctx context.Context, p *exportPackage, group assetGroup, put bool) error {
	aliases := group.aliases
	hash := aliases[0].SHA256
	var raw []byte
	if put {
		var err error
		// Validate the same bytes that will be consumed by every PUT. Reading
		// happens only after the dispatcher reserves this group's memory.
		raw, err = p.read(aliases[0].File, MaxAssetBytes)
		if err != nil {
			return err
		}
	}
	for _, a := range aliases {
		if put {
			query := url.Values{"kind": {a.Kind}, "path": {a.OriginalPath}}
			var meta assetMetadata
			if err := c.mutate(ctx, "PUT", "/api/v1/assets/"+hash+"?"+query.Encode(), raw, "application/octet-stream", &meta); err != nil {
				return err
			}
			if !matchesAsset(meta, a.File, a.Kind) {
				return failure("remote_alias_mismatch")
			}
		}
		if err := c.resolve(ctx, a.File, a.Kind); err != nil {
			return err
		}
	}
	return c.verifyBlob(ctx, aliases[0].File)
}

func (c *remote) assets(ctx context.Context, p *exportPackage, put bool) (int, error) {
	return runAssetGroups(ctx, groupAssets(p.manifest.Assets), func(ctx context.Context, group assetGroup) error {
		return c.asset(ctx, p, group, put)
	})
}

type finalizeRequest struct {
	SHA256       string `json:"sha256"`
	ChunkCount   int    `json:"chunkCount"`
	Bytes        int64  `json:"bytes"`
	OriginalPath string `json:"originalPath"`
}

type importResult struct {
	Revision   uint64 `json:"revision"`
	Items      int    `json:"items"`
	SHA256     string `json:"sha256"`
	Bytes      int64  `json:"bytes"`
	ChunkCount int    `json:"chunkCount"`
}

func (c *remote) finalize(ctx context.Context, m Manifest) (uint64, error) {
	s := summarize(m)
	body, _ := json.Marshal(finalizeRequest{SHA256: m.Root.SHA256, ChunkCount: s.ChunkCount, Bytes: m.Root.Bytes, OriginalPath: m.Root.OriginalPath})
	var result importResult
	err := c.mutate(ctx, "POST", "/api/v1/imports/migrate-"+m.Root.SHA256+"/finalize", body, "application/json", &result)
	if err != nil {
		return 0, err
	}
	if result.Revision != 1 || result.Items != m.Counts.Items || result.SHA256 != m.Root.SHA256 || result.Bytes != m.Root.Bytes || result.ChunkCount != s.ChunkCount {
		return 0, failure("finalize_receipt_mismatch")
	}
	return result.Revision, nil
}

func (c *remote) state(ctx context.Context) (transport.Snapshot, error) {
	var state transport.Snapshot
	// JSON HTTP envelopes may escape one original byte (such as '<') to six
	// bytes. The snapshot limit applies to source bytes, not that wire spelling.
	err := c.call(ctx, "GET", "/api/v1/state", nil, "", &state, 6*transport.MaxDocumentBytes+(1<<20))
	return state, err
}

func (c *remote) verifyRoot(ctx context.Context, p *exportPackage) (uint64, error) {
	if err := c.resolve(ctx, p.manifest.Root, "snapshot"); err != nil {
		return 0, err
	}
	if err := c.verifyBlob(ctx, p.manifest.Root); err != nil {
		return 0, err
	}
	state, err := c.state(ctx)
	if err != nil {
		return 0, err
	}
	if state.Revision == 0 || validateJSON(state.Document) != nil || !equalJSON(p.snapshot, state.Document) {
		return 0, failure("remote_state_mismatch")
	}
	return state.Revision, nil
}

// Upload validates the complete local package before contacting the server.
// Assets are sent and read back first. Finalize is last, on a virgin ledger;
// only an identical saved Finalize receipt permits a retry on an existing ledger.
func Upload(ctx context.Context, cfg transport.Config, dir string) (Summary, error) {
	p, err := loadExport(ctx, dir)
	if err != nil {
		return Summary{}, err
	}
	defer p.root.Close()
	summary := summarize(p.manifest)
	c, err := newRemote(cfg)
	if err != nil {
		return summary, err
	}
	defer c.http.CloseIdleConnections()
	state, err := c.state(ctx)
	if err != nil {
		return summary, err
	}
	alreadyFinalized := state.Revision != 0
	if alreadyFinalized {
		if _, err := c.finalize(ctx, p.manifest); err != nil {
			return summary, err
		}
	} else {
		rows, _, err := parseDocument(state.Document)
		if err != nil || len(rows) != 0 {
			return summary, failure("remote_ledger_not_empty")
		}
	}
	summary.VerifiedBlobs, err = c.assets(ctx, p, true)
	if err != nil {
		return summary, err
	}
	if !alreadyFinalized {
		for index, start := 0, 0; start < len(p.snapshot); index, start = index+1, start+ChunkBytes {
			end := min(start+ChunkBytes, len(p.snapshot))
			var receipt struct {
				OK    bool `json:"ok"`
				Index int  `json:"index"`
				Bytes int  `json:"bytes"`
			}
			err := c.mutate(ctx, "PUT", "/api/v1/imports/migrate-"+p.manifest.Root.SHA256+"/chunks/"+strconv.Itoa(index), p.snapshot[start:end], "application/octet-stream", &receipt)
			if err != nil {
				return summary, err
			}
			if !receipt.OK || receipt.Index != index || receipt.Bytes != end-start {
				return summary, failure("chunk_receipt_mismatch")
			}
		}
		if _, err := c.finalize(ctx, p.manifest); err != nil {
			return summary, err
		}
	}
	summary.Finalized = true
	summary.Revision, err = c.verifyRoot(ctx, p)
	if err == nil {
		summary.VerifiedBlobs++
	}
	return summary, err
}

// UploadAssets can pre-stage immutable history/specs before the caller freezes
// the source for cutover. It sends neither root chunks nor a Finalize request.
func UploadAssets(ctx context.Context, cfg transport.Config, dir string) (Summary, error) {
	p, err := loadExport(ctx, dir)
	if err != nil {
		return Summary{}, err
	}
	defer p.root.Close()
	summary := summarize(p.manifest)
	c, err := newRemote(cfg)
	if err == nil {
		defer c.http.CloseIdleConnections()
		summary.VerifiedBlobs, err = c.assets(ctx, p, true)
	}
	return summary, err
}

// Verify performs GETs only, including every exact path alias and blob digest.
func Verify(ctx context.Context, cfg transport.Config, dir string) (Summary, error) {
	p, err := loadExport(ctx, dir)
	if err != nil {
		return Summary{}, err
	}
	defer p.root.Close()
	summary := summarize(p.manifest)
	c, err := newRemote(cfg)
	if err != nil {
		return summary, err
	}
	defer c.http.CloseIdleConnections()
	summary.VerifiedBlobs, err = c.assets(ctx, p, false)
	if err == nil {
		summary.Revision, err = c.verifyRoot(ctx, p)
		if err == nil {
			summary.Finalized = true
			summary.VerifiedBlobs++
		}
	}
	return summary, err
}
