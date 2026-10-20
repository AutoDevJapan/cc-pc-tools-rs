package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

const burstIdleLimit = 2 * time.Minute

type burstRunner struct {
	ID        int64                         `json:"id"`
	Name      string                        `json:"name"`
	Busy      bool                          `json:"busy"`
	Ephemeral bool                          `json:"ephemeral"`
	Labels    []struct{ Name, Type string } `json:"labels"`
}

type burstDrainOps struct {
	find       func() (burstRunner, error)
	read       func(int64) (burstRunner, error)
	withdraw   func(int64) error
	workerIdle func() bool
	remove     func(int64) error
	pause      func(time.Duration)
}

// Only the additional, owned ephemeral identity can be withdrawn. Its labels
// are all custom (--no-default-labels), so zero labels prevents new assignment.
// A job already assigned at the boundary is allowed to finish normally.
func drainIdleBurst(p normalProfile, d burstDrainOps) (bool, error) {
	r, err := d.find()
	if errors.Is(err, errRunnerMetadataNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !p.ephemeral || r.ID <= 0 || r.Name != p.runner || !r.Ephemeral {
		return false, errors.New("idle drain runner identity rejected")
	}
	if r.Busy {
		return false, nil
	}
	for _, label := range r.Labels {
		if label.Type != "custom" {
			return false, errors.New("idle drain cannot withdraw default labels")
		}
	}
	if err := d.withdraw(r.ID); err != nil {
		return false, err
	}
	for sample := 0; sample < 3; sample++ {
		current, err := d.read(r.ID)
		if errors.Is(err, errRunnerMetadataNotFound) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if current.ID != r.ID || current.Name != p.runner || !current.Ephemeral || len(current.Labels) != 0 {
			return false, errors.New("withdrawn runner identity/labels not confirmed")
		}
		if current.Busy || !d.workerIdle() {
			return false, nil
		}
		d.pause(time.Second)
	}
	if err := d.remove(r.ID); err != nil {
		return false, err
	}
	return true, nil
}

func waitBurstProcess(done <-chan int, expired <-chan time.Time, idle func() (bool, error), record func(string)) int {
	select {
	case code := <-done:
		return code
	case <-expired:
		removed, err := idle()
		if err != nil {
			record("burst-idle-drain-unverified")
		} else if removed {
			record("burst-idle-unregistered")
		} else {
			record("burst-retaining-session")
		}
		// No force kill: an assigned job retains its original timeout. When the
		// registration was idle and removed, the official listener handles 404,
		// deletes local config and exits through the usual process-group drain.
		return <-done
	}
}

func awaitBurstProcess(done <-chan int) int {
	timer := time.NewTimer(burstIdleLimit)
	defer timer.Stop()
	path := func(id int64) string { return "repos/" + repo + "/actions/runners/" + strconv.FormatInt(id, 10) }
	return waitBurstProcess(done, timer.C, func() (bool, error) {
		return drainIdleBurst(compiledNormalProfile, burstDrainOps{
			find: func() (burstRunner, error) {
				var response struct {
					Total   int           `json:"total_count"`
					Runners []burstRunner `json:"runners"`
				}
				if err := runnerAPI("repos/"+repo+"/actions/runners?per_page=100", &response); err != nil {
					return burstRunner{}, err
				}
				if response.Total > 100 {
					return burstRunner{}, errors.New("runner inventory exceeds bound")
				}
				for _, r := range response.Runners {
					if r.Name == compiledNormalProfile.runner {
						return r, nil
					}
				}
				return burstRunner{}, errRunnerMetadataNotFound
			},
			read: func(id int64) (burstRunner, error) { var r burstRunner; err := runnerAPI(path(id), &r); return r, err },
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
			workerIdle: func() bool {
				_, err := output(command(docker, "exec", normalContainer, "/opt/bridge/bridge", "idle-state"))
				return err == nil
			},
			remove: func(id int64) error { return runnerRequest("DELETE", path(id), nil) },
			pause:  time.Sleep,
		})
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
	})
}
