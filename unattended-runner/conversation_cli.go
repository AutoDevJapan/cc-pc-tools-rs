package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const conversationDefaultConfig = "D:/CC/Tools/discord-ask/unattended/config.json"
const conversationCLIDeadline = 60 * time.Second

var errConversationArguments = errors.New("invalid conversation arguments")
var errConversationBusy = errors.New("conversation busy")

type conversationCLIOptions struct {
	Config       string
	Command      string
	RequestID    string
	Todo         uint64
	OriginThread string
	BodyFile     string
	DecisionFile string
	ReasonFile   string
	Title        string
	Wait         bool
	Thread       string
	Notify       bool
	Attachments  []string
}

// Diagnostics from flag must never repeat an argument, body path or secret.
func conversationStringFlag(fs *flag.FlagSet, name string, value *string) {
	seen := false
	fs.Func(name, "", func(raw string) error {
		if seen || strings.TrimSpace(raw) == "" {
			return errConversationArguments
		}
		seen = true
		*value = raw
		return nil
	})
}

type conversationBoolFlag struct {
	value *bool
	seen  bool
}

func (f *conversationBoolFlag) String() string   { return "false" }
func (f *conversationBoolFlag) IsBoolFlag() bool { return true }
func (f *conversationBoolFlag) Set(raw string) error {
	value, err := strconv.ParseBool(raw)
	if f.seen || err != nil {
		return errConversationArguments
	}
	f.seen = true
	*f.value = value
	return nil
}

func parseConversationCLI(args []string) (conversationCLIOptions, error) {
	opts := conversationCLIOptions{Config: conversationDefaultConfig}
	global := flag.NewFlagSet("conversation", flag.ContinueOnError)
	global.SetOutput(io.Discard)
	conversationStringFlag(global, "config", &opts.Config)
	if global.Parse(args) != nil || global.NArg() == 0 {
		return opts, errConversationArguments
	}
	opts.Command = global.Arg(0)
	fs := flag.NewFlagSet("conversation "+opts.Command, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var todo string
	switch opts.Command {
	case "ask":
		conversationStringFlag(fs, "request-id", &opts.RequestID)
		conversationStringFlag(fs, "todo-id", &todo)
		conversationStringFlag(fs, "origin-thread", &opts.OriginThread)
		conversationStringFlag(fs, "body-file", &opts.BodyFile)
		conversationStringFlag(fs, "decision-file", &opts.DecisionFile)
		conversationStringFlag(fs, "title", &opts.Title)
		fs.Var(&conversationBoolFlag{value: &opts.Wait}, "wait", "")
		conversationAttachmentFlags(fs, &opts.Attachments)
	case "log":
		conversationStringFlag(fs, "request-id", &opts.RequestID)
		conversationStringFlag(fs, "title", &opts.Title)
		conversationStringFlag(fs, "body-file", &opts.BodyFile)
		conversationStringFlag(fs, "thread", &opts.Thread)
		fs.Var(&conversationBoolFlag{value: &opts.Notify}, "notify", "")
		conversationAttachmentFlags(fs, &opts.Attachments)
	case "replies":
		conversationStringFlag(fs, "request-id", &opts.RequestID)
	case "withdraw":
		conversationStringFlag(fs, "request-id", &opts.RequestID)
		conversationStringFlag(fs, "reason-file", &opts.ReasonFile)
	case "status":
		if global.NArg() != 1 {
			return opts, errConversationArguments
		}
	default:
		return opts, errConversationArguments
	}
	if fs.Parse(global.Args()[1:]) != nil || fs.NArg() != 0 {
		return opts, errConversationArguments
	}
	switch opts.Command {
	case "ask":
		var ok bool
		opts.Todo, ok = number(todo)
		if !ok || opts.RequestID == "" || opts.BodyFile == "" || opts.DecisionFile == "" {
			return opts, errConversationArguments
		}
		if _, ok = number(opts.OriginThread); !ok {
			return opts, errConversationArguments
		}
	case "log":
		if opts.RequestID == "" || opts.Title == "" || opts.BodyFile == "" {
			return opts, errConversationArguments
		}
		if opts.Thread != "" {
			if _, ok := number(opts.Thread); !ok {
				return opts, errConversationArguments
			}
		}
	case "replies":
		if opts.RequestID == "" {
			return opts, errConversationArguments
		}
	case "withdraw":
		if !conversationID.MatchString(opts.RequestID) || opts.ReasonFile == "" {
			return opts, errConversationArguments
		}
	}
	return opts, nil
}

func conversationAttachmentFlags(fs *flag.FlagSet, attachments *[]string) {
	seen := make(map[string]bool)
	fs.Func("attach", "", func(raw string) error {
		if strings.TrimSpace(raw) == "" || seen[raw] || len(*attachments) >= 10 {
			return errConversationArguments
		}
		seen[raw] = true
		*attachments = append(*attachments, raw)
		return nil
	})
}

type conversationCLIBackend interface {
	conversationAsk(context.Context, ConversationAskInput) (ConversationQuestion, error)
	conversationLog(context.Context, string, string, string, string, bool, []string) (map[string]any, error)
	conversationStatus() (map[string]any, error)
	conversationReplies(context.Context, string) (map[string]any, error)
}

// This loader uses the independent runner state and lock, never the old ledger.
func loadConversationCLIRunner(configPath string) (conversationCLIBackend, func(), error) {
	var cfg Config
	if readJSON(configPath, 1<<20, &cfg) != nil {
		return nil, nil, errors.New("configuration unavailable")
	}
	since, err := time.Parse(time.RFC3339Nano, cfg.Since)
	if err != nil || since.Before(time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)) || since.After(time.Now()) || !filepath.IsAbs(cfg.Home) || cfg.Project != "cc-pc-tools-rs" {
		return nil, nil, errors.New("configuration scope invalid")
	}
	if os.MkdirAll(cfg.Home, 0700) != nil {
		return nil, nil, errors.New("independent home unavailable")
	}
	unlock, err := acquire(filepath.Join(cfg.Home, "runner.lock"))
	if err != nil {
		return nil, nil, errConversationBusy
	}
	loaded := false
	defer func() {
		if !loaded {
			unlock()
		}
	}()
	var destination Destination
	if readJSON(cfg.NotifyConfig, 1<<20, &destination) != nil {
		return nil, nil, errors.New("destination unavailable")
	}
	for _, id := range []string{destination.Guild, destination.Forum, destination.Owner, destination.Bot} {
		if _, ok := number(id); !ok {
			return nil, nil, errors.New("destination invalid")
		}
	}
	state, err := loadIndependentState(cfg, destination)
	if err != nil {
		return nil, nil, err
	}
	token := discordToken()
	if token == "" {
		return nil, nil, errors.New("Discord credential unavailable")
	}
	runner := &Runner{Config: cfg, State: state, Client: &http.Client{Timeout: 6 * time.Second}, Token: token, Base: "https://discord.com/api/v10", Run: runCommand, Now: time.Now, TodoPath: "D:/CC/Tools/todo/todo.json"}
	loaded = true
	return runner, unlock, nil
}

