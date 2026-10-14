package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestFiniteContainerRegistrationBoundary(t *testing.T) {
	base, err := inspectNormalContainer(validNormalContainerInspection)
	if err != nil {
		t.Fatal(err)
	}
	expected := "sha256:" + strings.Repeat("a", 64)
	base.Image, base.Config.Image = expected, finiteImage
	base.HostConfig.Memory, base.HostConfig.NanoCpus, base.HostConfig.PidsLimit = 6<<30, 2_000_000_000, 512
	base.HostConfig.MemorySwap = base.HostConfig.Memory
	cases := []struct {
		name     string
		change   func(*normalContainerInspection)
		accepted bool
	}{
		{"valid", func(*normalContainerInspection) {}, true},
		{"image-id", func(c *normalContainerInspection) { c.Image = "sha256:" + strings.Repeat("b", 64) }, false},
		{"image-tag", func(c *normalContainerInspection) { c.Config.Image = "other:latest" }, false},
		{"privileged", func(c *normalContainerInspection) { c.HostConfig.Privileged = true }, false},
		{"user", func(c *normalContainerInspection) { c.Config.User = "root" }, false},
		{"init", func(c *normalContainerInspection) { c.HostConfig.Init = false }, false},
		{"bind", func(c *normalContainerInspection) { c.HostConfig.Binds = []string{"/owner:/owner"} }, false},
		{"gpu", func(c *normalContainerInspection) {
			c.HostConfig.DeviceRequests = []json.RawMessage{json.RawMessage(`{"Count":1}`)}
		}, false},
		{"device", func(c *normalContainerInspection) {
			c.HostConfig.Devices = []json.RawMessage{json.RawMessage(`{"PathOnHost":"/dev/gpu"}`)}
		}, false},
		{"cap-drop", func(c *normalContainerInspection) { c.HostConfig.CapDrop = nil }, false},
		{"cap-add", func(c *normalContainerInspection) { c.HostConfig.CapAdd = []string{"SYS_ADMIN"} }, false},
		{"privilege-escalation", func(c *normalContainerInspection) { c.HostConfig.SecurityOpt = nil }, false},
		{"memory-unbounded", func(c *normalContainerInspection) { c.HostConfig.Memory = 0 }, false},
		{"memory-high", func(c *normalContainerInspection) { c.HostConfig.Memory = 7 << 30 }, false},
		{"cpu-unbounded", func(c *normalContainerInspection) { c.HostConfig.NanoCpus = 0 }, false},
		{"cpu-high", func(c *normalContainerInspection) { c.HostConfig.NanoCpus = 3_000_000_000 }, false},
		{"pid-unbounded", func(c *normalContainerInspection) { c.HostConfig.PidsLimit = -1 }, false},
		{"host-pid", func(c *normalContainerInspection) { c.HostConfig.PidMode = "host" }, false},
		{"host-network", func(c *normalContainerInspection) { c.HostConfig.NetworkMode = "host" }, false},
		{"shared-pid", func(c *normalContainerInspection) { c.HostConfig.PidMode = "container:other" }, false},
		{"shared-network", func(c *normalContainerInspection) { c.HostConfig.NetworkMode = "container:other" }, false},
		{"gpu-runtime", func(c *normalContainerInspection) { c.HostConfig.Runtime = "nvidia" }, false},
		{"gpu-env", func(c *normalContainerInspection) { c.Config.Env = []string{"NVIDIA_VISIBLE_DEVICES=all"} }, false},
		{"swap-unbounded", func(c *normalContainerInspection) { c.HostConfig.MemorySwap = -1 }, false},
		{"shared-ipc", func(c *normalContainerInspection) { c.HostConfig.IpcMode = "host" }, false},
		{"shared-uts", func(c *normalContainerInspection) { c.HostConfig.UTSMode = "host" }, false},
		{"shared-userns", func(c *normalContainerInspection) { c.HostConfig.UsernsMode = "host" }, false},
		{"shared-cgroupns", func(c *normalContainerInspection) { c.HostConfig.CgroupnsMode = "host" }, false},
		{"extra-security-opt", func(c *normalContainerInspection) {
			c.HostConfig.SecurityOpt = []string{"no-new-privileges:true", "seccomp=unconfined"}
		}, false},
		{"private-defaults", func(c *normalContainerInspection) {
			c.HostConfig.Runtime = "runc"
			c.HostConfig.IpcMode = "private"
			c.HostConfig.CgroupnsMode = "private"
			c.HostConfig.NetworkMode = "default"
		}, true},
		{"credential-no-noexec", func(c *normalContainerInspection) {
			c.HostConfig.Tmpfs = map[string]string{"/tmp": "rw", "/runner": "exec"}
		}, false},
		{"credential-tmpfs", func(c *normalContainerInspection) { c.HostConfig.Tmpfs = map[string]string{"/runner": "exec"} }, false},
		{"credential-exec", func(c *normalContainerInspection) {
			c.HostConfig.Tmpfs = map[string]string{"/tmp": "rw,exec", "/runner": "exec"}
		}, false},
		{"runner-noexec", func(c *normalContainerInspection) {
			c.HostConfig.Tmpfs = map[string]string{"/tmp": "noexec", "/runner": "noexec"}
		}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := base
			tc.change(&c)
			data, _ := json.Marshal([]normalContainerInspection{c})
			runtimeCalls := 0
			run := func(args ...string) (string, error) {
				switch args[0] {
				case "inspect":
					return string(data), nil
				case "image":
					return expected, nil
				case "exec":
					runtimeCalls++
					if args[2] == "id" {
						return "1001", nil
					}
					return "", nil
				default:
					return "", fmt.Errorf("unexpected operation %v", args)
				}
			}
			err := verifyFiniteContainerWith("fixture", run)
			if (err == nil) != tc.accepted {
				t.Fatalf("accepted=%v error=%v", tc.accepted, err)
			}
			if !tc.accepted && runtimeCalls != 0 {
				t.Fatal("runtime reached before isolation rejection")
			}
			if tc.accepted && runtimeCalls != 2 {
				t.Fatalf("runtime checks=%d", runtimeCalls)
			}
		})
	}
	if err := verifyFiniteContainerWith("fixture", func(...string) (string, error) { return "", errors.New("unavailable") }); err == nil {
		t.Fatal("accepted failed inspect")
	}
	data, _ := json.Marshal([]normalContainerInspection{base})
	for _, image := range []string{"", "sha256:bad", "unavailable"} {
		calls := 0
		err := verifyFiniteContainerWith("fixture", func(args ...string) (string, error) {
			if args[0] == "inspect" {
				return string(data), nil
			}
			if args[0] == "image" {
				if image == "unavailable" {
					return "", errors.New("unavailable")
				}
				return image, nil
			}
			calls++
			return "", nil
		})
		if err == nil || calls != 0 {
			t.Fatal("image inspection failure reached runtime")
		}
	}
	if err := verifyFiniteContainerWith("fixture", func(args ...string) (string, error) {
		if args[0] == "inspect" {
			return string(data), nil
		}
		if args[0] == "image" {
			return expected, nil
		}
		return "0", nil
	}); err == nil {
		t.Fatal("accepted a root runtime")
	}
}

