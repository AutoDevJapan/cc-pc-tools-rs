package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestPR148ListenerCleanupRefusalIsNarrow(t *testing.T) {
	message := []byte("An error occurred: Access to the path '/runner/.credentials' is denied.")
	if !pr148CredentialCleanupDenied(2, message) {
		t.Fatal("known SDK refusal not retained")
	}
	for _, exit := range []int{0, 1, 3, 124} {
		if pr148CredentialCleanupDenied(exit, message) {
			t.Fatal("unrelated exit accepted")
		}
	}
	for _, text := range []string{"permission denied", "Access to another path is denied", strings.ReplaceAll(string(message), "/runner/", "/work/")} {
		if pr148CredentialCleanupDenied(2, []byte(text)) {
			t.Fatal("unrelated failure classified as credential cleanup")
		}
	}
}

func TestPR148RequiresActualSuccessfulOwnedTerminalJob(t *testing.T) {
	p := pr148SyntheticPlan()
	for _, which := range []string{"success", "failure", "cancelled", "skipped", "wrong-id", "wrong-run", "wrong-attempt", "wrong-name", "wrong-runner", "wrong-state"} {
		t.Run(which, func(t *testing.T) {
			j := pr148LinuxJob{ID: p.JobID, RunID: p.RunID, RunAttempt: p.Attempt, Name: "test (ubuntu-latest)", Status: "completed", Conclusion: "success", RunnerName: "owned"}
			switch which {
			case "failure", "cancelled", "skipped":
				j.Conclusion = which
			case "wrong-id":
				j.ID++
			case "wrong-run":
				j.RunID++
			case "wrong-attempt":
				j.RunAttempt++
			case "wrong-name":
				j.Name = "test (windows-latest)"
			case "wrong-runner":
				j.RunnerName = "another"
			case "wrong-state":
				j.Status = "queued"
			}
			calls := 0
			read := func(_ context.Context, ep string, v any) error {
				calls++
				if !strings.HasSuffix(ep, "/actions/jobs/102") {
					t.Error("wrong target endpoint")
				}
				b, _ := json.Marshal(j)
				return json.Unmarshal(b, v)
			}
			err := pr148AwaitLinuxConclusion(context.Background(), p, "owned", read)
			if (err == nil) != (which == "success") || calls != 1 {
				t.Fatal("incorrect official result acceptance", which, calls)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := pr148AwaitLinuxConclusion(ctx, p, "owned", func(context.Context, string, any) error { t.Fatal("read after cancellation"); return nil })
	if !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation ignored")
	}
}
