//go:build !windows

package main

import "errors"

func hostResources(string) (uint64, uint64, error) {
	return 0, 0, errors.New("the Docker Desktop host helper requires Windows")
}
