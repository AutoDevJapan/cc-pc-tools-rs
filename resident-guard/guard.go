package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const (
	stateLimit = 128 * 1024
	logLimit   = 2 * 1024 * 1024
)

type service struct {
	ID, URL, Address, Task, Execute, Arguments, Dependency string
	Console                                                bool
}

type probeResult struct {
	Healthy bool   `json:"healthy"`
	Port    string `json:"port"`
	Code    string `json:"code"`
}

type taskResult struct {
	Valid, Enabled, Running bool
}

type backend interface {
	Probe(context.Context, service) probeResult
	Task(context.Context, service) (taskResult, error)
	Start(context.Context, service) error
}

type serviceState struct {
	Failures    int         `json:"failures"`
	LastHealthy *time.Time  `json:"last_healthy,omitempty"`
	Attempts    []time.Time `json:"attempts"`
}

type guardState struct {
	Version  int                     `json:"version"`
	Services map[string]serviceState `json:"services"`
}

type serviceReport struct {
	ID               string      `json:"id"`
	Probe            probeResult `json:"probe"`
	Action           string      `json:"action"`
	Failures         int         `json:"failures"`
	AttemptsLastHour int         `json:"attempts_last_hour"`
}

type report struct {
	Version    int             `json:"version"`
	CheckedAt  time.Time       `json:"checked_at"`
	FinishedAt time.Time       `json:"finished_at"`
	Healthy    bool            `json:"healthy"`
	StateError string          `json:"state_error,omitempty"`
	Services   []serviceReport `json:"services"`
	DurationMS int64           `json:"duration_ms"`
}

type policy struct {
	FailureThreshold int
	Cooldown         time.Duration
	AttemptWindow    time.Duration
	AttemptLimit     int
}

func defaultPolicy() policy {
	return policy{FailureThreshold: 2, Cooldown: 10 * time.Minute, AttemptWindow: time.Hour, AttemptLimit: 3}
}

// Absence, not merely a failed HTTP response, is required before task startup.
// A listening or unconfirmed port is retained for the existing owner-aware
// watchdog. The guard never kills a service or enables an intentionally disabled task.
func evaluate(ctx context.Context, definitions []service, state *guardState, b backend,
	now time.Time, p policy, persist func(guardState) error) report {
	out := report{Version: 1, CheckedAt: now, Healthy: true, Services: []serviceReport{}}
	observed := map[string]bool{}
	for _, definition := range definitions {
		current := state.Services[definition.ID]
		current.Attempts = recentAttempts(current.Attempts, now, p.AttemptWindow)
		result := serviceReport{ID: definition.ID, Probe: b.Probe(ctx, definition), Action: "healthy"}
		if result.Probe.Healthy {
			current.Failures = 0
			checked := now
			current.LastHealthy = &checked
		} else {
			out.Healthy = false
			current.Failures = min(current.Failures+1, 1_000_000)
			switch {
			case ctx.Err() != nil:
				result.Action = "deadline"
			case result.Probe.Port != "closed":
				result.Action = "listening_or_unconfirmed_port_retained"
			case definition.Dependency != "" && !observed[definition.Dependency]:
				result.Action = "dependency_unhealthy"
			case current.Failures < p.FailureThreshold:
				result.Action = "confirmation_pending"
			case len(current.Attempts) >= p.AttemptLimit:
				result.Action = "restart_budget_exhausted"
			case len(current.Attempts) > 0 && now.Sub(latestAttempt(current.Attempts)) < p.Cooldown:
				result.Action = "cooldown"
			default:
				task, err := b.Task(ctx, definition)
				switch {
				case err != nil:
					result.Action = "task_query_failed"
				case !task.Valid:
					result.Action = "task_definition_mismatch"
				case !task.Enabled:
					result.Action = "task_disabled"
				case task.Running:
					result.Action = "delegated_to_running_task"
				default:
					// Record intent durably before talking to Task Scheduler. A
					// crash or ambiguous startup therefore cannot produce a loop.
					current.Attempts = append(current.Attempts, now)
					state.Services[definition.ID] = current
					if persist(*state) != nil {
						result.Action = "restart_intent_write_failed"
					} else if b.Start(ctx, definition) != nil {
						result.Action = "task_start_unconfirmed"
					} else {
						result.Action = "task_start_requested"
					}
				}
			}
		}
		result.Failures = current.Failures
		result.AttemptsLastHour = len(current.Attempts)
		observed[definition.ID] = result.Probe.Healthy
		state.Services[definition.ID] = current
		out.Services = append(out.Services, result)
	}
	return out
}

func recentAttempts(attempts []time.Time, now time.Time, window time.Duration) []time.Time {
	recent := make([]time.Time, 0, len(attempts))
	for _, attempt := range attempts {
		// A future timestamp is retained rather than evading the restart budget
		// when the wall clock has moved backwards.
		if attempt.After(now.Add(-window)) {
			recent = append(recent, attempt)
		}
	}
	return recent
}

func latestAttempt(attempts []time.Time) time.Time {
	var latest time.Time
	for _, attempt := range attempts {
		if attempt.After(latest) {
			latest = attempt
		}
	}
	return latest
}

