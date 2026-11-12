package main

import (
	"strings"
	"testing"
)

func TestOutputBoundDoesNotAcceptAnOversizedRecord(t *testing.T) {
	var b boundedOutput
	if n, err := b.Write([]byte("ok")); err != nil || n != 2 {
		t.Fatal(n, err)
	}
	if n, err := b.Write([]byte(strings.Repeat("x", 16<<20))); err == nil || n != 0 {
		t.Fatal("oversized record accepted")
	}
	if b.data.String() != "ok" {
		t.Fatal("rejected record changed retained bytes")
	}
}
