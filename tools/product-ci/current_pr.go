package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// This is an additional finite route, not a replacement for the four original
// candidates or a latest-head policy. Every invocation supplies one frozen pin.
type currentPRBinding struct {
	RepoID           int64    `json:"repo_id"`
	PR               int      `json:"pr"`
	Head             string   `json:"head_sha"`
	Tree             string   `json:"head_tree"`
	Base             string   `json:"base_sha"`
	RunID            int64    `json:"run_id"`
	Attempt          int      `json:"attempt"`
	JobIDs           [8]int64 `json:"job_ids"`
	Kind             string   `json:"kind"`
	Context          string   `json:"context_sha"`
	ReviewSHA256     string   `json:"review_sha256"`
	AcceptanceSHA256 string   `json:"acceptance_sha256"`
}

type currentPRInput struct {
	currentPRBinding
	ReviewFile     string `json:"review_file"`
	AcceptanceFile string `json:"acceptance_file"`
}

func lowerHex(s string, n int) bool {
	return len(s) == n && strings.Trim(s, "0123456789abcdef") == ""
}

func (b currentPRBinding) valid() bool {
	if !(b.RepoID == 1285649124 && b.PR == 995 && lowerHex(b.Head, 40) && lowerHex(b.Tree, 40) && lowerHex(b.Base, 40) && b.RunID > 0 && b.Attempt > 0 && lowerHex(b.ReviewSHA256, 64) && lowerHex(b.AcceptanceSHA256, 64) && ((b.Kind == "secret" && lowerHex(b.Context, 40)) || (b.Kind == "codeql" && b.Context == ""))) {
		return false
	}
	previous, ended := int64(0), false
	for _, id := range b.JobIDs {
		if id == 0 {
			ended = true
			continue
		}
		if ended || id <= previous {
			return false
		}
		previous = id
	}
	return b.JobIDs[0] > 0 && (b.Kind != "secret" || b.JobIDs[1] == 0)
}

func readCurrentPRInput(file string) (currentPRBinding, error) {
	var input currentPRInput
	f, e := os.Open(file)
	if e != nil {
		return currentPRBinding{}, errors.New("current PR pin unavailable")
	}
	defer f.Close()
	d := json.NewDecoder(io.LimitReader(f, 64<<10))
	d.DisallowUnknownFields()
	if d.Decode(&input) != nil || d.Decode(new(any)) != io.EOF || !input.currentPRBinding.valid() {
		return currentPRBinding{}, errors.New("current PR pin rejected")
	}
	for _, proof := range []struct{ file, sha string }{{input.ReviewFile, input.ReviewSHA256}, {input.AcceptanceFile, input.AcceptanceSHA256}} {
		f, e := os.Open(proof.file)
		if e != nil {
			return currentPRBinding{}, errors.New("original current PR evidence unavailable")
		}
		data, readErr := io.ReadAll(io.LimitReader(f, 8<<20+1))
		closeErr := f.Close()
		if readErr != nil || closeErr != nil || len(data) == 0 || len(data) > 8<<20 || digest(data) != proof.sha {
			return currentPRBinding{}, errors.New("original current PR evidence differs")
		}
		kind := "review"
		if proof.file == input.AcceptanceFile {
			kind = "acceptance"
		}
		if e = verifyCurrentPREvidence(data, input.currentPRBinding, kind); e != nil {
			return currentPRBinding{}, e
		}
	}
	return input.currentPRBinding, nil
}

// The parent records the original output in this source-bound handoff envelope.
// Its payload is never printed or copied into host receipts. A hash alone cannot
// bind an old review to a new source, so the source and successful result are
// independently required here as well as the exact envelope byte fingerprint.
type currentPREvidence struct {
	OriginTodo     int    `json:"origin_todo_id"`
	RepoID         int64  `json:"repo_id"`
	PR             int    `json:"pr"`
	Head           string `json:"head_sha"`
	Tree           string `json:"head_tree"`
	Kind           string `json:"kind"`
	Success        bool   `json:"success"`
	Must           int    `json:"must"`
	OriginalSHA256 string `json:"original_sha256"`
	Original       []byte `json:"original"`
}

