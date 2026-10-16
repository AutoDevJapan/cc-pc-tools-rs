package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type codeQLRevalidation struct {
	RepoID      int64       `json:"repo_id"`
	Candidate   string      `json:"candidate_sha"`
	DefaultSHA  string      `json:"default_sha"`
	Started     time.Time   `json:"started"`
	Prior       codeQLSetup `json:"prior"`
	LatestRunID int64       `json:"latest_run_id"`
}

func revalidationPreflight(ctx context.Context, c candidate, read api) (codeQLRevalidation, string, error) {
	r, e := verify(ctx, c, read)
	if e != nil {
		return codeQLRevalidation{}, "", e
	}
	var s codeQLSetup
	if e = read(ctx, "repos/"+r.FullName+"/code-scanning/default-setup", &s); e != nil {
		return codeQLRevalidation{}, "", e
	}
	if s.State != "configured" || s.RunnerType != "labeled" || s.RunnerLabel != codeQLLabel(c.ID) || len(s.Languages) == 0 || (s.QuerySuite != "default" && s.QuerySuite != "extended") || (s.ThreatModel != "remote" && s.ThreatModel != "remote_and_local") {
		return codeQLRevalidation{}, "", errors.New("dedicated original configuration required")
	}
	// Reconfiguration is a bounded recovery of default setup, not cancellation
	// of a running scan. Any active dynamic run prevents the state transition.
	for _, status := range []string{"queued", "in_progress", "waiting", "pending", "requested"} {
		var runs struct {
			Total int `json:"total_count"`
		}
		if e = read(ctx, "repos/"+r.FullName+"/actions/runs?event=dynamic&status="+status+"&per_page=1", &runs); e != nil {
			return codeQLRevalidation{}, "", e
		}
		if runs.Total != 0 {
			return codeQLRevalidation{}, "", errors.New("active default setup run prevents reconfiguration")
		}
	}
	var meta struct {
		DefaultBranch string `json:"default_branch"`
	}
	if e = read(ctx, fmt.Sprintf("repositories/%d", c.ID), &meta); e != nil || meta.DefaultBranch == "" {
		return codeQLRevalidation{}, "", errors.New("default branch unavailable")
	}
	var ref struct{ Object struct{ SHA string } }
	if e = read(ctx, "repos/"+r.FullName+"/git/ref/heads/"+meta.DefaultBranch, &ref); e != nil || len(ref.Object.SHA) != 40 {
		return codeQLRevalidation{}, "", errors.New("default revision unavailable")
	}
	var latest struct {
		Runs []struct{ ID int64 } `json:"workflow_runs"`
	}
	if e = read(ctx, "repos/"+r.FullName+"/actions/runs?event=dynamic&per_page=1", &latest); e != nil || len(latest.Runs) != 1 || latest.Runs[0].ID < 1 {
		return codeQLRevalidation{}, "", errors.New("latest dynamic run baseline unavailable")
	}
	return codeQLRevalidation{RepoID: c.ID, Candidate: c.SHA, DefaultSHA: ref.Object.SHA, Started: time.Now().UTC(), Prior: s, LatestRunID: latest.Runs[0].ID}, r.FullName, nil
}

func revalidationBody(s codeQLSetup) map[string]any {
	// Preserve suite/threat/runner and let default setup detect ALL languages.
	// The REST write enumeration cannot represent every language returned by
	// GET (notably Rust). Refuse completion if redetection loses any coverage.
	return map[string]any{"state": "configured", "runner_type": s.RunnerType, "runner_label": s.RunnerLabel, "query_suite": s.QuerySuite, "threat_model": s.ThreatModel}
}

func readRevalidation(c candidate, dir string) (codeQLRevalidation, error) {
	b, e := os.ReadFile(filepath.Join(dir, "codeql-revalidation-intent.private.json"))
	var s codeQLRevalidation
	if e != nil || json.Unmarshal(b, &s) != nil || s.RepoID != c.ID || s.Candidate != c.SHA || len(s.DefaultSHA) != 40 || strings.Trim(s.DefaultSHA, "0123456789abcdef") != "" || s.Started.IsZero() || s.LatestRunID < 1 || len(s.Prior.Languages) < 1 || len(s.Prior.Languages) > 20 || (s.Prior.QuerySuite != "default" && s.Prior.QuerySuite != "extended") || (s.Prior.ThreatModel != "remote" && s.Prior.ThreatModel != "remote_and_local") || s.Prior.State != "configured" || s.Prior.RunnerType != "labeled" || s.Prior.RunnerLabel != codeQLLabel(c.ID) {
		return s, errors.New("revalidation intent unavailable")
	}
	return s, nil
}

