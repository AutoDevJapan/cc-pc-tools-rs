//go:build windows

package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
)

const cargoVendorFileLimit = 64 << 20
const cargoVendorTotalLimit = 512 << 20
const cargoVendorEntryLimit = 65536

func cargoVendorBuildArgs(root string) []string {
	return []string{"--config", `source.crates-io.replace-with="operation-vendor"`, "--config", "source.operation-vendor.directory=" + strconv.Quote(filepath.ToSlash(filepath.Join(root, "vendor")))}
}

func vendorSHA(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 32 && value == strings.ToLower(value)
}

func vendorRelativePath(path string) bool {
	if len(path) == 0 || len(path) > 4096 || strings.ContainsAny(path, "\\:\x00*?\"<>|") || !filepath.IsLocal(filepath.FromSlash(path)) || filepath.ToSlash(filepath.Clean(filepath.FromSlash(path))) != path {
		return false
	}
	parts := strings.Split(path, "/")
	if len(parts) > 64 {
		return false
	}
	for _, part := range parts {
		if strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") {
			return false
		}
		for _, ch := range part {
			if ch < 32 {
				return false
			}
		}
	}
	return true
}

// Cargo produces this metadata from its locked registry packages. Reject
// duplicate keys and Windows aliases before using it to admit any input.
func cargoVendorChecksums(raw []byte) (map[string]string, string, bool) {
	if len(raw) == 0 || len(raw) > cargoVendorFileLimit {
		return nil, "", false
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	object := func(want json.Delim) bool { token, err := d.Token(); return err == nil && token == want }
	if !object('{') {
		return nil, "", false
	}
	seen := map[string]bool{}
	files, names := map[string]string{}, map[string]bool{}
	packageSHA := ""
	for d.More() {
		token, err := d.Token()
		key, valid := token.(string)
		if err != nil || !valid || seen[key] {
			return nil, "", false
		}
		seen[key] = true
		switch key {
		case "package":
			if d.Decode(&packageSHA) != nil || !vendorSHA(packageSHA) {
				return nil, "", false
			}
		case "files":
			if !object('{') {
				return nil, "", false
			}
			for d.More() {
				token, err := d.Token()
				path, valid := token.(string)
				if err != nil || !valid || !vendorRelativePath(path) || strings.EqualFold(path, ".cargo-checksum.json") || names[strings.ToLower(path)] || len(files) >= cargoVendorEntryLimit {
					return nil, "", false
				}
				var sha string
				if d.Decode(&sha) != nil || !vendorSHA(sha) {
					return nil, "", false
				}
				names[strings.ToLower(path)], files[path] = true, sha
			}
			if !object('}') {
				return nil, "", false
			}
		default:
			return nil, "", false
		}
	}
	var trailing any
	if !object('}') || d.Decode(&trailing) != io.EOF || !seen["package"] || !seen["files"] || len(files) == 0 {
		return nil, "", false
	}
	return files, packageSHA, true
}

func (s *softwareBuildSnapshot) holdVendorDirectory(path string) error {
	relative, err := filepath.Rel(s.Root, path)
	if err != nil || !s.paths[s.Root] || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return errUpdateObject
	}
	if s.paths[path] {
		return nil
	}
	if err := s.holdVendorDirectory(filepath.Dir(path)); err != nil {
		return err
	}
	h, ok := openPlain(path, true)
	if !ok {
		return errors.New("vendor_directory_identity_unavailable")
	}
	s.dirs = append(s.dirs, h)
	s.paths[path] = true
	return nil
}

func (s *softwareBuildSnapshot) holdVendorFile(path string, total *int64) ([]byte, error) {
	if len(s.paths) >= cargoVendorEntryLimit || s.paths[path] {
		return nil, errors.New("vendor_duplicate_or_excess_input")
	}
	if err := s.holdVendorDirectory(filepath.Dir(path)); err != nil {
		return nil, err
	}
	h, ok := openPlain(path, false)
	if !ok {
		return nil, errors.New("vendor_file_identity_unavailable")
	}
	f := os.NewFile(uintptr(h), path)
	s.files = append(s.files, f)
	s.paths[path] = true
	info, err := f.Stat()
	if err != nil || info.Size() < 0 || info.Size() > cargoVendorFileLimit || *total > cargoVendorTotalLimit-info.Size() {
		return nil, errors.New("vendor_file_size_or_total_budget")
	}
	raw, err := io.ReadAll(io.LimitReader(f, info.Size()+1))
	if err != nil || int64(len(raw)) != info.Size() {
		clear(raw)
		return nil, errUpdateObject
	}
	*total += int64(len(raw))
	return raw, nil
}

type cargoArchiveReader func(context.Context, string, string) ([]byte, error)

type cargoInputError struct {
	cause    string
	stage    string
	err      error
	progress *cargoArchiveProgress
}

type cargoArchiveReadError struct {
	stage string
	err   error
}

func (e *cargoArchiveReadError) Error() string { return "archive_read_failed" }
func (e *cargoArchiveReadError) Unwrap() error { return e.err }

type cargoArchiveGuardError struct{ contextState string }

func (e *cargoArchiveGuardError) Error() string { return "public_archive_source_unqualified" }

