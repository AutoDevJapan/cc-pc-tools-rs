package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func collected3174Fixture(t *testing.T) (owned3107Access, []owned3107Proof) {
	t.Helper()
	a, _ := owned3107Fixture(t)
	a.at = time.Date(2026, 10, 20, 19, 0, 0, 0, time.UTC)
	i, e := a.identity(a.root)
	if e != nil {
		t.Fatal(e)
	}
	ft := func(n int64) syscall.Filetime {
		return syscall.Filetime{LowDateTime: uint32(n & 0xffffffff), HighDateTime: uint32(uint64(n) >> 32)}
	}
	post := map[string]any{".": map[string]any{"Volume": i.Volume, "ID": i.ID, "Attributes": i.Attributes, "Links": i.Links, "Owner": i.Owner, "SizeHigh": uint32(i.Size >> 32), "SizeLow": uint32(i.Size), "Created": ft(i.Created), "Written": ft(i.Written)}}
	var extra []owned3107Proof
	for idx, v := range []any{map[string]any{"exit": 0, "stdout": "fixed fixture collection success"}, post} {
		b, _ := json.Marshal(v)
		p := filepath.Join(filepath.Dir(a.root), "collected-"+string(rune('a'+idx))+".json")
		if e := os.WriteFile(p, b, 0600); e != nil {
			t.Fatal(e)
		}
		extra = append(extra, owned3107Proof{p, hashBytes(b), 4 << 20})
	}
	return a, extra
}

func TestCollected3174RootOnlyQualification(t *testing.T) {
	a, extra := collected3174Fixture(t)
	if !collected3174Allowed(a.root, a, extra, func() bool { return true }) {
		t.Fatal("collected root rejected")
	}
	for _, p := range []string{filepath.Dir(a.root), a.root + "-sibling", filepath.Join(a.root, "known.log"), filepath.Join(a.root, "link"), owned2736Root} {
		if collected3174Allowed(p, a, extra, func() bool { t.Fatal("out-of-scope marker read"); return true }) {
			t.Fatal("broader scope accepted")
		}
	}
}

func TestCollected3174QualificationFailsClosed(t *testing.T) {
	for _, which := range []string{"marker", "source", "official", "runtime", "owner", "proof", "deny", "expired"} {
		t.Run(which, func(t *testing.T) {
			a, extra := collected3174Fixture(t)
			marker := func() bool { return true }
			switch which {
			case "marker":
				marker = func() bool { return false }
			case "source":
				a.qualified = func() bool { return false }
			case "official":
				a.official = func() bool { return false }
			case "runtime":
				a.runtime = func(civ6Deployment) bool { return false }
			case "owner":
				a.expected.Owner = "other"
			case "proof":
				extra[0].sha = "bad"
			case "deny":
				a.protected = func(p string) bool { return same(p, a.root) }
			case "expired":
				a.now = func() time.Time { return time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC) }
			}
			if collected3174Allowed(a.root, a, extra, marker) {
				t.Fatal("unqualified collected root admitted")
			}
		})
	}
}
