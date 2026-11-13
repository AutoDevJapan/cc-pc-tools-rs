//go:build windows

package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func ownedCargoArchive(t *testing.T, headers []*tar.Header, contents [][]byte) []byte {
	t.Helper()
	var output bytes.Buffer
	z := gzip.NewWriter(&output)
	w := tar.NewWriter(z)
	for i, h := range headers {
		if err := w.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if h.Typeflag == tar.TypeReg {
			if _, err := w.Write(contents[i]); err != nil {
				t.Fatal(err)
			}
		}
	}
	if w.Close() != nil || z.Close() != nil {
		t.Fatal("owned archive close")
	}
	return append([]byte(nil), output.Bytes()...)
}

func newArchiveSnapshot(t *testing.T) *softwareBuildSnapshot {
	t.Helper()
	root := physicalSoftwareFixture(t)
	s := &softwareBuildSnapshot{Root: root, paths: map[string]bool{}}
	if err := snapshotDirectories(root, root, s); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	return s
}

func TestCargoArchiveActualBytesBindLockAndCompilerFiles(t *testing.T) {
	content := []byte("pub fn owned(){}\n")
	header := &tar.Header{Name: "owned-1.0.0/src/lib.rs", Size: int64(len(content)), Mode: 0600, Typeflag: tar.TypeReg}
	original := ownedCargoArchive(t, []*tar.Header{header}, [][]byte{content})
	defer clear(original)
	changedContent := []byte("pub fn replaced(){}\n")
	changed := ownedCargoArchive(t, []*tar.Header{{Name: header.Name, Size: int64(len(changedContent)), Mode: 0600, Typeflag: tar.TypeReg}}, [][]byte{changedContent})
	defer clear(changed)
	for _, fault := range []string{"normal", "valid_substitute", "wrong_digest", "read_failure", "canceled"} {
		t.Run(fault, func(t *testing.T) {
			s := newArchiveSnapshot(t)
			sha := digest(original)
			if fault == "wrong_digest" {
				sha = strings.Repeat("a", 64)
			}
			lock := []byte("version = 4\n[[package]]\nname = \"owned\"\nversion = \"1.0.0\"\nsource = \"registry+https://github.com/rust-lang/crates.io-index\"\nchecksum = \"" + sha + "\"\n")
			calls := 0
			read := func(ctx context.Context, name, want string) ([]byte, error) {
				calls++
				if name != "owned-1.0.0" || want != sha {
					t.Fatal("reader scope changed")
				}
				if fault == "read_failure" {
					return nil, errors.New("injected_read_failure")
				}
				if fault == "valid_substitute" {
					return append([]byte(nil), changed...), nil
				}
				return append([]byte(nil), original...), nil
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if fault == "canceled" {
				cancel()
			}
			err := s.admitCargoVendor(ctx, lock, read)
			if (err == nil) != (fault == "normal") {
				t.Fatal("wrong locked archive result", fault, err)
			}
			if fault == "canceled" && calls != 0 {
				t.Fatal("read after cancellation")
			}
			file := filepath.Join(s.Root, "vendor", "owned-1.0.0", "src", "lib.rs")
			if fault != "normal" {
				if _, e := os.Lstat(file); !os.IsNotExist(e) {
					t.Fatal("unverified archive materialized")
				}
				return
			}
			b, e := os.ReadFile(file)
			if e != nil || !bytes.Equal(b, content) {
				t.Fatal("compiler input differs from verified archive")
			}
			if !s.exact() {
				t.Fatal("namespace differs")
			}
			if s.sealDirectories() != nil {
				t.Fatal("seal")
			}
			if os.WriteFile(file, changedContent, 0600) == nil || os.Rename(file, file+".replaced") == nil {
				t.Fatal("verified bytes changed after extraction")
			}
		})
	}
}

func TestCargoArchiveRejectsUnsafeEntriesBeforeCreation(t *testing.T) {
	for _, fault := range []string{"traversal", "absolute", "foreign_root", "symlink", "hardlink", "duplicate", "case_alias", "checksum_override", "special", "oversize", "total_budget", "entry_budget", "bad_gzip", "truncated"} {
		t.Run(fault, func(t *testing.T) {
			s := newArchiveSnapshot(t)
			content := []byte("source")
			header := &tar.Header{Name: "owned-1.0.0/source.rs", Size: int64(len(content)), Mode: 0600, Typeflag: tar.TypeReg}
			budget := cargoExpansionBudget{}
			switch fault {
			case "traversal":
				header.Name = "owned-1.0.0/../foreign.rs"
			case "absolute":
				header.Name = "C:/foreign.rs"
			case "foreign_root":
				header.Name = "other-1.0.0/source.rs"
			case "symlink":
				header.Typeflag = tar.TypeSymlink
				header.Linkname = "source.rs"
				header.Size = 0
			case "hardlink":
				header.Typeflag = tar.TypeLink
				header.Linkname = "source.rs"
				header.Size = 0
			case "checksum_override":
				header.Name = "owned-1.0.0/.cargo-checksum.json"
			case "special":
				header.Typeflag = tar.TypeFifo
				header.Size = 0
			case "total_budget":
				budget.bytes = cargoVendorTotalLimit - int64(len(content)) + 1
			case "entry_budget":
				budget.entries = cargoVendorEntryLimit
			}
			headers := []*tar.Header{header}
			contents := [][]byte{content}
			if fault == "duplicate" || fault == "case_alias" {
				copy := *header
				if fault == "case_alias" {
					copy.Name = "owned-1.0.0/SOURCE.rs"
				}
				headers = append(headers, &copy)
				contents = append(contents, content)
			}
			var raw []byte
			if fault == "oversize" {
				var b bytes.Buffer
				z := gzip.NewWriter(&b)
				w := tar.NewWriter(z)
				header.Size = cargoVendorFileLimit + 1
				if w.WriteHeader(header) != nil {
					t.Fatal("header")
				}
				z.Close()
				raw = b.Bytes()
			} else {
				raw = ownedCargoArchive(t, headers, contents)
			}
			if fault == "bad_gzip" {
				raw = []byte("not gzip")
			}
			if fault == "truncated" {
				raw = raw[:len(raw)/2]
			}
			err := s.extractCargoArchive(context.Background(), "owned-1.0.0", digest(raw), raw, &budget)
			clear(raw)
			if err == nil {
				t.Fatal("invalid archive accepted", fault)
			}
			if fault == "oversize" || fault == "total_budget" || fault == "entry_budget" {
				if _, err := os.Lstat(filepath.Join(s.Root, "vendor", "owned-1.0.0", "source.rs")); !os.IsNotExist(err) {
					t.Fatal("budget checked after file creation")
				}
			}
		})
	}
}

func TestCargoArchiveHeldWindowsReaderConsumesExactBytes(t *testing.T) {
	root := physicalSoftwareFixture(t)
	content := []byte("owned native source")
	raw := ownedCargoArchive(t, []*tar.Header{{Name: "owned-1.0.0/source.rs", Size: int64(len(content)), Mode: 0600, Typeflag: tar.TypeReg}}, [][]byte{content})
	defer clear(raw)
	file := filepath.Join(root, "owned-1.0.0.crate")
	if os.WriteFile(file, raw, 0600) != nil {
		t.Fatal("fixture archive")
	}
	calls := 0
	held := func(o *nativeUpdateObject) error {
		calls++
		if !bytes.Equal(o.Raw(), raw) {
			t.Fatal("held archive bytes differ")
		}
		if os.WriteFile(file, []byte("replaced"), 0600) == nil || os.Rename(file, file+".changed") == nil {
			t.Fatal("archive changed while held")
		}
		return nil
	}
	result, err := readCargoArchiveAt(context.Background(), root, "owned-1.0.0", digest(raw), held)
	defer clear(result)
	if err != nil || calls != 1 || !bytes.Equal(result, raw) {
		t.Fatal("native archive read", err, calls)
	}
	if _, err := readCargoArchiveAt(context.Background(), root, "owned-1.0.0", strings.Repeat("a", 64), nil); err == nil {
		t.Fatal("native archive checksum bypass")
	}
	if result, err := readCargoArchiveAt(context.Background(), root, "../owned", digest(raw), nil); err == nil || len(result) != 0 {
		t.Fatal("unbound native name read")
	}
	s := newArchiveSnapshot(t)
	if err := s.extractCargoArchive(context.Background(), "owned-1.0.0", digest(raw), result, &cargoExpansionBudget{}); err != nil {
		t.Fatal(err)
	}
}

func TestCargoArchiveInputFailureHasOnlyFixedDiagnostic(t *testing.T) {
	s := newArchiveSnapshot(t)
	lock := []byte("version = 4\n[[package]]\nname = \"owned\"\nversion = \"1.0.0\"\nsource = \"registry+https://github.com/rust-lang/crates.io-index\"\nchecksum = \"" + strings.Repeat("a", 64) + "\"\n")
	secret := "sensitive-owned-placeholder"
	err := s.admitCargoVendor(context.Background(), lock, func(context.Context, string, string) ([]byte, error) { return nil, errors.New(secret) })
	d := cargoInputDiagnostic(err)
	if d == nil || d.Cause != "locked_archive_unavailable" {
		t.Fatal("archive failure diagnostic discarded")
	}
	raw, e := json.Marshal(d)
	if e != nil || bytes.Contains(raw, []byte(secret)) || strings.Contains(err.Error(), secret) {
		t.Fatal("unbounded archive error exposed")
	}
	if cargoInputDiagnostic(errors.New(secret)) != nil {
		t.Fatal("raw error promoted to fixed cause")
	}
}

func TestCargoArchiveDiagnosticDistinguishesDeadlineCancelAndOpen(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"deadline", context.DeadlineExceeded, "archive_input_deadline"},
		{"cancel", context.Canceled, "archive_input_canceled"},
		{"sharing", syscall.Errno(32), "archive_sharing_violation"},
		{"denied", syscall.Errno(5), "archive_access_denied"},
		{"missing", syscall.Errno(2), "archive_file_unavailable"},
		{"unknown", errors.New("sensitive-owned-placeholder"), "locked_archive_unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newArchiveSnapshot(t)
			lock := []byte("version = 4\n[[package]]\nname = \"owned\"\nversion = \"1.0.0\"\nsource = \"registry+https://github.com/rust-lang/crates.io-index\"\nchecksum = \"" + strings.Repeat("a", 64) + "\"\n")
			err := s.admitCargoVendor(context.Background(), lock, func(context.Context, string, string) ([]byte, error) { return nil, tc.err })
			d := cargoInputDiagnostic(err)
			if d == nil || d.Cause != tc.want {
				t.Fatalf("fixed diagnostic: got %#v, want %s", d, tc.want)
			}
			raw, e := json.Marshal(d)
			if e != nil || bytes.Contains(raw, []byte("sensitive-owned-placeholder")) || strings.Contains(err.Error(), "sensitive-owned-placeholder") {
				t.Fatal("raw archive failure exposed")
			}
		})
	}
}