func probeHTTP(ctx context.Context, target service, timeout time.Duration) probeResult {
	client := &http.Client{
		Timeout:       timeout,
		Transport:     &http.Transport{Proxy: nil, DisableKeepAlives: true},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.URL, nil)
	if err != nil {
		return probeResult{Port: "unknown", Code: "request_invalid"}
	}
	response, err := client.Do(request)
	if err == nil {
		defer response.Body.Close()
		if response.StatusCode == http.StatusOK {
			var payload struct {
				Status  string `json:"status"`
				Service string `json:"service"`
				Console struct {
					Online bool `json:"online"`
				} `json:"console"`
			}
			data, readErr := io.ReadAll(io.LimitReader(response.Body, 64*1024+1))
			if readErr == nil && len(data) <= 64*1024 && json.Unmarshal(data, &payload) == nil && payload.Status == "ok" &&
				((target.Console && payload.Console.Online) || (!target.Console && payload.Service == "opencodex")) {
				return probeResult{Healthy: true, Port: "open", Code: "healthy"}
			}
		}
		return probeResult{Port: "open", Code: "http_unhealthy"}
	}
	connection, dialErr := (&net.Dialer{Timeout: min(timeout, time.Second)}).DialContext(ctx, "tcp", target.Address)
	if dialErr == nil {
		connection.Close()
		return probeResult{Port: "open", Code: "http_unavailable"}
	}
	if connectionRefused(dialErr) {
		return probeResult{Port: "closed", Code: "connection_refused"}
	}
	return probeResult{Port: "unknown", Code: "connection_unconfirmed"}
}

func loadState(path string, definitions []service) (guardState, error) {
	state := guardState{Version: 1, Services: map[string]serviceState{}}
	data, err := readBounded(path, stateLimit)
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}
	if err != nil || json.Unmarshal(data, &state) != nil || state.Version != 1 || state.Services == nil {
		return state, errors.New("state_invalid_or_unreadable")
	}
	known := map[string]bool{}
	for _, definition := range definitions {
		known[definition.ID] = true
	}
	for id, value := range state.Services {
		if !known[id] || value.Failures < 0 || value.Failures > 1_000_000 || len(value.Attempts) > 64 {
			return state, errors.New("state_invalid_or_unreadable")
		}
		for _, attempt := range value.Attempts {
			if attempt.IsZero() {
				return state, errors.New("state_invalid_or_unreadable")
			}
		}
	}
	return state, nil
}

func readBounded(path string, limit int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, errors.New("file_limit_or_read_error")
	}
	return data, nil
}

func saveJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(directory, ".resident-guard-*.tmp")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if _, err = file.Write(append(data, '\n')); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return replaceFile(temporary, path)
}

func appendReport(path string, out report) error {
	data, err := json.Marshal(out)
	if err != nil {
		return err
	}
	if stat, statErr := os.Stat(path); statErr == nil && stat.Size()+int64(len(data))+1 > logLimit {
		if err := replaceFile(path, path+".1"); err != nil {
			return errors.New("log_rotation_failed")
		}
	} else if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return errors.New("log_stat_failed")
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.Write(append(data, '\n'))
	return err
}

func validateStateDirectory(directory string) error {
	if !filepath.IsAbs(directory) {
		return errors.New("state_directory_must_be_absolute")
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		return errors.New("state_directory_unavailable")
	}
	resolved, err := filepath.EvalSymlinks(directory)
	if err != nil {
		return errors.New("state_directory_indirection_rejected")
	}
	// Windows expands 8.3 names (including the runner's temporary directory)
	// when resolving a path. Compare file identity, and reject links in every
	// input component, rather than mistaking a spelling change for a link.
	for component := filepath.Clean(directory); ; component = filepath.Dir(component) {
		entry, err := os.Lstat(component)
		if err != nil || entry.Mode().Type() != os.ModeDir {
			return errors.New("state_directory_indirection_rejected")
		}
		if filepath.Dir(component) == component {
			break
		}
	}
	inputInfo, inputErr := os.Stat(directory)
	resolvedInfo, resolvedErr := os.Stat(resolved)
	if inputErr != nil || resolvedErr != nil || !os.SameFile(inputInfo, resolvedInfo) {
		return errors.New("state_directory_indirection_rejected")
	}
	for _, name := range []string{"state.json", "status.json", "events.jsonl", "events.jsonl.1"} {
		entry, err := os.Lstat(filepath.Join(directory, name))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return errors.New("state_path_unreadable")
		}
		if err == nil && (!entry.Mode().IsRegular() || entry.Mode()&os.ModeSymlink != 0) {
			return errors.New("state_path_indirection_rejected")
		}
	}
	return nil
}

func runCheck(ctx context.Context, directory string, definitions []service, b backend, p policy) (report, error) {
	started := time.Now()
	out := report{Version: 1, CheckedAt: started.UTC(), Services: []serviceReport{}}
	if err := validateStateDirectory(directory); err != nil {
		return out, err
	}
	statePath := filepath.Join(directory, "state.json")
	state, err := loadState(statePath, definitions)
	if err != nil {
		out.StateError = "state_invalid_or_unreadable"
	} else {
		out = evaluate(ctx, definitions, &state, b, started.UTC(), p, func(s guardState) error { return saveJSON(statePath, s) })
		if saveJSON(statePath, state) != nil {
			out.StateError = "state_write_failed"
		}
	}
	out.FinishedAt = time.Now().UTC()
	out.DurationMS = time.Since(started).Milliseconds()
	if saveJSON(filepath.Join(directory, "status.json"), out) != nil {
		return out, errors.New("status_write_failed")
	}
	if appendReport(filepath.Join(directory, "events.jsonl"), out) != nil {
		return out, errors.New("event_write_failed")
	}
	if out.StateError != "" {
		return out, errors.New(out.StateError)
	}
	return out, nil
}
