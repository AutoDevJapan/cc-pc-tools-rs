package main

import (
	"context"
	"encoding/json"
	"flag"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"

	"autodevjapan/shared-todo-cloud/internal/migrate"
	"autodevjapan/shared-todo-cloud/internal/transport"
)

type result struct {
	OK         bool                   `json:"ok"`
	Operation  string                 `json:"operation"`
	Error      string                 `json:"error,omitempty"`
	HTTPStatus int                    `json:"httpStatus,omitempty"`
	Backup     *migrate.BackupSummary `json:"backup,omitempty"`
	migrate.Summary
}

func run(ctx context.Context, args []string, out io.Writer) int {
	r := result{Error: "invalid_arguments"}
	var quiet bool
	finish := func() int {
		if !quiet && json.NewEncoder(out).Encode(r) != nil {
			return 1
		}
		if !r.OK {
			return 1
		}
		return 0
	}
	if len(args) == 0 {
		return finish()
	}
	switch args[0] {
	case "export", "upload", "verify", "backup":
		r.Operation = args[0]
	default:
		return finish()
	}
	flags := flag.NewFlagSet(r.Operation, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var home, dir, cfgPath string
	var assetsOnly, local bool
	var keep int
	if r.Operation == "export" || r.Operation == "backup" {
		flags.StringVar(&home, "home", "", "")
		flags.StringVar(&dir, "out", "", "")
		if r.Operation == "backup" {
			flags.BoolVar(&quiet, "quiet", false, "return the backup result through the exit code without standard output")
			flags.IntVar(&keep, "keep", migrate.DefaultBackupKeep, "")
			flags.StringVar(&cfgPath, "config", "", "")
		}
	} else {
		flags.StringVar(&cfgPath, "config", "", "")
		flags.StringVar(&dir, "export", "", "")
		if r.Operation == "upload" {
			flags.BoolVar(&assetsOnly, "assets-only", false, "")
		}
		if r.Operation == "verify" {
			flags.BoolVar(&local, "local", false, "")
		}
	}
	if flags.Parse(args[1:]) != nil || flags.NArg() != 0 || dir == "" || (r.Operation == "export" || r.Operation == "backup") && home == "" || (r.Operation == "upload" || r.Operation == "verify" && !local) && cfgPath == "" {
		return finish()
	}
	var err error
	if r.Operation == "backup" {
		if cfgPath != "" {
			cfg, loadErr := transport.LoadConfig(cfgPath)
			if loadErr != nil {
				r.Error = "configuration_unavailable"
				return finish()
			}
			expected, pathErr := filepath.Abs(filepath.Join(home, "todo.json"))
			matches := filepath.Clean(cfg.CachePath) == expected
			if runtime.GOOS == "windows" {
				matches = strings.EqualFold(filepath.Clean(cfg.CachePath), expected)
			}
			if pathErr != nil || !matches {
				r.Error = "configuration_home_mismatch"
				return finish()
			}
			if transport.Sync(ctx, cfg) != nil {
				r.Error = "pre_backup_sync_failed"
				return finish()
			}
		}
		var backup migrate.BackupSummary
		backup, err = migrate.Backup(ctx, home, dir, keep)
		r.Backup = &backup
		r.Summary.Counts = backup.Counts
	} else if r.Operation == "verify" && local {
		r.Operation = "verify_local"
		r.Summary, err = migrate.VerifyExport(ctx, dir)
	} else if r.Operation == "export" {
		r.Summary, err = migrate.Export(ctx, home, dir)
	} else {
		cfg, loadErr := transport.LoadConfig(cfgPath)
		if loadErr != nil {
			r.Error = "configuration_unavailable"
			return finish()
		}
		switch {
		case r.Operation == "verify":
			r.Summary, err = migrate.Verify(ctx, cfg, dir)
		case assetsOnly:
			r.Operation = "upload_assets"
			r.Summary, err = migrate.UploadAssets(ctx, cfg, dir)
		default:
			r.Summary, err = migrate.Upload(ctx, cfg, dir)
		}
	}
	if err != nil {
		r.Error, r.HTTPStatus = migrate.ErrorCode(err)
	} else {
		r.OK, r.Error = true, ""
	}
	return finish()
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := run(ctx, os.Args[1:], os.Stdout)
	stop()
	os.Exit(code)
}
