//go:build windows

package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"unsafe"
)

const mp4CargoWorkspace = "[workspace]\nresolver = \"3\"\nmembers = [\"cc-ask\",\"cc-chrome\",\"cc-job-policy\",\"cc-live\",\"cc-local-llm\",\"cc-task-title\"]\n"

func softwareProducerGoArgs(root, output string, files []string) []string {
	args := []string{"-C", filepath.Join(root, "unattended-runner"), "build", "-mod=readonly", "-pgo=off", "-trimpath", "-o", output}
	return append(args, files...)
}

type softwareBuildSnapshot struct {
	Root               string
	files              []*os.File
	dirs               []syscall.Handle
	paths              map[string]bool
	sealed             []snapshotDirectoryACL
	cacheLink          string
	cargoDrive         string
	cargoDriveTarget   string
	releaseCargoDrive  func() error
	borrowedCargoDrive bool
	LockSHA            string
	ManifestSHA        string
}

type snapshotDirectoryACL struct {
	handle           syscall.Handle
	descriptor, dacl uintptr
	protected        bool
	autoInherited    bool
}

func (s *softwareBuildSnapshot) Close() error {
	return s.closeWithRestore(func(a snapshotDirectoryACL) error {
		flags := uint32(4 | 0x20000000)
		if a.protected {
			flags = 4 | 0x80000000
		}
		var code uintptr
		if a.autoInherited {
			code, _, _ = updateSetSecurity.Call(uintptr(a.handle), 1, uintptr(flags), 0, 0, a.dacl, 0)
		} else {
			code, _, _ = updateNTSetSecurity.Call(uintptr(a.handle), 4, a.descriptor)
		}
		if code != 0 {
			return errors.New("snapshot_acl_restore_failed")
		}
		var actual uintptr
		code, _, _ = updateGetSecurity.Call(uintptr(a.handle), 1, 4, 0, 0, 0, 0, uintptr(unsafe.Pointer(&actual)))
		if code != 0 {
			return errors.New("snapshot_acl_restore_readback_failed")
		}
		defer updateLocalFree.Call(actual)
		capture := func(sd uintptr) string {
			var p *uint16
			var n uint32
			ok, _, _ := updateSDString.Call(sd, 1, 4, uintptr(unsafe.Pointer(&p)), uintptr(unsafe.Pointer(&n)))
			if ok == 0 || p == nil {
				return ""
			}
			defer updateLocalFree.Call(uintptr(unsafe.Pointer(p)))
			return syscall.UTF16ToString(unsafe.Slice(p, n))
		}
		if before, after := capture(a.descriptor), capture(actual); before == "" || before != after {
			return errors.New("snapshot_acl_restore_mismatch")
		}
		return nil
	})
}

func (s *softwareBuildSnapshot) closeWithRestore(restore func(snapshotDirectoryACL) error) error {
	var errs []error
	if e := s.closeCargoDirectory(); e != nil {
		errs = append(errs, e)
	}
	for i := 0; i < len(s.sealed); i++ {
		a := s.sealed[i]
		if e := restore(a); e != nil {
			errs = append(errs, e)
		}
		updateLocalFree.Call(a.descriptor)
		if e := syscall.CloseHandle(a.handle); e != nil {
			errs = append(errs, errors.New("snapshot_acl_handle_close_failed"))
		}
	}
	s.sealed = nil
	for i := len(s.files) - 1; i >= 0; i-- {
		if e := s.files[i].Close(); e != nil {
			errs = append(errs, errors.New("snapshot_file_close_failed"))
		}
	}
	s.files = nil
	for i := len(s.dirs) - 1; i >= 0; i-- {
		if e := syscall.CloseHandle(s.dirs[i]); e != nil {
			errs = append(errs, errors.New("snapshot_directory_close_failed"))
		}
	}
	s.dirs = nil
	if s.cacheLink != "" {
		target, e := os.Readlink(s.cacheLink)
		if e != nil || !sameCleanPath(target, mp4PublicCargoRegistry) {
			errs = append(errs, errors.New("snapshot_cache_link_identity_unknown"))
		} else if e = os.Remove(s.cacheLink); e != nil {
			errs = append(errs, errors.New("snapshot_cache_link_cleanup_failed"))
		}
		s.cacheLink = ""
	}
	return errors.Join(errs...)
}

