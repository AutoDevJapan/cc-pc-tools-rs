package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	// killGrace matches go test, which stops a test binary one minute after
	// its -test.timeout.
	killGrace = time.Minute
	// ioWaitDelay matches go test, which fails a test binary whose output
	// stays open this long after it exits, for example through a leftover
	// child process.
	ioWaitDelay = 5 * time.Second
)

// compileTestBinary builds the test binary of the package in dir.
func compileTestBinary(ctx context.Context, dir, bin string, race bool, out io.Writer) error {
	args := []string{"test"}
	if race {
		args = append(args, "-race")
	}
	args = append(args, "-c", "-o", bin, ".")
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = dir
	cmd.Stdout = out
	cmd.Stderr = out
	cmd.WaitDelay = ioWaitDelay
	owned, err := runOwned(cmd)
	if err != nil {
		return fmt.Errorf("go %s: %w", strings.Join(args, " "), err)
	}
	reportLeftovers(out, "the compile step", owned.leftovers)
	if owned.runErr != nil {
		return fmt.Errorf("go %s: %w", strings.Join(args, " "), owned.runErr)
	}
	return nil
}

// ownedResult is the outcome of runOwned.
type ownedResult struct {
	runErr    error    // the result of waiting for the root process
	leftovers []string // processes that were stopped after the root ended
}

func reportLeftovers(out io.Writer, step string, leftovers []string) {
	if len(leftovers) > 0 {
		fmt.Fprintf(out, "race-partition: %s left %d processes running; stopped them: %s\n", step, len(leftovers), strings.Join(leftovers, " "))
	}
}

// fileDigest returns the SHA-256 and the size of a file.
func fileDigest(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

// expectDigest fails unless the file still has the digest recorded after compiling.
func expectDigest(path, want, when string) error {
	got, _, err := fileDigest(path)
	if err != nil {
		return fmt.Errorf("read the test binary %s: %w", when, err)
	}
	if got != want {
		return fmt.Errorf("the test binary sha256 changed %s: %s, compiled %s", when, got, want)
	}
	return nil
}

func textDigest(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// listCases returns the -test.list output of the compiled binary.
func listCases(ctx context.Context, bin, dir string, out io.Writer) (string, error) {
	cmd := exec.CommandContext(ctx, bin, "-test.list", ".*")
	cmd.Dir = dir
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = out
	cmd.WaitDelay = ioWaitDelay
	owned, err := runOwned(cmd)
	if err != nil {
		return "", fmt.Errorf("list the cases of the test binary: %w", err)
	}
	reportLeftovers(out, "the case listing", owned.leftovers)
	if owned.runErr != nil {
		return "", fmt.Errorf("list the cases of the test binary: %w", owned.runErr)
	}
	return stdout.String(), nil
}

type partitionResult struct {
	exitCode int
	elapsed  time.Duration
	started  []string // top-level cases in start order
	problems []string
}

// effectiveTimeout returns the -test.timeout of a partition: the configured
// deadline, shortened when needed so that the binary and its stop grace end
// before the suite deadline.
func effectiveTimeout(configured, remaining time.Duration, hasDeadline bool) (time.Duration, error) {
	if !hasDeadline {
		return configured, nil
	}
	available := remaining - killGrace
	if available < time.Second {
		return 0, fmt.Errorf("the suite deadline leaves %v, not enough for a partition and its %v stop grace", remaining.Round(time.Second), killGrace)
	}
	return min(configured, available), nil
}

// runPartition runs one partition of the compiled binary. An error means
// that the partition could not run as planned; failed or missing cases are
// result problems. The partition's processes, including every descendant
// that did not break away on purpose, have exited when it returns.
func runPartition(ctx context.Context, bin, dir, digest string, p partition, configured time.Duration, out io.Writer) (partitionResult, error) {
	var res partitionResult
	deadline, hasDeadline := ctx.Deadline()
	timeout, err := effectiveTimeout(configured, time.Until(deadline), hasDeadline)
	if err != nil {
		return res, err
	}
	args := []string{"-test.paniconexit0", "-test.timeout=" + timeout.String(), "-test.v=test2json", p.selector()}
	if err := checkCommandLine(bin, args); err != nil {
		return res, err
	}
	if err := expectDigest(bin, digest, "before the partition"); err != nil {
		return res, err
	}
	fmt.Fprintf(out, "race-partition: partition %d/2 runs %d planned cases with -test.timeout=%s\n", p.index, len(p.cases), timeout)

	runCtx, cancel := context.WithTimeout(ctx, timeout+killGrace)
	defer cancel()
	cmd := exec.CommandContext(runCtx, bin, args...)
	cmd.Dir = dir
	scanner := newFrameScanner(out)
	cmd.Stdout = scanner
	cmd.Stderr = scanner
	cmd.WaitDelay = ioWaitDelay
	start := time.Now()
	owned, treeErr := runOwned(cmd)
	res.elapsed = time.Since(start)
	scanner.Flush()
	res.started = scanner.started
	if treeErr != nil {
		return res, fmt.Errorf("run the test binary: %w", treeErr)
	}
	if len(owned.leftovers) > 0 {
		fmt.Fprintf(out, "race-partition: partition %d/2 stopped %d processes still running after the test binary ended: %s\n",
			p.index, len(owned.leftovers), strings.Join(owned.leftovers, " "))
	}

	runErr := owned.runErr
	var exitErr *exec.ExitError
	switch {
	case runErr == nil:
	case runCtx.Err() != nil:
		res.exitCode = -1
		res.problems = append(res.problems, fmt.Sprintf("stopped %v after -test.timeout=%s", killGrace, timeout))
	case errors.Is(runErr, exec.ErrWaitDelay):
		res.problems = append(res.problems, fmt.Sprintf("test output stayed open %v after the binary exited", ioWaitDelay))
	case errors.As(runErr, &exitErr):
		res.exitCode = exitErr.ExitCode()
		res.problems = append(res.problems, fmt.Sprintf("exit status %d", res.exitCode))
	default:
		return res, fmt.Errorf("run the test binary: %w", runErr)
	}
	if err := expectDigest(bin, digest, "after the partition"); err != nil {
		return res, err
	}
	missing, extra, repeated := compareStarted(p.cases, res.started)
	if len(missing) > 0 {
		res.problems = append(res.problems, fmt.Sprintf("%d planned cases did not start: %s", len(missing), sample(missing)))
	}
	if len(extra) > 0 {
		res.problems = append(res.problems, fmt.Sprintf("%d cases outside the plan started: %s", len(extra), sample(extra)))
	}
	if len(repeated) > 0 {
		res.problems = append(res.problems, fmt.Sprintf("%d cases started more than once: %s", len(repeated), sample(repeated)))
	}
	return res, nil
}

// sample lists up to 20 names for a log line.
func sample(names []string) string {
	const limit = 20
	if len(names) <= limit {
		return strings.Join(names, " ")
	}
	return strings.Join(names[:limit], " ") + fmt.Sprintf(" and %d more", len(names)-limit)
}
