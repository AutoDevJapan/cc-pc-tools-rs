// Command unattended-race-partition runs the tests of one Go package as two
// exclusive partitions of a single compiled test binary. README.md describes
// the contract.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

const (
	exitPass  = 0
	exitFail  = 1
	exitUsage = 2
)

// exeSuffix is the file name suffix of executables on this system.
var exeSuffix = func() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}()

type config struct {
	dir              string
	race             bool
	partitionTimeout time.Duration
	suiteTimeout     time.Duration
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run executes the command and returns its exit code.
func run(args []string, stdout, stderr io.Writer) int {
	cfg, err := parseArgs(args, stderr)
	if err != nil {
		fmt.Fprintf(stderr, "race-partition: %v\n", err)
		return exitUsage
	}
	ctx, cancel := context.WithTimeout(context.Background(), cfg.suiteTimeout)
	defer cancel()
	if err := runSuite(ctx, cfg, stdout); err != nil {
		fmt.Fprintf(stdout, "race-partition: FAIL: %v\n", err)
		return exitFail
	}
	fmt.Fprintln(stdout, "race-partition: PASS")
	return exitPass
}

func parseArgs(args []string, stderr io.Writer) (config, error) {
	flags := flag.NewFlagSet("unattended-race-partition", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var cfg config
	flags.StringVar(&cfg.dir, "dir", "", "directory of the single Go package to test")
	flags.BoolVar(&cfg.race, "race", true, "compile the test binary with the race detector")
	flags.DurationVar(&cfg.partitionTimeout, "partition-timeout", 0, "-test.timeout of each partition")
	flags.DurationVar(&cfg.suiteTimeout, "suite-timeout", 0, "deadline for compiling, listing and running both partitions")
	if err := flags.Parse(args); err != nil {
		return config{}, err
	}
	switch {
	case flags.NArg() != 0:
		return config{}, fmt.Errorf("unexpected arguments %q", flags.Args())
	case cfg.dir == "":
		return config{}, errors.New("-dir is required")
	case cfg.partitionTimeout <= 0:
		return config{}, errors.New("-partition-timeout must be positive")
	case cfg.suiteTimeout <= 2*cfg.partitionTimeout:
		return config{}, fmt.Errorf("-suite-timeout %v must exceed the two partition deadlines (%v)", cfg.suiteTimeout, 2*cfg.partitionTimeout)
	}
	dir, err := filepath.Abs(cfg.dir)
	if err != nil {
		return config{}, err
	}
	cfg.dir = dir
	return cfg, nil
}
