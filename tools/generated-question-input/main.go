package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"time"
)

type connectInputRPC func(context.Context) (inputRPC, error)

func oneString(fs *flag.FlagSet, name string, dest *string) {
	seen := false
	fs.Func(name, "", func(value string) error {
		if seen || value == "" {
			return errors.New("invalid reference option")
		}
		seen = true
		*dest = value
		return nil
	})
}

func runInputCLI(args []string, out io.Writer, connect connectInputRPC, now func() time.Time) int {
	fs := flag.NewFlagSet("generated-question-input", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	ref := inputReference{}
	oneString(fs, "thread-id", &ref.ThreadID)
	oneString(fs, "turn-id", &ref.TurnID)
	oneString(fs, "item-id", &ref.ItemID)
	oneString(fs, "input-sha256", &ref.InputSHA256)
	oneString(fs, "image-name", &ref.ImageName)
	oneString(fs, "image-sha256", &ref.ImageSHA256)
	report := failedInput("invalid_reference")
	if fs.Parse(args) == nil && fs.NArg() == 0 && ref.valid() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		rpc, err := connect(ctx)
		if err != nil {
			report = failedInput("source_unavailable")
		} else {
			report = inspectInputReference(ctx, rpc, ref, now().Unix())
			if rpc.Close() != nil {
				report = failedInput("source_close_failed")
			}
		}
	}
	if json.NewEncoder(out).Encode(report) != nil {
		return 1
	}
	if !report.SourceMatched {
		return 1
	}
	return 0
}

func main() {
	os.Exit(runInputCLI(os.Args[1:], os.Stdout, connectInstalledAppServer, time.Now))
}