func TestCargoArchiveInputFailureReportsCompletedWorkWithoutPaths(t *testing.T) {
	s := newArchiveSnapshot(t)
	content := []byte("owned input")
	raw := ownedCargoArchive(t, []*tar.Header{{Name: "first-1.0.0/source.rs", Size: int64(len(content)), Mode: 0600, Typeflag: tar.TypeReg}}, [][]byte{content})
	defer clear(raw)
	lock := []byte("version = 4\n[[package]]\nname = \"first\"\nversion = \"1.0.0\"\nsource = \"registry+https://github.com/rust-lang/crates.io-index\"\nchecksum = \"" + digest(raw) + "\"\n[[package]]\nname = \"second\"\nversion = \"1.0.0\"\nsource = \"registry+https://github.com/rust-lang/crates.io-index\"\nchecksum = \"" + strings.Repeat("b", 64) + "\"\n")
	calls := 0
	err := s.admitCargoVendor(context.Background(), lock, func(context.Context, string, string) ([]byte, error) {
		calls++
		if calls == 1 {
			return append([]byte(nil), raw...), nil
		}
		return nil, context.DeadlineExceeded
	})
	d := cargoInputDiagnostic(err)
	if d == nil || d.Cause != "archive_input_deadline" || d.ArchiveProgress == nil || d.ArchiveProgress.Expected != 2 || d.ArchiveProgress.Completed != 1 || d.ArchiveProgress.Entries != 1 || d.ArchiveProgress.Bytes < int64(len(content)) || d.ArchiveProgress.ElapsedMS < 0 || calls != 2 {
		t.Fatalf("actual completed work unavailable: %#v", d)
	}
	b, e := json.Marshal(d)
	if e != nil || bytes.Contains(b, []byte(s.Root)) || bytes.Contains(b, []byte("first-1.0.0")) || bytes.Contains(b, []byte("second-1.0.0")) {
		t.Fatal("archive identity or source path exposed")
	}
}

