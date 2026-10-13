//go:build !windows

package main

import "os"

// Windows creation identities cannot be verified on another OS.
func nativeProbe(uint32) processObservation { return processObservation{} }
func replaceSnapshot(from, to string) error { return os.Rename(from, to) }
