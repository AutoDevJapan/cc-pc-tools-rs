//go:build windows && amd64

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	sdkprep "github.com/autodevjapan/cc-pc-tools-rs/tools/windows-ci-sdk-2737"
)

func main() {
	if len(os.Args) != 1 {
		fmt.Fprintln(os.Stderr, "fixed host workflow accepts no arguments")
		os.Exit(2)
	}
	worktree, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "task worktree unavailable")
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Hour)
	defer cancel()
	evidence, err := sdkprep.RunSDKPreparationCLI(ctx, worktree)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Windows SDK guest preparation did not complete; private stage record retained")
		os.Exit(1)
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(evidence); err != nil {
		fmt.Fprintln(os.Stderr, "evidence output failed; private evidence remains in owned root")
		os.Exit(1)
	}
}
