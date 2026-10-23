package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Civ6QueueConfig enables the finite CI queue pass (ToDo2962/2966). Nothing
// runs unless the deployed runner configuration pins the reviewed executable.
// The executable, its configuration and the arguments are fixed; no value from
// Discord, the ToDo list or GitHub reaches the command line.
type Civ6QueueConfig struct {
	Exe    string `json:"exe"`
	SHA256 string `json:"sha256"`
	Config string `json:"config"`
}

// Civ6QueueHealth is the bounded summary of one pass.
type Civ6QueueHealth struct {
	Status        string   `json:"status"`
	Admitted      int      `json:"admitted"`
	Launched      int      `json:"launched"`
	Active        int      `json:"active"`
	BudgetBlocked int      `json:"budget_blocked"`
	Fenced        []string `json:"fenced,omitempty"`
	Overdue       []string `json:"overdue,omitempty"`
	Errors        []string `json:"errors,omitempty"`
}

var civ6SHA = regexp.MustCompile(`^[0-9a-f]{64}$`)

const civ6QueueMinRemaining = 8 * time.Second
const civ6QueueBudget = 15 * time.Second

func (c Civ6QueueConfig) valid() bool {
	clean := func(p string) bool {
		return filepath.IsAbs(p) && filepath.ToSlash(filepath.Clean(p)) == filepath.ToSlash(p) && !strings.ContainsAny(p, "\x00\r\n")
	}
	return clean(c.Exe) && clean(c.Config) && civ6SHA.MatchString(c.SHA256)
}

func civ6VerifyPin(path, want string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Size() > 256<<20 {
		return errors.New("queue executable is not a bounded regular file")
	}
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return err
	}
	if hex.EncodeToString(h.Sum(nil)) != want {
		return errors.New("queue executable differs from its pin")
	}
	return nil
}

// civ6CIQueue runs one finite pass in periodicTick's separate bounded phase. It can
// only add a health note: a missing time budget, a rejected pin or a failing
// pass never changes intake, scheduling or reporting, and the heavy job is
// started by the queue as a detached fixed worker, never waited for here.
func (r *Runner) civ6CIQueue(ctx context.Context, health *Health) {
	cfg := r.Config.Civ6Queue
	if cfg == nil {
		return
	}
	if !cfg.valid() {
		health.Errors = append(health.Errors, "civ6_ci_queue_configuration_invalid")
		return
	}
	budget := civ6QueueBudget
	if deadline, ok := ctx.Deadline(); ok {
		left := time.Until(deadline)
		if left < civ6QueueMinRemaining {
			health.Errors = append(health.Errors, "civ6_ci_queue_skipped_no_time")
			return
		}
		if left-3*time.Second < budget {
			budget = left - 3*time.Second
		}
	}
	if ctx.Err() != nil {
		return
	}
	verify := r.Civ6Verify
	if verify == nil {
		verify = civ6VerifyPin
	}
	if err := verify(cfg.Exe, cfg.SHA256); err != nil {
		health.Errors = append(health.Errors, "civ6_ci_queue_pin_rejected")
		return
	}
	child, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	out, err := r.Run(child, cfg.Exe, "tick", "--config", cfg.Config)
	if err != nil {
		health.Errors = append(health.Errors, "civ6_ci_queue_deferred")
		return
	}
	var report Civ6QueueHealth
	if len(out) > 64<<10 || json.Unmarshal([]byte(out), &report) != nil {
		health.Errors = append(health.Errors, "civ6_ci_queue_report_unreadable")
		return
	}
	if len(report.Fenced) > 8 {
		report.Fenced = report.Fenced[:8]
	}
	if len(report.Overdue) > 8 {
		report.Overdue = report.Overdue[:8]
	}
	if len(report.Errors) > 8 {
		report.Errors = report.Errors[:8]
	}
	health.Civ6Queue = &report
	if len(report.Fenced) != 0 {
		health.Errors = append(health.Errors, "civ6_ci_queue_fenced")
	}
}
