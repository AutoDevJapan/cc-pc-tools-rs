//go:build windows

package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

const cargoArchiveLimit = 64 << 20
const cargoArchiveTotalLimit = 256 << 20
const cargoPublicArchiveRoot = "D:/CC/ToolCache/cargo-registry/cache/index.crates.io-1949cf8c6b5b557f"
const cargoPublicArchiveAlias = mp4PublicCargoRegistry + "/cache/index.crates.io-1949cf8c6b5b557f"

func cargoPackageDirectory(name string) bool {
	if name == "" || len(name) > 256 {
		return false
	}
	for _, ch := range name {
		if ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '-' || ch == '_' || ch == '.' || ch == '+' {
			continue
		}
		return false
	}
	return name != "." && name != ".."
}

// This private software-input operation accepts no caller-selected location.
// A registered build supplies only package identities from its admitted lock.
// Resolve the fixed public-cache directory by metadata, then retain the plain
// physical namespace and the exact archive handle; no credentials are read.
func readLockedCargoArchive(ctx context.Context, name, checksum string) ([]byte, error) {
	return readQualifiedCargoArchiveWithQualifier(ctx, cargoPublicArchiveAlias, cargoPublicArchiveRoot, name, checksum, qualifyPublicCargoPathResult)
}

func qualifyPublicCargoPath(ctx context.Context, path string) bool {
	return qualifyPublicCargoPathResult(ctx, path) == nil
}

var errArchiveSourceUnqualified = errors.New("public_archive_source_unqualified")

func qualifyPublicCargoPathResult(ctx context.Context, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r := askOwnedChild(ctx, installedGuardPath, "--guard-path", path)
	defer clear(r.Raw)
	return qualifyPublicCargoChildResult(ctx, r)
}

func qualifyPublicCargoChildResult(ctx context.Context, r askChildResult) error {
	// TimedOut samples the context after Wait. It cannot establish whether an
	// earlier denial or the cancellation happened first; retain that ambiguity.
	failure := &cargoArchiveGuardError{}
	switch ctx.Err() {
	case context.Canceled:
		failure.contextState = "canceled"
	case context.DeadlineExceeded:
		failure.contextState = "deadline"
	}
	if !r.Started || !r.Assigned || !r.WaitVerified || !r.Reaped || r.TimedOut || r.Exit != 0 {
		return failure
	}
	var v struct {
		Allowed    bool   `json:"access_allowed"`
		Client     bool   `json:"client_resource"`
		Unverified bool   `json:"classification_unverified"`
		Status     string `json:"protection_status"`
	}
	if json.Unmarshal(r.Raw, &v) != nil || !v.Allowed || v.Client || v.Unverified || v.Status != "clear" {
		return failure
	}
	return nil
}

func readQualifiedCargoArchive(ctx context.Context, alias, root, name, checksum string, qualify func(context.Context, string) bool) ([]byte, error) {
	if qualify == nil {
		return nil, errUpdateObject
	}
	return readQualifiedCargoArchiveWithQualifier(ctx, alias, root, name, checksum, func(ctx context.Context, path string) error {
		if !qualify(ctx, path) {
			return errArchiveSourceUnqualified
		}
		return nil
	})
}

