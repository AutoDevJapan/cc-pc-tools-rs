//go:build windows && amd64

package main

import (
	runner "github.com/autodevjapan/cc-pc-tools-rs/tools/windows-ci-runner-2737"
	"os"
)

func main() {
	// Host execution without the dedicated SCM service does no work. Errors are
	// intentionally silent; public protocol metadata is the reporting channel.
	var err error
	if len(os.Args) == 3 && os.Args[1] == "hook" {
		err = runner.RunHook(runner.Stage(os.Args[2]))
	} else if len(os.Args) == 1 && runner.IsLocalSystem() {
		err = runner.RunGuestAgent()
	} else {
		os.Exit(2)
	}
	if err != nil {
		os.Exit(1)
	}
}
