//go:build !windows || !amd64

package main

import "context"

// The production Windows helper is unsupported here; never claim a tree proof.
func runOwnedExe(ctx context.Context, path string, args []string, input []byte, onStart func(ProcID)) (StepResult, error) {
	return runExe(ctx, path, args, input, onStart)
}
