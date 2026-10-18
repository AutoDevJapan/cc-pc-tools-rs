//go:build windows && amd64

package windowsrunner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestDiagnosticScanBoundsEmptyDirectoriesAndDepth(t *testing.T) {
	t.Run("empty-directories", func(t *testing.T) {
		root := t.TempDir()
		for i := 0; i < 2049; i++ {
			if e := os.Mkdir(filepath.Join(root, fmt.Sprintf("empty-%04d", i)), 0700); e != nil {
				t.Fatal(e)
			}
		}
		if e := checkDiagnosticBound(context.Background(), root); e == nil {
			t.Fatal("empty directory flood escaped finite diagnostic scan")
		}
	})
	t.Run("depth", func(t *testing.T) {
		root := t.TempDir()
		dir := root
		for i := 0; i < 17; i++ {
			dir = filepath.Join(dir, "d")
			if e := os.Mkdir(dir, 0700); e != nil {
				t.Fatal(e)
			}
		}
		if e := checkDiagnosticBound(context.Background(), root); e == nil {
			t.Fatal("deep directory flood escaped finite diagnostic scan")
		}
	})
}
func TestDiagnosticScanAcceptsSmallTreeAndHonorsCancellation(t *testing.T) {
	root := t.TempDir()
	if e := os.Mkdir(filepath.Join(root, "empty"), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(root, "trace.txt"), []byte("synthetic"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := checkDiagnosticBound(context.Background(), root); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if e := checkDiagnosticBound(ctx, root); !errors.Is(e, context.Canceled) {
		t.Fatal("cancelled diagnostic scan continued", e)
	}
	if e := checkDiagnosticBound(context.Background(), filepath.Join(root, "absent")); e != nil {
		t.Fatal("absent diagnostics should be harmless", e)
	}
}