func TestCargoArchiveActualReadBoundariesRetainFixedCause(t *testing.T) {
	for _, mode := range []string{"normal", "root_denied", "leaf_denied", "late_cancel", "missing", "sharing"} {
		t.Run(mode, func(t *testing.T) {
			root := physicalSoftwareFixture(t)
			file := filepath.Join(root, "owned-1.0.0.crate")
			raw := []byte("owned public software")
			if mode != "missing" && os.WriteFile(file, raw, 0600) != nil {
				t.Fatal("fixture write")
			}
			if mode == "sharing" {
				p, _ := syscall.UTF16PtrFromString(file)
				h, err := syscall.CreateFile(p, syscall.GENERIC_READ|syscall.GENERIC_WRITE, 0, nil, syscall.OPEN_EXISTING, 0, 0)
				if err != nil {
					t.Fatal("owned writer", err)
				}
				defer syscall.CloseHandle(h)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			got, err := readQualifiedCargoArchive(ctx, root, root, "owned-1.0.0", digest(raw), func(context.Context, string) bool {
				calls++
				if mode == "late_cancel" {
					cancel()
					return false
				}
				return !(mode == "root_denied" && calls == 1 || mode == "leaf_denied" && calls == 2)
			})
			defer clear(got)
			if mode == "normal" {
				if err != nil || !bytes.Equal(got, raw) || calls != 2 {
					t.Fatal("normal read", err, calls)
				}
				return
			}
			if err == nil || got != nil {
				t.Fatal("failed read returned bytes")
			}
			d := cargoInputDiagnostic(&cargoInputError{cause: "locked_archive_unavailable", err: err})
			wantCause, wantStage, wantCode := "archive_source_unqualified", "root_qualification", uint32(0)
			switch mode {
			case "leaf_denied":
				wantStage = "leaf_qualification"
			case "late_cancel":
				// A false qualification is the primary failure even when the
				// context is canceled before its callback returns.
				wantCause = "archive_source_unqualified"
			case "missing":
				wantCause, wantStage, wantCode = "archive_file_unavailable", "leaf_open", 2
			case "sharing":
				wantCause, wantStage, wantCode = "archive_sharing_violation", "leaf_open", 32
			}
			if d == nil || d.Cause != wantCause || d.InputStage != wantStage || d.InputOSCode != wantCode {
				t.Fatalf("boundary diagnostic: %#v", d)
			}
			encoded, e := json.Marshal(d)
			if e != nil || bytes.Contains(encoded, []byte(root)) || bytes.Contains(encoded, []byte("owned-1.0.0")) {
				t.Fatal("path exposed")
			}
		})
	}
}

func TestCargoArchiveExplicitQualifierFailureRetainsContextCause(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"deadline", context.DeadlineExceeded, "archive_input_deadline"},
		{"canceled", context.Canceled, "archive_input_canceled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := physicalSoftwareFixture(t)
			raw := []byte("owned archive")
			if os.WriteFile(filepath.Join(root, "owned-1.0.0.crate"), raw, 0600) != nil {
				t.Fatal("fixture")
			}
			calls := 0
			got, err := readQualifiedCargoArchiveWithQualifier(context.Background(), root, root, "owned-1.0.0", digest(raw), func(context.Context, string) error { calls++; return tc.err })
			d := cargoInputDiagnostic(&cargoInputError{cause: "locked_archive_unavailable", err: err})
			if got != nil || calls != 1 || d == nil || d.Cause != tc.want || d.InputStage != "root_qualification" {
				t.Fatalf("reported qualifier failure lost: %#v", d)
			}
		})
	}
}

