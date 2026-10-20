//go:build windows

package main

import (
	"os"
	"path/filepath"
	"syscall"
)

func retiredIdentityMatches(path string, volume uint32, id uint64) bool {
	for cur := path; ; cur = filepath.Dir(cur) {
		fi, err := os.Lstat(cur)
		if err != nil || fi.Sys().(*syscall.Win32FileAttributeData).FileAttributes&0x400 != 0 {
			return false
		}
		if filepath.Dir(cur) == cur {
			break
		}
	}
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return false
	}
	h, err := syscall.CreateFile(p, 0x80, 7, nil, 3, 0x02000000|0x00200000, 0)
	if err != nil {
		return false
	}
	defer syscall.CloseHandle(h)
	var v syscall.ByHandleFileInformation
	if syscall.GetFileInformationByHandle(h, &v) != nil {
		return false
	}
	return v.FileAttributes&0x400 == 0 && v.FileAttributes&0x10 != 0 && v.VolumeSerialNumber == volume && uint64(v.FileIndexHigh)<<32|uint64(v.FileIndexLow) == id
}
