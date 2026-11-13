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

type cargoArchiveOperationKey struct{}
type cargoArchiveRootPin struct {
	handle syscall.Handle
	info   syscall.ByHandleFileInformation
}
type cargoArchiveOperation struct {
	roots map[string]cargoArchiveRootPin
}

func sameArchiveRoot(a, b syscall.ByHandleFileInformation) bool {
	return a.VolumeSerialNumber == b.VolumeSerialNumber && a.FileIndexHigh == b.FileIndexHigh && a.FileIndexLow == b.FileIndexLow && a.CreationTime == b.CreationTime
}

func qualifyArchiveRoot(ctx context.Context, root string, handle syscall.Handle, qualify func(context.Context, string) error) error {
	op, _ := ctx.Value(cargoArchiveOperationKey{}).(*cargoArchiveOperation)
	if op == nil {
		return qualify(ctx, root)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	var current syscall.ByHandleFileInformation
	actual, ok := handlePath(handle)
	if !ok || !sameCleanPath(actual, root) || syscall.GetFileInformationByHandle(handle, &current) != nil {
		return errUpdateObject
	}
	key := strings.ToLower(filepath.Clean(root))
	if pinned, exists := op.roots[key]; exists {
		var held syscall.ByHandleFileInformation
		if syscall.GetFileInformationByHandle(pinned.handle, &held) != nil || !sameArchiveRoot(held, current) || !sameArchiveRoot(pinned.info, current) {
			return errUpdateObject
		}
		return nil
	}
	// Attribute-only directory handles do not enforce rename sharing on Windows.
	// Pin and compare the same namespace before ordinary qualification starts.
	path, err := syscall.UTF16PtrFromString(root)
	if err != nil {
		return err
	}
	pin, err := syscall.CreateFile(path, 0x81, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE, nil, syscall.OPEN_EXISTING, 0x02200000, 0)
	if err != nil {
		return err
	}
	retained := false
	defer func() {
		if !retained {
			syscall.CloseHandle(pin)
		}
	}()
	var checked syscall.ByHandleFileInformation
	resolved, plain := handlePath(pin)
	if !plain || !sameCleanPath(resolved, root) || syscall.GetFileInformationByHandle(pin, &checked) != nil || checked.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0 || !sameArchiveRoot(current, checked) {
		return errUpdateObject
	}
	if err := qualify(ctx, root); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	op.roots[key] = cargoArchiveRootPin{handle: pin, info: current}
	retained = true
	return nil
}

func (op *cargoArchiveOperation) close() error {
	var errs []error
	for key, pin := range op.roots {
		errs = append(errs, syscall.CloseHandle(pin.handle))
		delete(op.roots, key)
	}
	return errors.Join(errs...)
}

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
		var err error
		if i == 0 {
			err = qualifyArchiveRoot(ctx, root, physical, qualify)
		} else {
			err = qualify(ctx, path)
		}
		if err != nil {
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

type archiveFileCompletion struct {
	path     string
	size     int64
	sha      string
	fragment *softwareBuildSnapshot
	err      error
}
type archiveFileWriter struct {
	create  func(string, []byte) *archiveFileCompletion
	collect func(*archiveFileCompletion) error
	pending []<-chan *archiveFileCompletion
	first   error
}

func (w *archiveFileWriter) submit(path string, raw []byte) error {
	if len(w.pending) == 2 {
		w.take()
	}
	if w.first != nil {
		clear(raw)
		return w.first
	}
	done := make(chan *archiveFileCompletion, 1)
	w.pending = append(w.pending, done)
	go func() { r := w.create(path, raw); clear(raw); done <- r }()
	return nil
}
func (w *archiveFileWriter) take() {
	r := <-w.pending[0]
	w.pending = w.pending[1:]
	if err := w.collect(r); err != nil && w.first == nil {
		w.first = err
	}
}
func (w *archiveFileWriter) finish() error {
	for len(w.pending) > 0 {
		w.take()
	}
	return w.first
}

func (s *softwareBuildSnapshot) fileCreationWorker(budget *cargoExpansionBudget, files map[string]string) *archiveFileWriter {
	return &archiveFileWriter{create: func(path string, raw []byte) *archiveFileCompletion {
		fragment := &softwareBuildSnapshot{Root: s.Root, paths: map[string]bool{}}
		err := fragment.create(path, raw)
		return &archiveFileCompletion{path: path, size: int64(len(raw)), sha: digest(raw), fragment: fragment, err: err}
	}, collect: func(r *archiveFileCompletion) error {
		if r.fragment == nil {
			return errUpdateObject
		}
		// Collection is called only by the tar/controller goroutine, after the
		// writer has finished. No snapshot maps or budgets are shared by workers.
		s.files = append(s.files, r.fragment.files...)
		r.fragment.files = nil
		s.handoffACLs = append(s.handoffACLs, r.fragment.handoffACLs...)
		r.fragment.handoffACLs = nil
		for path := range r.fragment.paths {
			s.paths[path] = true
		}
		if r.err != nil {
			return r.err
		}
		files[r.path] = r.sha
		budget.materialized += r.size
		return nil
	}}
}

func (s *softwareBuildSnapshot) createVendorDirectories(path string) error {
	return s.createVendorDirectoriesReserved(path, 0)
}

func (s *softwareBuildSnapshot) createVendorDirectoriesReserved(path string, reserved int) error {
	if s.paths[path] {
		return nil
	}
	rel, err := filepath.Rel(s.Root, path)
	if err != nil || !s.paths[s.Root] || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || reserved < 0 || len(s.paths)+reserved >= cargoVendorEntryLimit {
		return errUpdateObject
	}
	if err := s.createVendorDirectoriesReserved(filepath.Dir(path), reserved); err != nil {
		return err
	}
	if len(s.paths)+reserved >= cargoVendorEntryLimit {
		return errUpdateObject
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

func (s *softwareBuildSnapshot) extractCargoArchive(ctx context.Context, name, checksum string, raw []byte, budget *cargoExpansionBudget) (result error) {
	return s.extractCargoArchiveWithCreator(ctx, name, checksum, raw, budget, nil)
}

func (s *softwareBuildSnapshot) extractCargoArchiveWithCreator(ctx context.Context, name, checksum string, raw []byte, budget *cargoExpansionBudget, create func(string, []byte) *archiveFileCompletion) (result error) {
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
	createdFiles := map[string]string{}
	writers := s.fileCreationWorker(budget, createdFiles)
	if create != nil {
		writers.create = create
	}
	defer func() { result = errors.Join(result, writers.finish()) }()
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
			if err := s.createVendorDirectoriesReserved(filepath.Join(root, filepath.FromSlash(rel)), len(writers.pending)); err != nil {
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
			if len(writers.pending) == 2 {
				writers.take()
			}
			if writers.first != nil {
				clear(content)
				return writers.first
			}
			if err = s.createVendorDirectoriesReserved(filepath.Dir(path), len(writers.pending)); err == nil && len(s.paths)+len(writers.pending) < cargoVendorEntryLimit {
				err = writers.submit(path, content)
			} else if err == nil {
				err = errUpdateObject
			}
			if err != nil {
				clear(content)
				return err
			}
		default:
			return errors.New("archive_link_or_special_entry_rejected")
		}
	}
	if err := writers.finish(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	for path, sha := range createdFiles {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(rel)] = sha
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
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

func (s *softwareBuildSnapshot) extractLockedCargoVendor(ctx context.Context, lock []byte, read cargoArchiveReader) error {
	return s.extractLockedCargoVendorWithLimit(ctx, lock, read, cargoArchiveTotalLimit)
}

func (s *softwareBuildSnapshot) extractLockedCargoVendorWithLimit(ctx context.Context, lock []byte, read cargoArchiveReader, compressedLimit int64) (result error) {
	identities, ok := cargoLockIdentities(lock)
	if !ok || read == nil {
		return errUpdateObject
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	op := &cargoArchiveOperation{roots: map[string]cargoArchiveRootPin{}}
	ctx = context.WithValue(ctx, cargoArchiveOperationKey{}, op)
	defer func() { result = errors.Join(result, op.close()) }()
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
	if err := ctx.Err(); err != nil {
		return failure("archive_complete", "locked_archive_unavailable", err)
	}
	return s.exactOrError()
}

func (s *softwareBuildSnapshot) exactOrError() error {
	if !s.exact() {
		return errUpdateObject
	}
	return nil
}
