package main

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestSafeDeleteSelectsItsRustProtocolConsumer(t *testing.T) {
	m := fixture(t)
	pkg := m.Packages[3]
	pkg.Name = "cleanup-8h"
	pkg.Manifest = filepath.Join(m.Root, "cleanup-8h", "Cargo.toml")
	m.Packages[3] = pkg
	for _, file := range []string{"safe-delete/retirement_windows.go", "safe-delete/go.mod", "safe-delete/safe_delete_windows_test.go"} {
		p, e := selectPlan(m, []string{file}, "")
		if e != nil || p.Mode != "affected" || !reflect.DeepEqual(p.Packages, []string{"cleanup-8h"}) {
			t.Fatalf("producer change omitted consumer: %+v %v", p, e)
		}
	}
	p, e := selectPlan(m, []string{"safe-delete/README.md"}, "")
	if e != nil || p.Mode != "none" {
		t.Fatalf("documentation selected builds: %+v %v", p, e)
	}
}