func TestRegistrationIdentitySeparatesFiniteAndResidentLabels(t *testing.T) {
	for _, c := range []struct {
		name, labels, ephemeral string
		accepted                bool
	}{
		{normalRunner, normalRunner, "false", true},
		{"cc-pc-tools-linux-2511", finiteLabels, "true", true},
		{"cc-pc-tools-linux-2511", normalRunner, "true", true},
		{normalRunner, finiteLabels, "false", false},
		{normalRunner, normalRunner, "true", false},
		{"cc-pc-tools-linux-2511", finiteLabels, "false", false},
		{"other", finiteLabels, "true", false},
	} {
		if validRegistrationIdentity(c.name, c.labels, c.ephemeral) != c.accepted {
			t.Fatalf("identity %+v", c)
		}
	}
}

func TestNormalRunnerContainerIsolationWhenProvisioned(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("host Docker container is only available on the Windows host")
	}
	if _, err := output(command(docker, "inspect", normalContainer)); err != nil {
		t.Skip("dedicated normal runner container is not provisioned")
	}
	if err := verifyNormalContainer(); err != nil {
		t.Fatal(err)
	}
}

func TestConfiguredRunnerRecoveryDoesNotAmplifyLiveProcessTrees(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("host Docker container is only available on the Windows host")
	}
	if _, err := output(command(docker, "inspect", normalContainer)); err != nil {
		t.Skip("dedicated normal runner container is not provisioned")
	}
	listeners, err := configuredRunnerListenerCount(normalContainer, func(args ...string) (string, error) {
		return output(command(docker, args...))
	})
	if err != nil {
		t.Fatal(err)
	}
	if listeners == 0 {
		t.Skip("production runner has no live Listener; recovery is tested against a disposable container")
	}
	dockerRun := func(args ...string) (string, error) {
		return output(command(docker, args...))
	}
	started, recoveryErr := ensureConfiguredRunnerListener(normalContainer, dockerRun, time.Sleep)
	if listeners == 1 && (recoveryErr != nil || started) {
		t.Fatalf("already-running listener recovery changed state: started=%v err=%v", started, recoveryErr)
	}
	if listeners > 1 && (recoveryErr == nil || started) {
		t.Fatalf("existing duplicate listeners must not be amplified: started=%v err=%v", started, recoveryErr)
	}
	after, err := configuredRunnerListenerCount(normalContainer, dockerRun)
	if err != nil || after != listeners {
		t.Fatalf("live Listener count changed during recovery probe: before=%d after=%d err=%v", listeners, after, err)
	}
}

