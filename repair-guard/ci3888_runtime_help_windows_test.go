package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestCI3888RuntimePurposeHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runGuardCLI([]string{"--help"}, &stdout, &stderr, nil); code != 0 {
		t.Fatalf("help exit = %d", code)
	}
	if !strings.Contains(stderr.String(), "--verify-ci3888-runtime") {
		t.Fatal("normal help lacks the runtime-purpose metadata entry point")
	}
}
