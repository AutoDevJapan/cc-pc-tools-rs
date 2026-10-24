package main

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unicode/utf16"
	"unsafe"
)

const (
	fileFlagOpenReparsePoint = 0x00200000
	fileFlagBackupSemantics  = 0x02000000
	fileFlagSequentialScan   = 0x08000000
	fileListDirectory        = 0x00000001
	fileReadAttributes       = 0x00000080
)

// GetFileInformationByHandleEx returns FILE_FULL_DIR_INFO records: the offset
// of the next record, the name's length in bytes, then the name.
const (
	fileFullDirectoryInfo        = 14
	fileFullDirectoryRestartInfo = 15
	dirInfoNameLength            = 60
	dirInfoName                  = 68
)

var (
	kernel32                         = syscall.NewLazyDLL("kernel32.dll")
	procGetFinalPathNameByHandleW    = kernel32.NewProc("GetFinalPathNameByHandleW")
	procGetFileInformationByHandleEx = kernel32.NewProc("GetFileInformationByHandleEx")
)

// pinnedFile is one evidence file held open for reading with writes, deletes
// and renames denied to everyone else until the session closes.
type pinnedFile struct {
	file *os.File
	path string
	info syscall.ByHandleFileInformation
	size int64
}

// pinnedDir keeps the identity of a directory. Others may still create files
// in it, so only its identity and physical path are rechecked.
type pinnedDir struct {
	handle syscall.Handle
	path   string
	info   syscall.ByHandleFileInformation
}

// releasedFile is Git metadata that was classified, read and closed again, so
// the worktrees sharing the repository can keep updating it. The recheck
// reopens it and compares its identity, size, write time and digest.
type releasedFile struct {
	path string
	info syscall.ByHandleFileInformation
	sum  string
}

type session struct {
	env      environment
	files    []*pinnedFile
	dirs     []pinnedDir
	released []releasedFile
	absent   []string
	dirCache map[string]bool
}

func newSession(env environment) *session {
	return &session{env: env, dirCache: map[string]bool{}}
}

func (s *session) close() {
	for _, f := range s.files {
		f.file.Close()
	}
	for _, d := range s.dirs {
		syscall.CloseHandle(d.handle)
	}
}

