package main

import (
	"cc/product-ci/pressci"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func preparePressSDK(ctx context.Context) (pressSDKInventory, error) {
	var result pressSDKInventory
	if os.Geteuid() != 0 {
		return result, errors.New("Press preparation requires disposable guest root")
	}
	b, e := os.ReadFile("/etc/os-release")
	if e != nil || !strings.Contains("\n"+string(b), "\nID=ubuntu\n") || !strings.Contains(string(b), `VERSION_ID="24.04"`) {
		return result, errors.New("Press requires Ubuntu 24.04 guest")
	}
	result.OS = "ubuntu-24.04"
	if e = os.Mkdir("/opt/ci2766-sdk", 0700); e != nil {
		return result, errors.New("Press SDK preparation is single-use")
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	key, e := pressci.FetchSDKAsset(ctx, "chrome-linux-key")
	if e != nil {
		return result, e
	}
	write := func(path string, b []byte) error {
		f, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
		if e != nil {
			return e
		}
		_, e = f.Write(b)
		return errors.Join(e, f.Close())
	}
	if e = write("/usr/share/keyrings/todo2766-google.asc", key); e != nil {
		return result, e
	}
	if e = write("/etc/apt/sources.list.d/todo2766-google.list", []byte("deb [arch=amd64 signed-by=/usr/share/keyrings/todo2766-google.asc] https://dl.google.com/linux/chrome/deb/ stable main\n")); e != nil {
		return result, e
	}
	run := func(user bool, exe string, args ...string) ([]byte, error) {
		c := exec.CommandContext(ctx, exe, args...)
		c.WaitDelay = time.Second
		c.Env = []string{"HOME=/home/ci2737", "LANG=C.UTF-8", "PATH=" + liveRunnerPath, "DEBIAN_FRONTEND=noninteractive", "CARGO_HOME=/home/ci2737/.cargo", "RUSTUP_HOME=/home/ci2737/.rustup"}
		c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if user {
			c.SysProcAttr.Credential = &syscall.Credential{Uid: 1001, Gid: 1001}
		} else {
			// Root package operations never search the worker-writable cargo PATH.
			c.Env = []string{"HOME=/root", "LANG=C.UTF-8", "PATH=/usr/sbin:/usr/bin:/sbin:/bin", "DEBIAN_FRONTEND=noninteractive"}
		}
		c.Cancel = func() error {
			if c.Process != nil {
				return syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
			}
			return nil
		}
		var out, errout sdkBuffer
		out.Limit = 32 << 10
		errout.Limit = 32 << 10
		c.Stdout = &out
		c.Stderr = &errout
		err := c.Run()
		exit := 0
		if err != nil {
			exit = -1
			if ee, ok := err.(*exec.ExitError); ok {
				exit = ee.ExitCode()
			}
		}
		stage := filepath.Base(exe)
		if len(args) > 0 {
			stage += "/" + args[0]
		}
		result.Steps = append(result.Steps, pressSDKStep{Stage: stage, Exit: exit, Stdout: append([]byte(nil), out.Bytes()...), Stderr: append([]byte(nil), errout.Bytes()...)})
		if err != nil {
			return nil, errors.New("closed Press SDK preparation command failed")
		}
		return out.Bytes(), nil
	}
	if _, e = run(false, "/usr/bin/apt-get", "update", "-qq"); e != nil {
		return result, e
	}
	if _, e = run(false, "/usr/bin/apt-get", "install", "-qq", "-y", "--no-install-recommends", "google-chrome-stable"); e != nil {
		return result, e
	}
	// A second update also detects incompatible source entries from postinst.
	if _, e = run(false, "/usr/bin/apt-get", "update", "-qq"); e != nil {
		return result, e
	}
	if _, e = run(true, "/home/ci2737/.cargo/bin/rustup", "toolchain", "install", "1.95.0", "--profile", "minimal", "--component", "rustfmt", "--component", "clippy", "--no-self-update"); e != nil {
		return result, e
	}
	chrome, e := run(true, "/opt/google/chrome/chrome", "--version")
	if e != nil {
		return result, e
	}
	result.Chrome = strings.TrimSpace(string(chrome))
	rust, e := run(true, "/home/ci2737/.cargo/bin/rustc", "+1.95.0", "--version")
	if e != nil {
		return result, e
	}
	result.Rust = strings.TrimSpace(string(rust))
	if !strings.HasPrefix(result.Chrome, "Google Chrome ") || !strings.HasPrefix(result.Rust, "rustc 1.95.0 ") {
		return result, errors.New("Press SDK version rejected")
	}
	return result, nil
}
