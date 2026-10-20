package main

import (
	"strings"
	"testing"
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
	if primary.container == burst.container || primary.listen == burst.listen || primary.runner == burst.runner || primary.image == burst.image {
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

func TestCompiledNormalProfileRoutesHookAndCargoTogether(t *testing.T) {
	p, err := normalProfileFor(normalBuildProfile)
	if err != nil {
		t.Fatal(err)
	}
	args := dockerExecuteArgs("normal", "2", "3", []string{"check", "--locked"})
	if !strings.Contains(strings.Join(args, "\n"), "\n"+p.container+"\n") {
		t.Fatal("Cargo was sent to a different runner's container")
	}
	if bridgeURL("normal", "/authorize") != "http://host.docker.internal:"+strings.TrimPrefix(p.listen, "127.0.0.1:")+"/authorize" {
		t.Fatal("hook was sent to a different runner's host bridge")
	}
	t.Setenv("CC_LINUX_RUNNER_PROFILE", "untrusted-job-override")
	if normalContainerFor("normal") != p.container {
		t.Fatal("job environment redirected the compiled runner profile")
	}
	if normalContainerFor("finite") != container || bridgeURL("finite", "/authorize") != "http://host.docker.internal:17851/authorize" {
		t.Fatal("original finite runner was changed")
	}
}