func verifyCurrentPREvidence(data []byte, b currentPRBinding, kind string) error {
	var v currentPREvidence
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(&v) != nil || d.Decode(new(any)) != io.EOF || v.OriginTodo != 3056 || v.RepoID != b.RepoID || v.PR != b.PR || v.Head != b.Head || v.Tree != b.Tree || v.Kind != kind || !v.Success || v.Must != 0 || len(v.Original) == 0 || digest(v.Original) != v.OriginalSHA256 {
		return errors.New("current PR original evidence source or result rejected")
	}
	return nil
}

func planCurrentPR(ctx context.Context, b currentPRBinding, read api) (codeQLPlan, error) {
	return planCurrentPRWithPolicy(ctx, b, read, secretWorkflowPathSHA, secretWorkflowSHA)
}

func planCurrentPRWithPolicy(ctx context.Context, b currentPRBinding, read api, pathDigest, workflowDigest string) (codeQLPlan, error) {
	if !b.valid() {
		return codeQLPlan{}, errors.New("current PR scope rejected")
	}
	c := candidate{b.RepoID, b.PR, b.Head}
	kind := b.Kind
	if kind == "codeql" {
		kind = ""
	}
	var p codeQLPlan
	var e error
	if kind == "secret" {
		p, e = planSecretJobWithPolicy(ctx, c, b.RunID, read, pathDigest, workflowDigest)
	} else {
		p, e = productPlan(ctx, c, b.RunID, kind, read)
	}
	if e != nil {
		return codeQLPlan{}, e
	}
	if p.SHA != b.Head || p.Tree != b.Tree || p.Attempt != b.Attempt || p.ContextSHA != b.Context || (p.Role != "candidate" && p.Role != "candidate-secret-workflow") {
		return codeQLPlan{}, errors.New("current PR fixed run or source differs")
	}
	var pr struct {
		Draft bool
		Base  struct{ SHA string }
	}
	if e = read(ctx, fmt.Sprintf("repos/%s/pulls/%d", p.Repo, b.PR), &pr); e != nil {
		return codeQLPlan{}, e
	}
	if pr.Draft || pr.Base.SHA != b.Base {
		return codeQLPlan{}, errors.New("current PR base or review state differs")
	}
	type officialJob struct {
		ID         int64
		RunID      int64  `json:"run_id"`
		RunAttempt int    `json:"run_attempt"`
		Head       string `json:"head_sha"`
		Status     string
		Conclusion string
		RunnerID   int64 `json:"runner_id"`
		Labels     []string
	}
	// Use the attempt-specific listing too: /jobs/{id} alone is not sufficient
	// evidence that this job still belongs to the selected run attempt.
	var jobs struct {
		Total int `json:"total_count"`
		Jobs  []struct{ ID int64 }
	}
	if e = read(ctx, fmt.Sprintf("repos/%s/actions/runs/%d/attempts/%d/jobs?per_page=100", p.Repo, b.RunID, b.Attempt), &jobs); e != nil {
		return codeQLPlan{}, e
	}
	found := map[int64]bool{}
	for _, j := range jobs.Jobs {
		found[j.ID] = true
	}
	if jobs.Total != len(jobs.Jobs) {
		return codeQLPlan{}, errors.New("current PR job attempt differs")
	}
	active := false
	for _, id := range b.JobIDs {
		if id == 0 {
			break
		}
		if !found[id] {
			return codeQLPlan{}, errors.New("current PR job attempt differs")
		}
		var job officialJob
		if e = read(ctx, fmt.Sprintf("repos/%s/actions/jobs/%d", p.Repo, id), &job); e != nil {
			return codeQLPlan{}, e
		}
		labelOK := len(job.Labels) == 1 && job.Labels[0] == p.Label
		finished := b.Kind == "codeql" && job.Status == "completed" && job.Conclusion == "success"
		if job.ID != id || job.RunID != b.RunID || job.RunAttempt != b.Attempt || job.Head != b.Head || !labelOK || (!finished && job.Status != "queued" && job.Status != "in_progress") || (job.Status == "queued" && job.RunnerID != 0) {
			return codeQLPlan{}, errors.New("current PR official job differs")
		}
		active = active || !finished
	}
	if !active {
		return codeQLPlan{}, errors.New("current PR matrix already finished")
	}
	if _, e = verify(ctx, c, read); e != nil {
		return codeQLPlan{}, e
	}
	p.Current = b
	return p, nil
}

