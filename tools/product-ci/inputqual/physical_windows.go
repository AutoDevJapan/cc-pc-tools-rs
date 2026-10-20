package inputqual

import (
	"os"
	"os/exec"
	"syscall"
	"time"
)

func isReparse(info os.FileInfo) bool {
	v, ok := info.Sys().(*syscall.Win32FileAttributeData)
	return !ok || v.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0
}
func singleLink(f *os.File) bool {
	var info syscall.ByHandleFileInformation
	return syscall.GetFileInformationByHandle(syscall.Handle(f.Fd()), &info) == nil && info.NumberOfLinks == 1 && info.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT == 0
}
func prepareCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	cmd.WaitDelay = time.Second
}
