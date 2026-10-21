//go:build windows && amd64

package windowsrunner

import (
	sourcepacket "cc/ci-source-2737"
	"cc/product-ci/pressci"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func preparePressWindowsSDK(ctx context.Context, p Plan, env []string) error {
	if p.Source.Product != sourcepacket.PressProduct {
		return nil
	}
	exe, e := os.Executable()
	if e != nil || p.Validate() != nil || !fixedGuestService(exe, IsLocalSystem()) {
		return errors.New("Press SDK requires disposable SYSTEM guest agent")
	}
	root := sdkRoot + `\press-sdk`
	if e = os.Mkdir(root, 0700); e != nil {
		return errors.New("Press SDK preparation must be fresh")
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	run := func(exe string, args ...string) error {
		words := []string{syscall.EscapeArg(exe)}
		for _, a := range args {
			words = append(words, syscall.EscapeArg(a))
		}
		r := runTree(ctx, exe, strings.Join(words, " "), root, env, filepath.Join(root, filepath.Base(exe)+".log"))
		if r.err != nil || r.exit != 0 || !r.drained {
			return errors.New("closed Press SDK installer failed")
		}
		return nil
	}
	for _, name := range []string{"rustup-windows", "chrome-windows"} {
		if name == "chrome-windows" && p.Source.Workflow != "rust-v2-https.yml" {
			continue
		}
		b, e := pressci.FetchSDKAsset(ctx, name)
		if e != nil {
			return e
		}
		base := "rustup-init.exe"
		if name == "chrome-windows" {
			base = "chrome.msi"
		}
		path := filepath.Join(root, base)
		f, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			return e
		}
		_, e = f.Write(b)
		e = errors.Join(e, f.Sync(), f.Close())
		clear(b)
		if e != nil {
			return e
		}
		if name == "rustup-windows" {
			e = run(path, "-y", "--no-modify-path", "--default-toolchain", "1.95.0", "--profile", "minimal", "--component", "rustfmt", "--component", "clippy")
		} else {
			e = run(`C:\Windows\System32\msiexec.exe`, "/i", path, "/qn", "/norestart")
		}
		if e != nil {
			return e
		}
	}
	return nil
}

func servicePathFor(p Plan, sccache string) string {
	path := fixedServicePath(sccache)
	if p.Source.Product == sourcepacket.PressProduct {
		path = strings.Replace(path, sdkRoot+`\rust-1.97.1\bin`, sdkRoot+`\cargo-home\bin`, 1)
		path = runnerRoot + `\externals\node24\bin;` + path
	}
	return path
}
