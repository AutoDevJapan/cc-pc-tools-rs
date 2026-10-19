package main

// This profile alone moves Cargo artifacts off /work's RAM-backed tmpfs.
// Source, credentials, runner and toolchain retain their existing mounts.
import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"time"
)

const pr148TargetPath = pr148Workspace + "/tools/target"
const pr148TargetInitScript = `set -eu
test "$(id -u):$(id -g)" = 1001:1001
mkdir /tmp/target
chmod 700 /tmp/target
test "$(stat -c '%u:%g:%a' /tmp/target)" = 1001:1001:700
`

func pr148TargetInitLabels(owner, nonce string) map[string]string {
	// The immutable SDK inherits this public provenance label from rust's base
	// image. It grants no ownership; all three per-instance labels remain exact.
	return map[string]string{"cc.todo": "2736", "cc.instance": owner, "cc.nonce": nonce, "org.opencontainers.image.source": "https://github.com/rust-lang/docker-rust"}
}

type pr148DockerIO func(context.Context, ...string) ([]byte, error)

func pr148DockerCall(ctx context.Context, args ...string) ([]byte, error) {
	return cmd(ctx, docker, args...).Output()
}

type pr148TargetVolume struct {
	Name, Owner, Nonce, CreatedAt, Mountpoint        string
	NotBefore                                        time.Time
	CreateAttempted, Owned, Removed, RemoveAttempted bool
}

type pr148TargetInit struct {
	Name, ID                                                    string
	CreateAttempted, Owned, Removed, TreeGone, CleanupAttempted bool
}

func (v *pr148TargetVolume) labels() map[string]string {
	return map[string]string{"cc.todo": "2736", "cc.instance": v.Owner, "cc.nonce": v.Nonce, "cc.role": "cargo-target"}
}

func pr148Absent(ctx context.Context, call pr148DockerIO, kind, name string) error {
	var args []string
	if kind == "volume" {
		args = []string{"volume", "ls", "--filter", "name=^" + name + "$", "--format", "{{.Name}}"}
	} else {
		args = []string{"container", "ls", "--all", "--filter", "name=^/" + name + "$", "--format", "{{.ID}}"}
	}
	b, e := call(ctx, args...)
	if e != nil || strings.TrimSpace(string(b)) != "" {
		return errors.New("new owned " + kind + " absence unverified")
	}
	return nil
}

func (v *pr148TargetVolume) inspect(ctx context.Context, call pr148DockerIO) error {
	b, e := call(ctx, "volume", "inspect", v.Name)
	if e != nil {
		return errors.New("target volume inspection unavailable")
	}
	var all []struct {
		Name, Driver, Scope, CreatedAt, Mountpoint string
		Labels, Options                            map[string]string
	}
	if json.Unmarshal(b, &all) != nil || len(all) != 1 {
		return errors.New("target volume metadata rejected")
	}
	i := all[0]
	created, ce := time.Parse(time.RFC3339Nano, i.CreatedAt)
	if v.Name != v.Owner+"-target-"+v.Nonce || len(v.Nonce) != 32 || strings.Trim(v.Nonce, "0123456789abcdef") != "" ||
		i.Name != v.Name || i.Driver != "local" || i.Scope != "local" || len(i.Options) != 0 || !reflect.DeepEqual(i.Labels, v.labels()) ||
		i.Mountpoint != "/var/lib/docker/volumes/"+v.Name+"/_data" || ce != nil || v.NotBefore.IsZero() ||
		created.Before(v.NotBefore.Truncate(time.Second)) || created.After(time.Now().Add(time.Second)) ||
		(v.CreatedAt != "" && (v.CreatedAt != i.CreatedAt || v.Mountpoint != i.Mountpoint)) {
		return errors.New("new private local target volume ownership rejected")
	}
	v.CreatedAt, v.Mountpoint, v.Owned = i.CreatedAt, i.Mountpoint, true
	return nil
}

