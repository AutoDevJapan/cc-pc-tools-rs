package contract

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

func completePage(total, page, count int) bool {
	if total < 0 || total > 1000 || page < 1 {
		return false
	}
	want := total - (page-1)*100
	if want < 0 {
		return false
	}
	if want > 100 {
		want = 100
	}
	return count == want
}

// CheckRegistrationQueue checks the actual jobs a runner could consume. A job
// requesting only legacy/base labels also matches a runner with extra labels.
// This is a fresh pre-registration check, not a reservation; the SDK hook must
// still verify the actual assignment before executing repository code.
func CheckRegistrationQueue(ctx context.Context, p Plan, offered []string, read ReadAPI) error {
	if e := p.Validate(); e != nil {
		return e
	}
	labels := map[string]bool{}
	for _, label := range offered {
		label = strings.ToLower(label) // GitHub runner labels are case-insensitive.
		if label == "" || labels[label] {
			return errors.New("registration label set rejected")
		}
		labels[label] = true
	}
	seen := map[int64]bool{}
	found := false
	for _, status := range []string{"queued", "in_progress", "waiting", "pending", "requested"} {
		for page := 1; page <= 10; page++ {
			var runs struct {
				Total int                  `json:"total_count"`
				Runs  []struct{ ID int64 } `json:"workflow_runs"`
			}
			if e := read(ctx, fmt.Sprintf("repos/%s/actions/runs?status=%s&per_page=100&page=%d", Repository, status, page), &runs); e != nil {
				return e
			}
			if !completePage(runs.Total, page, len(runs.Runs)) {
				return errors.New("complete bounded run inventory required")
			}
			for _, r := range runs.Runs {
				if r.ID <= 0 {
					return errors.New("run inventory identity missing")
				}
				if seen[r.ID] {
					continue
				}
				seen[r.ID] = true
				for jp := 1; jp <= 10; jp++ {
					var jobs struct {
						Total int   `json:"total_count"`
						Jobs  []Job `json:"jobs"`
					}
					if e := read(ctx, fmt.Sprintf("repos/%s/actions/runs/%d/jobs?per_page=100&page=%d", Repository, r.ID, jp), &jobs); e != nil {
						return e
					}
					if !completePage(jobs.Total, jp, len(jobs.Jobs)) {
						return errors.New("complete bounded job inventory required")
					}
					for _, j := range jobs.Jobs {
						if j.Status == "completed" {
							continue
						}
						if j.ID <= 0 || j.RunID != r.ID || len(j.Labels) == 0 {
							return errors.New("active job inventory incomplete")
						}
						matches := true
						for _, required := range j.Labels {
							if !labels[strings.ToLower(required)] {
								matches = false
								break
							}
						}
						if !matches {
							continue
						}
						if j.ID != p.JobID || r.ID != p.RunID {
							return errors.New("another active job can consume this runner")
						}
						if j.Status != "queued" {
							return errors.New("planned job is not queued")
						}
						found = true
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
	if !found {
		return errors.New("planned job missing from matching queue")
	}
	return nil
}
