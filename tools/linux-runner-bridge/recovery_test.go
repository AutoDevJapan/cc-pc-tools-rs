package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRecoveryClearsRecordedSDKFailureAndPermitsTheNextSupply(t *testing.T) {
	p, _ := normalProfileFor("burst-3122")
	j := burstJournal{root: t.TempDir(), profile: p, containerID: strings.Repeat("a", 64)}
	s := burstDrainState{ID: 55, LabelsWithdrawn: true, RegistrationRemoved: true, ListenerStopped: true, ProcessDrained: true}
	if err := j.save(&s); err != nil {
		t.Fatal(err)
	}
	checks, clears := 0, 0
	d := burstRecoveryOps{
		inventory:     func() (int64, error) { return 0, errOwnedRunnerAbsent },
		configuration: func() (bool, error) { return clears > 0, nil },
		drain:         func(*burstDrainState) (bool, error) { t.Fatal("committed remote stages repeated"); return false, nil },
		quiescent:     func() error { checks++; return nil },
		clear:         func() error { clears++; return nil },
	}
	pcy := burstRetryPolicy{now: time.Now, pause: func(time.Duration) { t.Fatal("healthy recovery waited") }, record: func(string) {}}
	if ready, err := recoverStoredBurst(j, d, pcy); !ready || err != nil || checks != 1 || clears != 1 {
		t.Fatal("restart failed to finish verified owned cleanup", err)
	}
	d.inventory = func() (int64, error) { t.Fatal("completed cleanup queried old inventory"); return 0, nil }
	if ready, err := recoverStoredBurst(j, d, pcy); !ready || err != nil || clears != 1 {
		t.Fatal("next normal admission was blocked", err)
	}
}

func TestRecoveryRejectsDifferentSameNameAndUnknownInventory(t *testing.T) {
	for _, fault := range []string{"other-id", "inventory-404", "other-container"} {
		t.Run(fault, func(t *testing.T) {
			p, _ := normalProfileFor("burst-3122")
			j := burstJournal{root: t.TempDir(), profile: p, containerID: strings.Repeat("a", 64)}
			if err := j.save(&burstDrainState{ID: 55}); err != nil {
				t.Fatal(err)
			}
			if fault == "other-container" {
				j.containerID = strings.Repeat("b", 64)
			}
			d := burstRecoveryOps{inventory: func() (int64, error) {
				if fault == "inventory-404" {
					return 0, errRunnerMetadataNotFound
				}
				return 56, nil
			},
				drain: func(*burstDrainState) (bool, error) { t.Fatal("unknown registration changed"); return false, nil },
				clear: func() error { t.Fatal("unknown SDK configuration removed"); return nil }}
			if ready, err := recoverStoredBurst(j, d, burstRetryPolicy{}); ready || err == nil {
				t.Fatal("unbound ownership was accepted")
			}
		})
	}
}

func TestRecoveryDefersAnAssignedJobInsteadOfReusingItsSlot(t *testing.T) {
	p, _ := normalProfileFor("burst-3122")
	j := burstJournal{root: t.TempDir(), profile: p, containerID: strings.Repeat("a", 64)}
	if err := j.save(&burstDrainState{ID: 55}); err != nil {
		t.Fatal(err)
	}
	d := burstRecoveryOps{inventory: func() (int64, error) { return 55, nil },
		drain: func(*burstDrainState) (bool, error) { return false, nil },
		clear: func() error { t.Fatal("active SDK cleared"); return nil }}
	pcy := burstRetryPolicy{now: time.Now, record: func(string) {}, pause: func(time.Duration) { t.Fatal("active job was polled in a wait loop") }}
	if ready, err := recoverStoredBurst(j, d, pcy); ready || err != nil {
		t.Fatal("assigned work was not deferred", err)
	}
}

func TestProductionDrainConvertsStaleBusyAfterProcessCompletion(t *testing.T) {
	p, _ := normalProfileFor("burst-3122")
	s := burstDrainState{ID: 55, ProcessDrained: true}
	d := burstDrainOps{find: func() (burstRunner, error) { return burstRunner{ID: 55, Name: p.runner, Busy: true}, nil }}
	if ok, err := drainOwnedBurst(p, &s, d, func() bool { t.Fatal("completed historical admission was retired again"); return false }); ok || err == nil {
		t.Fatal("nil-channel wait remained reachable")
	}
}

func TestRegistrationCannotAcknowledgeBeforeItsRecoveryBindingIsDurable(t *testing.T) {
	p, _ := normalProfileFor("burst-3122")
	h := host{key: "fixture", mode: "normal", profile: p, registered: make(chan struct{}, 1), lookupRegistration: func() (int64, error) { return 55, nil }, persistRegistration: func(int64) error { return errors.New("disk unavailable") }}
	body, _ := json.Marshal(request{Repository: repo})
	r := httptest.NewRequest("POST", "http://bridge/registered", bytes.NewReader(body))
	r.Header.Set("Authorization", "Bearer fixture")
	w := httptest.NewRecorder()
	h.serve(w, r)
	if w.Code != 503 || h.boundRunnerID.Load() != 0 || len(h.registered) != 0 {
		t.Fatal("SDK could start without durable ownership")
	}
}