const validNormalContainerInspection = `[{"Config":{"Image":"todo2510-normal-linux:local","User":"runner"},"HostConfig":{"Init":true,"Privileged":false,"CapDrop":["ALL"],"SecurityOpt":["no-new-privileges:true"],"Devices":[],"DeviceRequests":[],"Binds":[],"Tmpfs":{"/tmp":"rw,noexec,nosuid,size=64m","/runner":"rw,exec,nosuid,size=1g,uid=1001,gid=1001"}},"Mounts":[]}]`

func TestNormalContainerRejectsMissingReaperBeforeStart(t *testing.T) {
	for _, setting := range []string{`"Init":false,`, `"Init":null,`, ""} {
		inspection := strings.Replace(validNormalContainerInspection, `"Init":true,`, setting, 1)
		run, started, _ := fakeDockerForNormalContainer(t, "false", inspection)
		if err := verifyNormalContainerWith("fixture", run); err == nil {
			t.Fatal("container without an init reaper was accepted")
		}
		if *started {
			t.Fatal("container started before its reaper was checked")
		}
	}
}

func TestFiniteContainerRequiresExplicitReaper(t *testing.T) {
	for _, value := range []string{"false", "null", "", "malformed", "true"} {
		err := verifyContainerReaper("fixture", func(args ...string) (string, error) {
			if strings.Join(args, " ") != "inspect --format {{json .HostConfig.Init}} fixture" {
				t.Fatalf("unexpected reaper probe: %v", args)
			}
			return value, nil
		})
		if (err == nil) != (value == "true") {
			t.Fatalf("reaper setting %q: %v", value, err)
		}
	}
	if err := verifyContainerReaper("fixture", func(...string) (string, error) {
		return "true", errors.New("inspection unavailable")
	}); err == nil {
		t.Fatal("unconfirmed reaper was accepted")
	}
}

func fakeDockerForNormalContainer(t *testing.T, initialState string, inspection string) (dockerOutput, *bool, *[][]string) {
	t.Helper()
	started := initialState == "true"
	calls := make([][]string, 0)
	run := func(args ...string) (string, error) {
		calls = append(calls, append([]string(nil), args...))
		if len(args) == 0 {
			return "", fmt.Errorf("empty docker args")
		}
		switch args[0] {
		case "inspect":
			if len(args) > 1 && args[1] == "--format" {
				if started {
					return "true", nil
				}
				return "false", nil
			}
			return inspection, nil
		case "start":
			started = true
			return "test-container", nil
		case "exec":
			if !started {
				return "", fmt.Errorf("exec against stopped fake container")
			}
			if len(args) > 2 && args[2] == "id" {
				return "1001", nil
			}
			return "", nil
		default:
			return "", fmt.Errorf("unexpected docker command: %s", args[0])
		}
	}
	return run, &started, &calls
}

