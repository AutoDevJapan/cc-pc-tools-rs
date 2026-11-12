package main

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestCandidateReadsStayOnTheObservedCommit(t *testing.T) {
	const head = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const tree = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	mutable := 0
	read := func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if len(args) == 4 && args[0] == "show" && args[1] == "-s" {
			return []byte(head + "\n" + tree + "\n"), nil
		}
		if len(args) == 5 && args[0] == "ls-tree" {
			if args[4] == "HEAD" {
				mutable++
			}
			return nil, nil
		}
		if len(args) == 2 && args[0] == "show" {
			if strings.HasPrefix(args[1], "HEAD:") {
				mutable++
			}
			return []byte("fixture"), nil
		}
		return nil, errors.New("unexpected read")
	}
	if _, err := observeWith(context.Background(), "unused", read); err != nil {
		t.Fatal(err)
	}
	if mutable != 0 {
		t.Fatalf("candidate content re-resolved mutable HEAD %d times", mutable)
	}
}

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
