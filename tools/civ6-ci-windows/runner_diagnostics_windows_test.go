//go:build windows && amd64

package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func init() { hideRunnerTestCommand = hidden }

func TestRunnerReceiptInheritedPipes(t *testing.T) {
	root := t.TempDir()
	c := exec.Command(os.Args[0], "-test.run=^TestGitHubReadCaptureChild$")
	hidden(c)
	c.Env = append(os.Environ(), "CC_API_CAPTURE_FIXTURE=parent")
	// The existing synthetic child prints its descendant PID. Record it through
	// a file supplied only to this test, never through runner diagnostics.
	pidFile := filepath.Join(root, "descendant.json")
	c.Env = append(c.Env, "CIW_DIAGNOSTIC_HOLDER_PID="+pidFile)
	start := time.Now()
	err := runRunnerCommand(root, "listener", c)
	b, readErr := os.ReadFile(pidFile)
	if readErr != nil {
		t.Fatal(readErr)
	}
	var child struct{ PID int }
	if json.Unmarshal(b, &child) != nil || child.PID == 0 {
		t.Fatal("holder identity missing")
	}
	p, findErr := os.FindProcess(child.PID)
	if findErr != nil {
		t.Fatal(findErr)
	}
	defer p.Release()
	if killErr := p.Kill(); killErr != nil {
		t.Fatal(killErr)
	}
	if _, waitErr := p.Wait(); waitErr != nil {
		t.Fatal(waitErr)
	}
	if err == nil || time.Since(start) >= 7*time.Second {
		t.Fatal("inherited pipe wait was not bounded")
	}
	b, readErr = os.ReadFile(filepath.Join(root, "runner-listener-result.json"))
	var result runnerCommandResult
	if readErr != nil || json.Unmarshal(b, &result) != nil || result.OK || !result.Started || result.Exit == nil || *result.Exit != 0 || result.Error != "exit_or_drain" {
		t.Fatal("pipe failure hidden by child exit zero")
	}
}