func readQualifiedCargoArchiveWithQualifier(ctx context.Context, alias, root, name, checksum string, qualify func(context.Context, string) error) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !cargoPackageDirectory(name) || !vendorSHA(checksum) || qualify == nil {
		return nil, errUpdateObject
	}
	physical, ok := openPlain(root, true)
	if !ok {
		return nil, &cargoArchiveReadError{stage: "root_open_or_identity", err: errUpdateObject}
	}
	defer syscall.CloseHandle(physical)
	p, err := syscall.UTF16PtrFromString(alias)
	if err != nil {
		return nil, errUpdateObject
	}
	logical, err := syscall.CreateFile(p, 0x80, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE, nil, syscall.OPEN_EXISTING, 0x02000000, 0)
	if err != nil {
		return nil, &cargoArchiveReadError{stage: "alias_open", err: err}
	}
	defer syscall.CloseHandle(logical)
	var a, b syscall.ByHandleFileInformation
	if syscall.GetFileInformationByHandle(logical, &a) != nil || syscall.GetFileInformationByHandle(physical, &b) != nil || a.VolumeSerialNumber != b.VolumeSerialNumber || a.FileIndexHigh != b.FileIndexHigh || a.FileIndexLow != b.FileIndexLow {
		return nil, &cargoArchiveReadError{stage: "alias_identity", err: errUpdateObject}
	}
	// Pin the leaf's metadata before asking the ordinary path guard. This
	// handle excludes replacement, but reads no body until qualification passes.
	leaf := filepath.Join(root, name+".crate")
	p, err = syscall.UTF16PtrFromString(leaf)
	if err != nil {
		return nil, errUpdateObject
	}
	metadata, err := syscall.CreateFile(p, syscall.GENERIC_READ, syscall.FILE_SHARE_READ, nil, syscall.OPEN_EXISTING, 0x00200000, 0)
	if err != nil {
		return nil, &cargoArchiveReadError{stage: "leaf_open", err: err}
	}
	defer syscall.CloseHandle(metadata)
	var leafInfo syscall.ByHandleFileInformation
	actual, ok := handlePath(metadata)
	if !ok || !sameCleanPath(actual, leaf) || syscall.GetFileInformationByHandle(metadata, &leafInfo) != nil || leafInfo.NumberOfLinks != 1 || leafInfo.FileAttributes&(syscall.FILE_ATTRIBUTE_REPARSE_POINT|syscall.FILE_ATTRIBUTE_DIRECTORY) != 0 {
		return nil, &cargoArchiveReadError{stage: "leaf_identity", err: errUpdateObject}
	}
	for i, path := range []string{root, leaf} {
		if err := qualify(ctx, path); err != nil {
			stage := "root_qualification"
			if i == 1 {
				stage = "leaf_qualification"
			}
			return nil, &cargoArchiveReadError{stage: stage, err: err}
		}
	}
	raw, err := readCargoArchiveAt(ctx, root, name, checksum, func(object *nativeUpdateObject) error {
		var current syscall.ByHandleFileInformation
		if syscall.GetFileInformationByHandle(syscall.Handle(object.f.Fd()), &current) != nil || leafInfo.VolumeSerialNumber != current.VolumeSerialNumber || leafInfo.FileIndexHigh != current.FileIndexHigh || leafInfo.FileIndexLow != current.FileIndexLow {
			return errUpdateObject
		}
		return nil
	})
	if err != nil {
		return nil, &cargoArchiveReadError{stage: "body_read_or_verification", err: err}
	}
	return raw, nil
}

func readCargoArchiveAt(ctx context.Context, archiveRoot, name, checksum string, held func(*nativeUpdateObject) error) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !cargoPackageDirectory(name) || !vendorSHA(checksum) {
		return nil, errUpdateObject
	}
	p, err := syscall.UTF16PtrFromString(archiveRoot)
	if err != nil {
		return nil, err
	}
	h, err := syscall.CreateFile(p, 0x80, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE, nil, syscall.OPEN_EXISTING, 0x02000000, 0)
	if err != nil {
		return nil, errUpdateObject
	}
	defer syscall.CloseHandle(h)
	root, ok := handlePath(h)
	if !ok {
		return nil, errUpdateObject
	}
	var info syscall.ByHandleFileInformation
	if syscall.GetFileInformationByHandle(h, &info) != nil || info.FileAttributes&syscall.FILE_ATTRIBUTE_DIRECTORY == 0 {
		return nil, errUpdateObject
	}
	object, err := openUpdateObject(filepath.Join(root, name+".crate"))
	if err != nil {
		return nil, errors.New("locked_public_archive_unavailable")
	}
	defer object.Close()
	if held != nil {
		if err := held(object.(*nativeUpdateObject)); err != nil {
			return nil, err
		}
	}
	raw := object.Raw()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(raw) == 0 || len(raw) > cargoArchiveLimit || digest(raw) != checksum {
		return nil, errors.New("locked_archive_checksum_mismatch")
	}
	// openUpdateObject's buffer remains pinned during this exact copy. Only
	// this owned result is subsequently expanded, never a reopened pathname.
	result := append([]byte(nil), raw...)
	if err := object.Close(); err != nil {
		clear(result)
		return nil, errors.New("locked_archive_close_failed")
	}
	return result, nil
}