func (v *pr148TargetVolume) create(ctx context.Context, dir string, call pr148DockerIO) error {
	if e := pr148Absent(ctx, call, "volume", v.Name); e != nil {
		return e
	}
	v.NotBefore, v.CreateAttempted, v.Removed = time.Now().UTC(), true, false
	if e := pr148SaveNew(filepath.Join(dir, "target-volume-intent.private.json"), v); e != nil {
		v.CreateAttempted = false
		v.Removed = true
		return e
	}
	a := []string{"volume", "create", "--driver", "local"}
	for _, k := range []string{"cc.todo", "cc.instance", "cc.nonce", "cc.role"} {
		a = append(a, "--label", k+"="+v.labels()[k])
	}
	a = append(a, v.Name)
	b, createErr := call(ctx, a...)
	probe, stop := context.WithTimeout(context.Background(), 15*time.Second)
	defer stop()
	if e := v.inspect(probe, call); e != nil {
		if pr148Absent(probe, call, "volume", v.Name) == nil {
			v.Removed = true
		}
		return errors.New("target volume create uncertain; retain intent, never repeat create")
	}
	if createErr != nil || strings.TrimSpace(string(b)) != v.Name {
		return errors.New("target volume create reply uncertain; owned volume retained for cleanup")
	}
	return pr148SaveNew(filepath.Join(dir, "target-volume-owned.private.json"), v)
}

func pr148TargetMount(v *pr148TargetVolume, dest, subpath string) string {
	s := "type=volume,source=" + v.Name + ",target=" + dest
	if subpath != "" {
		s += ",volume-subpath=" + subpath + ",volume-nocopy"
	}
	return s
}

// Validate the unmodified Docker inspect, including the actual resolved Source
// AND HostConfig's subpath (Docker reports the volume root as Mounts.Source).
// Only then remove the one approved mount from the legacy validator's copy.
func pr148CheckTargetMount(data []byte, v *pr148TargetVolume, dest, subpath string) error {
	if v == nil || !v.Owned || v.Removed || v.CreatedAt == "" {
		return errors.New("owned target receipt required")
	}
	var all []struct {
		Mounts []struct {
			Type, Name, Source, Destination, Driver, Mode, Propagation string
			RW                                                         bool
		}
		HostConfig struct {
			Mounts      []map[string]any
			VolumesFrom []string
		}
	}
	if json.Unmarshal(data, &all) != nil || len(all) != 1 {
		return errors.New("target mount inspect rejected")
	}
	i := all[0]
	want := map[string]any{"Type": "volume", "Source": v.Name, "Target": dest}
	if subpath != "" {
		want["VolumeOptions"] = map[string]any{"NoCopy": true, "Subpath": subpath}
	}
	if len(i.HostConfig.VolumesFrom) != 0 || len(i.HostConfig.Mounts) != 1 || !reflect.DeepEqual(i.HostConfig.Mounts[0], want) {
		return errors.New("target mount declaration/subpath rejected")
	}
	found := 0
	seen := map[string]bool{}
	for _, m := range i.Mounts {
		if seen[m.Destination] {
			return errors.New("duplicate mount rejected")
		}
		seen[m.Destination] = true
		if m.Destination == dest {
			found++
			if m.Type != "volume" || m.Name != v.Name || m.Source != v.Mountpoint || m.Driver != "local" || !m.RW || m.Propagation != "" || m.Mode != "z" {
				return errors.New("actual private target mount rejected")
			}
		} else if m.Type != "tmpfs" || subpath == "" || (m.Destination != "/runner" && m.Destination != "/work" && m.Destination != "/tmp" && m.Destination != "/home/runner" && m.Destination != "/toolchain") {
			return errors.New("additional mount rejected")
		}
	}
	if found != 1 {
		return errors.New("target mount absent")
	}
	return nil
}

