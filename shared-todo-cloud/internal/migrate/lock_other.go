//go:build !windows

package migrate

import (
	"os"

	"golang.org/x/sys/unix"
)

func retryableRename(error) bool { return false }

func linked(info os.FileInfo) bool { return info.Mode()&os.ModeSymlink != 0 }
func singleLink(file *os.File) bool {
	var stat unix.Stat_t
	return unix.Fstat(int(file.Fd()), &stat) == nil && stat.Nlink == 1
}
func lockFile(f *os.File) error { return unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB) }
func unlockFile(f *os.File)     { _ = unix.Flock(int(f.Fd()), unix.LOCK_UN) }
