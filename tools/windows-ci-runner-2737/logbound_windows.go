//go:build windows && amd64

package windowsrunner

import (
	"context"
	"errors"
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
				if e := checkDiagnosticBound(runnerRoot + `\_diag`); e != nil {
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
func checkDiagnosticBound(root string) error {
	var total int64
	count := 0
	return filepath.WalkDir(root, func(path string, d os.DirEntry, e error) error {
		if os.IsNotExist(e) && path == root {
			return nil
		}
		if e != nil {
			return errors.New("private diagnostics unavailable")
		}
		if d.Type()&os.ModeSymlink != 0 {
			return errors.New("private diagnostics link rejected")
		}
		if d.IsDir() {
			return nil
		}
		count++
		if count > 1024 {
			return errors.New("private diagnostic count bound")
		}
		info, e := d.Info()
		if e != nil {
			return errors.New("private diagnostic metadata failed")
		}
		total += info.Size()
		if total > 64<<20 {
			return errors.New("private diagnostic size bound")
		}
		return nil
	})
}
