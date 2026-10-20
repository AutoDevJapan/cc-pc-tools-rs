package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const burstPoolLimit = 16
const burstPoolWindow = 2 * time.Hour

var errRunnerMetadataNotFound = errors.New("runner metadata not found")

type queueRun struct {
	ID             int64  `json:"id"`
	HeadSHA        string `json:"head_sha"`
	HeadBranch     string `json:"head_branch"`
	Event          string `json:"event"`
	HeadRepository struct {
		FullName string `json:"full_name"`
	} `json:"head_repository"`
}

type queuedJob struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	RunnerID  int64     `json:"runner_id"`
	CreatedAt time.Time `json:"created_at"`
	Labels    []string  `json:"labels"`
}

type queueSnapshot struct {
	ObservedAt time.Time `json:"observedAt"`
	Queued     int       `json:"queued"`
	OldestSecs float64   `json:"oldestSeconds"`
}

func runnerAPI(path string, out any) error {
	return runnerRequest("GET", path, out)
}

func runnerRequest(method, path string, out any) error {
	if !strings.HasPrefix(path, "repos/"+repo+"/") {
		return errors.New("runner API repository rejected")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, gh, "api", "--method", method, path)
	hidden(c)
	c.WaitDelay = 3 * time.Second
	data, err := c.Output()
	if err != nil {
		var failed *exec.ExitError
		if errors.As(err, &failed) && strings.Contains(string(failed.Stderr), "HTTP 404") {
			return fmt.Errorf("%w: %s", errRunnerMetadataNotFound, path)
		}
		return fmt.Errorf("runner metadata request failed: %s", path)
	}
	if out == nil && len(data) == 0 {
		return nil
	}
	if len(data) > 16<<20 || (out != nil && json.Unmarshal(data, out) != nil) {
		return errors.New("runner metadata response rejected")
	}
	return nil
}

func currentQueue(api func(string, any) error, now time.Time) (queueSnapshot, error) {
	snapshot := queueSnapshot{ObservedAt: now}
	byID := map[int64]queueRun{}
	for _, status := range []string{"queued", "in_progress"} {
		var response struct {
			Total int        `json:"total_count"`
			Runs  []queueRun `json:"workflow_runs"`
		}
		if err := api("repos/"+repo+"/actions/runs?status="+status+"&per_page=100", &response); err != nil {
			return snapshot, err
		}
		if response.Total > 100 {
			return snapshot, errors.New("runner queue exceeds finite metadata scan")
		}
		for _, run := range response.Runs {
			if run.HeadRepository.FullName == repo && (run.Event == "pull_request" || run.Event == "push" || run.Event == "schedule") {
				byID[run.ID] = run
			}
		}
	}
	var runs []queueRun
	for _, run := range byID {
		runs = append(runs, run)
	}
	sort.Slice(runs, func(i, j int) bool { return runs[i].ID < runs[j].ID })
	heads := map[string]string{}
	for _, run := range runs {
		if !revisionPattern.MatchString(run.HeadSHA) || run.HeadBranch == "" || strings.HasPrefix(run.HeadBranch, "-") || strings.ContainsAny(run.HeadBranch, "\x00\r\n") {
			continue
		}
		head, found := heads[run.HeadBranch]
		if !found {
			var ref struct {
				Object struct{ SHA string } `json:"object"`
			}
			if err := api("repos/"+repo+"/git/ref/heads/"+url.PathEscape(run.HeadBranch), &ref); err != nil {
				if errors.Is(err, errRunnerMetadataNotFound) {
					// A deleted branch cannot be a current source. Keep unrelated
					// queued jobs eligible; do not hide network/auth/API failures.
					heads[run.HeadBranch] = ""
					continue
				}
				return snapshot, err
			}
			head = ref.Object.SHA
			heads[run.HeadBranch] = head
		}
		if head != run.HeadSHA || (run.Event != "pull_request" && run.HeadBranch != normalBranch) {
			continue
		}
		var jobs struct {
			Total int         `json:"total_count"`
			Jobs  []queuedJob `json:"jobs"`
		}
		if err := api("repos/"+repo+"/actions/runs/"+strconv.FormatInt(run.ID, 10)+"/jobs?per_page=100", &jobs); err != nil {
			return snapshot, err
		}
		if jobs.Total > 100 {
			return snapshot, errors.New("runner job list exceeds finite metadata scan")
		}
		for _, job := range jobs.Jobs {
			labels := map[string]bool{}
			for _, label := range job.Labels {
				labels[label] = true
			}
			if job.Status != "queued" || job.RunnerID != 0 || !labels["self-hosted"] || !labels["Linux"] || !labels["X64"] || !labels[normalLabel] {
				continue
			}
			snapshot.Queued++
			if !job.CreatedAt.IsZero() && job.CreatedAt.Before(now) {
				snapshot.OldestSecs = max(snapshot.OldestSecs, now.Sub(job.CreatedAt).Seconds())
			}
		}
	}
	return snapshot, nil
}

