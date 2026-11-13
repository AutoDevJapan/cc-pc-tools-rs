package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// runSuite compiles one test binary, lists its cases and runs both partitions.
func runSuite(ctx context.Context, cfg config, out io.Writer) error {
	logf := func(format string, args ...any) {
		fmt.Fprintf(out, "race-partition: "+format+"\n", args...)
	}
	pkg, err := singlePackage(ctx, cfg.dir)
	if err != nil {
		return err
	}
	logf("package %s in %s", pkg, cfg.dir)

	work, err := os.MkdirTemp("", "race-partition-")
	if err != nil {
		return err
	}
	defer func() {
		if err := os.RemoveAll(work); err != nil {
			logf("warning: could not remove %s: %v", work, err)
		}
	}()
	bin := filepath.Join(work, "package.test"+exeSuffix)
	compileStart := time.Now()
	if err := compileTestBinary(ctx, cfg.dir, bin, cfg.race, out); err != nil {
		return err
	}
	digest, size, err := fileDigest(bin)
	if err != nil {
		return fmt.Errorf("read the compiled test binary: %w", err)
	}
	logf("compiled one test binary (race %t) in %s: %d bytes, sha256 %s",
		cfg.race, time.Since(compileStart).Round(100*time.Millisecond), size, digest)

	listing, err := listCases(ctx, bin, cfg.dir, out)
	if err != nil {
		return err
	}
	if err := expectDigest(bin, digest, "after listing the cases"); err != nil {
		return err
	}
	listed, err := parseCensus(listing)
	if err != nil {
		return err
	}
	logf("census: %d runnable top-level cases (%d tests, %d fuzz targets, %d examples), sha256 %s; %d benchmarks are not run, as with go test without -bench",
		len(listed.runnable), listed.tests, listed.fuzzTargets, listed.examples,
		textDigest(strings.Join(listed.runnable, "\n")), listed.benchmarks)

	plan, err := planPartitions(listed.runnable)
	if err != nil {
		return err
	}
	for _, p := range plan {
		logf("partition %d/2: %d cases %s .. %s via %s (pattern %d bytes, sha256 %s)",
			p.index, len(p.cases), p.cases[0], p.cases[len(p.cases)-1], p.flagName, len(p.pattern), textDigest(p.pattern))
	}

	var started [2][]string
	var failures []string
	for i, p := range plan {
		res, err := runPartition(ctx, bin, cfg.dir, digest, p, cfg.partitionTimeout, out)
		if err != nil {
			failures = append(failures, fmt.Sprintf("partition %d/2: %v", p.index, err))
			break
		}
		started[i] = res.started
		elapsed := res.elapsed.Round(100 * time.Millisecond)
		if len(res.problems) == 0 {
			logf("partition %d/2 passed in %s: %d/%d planned cases started, binary sha256 unchanged",
				p.index, elapsed, len(res.started), len(p.cases))
			continue
		}
		problem := strings.Join(res.problems, "; ")
		logf("partition %d/2 failed in %s: %s", p.index, elapsed, problem)
		failures = append(failures, fmt.Sprintf("partition %d/2: %s", p.index, problem))
	}
	if len(failures) > 0 {
		return errors.New(strings.Join(failures, "; "))
	}
	if err := verifyCoverage(listed.runnable, started[0], started[1]); err != nil {
		return err
	}
	logf("verified %d cases from one binary (sha256 %s): the partitions' union equals the census and their intersection is empty",
		len(listed.runnable), digest)
	return nil
}

// singlePackage returns the import path of dir. The module must hold no
// other package, because the command partitions exactly one package.
func singlePackage(ctx context.Context, dir string) (string, error) {
	all, err := goOutput(ctx, dir, "list", "./...")
	if err != nil {
		return "", err
	}
	self, err := goOutput(ctx, dir, "list", ".")
	if err != nil {
		return "", err
	}
	pkgs, pkg := strings.Fields(all), strings.TrimSpace(self)
	if len(pkgs) != 1 || pkgs[0] != pkg {
		return "", fmt.Errorf("%s must hold exactly one package, found %q; run the other packages explicitly before using this command", dir, pkgs)
	}
	return pkg, nil
}

func goOutput(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("go %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}
