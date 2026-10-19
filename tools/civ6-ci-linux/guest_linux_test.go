//go:build linux && amd64

package main

import "testing"

func TestSuperviseRejectsUnfixedArguments(t *testing.T) {
	n := rep("a", 64)
	for _, args := range [][]string{
		nil, {"1", "1", "1", n},
		{"0", "1", "1", n, guestPlanFile}, {"11", "1", "1", n, guestPlanFile}, {"1", "11", "1", n, guestPlanFile},
		{"1", "1", "9", n, guestPlanFile}, {"1", "1", "1", "short", guestPlanFile}, {"1", "1", "1", n, "/tmp/other.json"},
		{"1", "1", "1", n, guestPlanFile, "other", "true"},
	} {
		if code := supervise(args); code < 120 || code > 122 {
			t.Errorf("%v returned %d", args, code)
		}
	}
}

func TestGroupAbsentRejectsInitAndNegativeIDs(t *testing.T) {
	if groupAbsent(1) || groupAbsent(0) || groupAbsent(-5) {
		t.Fatal("PID 1 or an invalid group was reported absent")
	}
}
