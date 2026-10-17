package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const normalCIWorkflowSHA = "01d3351d4a6bce1faeb7e3b1eaf29555affcc304ea7b91caa979825cc94bc4b0"
const normalCIFailedRun int64 = 38893656960
const normalCIRepo int64 = 1154066087
const normalCIRouteVariable = "CC_PRODUCT_2737_CI"

// An absent switch always restores the original OS route, including after
// merge. A task's ephemeral runner label must never strand main or future PRs.
func normalCIRunnerExpression(r normalCIRoute) string {
	return "${{ vars." + normalCIRouteVariable + " == 'enabled' && github.event_name == 'workflow_dispatch' && github.ref == 'refs/heads/codex/todo2641-http-response-pseudo-20261015' && '" + r.Proposed + "' || '" + r.Previous + "' }}"
}

type normalCIRoute struct {
	Job      string `json:"job"`
	OS       string `json:"required_os"`
	Previous string `json:"previous_label"`
	Proposed string `json:"proposed_label"`
}

// This is a proposal, not an executor. In particular, a route label is never
// evidence that a guest, Cargo admission, Docker daemon or Windows job exists.
type normalCIProposal struct {
	RepoID              int64                 `json:"repo_id"`
	PR                  int                   `json:"pr"`
	SourceSHA           string                `json:"source_sha"`
	SourceTree          string                `json:"source_tree"`
	WorkflowSHA         string                `json:"workflow_sha256"`
	ProposedWorkflowSHA string                `json:"proposed_workflow_sha256"`
	Routes              []normalCIRoute       `json:"routes"`
	WindowsCommands     int                   `json:"windows_cargo_commands"`
	FailedRun           int64                 `json:"failed_run_id"`
	FailedAttempt       int                   `json:"failed_run_attempt"`
	Jobs                []normalCIJob         `json:"original_jobs"`
	BudgetChecks        []normalCIBudgetCheck `json:"unstarted_budget_checks"`
	OriginalLogs        string                `json:"original_logs"`
	SourceRevalidated   bool                  `json:"source_revalidated"`
	ProposedOnly        bool                  `json:"proposal_only"`
	AllChecksSuccess    bool                  `json:"all_required_checks_success"`
	At                  time.Time             `json:"at"`
}

type normalCIBudgetCheck struct {
	ID                int64  `json:"check_id"`
	AnnotationJSONSHA string `json:"annotation_json_value_sha256"`
	Count             int    `json:"annotation_count"`
}

type normalCIJob struct {
	ID         int64    `json:"id"`
	RunID      int64    `json:"run_id"`
	Status     string   `json:"status"`
	Conclusion string   `json:"conclusion"`
	RunnerID   int64    `json:"runner_id"`
	Labels     []string `json:"labels"`
	Steps      []struct {
		Number int `json:"number"`
	} `json:"steps"`
}

func normalCIRoutes() []normalCIRoute {
	return []normalCIRoute{
		{"verify", "Linux", "ubuntu-latest", "cc-product-2737-1154066087-linux"},
		{"windows-native-check", "Windows", "windows-2025", "cc-product-2737-1154066087-windows"},
		{"pr-tree-receipt", "Linux", "ubuntu-latest", "cc-product-2737-1154066087-linux"},
		{"ci-gate", "Linux", "ubuntu-latest", "cc-product-2737-1154066087-linux"},
	}
}

// Reject aliases, duplicate mapping keys and multiple documents rather than
// selecting one interpretation of an executable workflow.
func normalCIYAML(data []byte) (*yaml.Node, error) {
	if len(data) == 0 || len(data) > 1<<20 {
		return nil, errors.New("normal CI workflow size rejected")
	}
	d := yaml.NewDecoder(bytes.NewReader(data))
	var root, extra yaml.Node
	if d.Decode(&root) != nil || len(root.Content) != 1 || root.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("normal CI workflow structure rejected")
	}
	if d.Decode(&extra) != io.EOF {
		return nil, errors.New("normal CI multiple documents rejected")
	}
	var visit func(*yaml.Node) error
	visit = func(n *yaml.Node) error {
		if n.Kind == yaml.AliasNode || n.Anchor != "" {
			return errors.New("normal CI aliases rejected")
		}
		if n.Kind == yaml.MappingNode {
			seen := map[string]bool{}
			for i := 0; i < len(n.Content); i += 2 {
				k := n.Content[i]
				if k.Kind != yaml.ScalarNode || k.Tag != "!!str" || seen[k.Value] {
					return errors.New("normal CI mapping key rejected")
				}
				seen[k.Value] = true
			}
		}
		for _, c := range n.Content {
			if e := visit(c); e != nil {
				return e
			}
		}
		return nil
	}
	if e := visit(&root); e != nil {
		return nil, e
	}
	return root.Content[0], nil
}

func yamlField(n *yaml.Node, key string) *yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}

