package main

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

type apiRepo struct {
	ID       int64  `json:"id"`
	FullName string `json:"full_name"`
	Fork     bool   `json:"fork"`
}
type apiRun struct {
	ID           int64   `json:"id"`
	Attempt      int     `json:"run_attempt"`
	Path         string  `json:"path"`
	Event        string  `json:"event"`
	Status       string  `json:"status"`
	HeadSHA      string  `json:"head_sha"`
	HeadBranch   string  `json:"head_branch"`
	Repository   apiRepo `json:"repository"`
	HeadRepo     apiRepo `json:"head_repository"`
	PullRequests []struct {
		Number int `json:"number"`
	} `json:"pull_requests"`
}
type apiJob struct {
	ID      int64    `json:"id"`
	RunID   int64    `json:"run_id"`
	Attempt int      `json:"run_attempt"`
	Name    string   `json:"name"`
	Status  string   `json:"status"`
	Labels  []string `json:"labels"`
}

// nativeJobs maps each admitted workflow to its GITHUB_JOB. Anything else is
// outside the contract and is never planned.
var nativeJobs = map[string]string{
	".github/workflows/rust.yml":          "test",
	".github/workflows/native-driver.yml": "native-driver",
	".github/workflows/live-chain.yml":    "live-chain",
}

func admittedJobName(workflow, name string) bool {
	switch workflow {
	case CodeQLWorkflow:
		return false // Standard hosted CodeQL is observed separately, never sent to this PC.
	case ".github/workflows/rust.yml":
		return name == "test (windows-latest)"
	case ".github/workflows/native-driver.yml":
		return name == "native-driver"
	case ".github/workflows/live-chain.yml":
		return name == "live-chain"
	}
	return false
}

type Candidate struct {
	Plan Plan
	OS   string
}

type ScanReport struct {
	Runs     int      `json:"runs_seen"`
	Queued   int      `json:"queued_jobs_seen"`
	Verified int      `json:"verified"`
	Rejected int      `json:"rejected"`
	Deferred int      `json:"deferred"`
	Skipped  int      `json:"skipped_known"`
	Errors   []string `json:"errors,omitempty"`
}

type Scanner struct {
	Read      ReadAPI
	C         Contract
	Now       func() time.Time
	MaxRuns   int
	MaxVerify int
}

func transient(e error) bool {
	return errors.Is(e, errCallBudget) || errors.Is(e, context.DeadlineExceeded) || errors.Is(e, context.Canceled) ||
		(e != nil && (strings.Contains(e.Error(), "GitHub read unavailable") || strings.Contains(e.Error(), "timed out")))
}

