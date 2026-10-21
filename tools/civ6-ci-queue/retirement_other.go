//go:build !windows

package main

func retiredIdentityMatches(string, uint32, uint64) bool { return false }
