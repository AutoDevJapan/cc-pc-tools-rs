//go:build windows && amd64

package main

import "testing"

func TestAbortCompletionHRESULTs(t *testing.T) {
	for _, tc := range []struct {
		name string
		hr   uint32
		want bool
	}{
		{"S_OK", 0, true},
		{"EVENT_S_NOSUBSCRIBERS", 0x00040202, true},
		{"EVENT_S_SOME_SUBSCRIBERS_FAILED", 0x00040200, false},
		{"S_FALSE_unlisted", 1, false},
		{"E_UNEXPECTED", 0x8000ffff, false},
		{"VSS_E_SNAPSHOT_SET_IN_PROGRESS", 0x80042316, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := abortCompleted(tc.hr); got != tc.want {
				t.Fatalf("abortCompleted(0x%08X) = %v, want %v", tc.hr, got, tc.want)
			}
		})
	}
}
