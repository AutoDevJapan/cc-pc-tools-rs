package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	dockerExe = "C:\\Program Files\\Docker\\Docker\\resources\\bin\\docker.exe"
	ghGate    = "D:\\CC\\Tools\\codex-jobs\\gh_gate.exe"
	cargoGate = "D:\\CC\\Tools\\codex-jobs\\cargo_gate.exe"
	gateDir   = "D:\\CC\\Tools\\codex-jobs"
	// The already verified immutable credential-free SDK image digest.
	sdkImage = "sha256:89988fa8449f89b0d8f12fd2369aa5b167bfe3389189df877808a15055869011"
	// Official PowerShell/PowerShell v7.6.7 linux-x64.tar.gz.
	powerShellSHA = "6896f9e3a6b0a354bf470835a96da76d2f801cd50dcf047ad7c4563bf93bf431"
	// Owner of every namespace this module creates.
	nsOwner = "civ6-ci-linux"
	nsTodo  = "2962"
	gib     = uint64(1) << 30
)

func digest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

func exitStatus(e error) int {
	if e == nil {
		return 0
	}
	type exitCoder interface{ ExitCode() int }
	if x, ok := e.(exitCoder); ok {
		return x.ExitCode()
	}
	return -1
}

// cmd never opens a console window and routes Docker through the local Desktop
// engine only (never a DOCKER_HOST/context from the environment).
func cmd(ctx context.Context, name string, args ...string) *exec.Cmd {
	if name == dockerExe {
		args = append([]string{"--host", "npipe:////./pipe/dockerDesktopLinuxEngine"}, args...)
	}
	c := exec.CommandContext(ctx, name, args...)
	hidden(c)
	c.WaitDelay = 5 * time.Second
	if name == dockerExe {
		var env []string
		for _, s := range os.Environ() {
			if k, _, _ := strings.Cut(s, "="); !strings.HasPrefix(strings.ToUpper(k), "DOCKER_") {
				env = append(env, s)
			}
		}
		c.Env = env
	}
	return c
}

func saveNew(path string, value any) error {
	f, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	e = json.NewEncoder(f).Encode(value)
	if e == nil {
		e = f.Sync()
	}
	return errors.Join(e, f.Close())
}

// profile is the fixed resource contract of a namespace. Rust gets 10 GiB and
// no swap (real memory.events recorded oom_kill at 8 GiB); CodeQL keeps its
// original 8 GiB. Neither can reach a host path, socket, device or GPU.
type profile struct {
	memGiB    int
	toolchain bool
}

func profileFor(k Kind) profile {
	if k == KindRust {
		return profile{10, true}
	}
	return profile{8, false}
}

func (p profile) tmpfs() map[string]string {
	m := map[string]string{
		"/runner":      "rw,exec,nosuid,size=4g,uid=1001,gid=1001",
		"/work":        "rw,exec,nosuid,size=16g,uid=1001,gid=1001",
		"/tmp":         "rw,nosuid,noexec,size=2g,uid=1001,gid=1001",
		"/home/runner": "rw,nosuid,size=1g,uid=1001,gid=1001",
	}
	if p.toolchain {
		m["/toolchain"] = "rw,exec,nosuid,size=8g,uid=1001,gid=1001"
	}
	return m
}

type namespace struct {
	Name, ID                             string
	Kind                                 Kind
	CreationUncertain, TreeGone, Removed bool
	Journal                              string `json:"Journal,omitempty"`
}

func createArgs(name string, k Kind, p Plan) []string {
	pr := profileFor(k)
	mem := strconv.Itoa(pr.memGiB) + "g"
	a := []string{"create", "--name", name,
		"--label", "cc.owner=" + nsOwner, "--label", "cc.todo=" + nsTodo, "--label", "cc.job=" + string(k),
		"--label", "cc.run=" + strconv.FormatInt(p.RunID, 10), "--label", "cc.job_id=" + strconv.FormatInt(p.JobID, 10),
		"--label", "cc.instance=" + name,
		"--init", "--user", "1001:1001", "--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--read-only",
		"--network", "bridge", "--cpus", "2", "--memory", mem, "--memory-swap", mem, "--pids-limit", "512"}
	for _, dir := range []string{"/runner", "/work", "/tmp", "/home/runner", "/toolchain"} {
		if o, ok := pr.tmpfs()[dir]; ok {
			a = append(a, "--tmpfs", dir+":"+o)
		}
	}
	return append(a, sdkImage, "sleep", "infinity")
}

