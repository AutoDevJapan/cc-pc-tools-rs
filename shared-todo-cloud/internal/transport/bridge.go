package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

type bridgeRequest struct {
	CachePath    string `json:"cachePath"`
	DocumentPath string `json:"documentPath"`
	ItemID       int64  `json:"itemId,omitempty"`
}

// Decode the dispatch key before decoding the other fields. The struct
// decoder accepts case-insensitive matches and uses the last duplicate.
func bridgeOperation(frame json.RawMessage) (string, error) {
	invalid := errors.New("todo_remote_invalid_control_frame")
	if !utf8.Valid(frame) {
		return "", invalid
	}
	decoder := json.NewDecoder(bytes.NewReader(frame))
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		return "", invalid
	}
	var op string
	for decoder.More() {
		key, err := decoder.Token()
		name, ok := key.(string)
		if err != nil || !ok {
			return "", invalid
		}
		if strings.EqualFold(name, "op") {
			if name != "op" || op != "" || decoder.Decode(&op) != nil || op == "" {
				return "", invalid
			}
		} else {
			var ignored json.RawMessage
			if decoder.Decode(&ignored) != nil {
				return "", invalid
			}
		}
	}
	if token, err := decoder.Token(); err != nil || token != json.Delim('}') || op == "" {
		return "", invalid
	}
	return op, nil
}

type bridgeResponse struct {
	OK          bool         `json:"ok"`
	Revision    uint64       `json:"revision,omitempty"`
	Error       string       `json:"error,omitempty"`
	CurrentNote *CurrentNote `json:"currentNote,omitempty"`
	Version     *Version     `json:"version,omitempty"`
}

