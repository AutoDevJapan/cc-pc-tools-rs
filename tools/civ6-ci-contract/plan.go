// Package contract is the shared, read-only admission policy for the one
// explicitly owned repository. It never registers runners or writes statuses.
package contract

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

const Repository = "AutoDevJapan/civ6-banseiki"
const RepoID int64 = 1392152644
const OriginalHead = "f3e26f45682ac04b8a9bf07821ce5b96d83ce012"
const OriginalTree = "a45e137db144ba41db75cd0ba30be63500efb905"
const OriginalBase = "1100676d1ed421794c252fb4b15ed3d954e21a5b"
const OriginalBranch = "scheduler/actions-cost-pr-concurrency-20261019"
const LinuxLabel = "civ6-ci-linux"
const WindowsLabel = "civ6-ci-windows"
const CodeQLLabel = "cc-product-2648-1392152644-codeql"
const CodeQLWorkflow = "dynamic/github-code-scanning/codeql"

type ReadAPI func(context.Context, string, any) error

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

var shaPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
var digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

func ValidSHA(s string) bool { return shaPattern.MatchString(s) }
func Digest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func ValidBranch(s string) bool {
	if s == "" || len(s) > 240 || strings.HasPrefix(s, "/") || strings.HasSuffix(s, "/") || strings.HasSuffix(s, ".") || strings.ContainsAny(s, " ~^:?*[\\\x00\r\n\t") || strings.Contains(s, "..") || strings.Contains(s, "@{") || strings.Contains(s, "//") {
		return false
	}
	for _, part := range strings.Split(s, "/") {
		if strings.HasPrefix(part, ".") || strings.HasSuffix(part, ".lock") {
			return false
		}
	}
	for _, r := range s {
		if r < 32 || r == 127 {
			return false
		}
	}
	return true
}
func (p Plan) Validate() error {
	if p.Version != 1 || p.RepoID != RepoID || !ValidSHA(p.Head) || !ValidSHA(p.Tree) || !ValidSHA(p.ContextSHA) || !ValidBranch(p.Branch) || p.RunID <= 0 || p.JobID <= 0 || p.Attempt < 1 || p.PR < 0 {
		return errors.New("fixed repository/source/job identity required")
	}
	switch p.Event {
	case "pull_request":
		if p.PR < 1 {
			return errors.New("same-repository pull request required")
		}
	case "push":
		if p.PR != 0 || p.Branch != "main" {
			return errors.New("only normal main pushes admitted")
		}
	case "workflow_dispatch":
		if p.PR == 0 && p.Branch != "main" {
			return errors.New("dispatch must name current main or a same-repository PR")
		}
	case "dynamic":
		if p.Workflow != CodeQLWorkflow || (p.PR == 0 && p.ContextSHA != p.Head) || p.WorkflowDigest != "" || p.WorkflowRef != "" || p.WorkflowSHA != p.Head {
			return errors.New("generated CodeQL context rejected")
		}
	default:
		return errors.New("unsupported event")
	}
	if p.Event != "dynamic" {
		if p.WorkflowSHA != p.ContextSHA || p.WorkflowRef != Repository+"/"+p.Workflow+"@"+p.Ref() || !digestPattern.MatchString(p.WorkflowDigest) {
			return errors.New("immutable workflow context required")
		}
		if p.Event != "pull_request" && p.ContextSHA != p.Head {
			return errors.New("branch event source context differs")
		}
	}
	if p.Workflow == CodeQLWorkflow {
		if p.Event != "dynamic" || p.Job != "analyze" || (p.JobName != "Analyze (actions)" && p.JobName != "Analyze (rust)") {
			return errors.New("original CodeQL analysis job required")
		}
	} else {
		w, ok := WorkflowByPath(p.Workflow)
		if !ok || p.Job != w.Job || p.Event == "dynamic" {
			return errors.New("unsupported native workflow/job")
		}
		if (p.Job == "test" && p.JobName != "test (ubuntu-latest)" && p.JobName != "test (windows-latest)") || (p.Job != "test" && p.JobName != p.Job) {
			return errors.New("original job name required")
		}
	}
	return nil
}

// MatchRunPath accepts the REST API's bare path and documented path@ref forms.
// It never drops an unvalidated suffix or confuses another workflow/ref.
func MatchRunPath(p Plan, path string) bool {
	if path == p.Workflow {
		return true
	}
	if p.Event == "dynamic" {
		return false
	}
	workflow, ref, found := strings.Cut(path, "@")
	if !found || workflow != p.Workflow {
		return false
	}
	return ref == p.Ref() || (p.Event != "pull_request" && ref == p.Branch)
}
func (p Plan) Ref() string {
	if p.Event == "pull_request" {
		return fmt.Sprintf("refs/pull/%d/merge", p.PR)
	}
	return "refs/heads/" + p.Branch
}

// JobLabel selects one workflow run/attempt/job and matrix OS. A listener never
// competes for another workflow's job or the other test matrix entry.
func JobLabel(p Plan) string {
	return fmt.Sprintf("civ6-ci-run-%d-%d-%s-%s", p.RunID, p.Attempt, p.Job, p.OS())
}

func Labels(p Plan) []string {
	if p.Workflow == CodeQLWorkflow {
		return []string{CodeQLLabel}
	}
	if p.JobName == "test (ubuntu-latest)" {
		return []string{"self-hosted", "Linux", "X64", LinuxLabel, JobLabel(p)}
	}
	return []string{"self-hosted", "Windows", "X64", WindowsLabel, JobLabel(p)}
}
func (p Plan) OS() string {
	if p.Workflow == CodeQLWorkflow || p.JobName == "test (ubuntu-latest)" {
		return "Linux"
	}
	return "Windows"
}

// CheckEvent checks the actual SDK context, independently of the controller's
// plan. Generated CodeQL has no repository-managed workflow ref to invent.
func CheckEvent(p Plan, env map[string]string, raw []byte) error {
	if err := p.Validate(); err != nil {
		return err
	}
	want := map[string]string{"GITHUB_REPOSITORY": Repository, "GITHUB_REPOSITORY_ID": strconv.FormatInt(RepoID, 10), "GITHUB_SHA": p.ContextSHA, "GITHUB_RUN_ID": strconv.FormatInt(p.RunID, 10), "GITHUB_RUN_ATTEMPT": strconv.Itoa(p.Attempt), "GITHUB_JOB": p.Job, "RUNNER_OS": p.OS()}
	if p.Event != "dynamic" {
		want["GITHUB_EVENT_NAME"] = p.Event
		want["GITHUB_REF"] = p.Ref()
		want["GITHUB_WORKFLOW_SHA"] = p.WorkflowSHA
		want["GITHUB_WORKFLOW_REF"] = p.WorkflowRef
	}
	for k, v := range want {
		if env[k] != v {
			return fmt.Errorf("actual job context differs: %s", k)
		}
	}
	return checkEventJSON(p, raw)
}