// Observe lists the queued official jobs of the fixed repository, builds the
// plan of each unknown admitted job from the actual API facts, and keeps only
// the ones the parent's Verify accepts. It reads only; the ledger decides
// admission afterwards.
func (s *Scanner) Observe(ctx context.Context, l *Ledger) ([]Candidate, ScanReport, map[string]string) {
	var rep ScanReport
	rejects := map[string]string{}
	var runs []apiRun
	seen := map[int64]bool{}
	for _, status := range []string{"queued", "in_progress"} {
		var page struct {
			Runs []apiRun `json:"workflow_runs"`
		}
		if e := s.Read(ctx, "repos/"+Repository+"/actions/runs?status="+status+"&per_page=30", &page); e != nil {
			rep.Errors = append(rep.Errors, "run_list_"+status+"_deferred")
			continue
		}
		for _, r := range page.Runs {
			if !seen[r.ID] {
				seen[r.ID] = true
				runs = append(runs, r)
			}
		}
	}
	sort.Slice(runs, func(i, j int) bool { return runs[i].ID < runs[j].ID })
	var out []Candidate
	now := s.Now()
	handled := 0
	for _, run := range runs {
		// Foreign, fork and unrelated workflows never reach plan building.
		if run.Repository.ID != RepoID || run.Repository.FullName != Repository || run.Repository.Fork || run.HeadRepo.ID != RepoID || run.HeadRepo.Fork {
			continue
		}
		// The REST API shows either the bare path or path@ref. The plan carries
		// the bare path; the parent's MatchRunPath later re-checks the raw form.
		rawPath := run.Path
		run.Path, _, _ = strings.Cut(rawPath, "@")
		if run.Path != CodeQLWorkflow && nativeJobs[run.Path] == "" {
			continue
		}
		if run.Event != "pull_request" && run.Event != "push" && run.Event != "workflow_dispatch" && run.Event != "dynamic" {
			continue
		}
		if ctx.Err() != nil || handled >= s.MaxRuns {
			break
		}
		rep.Runs++
		attempt := run.Attempt
		if attempt < 1 {
			attempt = 1
		}
		var jobs struct {
			Total int      `json:"total_count"`
			Jobs  []apiJob `json:"jobs"`
		}
		if e := s.Read(ctx, fmt.Sprintf("repos/%s/actions/runs/%d/attempts/%d/jobs?per_page=100", Repository, run.ID, attempt), &jobs); e != nil {
			rep.Deferred++
			continue
		}
		var fresh []apiJob
		for _, j := range jobs.Jobs {
			if j.Status != "queued" || j.RunID != run.ID || j.Attempt != attempt || !admittedJobName(run.Path, j.Name) {
				continue
			}
			rep.Queued++
			key := Plan{RunID: run.ID, Attempt: attempt, JobID: j.ID}.Key()
			if _, known := l.Entries[key]; known {
				rep.Skipped++
				continue
			}
			if r, ok := l.Refusals[key]; ok {
				if next, e := time.Parse(time.RFC3339Nano, r.Next); e == nil && now.Before(next) {
					rep.Skipped++
					continue
				}
			}
			fresh = append(fresh, j)
		}
		if len(fresh) == 0 {
			continue
		}
		handled++
		base, e := s.basePlan(ctx, run, attempt)
		if e != nil {
			if transient(e) {
				rep.Deferred++
			} else {
				rep.Rejected++
				for _, j := range fresh {
					rejects[Plan{RunID: run.ID, Attempt: attempt, JobID: j.ID}.Key()] = short(e)
				}
			}
			continue
		}
		for _, j := range fresh {
			if ctx.Err() != nil || rep.Verified >= s.MaxVerify {
				rep.Deferred++
				continue
			}
			p := base
			p.JobID, p.JobName = j.ID, j.Name
			key := p.Key()
			if !s.C.MatchRunPath(p, rawPath) {
				rep.Rejected++
				rejects[key] = "run path does not match the plan workflow"
				continue
			}
			if e := s.C.Validate(p); e != nil {
				rep.Rejected++
				rejects[key] = short(e)
				continue
			}
			if e := s.C.Verify(ctx, p, s.Read); e != nil {
				if transient(e) {
					rep.Deferred++
				} else {
					rep.Rejected++
					rejects[key] = short(e)
				}
				continue
			}
			osName := s.C.OS(p)
			if osName != "Linux" && osName != "Windows" {
				rep.Rejected++
				rejects[key] = "unsupported_os"
				continue
			}
			rep.Verified++
			out = append(out, Candidate{Plan: p, OS: osName})
		}
	}
	return out, rep, rejects
}

func short(e error) string {
	s := e.Error()
	if len(s) > 160 {
		s = s[:160]
	}
	return strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return ' '
		}
		return r
	}, s)
}

type apiPull struct {
	Number   int    `json:"number"`
	State    string `json:"state"`
	MergeSHA string `json:"merge_commit_sha"`
	Head     struct {
		SHA  string  `json:"sha"`
		Ref  string  `json:"ref"`
		Repo apiRepo `json:"repo"`
	} `json:"head"`
}