func pr148TargetIsolation(data []byte, name string, v *pr148TargetVolume) ([]byte, error) {
	if v == nil || v.Owner != name {
		return nil, errors.New("target owner mismatch")
	}
	if e := pr148CheckTargetMount(data, v, pr148TargetPath, "target"); e != nil {
		return nil, e
	}
	var all []map[string]any
	if json.Unmarshal(data, &all) != nil {
		return nil, errors.New("target normalization rejected")
	}
	config, ok := all[0]["Config"].(map[string]any)
	if !ok {
		return nil, errors.New("namespace config rejected")
	}
	labels, ok := config["Labels"].(map[string]any)
	if !ok || labels["cc.nonce"] != v.Nonce {
		return nil, errors.New("namespace nonce rejected")
	}
	var kept []any
	for _, m := range all[0]["Mounts"].([]any) {
		if m.(map[string]any)["Destination"] != pr148TargetPath {
			kept = append(kept, m)
		}
	}
	all[0]["Mounts"] = kept
	return json.Marshal(all)
}

func pr148PIDZero(b []byte) bool {
	var all []struct {
		State struct {
			Running bool
			Pid     int
			Status  string
		}
	}
	return json.Unmarshal(b, &all) == nil && len(all) == 1 && !all[0].State.Running && all[0].State.Pid == 0 && (all[0].State.Status == "created" || all[0].State.Status == "exited")
}

func (n *pr148Namespace) targetCreateArgs() []string {
	a := pr148CreateArgs(n.Name)
	end := len(a) - 3
	return append(append(append([]string{}, a[:end]...), "--label", "cc.nonce="+n.Nonce, "--mount", pr148TargetMount(n.Target, pr148TargetPath, "target")), a[end:]...)
}

func (n *pr148Namespace) initArgs() []string {
	return []string{"create", "--name", n.Init.Name, "--label", "cc.todo=2736", "--label", "cc.instance=" + n.Name, "--label", "cc.nonce=" + n.Nonce, "--init", "--user", "1001:1001", "--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--read-only", "--network", "none", "--cpus", "2", "--memory", "128m", "--memory-swap", "128m", "--pids-limit", "512", "--mount", pr148TargetMount(n.Target, "/tmp", ""), codeQLSDK, "sh", "-c", pr148TargetInitScript}
}

func (n *pr148Namespace) inspectInit(ctx context.Context, call pr148DockerIO) ([]byte, error) {
	b, e := call(ctx, "inspect", n.Init.Name)
	if e != nil {
		return nil, errors.New("target init inspect unavailable")
	}
	var all []struct {
		ID, Name string
		Config   struct {
			Image, User string
			Labels      map[string]string
			Cmd         []string
		}
		HostConfig struct {
			Init                                *bool
			ReadonlyRootfs, Privileged          bool
			NetworkMode, PidMode                string
			NanoCPUs, Memory, MemorySwap        int64
			PidsLimit                           int
			Binds, CapAdd, CapDrop, SecurityOpt []string
			Devices, DeviceRequests             []json.RawMessage
			Tmpfs                               map[string]string
		}
	}
	if json.Unmarshal(b, &all) != nil || len(all) != 1 {
		return nil, errors.New("target init metadata rejected")
	}
	i := all[0]
	h := i.HostConfig
	if len(i.ID) != 64 || strings.Trim(i.ID, "0123456789abcdef") != "" || (n.Init.ID != "" && n.Init.ID != i.ID) || i.Name != "/"+n.Init.Name || i.Config.Image != codeQLSDK || i.Config.User != "1001:1001" ||
		!reflect.DeepEqual(i.Config.Labels, pr148TargetInitLabels(n.Name, n.Nonce)) || !reflect.DeepEqual(i.Config.Cmd, []string{"sh", "-c", pr148TargetInitScript}) ||
		h.Init == nil || !*h.Init || !h.ReadonlyRootfs || h.Privileged || h.NetworkMode != "none" || h.PidMode != "" || h.NanoCPUs != 2e9 || h.Memory != 128<<20 || h.MemorySwap != 128<<20 || h.PidsLimit != 512 ||
		len(h.Binds) != 0 || len(h.CapAdd) != 0 || len(h.Devices) != 0 || len(h.DeviceRequests) != 0 || len(h.Tmpfs) != 0 || !reflect.DeepEqual(h.CapDrop, []string{"ALL"}) || len(h.SecurityOpt) != 1 || (h.SecurityOpt[0] != "no-new-privileges" && h.SecurityOpt[0] != "no-new-privileges=true") {
		return nil, errors.New("target init owner/isolation rejected")
	}
	if e = n.Target.inspect(ctx, call); e != nil {
		return nil, e
	}
	if e = pr148CheckTargetMount(b, n.Target, "/tmp", ""); e != nil {
		return nil, e
	}
	n.Init.ID, n.Init.Owned = i.ID, true
	return b, nil
}

