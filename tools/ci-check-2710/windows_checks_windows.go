package main

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

type winEntry struct{ Name, Mode, SHA string }
type winCheck struct {
	Name, Dir, Program string
	Args               []string
}
type winPlan struct {
	RepoID                  int64
	SHA, Tree, Root, Target string
	Entries                 []winEntry
	Checks                  []winCheck
	Jobs, Threads           int
	Smoke                   string
	Version                 string
}

func winPlanPath() string {
	exe, _ := os.Executable()
	return filepath.Join(filepath.Dir(filepath.Dir(exe)), "plan.private.json")
}
func readWin(p string) (v winPlan, e error) {
	b, e := os.ReadFile(p)
	if e == nil {
		e = json.Unmarshal(b, &v)
	}
	return
}
func writeJSON(p string, v any) error {
	b, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return e
	}
	return os.WriteFile(p, b, 0600)
}
func winPrepare(ctx context.Context, id int64, root string) error {
	c, e := pick(id)
	if e != nil {
		return e
	}
	r, e := verify(ctx, c, get)
	if e != nil {
		return e
	}
	var commit struct{ Tree struct{ SHA string } }
	if e = get(ctx, "repos/"+r.FullName+"/git/commits/"+c.SHA, &commit); e != nil {
		return e
	}
	raw, e := boundedOutput(cmd(ctx, gh, "api", "repos/"+r.FullName+"/tarball/"+c.SHA), 128<<20)
	if e != nil {
		return errors.New("Windows archive download failed")
	}
	archive, e := opaqueArchive(raw)
	if e != nil {
		return e
	}
	v := winPlan{RepoID: id, SHA: c.SHA, Tree: commit.Tree.SHA, Root: filepath.Join(root, ".ci-work", fmt.Sprintf("repo%d", id)), Target: filepath.Join(root, ".ci-work", fmt.Sprintf("target-repo%d", id))}
	source := filepath.Join(v.Root, "source")
	if _, e = os.Stat(source); !os.IsNotExist(e) {
		return errors.New("Windows source already exists")
	}
	if e = os.MkdirAll(source, 0700); e != nil {
		return e
	}
	tr := tar.NewReader(bytes.NewReader(archive))
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if strings.ContainsAny(h.Name, ":\x00\r\n") {
			return errors.New("Windows source path unsupported")
		}
		dest := filepath.Join(source, filepath.FromSlash(h.Name))
		rel, err := filepath.Rel(source, dest)
		if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
			return errors.New("Windows source escape")
		}
		if h.Typeflag == tar.TypeDir {
			if err = os.MkdirAll(dest, 0700); err != nil {
				return err
			}
			continue
		}
		if err = os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
			return err
		}
		f, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return errors.New("Windows source extraction failed")
		}
		_, err = io.CopyN(f, tr, h.Size)
		closeErr := f.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		mode := "100644"
		if h.Mode&0111 != 0 {
			mode = "100755"
		}
		v.Entries = append(v.Entries, winEntry{Name: h.Name, Mode: mode})
	}
	if e = writeJSON(filepath.Join(v.Root, "plan.private.json"), v); e != nil {
		return e
	}
	return writeJSON(filepath.Join(v.Root, "source-receipt.private.json"), map[string]any{"repo_id": id, "source_sha": c.SHA, "tree": v.Tree, "archive_sha256": digest(raw), "new_windows_ci_only": true})
}
func sdkDownload(ctx context.Context, root string) error {
	base := "https://static.rust-lang.org/rustup/dist/x86_64-pc-windows-msvc/rustup-init.exe"
	fetch := func(url string) ([]byte, error) {
		req, e := http.NewRequestWithContext(ctx, "GET", url, nil)
		if e != nil {
			return nil, e
		}
		resp, e := http.DefaultClient.Do(req)
		if e != nil {
			return nil, e
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return nil, errors.New("official SDK download unavailable")
		}
		return io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	}
	hash, e := fetch(base + ".sha256")
	if e != nil {
		return e
	}
	b, e := fetch(base)
	if e != nil {
		return e
	}
	fields := strings.Fields(string(hash))
	if len(fields) < 1 || digest(b) != fields[0] {
		return errors.New("official SDK checksum mismatch")
	}
	if e = os.WriteFile(filepath.Join(root, "rustup-init.exe"), b, 0700); e != nil {
		return e
	}
	return writeJSON(filepath.Join(root, "rustup-sdk.private.json"), map[string]string{"sha256": digest(b), "source": "official rustup Windows distribution"})
}

