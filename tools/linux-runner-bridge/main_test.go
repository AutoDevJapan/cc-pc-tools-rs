package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestDockerExecuteForwardsOnlyTheFixedNormalMode(t *testing.T) {
	for _, mode := range []string{"normal", "finite"} {
		args := dockerExecuteArgs(mode, "2", "3", []string{"check", "--locked"})
		joined := strings.Join(args, "\n")
		if strings.Contains(joined, "CC_LINUX_RUNNER_MODE=normal") != (mode == "normal") {
			t.Fatal("normal scratch mode lost across Cargo gate")
		}
		if !strings.Contains(joined, "execute\n2\n3\ncheck\n--locked") {
			t.Fatal("admission caps or arguments changed")
		}
		if strings.Contains(joined, "TMPDIR=") {
			t.Fatal("host supplied a scratch path")
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
					if joined != "exec\n-d\ntest-container\n/opt/bridge/bridge\nsupervise" {
						return "", errors.New("recovery did not use the single cycle supervisor")
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


func projectionVersionFixture(tool, release string) []byte {
	return []byte(fmt.Sprintf("%s %s (abcdef012 2026-01-01)\nrelease: %s\nhost: x86_64-unknown-linux-gnu\n", tool, release, release))
}

func TestProjectionToolchainClosedArguments(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		required, observation bool
		valid bool
	}{
		{"legacy", []string{"check", "--locked"}, false, false, true},
		{"metadata", []string{"metadata", projectionRustRequirement, "--locked"}, true, false, true},
		{"test", []string{"test", projectionRustRequirement, "-p", "cc-jobs"}, true, false, true},
		{"clamped", []string{"test", "--jobs=2", projectionRustRequirement, "--", "--test-threads=1"}, true, false, true},
		{"observe", []string{"metadata", projectionRustObservation}, true, true, true},
		{"other-version", []string{"metadata", "--ci-toolchain=1.90.0"}, false, false, false},
		{"unknown", []string{"metadata", "--ci-toolchain-other"}, false, false, false},
		{"duplicate", []string{"metadata", projectionRustRequirement, projectionRustRequirement}, false, false, false},
		{"harness", []string{"test", "--", projectionRustRequirement}, false, false, true},
		{"opaque-skip", []string{"test", "--", "--skip", "--ci-toolchain-legacy"}, false, false, true},
		{"required-opaque-skip", []string{"test", projectionRustRequirement, "--", "--skip", "--ci-toolchain-legacy"}, true, false, true},
		{"extra-observe", []string{"metadata", projectionRustObservation, "--locked"}, false, false, false},
		{"wrong-command", []string{"clippy", projectionRustRequirement}, false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clean, required, observation, err := splitProjectionToolchainArgs(tc.args)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v error=%v", tc.valid, err)
			}
			if !tc.valid { return }
			if required != tc.required || observation != tc.observation {
				t.Fatalf("policy changed: %v %v", required, observation)
			}
			for _, arg := range clean {
				if arg == "--" { break }
				if strings.HasPrefix(arg, "--ci-toolchain") { t.Fatal("internal marker reached Cargo options") }
			}
			if err := validate(request{Dir: "/work/cc-pc-tools-rs/cc-pc-tools-rs", Args: tc.args}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestProjectionToolchainRequestOnlyAddsFixedRestriction(t *testing.T) {
	original := []string{"metadata", "--locked", "--no-deps", "--format-version", "1"}
	got, err := projectionToolchainRequest(original, projectionRustRelease)
	if err != nil || !reflect.DeepEqual(got, append([]string{"metadata", projectionRustRequirement}, original[1:]...)) {
		t.Fatalf("fixed request=%v %v", got, err)
	}
	if original[1] != "--locked" { t.Fatal("caller arguments mutated") }
	for _, wrong := range []string{"true", "1.90.0", "1.95.0\n", "/other/path"} {
		if _, err := projectionToolchainRequest(original, wrong); err == nil {
			t.Fatalf("accepted arbitrary requirement %q", wrong)
		}
	}
	for _, args := range [][]string{{"check", "--locked"}, {"--version"}} {
		got, err := projectionToolchainRequest(args, "")
		if err != nil || !reflect.DeepEqual(got, args) { t.Fatal("legacy request changed") }
	}
	external, err := projectionToolchainRequest([]string{projectionRustObservation}, projectionRustRelease)
	if err != nil || !reflect.DeepEqual(external, []string{"metadata", projectionRustObservation}) { t.Fatal("external probe not bound to updated frontend") }
	probe := []string{"metadata", projectionRustObservation}
	got, err = projectionToolchainRequest(probe, projectionRustRelease)
	if err != nil || !reflect.DeepEqual(got, probe) { t.Fatal("probe marker duplicated") }
}

func TestProjectionCargoUsesClosedExecutionContext(t *testing.T) {
	t.Setenv("PATH", "/wrong/ambient")
	t.Setenv("RUSTUP_TOOLCHAIN", "1.90.0")
	t.Setenv("RUSTC", "/wrong/rustc")
	t.Setenv("RUSTC_WRAPPER", "/wrong/wrapper")
	cmd := projectionCargoCommand([]string{"test", "--locked"}, "2", "3", true, []string{"TMPDIR=/owned/scratch"})
	if cmd.Path != "/opt/rust/bin/cargo" || cmd.Dir != "/work/cc-pc-tools-rs/cc-pc-tools-rs" {
		t.Fatal("ambient executable or workspace was selected")
	}
	env := strings.Join(cmd.Env, "\n")
	for _, expected := range []string{"CARGO_BUILD_JOBS=2", "RUST_TEST_THREADS=3", "RUSTUP_TOOLCHAIN=1.95.0", "RUSTUP_AUTO_INSTALL=0", "RUSTC=/opt/rust/bin/rustc", "TMPDIR=/owned/scratch"} {
		if !strings.Contains(env, expected) { t.Fatalf("missing fixed context %s", expected) }
	}
	if strings.Contains(env, "/wrong/") { t.Fatal("ambient environment crossed execution boundary") }
	legacy := projectionCargoCommand([]string{"check"}, "1", "1", false, nil)
	if strings.Contains(strings.Join(legacy.Env, "\n"), "RUSTUP_TOOLCHAIN=") {
		t.Fatal("ordinary Cargo acquired a new version requirement")
	}
}

func TestProjectionVersionsUseTheActualCargoContext(t *testing.T) {
	template := projectionCargoCommand([]string{"test", "--locked"}, "2", "3", true, []string{"TMPDIR=/owned/scratch"})
	before := append([]string(nil), template.Env...)
	var paths []string
	result, err := checkProjectionToolchainWith(context.Background(), template, func(ctx context.Context, cmd *exec.Cmd) ([]byte, error) {
		if ctx.Err() != nil || cmd.Dir != template.Dir || !reflect.DeepEqual(cmd.Env, before) {
			t.Fatal("probe and real execution context differ")
		}
		if !reflect.DeepEqual(cmd.Args[1:], []string{"--version", "--verbose"}) {
			t.Fatal("probe accepted command arguments")
		}
		paths = append(paths, cmd.Path)
		tool := "cargo"
		if cmd.Path == "/opt/rust/bin/rustc" { tool = "rustc" } else if cmd.Path != "/opt/rust/bin/cargo" { t.Fatal("unexpected executable") }
		cmd.Env[0] = "HOME=/mutated-test-copy"
		return projectionVersionFixture(tool, projectionRustRelease), nil
	})
	if err != nil || result.Cargo != projectionRustRelease || result.Rustc != projectionRustRelease {
		t.Fatalf("fixed versions rejected: %v %v", result, err)
	}
	if !reflect.DeepEqual(paths, []string{"/opt/rust/bin/cargo", "/opt/rust/bin/rustc"}) || !reflect.DeepEqual(template.Env, before) {
		t.Fatal("probe changed native command identity/context")
	}
}

func TestProjectionVersionsRejectOldIncompleteAndFailedInputs(t *testing.T) {
	for _, tc := range []struct{name, cargo, rustc string; invalid string}{
		{"cargo190", "1.90.0", "1.95.0", ""},
		{"rustc190", "1.95.0", "1.90.0", ""},
		{"nightly", "1.95.0-nightly", "1.95.0", ""},
		{"missing", "1.95.0", "1.95.0", "missing"},
		{"empty", "1.95.0", "1.95.0", "empty"},
		{"duplicate", "1.95.0", "1.95.0", "duplicate"},
		{"mixed", "1.95.0", "1.95.0", "mixed"},
		{"oversize", "1.95.0", "1.95.0", "oversize"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			template := projectionCargoCommand([]string{"test"}, "1", "1", true, nil)
			_, err := checkProjectionToolchainWith(context.Background(), template, func(_ context.Context, cmd *exec.Cmd) ([]byte, error) {
				if tc.invalid == "missing" { return nil, os.ErrNotExist }
				tool, release := "cargo", tc.cargo
				if cmd.Path == "/opt/rust/bin/rustc" { tool, release = "rustc", tc.rustc }
				raw := projectionVersionFixture(tool, release)
				switch tc.invalid {
				case "empty": raw = nil
				case "duplicate": raw = append(raw, []byte("release: 1.95.0\n")...)
				case "mixed": raw = []byte(tool+" 1.95.0 (abcdef012 2026-01-01)\nrelease: 1.90.0\n")
				case "oversize": raw = append(raw, bytes.Repeat([]byte{'S'}, projectionVersionLimit)...)
				}
				return raw, nil
			})
			if err == nil { t.Fatal("unqualified toolchain accepted") }
		})
	}
}

func TestProjectionExecutionRechecksBeforeActualChild(t *testing.T) {
	version := projectionRustRelease
	observed, started := 0, 0
	read := func(_ context.Context, cmd *exec.Cmd) ([]byte, error) {
		observed++
		tool := "cargo"
		if cmd.Path == "/opt/rust/bin/rustc" { tool = "rustc" }
		return projectionVersionFixture(tool, version), nil
	}
	run := func(_ context.Context, cmd *exec.Cmd) int {
		started++
		if strings.Contains(strings.Join(cmd.Args, "\n"), "--ci-toolchain") { t.Fatal("marker passed to real Cargo") }
		return 0
	}
	var out, diagnostic bytes.Buffer
	if code := executeProjectionCargo(context.Background(), []string{"metadata", projectionRustObservation}, "1", "1", nil, &out, &diagnostic, read, run); code != 0 || started != 0 {
		t.Fatal("metadata probe started Cargo work or failed")
	}
	if observed != 2 { t.Fatal("both tools were not observed") }
	version = "1.90.0"
	out.Reset(); diagnostic.Reset()
	code := executeProjectionCargo(context.Background(), []string{"test", projectionRustRequirement, "-p", "cc-jobs"}, "2", "3", nil, &out, &diagnostic, read, run)
	if code == 0 || started != 0 || out.Len() != 0 || !strings.Contains(diagnostic.String(), "linux-toolchain-mismatch") {
		t.Fatal("old probe receipt authorized a later wrong-version child")
	}
	version = projectionRustRelease
	diagnostic.Reset()
	if code := executeProjectionCargo(context.Background(), []string{"test", projectionRustRequirement, "-p", "cc-jobs"}, "2", "3", nil, &out, &diagnostic, read, run); code != 0 || started != 1 {
		t.Fatal("fixed toolchain did not reach the real execution boundary")
	}
	if !strings.Contains(diagnostic.String(), "linux-toolchain-verified cargo=1.95.0 rustc=1.95.0") {
		t.Fatal("required child acknowledgement missing")
	}
}

func TestProjectionLegacyCargoDoesNotAcquireNewProbe(t *testing.T) {
	calls := 0
	code := executeProjectionCargo(context.Background(), []string{"check", "--locked"}, "2", "3", nil, io.Discard, io.Discard,
		func(context.Context, *exec.Cmd) ([]byte, error) { t.Fatal("legacy Cargo was probed"); return nil, os.ErrInvalid },
		func(context.Context, *exec.Cmd) int { calls++; return 7 })
	if code != 7 || calls != 1 { t.Fatal("legacy result or execution changed") }
}

func TestProjectionVersionCaptureBoundedAndDrainable(t *testing.T) {
	var output projectionVersionOutput
	input := bytes.Repeat([]byte{'S'}, projectionVersionLimit+1)
	if n, err := output.Write(input); n != len(input) || err != nil || !output.overflow || len(output.data) != projectionVersionLimit {
		t.Fatal("capture did not bound retained bytes while draining")
	}
	if n, err := output.Write([]byte("later")); n != 5 || err != nil || len(output.data) != projectionVersionLimit {
		t.Fatal("overflow stopped draining")
	}
}

type projectionShortWriter struct{}
func (projectionShortWriter) Write(p []byte) (int, error) {
	if len(p) == 0 { return 0, nil }
	return len(p)-1, nil
}

func TestProjectionVersionMetadataIsClosedAndChecksShortWrites(t *testing.T) {
	result := projectionToolchainVersions{Domain:"cc-pc-tools-linux-toolchain-v1", Cargo:projectionRustRelease, Rustc:projectionRustRelease}
	if writeProjectionVersions(projectionShortWriter{}, result) == nil { t.Fatal("short metadata write accepted") }
	var output bytes.Buffer
	if err := writeProjectionVersions(&output, result); err != nil { t.Fatal(err) }
	var value map[string]string
	if json.Unmarshal(output.Bytes(), &value) != nil || len(value) != 3 || value["domain"] != result.Domain || value["cargo_release"] != result.Cargo || value["rustc_release"] != result.Rustc {
		t.Fatal("metadata schema changed")
	}
}

func TestProjectionVersionChild(t *testing.T) {
	mode := os.Getenv("CC_BRIDGE_VERSION_FIXTURE")
	if mode == "" { t.Skip("owned version-capture child only") }
	switch mode {
	case "ok":
		fmt.Print(string(projectionVersionFixture("cargo", projectionRustRelease)))
		os.Exit(0)
	case "oversize":
		fmt.Print(strings.Repeat("S", projectionVersionLimit+1))
		os.Exit(0)
	case "failed":
		fmt.Print(string(projectionVersionFixture("cargo", projectionRustRelease)))
		os.Exit(7)
	case "wait":
		time.Sleep(30*time.Second)
		os.Exit(0)
	}
	os.Exit(8)
}

func TestProjectionVersionActualCaptureControls(t *testing.T) {
	exe, err := os.Executable()
	if err != nil { t.Fatal(err) }
	for _, mode := range []string{"ok", "oversize", "failed", "wait", "missing"} {
		t.Run(mode, func(t *testing.T) {
			path := exe
			if mode == "missing" { path = t.TempDir()+"/missing-version-tool" }
			template := exec.Command(path, "-test.run=^TestProjectionVersionChild$")
			template.Dir = t.TempDir()
			template.Env = append(os.Environ(), "CC_BRIDGE_VERSION_FIXTURE="+mode)
			deadline := 10*time.Second
			if mode == "wait" { deadline = 30*time.Millisecond }
			ctx, cancel := context.WithTimeout(context.Background(), deadline)
			defer cancel()
			raw, err := runProjectionVersion(ctx, template)
			if mode == "ok" {
				if err != nil { t.Fatal(err) }
				if _, err := projectionVersionRelease(raw, "cargo"); err != nil { t.Fatal(err) }
			} else if err == nil || len(raw) != 0 {
				t.Fatal("failed/incomplete native capture was accepted")
			}
		})
	}
}

func TestProjectionMarkerShortWriteDoesNotStartChild(t *testing.T) {
	started := 0
	code := executeProjectionCargo(context.Background(), []string{"test", projectionRustRequirement}, "1", "1", nil, io.Discard, projectionShortWriter{}, func(_ context.Context, cmd *exec.Cmd) ([]byte, error) {
		tool := "cargo"
		if cmd.Path == "/opt/rust/bin/rustc" { tool = "rustc" }
		return projectionVersionFixture(tool, projectionRustRelease), nil
	}, func(context.Context, *exec.Cmd) int { started++; return 0 })
	if code == 0 || started != 0 { t.Fatal("incomplete child acknowledgement accepted") }
}


func projectionInvocationFixture(t *testing.T) (*projectionInvocation, *os.File) {
	t.Helper()
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	owner, err := beginProjectionInvocation(read)
	if err != nil {
		read.Close()
		write.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		write.Close()
		if err := owner.Close(); err != nil {
			t.Error(err)
		}
	})
	return owner, write
}

func projectionCloseInvocationInput(t *testing.T, owner *projectionInvocation, write *os.File) {
	t.Helper()
	if err := write.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-owner.ctx.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("owned stdin EOF was not observed before the next invocation stage")
	}
}

func TestProjectionInvocationPositiveOwnsBothProbesMarkerAndChild(t *testing.T) {
	owner, _ := projectionInvocationFixture(t)
	probes, starts := 0, 0
	var diagnostic bytes.Buffer
	code := executeProjectionCargo(owner.ctx, []string{"test", projectionRustRequirement}, "2", "3", nil, io.Discard, &diagnostic,
		func(probeCtx context.Context, cmd *exec.Cmd) ([]byte, error) {
			probes++
			if owner.ctx.Err() != nil || probeCtx.Err() != nil {
				t.Fatal("normal invocation was cancelled")
			}
			tool := "cargo"
			if cmd.Path == "/opt/rust/bin/rustc" {
				tool = "rustc"
			}
			return projectionVersionFixture(tool, projectionRustRelease), nil
		},
		func(childCtx context.Context, cmd *exec.Cmd) int {
			starts++
			if childCtx != owner.ctx || childCtx.Err() != nil {
				t.Fatal("actual child lost the invocation context")
			}
			if cmd.Dir != "/work/cc-pc-tools-rs/cc-pc-tools-rs" {
				t.Fatal("actual child moved to another workspace")
			}
			return 0
		})
	if code != 0 || probes != 2 || starts != 1 {
		t.Fatalf("normal path code=%d probes=%d starts=%d", code, probes, starts)
	}
	if diagnostic.String() != "linux-toolchain-verified cargo=1.95.0 rustc=1.95.0\n" {
		t.Fatal("normal invocation did not emit exactly one bounded marker")
	}
}

func TestProjectionInvocationEOFDuringEachProbeStopsBeforeNextStage(t *testing.T) {
	for _, during := range []int{1, 2} {
		t.Run(fmt.Sprintf("probe%d", during), func(t *testing.T) {
			owner, write := projectionInvocationFixture(t)
			probes, starts := 0, 0
			var diagnostic bytes.Buffer
			code := executeProjectionCargo(owner.ctx, []string{"test", projectionRustRequirement}, "2", "3", nil, io.Discard, &diagnostic,
				func(probeCtx context.Context, cmd *exec.Cmd) ([]byte, error) {
					probes++
					if probes == during {
						projectionCloseInvocationInput(t, owner, write)
						select {
						case <-probeCtx.Done():
						case <-time.After(3*time.Second):
							t.Fatal("version probe was detached from stdin cancellation")
						}
					}
					tool := "cargo"
					if cmd.Path == "/opt/rust/bin/rustc" {
						tool = "rustc"
					}
					// A successful version result cannot hide the invocation EOF.
					return projectionVersionFixture(tool, projectionRustRelease), nil
				},
				func(context.Context, *exec.Cmd) int {
					starts++
					return 0
				})
			if code != 125 || probes != during || starts != 0 || diagnostic.Len() != 0 {
				t.Fatalf("cancel window accepted code=%d probes=%d starts=%d diagnostic=%q", code, probes, starts, diagnostic.String())
			}
		})
	}
}

type projectionEOFMarkerWriter struct {
	t *testing.T
	owner *projectionInvocation
	write *os.File
	writes int
}

func (w *projectionEOFMarkerWriter) Write(p []byte) (int, error) {
	w.writes++
	projectionCloseInvocationInput(w.t, w.owner, w.write)
	return len(p), nil
}

func TestProjectionInvocationEOFAfterProbesDoesNotStartActualChild(t *testing.T) {
	owner, write := projectionInvocationFixture(t)
	probes, starts := 0, 0
	marker := &projectionEOFMarkerWriter{t:t, owner:owner, write:write}
	code := executeProjectionCargo(owner.ctx, []string{"test", projectionRustRequirement}, "2", "3", nil, io.Discard, marker,
		func(probeCtx context.Context, cmd *exec.Cmd) ([]byte, error) {
			probes++
			if probeCtx.Err() != nil {
				t.Fatal("probe cancelled before the injected EOF")
			}
			tool := "cargo"
			if cmd.Path == "/opt/rust/bin/rustc" {
				tool = "rustc"
			}
			return projectionVersionFixture(tool, projectionRustRelease), nil
		},
		func(context.Context, *exec.Cmd) int {
			starts++
			return 0
		})
	if code != 125 || probes != 2 || marker.writes != 1 || starts != 0 {
		t.Fatalf("post-probe cancellation accepted code=%d probes=%d markers=%d starts=%d", code, probes, marker.writes, starts)
	}
}

func TestProjectionInvocationAlreadyCancelledStartsNothing(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	probes, starts := 0, 0
	code := executeProjectionCargo(ctx, []string{"test", projectionRustRequirement}, "1", "1", nil, io.Discard, io.Discard,
		func(context.Context, *exec.Cmd) ([]byte, error) {
			probes++
			return nil, os.ErrInvalid
		},
		func(context.Context, *exec.Cmd) int {
			starts++
			return 0
		})
	if code != 125 || probes != 0 || starts != 0 {
		t.Fatal("cancelled invocation began new work")
	}
}

func TestProjectionInvocationClosesAndJoinsWatcherOnce(t *testing.T) {
	owner, _ := projectionInvocationFixture(t)
	closeDone := make(chan error, 2)
	go func() { closeDone <- owner.Close() }()
	go func() { closeDone <- owner.Close() }()
	for i:=0; i<2; i++ {
		select {
		case err := <-closeDone:
			if err != nil { t.Fatal(err) }
		case <-time.After(3*time.Second):
			t.Fatal("invocation watcher was not joined")
		}
	}
	select {
	case <-owner.done:
	default:
		t.Fatal("Close returned before watcher termination")
	}
	if owner.ctx.Err() == nil {
		t.Fatal("Close did not invalidate invocation")
	}
}

func TestProjectionActualRunnerRejectsCancelledStart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	path := t.TempDir()+"/missing-child"
	if code := executeCargoCommand(ctx, exec.Command(path)); code != 125 {
		t.Fatalf("native Start ignored prior cancellation: %d", code)
	}
}

func TestProjectionHarnessTailRemainsOpaqueAtActualBoundary(t *testing.T) {
	tail := []string{"--", "--skip", "--ci-toolchain-legacy", projectionRustRequirement}
	for _, required := range []bool{false, true} {
		name := "legacy"
		args := []string{"test"}
		if required {
			name = "required"
			args = append(args, projectionRustRequirement)
		}
		t.Run(name, func(t *testing.T) {
			args = append(args, tail...)
			probes, starts := 0, 0
			code := executeProjectionCargo(context.Background(), args, "2", "3", nil, io.Discard, io.Discard,
				func(_ context.Context, cmd *exec.Cmd) ([]byte, error) {
					probes++
					tool := "cargo"
					if cmd.Path == "/opt/rust/bin/rustc" { tool = "rustc" }
					return projectionVersionFixture(tool, projectionRustRelease), nil
				},
				func(_ context.Context, cmd *exec.Cmd) int {
					starts++
					expected := append([]string{"test"}, tail...)
					if !reflect.DeepEqual(cmd.Args[1:], expected) {
						t.Fatalf("harness data changed: %v", cmd.Args)
					}
					return 7
				})
			wantProbes := 0
			if required { wantProbes = 2 }
			if code != 7 || starts != 1 || probes != wantProbes {
				t.Fatal("opaque harness values changed acceptance or execution count")
			}
		})
	}
}

func TestProjectionInputAlreadyEOFDoesNotWaitForNewWriter(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("fixed Linux execute descriptor boundary")
	}
	read, write, err := os.Pipe()
	if err != nil { t.Fatal(err) }
	defer read.Close()
	if err := write.Close(); err != nil { t.Fatal(err) }
	type openedInput struct { input *os.File; err error }
	result := make(chan openedInput, 1)
	go func() {
		input, err := openProjectionInvocationInput(read)
		result <- openedInput{input, err}
	}()
	select {
	case opened := <-result:
		if opened.err != nil { t.Fatal(opened.err) }
		owner, err := beginProjectionInvocation(opened.input)
		if err != nil { opened.input.Close(); t.Fatal(err) }
		defer owner.Close()
		select {
		case <-owner.ctx.Done():
		case <-time.After(3*time.Second):
			t.Fatal("pre-existing EOF was not delivered to the invocation")
		}
	case <-time.After(3*time.Second):
		t.Fatal("stdin duplicate waited for a writer instead of observing EOF")
	}
}


// Host admission remains stricter than Cargo's opaque harness tail. This
// regression exercises the same validate -> execution-boundary ordering.
func TestProjectionHostValidatedHarnessTailPreservedAtExecution(t *testing.T) {
	tail := []string{"--", "--skip", "--ci-toolchain-legacy", projectionRustRequirement}
	for _, required := range []bool{false, true} {
		name := "legacy"
		args := []string{"test"}
		if required {
			name = "required"
			args = append(args, projectionRustRequirement)
		}
		t.Run(name, func(t *testing.T) {
			args = append(args, tail...)
			q := request{Dir: "/work/cc-pc-tools-rs/cc-pc-tools-rs", Args: args}
			if err := validate(q); err != nil {
				t.Fatal(err)
			}
			probes, executions := 0, 0
			code := executeProjectionCargo(context.Background(), q.Args, "2", "3", nil, io.Discard, io.Discard,
				func(_ context.Context, cmd *exec.Cmd) ([]byte, error) {
					probes++
					return projectionVersionFixture(filepath.Base(cmd.Path), projectionRustRelease), nil
				},
				func(_ context.Context, cmd *exec.Cmd) int {
					executions++
					expected := append([]string{"test"}, tail...)
					if !reflect.DeepEqual(cmd.Args[1:], expected) {
						t.Fatalf("host-accepted harness tail changed: %v", cmd.Args[1:])
					}
					return 7
				})
			expectedProbes := 0
			if required {
				expectedProbes = 2
			}
			if code != 7 || executions != 1 || probes != expectedProbes {
				t.Fatal("host-accepted tail changed execution or version policy")
			}
		})
	}
	for _, value := range []string{"--config=other", "--target-dir=/tmp/other", "-C/tmp", "+nightly", "@other", "line\nbreak"} {
		t.Run("host-reject-"+strings.ReplaceAll(value, "/", "_"), func(t *testing.T) {
			args := []string{"test", projectionRustRequirement, "--", value}
			q := request{Dir: "/work/cc-pc-tools-rs/cc-pc-tools-rs", Args: args}
			// Passing this request into h.serve would otherwise reach real host
			// tools. Count the boundary only after its same production validator.
			probes, executions := 0, 0
			err := validate(q)
			if err == nil {
				_ = executeProjectionCargo(context.Background(), q.Args, "2", "3", nil, io.Discard, io.Discard,
					func(context.Context, *exec.Cmd) ([]byte, error) {
						probes++
						return nil, errProjectionToolchain
					},
					func(context.Context, *exec.Cmd) int {
						executions++
						return 0
					})
			}
			if err == nil || probes != 0 || executions != 0 {
				t.Fatal("host rejection crossed the probe or execution boundary")
			}
		})
	}
}
