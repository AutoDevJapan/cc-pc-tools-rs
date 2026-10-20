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

func TestIdleExpiryWithdrawsOnlyAnIdleOwnedEphemeralRunner(t *testing.T) {
	p, _ := normalProfileFor("burst-3122")
	for _, fault := range []string{"", "already-busy", "assignment-race", "worker", "probe-error", "retire-race", "labels", "identity", "api", "explicit-resident", "unbound", "other-id"} {
		t.Run(fault, func(t *testing.T) {
			r := burstRunner{ID: 43, Name: p.runner}
			r.Labels = append(r.Labels, struct{ Name, Type string }{normalLabel, "custom"})
			if fault == "already-busy" {
				r.Busy = true
			}
			if fault == "identity" {
				r.Name = "another-runner"
			}
			if fault == "explicit-resident" {
				resident := false
				r.Ephemeral = &resident
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
					if removed > 0 {
						return burstRunner{}, errRunnerMetadataNotFound
					}
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
				workerIdle: func() (bool, error) {
					if fault == "probe-error" {
						return false, errors.New("probe unavailable")
					}
					return fault != "worker", nil
				},
				retire: func() bool { return fault != "retire-race" },
				remove: func(id int64) error {
					if id != 43 {
						t.Fatal("other runner removed")
					}
					removed++
					return nil
				},
				pause: func(time.Duration) {},
			}
			bound := int64(43)
			if fault == "unbound" {
				bound = 0
			}
			if fault == "other-id" {
				bound = 99
			}
			stopped, err := drainIdleBurst(p, bound, d)
			if fault == "" {
				if err != nil || !stopped || withdrawn != 1 || removed != 1 {
					t.Fatal("idle registration did not expire")
				}
			}
			if fault != "" && (stopped || removed != 0) {
				t.Fatal("unknown or active work was unregistered")
			}
			if (fault == "already-busy" || fault == "identity" || fault == "unbound" || fault == "other-id" || fault == "explicit-resident") && withdrawn != 0 {
				t.Fatal("active/other runner labels were changed")
			}
		})
	}
}

func TestRegistrationLookupBindsOnlyItsUniqueValidName(t *testing.T) {
	p, _ := normalProfileFor("burst-3122")
	for _, tc := range []struct {
		body  string
		valid bool
	}{
		{`{"total_count":1,"runners":[{"id":44,"name":"cc-pc-tools-linux-3122"}]}`, true},
		{`{"total_count":1,"runners":[{"id":44,"name":"cc-pc-tools-linux-3122","ephemeral":false}]}`, false},
		{`{"total_count":0,"runners":[]}`, false},
		{`{"total_count":2,"runners":[{"id":44,"name":"cc-pc-tools-linux-3122"},{"id":45,"name":"cc-pc-tools-linux-3122"}]}`, false},
		{`{"total_count":101,"runners":[]}`, false},
	} {
		id, err := configuredBurstRunnerIDWith(p, func(_ string, out any) error { return json.Unmarshal([]byte(tc.body), out) })
		if (err == nil && id == 44) != tc.valid {
			t.Fatal("registration lookup accepted ambiguous/unknown identity")
		}
	}
}

func TestRetiredHostCannotAuthorizeARealJob(t *testing.T) {
	p, _ := normalProfileFor("burst-3122")
	sha := strings.Repeat("a", 40)
	h := host{key: "fixture-key", mode: "normal", profile: p, getHead: func(string) (string, error) { return sha, nil }}
	q := request{Repository: repo, Revision: sha, HeadSHA: sha, HeadRef: "candidate", HeadRepository: repo, BaseRepository: repo, BaseRef: "main", Ref: "refs/pull/1/merge", Event: "pull_request"}
	call := func() int {
		body, _ := json.Marshal(q)
		req := httptest.NewRequest("POST", "http://bridge/authorize", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer fixture-key")
		out := httptest.NewRecorder()
		h.serve(out, req)
		return out.Code
	}
	h.jobAdmission.Store(2)
	if call() != 503 {
		t.Fatal("retired host accepted source execution")
	}
	h.jobAdmission.Store(0)
	if call() != 204 || h.jobAdmission.Load() != 1 {
		t.Fatal("valid job did not retain its live RPC host")
	}
	if h.jobAdmission.CompareAndSwap(0, 2) {
		t.Fatal("accepted job could be retired during cleanup failure")
	}
}

func TestCompletedRegistrationBindsOnceBeforeJobAdmission(t *testing.T) {
	p, _ := normalProfileFor("burst-3122")
	lookups := 0
	h := host{key: "fixture-key", mode: "normal", profile: p, lookupRegistration: func() (int64, error) { lookups++; return int64(43 + lookups), nil }}
	for i := 0; i < 2; i++ {
		body, _ := json.Marshal(request{Repository: repo})
		req := httptest.NewRequest("POST", "http://bridge/registered", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer fixture-key")
		out := httptest.NewRecorder()
		h.serve(out, req)
		if out.Code != 204 || h.boundRunnerID.Load() != 44 {
			t.Fatal("completed bootstrap was not bound to its original ID")
		}
	}
	if lookups != 1 {
		t.Fatal("later notification rebound the runner")
	}
	primary, _ := normalProfileFor("primary")
	h.profile = primary
	h.boundRunnerID.Store(0)
	body, _ := json.Marshal(request{Repository: repo})
	req := httptest.NewRequest("POST", "http://bridge/registered", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer fixture-key")
	out := httptest.NewRecorder()
	h.serve(out, req)
	if out.Code != 403 || h.boundRunnerID.Load() != 0 {
		t.Fatal("primary adopted a burst registration")
	}
}

func TestIdleDrainResumesItsCommittedStage(t *testing.T) {
	p, _ := normalProfileFor("burst-3122")
	state := burstDrainState{ID: 43}
	withdraws, removals, stops, reads := 0, 0, 0, 0
	missing := false
	r := burstRunner{ID: 43, Name: p.runner}
	d := burstDrainOps{
		find:     func() (burstRunner, error) { return r, nil },
		withdraw: func(int64) error { withdraws++; return nil },
		read: func(int64) (burstRunner, error) {
			reads++
			if reads == 1 {
				return r, errors.New("transient after withdrawal")
			}
			if missing {
				return burstRunner{}, errRunnerMetadataNotFound
			}
			return r, nil
		},
		workerIdle: func() (bool, error) { return true, nil },
		remove:     func(int64) error { removals++; missing = true; return nil },
		stopIdle: func() error {
			stops++
			if stops == 1 {
				return errors.New("transient after removal")
			}
			return nil
		}, pause: func(time.Duration) {},
	}
	if ok, err := resumeIdleBurst(p, &state, d); ok || err == nil || !state.LabelsWithdrawn {
		t.Fatal("withdrawal stage was not retained")
	}
	if ok, err := resumeIdleBurst(p, &state, d); ok || err == nil || !state.RegistrationRemoved {
		t.Fatal("removal stage was not retained")
	}
	if ok, err := resumeIdleBurst(p, &state, d); !ok || err != nil || !state.ListenerStopped {
		t.Fatal("removed registration was not resumed at listener stop")
	}
	if withdraws != 1 || removals != 1 || stops != 2 {
		t.Fatal("retry repeated a committed destructive stage")
	}
}
