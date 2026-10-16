//go:build windows && amd64

package main

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

func hidden(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}
func checkPaths(root string) error {
	// Inspect the spelling from the volume to the root, and every existing
	// descendant. Do not normalize away reparse points or follow junctions.
	for cur := root; ; cur = filepath.Dir(cur) {
		fi, e := os.Lstat(cur)
		if e != nil {
			return e
		}
		if fi.Sys().(*syscall.Win32FileAttributeData).FileAttributes&0x400 != 0 {
			return errors.New("reparse ancestor rejected")
		}
		if filepath.Dir(cur) == cur {
			break
		}
	}
	return checkOwnedTree(root)
}

// The administrative preparation checks every ancestor. The restricted worker
// checks only its owned tree: traversing an ancestor is allowed by Windows but
// reading the owner's Temp directory metadata is neither needed nor granted.
func checkOwnedTree(root string) error {
	return filepath.Walk(root, func(path string, fi os.FileInfo, e error) error {
		if e != nil {
			return e
		}
		if fi.Sys().(*syscall.Win32FileAttributeData).FileAttributes&0x400 != 0 {
			return errors.New("reparse descendant rejected")
		}
		return nil
	})
}

func icacls(path string, args ...string) error {
	c := exec.Command(`C:\Windows\System32\icacls.exe`, append([]string{path}, args...)...)
	hidden(c)
	if e := c.Run(); e != nil {
		return fmt.Errorf("owned ACL operation failed: %w", e)
	}
	return nil
}
func prepare(p Plan, archive string) error {
	if _, e := os.Lstat(filepath.Join(p.Root, "helper.exe")); !os.IsNotExist(e) {
		return errors.New("prepare is single-use; helper already exists or unknown")
	}
	self, e := os.Executable()
	if e != nil {
		return e
	}
	b, e := os.ReadFile(self)
	if e != nil {
		return e
	}
	if e = os.WriteFile(filepath.Join(p.Root, "helper.exe"), b, 0700); e != nil {
		return e
	}
	if e = os.Mkdir(p.runtime(), 0700); e != nil {
		return e
	}
	for _, dir := range []string{"tmp", "home", "cargo", "rustup", "target"} {
		if e = os.Mkdir(filepath.Join(p.runtime(), dir), 0700); e != nil {
			return e
		}
	}
	if p.Fixture == "" {
		if archive == "" {
			return errors.New("official archive required")
		}
		if e = extractRunner(p, archive); e != nil {
			return e
		}
		for _, stage := range []string{"hook-start", "hook-completed"} {
			// Hook files and helper are outside the writable runner directory.
			text := "$ErrorActionPreference = 'Stop'\n& '" + strings.ReplaceAll(filepath.Join(p.Root, "helper.exe"), "'", "''") + "' '" + stage + "' '" + strings.ReplaceAll(p.Root, "'", "''") + "'\nexit $LASTEXITCODE\n"
			if e = os.WriteFile(filepath.Join(p.Root, stage+".ps1"), []byte(text), 0600); e != nil {
				return e
			}
		}
	}
	if e = installService(p); e != nil {
		return e
	}
	sid, e := serviceSID(p)
	if e != nil {
		return e
	}
	// Only this newly created resource receives ACEs. No SDK, shared gate,
	// sccache, old source, profile, bridge or allowlist is modified.
	if e = icacls(p.Root, "/inheritance:r", "/grant:r", "*S-1-5-18:(OI)(CI)F", "*S-1-5-32-544:(OI)(CI)F", "*"+p.ControllerSID+":(OI)(CI)RX", "*"+sid+":(OI)(CI)RX"); e != nil {
		return e
	}
	if e = icacls(p.runtime(), "/inheritance:r", "/grant:r", "*S-1-5-18:(OI)(CI)F", "*S-1-5-32-544:(OI)(CI)F", "*"+sid+":(OI)(CI)F"); e != nil {
		return e
	}
	return saveNew(filepath.Join(p.Root, "prepared.json"), map[string]any{"service": p.name(), "sid": sid, "helper_sha256": digest(b), "credential_free": p.Fixture != "", "official_runner_executed": false})
}

func extractRunner(p Plan, path string) error {
	// The parent obtains the official actions/runner win-x64 release asset and
	// its SHA256. Validate the same opened bytes before consuming any entry.
	b, e := os.ReadFile(path)
	if e != nil {
		return e
	}
	if len(b) > 512<<20 || digest(b) != p.RunnerDigest {
		return errors.New("official runner archive digest mismatch")
	}
	return extractZip(p.runner(), b)
}

func extractZip(root string, b []byte) error {
	z, e := zip.NewReader(strings.NewReader(string(b)), int64(len(b)))
	if e != nil {
		return e
	}
	if len(z.File) > 20000 {
		return errors.New("runner zip entry bound")
	}
	var total uint64
	seen := map[string]bool{}
	for _, f := range z.File {
		n := strings.TrimSuffix(f.Name, "/")
		if n == "" || strings.ContainsAny(n, "\\:\x00\r\n") || strings.HasPrefix(n, "/") || f.Mode()&os.ModeSymlink != 0 {
			return errors.New("runner zip path rejected")
		}
		for _, part := range strings.Split(n, "/") {
			if part == "" || part == "." || part == ".." || strings.TrimRight(part, " .") != part {
				return errors.New("runner zip component rejected")
			}
		}
		key := strings.ToLower(n)
		if seen[key] {
			return errors.New("duplicate runner zip path")
		}
		seen[key] = true
		total += f.UncompressedSize64
		if total > 2<<30 {
			return errors.New("runner zip size bound")
		}
	}
	if e = os.Mkdir(root, 0700); e != nil {
		return e
	}
	for _, f := range z.File {
		path := filepath.Join(root, filepath.FromSlash(f.Name))
		if e = beneath(root, path); e != nil {
			return e
		}
		if f.FileInfo().IsDir() {
			if e = os.MkdirAll(path, 0700); e != nil {
				return e
			}
			continue
		}
		if e = os.MkdirAll(filepath.Dir(path), 0700); e != nil {
			return e
		}
		in, e := f.Open()
		if e != nil {
			return e
		}
		out, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			in.Close()
			return e
		}
		_, a := io.Copy(out, in)
		e = errors.Join(a, in.Close(), out.Close())
		if e != nil {
			return e
		}
	}
	return nil
}
