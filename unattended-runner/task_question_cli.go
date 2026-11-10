package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"time"
)

// Check never registers, posts, waits or changes a task. Diagnostics are fixed
// codes and never include native records, root paths or lower-level messages.
func taskQuestionCLI(args []string, out io.Writer, r taskQuestionRegistry, register bool) int {
	f := flag.NewFlagSet("task-question-evidence", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	var path string
	conversationStringFlag(f, "evidence", &path)
	if f.Parse(args) != nil || f.NArg() != 0 || path == "" {
		_, _ = io.WriteString(out, "task question evidence invalid\n")
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	var err error
	if register {
		err = r.register(ctx, path)
	} else {
		_, _, err = r.check(ctx, path)
	}
	stage, code := "complete", "task_question_metadata_validated"
	if err != nil {
		stage, code = "task_source_validation", "task_question_metadata_unverified"
		var failure *generatedQuestionFailure
		if errors.As(err, &failure) {
			stage, code = failure.stage, failure.code
		}
	}
	if json.NewEncoder(out).Encode(map[string]any{"domain": taskQuestionDomain, "validated": err == nil, "registered": register && err == nil, "metadata_only": true, "send_allowed": false, "stage": stage, "code": code}) != nil {
		return 1
	}
	if err != nil {
		return 1
	}
	return 0
}
