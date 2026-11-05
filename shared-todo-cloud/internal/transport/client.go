// Package transport connects the existing PC store transaction to the cloud ledger.
package transport

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// The cache shares the legacy reader ceiling. HTTP envelopes have extra room;
// their limit must not truncate an otherwise valid 64 MiB document.
const MaxDocumentBytes = 64 << 20
const maxWireBytes = MaxDocumentBytes + (8 << 20)

type Lease struct {
	ID        string    `json:"id"`
	Revision  uint64    `json:"revision"`
	StateID   string    `json:"stateId,omitempty"`
	TTLMillis int64     `json:"ttlMs,omitempty"`
	ExpiresAt time.Time `json:"expiresAt"`
	deadline  time.Time
}
type Version struct {
	Revision uint64 `json:"revision"`
	StateID  string `json:"stateId,omitempty"`
}
type Snapshot struct {
	Revision uint64          `json:"revision"`
	StateID  string          `json:"stateId,omitempty"`
	Document json.RawMessage `json:"document"`
}
type Change struct {
	Revision      uint64            `json:"revision"`
	StateID       string            `json:"stateId,omitempty"`
	ParentStateID string            `json:"parentStateId,omitempty"`
	Upserts       []json.RawMessage `json:"upserts"`
	DeleteIDs     []int64           `json:"deleteIds"`
	Meta          json.RawMessage   `json:"meta,omitempty"`
}
type Changes struct {
	Revision  uint64   `json:"revision"`
	StateID   string   `json:"stateId,omitempty"`
	Changes   []Change `json:"changes"`
	NextAfter uint64   `json:"nextAfter"`
	HasMore   bool     `json:"hasMore"`
}
type Commit struct {
	RequestID    string            `json:"requestId"`
	LeaseID      string            `json:"leaseId"`
	BaseRevision uint64            `json:"baseRevision"`
	BaseStateID  string            `json:"baseStateId,omitempty"`
	Upserts      []json.RawMessage `json:"upserts"`
	DeleteIDs    []int64           `json:"deleteIds"`
	Meta         json.RawMessage   `json:"meta,omitempty"`
}
type Receipt struct {
	Revision  uint64 `json:"revision"`
	StateID   string `json:"stateId,omitempty"`
	Duplicate bool   `json:"duplicate"`
	Upserts   int    `json:"upserts"`
	Deleted   int    `json:"deleted"`
}

// HTTPError deliberately excludes response bodies and URLs, which may carry
// private store content or credentials supplied by an upstream proxy.
type HTTPError struct{ Status int }

func (e *HTTPError) Error() string { return fmt.Sprintf("todo_remote_http_%d", e.Status) }

type Client struct {
	base  string
	token string
	http  *http.Client
}

