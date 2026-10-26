package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func conversationAskArgs() []string {
	return []string{"ask", "--request-id", "question-2823", "--todo-id", "2823", "--origin-thread", "500", "--body-file", "question.txt", "--decision-file", "decision.json"}
}

func conversationLogArgs() []string {
	return []string{"log", "--request-id", "progress-2823", "--title", "作業の進捗", "--body-file", "progress.txt"}
}

func TestConversationCLIParseCommandsAndOptions(t *testing.T) {
	asks := conversationAskArgs()
	logs := conversationLogArgs()
	cases := []struct {
		name string
		args []string
		want conversationCLIOptions
	}{
		{"ask", asks, conversationCLIOptions{Config: conversationDefaultConfig, Command: "ask", RequestID: "question-2823", Todo: 2823, OriginThread: "500", BodyFile: "question.txt", DecisionFile: "decision.json"}},
		{"ask_wait_title", append(append([]string{}, asks...), "--wait", "--title", "判断のお願い"), conversationCLIOptions{Config: conversationDefaultConfig, Command: "ask", RequestID: "question-2823", Todo: 2823, OriginThread: "500", BodyFile: "question.txt", DecisionFile: "decision.json", Wait: true, Title: "判断のお願い"}},
		{"ask_explicit_false", append(append([]string{}, asks...), "--wait=false"), conversationCLIOptions{Config: conversationDefaultConfig, Command: "ask", RequestID: "question-2823", Todo: 2823, OriginThread: "500", BodyFile: "question.txt", DecisionFile: "decision.json"}},
		{"log", logs, conversationCLIOptions{Config: conversationDefaultConfig, Command: "log", RequestID: "progress-2823", Title: "作業の進捗", BodyFile: "progress.txt"}},
		{"log_options", append(append([]string{}, logs...), "--thread=600", "--notify", "--attach", "first.png", "--attach=second.mp4"), conversationCLIOptions{Config: conversationDefaultConfig, Command: "log", RequestID: "progress-2823", Title: "作業の進捗", BodyFile: "progress.txt", Thread: "600", Notify: true, Attachments: []string{"first.png", "second.mp4"}}},
		{"log_explicit_false", append(append([]string{}, logs...), "--notify=false"), conversationCLIOptions{Config: conversationDefaultConfig, Command: "log", RequestID: "progress-2823", Title: "作業の進捗", BodyFile: "progress.txt"}},
		{"replies", []string{"replies", "--request-id=question-2823"}, conversationCLIOptions{Config: conversationDefaultConfig, Command: "replies", RequestID: "question-2823"}},
		{"status", []string{"status"}, conversationCLIOptions{Config: conversationDefaultConfig, Command: "status"}},
		{"wait", []string{"wait", "--request-id=question-2823"}, conversationCLIOptions{Config: conversationDefaultConfig, Command: "wait", RequestID: "question-2823"}},
		{"wait_check", []string{"wait", "--request-id=question-2823", "--check-only"}, conversationCLIOptions{Config: conversationDefaultConfig, Command: "wait", RequestID: "question-2823", CheckOnly: true}},
		{"config_prefix", []string{"--config", "custom.json", "status"}, conversationCLIOptions{Config: "custom.json", Command: "status"}},
		{"config_equals", []string{"--config=custom.json", "status"}, conversationCLIOptions{Config: "custom.json", Command: "status"}},
		{"decimal_uint64", []string{"ask", "--decision-file=decision.json", "--body-file=question.txt", "--origin-thread=500", "--todo-id=18446744073709551615", "--request-id=question-2823"}, conversationCLIOptions{Config: conversationDefaultConfig, Command: "ask", RequestID: "question-2823", Todo: ^uint64(0), OriginThread: "500", BodyFile: "question.txt", DecisionFile: "decision.json"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseConversationCLI(tc.args)
			if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Fatal("parsed conversation options differ from the command contract")
			}
		})
	}
}

