//go:build windows && amd64

package windowsrunner

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"
)

// Stock diagnostic files remain private inside the clone. Bound storage by
// cancelling the owned process tree at 64MiB/1024 files, sampled every 100ms.
// The check may overshoot by writes within one interval; it is not a disk quota.
// Stdout retention has a separate hard 8MiB bound in runTree.
func withDiagnosticBound(ctx context.Context, run func(context.Context) processResult) processResult {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(context.Canceled)
	done := make(chan struct{})
	ended := make(chan struct{})
	go func() {
		defer close(ended)
		tick := time.NewTicker(100 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-tick.C:
				if e := checkDiagnosticBound(ctx, runnerRoot+`\_diag`); e != nil {
					cancel(e)
					return
				}
			}
		}
	}()
	r := run(ctx)
	close(done)
	<-ended
	if cause := context.Cause(ctx); cause != nil {
		r.err = errors.Join(r.err, cause)
	}
	return r
}
func checkDiagnosticBound(ctx context.Context, root string) error {
	var total int64
	entries, files := 0, 0
	info, e := os.Lstat(root)
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("private diagnostics root rejected")
	}
	var scan func(string, int) error
	scan = func(path string, depth int) error {
		if depth > 16 {
			return errors.New("private diagnostic depth bound")
		}
		if e := ctx.Err(); e != nil {
			return e
		}
		dir, e := os.Open(path)
		if e != nil {
			return errors.New("private diagnostics unavailable")
		}
		defer dir.Close()
		// ReadDir(n) prevents an enormous flat directory from being fully loaded
		// before the entry limit is checked. Empty directories consume budget too.
		for {
			batch, readErr := dir.ReadDir(64)
			for _, d := range batch {
				if e := ctx.Err(); e != nil {
					return e
				}
				entries++
				if entries > 2048 {
					return errors.New("private diagnostic entry bound")
				}
				if d.Type()&os.ModeSymlink != 0 {
					return errors.New("private diagnostics link rejected")
				}
				if d.IsDir() {
					if e := scan(filepath.Join(path, d.Name()), depth+1); e != nil {
						return e
					}
					continue
				}
				files++
				if files > 1024 {
					return errors.New("private diagnostic count bound")
				}
				info, e := d.Info()
				if e != nil || !info.Mode().IsRegular() {
					return errors.New("private diagnostic metadata failed")
				}
				total += info.Size()
				if total > 64<<20 {
					return errors.New("private diagnostic size bound")
				}
			}
			if readErr == io.EOF {
				return nil
			}
			if readErr != nil {
				return errors.New("private diagnostics enumeration failed")
			}
		}
	}
	return scan(root, 0)
}
