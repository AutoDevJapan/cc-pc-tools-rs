package contract

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

type repository struct {
	ID       int64  `json:"id"`
	FullName string `json:"full_name"`
	Private  bool   `json:"private"`
	Fork     bool   `json:"fork"`
	Owner    struct {
		Login string `json:"login"`
	} `json:"owner"`
	Permissions struct {
		Admin bool `json:"admin"`
	} `json:"permissions"`
}
type pull struct {
	Number   int    `json:"number"`
	State    string `json:"state"`
	Merged   bool   `json:"merged"`
	MergeSHA string `json:"merge_commit_sha"`
	Head     struct {
		SHA, Ref string
		Repo     repository
	} `json:"head"`
	Base struct {
		SHA, Ref string
		Repo     repository
	} `json:"base"`
}
type commit struct {
	SHA  string `json:"sha"`
	Tree struct {
		SHA string `json:"sha"`
	} `json:"tree"`
	Parents []struct {
		SHA string `json:"sha"`
	} `json:"parents"`
}
type treeEntry struct{ Path, Mode, Type, SHA string }
type treeReply struct {
	SHA       string
	Truncated bool
	Entries   []treeEntry `json:"tree"`
}
type Job struct {
	ID                       int64 `json:"id"`
	RunID                    int64 `json:"run_id"`
	RunAttempt               int   `json:"run_attempt"`
	Name, Status, Conclusion string
	Labels                   []string                                    `json:"labels"`
	RunnerName               string                                      `json:"runner_name"`
	Steps                    []struct{ Name, Status, Conclusion string } `json:"steps"`
}
type Run struct {
	ID                  int64 `json:"id"`
	RunAttempt          int   `json:"run_attempt"`
	Path, Event, Status string
	HeadSHA             string     `json:"head_sha"`
	HeadBranch          string     `json:"head_branch"`
	Repository          repository `json:"repository"`
	HeadRepo            repository `json:"head_repository"`
	PullRequests        []struct {
		Number int `json:"number"`
	} `json:"pull_requests"`
}

func blobID(b []byte) string {
	h := sha1.New()
	fmt.Fprintf(h, "blob %d\x00", len(b))
	h.Write(b)
	return hex.EncodeToString(h.Sum(nil))
}
func contents(ctx context.Context, read ReadAPI, path, head string) ([]byte, error) {
	var v struct{ Type, Encoding, Content, SHA string }
	if e := read(ctx, "repos/"+Repository+"/contents/"+path+"?ref="+head, &v); e != nil {
		return nil, e
	}
	if v.Type != "file" || v.Encoding != "base64" || len(v.Content) > 2<<20 {
		return nil, errors.New("workflow response rejected")
	}
	b, e := base64.StdEncoding.DecodeString(strings.ReplaceAll(v.Content, "\n", ""))
	if e != nil || len(b) > 1<<20 || blobID(b) != v.SHA {
		return nil, errors.New("workflow bytes differ from Git blob")
	}
	return b, nil
}
func sameRepo(r repository) bool { return r.ID == RepoID && r.FullName == Repository && !r.Fork }
func verifyCurrent(ctx context.Context, p Plan, read ReadAPI) (bool, error) {
	var original pull
	if e := read(ctx, "repos/"+Repository+"/pulls/149", &original); e != nil {
		return false, e
	}
	if original.Number != 149 || !sameRepo(original.Head.Repo) || !sameRepo(original.Base.Repo) || original.Head.Ref != OriginalBranch || original.Base.Ref != "main" {
		return false, errors.New("original PR identity differs")
	}
	initial := p.PR == 149 && !original.Merged
	if initial {
		if original.State != "open" || original.Head.SHA != p.Head || original.Base.SHA != OriginalBase || p.Branch != OriginalBranch {
			return false, errors.New("current original PR source differs")
		}
	} else if !original.Merged || original.State != "closed" || !ValidSHA(original.MergeSHA) {
		return false, errors.New("normal continuation requires actual PR149 merge")
	}
	if p.PR > 0 {
		current := original
		if p.PR != 149 {
			if e := read(ctx, fmt.Sprintf("repos/%s/pulls/%d", Repository, p.PR), &current); e != nil {
				return false, e
			}
		}
		if current.Number != p.PR || current.State != "open" || current.Head.SHA != p.Head || current.Head.Ref != p.Branch || !sameRepo(current.Head.Repo) || !sameRepo(current.Base.Repo) || current.Base.Ref != "main" {
			return false, errors.New("current same-repository PR rejected")
		}
		if p.Event == "pull_request" && current.MergeSHA != p.ContextSHA {
			return false, errors.New("actual PR merge context differs")
		}
		if p.Event == "dynamic" && p.ContextSHA != p.Head && p.ContextSHA != current.MergeSHA {
			return false, errors.New("generated PR context is neither current head nor actual merge")
		}
	} else if p.Branch != "main" {
		return false, errors.New("normal continuation requires main")
	}
	var ref struct{ Object struct{ SHA string } }
	if e := read(ctx, "repos/"+Repository+"/git/ref/heads/"+branchPath(p.Branch), &ref); e != nil {
		return false, e
	}
	if ref.Object.SHA != p.Head {
		return false, errors.New("obsolete branch source rejected")
	}
	if !initial {
		// Require the new routing to be in the actual main lineage, not merely a
		// coincidentally matching branch name. A PR's base must include that merge.
		target := p.Head
		if p.PR > 0 {
			var pr pull
			if e := read(ctx, fmt.Sprintf("repos/%s/pulls/%d", Repository, p.PR), &pr); e != nil {
				return false, e
			}
			target = pr.Base.SHA
		}
		var cmp struct{ Status string }
		if e := read(ctx, "repos/"+Repository+"/compare/"+original.MergeSHA+"..."+target, &cmp); e != nil {
			return false, e
		}
		if cmp.Status != "ahead" && cmp.Status != "identical" {
			return false, errors.New("routing merge is absent from continuation base")
		}
	}
	return initial, nil
}

