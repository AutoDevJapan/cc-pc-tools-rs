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
	fmt.Fprintln(os.Stderr, "usage: civ6-ci-queue <tick|worker|status|resume-unassigned|replace-expired|migrate-unexecuted-main392> --config <path> [--entry <run/attempt/job>] [--replacement <run/attempt/job>]")
	os.Exit(2)
}

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	cmd := os.Args[1]
	cfgPath, entry, replacement := "", "", ""
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
		case "--replacement":
			replacement = rest[1]
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
	case "resume-unassigned":
		if !keyPattern.MatchString(entry) {
			usage()
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		if err := d.ResumeUnassigned(ctx, entry); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println(`{"status":"reserved","official_success":false}`)
	case "replace-expired":
		if !keyPattern.MatchString(entry) || !keyPattern.MatchString(replacement) {
			usage()
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		if err := d.ReplaceExpired(ctx, entry, replacement); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println(`{"status":"replacement_reserved","official_success":false}`)
	case "migrate-unexecuted-main392":
		if !keyPattern.MatchString(entry) || !keyPattern.MatchString(replacement) {
			usage()
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		if err := d.MigrateUnexecutedSource(ctx, entry, replacement); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println(`{"status":"source_migration_reserved","official_success":false}`)
	default:
		usage()
	}
}
