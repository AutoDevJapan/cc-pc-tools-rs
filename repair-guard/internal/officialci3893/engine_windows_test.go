package officialci3893

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

type fixtureAuthority struct {
	evidence Evidence
	err      error
	calls    int
}

func (a *fixtureAuthority) Observe(context.Context) (Evidence, error) { return a.evidence, a.err }

type fixtureArchive struct {
	raw   []byte
	calls int
	err   error
}

func (a *fixtureArchive) Open(context.Context, Asset) (io.ReadCloser, error) {
	a.calls++
	if a.err != nil {
		return nil, a.err
	}
	return io.NopCloser(bytes.NewReader(a.raw)), nil
}

// Test-only authenticated store. The secret is generated fixture material, not
// an owner credential. Load authenticates the exact bytes subsequently decoded.
// This is deliberately not a production persistent registry or DPAPI adapter.
type sealedRecord struct{ bytes, mac []byte }
type fixtureJournal struct {
	mu        sync.Mutex
	records   map[string]sealedRecord
	failState State
}

var fixtureKey = []byte("synthetic receipt authenticator, never a live credential")

func seal(r Receipt) sealedRecord {
	b, _ := json.Marshal(r)
	m := hmac.New(sha256.New, fixtureKey)
	_, _ = m.Write(b)
	return sealedRecord{b, m.Sum(nil)}
}
func (s *fixtureJournal) read(id string) (Receipt, error) {
	v, ok := s.records[id]
	if !ok {
		return Receipt{}, ErrDenied
	}
	m := hmac.New(sha256.New, fixtureKey)
	_, _ = m.Write(v.bytes)
	if !hmac.Equal(v.mac, m.Sum(nil)) {
		return Receipt{}, ErrDenied
	}
	var r Receipt
	if err := json.Unmarshal(v.bytes, &r); err != nil {
		return r, err
	}
	return r, nil
}
func (s *fixtureJournal) Load(id string) (Receipt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.read(id)
}
func (s *fixtureJournal) Reserve(r Receipt) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.records[r.ID]; ok {
		return ErrDenied
	}
	s.records[r.ID] = seal(r)
	return nil
}
func (s *fixtureJournal) Replace(old, next Receipt) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.read(old.ID)
	if err != nil || !reflect.DeepEqual(r, old) || (s.failState != "" && next.State == s.failState) {
		return ErrDenied
	}
	s.records[old.ID] = seal(next)
	return nil
}

type countedFS struct {
	*NativeFS
	reads      atomic.Int64
	installErr error
}

func (f *countedFS) WithVerified(r Receipt, use func([]Payload) error) error {
	f.reads.Add(1)
	return f.NativeFS.WithVerified(r, use)
}
func (f *countedFS) Install(id string, root Identity, b []Payload) ([]FileRecord, error) {
	if f.installErr != nil {
		return nil, f.installErr
	}
	return f.NativeFS.Install(id, root, b)
}

type fixture struct {
	engine    *Engine
	authority *fixtureAuthority
	archive   *fixtureArchive
	journal   *fixtureJournal
	fs        *countedFS
	now       time.Time
	parent    string
}

