package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Real-Docker fixtures. They build only the CC-authored Go probe from an empty
// base image: no product source, token, registry push, runner or release job.
// Opt in with CC_RELEASE_BUILD_FIXTURE_ROOT=<fresh safe-delete owned directory>;
// every case gets its own empty subdirectory below it.
const fixtureProbeSource = `package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "child" {
		time.Sleep(time.Hour)
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "hold" {
		// A child that outlives the build step proves the whole PID namespace is stopped.
		child := exec.Command("/probe", "child")
		if child.Start() != nil {
			os.Exit(5)
		}
		if _, err := os.Stat("/proc/" + strconv.Itoa(child.Process.Pid)); err != nil {
			os.Exit(6)
		}
		// Printed only once the child exists; the harness waits for this line.
		fmt.Println("CC-PROBE-READY")
		time.Sleep(time.Hour)
		return
	}
	values := map[string]string{}
	for _, key := range []string{"cpu.max", "memory.max", "pids.max"} {
		b, e := os.ReadFile("/sys/fs/cgroup/" + key)
		if e != nil {
			os.Exit(2)
		}
		values[key] = strings.TrimSpace(string(b))
	}
	b, e := os.ReadFile("/proc/self/status")
	if e != nil {
		os.Exit(3)
	}
	for _, line := range strings.Split(string(b), "\n") {
		for _, key := range []string{"CapEff:", "NoNewPrivs:"} {
			if strings.HasPrefix(line, key) {
				values[strings.TrimSuffix(key, ":")] = strings.TrimSpace(strings.TrimPrefix(line, key))
			}
		}
	}
	raw, e := json.Marshal(values)
	if e != nil || os.WriteFile("/proof", raw, 0o600) != nil {
		os.Exit(4)
	}
}
`

func fixtureBuilderSpec(t *testing.T, probe []byte, hold bool) releaseBuilderSpec {
	t.Helper()
	run := "RUN [\"/probe\"]\n"
	if hold {
		run = "RUN [\"/probe\", \"hold\"]\n"
	}
	var raw bytes.Buffer
	gz := gzip.NewWriter(&raw)
	tw := tar.NewWriter(gz)
	for _, f := range []struct {
		name string
		mode int64
		data []byte
	}{{"Dockerfile", 0644, []byte("FROM scratch\nCOPY probe /probe\n" + run)}, {"probe", 0755, probe}} {
		if tw.WriteHeader(&tar.Header{Name: f.name, Mode: f.mode, Size: int64(len(f.data)), Typeflag: tar.TypeReg, Uid: 0, Gid: 0}) != nil {
			t.Fatal("fixture header failed")
		}
		if _, err := tw.Write(f.data); err != nil {
			t.Fatal(err)
		}
	}
	if tw.Close() != nil || gz.Close() != nil {
		t.Fatal("fixture archive failed")
	}
	return releaseBuilderSpec{profile: fixtureBuilderProfile, context: raw.Bytes(), contextSHA256: digest(raw.Bytes()), dockerfile: "Dockerfile", tree: "fixture", snapshotSHA256: "fixture", contextFiles: 2, maxDuration: releaseBuildMaxDuration}
}

type decoratedDocker struct {
	inner  builderDocker
	before func(args []string) error
	after  func(args []string, err error) error
}

func (d decoratedDocker) run(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, args ...string) error {
	if d.before != nil {
		if err := d.before(args); err != nil {
			return err
		}
	}
	err := d.inner.run(ctx, stdin, stdout, stderr, args...)
	if d.after != nil {
		return d.after(args, err)
	}
	return err
}

