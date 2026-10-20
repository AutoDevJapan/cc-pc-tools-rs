package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// Exercise the official consumer's main, without adding another interpretation
// of general path protection to repair-guard's shared registration point.
func TestCurrentCIInputMainDispatch(t *testing.T) {
	const marker = "CC_TEST_3089_CI_MAIN"
	if os.Getenv(marker) == "1" {
		os.Args = []string{"product-ci", "current-pr-qualify", "synthetic-unknown-private-input"}
		main()
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestCurrentCIInputMainDispatch$")
	cmd.Env = append(os.Environ(), marker+"=1")
	hideCurrentTestCommand(cmd)
	output, err := cmd.CombinedOutput()
	exit, ok := err.(*exec.ExitError)
	if !ok || exit.ExitCode() != 1 || !strings.Contains(string(output), "current CI purpose qualification rejected:") || strings.Contains(string(output), "synthetic-unknown-private-input") {
		t.Fatal("official CLI did not privately reject unknown current input")
	}
}