func zipFixture(t *testing.T, entries []Payload) []byte {
	t.Helper()
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for _, p := range entries {
		h := &zip.FileHeader{Name: p.Name, Method: zip.Store}
		h.SetMode(0600)
		w, err := z.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = w.Write(p.Bytes); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	anchor := os.Getenv("CC_3893_FIXTURE_ROOT")
	if anchor == "" {
		t.Fatal("the test process did not bind its new synthetic anchor")
	}
	parent, err := os.MkdirTemp(anchor, "case-")
	if err != nil {
		t.Fatal(err)
	}
	// Retain fixture material for one ordinary owned-root cleanup after testing.
	p := &pins{}
	if err = p.chain(parent); err != nil {
		p.close()
		t.Fatal(err)
	}
	id, err := identityOf(p.last)
	p.close()
	if err != nil {
		t.Fatal(err)
	}
	native, err := NewNativeFS(parent, id)
	if err != nil {
		t.Fatal(err)
	}
	raw := zipFixture(t, []Payload{{"bin/example.txt", []byte("synthetic public bytes")}, {"readme.txt", []byte("fixture documentation")}})
	pin := OfficialAsset()
	pin.Size = int64(len(raw))
	pin.SHA256 = digest(raw)
	b := FixedBinding(strings.Repeat("a", 40), strings.Repeat("b", 64), strings.Repeat("d", 64), "synthetic-current-3888-job")
	now := time.Unix(1792962000, 0).UTC()
	a := &fixtureAuthority{evidence: Evidence{Binding: b, Asset: pin, Parent: id, FreshOwnedAnchor: true, Observed: now, SourceVerified: true, JobVerified: true, Active: true, OfficialPublicAsset: true}}
	src := &fixtureArchive{raw: raw}
	store := &fixtureJournal{records: map[string]sealedRecord{}}
	fs := &countedFS{NativeFS: native}
	e, err := newEngine(b, pin, a, src, store, fs)
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{e, a, src, store, fs, now, parent}
	e.now = func() time.Time { return f.now }
	return f
}

func (f *fixture) create(t *testing.T) Receipt {
	t.Helper()
	r, err := f.engine.Construct(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r.State != Complete || len(r.Files) != 3 {
		t.Fatalf("incomplete: %+v", r)
	}
	return r
}
func (f *fixture) path(r Receipt, rel string) string {
	return filepath.Join(f.parent, r.ID, filepath.FromSlash(rel))
}

func TestConstructorConsumesPinnedSyntheticArchive(t *testing.T) {
	f := newFixture(t)
	r := f.create(t)
	if err := f.engine.Resume(context.Background(), r.ID); err != nil {
		t.Fatal(err)
	}
	var used []Payload
	if err := f.engine.Consume(context.Background(), r.ID, func(p []Payload) error { used = p; return nil }); err != nil {
		t.Fatal(err)
	}
	if len(used) != 2 || used[0].Name != "bin/example.txt" || string(used[0].Bytes) != "synthetic public bytes" {
		t.Fatalf("wrong public bytes: %+v", used)
	}
	saved, err := f.journal.Load(r.ID)
	if err != nil || saved.State != Consumed {
		t.Fatal("consumption not committed")
	}
	called := false
	if err := f.engine.Consume(context.Background(), r.ID, func([]Payload) error { called = true; return nil }); err == nil || called {
		t.Fatal("receipt replay")
	}
	if f.archive.calls != 1 {
		t.Fatal("archive was reopened")
	}
}

func TestIndependentEvidenceBeforeAnyArchiveOrCreation(t *testing.T) {
	cases := map[string]func(*fixture){
		"source-error":              func(f *fixture) { f.authority.err = ErrDenied },
		"source-unverified":         func(f *fixture) { f.authority.evidence.SourceVerified = false },
		"job-unverified":            func(f *fixture) { f.authority.evidence.JobVerified = false },
		"stopped":                   func(f *fixture) { f.authority.evidence.Active = false },
		"not-public":                func(f *fixture) { f.authority.evidence.OfficialPublicAsset = false },
		"other-self-owner":          func(f *fixture) { f.authority.evidence.Binding.SourceOwner = "other" },
		"other-self-repo":           func(f *fixture) { f.authority.evidence.Binding.SourceRepoID++ },
		"other-source-head":         func(f *fixture) { f.authority.evidence.Binding.SourceHead = strings.Repeat("c", 40) },
		"other-source-registration": func(f *fixture) { f.authority.evidence.Binding.SourceRegistration = strings.Repeat("c", 64) },
		"other-todo":                func(f *fixture) { f.authority.evidence.Binding.CreatorTodo++ },
		"other-job":                 func(f *fixture) { f.authority.evidence.Binding.CreatorJob = "other-job" },
		"other-repo":                func(f *fixture) { f.authority.evidence.Binding.RepoID++ },
		"other-owner":               func(f *fixture) { f.authority.evidence.Binding.Owner = "other" },
		"other-head":                func(f *fixture) { f.authority.evidence.Binding.Head = strings.Repeat("c", 40) },
		"other-tree":                func(f *fixture) { f.authority.evidence.Binding.Tree = strings.Repeat("c", 40) },
		"other-run":                 func(f *fixture) { f.authority.evidence.Binding.RunID++ },
		"other-attempt":             func(f *fixture) { f.authority.evidence.Binding.Attempt++ },
		"other-ci-job":              func(f *fixture) { f.authority.evidence.Binding.JobID++ },
		"other-labels":              func(f *fixture) { f.authority.evidence.Binding.Labels = "Windows" },
		"other-asset":               func(f *fixture) { f.authority.evidence.Asset.ID++ },
		"other-asset-hash":          func(f *fixture) { f.authority.evidence.Asset.SHA256 = strings.Repeat("c", 64) },
		"other-asset-url":           func(f *fixture) { f.authority.evidence.Asset.URL = "https://example.invalid/other.zip" },
		"stale":                     func(f *fixture) { f.authority.evidence.Observed = f.now.Add(-time.Minute - time.Nanosecond) },
		"future":                    func(f *fixture) { f.authority.evidence.Observed = f.now.Add(time.Nanosecond) },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			change(f)
			_, err := f.engine.Construct(context.Background())
			entries, listErr := os.ReadDir(f.parent)
			if err == nil || f.archive.calls != 0 || len(entries) != 0 || listErr != nil {
				t.Fatal("rejection was not before side effects")
			}
		})
	}
}