type burstPoolDeps struct {
	now      func() time.Time
	queue    func() (queueSnapshot, error)
	capacity func() error
	run      func() int
	record   func(string, int, queueSnapshot)
}

func supplyBurstPool(d burstPoolDeps) int {
	deadline := d.now().Add(burstPoolWindow)
	ended := 0
	for ; ended < burstPoolLimit && d.now().Before(deadline); ended++ {
		q, err := d.queue()
		if err != nil {
			d.record("metadata-unavailable", ended, q)
			return 1
		}
		if q.Queued == 0 {
			d.record("queue-drained", ended, q)
			return 0
		}
		if d.capacity() != nil {
			d.record("resource-wait", ended, q)
			return 0
		}
		// Metadata and resource probes consume the same finite admission window.
		if !d.now().Before(deadline) {
			d.record("admission-window-ended", ended, q)
			return 0
		}
		d.record("one-job-running", ended, q)
		if code := d.run(); code != 0 {
			d.record("one-job-host-failed", ended, q)
			return code
		}
		d.record("one-job-host-ended", ended+1, q)
	}
	d.record("finite-pool-ended", ended, queueSnapshot{ObservedAt: d.now()})
	return 0
}

func burstVMCapacity(data string) error {
	for _, line := range strings.Split(data, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 3 && fields[0] == "MemAvailable:" && fields[2] == "kB" {
			available, err := strconv.ParseUint(fields[1], 10, 64)
			if err == nil && available >= 9*1024*1024 {
				return nil
			}
		}
	}
	return errors.New("Docker VM needs 8GiB for the additional runner plus 1GiB reserve")
}

func burstPoolMain(args []string) int {
	if len(args) != 1 || !compiledNormalProfile.ephemeral {
		return 1
	}
	// Task Scheduler also uses IgnoreNew; this protects other simultaneous
	// invocations before they can request any registration token.
	lock, err := net.Listen("tcp", "127.0.0.1:17863")
	if err != nil {
		return 1
	}
	defer lock.Close()
	return supplyBurstPool(burstPoolDeps{
		now: time.Now,
		queue: func() (queueSnapshot, error) {
			return currentQueue(runnerAPI, time.Now().UTC())
		},
		capacity: func() error {
			if err := burstHostCapacity(); err != nil {
				return err
			}
			data, err := output(command(docker, "exec", normalContainer, "cat", "/proc/meminfo"))
			if err != nil {
				return err
			}
			return burstVMCapacity(data)
		},
		run: func() int {
			self, err := os.Executable()
			if err != nil {
				return 1
			}
			c := command(self, "host", "normal", args[0])
			c.Stdout, c.Stderr = os.Stdout, os.Stderr
			return exitCode(c.Run())
		},
		record: func(stage string, ended int, q queueSnapshot) {
			record := struct {
				Stage      string        `json:"stage"`
				ObservedAt time.Time     `json:"observedAt"`
				HostsEnded int           `json:"hostsEnded"`
				Queue      queueSnapshot `json:"queue"`
			}{stage, time.Now().UTC(), ended, q}
			b, err := json.MarshalIndent(record, "", "  ")
			if err == nil {
				_ = os.WriteFile(filepath.Join(".ci-runtime", "pool-status.private.json"), b, 0600)
			}
			fmt.Printf("linux-burst-pool stage=%s hosts-ended=%d queued=%d\n", stage, ended, q.Queued)
		},
	})
}
