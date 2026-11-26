package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

const (
	recycledGroupChildEnv    = "CC_BRIDGE_RECYCLED_GROUP_CHILD"
	recycledGroupTest        = "TestSuperviseGroupDoesNotSignalRecycledGroupNumber"
	recycledGroupUnavailable = "recycled-group-scenario-unavailable"
	nsLastPID                = "/proc/sys/kernel/ns_last_pid"
	recycledGroupAttempts    = 5
)

func joinArgs(parts ...[]string) []string {
	var all []string
	for _, part := range parts {
		all = append(all, part...)
	}
	return all
}

// The number of an exited and reaped group can be handed to an unrelated
// process. A private PID namespace lets this test recycle the number on demand:
// the supervisor must neither signal the new group nor mistake it for its own.
// The namespace needs privilege (root, a user namespace, or passwordless sudo).
// Without it the test is skipped; under GitHub Actions it must run.
func TestSuperviseGroupDoesNotSignalRecycledGroupNumber(t *testing.T) {
	if os.Getenv(recycledGroupChildEnv) == "1" {
		recycledGroupScenario(t)
		return
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	marked := []string{"env", recycledGroupChildEnv + "=1"}
	inner := []string{"--pid", "--fork", "--mount", "--mount-proc", "--kill-child=KILL", exe, "-test.run=^" + recycledGroupTest + "$", "-test.v"}
	type launcher struct {
		name string
		argv []string
	}
	var launchers []launcher
	if os.Geteuid() == 0 {
		launchers = append(launchers, launcher{"root", joinArgs(marked, []string{"unshare"}, inner)})
	} else {
		launchers = append(launchers,
			launcher{"user namespace", joinArgs(marked, []string{"unshare", "--user", "--map-root-user"}, inner)},
			launcher{"sudo", joinArgs([]string{"sudo", "-n"}, marked, []string{"unshare"}, inner)})
	}
	var unavailable []string
	for _, l := range launchers {
		out, err := exec.Command(l.argv[0], l.argv[1:]...).CombinedOutput()
		output := string(out)
		switch {
		case err == nil && strings.Contains(output, "--- PASS: "+recycledGroupTest):
			t.Logf("recycled group scenario passed through the %s launcher:\n%s", l.name, strings.TrimSpace(output))
			return
		case strings.Contains(output, "=== RUN") && !strings.Contains(output, recycledGroupUnavailable):
			t.Fatalf("recycled group scenario failed (%s): %v\n%s", l.name, err, output)
		}
		unavailable = append(unavailable, fmt.Sprintf("%s: %v %s", l.name, err, strings.TrimSpace(output)))
	}
	if os.Getenv("GITHUB_ACTIONS") == "true" {
		t.Fatalf("no private PID namespace could recycle a group number: %s", strings.Join(unavailable, "; "))
	}
	t.Skipf("no private PID namespace could recycle a group number: %s", strings.Join(unavailable, "; "))
}

// recycledGroupScenario runs as init of a private PID namespace. It reaps a
// group leader, makes the kernel give the same number to an unrelated group,
// and requires the supervisor to leave that group alone while it waits. A
// namespace that cannot recycle the number is reported as unavailable.
func recycledGroupScenario(t *testing.T) {
	if os.Getpid() != 1 {
		t.Skipf("%s: the scenario must run as init of a private PID namespace", recycledGroupUnavailable)
	}
	last, err := os.ReadFile(nsLastPID)
	if err == nil {
		err = os.WriteFile(nsLastPID, last, 0o644)
	}
	if err != nil {
		t.Skipf("%s: %s is not usable: %v", recycledGroupUnavailable, nsLastPID, err)
	}
	// A runtime thread can take the number before the unrelated group does. Only
	// that case is repeated, with a new leader and a bounded number of attempts.
	// Every attempt and its cause is logged, and any other fault fails the test.
	var causes []string
	for attempt := 1; attempt <= recycledGroupAttempts; attempt++ {
		recycled, cause := recycledGroupAttempt(t)
		if recycled {
			t.Logf("attempt %d/%d: the number was handed to an unrelated group; the assertions ran", attempt, recycledGroupAttempts)
			return
		}
		t.Logf("attempt %d/%d: %v", attempt, recycledGroupAttempts, cause)
		causes = append(causes, cause.Error())
	}
	t.Skipf("%s: the group number was not recycled in %d attempts: %s", recycledGroupUnavailable, recycledGroupAttempts, strings.Join(causes, "; "))
}

// recycledGroupAttempt reports whether the reaped leader's number was handed to
// an unrelated group; if it was not, the error says what the kernel did instead.
// The assertions run only after the hand-over, and any other fault fails the test.
func recycledGroupAttempt(t *testing.T) (bool, error) {
	leader := exec.Command("/bin/sh", "-c", "exit 0")
	group(leader)
	if err := leader.Start(); err != nil {
		t.Fatal(err)
	}
	number := leader.Process.Pid
	log := &groupSignalLog{}
	s := log.supervisor()
	var victim *exec.Cmd
	var fault, missed error
	recycled := make(chan struct{})
	s.afterReap = func() {
		defer close(recycled)
		log.markReaped()
		last := 0
		for try := 0; try < 100; try++ {
			if err := os.WriteFile(nsLastPID, []byte(strconv.Itoa(number-1)), 0o644); err != nil {
				fault = err
				return
			}
			c := exec.Command("/bin/sleep", "30")
			group(c)
			if err := c.Start(); err != nil {
				fault = err
				return
			}
			if c.Process.Pid == number {
				victim = c
				return
			}
			last = c.Process.Pid
			_ = c.Process.Kill()
			_ = c.Wait()
		}
		missed = fmt.Errorf("number %d was not handed to the unrelated group in 100 tries (last pid %d)", number, last)
	}
	done := make(chan struct{})
	go func() {
		s.run(context.Background(), leader, 100*time.Millisecond, time.Second)
		close(done)
	}()
	<-recycled
	if victim == nil {
		<-done
		if fault != nil {
			t.Fatalf("the unrelated group could not be started: %v", fault)
		}
		return false, missed
	}
	defer func() {
		_ = victim.Process.Kill()
		_ = victim.Wait()
	}()
	if groupID, err := syscall.Getpgid(victim.Process.Pid); err != nil || groupID != number {
		t.Fatalf("unrelated group does not carry the recycled number: %d %v", groupID, err)
	}
	// Many 25 ms observations pass while the recycled group exists.
	time.Sleep(500 * time.Millisecond)
	if exited, err := childState(victim.Process.Pid, false); err != nil || exited {
		t.Fatalf("the unrelated group was signalled: exited=%v err=%v", exited, err)
	}
	select {
	case <-done:
		t.Fatal("the target was released while the kernel still reported the group number")
	default:
	}
	log.requireOnlyUnreapedSignals(t, 0)
	_ = victim.Process.Kill()
	_ = victim.Wait()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the supervisor did not release after the kernel reported absence")
	}
	return true, nil
}
