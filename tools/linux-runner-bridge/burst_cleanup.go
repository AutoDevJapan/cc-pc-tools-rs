package main

import (
	"errors"
	"fmt"
	"time"
)

type burstCleanupOps struct {
	lookup      func() (int64, error)
	drain       func(*burstDrainState) (bool, error)
	waitStopped func() error
	clear       func() error
}

type burstRetryPolicy struct {
	now    func() time.Time
	pause  func(time.Duration)
	record func(string)
	save   func(*burstDrainState) error
}

// One retained transaction covers discovery, remote withdrawal, kernel drain,
// and SDK cleanup. Returning from the child does not commit SDK cleanup.
func resumeBurstCleanup(s *burstDrainState, d burstCleanupOps) (bool, error) {
	if s.ConfigCleared {
		return true, nil
	}
	if s.ID == 0 && !s.RegistrationRemoved {
		id, err := d.lookup()
		if errors.Is(err, errOwnedRunnerAbsent) {
			s.RegistrationRemoved, s.LabelsWithdrawn = true, true
		} else if err != nil {
			return false, err
		} else if id <= 0 {
			return false, errors.New("cleanup lookup returned an invalid identity")
		} else {
			s.ID = id
		}
	}
	if !s.ListenerStopped {
		stopped, err := d.drain(s)
		if err != nil || !stopped {
			return false, err
		}
		s.ListenerStopped = true
	}
	if !s.ProcessDrained {
		if err := d.waitStopped(); err != nil {
			return false, err
		}
		s.ProcessDrained = true
	}
	if err := d.clear(); err != nil {
		return false, err
	}
	s.ConfigCleared = true
	return true, nil
}

func retryBurstCleanup(s *burstDrainState, d burstCleanupOps, p burstRetryPolicy, deadline time.Time) (bool, error) {
	for {
		ok, err := resumeBurstCleanup(s, d)
		if p.save != nil {
			if saveErr := p.save(s); saveErr != nil {
				ok, err = false, saveErr
			}
		}
		if ok || err == nil {
			return ok, err // A confirmed assignment is handled by the caller.
		}
		remaining := deadline.Sub(p.now())
		if remaining <= 0 {
			return false, fmt.Errorf("finite cleanup window ended: %w", err)
		}
		p.record("burst-cleanup-retry: " + err.Error())
		p.pause(min(30*time.Second, remaining))
	}
}