// basePlan fills every Plan field that is shared by the jobs of one run from
// the official run, its PR and the immutable commit/workflow bytes.
func (s *Scanner) basePlan(ctx context.Context, run apiRun, attempt int) (Plan, error) {
	p := Plan{Version: 1, RepoID: RepoID, Head: run.HeadSHA, Event: run.Event, Workflow: run.Path, RunID: run.ID, Attempt: attempt, Branch: run.HeadBranch}
	if !shaPattern.MatchString(run.HeadSHA) {
		return p, errors.New("run head is not a commit")
	}
	if run.Path == CodeQLWorkflow {
		p.Job = "analyze"
	} else {
		p.Job = nativeJobs[run.Path]
	}
	if len(run.PullRequests) > 1 {
		return p, errors.New("run is associated with several pull requests")
	}
	switch run.Event {
	case "pull_request":
		if len(run.PullRequests) != 1 {
			return p, errors.New("pull request run without a same-repository pull request")
		}
		p.PR = run.PullRequests[0].Number
		var pr apiPull
		if e := s.Read(ctx, fmt.Sprintf("repos/%s/pulls/%d", Repository, p.PR), &pr); e != nil {
			return p, e
		}
		if pr.Number != p.PR || pr.State != "open" || pr.Head.SHA != run.HeadSHA || pr.Head.Repo.ID != RepoID || pr.Head.Repo.Fork {
			return p, errors.New("pull request head advanced or is not the same repository")
		}
		if !shaPattern.MatchString(pr.MergeSHA) {
			return p, fmt.Errorf("pull request merge context not ready: %w", context.DeadlineExceeded)
		}
		p.Branch, p.ContextSHA, p.WorkflowSHA = pr.Head.Ref, pr.MergeSHA, pr.MergeSHA
		p.WorkflowRef = Repository + "/" + run.Path + "@refs/pull/" + strconv.Itoa(p.PR) + "/merge"
	case "push", "workflow_dispatch":
		if run.Event == "workflow_dispatch" && run.HeadBranch != "main" {
			return p, errors.New("manual execution requires current main")
		}
		p.ContextSHA, p.WorkflowSHA = run.HeadSHA, run.HeadSHA
		p.WorkflowRef = Repository + "/" + run.Path + "@refs/heads/" + run.HeadBranch
	case "dynamic":
		// Generated CodeQL has no repository workflow ref or digest to invent.
		if len(run.PullRequests) == 1 {
			p.PR = run.PullRequests[0].Number
		}
		// GitHub's generated PR runs expose refs/pull/N/head and may omit
		// pull_requests entirely. Bind that numeric ref to the LIVE owned PR,
		// never to a display title or an arbitrary latest branch.
		if strings.HasPrefix(run.HeadBranch, "refs/pull/") && strings.HasSuffix(run.HeadBranch, "/head") {
			n, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(run.HeadBranch, "refs/pull/"), "/head"))
			if err != nil || n < 1 || (p.PR > 0 && p.PR != n) {
				return p, errors.New("generated PR ref identity differs")
			}
			p.PR = n
		}
		if p.PR > 0 {
			var pr apiPull
			if e := s.Read(ctx, fmt.Sprintf("repos/%s/pulls/%d", Repository, p.PR), &pr); e != nil {
				return p, e
			}
			if pr.Number != p.PR || pr.State != "open" || pr.Head.SHA != run.HeadSHA || pr.Head.Repo.ID != RepoID || pr.Head.Repo.FullName != Repository || pr.Head.Repo.Fork {
				return p, errors.New("generated PR source is not the current same repository")
			}
			if run.HeadBranch != pr.Head.Ref && run.HeadBranch != "refs/pull/"+strconv.Itoa(p.PR)+"/head" {
				return p, errors.New("generated PR branch differs")
			}
			p.Branch = pr.Head.Ref
		}
		p.ContextSHA, p.WorkflowSHA = run.HeadSHA, run.HeadSHA
	}
	var c struct {
		SHA  string `json:"sha"`
		Tree struct {
			SHA string `json:"sha"`
		} `json:"tree"`
	}
	if e := s.Read(ctx, "repos/"+Repository+"/git/commits/"+run.HeadSHA, &c); e != nil {
		return p, e
	}
	if c.SHA != run.HeadSHA || !shaPattern.MatchString(c.Tree.SHA) {
		return p, errors.New("run head commit/tree unreadable")
	}
	p.Tree = c.Tree.SHA
	if run.Event != "dynamic" {
		b, e := s.contents(ctx, run.Path, p.WorkflowSHA)
		if e != nil {
			return p, e
		}
		sum := sha256.Sum256(b)
		p.WorkflowDigest = hex.EncodeToString(sum[:])
	}
	return p, nil
}

func (s *Scanner) contents(ctx context.Context, path, ref string) ([]byte, error) {
	var v struct{ Type, Encoding, Content, SHA string }
	if e := s.Read(ctx, "repos/"+Repository+"/contents/"+path+"?ref="+ref, &v); e != nil {
		return nil, e
	}
	if v.Type != "file" || v.Encoding != "base64" || len(v.Content) > 2<<20 {
		return nil, errors.New("workflow response rejected")
	}
	b, e := base64.StdEncoding.DecodeString(strings.ReplaceAll(v.Content, "\n", ""))
	if e != nil || len(b) > 1<<20 {
		return nil, errors.New("workflow bytes unreadable")
	}
	h := sha1.New()
	fmt.Fprintf(h, "blob %d\x00", len(b))
	h.Write(b)
	if hex.EncodeToString(h.Sum(nil)) != v.SHA {
		return nil, errors.New("workflow bytes differ from Git blob")
	}
	return b, nil
}
