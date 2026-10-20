package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"time"
)

func generatedQuestionScopeArgs(scope generatedQuestionScope) []string {
	return []string{"--question-todo-id", fmt.Sprint(scope.Todo), "--question-sha256", scope.Body, "--decision-sha256", scope.Decision, "--question-owner", scope.Owner, "--question-guild", scope.Guild, "--question-forum", scope.Forum}
}

func generatedQuestionScopeFlags(fs *flag.FlagSet) func() (*generatedQuestionScope, bool) {
	var s generatedQuestionScope
	var todo string
	conversationStringFlag(fs, "question-todo-id", &todo)
	conversationStringFlag(fs, "question-sha256", &s.Body)
	conversationStringFlag(fs, "decision-sha256", &s.Decision)
	conversationStringFlag(fs, "question-owner", &s.Owner)
	conversationStringFlag(fs, "question-guild", &s.Guild)
	conversationStringFlag(fs, "question-forum", &s.Forum)
	return func() (*generatedQuestionScope, bool) {
		if todo == "" && s.Body == "" && s.Decision == "" && s.Owner == "" && s.Guild == "" && s.Forum == "" {
			return nil, true
		}
		var ok bool
		s.Todo, ok = number(todo)
		if !ok || !shaPattern.MatchString(s.Body) || !shaPattern.MatchString(s.Decision) {
			return nil, false
		}
		for _, id := range []string{s.Owner, s.Guild, s.Forum} {
			if _, ok := number(id); !ok {
				return nil, false
			}
		}
		return &s, true
	}
}

func generatedQuestionCLIWithSpec(args []string, out io.Writer, spec generatedQuestionSpec) int {
	fs := flag.NewFlagSet("qualify-generated-question-media", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var path string
	conversationStringFlag(fs, "path", &path)
	getScope := generatedQuestionScopeFlags(fs)
	fail := func() int { _, _ = io.WriteString(out, "generated question image not qualified\n"); return 1 }
	if fs.Parse(args) != nil || fs.NArg() != 0 || path == "" {
		return fail()
	}
	scope, ok := getScope()
	if !ok || scope == nil {
		return fail()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	proof, err := qualifyGeneratedQuestionImage(ctx, path, *scope, spec)
	if err != nil {
		return fail()
	}
	if json.NewEncoder(out).Encode(proof) != nil {
		return 1
	}
	return 0
}