func TestConversationCLIRejectsInvalidArgumentsBeforeLoading(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"empty", nil}, {"unknown_command", []string{"unknown-private-command"}},
		{"unknown_global", []string{"--private-flag", "status"}},
		{"missing_config", []string{"--config"}}, {"empty_config", []string{"--config=", "status"}},
		{"duplicate_config", []string{"--config", "one.json", "-config=two.json", "status"}},
		{"late_config", []string{"status", "--config", "private.json"}},
		{"status_extra", []string{"status", "private-argument"}}, {"status_separator", []string{"status", "--"}},
		{"status_flag", []string{"status", "--request-id=private"}},
		{"replies_missing", []string{"replies"}}, {"replies_empty", []string{"replies", "--request-id= "}},
		{"wait_missing", []string{"wait"}}, {"wait_invalid", []string{"wait", "--request-id=bad/private"}},
		{"wait_no_body", []string{"wait", "--request-id=q", "--body-file=private"}},
		{"wait_no_notify", []string{"wait", "--request-id=q", "--notify"}},
		{"wait_no_rebind", []string{"wait", "--request-id=q", "--todo-id=42"}},
		{"wait_duplicate", []string{"wait", "--request-id=q", "--check-only", "--check-only"}},
		{"replies_duplicate", []string{"replies", "--request-id=one", "-request-id=two"}},
		{"replies_extra", []string{"replies", "--request-id=one", "private-extra"}},
		{"replies_unknown", []string{"replies", "--request-id=one", "--private-flag=private"}},
		{"ask_unknown", append(conversationAskArgs(), "--private-flag=private")},
		{"ask_extra", append(conversationAskArgs(), "private-extra")},
		{"ask_wait_duplicate", append(conversationAskArgs(), "--wait", "-wait=false")},
		{"ask_wait_value", append(conversationAskArgs(), "--wait=private-value")},
		{"ask_wait_positional", append(conversationAskArgs(), "--wait", "false")},
		{"ask_title_duplicate", append(conversationAskArgs(), "--title=one", "--title=two")},
		{"ask_title_empty", append(conversationAskArgs(), "--title= ")},
		{"log_unknown", append(conversationLogArgs(), "--private-flag=private")},
		{"log_extra", append(conversationLogArgs(), "private-extra")},
		{"log_thread_invalid", append(conversationLogArgs(), "--thread=private")},
		{"log_thread_zero", append(conversationLogArgs(), "--thread=0")},
		{"log_thread_duplicate", append(conversationLogArgs(), "--thread=500", "--thread=600")},
		{"log_notify_duplicate", append(conversationLogArgs(), "--notify", "--notify=false")},
		{"log_notify_value", append(conversationLogArgs(), "--notify=private-value")},
		{"log_attachment_missing", append(conversationLogArgs(), "--attach")},
		{"log_attachment_empty", append(conversationLogArgs(), "--attach= ")},
		{"log_attachment_duplicate", append(conversationLogArgs(), "--attach=one.png", "--attach=one.png")},
	}
	// Each required flag is removed or repeated independently from a valid input.
	for _, base := range [][]string{conversationAskArgs(), conversationLogArgs()} {
		for i := 1; i < len(base); i += 2 {
			missing := append([]string{}, base[:i]...)
			missing = append(missing, base[i+2:]...)
			duplicate := append(append([]string{}, base...), base[i], base[i+1])
			empty := append([]string{}, base...)
			empty[i+1] = " "
			cases = append(cases,
				struct {
					name string
					args []string
				}{base[0] + "_missing_" + base[i], missing},
				struct {
					name string
					args []string
				}{base[0] + "_duplicate_" + base[i], duplicate},
				struct {
					name string
					args []string
				}{base[0] + "_empty_" + base[i], empty})
		}
	}
	for _, value := range []string{"0", "-1", "+1", "0x1", "1.2", " 1", "1 ", "18446744073709551616"} {
		for _, index := range []int{4, 6} {
			args := conversationAskArgs()
			args[index] = value
			cases = append(cases, struct {
				name string
				args []string
			}{"invalid_number", args})
		}
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			loaded := false
			code := conversationCLIWithLoader(tc.args, &out, func(string) (conversationCLIBackend, func(), error) {
				loaded = true
				return nil, nil, errors.New("private-loader-details")
			})
			if code != 1 || loaded || out.String() != "invalid conversation arguments\n" {
				t.Fatal("invalid arguments reached configuration loading or exposed argument text")
			}
		})
	}
}

type conversationCLIFake struct {
	command  string
	ask      ConversationAskInput
	log      conversationCLIOptions
	request  string
	ctx      context.Context
	result   map[string]any
	err      error
	question ConversationQuestion
	observe  func()
}

