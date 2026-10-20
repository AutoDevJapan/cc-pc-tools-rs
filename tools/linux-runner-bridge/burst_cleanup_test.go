package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCleanupRetriesLookupAndSDKWithoutRepeatingCommittedStages(t *testing.T) {
	state := burstDrainState{}
	lookup, drain, wait, clear := 0, 0, 0, 0
	ops := burstCleanupOps{
		lookup: func() (int64, error) {
			lookup++
			if lookup == 1 {
				return 0, errors.New("transient lookup")
			}
			return 49, nil
		},
		drain: func(s *burstDrainState) (bool, error) {
			drain++
			if s.ID != 49 {
				t.Fatal("unbound runner adopted")
			}
			s.ListenerStopped = true
			return true, nil
		},
		waitStopped: func() error { wait++; return nil },
		clear: func() error {
			clear++
			if clear == 1 {
				return errors.New("one lost remove-token response")
			}
			return nil
		},
	}
	now := time.Now()
	policy := burstRetryPolicy{now: func() time.Time { return now }, pause: func(d time.Duration) { now = now.Add(d) }, record: func(string) {}}
	if ok, err := retryBurstCleanup(&state, ops, policy, now.Add(10*time.Minute)); !ok || err != nil {
		t.Fatalf("recoverable cleanup stopped: %v", err)
	}
	if lookup != 2 || drain != 1 || wait != 1 || clear != 2 || !state.ConfigCleared || !state.ProcessDrained {
		t.Fatalf("committed stage repeated or cleanup omitted: %d %d %d %d %+v", lookup, drain, wait, clear, state)
	}
}

func testBurstLifecycle(t *testing.T, configure func(*burstLifecycleOps, *time.Time)) burstLifecycleOps {
	t.Helper()
	now := time.Now()
	d := burstLifecycleOps{
		now: func() time.Time { return now },
		after: func(delay time.Duration) <-chan time.Time {
			ch := make(chan time.Time, 1)
			if delay == burstIdleLimit {
				now = now.Add(delay)
				ch <- now
			}
			return ch
		},
		pause:          func(delay time.Duration) { now = now.Add(delay) },
		record:         func(string) {},
		stopUnadmitted: func() { t.Fatal("accepted/completed work was stopped") },
	}
	configure(&d, &now)
	return d
}

func TestAwaitFailedACKRetriesLookupAndSDKInTheSameWindow(t *testing.T) {
	done := make(chan int, 1)
	done <- 19
	lookup, clears := 0, 0
	d := testBurstLifecycle(t, func(d *burstLifecycleOps, _ *time.Time) {
		d.cleanup = func(s *burstDrainState) burstCleanupOps {
			return burstCleanupOps{
				lookup: func() (int64, error) {
					lookup++
					if lookup == 1 {
						return 0, errors.New("temporary API")
					}
					return 49, nil
				},
				drain: func(s *burstDrainState) (bool, error) { s.ListenerStopped = true; return true, nil },
				clear: func() error {
					clears++
					if clears == 1 {
						return errors.New("temporary SDK")
					}
					return nil
				},
			}
		}
	})
	if code := awaitBurstProcessWith(done, nil, func() int64 { return 0 }, func() bool { return false }, func() bool { return true }, d); code != 19 || lookup != 2 || clears != 2 {
		t.Fatalf("failed ACK did not finish its owned rollback: %d %d %d", code, lookup, clears)
	}
}

func TestAwaitUnprovenSetupNeverAdoptsAnExistingSameNameRunner(t *testing.T) {
	for _, code := range []int{96, 39, burstSetupUnverifiedExit} {
		done := make(chan int, 1)
		done <- code
		d := testBurstLifecycle(t, func(d *burstLifecycleOps, _ *time.Time) {
			d.cleanup = func(*burstDrainState) burstCleanupOps {
				t.Fatal("unowned registration adopted")
				return burstCleanupOps{}
			}
		})
		if got := awaitBurstProcessWith(done, nil, func() int64 { return 0 }, func() bool { return false }, func() bool { return true }, d); got != code {
			t.Fatal("unverified setup failure was hidden")
		}
	}
}

func TestAwaitIdleSDKRetryRetainsTheConsumedChildCompletion(t *testing.T) {
	done := make(chan int, 1)
	registered := make(chan struct{}, 1)
	registered <- struct{}{}
	drains, clears := 0, 0
	d := testBurstLifecycle(t, func(d *burstLifecycleOps, _ *time.Time) {
		d.cleanup = func(*burstDrainState) burstCleanupOps {
			return burstCleanupOps{
				drain: func(s *burstDrainState) (bool, error) {
					drains++
					s.ListenerStopped = true
					done <- 255
					return true, nil
				},
				clear: func() error {
					clears++
					if clears == 1 {
						return errors.New("SDK response lost")
					}
					return nil
				},
			}
		}
	})
	if code := awaitBurstProcessWith(done, registered, func() int64 { return 49 }, func() bool { return false }, func() bool { return true }, d); code != burstIdleExit || drains != 1 || clears != 2 {
		t.Fatalf("completed group was consumed again or SDK retry skipped: %d %d %d", code, drains, clears)
	}
}

func TestAwaitIdleAssignmentPreservesItsJobResult(t *testing.T) {
	done := make(chan int, 1)
	registered := make(chan struct{}, 1)
	registered <- struct{}{}
	drains := 0
	d := testBurstLifecycle(t, func(d *burstLifecycleOps, _ *time.Time) {
		d.cleanup = func(*burstDrainState) burstCleanupOps {
			return burstCleanupOps{
				drain: func(s *burstDrainState) (bool, error) {
					drains++
					if !s.ProcessDrained {
						s.Assigned = true
						done <- 7
						return false, nil
					}
					s.ListenerStopped = true
					return true, nil
				},
				clear: func() error { return nil },
			}
		}
	})
	if code := awaitBurstProcessWith(done, registered, func() int64 { return 49 }, func() bool { return false }, func() bool { return true }, d); code != 7 || drains != 2 {
		t.Fatal("assignment was cancelled or its result was replaced")
	}
}