// Verify admits only current source, including terminal jobs whose source is
// still current. After a branch advances/closes, cleanup uses ObserveJob and
// owned namespace proofs independently; it must not depend on current source.
func Verify(ctx context.Context, p Plan, read ReadAPI) error {
	if e := p.Validate(); e != nil {
		return e
	}
	var r repository
	if e := read(ctx, fmt.Sprintf("repositories/%d", RepoID), &r); e != nil {
		return e
	}
	if !sameRepo(r) || r.Owner.Login != "AutoDevJapan" || !r.Permissions.Admin {
		return errors.New("explicit owned repository rejected")
	}
	initial, e := verifyCurrent(ctx, p, read)
	if e != nil {
		return e
	}
	var c commit
	if e = read(ctx, "repos/"+Repository+"/git/commits/"+p.Head, &c); e != nil {
		return e
	}
	if c.SHA != p.Head || c.Tree.SHA != p.Tree {
		return errors.New("fixed source commit/tree differs")
	}
	if initial && (len(c.Parents) != 1 || c.Parents[0].SHA != OriginalHead) {
		return errors.New("initial routing commit must retain sole original parent")
	}
	expected := map[string]string{}
	for _, w := range workflows {
		original, e := contents(ctx, read, w.Path, OriginalHead)
		if e != nil {
			return e
		}
		want, e := Propose(original, w)
		if e != nil {
			return e
		}
		got, e := contents(ctx, read, w.Path, p.Head)
		if e != nil {
			return e
		}
		if !bytes.Equal(want, got) {
			return errors.New("source workflow differs from reviewed continuation template")
		}
		expected[w.Path] = blobID(got)
		if p.Event != "dynamic" {
			actual, e := contents(ctx, read, w.Path, p.WorkflowSHA)
			if e != nil {
				return e
			}
			if !bytes.Equal(actual, got) {
				return errors.New("actual executable workflow differs from reviewed source")
			}
			if w.Path == p.Workflow && Digest(actual) != p.WorkflowDigest {
				return errors.New("pinned workflow digest differs")
			}
		}
	}
	if initial {
		var a, b treeReply
		if e = read(ctx, "repos/"+Repository+"/git/trees/"+OriginalTree+"?recursive=1", &a); e != nil {
			return e
		}
		if e = read(ctx, "repos/"+Repository+"/git/trees/"+p.Tree+"?recursive=1", &b); e != nil {
			return e
		}
		if e = checkTree(a, b, p.Tree, expected); e != nil {
			return e
		}
	}
	var run Run
	if e = read(ctx, fmt.Sprintf("repos/%s/actions/runs/%d", Repository, p.RunID), &run); e != nil {
		return e
	}
	if run.ID != p.RunID || run.RunAttempt != p.Attempt || run.HeadSHA != p.Head || run.HeadBranch != p.Branch || run.Event != p.Event || !MatchRunPath(p, run.Path) || !sameRepo(run.Repository) || !sameRepo(run.HeadRepo) {
		return errors.New("official immutable run differs")
	}
	if run.Status != "queued" && run.Status != "in_progress" && run.Status != "waiting" && run.Status != "pending" && run.Status != "completed" {
		return errors.New("unsupported official run state")
	}
	if p.Event == "pull_request" {
		found := false
		for _, pr := range run.PullRequests {
			if pr.Number == p.PR {
				found = true
			}
		}
		if !found {
			return errors.New("official run PR association differs")
		}
	}
	if p.Event == "dynamic" {
		if e = verifyCodeQL(ctx, read); e != nil {
			return e
		}
	}
	var jobs struct {
		Total int   `json:"total_count"`
		Jobs  []Job `json:"jobs"`
	}
	if e = read(ctx, fmt.Sprintf("repos/%s/actions/runs/%d/attempts/%d/jobs?per_page=100", Repository, p.RunID, p.Attempt), &jobs); e != nil {
		return e
	}
	if jobs.Total != len(jobs.Jobs) || jobs.Total < 1 || jobs.Total > 3 {
		return errors.New("complete bounded job inventory required")
	}
	found := false
	names := map[string]bool{}
	for _, j := range jobs.Jobs {
		if j.RunID != p.RunID || j.RunAttempt != p.Attempt || names[j.Name] {
			return errors.New("job run/attempt or unique name differs")
		}
		names[j.Name] = true
		q := p
		q.JobName = j.Name
		if p.Workflow == CodeQLWorkflow && j.Name == "Adjust Configuration" {
			continue
		}
		if p.Job == "test" && j.Name != "test (ubuntu-latest)" && j.Name != "test (windows-latest)" {
			return errors.New("original OS matrix differs")
		}
		if p.Workflow == CodeQLWorkflow && j.Name != "Analyze (actions)" && j.Name != "Analyze (rust)" {
			return errors.New("CodeQL language coverage differs")
		}
		if !equalLabels(j.Labels, Labels(q)) {
			return errors.New("honest dedicated runner labels required")
		}
		if j.ID == p.JobID {
			if j.Name != p.JobName || (j.Status != "queued" && j.Status != "in_progress" && j.Status != "completed" && j.Status != "waiting" && j.Status != "pending") {
				return errors.New("target official job differs")
			}
			found = true
		}
	}
	if !found || (p.Job == "test" && (jobs.Total != 2 || !names["test (ubuntu-latest)"] || !names["test (windows-latest)"])) || (p.Workflow == CodeQLWorkflow && (!names["Analyze (actions)"] || !names["Analyze (rust)"])) || (p.Job != "test" && p.Workflow != CodeQLWorkflow && (jobs.Total != 1 || !names[p.Job])) {
		return errors.New("full original job coverage required")
	}
	return nil
}

