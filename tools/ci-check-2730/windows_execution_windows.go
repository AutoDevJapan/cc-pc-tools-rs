package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func winRun(ctx context.Context, file string) error {
	v, e := readWin(file)
	if e != nil {
		return e
	}
	if e = os.MkdirAll(v.Root, 0700); e != nil {
		return e
	}
	c, e := pick(v.RepoID)
	if e != nil {
		return e
	}
	if v.SHA != c.SHA {
		return errors.New("Windows fixed SHA mismatch")
	}
	if _, e = verify(ctx, c, get); e != nil {
		return e
	}
	exe, _ := os.Executable()
	x := cmd(context.Background(), `D:\CC\Tools\codex-jobs\cargo_gate.exe`, "test", "--target-dir", v.Target)
	x.Env = nil
	for _, k := range []string{"SystemRoot", "WINDIR", "SystemDrive", "TEMP", "TMP", "PATH", "USERPROFILE", "LOCALAPPDATA", "CC_JOB_ID"} {
		if val := os.Getenv(k); val != "" {
			x.Env = append(x.Env, k+"="+val)
		}
	}
	x.Env = append(x.Env, "CODEX_JOBS_REAL_CARGO="+exe, "CI2730_WINDOWS_PLAN="+file, "CI2730_CHILD=1", "CODEX_JOBS_CARGO_LOCK_DIR=D:/CC/Tools/codex-jobs")
	f, e := os.OpenFile(filepath.Join(v.Root, fmt.Sprintf("gate-%d.log", time.Now().UnixNano())), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	defer f.Close()
	x.Stdout = f
	x.Stderr = f
	pipe, e := x.StdinPipe()
	if e != nil {
		return e
	}
	if e = x.Start(); e != nil {
		return e
	}
	done := make(chan error, 1)
	go func() { done <- x.Wait() }()
	select {
	case e = <-done:
	case <-ctx.Done():
		pipe.Close()
		e = <-done
	}
	pipe.Close()
	_, after := verify(context.Background(), c, get)
	if after != nil {
		return errors.Join(e, errors.New("Windows post-run identity failed"))
	}
	return e
}

func winTree(v winPlan, env []string) error {
	source := filepath.Join(v.Root, "source")
	run := func(args []string, input string) ([]byte, error) {
		args = append([]string{"-c", "safe.directory=" + source}, args...)
		c := exec.Command(`C:\Program Files\Git\cmd\git.exe`, args...)
		hidden(c)
		c.Env = env
		c.Dir = source
		c.Stdin = strings.NewReader(input)
		var stderr bytes.Buffer
		c.Stderr = &stderr
		b, e := c.Output()
		if e != nil {
			_ = os.WriteFile(filepath.Join(v.Root, "git-sdk-error.log"), stderr.Bytes(), 0600)
			if strings.Contains(stderr.String(), "dubious ownership") {
				return nil, errors.New("new CI source owner mismatch")
			}
			if ex, ok := e.(*exec.ExitError); ok {
				return nil, fmt.Errorf("Git SDK exit %d", ex.ExitCode())
			}
			return nil, errors.New("Git SDK process unavailable")
		}
		return b, nil
	}
	if _, e := run([]string{"init", "-q"}, ""); e != nil {
		return fmt.Errorf("Windows Git init failed: %w", e)
	}
	var paths []string
	for _, x := range v.Entries {
		if x.Mode != "160000" {
			paths = append(paths, x.Name)
		}
	}
	b, e := run([]string{"hash-object", "--no-filters", "-w", "--stdin-paths"}, strings.Join(paths, "\n")+"\n")
	if e != nil {
		return errors.New("Windows raw blob hashing failed")
	}
	hashes := strings.Fields(string(b))
	if len(hashes) != len(paths) {
		return errors.New("Windows hash count mismatch")
	}
	var index strings.Builder
	i := 0
	for _, x := range v.Entries {
		sha := x.SHA
		if x.Mode != "160000" {
			sha = hashes[i]
			i++
		}
		fmt.Fprintf(&index, "%s %s\t%s\x00", x.Mode, sha, x.Name)
	}
	if _, e = run([]string{"update-index", "-z", "--index-info"}, index.String()); e != nil {
		return errors.New("Windows index failed")
	}
	b, e = run([]string{"write-tree"}, "")
	if e != nil || strings.TrimSpace(string(b)) != v.Tree {
		return errors.New("Windows source tree mismatch")
	}
	return nil
}

func winSDKEnvironment(v winPlan, env []string) ([]string, error) {
	sdkRoot := v.SDKRoot
	if sdkRoot == "" {
		sdkRoot = v.Root
	}
	vc := exec.Command(`C:\Windows\System32\cmd.exe`, "/d", "/s", "/c", `""C:\Program Files (x86)\Microsoft Visual Studio\2022\BuildTools\VC\Auxiliary\Build\vcvars64.bat" >nul && set PATH && set INCLUDE && set LIB && set LIBPATH"`)
	hidden(vc)
	vc.SysProcAttr.CmdLine = `C:\Windows\System32\cmd.exe /d /s /c ""C:\Program Files (x86)\Microsoft Visual Studio\2022\BuildTools\VC\Auxiliary\Build\vcvars64.bat" >nul && set PATH && set INCLUDE && set LIB && set LIBPATH"`
	vc.Env = env
	vb, err := vc.Output()
	if err != nil {
		return nil, errors.New("C SDK environment unavailable")
	}
	for _, line := range strings.Split(string(vb), "\n") {
		k, val, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		if strings.EqualFold(k, "PATH") || k == "INCLUDE" || k == "LIB" || k == "LIBPATH" {
			var filtered []string
			for _, entry := range env {
				ek, _, _ := strings.Cut(entry, "=")
				if !strings.EqualFold(ek, k) {
					filtered = append(filtered, entry)
				}
			}
			env = append(filtered, strings.ToUpper(k)+"="+val)
		}
	}
	sdkBin := filepath.Dir(os.Args[0])
	env = append(env, "RUSTUP_TOOLCHAIN="+v.Version, "SCCACHE_DIR=D:\\CC\\BuildCache\\sccache", "SCCACHE_CACHE_SIZE=30G", "SCCACHE_SERVER_PORT=42310", "RUSTC_WRAPPER=D:\\CC\\Tools\\sccache\\sccache.exe")
	core := filepath.Join(sdkRoot, "rustup", "toolchains", v.Version+"-x86_64-pc-windows-msvc", "bin")
	linkTools := filepath.Join(sdkRoot, "rustup", "toolchains", v.Version+"-x86_64-pc-windows-msvc", "lib", "rustlib", "x86_64-pc-windows-msvc", "bin")
	env = append(env, "RUSTC="+filepath.Join(core, "rustc.exe"), "RUSTDOC="+filepath.Join(core, "rustdoc.exe"))
	for i, entry := range env {
		if strings.HasPrefix(strings.ToUpper(entry), "PATH=") {
			env[i] = "PATH=" + filepath.Join(v.Root, "ci-tools") + ";" + sdkBin + ";" + core + ";" + linkTools + ";" + filepath.Join(sdkRoot, "cargo", "bin") + `;C:\Program Files\PowerShell\7;C:\Windows\System32\WindowsPowerShell\v1.0;` + entry[5:]
			if v.NodeRoot != "" {
				env[i] = "PATH=" + v.NodeRoot + ";" + env[i][5:]
			}
		}
	}
	return env, nil
}
