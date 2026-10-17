package sourcepacket

import (
	"os"
	"syscall"
)

func openRootDirectory(path string) (*os.File, error) {
	fd, e := syscall.Open(path, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if e != nil {
		return nil, e
	}
	return os.NewFile(uintptr(fd), path), nil
}
func openDirectoryAt(parent *os.File, name, path string) (*os.File, error) {
	fd, e := syscall.Openat(int(parent.Fd()), name, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if e != nil {
		return nil, e
	}
	return os.NewFile(uintptr(fd), path), nil
}
func openRegularAt(parent *os.File, name, path string) (*os.File, error) {
	fd, e := syscall.Openat(int(parent.Fd()), name, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC|syscall.O_NONBLOCK, 0)
	if e != nil {
		return nil, e
	}
	var info syscall.Stat_t
	if syscall.Fstat(fd, &info) != nil || info.Mode&syscall.S_IFMT != syscall.S_IFREG || info.Nlink != 1 {
		syscall.Close(fd)
		return nil, fixedErr()
	}
	return os.NewFile(uintptr(fd), path), nil
}