func TestNormalContainerPreflightStartsOnlyAfterStaticIsolationChecks(t *testing.T) {
	run, started, calls := fakeDockerForNormalContainer(t, "false", validNormalContainerInspection)
	if err := verifyNormalContainerWith("test-container", run); err != nil {
		t.Fatal(err)
	}
	if !*started {
		t.Fatal("stopped container was not started after passing static isolation checks")
	}
	startAt, runtimeExecAt := -1, -1
	for index, args := range *calls {
		if args[0] == "start" {
			startAt = index
		}
		if args[0] == "exec" && runtimeExecAt == -1 {
			runtimeExecAt = index
		}
	}
	if startAt < 0 || runtimeExecAt <= startAt {
		t.Fatalf("runtime checks must follow container start: calls=%v", *calls)
	}
}

func TestNormalContainerRejectsStaticIsolationBeforeStarting(t *testing.T) {
	invalid := strings.Replace(validNormalContainerInspection, `"Privileged":false`, `"Privileged":true`, 1)
	run, started, calls := fakeDockerForNormalContainer(t, "false", invalid)
	if err := verifyNormalContainerWith("test-container", run); err == nil {
		t.Fatal("accepted a privileged stopped container")
	}
	if *started {
		t.Fatal("rejected static configuration was started")
	}
	for _, args := range *calls {
		if args[0] == "start" || args[0] == "exec" {
			t.Fatalf("runtime operation occurred before static rejection: %v", *calls)
		}
	}
}

func TestNormalContainerAlreadyRunningIsNotStartedAgain(t *testing.T) {
	run, started, calls := fakeDockerForNormalContainer(t, "true", validNormalContainerInspection)
	if err := verifyNormalContainerWith("test-container", run); err != nil {
		t.Fatal(err)
	}
	if !*started {
		t.Fatal("fake running container unexpectedly stopped")
	}
	for _, args := range *calls {
		if args[0] == "start" {
			t.Fatalf("already-running container received another start: %v", *calls)
		}
	}
}

func TestNormalContainerRuntimeWaitsForRunnerInitToPopulateTmpfs(t *testing.T) {
	run, _, _ := fakeDockerForNormalContainer(t, "true", validNormalContainerInspection)
	readyChecks, pauses := 0, 0
	wrapped := func(args ...string) (string, error) {
		if len(args) > 2 && args[0] == "exec" && args[2] == "/bin/sh" {
			readyChecks++
			if readyChecks < 3 {
				return "", fmt.Errorf("runner-init has not populated the tmpfs yet")
			}
		}
		return run(args...)
	}
	if err := validateNormalContainerRuntime("test-container", wrapped, func(d time.Duration) {
		if d != time.Second {
			t.Fatalf("unexpected readiness wait duration: %s", d)
		}
		pauses++
	}); err != nil {
		t.Fatal(err)
	}
	if readyChecks != 3 || pauses != 2 {
		t.Fatalf("readiness checks=%d pauses=%d, want 3 and 2", readyChecks, pauses)
	}
}

func TestStoppedNormalRunnerContainerCanBeRecoveredWithDocker(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("host Docker container is only available on the Windows host")
	}
	if _, err := output(command(docker, "image", "inspect", normalImage)); err != nil {
		t.Skip("dedicated normal runner image is not provisioned")
	}
	name := fmt.Sprintf("todo2516-preflight-%d", os.Getpid())
	if _, err := output(command(docker, "inspect", name)); err == nil {
		t.Fatalf("test container name already exists: %s", name)
	}
	createArgs := []string{"create", "--init", "--name", name, "--network", "none", "--cap-drop", "ALL", "--security-opt", "no-new-privileges:true", "--tmpfs", "/tmp:rw,noexec,nosuid,size=64m", "--tmpfs", "/runner:rw,exec,nosuid,size=1g,uid=1001,gid=1001", "--user", "runner", normalImage}
	if _, err := output(command(docker, createArgs...)); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := output(command(docker, "rm", "--force", name)); err != nil {
			t.Errorf("remove test-only container: %v", err)
		}
	}()
	state, err := output(command(docker, "inspect", "--format", "{{.State.Running}}", name))
	if err != nil || state != "false" {
		t.Fatalf("container must begin stopped: state=%q err=%v", state, err)
	}
	if err := verifyNormalContainerWith(name, func(args ...string) (string, error) {
		return output(command(docker, args...))
	}); err != nil {
		t.Fatal(err)
	}
	state, err = output(command(docker, "inspect", "--format", "{{.State.Running}}", name))
	if err != nil || state != "true" {
		t.Fatalf("container did not recover to running: state=%q err=%v", state, err)
	}
}

