//go:build !windows || !amd64

package main

import "errors"

func platform(string, Plan, []string) error { return errors.New("Windows amd64 required") }
func checkPaths(string) error               { return errors.New("Windows amd64 required") }
func gateChild([]string) error              { return errors.New("Windows amd64 required") }
