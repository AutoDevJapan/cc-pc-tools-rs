//go:build !windows

package main

import (
	"context"
	"os"
)

// Windows creation identities cannot be verified on another OS.
func nativeProbe(uint32) processObservation       { return processObservation{} }
func newNativeProbe(context.Context) processProbe { return nativeProbe }
func transientSnapshotRead(error) bool            { return false }
func replaceSnapshot(from, to string) error       { return os.Rename(from, to) }
