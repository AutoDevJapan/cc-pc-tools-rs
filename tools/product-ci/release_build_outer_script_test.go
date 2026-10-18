package main

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// The outer script is run in a plain Alpine container with a shell function in
// place of QEMU. The function writes a valid, self-consistent result line, as a
// guest that finished its build would, and then exits with a chosen status. A
// clean emulator exit may yield the artifact; any other exit may not, even
// though the result line and its digest are correct. Opt in like the other
// real-Docker fixtures.
func TestOuterScriptTrustsTheResultOnlyFromACleanEmulatorExit(t *testing.T) {
	if os.Getenv("CC_RELEASE_BUILD_FIXTURE_ROOT") == "" {
		t.Skip("set CC_RELEASE_BUILD_FIXTURE_ROOT to run the real-Docker fixtures")
	}
	if _, err := os.Stat(docker); err != nil {
		t.Skip("Docker CLI unavailable")
	}
	var payload bytes.Buffer
	tw := tar.NewWriter(&payload)
	for _, n := range []string{"kernel", "initrd.gz"} {
		if tw.WriteHeader(&tar.Header{Name: n, Mode: 0o600, Typeflag: tar.TypeReg}) != nil {
			t.Fatal("payload header failed")
		}
	}
	if tw.Close() != nil {
		t.Fatal("payload failed")
	}
	for _, c := range []struct {
		name       string
		emulator   int
		wantExit   string
		wantOutput bool
	}{{"clean exit", 0, "script-exit=0", true}, {"emulator crash", 139, "script-exit=127", false}, {"emulator failure", 1, "script-exit=127", false}} {
		t.Run(c.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			prelude := fmt.Sprintf("qemu-system-x86_64() { printf 'CC-RESULT rc=0 size=5 sha=%%s\n' \"$(head -c 5 /w/out.img | sha256sum | cut -d' ' -f1)\" >> /w/result.txt; return %d; }\n", c.emulator)
			program := prelude + "(\n" + fixtureBuilderProfile.outerScript() + "\n)\necho script-exit=$?\nif [ -f /kaniko/output.tar ]; then echo has-output; fi\n"
			run := cmd(ctx, docker, "run", "--rm", "-i", "--entrypoint", "sh", "alpine:3.22", "-c", program)
			run.Stdin = bytes.NewReader(payload.Bytes())
			out, err := run.CombinedOutput()
			if err != nil {
				t.Fatalf("container failed: %v %s", err, out)
			}
			got := string(out)
			if !strings.Contains(got, c.wantExit) || strings.Contains(got, "has-output") != c.wantOutput {
				t.Fatalf("%s: want %s output=%v, got %q", c.name, c.wantExit, c.wantOutput, got)
			}
		})
	}
}
