//go:build windows

package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
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
	Root   string
	files  []*os.File
	dirs   []syscall.Handle
	paths  map[string]bool
	sealed []snapshotDirectoryACL
}

type snapshotDirectoryACL struct {
	handle           syscall.Handle
	descriptor, dacl uintptr
	protected        bool
}

func (s *softwareBuildSnapshot) Close() {
	for i := len(s.sealed) - 1; i >= 0; i-- {
		a := s.sealed[i]
		flags := uint32(4 | 0x20000000)
		if a.protected {
			flags = 4 | 0x80000000
		}
		updateSetSecurity.Call(uintptr(a.handle), 1, uintptr(flags), 0, 0, a.dacl, 0)
		updateLocalFree.Call(a.descriptor)
		syscall.CloseHandle(a.handle)
	}
	for i := len(s.files) - 1; i >= 0; i-- {
		s.files[i].Close()
	}
	for i := len(s.dirs) - 1; i >= 0; i-- {
		syscall.CloseHandle(s.dirs[i])
	}
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
	root := filepath.Join(build.Target, "source")
	if len(sources) != len(evidence.Inputs) || os.Mkdir(root, 0700) != nil {
		return nil, errUpdateObject
	}
	snapshot := &softwareBuildSnapshot{Root: root, paths: map[string]bool{}}
	ok := false
	defer func() {
		if !ok {
			snapshot.Close()
		}
	}()
	if err := snapshotDirectories(root, root, snapshot); err != nil {
		return nil, err
	}
	for i, source := range evidence.Inputs {
		if source.Path == "Cargo.toml" && !build.GoRunner {
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
	}
	if !snapshot.exact() {
		return nil, errUpdateObject
	}
	if err := snapshot.sealDirectories(); err != nil {
		return nil, err
	}
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
	text, e := syscall.UTF16PtrFromString("D:P(A;OICI;GRGX;;;" + sid + ")")
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
	for path := range s.paths {
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
		s.sealed = append(s.sealed, snapshotDirectoryACL{h, original, oldDacl, control&0x1000 != 0})
		code, _, _ = updateSetSecurity.Call(uintptr(h), 1, 4|0x80000000, 0, 0, dacl, 0)
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