func snapshotDirectories(path, root string, snapshot *softwareBuildSnapshot) error {
	if !sameCleanPath(path, root) {
		if err := snapshotDirectories(filepath.Dir(path), root, snapshot); err != nil {
			return err
		}
	}
	if !snapshot.paths[path] {
		if !sameCleanPath(path, root) {
			if err := os.Mkdir(path, 0700); err != nil {
				return err
			}
		}
		h, ok := openPlain(path, true)
		if !ok {
			return errUpdateObject
		}
		snapshot.dirs = append(snapshot.dirs, h)
		snapshot.paths[path] = true
	}
	return nil
}

// Materialize only independently validated held source buffers in a new build
// namespace. No input is taken from the mutable checkout during compilation.
// File handles and directory identities remain held through the producer ACK.
func materializeMP4BuildSnapshot(build softwareBuildPlan, evidence askBuildEvidence, sources []updateObject) (*softwareBuildSnapshot, error) {
	return materializeMP4BuildSnapshotContext(context.Background(), build, evidence, sources)
}

func materializeMP4BuildSnapshotContext(ctx context.Context, build softwareBuildPlan, evidence askBuildEvidence, sources []updateObject) (result *softwareBuildSnapshot, err error) {
	return materializeMP4BuildSnapshotWith(ctx, build, evidence, sources,
		func(s *softwareBuildSnapshot) error { return s.sealDirectories() },
		func(s *softwareBuildSnapshot) error { return s.Close() })
}

func materializeMP4BuildSnapshotWith(ctx context.Context, build softwareBuildPlan, evidence askBuildEvidence, sources []updateObject, seal func(*softwareBuildSnapshot) error, closeSnapshot func(*softwareBuildSnapshot) error) (result *softwareBuildSnapshot, err error) {
	root := filepath.Join(build.Target, "source")
	if len(sources) != len(evidence.Inputs) || os.Mkdir(root, 0700) != nil {
		return nil, errUpdateObject
	}
	snapshot := &softwareBuildSnapshot{Root: root, paths: map[string]bool{}}
	ok := false
	defer func() {
		if !ok {
			if cleanup := closeSnapshot(snapshot); cleanup != nil {
				err = errors.Join(err, errSnapshotCleanup, cleanup)
			}
		}
	}()
	if err := snapshotDirectories(root, root, snapshot); err != nil {
		return nil, err
	}
	for i, source := range evidence.Inputs {
		if source.Path == "Cargo.toml" && !build.GoRunner {
			continue
		}
		if source.Path == "Cargo.lock" && !build.GoRunner && build.Source == mp4Source {
			continue
		}
		if !askPlanSourcePath(source.Path, askSoftwarePlan{GoRunner: build.GoRunner}) {
			return nil, errUpdateObject
		}
		raw := sources[i].Raw()
		if digest(raw) != source.SHA {
			return nil, errUpdateObject
		}
		path := filepath.Join(root, filepath.FromSlash(source.Path))
		if err := snapshotDirectories(filepath.Dir(path), root, snapshot); err != nil {
			return nil, err
		}
		if err := snapshot.create(path, raw); err != nil {
			return nil, err
		}
	}
	if !build.GoRunner {
		if err := snapshot.create(filepath.Join(root, "Cargo.toml"), []byte(mp4CargoWorkspace)); err != nil {
			return nil, err
		}
		if build.Source == mp4Source {
			found := false
			for i, source := range evidence.Inputs {
				if source.Path == "Cargo.lock" {
					found = true
					if err := snapshot.resolveCargoLock(ctx, sources[i].Raw()); err != nil {
						return nil, err
					}
				}
			}
			if !found {
				return nil, errUpdateObject
			}
			if err := snapshot.materializeCargoVendor(ctx); err != nil {
				return nil, err
			}
		}
	}
	if !snapshot.exact() {
		return nil, errUpdateObject
	}
	if err := seal(snapshot); err != nil {
		return nil, err
	}
	if !snapshot.exact() {
		return nil, errUpdateObject
	}
	keys := make([]string, 0, len(snapshot.paths))
	for path := range snapshot.paths {
		keys = append(keys, path)
	}
	sort.Strings(keys)
	heldFiles := make(map[string]*os.File, len(snapshot.files))
	for _, f := range snapshot.files {
		if heldFiles[f.Name()] != nil {
			return nil, errUpdateObject
		}
		heldFiles[f.Name()] = f
	}
	var manifest []byte
	for _, path := range keys {
		info, e := os.Stat(path)
		if e != nil {
			return nil, e
		}
		if info.IsDir() {
			continue
		}
		f := heldFiles[path]
		if f == nil {
			return nil, errUpdateObject
		}
		if _, e = f.Seek(0, io.SeekStart); e != nil {
			return nil, e
		}
		b, e := io.ReadAll(io.LimitReader(f, cargoVendorFileLimit+1))
		if e != nil || len(b) > cargoVendorFileLimit || int64(len(b)) != info.Size() {
			clear(b)
			return nil, errors.Join(errUpdateObject, e)
		}
		rel, _ := filepath.Rel(snapshot.Root, path)
		manifest = append(manifest, []byte(filepath.ToSlash(rel)+"\n"+digest(b)+"\n")...)
		clear(b)
	}
	snapshot.ManifestSHA = digest(manifest)
	clear(manifest)
	ok = true
	return snapshot, nil
}

