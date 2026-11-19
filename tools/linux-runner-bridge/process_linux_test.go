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
	"sync"
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

// groupSignalLog wraps the real group signal and records, at every call,
// whether the leader is still an unreaped direct child of this process.
type groupSignalLog struct {
	mu      sync.Mutex
	sent    []syscall.Signal
	reaped  bool
	late    int
	unbound int
}

func (l *groupSignalLog) signal(groupID int, sig syscall.Signal) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, err := childState(groupID, false); err != nil {
		l.unbound++
	}
	if l.reaped {
		l.late++
	}
	l.sent = append(l.sent, sig)
	return syscall.Kill(-groupID, sig)
}

func (l *groupSignalLog) markReaped() {
	l.mu.Lock()
	l.reaped = true
	l.mu.Unlock()
}

func (l *groupSignalLog) supervisor() groupSupervisor {
	return groupSupervisor{signal: l.signal, members: groupMembers, afterReap: l.markReaped}
}

func (l *groupSignalLog) requireOnlyUnreapedSignals(t *testing.T, atLeast int) {
	t.Helper()
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.sent) < atLeast {
		t.Fatalf("group signals sent: %v, want at least %d", l.sent, atLeast)
	}
	if l.unbound != 0 || l.late != 0 {
		t.Fatalf("group signalled without an unreaped leader: unbound=%d after-reap=%d", l.unbound, l.late)
	}
}

func TestLiveTermIgnoringGroupRequiresForceKill(t *testing.T) {
	// The leader is not tied to the context: only the group drain may end it.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.Command("/bin/sh", "-c", "trap '' TERM; printf 'ready\\n'; exec sleep 30")
	group(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	if _, err := bufio.NewReader(stdout).ReadString('\n'); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatal(err)
	}
	if !processGroupExists(cmd.Process.Pid) {
		t.Fatal("live group was treated as drained")
	}
	cancel()
	log := &groupSignalLog{}
	started := time.Now()
	waitErr, cleanupErr := log.supervisor().run(ctx, cmd, 50*time.Millisecond, time.Second)
	if elapsed := time.Since(started); elapsed < 50*time.Millisecond || elapsed > 2*time.Second {
		t.Fatalf("force-kill grace period: %s", elapsed)
	}
	var exit *exec.ExitError
	if cleanupErr != nil || !errors.As(waitErr, &exit) || cmd.ProcessState == nil {
		t.Fatalf("force-killed leader was not drained and reaped: wait=%v cleanup=%v", waitErr, cleanupErr)
	}
	log.requireOnlyUnreapedSignals(t, 2)
	if processGroupExists(cmd.Process.Pid) {
		t.Fatal("group remained after the supervised drain")
	}
}

func TestSuccessfulLeaderDrainsBackgroundGroup(t *testing.T) {
	file := t.TempDir() + "/late-write"
	cmd := exec.Command("/bin/sh", "-c", "(sleep 30; printf unexpected > \"$1\") </dev/null >/dev/null 2>&1 & printf '%s\\n' \"$!\"", "sh", file)
	group(cmd)
	cmd.Stdout = io.Discard
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	leader := cmd.Process.Pid
	if exited, err := childState(leader, true); err != nil || !exited {
		t.Fatalf("leader exit was not observed without reaping it: exited=%v err=%v", exited, err)
	}
	if census, err := groupMembers(leader); err != nil || census.live == 0 {
		t.Fatal("fixture did not retain its background writer after a successful leader exit")
	}
	if !processGroupExists(leader) {
		t.Fatal("an unreaped leader did not keep its group number reserved")
	}
	log := &groupSignalLog{}
	waitErr, cleanupErr := log.supervisor().run(context.Background(), cmd, 200*time.Millisecond, 5*time.Second)
	if waitErr != nil || cleanupErr != nil {
		t.Fatalf("successful leader result changed: wait=%v cleanup=%v", waitErr, cleanupErr)
	}
	log.requireOnlyUnreapedSignals(t, 1)
	if cmd.ProcessState == nil || !cmd.ProcessState.Success() {
		t.Fatal("leader was not reaped with its own result")
	}
	if processGroupExists(leader) {
		t.Fatal("successful exit released the target while a background group remained")
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatal("background writer changed the target after leader completion")
	}
}