func NewClient(base, token string) (*Client, error) {
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || strings.TrimSpace(token) == "" || strings.ContainsAny(token, "\r\n") {
		return nil, errors.New("todo_remote_invalid_configuration")
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1")) {
		return nil, errors.New("todo_remote_https_required")
	}
	return &Client{base: strings.TrimRight(base, "/"), token: token, http: &http.Client{
		Timeout:       20 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

func requestID() (string, error) {
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", errors.New("todo_remote_random_failed")
	}
	return hex.EncodeToString(b[:]), nil
}

func (c *Client) call(ctx context.Context, method, path string, body []byte, out any) error {
	return c.callLimit(ctx, method, path, body, out, maxWireBytes)
}
func (c *Client) callLimit(ctx context.Context, method, path string, body []byte, out any, limit int64) error {
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, bytes.NewReader(body))
	if err != nil {
		return errors.New("todo_remote_request_failed")
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	if method == "PUT" && strings.HasPrefix(path, "/api/v1/assets/") {
		req.Header.Set("Content-Type", "application/octet-stream")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return errors.New("todo_remote_network_failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &HTTPError{resp.StatusCode}
	}
	if out == nil {
		return nil
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return errors.New("todo_remote_response_read_failed")
	}
	if int64(len(b)) > limit {
		return errors.New("todo_remote_response_too_large")
	}
	if json.Unmarshal(b, out) != nil {
		return errors.New("todo_remote_invalid_response")
	}
	return nil
}
func (c *Client) version(ctx context.Context) (Version, error) {
	var v Version
	err := c.callLimit(ctx, "GET", "/api/v1/version", nil, &v, 4096)
	return v, err
}
func encodeJSON(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(b.Bytes(), []byte{'\n'}), nil
}
func wire(v any) []byte { b, _ := encodeJSON(v); return b }
func (c *Client) acquire(ctx context.Context, clientID string) (Lease, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	id, err := requestID()
	if err != nil {
		return Lease{}, err
	}
	body := wire(map[string]string{"clientId": clientID, "requestId": id})
	var lease Lease
	delay := 250 * time.Millisecond
	transient := 0
	for {
		started := time.Now()
		err = c.call(ctx, "POST", "/api/v1/lease/acquire", body, &lease)
		if err == nil {
			if lease.ID == "" {
				return Lease{}, errors.New("todo_remote_invalid_lease")
			}
			if err = lease.startDeadline(started); err != nil {
				return Lease{}, err
			}
			return lease, nil
		}
		var status *HTTPError
		if errors.As(err, &status) && status.Status == 423 {
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return Lease{}, err
			case <-timer.C:
			}
			delay = min(1*time.Second, delay*2)
			continue
		}
		transient++
		if transient >= 2 || ctx.Err() != nil || (errors.As(err, &status) && status.Status < 500) {
			return Lease{}, err
		}
	}
}
func (l *Lease) startDeadline(started time.Time) error {
	// Time.Now carries a monotonic reading. Starting at send time is conservative
	// about transit delay and does not compare unrelated server/PC wall clocks.
	ttl := l.TTLMillis
	if ttl == 0 {
		ttl = 90000
	} // v1 servers have a fixed 90-second lease.
	if ttl < 0 || ttl > 90000 {
		return errors.New("todo_remote_invalid_lease_ttl")
	}
	l.deadline = started.Add(time.Duration(ttl) * time.Millisecond)
	if !time.Now().Before(l.deadline) {
		return errors.New("todo_remote_lease_expired")
	}
	return nil
}
func (c *Client) renew(ctx context.Context, id string) (Lease, error) {
	var lease Lease
	started := time.Now()
	err := c.call(ctx, "POST", "/api/v1/lease/renew", wire(map[string]string{"leaseId": id}), &lease)
	if err == nil && lease.ID != id {
		err = errors.New("todo_remote_invalid_lease")
	}
	if err == nil {
		err = lease.startDeadline(started)
	}
	return lease, err
}
func (c *Client) release(ctx context.Context, id string) error {
	return c.call(ctx, "POST", "/api/v1/lease/release", wire(map[string]string{"leaseId": id}), nil)
}

// An interrupted reply can follow a committed transaction. Query the operation
// and resend the *same bytes and request ID*, never an edit at a new revision.
func (c *Client) commit(ctx context.Context, commit Commit) (Receipt, error) {
	body, err := encodeJSON(commit)
	if err != nil {
		return Receipt{}, errors.New("todo_remote_invalid_commit")
	}
	return c.commitBody(ctx, commit.RequestID, body)
}
func (c *Client) commitBody(ctx context.Context, requestID string, body []byte) (Receipt, error) {
	var receipt Receipt
	var err error
	ambiguous := false
	for attempt := 0; attempt < 3; attempt++ {
		err = c.call(ctx, "POST", "/api/v1/commit", body, &receipt)
		if err == nil {
			return receipt, nil
		}
		var status *HTTPError
		if errors.As(err, &status) && status.Status < 500 {
			if ambiguous {
				return Receipt{}, &CommitUnconfirmed{}
			}
			return Receipt{}, err
		}
		ambiguous = true
		lookup := c.call(ctx, "GET", "/api/v1/operations/"+url.PathEscape(requestID), nil, &receipt)
		if lookup == nil {
			return receipt, nil
		}
		if errors.As(lookup, &status) && status.Status != 404 && status.Status < 500 {
			return Receipt{}, &CommitUnconfirmed{}
		}
		if ctx.Err() != nil {
			break
		}
	}
	return Receipt{}, &CommitUnconfirmed{}
}

func (c *Client) putAsset(ctx context.Context, sha, path, kind string, b []byte) error {
	if digest(b) != sha {
		return errors.New("todo_remote_asset_digest_mismatch")
	}
	query := url.Values{"path": {path}, "kind": {kind}}
	var meta struct {
		SHA256       string `json:"sha256"`
		OriginalPath string `json:"originalPath"`
		Kind         string `json:"kind"`
		Bytes        int64  `json:"bytes"`
	}
	if err := c.callLimit(ctx, "PUT", "/api/v1/assets/"+url.PathEscape(sha)+"?"+query.Encode(), b, &meta, 4096); err != nil {
		return err
	}
	if meta.SHA256 != sha || meta.OriginalPath != path || meta.Kind != kind || meta.Bytes != int64(len(b)) {
		return errors.New("todo_remote_asset_metadata_mismatch")
	}
	return nil
}