// Replace only the four original plain scalar tokens. All other bytes,
// including conditions, OS-specific steps, secrets, permissions, timeouts,
// caches and the exact Docker parity invocation, remain byte-for-byte equal.
// The proposed bytes exist in RAM only; the caller must not persist source.
func proposeNormalCIRoute(data []byte, expected string) ([]byte, error) {
	if digest(data) != expected {
		return nil, errors.New("fixed normal CI workflow digest changed")
	}
	root, e := normalCIYAML(data)
	if e != nil {
		return nil, e
	}
	jobs := yamlField(root, "jobs")
	routes := normalCIRoutes()
	if jobs == nil || jobs.Kind != yaml.MappingNode || len(jobs.Content) != 2*len(routes) {
		return nil, errors.New("fixed normal CI job set changed")
	}
	type replacement struct {
		start, end int
		value      string
	}
	var edits []replacement
	for _, r := range routes {
		job := yamlField(jobs, r.Job)
		n := yamlField(job, "runs-on")
		if n == nil || n.Kind != yaml.ScalarNode || n.Tag != "!!str" || n.Style != 0 || n.Value != r.Previous {
			return nil, errors.New("fixed normal CI original runner route changed")
		}
		// YAML line/column are Unicode positions. Walk runes before indexing bytes.
		line, column, offset := 1, 1, 0
		for i, ch := range string(data) {
			if line == n.Line && column == n.Column {
				offset = i
				break
			}
			if ch == '\n' {
				line, column = line+1, 1
			} else {
				column++
			}
		}
		if offset+len(r.Previous) > len(data) || string(data[offset:offset+len(r.Previous)]) != r.Previous {
			return nil, errors.New("normal CI runner token position unverified")
		}
		edits = append(edits, replacement{offset, offset + len(r.Previous), normalCIRunnerExpression(r)})
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].start < edits[j].start })
	var out bytes.Buffer
	previous := 0
	for _, edit := range edits {
		if edit.start < previous {
			return nil, errors.New("normal CI overlapping runner token")
		}
		out.Write(data[previous:edit.start])
		out.WriteString(edit.value)
		previous = edit.end
	}
	out.Write(data[previous:])
	parsed, e := normalCIYAML(out.Bytes())
	if e != nil {
		return nil, e
	}
	for _, r := range routes {
		if n := yamlField(yamlField(yamlField(parsed, "jobs"), r.Job), "runs-on"); n == nil || n.Value != normalCIRunnerExpression(r) {
			return nil, errors.New("normal CI prepared route reparse rejected")
		}
	}
	return out.Bytes(), nil
}

func countWindowsCargo(root *yaml.Node) (int, error) {
	job := yamlField(yamlField(root, "jobs"), "windows-native-check")
	steps := yamlField(job, "steps")
	if steps == nil || steps.Kind != yaml.SequenceNode {
		return 0, errors.New("normal CI Windows steps unavailable")
	}
	count := 0
	for _, step := range steps.Content {
		if run := yamlField(step, "run"); run != nil && run.Kind == yaml.ScalarNode {
			for _, line := range strings.Split(run.Value, "\n") {
				if strings.HasPrefix(strings.TrimSpace(line), "cargo ") {
					count++
				}
			}
		}
	}
	if count != 13 {
		return count, errors.New("fixed normal CI Windows thirteen commands changed")
	}
	return count, nil
}

func inspectNormalCI(ctx context.Context, c candidate, read api) (normalCIProposal, error) {
	return inspectNormalCIWithPolicy(ctx, c, read, normalCIWorkflowSHA)
}

