//go:build windows

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"syscall"
	"time"
)

const geminiID = "daab67bf8e2c4e2e7d805c3cf9a06a6d"
const geminiRoot = `C:\Users\makhmeto\AppData\Local\Temp\codex-owned-daab67bf8e2c4e2e7d805c3cf9a06a6d`
const geminiMedia = `C:\Users\makhmeto\Desktop\dev\ADJ\GoDD-VirtualStreamer-wt\agent-todo-1784-9c543d4a5189\artifacts\todo-1784\gemini-20261010\aoi-full-preview.mp4`

// Only quarantine constructs this scope. Ordinary planning always uses the
// original closed walk. There is no flag for supplying arbitrary shared paths.
type geminiScope struct {
	external string
	identity syscall.ByHandleFileInformation
	hash     string
	entries  int
	bytes    int64
}

type geminiEntry struct {
	Volume                               uint32
	ID                                   uint64
	Attributes, Links, SizeHigh, SizeLow uint32
	Created, Written                     syscall.Filetime
	Hash                                 string
}

func geminiMetadata(i syscall.ByHandleFileInformation) geminiEntry {
	return geminiEntry{Volume: i.VolumeSerialNumber, ID: fileID(i), Attributes: i.FileAttributes,
		Links: i.NumberOfLinks, SizeHigh: i.FileSizeHigh, SizeLow: i.FileSizeLow, Created: i.CreationTime, Written: i.LastWriteTime}
}

func contentHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fail("PRESERVATION_READ_FAILED")
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return "", fail("PRESERVATION_READ_FAILED")
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func clientGuard(path string) error {
	b, err := runHidden(10*time.Second, `D:\CC\Tools\client-records\scrub391.exe`, "--guard-path", path)
	var result struct {
		Client *bool `json:"client_resource"`
	}
	if err != nil || json.Unmarshal(b, &result) != nil || result.Client == nil || *result.Client {
		return fail("CLIENT_RESOURCE_OR_GUARD_FAILED")
	}
	return nil
}

func (c config) geminiRecovery(id string) (*geminiScope, error) {
	if id != geminiID {
		return nil, fail("HARDLINK_RECOVERY_SCOPE_MISMATCH")
	}
	r, err := c.load(id, true)
	if err != nil {
		return nil, err
	}
	if !same(r.Path, geminiRoot) || r.Project != "GoDD-VirtualStreamer" || r.Content != "gemini-voice-preview" ||
		r.Created != "2026-10-10T22:46:11.3284405+09:00" || r.Volume != 2229119083 || r.FileID != 9570149211999247 {
		return nil, fail("HARDLINK_RECOVERY_SCOPE_MISMATCH")
	}
	for _, p := range []string{r.Path, geminiMedia} {
		if err = clientGuard(p); err != nil {
			return nil, err
		}
		if _, err = checked(p); err != nil {
			return nil, err
		}
	}
	i, err := fileIdentity(geminiMedia)
	if err != nil {
		return nil, err
	}
	if i.VolumeSerialNumber != r.Volume || fileID(i) != 0x100000003bc801 || i.NumberOfLinks != 2 ||
		i.FileAttributes&syscall.FILE_ATTRIBUTE_DIRECTORY != 0 || i.FileSizeHigh != 0 || i.FileSizeLow != 9536831 {
		return nil, fail("SHARED_MEDIA_IDENTITY_CHANGED")
	}
	h, err := contentHash(geminiMedia)
	if err != nil {
		return nil, err
	}
	return &geminiScope{external: geminiMedia, identity: i, hash: h, entries: 13, bytes: 27518293}, nil
}

func (s *geminiScope) accepts(path, root string, i syscall.ByHandleFileInformation) bool {
	return same(path, filepath.Join(root, "verified-preview.mp4")) &&
		i.NumberOfLinks == 2 && geminiMetadata(i) == geminiMetadata(s.identity)
}

func (s *geminiScope) externalUnchanged() error {
	if _, err := checked(s.external); err != nil {
		return err
	}
	i, err := fileIdentity(s.external)
	if err != nil || geminiMetadata(i) != geminiMetadata(s.identity) {
		return fail("SHARED_MEDIA_IDENTITY_CHANGED")
	}
	h, err := contentHash(s.external)
	if err != nil {
		return err
	}
	if h != s.hash {
		return fail("SHARED_MEDIA_CONTENT_CHANGED")
	}
	return nil
}

func (s *geminiScope) snapshot(root string) (map[string]geminiEntry, error) {
	if err := s.externalUnchanged(); err != nil {
		return nil, err
	}
	shared, err := fileIdentity(filepath.Join(root, "verified-preview.mp4"))
	if err != nil || !s.accepts(filepath.Join(root, "verified-preview.mp4"), root, shared) {
		return nil, fail("SHARED_MEDIA_IDENTITY_CHANGED")
	}
	result := make(map[string]geminiEntry)
	// Enumerate metadata with the bounded, reparse-free scanner before hashing.
	c := config{MaxEntries: 10000}
	m, err := c.measureWithShared(root, s)
	if err != nil {
		return nil, err
	}
	if m.Entries != s.entries || m.Bytes != s.bytes {
		return nil, fail("GEMINI_INVENTORY_CHANGED")
	}
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fail("CONTENT_METADATA_FAILED")
		}
		if len(result) > s.entries || protected(filepath.Base(path)) {
			return fail("GEMINI_INVENTORY_CHANGED")
		}
		i, e := fileIdentity(path)
		if e != nil {
			return e
		}
		entry := geminiMetadata(i)
		if !d.IsDir() {
			entry.Hash, e = contentHash(path)
			if e != nil {
				return e
			}
		}
		rel, e := filepath.Rel(root, path)
		if e != nil {
			return fail("INVALID_PATH")
		}
		result[rel] = entry
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err = s.externalUnchanged(); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *geminiScope) verify(root string, before map[string]geminiEntry, r receipt) error {
	i, err := fileIdentity(root)
	if err != nil || i.VolumeSerialNumber != r.Volume || fileID(i) != r.FileID {
		return fail("TARGET_IDENTITY_CHANGED")
	}
	after, err := s.snapshot(root)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(before, after) {
		return fail("PRESERVATION_MISMATCH")
	}
	return nil
}