type cargoArchiveProgress struct {
	Expected  int   `json:"archives_expected"`
	Completed int   `json:"archives_completed"`
	Entries   int   `json:"entries_processed"`
	Bytes     int64 `json:"bytes_materialized"`
	ElapsedMS int64 `json:"elapsed_ms"`
}

func (e *cargoInputError) Error() string { return "cargo_input_" + e.cause }
func (e *cargoInputError) Unwrap() error { return e.err }
func cargoInputDiagnostic(err error) *compilerDiagnostic {
	var input *cargoInputError
	if errors.As(err, &input) {
		switch input.cause {
		case "locked_archive_unavailable", "archive_expansion_rejected":
			cause := input.cause
			var errno syscall.Errno
			var read *cargoArchiveReadError
			var guard *cargoArchiveGuardError
			d := &compilerDiagnostic{Cause: cause, InputStage: input.stage, ArchiveProgress: input.progress}
			if errors.As(input.err, &guard) {
				d.InputContextState = guard.contextState
			}
			if errors.As(input.err, &read) {
				d.InputStage = read.stage
				if read.stage == "root_qualification" || read.stage == "leaf_qualification" {
					cause = "archive_source_unqualified"
				}
			}
			switch {
			case errors.Is(input.err, context.DeadlineExceeded):
				cause = "archive_input_deadline"
			case errors.Is(input.err, context.Canceled):
				cause = "archive_input_canceled"
			case errors.As(input.err, &errno):
				switch errno {
				case 32:
					d.InputOSCode = uint32(errno)
					cause = "archive_sharing_violation"
				case 5:
					d.InputOSCode = uint32(errno)
					cause = "archive_access_denied"
				case 2, 3:
					d.InputOSCode = uint32(errno)
					cause = "archive_file_unavailable"
				}
			}
			d.Cause = cause
			return d
		}
	}
	return nil
}

func (s *softwareBuildSnapshot) admitCargoVendor(ctx context.Context, lock []byte, archives ...cargoArchiveReader) error {
	if len(archives) != 1 || archives[0] == nil {
		return errors.New("locked_archive_reader_unavailable")
	}
	return s.extractLockedCargoVendor(ctx, lock, archives[0])
}

// Validation of an already materialized namespace remains separate from the
// archive trust boundary. It cannot authorize unverified files on its own.
func (s *softwareBuildSnapshot) validateCargoVendor(ctx context.Context, lock []byte) error {
	identities, ok := cargoLockIdentities(lock)
	if !ok {
		return errUpdateObject
	}
	expected := map[string]string{}
	for identity := range identities {
		fields := strings.Split(identity, "\n")
		if fields[2] == "" {
			continue
		}
		directory := fields[0] + "-" + fields[1]
		if !vendorRelativePath(directory) || strings.Contains(directory, "/") || !vendorSHA(fields[3]) || expected[strings.ToLower(directory)] != "" {
			return errUpdateObject
		}
		expected[strings.ToLower(directory)] = fields[3]
	}
	vendor := filepath.Join(s.Root, "vendor")
	if err := s.holdVendorDirectory(vendor); err != nil {
		return err
	}
	entries, err := os.ReadDir(vendor)
	if err != nil || len(entries) != len(expected) {
		return errors.New("vendor_package_set_mismatch")
	}
	var total int64
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		checksum := expected[strings.ToLower(entry.Name())]
		if !entry.IsDir() || checksum == "" {
			return errUpdateObject
		}
		delete(expected, strings.ToLower(entry.Name()))
		root := filepath.Join(vendor, entry.Name())
		raw, err := s.holdVendorFile(filepath.Join(root, ".cargo-checksum.json"), &total)
		if err != nil {
			return err
		}
		files, packageSHA, valid := cargoVendorChecksums(raw)
		clear(raw)
		if !valid || packageSHA != checksum {
			return errors.New("vendor_checksum_metadata_mismatch")
		}
		paths := make([]string, 0, len(files))
		for path := range files {
			paths = append(paths, path)
		}
		sort.Strings(paths)
		for _, path := range paths {
			if err := ctx.Err(); err != nil {
				return err
			}
			raw, err := s.holdVendorFile(filepath.Join(root, filepath.FromSlash(path)), &total)
			valid := err == nil && digest(raw) == files[path]
			clear(raw)
			if !valid {
				return errors.Join(errors.New("vendor_file_checksum_mismatch"), err)
			}
		}
	}
	if len(expected) != 0 || !s.exact() {
		return errUpdateObject
	}
	return nil
}

// Extract verified locked archives instead of trusting Cargo's cached archive
// or generated checksum metadata. The same retained compressed bytes are
// hashed and consumed, with finite expansion limits before each creation.
func (s *softwareBuildSnapshot) materializeCargoVendor(ctx context.Context) error {
	for _, f := range s.files {
		if sameCleanPath(f.Name(), filepath.Join(s.Root, "Cargo.lock")) {
			if _, err := f.Seek(0, io.SeekStart); err != nil {
				return err
			}
			raw, err := io.ReadAll(io.LimitReader(f, 1<<20+1))
			defer clear(raw)
			if err != nil || digest(raw) != s.LockSHA {
				return errUpdateObject
			}
			return s.admitCargoVendor(ctx, raw, readLockedCargoArchive)
		}
	}
	return errUpdateObject
}
