package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIntakeDiagnosticKeepsLegacyFailureAndExcludesRawErrors(t *testing.T) {
	for _, diagnostic := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy", true: "diagnostic"}[diagnostic], func(t *testing.T) {
			r, path, _ := recoveryBudgetFixture(t, 1024, "new-owner-recovery-messages")
			plan := intakeBindInput{Domain: "discord-intake-binding-v1", Message: "600", Thread: "500", Author: "300", IntakeBinding: IntakeBinding{Todo: 42, Project: "fixture", SourceSHA: strings.Repeat("a", 64), Evidence: "verified fixture only"}}
			input := filepath.Join(filepath.Dir(path), "binding.private.json")
			data, _ := json.Marshal(plan)
			if err := os.WriteFile(input, data, 0600); err != nil {
				t.Fatal(err)
			}
			const private = "private-loader-error-not-for-output"
			var out bytes.Buffer
			args := []string{"bind", "--input", input}
			if diagnostic {
				args = append(args, "--diagnostic")
			}
			code := intakeCLIWith(args, &out, func(string, bool) (*Runner, func(), error) {
				return nil, nil, errors.New(private + " " + r.Config.Home)
			}, func(context.Context, string) bool { return true })
			if code != 1 || strings.Contains(out.String(), private) || strings.Contains(out.String(), "fixture") {
				t.Fatal("intake failure disclosed private or supplied error text", code)
			}
			if !diagnostic {
				if out.String() != "intake unavailable\n" {
					t.Fatal("legacy failure contract changed")
				}
				return
			}
			var result map[string]any
			if json.Unmarshal(out.Bytes(), &result) != nil || result["domain"] != "discord-intake-diagnostic-v1" || result["stage"] != "saved_state_loader" || result["state_change"] != "not_inferred" {
				t.Fatal("fixed loader stage unavailable", out.String())
			}
		})
	}
}

func TestIntakeDiagnosticIdentifiesDedicatedQuestionWithoutMutation(t *testing.T) {
	r, path, before := recoveryBudgetFixture(t, 1024, "new-owner-recovery-messages")
	q := &Request{Message: "600", Thread: "500", Author: "300", Phase: "origin_pending", Text: "private-current-source"}
	r.State.Requests[q.Message] = q
	plan := intakeBindInput{Domain: "discord-intake-binding-v1", Message: q.Message, Thread: q.Thread, Author: q.Author, IntakeBinding: IntakeBinding{Todo: 42, Project: "fixture", SourceSHA: ordinaryVersion(q), Evidence: "verified fixture only"}}
	input := filepath.Join(filepath.Dir(path), "binding.private.json")
	data, _ := json.Marshal(plan)
	if err := os.WriteFile(input, data, 0600); err != nil {
		t.Fatal(err)
	}
	stateBefore, _ := json.Marshal(r.State)
	var out bytes.Buffer
	loads, unlocks, commands := 0, 0, 0
	r.Run = func(context.Context, string, ...string) (string, error) {
		commands++
		return "", errors.New("unexpected external command")
	}
	code := intakeCLIWith([]string{"bind", "--input", input, "--diagnostic"}, &out, func(string, bool) (*Runner, func(), error) {
		loads++
		return r, func() { unlocks++ }, nil
	}, func(context.Context, string) bool { return true })
	var result map[string]any
	after, err := os.ReadFile(path)
	stateAfter, _ := json.Marshal(r.State)
	if code != 1 || json.Unmarshal(out.Bytes(), &result) != nil || result["stage"] != "original_question_context" || loads != 1 || unlocks != 1 || commands != 0 || err != nil || !bytes.Equal(before, after) || !bytes.Equal(stateBefore, stateAfter) || strings.Contains(out.String(), q.Text) {
		t.Fatal("diagnostic bypassed original context or changed input", code, out.String())
	}
}

func TestIntakeDiagnosticDoesNotReplaceTheReturnedFailureWithAnotherGate(t *testing.T) {
	r := &Runner{State: &State{Requests: map[string]*Request{"600": {Message: "600", Thread: "500"}}}, KnownQuestionThreads: map[string]bool{"500": true}}
	plan := intakeBindInput{Message: "600"}
	if got := intakeBindingFailureStage(r, plan, errors.New("unknown private I/O error")); got != "binding_operation" {
		t.Fatal("unrelated failed operation was mislabeled as a routing gate", got)
	}
	if got := intakeBindingFailureStage(r, plan, errors.New("ordinary binding task unavailable")); got != "target_task_scope" {
		t.Fatal("returned target failure was replaced by a different gate", got)
	}
}
