package main

import (
	"errors"
	"fmt"
	"os"
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
		return errors.New("usage: pr148-windows-runner <prepare|service|worker|execute|stop|collect|hook-start|verify-source|hook-completed|audit> <owned-root> [official-runner.zip]")
	}
	p, e := readPlan(args[1])
	if e != nil {
		return e
	}
	if e = checkPaths(p.Root); e != nil {
		return e
	}
	return platform(args[0], p, args[2:])
}
