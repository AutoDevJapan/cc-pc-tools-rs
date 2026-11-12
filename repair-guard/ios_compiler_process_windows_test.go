package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

func TestIOSCompilerNativeRoamingFolderChild(t *testing.T) {
	if len(os.Args) == 0 || os.Args[len(os.Args)-1] != "ios4431-config-child" {
		t.Skip("owned subprocess only")
	}
	id := syscall.GUID{Data1: 0x3eb685db, Data2: 0x65f9, Data3: 0x4cf6, Data4: [8]byte{0xa0, 0x3a, 0xe3, 0xef, 0x65, 0x72, 0x9f, 0x3d}}
	var path *uint16
	hr, _, _ := syscall.NewLazyDLL("shell32.dll").NewProc("SHGetKnownFolderPath").Call(uintptr(unsafe.Pointer(&id)), 0, 0, uintptr(unsafe.Pointer(&path)))
	if path != nil {
		defer syscall.NewLazyDLL("ole32.dll").NewProc("CoTaskMemFree").Call(uintptr(unsafe.Pointer(path)))
	}
	if hr != 0 || path == nil {
		t.Fatal("current-user known folder unavailable")
	}
	var chars []uint16
	for i := 0; i < 32768; i++ {
		ch := *(*uint16)(unsafe.Add(unsafe.Pointer(path), i*2))
		if ch == 0 {
			break
		}
		chars = append(chars, ch)
	}
	if len(chars) == 0 || len(chars) == 32768 || !strings.EqualFold(os.Getenv("APPDATA"), syscall.UTF16ToString(chars)) {
		t.Fatal("metadata child lost the current user's normal configuration directory")
	}
}

func TestIOSCompilerNativeCommandKeepsUserConfigWithoutAppData(t *testing.T) {
	t.Setenv("APPDATA", "")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	raw, err := iosCompilerCommand(ctx, exe, "-test.run=^TestIOSCompilerNativeRoamingFolderChild$", "--", "ios4431-config-child")
	defer clear(raw)
	if err != nil || !strings.Contains(string(raw), "PASS") {
		t.Fatal("missing APPDATA changed metadata child's normal config", err, string(raw))
	}
}

func TestIOSCompilerEnvironmentKeepsExistingAndUsesOnlyNativeMissingLocation(t *testing.T) {
	for _, mode := range []string{"present", "empty", "missing", "native_error", "relative"} {
		t.Run(mode, func(t *testing.T) {
			nativeCalls := 0
			lookup := func(key string) (string, bool) {
				if strings.Contains(strings.ToUpper(key), "TOKEN") || strings.Contains(strings.ToUpper(key), "KEY") || key == "GH_CONFIG_DIR" {
					t.Fatal("credential or alternate account override inspected")
				}
				if key == "APPDATA" {
					if mode == "present" {
						return "C:/normal-roaming", true
					}
					return "", mode == "empty"
				}
				if key == "CC_JOB_ID" {
					return "", false
				}
				return "normal", true
			}
			env, err := iosCompilerEnvironment(lookup, func() (string, error) {
				nativeCalls++
				if mode == "native_error" {
					return "", errIOSCompiler
				}
				if mode == "relative" {
					return "relative", nil
				}
				return "C:/native-roaming", nil
			})
			if mode == "native_error" || mode == "relative" {
				if err == nil || env != nil || nativeCalls != 1 {
					t.Fatal("unknown native location admitted", err, nativeCalls)
				}
				return
			}
			want := "APPDATA=C:/native-roaming"
			if mode == "present" {
				want = "APPDATA=C:/normal-roaming"
				if nativeCalls != 0 {
					t.Fatal("existing normal account location replaced")
				}
			} else if nativeCalls != 1 {
				t.Fatal("native location not obtained exactly once")
			}
			count := 0
			for _, field := range env {
				if strings.HasPrefix(field, "APPDATA=") {
					count++
					if field != want {
						t.Fatal("wrong appdata binding")
					}
				}
				if strings.HasPrefix(field, "CC_JOB_ID=") {
					t.Fatal("fabricated native scheduler job")
				}
			}
			if err != nil || count != 1 {
				t.Fatal("missing/duplicate normal config location", err, count)
			}
		})
	}
}

func TestIOSCompilerNativeCommandContainsAndReapsGateChildren(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// The command launches a child process for the timeout case. Success/error
	// returns are gated on the exact owned Windows job's active count reaching 0.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe")
	raw, err := iosCompilerCommand(ctx, cmd, "/d", "/c", "echo metadata-ok")
	if err != nil || strings.TrimSpace(string(raw)) != "metadata-ok" {
		t.Fatal("normal contained metadata command", err)
	}
	clear(raw)
	ctx2, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	var before, after uint32
	raw, err = iosCompilerCommandObserved(ctx2, cmd, []string{"/d", "/c", "ping -n 20 127.0.0.1 >nul"}, func(a, b uint32) { before, after = a, b })
	if err == nil || len(raw) != 0 || ctx2.Err() == nil {
		t.Fatal("timed out tree accepted", err)
	}
	if before < 2 || after != 0 {
		t.Fatal("tree fixture did not prove child creation and full drain", before, after)
	}
	if raw, err := iosCompilerCommand(context.Background(), exe+".missing"); err == nil || len(raw) != 0 {
		t.Fatal("failed launch accepted")
	}
}