func conversationCLI(args []string, out io.Writer) int {
	return conversationCLIWithLoader(args, out, loadConversationCLIRunner)
}

func conversationCLIWithLoader(args []string, out io.Writer, load func(string) (conversationCLIBackend, func(), error)) int {
	opts, err := parseConversationCLI(args)
	if err != nil {
		_, _ = io.WriteString(out, "invalid conversation arguments\n")
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), conversationCLIDeadline)
	defer cancel()
	runner, unlock, err := load(opts.Config)
	if errors.Is(err, errConversationBusy) {
		_, _ = io.WriteString(out, "busy\n")
		return 2
	}
	if err != nil {
		_, _ = io.WriteString(out, "conversation unavailable\n")
		return 1
	}
	defer unlock()
	var result any
	if ctx.Err() == nil {
		switch opts.Command {
		case "ask":
			var question ConversationQuestion
			question, err = runner.conversationAsk(ctx, ConversationAskInput{RequestID: opts.RequestID, Todo: opts.Todo, OriginThread: opts.OriginThread, BodyFile: opts.BodyFile, DecisionFile: opts.DecisionFile, Title: opts.Title, Wait: opts.Wait, Attachments: opts.Attachments})
			result = conversationQuestionPublic(question)
		case "log":
			result, err = runner.conversationLog(ctx, opts.RequestID, opts.Title, opts.BodyFile, opts.Thread, opts.Notify, opts.Attachments)
		case "replies":
			result, err = runner.conversationReplies(ctx, opts.RequestID)
		case "withdraw":
			backend, ok := runner.(interface {
				conversationWithdraw(context.Context, string, string) (map[string]any, error)
			})
			if !ok {
				err = errConversationArguments
			} else {
				result, err = backend.conversationWithdraw(ctx, opts.RequestID, opts.ReasonFile)
			}
		case "status":
			result, err = runner.conversationStatus()
		}
	}
	if err != nil || ctx.Err() != nil {
		_, _ = io.WriteString(out, "conversation unavailable\n")
		return 1
	}
	data, err := json.Marshal(result)
	if err != nil {
		_, _ = io.WriteString(out, "conversation unavailable\n")
		return 1
	}
	data = append(data, '\n')
	n, err := out.Write(data)
	if err != nil || n != len(data) {
		return 1
	}
	return 0
}

func conversationQuestionPublic(q ConversationQuestion) map[string]any {
	result := map[string]any{"domain": conversationDomain, "request_id": q.RequestID, "todo_id": q.Todo, "phase": q.Phase, "wait_requested": q.Wait}
	for key, value := range map[string]string{"origin_thread_id": q.OriginThread, "thread_id": q.Thread, "question_message_id": q.Message, "progress_thread_id": q.Progress, "question_fingerprint": q.Fingerprint, "question_receipt_sha256": q.ReceiptSHA, "created_at": q.Created} {
		if value != "" {
			result[key] = value
		}
	}
	return result
}
