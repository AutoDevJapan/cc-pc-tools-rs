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
	const docker = `C:\Program Files\Docker\Docker\resources\bin\docker.exe`
	dockerCmd := func(args ...string) *exec.Cmd {
		return cmd(docker, append([]string{"--host", "npipe:////./pipe/dockerDesktopLinuxEngine"}, args...)...)
	}
	args := []string{"create", "--name", name, "--label", "cc.todo=2766", "--label", "cc.instance=" + lease, "--read-only", "--network", "none", "--cpus", "1", "--memory", "256m", "--pids-limit", "32", "--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--tmpfs", "/work:rw,noexec,nosuid,size=16m,mode=1777", "--tmpfs", "/run-tests:rw,exec,nosuid,size=64m", "sha256:89988fa8449f89b0d8f12fd2369aa5b167bfe3389189df877808a15055869011", "sleep", "infinity"}
	idBytes, e := dockerCmd(args...).Output()
	if e != nil {
		t.Fatal("owned diagnostic container creation failed")
	}
	id := strings.TrimSpace(string(idBytes))
	if len(id) != 64 {
		t.Fatal("container identity unavailable")
	}
	// Cleanup is explicit below; on an unexpected failure the ID is retained in
	// this full test log for reconciliation rather than hidden by a defer.
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
	if e = dockerCmd("stop", "--time", "5", id).Run(); e != nil {
		t.Fatal(e)
	}
	var rows []struct {
		Id, Name string
		Config   struct{ Labels map[string]string }
		State    struct {
			Pid     int
			Running bool
		}
	}
	b, e := dockerCmd("inspect", id).Output()
	if e != nil || json.Unmarshal(b, &rows) != nil || len(rows) != 1 || rows[0].Id != id || rows[0].Name != "/"+name || rows[0].Config.Labels["cc.instance"] != lease || rows[0].Config.Labels["cc.todo"] != "2766" || rows[0].State.Pid != 0 || rows[0].State.Running {
		t.Fatal("owned container stop unverified")
	}
	if e = dockerCmd("rm", "-v", id).Run(); e != nil {
		t.Fatal(e)
	}
	b, e = dockerCmd("container", "ls", "-a", "--no-trunc", "--filter", "id="+id, "--format", "{{.ID}}").Output()
	if e != nil || strings.TrimSpace(string(b)) != "" {
		t.Fatal("owned container absence unverified")
	}
	t.Log("workflow diagnostic container PID0/removed/absent; official_ci_success=false; full_product_git_proof=false")
}