func TestArchiveIdentityAndZipBoundaries(t *testing.T) {
	for _, name := range []string{"../escape.txt", "/escape.txt", "C:/escape.txt", `bin\escape.txt`, "bin/file:stream", "bin/x.", "bin/x ", "bin/../x", "bin//x", "NUL.txt", "con", "COM1.txt", "LPT9", "é.txt"} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			raw := zipFixture(t, []Payload{{name, []byte("synthetic")}})
			f.archive.raw = raw
			f.engine.asset.Size = int64(len(raw))
			f.engine.asset.SHA256 = digest(raw)
			f.authority.evidence.Asset = f.engine.asset
			if _, err := f.engine.Construct(context.Background()); err == nil {
				t.Fatal("unsafe ZIP admitted")
			}
			if len(f.journal.records) != 0 {
				t.Fatal("receipt created for unsafe ZIP")
			}
		})
	}
	for _, kind := range []string{"hash", "short", "long", "crc", "case-alias", "directory-alias", "file-parent", "expanded-limit", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t)
			raw := append([]byte(nil), f.archive.raw...)
			switch kind {
			case "hash":
				raw[0] ^= 1
			case "short":
				raw = raw[:len(raw)-1]
			case "long":
				raw = append(raw, 0)
			case "crc":
				at := bytes.Index(raw, []byte("synthetic public bytes"))
				raw[at] ^= 1
			case "case-alias":
				raw = zipFixture(t, []Payload{{"A.txt", []byte("a")}, {"a.txt", []byte("b")}})
			case "directory-alias":
				raw = zipFixture(t, []Payload{{"Bin/a", []byte("a")}, {"bin/b", []byte("b")}})
			case "file-parent":
				raw = zipFixture(t, []Payload{{"bin", []byte("a")}, {"bin/a", []byte("b")}})
			case "expanded-limit":
				at := bytes.Index(raw, []byte{'P', 'K', 1, 2})
				binary.LittleEndian.PutUint32(raw[at+24:at+28], maxExpanded+1)
			case "symlink":
				var out bytes.Buffer
				z := zip.NewWriter(&out)
				h := &zip.FileHeader{Name: "link", Method: zip.Store}
				h.SetMode(os.ModeSymlink | 0700)
				w, err := z.CreateHeader(h)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = w.Write([]byte("target")); err != nil {
					t.Fatal(err)
				}
				if err = z.Close(); err != nil {
					t.Fatal(err)
				}
				raw = out.Bytes()
			}
			f.archive.raw = raw
			if kind != "hash" && kind != "short" && kind != "long" {
				f.engine.asset.Size = int64(len(raw))
				f.engine.asset.SHA256 = digest(raw)
				f.authority.evidence.Asset = f.engine.asset
			}
			if _, err := f.engine.Construct(context.Background()); err == nil {
				t.Fatal("bad archive admitted")
			}
			if len(f.journal.records) != 0 {
				t.Fatal("bad archive created a receipt")
			}
		})
	}
}