func TestConfiguredRunnerListenerRecoveryDoesNotDuplicateRunningListener(t *testing.T) {
	tests := []struct {
		name      string
		counts    [][2]int // live listeners, live supervisors
		wantStart bool
		wantErr   bool
	}{
		{name: "already running", counts: [][2]int{{1, 2}}},
		{name: "existing runner retry owns recovery", counts: [][2]int{{0, 2}, {0, 1}, {1, 1}}},
		{name: "supervisor starting during grace owns recovery", counts: [][2]int{{0, 0}, {0, 1}, {1, 1}}},
		{name: "stopped listener starts once after two empty probes", counts: [][2]int{{0, 0}, {0, 0}, {1, 1}}, wantStart: true},
		{name: "existing duplicates are not amplified", counts: [][2]int{{2, 2}}, wantErr: true},
		{name: "retrying supervisor is not raced", counts: [][2]int{{0, 1}, {0, 1}, {0, 1}, {0, 1}, {0, 1}, {0, 1}, {0, 1}, {0, 1}, {0, 1}, {0, 1}, {0, 1}, {0, 1}}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			probes, starts := 0, 0
			run := func(args ...string) (string, error) {
				if len(args) >= 3 && args[0] == "exec" && args[1] == "-d" {
					joined := strings.Join(args, "\n")
					if !strings.Contains(joined, "CC_LINUX_RUNNER_MODE=normal") || !strings.Contains(joined, "ACTIONS_RUNNER_HOOK_JOB_STARTED=/opt/bridge/job-hook.sh") {
						return "", errors.New("recovery omitted mode or the pre-job admission hook")
					}
					starts++
					return "started", nil
				}
				if len(args) >= 5 && args[0] == "exec" && args[2] == "/bin/sh" {
					pair := test.counts[min(probes, len(test.counts)-1)]
					value := pair[0]
					if strings.Contains(args[4], "Runner.Listener") {
						if value == 1 {
							probes++
						}
					} else {
						value = pair[1]
						probes++
					}
					return fmt.Sprint(value), nil
				}
				return "", fmt.Errorf("unexpected command: %v", args)
			}
			started, err := ensureConfiguredRunnerListener("test-container", run, func(time.Duration) {})
			if (err != nil) != test.wantErr {
				t.Fatalf("unexpected error: %v", err)
			}
			if started != test.wantStart || (starts == 1) != test.wantStart {
				t.Fatalf("started=%v starts=%d wantStart=%v", started, starts, test.wantStart)
			}
			if starts > 1 {
				t.Fatalf("duplicate runner launch requested: %d", starts)
			}
		})
	}
}

func TestCargoBoundary(t *testing.T) {
	for _, args := range [][]string{{"test", "--locked", "-p", "cc-task-title", "-j64"}, {"nextest", "run", "--locked", "--test-threads=32"}, {"--version"}, {"fmt", "--manifest-path", "cc-bookmark-queue/Cargo.toml", "--check"}, {"fmt", "--manifest-path=cc-bookmark-queue/Cargo.toml", "--check"}} {
		if e := validate(request{Dir: "/work/cc-pc-tools-rs/cc-pc-tools-rs", Args: args}); e != nil {
			t.Fatal(e)
		}
	}
	for _, q := range []request{{Dir: "/etc", Args: []string{"test"}}, {Dir: "/work/cc-pc-tools-rs/cc-pc-tools-rs", Args: []string{"test", "--config=x"}}, {Dir: "/work/cc-pc-tools-rs/cc-pc-tools-rs", Args: []string{"test", "--manifest-path=/etc/a"}}, {Dir: "/work/cc-pc-tools-rs/cc-pc-tools-rs", Args: []string{"test", "--manifest-path", "../Cargo.toml"}}, {Dir: "/work/cc-pc-tools-rs/cc-pc-tools-rs", Args: []string{"test", "--manifest-path", "D:/repo/Cargo.toml"}}, {Dir: "/work/cc-pc-tools-rs/cc-pc-tools-rs", Args: []string{"test", "--manifest-path", "other/Cargo.toml"}}, {Dir: "/work/cc-pc-tools-rs/cc-pc-tools-rs", Args: []string{"test", "--target-dir=/etc/a"}}, {Dir: "/work/cc-pc-tools-rs/cc-pc-tools-rs", Args: []string{"run"}}, {Dir: "/work/cc-pc-tools-rs/cc-pc-tools-rs", Args: []string{"test", "-C/etc"}}} {
		if validate(q) == nil {
			t.Fatalf("accepted %+v", q)
		}
	}
}