// Complete only this recorded recovery's interrupted re-enable operation.
// An independently configured or changed route is never overwritten.
func recoverCodeQLRevalidation(ctx context.Context, c candidate, dir string) error {
	return recoverCodeQLRevalidationUsing(ctx, c, dir, get, mutateJSON)
}

type githubMutator func(context.Context, string, string, any, any) error

func saveRevalidationRun(c candidate, dir string, runID int64) error {
	b, _ := json.MarshalIndent(map[string]any{"repo_id": c.ID, "run_id": runID, "role": "setup-validation-not-candidate"}, "", "  ")
	return os.WriteFile(filepath.Join(dir, "codeql-route-response.private.json"), b, 0600)
}

func recoverCodeQLRevalidationUsing(ctx context.Context, c candidate, dir string, read api, write githubMutator) error {
	s, e := readRevalidation(c, dir)
	if e != nil {
		return e
	}
	r, e := verify(ctx, c, read)
	if e != nil {
		return e
	}
	var now codeQLSetup
	endpoint := "repos/" + r.FullName + "/code-scanning/default-setup"
	if e = read(ctx, endpoint, &now); e != nil {
		return e
	}
	if now.State == "not-configured" {
		var response struct {
			RunID int64 `json:"run_id"`
		}
		if e = write(ctx, "PATCH", endpoint, revalidationBody(s.Prior), &response); e != nil {
			return e
		}
		if e = saveRevalidationRun(c, dir, response.RunID); e != nil {
			return e
		}
		if e = read(ctx, endpoint, &now); e != nil {
			return e
		}
	}
	if now.State == "configured" && len(now.Languages) == 0 && now.RunnerType == s.Prior.RunnerType && now.RunnerLabel == s.Prior.RunnerLabel && now.QuerySuite == s.Prior.QuerySuite && now.ThreatModel == s.Prior.ThreatModel {
		// GitHub exposes no enabled languages until its fresh validation finishes.
		// Admit only if the actual generated matrix retains every prior language.
		var response struct {
			RunID int64 `json:"run_id"`
		}
		b, e := os.ReadFile(filepath.Join(dir, "codeql-route-response.private.json"))
		if e != nil || json.Unmarshal(b, &response) != nil || response.RunID <= s.LatestRunID {
			var list struct {
				Runs []struct {
					ID  int64
					SHA string `json:"head_sha"`
				} `json:"workflow_runs"`
			}
			if e = read(ctx, "repos/"+r.FullName+"/actions/runs?event=dynamic&head_sha="+s.DefaultSHA+"&per_page=100", &list); e != nil {
				return e
			}
			var ids []int64
			for _, run := range list.Runs {
				if run.ID > s.LatestRunID && run.SHA == s.DefaultSHA {
					ids = append(ids, run.ID)
				}
			}
			if len(ids) != 1 {
				return errors.New("pending validation identity absent or ambiguous")
			}
			response.RunID = ids[0]
			if e = verifyRevalidationRun(ctx, c, s, r.FullName, response.RunID, read); e != nil {
				return e
			}
			if e = saveRevalidationRun(c, dir, response.RunID); e != nil {
				return e
			}
		}
		return verifyRevalidationRun(ctx, c, s, r.FullName, response.RunID, read)
	}
	if !sameCodeQLSettings(now, s.Prior) || now.RunnerType != s.Prior.RunnerType || now.RunnerLabel != s.Prior.RunnerLabel {
		return errors.New("revalidated settings differ; preserve recovery lock")
	}
	return nil
}

