package pressci

import (
	"bytes"
	sourcepacket "cc/ci-source-2737"
	"cc/product-ci/normalci"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

type Pin struct {
	Source           sourcepacket.Pin
	RunID            int64
	Attempt          int
	Role, RouteLease string
	RunnerID         int64
	RunnerName       string
}

func readWorkflow(ctx context.Context, sha, name string, read normalci.API) ([]byte, error) {
	var c struct{ SHA, Content, Encoding string }
	if read(ctx, "repos/"+Repository+"/contents/.github/workflows/"+name+"?ref="+sha, &c) != nil || c.Encoding != "base64" {
		return nil, errors.New("Press workflow read rejected")
	}
	b, e := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(c.Content), ""))
	if e != nil || len(b) == 0 || len(b) > 1<<20 || blobID(b) != c.SHA {
		return nil, errors.New("Press workflow bytes rejected")
	}
	return b, nil
}

func Plan(ctx context.Context, p Pin, read normalci.API) (normalci.Binding, error) {
	return plan(ctx, p, read, workflows())
}

// All request-selected values are immutable expectations. This read-only
// resolver cannot publish routes, choose a new candidate, register or start a
// runner. The returned Binding still requires the guest's physical proof.
func plan(ctx context.Context, p Pin, read normalci.API, originals []workflow) (normalci.Binding, error) {
	stage := "expectations"
	bad := func() (normalci.Binding, error) {
		return normalci.Binding{}, fmt.Errorf("Press binding rejected at %s", stage)
	}
	profile, ok := p.Source.Profile()
	role, roleOK := RoleJob(p.Source.Workflow, p.Role)
	if !ok || p.Source.Product != sourcepacket.PressProduct || profile.RepoID != RepoID || profile.PR != PR || !roleOK || p.RunID < 1 || p.Attempt < 1 || LeaseValue(p.RouteLease) == "" || p.RunnerID < 0 || (p.RunnerID == 0 && p.RunnerName != "") {
		return bad()
	}
	event := "pull_request"
	if p.Source.Workflow == "local-checks.yml" {
		event = "workflow_dispatch"
		if p.Source.PullHeadSHA != "" {
			return bad()
		}
	} else if p.Source.PullHeadSHA == "" {
		return bad()
	}
	stage = "source-identity"
	if sourcepacket.VerifyRemote(ctx, p.Source, read) != nil {
		return bad()
	}
	stage = "route-owner"
	var variable struct{ Name, Value string }
	if read(ctx, "repos/"+Repository+"/actions/variables/"+RouteVariable, &variable) != nil || variable.Name != RouteVariable || variable.Value != LeaseValue(p.RouteLease) {
		return bad()
	}
	stage = "workflow-contract"
	var selected Workflow
	var selectedBytes []byte
	for _, w := range originals {
		original, e := readWorkflow(ctx, Head, w.name, read)
		if e != nil || blobID(original) != w.blob {
			return bad()
		}
		proposed, e := prepareRoutes(w.name, original, p.RouteLease)
		if e != nil {
			return bad()
		}
		candidate, e := readWorkflow(ctx, p.Source.CandidateSHA(), w.name, read)
		if e != nil || !bytes.Equal(candidate, proposed) {
			return bad()
		}
		if w.name == p.Source.Workflow {
			selected, e = outlineWorkflow(original, w.name)
			if e != nil {
				return bad()
			}
			selectedBytes = proposed
		}
	}
	if selected.Path == "" || len(originals) != 3 {
		return bad()
	}
	if verifyCandidateFiles(ctx, p.Source.CandidateSHA(), read) != nil {
		return bad()
	}
	execution, e := readWorkflow(ctx, p.Source.HeadSHA, p.Source.Workflow, read)
	if e != nil || !bytes.Equal(execution, selectedBytes) {
		return bad()
	}
	h := sha256.Sum256(execution)
	if hex.EncodeToString(h[:]) != p.Source.WorkflowSHA256 {
		return bad()
	}
	stage = "official-run"
	var run struct {
		ID                  int64
		RunAttempt          int `json:"run_attempt"`
		Path, Event, Status string
		HeadSHA             string `json:"head_sha"`
		HeadBranch          string `json:"head_branch"`
		Repository          normalci.Repository
		HeadRepo            normalci.Repository    `json:"head_repository"`
		PullRequests        []struct{ Number int } `json:"pull_requests"`
	}
	if read(ctx, fmt.Sprintf("repos/%s/actions/runs/%d", Repository, p.RunID), &run) != nil || run.ID != p.RunID || run.RunAttempt != p.Attempt || run.Path != profile.Workflow || run.Event != event || run.HeadSHA != p.Source.CandidateSHA() || run.HeadBranch != Ref || run.Repository.ID != RepoID || run.Repository.FullName != Repository || run.Repository.Fork || run.HeadRepo.ID != RepoID || run.HeadRepo.FullName != Repository || run.HeadRepo.Fork || (run.Status != "queued" && run.Status != "in_progress" && run.Status != "waiting") {
		return bad()
	}
	if event == "pull_request" && (len(run.PullRequests) != 1 || run.PullRequests[0].Number != PR) {
		return bad()
	}
	stage = "official-job"
	var js struct {
		Total int `json:"total_count"`
		Jobs  []struct {
			ID           int64
			RunID        int64  `json:"run_id"`
			Attempt      int    `json:"run_attempt"`
			HeadSHA      string `json:"head_sha"`
			Name, Status string
			RunnerID     int64  `json:"runner_id"`
			RunnerName   string `json:"runner_name"`
			Labels       []string
		}
	}
	if read(ctx, fmt.Sprintf("repos/%s/actions/runs/%d/attempts/%d/jobs?per_page=100", Repository, p.RunID, p.Attempt), &js) != nil || js.Total < 1 || js.Total > len(selected.Jobs) || js.Total != len(js.Jobs) {
		return bad()
	}
	known := map[string]Job{}
	for _, j := range selected.Jobs {
		known[j.Name] = j
	}
	seen := map[string]bool{}
	seenIDs := map[int64]bool{}
	b := normalci.Binding{RepoID: RepoID, PR: PR, RunID: p.RunID, Attempt: p.Attempt, SHA: p.Source.HeadSHA, Tree: p.Source.TreeSHA, WorkflowSHA: p.Source.WorkflowSHA256, Role: p.Role, Label: RuntimeLabel(p.Source.Workflow, p.Role, p.RunID, p.Attempt), OS: role.OS, JobName: role.Name, TimeoutMinutes: role.TimeoutMinutes}
	for _, j := range js.Jobs {
		expected, exists := known[j.Name]
		if !exists || seen[j.Name] || j.ID < 1 || seenIDs[j.ID] || j.RunID != p.RunID || j.Attempt != p.Attempt || j.HeadSHA != p.Source.CandidateSHA() || len(j.Labels) != 1 || j.Labels[0] != RuntimeLabel(p.Source.Workflow, Role(expected), p.RunID, p.Attempt) {
			return bad()
		}
		seen[j.Name], seenIDs[j.ID] = true, true
		if j.Name == b.JobName {
			b.JobID = j.ID
			job := normalci.OfficialJob{ID: j.ID, RunID: j.RunID, RunnerID: j.RunnerID, Name: j.Name, Status: j.Status, RunnerName: j.RunnerName, Labels: j.Labels}
			if p.RunnerID == 0 {
				if j.Status != "queued" || j.RunnerID != 0 || j.RunnerName != "" {
					return bad()
				}
			} else if normalci.VerifyRunnerClaim(b, job, p.RunnerID, p.RunnerName) != nil {
				return bad()
			}
		}
	}
	if b.JobID < 1 {
		return bad()
	}
	stage = "source-revalidation"
	if sourcepacket.VerifyRemote(ctx, p.Source, read) != nil {
		return bad()
	}
	return b, nil
}

