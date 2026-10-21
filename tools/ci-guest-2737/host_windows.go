package main

import (
	"archive/tar"
	"bufio"
	"bytes"
	sourcepacket "cc/ci-source-2737"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const dockerExe = `C:\Program Files\Docker\Docker\resources\bin\docker.exe`
const sdkImage = "sha256:89988fa8449f89b0d8f12fd2369aa5b167bfe3389189df877808a15055869011"
const imageArchiveHash = "3543723afd820d7a8a64ea7399376856a95ce15200439c38447170652a60a5f3"
const genericImageHash = "6a81c37564db9b1ee84e141922625e1d7c5b389b99bb3c572e0243607d5bb4d2"
const broker = `D:\CC\Tools\admin-broker\run-as-admin.ps1`

func command(ctx context.Context, name string, args ...string) *exec.Cmd {
	c := exec.CommandContext(ctx, name, args...)
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	c.WaitDelay = 5 * time.Second
	if name == dockerExe {
		c.Args = append([]string{name, "--host", "npipe:////./pipe/dockerDesktopLinuxEngine"}, args...)
		for _, v := range os.Environ() {
			k, _, _ := strings.Cut(v, "=")
			if !strings.HasPrefix(strings.ToUpper(k), "DOCKER_") {
				c.Env = append(c.Env, v)
			}
		}
	}
	return c
}
func newID() string {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		panic("random source unavailable")
	}
	return hex.EncodeToString(b[:])
}
func writeNew(path string, data []byte) error {
	f, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	_, e = f.Write(data)
	if e == nil {
		e = f.Sync()
	}
	return errors.Join(e, f.Close())
}
func record(root, name string, v any) error {
	b, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return e
	}
	return writeNew(filepath.Join(root, name), b)
}
func psQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
func admin(ctx context.Context, root, name, body string) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	p := filepath.Join(root, name+".private.ps1")
	if e := writeNew(p, []byte("$ErrorActionPreference='Stop'\n"+body)); e != nil {
		return e
	}
	f, e := os.OpenFile(filepath.Join(root, name+".private.log"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	defer f.Close()
	// The broker owns the admitted script; cancelling its client does not cancel
	// the elevated work. Drain that flight before reading receipts or cleanup.
	flight, stop := context.WithTimeout(context.Background(), 330*time.Second)
	defer stop()
	c := command(flight, `C:\Program Files\PowerShell\7\pwsh.exe`, "-NoProfile", "-File", broker, "-Script", p, "-TimeoutSec", "300")
	c.Stdout = f
	c.Stderr = f
	if e = c.Run(); e != nil {
		return errors.New("owned guest admin stage failed; preserve private evidence")
	}
	return nil
}

func extractImage(root string) error {
	f, e := os.Open(filepath.Join(root, "linux-public.vhd.tar.gz"))
	if e != nil {
		return e
	}
	defer f.Close()
	h, e := io.ReadAll(io.LimitReader(f, 1<<30))
	if e != nil || hash(h) != imageArchiveHash {
		return errors.New("public Linux image archive digest rejected")
	}
	z, e := gzip.NewReader(bytes.NewReader(h))
	if e != nil {
		return e
	}
	defer z.Close()
	tr := tar.NewReader(z)
	found := false
	var size int64
	for {
		header, e := tr.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			return e
		}
		if header.Typeflag == tar.TypeDir {
			continue
		}
		// Canonical's pinned Azure VHD uses GNU sparse tar (type S). Go's
		// Reader expands its holes; header.Size remains the logical byte count.
		if found || (header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeGNUSparse) || !strings.HasSuffix(strings.ToLower(header.Name), ".vhd") || header.Size < 1<<30 || header.Size > 40<<30 {
			return errors.New("public VHD archive shape rejected")
		}
		out, e := os.OpenFile(filepath.Join(root, "linux-public.vhd"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			return e
		}
		n, e := io.Copy(out, tr)
		if e == nil {
			e = out.Sync()
		}
		e = errors.Join(e, out.Close())
		if e != nil || n != header.Size {
			return errors.New("public VHD extraction incomplete")
		}
		size = n
		found = true
	}
	if !found {
		return errors.New("public VHD absent")
	}
	return record(root, "linux-image-extracted.private.json", map[string]any{"archive_sha256": imageArchiveHash, "vhd_bytes": size, "product_source": false})
}

type toolState struct{ Name, ID string }

func toolInspect(ctx context.Context, s toolState) error {
	b, e := command(ctx, dockerExe, "inspect", s.ID).Output()
	if e != nil {
		return e
	}
	var entries []struct {
		ID, Name string
		Config   struct {
			Image  string
			Labels map[string]string
		}
		HostConfig struct {
			Privileged              bool
			Binds                   []string
			Devices, DeviceRequests []any
			PidMode                 string
		}
		Mounts []struct{ Type, Destination string }
	}
	if json.Unmarshal(b, &entries) != nil || len(entries) != 1 {
		return errors.New("owned SDK tool container unavailable")
	}
	v := entries[0]
	if v.ID != s.ID || v.Name != "/"+s.Name || v.Config.Image != sdkImage || v.Config.Labels["cc.todo"] != "2737" || v.Config.Labels["cc.instance"] != s.Name || v.HostConfig.Privileged || len(v.HostConfig.Binds) > 0 || len(v.HostConfig.Devices) > 0 || len(v.HostConfig.DeviceRequests) > 0 || v.HostConfig.PidMode != "" {
		return errors.New("owned SDK tool container identity rejected")
	}
	for _, m := range v.Mounts {
		if m.Type != "tmpfs" || (m.Destination != "/work" && m.Destination != "/tmp") {
			return errors.New("SDK tool host mount rejected")
		}
	}
	return nil
}
func prepareTools(ctx context.Context, root string) error {
	s := toolState{Name: "todo2737-tools-" + newID()}
	if e := record(root, "tools-intent.private.json", s); e != nil {
		return e
	}
	b, e := command(ctx, dockerExe, "create", "--name", s.Name, "--label", "cc.todo=2737", "--label", "cc.instance="+s.Name, "--init", "--user", "0:0", "--security-opt", "no-new-privileges", "--cpus", "1", "--memory", "1g", "--pids-limit", "256", "--tmpfs", "/work:rw,nosuid,size=256m", "--tmpfs", "/tmp:rw,nosuid,size=256m", sdkImage, "sleep", "infinity").Output()
	if e != nil {
		return errors.New("SDK tool create uncertain; preserve exact recorded name")
	}
	s.ID = strings.TrimSpace(string(b))
	if !validHash(s.ID) {
		return errors.New("SDK tool create identity unavailable")
	}
	if e = record(root, "tools-created.private.json", s); e != nil {
		return e
	}
	if e = toolInspect(ctx, s); e != nil {
		return e
	}
	if e = command(ctx, dockerExe, "start", s.ID).Run(); e != nil {
		return e
	}
	f, e := os.OpenFile(filepath.Join(root, "tools-install.private.log"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	defer f.Close()
	c := command(ctx, dockerExe, "exec", s.ID, "sh", "-c", "apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends genisoimage")
	c.Stdout = f
	c.Stderr = f
	if c.Run() != nil {
		return errors.New("public genisoimage installation failed")
	}
	return record(root, "tools-ready.private.json", s)
}
func readTools(root string) (toolState, error) {
	var s toolState
	b, e := os.ReadFile(filepath.Join(root, "tools-ready.private.json"))
	if e != nil {
		return s, e
	}
	if json.Unmarshal(b, &s) != nil || !validHash(s.ID) || !strings.HasPrefix(s.Name, "todo2737-tools-") {
		return s, errors.New("owned tool receipt invalid")
	}
	return s, nil
}

func linuxTests(ctx context.Context, root, binary string) error {
	s, e := readTools(root)
	if e != nil {
		return e
	}
	if e = toolInspect(ctx, s); e != nil {
		return e
	}
	b, e := os.ReadFile(binary)
	if e != nil || len(b) > 32<<20 || !bytes.HasPrefix(b, []byte{0x7f, 'E', 'L', 'F'}) {
		return errors.New("Linux fixture test binary rejected")
	}
	// Docker's archive API does not populate a running tmpfs mount. Transfer
	// through exec stdin so the real process sees these exact bytes.
	copy := command(ctx, dockerExe, "exec", "-i", s.ID, "sh", "-c", "cat > /usr/local/bin/ci2737-tests && chmod 700 /usr/local/bin/ci2737-tests")
	copy.Stdin = bytes.NewReader(b)
	if copy.Run() != nil {
		return errors.New("Linux test stdin transfer failed")
	}
	f, e := os.OpenFile(filepath.Join(root, "linux-unit-"+hash(b)[:16]+".private.log"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	defer f.Close()
	run := command(ctx, dockerExe, "exec", s.ID, "/usr/local/bin/ci2737-tests", "-test.v")
	run.Stdout = f
	run.Stderr = f
	if run.Run() != nil {
		return errors.New("real Linux fixture tests failed; preserve original log")
	}
	return nil
}
func cleanupTools(ctx context.Context, root string) error {
	var s toolState
	b, e := os.ReadFile(filepath.Join(root, "tools-created.private.json"))
	if e != nil || json.Unmarshal(b, &s) != nil {
		return errors.New("owned tool creation receipt unavailable")
	}
	if e = toolInspect(ctx, s); e != nil {
		return e
	}
	if command(ctx, dockerExe, "stop", "--time", "10", s.ID).Run() != nil {
		return errors.New("owned tool stop failed")
	}
	b, e = command(ctx, dockerExe, "inspect", s.ID).Output()
	var v []struct {
		State struct {
			Running bool
			Pid     int
		}
	}
	if e != nil || json.Unmarshal(b, &v) != nil || len(v) != 1 || v[0].State.Running || v[0].State.Pid != 0 {
		return errors.New("owned tool PID namespace stop unverified")
	}
	if command(ctx, dockerExe, "rm", "-v", s.ID).Run() != nil {
		return errors.New("owned tool removal failed")
	}
	b, e = command(ctx, dockerExe, "container", "ls", "-a", "--no-trunc", "--filter", "id="+s.ID, "--format", "{{.ID}}").Output()
	if e != nil || len(bytes.TrimSpace(b)) != 0 {
		return errors.New("owned tool absence unverified")
	}
	return record(root, "tools-cleanup.private.json", map[string]any{"id": s.ID, "pid": 0, "removed": true})
}
func prepareSeed(ctx context.Context, root, toolRoot, helper, instance string, i identity) error {
	return prepareSeedForSource(ctx, root, toolRoot, helper, instance, i, fixedSourcePin())
}
func prepareSeedForSource(ctx context.Context, root, toolRoot, helper, instance string, i identity, pin sourcepacket.Pin) error {
	if !validSourcePin(pin) {
		return errors.New("seed source expectation rejected")
	}
	s, e := readTools(toolRoot)
	if e != nil {
		return e
	}
	if e = toolInspect(ctx, s); e != nil {
		return e
	}
	guest, e := os.ReadFile(helper)
	if e != nil || len(guest) > 32<<20 || !bytes.HasPrefix(guest, []byte{0x7f, 'E', 'L', 'F'}) {
		return errors.New("Linux guest helper rejected")
	}
	public, _ := json.Marshal(publicConfig{Instance: instance, ClientCertificate: i.Digest, Source: pin})
	user := "#cloud-config\nhostname: ci2737-" + instance[:8] + "\nusers: []\ndisable_root: true\nssh_pwauth: false\nwrite_files:\n - path: /usr/local/bin/ci2737-guest\n   permissions: '0755'\n   encoding: b64\n   content: " + base64.StdEncoding.EncodeToString(guest) + "\n - path: /etc/ci2737-public.json\n   permissions: '0644'\n   content: '" + string(public) + "'\nruncmd:\n - [systemctl, stop, serial-getty@ttyS0.service]\n - [/usr/local/bin/ci2737-guest, guest]\n"
	seedPath := "/work/seed-" + instance
	if e = command(ctx, dockerExe, "exec", s.ID, "mkdir", seedPath).Run(); e != nil {
		return e
	}
	for name, data := range map[string]string{"user-data": user, "meta-data": "instance-id: ci2737-" + instance + "\nlocal-hostname: ci2737\n", "network-config": "version: 2\nethernets:\n  primary:\n    match:\n      name: 'eth*'\n    dhcp4: true\n"} {
		c := command(ctx, dockerExe, "exec", "-i", s.ID, "sh", "-c", "cat > "+seedPath+"/"+name)
		c.Stdin = strings.NewReader(data)
		if c.Run() != nil {
			return errors.New("public seed transfer failed")
		}
	}
	if command(ctx, dockerExe, "exec", s.ID, "genisoimage", "-quiet", "-output", seedPath+".iso", "-volid", "cidata", "-joliet", "-rock", seedPath).Run() != nil {
		return errors.New("public seed ISO generation failed")
	}
	b, e := command(ctx, dockerExe, "exec", s.ID, "cat", seedPath+".iso").Output()
	if e != nil || len(b) > 64<<20 {
		return errors.New("public seed ISO transfer failed")
	}
	return writeNew(filepath.Join(root, "seed.iso"), b)
}

func readSerial(ctx context.Context, pipe, instance, dest string) error {
	if !validInstance(instance) || pipe != `\\.\pipe\cc-ci-2737-`+instance {
		return errors.New("serial identity rejected")
	}
	f, e := os.OpenFile(pipe, os.O_RDWR, 0)
	if e != nil {
		return errors.New("owned serial pipe unavailable")
	}
	defer f.Close()
	console, e := os.OpenFile(dest+".console.private.log", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	defer console.Close()
	go func() { <-ctx.Done(); _ = f.Close() }()
	s := bufio.NewScanner(io.TeeReader(io.LimitReader(f, 4<<20), console))
	s.Buffer(make([]byte, 8192), 1<<20)
	for s.Scan() {
		line := s.Text()
		idx := strings.Index(line, "CI2737-READY ")
		if idx < 0 {
			continue
		}
		b, e := base64.StdEncoding.DecodeString(strings.TrimSpace(line[idx+len("CI2737-READY "):]))
		var r ready
		if e != nil || json.Unmarshal(b, &r) != nil || validateReady(r, instance) != nil {
			return errors.New("owned serial readiness invalid")
		}
		return writeNew(dest, b)
	}
	return errors.New("owned serial readiness unavailable")
}
func guestDial(i identity, r ready) (*tls.Conn, error) {
	d := net.Dialer{Timeout: 10 * time.Second}
	c, e := tls.DialWithDialer(&d, "tcp4", net.JoinHostPort(r.IP, fmt.Sprint(r.Port)), clientTLS(i, r.Certificate))
	if e == nil {
		e = c.SetDeadline(time.Now().Add(20 * time.Second))
	}
	return c, e
}
func runFixture(i identity, r ready, cancel bool) (fixtureResult, error) {
	id := newID()
	kind := "normal"
	if cancel {
		kind = "cancel"
	}
	c, e := guestDial(i, r)
	if e != nil {
		return fixtureResult{}, e
	}
	defer c.Close()
	if e = json.NewEncoder(c).Encode(request{kind, id}); e != nil {
		return fixtureResult{}, e
	}
	var observed fixtureResult
	if e = json.NewDecoder(c).Decode(&observed); e != nil || observed.ID != id || !observed.Observed || observed.Parent <= 1 || observed.Child <= 1 || observed.Group != observed.Parent {
		return observed, errors.New("real fixture child observation failed")
	}
	if !cancel {
		if e = json.NewEncoder(c).Encode(request{"continue", id}); e != nil {
			return observed, e
		}
		_, e = io.Copy(io.Discard, c)
		if e != nil {
			return observed, e
		}
	}
	_ = c.Close()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		probe, e := guestDial(i, r)
		if e != nil {
			return observed, e
		}
		_ = json.NewEncoder(probe).Encode(request{"status", id})
		var v fixtureResult
		e = json.NewDecoder(probe).Decode(&v)
		_ = probe.Close()
		if e != nil {
			return observed, e
		}
		if v.Done {
			if v.ID != id || !v.Observed || !v.Gone || v.Failure != "" || v.Cancelled != cancel || v.Parent != observed.Parent || v.Child != observed.Child {
				return v, errors.New("real fixture cleanup unverified")
			}
			return v, nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return observed, errors.New("real fixture cleanup deadline exceeded")
}
func smoke(ctx context.Context, root, toolRoot, helper, image string) (ret error) {
	i, e := newIdentity()
	if e != nil {
		return e
	}
	instance := newID()
	if e = prepareSeed(ctx, root, toolRoot, helper, instance, i); e != nil {
		return e
	}
	s, e := createVM(ctx, root, instance, image)
	if s.ID != "" {
		defer func() {
			cleanCtx, stop := context.WithTimeout(context.Background(), 5*time.Minute)
			defer stop()
			ret = errors.Join(ret, cleanupVM(cleanCtx, s))
		}()
	}
	if e != nil {
		return e
	}
	exe, e := os.Executable()
	if e != nil {
		return e
	}
	body := "& " + psQuote(exe) + " serial " + psQuote(s.Pipe) + " " + psQuote(instance) + " " + psQuote(filepath.Join(root, "guest-ready.private.json")) + "\nif($LASTEXITCODE -ne 0){throw 'SERIAL_PROBE_FAILED'}\n"
	if e = admin(ctx, root, "vm-readiness", body); e != nil {
		return e
	}
	b, e := os.ReadFile(filepath.Join(root, "guest-ready.private.json"))
	var r ready
	if e != nil || json.Unmarshal(b, &r) != nil || validateReady(r, instance) != nil {
		return errors.New("owned guest readiness receipt unavailable")
	}
	for _, cancel := range []bool{false, true} {
		result, e := runFixture(i, r, cancel)
		save := record(root, fmt.Sprintf("fixture-%t.private.json", cancel), result)
		if e != nil || save != nil {
			return errors.Join(e, save)
		}
	}
	return record(root, "guest-smoke.private.json", map[string]any{"instance": instance, "vm_id": s.ID, "normal": true, "cancel": true, "mutual_tls": true, "official_ci_success": false, "product_source": false})
}
func main() {
	if os.Getenv("CC_CI_2737_GATE_CHILD") == "1" {
		if e := gateChild(); e != nil {
			fmt.Fprintln(os.Stderr, e)
			os.Exit(1)
		}
		return
	}
	ctx, stop := context.WithTimeout(context.Background(), hostJobTimeout(os.Args))
	defer stop()
	var e error
	if len(os.Args) == 4 && os.Args[1] == "windows-serial" {
		e = readWindowsSerial(ctx, os.Args[2], os.Args[3])
	} else if len(os.Args) == 2 && os.Args[1] == "inspect-executor" {
		var proof map[string]any
		proof, e = linkedExecutorIdentity()
		if e == nil {
			e = json.NewEncoder(os.Stdout).Encode(proof)
		}
	} else if len(os.Args) == 5 && os.Args[1] == "windows-job" {
		e = runGatedWindowsJob(ctx, os.Args[2], os.Args[3], os.Args[4])
	} else if len(os.Args) == 6 && os.Args[1] == "linux-job" {
		e = runGatedLinuxJob(ctx, os.Args[2], os.Args[3], os.Args[4], os.Args[5])
	} else if len(os.Args) == 5 && os.Args[1] == "bootstrap-offline" {
		e = runGatedBootstrap(ctx, os.Args[2], os.Args[3], os.Args[4])
	} else if len(os.Args) == 5 && os.Args[1] == "probe-sdk" {
		e = runGatedJob(ctx, gatedHello{Mode: "sdk-probe", Root: os.Args[2], SDKRoot: os.Args[3], Helper: os.Args[4]})
	} else if len(os.Args) == 3 && os.Args[1] == "test-gate" {
		e = testGateFixtures(ctx, os.Args[2])
	} else if len(os.Args) == 2 && os.Args[1] == "inspect-source" {
		e = inspectSource(ctx)
	} else if len(os.Args) == 4 && os.Args[1] == "source-smoke" {
		e = sourceSmoke(ctx, os.Args[2], os.Args[3])
	} else if len(os.Args) == 5 && os.Args[1] == "serial" {
		sub, cancel := context.WithTimeout(ctx, 4*time.Minute)
		defer cancel()
		e = readSerial(sub, os.Args[2], os.Args[3], os.Args[4])
	} else if len(os.Args) == 3 && os.Args[1] == "extract" {
		e = extractImage(os.Args[2])
	} else if len(os.Args) == 3 && os.Args[1] == "test-constructor-failure" {
		e = constructorFailure(ctx, os.Args[2], "after-receipt")
	} else if len(os.Args) == 3 && os.Args[1] == "test-constructor-receipt-failure" {
		e = constructorFailure(ctx, os.Args[2], "receipt-write")
	} else if len(os.Args) == 3 && os.Args[1] == "test-constructor-double-failure" {
		e = constructorFailure(ctx, os.Args[2], "receipt-and-rollback")
	} else if len(os.Args) == 3 && os.Args[1] == "tools" {
		e = prepareTools(ctx, os.Args[2])
	} else if len(os.Args) == 3 && os.Args[1] == "cleanup-tools" {
		e = cleanupTools(ctx, os.Args[2])
	} else if len(os.Args) == 4 && os.Args[1] == "test-linux" {
		e = linuxTests(ctx, os.Args[2], os.Args[3])
	} else if len(os.Args) == 3 && os.Args[1] == "test-public-sdk" {
		e = testPublicSDK(ctx, os.Args[2])
	} else if len(os.Args) == 4 && os.Args[1] == "smoke" {
		e = smoke(ctx, os.Args[2], os.Args[2], os.Args[3], filepath.Join(os.Args[2], "linux-public.vhd"))
	} else if len(os.Args) == 4 && os.Args[1] == "smoke-generic" {
		root := os.Args[2]
		attempt := filepath.Join(root, "attempt-"+newID())
		if e = os.Mkdir(attempt, 0700); e == nil {
			e = smoke(ctx, attempt, root, os.Args[3], filepath.Join(root, "linux-generic.vhdx"))
		}
	} else if len(os.Args) == 3 && os.Args[1] == "prepare-generic" {
		e = prepareGeneric(ctx, os.Args[2])
	} else if len(os.Args) == 3 && os.Args[1] == "capture-sdk" {
		e = captureSDK(ctx, os.Args[2])
	} else if len(os.Args) == 4 && os.Args[1] == "sdk-smoke" {
		e = sdkSmoke(ctx, os.Args[2], os.Args[3])
	} else {
		e = errors.New("usage: ci2737-host extract/tools <owned-root> | smoke <owned-root> <Linux-helper>")
	}
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}

func testPublicSDK(ctx context.Context, root string) error {
	s, e := readTools(root)
	if e != nil {
		return e
	}
	if e = toolInspect(ctx, s); e != nil {
		return e
	}
	if command(ctx, dockerExe, "exec", s.ID, "mkdir", "-p", "/opt/ci2737-public-tests").Run() != nil {
		return errors.New("owned SDK test directory unavailable")
	}
	f, e := os.Open(filepath.Join(root, "public-runner-sdk.tar.gz"))
	if e != nil {
		return e
	}
	defer f.Close()
	out, e := os.OpenFile(filepath.Join(root, "public-sdk-test-"+newID()+".private.log"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	defer out.Close()
	c := command(ctx, dockerExe, "exec", "-i", "-e", "CC2737_PUBLIC_SDK_STDIN=1", "-e", "TMPDIR=/opt/ci2737-public-tests", s.ID, "/usr/local/bin/ci2737-tests", "-test.v", "-test.run", "^TestPublicRunnerSDKArchive$")
	c.Stdin = f
	c.Stdout = out
	c.Stderr = out
	if c.Run() != nil {
		return errors.New("pinned public SDK extraction test failed; preserve log")
	}
	return nil
}

func captureSDK(ctx context.Context, root string) error {
	s, e := readTools(root)
	if e != nil {
		return e
	}
	if e = toolInspect(ctx, s); e != nil {
		return e
	}
	if command(ctx, dockerExe, "exec", s.ID, "sh", "-c", "test ! -e /opt/actions-runner/.runner && test ! -e /opt/actions-runner/.credentials && test ! -e /opt/actions-runner/.credentials_rsaparams").Run() != nil {
		return errors.New("configured SDK rejected without reading credentials")
	}
	f, e := os.OpenFile(filepath.Join(root, "public-runner-sdk.tar.gz"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	c := command(ctx, dockerExe, "exec", s.ID, "tar", "-czf", "-", "-C", "/opt/actions-runner", ".")
	c.Stdout = f
	e = c.Run()
	if e == nil {
		e = f.Sync()
	}
	e = errors.Join(e, f.Close())
	if e != nil {
		return errors.New("public runner SDK capture failed")
	}
	runner, e := os.ReadFile(filepath.Join(root, "public-runner-sdk.tar.gz"))
	if e != nil || len(runner) > 768<<20 {
		return errors.New("public runner SDK capture size rejected")
	}
	rustup, e := command(ctx, dockerExe, "exec", s.ID, "cat", "/opt/rust/bin/rustup").Output()
	if e != nil || !bytes.HasPrefix(rustup, []byte{0x7f, 'E', 'L', 'F'}) || len(rustup) > 64<<20 {
		return errors.New("public rustup SDK capture rejected")
	}
	if e = writeNew(filepath.Join(root, "public-rustup"), rustup); e != nil {
		return e
	}
	sccache, e := os.ReadFile(filepath.Join(root, "sccache-linux-verified.tar.gz"))
	if e != nil || hash(sccache) != publicSccacheSHA {
		return errors.New("public sccache SDK capture rejected")
	}
	return record(root, "public-sdk-manifest.private.json", sdkManifest{publicRunnerImage, hash(runner), hash(rustup), publicSccacheSHA})
}

func sdkSmoke(ctx context.Context, root, helper string) (ret error) {
	i, e := newIdentity()
	if e != nil {
		return e
	}
	instance := newID()
	attempt := filepath.Join(root, "sdk-attempt-"+instance)
	if e = os.Mkdir(attempt, 0700); e != nil {
		return e
	}
	if e = prepareSeed(ctx, attempt, root, helper, instance, i); e != nil {
		return e
	}
	s, e := createVM(ctx, attempt, instance, filepath.Join(root, "linux-generic.vhdx"))
	if s.ID != "" {
		defer func() {
			clean, stop := context.WithTimeout(context.Background(), 5*time.Minute)
			defer stop()
			ret = errors.Join(ret, cleanupVM(clean, s))
		}()
	}
	if e != nil {
		return e
	}
	exe, e := os.Executable()
	if e != nil {
		return e
	}
	body := "& " + psQuote(exe) + " serial " + psQuote(s.Pipe) + " " + psQuote(instance) + " " + psQuote(filepath.Join(attempt, "guest-ready.private.json")) + "\nif($LASTEXITCODE -ne 0){throw 'SERIAL_PROBE_FAILED'}\n"
	if e = admin(ctx, attempt, "vm-readiness", body); e != nil {
		return e
	}
	b, e := os.ReadFile(filepath.Join(attempt, "guest-ready.private.json"))
	var r ready
	if e != nil || json.Unmarshal(b, &r) != nil || validateReady(r, instance) != nil {
		return errors.New("SDK guest readiness unavailable")
	}
	c, e := guestDial(i, r)
	if e != nil {
		return e
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(13 * time.Minute))
	if e = json.NewEncoder(c).Encode(request{"prepare-sdk", instance}); e != nil {
		return e
	}
	// Read the typed response concurrently: a rejected SDK may return before
	// all blobs were sent. Do not discard its reason behind a write/reset error.
	upload := make(chan error, 1)
	go func() {
		for _, name := range []string{"public-sdk-manifest.private.json", "public-runner-sdk.tar.gz", "public-rustup", "sccache-linux-verified.tar.gz"} {
			data, err := os.ReadFile(filepath.Join(root, name))
			if err == nil {
				err = writeBlob(c, data)
			}
			if err != nil {
				upload <- err
				return
			}
		}
		upload <- nil
	}()
	var result sdkResult
	e = json.NewDecoder(io.LimitReader(c, 24<<20)).Decode(&result)
	_ = c.Close()
	uploadErr := <-upload
	if e != nil {
		return errors.Join(errors.New("SDK guest result unavailable"), uploadErr)
	}
	if result.ID != instance || result.ProductSource || result.RunnerRegistered {
		return errors.New("SDK guest result identity rejected")
	}
	if e = writeNew(filepath.Join(attempt, "sdk-bootstrap.private.log"), result.Log); e != nil {
		return e
	}
	logSHA := hash(result.Log)
	result.Log = nil
	if e = record(attempt, "sdk-result.private.json", result); e != nil {
		return e
	}
	if e = record(attempt, "sdk-log-hash.private.json", map[string]any{"sha256": logSHA, "official_ci_success": false}); e != nil {
		return e
	}
	if result.Failure != "" {
		return errors.New("public SDK bootstrap failed; see typed result and private log")
	}
	if e = validatePublicSDKSeal(result); e != nil {
		return e
	}
	return uploadErr
}

func prepareGeneric(ctx context.Context, root string) error {
	s, e := readTools(root)
	if e != nil {
		return e
	}
	if e = toolInspect(ctx, s); e != nil {
		return e
	}
	b, e := os.ReadFile(filepath.Join(root, "linux-public.qcow2"))
	if e != nil || len(b) > 1<<30 || hash(b) != genericImageHash || !bytes.HasPrefix(b, []byte{'Q', 'F', 'I', 0xfb}) {
		return errors.New("pinned generic public image rejected")
	}
	f, e := os.OpenFile(filepath.Join(root, "generic-conversion.private.log"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	defer f.Close()
	c := command(ctx, dockerExe, "exec", s.ID, "sh", "-c", "DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends qemu-utils && mkdir /opt/ci2737-public")
	c.Stdout = f
	c.Stderr = f
	if c.Run() != nil {
		return errors.New("public qemu installation failed")
	}
	c = command(ctx, dockerExe, "exec", "-i", s.ID, "sh", "-c", "cat > /opt/ci2737-public/noble.qcow2")
	c.Stdin = bytes.NewReader(b)
	c.Stdout = f
	c.Stderr = f
	if c.Run() != nil {
		return errors.New("public qcow2 transfer failed")
	}
	b = nil
	c = command(ctx, dockerExe, "exec", s.ID, "qemu-img", "convert", "-f", "qcow2", "-O", "vhdx", "-o", "subformat=dynamic", "/opt/ci2737-public/noble.qcow2", "/opt/ci2737-public/noble.vhdx")
	c.Stdout = f
	c.Stderr = f
	if c.Run() != nil {
		return errors.New("public generic VHDX conversion failed")
	}
	out, e := os.OpenFile(filepath.Join(root, "linux-generic.vhdx"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	c = command(ctx, dockerExe, "exec", s.ID, "cat", "/opt/ci2737-public/noble.vhdx")
	c.Stdout = out
	c.Stderr = f
	e = c.Run()
	if e == nil {
		e = out.Sync()
	}
	e = errors.Join(e, out.Close())
	if e != nil {
		return errors.New("public generic VHDX transfer incomplete")
	}
	return record(root, "generic-image-ready.private.json", map[string]any{"source_qcow2_sha256": genericImageHash, "product_source": false})
}
