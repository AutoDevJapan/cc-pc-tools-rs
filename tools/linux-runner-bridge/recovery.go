package main

import (
	"errors"
	"net"
	"path/filepath"
	"time"
)

type burstRecoveryOps struct {
	inventory     func() (int64, error)
	configuration func() (bool, error)
	drain         func(*burstDrainState) (bool, error)
	quiescent     func() error
	clear         func() error
}

func recoverStoredBurst(j burstJournal, d burstRecoveryOps, p burstRetryPolicy) (bool, error) {
	state, pending, err := j.pending()
	if err != nil {
		return false, err
	}
	if !pending {
		absent, err := d.configuration()
		if err != nil {
			return false, err
		}
		if !absent {
			return false, errors.New("configured slot has no unfinished bound recovery receipt")
		}
		if err := j.maintain(); err != nil {
			return false, err
		}
		if err := j.capacity(); err != nil {
			return false, err
		}
		return true, nil
	}
	id, err := d.inventory()
	if err != nil && !errors.Is(err, errOwnedRunnerAbsent) {
		return false, err
	}
	if err == nil && id != state.ID {
		return false, errors.New("recovery refuses a different same-name registration")
	}
	if state.ProcessDrained {
		// A recorded exit never substitutes for a fresh supervisor/kernel check.
		if err := d.quiescent(); err != nil {
			return false, err
		}
	}
	p.save = j.save
	return retryBurstCleanup(&state, burstCleanupOps{drain: d.drain, waitStopped: d.quiescent, clear: d.clear}, p, p.now().Add(10*time.Minute))
}

func runtimeBurstJournal() (burstJournal, error) {
	if err := journalDirectory(".ci-runtime"); err != nil {
		return burstJournal{}, err
	}
	id, err := burstOutput(15*time.Second, docker, "inspect", "--format", "{{.Id}}", normalContainer)
	if err != nil {
		return burstJournal{}, errors.New("owned container identity unavailable")
	}
	j := burstJournal{root: filepath.Join(".ci-runtime", "burst-cleanup"), profile: compiledNormalProfile, containerID: id}
	if _, err := j.binding(1); err != nil {
		return burstJournal{}, err
	}
	return j, nil
}

func probeBurstConfiguration() (bool, error) {
	_, err := burstOutput(15*time.Second, docker, "exec", normalContainer, "/opt/bridge/bridge", "configuration-absent")
	if err == nil {
		return true, nil
	}
	if exitCode(err) == burstConfigurationPresentExit {
		return false, nil
	}
	return false, errors.New("owned SDK/kernel state unavailable")
}

func awaitRecoveredKernel() error {
	deadline := time.Now().Add(3 * time.Minute)
	for {
		if _, err := probeBurstConfiguration(); err == nil {
			return nil
		}
		if !time.Now().Before(deadline) {
			return errors.New("recovery kernel drain remains unverified")
		}
		time.Sleep(time.Second)
	}
}

func prepareBurstRecovery() (bool, error) {
	if err := verifyNormalContainer(); err != nil {
		return false, err
	}
	j, err := runtimeBurstJournal()
	if err != nil {
		return false, err
	}
	// A live original host keeps both its RPC key and its accepted job. Recovery
	// cannot overlap it, and no secret is copied from the old process/container.
	guard, err := net.Listen("tcp", compiledNormalProfile.listen)
	if err != nil {
		return false, nil
	}
	defer guard.Close()
	return recoverStoredBurst(j, burstRecoveryOps{
		inventory:     func() (int64, error) { return configuredBurstRunnerID(compiledNormalProfile) },
		configuration: probeBurstConfiguration,
		drain: func(s *burstDrainState) (bool, error) {
			return drainOwnedBurst(compiledNormalProfile, s, burstDrainBackend(s.ID), func() bool { return true })
		},
		quiescent: awaitRecoveredKernel,
		clear:     clearBurstConfiguration,
	}, burstRetryPolicy{now: time.Now, pause: time.Sleep, record: recordBurstLifecycle})
}
