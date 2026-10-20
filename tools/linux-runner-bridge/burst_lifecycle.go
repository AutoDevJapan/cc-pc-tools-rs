package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const burstIdleLimit = 2 * time.Minute

const burstIdleExit = 75
const burstUnverifiedExit = 74

type burstDrainState struct {
	ID                                                              int64
	LabelsWithdrawn, RegistrationRemoved, ListenerStopped, Assigned bool
	ProcessDrained, ConfigCleared                                   bool
}

type burstRunner struct {
	ID        int64                         `json:"id"`
	Name      string                        `json:"name"`
	Busy      bool                          `json:"busy"`
	Ephemeral *bool                         `json:"ephemeral"`
	Labels    []struct{ Name, Type string } `json:"labels"`
}

type burstDrainOps struct {
	find       func() (burstRunner, error)
	read       func(int64) (burstRunner, error)
	withdraw   func(int64) error
	workerIdle func() (bool, error)
	remove     func(int64) error
	stopIdle   func() error
	retire     func() bool
	pause      func(time.Duration)
}

// Only the additional, owned ephemeral identity can be withdrawn. Its labels
// are all custom (--no-default-labels), so zero labels prevents new assignment.
// A job already assigned at the boundary is allowed to finish normally.
func drainIdleBurst(p normalProfile, boundID int64, d burstDrainOps) (bool, error) {
	return resumeIdleBurst(p, &burstDrainState{ID: boundID}, d)
}

func resumeIdleBurst(p normalProfile, state *burstDrainState, d burstDrainOps) (bool, error) {
	boundID := state.ID
	if !p.ephemeral || (boundID <= 0 && !state.RegistrationRemoved) {
		return false, errors.New("registration is not bound to this host")
	}
	if state.ListenerStopped {
		return true, nil
	}
	if !state.RegistrationRemoved {
		r, err := d.find()
		if errors.Is(err, errRunnerMetadataNotFound) {
			state.RegistrationRemoved = true
			state.LabelsWithdrawn = true
		} else {
			if err != nil {
				return false, err
			}
			if r.ID != boundID || r.Name != p.runner || (r.Ephemeral != nil && !*r.Ephemeral) {
				return false, errors.New("idle drain runner identity rejected")
			}
			if r.Busy {
				state.Assigned = true
				return false, nil
			}
			if !state.LabelsWithdrawn {
				for _, label := range r.Labels {
					if label.Type != "custom" {
						return false, errors.New("idle drain cannot withdraw default labels")
					}
				}
				if len(r.Labels) != 0 {
					if err := d.withdraw(boundID); err != nil {
						return false, err
					}
				}
				state.LabelsWithdrawn = true
			}
		}
		if !state.RegistrationRemoved {
			for sample := 0; sample < 3; sample++ {
				current, err := d.read(boundID)
				if errors.Is(err, errRunnerMetadataNotFound) {
					state.RegistrationRemoved = true
					break
				}
				if err != nil {
					return false, err
				}
				if current.ID != boundID || current.Name != p.runner || (current.Ephemeral != nil && !*current.Ephemeral) || len(current.Labels) != 0 {
					return false, errors.New("withdrawn runner identity/labels not confirmed")
				}
				idle, probeErr := d.workerIdle()
				if probeErr != nil {
					return false, probeErr
				}
				if current.Busy || !idle {
					state.Assigned = true
					return false, nil
				}
				if sample < 2 {
					d.pause(5 * time.Second)
				}
			}
			if !state.RegistrationRemoved {
				if d.retire != nil && !d.retire() {
					state.Assigned = true
					return false, nil
				}
				if err := d.remove(boundID); err != nil {
					return false, err
				}
				state.RegistrationRemoved = true
			}
		}
	}
	if boundID > 0 {
		if _, err := d.read(boundID); !errors.Is(err, errRunnerMetadataNotFound) {
			return false, errors.New("idle runner removal was not confirmed")
		}
	}
	idle, probeErr := d.workerIdle()
	if probeErr != nil {
		return false, probeErr
	}
	if !idle {
		state.Assigned = true
		return false, nil
	}
	if d.retire != nil && !d.retire() {
		state.Assigned = true
		return false, nil
	}
	if d.stopIdle != nil {
		if err := d.stopIdle(); err != nil {
			return false, err
		}
	}
	state.ListenerStopped = true
	return true, nil
}

type burstLifecycleOps struct {
	now            func() time.Time
	after          func(time.Duration) <-chan time.Time
	pause          func(time.Duration)
	record         func(string)
	cleanup        func(*burstDrainState) burstCleanupOps
	stopUnadmitted func()
	save           func(*burstDrainState) error
}

