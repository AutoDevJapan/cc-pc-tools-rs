package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type sdkBuffer struct {
	Data  bytes.Buffer
	Limit int
}

func (b *sdkBuffer) Len() int      { return b.Data.Len() }
func (b *sdkBuffer) Bytes() []byte { return b.Data.Bytes() }
func (b *sdkBuffer) Write(p []byte) (int, error) {
	if len(p) > b.Limit-b.Len() {
		return 0, errors.New("public SDK output bound exceeded")
	}
	return b.Data.Write(p)
}

// Only public SDK data crosses this path. It cannot configure a runner,
// receive product source, select a shell command or start Cargo compilation.
func prepareSDK(conn net.Conn, incoming io.Reader, id string) (result sdkResult) {
	result.ID = id
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()
	var log bytes.Buffer
	defer func() { result.Log = append([]byte(nil), log.Bytes()...) }()
	fail := func(code string) sdkResult { result.Failure = code; return result }
	meta, e := readBlob(incoming, 32<<10)
	if e != nil {
		return fail("sdk_manifest_unavailable")
	}
	var m sdkManifest
	d := json.NewDecoder(bytes.NewReader(meta))
	d.DisallowUnknownFields()
	if d.Decode(&m) != nil || d.Decode(new(any)) != io.EOF || m.Image != publicRunnerImage || !validHash(m.RunnerSHA) || !validHash(m.RustupSHA) || m.SccacheSHA != publicSccacheSHA {
		return fail("sdk_manifest_rejected")
	}
	runner, e := readBlob(incoming, 768<<20)
	if e != nil || hash(runner) != m.RunnerSHA {
		return fail("runner_sdk_rejected")
	}
	if e = extractRunnerSDK(runner, "/opt/ci2737-runner"); e != nil {
		return fail("runner_sdk_extraction_failed")
	}
	runner = nil
	rustup, e := readBlob(incoming, 64<<20)
	if e != nil || hash(rustup) != m.RustupSHA || !bytes.HasPrefix(rustup, []byte{0x7f, 'E', 'L', 'F'}) {
		return fail("rustup_sdk_rejected")
	}
	if e = os.Mkdir("/opt/ci2737-public-tools", 0755); e != nil {
		return fail("sdk_tools_directory_rejected")
	}
	if e = os.WriteFile("/opt/ci2737-public-tools/rustup", rustup, 0755); e != nil {
		return fail("rustup_sdk_write_failed")
	}
	rustup = nil
	sccache, e := readBlob(incoming, 32<<20)
	if e != nil || hash(sccache) != publicSccacheSHA {
		return fail("sccache_sdk_rejected")
	}
	if e = extractSccacheSDK(sccache, "/usr/local/bin/sccache"); e != nil {
		return fail("sccache_sdk_extraction_failed")
	}
	sccache = nil
	// A closed controller cancels every command group. The host still stops the
	// entire owned VM and checks memory zero before considering cleanup done.
	go func() { var one [1]byte; _, _ = incoming.Read(one[:]); cancel() }()
	var runnerGroups []uint32
	command := func(asRunner bool, name string, args ...string) ([]byte, error) {
		c := exec.CommandContext(ctx, name, args...)
		c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if asRunner {
			c.SysProcAttr.Credential = &syscall.Credential{Uid: 1001, Gid: 1001, Groups: runnerGroups}
		}
		c.Cancel = func() error {
			if c.Process != nil {
				return syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
			}
			return nil
		}
		c.WaitDelay = 5 * time.Second
		c.Env = []string{"PATH=/home/ci2737/.cargo/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin", "HOME=/home/ci2737", "CARGO_HOME=/home/ci2737/.cargo", "RUSTUP_HOME=/home/ci2737/.rustup", "DEBIAN_FRONTEND=noninteractive", "SCCACHE_DIR=/home/ci2737/.cache/sccache", "SCCACHE_CACHE_SIZE=30G", "CUDA_VISIBLE_DEVICES="}
		b := sdkBuffer{Limit: (16 << 20) - log.Len()}
		c.Stdout = &b
		c.Stderr = &b
		e := c.Run()
		if log.Len()+b.Len() > 16<<20 {
			return nil, errors.New("public SDK output bound exceeded")
		}
		log.Write(b.Bytes())
		return b.Bytes(), e
	}
	if _, e = command(false, "apt-get", "update"); e != nil {
		return fail("sdk_apt_update_failed")
	}
	if _, e = command(false, "apt-get", "install", "-y", "--no-install-recommends", "git", "curl", "ca-certificates", "build-essential", "pkg-config", "libssl-dev", "clang", "lld", "nodejs", "npm", "docker.io", "docker-buildx", "libicu74", "libkrb5-3", "zlib1g", "libunwind8", "liblttng-ust1t64"); e != nil {
		return fail("sdk_apt_install_failed")
	}
	if _, e = command(false, "useradd", "--create-home", "--uid", "1001", "--user-group", "--shell", "/usr/sbin/nologin", "ci2737"); e != nil {
		return fail("sdk_user_create_failed")
	}
	if _, e = command(false, "usermod", "-aG", "docker", "ci2737"); e != nil {
		return fail("sdk_docker_group_failed")
	}
	dockerGroup, e := user.LookupGroup("docker")
	if e != nil {
		return fail("sdk_docker_gid_unavailable")
	}
	dockerGID, e := strconv.ParseUint(dockerGroup.Gid, 10, 32)
	if e != nil {
		return fail("sdk_docker_gid_rejected")
	}
	runnerGroups = []uint32{uint32(dockerGID)}
	if _, e = command(false, "systemctl", "start", "docker"); e != nil {
		return fail("sdk_docker_start_failed")
	}
	for _, dir := range []string{"/home/ci2737/.cargo", "/home/ci2737/.cargo/bin", "/home/ci2737/.rustup", "/home/ci2737/.cache", "/home/ci2737/.cache/sccache"} {
		if e = os.MkdirAll(dir, 0755); e != nil {
			return fail("sdk_user_directory_failed")
		}
		if e = os.Chown(dir, 1001, 1001); e != nil {
			return fail("sdk_user_directory_owner_failed")
		}
	}
	for _, name := range []string{"rustup", "cargo", "rustc", "rustdoc", "rustfmt", "cargo-fmt", "cargo-clippy", "clippy-driver"} {
		if e = os.Symlink("/opt/ci2737-public-tools/rustup", "/home/ci2737/.cargo/bin/"+name); e != nil {
			return fail("sdk_rust_proxy_failed")
		}
	}
	if _, e = command(true, "/home/ci2737/.cargo/bin/rustup", "toolchain", "install", "1.97.1", "--profile", "minimal", "--component", "rustfmt", "--component", "clippy", "--no-self-update"); e != nil {
		return fail("sdk_toolchain_install_failed")
	}
	if _, e = command(true, "/home/ci2737/.cargo/bin/rustup", "default", "1.97.1"); e != nil {
		return fail("sdk_default_toolchain_failed")
	}
	for _, check := range []struct {
		name string
		args []string
		dest *string
		user bool
	}{{"/opt/ci2737-runner/bin/Runner.Listener", []string{"--version"}, &result.RunnerVersion, true}, {"/home/ci2737/.cargo/bin/cargo", []string{"--version"}, &result.CargoVersion, true}, {"docker", []string{"version", "--format", "{{.Server.Version}}"}, &result.DockerVersion, true}, {"docker", []string{"buildx", "version"}, &result.BuildxVersion, true}, {"/usr/local/bin/sccache", []string{"--version"}, &result.SccacheVersion, true}} {
		b, e := command(check.user, check.name, check.args...)
		if e != nil || len(b) > 1024 {
			return fail("sdk_version_probe_failed")
		}
		*check.dest = strings.TrimSpace(string(b))
	}
	// A successful cargo --version does not load librustc_driver. Flush the
	// installed filesystem before the host can stop this disposable VM, then
	// verify the compiler and the shared object that the failing SDK truncated.
	if _, e = command(false, "sync", "--file-system", "/home/ci2737"); e != nil {
		return fail("sdk_filesystem_sync_failed")
	}
	result.DiskSynced = true
	result.RustProbe = probeRustAt(ctx, "/home/ci2737")
	result.RustDriver = probeSDKDriver(ctx)
	if validatePublicSDKSeal(result) != nil {
		return fail("sdk_rust_runtime_rejected")
	}
	return result
}

