package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const burstIdleLimit = 2 * time.Minute

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
	workerIdle func() bool
	remove     func(int64) error
	stopIdle   func() error
	pause      func(time.Duration)
}

// Only the additional, owned ephemeral identity can be withdrawn. Its labels
// are all custom (--no-default-labels), so zero labels prevents new assignment.
// A job already assigned at the boundary is allowed to finish normally.
func drainIdleBurst(p normalProfile, boundID int64, d burstDrainOps) (bool, error) {
	if !p.ephemeral || boundID <= 0 {
		return false, errors.New("registration is not bound to this host")
	}
	r, err := d.find()
	if errors.Is(err, errRunnerMetadataNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if r.ID != boundID || r.Name != p.runner || (r.Ephemeral != nil && !*r.Ephemeral) {
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
		if current.ID != boundID || current.Name != p.runner || (current.Ephemeral != nil && !*current.Ephemeral) || len(current.Labels) != 0 {
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
	if _, err := d.read(r.ID); !errors.Is(err, errRunnerMetadataNotFound) {
		return false, errors.New("idle runner removal was not confirmed")
	}
	if d.stopIdle != nil {
		if err := d.stopIdle(); err != nil {
			return false, err
		}
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
		code := <-done
		if removed && (code == 0 || code == 130 || code == 143 || code == 255) {
			return 0
		}
		return code
	}
}

func awaitBurstProcess(done <-chan int, bound func() int64) int {
	timer := time.NewTimer(burstIdleLimit)
	defer timer.Stop()
	path := func(id int64) string { return "repos/" + repo + "/actions/runners/" + strconv.FormatInt(id, 10) }
	idleRemoved := false
	code := waitBurstProcess(done, timer.C, func() (bool, error) {
		id := bound()
		if id <= 0 {
			return false, errors.New("bootstrap registration was not bound")
		}
		removed, err := drainIdleBurst(compiledNormalProfile, id, burstDrainOps{
			find: func() (burstRunner, error) {
				var r burstRunner
				err := runnerAPI(path(id), &r)
				return r, err
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
			stopIdle: func() error {
				_, err := output(command(docker, "exec", normalContainer, "/opt/bridge/bridge", "stop-idle"))
				return err
			},
			pause: time.Sleep,
		})
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
	})
	if idleRemoved && code == 0 {
		// After the registered listener group is gone, ask the official SDK to
		// clear its own local configuration, using a RAM-only removal token.
		// No configuration/authentication file is read or manually deleted.
		token, err := output(command(gh, "api", "--method", "POST", "repos/"+repo+"/actions/runners/remove-token", "--jq", ".token"))
		if err != nil || token == "" {
			reportStage("burst-idle-unconfigure-token-failed")
			return 1
		}
		c := command(docker, "exec", "-i", normalContainer, "/opt/bridge/bridge", "unconfigure-idle")
		c.Stdin = strings.NewReader(token + "\n")
		token = ""
		if c.Run() != nil {
			reportStage("burst-idle-unconfigure-failed")
			return 1
		}
		reportStage("burst-idle-local-config-cleared")
	}
	return code
}

// This notification is made synchronously after config.sh succeeds and before
// run.sh can receive any job. A pre-existing .runner returns 96 before it.
func notifyBurstRegistration() bool {
	key, err := os.ReadFile("/tmp/bridge-key")
	if err != nil {
		return false
	}
	body, _ := json.Marshal(request{Repository: repo})
	req, err := http.NewRequest("POST", bridgeURL("normal", "/registered"), bytes.NewReader(body))
	if err != nil {
		return false
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(key)))
	client := http.Client{Timeout: 30 * time.Second}
	response, err := client.Do(req)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	return response.StatusCode == 204
}

func configuredBurstRunnerID(p normalProfile) (int64, error) {
	var response struct {
		Total   int           `json:"total_count"`
		Runners []burstRunner `json:"runners"`
	}
	if err := runnerAPI("repos/"+repo+"/actions/runners?per_page=100", &response); err != nil {
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
