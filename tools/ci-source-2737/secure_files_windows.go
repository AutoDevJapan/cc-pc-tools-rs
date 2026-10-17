package sourcepacket

import (
	"os"
	"syscall"
)

// FILE_SHARE_READ only: an opened ancestor cannot be renamed or changed into a
// junction/reparse point while its child is opened and read. No reparse target
// is resolved, including for the volume-to-worktree ancestor chain.
func openWindowsPinned(path string, directory bool) (*os.File, error) {
	name, e := syscall.UTF16PtrFromString(path)
	if e != nil {
		return nil, e
	}
	access := uint32(syscall.GENERIC_READ)
	if directory {
		access = syscall.GENERIC_READ
	}
	h, e := syscall.CreateFile(name, access, syscall.FILE_SHARE_READ, nil, syscall.OPEN_EXISTING, syscall.FILE_FLAG_BACKUP_SEMANTICS|syscall.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if e != nil {
		return nil, e
	}
	var info syscall.ByHandleFileInformation
	if syscall.GetFileInformationByHandle(h, &info) != nil || info.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0 || (info.FileAttributes&syscall.FILE_ATTRIBUTE_DIRECTORY != 0) != directory || (!directory && info.NumberOfLinks != 1) {
		syscall.CloseHandle(h)
		return nil, fixedErr()
	}
	return os.NewFile(uintptr(h), path), nil
}
func openRootDirectory(path string) (*os.File, error) { return openWindowsPinned(path, true) }
func openDirectoryAt(_ *os.File, _ string, path string) (*os.File, error) {
	return openWindowsPinned(path, true)
}
func openRegularAt(_ *os.File, _ string, path string) (*os.File, error) {
	return openWindowsPinned(path, false)
}
