package observer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

var kernel = syscall.NewLazyDLL("kernel32.dll")
var lockFile = kernel.NewProc("LockFileEx")
var moveFile = kernel.NewProc("MoveFileExW")

const maxStateBytes = 1 << 20

type FileStore struct {
	Path string
	// Fault injection is package-local and is never set by production callers.
	fault func(string) error
}

func (s *FileStore) Lock(ctx context.Context) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(s.Path+".lock", os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return nil, err
	}
	var ov syscall.Overlapped
	ok, _, callErr := lockFile.Call(f.Fd(), 3, 0, 1, 0, uintptr(unsafe.Pointer(&ov)))
	if ok == 0 {
		f.Close()
		if callErr == syscall.Errno(33) {
			return nil, ErrBusy
		}
		return nil, callErr
	}
	return func() { _ = f.Close() }, nil // Closing releases the kernel lock on crashes too.
}

type envelope struct {
	SHA256 string          `json:"sha256"`
	State  json.RawMessage `json:"state"`
}

func decodeStrict(b []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return ErrCorrupt
	}
	if d.Decode(new(any)) != io.EOF {
		return ErrCorrupt
	}
	return nil
}

func (s *FileStore) Load() (State, error) {
	f, err := os.Open(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return NewState(), nil
	}
	if err != nil {
		return State{}, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxStateBytes+1))
	if err != nil {
		return State{}, err
	}
	if len(b) > maxStateBytes {
		return State{}, ErrCorrupt
	}
	var env envelope
	if err = decodeStrict(b, &env); err != nil {
		return State{}, err
	}
	h := sha256.Sum256(env.State)
	if hex.EncodeToString(h[:]) != env.SHA256 {
		return State{}, ErrCorrupt
	}
	var state State
	if err = decodeStrict(env.State, &state); err != nil {
		return State{}, err
	}
	if err = validateState(state); err != nil {
		return State{}, err
	}
	return state, nil
}

func isFingerprint(v string) bool {
	if len(v) != 64 {
		return false
	}
	b, err := hex.DecodeString(v)
	return err == nil && len(b) == 32
}

func validateState(s State) error {
	if s.LiveActivationEnabled || s.LiveActivationCalls != 0 {
		return ErrCorrupt
	}
	switch s.IntegrityHold {
	case "", "clock_reversed", "log_reset", "state_target_mismatch", "event_time_or_epoch_unknown":
	default:
		return ErrCorrupt
	}
	if s.Version != 1 || s.Receipts == nil || len(s.Receipts) > 4096 || len(s.Requests) > 4096 {
		return ErrCorrupt
	}
	if s.Initialized && (!isFingerprint(s.Target) || s.Cursor.Epoch == "" || s.LastAt.IsZero()) {
		return ErrCorrupt
	}
	if s.LastInstance != "" && !isFingerprint(s.LastInstance) {
		return ErrCorrupt
	}
	for k, r := range s.Receipts {
		if !isFingerprint(k) || r.Rechecks < 0 || r.Rechecks > PreflightLimit {
			return ErrCorrupt
		}
		switch r.Attempt {
		case "", "intent_committed", "unconfirmed", "failed_consumed", "completed", "suppressed", "superseded", "unknown_exhausted":
		default:
			return ErrCorrupt
		}
		if r.Attempt != "" && r.At.IsZero() {
			return ErrCorrupt
		}
		if r.At.After(s.LastAt) || r.PreflightStarted.After(s.LastAt) || (r.Rechecks > 0 && r.PreflightStarted.IsZero()) {
			return ErrCorrupt
		}
		if !r.NextCheck.IsZero() && (r.PreflightStarted.IsZero() || r.NextCheck.After(r.PreflightStarted.Add(PreflightWindow))) {
			return ErrCorrupt
		}
	}
	for _, at := range s.Requests {
		if at.IsZero() || at.After(s.LastAt) {
			return ErrCorrupt
		}
	}
	if s.Pending != nil && (s.Pending.At.IsZero() || s.Pending.At.After(s.LastAt) || s.Pending.Record > s.Cursor.Record || !isFingerprint(s.Pending.Instance) || s.Pending.Epoch != s.Cursor.Epoch) {
		return ErrCorrupt
	}
	return nil
}

func (s *FileStore) fail(stage string) error {
	if s.fault != nil {
		return s.fault(stage)
	}
	return nil
}

// Save publishes an old-or-new checksummed state on the same volume. A failed
// publish never authorizes a virtual request. Uncertainty after replacement is
// also a failure; callers reload and reconcile instead of replaying requests.
func (s *FileStore) Save(state State) error {
	if err := validateState(state); err != nil {
		return err
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return err
	}
	h := sha256.Sum256(raw)
	b, err := json.Marshal(envelope{hex.EncodeToString(h[:]), raw})
	if err != nil {
		return err
	}
	if len(b) > maxStateBytes {
		return ErrCorrupt
	}
	if err = s.fail("create"); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(s.Path), ".observer-state-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	defer f.Close()
	if err = s.fail("write"); err != nil {
		return err
	}
	if _, err = f.Write(b); err != nil {
		return err
	}
	if err = s.fail("flush"); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = s.fail("replace"); err != nil {
		return err
	}
	from, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return err
	}
	to, err := syscall.UTF16PtrFromString(s.Path)
	if err != nil {
		return err
	}
	ok, _, callErr := moveFile.Call(uintptr(unsafe.Pointer(from)), uintptr(unsafe.Pointer(to)), 0x1|0x8)
	if ok == 0 {
		return callErr
	}
	return s.fail("after_replace")
}