func TestSuperviseGroupKeepsPrimaryResult(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "sleep 30 </dev/null >/dev/null 2>&1 & exit 7")
	group(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	leader := cmd.Process.Pid
	log := &groupSignalLog{}
	waitErr, cleanupErr := log.supervisor().run(context.Background(), cmd, 200*time.Millisecond, 5*time.Second)
	var exit *exec.ExitError
	if cleanupErr != nil || !errors.As(waitErr, &exit) || exit.ExitCode() != 7 {
		t.Fatalf("leader result was replaced: wait=%v cleanup=%v", waitErr, cleanupErr)
	}
	log.requireOnlyUnreapedSignals(t, 0)
	if processGroupExists(leader) {
		t.Fatal("background member survived the supervised drain")
	}
}

func TestSuperviseGroupCancelDrainsAndReapsLeader(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", "sleep 30 & wait")
	group(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	leader := cmd.Process.Pid
	deadline := time.Now().Add(5 * time.Second)
	for {
		census, err := groupMembers(leader)
		if err == nil && census.live > 0 {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			_ = cmd.Wait()
			t.Fatal("fixture member did not start")
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	log := &groupSignalLog{}
	started := time.Now()
	waitErr, cleanupErr := log.supervisor().run(ctx, cmd, 3*time.Second, 15*time.Second)
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("process group drain exceeded its expected bound: %s", elapsed)
	}
	if cleanupErr != nil || waitErr == nil || cmd.ProcessState == nil {
		t.Fatalf("cancelled leader was not drained and reaped: wait=%v cleanup=%v", waitErr, cleanupErr)
	}
	log.requireOnlyUnreapedSignals(t, 1)
	if processGroupExists(leader) {
		t.Fatal("process group still exists after drain returned")
	}
}

func TestSuperviseGroupUnknownCensusStillDrainsBeforeReap(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "sleep 30 </dev/null >/dev/null 2>&1 & exit 0")
	group(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	leader := cmd.Process.Pid
	log := &groupSignalLog{}
	s := log.supervisor()
	s.members = func(int) (groupCensus, error) { return groupCensus{}, errors.New("census unavailable") }
	waitErr, cleanupErr := s.run(context.Background(), cmd, 50*time.Millisecond, 100*time.Millisecond)
	if waitErr != nil || cleanupErr != nil {
		t.Fatalf("unknown census changed the results: wait=%v cleanup=%v", waitErr, cleanupErr)
	}
	log.requireOnlyUnreapedSignals(t, 2)
	if processGroupExists(leader) {
		t.Fatal("release did not wait for kernel group absence")
	}
}

func TestSuperviseGroupRefusesGroupWithoutOwnedLeader(t *testing.T) {
	// The sentinel leads a live group but is not a child of this process, as a
	// recycled group number would be: signalling it would hit an unrelated group.
	pidFile := filepath.Join(t.TempDir(), "sentinel-pid")
	starter := exec.Command("/bin/sh", "-c", "setsid sh -c 'echo $$ > \"$1\"; exec sleep 30' sh \"$1\" </dev/null >/dev/null 2>&1 &", "sh", pidFile)
	if err := starter.Run(); err != nil {
		t.Fatal(err)
	}
	var sentinel int
	for deadline := time.Now().Add(5 * time.Second); sentinel == 0; time.Sleep(5 * time.Millisecond) {
		if data, err := os.ReadFile(pidFile); err == nil {
			sentinel, _ = strconv.Atoi(strings.TrimSpace(string(data)))
		}
		if time.Now().After(deadline) {
			t.Fatal("sentinel did not start")
		}
	}
	defer syscall.Kill(sentinel, syscall.SIGKILL)
	if groupID, err := syscall.Getpgid(sentinel); err != nil || groupID != sentinel {
		t.Fatalf("sentinel does not lead its own group: %d %v", groupID, err)
	}
	proc, err := os.FindProcess(sentinel)
	if err != nil {
		t.Fatal(err)
	}
	stale := &exec.Cmd{Process: proc, SysProcAttr: &syscall.SysProcAttr{Setpgid: true}}
	log := &groupSignalLog{}
	waitErr, cleanupErr := log.supervisor().run(context.Background(), stale, 50*time.Millisecond, time.Second)
	if waitErr != nil || !errors.Is(cleanupErr, errGroupBindingUnknown) {
		t.Fatalf("unowned group was not refused: wait=%v cleanup=%v", waitErr, cleanupErr)
	}
	if len(log.sent) != 0 {
		t.Fatalf("unowned group was signalled: %v", log.sent)
	}
	if err := syscall.Kill(sentinel, 0); err != nil {
		t.Fatalf("unowned group was terminated: %v", err)
	}
}

func TestSuperviseGroupWithoutOwnGroupReapsOnlyTheDirectChild(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "sleep 30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	log := &groupSignalLog{}
	waitErr, cleanupErr := log.supervisor().run(context.Background(), cmd, 50*time.Millisecond, time.Second)
	if waitErr == nil || !errors.Is(cleanupErr, errGroupBindingUnknown) || cmd.ProcessState == nil {
		t.Fatalf("child outside an own group was not refused and reaped: wait=%v cleanup=%v", waitErr, cleanupErr)
	}
	if len(log.sent) != 0 {
		t.Fatalf("a group was signalled without a proven leader: %v", log.sent)
	}
}

func TestSuperviseGroupNormalChildSendsNoSignal(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "exit 0")
	group(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	leader := cmd.Process.Pid
	log := &groupSignalLog{}
	waitErr, cleanupErr := log.supervisor().run(context.Background(), cmd, time.Second, time.Second)
	if waitErr != nil || cleanupErr != nil || cmd.ProcessState == nil || !cmd.ProcessState.Success() {
		t.Fatalf("normal child result changed: wait=%v cleanup=%v", waitErr, cleanupErr)
	}
	log.requireOnlyUnreapedSignals(t, 0)
	if len(log.sent) != 0 {
		t.Fatalf("a group without members was signalled: %v", log.sent)
	}
	if processGroupExists(leader) {
		t.Fatal("group still exists after a normal child was supervised")
	}
}

func TestSuperviseGroupReleasesWhenLastMemberExits(t *testing.T) {
	// The only descendant ignores SIGTERM and ends by itself. The group census
	// must release the reap once the group is empty: no wait for the grace
	// period and no SIGKILL.
	cmd := exec.Command("/bin/sh", "-c", "(trap '' TERM; sleep 0.3) </dev/null >/dev/null 2>&1 & exit 0")
	group(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	leader := cmd.Process.Pid
	log := &groupSignalLog{}
	started := time.Now()
	waitErr, cleanupErr := log.supervisor().run(context.Background(), cmd, 10*time.Second, 10*time.Second)
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("an empty group was not released when its last member exited: %s", elapsed)
	}
	if waitErr != nil || cleanupErr != nil {
		t.Fatalf("results changed: wait=%v cleanup=%v", waitErr, cleanupErr)
	}
	log.requireOnlyUnreapedSignals(t, 0)
	for _, sig := range log.sent {
		if sig == syscall.SIGKILL {
			t.Fatalf("an empty group was force-killed: %v", log.sent)
		}
	}
	if processGroupExists(leader) {
		t.Fatal("group still exists after its last member exited")
	}
}

func TestSuperviseGroupLeavesOtherOwnedGroupAlone(t *testing.T) {
	other := exec.Command("/bin/sleep", "30")
	group(other)
	if err := other.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = other.Process.Kill()
		_ = other.Wait()
	}()
	cmd := exec.Command("/bin/sh", "-c", "sleep 30 </dev/null >/dev/null 2>&1 & exit 3")
	group(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	log := &groupSignalLog{}
	waitErr, cleanupErr := log.supervisor().run(context.Background(), cmd, 100*time.Millisecond, 5*time.Second)
	var exit *exec.ExitError
	if cleanupErr != nil || !errors.As(waitErr, &exit) || exit.ExitCode() != 3 {
		t.Fatalf("leader result was replaced: wait=%v cleanup=%v", waitErr, cleanupErr)
	}
	log.requireOnlyUnreapedSignals(t, 1)
	if exited, err := childState(other.Process.Pid, false); err != nil || exited {
		t.Fatalf("another owned group was signalled: exited=%v err=%v", exited, err)
	}
	if err := syscall.Kill(-other.Process.Pid, 0); err != nil {
		t.Fatalf("another owned group is gone: %v", err)
	}
}

// The version probe keeps the outcome exec.Cmd.WaitDelay gave it: output that a
// descendant still holds open one second after the probe exited is rejected,
// and anything else is accepted. Either way the probe's group is drained.
func TestProjectionVersionOutputHeldOpenDecidesTheOutcome(t *testing.T) {
	const version = "cargo 1.95.0 (fixture)\n"
	cases := []struct {
		name    string
		member  string // the background member the probe leaves behind
		accept  bool
		escaped bool // the member leaves the probe's group and session
	}{
		{"stdout-and-stderr-held", "sleep 30", false, false},
		{"stderr-held", "sleep 30 >/dev/null", false, false},
		{"released-within-the-delay", "sleep 0.3", true, false},
		{"not-held", "sleep 30 </dev/null >/dev/null 2>&1", true, false},
		{"escaped-holder", "setsid sleep 30", false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pidFile := filepath.Join(t.TempDir(), "member-pid")
			script := "printf 'cargo 1.95.0 (fixture)\\n'; " + tc.member + " & echo $! > \"$1\"; exit 0"
			template := exec.Command("/bin/sh", "-c", script, "sh", pidFile)
			template.Dir = t.TempDir()
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			started := time.Now()
			raw, err := runProjectionVersion(ctx, template)
			elapsed := time.Since(started)
			data, readErr := os.ReadFile(pidFile)
			pid, convErr := strconv.Atoi(strings.TrimSpace(string(data)))
			if readErr != nil || convErr != nil {
				t.Fatalf("member pid unavailable: %v %v", readErr, convErr)
			}
			if tc.escaped {
				defer syscall.Kill(pid, syscall.SIGKILL)
			}
			if tc.accept {
				if err != nil || string(raw) != version {
					t.Fatalf("output was not held open, yet the probe was rejected: %q %v", raw, err)
				}
			} else {
				if !errors.Is(err, errProjectionToolchain) || raw != nil {
					t.Fatalf("output held open past the delay was accepted: %q %v", raw, err)
				}
				if elapsed < 900*time.Millisecond || elapsed > 8*time.Second {
					t.Fatalf("the one-second wait for the output was not applied: %s", elapsed)
				}
			}
			if alive := syscall.Kill(pid, 0) == nil; alive != tc.escaped {
				t.Fatalf("member alive after the probe = %v, want %v", alive, tc.escaped)
			}
		})
	}
}

func TestSuperviseGroupAfterExitRunsBeforeAnySignal(t *testing.T) {
	t.Run("leader-exited", func(t *testing.T) {
		cmd := exec.Command("/bin/sh", "-c", "sleep 30 </dev/null >/dev/null 2>&1 & exit 4")
		group(cmd)
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		leader := cmd.Process.Pid
		log := &groupSignalLog{}
		s := log.supervisor()
		calls, signalled, unreaped := 0, -1, false
		s.afterExit = func(context.Context) {
			calls++
			log.mu.Lock()
			signalled = len(log.sent)
			log.mu.Unlock()
			exited, err := childState(leader, false)
			unreaped = err == nil && exited
		}
		waitErr, cleanupErr := s.run(context.Background(), cmd, 200*time.Millisecond, 5*time.Second)
		if calls != 1 || signalled != 0 || !unreaped {
			t.Fatalf("hook: calls=%d signals-before=%d leader-exited-and-unreaped=%v", calls, signalled, unreaped)
		}
		var exit *exec.ExitError
		if cleanupErr != nil || !errors.As(waitErr, &exit) || exit.ExitCode() != 4 {
			t.Fatalf("results changed: wait=%v cleanup=%v", waitErr, cleanupErr)
		}
		log.requireOnlyUnreapedSignals(t, 1)
		if processGroupExists(leader) {
			t.Fatal("member survived the supervised drain")
		}
	})
	t.Run("cancelled-before-exit", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cmd := exec.Command("/bin/sh", "-c", "sleep 30")
		group(cmd)
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		log := &groupSignalLog{}
		s := log.supervisor()
		called := false
		s.afterExit = func(context.Context) { called = true }
		cancel()
		waitErr, cleanupErr := s.run(ctx, cmd, 50*time.Millisecond, time.Second)
		if called || cleanupErr != nil || waitErr == nil {
			t.Fatalf("hook ran for a cancelled command or results changed: called=%v wait=%v cleanup=%v", called, waitErr, cleanupErr)
		}
		log.requireOnlyUnreapedSignals(t, 1)
	})
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
