//go:build !windows

package transport

func pathsEqual(a, b string) bool { return a == b }
