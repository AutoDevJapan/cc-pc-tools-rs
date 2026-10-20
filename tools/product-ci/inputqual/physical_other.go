//go:build !windows

package inputqual

import (
	"os"
	"os/exec"
	"syscall"
	"time"
)

func isReparse(info os.FileInfo) bool { return false }
func singleLink(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	s, ok := info.Sys().(*syscall.Stat_t)
	return ok && s.Nlink == 1
}
func prepareCommand(cmd *exec.Cmd) { cmd.WaitDelay = time.Second }
