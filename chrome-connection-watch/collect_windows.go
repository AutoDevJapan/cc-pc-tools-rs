package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"unsafe"
)

func hidden(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}

func registryManifest() string {
	adv := syscall.NewLazyDLL("advapi32.dll")
	open := adv.NewProc("RegOpenKeyExW")
	query := adv.NewProc("RegQueryValueExW")
	closeKey := adv.NewProc("RegCloseKey")
	keyName, _ := syscall.UTF16PtrFromString(`Software\Google\Chrome\NativeMessagingHosts\com.openai.codexextension`)
	var handle uintptr
	r, _, _ := open.Call(0x80000001, uintptr(unsafe.Pointer(keyName)), 0, 0x20019, uintptr(unsafe.Pointer(&handle)))
	if r != 0 {
		return ""
	}
	defer closeKey.Call(handle)
	var kind, size uint32
	r, _, _ = query.Call(handle, 0, 0, uintptr(unsafe.Pointer(&kind)), 0, uintptr(unsafe.Pointer(&size)))
	if r != 0 || kind != 1 || size == 0 || size > 65536 || size%2 != 0 {
		return ""
	}
	buf := make([]uint16, size/2)
	r, _, _ = query.Call(handle, 0, 0, uintptr(unsafe.Pointer(&kind)), uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)))
	if r != 0 || kind != 1 {
		return ""
	}
	return syscall.UTF16ToString(buf)
}

func collect(r *report) {
	checkExtension(filepath.Join(os.Getenv("LOCALAPPDATA"), "Google", "Chrome", "User Data"), r)
	checkManifest(registryManifest(), r)
	config, err := os.ReadFile(filepath.Join(os.Getenv("USERPROFILE"), ".codex", "config.toml"))
	if err != nil {
		r.Problems = append(r.Problems, "codex_config_unreadable")
		return
	}
	var pipe string
	r.DesktopPipeConfigured, pipe = desktopReference(config)
	if !r.DesktopPipeConfigured {
		return
	}
	entries, err := os.ReadDir(`\\.\pipe\`)
	if err != nil {
		r.Problems = append(r.Problems, "native_pipe_inventory_failed")
		return
	}
	for _, e := range entries {
		if `\\.\pipe\`+e.Name() == pipe {
			r.DesktopPipePresent = true
			break
		}
	}
	if !r.DesktopPipePresent {
		r.Problems = append(r.Problems, "desktop_native_reference_stale")
	}
}