func (f *conversationCLIFake) conversationAsk(ctx context.Context, input ConversationAskInput) (ConversationQuestion, error) {
	f.command, f.ctx, f.ask = "ask", ctx, input
	return f.question, f.err
}
func (f *conversationCLIFake) conversationLog(ctx context.Context, requestID, title, bodyFile, thread string, notify bool, attachments []string) (map[string]any, error) {
	f.command, f.ctx = "log", ctx
	f.log = conversationCLIOptions{RequestID: requestID, Title: title, BodyFile: bodyFile, Thread: thread, Notify: notify, Attachments: attachments}
	return f.result, f.err
}
func (f *conversationCLIFake) conversationReplies(ctx context.Context, requestID string) (map[string]any, error) {
	f.command, f.ctx, f.request = "replies", ctx, requestID
	return f.result, f.err
}
func (f *conversationCLIFake) conversationWait(ctx context.Context, requestID string, check bool) (map[string]any, error) {
	f.command, f.ctx, f.request = "wait", ctx, requestID
	f.log.CheckOnly = check
	return f.result, f.err
}
func (f *conversationCLIFake) conversationStatus() (map[string]any, error) {
	if f.observe != nil {
		f.observe()
	}
	f.command = "status"
	return f.result, f.err
}

func TestConversationCLIDispatchesContractAndOnlyParentJSON(t *testing.T) {
	for _, args := range [][]string{
		append(conversationAskArgs(), "--title=判断のお願い", "--wait"),
		append(conversationAskArgs(), "--wait=false"),
		append(conversationLogArgs(), "--thread=600", "--notify", "--attach=one.png", "--attach=two.mp4"),
		conversationLogArgs(), {"replies", "--request-id=question-2823"}, {"status"},
		{"wait", "--request-id=question-2823"}, {"wait", "--request-id=question-2823", "--check-only"},
	} {
		t.Run(args[0], func(t *testing.T) {
			fake := &conversationCLIFake{result: map[string]any{"phase": "verified"}, question: ConversationQuestion{RequestID: "question-2823", Todo: 2823, Phase: "posted"}}
			var out bytes.Buffer
			loaded, unlocked := 0, 0
			start := time.Now()
			code := conversationCLIWithLoader(append([]string{"--config=custom.json"}, args...), &out, func(config string) (conversationCLIBackend, func(), error) {
				loaded++
				if config != "custom.json" {
					t.Error("configuration prefix was lost")
				}
				return fake, func() { unlocked++ }, nil
			})
			if code != 0 || loaded != 1 || unlocked != 1 || fake.command != args[0] {
				t.Fatal("command dispatch or lock lifetime differs")
			}
			var result any = fake.result
			if fake.command == "ask" {
				result = map[string]any{"domain": conversationDomain, "request_id": "question-2823", "todo_id": uint64(2823), "phase": "posted", "wait_requested": false}
				want := ConversationAskInput{RequestID: "question-2823", Todo: 2823, OriginThread: "500", BodyFile: "question.txt", DecisionFile: "decision.json"}
				if len(args) == len(conversationAskArgs())+2 {
					want.Title, want.Wait = "判断のお願い", true
				}
				if !reflect.DeepEqual(fake.ask, want) {
					t.Fatal("ask options were not forwarded exactly")
				}
			}
			if fake.command == "log" {
				want := conversationCLIOptions{RequestID: "progress-2823", Title: "作業の進捗", BodyFile: "progress.txt"}
				if len(args) > len(conversationLogArgs()) {
					want.Thread, want.Notify, want.Attachments = "600", true, []string{"one.png", "two.mp4"}
				}
				if !reflect.DeepEqual(fake.log, want) {
					t.Fatal("log options were not forwarded exactly")
				}
			}
			if fake.command == "replies" && fake.request != "question-2823" {
				t.Fatal("reply request identity was lost")
			}
			if fake.command == "wait" && (fake.request != "question-2823" || fake.log.CheckOnly != (len(args) == 3)) {
				t.Fatal("wait identity or inspection flag was lost")
			}
			want, err := json.Marshal(result)
			if err != nil || out.String() != string(want)+"\n" {
				t.Fatal("stdout contains something other than the parent result")
			}
			if fake.ctx != nil {
				deadline, ok := fake.ctx.Deadline()
				if !ok || deadline.Before(start.Add(59*time.Second)) || deadline.After(time.Now().Add(60*time.Second)) || !errors.Is(fake.ctx.Err(), context.Canceled) {
					t.Fatal("command context must have a 60-second deadline and be canceled on return")
				}
			}
		})
	}
}