// finalPath returns the handle's physical DOS path. Links and junctions in
// any ancestor, and 8.3 aliases, make it differ from the requested spelling.
func finalPath(h syscall.Handle) (string, bool) {
	buf := make([]uint16, 32768)
	n, _, _ := procGetFinalPathNameByHandleW.Call(uintptr(h), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), 0)
	if n == 0 || n >= uintptr(len(buf)) {
		return "", false
	}
	path := syscall.UTF16ToString(buf[:n])
	if strings.HasPrefix(path, `\\?\UNC\`) || !strings.HasPrefix(path, `\\?\`) {
		return "", false
	}
	path = path[4:]
	if len(path) < 3 || path[1] != ':' || path[2] != '\\' {
		return "", false
	}
	return path, true
}

func reparse(attributes uint32) bool {
	return attributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0
}

// lstatPlain reads the Windows attributes directly: recent Go releases do not
// report every reparse point (such as a junction) as a symlink.
func lstatPlain(path string) (os.FileInfo, bool) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, false
	}
	data, ok := info.Sys().(*syscall.Win32FileAttributeData)
	return info, ok && !reparse(data.FileAttributes)
}

func (s *session) plainDir(dir string) bool {
	key := strings.ToLower(dir)
	if ok, cached := s.dirCache[key]; cached {
		return ok
	}
	info, ok := lstatPlain(dir)
	ok = ok && info.IsDir()
	s.dirCache[key] = ok
	return ok
}

// plainAncestors checks every directory between the volume root and path.
func (s *session) plainAncestors(path string) bool {
	for dir := filepath.Dir(path); filepath.Dir(dir) != dir; dir = filepath.Dir(dir) {
		if !s.plainDir(dir) {
			return false
		}
	}
	return true
}

func plainFile(info syscall.ByHandleFileInformation) bool {
	return !reparse(info.FileAttributes) && info.FileAttributes&syscall.FILE_ATTRIBUTE_DIRECTORY == 0 && info.NumberOfLinks == 1
}

// open opens one regular, single-link file without following a reparse point
// and without reading it. The handle shares only reads.
func (s *session) open(path string, limit int64) (*pinnedFile, bool) {
	return s.openShared(path, limit, syscall.FILE_SHARE_READ)
}

// openObject opens a file of the object store the same way, but lets the
// worktrees sharing the repository delete or rename it meanwhile, as Git does
// when it repacks. Writes stay denied, and objects are checked by their names.
func (s *session) openObject(path string, limit int64) (*pinnedFile, bool) {
	return s.openShared(path, limit, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_DELETE)
}

func (s *session) openShared(path string, limit int64, share uint32) (*pinnedFile, bool) {
	if !s.plainAncestors(path) {
		return nil, false
	}
	if info, ok := lstatPlain(path); !ok || !info.Mode().IsRegular() {
		return nil, false
	}
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, false
	}
	h, err := syscall.CreateFile(name, syscall.GENERIC_READ, share, nil, syscall.OPEN_EXISTING, fileFlagOpenReparsePoint|fileFlagSequentialScan, 0)
	if err != nil {
		return nil, false
	}
	f := &pinnedFile{file: os.NewFile(uintptr(h), path), path: path}
	if syscall.GetFileInformationByHandle(h, &f.info) != nil || !plainFile(f.info) {
		f.file.Close()
		return nil, false
	}
	f.size = int64(f.info.FileSizeHigh)<<32 | int64(f.info.FileSizeLow)
	if actual, ok := finalPath(h); !ok || !strings.EqualFold(actual, path) || f.size < 1 || f.size > limit {
		f.file.Close()
		return nil, false
	}
	return f, true
}

// pin opens a file and keeps it open until the session closes.
func (s *session) pin(path string, limit int64) (*pinnedFile, bool) {
	f, ok := s.open(path, limit)
	if ok {
		s.files = append(s.files, f)
	}
	return f, ok
}

// release closes a file that was read once and keeps its identity and the
// digest of the bytes read for the recheck.
func (s *session) release(f *pinnedFile, sum string) {
	s.released = append(s.released, releasedFile{path: f.path, info: f.info, sum: sum})
	f.file.Close()
}

// absentFile records a path that must not exist now or at the recheck.
func (s *session) absentFile(path string) bool {
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		return false
	}
	s.absent = append(s.absent, path)
	return true
}

// read returns exactly the bytes behind the pinned handle and their digest.
// Callers parse these same bytes; nothing is reopened by name.
func (s *session) read(f *pinnedFile) ([]byte, string, bool) {
	if s.env.beforeRead != nil {
		s.env.beforeRead(f.path)
	}
	if _, err := f.file.Seek(0, io.SeekStart); err != nil {
		return nil, "", false
	}
	data, err := io.ReadAll(io.LimitReader(f.file, f.size+1))
	if err != nil || int64(len(data)) != f.size || !s.unchanged(f) {
		return nil, "", false
	}
	sum := sha256.Sum256(data)
	return data, hex.EncodeToString(sum[:]), true
}

// unchanged compares the handle's identity, size, write time, link count and
// attributes with the values at pin time; only the access time may differ.
func (s *session) unchanged(f *pinnedFile) bool {
	h := syscall.Handle(f.file.Fd())
	var now syscall.ByHandleFileInformation
	if syscall.GetFileInformationByHandle(h, &now) != nil || !plainFile(now) {
		return false
	}
	before := f.info
	before.LastAccessTime = now.LastAccessTime
	if before != now {
		return false
	}
	actual, ok := finalPath(h)
	info, plain := lstatPlain(f.path)
	return ok && strings.EqualFold(actual, f.path) && plain && info.Mode().IsRegular()
}

// pinDir opens a directory without following a reparse point and keeps it open
// until the session closes. Its entries are listed through the returned handle.
func (s *session) pinDir(path string) (pinnedDir, bool) {
	if !s.plainAncestors(path) || !s.plainDir(path) {
		return pinnedDir{}, false
	}
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return pinnedDir{}, false
	}
	h, err := syscall.CreateFile(name, fileListDirectory|fileReadAttributes, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE, nil, syscall.OPEN_EXISTING, fileFlagBackupSemantics|fileFlagOpenReparsePoint, 0)
	if err != nil {
		return pinnedDir{}, false
	}
	d := pinnedDir{handle: h, path: path}
	actual, ok := finalPath(h)
	if syscall.GetFileInformationByHandle(h, &d.info) != nil || reparse(d.info.FileAttributes) ||
		d.info.FileAttributes&syscall.FILE_ATTRIBUTE_DIRECTORY == 0 || !ok || !strings.EqualFold(actual, path) {
		syscall.CloseHandle(h)
		return pinnedDir{}, false
	}
	s.dirs = append(s.dirs, d)
	return d, true
}

// list returns the names of the pinned directory's entries, read through its
// handle, so a directory that takes the path after the pin is never listed.
// A directory of more than limit entries is refused.
func (d pinnedDir) list(limit int) ([]string, bool) {
	buf := make([]uint64, 8<<10) // the records start at 8-byte boundaries
	raw := unsafe.Slice((*byte)(unsafe.Pointer(&buf[0])), 8*len(buf))
	var names []string
	for class := uintptr(fileFullDirectoryRestartInfo); ; class = fileFullDirectoryInfo {
		r, _, err := procGetFileInformationByHandleEx.Call(uintptr(d.handle), class, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(raw)))
		if r == 0 {
			return names, err == syscall.ERROR_NO_MORE_FILES
		}
		for at := 0; ; {
			if at > len(raw)-dirInfoName {
				return nil, false
			}
			next := int(binary.LittleEndian.Uint32(raw[at:]))
			size := int(binary.LittleEndian.Uint32(raw[at+dirInfoNameLength:]))
			if size%2 != 0 || size > len(raw)-at-dirInfoName {
				return nil, false
			}
			units := make([]uint16, size/2)
			for i := range units {
				units[i] = binary.LittleEndian.Uint16(raw[at+dirInfoName+2*i:])
			}
			if name := string(utf16.Decode(units)); name != "." && name != ".." {
				if len(names) == limit {
					return nil, false
				}
				names = append(names, name)
			}
			if next == 0 {
				break
			}
			if next < dirInfoName {
				return nil, false
			}
			at += next
		}
	}
}

func dirUnchanged(d pinnedDir) bool {
	var now syscall.ByHandleFileInformation
	if syscall.GetFileInformationByHandle(d.handle, &now) != nil {
		return false
	}
	actual, ok := finalPath(d.handle)
	return ok && strings.EqualFold(actual, d.path) && now.VolumeSerialNumber == d.info.VolumeSerialNumber &&
		now.FileIndexHigh == d.info.FileIndexHigh && now.FileIndexLow == d.info.FileIndexLow &&
		!reparse(now.FileAttributes) && now.FileAttributes&syscall.FILE_ATTRIBUTE_DIRECTORY != 0
}

// recheck repeats every physical check with a fresh ancestor cache.
func (s *session) recheck() bool {
	s.dirCache = map[string]bool{}
	for _, f := range s.files {
		if !s.plainAncestors(f.path) || !s.unchanged(f) {
			return false
		}
	}
	for _, d := range s.dirs {
		if !s.plainAncestors(d.path) || !s.plainDir(d.path) || !dirUnchanged(d) {
			return false
		}
	}
	for _, r := range s.released {
		f, ok := s.open(r.path, maxMetadataBytes)
		if !ok {
			return false
		}
		_, sum, read := s.read(f)
		now := f.info
		f.file.Close()
		before := r.info
		before.LastAccessTime = now.LastAccessTime
		if !read || sum != r.sum || before != now {
			return false
		}
	}
	for _, path := range s.absent {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			return false
		}
	}
	return true
}