func branchPath(branch string) string {
	parts := strings.Split(branch, "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	return strings.Join(parts, "/")
}
func equalLabels(a, b []string) bool {
	a = append([]string(nil), a...)
	b = append([]string(nil), b...)
	sort.Strings(a)
	sort.Strings(b)
	return strings.Join(a, "\x00") == strings.Join(b, "\x00")
}
func verifyCodeQL(ctx context.Context, read ReadAPI) error {
	var s struct {
		State       string
		Languages   []string
		QuerySuite  string `json:"query_suite"`
		ThreatModel string `json:"threat_model"`
		RunnerType  string `json:"runner_type"`
		RunnerLabel string `json:"runner_label"`
	}
	if e := read(ctx, "repos/"+Repository+"/code-scanning/default-setup", &s); e != nil {
		return e
	}
	sort.Strings(s.Languages)
	if s.State != "configured" || strings.Join(s.Languages, ",") != "actions,rust" || s.QuerySuite != "default" || s.ThreatModel != "remote" || s.RunnerType != "labeled" || s.RunnerLabel != CodeQLLabel {
		return errors.New("original CodeQL settings differ")
	}
	return nil
}

// ObserveJob validates the immutable official job separately from its run.
// Helpers use the unique instance runner name to distinguish a same-run matrix
// assignment; GITHUB_JOB alone is shared by all matrix expansions.
func ObserveJob(ctx context.Context, p Plan, read ReadAPI) (Job, error) {
	var j Job
	if e := p.Validate(); e != nil {
		return j, e
	}
	if e := read(ctx, fmt.Sprintf("repos/%s/actions/jobs/%d", Repository, p.JobID), &j); e != nil {
		return j, e
	}
	if j.ID != p.JobID || j.RunID != p.RunID || j.RunAttempt != p.Attempt || j.Name != p.JobName || !equalLabels(j.Labels, Labels(p)) {
		return Job{}, errors.New("official job identity differs")
	}
	return j, nil
}

func VerifyAssigned(ctx context.Context, p Plan, runnerName string, read ReadAPI) error {
	if runnerName == "" {
		return errors.New("unique instance runner name required")
	}
	if e := Verify(ctx, p, read); e != nil {
		return e
	}
	j, e := ObserveJob(ctx, p, read)
	if e != nil {
		return e
	}
	if j.RunnerName != runnerName || (j.Status != "in_progress" && j.Status != "completed") {
		return errors.New("actual assigned runner differs")
	}
	return nil
}

func RequireSuccess(ctx context.Context, p Plan, runnerName string, read ReadAPI) error {
	if e := VerifyAssigned(ctx, p, runnerName, read); e != nil {
		return e
	}
	j, e := ObserveJob(ctx, p, read)
	if e != nil {
		return e
	}
	if j.Status != "completed" || j.Conclusion != "success" || len(j.Steps) == 0 {
		return errors.New("official target job success not confirmed")
	}
	return nil
}
func checkTree(a, b treeReply, wantTree string, expected map[string]string) error {
	if a.Truncated || b.Truncated || a.SHA != OriginalTree || b.SHA != wantTree || len(a.Entries) != len(b.Entries) || len(expected) != 3 {
		return errors.New("complete original source tree required")
	}
	old := map[string]treeEntry{}
	for _, x := range a.Entries {
		if x.Path == "" || !ValidSHA(x.SHA) || old[x.Path].Path != "" {
			return errors.New("invalid original source tree")
		}
		old[x.Path] = x
	}
	count := 0
	for _, y := range b.Entries {
		x, ok := old[y.Path]
		if !ok || x.Mode != y.Mode || x.Type != y.Type || !ValidSHA(y.SHA) {
			return errors.New("source path/mode/type differs")
		}
		delete(old, y.Path)
		if want, ok := expected[y.Path]; ok {
			if y.Type != "blob" || y.SHA != want || x.SHA == y.SHA {
				return errors.New("initial workflow tree differs")
			}
			count++
		} else if x.SHA != y.SHA && !(y.Type == "tree" && (y.Path == ".github" || y.Path == ".github/workflows")) {
			return errors.New("unrelated original source changed")
		}
	}
	if len(old) != 0 || count != 3 {
		return errors.New("initial workflow delta incomplete")
	}
	return nil
}
func checkEventJSON(p Plan, raw []byte) error {
	if p.Event == "dynamic" {
		return nil
	} // Generated events have no stable public JSON schema; live API and actual SDK job context bind them.
	if len(raw) == 0 || len(raw) > 1<<20 {
		return errors.New("actual event payload unavailable")
	}
	var e struct {
		Number      int
		Ref, After  string
		Repository  repository
		PullRequest pull `json:"pull_request"`
	}
	if json.Unmarshal(raw, &e) != nil || !sameRepo(e.Repository) {
		return errors.New("actual event repository differs")
	}
	if p.Event == "pull_request" {
		if e.Number != p.PR || !sameRepo(e.PullRequest.Head.Repo) || !sameRepo(e.PullRequest.Base.Repo) || e.PullRequest.Head.SHA != p.Head || e.PullRequest.Head.Ref != p.Branch || e.PullRequest.Base.Ref != "main" {
			return errors.New("actual same-repository PR payload differs")
		}
	} else if p.Event == "push" {
		if e.Ref != p.Ref() || e.After != p.Head {
			return errors.New("actual branch push differs")
		}
	} else if e.Ref != p.Branch && e.Ref != p.Ref() {
		return errors.New("actual dispatch branch differs")
	}
	return nil
}