func TestConversationCLIHasFixedErrorsAndReleasesLock(t *testing.T) {
	for _, args := range [][]string{conversationAskArgs(), conversationLogArgs(), {"replies", "--request-id=question-2823"}, {"status"}} {
		t.Run(args[0], func(t *testing.T) {
			fake := &conversationCLIFake{err: errors.New("private-question-body fixture-token private-path"), result: map[string]any{"body": "private-question-body"}}
			var out bytes.Buffer
			unlocked := false
			code := conversationCLIWithLoader(args, &out, func(string) (conversationCLIBackend, func(), error) {
				return fake, func() { unlocked = true }, nil
			})
			if code != 1 || !unlocked || out.String() != "conversation unavailable\n" {
				t.Fatal("operation error leaked details, emitted its result or retained the lock")
			}
		})
	}
	for _, loadErr := range []error{errors.New("private-config-path fixture-token"), errConversationBusy} {
		var out bytes.Buffer
		code := conversationCLIWithLoader([]string{"status"}, &out, func(string) (conversationCLIBackend, func(), error) { return nil, nil, loadErr })
		wantCode, wantOutput := 1, "conversation unavailable\n"
		if errors.Is(loadErr, errConversationBusy) {
			wantCode, wantOutput = 2, "busy\n"
		}
		if code != wantCode || out.String() != wantOutput {
			t.Fatal("load failure has the wrong exit code or exposed private details")
		}
	}
	var out bytes.Buffer
	fake := &conversationCLIFake{result: map[string]any{"invalid": make(chan int)}}
	unlocked := false
	if conversationCLIWithLoader([]string{"status"}, &out, func(string) (conversationCLIBackend, func(), error) { return fake, func() { unlocked = true }, nil }) != 1 || !unlocked || out.String() != "conversation unavailable\n" {
		t.Fatal("JSON failure must return a fixed error and release the lock")
	}
}

type conversationShortWriter struct{}

func (conversationShortWriter) Write(p []byte) (int, error) { return len(p) - 1, nil }

func TestConversationCLIRejectsShortOutputWrite(t *testing.T) {
	fake := &conversationCLIFake{result: map[string]any{"phase": "verified"}}
	unlocked := false
	code := conversationCLIWithLoader([]string{"status"}, conversationShortWriter{}, func(string) (conversationCLIBackend, func(), error) { return fake, func() { unlocked = true }, nil })
	if code != 1 || !unlocked {
		t.Fatal("short output write falsely succeeded or retained the lock")
	}
}

type conversationCLIConfigFixture struct {
	root, configPath, statePath, destinationPath string
	config                                       Config
	destination                                  Destination
	state                                        State
}

func conversationWriteJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil || os.WriteFile(path, data, 0600) != nil {
		t.Fatal("could not write synthetic configuration fixture")
	}
}

func conversationConfigFixture(t *testing.T) conversationCLIConfigFixture {
	t.Helper()
	t.Setenv("GODD_DISCORD_BOT_TOKEN", "fixture-token")
	f := conversationCLIConfigFixture{root: t.TempDir()}
	home := filepath.Join(f.root, "independent")
	if os.MkdirAll(home, 0700) != nil {
		t.Fatal("could not create synthetic independent home")
	}
	f.configPath = filepath.Join(f.root, "config.json")
	f.destinationPath = filepath.Join(f.root, "destination.json")
	f.statePath = filepath.Join(home, "unattended-state.json")
	f.config = Config{Since: "2026-10-13T00:59:25Z", Home: home, NotifyConfig: f.destinationPath, Project: "cc-pc-tools-rs"}
	f.destination = Destination{Guild: "100", Forum: "200", Owner: "300", Bot: "400"}
	f.state = State{Domain: "unattended-owner-v1", Since: f.config.Since, Destination: f.destination, Threads: map[string]Thread{}, Scans: map[string]*Scan{}, Requests: map[string]*Request{}}
	conversationWriteJSON(t, f.configPath, f.config)
	conversationWriteJSON(t, f.destinationPath, f.destination)
	conversationWriteJSON(t, f.statePath, f.state)
	if os.WriteFile(filepath.Join(home, "runner.lock"), nil, 0600) != nil || os.WriteFile(filepath.Join(f.root, "legacy-state.json"), []byte("legacy-sentinel"), 0600) != nil {
		t.Fatal("could not create synthetic preservation sentinels")
	}
	return f
}

func conversationFixtureSnapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	files := make(map[string]string)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			files[path] = string(data)
		}
		return nil
	})
	if err != nil {
		t.Fatal("could not snapshot synthetic fixture")
	}
	return files
}

func TestConversationCLILoadRejectsInvalidConfigAndExistingStateWithoutChanges(t *testing.T) {
	cases := []struct {
		name   string
		change func(*conversationCLIConfigFixture)
		want   string
	}{
		{"config_missing", func(f *conversationCLIConfigFixture) {
			if os.Remove(f.configPath) != nil {
				t.Fatal("fixture removal failed")
			}
		}, "configuration unavailable"},
		{"config_json", func(f *conversationCLIConfigFixture) { conversationWriteJSON(t, f.configPath, "invalid") }, "configuration unavailable"},
		{"config_null", func(f *conversationCLIConfigFixture) { conversationWriteJSON(t, f.configPath, nil) }, "configuration scope invalid"},
		{"since_invalid", func(f *conversationCLIConfigFixture) {
			f.config.Since = "invalid"
			conversationWriteJSON(t, f.configPath, f.config)
		}, "configuration scope invalid"},
		{"since_old", func(f *conversationCLIConfigFixture) {
			f.config.Since = "2026-10-12T00:00:00Z"
			conversationWriteJSON(t, f.configPath, f.config)
		}, "configuration scope invalid"},
		{"since_future", func(f *conversationCLIConfigFixture) {
			f.config.Since = time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339Nano)
			conversationWriteJSON(t, f.configPath, f.config)
		}, "configuration scope invalid"},
		{"home_relative", func(f *conversationCLIConfigFixture) {
			f.config.Home = "relative-home"
			conversationWriteJSON(t, f.configPath, f.config)
		}, "configuration scope invalid"},
		{"project_wrong", func(f *conversationCLIConfigFixture) {
			f.config.Project = "foreign-project"
			conversationWriteJSON(t, f.configPath, f.config)
		}, "configuration scope invalid"},
		{"destination_missing", func(f *conversationCLIConfigFixture) {
			if os.Remove(f.destinationPath) != nil {
				t.Fatal("fixture removal failed")
			}
		}, "destination unavailable"},
		{"destination_json", func(f *conversationCLIConfigFixture) { conversationWriteJSON(t, f.destinationPath, "invalid") }, "destination unavailable"},
		{"destination_null", func(f *conversationCLIConfigFixture) { conversationWriteJSON(t, f.destinationPath, nil) }, "destination invalid"},
		{"destination_guild", func(f *conversationCLIConfigFixture) {
			f.destination.Guild = "0"
			conversationWriteJSON(t, f.destinationPath, f.destination)
		}, "destination invalid"},
		{"destination_forum", func(f *conversationCLIConfigFixture) {
			f.destination.Forum = "invalid"
			conversationWriteJSON(t, f.destinationPath, f.destination)
		}, "destination invalid"},
		{"destination_owner", func(f *conversationCLIConfigFixture) {
			f.destination.Owner = ""
			conversationWriteJSON(t, f.destinationPath, f.destination)
		}, "destination invalid"},
		{"destination_bot", func(f *conversationCLIConfigFixture) {
			f.destination.Bot = "18446744073709551616"
			conversationWriteJSON(t, f.destinationPath, f.destination)
		}, "destination invalid"},
		{"state_json", func(f *conversationCLIConfigFixture) {
			if os.WriteFile(f.statePath, []byte("{"), 0600) != nil {
				t.Fatal("fixture write failed")
			}
		}, "independent state unavailable"},
		{"state_directory", func(f *conversationCLIConfigFixture) {
			if os.Remove(f.statePath) != nil || os.Mkdir(f.statePath, 0700) != nil {
				t.Fatal("fixture directory creation failed")
			}
		}, "independent state unavailable"},
		{"state_oversized", func(f *conversationCLIConfigFixture) {
			if os.WriteFile(f.statePath, bytes.Repeat([]byte(" "), maxState+1), 0600) != nil {
				t.Fatal("oversized fixture write failed")
			}
		}, "independent state unavailable"},
		{"state_null", func(f *conversationCLIConfigFixture) { conversationWriteJSON(t, f.statePath, nil) }, "independent state scope differs"},
		{"state_empty", func(f *conversationCLIConfigFixture) { conversationWriteJSON(t, f.statePath, map[string]any{}) }, "independent state scope differs"},
		{"state_domain", func(f *conversationCLIConfigFixture) {
			f.state.Domain = "foreign"
			conversationWriteJSON(t, f.statePath, f.state)
		}, "independent state scope differs"},
		{"state_since", func(f *conversationCLIConfigFixture) {
			f.state.Since = "2026-10-13T00:59:26Z"
			conversationWriteJSON(t, f.statePath, f.state)
		}, "independent state scope differs"},
		{"state_destination", func(f *conversationCLIConfigFixture) {
			f.state.Destination.Owner = "999"
			conversationWriteJSON(t, f.statePath, f.state)
		}, "independent state scope differs"},
		{"state_threads", func(f *conversationCLIConfigFixture) {
			f.state.Threads = nil
			conversationWriteJSON(t, f.statePath, f.state)
		}, "independent state scope differs"},
		{"state_scans", func(f *conversationCLIConfigFixture) {
			f.state.Scans = nil
			conversationWriteJSON(t, f.statePath, f.state)
		}, "independent state scope differs"},
		{"state_requests", func(f *conversationCLIConfigFixture) {
			f.state.Requests = nil
			conversationWriteJSON(t, f.statePath, f.state)
		}, "independent state scope differs"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := conversationConfigFixture(t)
			tc.change(&f)
			before := conversationFixtureSnapshot(t, f.root)
			runner, unlock, err := loadConversationCLIRunner(f.configPath)
			if unlock != nil {
				unlock()
			}
			if err == nil || err.Error() != tc.want || runner != nil {
				t.Fatal("invalid fixture was accepted or failed for another reason")
			}
			if !reflect.DeepEqual(before, conversationFixtureSnapshot(t, f.root)) {
				t.Fatal("rejected configuration or state was mutated or reinitialized")
			}
			if f.config.Home != "relative-home" {
				unlock, err = acquire(filepath.Join(f.config.Home, "runner.lock"))
				if err != nil {
					t.Fatal("failed load retained the runner lock")
				}
				unlock()
			}
		})
	}
}

