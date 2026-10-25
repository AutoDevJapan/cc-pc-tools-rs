//go:build !windows

package main

func omb3255AcquireRuntime() omb3255RuntimeAcquisition {
	return omb3255RuntimeAcquisition{state: "platform_unavailable"}
}
