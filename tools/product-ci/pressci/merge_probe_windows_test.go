package pressci

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestPressWorkflowRouteMergeInIsolatedGuest(t *testing.T) {
	if os.Getenv("CC2766_MERGE_HOST") != "1" {
		t.Skip("explicit read-only original workflow diagnostic")
	}
	guest := os.Getenv("CC2766_MERGE_GUEST_BINARY")
	if guest == "" {
		t.Fatal("Linux diagnostic binary missing")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	cmd := func(exe string, args ...string) *exec.Cmd {
		c := exec.CommandContext(ctx, exe, args...)
		c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
		c.WaitDelay = time.Second
		return c
	}
	read := func(ctx context.Context, ep string, out any) error {
		b, e := cmd(`D:\CC\Tools\codex-jobs\gh_gate.exe`, "api", "--method", "GET", ep).Output()
		if e != nil {
			return errors.New("read-only original API failed")
		}
		return json.Unmarshal(b, out)
	}
	if _, e := InspectOriginal(ctx, read); e != nil {
		t.Fatal(e)
	}
	var ref struct{ Object struct{ SHA string } }
	if e := read(ctx, "repos/"+Repository+"/git/ref/heads/main", &ref); e != nil {
		t.Fatal(e)
	}
	mainSHA := ref.Object.SHA
	var nonce [16]byte
	if _, e := rand.Read(nonce[:]); e != nil {
		t.Fatal(e)
	}
	lease := hex.EncodeToString(nonce[:])
	var input []workflowMergeInput
	for _, w := range workflows() {
		get := func(sha string) ([]byte, string) {
			var v struct{ SHA, Encoding, Content string }
			if e := read(ctx, "repos/"+Repository+"/contents/.github/workflows/"+w.name+"?ref="+sha, &v); e != nil {
				t.Fatal(e)
			}
			b, e := base64.StdEncoding.DecodeString(v.Content)
			if e != nil || v.Encoding != "base64" || blobID(b) != v.SHA {
				t.Fatal("API workflow blob rejected")
			}
			return b, v.SHA
		}
		base, baseBlob := get(Base)
		candidate, _ := get(Head)
		main, mainBlob := get(mainSHA)
		candidate, e := PrepareRoutes(w.name, candidate, lease)
		if e != nil {
			t.Fatal(e)
		}
		input = append(input, workflowMergeInput{w.name, base, candidate, main, baseBlob, blobID(candidate), mainBlob})
	}
	payload, e := json.Marshal(input)
	if e != nil {
		t.Fatal(e)
	}
	defer clear(payload)
	binary, e := os.ReadFile(guest)
	if e != nil {
		t.Fatal(e)
	}
	name := "todo2766-merge-" + lease
	dockerCmd := func(args ...string) *exec.Cmd {
		return workflowDiagnosticCommand(ctx, args...)
	}
	args := workflowDiagnosticArgs(name, lease)
	// Register before create: a lost response can still leave our nonce-named
	// resource. Cleanup has its own finite budget even after t.Fatal/timeout.
	t.Cleanup(func() {
		cleanupCtx, stop := context.WithTimeout(context.WithoutCancel(ctx), 45*time.Second)
		defer stop()
		if e := cleanupWorkflowDiagnostic(cleanupCtx, name, lease); e != nil {
			t.Errorf("owned workflow diagnostic cleanup unverified: %v", e)
		} else {
			t.Log("owned workflow diagnostic PID0/removed/absent")
		}
	})
	idBytes, e := dockerCmd(args...).Output()
	if e != nil {
		t.Fatal("owned diagnostic container creation failed")
	}
	id := strings.TrimSpace(string(idBytes))
	if len(id) != 64 {
		t.Fatal("container identity unavailable")
	}
	t.Log("owned_merge_container=" + id + " name=" + name)
	if e = dockerCmd("start", id).Run(); e != nil {
		t.Fatal(e)
	}
	c := dockerCmd("exec", "-i", id, "/bin/sh", "-c", "cat > /run-tests/test && chmod 700 /run-tests/test")
	c.Stdin = bytes.NewReader(binary)
	if e = c.Run(); e != nil {
		t.Fatal(e)
	}
	clear(binary)
	c = dockerCmd("exec", "-i", "-e", "CC2766_MERGE_GUEST=1", id, "/run-tests/test", "-test.v", "-test.run", "^TestPressWorkflowMergeGuest$")
	c.Stdin = bytes.NewReader(payload)
	out, e := c.CombinedOutput()
	t.Log(string(out)) // Guest emits metadata/closed errors only, even on failure.
	if e != nil {
		t.Fatal("guest merge failed")
	}
	if _, e := InspectOriginal(ctx, read); e != nil {
		t.Fatal(e)
	}
	if e = read(ctx, "repos/"+Repository+"/git/ref/heads/main", &ref); e != nil || ref.Object.SHA != mainSHA {
		t.Fatal("main changed during workflow merge diagnostic")
	}
	t.Log("workflow diagnostic complete; cleanup registered; official_ci_success=false; full_product_git_proof=false")
}

func workflowDiagnosticArgs(name, lease string) []string {
	return []string{"create", "--pull=never", "--name", name, "--label", "cc.todo=2766", "--label", "cc.instance=" + lease, "--read-only", "--network", "none", "--cpus", "1", "--memory", "256m", "--pids-limit", "32", "--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--tmpfs", "/work:rw,noexec,nosuid,size=16m,mode=1777", "--tmpfs", "/run-tests:rw,exec,nosuid,size=64m", "sha256:89988fa8449f89b0d8f12fd2369aa5b167bfe3389189df877808a15055869011", "sleep", "infinity"}
}

func workflowDiagnosticCommand(ctx context.Context, args ...string) *exec.Cmd {
	c := exec.CommandContext(ctx, `C:\Program Files\Docker\Docker\resources\bin\docker.exe`, append([]string{"--host", "npipe:////./pipe/dockerDesktopLinuxEngine"}, args...)...)
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	c.WaitDelay = time.Second
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if !strings.HasPrefix(strings.ToUpper(key), "DOCKER_") {
			c.Env = append(c.Env, entry)
		}
	}
	return c
}

