package transport

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

// CommitUnconfirmed is an outcome, not proof of a rejected domain operation.
// Only fixed codes cross the process boundary; the private journal owns details.
type CommitUnconfirmed struct{}

func (*CommitUnconfirmed) Error() string { return "todo_remote_commit_unconfirmed" }

type CacheResyncRequired struct{}

func (*CacheResyncRequired) Error() string { return "todo_remote_cache_resync_required" }

type pendingCommit struct {
	Namespace       string          `json:"namespace"`
	RequestID       string          `json:"requestId"`
	BaseRevision    uint64          `json:"baseRevision"`
	BaseStateID     string          `json:"baseStateId,omitempty"`
	CandidateSHA256 string          `json:"candidateSHA256"`
	BodySHA256      string          `json:"bodySHA256"`
	Body            json.RawMessage `json:"body"`
	Confirmed       *Receipt        `json:"confirmed,omitempty"`
}
type event struct {
	Kind      string `json:"kind"`
	At        string `json:"at"`
	RequestID string `json:"requestId,omitempty"`
	Revision  uint64 `json:"revision,omitempty"`
	SHA256    string `json:"sha256,omitempty"`
}

func namespace(base, clientID string) string { return digest([]byte(base + "\n" + clientID)) }
func pendingPath(cache string) string        { return cache + ".remote-pending" }
func invalidPath(cache string) string        { return cache + ".remote-invalid" }
func needsFresh(cache string) bool {
	for _, path := range []string{pendingPath(cache), invalidPath(cache)} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			return true
		}
	}
	return false
}
func appendEvent(cache string, value event) error {
	value.At = time.Now().UTC().Format(time.RFC3339Nano)
	f, err := os.OpenFile(cache+".remote-events.jsonl", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return errors.New("todo_remote_event_write_failed")
	}
	defer f.Close()
	b := append(wire(value), '\n')
	if _, err = f.Write(b); err != nil {
		return errors.New("todo_remote_event_write_failed")
	}
	if f.Sync() != nil {
		return errors.New("todo_remote_event_write_failed")
	}
	if syncDirectory(filepath.Dir(cache)) != nil {
		return errors.New("todo_remote_event_write_failed")
	}
	return nil
}
func invalidate(cache string, value event) error {
	if err := atomicWrite(invalidPath(cache), wire(value)); err != nil {
		return err
	}
	return appendEvent(cache, value)
}
func savePending(cache string, p *pendingCommit) error {
	private, err := protect(wire(p))
	if err != nil {
		return err
	}
	return atomicWrite(pendingPath(cache), private)
}
func readPending(cache string) (*pendingCommit, error) {
	path := pendingPath(cache)
	private, err := readFileLimit(path, maxWireBytes+(1<<20))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, &CommitUnconfirmed{}
	}
	b, err := unprotect(path, private)
	if err != nil {
		return nil, &CommitUnconfirmed{}
	}
	var p pendingCommit
	var commit Commit
	if json.Unmarshal(b, &p) != nil || len(p.Body) > maxWireBytes || digest(p.Body) != p.BodySHA256 || json.Unmarshal(p.Body, &commit) != nil || commit.RequestID != p.RequestID || commit.BaseRevision != p.BaseRevision || commit.BaseStateID != p.BaseStateID || len(p.CandidateSHA256) != 64 {
		return nil, &CommitUnconfirmed{}
	}
	if id, e := hex.DecodeString(p.RequestID); e != nil || len(id) != 24 {
		return nil, &CommitUnconfirmed{}
	}
	return &p, nil
}
func removeIfPresent(path string) error {
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
func clearResolved(cache string, p *pendingCommit, kind string) error {
	if err := appendEvent(cache, event{Kind: kind, RequestID: p.RequestID, Revision: p.BaseRevision, SHA256: p.CandidateSHA256}); err != nil {
		return err
	}
	// Only uncertain outcomes retain a private recovery body. Ordinary confirmed
	// commits do not accumulate another copy of every root/item mutation.
	if kind == "pending_resolved_not_applied" {
		b, err := readFileLimit(pendingPath(cache), maxWireBytes+(1<<20))
		if err != nil {
			return err
		}
		if err = atomicWrite(cache+".remote-not-applied-"+p.RequestID, b); err != nil {
			return err
		}
	}
	return removeIfPresent(pendingPath(cache))
}

// Resolve only after acquiring a new exclusive lease. Its different ID makes
// an old in-flight request incapable of committing after an operation 404.
func (s *Session) resolvePending(ctx context.Context, p *pendingCommit) error {
	if p == nil {
		return nil
	}
	if p.Namespace != s.namespace {
		return &CommitUnconfirmed{}
	}
	if invalidate(s.cache, event{Kind: "pending_resolution_requires_fresh_pull", RequestID: p.RequestID, Revision: p.BaseRevision}) != nil {
		return &CommitUnconfirmed{}
	}
	var receipt Receipt
	err := s.client.call(ctx, "GET", "/api/v1/operations/"+url.PathEscape(p.RequestID), nil, &receipt)
	if err == nil {
		if receipt.Revision != p.BaseRevision+1 {
			return &CommitUnconfirmed{}
		}
		p.Confirmed = &receipt
		if savePending(s.cache, p) != nil {
			return &CommitUnconfirmed{}
		}
		s.resolution = "pending_resolved_applied"
		return nil
	}
	var status *HTTPError
	if !errors.As(err, &status) || status.Status != 404 {
		return &CommitUnconfirmed{}
	}
	var commit Commit
	if json.Unmarshal(p.Body, &commit) != nil || commit.LeaseID == s.lease.ID {
		return &CommitUnconfirmed{}
	}
	s.resolution = "pending_resolved_not_applied"
	return nil
}

// A digest/sidecar mismatch may be an old writer or a torn cache update. Keep
// the exact existing bytes before replacing anything; never infer their source.
func preserveForeign(cache string) error {
	in, err := os.Open(cache)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return errors.New("todo_remote_foreign_cache_unreadable")
	}
	defer in.Close()
	out, err := os.CreateTemp(filepath.Dir(cache), ".todo-foreign-*")
	if err != nil {
		return errors.New("todo_remote_foreign_cache_preserve_failed")
	}
	temp := out.Name()
	defer os.Remove(temp)
	if err = out.Chmod(0600); err != nil {
		_ = out.Close()
		return errors.New("todo_remote_foreign_cache_preserve_failed")
	}
	h := sha256.New()
	_, err = io.Copy(io.MultiWriter(out, h), in)
	if err == nil {
		err = out.Sync()
	}
	closeErr := out.Close()
	if err != nil || closeErr != nil {
		return errors.New("todo_remote_foreign_cache_preserve_failed")
	}
	sha := hex.EncodeToString(h.Sum(nil))
	target := cache + ".foreign-" + sha
	if existing, e := os.Open(target); e == nil {
		check := sha256.New()
		_, copyErr := io.Copy(check, existing)
		_ = existing.Close()
		if copyErr != nil || !bytes.Equal(check.Sum(nil), h.Sum(nil)) {
			return errors.New("todo_remote_foreign_cache_backup_conflict")
		}
	} else if os.IsNotExist(e) {
		if replaceFile(temp, target) != nil {
			return errors.New("todo_remote_foreign_cache_preserve_failed")
		}
	} else {
		return errors.New("todo_remote_foreign_cache_preserve_failed")
	}
	if syncDirectory(filepath.Dir(cache)) != nil {
		return errors.New("todo_remote_foreign_cache_preserve_failed")
	}
	return appendEvent(cache, event{Kind: "cache_unverified_bytes_preserved", SHA256: sha})
}