type inspected struct {
	ID, Name string
	State    struct {
		Running bool
		Pid     int
	}
	Config struct {
		Image, User string
		Labels      map[string]string
	}
	HostConfig struct {
		Init                                *bool
		Privileged, ReadonlyRootfs          bool
		NetworkMode, PidMode                string
		Binds, CapAdd, CapDrop, SecurityOpt []string
		Devices, DeviceRequests             []json.RawMessage
		Tmpfs                               map[string]string
		NanoCPUs, Memory, MemorySwap        int64
		PidsLimit                           *int64
	}
	Mounts []struct{ Type, Destination string }
}

func parseInspect(data []byte) (inspected, error) {
	var all []inspected
	if json.Unmarshal(data, &all) != nil || len(all) != 1 {
		return inspected{}, errors.New("namespace inspection unavailable")
	}
	return all[0], nil
}

// validateIsolation rejects any drift from the fixed namespace: image, owner
// labels, no host mount/socket/device/GPU, capabilities, privileges, CPU, memory
// (no swap), PIDs and the exact tmpfs set.
func validateIsolation(data []byte, name, id string, k Kind) error {
	i, e := parseInspect(data)
	if e != nil {
		return e
	}
	h := i.HostConfig
	pr := profileFor(k)
	if (id != "" && i.ID != id) || i.Name != "/"+name || i.Config.Image != sdkImage || i.Config.User != "1001:1001" ||
		i.Config.Labels["cc.instance"] != name || i.Config.Labels["cc.owner"] != nsOwner || i.Config.Labels["cc.todo"] != nsTodo || i.Config.Labels["cc.job"] != string(k) {
		return errors.New("namespace owner/image rejected")
	}
	mem := int64(pr.memGiB) << 30
	if h.Init == nil || !*h.Init || h.Privileged || !h.ReadonlyRootfs || h.NetworkMode != "bridge" || h.PidMode != "" ||
		len(h.Binds) != 0 || len(h.CapAdd) != 0 || len(h.Devices) != 0 || len(h.DeviceRequests) != 0 ||
		h.NanoCPUs != 2000000000 || h.Memory != mem || h.MemorySwap != mem || h.PidsLimit == nil || *h.PidsLimit != 512 {
		return errors.New("namespace isolation or fixed resource budget rejected")
	}
	if len(h.CapDrop) != 1 || !strings.EqualFold(h.CapDrop[0], "ALL") || len(h.SecurityOpt) != 1 ||
		(h.SecurityOpt[0] != "no-new-privileges" && h.SecurityOpt[0] != "no-new-privileges=true") {
		return errors.New("namespace privileges rejected")
	}
	want := pr.tmpfs()
	if len(h.Tmpfs) != len(want) {
		return errors.New("namespace scratch set rejected")
	}
	for dir, opt := range want {
		if h.Tmpfs[dir] != opt {
			return errors.New("namespace scratch options rejected")
		}
	}
	for _, m := range i.Mounts {
		if _, ok := want[m.Destination]; m.Type != "tmpfs" || !ok {
			return errors.New("namespace host mount rejected")
		}
	}
	return nil
}

func (n *namespace) inspect(ctx context.Context) ([]byte, error) {
	b, e := cmd(ctx, dockerExe, "inspect", n.Name).Output()
	if e != nil {
		return nil, errors.New("owned namespace inspection unavailable")
	}
	if n.ID == "" {
		i, e := parseInspect(b)
		if e != nil || len(i.ID) != 64 || strings.Trim(i.ID, "0123456789abcdef") != "" {
			return nil, errors.New("owned namespace ID unavailable")
		}
		n.ID = i.ID
	}
	if e = validateIsolation(b, n.Name, n.ID, n.Kind); e != nil {
		return nil, e
	}
	return b, nil
}

