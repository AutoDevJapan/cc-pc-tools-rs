//go:build !windows

package main

import (
	"errors"
	"os/exec"
)

// runOwned is implemented only on Windows, where the workflow runs this
// command. Elsewhere it refuses to start a command whose descendants it could
// not account for.
func runOwned(cmd *exec.Cmd) (ownedResult, error) {
	return ownedResult{}, errors.New("owned process trees are implemented only on Windows")
}
