package main

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"testing"
	"unsafe"
)

type iosCompilerTestAuthority struct {
	snapshot                        *iosCompilerTestSnapshot
	providerErr, sealErr, unsealErr error
	pins, seals, unseals            int
	corrupt                         bool
}

func (a *iosCompilerTestAuthority) Provider(context.Context) error { return a.providerErr }
func (a *iosCompilerTestAuthority) PinOriginal(context.Context) (iosCompilerSnapshot, error) {
	a.pins++
	return a.snapshot, nil
}
func (a *iosCompilerTestAuthority) Seal(raw []byte) ([]byte, error) {
	a.seals++
	return append([]byte("synthetic-only:"), raw...), a.sealErr
}
func (a *iosCompilerTestAuthority) Unseal(raw []byte) ([]byte, error) {
	a.unseals++
	out := append([]byte(nil), raw[len("synthetic-only:"):]...)
	if a.corrupt {
		out[0] ^= 1
	}
	return out, a.unsealErr
}

type iosCompilerTestSnapshot struct {
	archive                 []byte
	files                   map[string][]byte
	badIdentity, recheckErr bool
	closeErr                error
	closed                  bool
	read                    []string
}

func (s *iosCompilerTestSnapshot) Archive() ([]byte, string) { return s.archive, "archive-owned" }
func (s *iosCompilerTestSnapshot) File(name string) ([]byte, string, error) {
	s.read = append(s.read, name)
	if s.badIdentity {
		return s.files[name], "", nil
	}
	return s.files[name], "owned-" + name, nil
}
func (s *iosCompilerTestSnapshot) Recheck() error {
	if s.recheckErr {
		return errIOSCompiler
	}
	return nil
}
func (s *iosCompilerTestSnapshot) Close() error { s.closed = true; return s.closeErr }

func iosCompilerFixture(t *testing.T, extra string) (*iosCompilerTestAuthority, string) {
	t.Helper()
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	s := &iosCompilerTestSnapshot{files: map[string][]byte{}}
	for _, name := range iosCompilerNames {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		s.files[name] = []byte("synthetic private " + name)
		if _, err = f.Write(s.files[name]); err != nil {
			t.Fatal(err)
		}
	}
	if extra != "" {
		f, err := w.Create(extra)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = f.Write([]byte("extra")); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	s.archive = b.Bytes()
	return &iosCompilerTestAuthority{snapshot: s}, iosCompilerHash(s.archive)
}

func TestIOSCompilerActualControllerRequiresIndependentProviderAndIssuer(t *testing.T) {
	for _, name := range []string{"valid", "provider", "cancelled", "digest", "archive-byte", "file-byte", "missing", "identity", "recheck", "seal", "unseal", "corrupt", "consumer", "cleanup"} {
		t.Run(name, func(t *testing.T) {
			a, digest := iosCompilerFixture(t, "")
			ctx := context.Background()
			switch name {
			case "provider":
				a.providerErr = errIOSCompiler
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "digest":
				digest = "wrong"
			case "archive-byte":
				a.snapshot.archive[0] ^= 1
			case "file-byte":
				a.snapshot.files["input.swift"][0] ^= 1
			case "missing":
				delete(a.snapshot.files, "input.swift")
			case "identity":
				a.snapshot.badIdentity = true
			case "recheck":
				a.snapshot.recheckErr = true
			case "seal":
				a.sealErr = errIOSCompiler
			case "unseal":
				a.unsealErr = errIOSCompiler
			case "corrupt":
				a.corrupt = true
			case "cleanup":
				a.snapshot.closeErr = errIOSCompiler
			}
			calls := 0
			r := iosCompilerRead(ctx, a, digest, func(m iosCompilerManifest, files map[string][]byte) error {
				calls++
				if len(files) != 11 || m.Domain != iosCompilerDomain || &files["input.swift"][0] != &a.snapshot.files["input.swift"][0] {
					t.Fatal("consumer did not receive the verified pinned buffer")
				}
				if name == "consumer" {
					return errIOSCompiler
				}
				return nil
			})
			want := name == "valid" || name == "cleanup"
			if r.Consumed != want || (want && calls != 1) || (!want && name != "consumer" && calls != 0) {
				t.Fatal("controller boundary", r, calls)
			}
			if name == "provider" || name == "cancelled" {
				if a.pins != 0 || a.seals != 0 {
					t.Fatal("unqualified provider opened original")
				}
			} else if !a.snapshot.closed {
				t.Fatal("snapshot not closed")
			}
			if r.DataAccessGranted || r.OriginalRegistryRecovered || r.CleanupComplete == (name == "cleanup") {
				t.Fatal("general grant or cleanup misreported")
			}
			for _, fileName := range a.snapshot.read {
				data := a.snapshot.files[fileName]
				if a.pins != 0 && !bytes.Equal(data, make([]byte, len(data))) {
					t.Fatal("consumed file buffer not erased")
				}
			}
		})
	}
}

func TestIOSCompilerArchiveRejectsWrongNamespaceBeforeSeal(t *testing.T) {
	for _, name := range []string{"input.swift", "INPUT.SWIFT", "../input.swift", "C:/private", "private\\data", "trailing.", "public-metadata.txt"} {
		t.Run(name, func(t *testing.T) {
			a, digest := iosCompilerFixture(t, name)
			r := iosCompilerRead(context.Background(), a, digest, nil)
			want := name == "public-metadata.txt"
			if r.Qualified != want || (a.seals != 0) != want {
				t.Fatal("archive namespace qualification", r)
			}
		})
	}
}

type iosCompilerTrackedReader struct {
	remaining  int
	fail       bool
	first, all []byte
	start      uintptr
	offset     int
	swapped    bool
}

func (r *iosCompilerTrackedReader) Read(p []byte) (int, error) {
	if r.first == nil {
		r.first = p
		r.start = uintptr(unsafe.Pointer(&p[0]))
	}
	if uintptr(unsafe.Pointer(&p[0])) != r.start+uintptr(r.offset) {
		r.swapped = true
	}
	if r.remaining == 0 {
		if r.fail {
			return 0, io.ErrUnexpectedEOF
		}
		return 0, io.EOF
	}
	n := min(r.remaining, len(p), 23)
	for i := range p[:n] {
		p[i] = 'S'
	}
	r.remaining -= n
	r.offset += n
	r.all = append(r.all, p[:n]...)
	return n, nil
}
func TestIOSCompilerFixedReadRetainsOneBackingBufferAndErasesFailures(t *testing.T) {
	for _, name := range []string{"normal", "read-error", "oversize", "short", "empty"} {
		t.Run(name, func(t *testing.T) {
			size := int64(20000)
			r := &iosCompilerTrackedReader{remaining: 20000}
			switch name {
			case "read-error":
				r.remaining = 1024
				r.fail = true
			case "oversize":
				r.remaining++
			case "short":
				r.remaining--
			case "empty":
				size = 0
				r.remaining = 0
			}
			raw, err := iosCompilerReadFixed(r, size, 20000)
			if name == "normal" || name == "empty" {
				if err != nil || len(raw) != int(size) || r.swapped {
					t.Fatal("fixed read buffer changed", name, err, r.swapped)
				}
				if len(raw) > 0 && uintptr(unsafe.Pointer(&raw[0])) != r.start {
					t.Fatal("returned a copied backing buffer")
				}
				clear(raw)
			} else {
				if err == nil {
					clear(raw)
					t.Fatal("bad read accepted")
				}
				if !bytes.Equal(r.first, make([]byte, len(r.first))) {
					t.Fatal("failed buffer retained bytes")
				}
			}
		})
	}
}
