//go:build windows && amd64

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const rustupDistribution = "https://static.rust-lang.org/rustup/dist/x86_64-pc-windows-msvc/rustup-init.exe"

func publicDistribution(url string, limit int64) ([]byte, error) {
	// Fixed public distribution hosts only, with no proxy, cookie jar or auth.
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	req, e := http.NewRequestWithContext(ctx, "GET", url, nil)
	if e != nil {
		return nil, e
	}
	c := http.Client{Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) > 5 || r.URL.Scheme != "https" || (r.URL.Host != "github.com" && r.URL.Host != "release-assets.githubusercontent.com" && r.URL.Host != "static.rust-lang.org") {
			return errors.New("public distribution redirect rejected")
		}
		return nil
	}}
	resp, e := c.Do(req)
	if e != nil {
		return nil, errors.New("official public distribution unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("official distribution HTTP %d", resp.StatusCode)
	}
	return boundedRead(resp.Body, limit)
}

func prepareRuntimeSDK(p Plan) error {
	for _, sub := range []string{"sdk", "cargo/bin", "home/AppData/Local", "home/AppData/Roaming"} {
		if e := os.MkdirAll(filepath.Join(p.runtime(), filepath.FromSlash(sub)), 0700); e != nil {
			return e
		}
	}
	sha, e := publicDistribution(rustupDistribution+".sha256", 512)
	if e != nil {
		return e
	}
	fields := strings.Fields(string(sha))
	if len(fields) < 1 || !hex64.MatchString(fields[0]) {
		return errors.New("official rustup digest unavailable")
	}
	raw, e := publicDistribution(rustupDistribution, 32<<20)
	if e != nil {
		return e
	}
	if digest(raw) != fields[0] {
		return errors.New("official rustup distribution digest mismatch")
	}
	if e = writeNewBytes(filepath.Join(p.runtime(), "sdk", "rustup-init.exe"), raw); e != nil {
		return e
	}
	helper, e := os.ReadFile(filepath.Join(p.Root, "helper.exe"))
	if e != nil {
		return e
	}
	// CARGO_HOME/bin is also the directory toolchain actions prepend via
	// GITHUB_PATH. Keep the tool-entry guards at that exact conventional path.
	for _, name := range []string{"rustup.exe", "cargo.exe"} {
		if e = writeNewBytes(filepath.Join(p.runtime(), "cargo", "bin", name), helper); e != nil {
			return e
		}
	}
	return saveNew(filepath.Join(p.Root, "sdk-distribution.json"), map[string]any{"url": rustupDistribution, "sha256": digest(raw), "bytes": len(raw), "guard_sha256": digest(helper), "credential_free": true})
}

func replaceEnv(env []string, key, value string) []string {
	out := []string{}
	for _, s := range env {
		k, _, _ := strings.Cut(s, "=")
		if !strings.EqualFold(k, key) {
			out = append(out, s)
		}
	}
	return append(out, key+"="+value)
}

