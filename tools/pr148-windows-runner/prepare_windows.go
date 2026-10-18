//go:build windows && amd64

package main

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
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

// Controller commands never descend into the service-only credential/target
// subtree. Control files and each accessible ancestor still reject reparse.
func checkControl(root string, ancestors bool) error {
	paths := []string{root, filepath.Join(root, "plan.json"), filepath.Join(root, "helper.exe"), filepath.Join(root, "prepared.json")}
	if ancestors {
		for cur := filepath.Dir(root); ; cur = filepath.Dir(cur) {
			paths = append(paths, cur)
			if filepath.Dir(cur) == cur {
				break
			}
		}
	}
	for _, path := range paths {
		fi, e := os.Lstat(path)
		if e != nil {
			return e
		}
		if fi.Sys().(*syscall.Win32FileAttributeData).FileAttributes&0x400 != 0 {
			return errors.New("control reparse rejected")
		}
	}
	return nil
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
	return icaclsWithin(30*time.Second, path, args...)
}
func icaclsWithin(budget time.Duration, path string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()
	c := exec.CommandContext(ctx, `C:\Windows\System32\icacls.exe`, append([]string{path}, args...)...)
	c.WaitDelay = time.Second
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
	if e = writeNewBytes(filepath.Join(p.Root, "helper.exe"), b); e != nil {
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
	if p.Fixture == "" || p.RuntimeFixture {
		if e = prepareRuntimeSDK(p); e != nil {
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
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
		read := func(path string, v any) error {
			limit := int64(4 << 20)
			if strings.HasPrefix(path, "repos/"+repository+"/git/blobs/") {
				limit = 24 << 20
			}
			return apiReadLimit(ctx, path, v, limit)
		}
		e = materializeSource(p, read)
		cancel()
		if e != nil {
			return e
		}
		for _, stage := range []string{"hook-start", "hook-completed"} {
			// Hook files and helper are outside the writable runner directory.
			text := "$ErrorActionPreference = 'Stop'\n& '" + strings.ReplaceAll(filepath.Join(p.Root, "helper.exe"), "'", "''") + "' '" + stage + "' '" + strings.ReplaceAll(p.Root, "'", "''") + "'\nexit $LASTEXITCODE\n"
			if e = writeNewBytes(filepath.Join(p.Root, stage+".ps1"), []byte(text)); e != nil {
				return e
			}
		}
	}
	if p.RuntimeFixture {
		h, tree, data := syntheticSource()
		s, err := fetchSource(h, tree, syntheticRead(data))
		if err != nil {
			return err
		}
		if e = writeSource(p.source(), s); e != nil {
			return e
		}
		if e = prepareCheckoutFixture(p); e != nil {
			return e
		}
	}
	if p.Fixture == "" || p.RuntimeFixture {
		if _, e = sourceGit(p.source(), "config", "--file", filepath.Join(p.runtime(), "home", ".gitconfig"), "--add", "safe.directory", p.source()); e != nil {
			return e
		}
	}
	if e = installService(p); e != nil {
		return e
	}
	sid, e := serviceSID(p)
	if e != nil {
		return e
	}
	if p.Fixture == "" || p.RuntimeFixture {
		if e = checkOwnedTree(p.source()); e != nil {
			return e
		}
		if e = icacls(p.source(), "/setowner", "*"+sid, "/T", "/L"); e != nil {
			return e
		}
		if e = saveNew(filepath.Join(p.Root, "source-owner.json"), map[string]any{"source": p.source(), "sid": sid, "owned_source_only": true}); e != nil {
			return e
		}
	}
	// Only this newly created resource receives ACEs. No SDK, shared gate,
	// sccache, old source, profile, bridge or allowlist is modified.
	if e = icacls(p.Root, "/inheritance:r", "/grant:r", "*S-1-5-18:(OI)(CI)F", "*S-1-5-32-544:(OI)(CI)F", "*"+p.ControllerSID+":(OI)(CI)RX", "*"+sid+":(OI)(CI)RX"); e != nil {
		return e
	}
	if e = icacls(p.runtime(), "/inheritance:r", "/grant:r", "*S-1-5-18:(OI)(CI)F", "*S-1-5-32-544:(OI)(CI)F", "*"+sid+":(OI)(CI)F"); e != nil {
		return e
	}
	if e = prepareLauncherReport(p, sid); e != nil {
		return e
	}
	return saveNew(filepath.Join(p.Root, "prepared.json"), map[string]any{"service": p.name(), "sid": sid, "helper_sha256": digest(b), "credential_free": p.Fixture != "", "official_runner_executed": false})
}
func writeNewBytes(path string, b []byte) error {
	f, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	_, e = f.Write(b)
	return errors.Join(e, f.Close())
}

func extractRunner(p Plan, path string) error {
	// The parent obtains the official actions/runner win-x64 release asset and
	// its SHA256. Validate the same opened bytes before consuming any entry.
	var release struct {
		Tag    string `json:"tag_name"`
		Assets []struct {
			Name, Digest string
			URL          string `json:"browser_download_url"`
		}
	}
	if e := api("repos/actions/runner/releases/tags/v"+p.RunnerVersion, &release); e != nil {
		return e
	}
	assetName := "actions-runner-win-x64-" + p.RunnerVersion + ".zip"
	found := false
	for _, a := range release.Assets {
		if a.Name == assetName {
			if found || a.Digest != "sha256:"+p.RunnerDigest || a.URL != "https://github.com/actions/runner/releases/download/v"+p.RunnerVersion+"/"+assetName {
				return errors.New("official release digest/version/source mismatch")
			}
			found = true
		}
	}
	if !found || release.Tag != "v"+p.RunnerVersion {
		return errors.New("official runner asset absent")
	}
	f, e := os.Open(path)
	if e != nil {
		return e
	}
	defer f.Close()
	fi, e := f.Stat()
	if e != nil {
		return e
	}
	if !fi.Mode().IsRegular() || fi.Size() > 512<<20 {
		return errors.New("official archive size/type rejected")
	}
	b, e := boundedRead(f, 512<<20)
	if e != nil {
		return e
	}
	if len(b) > 512<<20 || digest(b) != p.RunnerDigest {
		return errors.New("official runner archive digest mismatch")
	}
	if e = extractZip(p.runner(), b); e != nil {
		return e
	}
	fi, e = os.Lstat(filepath.Join(p.runner(), "bin", "Runner.Listener.exe"))
	if e != nil || !fi.Mode().IsRegular() {
		return errors.New("official listener missing")
	}
	return nil
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
			stem := strings.ToUpper(strings.SplitN(part, ".", 2)[0])
			if stem == "CON" || stem == "PRN" || stem == "AUX" || stem == "NUL" || (len(stem) == 4 && (strings.HasPrefix(stem, "COM") || strings.HasPrefix(stem, "LPT")) && stem[3] >= '1' && stem[3] <= '9') {
				return errors.New("runner zip device name rejected")
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