func recordBurstLifecycle(stage string) {
	reportStage(stage)
	data, err := json.Marshal(struct {
		Stage  string    `json:"stage"`
		At     time.Time `json:"at"`
		Runner string    `json:"runner"`
	}{stage, time.Now().UTC(), compiledNormalProfile.runner})
	if err == nil {
		_ = os.WriteFile(filepath.Join(".ci-runtime", "burst-idle-status.private.json"), data, 0600)
	}
}

func awaitBurstProcess(done <-chan int, registered <-chan struct{}, bound func() int64, active func() bool, retire func() bool, journal *burstJournal) int {
	var save func(*burstDrainState) error
	if journal != nil {
		save = journal.save
	}
	return awaitBurstProcessWith(done, registered, bound, active, retire, burstLifecycleOps{
		now: time.Now, after: time.After, pause: time.Sleep, record: recordBurstLifecycle,
		save: save,
		stopUnadmitted: func() {
			_, _ = burstOutput(20*time.Second, docker, "exec", normalContainer, "/opt/bridge/bridge", "stop-idle")
		},
		cleanup: func(state *burstDrainState) burstCleanupOps {
			return burstCleanupOps{
				lookup: func() (int64, error) { return configuredBurstRunnerID(compiledNormalProfile) },
				drain: func(s *burstDrainState) (bool, error) {
					return drainOwnedBurst(compiledNormalProfile, s, burstDrainBackend(s.ID), retire)
				},
				clear: clearBurstConfiguration,
			}
		},
	})
}

func awaitBurstProcessWith(done <-chan int, registered <-chan struct{}, bound func() int64, active func() bool, retire func() bool, d burstLifecycleOps) int {
	state := burstDrainState{}
	code := burstUnverifiedExit
	idleExpired, setupExpired := false, false
	consume := func(result int) { code = result; state.ProcessDrained = true; done = nil }
	waitStopped := func() error {
		if state.ProcessDrained {
			return nil
		}
		select {
		case result := <-done:
			consume(result)
			return nil
		case <-d.after(3 * time.Minute):
			return errors.New("owned runner group completion unverified")
		}
	}
	// A successful configuration must acknowledge before run.sh can start.
	select {
	case result := <-done:
		consume(result)
	case <-registered:
		select {
		case result := <-done:
			consume(result)
		case <-d.after(burstIdleLimit):
			idleExpired = true
			if active() {
				d.record("burst-assigned-job-running")
				consume(<-done)
				idleExpired = false
			}
		}
	case <-d.after(6 * time.Minute):
		setupExpired = true
		if active() || !retire() {
			d.record("burst-assigned-job-running")
			consume(<-done)
			setupExpired = false
		} else if err := waitStopped(); err != nil {
			d.stopUnadmitted()
			d.record("burst-setup-unverified")
			return burstUnverifiedExit
		}
	}
	state.ID = bound()
	// Only ACK-bound identity or config.sh's proven-success/failed-ACK exit 19
	// permits discovery. Never adopt another registration after exit 96 or an
	// unproven config failure, even when it has the same display name.
	if state.ID == 0 && state.ProcessDrained && code != 19 {
		if setupExpired {
			return burstUnverifiedExit
		}
		return code
	}
	ops := d.cleanup(&state)
	ops.waitStopped = waitStopped
	policy := burstRetryPolicy{now: d.now, pause: d.pause, record: d.record, save: d.save}
	for {
		ok, err := retryBurstCleanup(&state, ops, policy, d.now().Add(10*time.Minute))
		if err != nil {
			if !state.ProcessDrained && (active() || !retire()) {
				d.record("burst-assigned-job-running")
				consume(<-done)
				idleExpired = false
				continue // The post-job cleanup gets its own bounded window.
			}
			if !state.ProcessDrained {
				d.stopUnadmitted()
			}
			d.record("burst-cleanup-unverified: " + err.Error())
			return burstUnverifiedExit
		}
		if ok {
			break
		}
		d.record("burst-assigned-job-running")
		consume(<-done)
		state.Assigned = false
		idleExpired = false
	}
	d.record("burst-local-config-cleared")
	if setupExpired {
		return burstUnverifiedExit
	}
	if idleExpired && (code == 0 || code == 130 || code == 143 || code == 255) {
		return burstIdleExit
	}
	return code
}

