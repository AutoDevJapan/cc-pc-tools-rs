package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCapacity3628KeepsExactSoftwareTargetAndOriginalProtection(t *testing.T) {
	p := capacity3628UpdateProfile
	if p.todo != 3628 || p.domain != "capacity-runtime-update-3628" || len(p.leaves) != 1 || !same(p.leaves[0].target, capacity3085UpdateProfile.leaves[0].target) {
		t.Fatal("software scope expanded")
	}
	f := newConsoleFixture(t)
	newPath := filepath.Join(f.root, "new.exe")
	if os.WriteFile(newPath, []byte("reviewed census"), 0600) != nil {
		t.Fatal("fixture write")
	}
	p.root, p.evidence, p.evidenceSHA = f.root, f.receipt, f.leaves[0].pins[0].evidence[0].sha256
	p.leaves = []capacityUpdateLeaf{{"capacity", f.leaf, newPath, f.leaves[0].pins[0].sha256, hashBytes([]byte("reviewed census"))}}
	query := func(path string, blocked bool) object {
		return capacity3085RuntimeWith(path, p, func(string) bool { return blocked }, func(root string) bool { return same(root, f.root) })
	}
	r := query(f.leaf, false)
	if r["qualified"] != true || r["todo_id"] != int64(3628) || r["domain"] != p.domain || r["data_access_granted"] != false || r["original_registry_recovered"] != false {
		t.Fatal("wrong software purpose")
	}
	if query(f.leaf, true)["qualified"] != false {
		t.Fatal("protected target admitted")
	}
	for _, path := range []string{f.root, newPath, f.receipt, filepath.Join(f.root, "all-drive-state-v1.json"), filepath.Join(f.root, "all-drive-latest-v1.json")} {
		if query(path, false)["qualified"] != false {
			t.Fatal("software purpose became a data permit")
		}
	}
}
