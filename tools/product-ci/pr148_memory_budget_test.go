package main

import (
	"encoding/json"
	"testing"
)

func TestPR148MemoryBudgetRetainsKernelIsolation(t *testing.T) {
	name, id := "todo2736-memory-fixture", "fixed-id"
	init, pids := true, int64(512)
	fixture := map[string]any{
		"Id": id, "Name": "/" + name,
		"Config":     map[string]any{"Image": codeQLSDK, "User": "1001:1001", "Labels": map[string]string{"cc.todo": "2736", "cc.instance": name}},
		"HostConfig": map[string]any{"Init": &init, "ReadonlyRootfs": true, "NetworkMode": "bridge", "NanoCPUs": int64(2e9), "Memory": int64(16 << 30), "MemorySwap": int64(16 << 30), "PidsLimit": &pids, "CapDrop": []string{"ALL"}, "SecurityOpt": []string{"no-new-privileges"}, "Tmpfs": map[string]string{"/runner": "exec,uid=1001", "/work": "exec,uid=1001", "/tmp": "noexec,uid=1001", "/home/runner": "uid=1001", "/toolchain": "rw,exec,nosuid,size=8g,uid=1001,gid=1001"}},
	}
	encode := func() []byte { b, _ := json.Marshal([]any{fixture}); return b }
	if err := pr148Isolation(encode(), name, id); err != nil {
		t.Fatal(err)
	}
	h := fixture["HostConfig"].(map[string]any)
	for _, tc := range []struct {
		key string
		bad any
	}{{"Memory", int64(8 << 30)}, {"Memory", int64(32 << 30)}, {"MemorySwap", int64(32 << 30)}, {"NanoCPUs", int64(4e9)}, {"Privileged", true}, {"Binds", []string{"/private:/work"}}, {"DeviceRequests", []map[string]any{{"Capabilities": [][]string{{"gpu"}}}}}} {
		old, existed := h[tc.key]
		h[tc.key] = tc.bad
		if pr148Isolation(encode(), name, id) == nil {
			t.Fatalf("accepted changed %s", tc.key)
		}
		if existed {
			h[tc.key] = old
		} else {
			delete(h, tc.key)
		}
	}
	args := pr148CreateArgs(name)
	for _, key := range []string{"--memory", "--memory-swap"} {
		found := 0
		for i, s := range args {
			if s == key {
				found++
				if i+1 == len(args) || args[i+1] != "16g" {
					t.Fatal("wrong fixed budget")
				}
			}
		}
		if found != 1 {
			t.Fatal("budget missing or duplicate")
		}
	}
}
