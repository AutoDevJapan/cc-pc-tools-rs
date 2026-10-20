//go:build windows && amd64

package windowsrunner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkerMutablePathsStayOutsidePrivateSDKAncestors(t *testing.T) {
	service := fixedEnvironmentValues(testSeed().Plan, `C:\public\sccache.exe`)
	before := strings.Join(service, "\n")
	worker := runnerEnvironment(append(append([]string(nil), service...), "hOmE=old", "USERPROFILE=old", "cargo_target_dir=old"))
	if strings.Join(service, "\n") != before {
		t.Fatal("worker selection mutated SYSTEM proof environment")
	}
	wanted := map[string]string{"TMP": "_temp", "TEMP": "_temp", "USERPROFILE": "_profile", "HOME": "_profile", "APPDATA": `_profile\AppData\Roaming`, "LOCALAPPDATA": `_profile\AppData\Local`, "CARGO_TARGET_DIR": "_target"}
	counts := map[string]int{}
	for _, entry := range worker {
		key, value, _ := strings.Cut(entry, "=")
		key = strings.ToUpper(key)
		if key == "SYSTEMTEMP" {
			t.Fatal("SYSTEM-only temp override entered worker")
		}
		if suffix, ok := wanted[key]; ok {
			counts[key]++
			if value != filepath.Join(runnerRoot, suffix) {
				t.Fatalf("%s still crosses a private SDK ancestor", key)
			}
		}
	}
	for key := range wanted {
		if counts[key] != 1 {
			t.Fatal("worker path absent or duplicated", key)
		}
	}
}

func TestWorkerDirectoriesAreFreshAndContainProfilePaths(t *testing.T) {
	root := t.TempDir()
	if err := prepareWorkerDirectories(root); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"_temp", "_target", `_profile\AppData\Roaming`, `_profile\AppData\Local`} {
		if info, err := os.Lstat(filepath.Join(root, name)); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			t.Fatal("worker directory missing or linked", name)
		}
	}
	if err := prepareWorkerDirectories(root); err == nil {
		t.Fatal("stale worker directories accepted")
	}
}

func TestGuestSymlinkAuthorizationRejectsHostAndWorkerIdentities(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if fixedGuestService(exe, true) || fixedGuestService(helperPath, false) || !fixedGuestService(helperPath, true) {
		t.Fatal("guest-only prerequisite identity boundary changed")
	}
}

// No host registry access: execute/readback are private unit seams. Positive
// control requires the real fixed command and a measured successful drain.
func TestGuestSymlinkSetupRequiresProcessDrainAndDWORDReadback(t *testing.T) {
	for _, mode := range []string{"ok", "exit", "error", "drain", "read-error", "wrong-value"} {
		t.Run(mode, func(t *testing.T) {
			runs, reads := 0, 0
			run := func(_ context.Context, exe, line, dir string, _ []string, log string) processResult {
				runs++
				if exe != `C:\Windows\System32\reg.exe` || dir != runnerRoot || log != "" || !strings.Contains(line, `HKLM\`+developerKey) || !strings.HasSuffix(line, `/v AllowDevelopmentWithoutDevLicense /t REG_DWORD /d 1 /f /reg:64`) {
					t.Error("prerequisite command escaped fixed guest scope")
				}
				r := processResult{drained: true}
				if mode == "exit" {
					r.exit = 1
				}
				if mode == "error" {
					r.err = errors.New("synthetic process failure")
				}
				if mode == "drain" {
					r.drained = false
				}
				return r
			}
			read := func() (uint32, error) {
				reads++
				if mode == "read-error" {
					return 0, errors.New("synthetic read failure")
				}
				if mode == "wrong-value" {
					return 0, nil
				}
				return 1, nil
			}
			err := applyGuestSymlinkSupport(context.Background(), nil, run, read)
			if (err == nil) != (mode == "ok") || runs != 1 {
				t.Fatal("setup/readback failure accepted or process not executed")
			}
			wantRead := 1
			if mode == "exit" || mode == "error" || mode == "drain" {
				wantRead = 0
			}
			if reads != wantRead {
				t.Fatal("readback preceded measured process completion")
			}
		})
	}
}
