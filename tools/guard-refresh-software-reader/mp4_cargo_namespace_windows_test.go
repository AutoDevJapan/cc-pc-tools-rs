//go:build windows

package main

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

func TestOwnedDriveCargoDoesNotDiscoverPhysicalAncestorConfiguration(t *testing.T) {
	root := physicalSoftwareFixture(t)
	for _, name := range []string{"cwd", ".cargo", "project", "project/src", "home"} {
		if e := os.Mkdir(filepath.Join(root, name), 0700); e != nil {
			t.Fatal(e)
		}
	}
	for path, raw := range map[string]string{
		".cargo/config.toml":  "[env]\nOWNED_PHYSICAL_ANCESTOR=\"injected\"\n",
		"project/Cargo.toml":  "[package]\nname=\"owned-root-view\"\nversion=\"0.1.0\"\nedition=\"2021\"\n[workspace]\n",
		"project/src/main.rs": "fn main(){assert!(option_env!(\"OWNED_PHYSICAL_ANCESTOR\").is_none());}\n",
	} {
		if e := os.WriteFile(filepath.Join(root, path), []byte(raw), 0600); e != nil {
			t.Fatal(e)
		}
	}
	kernel := syscall.NewLazyDLL("kernel32.dll")
	query := kernel.NewProc("QueryDosDeviceW")
	define := kernel.NewProc("DefineDosDeviceW")
	var name *uint16
	var drive string
	for letter := 'Z'; letter >= 'P'; letter-- {
		drive = string(letter) + ":"
		name, _ = syscall.UTF16PtrFromString(drive)
		buf := make([]uint16, 1024)
		n, _, e := query.Call(uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
		if n == 0 && e == syscall.ERROR_FILE_NOT_FOUND {
			break
		}
		drive = ""
	}
	if drive == "" {
		t.Fatal("no unused owned drive view")
	}
	target, _ := syscall.UTF16PtrFromString("\\??\\" + filepath.Join(root, "cwd"))
	ok, _, e := define.Call(1|8, uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(target)))
	if ok == 0 {
		t.Fatal("owned drive define", e)
	}
	defer func() {
		ok, _, e := define.Call(1|2|4|8, uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(target)))
		if ok == 0 {
			t.Error("owned drive cleanup", e)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	settings := mp4CompilerEnvironment(filepath.Join(root, "home"))
	settings["RUSTC"] = snapshotTestTool(t, "rustc")
	settings["RUSTC_WRAPPER"] = ""
	ctx = context.WithValue(ctx, askChildEnvironment{}, settings)
	ctx = context.WithValue(ctx, askChildExactEnvironment{}, true)
	result := askOwnedChildAt(ctx, drive+"\\", snapshotTestTool(t, "cargo"), "build", "--offline", "--manifest-path", filepath.Join(root, "project", "Cargo.toml"), "--target-dir", filepath.Join(root, "target"))
	defer clear(result.Raw)
	if result.Exit != 0 || !result.Reaped {
		t.Fatal("owned root Cargo build", result.Exit)
	}
	ran := askOwnedChild(ctx, filepath.Join(root, "target", "debug", "owned-root-view.exe"))
	defer clear(ran.Raw)
	if ran.Exit != 0 || !ran.Reaped {
		t.Fatal("Cargo followed physical cwd ancestors", ran.Exit)
	}
}