// The snapshot is this operation's new private build input, never an existing
// data tree. Remove child-creation permission during compilation, retaining
// exact original DACLs and WRITE_DAC handles for restoration at release.
func (s *softwareBuildSnapshot) sealDirectories() error {
	token, e := syscall.OpenCurrentProcessToken()
	if e != nil {
		return e
	}
	defer token.Close()
	u, e := token.GetTokenUser()
	if e != nil {
		return e
	}
	sid, e := u.User.Sid.String()
	if e != nil {
		return e
	}
	text, e := syscall.UTF16PtrFromString("D:P(A;;GRGX;;;" + sid + ")")
	if e != nil {
		return e
	}
	var restricted uintptr
	ok, _, _ := updateStringSD.Call(uintptr(unsafe.Pointer(text)), 1, uintptr(unsafe.Pointer(&restricted)), 0)
	if ok == 0 {
		return errUpdateObject
	}
	defer updateLocalFree.Call(restricted)
	var dacl uintptr
	var present, defaulted int32
	ok, _, _ = updateAdvapi.NewProc("GetSecurityDescriptorDacl").Call(restricted, uintptr(unsafe.Pointer(&present)), uintptr(unsafe.Pointer(&dacl)), uintptr(unsafe.Pointer(&defaulted)))
	if ok == 0 || present == 0 {
		return errUpdateObject
	}
	paths := make([]string, 0, len(s.paths))
	for path := range s.paths {
		paths = append(paths, path)
	}
	sort.Slice(paths, func(i, j int) bool { return len(paths[i]) < len(paths[j]) })
	for _, path := range paths {
		info, e := os.Stat(path)
		if e != nil {
			return e
		}
		if !info.IsDir() {
			continue
		}
		p, e := syscall.UTF16PtrFromString(path)
		if e != nil {
			return e
		}
		h, e := syscall.CreateFile(p, 0x00020000|0x00040000, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE, nil, syscall.OPEN_EXISTING, 0x02000000|0x00200000, 0)
		if e != nil {
			return e
		}
		var original, oldDacl uintptr
		code, _, _ := updateGetSecurity.Call(uintptr(h), 1, 4, 0, 0, uintptr(unsafe.Pointer(&oldDacl)), 0, uintptr(unsafe.Pointer(&original)))
		if code != 0 {
			syscall.CloseHandle(h)
			return errUpdateObject
		}
		var control uint16
		var revision uint32
		ok, _, _ = updateSDControl.Call(original, uintptr(unsafe.Pointer(&control)), uintptr(unsafe.Pointer(&revision)))
		if ok == 0 {
			updateLocalFree.Call(original)
			syscall.CloseHandle(h)
			return errUpdateObject
		}
		s.sealed = append(s.sealed, snapshotDirectoryACL{handle: h, descriptor: original, dacl: oldDacl, protected: control&0x1000 != 0, autoInherited: control&0x0400 != 0})
	}
	// Capture every original child descriptor before changing any parent.
	for _, a := range s.sealed {
		code, _, _ := updateNTSetSecurity.Call(uintptr(a.handle), 4, restricted)
		if code != 0 {
			return errUpdateObject
		}
	}
	return nil
}

