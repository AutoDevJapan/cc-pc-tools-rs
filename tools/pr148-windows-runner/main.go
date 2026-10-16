package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	if err := dispatch(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func dispatch(args []string) error {
	if len(args) > 0 && args[0] == "test" && os.Getenv("PR148_GATE_CHILD") == "1" {
		return gateChild(args)
	}
	if len(args) < 2 {
		return errors.New("usage: pr148-windows-runner <prepare|service|worker|execute|stop|collect|cleanup-profile|hook-start|verify-source|hook-completed|audit> <owned-root> [official-runner.zip]")
	}
	p, e := readPlan(args[1])
	if e != nil {
		return e
	}
	if args[0] == "service" || args[0] == "worker" || args[0] == "fixture-child" || args[0] == "hook-start" || args[0] == "verify-source" || args[0] == "hook-completed" {
		e = checkControl(p.Root, false)
	} else {
		if args[0] == "prepare" {
			e = checkPaths(p.Root)
		} else {
			e = checkControl(p.Root, true)
		}
	}
	if e != nil {
		if args[0] == "service" && p.Fixture != "" {
			_ = saveNew(filepath.Join(p.runtime(), "startup-path-error.json"), map[string]any{"error": e.Error()})
		}
		return e
	}
	return platform(args[0], p, args[2:])
}