type cargoExpansionBudget struct {
	bytes        int64
	materialized int64
	entries      int
	expanded     int64
}

func (s *softwareBuildSnapshot) createVendorDirectories(path string) error {
	if s.paths[path] {
		return nil
	}
	rel, err := filepath.Rel(s.Root, path)
	if err != nil || !s.paths[s.Root] || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || len(s.paths) >= cargoVendorEntryLimit {
		return errUpdateObject
	}
	if err := s.createVendorDirectories(filepath.Dir(path)); err != nil {
		return err
	}
	if os.Mkdir(path, 0700) != nil {
		return errUpdateObject
	}
	h, ok := openPlain(path, true)
	if !ok {
		return errUpdateObject
	}
	s.dirs = append(s.dirs, h)
	s.paths[path] = true
	return nil
}

func (s *softwareBuildSnapshot) extractCargoArchive(ctx context.Context, name, checksum string, raw []byte, budget *cargoExpansionBudget) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if !cargoPackageDirectory(name) || !vendorSHA(checksum) || len(raw) == 0 || len(raw) > cargoArchiveLimit || digest(raw) != checksum {
		return errors.New("locked_archive_checksum_mismatch")
	}
	z, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return errUpdateObject
	}
	defer z.Close()
	// Bound all decompressed bytes, including tar headers/padding, independently
	// of declared payload sizes. Each file is also budgeted before allocation.
	remaining := int64(cargoVendorTotalLimit+(32<<20)) - budget.expanded
	if remaining <= 0 {
		return errors.New("archive_expansion_budget_exceeded")
	}
	expanded := &io.LimitedReader{R: z, N: remaining + 1}
	defer func() { budget.expanded += remaining + 1 - expanded.N }()
	t := tar.NewReader(expanded)
	root := filepath.Join(s.Root, "vendor", name)
	if err := s.createVendorDirectories(root); err != nil {
		return err
	}
	files := map[string]string{}
	names := map[string]bool{}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		header, err := t.Next()
		if err == io.EOF {
			break
		}
		if err != nil || expanded.N <= 0 || budget.entries >= cargoVendorEntryLimit {
			return errors.New("archive_expansion_invalid_or_excessive")
		}
		budget.entries++
		path := strings.TrimSuffix(header.Name, "/")
		if path == name && header.Typeflag == tar.TypeDir {
			continue
		}
		if !strings.HasPrefix(path, name+"/") {
			return errUpdateObject
		}
		rel := strings.TrimPrefix(path, name+"/")
		key := strings.ToLower(rel)
		if !vendorRelativePath(rel) || names[key] || strings.EqualFold(rel, ".cargo-checksum.json") {
			return errUpdateObject
		}
		names[key] = true
		switch header.Typeflag {
		case tar.TypeDir:
			if header.Size != 0 {
				return errUpdateObject
			}
			if err := s.createVendorDirectories(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if header.Size < 0 || header.Size > cargoVendorFileLimit || budget.bytes > cargoVendorTotalLimit-header.Size {
				return errors.New("archive_expansion_budget_exceeded")
			}
			budget.bytes += header.Size
			content := make([]byte, int(header.Size))
			_, err := io.ReadFull(t, content)
			if err != nil {
				clear(content)
				return errUpdateObject
			}
			path := filepath.Join(root, filepath.FromSlash(rel))
			if err = s.createVendorDirectories(filepath.Dir(path)); err == nil && len(s.paths) < cargoVendorEntryLimit {
				err = s.create(path, content)
			} else if err == nil {
				err = errUpdateObject
			}
			if err == nil {
				files[rel] = digest(content)
				budget.materialized += header.Size
			}
			clear(content)
			if err != nil {
				return err
			}
		default:
			return errors.New("archive_link_or_special_entry_rejected")
		}
	}
	if len(files) == 0 || expanded.N <= 0 {
		return errUpdateObject
	}
	// Consume the same gzip stream to its checksum/trailer boundary. Reject
	// nonzero trailing tar data and expansion beyond the finite budget.
	var scratch [8192]byte
	defer clear(scratch[:])
	for {
		n, err := expanded.Read(scratch[:])
		for _, b := range scratch[:n] {
			if b != 0 {
				return errUpdateObject
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil || expanded.N <= 0 {
			return errUpdateObject
		}
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	metadata, err := json.Marshal(map[string]any{"package": checksum, "files": files})
	if err != nil {
		return err
	}
	defer clear(metadata)
	if len(s.paths) >= cargoVendorEntryLimit || int64(len(metadata)) > cargoVendorTotalLimit-budget.bytes {
		return errors.New("archive_metadata_budget_exceeded")
	}
	budget.bytes += int64(len(metadata))
	if err := s.create(filepath.Join(root, ".cargo-checksum.json"), metadata); err != nil {
		return err
	}
	budget.materialized += int64(len(metadata))
	return nil
}

func (s *softwareBuildSnapshot) extractLockedCargoVendor(ctx context.Context, lock []byte, read cargoArchiveReader) error {
	return s.extractLockedCargoVendorWithLimit(ctx, lock, read, cargoArchiveTotalLimit)
}

func (s *softwareBuildSnapshot) extractLockedCargoVendorWithLimit(ctx context.Context, lock []byte, read cargoArchiveReader, compressedLimit int64) error {
	identities, ok := cargoLockIdentities(lock)
	if !ok || read == nil {
		return errUpdateObject
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	started := time.Now()
	type archive struct{ name, sha string }
	var packages []archive
	seen := map[string]bool{}
	for identity := range identities {
		fields := strings.Split(identity, "\n")
		if fields[2] == "" {
			continue
		}
		name := fields[0] + "-" + fields[1]
		if !cargoPackageDirectory(name) || !vendorSHA(fields[3]) || seen[strings.ToLower(name)] {
			return errUpdateObject
		}
		seen[strings.ToLower(name)] = true
		packages = append(packages, archive{name, fields[3]})
	}
	sort.Slice(packages, func(i, j int) bool { return packages[i].name < packages[j].name })
	if err := s.createVendorDirectories(filepath.Join(s.Root, "vendor")); err != nil {
		return err
	}
	budget := cargoExpansionBudget{}
	var compressed int64
	completed := 0
	failure := func(stage, cause string, err error) error {
		return &cargoInputError{cause: cause, stage: stage, err: err, progress: &cargoArchiveProgress{Expected: len(packages), Completed: completed, Entries: budget.entries, Bytes: budget.materialized, ElapsedMS: time.Since(started).Milliseconds()}}
	}
	for _, pkg := range packages {
		if err := ctx.Err(); err != nil {
			return failure("before_read", "locked_archive_unavailable", err)
		}
		raw, err := read(ctx, pkg.name, pkg.sha)
		if err != nil {
			clear(raw)
			return failure("archive_read", "locked_archive_unavailable", err)
		}
		if compressed > compressedLimit-int64(len(raw)) {
			clear(raw)
			return failure("compressed_budget", "archive_expansion_rejected", errors.New("archive_compressed_budget_exceeded"))
		}
		compressed += int64(len(raw))
		err = s.extractCargoArchive(ctx, pkg.name, pkg.sha, raw, &budget)
		clear(raw)
		if err != nil {
			return failure("archive_expansion", "archive_expansion_rejected", err)
		}
		completed++
	}
	return s.exactOrError()
}

func (s *softwareBuildSnapshot) exactOrError() error {
	if !s.exact() {
		return errUpdateObject
	}
	return nil
}
