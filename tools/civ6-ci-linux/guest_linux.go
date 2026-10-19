//go:build linux && amd64

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"syscall"
	"time"
	"unsafe"
)

// seccomp is inherited by ALL descendants. Once the supervisor creates the Cargo
// process group, no descendant can leave it, create a namespace, ptrace the
// supervisor or obtain another process's descriptors.
func constrainGroup() error {
	const ld = 0x20
	const jeq = 0x15
	const ret = 0x06
	f := []syscall.SockFilter{{Code: ld, K: 4}, {Code: jeq, K: 0xc000003e, Jt: 1}, {Code: ret, K: 0x80000000}, {Code: ld, K: 0}}
	// Also disallow the x32 syscall-number space.
	f = append(f, syscall.SockFilter{Code: 0x35, K: 0x40000000, Jf: 1}, syscall.SockFilter{Code: ret, K: 0x80000000})
	for _, nr := range []uint32{syscall.SYS_SETSID, syscall.SYS_SETPGID, syscall.SYS_UNSHARE, 308, syscall.SYS_PTRACE, 311, 310, 438} {
		f = append(f, syscall.SockFilter{Code: jeq, K: nr, Jf: 1}, syscall.SockFilter{Code: ret, K: 0x00050000 | uint32(syscall.EPERM)})
	}
	f = append(f, syscall.SockFilter{Code: ret, K: 0x7fff0000})
	p := syscall.SockFprog{Len: uint16(len(f)), Filter: &f[0]}
	if _, _, e := syscall.Syscall6(syscall.SYS_PRCTL, 38, 1, 0, 0, 0, 0); e != 0 {
		return e
	}
	if _, _, e := syscall.Syscall6(syscall.SYS_PRCTL, 22, 2, uintptr(unsafe.Pointer(&p)), 0, 0, 0); e != 0 {
		return e
	}
	runtime.KeepAlive(f)
	return nil
}

// Deny the official worker access to every real Cargo/rustup/toolchain byte.
// Only host-created docker exec processes can enter the real toolchain.
func landlockFilesystem(runner bool) error {
	const create, add, restrict = 444, 445, 446
	const execute = uint64(1)
	const readFile = uint64(4)
	const readDir = uint64(8)
	type ruleset struct{ Handled uint64 }
	// REFER is required for cross-directory rename/link; TRUNCATE prevents a
	// worker from damaging the read-only helper without opening it for writing.
	abi, _, probeErr := syscall.Syscall(create, 0, 0, 1)
	if probeErr != 0 || abi < 3 {
		return errors.New("Landlock ABI >= 3 required for REFER and TRUNCATE")
	}
	const allFS = uint64(32767)
	a := ruleset{allFS}
	fd, _, errno := syscall.Syscall(create, uintptr(unsafe.Pointer(&a)), unsafe.Sizeof(a), 0)
	if errno != 0 {
		return fmt.Errorf("Landlock required: %w", errno)
	}
	defer syscall.Close(int(fd))
	type rule struct {
		Allowed uint64
		Parent  int32
		Padding uint32
	}
	paths := []string{"/", "/usr", "/bin", "/lib", "/lib64", "/etc", "/dev", "/proc", "/sys", "/runner", "/runner/_diag", "/work", "/tmp", "/home/runner"}
	if !runner {
		paths = append(paths, "/opt/rust", "/opt/rustup", "/toolchain")
	}
	for _, path := range paths {
		p, e := syscall.Open(path, 0x200000|syscall.O_CLOEXEC, 0)
		if e != nil {
			if errors.Is(e, syscall.ENOENT) {
				continue
			}
			return e
		}
		allowed := readFile | readDir
		// .NET resolves ancestor directories while starting Runner.Listener.
		// Listing the immutable SDK root grants neither file reads nor exec.
		if path == "/" {
			allowed = readDir
		}
		if path == "/usr" || path == "/bin" || path == "/lib" || path == "/lib64" || path == "/runner" || path == "/work" || path == "/home/runner" {
			allowed |= execute
		}
		if path == "/work" || path == "/tmp" || path == "/home/runner" || path == "/runner/_diag" || path == "/toolchain" {
			allowed = allFS
		}
		if path == "/opt/rust" || path == "/opt/rustup" {
			allowed |= execute
		}
		if path == "/dev" {
			allowed |= 2
		}
		r := rule{Allowed: allowed, Parent: int32(p)}
		_, _, errno = syscall.Syscall6(add, fd, 1, uintptr(unsafe.Pointer(&r)), 0, 0, 0)
		syscall.Close(p)
		if errno != 0 {
			return errno
		}
	}
	if _, _, errno = syscall.Syscall6(syscall.SYS_PRCTL, 38, 1, 0, 0, 0, 0); errno != 0 {
		return errno
	}
	if _, _, errno = syscall.Syscall(restrict, fd, 0, 0); errno != 0 {
		return errno
	}
	return nil
}

// execRestricted must be called on a locked OS thread immediately before Exec.
func execRestricted(path string, args, env []string, runner bool) error {
	runtime.LockOSThread()
	e := landlockFilesystem(runner)
	if e == nil {
		e = constrainGroup()
	}
	if e != nil {
		return e
	}
	return syscall.Exec(path, append([]string{path}, args...), env)
}

