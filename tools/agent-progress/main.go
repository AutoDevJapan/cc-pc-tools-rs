// agent-progress emits one read-only, privacy-restricted JSON observation.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const usage = `agent-progress --once [--output snapshot.json] [--task 2547] [--include-done] [--active-only]
Reads canonical todo.json and jobs/*/job.json once. JSON stdout is the default.
--todo, --jobs, --priority-file override metadata inputs; an empty priority-file disables the optional policy.
--max-rows (1000), --max-jobs (10000), --stale-after (10m), --deadline (20s) bound observation.
Exit 0: complete observation; 1: degraded/unavailable data; 2: invalid arguments/output failure.
No UI, network, process launch, source mutation, or resident watcher.
`

type taskFilter map[int64]bool

func (t taskFilter) String() string { return "" }
func (t taskFilter) Set(raw string) error {
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return fmt.Errorf("invalid task ID")
	}
	t[id] = true
	return nil
}

func run(args []string, stdout, stderr io.Writer) int {
	opts := defaults()
	flags := flag.NewFlagSet("agent-progress", flag.ContinueOnError)
	flags.SetOutput(io.Discard) // Parser diagnostics must never echo private arguments.
	once := flags.Bool("once", true, "one observation")
	output := flags.String("output", "", "atomically publish JSON instead of stdout")
	flags.StringVar(&opts.TodoFile, "todo", opts.TodoFile, "ToDo metadata")
	flags.StringVar(&opts.JobsDir, "jobs", opts.JobsDir, "job metadata directory")
	flags.StringVar(&opts.PriorityFile, "priority-file", opts.PriorityFile, "optional recorded priority policy")
	flags.BoolVar(&opts.IncludeDone, "include-done", false, "include finished ToDos")
	flags.BoolVar(&opts.ActiveOnly, "active-only", false, "only verified workers with recent processing evidence")
	flags.StringVar(&opts.ResourceQueueDir, "resource-queue", opts.ResourceQueueDir, "existing live-chain resource queue; empty disables")
	flags.Var(taskFilter(opts.TaskIDs), "task", "numeric task ID, repeatable")
	flags.IntVar(&opts.MaxRows, "max-rows", opts.MaxRows, "maximum rows per list")
	flags.IntVar(&opts.MaxJobs, "max-jobs", opts.MaxJobs, "maximum directory entries")
	flags.DurationVar(&opts.StaleAfter, "stale-after", opts.StaleAfter, "age threshold")
	flags.DurationVar(&opts.Deadline, "deadline", opts.Deadline, "finite scan deadline")
	err := flags.Parse(args)
	if err == flag.ErrHelp {
		_, _ = io.WriteString(stdout, usage)
		return 0
	}
	if err != nil || flags.NArg() != 0 || !*once || opts.MaxRows < 1 || opts.MaxRows > 10000 ||
		opts.MaxJobs < 1 || opts.MaxJobs > 20000 || opts.StaleAfter <= 0 || opts.StaleAfter > 30*24*time.Hour ||
		opts.Deadline < time.Millisecond || opts.Deadline > time.Minute || opts.TodoFile == "" || opts.JobsDir == "" {
		_, _ = io.WriteString(stderr, "invalid_arguments\n")
		return 2
	}
	if *output != "" && !safeOutput(*output, opts) {
		_, _ = io.WriteString(stderr, "output_overlaps_source_or_unresolved\n")
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), opts.Deadline)
	defer cancel()
	// Cache handles and one validated system-snapshot fallback per acquisition.
	probe := newNativeProbe(ctx)
	snapshot := collect(ctx, opts, time.Now(), probe)
	data, err := json.Marshal(snapshot)
	if err != nil {
		_, _ = io.WriteString(stderr, "snapshot_encode_failed\n")
		return 2
	}
	data = append(data, '\n')
	if *output != "" {
		err = publish(*output, data, opts)
	} else {
		var n int
		n, err = stdout.Write(data)
		if err == nil && n != len(data) {
			err = io.ErrShortWrite
		}
	}
	if err != nil {
		_, _ = io.WriteString(stderr, "snapshot_output_failed\n")
		return 2
	}
	if snapshot.Status != "ok" {
		return 1
	}
	return 0
}

func canonical(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	var suffix []string
	for {
		resolved, err := filepath.EvalSymlinks(abs)
		if err == nil {
			for i := len(suffix) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, suffix[i])
			}
			if runtime.GOOS == "windows" {
				resolved = strings.ToLower(resolved)
			}
			return filepath.Clean(resolved), nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			return "", err
		}
		suffix = append(suffix, filepath.Base(abs))
		abs = parent
	}
}

func inside(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && !filepath.IsAbs(rel)
}

func safeOutput(path string, opts Options) bool {
	out, err := canonical(path)
	if err != nil {
		return false
	}
	defaults := defaults()
	for _, input := range []string{opts.TodoFile, opts.PriorityFile, defaults.TodoFile, defaults.PriorityFile} {
		if input == "" {
			continue
		}
		resolved, err := canonical(input)
		if err != nil || out == resolved {
			return false
		}
		if original, err := os.Stat(input); err == nil {
			if target, err := os.Stat(path); err == nil && os.SameFile(original, target) {
				return false
			}
		}
	}
	for _, root := range []string{filepath.Dir(opts.TodoFile), opts.JobsDir, opts.ResourceQueueDir, defaults.ResourceQueueDir, filepath.Dir(defaults.TodoFile), filepath.Dir(defaults.JobsDir), filepath.Dir(defaults.PriorityFile)} {
		if root == "" {
			continue
		}
		resolved, err := canonical(root)
		if err != nil || inside(out, resolved) {
			return false
		}
	}
	return true
}

func publish(path string, data []byte, opts Options) error {
	if !safeOutput(path, opts) {
		return fmt.Errorf("protected output")
	}
	// Resolve the parent before staging, so an existing directory alias does not
	// cause a later rename to target a source directory through a junction.
	parent, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return err
	}
	target := filepath.Join(parent, filepath.Base(path))
	f, err := os.CreateTemp(parent, ".agent-progress-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, writeErr := f.Write(data)
	if writeErr == nil {
		writeErr = f.Sync()
	}
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	if !safeOutput(target, opts) {
		return fmt.Errorf("protected output")
	}
	return replaceSnapshot(f.Name(), target)
}

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }
