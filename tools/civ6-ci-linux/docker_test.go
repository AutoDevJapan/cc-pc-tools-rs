package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func inspectFixture(name, id string, k Kind) map[string]any {
	init, pids := true, int64(512)
	mem := int64(profileFor(k).memGiB) << 30
	tmpfs := map[string]string{}
	var mounts []map[string]string
	for d, o := range profileFor(k).tmpfs() {
		tmpfs[d] = o
		mounts = append(mounts, map[string]string{"Type": "tmpfs", "Destination": d})
	}
	return map[string]any{
		"Id": id, "Name": "/" + name,
		"Config": map[string]any{"Image": sdkImage, "User": "1001:1001", "Labels": map[string]string{"cc.instance": name, "cc.owner": nsOwner, "cc.todo": nsTodo, "cc.job": string(k)}},
		"HostConfig": map[string]any{"Init": &init, "ReadonlyRootfs": true, "NetworkMode": "bridge", "NanoCPUs": int64(2e9), "Memory": mem, "MemorySwap": mem,
			"PidsLimit": &pids, "CapDrop": []string{"ALL"}, "SecurityOpt": []string{"no-new-privileges"}, "Tmpfs": tmpfs},
		"Mounts": mounts,
	}
}

func encodeInspect(m map[string]any) []byte { b, _ := json.Marshal([]any{m}); return b }

func TestIsolationAcceptsFixedNamespaceAndRejectsDrift(t *testing.T) {
	for _, k := range []Kind{KindRust, KindCodeQL} {
		name, id := "civ6-ci-linux-"+string(k)+"-1", rep("a", 64)
		f := inspectFixture(name, id, k)
		if e := validateIsolation(encodeInspect(f), name, id, k); e != nil {
			t.Fatalf("%s: %v", k, e)
		}
		h := f["HostConfig"].(map[string]any)
		mem := int64(profileFor(k).memGiB) << 30
		for _, tc := range []struct {
			key string
			bad any
		}{
			{"Memory", mem + (1 << 30)}, {"Memory", mem - (1 << 30)}, {"MemorySwap", mem * 2}, {"NanoCPUs", int64(4e9)}, {"Privileged", true},
			{"Binds", []string{"C:\\data:/work"}}, {"Binds", []string{"/var/run/docker.sock:/var/run/docker.sock"}}, {"CapAdd", []string{"SYS_ADMIN"}},
			{"DeviceRequests", []map[string]any{{"Capabilities": [][]string{{"gpu"}}}}}, {"Devices", []map[string]string{{"PathOnHost": "/dev/dri"}}},
			{"NetworkMode", "host"}, {"PidMode", "host"}, {"ReadonlyRootfs", false}, {"SecurityOpt", []string{"seccomp=unconfined"}},
			{"Tmpfs", map[string]string{"/work": "rw"}}, {"CapDrop", []string{"NET_RAW"}},
		} {
			old, existed := h[tc.key]
			h[tc.key] = tc.bad
			if validateIsolation(encodeInspect(f), name, id, k) == nil {
				t.Errorf("%s accepted changed %s", k, tc.key)
			}
			if existed {
				h[tc.key] = old
			} else {
				delete(h, tc.key)
			}
		}
		c := f["Config"].(map[string]any)
		for _, bad := range []func(){
			func() { c["Image"] = "alpine" }, func() { c["User"] = "0:0" },
			func() { c["Labels"].(map[string]string)["cc.owner"] = "other" },
		} {
			saved := map[string]any{"Image": c["Image"], "User": c["User"]}
			savedOwner := c["Labels"].(map[string]string)["cc.owner"]
			bad()
			if validateIsolation(encodeInspect(f), name, id, k) == nil {
				t.Errorf("%s accepted config drift", k)
			}
			c["Image"], c["User"] = saved["Image"], saved["User"]
			c["Labels"].(map[string]string)["cc.owner"] = savedOwner
		}
		f["Mounts"] = append(f["Mounts"].([]map[string]string), map[string]string{"Type": "bind", "Destination": "/host"})
		if validateIsolation(encodeInspect(f), name, id, k) == nil {
			t.Errorf("%s accepted a host mount", k)
		}
		if validateIsolation(encodeInspect(inspectFixture(name, id, k)), name, rep("b", 64), k) == nil {
			t.Errorf("%s accepted another container id", k)
		}
		if validateIsolation(encodeInspect(inspectFixture(name, id, k)), name, id, map[Kind]Kind{KindRust: KindCodeQL, KindCodeQL: KindRust}[k]) == nil {
			t.Errorf("%s accepted the other job profile", k)
		}
	}
}

func TestCreateArgsAreTheFixedSdkAndBudget(t *testing.T) {
	for k, mem := range map[Kind]string{KindRust: "10g", KindCodeQL: "8g"} {
		args := createArgs("civ6-ci-linux-x", k, rustPlan())
		joined := " " + strings.Join(args, " ") + " "
		for _, forbidden := range []string{" -v ", " --volume ", " --mount ", " --privileged ", " --gpus ", " --device ", "docker.sock", " --pid ", " --cap-add ", " --network host "} {
			if strings.Contains(joined, forbidden) {
				t.Errorf("%s: forbidden %q", k, forbidden)
			}
		}
		for _, key := range []string{"--memory", "--memory-swap"} {
			found := 0
			for i, s := range args {
				if s == key {
					found++
					if args[i+1] != mem {
						t.Errorf("%s: %s=%s", k, key, args[i+1])
					}
				}
			}
			if found != 1 {
				t.Errorf("%s: %s count %d", k, key, found)
			}
		}
		if args[len(args)-3] != sdkImage || args[len(args)-2] != "sleep" {
			t.Errorf("%s: image is not the immutable digest", k)
		}
		if tc := strings.Count(joined, " --tmpfs /toolchain:"); (k == KindRust) != (tc == 1) {
			t.Errorf("%s: toolchain tmpfs count %d", k, tc)
		}
	}
}

func TestCapacityRequiresBudgetPlusReservation(t *testing.T) {
	ok := func(k Kind, vm, host, disk uint64) bool { return checkCapacity(k, vm, host, disk) == nil }
	if !ok(KindRust, 11*gib, gib, 4*gib) || ok(KindRust, 11*gib-1, gib, 4*gib) || ok(KindRust, 11*gib, gib-1, 4*gib) || ok(KindRust, 11*gib, gib, 4*gib-1) {
		t.Fatal("Rust 10 GiB + 1 GiB reservation boundary wrong")
	}
	if !ok(KindCodeQL, 9*gib, gib, 4*gib) || ok(KindCodeQL, 9*gib-1, gib, 4*gib) {
		t.Fatal("CodeQL 8 GiB + 1 GiB reservation boundary wrong")
	}
	if m, e := parseMemAvailable([]byte("MemTotal:  16000000 kB\nMemAvailable:   12345678 kB\n")); e != nil || m != 12345678<<10 {
		t.Fatalf("%d %v", m, e)
	}
	for _, bad := range []string{"", "MemAvailable: 12 MB\n", "MemAvailable: x kB\n", "MemFree: 1 kB\n"} {
		if _, e := parseMemAvailable([]byte(bad)); e == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
}