func safeSDKPath(root, name string) (string, error) {
	name = strings.TrimPrefix(name, "./")
	if name == "" || name == "." {
		return root, nil
	}
	if path.Clean(name) != name || strings.HasPrefix(name, "/") || name == ".." || strings.HasPrefix(name, "../") || strings.ContainsAny(name, "\\\x00\r\n") {
		return "", errors.New("public SDK path rejected")
	}
	return filepath.Join(root, filepath.FromSlash(name)), nil
}
func extractRunnerSDK(data []byte, root string) error {
	if e := os.Mkdir(root, 0755); e != nil {
		return e
	}
	z, e := gzip.NewReader(bytes.NewReader(data))
	if e != nil {
		return e
	}
	defer z.Close()
	tr := tar.NewReader(z)
	var total int64
	type sdkLink struct{ dest, target, spelling string }
	var links []sdkLink
	for {
		h, e := tr.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			return e
		}
		if h.Typeflag == tar.TypeXGlobalHeader {
			continue
		}
		name := h.Name
		if h.Typeflag == tar.TypeDir {
			// tar's directory header spelling includes one trailing slash.
			// Normalize only that suffix, before the ordinary containment checks.
			name = strings.TrimSuffix(name, "/")
		}
		dest, e := safeSDKPath(root, name)
		if e != nil {
			return e
		}
		if strings.HasPrefix(path.Base(name), ".credentials") || path.Base(name) == ".runner" {
			return errors.New("configured runner SDK rejected")
		}
		if h.Typeflag == tar.TypeSymlink {
			if path.IsAbs(h.Linkname) || strings.ContainsAny(h.Linkname, "\\\x00\r\n") || h.Linkname == "" {
				return errors.New("SDK link target rejected")
			}
			target, e := safeSDKPath(root, path.Clean(path.Join(path.Dir(name), h.Linkname)))
			if e != nil {
				return e
			}
			// No links exist during file extraction. Resolve only to a regular
			// file after all files are present, never through another link.
			links = append(links, sdkLink{dest, target, h.Linkname})
			continue
		}
		if h.Typeflag == tar.TypeDir {
			if e = os.MkdirAll(dest, 0755); e != nil {
				return e
			}
			continue
		}
		if h.Typeflag != tar.TypeReg || h.Size < 0 || h.Size > 1<<30 || total+h.Size > 3<<30 {
			return fmt.Errorf("public runner SDK member rejected (type=%d, bytes=%d, expanded_bytes=%d)", h.Typeflag, h.Size, total+h.Size)
		}
		total += h.Size
		if strings.HasPrefix(path.Base(h.Name), ".credentials") || path.Base(h.Name) == ".runner" {
			return errors.New("configured runner SDK rejected")
		}
		if e = os.MkdirAll(filepath.Dir(dest), 0755); e != nil {
			return e
		}
		f, e := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, os.FileMode(h.Mode)&0755)
		if e != nil {
			return e
		}
		_, e = io.Copy(f, tr)
		e = errors.Join(e, f.Close())
		if e != nil {
			return e
		}
		if e = os.Chown(dest, 1001, 1001); e != nil {
			return e
		}
	}
	for _, link := range links {
		info, e := os.Lstat(link.target)
		if e != nil || !info.Mode().IsRegular() {
			return errors.New("SDK link requires an internal regular target")
		}
		if e = os.MkdirAll(filepath.Dir(link.dest), 0755); e != nil {
			return e
		}
		if e = os.Symlink(link.spelling, link.dest); e != nil {
			return e
		}
		if e = os.Lchown(link.dest, 1001, 1001); e != nil {
			return e
		}
	}
	if _, e = os.Stat(filepath.Join(root, "config.sh")); e != nil {
		return errors.New("official runner SDK entry missing")
	}
	return filepath.Walk(root, func(p string, info os.FileInfo, e error) error {
		if e != nil {
			return e
		}
		if info.IsDir() {
			return os.Chown(p, 1001, 1001)
		}
		return nil
	})
}
func extractSccacheSDK(data []byte, dest string) error {
	z, e := gzip.NewReader(bytes.NewReader(data))
	if e != nil {
		return e
	}
	defer z.Close()
	tr := tar.NewReader(z)
	found := false
	for {
		h, e := tr.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			return e
		}
		if h.Typeflag == tar.TypeDir || h.Typeflag == tar.TypeXGlobalHeader {
			continue
		}
		if h.Typeflag == tar.TypeReg && (path.Base(h.Name) == "README.md" || path.Base(h.Name) == "LICENSE") && h.Size <= 1<<20 {
			continue
		}
		if found || h.Typeflag != tar.TypeReg || path.Base(h.Name) != "sccache" || h.Size > 64<<20 || h.Size < 1 {
			return fmt.Errorf("public sccache member rejected")
		}
		f, e := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0755)
		if e != nil {
			return e
		}
		_, e = io.Copy(f, tr)
		e = errors.Join(e, f.Close())
		if e != nil {
			return e
		}
		found = true
	}
	if !found {
		return errors.New("public sccache executable missing")
	}
	return nil
}
