//go:build windows

package main

import (
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"unsafe"
)

const defaultQuarantinePath = `C:\Users\makhmeto\Downloads\削除希望`

// Destination selection never grants access to a source. Quarantine calls this
// only after checking the original owned receipt and measuring its source.
func ownedQuarantineParent(source, configured string) string {
	return ownedQuarantineParentWithType(source, configured, quarantineDriveType)
}

func ownedQuarantineParentWithType(source, configured string, driveType func(string) uint32) string {
	if !same(configured, defaultQuarantinePath) {
		return configured
	}
	volume := filepath.VolumeName(source)
	if len(volume) != 2 || volume[1] != ':' || strings.EqualFold(volume, filepath.VolumeName(configured)) {
		return configured
	}
	switch driveType(volume + `\`) {
	case 2, 3, 6: // DRIVE_REMOVABLE, DRIVE_FIXED, DRIVE_RAMDISK.
		return filepath.Join(volume+`\`, "削除希望")
	default:
		return configured
	}
}

var quarantineGetDriveType = syscall.NewLazyDLL("kernel32.dll").NewProc("GetDriveTypeW")

func quarantineDriveType(root string) uint32 {
	p, err := syscall.UTF16PtrFromString(root)
	if err != nil || quarantineGetDriveType.Find() != nil {
		return 0
	}
	kind, _, _ := quarantineGetDriveType.Call(uintptr(unsafe.Pointer(p)))
	runtime.KeepAlive(p)
	return uint32(kind)
}
