package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

func sameCodeQLSettings(a, b codeQLSetup) bool {
	x := append([]string(nil), a.Languages...)
	y := append([]string(nil), b.Languages...)
	sort.Strings(x)
	sort.Strings(y)
	if a.State != b.State || a.QuerySuite != b.QuerySuite || a.ThreatModel != b.ThreatModel || len(x) != len(y) {
		return false
	}
	for i := range x {
		if x[i] != y[i] {
			return false
		}
	}
	return true
}

func restoreCodeQL(ctx context.Context, c candidate, dir string) error {
	r, e := verify(ctx, c, get)
	if e != nil {
		return e
	}
	b, e := os.ReadFile(filepath.Join(dir, fmt.Sprintf("repo-%d-codeql-route.private.json", c.ID)))
	if e != nil {
		return errors.New("original route receipt unavailable")
	}
	var saved struct {
		RepoID int64       `json:"repo_id"`
		SHA    string      `json:"candidate_sha"`
		Prior  codeQLSetup `json:"prior"`
		Label  string      `json:"label"`
	}
	if json.Unmarshal(b, &saved) != nil || saved.RepoID != c.ID || saved.SHA != c.SHA || saved.Label != codeQLLabel(c.ID) || saved.Prior.RunnerType != "standard" || saved.Prior.RunnerLabel != "" {
		return errors.New("original route receipt rejected")
	}
	var current codeQLSetup
	if e = get(ctx, "repos/"+r.FullName+"/code-scanning/default-setup", &current); e != nil {
		return e
	}
	if !sameCodeQLSettings(current, saved.Prior) {
		return errors.New("settings changed independently; restoration rejected")
	}
	if current.RunnerType == saved.Prior.RunnerType && current.RunnerLabel == saved.Prior.RunnerLabel {
		return nil
	}
	if current.RunnerType != "labeled" || current.RunnerLabel != saved.Label {
		return errors.New("route changed independently; restoration rejected")
	}
	f, e := os.OpenFile(filepath.Join(dir, "codeql-restore-intent.private.json"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return errors.New("restore intent already exists; inspect current route before retry")
	}
	e = json.NewEncoder(f).Encode(map[string]any{"repo_id": c.ID, "runner_type": saved.Prior.RunnerType, "runner_label": saved.Prior.RunnerLabel})
	if e == nil {
		e = f.Sync()
	}
	e = errors.Join(e, f.Close())
	if e != nil {
		return e
	}
	if e = mutateJSON(ctx, "PATCH", "repos/"+r.FullName+"/code-scanning/default-setup", map[string]any{"runner_type": "standard", "runner_label": nil}, nil); e != nil {
		return e
	}
	if e = get(ctx, "repos/"+r.FullName+"/code-scanning/default-setup", &current); e != nil {
		return e
	}
	if !sameCodeQLSettings(current, saved.Prior) || current.RunnerType != "standard" || current.RunnerLabel != "" {
		return errors.New("route restoration not yet confirmed")
	}
	return nil
}

type codeQLQueuedJob struct {
	ID     int64
	RunID  int64 `json:"run_id"`
	Status string
	Labels []string
}

// A GitHub runner cannot reserve a workflow job atomically. Check every active
// repo run immediately before registration, then fail closed again at the hook.
// Matrix languages in this SAME run are admitted one per ephemeral session.
func checkCodeQLQueue(ctx context.Context, p codeQLPlan, read api) error {
	seen := map[int64]bool{}
	targetJobs := 0
	for _, status := range []string{"queued", "in_progress", "waiting", "pending", "requested"} {
		for page := 1; page <= 10; page++ {
			var runs struct {
				Total int                  `json:"total_count"`
				Runs  []struct{ ID int64 } `json:"workflow_runs"`
			}
			if e := read(ctx, fmt.Sprintf("repos/%s/actions/runs?status=%s&per_page=100&page=%d", p.Repo, status, page), &runs); e != nil {
				return e
			}
			if runs.Total > 1000 {
				return errors.New("active run inventory exceeds finite bound")
			}
			for _, r := range runs.Runs {
				if seen[r.ID] {
					continue
				}
				seen[r.ID] = true
				for jp := 1; jp <= 10; jp++ {
					var jobs struct {
						Total int `json:"total_count"`
						Jobs  []codeQLQueuedJob
					}
					if e := read(ctx, fmt.Sprintf("repos/%s/actions/runs/%d/jobs?per_page=100&page=%d", p.Repo, r.ID, jp), &jobs); e != nil {
						return e
					}
					if jobs.Total > 1000 {
						return errors.New("active job inventory exceeds finite bound")
					}
					for _, j := range jobs.Jobs {
						if j.Status == "completed" {
							continue
						}
						matched := false
						for _, l := range j.Labels {
							if l == p.Label {
								matched = true
							}
						}
						if matched {
							if r.ID != p.RunID || j.RunID != p.RunID {
								return errors.New("other run occupies the dedicated CodeQL label")
							}
							targetJobs++
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
	if targetJobs == 0 {
		return errors.New("no active target job requests the dedicated label")
	}
	return nil
}
