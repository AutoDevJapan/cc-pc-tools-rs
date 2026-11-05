//go:build !windows

package transport

import (
	"os/exec"
	"testing"
)

func hideCommand(*exec.Cmd) {}

func denyFixtureDocumentRead(*testing.T, string) func() { return func() {} }
