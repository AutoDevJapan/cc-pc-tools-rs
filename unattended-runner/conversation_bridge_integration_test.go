package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// This helper is called by the real Rust bridge test executable. Only the
// Discord transport is substituted; the Go CLI/log/index/immutable storage are real.
func TestClarificationGoStateHelper(t *testing.T) {
	if os.Getenv("CLARIFICATION_GO_STATE_HELPER") != "1" {
		return
	}
	var args []string
	if json.Unmarshal([]byte(os.Getenv("CLARIFICATION_CALL_ARGS")), &args) != nil || len(args) < 5 {
		t.Fatal("helper input invalid")
	}
	home := os.Getenv("CLARIFICATION_BRIDGE_FIXTURE_HOME")
	r := &Runner{Config: Config{Home: home, Project: "cc-pc-tools-rs"}, State: &State{Destination: Destination{Guild: "100", Forum: "200", Owner: "300", Bot: "400"}}, Now: time.Now}
	r.Run = func(_ context.Context, _ string, argv ...string) (string, error) {
		if argValue(argv, "--clarification-file") == "" {
			return "", errors.New("ordinary log cannot post to a question")
		}
		for _, arg := range argv {
			if arg == "--check-only" {
				return firstCheckReport(argv), nil
			}
		}
		return `{"domain":"new-owner-recovery-messages","delivery":"verified","thread_id":"610","message_id":"620","url":"https://discord.com/channels/100/610","reused":false}`, nil
	}
	var out bytes.Buffer
	code := conversationCLIWithLoader(args[2:], &out, func(string) (conversationCLIBackend, func(), error) { return r, func() {}, nil })
	fmt.Printf("CC_CODE=%d\nCC_REPORT=%s\n", code, strings.TrimSpace(out.String()))
}

func TestClarificationRealRustBridgeCRLFRetryAndExistingGoReservation(t *testing.T) {
	rustTest := os.Getenv("CLARIFICATION_RUST_TEST_EXE")
	if rustTest == "" {
		t.Skip("explicit Rust test executable required for cross-language acceptance")
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, seed := range []bool{false, true} {
		t.Run(fmt.Sprint(seed), func(t *testing.T) {
			// Keep the synthetic Windows path short enough for the existing
			// content-addressed bridge filename (the normal request ID is long).
			home, err := os.MkdirTemp("", "cc3259-")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := os.RemoveAll(home); err != nil {
					t.Error(err)
				}
			})
			command := exec.Command(rustTest, "--exact", "real_go_state_bridge_fixture", "--nocapture")
			command.Env = append(os.Environ(), "CLARIFICATION_BRIDGE_FIXTURE_HOME="+home, "CLARIFICATION_GO_TEST_EXE="+self)
			if seed {
				command.Env = append(command.Env, "CLARIFICATION_SEED_FAILED_LOG=1")
			}
			hide(command)
			result, err := command.CombinedOutput()
			if err != nil {
				t.Fatalf("Rust bridge / real Go retry failed: %v\n%s", err, result)
			}
			var index conversationIndex
			if readJSON(filepath.Join(home, "conversation-questions.json"), maxState, &index) != nil || len(index.Logs) != 1 {
				t.Fatal("one unchanged normal log expected")
			}
			for _, record := range index.Logs {
				body, err := os.ReadFile(record.BodyFile)
				if err != nil || string(body) != "固定の説明です。\n同じ本文を保持します。" || record.Message != "620" {
					t.Fatal("immutable Go input or receipt differs")
				}
			}
		})
	}
}
