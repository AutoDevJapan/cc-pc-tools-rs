package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// Execute the actual guest's output-checking block with CC-authored files.
// Substituting only its two absolute device/file paths keeps the real shell
// branches, hash and copy behavior under test without a VM or product source.
func TestGuestArtifactSizeEvidenceRealShell(t *testing.T) {
	if os.Getenv("CC_RELEASE_BUILD_FIXTURE_ROOT") == "" {
		t.Skip("set CC_RELEASE_BUILD_FIXTURE_ROOT to run the real-Docker fixtures")
	}
	start := strings.Index(guestInitScript, "rc=$?\nsync\nSZ=0\n")
	if start < 0 {
		t.Fatal("guest artifact block not found")
	}
	block := strings.NewReplacer("/work/out/output.tar", "image.tar", "/dev/vdb", "copied.tar").Replace(guestInitScript[start:])
	for _, c := range []struct {
		name     string
		size     int // -1 means missing
		buildOK  bool
		wantRC   int
		wantSize int
		copy     bool
	}{
		{"missing", -1, true, 124, 0, false},
		{"empty", 0, true, 124, 0, false},
		{"over limit", 17, true, 124, 17, false},
		{"at limit", 16, true, 0, 16, true},
		{"below limit", 5, true, 0, 5, true},
		{"failed build", 5, false, 1, 0, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			owner, err := newReleasePhaseRecord(t.Name())
			if err != nil {
				t.Fatal("fixture nonce unavailable")
			}
			name := "cc-todo2757-artifact-" + owner.Nonce
			defer func() {
				clean, stop := context.WithTimeout(context.Background(), 20*time.Second)
				defer stop()
				// --rm handles normal completion. A cancelled Docker client may
				// leave its known container alive; stop and remove it explicitly.
				_ = cmd(clean, docker, "container", "stop", "--time", "1", name).Run()
				_ = cmd(clean, docker, "container", "rm", "--volumes", name).Run()
				ids, err := boundedOutput(cmd(clean, docker, "container", "ls", "--all", "--quiet", "--filter", "name=^/"+name+"$"), 4096)
				if err != nil || strings.TrimSpace(string(ids)) != "" {
					t.Error("fixture container absence was not confirmed")
				}
			}()
			setup := "mkdir /fixture; cd /fixture; OUTMAX=16; Z=" + strings.Repeat("0", 64) + "\n"
			if c.size >= 0 {
				setup += fmt.Sprintf("head -c %d /dev/zero > image.tar\n", c.size)
			}
			setup += "finish() { echo result-rc=$1; echo result-size=$2; }\n"
			if c.buildOK {
				setup += "true\n"
			} else {
				setup += "false\n"
			}
			program := setup + block + "\nif [ -f copied.tar ]; then echo has-copy; cmp image.tar copied.tar || exit 50; fi\n"
			// Use the existing fixture image without introducing an image tag.
			run := cmd(ctx, docker, "run", "--name", name, "--label", "cc.todo=2757", "--rm", "--pull", "never", "--network", "none", "--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--cpus", "0.25", "--memory", "67108864", "--pids-limit", "32", "--entrypoint", "sh", "alpine:3.22", "-c", program)
			out, err := run.CombinedOutput()
			if err != nil {
				t.Fatalf("CC shell fixture failed: %v", err)
			}
			got := string(out)
			if !strings.Contains(got, fmt.Sprintf("result-rc=%d\n", c.wantRC)) || strings.Contains(got, "has-copy") != c.copy {
				t.Fatalf("wrong result or copy behavior: %q", got)
			}
			marker := fmt.Sprintf("cc-guest: artifact bytes=%d limit=16\n", c.wantSize)
			if strings.Contains(got, "cc-guest: artifact") != c.buildOK || c.buildOK && !strings.Contains(got, marker) {
				t.Fatalf("missing or incorrect measured-size evidence: %q", got)
			}
			// Oversized and absent outputs still report zero on the success-only
			// result channel. Diagnostics must never make rejected bytes usable.
			resultSize := c.wantSize
			if !c.copy {
				resultSize = 0
			}
			if !strings.Contains(got, fmt.Sprintf("result-size=%d\n", resultSize)) {
				t.Fatalf("rejected output entered the result channel: %q", got)
			}
		})
	}
}
