//go:build !linux || !amd64

package main

import "errors"

func pr148ExecRestricted(string, []string, []string, bool) error {
	return errors.New("Linux amd64 namespace required")
}
func pr148Supervise([]string) int { return 120 }
func pr148GroupAbsent(int) bool   { return false }