func TestReleaseBuilderRealFixture(t *testing.T) {
	root := os.Getenv("CC_RELEASE_BUILD_FIXTURE_ROOT")
	if root == "" {
		t.Skip("set CC_RELEASE_BUILD_FIXTURE_ROOT to a fresh safe-delete owned directory")
	}
	if _, err := os.Stat(docker); err != nil {
		t.Skip("Docker CLI unavailable")
	}
	root, err := os.MkdirTemp(root, "run-")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	newDir := func(name string) string {
		dir := filepath.Join(root, name)
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	// Build the Linux probe from the embedded source.
	src := newDir("probe-src")
	if err := os.WriteFile(filepath.Join(src, "main.go"), []byte(fixtureProbeSource), 0o600); err != nil {
		t.Fatal(err)
	}
	build := cmd(ctx, "go", "build", "-trimpath", "-ldflags=-s -w", "-o", filepath.Join(src, "probe"), filepath.Join(src, "main.go"))
	build.Dir = src
	build.Env = append(os.Environ(), "GOOS=linux", "GOARCH=amd64", "CGO_ENABLED=0", "GOFLAGS=")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("probe build failed: %s", out)
	}
	probe, err := os.ReadFile(filepath.Join(src, "probe"))
	if err != nil {
		t.Fatal(err)
	}
	config := newDir("independent-docker-config")
	independent := cliBuilderDocker{config}
	text := func(args ...string) (string, error) {
		out, _, err := dockerText(ctx, independent, 1<<20, args...)
		return out, err
	}
	imageAbsent := func() bool {
		_, stderr, err := dockerText(ctx, independent, 1<<16, "image", "inspect", "--format", "{{.Id}}", releaseBuilderTool)
		return err != nil && strings.Contains(strings.ToLower(stderr), "no such image")
	}
	assertGone := func(rec releaseBuildReceipt) {
		t.Helper()
		ids, err := text("container", "ls", "--all", "--no-trunc", "--quiet", "--filter", "label="+releaseBuildLabel+"="+rec.Nonce)
		if err != nil || ids != "" {
			t.Fatalf("builder container still present: %v", err)
		}
		for _, v := range rec.VolumeNames {
			if out, err := text("volume", "ls", "--quiet"); err != nil || strings.Contains(out, v) {
				t.Fatal("builder volume still present")
			}
		}
		if !rec.PhaseMayRelease || rec.RecoveryRequired || !rec.PIDZero && rec.CreateAttempted && rec.ContainerID != "" {
			t.Fatalf("disposition not confirmed: %+v", rec.PhaseMayRelease)
		}
	}
	var reports []releaseBuildReceipt
	var isolationOnly []struct {
		Kind                            string
		OuterIsolationVerified, Started bool
		Network                         string
		CPUNano, MemoryBytes, Pids      int64
		PhaseMayRelease                 bool
	}
	factory := func(cfg string) builderDocker { return cliBuilderDocker{cfg} }
	save := func() {
		raw, _ := json.MarshalIndent(map[string]any{"builds": reports, "isolation_checks": isolationOnly}, "", "  ")
		_ = os.WriteFile(filepath.Join(root, "fixture-report.json"), raw, 0o600)
	}
	defer save()

	introduced := imageAbsent()
	t.Run("normal", func(t *testing.T) {
		rec, err := runReleaseBuilder(ctx, factory, fixtureBuilderSpec(t, probe, false), newDir("normal"))
		reports = append(reports, rec)
		if err != nil || !rec.Succeeded || !rec.KernelProofVerified || rec.ExitCode != 0 || !rec.OuterIsolationVerified || !rec.PIDZero || !rec.ContainerRemoved || rec.ArtifactBytes == 0 || rec.LogSHA256 == "" {
			t.Fatalf("normal fixture failed: %v %s", err, rec.Failure)
		}
		art, err := rec.verifiedArtifact()
		if err != nil {
			t.Fatal(err)
		}
		// The fixture image holds only the CC-authored probe; keep it for inspection.
		if err := os.WriteFile(filepath.Join(root, "normal-artifact.tar"), art, 0o600); err != nil {
			t.Fatal(err)
		}
		if introduced && (!rec.ToolImageIntroduced || !rec.ToolImageRemoved || !imageAbsent()) {
			t.Fatal("introduced tool image was not removed")
		}
		assertGone(rec)
	})
	// Keep the tool image for the remaining cases and remove it ourselves at the end.
	if imageAbsent() {
		if _, _, err := dockerText(ctx, independent, 1<<20, "pull", releaseBuilderTool); err != nil {
			t.Fatal("tool image pull failed")
		}
		defer func() {
			if _, _, err := dockerText(ctx, independent, 1<<16, "image", "rm", releaseBuilderTool); err != nil || !imageAbsent() {
				t.Error("harness tool image not removed")
			}
		}()
	}
	// The held probe prints a marker into the builder log once its child exists.
	// A harness that never sees it fails: nothing is concluded from a cancelled,
	// expired or never-started build.
	waitMarker := func(dir string, within time.Duration) bool {
		deadline := time.Now().Add(within)
		for time.Now().Before(deadline) {
			if b, err := os.ReadFile(filepath.Join(dir, releaseBuildLogFile)); err == nil && strings.Contains(string(b), "CC-PROBE-READY") {
				return true
			}
			time.Sleep(500 * time.Millisecond)
		}
		return false
	}
	logHasMarker := func(dir string) bool {
		b, err := os.ReadFile(filepath.Join(dir, releaseBuildLogFile))
		return err == nil && strings.Contains(string(b), "CC-PROBE-READY")
	}
	t.Run("cancel", func(t *testing.T) {
		cctx, cancel := context.WithCancel(ctx)
		defer cancel()
		dir := newDir("cancel")
		reached := make(chan bool, 1)
		go func() {
			ok := waitMarker(dir, 3*time.Minute)
			reached <- ok
			cancel()
		}()
		rec, err := runReleaseBuilder(cctx, factory, fixtureBuilderSpec(t, probe, true), dir)
		reports = append(reports, rec)
		if !<-reached {
			t.Fatal("the held probe and its child were never observed")
		}
		if err == nil || rec.Failure != "canceled" || !rec.Started || rec.Succeeded {
			t.Fatalf("cancel fixture: %v %s", err, rec.Failure)
		}
		assertGone(rec)
	})
	t.Run("timeout", func(t *testing.T) {
		tctx, cancel := context.WithTimeout(ctx, 75*time.Second)
		defer cancel()
		dir := newDir("timeout")
		rec, err := runReleaseBuilder(tctx, factory, fixtureBuilderSpec(t, probe, true), dir)
		reports = append(reports, rec)
		if !logHasMarker(dir) || !rec.Started {
			t.Fatal("the deadline expired before the held probe and its child were observed")
		}
		if err == nil || rec.Failure != "timeout" || rec.Succeeded {
			t.Fatalf("timeout fixture: %v %s", err, rec.Failure)
		}
		assertGone(rec)
	})
	t.Run("sweep-inert-leftover", func(t *testing.T) {
		// What a lost create could leave behind: a never-started container of the
		// exact name/label/image pattern. The next phase removes it and goes on.
		nonce := strings.Repeat("ab", 16)
		leftover := "cc-release-build-" + nonce
		if _, _, err := dockerText(ctx, independent, 1<<16, "create", "--name", leftover, "--label", releaseBuildLabel+"="+nonce, "--network", "none", "--cap-drop", "ALL", "--memory", "67108864", "--tmpfs", "/tmp:rw,nosuid,nodev,size=1m", "--tmpfs", "/workspace:rw,nosuid,nodev,size=1m", releaseBuilderTool, "--version"); err != nil {
			t.Fatal("could not create the leftover")
		}
		rec, err := runReleaseBuilder(ctx, factory, fixtureBuilderSpec(t, probe, false), newDir("sweep"))
		reports = append(reports, rec)
		if err != nil || !rec.Succeeded {
			t.Fatalf("the phase was not freed of the inert leftover: %v %s", err, rec.Failure)
		}
		if out, e := text("container", "ls", "--all", "--quiet", "--filter", "label="+releaseBuildLabel+"="+nonce); e != nil || out != "" {
			t.Fatal("the leftover still exists")
		}
		assertGone(rec)
	})
	t.Run("uncertain-create", func(t *testing.T) {
		lost := func(cfg string) builderDocker {
			return decoratedDocker{inner: cliBuilderDocker{cfg}, after: func(args []string, err error) error {
				if args[0] == "create" {
					return errors.New("response lost") // The real daemon did create it.
				}
				return err
			}}
		}
		rec, err := runReleaseBuilder(ctx, lost, fixtureBuilderSpec(t, probe, false), newDir("uncertain-create"))
		reports = append(reports, rec)
		if err == nil || rec.Failure != "create-uncertain" || !rec.UncertainCreate || rec.Started || !rec.ContainerRemoved {
			t.Fatalf("uncertain create fixture: %v %s", err, rec.Failure)
		}
		assertGone(rec)
	})
	t.Run("uncertain-create-never-landed", func(t *testing.T) {
		// The real create never reaches the daemon and its outcome is unknown: only
		// taking the name with an inert container resolves it, and that container
		// is removed again.
		silent := func(cfg string) builderDocker {
			return decoratedDocker{inner: cliBuilderDocker{cfg}, before: func(args []string) error {
				if args[0] == "create" && strings.Contains(strings.Join(args, " "), " --init ") {
					return errors.New("response lost before reaching the daemon")
				}
				return nil
			}}
		}
		rec, err := runReleaseBuilder(ctx, silent, fixtureBuilderSpec(t, probe, false), newDir("uncertain-never-landed"))
		reports = append(reports, rec)
		if err == nil || rec.Failure != "create-uncertain" || !rec.UncertainCreate || rec.Started || !rec.ContainerRemoved || !rec.PhaseMayRelease || len(rec.VolumeNames) != 0 {
			t.Fatalf("name hold fixture: %v %s", err, rec.Failure)
		}
		assertGone(rec)
	})
	t.Run("recovery", func(t *testing.T) {
		dir := newDir("recovery")
		refuse := func(cfg string) builderDocker {
			return decoratedDocker{inner: cliBuilderDocker{cfg}, before: func(args []string) error {
				if len(args) > 1 && args[0] == "container" && args[1] == "rm" {
					return errors.New("removal refused")
				}
				return nil
			}}
		}
		rec, err := runReleaseBuilder(ctx, refuse, fixtureBuilderSpec(t, probe, false), dir)
		if err == nil || rec.PhaseMayRelease || !rec.RecoveryRequired || !rec.PIDZero {
			t.Fatalf("unconfirmed disposal released: %v", err)
		}
		if ids, e := text("container", "ls", "--all", "--no-trunc", "--quiet", "--filter", "label="+releaseBuildLabel+"="+rec.Nonce); e != nil || ids == "" {
			t.Fatal("expected the retained container")
		}
		again, err := recoverReleaseBuilder(ctx, factory, dir)
		reports = append(reports, again)
		if err != nil || !again.PhaseMayRelease {
			t.Fatalf("recovery failed: %v", err)
		}
		assertGone(again)
	})
	// Real daemon values for the product profile (1.5 CPU, 6 GiB, 448 PIDs, bridge
	// network). The builder is created and inspected but never started, and no
	// product source is involved: the context comes from a synthetic snapshot.
	t.Run("product-profile-isolation-only", func(t *testing.T) {
		c := synthSnapshot(t, synthFiles(synthCompose))
		in, err := prepareReleaseBuild(c, synthPins(c))
		if err != nil {
			t.Fatal(err)
		}
		spec, err := newReleaseBuilderSpec(productBuilderProfile, in)
		if err != nil {
			t.Fatal(err)
		}
		b := &builderRun{parent: ctx, spec: spec, dir: newDir("product-isolation"), rec: newBuildReceipt(spec)}
		if class := b.begin(factory); class != "" {
			t.Fatal(class)
		}
		sctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
		defer cancel()
		for _, step := range []func(context.Context) string{b.toolImage, b.create, b.verifyOuter} {
			if class := step(sctx); class != "" {
				b.rec.Failure = class
				break
			}
		}
		b.dispose()
		rec, err := b.finish()
		// Not a build: the receipt must say so, and the result is recorded apart
		// from the real build reports.
		if err == nil || rec.Succeeded || rec.Failure != "build-incomplete" {
			t.Fatalf("an unstarted builder was reported as a build: %v %s", err, rec.Failure)
		}
		if !rec.OuterIsolationVerified || rec.Started || rec.Network != "bridge" || rec.CPUNano != 1_500_000_000 || rec.MemoryBytes != 6<<30 || rec.Pids != 448 {
			t.Fatalf("product profile isolation: %s", rec.Failure)
		}
		isolationOnly = append(isolationOnly, struct {
			Kind                            string
			OuterIsolationVerified, Started bool
			Network                         string
			CPUNano, MemoryBytes, Pids      int64
			PhaseMayRelease                 bool
		}{"isolation-only (not a build)", rec.OuterIsolationVerified, rec.Started, rec.Network, rec.CPUNano, rec.MemoryBytes, rec.Pids, rec.PhaseMayRelease})
		assertGone(rec)
	})
}