func TestNormalJobIdentityUsesPRHeadAndRejectsForkOrStaleHead(t *testing.T) {
	head := strings.Repeat("a", 40)
	h := host{mode: "normal", getHead: func(branch string) (string, error) {
		if branch != "feature/linux-ci" {
			t.Fatalf("unexpected branch lookup %q", branch)
		}
		return head, nil
	}}
	valid := request{Repository: repo, Revision: head, Ref: "refs/pull/17/merge", Event: "pull_request", HeadSHA: head, HeadRef: "feature/linux-ci", HeadRepository: repo, BaseRepository: repo, BaseRef: "main"}
	if err := h.authorizeNormal(valid); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []request{
		{Repository: repo, Revision: strings.Repeat("b", 40), Ref: valid.Ref, Event: valid.Event, HeadSHA: head, HeadRef: valid.HeadRef, HeadRepository: repo, BaseRepository: repo, BaseRef: "main"},
		{Repository: repo, Revision: head, Ref: valid.Ref, Event: valid.Event, HeadSHA: head, HeadRef: valid.HeadRef, HeadRepository: "fork/cc-pc-tools-rs", BaseRepository: repo, BaseRef: "main"},
		{Repository: repo, Revision: head, Ref: valid.Ref, Event: valid.Event, HeadSHA: head, HeadRef: valid.HeadRef, HeadRepository: repo, BaseRepository: "other/repo", BaseRef: "main"},
		{Repository: repo, Revision: head, Ref: valid.Ref, Event: valid.Event, HeadSHA: head, HeadRef: valid.HeadRef, HeadRepository: repo, BaseRepository: repo, BaseRef: "release"},
		{Repository: "other/repo", Revision: head, Ref: valid.Ref, Event: valid.Event, HeadSHA: head, HeadRef: valid.HeadRef, HeadRepository: repo, BaseRepository: repo, BaseRef: "main"},
	} {
		if h.authorizeNormal(invalid) == nil {
			t.Fatalf("accepted invalid identity %+v", invalid)
		}
	}
}

func TestNormalPushRequiresCurrentMainHead(t *testing.T) {
	head := strings.Repeat("c", 40)
	h := host{mode: "normal", getHead: func(branch string) (string, error) {
		if branch != "main" {
			t.Fatalf("unexpected branch lookup %q", branch)
		}
		return head, nil
	}}
	valid := request{Repository: repo, Revision: head, Ref: "refs/heads/main", Event: "push", HeadSHA: head, HeadRef: "main", HeadRepository: repo, BaseRepository: repo}
	if err := h.authorizeNormal(valid); err != nil {
		t.Fatal(err)
	}
	valid.HeadSHA = strings.Repeat("d", 40)
	valid.Revision = valid.HeadSHA
	if h.authorizeNormal(valid) == nil {
		t.Fatal("accepted a non-current main commit")
	}
}

