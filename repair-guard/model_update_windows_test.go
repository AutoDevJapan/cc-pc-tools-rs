package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestModelUpdateCLIHasPurposeSpecificFailure(t *testing.T) {
	// A synthetic path: the missing update reader must not fall through to
	// general data classification or report ordinary success for an unknown leaf.
	var out, err bytes.Buffer
	code := runGuardCLI([]string{"--verify-model-runtime-update", "C:/synthetic-3177/unknown.exe"}, &out, &err, func(int) object {
		t.Fatal("update purpose reached the ToDo verifier")
		return nil
	})
	if code != 1 || out.String() != "{\"qualified\":false}\n" || err.Len() != 0 {
		t.Fatalf("missing purpose reader: exit=%d stdout=%q stderr=%q", code, out.String(), err.String())
	}
}

func TestModelUpdateHelpAddsPurposeWithoutChangingHelpContract(t *testing.T) {
	var out, err bytes.Buffer
	code := runGuardCLI([]string{"--help"}, &out, &err, func(int) object { return nil })
	if code != 0 || out.Len() != 0 || !strings.Contains(err.String(), "Usage of scrub391:") || !strings.Contains(err.String(), "-verify-model-runtime-update string") {
		t.Fatal("model update purpose is missing from existing stderr help")
	}
}
