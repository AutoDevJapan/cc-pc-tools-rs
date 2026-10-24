package main

// The existing dispatcher gets its own bounded invocation. It shares the
// scheduler's normal lock, priority, ownership, scope and resource checks.
// This command neither changes task eligibility nor polls in a new loop.
import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"time"
)

const schedulerStageHome = "D:/CC/Tools/discord-ask/unattended"
const schedulerStageProgram = "D:/CC/Tools/scheduler/scheduler.exe"
const schedulerStageBudget = 150 * time.Second
const schedulerStageFreshness = 3 * time.Minute

type schedulerStageReport struct {
	Domain   string `json:"domain"`
	Started  string `json:"started_at"`
	Finished string `json:"finished_at"`
	Mode     string `json:"source_mode"`
	OK       bool   `json:"scheduler_ok"`
	Error    string `json:"error,omitempty"`
}

func schedulerStageEnabled(home string) bool {
	var mode struct {
		Domain  string `json:"domain"`
		Enabled bool   `json:"enabled"`
	}
	return readJSON(filepath.Join(home, "scheduler-stage-mode.json"), 4096, &mode) == nil &&
		mode.Domain == "separated-repair-dispatcher-v1" && mode.Enabled
}

func schedulerStageHealthy(home string, now time.Time) bool {
	var report schedulerStageReport
	if readJSON(filepath.Join(home, "scheduler-stage-health.json"), 4096, &report) != nil ||
		report.Domain != "scheduler-stage-result-v1" || report.Mode != "repair-step" || !report.OK || report.Error != "" {
		return false
	}
	started, se := time.Parse(time.RFC3339Nano, report.Started)
	finished, fe := time.Parse(time.RFC3339Nano, report.Finished)
	return se == nil && fe == nil && !finished.Before(started) && !finished.After(now) &&
		now.Sub(finished) <= schedulerStageFreshness && finished.Sub(started) <= schedulerStageBudget+2*time.Second
}

func runSchedulerStage(ctx context.Context, home string, now func() time.Time, run func(context.Context, string, ...string) (string, error)) (schedulerStageReport, error) {
	report := schedulerStageReport{Domain: "scheduler-stage-result-v1", Mode: "repair-step", Started: now().UTC().Format(time.RFC3339Nano)}
	unlock, err := acquire(filepath.Join(home, "scheduler-stage.lock"))
	if err != nil {
		return report, errors.New("scheduler stage busy")
	}
	defer unlock()
	stage, cancel := context.WithTimeout(ctx, schedulerStageBudget)
	defer cancel()
	raw, runErr := run(stage, schedulerStageProgram, "repair-step")
	report.OK = stage.Err() == nil && runErr == nil && schedulerOK(raw)
	if !report.OK {
		report.Error = "scheduler_step_deferred"
	}
	report.Finished = now().UTC().Format(time.RFC3339Nano)
	if err := publish(filepath.Join(home, "scheduler-stage-health.json"), report); err != nil {
		return report, errors.New("scheduler stage report unavailable")
	}
	return report, nil
}

func schedulerStageCLI(args []string, output io.Writer) int {
	if len(args) != 0 {
		fmt.Fprintln(output, "usage: unattended-runner scheduler-tick")
		return 2
	}
	// Fixed production endpoints only. No caller path, grant, ToDo rewrite,
	// stop removal, alternate launcher or arbitrary command is accepted.
	if !schedulerStageEnabled(schedulerStageHome) {
		fmt.Fprintln(output, "scheduler stage not enabled")
		return 1
	}
	report, err := runSchedulerStage(context.Background(), schedulerStageHome, time.Now, func(ctx context.Context, path string, args ...string) (string, error) {
		return runCommandBounded(ctx, schedulerStageBudget, path, args...)
	})
	if err != nil {
		fmt.Fprintln(output, "scheduler stage deferred")
		return 1
	}
	_ = json.NewEncoder(output).Encode(report)
	if !report.OK {
		return 1
	}
	return 0
}