func TestConversationCLILoadInitializesOnlyMissingStateInMemory(t *testing.T) {
	for _, missing := range []bool{false, true} {
		f := conversationConfigFixture(t)
		if missing && os.Remove(f.statePath) != nil {
			t.Fatal("fixture removal failed")
		}
		before := conversationFixtureSnapshot(t, f.root)
		backend, unlock, err := loadConversationCLIRunner(f.configPath)
		if err != nil {
			t.Fatal("valid synthetic configuration failed to load")
		}
		runner, ok := backend.(*Runner)
		if !ok || runner.State == nil || runner.State.Domain != "unattended-owner-v1" || runner.State.Since != f.config.Since || runner.State.Destination != f.destination || runner.State.ProgressLinks == nil || runner.Run == nil || runner.Now == nil || runner.Token != "fixture-token" || runner.Client.Timeout != 6*time.Second {
			unlock()
			t.Fatal("loaded runner lost its scope, clock, command runner or credential")
		}
		unlock()
		if !reflect.DeepEqual(before, conversationFixtureSnapshot(t, f.root)) {
			t.Fatal("loading alone persisted a new state or touched the old ledger")
		}
	}
}

func TestConversationCLINonStatusUsesExistingLockAndStatusDoesNotExposeBody(t *testing.T) {
	f := conversationConfigFixture(t)
	f.state.Requests["500"] = &Request{Text: "private-question-body fixture-token"}
	conversationWriteJSON(t, f.statePath, f.state)
	conversationWriteJSON(t, filepath.Join(f.config.Home, "conversation-questions.json"), conversationIndex{Domain: conversationDomain, Since: f.config.Since, Destination: f.destination, Questions: map[string]ConversationQuestion{}})
	before := conversationFixtureSnapshot(t, f.root)
	unlock, err := acquire(filepath.Join(f.config.Home, "runner.lock"))
	if err != nil {
		t.Fatal("could not hold synthetic runner lock")
	}
	var out bytes.Buffer
	code := conversationCLI([]string{"--config", f.configPath, "replies", "--request-id", "fixture"}, &out)
	unlock()
	if code != 2 || out.String() != "busy\n" {
		t.Fatal("CLI did not reject a concurrent tick with exit 2")
	}
	out.Reset()
	if conversationCLI([]string{"--config", f.configPath, "status"}, &out) != 0 {
		t.Fatal("status did not recover after releasing the runner lock")
	}
	var result map[string]any
	if json.Unmarshal(out.Bytes(), &result) != nil || result["domain"] != conversationDomain || result["questions"] != float64(0) || result["legacy_state"] != "unchanged" || result["original_registry_recovered"] != false {
		t.Fatal("status did not return safe parent metadata")
	}
	if bytes.Contains(out.Bytes(), []byte("fixture-token")) || bytes.Contains(out.Bytes(), []byte("private-question-body")) || bytes.Contains(out.Bytes(), []byte(f.root)) {
		t.Fatal("status exposed a credential or private fixture path")
	}
	if !reflect.DeepEqual(before, conversationFixtureSnapshot(t, f.root)) {
		t.Fatal("status persisted or modified conversation state")
	}
}

