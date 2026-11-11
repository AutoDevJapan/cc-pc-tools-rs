//go:build !windows

package main

import (
	"context"
	"io"
)

// This installed-desktop software lease is Windows specific. Other platforms
// keep the preexisting prefix path and refuse this optional native adapter.
func generatedQuestionOpenSoftware(context.Context, string) (io.Closer, error) {
	return nil, errConversationMedia
}
