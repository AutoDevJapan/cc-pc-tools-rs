package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"time"
)

var keyPattern = regexp.MustCompile(`^[0-9]{1,18}/[0-9]{1,4}/[0-9]{1,18}$`)

func usage() {
	fmt.Fprintln(os.Stderr, "usage: civ6-ci-queue <tick|worker|status> --config <path> [--entry <run/attempt/job>]")
	os.Exit(2)
}

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	cmd := os.Args[1]
	cfgPath, entry := "", ""
	rest := os.Args[2:]
	for len(rest) > 0 {
		if len(rest) < 2 {
			usage()
		}
		switch rest[0] {
		case "--config":
			cfgPath = rest[1]
		case "--entry":
			entry = rest[1]
		default:
			usage()
		}
		rest = rest[2:]
	}
	if !absClean(cfgPath) {
		usage()
	}
	if _, err := os.Stat(cfgPath); os.IsNotExist(err) && cmd == "tick" {
		// No deployed configuration means the route is not enabled.
		fmt.Println(`{"status":"disabled"}`)
		return
	}
	d, err := NewDeps(cfgPath)
	if err != nil {
		fmt.Println(`{"status":"error","errors":["configuration_rejected"]}`)
		os.Exit(1)
	}
	switch cmd {
	case "tick":
		secs := d.Cfg.TickSeconds
		if secs == 0 {
			secs = 12
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(secs)*time.Second)
		defer cancel()
		rep := d.Tick(ctx)
		b, _ := json.Marshal(rep)
		fmt.Println(string(b))
		if rep.Status == "error" {
			os.Exit(1)
		}
	case "worker":
		if !keyPattern.MatchString(entry) {
			usage()
		}
		if err := d.Supervise(context.Background(), entry); err != nil {
			os.Exit(1)
		}
	case "status":
		l, err := d.Store.View()
		if err != nil {
			os.Exit(1)
		}
		b, _ := json.MarshalIndent(l, "", "  ")
		fmt.Println(string(b))
	default:
		usage()
	}
}
