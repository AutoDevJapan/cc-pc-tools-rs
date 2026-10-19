// admin_broker uses only the existing, owner-authorized CC-AdminBroker task.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const installedRoot = `D:\CC\Tools\admin-broker`
const requestLimit = 10 * time.Minute

type result struct {
	Exit           int    `json:"exit"`
	Stdout         string `json:"stdout"`
	Stderr         string `json:"stderr"`
	TimedOut       bool   `json:"timed_out,omitempty"`
	Truncated      bool   `json:"truncated,omitempty"`
	CleanupPending bool   `json:"cleanup_pending,omitempty"`
}

type requestMeta struct {
	Deadline time.Time `json:"deadline"`
}

type broker struct {
	root string
	poll time.Duration
	run  func(context.Context, string) result
}

func (b broker) path(dir, name string) string { return filepath.Join(b.root, dir, name) }

// A Windows rename can expose its new name before its delete-access handle
// closes. Retry only sharing/lock conflicts, with a fixed filesystem deadline.
func retryFile(operation func() error) error {
	deadline := time.Now().Add(2 * time.Second)
	for {
		err := operation()
		if !fileBusy(err) || !time.Now().Before(deadline) {
			return err
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func readFile(path string) (data []byte, err error) {
	err = retryFile(func() error { data, err = os.ReadFile(path); return err })
	return
}

func (b broker) directories() error {
	for _, dir := range []string{"requests", "running", "results", "done"} {
		if err := os.MkdirAll(b.path(dir, ""), 0700); err != nil {
			return err
		}
	}
	return nil
}

// Publish only complete files; the broker never consumes a submitter's staging file.
func publish(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".broker-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return retryFile(func() error { return os.Rename(f.Name(), path) })
}

func writeJSON(path string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return publish(path, data)
}

func requestName(name string) bool {
	return strings.HasSuffix(name, ".ps1") && !strings.HasPrefix(name, ".")
}

func (b broker) finish(name string, r result) error {
	id := strings.TrimSuffix(name, ".ps1")
	if err := writeJSON(b.path("results", id+".json"), r); err != nil {
		return err
	}
	return b.archive(name)
}

func (b broker) archive(name string) error {
	// Only the matching metadata of this claimed request belongs to this operation.
	err := retryFile(func() error { return os.Remove(b.path("requests", strings.TrimSuffix(name, ".ps1")+".meta.json")) })
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return retryFile(func() error { return os.Rename(b.path("running", name), b.path("done", name)) })
}

// A claimed request may already have changed the machine. Never replay it after
// a broker interruption, including an interruption before result publication.
func (b broker) recoverClaims() error {
	entries, err := os.ReadDir(b.path("running", ""))
	if err != nil {
		return err
	}
	pending := false
	for _, entry := range entries {
		if entry.IsDir() || !requestName(entry.Name()) {
			continue
		}
		path := b.path("results", strings.TrimSuffix(entry.Name(), ".ps1")+".json")
		if data, err := readFile(path); err == nil {
			var r result
			if json.Unmarshal(data, &r) != nil {
				return errors.New("claimed request result is invalid; retained for recovery")
			}
			if r.CleanupPending {
				pending = true
				continue
			}
			if err := b.archive(entry.Name()); err != nil {
				return err
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		} else {
			// A broker crash closes its owned Job, but that alone is not proof
			// that all descendants have signaled. Preserve this claim as unfinished.
			if err := writeJSON(path, result{Exit: 125, CleanupPending: true, Stderr: "broker interrupted; request was not replayed; cleanup requires verification"}); err != nil {
				return err
			}
			pending = true
		}
	}
	if pending {
		return errors.New("request cleanup is unverified; running claims retained for recovery")
	}
	return nil
}

func (b broker) process(ctx context.Context, name string) error {
	path := b.path("running", name)
	// Claim before executing, rather than leaving an executable request in the queue.
	if err := retryFile(func() error { return os.Rename(b.path("requests", name), path) }); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	id := strings.TrimSuffix(name, ".ps1")
	if _, err := os.Stat(b.path("results", id+".json")); err == nil {
		return b.archive(name)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return b.finish(name, result{Exit: 125, Stderr: "request is not a regular file"})
	}
	deadline := time.Now().Add(requestLimit)
	data, err := readFile(b.path("requests", id+".meta.json"))
	if err == nil {
		var meta requestMeta
		if json.Unmarshal(data, &meta) != nil || meta.Deadline.IsZero() {
			return b.finish(name, result{Exit: 125, Stderr: "invalid request metadata"})
		}
		if meta.Deadline.Before(deadline) {
			deadline = meta.Deadline
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if !time.Now().Before(deadline) {
		return b.finish(name, result{Exit: 124, TimedOut: true, Stderr: "request expired before execution"})
	}
	runCtx, cancel := context.WithDeadline(ctx, deadline)
	r := b.run(runCtx, path)
	cancel()
	if r.CleanupPending {
		if err := writeJSON(b.path("results", id+".json"), r); err != nil {
			return err
		}
		return errors.New("request cleanup is unverified; claim retained for recovery")
	}
	return b.finish(name, r)
}

func (b broker) serve(ctx context.Context) error {
	if err := b.recoverClaims(); err != nil {
		return err
	}
	for {
		entries, err := os.ReadDir(b.path("requests", ""))
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if !entry.IsDir() && requestName(entry.Name()) {
				if err := b.process(ctx, entry.Name()); err != nil {
					// No arbitrary request path, script, or script output goes to daemon logs.
					return errors.New("request processing failed; claimed request will not be replayed")
				}
			}
		}
		// Continue after an empty queue too: IgnoreNew task starts are not wakeups.
		timer := time.NewTimer(b.poll)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (b broker) enqueue(script string, deadline time.Time) (string, error) {
	path, err := filepath.Abs(script)
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return "", errors.New("script must be a regular file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var nonce [16]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		return "", err
	}
	id := time.Now().UTC().Format("20060102-150405") + "-" + hex.EncodeToString(nonce[:])
	meta := b.path("requests", id+".meta.json")
	if err = writeJSON(meta, requestMeta{Deadline: deadline}); err != nil {
		return "", err
	}
	if err = publish(b.path("requests", id+".ps1"), data); err != nil {
		os.Remove(meta)
		return "", err
	}
	return id, nil
}

func (b broker) await(ctx context.Context, id string, output io.Writer) error {
	timer := time.NewTicker(b.poll)
	defer timer.Stop()
	for {
		data, err := readFile(b.path("results", id+".json"))
		if err == nil {
			var r result
			if err := json.Unmarshal(data, &r); err != nil {
				return err
			}
			fmt.Fprintf(output, "request=%s\nexit=%d\n--- stdout ---\n%s\n--- stderr ---\n%s\n", id, r.Exit, r.Stdout, r.Stderr)
			if r.Exit != 0 {
				return fmt.Errorf("admin script exit %d", r.Exit)
			}
			return nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("admin result pending: %s (request deadline retained)", id)
		case <-timer.C:
		}
	}
}

func main() {
	mode := flag.Bool("broker", false, "serve the existing authorized admin queue")
	script := flag.String("script", "", "PowerShell script to submit")
	seconds := flag.Int("timeout", 600, "total request deadline in seconds (1..86400)")
	flag.Parse()
	b := broker{root: installedRoot, poll: 250 * time.Millisecond, run: runScript}
	err := b.directories()
	if err == nil {
		if *mode {
			err = runBroker(b)
		} else if *script == "" || *seconds < 1 || *seconds > 86400 {
			err = errors.New("provide --script and a timeout in 1..86400 seconds")
		} else {
			ctx, cancel := context.WithTimeout(context.Background(), time.Duration(*seconds)*time.Second)
			defer cancel()
			deadline, _ := ctx.Deadline()
			var id string
			if id, err = b.enqueue(*script, deadline); err == nil {
				if err = startTask(ctx); err == nil {
					err = b.await(ctx, id, os.Stdout)
				} else {
					err = fmt.Errorf("admin task start failed; request %s retains its deadline", id)
				}
			}
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