// A finite SCM invocation, not a GitHub runner, listener or recovery monitor.
func winGateChild() int {
	path := os.Getenv("CI2710_WINDOWS_PLAN")
	v, e := readWin(path)
	if e != nil {
		return 120
	}
	v.Jobs, _ = strconv.Atoi(os.Getenv("CARGO_BUILD_JOBS"))
	v.Threads, _ = strconv.Atoi(os.Getenv("RUST_TEST_THREADS"))
	if os.Getenv("CODEX_JOBS_CARGO_LOCK_HELD") != "1" || v.Jobs < 1 || v.Jobs > 10 || v.Threads < 1 || v.Threads > 10 {
		return 121
	}
	exe, _ := os.Executable()
	config := filepath.Join(filepath.Dir(exe), "windows", ".ci-runtime", "windows", "plan.private.json")
	if e = writeJSON(config, v); e != nil {
		return 122
	}
	scm, _, _ := advapi.NewProc("OpenSCManagerW").Call(0, 0, 1)
	if scm == 0 {
		return 123
	}
	defer advapi.NewProc("CloseServiceHandle").Call(scm)
	name, _ := syscall.UTF16PtrFromString(serviceName)
	svc, _, _ := advapi.NewProc("OpenServiceW").Call(scm, uintptr(unsafe.Pointer(name)), 0x0010|0x0020|0x0004)
	if svc == 0 {
		return 124
	}
	defer advapi.NewProc("CloseServiceHandle").Call(svc)
	r, _, _ := advapi.NewProc("StartServiceW").Call(svc, 0, 0)
	if r == 0 {
		return 126
	}
	cancel := make(chan struct{})
	go func() { io.Copy(io.Discard, os.Stdin); close(cancel) }()
	stopping := false
	for {
		select {
		case <-cancel:
			if !stopping {
				var status [9]uint32
				advapi.NewProc("ControlService").Call(svc, 1, uintptr(unsafe.Pointer(&status[0])))
				stopping = true
			}
		default:
		}
		var status [9]uint32
		var needed uint32
		r, _, _ := advapi.NewProc("QueryServiceStatusEx").Call(svc, 0, uintptr(unsafe.Pointer(&status[0])), uintptr(unsafe.Sizeof(status)), uintptr(unsafe.Pointer(&needed)))
		if r == 0 {
			return 127
		}
		if status[1] == 1 {
			if status[3] != 0 {
				return 128
			}
			fmt.Fprintln(os.Stderr, "windows_service_stopped=true owned_job_drained=true")
			return 0
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func windowsWorker() int {
	v, e := readWin(winPlanPath())
	if e != nil {
		return 120
	}
	if v.Jobs < 1 || v.Jobs > 10 || v.Threads < 1 || v.Threads > 10 {
		return 121
	}
	// Only a new virtual service account is accepted; no owner token/profile.
	var token syscall.Handle
	r, _, _ := openToken.Call(^uintptr(0), 8, uintptr(unsafe.Pointer(&token)))
	if r == 0 {
		return 122
	}
	sid, e := tokenUserSID(token)
	syscall.CloseHandle(token)
	if e != nil || !strings.HasPrefix(sid, "S-1-5-80-") {
		return 123
	}
	tmp := filepath.Join(v.Root, "tmp")
	os.MkdirAll(tmp, 0700)
	env := []string{"SystemRoot=C:\\Windows", "WINDIR=C:\\Windows", "SystemDrive=C:", "TEMP=" + tmp, "TMP=" + tmp, "CARGO_HOME=" + filepath.Join(v.Root, "cargo"), "RUSTUP_HOME=" + filepath.Join(v.Root, "rustup"), "CARGO_BUILD_JOBS=" + strconv.Itoa(v.Jobs), "RUST_TEST_THREADS=" + strconv.Itoa(v.Threads), "NEXTEST_TEST_THREADS=" + strconv.Itoa(v.Threads), "CARGO_INCREMENTAL=0", "CARGO_PROFILE_DEV_DEBUG=0", "CARGO_PROFILE_TEST_DEBUG=0", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=NUL", "PATH=C:\\Windows\\System32;C:\\Program Files\\Git\\cmd"}
	home := filepath.Join(v.Root, "cli-home")
	os.MkdirAll(home, 0700)
	env = append(env, "HOME="+home)
	result := map[string]any{"repo_id": v.RepoID, "source_sha": v.SHA, "virtual_account": true, "profile_loading_requested": false, "all_required_checks_success": false}
	defer func() { _ = writeJSON(filepath.Join(v.Root, "windows-result.private.json"), result) }()
	if v.Smoke == "fs-probe" {
		p := filepath.Join(v.Root, "source", ".git", "refs", "_ci2710_probe")
		err := os.Mkdir(p, 0700)
		code := uint32(0)
		if err != nil {
			if pe, ok := err.(*os.PathError); ok {
				if n, ok := pe.Err.(syscall.Errno); ok {
					code = uint32(n)
				}
			}
		}
		result["mkdir_win32"] = code
		p = filepath.Join(v.Root, "source")
		u, _ := syscall.UTF16PtrFromString(p)
		h, err := syscall.CreateFile(u, 0x80, 7, nil, 3, 0x02000000, 0)
		if err == nil {
			defer syscall.CloseHandle(h)
			b := make([]uint16, 4096)
			n, _, err := kernel.NewProc("GetFinalPathNameByHandleW").Call(uintptr(h), uintptr(unsafe.Pointer(&b[0])), 4096, 0)
			result["canonicalized"] = n > 0
			if n == 0 {
				result["canonical_win32"] = uint32(err.(syscall.Errno))
			}
		} else {
			result["directory_open_failed"] = true
		}
		return 0
	}
	if v.Smoke != "" {
		exe, _ := os.Executable()
		child := exec.Command(exe, "win-sleep")
		hidden(child)
		child.Env = env
		if e = child.Start(); e != nil {
			return 131
		}
		h, e := syscall.OpenProcess(0x100000, false, uint32(child.Process.Pid))
		if e != nil {
			return 132
		}
		w, e := syscall.WaitForSingleObject(h, 0)
		syscall.CloseHandle(h)
		if e != nil || w != 258 {
			return 133
		}
		result["child_observed"] = true
		result["child_pid"] = child.Process.Pid
		result["smoke"] = v.Smoke
		if e = writeJSON(filepath.Join(v.Root, "windows-result.private.json"), result); e != nil {
			return 134
		}
		if v.Smoke == "cancel" {
			time.Sleep(10 * time.Minute)
		}
		return 0
	}
	if e = winTree(v, env); e != nil {
		result["failure"] = e.Error()
		return 129
	}
	result["source_tree_verified"] = true
	env, e = winSDKEnvironment(v, env)
	if e != nil {
		result["failure"] = e.Error()
		return 130
	}
	var rows []map[string]any
	for _, q := range v.Checks {
		file := filepath.Join(v.Root, q.Name+".log")
		log, err := os.OpenFile(file, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return 124
		}
		c := exec.Command(q.Program, q.Args...)
		hidden(c)
		c.Env = env
		c.Dir = filepath.Join(v.Root, "source", q.Dir)
		if q.Dir == "@root" {
			c.Dir = v.Root
		}
		c.Stdout = log
		c.Stderr = log
		start := time.Now()
		err = c.Run()
		log.Close()
		code := 0
		if err != nil {
			code = 1
			if ex, ok := err.(*exec.ExitError); ok {
				code = ex.ExitCode()
			}
		}
		b, _ := os.ReadFile(file)
		rows = append(rows, map[string]any{"check": q.Name, "exit": code, "log_sha256": digest(b), "log_bytes": len(b), "duration_ms": time.Since(start).Milliseconds()})
		result["checks"] = rows
		if err = writeJSON(filepath.Join(v.Root, "windows-result.private.json"), result); err != nil {
			return 125
		}
	}
	return 0
}
