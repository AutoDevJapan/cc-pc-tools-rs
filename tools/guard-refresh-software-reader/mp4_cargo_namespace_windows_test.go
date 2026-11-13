//go:build windows

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

func TestSupervisedCargoDirectoryChild(t *testing.T) {
	mode := os.Getenv("MP4_SUPERVISOR_TEST_CHILD")
	if mode == "" {
		return
	}
	if mode == "timeout" {
		time.Sleep(20 * time.Second)
	}
	if mode == "crash" {
		os.Exit(7)
	}
	fmt.Fprintln(os.Stdout, "owned-child")
	os.Exit(0)
}

func TestSupervisedCargoDirectoryReclaimsNormalKilledCrashAndEarlyStop(t *testing.T) {
	for _, mode := range []string{"normal", "timeout", "crash", "not_started"} {
		t.Run(mode, func(t *testing.T) {
			root := physicalSoftwareFixture(t)
			build := softwareBuildPlan{Target: filepath.Join(root, "build")}
			parent, e := prepareSupervisedCargoDirectory(build)
			if e != nil {
				t.Fatal(e)
			}
			drive := parent.cargoDrive
			defer parent.Close()
			result := askChildResult{}
			if mode != "not_started" {
				t.Setenv("MP4_SUPERVISOR_TEST_CHILD", mode)
				budget := 2 * time.Second
				if mode == "timeout" {
					budget = 200 * time.Millisecond
				}
				ctx, cancel := context.WithTimeout(context.Background(), budget)
				result = askOwnedChild(ctx, os.Args[0], "-test.run=^TestSupervisedCargoDirectoryChild$")
				cancel()
				defer clear(result.Raw)
				if !result.Started || !result.Reaped || mode == "timeout" && !result.TimedOut || mode == "crash" && result.Exit != 7 {
					t.Fatal("owned child lifecycle mismatch", result.Exit, result.Reaped)
				}
			}
			if !parent.cargoDirectoryUnchanged() {
				t.Fatal("mapping vanished before parent reconciliation")
			}
			if ok, reason := finishSupervisedCargoDirectory(parent, result); !ok {
				t.Fatal(reason)
			}
			if _, e := cargoDriveMapping(drive); e != syscall.ERROR_FILE_NOT_FOUND {
				t.Fatal("parent mapping remained after collected child", e)
			}
		})
	}
}

func TestSupervisedCargoDirectoryUnknownAndForeignNeverRemoved(t *testing.T) {
	for _, mode := range []string{"unknown_child", "foreign_mapping"} {
		t.Run(mode, func(t *testing.T) {
			root := physicalSoftwareFixture(t)
			parent, e := prepareSupervisedCargoDirectory(softwareBuildPlan{Target: filepath.Join(root, "build")})
			if e != nil {
				t.Fatal(e)
			}
			drive, target := parent.cargoDrive, parent.cargoDriveTarget
			defer parent.Close()
			if mode == "unknown_child" {
				if ok, _ := finishSupervisedCargoDirectory(parent, askChildResult{Started: true}); ok || !parent.cargoDirectoryUnchanged() {
					t.Fatal("uncollected child mapping was removed")
				}
				return
			}
			foreign := filepath.Join(root, "foreign")
			if e := os.Mkdir(foreign, 0700); e != nil {
				t.Fatal(e)
			}
			device, _ := syscall.UTF16PtrFromString(drive)
			foreignTarget := "\\??\\" + foreign
			native, _ := syscall.UTF16PtrFromString(foreignTarget)
			ok, _, e := mp4DefineDosDevice.Call(1|8, uintptr(unsafe.Pointer(device)), uintptr(unsafe.Pointer(native)))
			if ok == 0 {
				t.Fatal(e)
			}
			if accepted, _ := finishSupervisedCargoDirectory(parent, askChildResult{Started: true, Reaped: true}); accepted {
				t.Fatal("foreign mapping cleanup reported success")
			}
			if mapping, _ := cargoDriveMapping(drive); mapping != foreignTarget {
				t.Fatal("foreign mapping changed")
			}
			// Remove only this test's two definitions after observing preservation.
			mp4DefineDosDevice.Call(1|2|4|8, uintptr(unsafe.Pointer(device)), uintptr(unsafe.Pointer(native)))
			native, _ = syscall.UTF16PtrFromString(target)
			mp4DefineDosDevice.Call(1|2|4|8, uintptr(unsafe.Pointer(device)), uintptr(unsafe.Pointer(native)))
			if _, e := cargoDriveMapping(drive); e != syscall.ERROR_FILE_NOT_FOUND {
				t.Fatal("owned fixture mapping cleanup failed", e)
			}
		})
	}
}

