package main

import (
	"autodevjapan/shared-todo-cloud/internal/transport"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	flags := flag.NewFlagSet("todo-cloud-bridge", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	config := flags.String("config", "", "owner-protected credentials file")
	bridge := flags.String("bridge", "", "absolute bridge executable path for cutover activation")
	expectedRevision := flags.Uint64("expected-cloud-revision", 1, "verified import revision; a changed cloud requires explicit reconciliation")
	if len(os.Args) < 2 {
		return fmt.Errorf("usage: todo-cloud-bridge session|sync|watch|configure|cutover --config PATH [--bridge PATH]")
	}
	if err := flags.Parse(os.Args[2:]); err != nil {
		return err
	}
	if *config == "" || flags.NArg() != 0 {
		return fmt.Errorf("todo_remote_configuration_path_required")
	}
	if os.Args[1] == "configure" {
		return transport.Provision(*config, os.Stdin)
	}
	cfg, err := transport.LoadConfig(*config)
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	switch os.Args[1] {
	case "session":
		return transport.Serve(ctx, cfg, os.Stdin, os.Stdout)
	case "sync":
		return transport.Sync(ctx, cfg)
	case "watch":
		return transport.Watch(ctx, cfg)
	case "cutover":
		result, err := transport.CutoverAtRevision(ctx, cfg, *config, *bridge, *expectedRevision)
		if encodeErr := json.NewEncoder(os.Stdout).Encode(result); encodeErr != nil {
			return fmt.Errorf("todo_remote_cutover_result_write_failed")
		}
		return err
	default:
		return fmt.Errorf("todo_remote_unknown_command")
	}
}
