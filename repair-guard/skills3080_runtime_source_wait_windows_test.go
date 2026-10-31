package main

import (
	"strings"
	"testing"
	"time"
)

func sourceWaitResult(raw string) skillsSourceCommandResult {
	return skillsSourceCommandResult{stdout: []byte(raw), ok: true, diagnostic: object{}}
}

func TestSkills3080SourceAdmissionWaitsOnlyForKnownNonLaunch(t *testing.T) {
	for _, reason := range []string{"capacity", "priority", "busy"} {
		t.Run(reason, func(t *testing.T) {
			now := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
			deadline := now.Add(skillsSourceAdmissionBudget)
			calls, checks, waits := 0, 0, 0
			first := `{"id":6291,"launched":null,"allowance_now":0}`
			if reason == "priority" {
				first = `{"id":6291,"launched":null,"priority_wait":6291}`
			}
			if reason == "busy" {
				first = `{"id":6291,"launched":null,"busy":true}`
			}
			ops := skillsSourceAdmissionOps{now: func() time.Time { return now }, check: func() string { checks++; return "" }, wait: func(d time.Duration) { waits++; now = now.Add(d) }, run: func(d time.Duration) skillsSourceCommandResult {
				calls++
				now = now.Add(30 * time.Second)
				if calls == 1 {
					return sourceWaitResult(first)
				}
				return sourceWaitResult(`{"id":6291,"launched":{"item":6291,"job":"20300101-000001-todo-6291"}}`)
			}}
			out := skillsSourceAwaitAdmission(deadline, 6291, hashBytes([]byte("request")), strings.Repeat("a", 32), ops)
			if out["qualified"] != true || calls != 2 || checks != 2 || waits != 1 {
				t.Fatalf("normal admission did not continue once: qualified=%v calls=%d checks=%d waits=%d", out["qualified"], calls, checks, waits)
			}
		})
	}
}

func TestSkills3080SourceAdmissionNeverRepeatsUnknownActiveOrAccepted(t *testing.T) {
	for _, c := range []struct {
		name   string
		result skillsSourceCommandResult
	}{
		{"accepted", sourceWaitResult(`{"id":6291,"launched":"20300101-000001-todo-6291"}`)},
		{"active", sourceWaitResult(`{"id":6291,"launched":null,"active":true,"allowance_now":0}`)},
		{"invalid", sourceWaitResult(`{"id":6291,"launched":false}`)},
		{"unknown", sourceWaitResult(`{"id":6291}`)},
		{"preparation", sourceWaitResult(`{"id":6291,"launched":null,"preparation_reason":"tool_failed"}`)},
		{"normal_timeout", skillsSourceCommandResult{diagnostic: object{"outcome": "command_failed", "command_error": "TimeoutError"}}},
		{"outer_timeout", skillsSourceCommandResult{diagnostic: object{"outcome": "timeout"}}},
	} {
		t.Run(c.name, func(t *testing.T) {
			now := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
			calls, waits := 0, 0
			ops := skillsSourceAdmissionOps{now: func() time.Time { return now }, check: func() string { return "" }, wait: func(time.Duration) { waits++ }, run: func(time.Duration) skillsSourceCommandResult { calls++; return c.result }}
			skillsSourceAwaitAdmission(now.Add(skillsSourceAdmissionBudget), 6291, hashBytes([]byte("request")), strings.Repeat("a", 32), ops)
			if calls != 1 || waits != 0 {
				t.Fatalf("non-retriable outcome repeated: calls=%d waits=%d", calls, waits)
			}
		})
	}
}

func TestSkills3080SourceAdmissionRechecksBeforeEveryAttempt(t *testing.T) {
	for _, stage := range []string{"source_inputs_changed", "source_not_empty", "source_directory", "source_registration"} {
		now := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
		calls, checks := 0, 0
		ops := skillsSourceAdmissionOps{now: func() time.Time { return now }, check: func() string {
			checks++
			if checks == 2 {
				return stage
			}
			return ""
		}, wait: func(d time.Duration) { now = now.Add(d) }, run: func(time.Duration) skillsSourceCommandResult {
			calls++
			return sourceWaitResult(`{"id":6291,"launched":null,"allowance_now":0}`)
		}}
		out := skillsSourceAwaitAdmission(now.Add(skillsSourceAdmissionBudget), 6291, hashBytes([]byte("request")), strings.Repeat("a", 32), ops)
		if calls != 1 || checks != 2 || out["qualified"] != false || out["failure_stage"] != stage {
			t.Fatal("changed actor/spec/generation/source was ignored")
		}
	}
}

func TestSkills3080SourceAdmissionUsesOneDeadlineIncludingChecks(t *testing.T) {
	for _, used := range []time.Duration{0, 100 * time.Second, skillsSourceAdmissionBudget} {
		now := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
		deadline := now.Add(skillsSourceAdmissionBudget)
		calls := 0
		var limit time.Duration
		ops := skillsSourceAdmissionOps{now: func() time.Time { return now }, check: func() string { now = now.Add(used); return "" }, wait: func(d time.Duration) { now = now.Add(d) }, run: func(d time.Duration) skillsSourceCommandResult {
			calls++
			limit = d
			now = now.Add(d)
			return sourceWaitResult(`{"id":6291,"launched":null,"allowance_now":0}`)
		}}
		out := skillsSourceAwaitAdmission(deadline, 6291, hashBytes([]byte("request")), strings.Repeat("a", 32), ops)
		if used == skillsSourceAdmissionBudget {
			if calls != 0 || out["failure_stage"] != "source_wait_deadline" {
				t.Fatal("check consumed deadline but command was started")
			}
			continue
		}
		want := skillsSourceAdmissionAttempt
		if skillsSourceAdmissionBudget-used < want {
			want = skillsSourceAdmissionBudget - used
		}
		if limit > want || calls > skillsSourceAdmissionAttempts || now.After(deadline) {
			t.Fatalf("relative budget restarted: calls=%d limit=%v elapsed=%v", calls, limit, now.Sub(deadline.Add(-skillsSourceAdmissionBudget)))
		}
	}
}

func TestSkills3080SourceAdmissionHasFiniteAttemptLimitAndCLIMetadata(t *testing.T) {
	now := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	calls := 0
	ops := skillsSourceAdmissionOps{now: func() time.Time { return now }, check: func() string { return "" }, wait: func(d time.Duration) { now = now.Add(d) }, run: func(time.Duration) skillsSourceCommandResult {
		calls++
		return sourceWaitResult(`{"id":6291,"launched":null,"allowance_now":0}`)
	}}
	out := skillsSourceAwaitAdmission(now.Add(skillsSourceAdmissionBudget), 6291, hashBytes([]byte("request")), strings.Repeat("a", 32), ops)
	d := obj(out["normal_dispatch"])
	if calls != skillsSourceAdmissionAttempts || out["qualified"] != false || d["admission_attempts"] != calls || d["admission_wait_exhausted"] != true {
		t.Fatal("normal wait had no finite endpoint")
	}
	if _, valid := skillsSourceDispatchDiagnostic(d); !valid {
		t.Fatal("normal wait diagnostic would be discarded by the CLI")
	}
}