func TestCargoArchiveMultiplePackagesShareExpansionBudget(t *testing.T) {
	s := newArchiveSnapshot(t)
	content := []byte("owned payload")
	budget := cargoExpansionBudget{bytes: cargoVendorTotalLimit - 4096}
	for i, name := range []string{"first-1.0.0", "second-1.0.0"} {
		raw := ownedCargoArchive(t, []*tar.Header{{Name: name + "/source.rs", Size: int64(len(content)), Mode: 0600, Typeflag: tar.TypeReg}}, [][]byte{content})
		if i == 1 {
			budget.bytes = cargoVendorTotalLimit - int64(len(content)) + 1
		}
		err := s.extractCargoArchive(context.Background(), name, digest(raw), raw, &budget)
		clear(raw)
		if i == 0 && err != nil {
			t.Fatal("normal first archive", err)
		}
		if i == 1 {
			if err == nil {
				t.Fatal("second archive ignored accumulated payload budget")
			}
			if _, err := os.Lstat(filepath.Join(s.Root, "vendor", name, "source.rs")); !os.IsNotExist(err) {
				t.Fatal("overflow file created")
			}
		}
	}
	budget.expanded = cargoVendorTotalLimit + (32 << 20)
	raw := ownedCargoArchive(t, []*tar.Header{{Name: "third-1.0.0/source.rs", Size: int64(len(content)), Mode: 0600, Typeflag: tar.TypeReg}}, [][]byte{content})
	defer clear(raw)
	if s.extractCargoArchive(context.Background(), "third-1.0.0", digest(raw), raw, &budget) == nil {
		t.Fatal("aggregate expanded-stream limit ignored")
	}
	if _, err := os.Lstat(filepath.Join(s.Root, "vendor", "third-1.0.0")); !os.IsNotExist(err) {
		t.Fatal("stream overflow package created")
	}
}

