package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"time"
)

func exists(p int) bool { return !errors.Is(syscall.Kill(-p, 0), syscall.ESRCH) }
func drain(p int) {
	if !exists(p) {
		return
	}
	_ = syscall.Kill(-p, syscall.SIGTERM)
	deadline := time.Now().Add(3 * time.Second)
	for exists(p) {
		if time.Now().After(deadline) {
			_ = syscall.Kill(-p, syscall.SIGKILL)
		}
		time.Sleep(25 * time.Millisecond)
	}
}
func execute() int {
	if len(os.Args) != 4 {
		return 120
	}
	j, e := strconv.Atoi(os.Args[2])
	if e != nil || j < 1 || j > 10 {
		return 121
	}
	t, e := strconv.Atoi(os.Args[3])
	if e != nil || t < 1 || t > 10 {
		return 122
	}
	c := exec.Command("/bin/sh", os.Args[1])
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	c.Dir = "/work/source"
	if e = os.MkdirAll("/work/tmp", 0700); e != nil {
		return 124
	}
	// Env-clearing fixtures still use /tmp. It belongs only to this non-root
	// container user; a private mode satisfies their real ancestor admission.
	if e = os.Chmod("/tmp", 0700); e != nil {
		return 124
	}
	c.Env = []string{"HOME=/work/home", "TMPDIR=/work/tmp", "PATH=/opt/rust/bin:/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin", "CARGO_HOME=/work/cargo", "RUSTUP_HOME=/opt/rustup", "CARGO_TARGET_DIR=/work/target", "CARGO_BUILD_JOBS=" + os.Args[2], "RUST_TEST_THREADS=" + os.Args[3], "NEXTEST_TEST_THREADS=" + os.Args[3], "CARGO_INCREMENTAL=0", "CARGO_PROFILE_DEV_DEBUG=0", "CARGO_PROFILE_TEST_DEBUG=0"}
	c.Env = append(c.Env, "CI=true")
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if c.Start() != nil {
		return 123
	}
	done := make(chan error, 1)
	go func() { done <- c.Wait() }()
	stop := make(chan struct{})
	go func() { io.Copy(io.Discard, os.Stdin); close(stop) }()
	code := 0
	select {
	case e = <-done:
		if e != nil {
			code = 1
		}
	case <-stop:
		drain(c.Process.Pid)
		<-done
		code = 125
	}
	drain(c.Process.Pid)
	fmt.Fprintf(os.Stderr, "kernel_group_absent=true direct_child_reaped=true exit=%d\n", code)
	return code
}
func main() {
	if len(os.Args) == 4 && os.Args[1] == "unzip-sdk" {
		if e := unzipSDK(os.Args[2], os.Args[3]); e != nil {
			fmt.Fprintln(os.Stderr, "SDK extraction failed")
			os.Exit(1)
		}
		return
	}
	os.Exit(execute())
}
