package main

import (
	"bytes"
	"cc/product-ci/pr148scope"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const repository = "AutoDevJapan/civ6-banseiki"
const repositoryID int64 = 1392152644
const sourceHead = pr148scope.Head
const sourceTree = pr148scope.Tree
const baseHead = pr148scope.Base
const branch = pr148scope.Branch
const routeLabel = "cc-product-2736-1392152644-windows-latest"

var hex32 = regexp.MustCompile(`^[a-f0-9]{32}$`)
var hex64 = regexp.MustCompile(`^[a-f0-9]{64}$`)

// Plan contains no credentials. Parent 2736 supplies fresh API observations and
// an actual workflow digest after its separately reviewed routing commit.
// Head/Tree explicitly pin the routing-only child of the immutable source.
type Plan struct {
	Instance       string `json:"instance"`
	Root           string `json:"root"`
	Repository     string `json:"repository"`
	RepositoryID   int64  `json:"repository_id"`
	PR             int    `json:"pr"`
	Head           string `json:"head"`
	Tree           string `json:"tree"`
	Base           string `json:"base"`
	Branch         string `json:"branch"`
	RunID          int64  `json:"run_id"`
	JobID          int64  `json:"job_id"`
	Attempt        int    `json:"attempt"`
	Job            string `json:"job"`
	JobName        string `json:"job_name"`
	Workflow       string `json:"workflow"`
	PRHead         string `json:"pr_head"`
	WorkflowSHA    string `json:"workflow_sha"`
	WorkflowDigest string `json:"workflow_digest"`
	Label          string `json:"label"`
	RunnerVersion  string `json:"runner_version"`
	RunnerDigest   string `json:"runner_digest"`
	ControllerSID  string `json:"controller_sid"`
	Seconds        int    `json:"seconds"`
	CleanupSeconds int    `json:"cleanup_seconds,omitempty"`
	Fixture        string `json:"fixture,omitempty"`
	RuntimeFixture bool   `json:"runtime_fixture,omitempty"`
}

func (p Plan) name() string    { return "CC-PR148-" + p.Instance }
func (p Plan) runtime() string { return filepath.Join(p.Root, "runtime") }
func (p Plan) runner() string  { return filepath.Join(p.runtime(), "runner") }
func (p Plan) validate() error {
	if !hex32.MatchString(p.Instance) || !filepath.IsAbs(p.Root) || filepath.Clean(p.Root) != p.Root || filepath.Base(p.Root) != "codex-owned-"+p.Instance {
		return errors.New("new safe-delete root and instance required")
	}
	if p.Repository != repository || p.RepositoryID != repositoryID || p.PR != 148 || p.Base != baseHead || p.Branch != branch || p.Label != routeLabel {
		return errors.New("fixed PR148 identity mismatch")
	}
	if p.Seconds < 1 || p.Seconds > 14400 || !regexp.MustCompile(`^S-1-5-21-[0-9]+-[0-9]+-[0-9]+-[0-9]+$`).MatchString(p.ControllerSID) {
		return errors.New("finite deadline and controller SID required")
	}
	if p.CleanupSeconds != 0 && (p.CleanupSeconds < 60 || p.CleanupSeconds > 14400) {
		return errors.New("cleanup deadline must be 60..14400 seconds or zero for 1800")
	}
	if p.Fixture != "" {
		if p.Fixture != "normal" && p.Fixture != "cancel" {
			return errors.New("unknown fixture")
		}
		if p.Head != sourceHead || p.Tree != sourceTree {
			return errors.New("credential-free fixture must retain original source pins")
		}
		return nil
	}
	if p.RuntimeFixture {
		return errors.New("runtime fixture cannot be official execution")
	}
	if !validSHA(p.Head) || p.Head == sourceHead || !validSHA(p.Tree) || p.Tree == sourceTree || p.PRHead != p.Head || p.WorkflowSHA != p.Head {
		return errors.New("explicit new routing-only dispatch source required")
	}
	validJob := map[string]bool{"test": p.Workflow == ".github/workflows/rust.yml", "native-driver": p.Workflow == ".github/workflows/native-driver.yml", "live-chain": p.Workflow == ".github/workflows/live-chain.yml"}
	if !validJob[p.Job] || p.JobName == "" || len(p.JobName) > 200 || p.RunID <= 0 || p.JobID <= 0 || p.Attempt < 1 || !hex64.MatchString(p.WorkflowDigest) || !validSHA(p.PRHead) || !validSHA(p.WorkflowSHA) || !hex64.MatchString(p.RunnerDigest) || !regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`).MatchString(p.RunnerVersion) {
		return errors.New("actual run/attempt/workflow and official runner pin required")
	}
	name := p.Job
	if p.Job == "test" {
		name = "test (windows-latest)"
	}
	if p.JobName != name {
		return errors.New("exact original Windows job name required")
	}
	return nil
}

func readPlan(root string) (p Plan, err error) {
	b, err := os.ReadFile(filepath.Join(root, "plan.json"))
	if err != nil {
		return p, err
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err = d.Decode(&p); err != nil {
		return p, errors.New("invalid plan")
	}
	if d.Decode(new(any)) != io.EOF {
		return p, errors.New("trailing plan data")
	}
	if err = p.validate(); err != nil {
		return p, err
	}
	if p.Root != root {
		return p, errors.New("root mismatch")
	}
	return p, nil
}
func digest(b []byte) string { x := sha256.Sum256(b); return hex.EncodeToString(x[:]) }
func saveNew(path string, v any) error {
	b, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return e
	}
	f, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	_, e = f.Write(append(b, '\n'))
	c := f.Close()
	return errors.Join(e, c)
}
func boundedRead(r io.Reader, n int64) ([]byte, error) {
	b, e := io.ReadAll(io.LimitReader(r, n+1))
	if e == nil && int64(len(b)) > n {
		e = errors.New("input exceeds limit")
	}
	return b, e
}

// No caller-controlled command, executable, environment, host callback, or
// download URL is accepted by this helper.
func configureArgs(p Plan) []string {
	return []string{"configure", "--unattended", "--url", "https://github.com/" + repository, "--name", p.name(), "--labels", p.Label, "--ephemeral", "--no-default-labels", "--disableupdate", "--work", "_work"}
}

func checkEvent(p Plan, env map[string]string, raw []byte) error {
	if p.Fixture != "" || p.validate() != nil {
		return errors.New("official dispatch hook requires a real pinned plan")
	}
	want := map[string]string{"GITHUB_REPOSITORY": repository, "GITHUB_REPOSITORY_ID": fmt.Sprint(repositoryID), "GITHUB_EVENT_NAME": "workflow_dispatch", "GITHUB_SHA": p.Head, "GITHUB_HEAD_REF": "", "GITHUB_BASE_REF": "", "GITHUB_REF": "refs/heads/" + branch, "GITHUB_RUN_ID": fmt.Sprint(p.RunID), "GITHUB_RUN_ATTEMPT": fmt.Sprint(p.Attempt), "GITHUB_JOB": p.Job, "GITHUB_WORKFLOW_SHA": p.WorkflowSHA, "GITHUB_WORKFLOW_REF": repository + "/" + p.Workflow + "@refs/heads/" + branch, "RUNNER_NAME": p.name(), "RUNNER_OS": "Windows"}
	for k, v := range want {
		if env[k] != v {
			return fmt.Errorf("job context rejected: %s", k)
		}
	}
	var ev struct {
		Number     int    `json:"number"`
		Ref        string `json:"ref"`
		Repository struct {
			ID       int64  `json:"id"`
			FullName string `json:"full_name"`
		} `json:"repository"`
		PullRequest struct {
			Head struct {
				SHA, Ref string
				Repo     struct {
					ID       int64  `json:"id"`
					FullName string `json:"full_name"`
				}
			} `json:"head"`
			Base struct {
				SHA, Ref string
				Repo     struct {
					ID       int64  `json:"id"`
					FullName string `json:"full_name"`
				}
			} `json:"base"`
		} `json:"pull_request"`
	}
	if json.Unmarshal(raw, &ev) != nil {
		return errors.New("event JSON rejected")
	}
	if ev.Number != 0 || ev.Repository.ID != repositoryID || ev.Repository.FullName != repository || (ev.Ref != branch && ev.Ref != "refs/heads/"+branch) || ev.PullRequest.Head.SHA != "" {
		return errors.New("event dispatch/source identity rejected")
	}
	return nil
}
func validSHA(s string) bool { return regexp.MustCompile(`^[a-f0-9]{40}$`).MatchString(s) }

func beneath(root, path string) error {
	rel, e := filepath.Rel(root, path)
	if e != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return errors.New("path must be strictly inside owned root")
	}
	return nil
}
