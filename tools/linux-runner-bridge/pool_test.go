package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestQueueCountsOnlyUnassignedCurrentSameRepoLinuxJobs(t *testing.T) {
	now := time.Date(2026, 10, 20, 14, 13, 52, 0, time.UTC)
	head := strings.Repeat("a", 40)
	old := strings.Repeat("b", 40)
	api := func(path string, out any) error {
		var body string
		switch {
		case strings.Contains(path, "/actions/runs?status="):
			body = fmt.Sprintf(`{"total_count":3,"workflow_runs":[{"id":1,"head_sha":%q,"head_branch":"current","event":"pull_request","head_repository":{"full_name":%q}},{"id":2,"head_sha":%q,"head_branch":"current","event":"pull_request","head_repository":{"full_name":%q}},{"id":3,"head_sha":%q,"head_branch":"current","event":"pull_request","head_repository":{"full_name":"external/fork"}}]}`, head, repo, old, repo, head)
		case strings.Contains(path, "/git/ref/heads/current"):
			body = fmt.Sprintf(`{"object":{"sha":%q}}`, head)
		case strings.Contains(path, "/actions/runs/1/jobs?"):
			body = `{"total_count":4,"jobs":[{"id":10,"status":"queued","runner_id":0,"created_at":"2026-10-20T12:45:46Z","labels":["self-hosted","Linux","X64","cc-pc-tools-linux"]},{"id":11,"status":"completed","runner_id":27,"labels":["self-hosted","Linux","X64","cc-pc-tools-linux"]},{"id":12,"status":"queued","runner_id":0,"labels":["self-hosted","Windows","X64","cc-pc-tools-windows"]},{"id":13,"status":"queued","runner_id":27,"labels":["self-hosted","Linux","X64","cc-pc-tools-linux"]}]}`
		default:
			t.Fatalf("unexpected source request: %s", path)
		}
		return json.Unmarshal([]byte(body), out)
	}
	q, err := currentQueue(api, now)
	if err != nil || q.Queued != 1 || q.OldestSecs != 5286 {
		t.Fatalf("queue and UTC age differ: %+v %v", q, err)
	}
}

func TestBurstPoolResuppliesAfterEachRealHostExit(t *testing.T) {
	now := time.Now()
	remaining, active, started, admitted := 3, 0, 0, 0
	var stages []string
	code := supplyBurstPool(burstPoolDeps{
		maxHosts: burstPoolLimit,
		now:      func() time.Time { return now },
		queue: func() (queueSnapshot, error) {
			if active != 0 {
				t.Fatal("queried next supply before the prior host drained")
			}
			return queueSnapshot{Queued: remaining}, nil
		},
		capacity: func() error { admitted++; return nil },
		run: func() int {
			active++
			started++
			remaining--
			now = now.Add(time.Minute)
			active--
			return 0
		},
		record: func(stage string, _ int, _ queueSnapshot) { stages = append(stages, stage) },
	})
	if code != 0 || started != 3 || admitted != 3 || remaining != 0 || stages[len(stages)-1] != "queue-drained" {
		t.Fatalf("finite resupply did not drain the queue: code=%d started=%d admitted=%d remaining=%d stages=%v", code, started, admitted, remaining, stages)
	}
}

