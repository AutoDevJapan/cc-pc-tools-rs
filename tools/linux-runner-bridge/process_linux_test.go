package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestZombieGroupRetainsTargetUntilReaped(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "exit 0")
	group(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Wait()
	deadline := time.Now().Add(2 * time.Second)
	for {
		stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", cmd.Process.Pid))
		end := strings.LastIndexByte(string(stat), ')')
		if err == nil && end >= 0 {
			fields := strings.Fields(string(stat[end+1:]))
			if len(fields) > 0 && fields[0] == "Z" {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("child did not become an unreaped zombie")
		}
		time.Sleep(time.Millisecond)
	}
	if !processGroupExists(cmd.Process.Pid) {
		t.Fatal("a process snapshot released a group still present in the kernel")
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	if processGroupExists(cmd.Process.Pid) {
		t.Fatal("reaped group still exists")
	}
}

func TestLiveTermIgnoringGroupRequiresForceKill(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", "trap '' TERM; printf 'ready\\n'; exec sleep 30")
	group(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	if _, err := bufio.NewReader(stdout).ReadString('\n'); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatal(err)
	}
	go func() { done <- cmd.Wait() }()
	if !processGroupExists(cmd.Process.Pid) {
		t.Fatal("live group was treated as drained")
	}
	started := time.Now()
	if err := terminateAndDrainGroup(cmd, 50*time.Millisecond, time.Second); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed < 50*time.Millisecond || elapsed > 2*time.Second {
		t.Fatalf("force-kill grace period: %s", elapsed)
	}
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("force-killed child was not reaped")
	}
}

func TestSuccessfulLeaderDrainsBackgroundGroup(t *testing.T) {
	file := t.TempDir() + "/late-write"
	cmd := exec.Command("/bin/sh", "-c", `(sleep 30; printf unexpected > "$1") </dev/null >/dev/null 2>&1 & printf '%s\n' "$!"`, "sh", file)
	group(cmd)
	if _, err := cmd.Output(); err != nil {
		t.Fatal(err)
	}
	defer killGroup(cmd)
	if !processGroupExists(cmd.Process.Pid) {
		t.Fatal("fixture did not retain its background writer after a successful leader exit")
	}
	if code := finishGroup(cmd, nil); code != 0 {
		t.Fatalf("successful leader result changed: %d", code)
	}
	if processGroupExists(cmd.Process.Pid) {
		t.Fatal("successful exit released the target while a background group remained")
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatal("background writer changed the target after leader completion")
	}
}

func TestKillGroupDrainsBeforeReturning(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "sleep 30 & wait")
	group(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	groupID := cmd.Process.Pid
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	started := time.Now()
	killGroup(cmd)
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("process group drain exceeded its expected bound: %s", elapsed)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("leader was not reaped after the group drained")
	}
	if processGroupExists(groupID) {
		t.Fatal("process group still exists after drain returned")
	}
}

// This helper runs only as this test binary's explicitly owned fixture process.
func TestProjectionNativeOwnedChild(t *testing.T) {
	ready := os.Getenv("CC_PROJECTION_NATIVE_READY")
	if ready == "" {
		return
	}
	release := os.Getenv("CC_PROJECTION_NATIVE_RELEASE")
	if release == "" {
		t.Fatal("owned fixture release path missing")
	}
	readyPath := ready + ".pending"
	if err := os.WriteFile(readyPath, []byte(strconv.Itoa(os.Getpid())), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(readyPath, ready); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(release); err == nil {
			return
		} else if !os.IsNotExist(err) {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("owned fixture release deadline")
}

func projectionNativeFixtureCommand(t *testing.T, ready, release string) *exec.Cmd {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-test.run=^TestProjectionNativeOwnedChild$")
	cmd.Env = append(os.Environ(), "CC_PROJECTION_NATIVE_READY="+ready, "CC_PROJECTION_NATIVE_RELEASE="+release)
	return cmd
}

func projectionAwaitNativeReady(path string) (int, error) {
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			pid, err := strconv.Atoi(string(data))
			if err != nil || pid <= 0 {
				return 0, errors.New("owned fixture PID rejected")
			}
			return pid, nil
		}
		if !os.IsNotExist(err) {
			return 0, err
		}
		time.Sleep(10 * time.Millisecond)
	}
	return 0, errors.New("owned fixture ready deadline")
}

