package main

import (
	"errors"
	"testing"
	"time"
)

func TestIdleExpiryWithdrawsOnlyAnIdleOwnedEphemeralRunner(t *testing.T) {
	p, _ := normalProfileFor("burst-3122")
	for _, fault := range []string{"", "already-busy", "assignment-race", "worker", "labels", "identity", "api"} {
		t.Run(fault, func(t *testing.T) {
			r := burstRunner{ID: 43, Name: p.runner, Ephemeral: true}
			r.Labels = append(r.Labels, struct{ Name, Type string }{normalLabel, "custom"})
			if fault == "already-busy" {
				r.Busy = true
			}
			if fault == "identity" {
				r.Name = "another-runner"
			}
			withdrawn, removed := 0, 0
			d := burstDrainOps{
				find: func() (burstRunner, error) { return r, nil },
				withdraw: func(id int64) error {
					if id != 43 {
						t.Fatal("other runner changed")
					}
					withdrawn++
					return nil
				},
				read: func(id int64) (burstRunner, error) {
					if fault == "api" {
						return burstRunner{}, errors.New("unavailable")
					}
					current := r
					current.Labels = nil
					if fault == "assignment-race" {
						current.Busy = true
					}
					if fault == "labels" {
						current.Labels = r.Labels
					}
					return current, nil
				},
				workerIdle: func() bool { return fault != "worker" },
				remove: func(id int64) error {
					if id != 43 {
						t.Fatal("other runner removed")
					}
					removed++
					return nil
				},
				pause: func(time.Duration) {},
			}
			stopped, err := drainIdleBurst(p, d)
			if fault == "" {
				if err != nil || !stopped || withdrawn != 1 || removed != 1 {
					t.Fatal("idle registration did not expire")
				}
			}
			if fault != "" && (stopped || removed != 0) {
				t.Fatal("unknown or active work was unregistered")
			}
			if (fault == "already-busy" || fault == "identity") && withdrawn != 0 {
				t.Fatal("active/other runner labels were changed")
			}
		})
	}
}

func TestBurstIdleDeadlineWaitsForAnAssignedJobWithoutCancellingIt(t *testing.T) {
	done := make(chan int, 1)
	expired := make(chan time.Time, 1)
	expired <- time.Now()
	stages := []string{}
	code := waitBurstProcess(done, expired, func() (bool, error) { done <- 7; return false, nil }, func(s string) { stages = append(stages, s) })
	if code != 7 || len(stages) != 1 || stages[0] != "burst-retaining-session" {
		t.Fatal("assigned job result changed")
	}
	done = make(chan int, 1)
	done <- 0
	if got := waitBurstProcess(done, make(chan time.Time), func() (bool, error) { t.Fatal("completed host was mutated"); return false, nil }, func(string) {}); got != 0 {
		t.Fatal("host success lost")
	}
}
