//go:build windows && amd64

package main

import (
	"os"

	sdkprep "github.com/autodevjapan/cc-pc-tools-rs/tools/windows-ci-sdk-2737"
)

func main() {
	if sdkprep.RunGuestService() != nil {
		os.Exit(1)
	}
}
