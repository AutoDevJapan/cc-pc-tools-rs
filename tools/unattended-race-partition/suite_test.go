package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

// requireWindows skips tests that start processes: the command runs owned
// process trees only on Windows, where the workflow uses it.
func requireWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "windows" {
		t.Skip("the command runs owned process trees only on Windows")
	}
}

// runFixture runs the command against one fixture module in testdata.
func runFixture(t *testing.T, fixture, partitionTimeout string) (int, string) {
	t.Helper()
	requireWindows(t)
	t.Setenv("GOWORK", "off")
	var out bytes.Buffer
	code := run([]string{
		"-dir", filepath.Join("testdata", fixture),
		"-race=false",
		"-partition-timeout", partitionTimeout,
		"-suite-timeout", "5m",
	}, &out, &out)
	return code, out.String()
}

func requireOutput(t *testing.T, out string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q", want)
		}
	}
	if t.Failed() {
		t.Logf("output:\n%s", out)
	}
}

func TestRunPassesWithBothPartitionsOfOneBinary(t *testing.T) {
	code, out := runFixture(t, "basic", "2m")
	if code != exitPass {
		t.Fatalf("exit %d, want %d\n%s", code, exitPass, out)
	}
	requireOutput(t, out,
		"census: 6 runnable top-level cases (4 tests, 1 fuzz targets, 1 examples)",
		"1 benchmarks are not run",
		"partition 1/2: 3 cases TestAlpha .. TestGamma via -test.run",
		"partition 2/2: 3 cases TestDelta .. ExampleEcho via -test.skip",
		"=== RUN   TestBeta/one\n",
		"=== RUN   TestBeta/two\n",
		"=== RUN   FuzzEcho/seed#0\n",
		"partition 1/2 passed",
		"partition 2/2 passed",
		"the partitions' union equals the census and their intersection is empty",
		"race-partition: PASS",
	)
	for _, name := range []string{"TestAlpha", "TestBeta", "TestGamma", "TestDelta", "FuzzEcho", "ExampleEcho"} {
		if n := strings.Count(out, "=== RUN   "+name+"\n"); n != 1 {
			t.Errorf("%s started %d times, want once", name, n)
		}
	}
	if strings.Contains(out, "=== RUN   BenchmarkEcho") {
		t.Error("the benchmark ran without -bench")
	}
}

func TestRunRunsBothPartitionsAfterAFailure(t *testing.T) {
	t.Setenv("FIXTURE_FAIL", "TestAlpha")
	code, out := runFixture(t, "basic", "2m")
	if code != exitFail {
		t.Fatalf("exit %d, want %d\n%s", code, exitFail, out)
	}
	requireOutput(t, out,
		"--- FAIL: TestAlpha",
		"partition 1/2 failed",
		"exit status 1",
		"=== RUN   ExampleEcho\n",
		"partition 2/2 passed",
		"race-partition: FAIL: partition 1/2",
	)
}

func TestRunReportsAPartitionDeadline(t *testing.T) {
	t.Setenv("FIXTURE_HANG", "TestDelta")
	code, out := runFixture(t, "basic", "3s")
	if code != exitFail {
		t.Fatalf("exit %d, want %d\n%s", code, exitFail, out)
	}
	requireOutput(t, out,
		"partition 1/2 passed",
		"panic: test timed out after 3s",
		"partition 2/2 failed",
		"2 planned cases did not start: FuzzEcho ExampleEcho",
	)
}

func TestRunTreatsOSExitAsAFailure(t *testing.T) {
	t.Setenv("FIXTURE_EXIT", "TestGamma")
	code, out := runFixture(t, "basic", "2m")
	if code != exitFail {
		t.Fatalf("exit %d, want %d\n%s", code, exitFail, out)
	}
	requireOutput(t, out, "os.Exit(0)", "partition 1/2 failed", "partition 2/2 passed")
}

func TestRunFailsClosedWithoutRunnableCases(t *testing.T) {
	code, out := runFixture(t, "notests", "2m")
	if code != exitFail {
		t.Fatalf("exit %d, want %d\n%s", code, exitFail, out)
	}
	requireOutput(t, out, "no runnable top-level cases")
}