// The label cannot select one numeric matrix job atomically. Only this frozen
// job set may be assigned to the unique owned runner; the hook checks the real
// API assignment rather than trusting GITHUB_JOB or a guest-supplied job ID.
func checkCurrentPRRunner(ctx context.Context, p codeQLPlan, name string, read api) error {
	if !currentPRGuestPlan(p) || name == "" {
		return errors.New("current PR runner binding missing")
	}
	matched := 0
	for _, id := range p.Current.JobIDs {
		if id == 0 {
			break
		}
		var j struct {
			ID         int64
			RunID      int64  `json:"run_id"`
			Attempt    int    `json:"run_attempt"`
			Head       string `json:"head_sha"`
			RunnerName string `json:"runner_name"`
			RunnerID   int64  `json:"runner_id"`
			Status     string
		}
		if e := read(ctx, fmt.Sprintf("repos/%s/actions/jobs/%d", p.Repo, id), &j); e != nil {
			return e
		}
		if j.RunnerName == name {
			if j.ID != id || j.RunID != p.RunID || j.Attempt != p.Attempt || j.Head != p.SHA || j.RunnerID < 1 || j.Status != "in_progress" {
				return errors.New("current PR runner assignment differs")
			}
			matched++
		}
	}
	if matched != 1 {
		return errors.New("current PR runner did not receive one frozen job")
	}
	return nil
}

func replanProductJob(ctx context.Context, c candidate, p codeQLPlan, read api) (codeQLPlan, error) {
	if p.Current != (currentPRBinding{}) {
		if c != (candidate{p.Current.RepoID, p.Current.PR, p.Current.Head}) {
			return codeQLPlan{}, errors.New("current PR execution identity differs")
		}
		return planCurrentPR(ctx, p.Current, read)
	}
	return productPlan(ctx, c, p.RunID, p.Kind, read)
}

func currentPRGuestPlan(p codeQLPlan) bool {
	b := p.Current
	return b.valid() && p.RepoID == b.RepoID && p.PR == b.PR && p.Candidate == b.Head && p.SHA == b.Head && p.Tree == b.Tree && p.RunID == b.RunID && p.Attempt == b.Attempt && p.ContextSHA == b.Context && ((b.Kind == "secret" && p.Kind == "secret" && p.Label == secretLabel(b.RepoID) && p.Role == "candidate-secret-workflow") || (b.Kind == "codeql" && p.Kind == "" && p.Label == codeQLLabel(b.RepoID) && p.Role == "candidate"))
}

func currentPRCLI(ctx context.Context, args []string) error {
	if !((len(args) == 2 && args[0] == "current-pr-inspect") || ((len(args) == 4 || len(args) == 5) && args[0] == "current-pr-job")) {
		return errors.New("current-pr-inspect <pin-json> | current-pr-job <pin-json> <new-owned-dir> <Linux-helper> [gitleaks-ELF]")
	}
	b, e := readCurrentPRInput(args[1])
	if e != nil {
		return e
	}
	p, e := planCurrentPR(ctx, b, get)
	if e != nil {
		return e
	}
	// All input/API/workflow/job/queue failures precede Docker and tokens.
	if e = checkCodeQLQueue(ctx, p, get); e != nil {
		return e
	}
	if args[0] == "current-pr-inspect" && len(args) == 2 {
		return json.NewEncoder(os.Stdout).Encode(p)
	}
	if args[0] != "current-pr-job" || (b.Kind == "secret" && len(args) != 5) || (b.Kind == "codeql" && len(args) != 4) {
		return errors.New("current PR execution arguments rejected")
	}
	scanner := ""
	if len(args) == 5 {
		scanner = args[4]
	}
	return executeProductPlan(ctx, candidate{b.RepoID, b.PR, b.Head}, p, args[2], args[3], scanner)
}

// Keep formatting from exposing original evidence through fmt diagnostics.
func (b currentPRInput) String() string   { return "[current PR private input]" }
func (b currentPRInput) GoString() string { return b.String() }
