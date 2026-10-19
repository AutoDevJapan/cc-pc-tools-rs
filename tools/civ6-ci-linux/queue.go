package main

import (
	"context"
	"errors"
	"fmt"
)

type queuedJob struct {
	ID     int64
	RunID  int64 `json:"run_id"`
	Status string
	Labels []string
}

// checkQueue runs immediately before registration. A GitHub runner cannot be
// reserved for a job, so the dedicated label must be requested by NO other run,
// and inside the planned run the planned job must be the oldest queued job with
// the label (the CodeQL language matrix shares one label). The hook re-checks
// the actual assignment on this unique runner and fails closed.
func checkQueue(ctx context.Context, p Plan, label string, read ReadAPI) error {
	seen := map[int64]bool{}
	var matched []queuedJob
	for _, status := range []string{"queued", "in_progress", "waiting", "pending", "requested"} {
		for page := 1; page <= 10; page++ {
			var runs struct {
				Total int                  `json:"total_count"`
				Runs  []struct{ ID int64 } `json:"workflow_runs"`
			}
			if e := read(ctx, fmt.Sprintf("repos/%s/actions/runs?status=%s&per_page=100&page=%d", repoFull, status, page), &runs); e != nil {
				return e
			}
			if runs.Total > 1000 {
				return errors.New("active run inventory exceeds the finite bound")
			}
			for _, r := range runs.Runs {
				if seen[r.ID] {
					continue
				}
				seen[r.ID] = true
				for jp := 1; jp <= 10; jp++ {
					var jobs struct {
						Total int `json:"total_count"`
						Jobs  []queuedJob
					}
					if e := read(ctx, fmt.Sprintf("repos/%s/actions/runs/%d/jobs?per_page=100&page=%d", repoFull, r.ID, jp), &jobs); e != nil {
						return e
					}
					if jobs.Total > 1000 {
						return errors.New("active job inventory exceeds the finite bound")
					}
					for _, j := range jobs.Jobs {
						if j.Status == "completed" {
							continue
						}
						for _, l := range j.Labels {
							if l == label {
								if r.ID != p.RunID || j.RunID != p.RunID {
									return errors.New("another run occupies the dedicated label")
								}
								matched = append(matched, j)
							}
						}
					}
					if jp*100 >= jobs.Total {
						break
					}
				}
			}
			if page*100 >= runs.Total {
				break
			}
		}
	}
	found := false
	for _, j := range matched {
		if j.ID == p.JobID {
			if j.Status != "queued" {
				return errors.New("planned job is not waiting for a runner")
			}
			found = true
		}
	}
	if !found {
		return errors.New("planned job is not an active job of the dedicated label")
	}
	for _, j := range matched {
		if j.ID != p.JobID && j.Status == "queued" && j.ID < p.JobID {
			return errors.New("an older queued job of the same run would take the runner")
		}
	}
	return nil
}
