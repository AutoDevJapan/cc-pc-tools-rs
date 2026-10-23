package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// environment is the reader's only connection to the outside: the deployed
// guard's CLI and Git. Tests replace the guard with a recorded fake.
type environment struct {
	guard         func(args ...string) (stdout []byte, exitCode int, ran bool)
	git           func(root string, args ...string) ([]byte, error)
	beforeRead    func(path string)
	beforeRecheck func()
}

const guardExecutable = `D:\CC\Tools\client-records\scrub391.exe`

const createNoWindow = 0x08000000

func productionEnvironment() environment {
	return environment{guard: runGuard, git: runGit}
}

// verifyUpdate is not implemented yet (ToDo3463 Red).
func verifyUpdate(path string, p profile, env environment) (qualification, string) {
	return qualification{}, "unimplemented"
}

// exactLeaf is not implemented yet (ToDo3463 Red).
func exactLeaf(path, target string) bool {
	return false
}

type limitedBuffer struct {
	bytes.Buffer
	limit    int
	overflow bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > b.limit {
		b.overflow = true
		return 0, errors.New("output limit exceeded")
	}
	return b.Buffer.Write(p)
}

// childEnvironment removes inherited GIT_* overrides so another repository,
// index or object store cannot stand in for the declared source.
func childEnvironment(parent []string) []string {
	out := make([]string, 0, len(parent)+3)
	for _, entry := range parent {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(strings.ToUpper(key), "GIT_") {
			continue
		}
		out = append(out, entry)
	}
	return append(out, "GIT_NO_LAZY_FETCH=1", "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0")
}

func hiddenProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	cmd.WaitDelay = time.Second
	cmd.Env = childEnvironment(os.Environ())
	cmd.Stderr = io.Discard
}

func runGit(root string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	argv := append([]string{"-C", root, "--no-replace-objects", "-c", "core.fsmonitor=false"}, args...)
	cmd := exec.CommandContext(ctx, "git", argv...)
	hiddenProcess(cmd)
	out := &limitedBuffer{limit: 32 << 20}
	cmd.Stdout = out
	err := cmd.Run()
	if out.overflow {
		return nil, errors.New("git output limit exceeded")
	}
	return out.Bytes(), err
}

// runGuard executes the deployed guard by its fixed path. The executable must
// be a plain file; its classification is trusted as the PC's protection authority.
func runGuard(args ...string) ([]byte, int, bool) {
	info, err := os.Lstat(guardExecutable)
	if err != nil || !info.Mode().IsRegular() {
		return nil, -1, false
	}
	if data, ok := info.Sys().(*syscall.Win32FileAttributeData); !ok || data.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return nil, -1, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, guardExecutable, args...)
	hiddenProcess(cmd)
	out := &limitedBuffer{limit: 1 << 20}
	cmd.Stdout = out
	err = cmd.Run()
	if out.overflow {
		return nil, -1, false
	}
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return out.Bytes(), exit.ExitCode(), true
		}
		return nil, -1, false
	}
	return out.Bytes(), 0, true
}
