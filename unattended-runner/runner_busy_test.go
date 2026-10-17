package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBusySteeringCLIHasNonzeroExitButDuplicateTickIsBenign(t *testing.T) {
	if mode := os.Getenv("CC_TEST_RUNNER_BUSY_MODE"); mode != "" {
		os.Args = []string{"unattended-runner", "--config", os.Getenv("CC_TEST_RUNNER_BUSY_CONFIG")}
		if mode != "tick" {
			os.Args = append(os.Args, mode, "100")
		}
		main()
		return
	}
	for _, mode := range []string{"tick", "--prepare-steering-delivery", "--verify-steering-delivery"} {
		t.Run(mode, func(t *testing.T) {
			home := t.TempDir()
			config := filepath.Join(home, "config.json")
			data, _ := json.Marshal(Config{Since: "2026-10-13T00:59:25Z", Home: home, Project: "cc-pc-tools-rs"})
			if err := os.WriteFile(config, data, 0600); err != nil {
				t.Fatal(err)
			}
			unlock, err := acquire(filepath.Join(home, "runner.lock"))
			if err != nil {
				t.Fatal(err)
			}
			defer unlock()
			cmd := exec.Command(os.Args[0], "-test.run=^TestBusySteeringCLIHasNonzeroExitButDuplicateTickIsBenign$")
			cmd.Env = append(os.Environ(), "CC_TEST_RUNNER_BUSY_MODE="+mode, "CC_TEST_RUNNER_BUSY_CONFIG="+config)
			hide(cmd)
			out, err := cmd.CombinedOutput()
			if !strings.Contains(string(out), "busy") {
				t.Fatal("child did not reach the held runner lock")
			}
			if mode == "tick" && err != nil {
				t.Fatal("duplicate scheduled tick was not benign", err)
			}
			if mode != "tick" {
				if e, ok := err.(*exec.ExitError); !ok || e.ExitCode() != 2 {
					t.Fatal("busy steering command falsely succeeded", err)
				}
			}
		})
	}
}
