//go:build !windows

package main

import "fmt"

func runnerService() error { return fmt.Errorf("Windows service mode is required") }