func TestScheduledCodeQLRequiresCurrentMainHead(t *testing.T) {
	head := strings.Repeat("c", 40)
	path := t.TempDir() + "/event.json"
	if err := os.WriteFile(path, []byte(`{"repository":{"full_name":"AutoDevJapan/cc-pc-tools-rs"},"schedule":"0 22 * * 0"}`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CC_LINUX_RUNNER_MODE", "normal")
	t.Setenv("GITHUB_REPOSITORY", repo)
	t.Setenv("GITHUB_EVENT_NAME", "schedule")
	t.Setenv("GITHUB_EVENT_PATH", path)
	t.Setenv("GITHUB_REF", "refs/heads/main")
	t.Setenv("GITHUB_SHA", head)
	r, ok := jobRequestFromEnv()
	if !ok {
		t.Fatal("scheduled main identity was rejected")
	}
	h := host{mode: "normal", getHead: func(branch string) (string, error) {
		if branch != "main" {
			return "", errors.New("unexpected scheduled branch")
		}
		return head, nil
	}}
	if err := h.authorizeNormal(r); err != nil {
		t.Fatal(err)
	}
	r.HeadSHA, r.Revision = strings.Repeat("d", 40), strings.Repeat("d", 40)
	if h.authorizeNormal(r) == nil {
		t.Fatal("scheduled job accepted a stale main SHA")
	}
	t.Setenv("GITHUB_REF", "refs/heads/feature/linux-ci")
	if _, ok := jobRequestFromEnv(); ok {
		t.Fatal("scheduled job accepted a feature branch")
	}
	if err := os.WriteFile(path, []byte(`{"repository":{"full_name":"other/repo"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GITHUB_REF", "refs/heads/main")
	if _, ok := jobRequestFromEnv(); ok {
		t.Fatal("scheduled job accepted another repository")
	}
}

func TestBridgePortPreservesFiniteAndNormalListeners(t *testing.T) {
	if got := bridgeURL("finite", "/authorize"); got != "http://host.docker.internal:17851/authorize" {
		t.Fatalf("finite bridge URL: %s", got)
	}
	if got := bridgeURL("normal", "/authorize"); got != "http://host.docker.internal:17852/authorize" {
		t.Fatalf("normal bridge URL: %s", got)
	}
}

func TestJobRequestFromEnvUsesPRHeadNotMergeSHA(t *testing.T) {
	path := t.TempDir() + "/event.json"
	if err := os.WriteFile(path, []byte(`{"repository":{"full_name":"AutoDevJapan/cc-pc-tools-rs"},"pull_request":{"head":{"sha":"`+strings.Repeat("e", 40)+`","ref":"feature/linux-ci","repo":{"full_name":"AutoDevJapan/cc-pc-tools-rs"}},"base":{"ref":"main","repo":{"full_name":"AutoDevJapan/cc-pc-tools-rs"}}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CC_LINUX_RUNNER_MODE", "normal")
	t.Setenv("GITHUB_REPOSITORY", repo)
	t.Setenv("GITHUB_EVENT_NAME", "pull_request")
	t.Setenv("GITHUB_EVENT_PATH", path)
	t.Setenv("GITHUB_REF", "refs/pull/17/merge")
	t.Setenv("GITHUB_SHA", strings.Repeat("f", 40))
	r, ok := jobRequestFromEnv()
	if !ok {
		t.Fatal("did not parse pull request identity")
	}
	if r.HeadSHA != strings.Repeat("e", 40) || r.Revision != r.HeadSHA || r.Revision == os.Getenv("GITHUB_SHA") {
		t.Fatalf("used merge revision: %+v", r)
	}
}
func TestUnauthorizedRequestCannotInvokeGate(t *testing.T) {
	h := host{key: "secret"}
	r := httptest.NewRequest("POST", "/cargo", strings.NewReader(`{"Dir":"/work/cc-pc-tools-rs/cc-pc-tools-rs","Args":["test"]}`))
	w := httptest.NewRecorder()
	h.serve(w, r)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
	r = httptest.NewRequest("POST", "/cargo", strings.NewReader(`{"unknown":1}`))
	r.Header.Set("Authorization", "Bearer secret")
	w = httptest.NewRecorder()
	h.serve(w, r)
	if w.Code != 400 {
		t.Fatal(w.Code)
	}
}
func TestJobIdentity(t *testing.T) {
	h := host{revision: strings.Repeat("a", 40), branch: "scheduler/todo-2511-test"}
	for _, q := range []request{{Repository: "other/repo"}, {Repository: repo, Revision: h.revision, Ref: "refs/heads/other"}, {Repository: repo, Revision: h.revision, Ref: "refs/heads/" + h.branch, Event: "pull_request"}} {
		if h.authorize(q) == nil {
			t.Fatal("unexpected authorized job")
		}
	}
}