func TestWorkflowDiagnosticIgnoresInheritedRemoteDockerRouting(t *testing.T) {
	t.Setenv("DOCKER_HOST", "tcp://untrusted.invalid:2375")
	t.Setenv("DOCKER_CONTEXT", "untrusted")
	t.Setenv("DOCKER_CONFIG", "untrusted-config")
	c := workflowDiagnosticCommand(context.Background(), "inspect", "fixed-id")
	if len(c.Args) < 3 || c.Args[1] != "--host" || c.Args[2] != "npipe:////./pipe/dockerDesktopLinuxEngine" || !c.SysProcAttr.HideWindow || c.SysProcAttr.CreationFlags&0x08000000 == 0 || c.WaitDelay != time.Second {
		t.Fatal("diagnostic command lost local engine or hidden bounded execution")
	}
	for _, entry := range c.Env {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(strings.ToUpper(key), "DOCKER_") {
			t.Fatal("inherited Docker routing/config reached diagnostic")
		}
	}
	if !strings.Contains(strings.Join(workflowDiagnosticArgs("fixed-name", "fixed-lease"), " "), "--pull=never") {
		t.Fatal("diagnostic can pull a replacement for the retained image")
	}
}

// No product, credentials or API. The real cleanup is reached with an already
// cancelled work context, including the t.Cleanup path used after t.Fatal.
func TestWorkflowDiagnosticCleanupAfterCancelledWork(t *testing.T) {
	if os.Getenv("CC2766_CLEANUP_PROBE") != "1" {
		t.Skip("explicit source-free owned resource probe")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// Do not create a resource when the shared engine/image is unavailable.
	if b, e := workflowDiagnosticCommand(ctx, "image", "inspect", "sha256:89988fa8449f89b0d8f12fd2369aa5b167bfe3389189df877808a15055869011", "--format", "{{.Id}}").Output(); e != nil || strings.TrimSpace(string(b)) != "sha256:89988fa8449f89b0d8f12fd2369aa5b167bfe3389189df877808a15055869011" {
		t.Fatal("shared engine/fixed image unavailable before resource creation")
	}
	var nonce [16]byte
	if _, e := rand.Read(nonce[:]); e != nil {
		t.Fatal(e)
	}
	lease := hex.EncodeToString(nonce[:])
	name := "todo2766-cleanup-" + lease
	t.Log("owned_cleanup_name=" + name)
	t.Cleanup(func() {
		cleanupCtx, stop := context.WithTimeout(context.WithoutCancel(ctx), 45*time.Second)
		defer stop()
		if e := cleanupWorkflowDiagnostic(cleanupCtx, name, lease); e != nil {
			t.Errorf("owned cancelled diagnostic cleanup unverified: %v", e)
		} else {
			t.Log("cancelled diagnostic PID0/removed/absent")
		}
	})
	command := func(args ...string) *exec.Cmd {
		return workflowDiagnosticCommand(ctx, args...)
	}
	b, e := command(workflowDiagnosticArgs(name, lease)...).Output()
	if e != nil {
		t.Fatal("owned source-free diagnostic create failed")
	}
	id := strings.TrimSpace(string(b))
	if len(id) != 64 {
		t.Fatal("owned source-free identity unavailable")
	}
	t.Log("owned_cleanup_container=" + id + " name=" + name)
	if e = command("start", id).Run(); e != nil {
		t.Fatal("owned diagnostic start failed")
	}
	cancel()
}

func cleanupWorkflowDiagnostic(ctx context.Context, name, lease string) error {
	run := func(args ...string) *exec.Cmd {
		return workflowDiagnosticCommand(ctx, args...)
	}
	b, e := run("container", "ls", "-a", "--no-trunc", "--filter", "name=^/"+name+"$", "--format", "{{.ID}}").Output()
	if e != nil {
		return errors.New("owned container lookup failed")
	}
	id := strings.TrimSpace(string(b))
	if id == "" {
		return nil
	}
	if len(id) != 64 {
		return errors.New("owned container identity unavailable")
	}
	var rows []struct {
		Id, Name string
		Config   struct {
			Image  string
			Labels map[string]string
		}
		State struct {
			Pid     int
			Running bool
		}
	}
	inspect := func() error {
		b, e := run("inspect", id).Output()
		if e != nil || json.Unmarshal(b, &rows) != nil || len(rows) != 1 || rows[0].Id != id || rows[0].Name != "/"+name || rows[0].Config.Labels["cc.instance"] != lease || rows[0].Config.Labels["cc.todo"] != "2766" || rows[0].Config.Image != "sha256:89988fa8449f89b0d8f12fd2369aa5b167bfe3389189df877808a15055869011" {
			return errors.New("owned container metadata rejected")
		}
		return nil
	}
	if e = inspect(); e != nil {
		return e
	}
	if rows[0].State.Running || rows[0].State.Pid != 0 {
		if e = run("stop", "--time", "5", id).Run(); e != nil {
			return errors.New("owned container stop failed")
		}
	}
	if e = inspect(); e != nil {
		return e
	}
	if rows[0].State.Pid != 0 || rows[0].State.Running {
		return errors.New("owned container PID0 unverified")
	}
	if e = run("rm", "-v", id).Run(); e != nil {
		return errors.New("owned container removal failed")
	}
	b, e = run("container", "ls", "-a", "--no-trunc", "--filter", "id="+id, "--format", "{{.ID}}").Output()
	if e != nil || strings.TrimSpace(string(b)) != "" {
		return errors.New("owned container absence unverified")
	}
	return nil
}
