package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Synthetic Docker metadata, with mutations recorded independently of returned
// replies. Lost replies can therefore leave a real-looking owned resource.
type pr148TargetDockerFixture struct {
	t          *testing.T
	dir, fail  string
	n          *pr148Namespace
	volume     bool
	containers map[string]string
	running    map[string]bool
	calls      []string
}

func (f *pr148TargetDockerFixture) call(_ context.Context, a ...string) ([]byte, error) {
	f.calls = append(f.calls, strings.Join(a, " "))
	bad := func(s string) bool { return f.fail == s }
	encode := func(x any) ([]byte, error) { return json.Marshal(x) }
	if a[0] == "volume" {
		switch a[1] {
		case "ls":
			if bad("volume-list") {
				return nil, errors.New("unavailable")
			}
			if f.volume {
				return []byte("existing\n"), nil
			}
			return nil, nil
		case "create":
			f.intent("target-volume-intent.private.json")
			var v pr148TargetVolume
			f.read("target-volume-intent.private.json", &v)
			f.n = &pr148Namespace{Name: v.Owner, Nonce: v.Nonce, Target: &v, ReceiptDir: f.dir, Init: &pr148TargetInit{Name: v.Owner + "-target-init", Removed: true}}
			f.volume = true
			if bad("volume-create-reply") {
				return nil, errors.New("lost")
			}
			return []byte(v.Name + "\n"), nil
		case "inspect":
			if !f.volume || bad("volume-inspect") {
				return nil, errors.New("unavailable")
			}
			v := f.n.Target
			return encode([]any{map[string]any{"Name": v.Name, "Driver": "local", "Scope": "local", "CreatedAt": v.NotBefore.Truncate(time.Second).Format(time.RFC3339), "Mountpoint": "/var/lib/docker/volumes/" + v.Name + "/_data", "Labels": v.labels(), "Options": nil}})
		case "rm":
			f.intent("target-volume-remove-intent.private.json")
			if bad("volume-rm") {
				return nil, errors.New("busy")
			}
			f.volume = false
			return []byte(a[2]), nil
		}
	}
	if a[0] == "container" && a[1] == "ls" {
		name := strings.TrimSuffix(strings.TrimPrefix(a[4], "name=^/"), "$")
		if id := f.containers[name]; id != "" {
			return []byte(id), nil
		}
		return nil, nil
	}
	switch a[0] {
	case "create":
		name := a[2]
		init := strings.HasSuffix(name, "-target-init")
		file := "namespace-create-intent.private.json"
		id := strings.Repeat("b", 64)
		if init {
			file = "target-init-intent.private.json"
			id = strings.Repeat("a", 64)
		}
		f.intent(file)
		f.containers[name] = id
		if bad("init-create-reply") && init || bad("main-create-reply") && !init {
			return nil, errors.New("lost reply")
		}
		return []byte(id), nil
	case "inspect":
		name := a[1]
		id := f.containers[name]
		if id == "" {
			return nil, errors.New("missing")
		}
		if bad("main-inspect") && name == f.n.Name {
			return nil, errors.New("unavailable")
		}
		return encode([]any{f.metadata(name, id)})
	case "start":
		id := a[len(a)-1]
		init := id == strings.Repeat("a", 64)
		if bad("init-start") && init || bad("main-start") && !init {
			return nil, errors.New("start error")
		}
		f.running[id] = !init
		return nil, nil
	case "stop":
		if bad("stop") {
			return nil, errors.New("stop lost")
		}
		f.running[a[len(a)-1]] = false
		return nil, nil
	case "rm":
		id := a[len(a)-1]
		init := id == strings.Repeat("a", 64)
		if bad("init-rm") && init || bad("main-rm") && !init {
			return nil, errors.New("rm lost")
		}
		for k, v := range f.containers {
			if v == id {
				delete(f.containers, k)
			}
		}
		return nil, nil
	}
	f.t.Fatalf("unexpected Docker operation: %v", a)
	return nil, errors.New("unexpected")
}

func (f *pr148TargetDockerFixture) intent(file string) {
	f.t.Helper()
	b, e := os.ReadFile(filepath.Join(f.dir, file))
	if e != nil || !json.Valid(b) {
		f.t.Fatalf("mutation before durable intent %s: %v", file, e)
	}
}
func (f *pr148TargetDockerFixture) read(file string, out any) {
	f.t.Helper()
	b, e := os.ReadFile(filepath.Join(f.dir, file))
	if e != nil || json.Unmarshal(b, out) != nil {
		f.t.Fatal("fixture intent unavailable")
	}
}