func TestRunFailsClosedForSeveralPackages(t *testing.T) {
	code, out := runFixture(t, "twopkgs", "2m")
	if code != exitFail {
		t.Fatalf("exit %d, want %d\n%s", code, exitFail, out)
	}
	requireOutput(t, out, "must hold exactly one package")
	if strings.Contains(out, "compiled one test binary") {
		t.Errorf("compiled a test binary although the module holds several packages:\n%s", out)
	}
}

// compileFixture compiles a fixture once and returns its directory, binary,
// binary digest and case census.
func compileFixture(t *testing.T, fixture string) (string, string, string, census) {
	t.Helper()
	requireWindows(t)
	t.Setenv("GOWORK", "off")
	ctx := context.Background()
	dir, err := filepath.Abs(filepath.Join("testdata", fixture))
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "package.test"+exeSuffix)
	if err := compileTestBinary(ctx, dir, bin, false, io.Discard); err != nil {
		t.Fatal(err)
	}
	digest, _, err := fileDigest(bin)
	if err != nil {
		t.Fatal(err)
	}
	listing, err := listCases(ctx, bin, dir, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	listed, err := parseCensus(listing)
	if err != nil {
		t.Fatal(err)
	}
	return dir, bin, digest, listed
}

func TestSecondPartitionReportsACaseMissingFromTheCensus(t *testing.T) {
	dir, bin, digest, listed := compileFixture(t, "basic")
	// Plan without the last listed case, as if -test.list had not printed it.
	plan, err := planPartitions(listed.runnable[:len(listed.runnable)-1])
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	res, err := runPartition(context.Background(), bin, dir, digest, plan[1], time.Minute, &out)
	if err != nil {
		t.Fatal(err)
	}
	if want := "1 cases outside the plan started: ExampleEcho"; !slices.Contains(res.problems, want) {
		t.Fatalf("problems = %q, want %q\n%s", res.problems, want, out.String())
	}
}

func TestPartitionRefusesABinaryReplacedAfterTheCensus(t *testing.T) {
	dir, bin, digest, listed := compileFixture(t, "basic")
	plan, err := planPartitions(listed.runnable)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(bin, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte{0}); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if _, err := runPartition(context.Background(), bin, dir, digest, plan[0], time.Minute, &out); err == nil || !strings.Contains(err.Error(), "sha256 changed") {
		t.Fatalf("runPartition with a replaced binary returned %v", err)
	}
	if strings.Contains(out.String(), "=== RUN") {
		t.Fatalf("the replaced binary ran:\n%s", out.String())
	}
}

func TestEffectiveTimeoutStaysInsideTheSuiteDeadline(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		configured, remaining time.Duration
		hasDeadline           bool
		want                  time.Duration
		wantErr               bool
	}{
		{"no suite deadline", 10 * time.Minute, 0, false, 10 * time.Minute, false},
		{"enough time", 10 * time.Minute, 30 * time.Minute, true, 10 * time.Minute, false},
		{"shortened", 10 * time.Minute, 5 * time.Minute, true, 4 * time.Minute, false},
		{"no time left", 10 * time.Minute, time.Minute, true, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := effectiveTimeout(tc.configured, tc.remaining, tc.hasDeadline)
			if (err != nil) != tc.wantErr || got != tc.want {
				t.Fatalf("effectiveTimeout = %v, %v; want %v, error %t", got, err, tc.want, tc.wantErr)
			}
		})
	}
}

func TestRunRejectsInvalidArguments(t *testing.T) {
	for name, args := range map[string][]string{
		"missing directory":                     {"-partition-timeout", "1m", "-suite-timeout", "3m"},
		"zero partition deadline":               {"-dir", ".", "-partition-timeout", "0s", "-suite-timeout", "3m"},
		"suite deadline within both partitions": {"-dir", ".", "-partition-timeout", "2m", "-suite-timeout", "4m"},
		"extra argument":                        {"-dir", ".", "-partition-timeout", "1m", "-suite-timeout", "3m", "./..."},
	} {
		t.Run(name, func(t *testing.T) {
			var out bytes.Buffer
			if code := run(args, &out, &out); code != exitUsage {
				t.Fatalf("exit %d, want %d\n%s", code, exitUsage, out.String())
			}
		})
	}
}
