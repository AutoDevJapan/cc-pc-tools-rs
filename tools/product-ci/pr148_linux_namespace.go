package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type pr148Namespace struct {
	Name, ID                             string
	CreationUncertain, TreeGone, Removed bool
}

func pr148OwnedReceiptRoot(ctx context.Context, dir string) error {
	abs, e := filepath.Abs(dir)
	if e != nil {
		return e
	}
	id := strings.TrimPrefix(filepath.Base(abs), "codex-owned-")
	if len(id) != 32 || strings.Trim(id, "0123456789abcdef") != "" {
		return errors.New("safe-delete created receipt root required")
	}
	b, e := cmd(ctx, `D:\CC\Tools\safe-delete\safe-delete.exe`, "plan", "--id", id).Output()
	var r struct{ ID, Path, Action string }
	if e != nil || json.Unmarshal(b, &r) != nil || r.ID != id || !strings.EqualFold(filepath.Clean(r.Path), filepath.Clean(abs)) || r.Action != "submit_to_exec_tool" {
		return errors.New("receipt root ownership verification failed")
	}
	return nil
}

func pr148CreateArgs(name string) []string {
	a := codeQLCreateArgs(name)
	for i := range a {
		if a[i] == "cc.todo=2648" {
			a[i] = "cc.todo=2736"
		}
		if a[i] == "--memory" {
			a[i+1] = "16g"
		}
	}
	// The toolchain lives outside the runner's Landlock-visible filesystem.
	idx := len(a) - 3
	return append(append(append([]string{}, a[:idx]...), "--memory-swap", "16g", "--tmpfs", "/toolchain:rw,exec,nosuid,size=8g,uid=1001,gid=1001"), a[idx:]...)
}

func pr148Isolation(data []byte, name, id string) error {
	var raw []map[string]json.RawMessage
	if json.Unmarshal(data, &raw) != nil || len(raw) != 1 {
		return errors.New("Linux isolation unavailable")
	}
	var identity struct {
		ID, Name string
		Config   struct {
			Image, User string
			Labels      map[string]string
		}
	}
	entry, _ := json.Marshal(raw[0])
	if json.Unmarshal(entry, &identity) != nil || identity.ID != id || identity.Name != "/"+name || identity.Config.Labels["cc.instance"] != name || identity.Config.Labels["cc.todo"] != "2736" {
		return errors.New("Linux namespace owner rejected")
	}
	var i []isolation
	if json.Unmarshal(data, &i) != nil || len(i) != 1 {
		return errors.New("Linux isolation parse failed")
	}
	var limits []struct {
		HostConfig struct{ Memory, MemorySwap int64 }
	}
	if json.Unmarshal(data, &limits) != nil || len(limits) != 1 || limits[0].HostConfig.Memory != 16<<30 || limits[0].HostConfig.MemorySwap != 16<<30 {
		return errors.New("fixed Linux memory/no-swap budget rejected")
	}
	// The Rust toolchain, tmpfs artifacts and linker shared the old 8 GiB
	// cgroup; real memory.events recorded oom_kill=1 even at two jobs. Only
	// this PR148 profile gets 16 GiB, with swap disabled. Validate that exact
	// budget before adapting the copy for the unchanged legacy validator.
	i[0].HostConfig.Memory = 8 << 30
	// Reuse only the established static validator, never its runner or admission.
	i[0].Config.Labels["cc.todo"] = "2648"
	options, ok := i[0].HostConfig.Tmpfs["/toolchain"]
	if !ok || options != "rw,exec,nosuid,size=8g,uid=1001,gid=1001" {
		return errors.New("private toolchain tmpfs rejected")
	}
	delete(i[0].HostConfig.Tmpfs, "/toolchain")
	var mounts = i[0].Mounts[:0]
	for _, m := range i[0].Mounts {
		if m.Destination == "/toolchain" && m.Type == "tmpfs" {
			continue
		}
		mounts = append(mounts, m)
	}
	i[0].Mounts = mounts
	b, _ := json.Marshal(i)
	return codeQLIsolation(b)
}

func (n *pr148Namespace) inspect(ctx context.Context) ([]byte, error) {
	b, e := cmd(ctx, docker, "inspect", n.Name).Output()
	if e != nil {
		return nil, errors.New("owned Linux namespace inspection unavailable")
	}
	if n.ID == "" {
		var v []struct{ ID string }
		if json.Unmarshal(b, &v) != nil || len(v) != 1 || len(v[0].ID) != 64 || strings.Trim(v[0].ID, "0123456789abcdef") != "" {
			return nil, errors.New("owned namespace ID unavailable")
		}
		n.ID = v[0].ID
	}
	if e = pr148Isolation(b, n.Name, n.ID); e != nil {
		return nil, e
	}
	return b, nil
}

func (n *pr148Namespace) stop() error {
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	if _, e := n.inspect(ctx); e != nil {
		return e
	}
	if e := cmd(ctx, docker, "stop", "--time", "10", n.ID).Run(); e != nil {
		return errors.New("owned namespace stop failed")
	}
	b, e := n.inspect(ctx)
	var v []struct {
		State struct {
			Running bool
			Pid     int
		}
	}
	if e != nil || json.Unmarshal(b, &v) != nil || len(v) != 1 || v[0].State.Running || v[0].State.Pid != 0 {
		return errors.New("owned namespace process absence unverified")
	}
	n.TreeGone = true
	return nil
}
func (n *pr148Namespace) dispose() error {
	if e := n.stop(); e != nil {
		return e
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if e := cmd(ctx, docker, "rm", "-v", n.ID).Run(); e != nil {
		return errors.New("owned namespace removal failed")
	}
	n.Removed = true
	return nil
}

// Intent is fsynced before Docker create. A lost create reply is recovered by
// the exact name/instance/image; uncertainty never authorizes another create.
func newPR148Namespace(ctx context.Context, dir string) (*pr148Namespace, error) {
	n := &pr148Namespace{Name: fmt.Sprintf("todo2736-linux-%d", time.Now().UnixNano()), CreationUncertain: true}
	if e := pr148SaveNew(filepath.Join(dir, "namespace-intent.private.json"), n); e != nil {
		return n, e
	}
	b, createErr := cmd(ctx, docker, pr148CreateArgs(n.Name)...).Output()
	probe, stop := context.WithTimeout(context.Background(), 15*time.Second)
	defer stop()
	if _, e := n.inspect(probe); e != nil {
		return n, errors.New("Linux create uncertain; keep intent and candidate lock")
	}
	if createErr != nil || strings.TrimSpace(string(b)) != n.ID {
		return n, errors.New("Linux create reply uncertain; exact owned namespace recovered for cleanup")
	}
	n.CreationUncertain = false
	if e := cmd(ctx, docker, "start", n.ID).Run(); e != nil {
		return n, errors.New("Linux namespace start failed")
	}
	return n, nil
}

func pr148SaveNew(path string, value any) error {
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