// Only the three route blobs may differ from the exact reviewed candidate.
// Merge-base changes are represented by the separately pinned execution tree;
// they must never be silently attributed to the candidate's original source.
func verifyCandidateFiles(ctx context.Context, sha string, read normalci.API) error {
	var c struct {
		SHA  string
		Tree struct{ SHA string }
	}
	if read(ctx, "repos/"+Repository+"/git/commits/"+sha, &c) != nil || c.SHA != sha || !normalci.Revision(c.Tree.SHA) {
		return errors.New("Press candidate commit rejected")
	}
	type entry struct{ Path, Mode, Type, SHA string }
	readTree := func(tree string) (map[string]entry, error) {
		var t struct {
			SHA       string
			Truncated bool
			Tree      []entry
		}
		if read(ctx, "repos/"+Repository+"/git/trees/"+tree+"?recursive=1", &t) != nil || t.SHA != tree || t.Truncated || len(t.Tree) > 200000 {
			return nil, errors.New("Press candidate tree rejected")
		}
		m := map[string]entry{}
		for _, e := range t.Tree {
			if e.Type == "tree" {
				continue
			}
			if _, exists := m[e.Path]; exists {
				return nil, errors.New("Press duplicate tree path")
			}
			m[e.Path] = e
		}
		return m, nil
	}
	original, e := readTree(Tree)
	if e != nil {
		return e
	}
	current, e := readTree(c.Tree.SHA)
	if e != nil {
		return e
	}
	if len(original) == 0 || len(original) != len(current) {
		return errors.New("Press candidate files changed")
	}
	allowed := map[string]bool{}
	for _, w := range workflows() {
		allowed[".github/workflows/"+w.name] = true
	}
	for path, before := range original {
		after, ok := current[path]
		if !ok || before.Path != after.Path || before.Type != after.Type || before.Mode != after.Mode || (!allowed[path] && before.SHA != after.SHA) {
			return errors.New("Press candidate non-route change rejected")
		}
	}
	return nil
}
