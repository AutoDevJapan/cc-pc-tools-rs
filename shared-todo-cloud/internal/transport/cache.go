package transport

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"
)

type cacheState struct {
	Revision uint64 `json:"revision"`
	StateID  string `json:"stateId,omitempty"`
	SHA256   string `json:"sha256"`
	Bytes    int64  `json:"bytes,omitempty"`
}

func digest(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }
func readFileLimit(path string, limit int64) ([]byte, error) {
	var f *os.File
	var err error
	for attempt := 0; ; attempt++ {
		f, err = os.Open(path)
		if err == nil || attempt >= 49 || !retryableOpen(err) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, errors.New("todo_remote_file_too_large")
	}
	return b, nil
}
func loadCache(path string) (*document, uint64, error) {
	b, state, err := loadCacheEntry(path)
	if err != nil {
		return nil, 0, err
	}
	d, err := parseDocument(b)
	return d, state.Revision, err
}
func readCacheState(path string) (cacheState, error) {
	b, err := readFileLimit(path+".remote-revision", 4096)
	if err != nil {
		return cacheState{}, err
	}
	var state cacheState
	if json.Unmarshal(b, &state) != nil {
		return state, errors.New("todo_remote_cache_revision_mismatch")
	}
	if decoded, e := hex.DecodeString(state.SHA256); e != nil || len(decoded) != sha256.Size {
		return state, errors.New("todo_remote_cache_revision_mismatch")
	}
	return state, nil
}
func loadCacheEntry(path string) ([]byte, cacheState, error) {
	b, err := readFileLimit(path, MaxDocumentBytes)
	if err != nil {
		return nil, cacheState{}, err
	}
	state, err := readCacheState(path)
	if err != nil {
		return nil, state, err
	}
	if state.SHA256 != digest(b) || (state.Bytes != 0 && state.Bytes != int64(len(b))) {
		return nil, state, errors.New("todo_remote_cache_revision_mismatch")
	}
	return b, state, nil
}

// A crash between these two replacements produces a digest mismatch, causing
// the next locked session to fetch a snapshot instead of trusting a wrong rev.
func saveCache(path string, b []byte, revision uint64, stateIDs ...string) error {
	if len(b) > MaxDocumentBytes {
		return errors.New("todo_remote_document_too_large")
	}
	if err := atomicWrite(path, b); err != nil {
		return err
	}
	id := ""
	if len(stateIDs) > 0 {
		id = stateIDs[0]
	}
	return atomicWrite(path+".remote-revision", wire(cacheState{Revision: revision, StateID: id, SHA256: digest(b), Bytes: int64(len(b))}))
}
func atomicWrite(path string, b []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".todo-cache-*")
	if err != nil {
		return errors.New("todo_remote_cache_create_failed")
	}
	temp := f.Name()
	defer os.Remove(temp)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		return errors.New("todo_remote_cache_write_failed")
	}
	if replaceFile(temp, path) != nil {
		return errors.New("todo_remote_cache_replace_failed")
	}
	if syncDirectory(filepath.Dir(path)) != nil {
		return errors.New("todo_remote_cache_sync_failed")
	}
	return nil
}
