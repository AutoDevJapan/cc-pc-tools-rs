package main

import (
	"context"

	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Opt-in authentication-free storage probe. No product bytes, Cargo, runner
// registration, dispatch, GPU, host bind, image creation or shared stop.
func TestPR148TargetVolumeDocker(t *testing.T) {
	dir := os.Getenv("CC_PR148_TARGET_DOCKER_DIR")
	if dir == "" {
		t.Skip("explicit safe-delete-owned receipt root required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if e := pr148OwnedReceiptRoot(ctx, dir); e != nil {
		t.Fatal(e)
	}
	rec := struct {
		Namespace                                                              *pr148Namespace
		LogSHA, Failure                                                        string
		LogBytes                                                               int
		StorageProbeSuccess, ResourcesGone                                     bool
		ProductSourceSupplied, RegistrationAttempted, AllRequiredChecksSuccess bool
	}{}
	defer func() {
		if e := pr148SaveNew(filepath.Join(dir, "target-docker-result.private.json"), rec); e != nil {
			t.Error(e)
		}
	}()
	n, e := newPR148Namespace(ctx, dir)
	rec.Namespace = n
	if n != nil {
		defer func() {
			if e := n.dispose(); e != nil {
				rec.Failure = e.Error()
				t.Error(e)
			}
			rec.ResourcesGone = n.resourcesGone()
		}()
	}
	if e != nil {
		rec.Failure = e.Error()
		t.Fatal(e)
	}
	if _, e = n.inspect(ctx); e != nil {
		t.Fatal(e)
	}
	script := `set -eu
target=/work/civ6-banseiki/civ6-banseiki/tools/target
test "$(id -u):$(id -g)" = 1001:1001
test "$(stat -c '%u:%g:%a' "$target")" = 1001:1001:700
test "$(stat -f -c %T /work)" = tmpfs
test "$(stat -f -c %T "$target")" != tmpfs
test ! -e /var/run/docker.sock
test ! -e /opt/actions-runner/.credentials
test ! -e /opt/actions-runner/.runner
test ! -e /work/civ6-banseiki/civ6-banseiki/.git
printf 'todo2933 credential-free storage fixture\n' > "$target/probe.txt"
test "$(cat "$target/probe.txt")" = 'todo2933 credential-free storage fixture'
printf 'owner='; stat -c '%u:%g:%a' "$target"
printf 'work_fs='; stat -f -c %T /work
printf 'target_fs='; stat -f -c %T "$target"
cat /sys/fs/cgroup/memory.max /sys/fs/cgroup/memory.peak /sys/fs/cgroup/memory.events
`
	b, e := cmd(ctx, docker, "exec", n.ID, "sh", "-c", script).CombinedOutput()
	rec.LogBytes, rec.LogSHA = len(b), digest(b)
	if we := os.WriteFile(filepath.Join(dir, "target-docker.private.log"), b, 0600); we != nil {
		t.Fatal(we)
	}
	if e != nil || !strings.Contains(string(b), "oom_kill 0") {
		rec.Failure = "credential-free target probe failed"
		t.Fatalf("%s: %v", rec.Failure, e)
	}
	rec.StorageProbeSuccess = true
}
