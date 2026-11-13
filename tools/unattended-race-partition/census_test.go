package main

import (
	"reflect"
	"testing"
)

func TestParseCensusKeepsRunOrderAndExcludesBenchmarks(t *testing.T) {
	got, err := parseCensus("TestB\nTestA\nBenchmarkX\nFuzzZ\nExampleY\nExample_second\n")
	if err != nil {
		t.Fatalf("parseCensus returned %v", err)
	}
	want := []string{"TestB", "TestA", "FuzzZ", "ExampleY", "Example_second"}
	if !reflect.DeepEqual(got.runnable, want) {
		t.Fatalf("runnable = %q, want %q", got.runnable, want)
	}
	if got.tests != 2 || got.benchmarks != 1 || got.fuzzTargets != 1 || got.examples != 2 {
		t.Fatalf("counts = %d tests, %d benchmarks, %d fuzz targets, %d examples; want 2, 1, 1, 2",
			got.tests, got.benchmarks, got.fuzzTargets, got.examples)
	}
}

func TestParseCensusAcceptsEveryGoCaseNameForm(t *testing.T) {
	want := []string{"Test", "Test_lower", "Test2", "TestÄrger", "Fuzz", "Example", "ExampleT_M"}
	got, err := parseCensus("Test\nTest_lower\nTest2\nTestÄrger\nFuzz\nExample\nExampleT_M\n")
	if err != nil {
		t.Fatalf("parseCensus returned %v", err)
	}
	if !reflect.DeepEqual(got.runnable, want) {
		t.Fatalf("runnable = %q, want %q", got.runnable, want)
	}
}

func TestParseCensusAcceptsCRLF(t *testing.T) {
	got, err := parseCensus("TestA\r\nTestB\r\n")
	if err != nil {
		t.Fatalf("parseCensus returned %v", err)
	}
	if want := []string{"TestA", "TestB"}; !reflect.DeepEqual(got.runnable, want) {
		t.Fatalf("runnable = %q, want %q", got.runnable, want)
	}
}

func TestParseCensusFailsClosed(t *testing.T) {
	for name, listing := range map[string]string{
		"no output":            "",
		"only benchmarks":      "BenchmarkX\n",
		"repeated case":        "TestA\nTestA\n",
		"tool warning":         "TestA\ntesting: warning: no tests to run\n",
		"not a case name":      "Testable\n",
		"blank line":           "TestA\n\nTestB\n",
		"subtest path":         "TestA/sub\n",
		"space in name":        "Test A\n",
		"regexp metacharacter": "TestA.B\n",
		"truncated last line":  "TestA\nTestB",
	} {
		t.Run(name, func(t *testing.T) {
			if got, err := parseCensus(listing); err == nil {
				t.Fatalf("parseCensus(%q) = %q, want an error", listing, got.runnable)
			}
		})
	}
}
