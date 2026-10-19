//go:build !linux || !amd64

package main

import "errors"

func execRestricted(string, []string, []string, bool) error {
	return errors.New("Linux amd64 namespace required")
}
func supervise([]string) int        { return 120 }
func groupAbsent(int) bool          { return false }
func guestExecChild([]string) error { return errors.New("Linux amd64 namespace required") }