func (s *softwareBuildSnapshot) create(path string, raw []byte) error {
	p, e := syscall.UTF16PtrFromString(path)
	if e != nil {
		return e
	}
	h, e := syscall.CreateFile(p, syscall.GENERIC_READ|syscall.GENERIC_WRITE, syscall.FILE_SHARE_READ, nil, syscall.CREATE_NEW, 0x00200000, 0)
	if e != nil {
		return e
	}
	f := os.NewFile(uintptr(h), path)
	keep := false
	defer func() {
		if !keep {
			f.Close()
		}
	}()
	if n, e := f.Write(raw); e != nil || n != len(raw) {
		return errUpdateObject
	}
	if f.Sync() != nil {
		return errUpdateObject
	}
	if _, e = f.Seek(0, io.SeekStart); e != nil {
		return e
	}
	check := make([]byte, len(raw)+1)
	defer clear(check)
	n, e := io.ReadFull(f, check)
	if e != io.EOF && e != io.ErrUnexpectedEOF || n != len(raw) || !bytes.Equal(raw, check[:n]) {
		return errUpdateObject
	}
	if _, e = fileSoftwareIdentity(f); e != nil {
		return e
	}
	// Retain the original creator handle. Go/Rust ordinary readers share write
	// and may read it, while new write/delete opens remain excluded by this
	// handle's FILE_SHARE_READ. No pathname close/reopen gap is introduced.
	s.files = append(s.files, f)
	s.paths[path] = true
	keep = true
	return nil
}

func (s *softwareBuildSnapshot) exact() bool {
	seen := map[string]bool{}
	err := filepath.WalkDir(s.Root, func(path string, d os.DirEntry, err error) error {
		if err != nil || !s.paths[path] || d.Type()&os.ModeSymlink != 0 {
			return errUpdateObject
		}
		seen[path] = true
		return nil
	})
	return err == nil && len(seen) == len(s.paths)
}

func snapshotSelectedGoFiles(files []string, snapshot *softwareBuildSnapshot) []string {
	result := make([]string, 0, len(files))
	for _, f := range files {
		result = append(result, filepath.Join(snapshot.Root, "unattended-runner", filepath.Base(f)))
	}
	return result
}

func compilerEnvironmentFixed() bool {
	for _, key := range []string{"RUSTFLAGS", "CARGO_ENCODED_RUSTFLAGS", "CARGO_BUILD_RUSTFLAGS", "RUSTC_WRAPPER", "RUSTC_WORKSPACE_WRAPPER", "CARGO_BUILD_RUSTC_WRAPPER", "RUSTC", "CARGO_BUILD_RUSTC", "CARGO_BUILD_TARGET", "CARGO_TARGET_X86_64_PC_WINDOWS_MSVC_RUSTFLAGS"} {
		if v := os.Getenv(key); v != "" && !(key == "RUSTC_WRAPPER" && strings.EqualFold(filepath.Clean(v), filepath.Clean("D:/CC/Tools/sccache/sccache.exe"))) {
			return false
		}
	}
	return true
}
