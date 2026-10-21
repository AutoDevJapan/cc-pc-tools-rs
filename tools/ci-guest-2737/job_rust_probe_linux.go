package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"syscall"
	"time"
)

// Only closed commands and public version/hash fields cross the existing wire.
// These probes neither install a toolchain nor compile the product, and are not
// acceptance conditions. In particular, a cachekey is not compiler provenance.
func probeLiveRust(ctx context.Context) []rustProbeResult {
	return probeRustAt(ctx, liveWorkspace)
}

func probeRustAt(ctx context.Context, directory string) []rustProbeResult {
	var result []rustProbeResult
	for _, probe := range []struct{ name, script string }{
		{"compiler", "/home/ci2737/.cargo/bin/rustup which --toolchain 1.97.1 rustc"},
		{"verbose", "command -v rustc >/dev/null && rustc +1.97.1 --version --verbose"},
		{"cachekey", `DATE=$(rustc +1.97.1 --version --verbose | sed -ne 's/^commit-date: \(20[0-9][0-9]\)-\([01][0-9]\)-\([0-3][0-9]\)$/\1\2\3/p')
HASH=$(rustc +1.97.1 --version --verbose | sed -ne 's/^commit-hash: //p')
echo "$DATE$HASH" | head -c12`},
	} {
		flight, cancel := context.WithTimeout(ctx, 8*time.Second)
		c := exec.CommandContext(flight, "/bin/bash", "-e", "-o", "pipefail", "-c", probe.script)
		c.Dir = directory
		c.Env = []string{"HOME=/home/ci2737", "LANG=C.UTF-8", "PATH=" + liveRunnerPath, "CARGO_HOME=/home/ci2737/.cargo", "RUSTUP_HOME=/home/ci2737/.rustup", "RUSTUP_AUTO_INSTALL=0", "RUSTC_WRAPPER=/usr/local/bin/sccache"}
		c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Credential: &syscall.Credential{Uid: 1001, Gid: 1001}}
		c.WaitDelay = time.Second
		c.Cancel = func() error {
			if c.Process != nil {
				return syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
			}
			return nil
		}
		out, stderr := sdkBuffer{Limit: 8192}, sdkBuffer{Limit: 8192}
		c.Stdout, c.Stderr = &out, &stderr
		err := c.Run()
		timedOut := flight.Err() == context.DeadlineExceeded
		cancel()
		exit := 0
		if err != nil {
			exit = -1
			if e, ok := err.(*exec.ExitError); ok {
				exit = e.ExitCode()
			}
		}
		r := summarizeRustProbe(probe.name, exit, out.Bytes(), stderr.Bytes())
		r.TimedOut = timedOut
		if e, ok := err.(*exec.ExitError); ok {
			if status, ok := e.Sys().(syscall.WaitStatus); ok && status.Signaled() {
				r.Signal = int(status.Signal())
			}
		} else if err != nil {
			r.FailureCode = "launch_failed"
		}
		if timedOut {
			r.FailureCode = "timeout"
		}
		// A returned path is never a file-read capability: only this single known
		// SDK compiler is eligible for an observational hash, never a source proof.
		const compiler = "/home/ci2737/.rustup/toolchains/1.97.1-x86_64-unknown-linux-gnu/bin/rustc"
		if probe.name == "compiler" && exit == 0 && strings.TrimSpace(string(out.Bytes())) == compiler {
			if f, e := os.Open(compiler); e == nil {
				if info, e := f.Stat(); e == nil && info.Mode().IsRegular() && info.Size() <= 128<<20 {
					r.CompilerSHA256, _ = hashReader(io.LimitReader(f, 128<<20))
				}
				_ = f.Close()
			}
		}
		result = append(result, r)
	}
	return result
}

func summarizeRustProbe(name string, exit int, out, stderr []byte) rustProbeResult {
	r := rustProbeResult{Name: name, Exit: exit, StdoutBytes: len(out), StderrBytes: len(stderr), StdoutSHA256: hash(out), StderrSHA256: hash(stderr)}
	if exit != 0 {
		r.FailureCode = "other_exit"
		if len(out) == 0 && len(stderr) == 0 {
			r.FailureCode = "silent_exit"
		}
		low := strings.ToLower(string(stderr))
		for _, c := range []struct{ pattern, code string }{
			{"command not found", "command_missing"}, {"no such file or directory", "file_missing"},
			{"permission denied", "permission"}, {"error while loading shared libraries", "loader"},
			{"not installed", "toolchain_missing"}, {"could not choose", "toolchain_missing"},
			{"failed to download", "download"}, {"unexpected argument", "arguments"},
		} {
			if strings.Contains(low, c.pattern) {
				r.FailureCode = c.code
				break
			}
		}
	}
	for _, line := range bytes.Split(out, []byte{'\n'}) {
		s := strings.TrimSpace(string(line))
		if regexp.MustCompile(`^rustc 1\.97\.1 \([a-f0-9]{7,40} 20[0-9]{2}-[0-9]{2}-[0-9]{2}\)$`).MatchString(s) {
			r.Version = s
		}
		if regexp.MustCompile(`^commit-hash: [a-f0-9]{40}$`).MatchString(s) {
			r.CommitHash = strings.TrimPrefix(s, "commit-hash: ")
		}
		if regexp.MustCompile(`^commit-date: 20[0-9]{2}-[0-9]{2}-[0-9]{2}$`).MatchString(s) {
			r.CommitDate = strings.TrimPrefix(s, "commit-date: ")
		}
		if name == "cachekey" && regexp.MustCompile(`^[0-9]{8}[a-f0-9]{4}$`).MatchString(s) {
			r.CacheKey = s
		}
	}
	return r
}
