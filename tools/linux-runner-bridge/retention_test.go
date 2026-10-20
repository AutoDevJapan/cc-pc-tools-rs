package main

import (
	"os"
	"path/filepath"
	"testing"
)

func diagnosticFixture(t *testing.T, root, name string, size int, complete bool) string {
	t.Helper()
	p := filepath.Join(root, name)
	if err := os.Mkdir(p, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p, "owner"), []byte(cycleOwner), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p, "Worker.log"), make([]byte, size), 0600); err != nil {
		t.Fatal(err)
	}
	if complete {
		if err := os.WriteFile(filepath.Join(p, "complete"), []byte(cycleOwner), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return p
}

func TestDiagnosticRetentionByteAndCountBoundaries(t *testing.T) {
	for _, over := range []bool{false, true} {
		t.Run(map[bool]string{false: "exact budget retained", true: "one byte over removes oldest"}[over], func(t *testing.T) {
			root := t.TempDir()
			old := diagnosticFixture(t, root, "cycle-001", 100, true)
			diagnosticFixture(t, root, "cycle-002", 100, true)
			budget := int64(200 + 4*len(cycleOwner))
			if over {
				budget--
			}
			if err := retainDiagnostics(root, budget, 8); err != nil {
				t.Fatal(err)
			}
			_, err := os.Stat(old)
			if os.IsNotExist(err) != over {
				t.Fatalf("oldest deletion=%v want=%v", err, over)
			}
		})
	}
	root := t.TempDir()
	old := diagnosticFixture(t, root, "cycle-001", 1, true)
	diagnosticFixture(t, root, "cycle-002", 1, true)
	if err := retainDiagnostics(root, diagnosticBudget, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("count limit not applied")
	}
}

func TestRetentionKeepsUnfinishedAndUnknownData(t *testing.T) {
	root := t.TempDir()
	pending := diagnosticFixture(t, root, "cycle-001", 200, false)
	finished := diagnosticFixture(t, root, "cycle-002", 200, true)
	if err := retainDiagnostics(root, 0, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(pending); err != nil {
		t.Fatal("unfinished evidence removed", err)
	}
	if _, err := os.Stat(finished); !os.IsNotExist(err) {
		t.Fatal("completed data not bounded")
	}
	if err := os.WriteFile(filepath.Join(root, "original.tar.gz"), []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := retainDiagnostics(root, 0, 0); err == nil {
		t.Fatal("unknown data accepted")
	}
	if b, err := os.ReadFile(filepath.Join(root, "original.tar.gz")); err != nil || string(b) != "original" {
		t.Fatal("original changed")
	}
}

func TestInterruptedEvidenceStopsNewAllocationAtItsBoundary(t *testing.T) {
	root := t.TempDir()
	cycle := diagnosticFixture(t, root, "cycle-001", 0, false)
	f, err := os.OpenFile(filepath.Join(cycle, "Worker.log"), os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := f.Truncate(diagnosticBudget - int64(len(cycleOwner))); err != nil {
		t.Fatal(err)
	}
	if err := retainDiagnostics(root, diagnosticBudget, 8); err != nil {
		t.Fatal("exact interrupted limit rejected", err)
	}
	if err := f.Truncate(diagnosticBudget - int64(len(cycleOwner)) + 1); err != nil {
		t.Fatal(err)
	}
	if err := retainDiagnostics(root, diagnosticBudget, 8); err == nil {
		t.Fatal("new allocations allowed beyond retained evidence limit")
	}
	if _, err := os.Stat(cycle); err != nil {
		t.Fatal("interrupted evidence removed")
	}
}