func verifyRevalidationRun(ctx context.Context, c candidate, s codeQLRevalidation, repo string, runID int64, read api) error {
	if runID < 1 {
		return errors.New("fresh setup validation run absent")
	}
	var run struct {
		ID                              int64
		Created                         time.Time `json:"created_at"`
		Path, Event, Status, Conclusion string
		SHA                             string `json:"head_sha"`
		Repository                      repository
		HeadRepo                        repository `json:"head_repository"`
	}
	if e := read(ctx, fmt.Sprintf("repos/%s/actions/runs/%d", repo, runID), &run); e != nil {
		return e
	}
	if run.ID != runID || runID <= s.LatestRunID || run.SHA != s.DefaultSHA || run.Repository.ID != c.ID || run.HeadRepo.ID != c.ID || run.Repository.FullName != repo || run.HeadRepo.FullName != repo || run.HeadRepo.Fork || run.Path != "dynamic/github-code-scanning/codeql" || run.Event != "dynamic" || (run.Status != "queued" && run.Status != "in_progress" && run.Status != "waiting" && !(run.Status == "completed" && run.Conclusion == "success")) {
		return errors.New("historical or unexpected validation run rejected")
	}
	var jobs struct {
		Total int `json:"total_count"`
		Jobs  []struct {
			ID                 int64
			RunID              int64 `json:"run_id"`
			Name               string
			Labels             []string
			Status, Conclusion string
		}
	}
	if e := read(ctx, fmt.Sprintf("repos/%s/actions/runs/%d/jobs?per_page=100", repo, runID), &jobs); e != nil {
		return e
	}
	want := codeQLLanguages(s.Prior.Languages)
	seen := map[string]bool{}
	if len(want) == 0 || jobs.Total != len(jobs.Jobs) || jobs.Total > 100 {
		return errors.New("full validation matrix unavailable")
	}
	for _, j := range jobs.Jobs {
		if j.Name == "Adjust Configuration" && j.Status == "completed" && j.Conclusion == "skipped" {
			continue
		}
		language, ok := strings.CutPrefix(j.Name, "Analyze (")
		if !ok || !strings.HasSuffix(language, ")") || j.ID < 1 || j.RunID != runID || len(j.Labels) != 1 || j.Labels[0] != codeQLLabel(c.ID) {
			return errors.New("dedicated generated validation job rejected")
		}
		language = strings.TrimSuffix(language, ")")
		if seen[language] {
			return errors.New("duplicate validation language")
		}
		seen[language] = true
		if run.Status == "completed" && (j.Status != "completed" || j.Conclusion != "success") {
			return errors.New("validation analysis did not succeed")
		}
	}
	if len(seen) != len(want) {
		return errors.New("validation language count differs")
	}
	for _, language := range want {
		if !seen[language] {
			return errors.New("original validation language missing")
		}
	}
	return nil
}

func revalidateCodeQL(ctx context.Context, c candidate, dir string) error {
	s, repo, e := revalidationPreflight(ctx, c, get)
	if e != nil {
		return e
	}
	f, e := os.OpenFile(filepath.Join(dir, "codeql-revalidation-intent.private.json"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return errors.New("new revalidation intent required")
	}
	e = json.NewEncoder(f).Encode(s)
	if e == nil {
		e = f.Sync()
	}
	e = errors.Join(e, f.Close())
	if e != nil {
		return e
	}
	endpoint := "repos/" + repo + "/code-scanning/default-setup"
	// Official default-setup recovery: immediately re-enable the same suite,
	// threat model and dedicated runner route, and verify every language. No
	// required check is removed from source or replaced by a success status.
	if e = mutateJSON(ctx, "PATCH", endpoint, map[string]string{"state": "not-configured"}, nil); e != nil {
		return e
	}
	var response struct {
		RunID int64 `json:"run_id"`
	}
	if e = mutateJSON(ctx, "PATCH", endpoint, revalidationBody(s.Prior), &response); e != nil {
		return e
	}
	if e = saveRevalidationRun(c, dir, response.RunID); e != nil {
		return e
	}
	if response.RunID <= s.LatestRunID {
		return recoverCodeQLRevalidation(ctx, c, dir)
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		e = verifyRevalidationRun(ctx, c, s, repo, response.RunID, get)
		if e == nil {
			break
		}
		if time.Now().After(deadline) {
			return e
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	return recoverCodeQLRevalidation(ctx, c, dir)
}