type conversationObservedWriter struct {
	bytes.Buffer
	observe func()
}

func (w *conversationObservedWriter) Write(p []byte) (int, error) {
	w.observe()
	return w.Buffer.Write(p)
}

func TestConversationCLIHoldsRunnerLockThroughOperationAndOutput(t *testing.T) {
	f := conversationConfigFixture(t)
	checks := 0
	observe := func() {
		checks++
		unlock, err := acquire(filepath.Join(f.config.Home, "runner.lock"))
		if err == nil {
			unlock()
			t.Error("shared runner lock was released before the operation or output completed")
		}
	}
	fake := &conversationCLIFake{observe: observe, result: map[string]any{"phase": "verified"}}
	out := &conversationObservedWriter{observe: observe}
	code := conversationCLIWithLoader([]string{"--config", f.configPath, "status"}, out, func(config string) (conversationCLIBackend, func(), error) {
		_, unlock, err := loadConversationCLIRunner(config)
		return fake, unlock, err
	})
	if code != 0 || checks != 2 {
		t.Fatal("operation and output did not both run under the shared runner lock")
	}
	unlock, err := acquire(filepath.Join(f.config.Home, "runner.lock"))
	if err != nil {
		t.Fatal("completed CLI retained the shared runner lock")
	}
	unlock()
}

func TestConversationCLIStatusRejectsMalformedConversationIndex(t *testing.T) {
	for _, data := range []string{"{", "null", "{}", "[]"} {
		t.Run("malformed_index", func(t *testing.T) {
			f := conversationConfigFixture(t)
			path := filepath.Join(f.config.Home, "conversation-questions.json")
			if os.WriteFile(path, []byte(data), 0600) != nil {
				t.Fatal("could not create synthetic malformed conversation index")
			}
			before := conversationFixtureSnapshot(t, f.root)
			var out bytes.Buffer
			code := conversationCLI([]string{"--config", f.configPath, "status"}, &out)
			if code != 1 || out.String() != "conversation unavailable\n" {
				t.Fatal("malformed existing conversation index was treated as a fresh empty index")
			}
			if !reflect.DeepEqual(before, conversationFixtureSnapshot(t, f.root)) {
				t.Fatal("malformed existing conversation index was changed")
			}
		})
	}
}

func TestConversationCLIAskDoesNotExposePrivateProjectOrSnapshotFields(t *testing.T) {
	fake := &conversationCLIFake{question: ConversationQuestion{RequestID: "opaque-question", Todo: 42, Phase: "posted", Project: "PRIVATE_PROJECT", SenderID: "PRIVATE_SENDER", Title: "PRIVATE_TITLE", BodyFile: "PRIVATE_BODY_FILE", DecisionFile: "PRIVATE_DECISION_FILE", ReceiptFile: "PRIVATE_RECEIPT_FILE"}}
	var out bytes.Buffer
	code := conversationCLIWithLoader(conversationAskArgs(), &out, func(string) (conversationCLIBackend, func(), error) { return fake, func() {}, nil })
	if code != 0 || bytes.Contains(out.Bytes(), []byte("PRIVATE_")) || !bytes.Contains(out.Bytes(), []byte("opaque-question")) {
		t.Fatal("ask metadata exposed private scope or input snapshots")
	}
}