func (f *pr148TargetDockerFixture) metadata(name, id string) map[string]any {
	n := f.n
	v := n.Target
	init := strings.HasSuffix(name, "-target-init")
	v.CreatedAt = v.NotBefore.Truncate(time.Second).Format(time.RFC3339)
	v.Mountpoint = "/var/lib/docker/volumes/" + v.Name + "/_data"
	v.Owned = true
	dest := pr148TargetPath
	memory := int64(10 << 30)
	network := "bridge"
	labels := map[string]string{"cc.todo": "2736", "cc.instance": name, "cc.nonce": v.Nonce}
	tmpfs := map[string]string{"/runner": "rw,exec,uid=1001", "/work": "rw,exec,uid=1001", "/tmp": "rw,noexec,uid=1001", "/home/runner": "rw,uid=1001", "/toolchain": "rw,exec,nosuid,size=8g,uid=1001,gid=1001"}
	decl := map[string]any{"Type": "volume", "Source": v.Name, "Target": dest, "VolumeOptions": map[string]any{"NoCopy": true, "Subpath": "target"}}
	config := map[string]any{"Image": codeQLSDK, "User": "1001:1001", "Labels": labels}
	if init {
		config["Labels"] = pr148TargetInitLabels(n.Name, n.Nonce)
		dest = "/tmp"
		memory = 128 << 20
		network = "none"
		tmpfs = nil
		labels["cc.instance"] = n.Name
		config["Cmd"] = []string{"sh", "-c", pr148TargetInitScript}
		decl = map[string]any{"Type": "volume", "Source": v.Name, "Target": dest}
	}
	h := map[string]any{"Init": true, "ReadonlyRootfs": true, "NetworkMode": network, "NanoCPUs": int64(2e9), "Memory": memory, "MemorySwap": memory, "PidsLimit": int64(512), "CapDrop": []string{"ALL"}, "SecurityOpt": []string{"no-new-privileges"}, "Tmpfs": tmpfs, "Mounts": []any{decl}}
	m := map[string]any{"Type": "volume", "Name": v.Name, "Source": v.Mountpoint, "Destination": dest, "Driver": "local", "Mode": "z", "RW": true, "Propagation": ""}
	status := "exited"
	if f.running[id] {
		status = "running"
	}
	pid := 0
	if f.running[id] {
		pid = 123
	}
	exit := 0
	if init && f.fail == "init-exit" {
		exit = 1
	}
	return map[string]any{"Id": id, "Name": "/" + name, "Config": config, "HostConfig": h, "Mounts": []any{m}, "State": map[string]any{"Running": f.running[id], "Pid": pid, "Status": status, "ExitCode": exit}}
}

func newPR148TargetFixture(t *testing.T, fail string) *pr148TargetDockerFixture {
	return &pr148TargetDockerFixture{t: t, dir: t.TempDir(), fail: fail, containers: map[string]string{}, running: map[string]bool{}}
}

func TestPR148TargetLifecycleAndFailurePropagation(t *testing.T) {
	for _, failure := range []string{"", "volume-list", "volume-create-reply", "volume-inspect", "init-create-reply", "init-start", "init-exit", "init-rm", "main-create-reply", "main-inspect", "main-start", "stop", "main-rm", "volume-rm"} {
		t.Run(failure, func(t *testing.T) {
			f := newPR148TargetFixture(t, failure)
			n, e := newPR148NamespaceWith(context.Background(), f.dir, f.call)
			createFailure := failure != "" && failure != "stop" && failure != "main-rm" && failure != "volume-rm"
			if (e != nil) != createFailure {
				t.Fatalf("create failure %q: %v", failure, e)
			}
			ce := n.disposeWith(context.Background(), f.call)
			remaining := failure == "volume-inspect" || failure == "init-rm" || failure == "main-inspect" || failure == "stop" || failure == "main-rm" || failure == "volume-rm"
			if (ce != nil) != remaining || n.resourcesGone() == remaining {
				t.Fatalf("cleanup %q: %v gone=%v", failure, ce, n.resourcesGone())
			}
			if n.resourcesGone() && (f.volume || len(f.containers) != 0) {
				t.Fatal("lock releasable with residual resources")
			}
			mutations := func() int {
				count := 0
				for _, s := range f.calls {
					if strings.HasPrefix(s, "create ") || strings.HasPrefix(s, "volume create ") || strings.HasPrefix(s, "rm ") || strings.HasPrefix(s, "volume rm ") || strings.HasPrefix(s, "stop ") {
						count++
					}
				}
				return count
			}
			before := mutations()
			_ = n.disposeWith(context.Background(), f.call)
			if mutations() != before {
				t.Fatal("repeated uncertain mutation")
			}
			for _, prefix := range []string{"volume create ", "create --name " + n.Init.Name + " ", "create --name " + n.Name + " ", "volume rm "} {
				count := 0
				for _, s := range f.calls {
					if strings.HasPrefix(s, prefix) {
						count++
					}
				}
				if count > 1 {
					t.Fatalf("replayed %s", prefix)
				}
			}
		})
	}
}

func TestPR148TargetExistingVolumeRejectedBeforeMutation(t *testing.T) {
	f := newPR148TargetFixture(t, "")
	f.volume = true
	n, e := newPR148NamespaceWith(context.Background(), f.dir, f.call)
	if e == nil || n.Target.CreateAttempted || !n.resourcesGone() || !f.volume {
		t.Fatal("existing volume accepted or adopted")
	}
	for _, s := range f.calls {
		if strings.Contains(s, "create ") || strings.Contains(s, " rm ") {
			t.Fatal("mutated existing resource")
		}
	}
}