func groupAlive(id int) bool  { return !errors.Is(syscall.Kill(-id, 0), syscall.ESRCH) }
func groupAbsent(id int) bool { return id > 1 && !groupAlive(id) }
func drainGroup(id int) error {
	for _, s := range []syscall.Signal{syscall.SIGTERM, syscall.SIGKILL} {
		if !groupAlive(id) {
			return nil
		}
		_ = syscall.Kill(-id, s)
		end := time.Now().Add(4 * time.Second)
		for time.Now().Before(end) {
			if !groupAlive(id) {
				return nil
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	return errors.New("Cargo process group still present; host must stop the owned namespace before releasing the gate")
}

// supervise: args are jobs, tests, index, nonce, plan-file [fixture cancel].
func supervise(args []string) int {
	if len(args) != 5 && len(args) != 7 {
		return 120
	}
	jobs, e := strconv.Atoi(args[0])
	tests, te := strconv.Atoi(args[1])
	index, ie := strconv.Atoi(args[2])
	nonce := args[3]
	if e != nil || te != nil || ie != nil || jobs < 1 || jobs > 10 || tests < 1 || tests > 10 || index < 0 || index > cmdRustup || len(nonce) != 64 || args[4] != guestPlanFile {
		return 121
	}
	c := exec.Command(guestBinary, "guest-exec-child", strconv.Itoa(jobs), strconv.Itoa(tests), strconv.Itoa(index))
	if len(args) == 7 {
		if args[5] != "fixture" {
			return 122
		}
		c.Args = append(c.Args, "fixture", args[6])
	}
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	c.Env = toolEnv(jobs, tests)
	c.Stdout, c.Stderr = os.Stdout, os.Stderr
	ready, e := c.StdinPipe()
	if e != nil {
		return 123
	}
	if c.Start() != nil {
		_ = ready.Close()
		return 123
	}
	// The child cannot emit product output before this trusted handshake.
	_ = json.NewEncoder(os.Stdout).Encode(struct {
		Nonce string
		PGID  int
	}{nonce, c.Process.Pid})
	_, _ = ready.Write([]byte{'G'})
	_ = ready.Close()
	done := make(chan error, 1)
	go func() { done <- c.Wait() }()
	disconnected := make(chan struct{})
	go func() { _, _ = io.Copy(io.Discard, os.Stdin); close(disconnected) }()
	code := 0
	select {
	case e = <-done:
		code = exitStatus(e)
	case <-disconnected:
		_ = syscall.Kill(-c.Process.Pid, syscall.SIGTERM)
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			_ = syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
			<-done
		}
		code = 125
	}
	if e = drainGroup(c.Process.Pid); e != nil {
		return 124
	}
	b, _ := json.Marshal(struct {
		Nonce   string
		Index   int
		Drained bool
	}{nonce, index, true})
	if e = os.WriteFile("/runner/drain-"+nonce+"-"+strconv.Itoa(index)+".json", b, 0600); e != nil {
		return 126
	}
	return code
}

func guestExecChild(args []string) error {
	var ready [1]byte
	if _, e := io.ReadFull(os.Stdin, ready[:]); e != nil || ready[0] != 'G' {
		return errors.New("supervisor start handshake absent")
	}
	if len(args) != 3 && len(args) != 5 {
		return errors.New("executor argv rejected")
	}
	jobs, e := strconv.Atoi(args[0])
	tests, te := strconv.Atoi(args[1])
	index, ie := strconv.Atoi(args[2])
	if e != nil || te != nil || ie != nil || jobs != 1 || tests != 1 || index < 0 || index > cmdRustup {
		return errors.New("executor caps rejected")
	}
	p, e := readGuestPacket()
	if e != nil {
		return e
	}
	if len(args) == 5 {
		if args[3] != "fixture" || (args[4] != "true" && args[4] != "false") {
			return errors.New("fixture argv rejected")
		}
		// Synthetic process-lifecycle fixture, never a product Cargo command.
		env := []string{"HOME=/home/runner", "PATH=/opt/rust/bin:/usr/bin:/bin", "CARGO_HOME=/opt/rust", "RUSTUP_HOME=/opt/rustup", "CARGO_TARGET_DIR=" + workspace + "/tools/target", "CARGO_BUILD_JOBS=1", "RUST_TEST_THREADS=1", "CC_CIL_CANCEL_FIXTURE=" + args[4]}
		if e = os.Chdir(workspace); e != nil {
			return e
		}
		return execRestricted("/opt/rust/bin/cargo", []string{"test", "--offline", "--manifest-path", "tools/Cargo.toml", "--workspace", "--all-targets"}, env, false)
	}
	if p.Kind != KindRust || !p.Policy.Present[index] {
		return errors.New("fixed command not in this job's policy")
	}
	path := "/toolchain/rustup/toolchains/1.95.0-x86_64-unknown-linux-gnu/bin/cargo"
	command := p.Policy.args(index)
	if index == cmdRustup {
		path = "/toolchain/cargo/bin/rustup"
	}
	env, dir, e := stepEnvironment(p.Policy, index, jobs, tests)
	if e != nil {
		return e
	}
	if e = os.Chdir(dir); e != nil {
		return e
	}
	return execRestricted(path, command, env, false)
}