func inspectNormalCIWithPolicy(ctx context.Context, c candidate, read api, expectedWorkflow string) (normalCIProposal, error) {
	p := normalCIProposal{RepoID: c.ID, PR: c.PR, SourceSHA: c.SHA, ProposedOnly: true, FailedRun: normalCIFailedRun, At: time.Now().UTC(), OriginalLogs: "not-requested-unstarted-jobs"}
	if c.ID != normalCIRepo || c.PR != 3613 || c.SHA != "b194d66524ef86330bb73ca3f22f9e5dd6ae0a07" {
		return p, errors.New("unknown normal CI candidate")
	}
	r, e := verify(ctx, c, read)
	if e != nil {
		return p, e
	}
	var pr struct{ Head struct{ Ref string } }
	if e = read(ctx, fmt.Sprintf("repos/%s/pulls/%d", r.FullName, c.PR), &pr); e != nil || pr.Head.Ref != "codex/todo2641-http-response-pseudo-20261015" {
		return p, errors.New("fixed normal CI branch changed")
	}
	var commit struct{ Tree struct{ SHA string } }
	if e = read(ctx, "repos/"+r.FullName+"/git/commits/"+c.SHA, &commit); e != nil {
		return p, e
	}
	if commit.Tree.SHA != "f7eea82c96449b7749f98dd6c4b0344b64f1d651" {
		return p, errors.New("fixed normal CI source tree changed")
	}
	p.SourceTree = commit.Tree.SHA
	var file struct{ Content, Encoding, Type string }
	if e = read(ctx, "repos/"+r.FullName+"/contents/.github/workflows/ci.yml?ref="+c.SHA, &file); e != nil {
		return p, e
	}
	data, e := base64.StdEncoding.DecodeString(strings.ReplaceAll(file.Content, "\n", ""))
	if e != nil || file.Encoding != "base64" || file.Type != "file" {
		return p, errors.New("normal CI workflow content rejected")
	}
	proposed, e := proposeNormalCIRoute(data, expectedWorkflow)
	if e != nil {
		return p, e
	}
	root, _ := normalCIYAML(data)
	p.WindowsCommands, e = countWindowsCargo(root)
	if e != nil {
		return p, e
	}
	p.WorkflowSHA, p.ProposedWorkflowSHA = digest(data), digest(proposed)
	p.Routes = normalCIRoutes()
	var run struct {
		codeQLRun
		Conclusion string
	}
	if e = read(ctx, fmt.Sprintf("repos/%s/actions/runs/%d", r.FullName, p.FailedRun), &run); e != nil {
		return p, e
	}
	if run.ID != p.FailedRun || run.Repository.ID != c.ID || run.HeadRepo.ID != c.ID || run.Repository.FullName != r.FullName || run.HeadRepo.FullName != r.FullName || run.HeadRepo.Fork || run.HeadSHA != c.SHA || run.HeadBranch != pr.Head.Ref || run.Path != ".github/workflows/ci.yml" || run.Event != "pull_request" || run.Status != "completed" || run.Conclusion != "failure" || run.RunAttempt != 1 {
		return p, errors.New("original normal CI failure identity changed")
	}
	p.FailedAttempt = run.RunAttempt
	var jobs struct {
		Total int `json:"total_count"`
		Jobs  []normalCIJob
	}
	if e = read(ctx, fmt.Sprintf("repos/%s/actions/runs/%d/attempts/%d/jobs?per_page=100", r.FullName, p.FailedRun, p.FailedAttempt), &jobs); e != nil {
		return p, e
	}
	if jobs.Total != 4 || len(jobs.Jobs) != jobs.Total {
		return p, errors.New("original normal CI job inventory changed")
	}
	expectedJobs := map[int64]string{116948883594: "failure", 116948895629: "failure", 116948897372: "skipped", 116948897826: "skipped"}
	for _, job := range jobs.Jobs {
		if job.RunID != p.FailedRun || job.Status != "completed" || expectedJobs[job.ID] == "" || job.Conclusion != expectedJobs[job.ID] || len(job.Steps) != 0 || job.RunnerID != 0 {
			return p, errors.New("original normal CI unstarted jobs changed")
		}
		delete(expectedJobs, job.ID)
		if job.Conclusion == "failure" {
			var raw json.RawMessage
			if e = read(ctx, fmt.Sprintf("repos/%s/check-runs/%d/annotations?per_page=100", r.FullName, job.ID), &raw); e != nil {
				return p, e
			}
			var annotations []struct {
				Level   string `json:"annotation_level"`
				Message string `json:"message"`
			}
			if json.Unmarshal(raw, &annotations) != nil || len(annotations) == 0 || len(annotations) >= 100 {
				return p, errors.New("normal CI annotation inventory incomplete")
			}
			budget := false
			for _, a := range annotations {
				if a.Level == "failure" {
					if a.Message != "The job was not started because an Actions budget is preventing further use." {
						return p, errors.New("normal CI failure is not the recorded budget refusal")
					}
					budget = true
				}
			}
			if !budget {
				return p, errors.New("normal CI budget annotation missing")
			}
			p.BudgetChecks = append(p.BudgetChecks, normalCIBudgetCheck{job.ID, digest(raw), len(annotations)})
		}
	}
	if len(p.BudgetChecks) != 2 {
		return p, errors.New("original normal CI failure set changed")
	}
	p.Jobs = jobs.Jobs
	if _, e = verify(ctx, c, read); e != nil {
		return p, e
	}
	if e = read(ctx, fmt.Sprintf("repos/%s/pulls/%d", r.FullName, c.PR), &pr); e != nil || pr.Head.Ref != "codex/todo2641-http-response-pseudo-20261015" {
		return p, errors.New("fixed normal CI branch changed after inspection")
	}
	p.SourceRevalidated = true
	return p, nil
}

func saveNormalCIProposal(ctx context.Context, dir string) error {
	// No workflow mutation, dispatch, registration, checkout or success status.
	c := candidate{normalCIRepo, 3613, "b194d66524ef86330bb73ca3f22f9e5dd6ae0a07"}
	p, e := inspectNormalCI(ctx, c, get)
	if e != nil {
		return e
	}
	f, e := os.OpenFile(filepath.Join(dir, "normal-ci-proposal.private.json"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return errors.New("new owned normal CI evidence directory required")
	}
	e = json.NewEncoder(f).Encode(p)
	if e == nil {
		e = f.Sync()
	}
	return errors.Join(e, f.Close())
}