func TestReceiptTamperPartialSaveExpiryAndResume(t *testing.T) {
	for _, kind := range []string{"mac", "copy-to-sibling", "domain", "repo", "head", "run", "asset", "pending", "consuming", "failed", "future", "expiry", "extended-expiry", "parent", "root", "file-identity", "directory-identity", "file-hash"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t)
			r := f.create(t)
			id := r.ID
			switch kind {
			case "mac":
				v := f.journal.records[id]
				v.bytes[len(v.bytes)/2] ^= 1
				f.journal.records[id] = v
			case "copy-to-sibling":
				id = strings.Repeat("e", 32)
				f.journal.records[id] = f.journal.records[r.ID]
			case "domain":
				r.Domain = "other"
			case "repo":
				r.Binding.RepoID++
			case "head":
				r.Binding.Head = strings.Repeat("c", 40)
			case "run":
				r.Binding.RunID++
			case "asset":
				r.Asset.ID++
			case "pending":
				r.State = Pending
			case "consuming":
				r.State = Consuming
			case "failed":
				r.State = Failed
			case "future":
				r.Created = f.now.Add(time.Second)
				r.Expires = r.Created.Add(ReceiptLifetime)
			case "expiry":
				f.now = r.Expires
				f.authority.evidence.Observed = f.now
			case "extended-expiry":
				r.Expires = r.Expires.Add(time.Minute)
			case "parent":
				r.Parent.FileID++
			case "root":
				r.Root.FileID++
			case "file-identity":
				r.Files[1].Identity.Birth++
			case "directory-identity":
				r.Files[0].Identity.FileID++
			case "file-hash":
				r.Files[1].SHA256 = strings.Repeat("c", 64)
			}
			if kind != "mac" && kind != "copy-to-sibling" && kind != "expiry" {
				f.journal.records[r.ID] = seal(r)
			}
			if err := f.engine.Resume(context.Background(), id); err == nil {
				t.Fatal("invalid receipt resumed")
			}
			called := false
			err := f.engine.Consume(context.Background(), id, func([]Payload) error { called = true; return nil })
			if err == nil || called {
				t.Fatal("invalid receipt consumed")
			}
		})
	}
}

func TestNativeReplacementLinksStreamsAndUnknownEntries(t *testing.T) {
	for _, kind := range []string{"unknown-data", "missing", "changed-bytes", "same-bytes-new-file", "new-root", "same-files-new-directory", "hardlink", "file-ADS", "root-ADS", "junction"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t)
			r := f.create(t)
			file := f.path(r, "bin/example.txt")
			var err error
			switch kind {
			case "unknown-data":
				err = os.WriteFile(f.path(r, "unrelated-auth.json"), []byte("synthetic unrelated data"), 0600)
			case "missing":
				err = fixtureRename(file, filepath.Join(f.parent, "retained-file"))
			case "changed-bytes":
				err = os.WriteFile(file, []byte("different synthetic bytes"), 0600)
			case "same-bytes-new-file":
				err = fixtureRename(file, filepath.Join(f.parent, "retained-file"))
				if err == nil {
					err = os.WriteFile(file, []byte("synthetic public bytes"), 0600)
				}
			case "new-root":
				err = fixtureRename(f.path(r, ""), filepath.Join(f.parent, "retained-root"))
				if err == nil {
					err = os.Mkdir(f.path(r, ""), 0700)
				}
			case "same-files-new-directory":
				saved := filepath.Join(f.parent, "retained-bin")
				err = fixtureRename(f.path(r, "bin"), saved)
				if err == nil {
					err = os.Mkdir(f.path(r, "bin"), 0700)
				}
				if err == nil {
					err = fixtureRename(filepath.Join(saved, "example.txt"), file)
				}
			case "hardlink":
				err = os.Link(file, filepath.Join(f.parent, "retained-link"))
			case "file-ADS":
				err = os.WriteFile(file+":synthetic", []byte("synthetic stream"), 0600)
			case "root-ADS":
				err = os.WriteFile(f.path(r, "")+":synthetic", []byte("synthetic stream"), 0600)
			case "junction":
				saved := filepath.Join(f.parent, "retained-bin")
				err = fixtureRename(f.path(r, "bin"), saved)
				if err == nil {
					err = fixtureJunction(f.path(r, "bin"), saved)
					if err == nil {
						junction := f.path(r, "bin")
						t.Cleanup(func() {
							p, e := syscall.UTF16PtrFromString(junction)
							if e != nil {
								t.Error(e)
								return
							}
							attrs, e := syscall.GetFileAttributes(p)
							if e != nil || attrs&syscall.FILE_ATTRIBUTE_REPARSE_POINT == 0 {
								t.Error("owned fixture junction changed before unlink")
								return
							}
							// Remove only this self-created link, never its target/tree.
							if e = syscall.RemoveDirectory(p); e != nil {
								t.Error(e)
							}
						})
					}
				}
			}
			if err != nil {
				t.Fatalf("fixture setup: %v", err)
			}
			called := false
			err = f.engine.Consume(context.Background(), r.ID, func([]Payload) error { called = true; return nil })
			if err == nil || called {
				t.Fatal("changed physical tree consumed")
			}
		})
	}
}