func TestCargoArchiveQualifiedPhysicalRootRejectsRedirectBeforeBody(t *testing.T) {
	base := physicalSoftwareFixture(t)
	root, other, alias := filepath.Join(base, "qualified"), filepath.Join(base, "foreign"), filepath.Join(base, "alias")
	if os.Mkdir(root, 0700) != nil || os.Mkdir(other, 0700) != nil {
		t.Fatal("fixture roots")
	}
	raw := []byte("same matching public software archive")
	for _, p := range []string{root, other} {
		if os.WriteFile(filepath.Join(p, "owned-1.0.0.crate"), raw, 0600) != nil {
			t.Fatal("fixture file")
		}
	}
	if os.Symlink(other, alias) != nil {
		t.Fatal("fixture redirect")
	}
	qualifyCalls := 0
	qualify := func(context.Context, string) bool { qualifyCalls++; return true }
	if got, err := readQualifiedCargoArchive(context.Background(), alias, root, "owned-1.0.0", digest(raw), qualify); err == nil || got != nil || qualifyCalls != 0 {
		t.Fatal("foreign root reached qualification or body")
	}
	if os.Remove(alias) != nil || os.Symlink(root, alias) != nil {
		t.Fatal("fixture normal alias")
	}
	got, err := readQualifiedCargoArchive(context.Background(), alias, root, "owned-1.0.0", digest(raw), qualify)
	defer clear(got)
	if err != nil || !bytes.Equal(got, raw) || qualifyCalls != 2 {
		t.Fatal("qualified root rejected", err, qualifyCalls)
	}
	if got, err := readQualifiedCargoArchive(context.Background(), alias, root, "owned-1.0.0", digest(raw), func(context.Context, string) bool { return false }); err == nil || got != nil {
		t.Fatal("unqualified source body read")
	}
}

