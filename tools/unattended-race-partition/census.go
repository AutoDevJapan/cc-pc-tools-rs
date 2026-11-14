package main

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// census holds the top-level cases that one test binary prints for -test.list.
type census struct {
	runnable    []string // tests, fuzz targets and examples in run order
	tests       int
	fuzzTargets int
	examples    int
	benchmarks  int // listed, but go test runs them only with -bench
}

// parseCensus reads the output of -test.list and rejects anything that is not
// a well-formed, unique top-level case name.
func parseCensus(listing string) (census, error) {
	var c census
	if listing == "" {
		return c, errors.New("the test binary has no runnable top-level cases")
	}
	if !strings.HasSuffix(listing, "\n") {
		return c, errors.New("the case listing does not end with a newline")
	}
	seen := make(map[string]bool)
	for _, line := range strings.Split(strings.TrimSuffix(listing, "\n"), "\n") {
		name := strings.TrimSuffix(line, "\r")
		kind := caseKind(name)
		if kind == "" {
			return census{}, fmt.Errorf("unexpected case listing line %q", line)
		}
		if seen[name] {
			return census{}, fmt.Errorf("the case listing repeats %s", name)
		}
		seen[name] = true
		switch kind {
		case "Benchmark":
			c.benchmarks++
			continue
		case "Test":
			c.tests++
		case "Fuzz":
			c.fuzzTargets++
		case "Example":
			c.examples++
		}
		c.runnable = append(c.runnable, name)
	}
	if len(c.runnable) == 0 {
		return census{}, errors.New("the test binary has no runnable top-level cases")
	}
	return c, nil
}

// caseKind returns the prefix of a Go test, benchmark, fuzz target or example
// name, or "" for anything else. Like go test, it rejects a name such as
// Testable whose prefix is followed by a lower-case letter.
func caseKind(name string) string {
	if !isIdentifier(name) {
		return ""
	}
	for _, prefix := range []string{"Test", "Benchmark", "Fuzz", "Example"} {
		rest, ok := strings.CutPrefix(name, prefix)
		if !ok {
			continue
		}
		if r, _ := utf8.DecodeRuneInString(rest); rest == "" || !unicode.IsLower(r) {
			return prefix
		}
		return ""
	}
	return ""
}

func isIdentifier(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		if r != '_' && !unicode.IsLetter(r) && (i == 0 || !unicode.IsDigit(r)) {
			return false
		}
	}
	return true
}