// Windows can transiently deny the synthetic replacement setup after creation.
// The observed directory had ordinary attributes and ten isolated repetitions
// succeeded. Bound the same rename's sharing/access-denied retry; a persistent
// lock still fails setup. No permissions or production validation are changed.
func fixtureRename(from, to string) error {
	deadline := time.Now().Add(500 * time.Millisecond)
	for {
		err := os.Rename(from, to)
		if (!errors.Is(err, syscall.ERROR_ACCESS_DENIED) && !errors.Is(err, syscall.Errno(32))) || !time.Now().Before(deadline) {
			return err
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Construct one junction solely between synthetic paths. No process, shell,
// administrator operation, symlink privilege or existing target is involved.
func fixtureJunction(path, target string) error {
	if err := os.Mkdir(path, 0700); err != nil {
		return err
	}
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	h, err := syscall.CreateFile(name, syscall.GENERIC_WRITE, 0, nil, syscall.OPEN_EXISTING, 0x02200000, 0)
	if err != nil {
		return err
	}
	defer syscall.CloseHandle(h)
	sub := syscall.StringToUTF16(`\??\` + target)
	print := syscall.StringToUTF16(target)
	data := make([]byte, 16+2*(len(sub)+len(print)))
	binary.LittleEndian.PutUint32(data[0:4], 0xa0000003)
	binary.LittleEndian.PutUint16(data[4:6], uint16(len(data)-8))
	binary.LittleEndian.PutUint16(data[10:12], uint16((len(sub)-1)*2))
	binary.LittleEndian.PutUint16(data[12:14], uint16(len(sub)*2))
	binary.LittleEndian.PutUint16(data[14:16], uint16((len(print)-1)*2))
	for i, v := range append(sub, print...) {
		binary.LittleEndian.PutUint16(data[16+i*2:], v)
	}
	var returned uint32
	return syscall.DeviceIoControl(h, 0x000900a4, &data[0], uint32(len(data)), nil, 0, &returned, nil)
}

func TestSameBytesUseUnderPinnedHandles(t *testing.T) {
	f := newFixture(t)
	r := f.create(t)
	var writeErr, renameErr error
	var used string
	err := f.engine.Consume(context.Background(), r.ID, func(p []Payload) error {
		writeErr = os.WriteFile(f.path(r, "bin/example.txt"), []byte("replacement"), 0600)
		renameErr = os.Rename(f.path(r, ""), filepath.Join(f.parent, "moved-root"))
		used = string(p[0].Bytes)
		return nil
	})
	if err != nil || writeErr == nil || renameErr == nil || used != "synthetic public bytes" {
		t.Fatalf("not pinned/same bytes: %v write=%v rename=%v bytes=%q", err, writeErr, renameErr, used)
	}
}

func TestInterruptedSaveAndConsumerFailureNeverAdopt(t *testing.T) {
	for _, kind := range []string{"install", "complete-save", "consumed-save", "consumer", "cancelled"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t)
			if kind == "install" {
				f.fs.installErr = errors.New("synthetic disk error")
			}
			if kind == "complete-save" {
				f.journal.failState = Complete
			}
			r, err := f.engine.Construct(context.Background())
			if kind == "install" || kind == "complete-save" {
				if err == nil || r.ID == "" {
					t.Fatal("interrupted construction succeeded")
				}
				f.journal.failState = ""
				f.fs.installErr = nil
				if err = f.engine.Resume(context.Background(), r.ID); err == nil {
					t.Fatal("pending/failed root adopted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if kind == "consumed-save" {
				f.journal.failState = Consumed
			}
			ctx := context.Background()
			if kind == "cancelled" {
				cancelled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = cancelled
			}
			calls := 0
			err = f.engine.Consume(ctx, r.ID, func([]Payload) error {
				calls++
				if kind == "consumer" {
					return errors.New("synthetic consumer failure")
				}
				return nil
			})
			if err == nil {
				t.Fatal("failure reported success")
			}
			if kind == "cancelled" && calls != 0 {
				t.Fatal("cancelled callback")
			}
			f.journal.failState = ""
			if kind != "cancelled" && f.engine.Resume(context.Background(), r.ID) == nil {
				t.Fatal("partial consume adopted")
			}
		})
	}
}

func TestCreateNewNeverAdoptsExistingRootAndAnchorCannotBeRebound(t *testing.T) {
	f := newFixture(t)
	id := strings.Repeat("d", 32)
	if _, err := f.fs.CreateRoot(id); err != nil {
		t.Fatal(err)
	}
	if _, err := f.fs.CreateRoot(id); err == nil {
		t.Fatal("existing root adopted")
	}
	wrong := f.fs.identity
	wrong.Birth++
	if _, err := NewNativeFS(f.parent, wrong); err == nil {
		t.Fatal("caller identity accepted")
	}
}
