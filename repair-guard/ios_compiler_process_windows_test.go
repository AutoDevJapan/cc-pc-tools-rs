package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

func init() {
	// The test executable stands in for the same production image's fixed
	// self-helper entry, so the ordinary command path exercises isolation too.
	if len(os.Args) == 2 && os.Args[1] == iosCompilerUserConfigFlag {
		os.Exit(runGuardCLIWithRefresh(os.Args[1:], os.Stdout, os.Stderr, nil, nil))
	}
}

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

func TestIOSCompilerUserConfigIsolationChild(t *testing.T) {
	if len(os.Args) < 2 || os.Args[len(os.Args)-2] != "ios4431-isolation" {
		t.Skip("owned subprocess only")
	}
	if os.Args[len(os.Args)-1] == "hang" {
		time.Sleep(time.Minute)
		os.Exit(3)
	}
	os.Exit(runIOSCompilerUserConfigCLI([]string{iosCompilerUserConfigFlag}, os.Stdout, iosCompilerRoamingData))
}

func TestIOSCompilerIsolatedUserConfigNormalAndTimeout(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"normal", "hang"} {
		t.Run(mode, func(t *testing.T) {
			timeout := 3 * time.Second
			if mode == "hang" {
				timeout = 150 * time.Millisecond
			}
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			var before, after uint32
			started := time.Now()
			path, err := iosCompilerRoamingCommand(ctx, exe, []string{"-test.run=^TestIOSCompilerUserConfigIsolationChild$", "--", "ios4431-isolation", mode}, func(a, b uint32) { before, after = a, b })
			if mode == "normal" {
				want, nativeErr := iosCompilerRoamingData()
				if err != nil || nativeErr != nil || !strings.EqualFold(path, want) || after != 0 {
					t.Fatal("native helper did not preserve the current user", err, nativeErr, after)
				}
			} else if err == nil || path != "" || ctx.Err() == nil || time.Since(started) > 2*time.Second || before < 1 || after != 0 {
				t.Fatal("stalled helper escaped deadline or cleanup", err, before, after, time.Since(started))
			}
		})
	}
}

func TestIOSCompilerUserConfigProtocolRejectsCallerOverrides(t *testing.T) {
	for _, args := range [][]string{{}, {iosCompilerUserConfigFlag, "C:/caller"}, {iosCompilerUserConfigFlag + "=C:/caller"}, {iosCompilerUserConfigFlag, "--guard-path", "C:/unopened"}} {
		calls := 0
		var out bytes.Buffer
		if code := runIOSCompilerUserConfigCLI(args, &out, func() (string, error) { calls++; return "C:/native", nil }); code != 2 || calls != 0 || out.Len() != 0 {
			t.Fatal("caller-selected helper admitted", code, calls, out.Len())
		}
	}
	for _, raw := range []string{`{}`, `{"domain":"ios-compiler-current-user-config-v1","path":"relative"}`, `{"domain":"wrong","path":"C:/native"}`, `{"domain":"ios-compiler-current-user-config-v1","path":"C:/a","path":"C:/b"}`, `{"domain":"ios-compiler-current-user-config-v1","Path":"C:/a"}`, `{"domain":"ios-compiler-current-user-config-v1","path":"C:/a","grant":true}`} {
		if path, err := iosCompilerReadUserConfig([]byte(raw)); err == nil || path != "" {
			t.Fatal("unverified helper output admitted")
		}
	}
	var out bytes.Buffer
	if runGuardCLIWithRefresh([]string{iosCompilerUserConfigFlag, "--guard-path", "C:/unopened"}, &out, &out, nil, nil) != 2 || out.Len() != 0 {
		t.Fatal("real dispatch bypassed helper argument check")
	}
	if runIOSCompilerUserConfigCLI([]string{iosCompilerUserConfigFlag}, iosCompilerShortWriter{}, func() (string, error) { return "C:/native", nil }) != 1 {
		t.Fatal("short helper output admitted")
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

func TestIOSCompilerCancellationDuringEnvironmentDoesNotStartChild(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	environments, observations := 0, 0
	cmd := filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe")
	raw, err := iosCompilerCommandWithEnvironment(ctx, cmd, []string{"/d", "/c", "exit 0"}, func(uint32, uint32) {
		observations++
	}, func() ([]string, error) {
		environments++
		cancel()
		return official3894Env(), nil
	})
	defer clear(raw)
	if err == nil || len(raw) != 0 || environments != 1 || observations != 0 {
		t.Fatalf("expired preparation started child: err=%v bytes=%d environments=%d observations=%d", err, len(raw), environments, observations)
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
