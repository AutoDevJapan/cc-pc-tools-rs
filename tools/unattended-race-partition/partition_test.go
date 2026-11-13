package main

import (
	"reflect"
	"regexp"
	"strings"
	"testing"
)

func TestPlanPartitionsIsExclusiveCompleteAndOrdered(t *testing.T) {
	names := []string{"TestA", "TestAB", "TestB", "TestA2", "FuzzA", "ExampleA", "ExampleA_second"}
	plan, err := planPartitions(names)
	if err != nil {
		t.Fatalf("planPartitions returned %v", err)
	}
	if want := names[:4]; !reflect.DeepEqual(plan[0].cases, want) {
		t.Fatalf("partition 1 = %q, want %q", plan[0].cases, want)
	}
	if want := names[4:]; !reflect.DeepEqual(plan[1].cases, want) {
		t.Fatalf("partition 2 = %q, want %q", plan[1].cases, want)
	}
	if plan[0].flagName != "-test.run" || plan[1].flagName != "-test.skip" || plan[0].pattern != plan[1].pattern {
		t.Fatalf("selectors = %q and %q, want -test.run and -test.skip with one pattern", plan[0].selector(), plan[1].selector())
	}
	re := regexp.MustCompile(plan[0].pattern)
	for i, name := range names {
		if got, want := re.MatchString(name), i < 4; got != want {
			t.Errorf("pattern %q matches %s = %t, want %t", plan[0].pattern, name, got, want)
		}
	}
}

func TestExactNamePatternEscapesMetacharacters(t *testing.T) {
	re := regexp.MustCompile(exactNamePattern([]string{"A.B", "C+D", "E|F"}))
	for name, want := range map[string]bool{
		"A.B": true, "AxB": false, "C+D": true, "CCD": false,
		"E|F": true, "E": false, "F": false, "A.B/sub": false, "xA.B": false,
	} {
		if got := re.MatchString(name); got != want {
			t.Errorf("match %q = %t, want %t", name, got, want)
		}
	}
}

func TestPlanPartitionsNeedsTwoCases(t *testing.T) {
	for _, names := range [][]string{nil, {"TestOnly"}} {
		if plan, err := planPartitions(names); err == nil {
			t.Fatalf("planPartitions(%q) = %+v, want an error", names, plan)
		}
	}
}

func TestCheckCommandLineFailsClosedAboveTheLimit(t *testing.T) {
	bin := `C:\work\package.test.exe`
	if err := checkCommandLine(bin, []string{"-test.run=^(?:TestA)$"}); err != nil {
		t.Fatalf("short command line rejected: %v", err)
	}
	long := "-test.run=" + strings.Repeat("A", maxCommandLine)
	if err := checkCommandLine(bin, []string{long}); err == nil {
		t.Fatal("command line above the limit was accepted")
	}
}

func TestCompareStartedReportsMissingExtraAndRepeatedCases(t *testing.T) {
	missing, extra, repeated := compareStarted(
		[]string{"TestA", "TestB", "TestC"},
		[]string{"TestA", "TestC", "TestC", "TestD"},
	)
	if !reflect.DeepEqual(missing, []string{"TestB"}) || !reflect.DeepEqual(extra, []string{"TestD"}) || !reflect.DeepEqual(repeated, []string{"TestC"}) {
		t.Fatalf("missing %q, extra %q, repeated %q", missing, extra, repeated)
	}
	missing, extra, repeated = compareStarted([]string{"TestA", "TestB"}, []string{"TestB", "TestA"})
	if missing != nil || extra != nil || repeated != nil {
		t.Fatalf("exact set reported missing %q, extra %q, repeated %q", missing, extra, repeated)
	}
}

func TestVerifyCoverageNeedsUnionAndNoIntersection(t *testing.T) {
	listed := []string{"TestA", "TestB", "TestC"}
	if err := verifyCoverage(listed, []string{"TestA", "TestB"}, []string{"TestC"}); err != nil {
		t.Fatalf("exact partitions rejected: %v", err)
	}
	if err := verifyCoverage(listed, []string{"TestA", "TestB"}, []string{"TestB", "TestC"}); err == nil {
		t.Fatal("a case in both partitions was accepted")
	}
	if err := verifyCoverage(listed, []string{"TestA"}, []string{"TestC"}); err == nil {
		t.Fatal("a listed case outside both partitions was accepted")
	}
}
