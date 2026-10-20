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
	if mode := os.Getenv(marker); mode != "" {
		os.Args = []string{"product-ci", mode, "synthetic-unknown-private-input"}
		if mode == "current-pr-job" {
			os.Args = append(os.Args, "synthetic-owned", "synthetic-linux", "synthetic-scanner")
		}
		main()
		return
	}
	for _, mode := range []string{"current-pr-qualify", "current-pr-inspect", "current-pr-job"} {
		t.Run(mode, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=^TestCurrentCIInputMainDispatch$")
			cmd.Env = append(os.Environ(), marker+"="+mode)
			hideCurrentTestCommand(cmd)
			output, err := cmd.CombinedOutput()
			exit, ok := err.(*exec.ExitError)
			if !ok || exit.ExitCode() != 1 || !strings.Contains(string(output), "legacy_input_route_unavailable") || strings.Contains(string(output), "synthetic-unknown-private-input") {
				t.Fatal("official CLI did not privately reject unknown current input")
			}
		})
	}
}
