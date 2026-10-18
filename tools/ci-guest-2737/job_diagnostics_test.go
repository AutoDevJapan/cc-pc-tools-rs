package main

import (
	"errors"
	"strings"
	"testing"
)

func TestHookDiagnosticsRejectUnknownValuesAndNeverExposeProofErrors(t *testing.T) {
	for _, d := range []*hookDiagnostic{nil, {2, "start", "context_sha"}, {1, "other", "context_sha"}, {1, "complete", "/private/SENSITIVE"}} {
		if validHookDiagnostic(d) {
			t.Fatal("unbounded diagnostic admitted")
		}
	}
	if got := sourceFailureCode(errors.New("checkout proof rejected at physical-blobs")); got != "source_physical-blobs" {
		t.Fatal(got)
	}
	if got := sourceFailureCode(errors.New("/private/SENSITIVE")); got != "source_unknown" || strings.Contains(got, "SENSITIVE") {
		t.Fatal("raw proof error escaped")
	}
}