// Serve keeps snapshots and candidates on disk. One-shot readers use
// the existing authenticated connection without opening a writer session.
// EOF, including a dead Rust parent, releases the lease via the same defer.
func Serve(ctx context.Context, cfg Config, in io.Reader, out io.Writer) error {
	if err := requireActivation(cfg); err != nil {
		return err
	}
	client, err := NewClient(cfg.BaseURL, cfg.SyncToken)
	if err != nil {
		return err
	}
	dec := json.NewDecoder(in)
	enc := json.NewEncoder(out)
	var session *Session
	defer func() {
		if session != nil {
			_ = session.Close()
		}
	}()
	for {
		var frame json.RawMessage
		if err = dec.Decode(&frame); err == io.EOF {
			return nil
		} else if err != nil {
			return errors.New("todo_remote_invalid_control_frame")
		}
		op, frameErr := bridgeOperation(frame)
		if frameErr != nil {
			return frameErr
		}
		var req bridgeRequest
		if json.Unmarshal(frame, &req) != nil {
			return errors.New("todo_remote_invalid_control_frame")
		}
		var revision uint64
		var note *CurrentNote
		var version *Version
		switch op {
		case "capacity-version":
			_, frameErr := readProjectionFields(frame, "op", "cachePath")
			if frameErr != nil || session != nil || !samePath(req.CachePath, cfg.CachePath) {
				err = errors.New("todo_remote_invalid_control_frame")
				break
			}
			var observed Version
			observed, err = client.capacityVersion(ctx)
			if err == nil {
				version = &observed
			}
		case "current-note":
			if session != nil || req.ItemID <= 0 || !samePath(req.CachePath, cfg.CachePath) || req.DocumentPath != "" {
				err = errors.New("todo_remote_invalid_note_request")
				break
			}
			var projection CurrentNote
			projection, err = client.currentNote(ctx, req.ItemID)
			if err == nil {
				note = &projection
			}
		case "begin":
			if session != nil || !samePath(req.CachePath, cfg.CachePath) {
				err = errors.New("todo_remote_cache_path_mismatch")
				break
			}
			session, err = Begin(ctx, client, cfg.ClientID, cfg.CachePath)
			if err == nil {
				revision = session.Revision()
			}
		case "commit":
			if session == nil {
				err = errors.New("todo_remote_session_required")
				break
			}
			if !samePath(filepath.Dir(req.DocumentPath), filepath.Dir(cfg.CachePath)) || !strings.HasPrefix(filepath.Base(req.DocumentPath), ".todo-remote-") {
				err = errors.New("todo_remote_candidate_path_mismatch")
				break
			}
			var b []byte
			b, err = readFileLimit(req.DocumentPath, MaxDocumentBytes)
			if err != nil {
				err = errors.New("todo_remote_candidate_read_failed")
				break
			}
			var receipt Receipt
			receipt, err = session.Commit(ctx, b)
			revision = receipt.Revision
		case "close":
			if session != nil {
				err = session.Close()
				session = nil
			}
		default:
			err = errors.New("todo_remote_unknown_operation")
		}
		resp := bridgeResponse{OK: err == nil, Revision: revision, CurrentNote: note, Version: version}
		if err != nil {
			resp.Error = err.Error()
		}
		if encodeErr := enc.Encode(resp); encodeErr != nil {
			return errors.New("todo_remote_control_write_failed")
		}
		if op == "close" || op == "current-note" || op == "capacity-version" || err != nil {
			return nil
		}
	}
}
func samePath(a, b string) bool {
	resolve := func(p string) string {
		p = filepath.Clean(p)
		if parent, err := filepath.EvalSymlinks(filepath.Dir(p)); err == nil {
			p = filepath.Join(parent, filepath.Base(p))
		}
		// Rust canonical paths may contain the Win32 extended-path prefix.
		return strings.TrimPrefix(p, `\\?\`)
	}
	a = resolve(a)
	b = resolve(b)
	// filepath.Clean uses the native separator; EqualFold is needed on Windows.
	return pathsEqual(a, b)
}

// Sync holds the same legacy lock. Watch runs just one cache synchronizer per
// PC cache, never one watcher per task. Existing readers only read this cache.
func Sync(ctx context.Context, cfg Config) error {
	if err := requireActivation(cfg); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(filepath.Dir(cfg.CachePath), "todo.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return errors.New("todo_remote_lock_open_failed")
	}
	defer f.Close()
	if lockFile(f) != nil {
		return errors.New("todo_remote_lock_busy")
	}
	defer unlockFile(f)
	c, err := NewClient(cfg.BaseURL, cfg.SyncToken)
	if err != nil {
		return err
	}
	s, err := Begin(ctx, c, cfg.ClientID, cfg.CachePath)
	if err != nil {
		return err
	}
	return s.Close()
}
func Watch(ctx context.Context, cfg Config) error {
	if err := requireActivation(cfg); err != nil {
		return err
	}
	f, err := os.OpenFile(cfg.CachePath+".remote-sync.lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return errors.New("todo_remote_sync_lock_open_failed")
	}
	defer f.Close()
	if lockFile(f) != nil {
		return errors.New("todo_remote_sync_already_running")
	}
	defer unlockFile(f)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	client, err := NewClient(cfg.BaseURL, cfg.SyncToken)
	if err != nil {
		return err
	}
	previousError := ""
	for {
		// A failed pull preserves the cache; the next tick is the only retry.
		err := syncIfChanged(ctx, cfg, client)
		code := ""
		if err != nil {
			code = err.Error()
		}
		if code != previousError {
			if code != "" {
				fmt.Fprintln(os.Stderr, code)
			} else {
				fmt.Fprintln(os.Stderr, "todo_remote_cache_sync_recovered")
			}
			previousError = code
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// An unchanged poll reads only the bounded sidecar and stat information. It
// neither opens the large JSON nor contends with Web/PC writers for a lease.
func syncIfChanged(ctx context.Context, cfg Config, client *Client) error {
	version, err := client.version(ctx)
	if err != nil {
		var status *HTTPError
		if errors.As(err, &status) && status.Status == 404 {
			return Sync(ctx, cfg)
		}
		return err
	}
	state, err := readCacheState(cfg.CachePath)
	if err == nil && !needsFresh(cfg.CachePath) && sameState(state.Revision, state.StateID, version.Revision, version.StateID) && state.Bytes > 0 {
		if info, e := os.Stat(cfg.CachePath); e == nil && info.Mode().IsRegular() && info.Size() == state.Bytes {
			return nil
		}
	}
	return Sync(ctx, cfg)
}

func requireActivation(cfg Config) error {
	var marker struct {
		Bridge      string `json:"bridge"`
		Credentials string `json:"credentials"`
	}
	b, err := readFileLimit(filepath.Join(filepath.Dir(cfg.CachePath), "todo.remote.json"), 16<<10)
	if err != nil {
		return errors.New("todo_remote_not_active")
	}
	if json.Unmarshal(b, &marker) != nil || !filepath.IsAbs(marker.Bridge) || !filepath.IsAbs(marker.Credentials) {
		return errors.New("todo_remote_invalid_activation_marker")
	}
	installed, err := LoadConfig(marker.Credentials)
	if err != nil || installed != cfg {
		return errors.New("todo_remote_activation_configuration_mismatch")
	}
	return nil
}
