// Package main is the finite Linux half of the Civ6 CI route: one official
// ephemeral GitHub runner per job, inside a new credential-free Docker
// namespace. The queue owner decides WHEN to call it; this helper never selects
// a job, never rewrites a workflow and never accepts a command from its input.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"sort"
	"strings"
)

const (
	repoFull             = "AutoDevJapan/civ6-banseiki"
	repoID         int64 = 1392152644
	linuxLabel           = "civ6-ci-linux"
	codeQLLabel          = "cc-product-2648-1392152644-codeql"
	codeQLWorkflow       = "dynamic/github-code-scanning/codeql"
	rustWorkflow         = ".github/workflows/rust.yml"
	workspace            = "/work/civ6-banseiki/civ6-banseiki"
)

// Plan has exactly the JSON shape fixed by the parent's contract document. It
// holds no secret. Decoding is strict so an extra field can never smuggle a
// command, path or environment value into this helper.
type Plan struct {
	Version        int    `json:"version"`
	RepoID         int64  `json:"repo_id"`
	Head           string `json:"head"`
	Tree           string `json:"tree"`
	Event          string `json:"event"`
	Branch         string `json:"branch"`
	PR             int    `json:"pr"`
	ContextSHA     string `json:"context_sha"`
	WorkflowSHA    string `json:"workflow_sha"`
	WorkflowRef    string `json:"workflow_ref"`
	Workflow       string `json:"workflow"`
	WorkflowDigest string `json:"workflow_digest"`
	RunID          int64  `json:"run_id"`
	Attempt        int    `json:"attempt"`
	JobID          int64  `json:"job_id"`
	Job            string `json:"job"`
	JobName        string `json:"job_name"`
}

type Kind string

const (
	KindRust   Kind = "rust"
	KindCodeQL Kind = "codeql"
)

func isHex(s string, n int) bool { return len(s) == n && strings.Trim(s, "0123456789abcdef") == "" }

// kind accepts only the three Linux jobs of the owned repository: the Rust
// Linux matrix job and the two original CodeQL languages. Native/live jobs are
// the Windows helper's, never ours.
func (p Plan) kind() (Kind, error) {
	if p.Workflow == codeQLWorkflow && p.Job == "analyze" && (p.JobName == "Analyze (actions)" || p.JobName == "Analyze (rust)") {
		return KindCodeQL, nil
	}
	if p.Workflow == rustWorkflow && p.Job == "test" && p.JobName == "test (ubuntu-latest)" {
		return KindRust, nil
	}
	return "", errors.New("not a Linux job of the fixed repository")
}

// localCheck is only a defensive shape check. The authoritative checks are the
// parent's Validate/Verify/CheckEvent, which are always called as well.
func (p Plan) localCheck() error {
	if p.Version != 1 || p.RepoID != repoID || !isHex(p.Head, 40) || !isHex(p.Tree, 40) || !isHex(p.ContextSHA, 40) ||
		!isHex(p.WorkflowSHA, 40) || p.RunID <= 0 || p.JobID <= 0 || p.Attempt < 1 || p.PR < 0 || p.Branch == "" || len(p.Branch) > 240 {
		return errors.New("fixed repository/source/job identity required")
	}
	if p.Event == "dynamic" {
		if p.WorkflowDigest != "" {
			return errors.New("generated CodeQL context must not carry a workflow digest")
		}
	} else if !isHex(p.WorkflowDigest, 64) {
		return errors.New("workflow digest required")
	}
	_, e := p.kind()
	return e
}

func LoadPlan(path string) (Plan, error) {
	f, e := os.Open(path)
	if e != nil {
		return Plan{}, errors.New("plan file unavailable")
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, 64<<10+1))
	if e != nil || len(b) > 64<<10 {
		return Plan{}, errors.New("plan file unreadable or too large")
	}
	return ParsePlan(b)
}

func ParsePlan(b []byte) (Plan, error) {
	var p Plan
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(&p) != nil || d.Decode(new(any)) != io.EOF {
		return Plan{}, errors.New("plan JSON rejected")
	}
	if e := p.localCheck(); e != nil {
		return Plan{}, e
	}
	return p, nil
}

// registrationLabels turns the parent's runs-on labels into the official
// runner registration. The Linux route keeps the default labels (so the job's
// self-hosted/Linux/X64 are honest) plus one custom label; the original CodeQL
// route is the single dedicated label with default labels disabled.
func registrationLabels(k Kind, labels []string) (custom string, defaults bool, err error) {
	got := append([]string(nil), labels...)
	sort.Strings(got)
	switch k {
	case KindRust:
		want := []string{"Linux", "X64", linuxLabel, "self-hosted"}
		if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
			return "", false, errors.New("Linux job labels differ from the fixed route")
		}
		return linuxLabel, true, nil
	case KindCodeQL:
		if len(got) != 1 || got[0] != codeQLLabel {
			return "", false, errors.New("CodeQL job label differs from the original dedicated label")
		}
		return codeQLLabel, false, nil
	}
	return "", false, errors.New("unsupported job kind")
}
