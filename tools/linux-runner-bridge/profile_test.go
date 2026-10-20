package main

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestNormalProfilesSeparateQueuedJobExecution(t *testing.T) {
	primary, err := normalProfileFor("primary")
	if err != nil {
		t.Fatal(err)
	}
	burst, err := normalProfileFor("burst-3122")
	if err != nil {
		t.Fatal(err)
	}
	if primary.container == burst.container || primary.listen == burst.listen || primary.runner == burst.runner || primary.image == burst.image || primary.targetDir == burst.targetDir {
		t.Fatal("simultaneous runners share execution state or registration identity")
	}
	if primary.label != burst.label || primary.label != "cc-pc-tools-linux" {
		t.Fatal("additional runner cannot receive the already queued labels")
	}
	if primary.ephemeral || primary.maxCycles != 0 || !burst.ephemeral || burst.maxCycles != 1 {
		t.Fatal("burst runner must finish after one real job and leave the primary resident")
	}
	for _, invalid := range []string{"", "burst-3123", "../../../other", "primary\n"} {
		if _, err := normalProfileFor(invalid); err == nil {
			t.Fatalf("arbitrary runner profile accepted: %q", invalid)
		}
	}
}

func TestBurstDoesNotRetryEitherCompletedOrFailedOneJob(t *testing.T) {
	for _, exit := range []int{0, 1, 96} {
		calls, retries := 0, 0
		code, err := superviseCycles(1, func() (int, error) { calls++; return exit, nil }, func(time.Duration) { retries++ })
		if err != nil || code != exit || calls != 1 || retries != 0 {
			t.Fatalf("one-job limit lost: code=%d calls=%d retries=%d err=%v", code, calls, retries, err)
		}
	}
	stop := errors.New("fixture supervisor stopped")
	calls, retries := 0, 0
	_, err := superviseCycles(0, func() (int, error) {
		calls++
		if calls == 3 {
			return 1, stop
		}
		if calls == 1 {
			return 1, nil
		}
		return 0, nil
	}, func(d time.Duration) {
		if d != 30*time.Second {
			t.Fatal("resident retry delay changed")
		}
		retries++
	})
	if !errors.Is(err, stop) || calls != 3 || retries != 1 {
		t.Fatal("primary resident lifecycle changed")
	}
}

func TestCompiledNormalProfileRoutesHookAndCargoTogether(t *testing.T) {
	for _, profile := range []string{"primary", "burst-3122"} {
		p, err := normalProfileFor(profile)
		if err != nil {
			t.Fatal(err)
		}
		args := dockerExecuteArgsForProfile(p, "normal", "2", "3", []string{"check", "--locked"})
		if !strings.Contains(strings.Join(args, "\n"), "\n"+p.container+"\n") {
			t.Fatal("Cargo was sent to a different runner's container")
		}
		if bridgeURLForProfile(p, "normal", "/authorize") != "http://host.docker.internal:"+strings.TrimPrefix(p.listen, "127.0.0.1:")+"/authorize" {
			t.Fatal("hook was sent to a different runner's host bridge")
		}
		if bridgeURLForProfile(p, "finite", "/authorize") != "http://host.docker.internal:17851/authorize" {
			t.Fatal("original finite runner was changed")
		}
	}
}

func TestBurstRegistrationCannotAdoptTheFiniteOrResidentIdentity(t *testing.T) {
	primary, _ := normalProfileFor("primary")
	burst, _ := normalProfileFor("burst-3122")
	for _, test := range []struct {
		profile         normalProfile
		name, ephemeral string
		allowed         bool
	}{
		{primary, primary.runner, "false", true},
		{primary, "cc-pc-tools-linux-2511", "true", true},
		{burst, burst.runner, "true", true},
		{burst, burst.runner, "false", false},
		{burst, "cc-pc-tools-linux-2511", "true", false},
		{burst, primary.runner, "true", false},
	} {
		if got := validRegistration(test.profile, test.name, normalLabel, test.ephemeral); got != test.allowed {
			t.Fatalf("registration name=%s ephemeral=%s accepted=%v", test.name, test.ephemeral, got)
		}
	}
}

func TestBurstContainerBoundsAreValidatedBeforeRegistration(t *testing.T) {
	burst, _ := normalProfileFor("burst-3122")
	valid, err := inspectNormalContainer(validNormalContainerInspection)
	if err != nil {
		t.Fatal(err)
	}
	valid.Config.Image = burst.image
	valid.HostConfig.NanoCPUs, valid.HostConfig.Memory, valid.HostConfig.PidsLimit = 4_000_000_000, 8<<30, 1024
	if err := validateNormalContainerForProfile(valid, burst); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*normalContainerInspection){
		func(c *normalContainerInspection) { c.HostConfig.NanoCPUs = 0 },
		func(c *normalContainerInspection) { c.HostConfig.NanoCPUs++ },
		func(c *normalContainerInspection) { c.HostConfig.Memory = 0 },
		func(c *normalContainerInspection) { c.HostConfig.Memory++ },
		func(c *normalContainerInspection) { c.HostConfig.PidsLimit = 0 },
		func(c *normalContainerInspection) { c.HostConfig.PidsLimit++ },
	} {
		bad := valid
		mutate(&bad)
		if validateNormalContainerForProfile(bad, burst) == nil {
			t.Fatal("unlimited or oversized additional runner accepted")
		}
	}
}