func (n *pr148Namespace) prepareTarget(ctx context.Context, call pr148DockerIO) error {
	if e := n.Target.create(ctx, n.ReceiptDir, call); e != nil {
		return e
	}
	if e := pr148Absent(ctx, call, "container", n.Init.Name); e != nil {
		return e
	}
	n.Init.CreateAttempted, n.Init.Removed = true, false
	if e := pr148SaveNew(filepath.Join(n.ReceiptDir, "target-init-intent.private.json"), n); e != nil {
		n.Init.CreateAttempted = false
		n.Init.Removed = true
		return e
	}
	b, ce := call(ctx, n.initArgs()...)
	probe, stop := context.WithTimeout(context.Background(), 15*time.Second)
	_, e := n.inspectInit(probe, call)
	if e != nil && pr148Absent(probe, call, "container", n.Init.Name) == nil {
		n.Init.Removed = true
	}
	stop()
	if e != nil {
		return errors.New("target init create uncertain; retain intent")
	}
	if ce != nil || strings.TrimSpace(string(b)) != n.Init.ID {
		return errors.New("target init create reply uncertain; cleanup only")
	}
	if _, e = call(ctx, "start", "--attach", n.Init.ID); e != nil {
		return errors.New("private target initialization failed")
	}
	b, e = n.inspectInit(ctx, call)
	var state []struct {
		State struct {
			ExitCode  int
			OOMKilled bool
		}
	}
	if e != nil || !pr148PIDZero(b) || json.Unmarshal(b, &state) != nil || state[0].State.ExitCode != 0 || state[0].State.OOMKilled {
		return errors.New("target init successful exit/PID0 unverified")
	}
	return n.disposeInit(ctx, call)
}

func newPR148NamespaceWith(ctx context.Context, dir string, call pr148DockerIO) (*pr148Namespace, error) {
	var nonce [16]byte
	if _, e := rand.Read(nonce[:]); e != nil {
		return nil, e
	}
	key := hex.EncodeToString(nonce[:])
	n := &pr148Namespace{Name: "todo2736-linux-" + key, Nonce: key, ReceiptDir: dir, CreationUncertain: true, Removed: true}
	n.Target = &pr148TargetVolume{Name: n.Name + "-target-" + key, Owner: n.Name, Nonce: key, Removed: true}
	n.Init = &pr148TargetInit{Name: n.Name + "-target-init", Removed: true}
	if e := pr148SaveNew(filepath.Join(dir, "namespace-intent.private.json"), n); e != nil {
		return n, e
	}
	if e := n.prepareTarget(ctx, call); e != nil {
		return n, e
	}
	if e := pr148Absent(ctx, call, "container", n.Name); e != nil {
		return n, e
	}
	n.CreateAttempted, n.Removed = true, false
	if e := pr148SaveNew(filepath.Join(dir, "namespace-create-intent.private.json"), n); e != nil {
		n.CreateAttempted = false
		n.Removed = true
		return n, e
	}
	b, ce := call(ctx, n.targetCreateArgs()...)
	probe, stop := context.WithTimeout(context.Background(), 15*time.Second)
	_, e := n.inspectWith(probe, call)
	if e != nil && pr148Absent(probe, call, "container", n.Name) == nil {
		n.Removed = true
	}
	stop()
	if e != nil {
		return n, errors.New("Linux create uncertain; retain target, intent and candidate lock")
	}
	if ce != nil || strings.TrimSpace(string(b)) != n.ID {
		return n, errors.New("Linux create reply uncertain; exact resources recovered for cleanup")
	}
	n.CreationUncertain = false
	if _, e = call(ctx, "start", n.ID); e != nil {
		return n, errors.New("Linux namespace start failed")
	}
	return n, nil
}

