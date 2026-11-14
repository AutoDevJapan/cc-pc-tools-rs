package main

import (
	"fmt"
	"regexp"
	"strings"
)

// maxCommandLine stays below the 32767-character command line limit of
// Windows CreateProcess.
const maxCommandLine = 32000

// partition is one of the two exclusive groups of top-level cases.
type partition struct {
	index    int      // 1 or 2
	cases    []string // planned cases in run order
	flagName string   // -test.run selects the first partition, -test.skip the second
	pattern  string   // exact names of the first partition
}

func (p partition) selector() string { return p.flagName + "=" + p.pattern }

// planPartitions keeps the listed cases in run order and splits them into a
// first half selected by an exact name pattern and the complement of that
// pattern. A case that runs without being listed therefore starts in the
// second partition, where the start check reports it.
func planPartitions(runnable []string) ([2]partition, error) {
	var plan [2]partition
	if len(runnable) < 2 {
		return plan, fmt.Errorf("two non-empty partitions need at least 2 cases, found %d", len(runnable))
	}
	half := (len(runnable) + 1) / 2
	pattern := exactNamePattern(runnable[:half])
	re, err := regexp.Compile(pattern)
	if err != nil {
		return plan, fmt.Errorf("compile the partition pattern: %w", err)
	}
	for i, name := range runnable {
		if re.MatchString(name) != (i < half) {
			return plan, fmt.Errorf("the partition pattern places %s in the wrong partition", name)
		}
	}
	plan[0] = partition{index: 1, cases: runnable[:half:half], flagName: "-test.run", pattern: pattern}
	plan[1] = partition{index: 2, cases: runnable[half:], flagName: "-test.skip", pattern: pattern}
	return plan, nil
}

// exactNamePattern matches exactly the given names. The alternation stays
// inside one group, so the testing package does not split it at "|".
func exactNamePattern(names []string) string {
	quoted := make([]string, len(names))
	for i, name := range names {
		quoted[i] = regexp.QuoteMeta(name)
	}
	return "^(?:" + strings.Join(quoted, "|") + ")$"
}

// checkCommandLine rejects a command line that Windows could not start.
func checkCommandLine(bin string, args []string) error {
	n := len(bin) + 2
	for _, arg := range args {
		n += len(arg) + 3
	}
	if n > maxCommandLine {
		return fmt.Errorf("the partition command line needs %d characters, above the %d limit", n, maxCommandLine)
	}
	return nil
}

// compareStarted compares planned cases with the top-level cases that started.
func compareStarted(want, got []string) (missing, extra, repeated []string) {
	planned := make(map[string]bool, len(want))
	for _, name := range want {
		planned[name] = true
	}
	count := make(map[string]int, len(got))
	for _, name := range got {
		count[name]++
		switch {
		case count[name] == 2:
			repeated = append(repeated, name)
		case count[name] == 1 && !planned[name]:
			extra = append(extra, name)
		}
	}
	for _, name := range want {
		if count[name] == 0 {
			missing = append(missing, name)
		}
	}
	return missing, extra, repeated
}

// verifyCoverage checks that the cases started by the two partitions are
// disjoint and together equal the listed cases.
func verifyCoverage(listed, first, second []string) error {
	where := make(map[string]int, len(listed))
	for _, name := range first {
		where[name] |= 1
	}
	for _, name := range second {
		where[name] |= 2
	}
	for name, in := range where {
		if in == 3 {
			return fmt.Errorf("%s started in both partitions", name)
		}
	}
	for _, name := range listed {
		if where[name] == 0 {
			return fmt.Errorf("listed case %s started in neither partition", name)
		}
	}
	if len(where) != len(listed) {
		return fmt.Errorf("the partitions started %d distinct cases, the census lists %d", len(where), len(listed))
	}
	return nil
}
