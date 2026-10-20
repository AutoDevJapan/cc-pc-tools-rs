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
	if !p.ephemeral || boundID <= 0 {
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
				if err := d.withdraw(boundID); err != nil {
					return false, err
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
				if err := d.remove(boundID); err != nil {
					return false, err
				}
				state.RegistrationRemoved = true
			}
		}
	}
	if _, err := d.read(boundID); !errors.Is(err, errRunnerMetadataNotFound) {
		return false, errors.New("idle runner removal was not confirmed")
	}
	idle, probeErr := d.workerIdle()
	if probeErr != nil {
		return false, probeErr
	}
	if !idle {
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

type burstWaitPolicy struct {
	now            func() time.Time
	after          func(time.Duration) <-chan time.Time
	active         func() bool
	retire         func() bool
	stopUnadmitted func()
}

func waitBurstProcess(done <-chan int, expired <-chan time.Time, idle func() (bool, error), record func(string), policies ...burstWaitPolicy) int {
	p := burstWaitPolicy{now: time.Now, after: time.After, active: func() bool { return false }, retire: func() bool { return true }, stopUnadmitted: func() {}}
	if len(policies) > 0 {
		p = policies[0]
	}
	select {
	case code := <-done:
		return code
	case <-expired:
		deadline := p.now().Add(10 * time.Minute)
		for {
			select {
			case code := <-done:
				return code
			default:
			}
			if p.active() {
				record("burst-assigned-job-running")
				return <-done
			}
			removed, err := idle()
			if removed && err == nil {
				record("burst-idle-unregistered")
				select {
				case code := <-done:
					if code == 0 || code == 130 || code == 143 || code == 255 {
						return burstIdleExit
					}
					return code
				case <-p.after(time.Minute):
					if p.active() || !p.retire() {
						return <-done
					}
					record("burst-idle-group-unverified")
					return burstUnverifiedExit
				}
			}
			if err == nil {
				record("burst-assigned-job-running")
				return <-done
			}
			if !p.now().Before(deadline) {
				// Retirement is atomic with the job-start hook. Accepted work
				// keeps its RPC server; no new source may start after retirement.
				if p.active() || !p.retire() {
					record("burst-assigned-job-running")
					return <-done
				}
				p.stopUnadmitted()
				record("burst-idle-drain-unverified")
				return burstUnverifiedExit
			}
			record("burst-idle-drain-retry")
			select {
			case code := <-done:
				return code
			case <-p.after(30 * time.Second):
			}
		}
	}
}

func awaitBurstProcess(done <-chan int, registered <-chan struct{}, bound func() int64, active func() bool, retire func() bool) int {
	// run.sh cannot start before the authenticated registration acknowledgement.
	select {
	case code := <-done:
		if code == 19 {
			return rollbackConfiguredBurst(bound())
		}
		return code
	case <-registered:
	case <-time.After(6 * time.Minute):
		if active() || !retire() {
			return <-done
		}
		reportStage("burst-setup-unverified")
		return burstUnverifiedExit
	}
	timer := time.NewTimer(burstIdleLimit)
	defer timer.Stop()
	state := burstDrainState{ID: bound()}
	idleRemoved := false
	code := waitBurstProcess(done, timer.C, func() (bool, error) {
		removed, err := resumeIdleBurst(compiledNormalProfile, &state, burstDrainBackend(state.ID))
		idleRemoved = removed
		return removed, err
	}, func(stage string) {
		reportStage(stage)
		data, err := json.Marshal(struct {
			Stage       string    `json:"stage"`
			At          time.Time `json:"at"`
			Runner      string    `json:"runner"`
			IdleSeconds int       `json:"idleSeconds"`
		}{stage, time.Now().UTC(), compiledNormalProfile.runner, int(burstIdleLimit / time.Second)})
		if err == nil {
			_ = os.WriteFile(filepath.Join(".ci-runtime", "burst-idle-status.private.json"), data, 0600)
		}
	}, burstWaitPolicy{now: time.Now, after: time.After, active: active, retire: retire, stopUnadmitted: func() {
		_, _ = burstOutput(20*time.Second, docker, "exec", normalContainer, "/opt/bridge/bridge", "stop-idle")
	}})
	if code == 19 {
		return rollbackConfiguredBurst(bound())
	}
	if idleRemoved && code == burstIdleExit {
		// After the registered listener group is gone, ask the official SDK to
		// clear its own local configuration, using a RAM-only removal token.
		// No configuration/authentication file is read or manually deleted.
		if !clearBurstConfiguration() {
			reportStage("burst-idle-unconfigure-failed")
			return burstUnverifiedExit
		}
		reportStage("burst-idle-local-config-cleared")
	}
	return code
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
			if err == nil {
				return true, nil
			}
			var exit *exec.ExitError
			if errors.As(err, &exit) && exit.ExitCode() == 2 {
				return false, nil
			}
			return false, err
		},
		remove: func(id int64) error { return runnerRequest("DELETE", path(id), nil) },
		stopIdle: func() error {
			_, err := burstOutput(20*time.Second, docker, "exec", normalContainer, "/opt/bridge/bridge", "stop-idle")
			return err
		}, pause: time.Sleep,
	}
}

func clearBurstConfiguration() bool {
	token, err := burstOutput(30*time.Second, gh, "api", "--method", "POST", "repos/"+repo+"/actions/runners/remove-token", "--jq", ".token")
	if err != nil || token == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, docker, "exec", "-i", normalContainer, "/opt/bridge/bridge", "unconfigure-idle")
	hidden(c)
	c.WaitDelay = 3 * time.Second
	c.Stdin = strings.NewReader(token + "\n")
	token = ""
	return c.Run() == nil
}

func burstOutput(limit time.Duration, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()
	c := exec.CommandContext(ctx, name, args...)
	hidden(c)
	c.WaitDelay = 3 * time.Second
	return output(c)
}

func rollbackConfiguredBurst(knownID int64) int {
	// Exit 19 is emitted only after this trusted config.sh succeeded and before
	// run.sh starts. It permits a finite lookup/rollback of that unique name.
	id := knownID
	var err error
	if id <= 0 {
		id, err = configuredBurstRunnerID(compiledNormalProfile)
	}
	if errors.Is(err, errRunnerMetadataNotFound) {
		if clearBurstConfiguration() {
			return 19
		}
		return burstUnverifiedExit
	}
	if err != nil {
		return burstUnverifiedExit
	}
	state := burstDrainState{ID: id}
	deadline := time.Now().Add(10 * time.Minute)
	for {
		removed, err := resumeIdleBurst(compiledNormalProfile, &state, burstDrainBackend(id))
		if removed && err == nil {
			if clearBurstConfiguration() {
				return 19
			}
			return burstUnverifiedExit
		}
		if state.Assigned || !time.Now().Before(deadline) {
			reportStage("burst-bootstrap-rollback-unverified")
			return burstUnverifiedExit
		}
		reportStage("burst-bootstrap-rollback-retry")
		time.Sleep(30 * time.Second)
	}
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
		return 0, errRunnerMetadataNotFound
	}
	return id, nil
}