func TestCargoArchiveCompressedLimitReachesConsumerDiagnostic(t *testing.T) {
	s := newArchiveSnapshot(t)
	content := []byte("owned source")
	raw := ownedCargoArchive(t, []*tar.Header{{Name: "owned-1.0.0/source.rs", Size: int64(len(content)), Mode: 0600, Typeflag: tar.TypeReg}}, [][]byte{content})
	defer clear(raw)
	lock := []byte("version = 4\n[[package]]\nname = \"owned\"\nversion = \"1.0.0\"\nsource = \"registry+https://github.com/rust-lang/crates.io-index\"\nchecksum = \"" + digest(raw) + "\"\n")
	err := s.extractLockedCargoVendorWithLimit(context.Background(), lock, func(context.Context, string, string) ([]byte, error) { return append([]byte(nil), raw...), nil }, int64(len(raw))-1)
	diagnostic := cargoInputDiagnostic(err)
	if err == nil || diagnostic == nil || diagnostic.Cause != "archive_expansion_rejected" {
		t.Fatal("compressed limit cause discarded")
	}
	if _, err := os.Stat(filepath.Join(s.Root, "vendor", "owned-1.0.0")); !os.IsNotExist(err) {
		t.Fatal("compressed limit checked after package creation")
	}
}

func TestCargoArchiveVerifiedBeforeResolverCanRun(t *testing.T) {
	for _, fault := range []string{"normal", "substitute", "missing"} {
		t.Run(fault, func(t *testing.T) {
			s := newArchiveSnapshot(t)
			content := []byte("owned source")
			raw := ownedCargoArchive(t, []*tar.Header{{Name: "owned-1.0.0/source.rs", Size: int64(len(content)), Mode: 0600, Typeflag: tar.TypeReg}}, [][]byte{content})
			defer clear(raw)
			lock := []byte("version = 4\n[[package]]\nname = \"owned\"\nversion = \"1.0.0\"\nsource = \"registry+https://github.com/rust-lang/crates.io-index\"\nchecksum = \"" + digest(raw) + "\"\n")
			resolved := 0
			err := s.prepareLockedCargoInputsWith(context.Background(), lock, func(context.Context, string, string) ([]byte, error) {
				if fault == "missing" {
					return nil, errors.New("missing")
				}
				if fault == "substitute" {
					return []byte("changed archive"), nil
				}
				return append([]byte(nil), raw...), nil
			}, func(context.Context, []byte) error {
				resolved++
				b, e := os.ReadFile(filepath.Join(s.Root, "vendor", "owned-1.0.0", "source.rs"))
				if e != nil || !bytes.Equal(b, content) {
					t.Fatal("resolver ran before verified file creation")
				}
				return nil
			})
			if fault == "normal" {
				if err != nil || resolved != 1 {
					t.Fatal("normal resolver", err, resolved)
				}
			} else if err == nil || resolved != 0 {
				t.Fatal("unverified archive reached resolver")
			}
		})
	}
}