func TestAwaitCleanupFailureHasFiniteRetirement(t *testing.T) {
	registered := make(chan struct{}, 1)
	registered <- struct{}{}
	retired, stopped, attempts := 0, 0, 0
	d := testBurstLifecycle(t, func(d *burstLifecycleOps, _ *time.Time) {
		d.stopUnadmitted = func() { stopped++ }
		d.cleanup = func(*burstDrainState) burstCleanupOps {
			return burstCleanupOps{drain: func(*burstDrainState) (bool, error) { attempts++; return false, errors.New("metadata unavailable") }}
		}
	})
	code := awaitBurstProcessWith(make(chan int), registered, func() int64 { return 49 }, func() bool { return false }, func() bool { retired++; return true }, d)
	if code != burstUnverifiedExit || attempts != 21 || retired != 1 || stopped != 1 {
		t.Fatalf("cleanup deadline escaped: %d %d %d %d", code, attempts, retired, stopped)
	}
}

func TestSDKAbsenceIsAnOpaqueIdempotentProbe(t *testing.T) {
	root := t.TempDir()
	if absent, err := burstConfigurationAbsent(root); err != nil || !absent {
		t.Fatal("empty SDK not accepted")
	}
	file := filepath.Join(root, ".credentials")
	if err := os.WriteFile(file, []byte("invalid-opaque-fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	if absent, err := burstConfigurationAbsent(root); err != nil || absent {
		t.Fatal("SDK contents were decoded or a present file was ignored")
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if absent, err := burstConfigurationAbsent(root); err != nil || !absent {
		t.Fatal("completed SDK removal was not idempotent")
	}
	if err := os.Mkdir(filepath.Join(root, ".runner"), 0700); err != nil {
		t.Fatal(err)
	}
	if absent, err := burstConfigurationAbsent(root); err == nil || absent {
		t.Fatal("non-file configuration was accepted")
	}
}

func TestPanicExitIsNotReportedAsAWorkerAssignment(t *testing.T) {
	probeErr := errors.New("probe process failed")
	if idle, err := burstIdleProbeResult(2, probeErr); idle || err == nil {
		t.Fatal("Go panic was classified as assigned work")
	}
	if idle, err := burstIdleProbeResult(burstWorkerActiveExit, probeErr); idle || err != nil {
		t.Fatal("Worker presence was lost")
	}
}

func TestSetupDeadlineRetiresThenCleansOnlyProvenConfiguration(t *testing.T) {
	done := make(chan int, 1)
	retired, cleared := 0, 0
	d := testBurstLifecycle(t, func(d *burstLifecycleOps, now *time.Time) {
		d.after = func(delay time.Duration) <-chan time.Time {
			ch := make(chan time.Time, 1)
			if delay == 6*time.Minute {
				*now = now.Add(delay)
				ch <- *now
			}
			return ch
		}
		d.cleanup = func(*burstDrainState) burstCleanupOps {
			return burstCleanupOps{
				lookup: func() (int64, error) { return 49, nil },
				drain:  func(s *burstDrainState) (bool, error) { s.ListenerStopped = true; return true, nil },
				clear:  func() error { cleared++; return nil },
			}
		}
	})
	code := awaitBurstProcessWith(done, nil, func() int64 { return 0 }, func() bool { return false }, func() bool { retired++; done <- 19; return true }, d)
	if code != burstUnverifiedExit || retired != 1 || cleared != 1 {
		t.Fatal("setup deadline lost its finite owned rollback")
	}
}

func TestSDKFailureConsumesTheOriginalCleanupWindow(t *testing.T) {
	state := burstDrainState{ID: 49, ListenerStopped: true, ProcessDrained: true}
	attempts := 0
	now := time.Now()
	start := now
	ops := burstCleanupOps{clear: func() error { attempts++; return errors.New("SDK unavailable") }}
	policy := burstRetryPolicy{now: func() time.Time { return now }, pause: func(d time.Duration) { now = now.Add(d) }, record: func(string) {}}
	if ok, err := retryBurstCleanup(&state, ops, policy, start.Add(10*time.Minute)); ok || err == nil || state.ConfigCleared {
		t.Fatal("unverified local SDK cleanup became successful")
	}
	if attempts != 21 || now.Sub(start) != 10*time.Minute {
		t.Fatalf("SDK stage reset or escaped the finite window: %d %v", attempts, now.Sub(start))
	}
}

func TestMissingRegistrationStillWaitsForDrainAndIdempotentSDKCleanup(t *testing.T) {
	state := burstDrainState{}
	stopped, cleared := false, false
	ops := burstCleanupOps{
		lookup: func() (int64, error) { return 0, errOwnedRunnerAbsent },
		drain: func(s *burstDrainState) (bool, error) {
			if !s.RegistrationRemoved {
				t.Fatal("missing inventory was not retained")
			}
			s.ListenerStopped = true
			return true, nil
		},
		waitStopped: func() error { stopped = true; return nil },
		clear: func() error {
			if !stopped {
				t.Fatal("SDK removed before group drain")
			}
			cleared = true
			return nil
		},
	}
	if ok, err := resumeBurstCleanup(&state, ops); !ok || err != nil || !cleared || !state.ConfigCleared {
		t.Fatal("missing registration prevented owned local cleanup")
	}
	if ok, err := resumeBurstCleanup(&state, burstCleanupOps{}); !ok || err != nil {
		t.Fatal("completed cleanup was not idempotent")
	}
}
