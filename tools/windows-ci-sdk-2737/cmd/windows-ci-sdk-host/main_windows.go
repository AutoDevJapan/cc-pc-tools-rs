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
	if len(os.Args) == 2 && os.Args[1] == "--finalize-verified-guest" {
		worktree, err := os.Getwd()
		if err != nil {
			os.Exit(1)
		}
		evidence, err := sdkprep.FinalizeVerifiedGuestCLI(worktree)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		_ = json.NewEncoder(os.Stdout).Encode(evidence)
		return
	}
	if len(os.Args) == 2 && os.Args[1] == "--validate-prepared-iso" {
		worktree, err := os.Getwd()
		if err != nil {
			fmt.Fprintln(os.Stderr, "task worktree unavailable")
			os.Exit(1)
		}
		size, digest, err := sdkprep.ValidatePreparedEvaluationISOCLI(worktree)
		if err != nil {
			fmt.Fprintln(os.Stderr, "prepared evaluation ISO validation failed")
			os.Exit(1)
		}
		fmt.Printf("prepared_iso size=%d sha256=%s\n", size, digest)
		return
	}
	if len(os.Args) != 1 {
		fmt.Fprintln(os.Stderr, "host workflow accepts no arguments or the fixed ISO validation option")
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