func TestCargoArchiveOwnedResolverHasNoSharedRegistry(t *testing.T) {
	if _, err := os.Stat("C:/Users/makhmeto/.rustup/toolchains/1.95.0-x86_64-pc-windows-msvc/bin/cargo.exe"); os.IsNotExist(err) {
		t.Skip("explicit workstation Cargo resolution")
	}
	root := physicalSoftwareFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ctx, config, err := prepareMP4CargoContext(ctx, softwareBuildPlan{Target: root, Source: mp4Source})
	if err != nil {
		t.Fatal(err)
	}
	defer config.Close()
	if config.cacheLink != "" {
		t.Fatal("new resolver inherited shared registry")
	}
	if _, err := os.Lstat(filepath.Join(config.Root, "registry")); !os.IsNotExist(err) {
		t.Fatal("shared registry path exists")
	}
	s := &softwareBuildSnapshot{Root: filepath.Join(root, "source"), paths: map[string]bool{}}
	if os.Mkdir(s.Root, 0700) != nil || snapshotDirectories(s.Root, s.Root, s) != nil {
		t.Fatal("snapshot")
	}
	defer s.Close()
	if s.create(filepath.Join(s.Root, "Cargo.toml"), []byte(mp4CargoWorkspace)) != nil {
		t.Fatal("workspace")
	}
	for _, name := range []string{"cc-ask", "cc-chrome", "cc-job-policy", "cc-live", "cc-local-llm", "cc-task-title"} {
		manifest := "[package]\nname=\"" + name + "\"\nversion=\"0.1.0\"\nedition=\"2021\"\n"
		if name == "cc-ask" {
			manifest += "[dependencies]\nowned=\"=1.0.0\"\n"
		}
		if snapshotDirectories(filepath.Join(s.Root, name, "src"), s.Root, s) != nil || s.create(filepath.Join(s.Root, name, "Cargo.toml"), []byte(manifest)) != nil || s.create(filepath.Join(s.Root, name, "src/lib.rs"), []byte("pub fn local(){}\n")) != nil {
			t.Fatal("local fixture")
		}
	}
	manifest := []byte("[package]\nname=\"owned\"\nversion=\"1.0.0\"\nedition=\"2021\"\n")
	lib := []byte("pub fn archived(){}\n")
	raw := ownedCargoArchive(t, []*tar.Header{{Name: "owned-1.0.0/Cargo.toml", Size: int64(len(manifest)), Mode: 0600, Typeflag: tar.TypeReg}, {Name: "owned-1.0.0/src/lib.rs", Size: int64(len(lib)), Mode: 0600, Typeflag: tar.TypeReg}}, [][]byte{manifest, lib})
	defer clear(raw)
	lock := []byte("version = 4\n[[package]]\nname = \"owned\"\nversion = \"1.0.0\"\nsource = \"registry+https://github.com/rust-lang/crates.io-index\"\nchecksum = \"" + digest(raw) + "\"\n")
	for _, name := range []string{"cc-ask", "cc-chrome", "cc-job-policy", "cc-live", "cc-local-llm", "cc-task-title"} {
		lock = append(lock, []byte("[[package]]\nname = \""+name+"\"\nversion = \"0.1.0\"\n")...)
		if name == "cc-ask" {
			lock = append(lock, []byte("dependencies = [\n \"owned\",\n]\n")...)
		}
	}
	if err := s.prepareLockedCargoInputsWith(ctx, lock, func(context.Context, string, string) ([]byte, error) { return append([]byte(nil), raw...), nil }, s.resolveCargoLock); err != nil {
		t.Fatal("owned resolution", err)
	}
	if !s.exact() || !config.cargoConfigurationUnchanged() {
		t.Fatal("resolution changed owned namespace")
	}
}

func TestCargoArchiveLeafCannotChangeAfterQualification(t *testing.T) {
	root := physicalSoftwareFixture(t)
	file := filepath.Join(root, "owned-1.0.0.crate")
	raw := []byte("same archive bytes across unrelated generation")
	if os.WriteFile(file, raw, 0600) != nil {
		t.Fatal("fixture file")
	}
	swapped := false
	qualify := func(_ context.Context, path string) bool {
		if sameCleanPath(path, file) {
			if os.Rename(file, file+".old") == nil {
				if os.WriteFile(file, raw, 0600) != nil {
					t.Fatal("fixture replacement")
				}
				swapped = true
			}
		}
		return true
	}
	got, err := readQualifiedCargoArchive(context.Background(), root, root, "owned-1.0.0", digest(raw), qualify)
	defer clear(got)
	if swapped || err != nil || !bytes.Equal(got, raw) {
		t.Fatal("leaf generation was not held through qualification", swapped, err)
	}
}
