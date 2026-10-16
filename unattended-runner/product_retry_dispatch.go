package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const productRetryTodoCLI = "D:/CC/Tools/todo/todo.exe"
const productRetryRunCLI = "D:/CC/Tools/codex-jobs/codex_jobs.exe"

var productRetryKeyPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)
var productRetryJobPattern = regexp.MustCompile(`^[0-9]{8}-[0-9]{6}-[A-Za-z0-9_-]+$`)

// This is an observation, never a second launch ledger. The normal API owns
// the durable intent, snapshots, actual session, attempt limit and job binding.
type productRetryObservation struct {
	ID             uint64 `json:"id"`
	At             string `json:"checked_at"`
	BaseKey        string `json:"base_key"`
	Key            string `json:"key,omitempty"`
	Worker         string `json:"observed_worker"`
	Job            string `json:"job,omitempty"`
	Phase          string `json:"phase"`
	Saved          bool   `json:"saved"`
	Deferred       bool   `json:"deferred"`
	ReserveMS      int64  `json:"reserve_ms"`
	RegistrationMS int64  `json:"registration_ms"`
	BindingMS      int64  `json:"binding_ms"`
}

func (r *Runner) productRetryObservation(h *Health, o productRetryObservation) {
	if err := publish(filepath.Join(r.Config.Home, "product-retry-observation.json"), o); err != nil {
		h.Errors = append(h.Errors, "product_retry_observation_deferred")
	}
}

func (r *Runner) resumeProductRetry(ctx context.Context, h *Health, candidate *productRetryCandidate) {
	if ctx.Err() != nil || !finiteProductRetry(r.Now()) {
		return
	}
	o := productRetryObservation{ID: productRetryID, At: r.Now().UTC().Format(time.RFC3339Nano), BaseKey: candidate.Key, Worker: candidate.ObservedWorker}
	fail := func(phase string) {
		o.Phase = phase
		o.Deferred = true
		h.Errors = append(h.Errors, "product_retry_"+phase)
		r.productRetryObservation(h, o)
	}
	// Reserve invokes the existing verify-product-resume guard and qualification
	// checks under the normal locks, including real PID/session metadata. Never
	// release-ready or Start here: both would introduce a destructive crash gap.
	started := time.Now()
	raw, err := r.Run(ctx, productRetryTodoCLI, "product-retry-reserve", "1626", "--key", candidate.Key, "--expected-worker", candidate.ObservedWorker)
	o.ReserveMS = time.Since(started).Milliseconds()
	var reserved struct {
		ID            uint64   `json:"id"`
		Reserved      bool     `json:"reserved"`
		Continued     bool     `json:"continued_existing"`
		Saved         *bool    `json:"saved"`
		Key           string   `json:"key"`
		BaseKey       string   `json:"base_key"`
		Attempt       *uint64  `json:"attempt"`
		Launched      *uint64  `json:"launched"`
		WaitPreserved bool     `json:"wait_preserved"`
		Executable    string   `json:"run_executable"`
		Args          []string `json:"run_args"`
		Job           string   `json:"job"`
	}
	if err != nil || json.Unmarshal([]byte(raw), &reserved) != nil || reserved.ID != productRetryID || reserved.Launched == nil || *reserved.Launched != 0 || reserved.Saved == nil {
		fail("reserve_deferred")
		return
	}
	if reserved.Continued {
		if reserved.Reserved || !productRetryJobPattern.MatchString(reserved.Job) || len(reserved.Args) != 0 || reserved.Executable != "" {
			fail("reserve_receipt_deferred")
			return
		}
		o.Phase, o.Job, o.Saved = "continued_existing", reserved.Job, *reserved.Saved
		if o.Saved {
			h.Retried++
		}
		r.productRetryObservation(h, o)
		return // The same bound queued job is already owned by the normal queue.
	}
	if !reserved.Reserved || !reserved.WaitPreserved || reserved.BaseKey != candidate.Key || !productRetryKeyPattern.MatchString(reserved.Key) || reserved.Attempt == nil || *reserved.Attempt > 2 ||
		!sameProductPath(reserved.Executable, productRetryRunCLI) || !productRetryArgsMatch(reserved.Args, reserved.Key) {
		fail("reserve_receipt_deferred")
		return
	}
	o.Key = reserved.Key // BASE input and EFFECTIVE attempt key are distinct.
	if ctx.Err() != nil || !finiteProductRetry(r.Now()) {
		fail("registration_deferred")
		return
	}
	// A lost response is recovered next tick by Reserve and the SAME launch key.
	// Never concatenate the returned argument array into a shell command.
	started = time.Now()
	raw, err = r.Run(ctx, reserved.Executable, reserved.Args...)
	o.RegistrationMS = time.Since(started).Milliseconds()
	job := strings.TrimSpace(raw)
	if err != nil || !productRetryJobPattern.MatchString(job) {
		fail("registration_deferred")
		return
	}
	o.Job = job
	if ctx.Err() != nil || !finiteProductRetry(r.Now()) {
		fail("binding_deferred")
		return
	}
	started = time.Now()
	raw, err = r.Run(ctx, productRetryTodoCLI, "product-retry-bind", "1626", "--key", reserved.Key, "--job", job)
	o.BindingMS = time.Since(started).Milliseconds()
	var bound struct {
		ID       uint64  `json:"id"`
		Bound    bool    `json:"bound"`
		Reused   bool    `json:"reused"`
		Deferred bool    `json:"deferred"`
		Job      string  `json:"job"`
		Launched *uint64 `json:"launched"`
	}
	if err != nil || json.Unmarshal([]byte(raw), &bound) != nil || bound.ID != productRetryID || !bound.Bound || bound.Job != job || bound.Launched == nil || *bound.Launched != 0 {
		fail("binding_deferred")
		return
	}
	o.Phase, o.Saved, o.Deferred = "bound", !bound.Reused, bound.Deferred
	if !bound.Deferred && !bound.Reused {
		h.Retried++
	}
	r.productRetryObservation(h, o)
}

func productRetryArgsMatch(args []string, key string) bool {
	if len(args) == 0 || args[0] != "run" {
		return false
	}
	// Check the identity envelope; execution settings remain the exact API
	// array. Reconstructing session/model/scope here would discard its proof.
	for flag, expected := range map[string]string{"--todo-id": "1626", "--launch-key": key, "--original-product-retry-key": key} {
		matches := 0
		for i := 1; i < len(args); i++ {
			if args[i] == flag {
				if i+1 >= len(args) || args[i+1] != expected {
					return false
				}
				matches++
			}
		}
		if matches != 1 {
			return false
		}
	}
	return true
}