func TestQueueDeletedBranchDoesNotStrandUnrelatedWork(t *testing.T) {
	for _, missing := range []bool{true, false} {
		api := func(path string, out any) error {
			var body string
			switch {
			case strings.Contains(path, "/actions/runs?status="):
				body = fmt.Sprintf(`{"total_count":2,"workflow_runs":[{"id":1,"head_sha":%q,"head_branch":"deleted","event":"pull_request","head_repository":{"full_name":%q}},{"id":2,"head_sha":%q,"head_branch":"current","event":"pull_request","head_repository":{"full_name":%q}}]}`, strings.Repeat("a", 40), repo, strings.Repeat("a", 40), repo)
			case strings.Contains(path, "/git/ref/heads/deleted"):
				if missing {
					return errRunnerMetadataNotFound
				}
				return errors.New("network unavailable")
			case strings.Contains(path, "/git/ref/heads/current"):
				body = fmt.Sprintf(`{"object":{"sha":%q}}`, strings.Repeat("a", 40))
			case strings.Contains(path, "/actions/runs/2/jobs?"):
				body = `{"total_count":1,"jobs":[{"id":20,"status":"queued","runner_id":0,"labels":["self-hosted","Linux","X64","cc-pc-tools-linux"]}]}`
			default:
				t.Fatalf("deleted branch caused an unexpected job request: %s", path)
			}
			return json.Unmarshal([]byte(body), out)
		}
		q, err := currentQueue(api, time.Now())
		if missing && (err != nil || q.Queued != 1) {
			t.Fatalf("deleted branch stranded unrelated current work: %+v %v", q, err)
		}
		if !missing && err == nil {
			t.Fatal("network failure was treated as a deleted source")
		}
	}
}

func TestBurstPoolDoesNotStartWithoutFreshCapacityOrAfterDeadline(t *testing.T) {
	for _, test := range []string{"resource", "deadline", "metadata", "host-error", "limit"} {
		t.Run(test, func(t *testing.T) {
			now := time.Now()
			started := 0
			d := burstPoolDeps{
				maxHosts: burstPoolLimit,
				now:      func() time.Time { return now },
				queue: func() (queueSnapshot, error) {
					if test == "metadata" {
						return queueSnapshot{}, errors.New("unavailable")
					}
					return queueSnapshot{Queued: 100}, nil
				},
				capacity: func() error {
					if test == "resource" {
						return errors.New("busy")
					}
					if test == "deadline" {
						now = now.Add(burstPoolWindow)
					}
					return nil
				},
				run: func() int {
					started++
					if test == "host-error" {
						return 7
					}
					return 0
				},
				record: func(string, int, queueSnapshot) {},
			}
			code := supplyBurstPool(d)
			if test == "limit" {
				if code != 0 || started != burstPoolLimit {
					t.Fatal("finite pool limit not enforced")
				}
			} else if test == "host-error" {
				if code != 7 || started != 1 {
					t.Fatal("failed host was repeated")
				}
			} else if started != 0 {
				t.Fatal("runner started without admission")
			}
		})
	}
}

func TestDefaultInvocationYieldsAfterOneHostAndCanBeResupplied(t *testing.T) {
	remaining, starts := 2, 0
	last := queueSnapshot{}
	d := burstPoolDeps{
		now:      time.Now,
		queue:    func() (queueSnapshot, error) { return queueSnapshot{ObservedAt: time.Now(), Queued: remaining}, nil },
		capacity: func() error { return nil },
		run:      func() int { starts++; remaining--; return 0 },
		record:   func(_ string, _ int, q queueSnapshot) { last = q },
	}
	if supplyBurstPool(d) != 0 || starts != 1 || remaining != 1 {
		t.Fatal("task invocation did not yield at one host boundary")
	}
	if last.Queued != 2 {
		t.Fatal("end marker invented a drained queue instead of preserving the observed snapshot")
	}
	if supplyBurstPool(d) != 0 || starts != 2 || remaining != 0 {
		t.Fatal("next normal task invocation did not resupply the same slot")
	}
	d.maxHosts = burstPoolLimit + 1
	if supplyBurstPool(d) == 0 || starts != 2 {
		t.Fatal("unbounded pool admission accepted")
	}
}

func TestBurstVMCapacityKeepsReserve(t *testing.T) {
	for _, data := range []string{"", "MemAvailable: 8388608 kB", "MemAvailable: 9437183 kB", "MemAvailable: bad kB"} {
		if burstVMCapacity(data) == nil {
			t.Fatalf("insufficient or unknown capacity admitted: %q", data)
		}
	}
	if err := burstVMCapacity("MemAvailable: 9437184 kB\n"); err != nil {
		t.Fatal(err)
	}
}