func TestSupervisedCargoDirectoryRejectsConfigurationInsertedBeforeSeal(t *testing.T) {
	root := physicalSoftwareFixture(t)
	build := softwareBuildPlan{Target: filepath.Join(root, "build")}
	drive := ""
	result, e := prepareSupervisedCargoDirectoryWith(build, func(s *softwareBuildSnapshot) error {
		drive = s.cargoDrive
		config := filepath.Join(s.Root, "cwd", ".cargo")
		if e := os.Mkdir(config, 0700); e != nil {
			return e
		}
		if e := os.WriteFile(filepath.Join(config, "config.toml"), []byte("[env]\nUNPROVED=\"1\"\n"), 0600); e != nil {
			return e
		}
		return s.sealDirectories()
	})
	if e == nil || result != nil || drive == "" {
		t.Fatal("pre-seal Cargo configuration admitted")
	}
	if _, e := cargoDriveMapping(drive); e != syscall.ERROR_FILE_NOT_FOUND {
		t.Fatal("failed preparation mapping remained", e)
	}
}

func TestCargoWorkerBorrowsParentNamespaceUntilCollected(t *testing.T) {
	if _, e := os.Stat(mp4PublicCargoRegistry); e != nil {
		t.Skip("explicit workstation public registry integration")
	}
	root := physicalSoftwareFixture(t)
	build := softwareBuildPlan{Target: filepath.Join(root, "build")}
	parent, e := prepareSupervisedCargoDirectory(build)
	if e != nil {
		t.Fatal(e)
	}
	defer parent.Close()
	if os.Mkdir(build.Target, 0700) != nil {
		t.Fatal("owned build namespace")
	}
	admitted, ok := admitSupervisedCargoDirectory(build, parent.cargoDrive)
	if !ok {
		t.Fatal("exact supervised namespace rejected")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ctx = context.WithValue(ctx, mp4SupervisedCargoDirectory{}, admitted)
	ctx, config, e := prepareMP4CargoContext(ctx, build)
	if e != nil {
		t.Fatal(e)
	}
	defer config.Close()
	project := filepath.Join(root, "project")
	if os.Mkdir(project, 0700) != nil || os.Mkdir(filepath.Join(project, "src"), 0700) != nil {
		t.Fatal("owned source directories")
	}
	if os.WriteFile(filepath.Join(project, "Cargo.toml"), []byte("[package]\nname=\"owned-borrowed-cwd\"\nversion=\"0.1.0\"\nedition=\"2021\"\n[workspace]\n"), 0600) != nil || os.WriteFile(filepath.Join(project, "src", "main.rs"), []byte("fn main(){}"), 0600) != nil {
		t.Fatal("owned source bytes")
	}
	result := askOwnedChildAt(ctx, ctx.Value(mp4CargoDirectory{}).(string), snapshotTestTool(t, "cargo"), "build", "--offline", "--manifest-path", filepath.Join(project, "Cargo.toml"), "--target-dir", filepath.Join(root, "target"))
	defer clear(result.Raw)
	if result.Exit != 0 || !result.Reaped || !config.cargoConfigurationUnchanged() {
		t.Fatal("supervised normal Cargo input flow failed", result.Exit)
	}
	if e := config.Close(); e != nil || !parent.cargoDirectoryUnchanged() {
		t.Fatal("worker closed parent-owned mapping", e)
	}
	if ok, reason := finishSupervisedCargoDirectory(parent, result); !ok {
		t.Fatal(reason)
	}
	if _, e := cargoDriveMapping(admitted.cargoDrive); e != syscall.ERROR_FILE_NOT_FOUND {
		t.Fatal("mapping remained after actual Cargo collection", e)
	}
}

func TestCargoSupervisorRestoresDirectoryWithoutAutoInheritedControl(t *testing.T) {
	root := physicalSoftwareFixture(t)
	var s *softwareBuildSnapshot
	var descriptor uintptr
	var identity syscall.Handle
	build := softwareBuildPlan{Target: filepath.Join(root, "build")}
	s, e := prepareSupervisedCargoDirectoryWith(build, func(s *softwareBuildSnapshot) error {
		p, _ := syscall.UTF16PtrFromString(s.Root)
		h, e := syscall.CreateFile(p, 0x00020000|0x00040000, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE, nil, syscall.OPEN_EXISTING, 0x02000000|0x00200000, 0)
		if e != nil {
			return e
		}
		identity = h
		code, _, _ := updateGetSecurity.Call(uintptr(h), 1, 4, 0, 0, 0, 0, uintptr(unsafe.Pointer(&descriptor)))
		if code != 0 {
			return fmt.Errorf("owned descriptor capture")
		}
		ok, _, _ := updateAdvapi.NewProc("SetSecurityDescriptorControl").Call(descriptor, 0x0400, 0)
		if ok == 0 {
			return fmt.Errorf("owned control")
		}
		code, _, _ = updateNTSetSecurity.Call(uintptr(h), 4, descriptor)
		if code != 0 {
			return fmt.Errorf("owned control set")
		}
		return s.sealDirectories()
	})
	defer func() {
		if identity != 0 {
			updateNTSetSecurity.Call(uintptr(identity), 4, descriptor)
			syscall.CloseHandle(identity)
		}
		if descriptor != 0 {
			updateLocalFree.Call(descriptor)
		}
	}()
	if e != nil {
		t.Fatal(e)
	}
	if e := s.Close(); e != nil {
		t.Fatal("exact non-auto inherited directory restore", e)
	}
}

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