// SDK commands are a closed, non-administrative bootstrap sequence inside the
// same Job Object. This is not an admin callback or a command supplied in JSON.
func sdkCommand(p Plan, stage, exe string, env []string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()
	c := exec.CommandContext(ctx, exe, args...)
	hidden(c)
	if stage == "msvc" && len(args) == 4 {
		c.SysProcAttr.CmdLine = syscall.EscapeArg(exe) + " /d /s /c " + args[3]
	}
	c.WaitDelay = 2 * time.Second
	c.Env = env
	c.Dir = p.runtime()
	if strings.HasPrefix(stage, "fixture-") {
		c.Dir = filepath.Join(p.source(), "tools")
	}
	f, e := os.OpenFile(filepath.Join(p.runtime(), "sdk-"+stage+".log"), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	c.Stdout = f
	c.Stderr = f
	runErr := c.Run()
	if _, e = f.Seek(0, 0); e != nil {
		return nil, e
	}
	b, e := boundedRead(f, 4<<20)
	code := -1
	if c.ProcessState != nil {
		code = c.ProcessState.ExitCode()
	}
	if err := saveNew(filepath.Join(p.runtime(), "sdk-"+stage+".json"), map[string]any{"exit": code, "sha256": digest(b), "bytes": len(b), "completed": runErr == nil, "credential_free": true, "official_ci_claimed": false}); err != nil {
		return nil, err
	}
	if e != nil || runErr != nil {
		return nil, fmt.Errorf("credential-free SDK stage %s failed (exit %d)", stage, code)
	}
	return b, nil
}

func initializeSDK(p Plan) error {
	if _, e := os.Lstat(filepath.Join(p.runtime(), "sdk-ready.json")); !os.IsNotExist(e) {
		return errors.New("SDK initialization is single use")
	}
	env := workerEnv(p)
	bootstrap := filepath.Join(p.runtime(), "sdk", "bootstrap")
	installEnv := replaceEnv(env, "CARGO_HOME", bootstrap)
	installEnv = replaceEnv(installEnv, "RUSTUP_INIT_SKIP_PATH_CHECK", "yes")
	installEnv = replaceEnv(installEnv, "RUSTUP_INIT_SKIP_MSVC_CHECK", "yes")
	if _, e := sdkCommand(p, "rustup-init", filepath.Join(p.runtime(), "sdk", "rustup-init.exe"), installEnv, "-y", "--no-modify-path", "--profile", "minimal", "--default-toolchain", "none"); e != nil {
		return e
	}
	rustup := filepath.Join(bootstrap, "bin", "rustup.exe")
	for _, toolchain := range []string{"1.95.0", "stable"} {
		if _, e := sdkCommand(p, "install-"+toolchain, rustup, env, "toolchain", "install", toolchain, "--profile", "minimal", "--component", "rustfmt", "--component", "clippy", "--no-self-update"); e != nil {
			return e
		}
	}
	if _, e := sdkCommand(p, "default", rustup, env, "default", "1.95.0"); e != nil {
		return e
	}
	msvc, e := msvcEnvironment(p, env)
	if e != nil {
		return e
	}
	if e = saveNew(filepath.Join(p.runtime(), "sdk-environment.json"), msvc); e != nil {
		return e
	}
	env = withMSVC(env, msvc)
	for _, v := range []struct {
		stage, exe string
		args       []string
	}{
		{"git", `C:\Program Files\Git\cmd\git.exe`, []string{"--version"}},
		{"powershell", `C:\Program Files\PowerShell\7\pwsh.exe`, []string{"-NoLogo", "-NoProfile", "-NonInteractive", "-Command", "$PSVersionTable.PSVersion.ToString()"}},
		{"sccache", `D:\CC\Tools\sccache\sccache.exe`, []string{"--version"}},
		{"cargo-gate-image", `D:\CC\Tools\codex-jobs\cargo_gate.exe`, nil},
		{"rustc-1.95.0", filepath.Join(bootstrap, "bin", "rustc.exe"), []string{"+1.95.0", "-vV"}},
		{"rustc-stable", filepath.Join(bootstrap, "bin", "rustc.exe"), []string{"+stable", "-vV"}},
	} {
		if v.args == nil {
			// Measure executable read access without trying to take a second
			// gate lock under the service identity. Actual admission is held
			// by the controller throughout the finite service lifetime.
			b, e := os.ReadFile(v.exe)
			if e != nil {
				return errors.New("filtered account cannot read shared cargo gate")
			}
			if e = saveNew(filepath.Join(p.runtime(), "sdk-"+v.stage+".json"), map[string]any{"sha256": digest(b), "bytes": len(b), "read_access": true, "lock_access_not_claimed": true}); e != nil {
				return e
			}
			continue
		}
		if _, e = sdkCommand(p, v.stage, v.exe, env, v.args...); e != nil {
			return e
		}
	}
	return saveNew(filepath.Join(p.runtime(), "sdk-ready.json"), map[string]any{"rust_1_95_0": true, "stable": true, "msvc_environment": true, "credential_free": true, "official_ci_claimed": false})
}

func msvcEnvironment(p Plan, env []string) (map[string]string, error) {
	b, e := sdkCommand(p, "vswhere", `C:\Program Files (x86)\Microsoft Visual Studio\Installer\vswhere.exe`, env, "-latest", "-products", "*", "-requires", "Microsoft.VisualStudio.Component.VC.Tools.x86.x64", "-property", "installationPath")
	if e != nil {
		return nil, e
	}
	install := strings.TrimSpace(string(b))
	if strings.ContainsAny(install, "\r\n\"&|<>^%!") || (!strings.HasPrefix(install, `C:\Program Files\Microsoft Visual Studio\`) && !strings.HasPrefix(install, `C:\Program Files (x86)\Microsoft Visual Studio\`)) {
		return nil, errors.New("installed MSVC path rejected")
	}
	bat := filepath.Join(install, "Common7", "Tools", "VsDevCmd.bat")
	// Only a validated installed SDK script runs; no plan/CLI command text is
	// accepted. Its output comes from the credential-free environment above.
	b, e = sdkCommand(p, "msvc", `C:\Windows\System32\cmd.exe`, env, "/d", "/s", "/c", `""`+bat+`" -no_logo -arch=x64 -host_arch=x64 >nul && set"`)
	if e != nil {
		return nil, e
	}
	allow := map[string]bool{"PATH": true, "INCLUDE": true, "LIB": true, "LIBPATH": true, "WINDOWSSDKDIR": true, "WINDOWSSDKVERSION": true, "VCTOOLSINSTALLDIR": true, "VCTOOLSVERSION": true, "VCINSTALLDIR": true, "VSINSTALLDIR": true, "UCRTVERSION": true, "UNIVERSALCRTSDKDIR": true}
	out := map[string]string{}
	for _, line := range strings.Split(string(b), "\n") {
		k, v, ok := strings.Cut(strings.TrimSuffix(line, "\r"), "=")
		if ok && allow[strings.ToUpper(k)] {
			out[strings.ToUpper(k)] = v
		}
	}
	if out["INCLUDE"] == "" || out["LIB"] == "" || out["PATH"] == "" {
		return nil, errors.New("MSVC include/lib/path unavailable")
	}
	return out, nil
}
func withMSVC(env []string, values map[string]string) []string {
	for k, v := range values {
		env = replaceEnv(env, k, v)
	}
	return env
}
func runtimeSDKEnv(p Plan, env []string) []string {
	b, e := os.ReadFile(filepath.Join(p.runtime(), "sdk-environment.json"))
	if e != nil {
		return env
	}
	var values map[string]string
	if json.Unmarshal(b, &values) != nil {
		return env
	}
	return withMSVC(env, values)
}