func TestPR148TargetRealMountValidatedBeforeNormalization(t *testing.T) {
	f := newPR148TargetFixture(t, "")
	n, e := newPR148NamespaceWith(context.Background(), f.dir, f.call)
	if e != nil {
		t.Fatal(e)
	}
	for _, bad := range []string{"", "name", "source", "destination", "readonly", "driver", "propagation", "subpath", "copy", "extra", "duplicate", "host-mount", "nonce", "memory", "gpu", "owner", "malformed-config"} {
		t.Run(bad, func(t *testing.T) {
			i := f.metadata(n.Name, n.ID)
			mounts := i["Mounts"].([]any)
			m := mounts[0].(map[string]any)
			h := i["HostConfig"].(map[string]any)
			declaration := h["Mounts"].([]any)[0].(map[string]any)
			switch bad {
			case "name":
				m["Name"] = "other"
			case "source":
				m["Source"] = "/host/path"
			case "destination":
				m["Destination"] = "/work"
			case "readonly":
				m["RW"] = false
			case "driver":
				m["Driver"] = "other"
			case "propagation":
				m["Propagation"] = "shared"
			case "subpath":
				declaration["VolumeOptions"].(map[string]any)["Subpath"] = ""
			case "copy":
				declaration["VolumeOptions"].(map[string]any)["NoCopy"] = false
			case "extra":
				i["Mounts"] = append(mounts, map[string]any{"Type": "bind", "Destination": "/other"})
			case "duplicate":
				i["Mounts"] = append(mounts, m)
			case "host-mount":
				h["Mounts"] = append(h["Mounts"].([]any), map[string]any{"Type": "bind", "Source": "/host", "Target": "/other"})
			case "nonce":
				i["Config"].(map[string]any)["Labels"].(map[string]string)["cc.nonce"] = "wrong"
			case "memory":
				h["Memory"] = int64(32 << 30)
			case "gpu":
				h["DeviceRequests"] = []any{map[string]any{"Capabilities": [][]string{{"gpu"}}}}
			case "owner":
				i["Config"].(map[string]any)["Labels"].(map[string]string)["cc.instance"] = "other"
			case "malformed-config":
				i["Config"] = nil
			}
			b, _ := json.Marshal([]any{i})
			e := pr148Isolation(b, n.Name, n.ID, n.Target)
			if (e != nil) != (bad != "") {
				t.Fatalf("mount/isolation %q: %v", bad, e)
			}
		})
	}
}

func TestPR148TargetVolumeMetadataRejectsReuseAndOptions(t *testing.T) {
	f := newPR148TargetFixture(t, "")
	n, e := newPR148NamespaceWith(context.Background(), f.dir, f.call)
	if e != nil {
		t.Fatal(e)
	}
	for _, bad := range []string{"", "old-created", "driver", "options", "labels", "mountpoint", "renamed"} {
		t.Run(bad, func(t *testing.T) {
			call := func(ctx context.Context, a ...string) ([]byte, error) {
				b, e := f.call(ctx, a...)
				var all []map[string]any
				if json.Unmarshal(b, &all) != nil {
					t.Fatal("fixture")
				}
				i := all[0]
				switch bad {
				case "old-created":
					i["CreatedAt"] = "2000-01-01T00:00:00Z"
				case "driver":
					i["Driver"] = "nfs"
				case "options":
					i["Options"] = map[string]string{"device": "/host"}
				case "labels":
					i["Labels"] = map[string]string{"cc.todo": "other"}
				case "mountpoint":
					i["Mountpoint"] = "/host"
				case "renamed":
					i["Name"] = "other"
				}
				b, _ = json.Marshal(all)
				return b, e
			}
			v := *n.Target
			if (v.inspect(context.Background(), call) != nil) != (bad != "") {
				t.Fatal("volume metadata accepted/rejected incorrectly")
			}
		})
	}
}

func TestPR148TargetDoesNotAlterCargoArgumentsOrGuestTarget(t *testing.T) {
	if pr148TargetPath != "/work/civ6-banseiki/civ6-banseiki/tools/target" {
		t.Fatal("existing target changed")
	}
	if !strings.Contains(strings.Join(pr148ToolEnv(1, 1), "\n"), "CARGO_TARGET_DIR="+pr148TargetPath) {
		t.Fatal("guest target changed")
	}
	f := newPR148TargetFixture(t, "")
	n, e := newPR148NamespaceWith(context.Background(), f.dir, f.call)
	if e != nil {
		t.Fatal(e)
	}
	a := n.targetCreateArgs()
	old := pr148CreateArgs(n.Name)
	var stripped []string
	for i := 0; i < len(a); i++ {
		if a[i] == "--mount" || a[i] == "--label" && i+1 < len(a) && strings.HasPrefix(a[i+1], "cc.nonce=") {
			i++
			continue
		}
		stripped = append(stripped, a[i])
	}
	if !reflect.DeepEqual(stripped, old) {
		t.Fatal("namespace options changed beyond nonce and target mount")
	}
}