func (n *pr148Namespace) resourcesGone() bool {
	return n.Removed && (n.Init == nil || n.Init.Removed) && (n.Target == nil || n.Target.Removed)
}

func (n *pr148Namespace) disposeInit(ctx context.Context, call pr148DockerIO) error {
	i := n.Init
	if i == nil || i.Removed {
		return nil
	}
	if i.CleanupAttempted {
		return errors.New("target init cleanup already attempted; retain original failure")
	}
	i.CleanupAttempted = true
	b, e := n.inspectInit(ctx, call)
	if e != nil {
		return e
	}
	if e = pr148SaveNew(filepath.Join(n.ReceiptDir, "target-init-cleanup-intent.private.json"), n); e != nil {
		return e
	}
	if !pr148PIDZero(b) {
		if _, e = call(ctx, "stop", "--time", "10", i.ID); e != nil {
			return errors.New("target init stop failed")
		}
		b, e = n.inspectInit(ctx, call)
	}
	if e != nil || !pr148PIDZero(b) {
		return errors.New("target init PID0 unverified")
	}
	i.TreeGone = true
	if _, e = call(ctx, "rm", "-v", i.ID); e != nil {
		return errors.New("target init removal uncertain; do not repeat")
	}
	if e = pr148Absent(ctx, call, "container", i.Name); e != nil {
		return e
	}
	i.Removed = true
	return nil
}

func (n *pr148Namespace) disposeWith(ctx context.Context, call pr148DockerIO) (ret error) {
	if n.resourcesGone() {
		return nil
	}
	if n.CleanupAttempted {
		return errors.New("namespace cleanup already attempted: " + n.CleanupFailure)
	}
	n.CleanupAttempted = true
	defer func() {
		if ret != nil {
			n.CleanupFailure = ret.Error()
		}
	}()
	if n.Init != nil && !n.Init.Removed {
		if e := n.disposeInit(ctx, call); e != nil {
			ret = errors.Join(ret, e)
		}
	}
	if !n.Removed {
		if e := n.stopWith(ctx, call); e != nil {
			ret = errors.Join(ret, e)
		} else {
			if e = pr148SaveNew(filepath.Join(n.ReceiptDir, "namespace-remove-intent.private.json"), n); e == nil {
				_, e = call(ctx, "rm", "-v", n.ID)
				if e == nil {
					e = pr148Absent(ctx, call, "container", n.Name)
					if e == nil {
						n.Removed = true
					}
				}
			}
			ret = errors.Join(ret, e)
		}
	}
	// Named volumes survive rm -v. Do not release the candidate lock until an
	// explicit volume rm AND an independent absence query have both succeeded.
	if n.Removed && (n.Init == nil || n.Init.Removed) && n.Target != nil && !n.Target.Removed {
		v := n.Target
		if !v.Owned {
			return errors.Join(ret, errors.New("uncertain volume owner; retain candidate lock"))
		}
		if e := v.inspect(ctx, call); e != nil {
			return errors.Join(ret, e)
		}
		if e := pr148SaveNew(filepath.Join(n.ReceiptDir, "target-volume-remove-intent.private.json"), v); e != nil {
			return errors.Join(ret, e)
		}
		v.RemoveAttempted = true
		if _, e := call(ctx, "volume", "rm", v.Name); e != nil {
			return errors.Join(ret, errors.New("target volume removal uncertain; do not repeat"))
		}
		if e := pr148Absent(ctx, call, "volume", v.Name); e != nil {
			return errors.Join(ret, e)
		}
		v.Removed = true
	}
	if !n.resourcesGone() {
		ret = errors.Join(ret, fmt.Errorf("owned resources remain; candidate lock must be retained"))
	}
	return ret
}