func drainOwnedBurst(p normalProfile, s *burstDrainState, backend burstDrainOps, retire func() bool) (bool, error) {
	backend.retire = func() bool { return s.ProcessDrained || retire() }
	ok, err := resumeIdleBurst(p, s, backend)
	if !ok && err == nil && s.ProcessDrained {
		s.Assigned = false
		return false, errors.New("completed runner metadata has not settled")
	}
	return ok, err
}

func burstDrainBackend(id int64) burstDrainOps {
	path := func(id int64) string { return "repos/" + repo + "/actions/runners/" + strconv.FormatInt(id, 10) }
	read := func(id int64) (burstRunner, error) { var r burstRunner; err := runnerAPI(path(id), &r); return r, err }
	return burstDrainOps{
		find: func() (burstRunner, error) { return read(id) }, read: read,
		withdraw: func(id int64) error {
			var response struct {
				Labels []any `json:"labels"`
			}
			if err := runnerRequest("DELETE", path(id)+"/labels", &response); err != nil {
				return err
			}
			if len(response.Labels) != 0 {
				return errors.New("runner labels remain")
			}
			return nil
		},
		workerIdle: func() (bool, error) {
			_, err := burstOutput(15*time.Second, docker, "exec", normalContainer, "/opt/bridge/bridge", "idle-state")
			return burstIdleProbeResult(exitCode(err), err)
		},
		remove: func(id int64) error { return runnerRequest("DELETE", path(id), nil) },
		stopIdle: func() error {
			_, err := burstOutput(20*time.Second, docker, "exec", normalContainer, "/opt/bridge/bridge", "stop-idle")
			return err
		}, pause: time.Sleep,
	}
}

func burstIdleProbeResult(code int, err error) (bool, error) {
	if err == nil {
		return true, nil
	}
	if code == burstWorkerActiveExit {
		return false, nil
	}
	return false, err
}

func clearBurstConfiguration() error {
	_, err := burstOutput(15*time.Second, docker, "exec", normalContainer, "/opt/bridge/bridge", "configuration-absent")
	if err == nil {
		return nil
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != burstConfigurationPresentExit {
		return errors.New("owned SDK configuration state unavailable")
	}
	token, err := burstOutput(30*time.Second, gh, "api", "--method", "POST", "repos/"+repo+"/actions/runners/remove-token", "--jq", ".token")
	if err != nil || token == "" {
		return errors.New("owned SDK removal token unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, docker, "exec", "-i", normalContainer, "/opt/bridge/bridge", "unconfigure-idle")
	hidden(c)
	c.WaitDelay = 3 * time.Second
	c.Stdin = strings.NewReader(token + "\n")
	token = ""
	if err := c.Run(); err != nil {
		return errors.New("owned SDK configuration removal unverified")
	}
	return nil
}

func burstOutput(limit time.Duration, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()
	c := exec.CommandContext(ctx, name, args...)
	hidden(c)
	c.WaitDelay = 3 * time.Second
	return output(c)
}

// This notification is made synchronously after config.sh succeeds and before
// run.sh can receive any job. A pre-existing .runner returns 96 before it.
func notifyBurstRegistration() bool {
	key, err := os.ReadFile("/tmp/bridge-key")
	if err != nil {
		return false
	}
	body, _ := json.Marshal(request{Repository: repo})
	client := http.Client{Timeout: 45 * time.Second}
	for attempt := 0; attempt < 3; attempt++ {
		req, err := http.NewRequest("POST", bridgeURL("normal", "/registered"), bytes.NewReader(body))
		if err != nil {
			return false
		}
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(key)))
		response, err := client.Do(req)
		if err == nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
			response.Body.Close()
			if response.StatusCode == 204 {
				return true
			}
		}
		if attempt < 2 {
			time.Sleep(time.Second)
		}
	}
	return false
}

func configuredBurstRunnerID(p normalProfile) (int64, error) {
	return configuredBurstRunnerIDWith(p, runnerAPI)
}

func configuredBurstRunnerIDWith(p normalProfile, api func(string, any) error) (int64, error) {
	var response struct {
		Total   int           `json:"total_count"`
		Runners []burstRunner `json:"runners"`
	}
	if err := api("repos/"+repo+"/actions/runners?per_page=100", &response); err != nil {
		return 0, err
	}
	if response.Total > 100 {
		return 0, errors.New("runner inventory exceeds bound")
	}
	id := int64(0)
	for _, r := range response.Runners {
		if r.Name == p.runner {
			if id != 0 || r.ID <= 0 || (r.Ephemeral != nil && !*r.Ephemeral) {
				return 0, errors.New("registration identity ambiguous")
			}
			id = r.ID
		}
	}
	if id == 0 {
		return 0, errOwnedRunnerAbsent
	}
	return id, nil
}
