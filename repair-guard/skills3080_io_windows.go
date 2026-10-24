package main

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"unsafe"
)

// NtCreateFile with RootDirectory and a single component gives an atomic
// non-following open/create relative to the held physical parent. FILE_CREATE
// fails on collision and returns the created object's handle in the same call.
// https://learn.microsoft.com/windows/win32/api/winternl/nf-winternl-ntcreatefile
type skillsUnicode struct {
	Length, MaximumLength uint16
	Buffer                *uint16
}
type skillsObjectAttributes struct {
	Length                                       uint32
	Root                                         syscall.Handle
	Name                                         *skillsUnicode
	Attributes                                   uint32
	SecurityDescriptor, SecurityQualityOfService unsafe.Pointer
}
type skillsIOStatus struct{ Status, Information uintptr }

var skillsNtCreateFile = syscall.NewLazyDLL("ntdll.dll").NewProc("NtCreateFile")

func skillsOpenAt(parent syscall.Handle, name string, directory, create bool) (syscall.Handle, bool) {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\:`) {
		return 0, false
	}
	text, e := syscall.UTF16FromString(name)
	if e != nil || len(text) > 32767 {
		return 0, false
	}
	u := skillsUnicode{uint16((len(text) - 1) * 2), uint16(len(text) * 2), &text[0]}
	a := skillsObjectAttributes{Root: parent, Name: &u, Attributes: 0x40}
	a.Length = uint32(unsafe.Sizeof(a))
	var status skillsIOStatus
	var h syscall.Handle
	access := uint32(syscall.GENERIC_READ | 0x100000) // explicit SYNCHRONIZE for native synchronous I/O
	share := uint32(syscall.FILE_SHARE_READ)
	options := uint32(0x00200000 | 0x20 | 0x40)
	if directory {
		access = 0x100000 | 0x80 | 1
		share |= syscall.FILE_SHARE_WRITE
		options = 0x00200000 | 0x20 | 1
	} else if create {
		access |= syscall.GENERIC_WRITE
	}
	disposition := uint32(1)
	if create {
		disposition = 2
	}
	result, _, _ := skillsNtCreateFile.Call(uintptr(unsafe.Pointer(&h)), uintptr(access), uintptr(unsafe.Pointer(&a)), uintptr(unsafe.Pointer(&status)), 0, syscall.FILE_ATTRIBUTE_NORMAL, uintptr(share), uintptr(disposition), uintptr(options), 0, 0)
	runtime.KeepAlive(text)
	runtime.KeepAlive(a)
	if result != 0 {
		return 0, false
	}
	var info syscall.ByHandleFileInformation
	if syscall.GetFileInformationByHandle(h, &info) != nil || info.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0 || (info.FileAttributes&syscall.FILE_ATTRIBUTE_DIRECTORY != 0) != directory || (!directory && info.NumberOfLinks != 1) {
		syscall.CloseHandle(h)
		return 0, false
	}
	return h, true
}

type skillsFS struct {
	dirs    map[string]syscall.Handle
	files   []*os.File
	blocked func(string) bool
}

// Open only the declared regular object; keep it non-sharing until the caller
// has consumed the qualified bytes or finished its narrow stream projection.
func (s *skillsFS) open(path string, limit int64) (*os.File, bool) {
	if !filepath.IsAbs(path) || s.blocked(path) || ownerWindowsAlias(path) {
		return nil, false
	}
	parent, ok := s.directory(filepath.Dir(path))
	if !ok {
		return nil, false
	}
	h, ok := skillsOpenAt(parent, filepath.Base(path), false, false)
	if !ok {
		return nil, false
	}
	f := os.NewFile(uintptr(h), path)
	actual, e := ownerHandlePath(h)
	info, e2 := f.Stat()
	if e != nil || e2 != nil || !same(actual, path) || info.Size() < 1 || info.Size() > limit || !info.Mode().IsRegular() {
		f.Close()
		return nil, false
	}
	s.files = append(s.files, f)
	return f, true
}

func (s *skillsFS) close() {
	for _, f := range s.files {
		f.Close()
	}
	for _, h := range s.dirs {
		syscall.CloseHandle(h)
	}
}
func (s *skillsFS) directory(path string) (syscall.Handle, bool) {
	if !filepath.IsAbs(path) || ownerWindowsAlias(path) {
		return 0, false
	}
	path = filepath.Clean(path)
	key := strings.ToLower(path)
	if h, ok := s.dirs[key]; ok {
		return h, true
	}
	if s.dirs == nil {
		s.dirs = map[string]syscall.Handle{}
	}
	parent := filepath.Dir(path)
	var h syscall.Handle
	if parent == path {
		name, e := syscall.UTF16PtrFromString(path)
		if e != nil {
			return 0, false
		}
		var err error
		h, err = syscall.CreateFile(name, 0x80, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE, nil, syscall.OPEN_EXISTING, 0x02000000|0x00200000, 0)
		if err != nil {
			return 0, false
		}
	} else {
		ph, ok := s.directory(parent)
		if !ok {
			return 0, false
		}
		var ok2 bool
		h, ok2 = skillsOpenAt(ph, filepath.Base(path), true, false)
		if !ok2 {
			return 0, false
		}
	}
	var info syscall.ByHandleFileInformation
	actual, e := ownerHandlePath(h)
	if e != nil || !same(actual, path) || syscall.GetFileInformationByHandle(h, &info) != nil || info.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0 || info.FileAttributes&syscall.FILE_ATTRIBUTE_DIRECTORY == 0 {
		syscall.CloseHandle(h)
		return 0, false
	}
	s.dirs[key] = h
	return h, true
}
func (s *skillsFS) read(path string, limit int64) ([]byte, bool) {
	if !filepath.IsAbs(path) || s.blocked(path) || ownerWindowsAlias(path) {
		return nil, false
	}
	ph, ok := s.directory(filepath.Dir(path))
	if !ok {
		return nil, false
	}
	h, ok := skillsOpenAt(ph, filepath.Base(path), false, false)
	if !ok {
		return nil, false
	}
	f := os.NewFile(uintptr(h), path)
	kept := false
	defer func() {
		if !kept {
			f.Close()
		}
	}()
	actual, e := ownerHandlePath(h)
	info, e2 := f.Stat()
	if e != nil || e2 != nil || !same(actual, path) || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > limit {
		return nil, false
	}
	var before, after syscall.ByHandleFileInformation
	if syscall.GetFileInformationByHandle(h, &before) != nil {
		return nil, false
	}
	b, e := io.ReadAll(io.LimitReader(f, limit+1))
	if e != nil || int64(len(b)) != info.Size() || syscall.GetFileInformationByHandle(h, &after) != nil {
		return nil, false
	}
	before.LastAccessTime = after.LastAccessTime
	if before != after || s.blocked(path) {
		return nil, false
	}
	s.files = append(s.files, f)
	kept = true
	return b, true
}
func skillsBlocked(path string) bool {
	return legacyMatch(path) || ownerCardPath(path) || ownerNonSourcePath(path)
}

func skillsTargets(m skillsManifest) []string {
	names := map[string]bool{}
	for _, f := range m.Files {
		names[strings.Split(f.Target, "/")[0]] = true
	}
	out := []string{}
	for name := range names {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
func skillsTargetsAbsent(m skillsManifest, root string, s *skillsFS) string {
	if s.blocked(root) {
		return "protection"
	}
	if _, ok := s.directory(root); !ok {
		return "physical_parent"
	}
	for _, name := range skillsTargets(m) {
		target := filepath.Join(root, name)
		if s.blocked(target) {
			return "protection"
		}
		if _, e := os.Lstat(target); !os.IsNotExist(e) {
			return "existing_target"
		}
	}
	return ""
}
func skillsCreate(m skillsManifest, root string, s *skillsFS) (int, string) {
	for _, output := range m.Files {
		if len(output.body) == 0 || hashBytes(output.body) != output.SHA {
			return 0, "manifest"
		}
	}
	if reason := skillsTargetsAbsent(m, root, s); reason != "" {
		return 0, reason
	}
	rootH := s.dirs[strings.ToLower(filepath.Clean(root))]
	// Hold every new directory from its atomic creation, so no junction or
	// alternate directory can be substituted between validation and consumption.
	for _, name := range skillsTargets(m) {
		h, ok := skillsOpenAt(rootH, name, true, true)
		if !ok {
			return 0, "existing_target"
		}
		s.dirs[strings.ToLower(filepath.Join(root, name))] = h
	}
	outputs := append([]skillsOutput(nil), m.Files...)
	sort.SliceStable(outputs, func(i, j int) bool {
		a := filepath.Base(outputs[i].Target) == "SKILL.md"
		b := filepath.Base(outputs[j].Target) == "SKILL.md"
		return !a && b
	})
	count := 0
	for _, output := range outputs {
		path := filepath.Join(root, filepath.FromSlash(output.Target))
		if s.blocked(path) {
			return count, "protection"
		}
		parent := filepath.Dir(path)
		// These parents are all inside an atomically created, still held target.
		parts := strings.Split(output.Target, "/")
		p := filepath.Join(root, parts[0])
		for _, part := range parts[1 : len(parts)-1] {
			next := filepath.Join(p, part)
			key := strings.ToLower(next)
			if _, ok := s.dirs[key]; !ok {
				h, ok := skillsOpenAt(s.dirs[strings.ToLower(p)], part, true, true)
				if !ok {
					return count, "create_failed"
				}
				s.dirs[key] = h
			}
			p = next
		}
		ph, ok := s.dirs[strings.ToLower(parent)]
		if !ok {
			return count, "create_failed"
		}
		h, ok := skillsOpenAt(ph, filepath.Base(path), false, true)
		if !ok {
			return count, "create_failed"
		}
		f := os.NewFile(uintptr(h), path)
		s.files = append(s.files, f)
		if hashBytes(output.body) != output.SHA {
			return count, "manifest"
		}
		n, e := f.Write(output.body)
		if e != nil || n != len(output.body) || f.Sync() != nil {
			return count, "create_failed"
		}
		// Read back through this same created object, without opening any old leaf.
		if _, e = f.Seek(0, io.SeekStart); e != nil {
			return count, "create_failed"
		}
		b, e := io.ReadAll(io.LimitReader(f, int64(len(output.body))+1))
		if e != nil || hashBytes(b) != output.SHA {
			return count, "create_failed"
		}
		count++
	}
	return count, ""
}