// stop proves the PID namespace is empty (not running, PID 0) and is the only
// way a Cargo/CodeQL tree is declared gone.
func (n *namespace) stop() error {
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	b, e := n.inspect(ctx)
	if e != nil {
		return e
	}
	state, e := parseInspect(b)
	if e != nil {
		return e
	}
	if !state.State.Running && state.State.Pid == 0 {
		n.TreeGone = true
		return nil
	}
	if n.Journal != "" {
		if e = saveNew(filepath.Join(n.Journal, "namespace-stop-attempt.private.json"), map[string]string{"name": n.Name, "id": n.ID}); e != nil {
			return errors.New("namespace stop already attempted; observe only, never repeat")
		}
	}
	if e := cmd(ctx, dockerExe, "stop", "--time", "10", n.ID).Run(); e != nil {
		return errors.New("owned namespace stop failed")
	}
	b, e = n.inspect(ctx)
	if e != nil {
		return e
	}
	i, e := parseInspect(b)
	if e != nil || i.State.Running || i.State.Pid != 0 {
		return errors.New("owned namespace process absence unverified")
	}
	n.TreeGone = true
	return nil
}

func (n *namespace) dispose() error {
	if !n.TreeGone {
		if e := n.stop(); e != nil {
			return e
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if n.Journal != "" {
		if e := saveNew(filepath.Join(n.Journal, "namespace-remove-attempt.private.json"), map[string]string{"name": n.Name, "id": n.ID}); e != nil {
			return errors.New("namespace removal already attempted; observe only, never repeat")
		}
	}
	if e := cmd(ctx, dockerExe, "rm", "-v", n.ID).Run(); e != nil {
		return errors.New("owned namespace removal failed")
	}
	n.Removed = true
	return nil
}

// newNamespace saves intent BEFORE Docker create. A lost create reply is
// recovered by the exact name/labels/image; uncertainty never permits another
// create.
func newNamespace(ctx context.Context, dir string, k Kind, p Plan) (*namespace, error) {
	n := &namespace{Name: fmt.Sprintf("civ6-ci-linux-%s-%d", k, time.Now().UnixNano()), Kind: k, CreationUncertain: true, Journal: dir}
	if e := saveNew(filepath.Join(dir, "namespace-intent.private.json"), n); e != nil {
		return n, e
	}
	b, createErr := cmd(ctx, dockerExe, createArgs(n.Name, k, p)...).Output()
	probe, stop := context.WithTimeout(context.Background(), 15*time.Second)
	defer stop()
	if _, e := n.inspect(probe); e != nil {
		return n, errors.New("Linux create uncertain; keep intent and the job lock")
	}
	if createErr != nil || strings.TrimSpace(string(b)) != n.ID {
		return n, errors.New("Linux create reply uncertain; exact owned namespace recovered for cleanup")
	}
	n.CreationUncertain = false
	if e := cmd(ctx, dockerExe, "start", n.ID).Run(); e != nil {
		return n, errors.New("Linux namespace start failed")
	}
	return n, nil
}

// execStdin sends bytes to a command in the namespace through stdin only.
func (n *namespace) execStdin(ctx context.Context, data []byte, script string) error {
	c := cmd(ctx, dockerExe, "exec", "-i", n.ID, "sh", "-c", script)
	c.Stdin = bytes.NewReader(data)
	return c.Run()
}

// vmMemAvailable reads the Docker VM's real MemAvailable from inside the fresh
// namespace (nothing is reserved yet, so the figure is the VM's, not ours).
func (n *namespace) vmMemAvailable(ctx context.Context) (uint64, error) {
	out, e := boundedOutput(cmd(ctx, dockerExe, "exec", n.ID, "cat", "/proc/meminfo"), 16<<10)
	if e != nil {
		return 0, errors.New("VM memory observation unavailable")
	}
	return parseMemAvailable(out)
}

func parseMemAvailable(out []byte) (uint64, error) {
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) == 3 && f[0] == "MemAvailable:" && f[2] == "kB" {
			kb, e := strconv.ParseUint(f[1], 10, 64)
			if e == nil {
				return kb << 10, nil
			}
		}
	}
	return 0, errors.New("VM MemAvailable absent")
}

// checkCapacity: the namespace budget plus a 1 GiB reservation must fit in the
// VM's real MemAvailable; the host needs the reservation in physical memory and
// the receipt drive needs room for full logs. A shortage rejects, never waits.
func checkCapacity(k Kind, vmAvail, hostPhys, diskFree uint64) error {
	budget := uint64(profileFor(k).memGiB) * gib
	const reserve = gib
	if vmAvail < budget+reserve {
		return fmt.Errorf("VM free memory %d is below the %d byte namespace budget plus 1 GiB reservation", vmAvail, budget)
	}
	if hostPhys < reserve {
		return errors.New("host free memory is below the 1 GiB reservation")
	}
	if diskFree < 4*gib {
		return errors.New("receipt drive free space is below 4 GiB")
	}
	return nil
}