func TestProjectionActualCommandStartCancelAndReap(t *testing.T) {
	for _, action := range []string{"complete", "eof", "cancel"} {
		t.Run(action, func(t *testing.T) {
			root := t.TempDir()
			owner, writer := projectionInvocationFixture(t)
			ctx, cancel := context.WithCancel(owner.ctx)
			commandResult := make(chan int, 1)
			memberResult := make(chan error, 1)
			rootReturned, memberReturned := false, false
			var member *exec.Cmd
			memberStarted := false
			t.Cleanup(func() {
				cancel()
				_ = writer.Close()
				if memberStarted {
					_ = member.Process.Kill()
					if !memberReturned {
						select {
						case <-memberResult:
							memberReturned = true
						case <-time.After(5 * time.Second):
							t.Error("owned group member was not reaped during cleanup")
						}
					}
				}
				if !rootReturned {
					select {
					case <-commandResult:
						rootReturned = true
					case <-time.After(10 * time.Second):
						t.Error("actual command did not complete during cleanup")
					}
				}
			})
			ready := filepath.Join(root, "leader-ready")
			release := filepath.Join(root, "leader-release")
			template := projectionNativeFixtureCommand(t, ready, release)
			go func() { commandResult <- executeCargoCommand(ctx, template) }()
			pid, err := projectionAwaitNativeReady(ready)
			if err != nil {
				t.Fatal(err)
			}
			if groupID, err := syscall.Getpgid(pid); err != nil || groupID != pid {
				t.Fatal("actual command did not create its owned process group")
			}
			// The test owns and waits this second child directly. It shares the
			// actual command's group, so this check needs no orphan/init reaper.
			memberReady := filepath.Join(root, "member-ready")
			member = projectionNativeFixtureCommand(t, memberReady, filepath.Join(root, "member-never-released"))
			member.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pgid: pid}
			if err := member.Start(); err != nil {
				t.Fatal(err)
			}
			memberStarted = true
			go func() { memberResult <- member.Wait() }()
			memberPID, err := projectionAwaitNativeReady(memberReady)
			if err != nil || memberPID != member.Process.Pid {
				t.Fatal("owned group member did not become ready")
			}
			if groupID, err := syscall.Getpgid(memberPID); err != nil || groupID != pid {
				t.Fatal("owned group member joined another process group")
			}
			want := 125
			switch action {
			case "complete":
				want = 0
				if err := os.WriteFile(release, []byte("release"), 0600); err != nil {
					t.Fatal(err)
				}
			case "eof":
				if err := writer.Close(); err != nil {
					t.Fatal(err)
				}
			case "cancel":
				cancel()
			}
			select {
			case code := <-commandResult:
				rootReturned = true
				if code != want {
					t.Fatalf("actual command exit=%d want=%d", code, want)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("actual command did not drain and return")
			}
			select {
			case err := <-memberResult:
				memberReturned = true
				var exit *exec.ExitError
				if !errors.As(err, &exit) || member.ProcessState == nil {
					t.Fatal("owned group member was not terminated by group cleanup")
				}
			case <-time.After(3 * time.Second):
				t.Fatal("actual command returned while its group member remained")
			}
			for _, childPID := range []int{pid, memberPID} {
				var status syscall.WaitStatus
				if _, err := syscall.Wait4(childPID, &status, syscall.WNOHANG, nil); !errors.Is(err, syscall.ECHILD) {
					t.Fatal("owned child had not been waited before return")
				}
			}
			if processGroupExists(pid) {
				t.Fatal("actual command returned before kernel group absence")
			}
		})
	}
}

func TestProjectionPollableInputFirstProbeFailureClosesWhileWriterOpen(t *testing.T) {
	for _, failFirst := range []bool{false, true} {
		name := "positive"
		if failFirst {
			name = "first-probe-error"
		}
		t.Run(name, func(t *testing.T) {
			read, writer, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer read.Close()
			defer writer.Close()
			input, err := openProjectionInvocationInput(read)
			if err != nil {
				t.Fatal(err)
			}
			if input == read {
				input.Close()
				t.Fatal("proc-fd reopen returned the borrowed descriptor")
			}
			// SyscallConn avoids File.Fd's switch back to blocking mode.
			raw, err := input.SyscallConn()
			if err != nil {
				input.Close()
				t.Fatal(err)
			}
			var flags uintptr
			var flagErr syscall.Errno
			if err := raw.Control(func(fd uintptr) {
				flags, _, flagErr = syscall.Syscall(syscall.SYS_FCNTL, fd, syscall.F_GETFL, 0)
			}); err != nil || flagErr != 0 || flags&syscall.O_NONBLOCK == 0 {
				input.Close()
				t.Fatal("actual proc-fd reopen did not preserve O_NONBLOCK")
			}
			owner, err := beginProjectionInvocation(input)
			if err != nil {
				input.Close()
				t.Fatal(err)
			}
			t.Cleanup(func() {
				writer.Close()
				if err := owner.Close(); err != nil {
					t.Error(err)
				}
			})
			probes, starts := 0, 0
			var diagnostic bytes.Buffer
			code := executeProjectionCargo(owner.ctx, []string{"test", projectionRustRequirement}, "2", "3", nil, io.Discard, &diagnostic,
				func(_ context.Context, cmd *exec.Cmd) ([]byte, error) {
					probes++
					if failFirst {
						return nil, errProjectionToolchain
					}
					tool := filepath.Base(cmd.Path)
					return projectionVersionFixture(tool, projectionRustRelease), nil
				},
				func(context.Context, *exec.Cmd) int {
					starts++
					return 0
				})
			if failFirst {
				if code != 1 || probes != 1 || starts != 0 || strings.Contains(diagnostic.String(), "linux-toolchain-verified") {
					t.Fatal("first probe error reached a later stage")
				}
			} else if code != 0 || probes != 2 || starts != 1 {
				t.Fatal("positive pollable input did not reach the execution boundary")
			}
			closed := make(chan error, 1)
			go func() { closed <- owner.Close() }()
			select {
			case err := <-closed:
				if err != nil {
					t.Fatal(err)
				}
				if _, err := writer.Stat(); err != nil {
					t.Fatal("Close/join required the still-owned writer to close")
				}
				if _, err := read.Stat(); err != nil {
					t.Fatal("Close invalidated the borrowed original read descriptor")
				}
				select {
				case <-owner.done:
				default:
					t.Fatal("Close returned before input monitor joined")
				}
			case <-time.After(5 * time.Second):
				// Release only this owned fixture writer before reporting failure.
				writer.Close()
				select {
				case <-closed:
				case <-time.After(5 * time.Second):
					t.Error("input Close remained blocked after fixture EOF")
				}
				t.Fatal("pollable Close/join did not finish while writer remained open")
			}
		})
	}
}
