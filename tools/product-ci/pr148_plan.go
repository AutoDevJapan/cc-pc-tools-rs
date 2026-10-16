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

	"gopkg.in/yaml.v3"
)

const pr148Head = "75a4051305b6f47c70ddb59a8d2bf9015666f12c"
const pr148Tree = "cecb9cd535d135a1eb4407882db532c325f42860"
const pr148Base = "1100676d1ed421794c252fb4b15ed3d954e21a5b"
const pr148Branch = "codex/todo2640-workspace-doctests-20261015"
const pr148Dedicated = "github.repository_id == '1392152644' && ((github.event_name == 'pull_request' && github.event.pull_request.number == 148 && github.event.pull_request.head.repo.id == 1392152644) || (github.event_name == 'workflow_dispatch' && github.ref == 'refs/heads/" + pr148Branch + "'))"

type pr148Workflow struct {
	Path, Job, OriginalSHA, OriginalRoute string
	RunID                                 int64
	Jobs                                  int
}

var pr148Workflows = []pr148Workflow{
	{".github/workflows/rust.yml", "test", "841ca7d8515d4ce9802effecf38dbf30052560bac3ecd8c3b679d25a598933e9", "${{ matrix.os }}", 38892356460, 2},
	{".github/workflows/native-driver.yml", "native-driver", "cb8fe6b12866934d101ee186c75db152d5a9ec3552182f91a8580967b59f6e8f", "windows-latest", 38892356435, 1},
	{".github/workflows/live-chain.yml", "live-chain", "b151c7841d615ef3e4583a1e36de6b551e6cfc7421ea625f23b6370fc23513b9", "windows-latest", 38892356407, 1},
}

// YAML is parsed before byte-span edits; aliases, duplicate keys and multiple
// documents are rejected rather than selecting an executable interpretation.
func pr148YAML(data []byte) (*yaml.Node, error) {
	if len(data) == 0 || len(data) > 1<<20 {
		return nil, errors.New("workflow size rejected")
	}
	d := yaml.NewDecoder(bytes.NewReader(data))
	var root, extra yaml.Node
	if d.Decode(&root) != nil || len(root.Content) != 1 || root.Content[0].Kind != yaml.MappingNode || d.Decode(&extra) != io.EOF {
		return nil, errors.New("workflow document rejected")
	}
	var walk func(*yaml.Node) error
	walk = func(n *yaml.Node) error {
		if n.Kind == yaml.AliasNode || n.Anchor != "" {
			return errors.New("workflow alias rejected")
		}
		if n.Kind == yaml.MappingNode {
			seen := map[string]bool{}
			for i := 0; i < len(n.Content); i += 2 {
				k := n.Content[i]
				if k.Kind != yaml.ScalarNode || k.Tag != "!!str" || seen[k.Value] {
					return errors.New("workflow mapping rejected")
				}
				seen[k.Value] = true
			}
		}
		for _, c := range n.Content {
			if e := walk(c); e != nil {
				return e
			}
		}
		return nil
	}
	if e := walk(&root); e != nil {
		return nil, e
	}
	return root.Content[0], nil
}
func pr148Field(n *yaml.Node, k string) *yaml.Node {
	if n != nil && n.Kind == yaml.MappingNode {
		for i := 0; i < len(n.Content); i += 2 {
			if n.Content[i].Value == k {
				return n.Content[i+1]
			}
		}
	}
	return nil
}
func pr148Offset(data []byte, n *yaml.Node) (int, error) {
	line, col := 1, 1
	for i, ch := range string(data) {
		if line == n.Line && col == n.Column {
			return i, nil
		}
		if ch == '\n' {
			line++
			col = 1
		} else {
			col++
		}
	}
	return 0, errors.New("workflow position unavailable")
}

type pr148Edit struct {
	Start, End int
	Text       string
}

func pr148Route(w pr148Workflow) string {
	label := "'cc-product-2736-1392152644-windows-latest'"
	fallback := "'windows-latest'"
	if w.Job == "test" {
		label = "format('cc-product-2736-1392152644-{0}', matrix.os)"
		fallback = "matrix.os"
	}
	return "${{ (" + pr148Dedicated + ") && " + label + " || " + fallback + " }}"
}

// Preserve all original bytes except runs-on and the checkout ref/auth options.
// Main and unrelated PRs keep their original hosted route. No mutation is sent.
func proposePR148(data []byte, w pr148Workflow) ([]byte, error) {
	if digest(data) != w.OriginalSHA {
		return nil, errors.New("fixed workflow digest changed")
	}
	root, e := pr148YAML(data)
	if e != nil {
		return nil, e
	}
	jobs := pr148Field(root, "jobs")
	job := pr148Field(jobs, w.Job)
	if jobs == nil || len(jobs.Content) != 2 || job == nil {
		return nil, errors.New("fixed job set changed")
	}
	route := pr148Field(job, "runs-on")
	if route == nil || route.Kind != yaml.ScalarNode || route.Style != 0 || route.Value != w.OriginalRoute {
		return nil, errors.New("original route rejected")
	}
	off, e := pr148Offset(data, route)
	if e != nil {
		return nil, e
	}
	if off+len(route.Value) > len(data) || string(data[off:off+len(route.Value)]) != route.Value {
		return nil, errors.New("route span rejected")
	}
	edits := []pr148Edit{{off, off + len(route.Value), pr148Route(w)}}
	steps := pr148Field(job, "steps")
	if steps == nil || steps.Kind != yaml.SequenceNode {
		return nil, errors.New("steps unavailable")
	}
	checkouts := 0
	for _, s := range steps.Content {
		u := pr148Field(s, "uses")
		if u == nil || !strings.HasPrefix(u.Value, "actions/checkout@") {
			continue
		}
		checkouts++
		if u.Style != 0 {
			return nil, errors.New("checkout token style rejected")
		}
		with := pr148Field(s, "with")
		if with != nil && pr148Field(with, "ref") != nil {
			return nil, errors.New("unexpected original checkout ref")
		}
		off, e := pr148Offset(data, u)
		if e != nil {
			return nil, e
		}
		end := bytes.IndexByte(data[off:], '\n')
		if end < 0 {
			return nil, errors.New("checkout line unavailable")
		}
		end += off + 1
		insert := "        with:\n          persist-credentials: false\n          ref: ${{ github.event.pull_request.head.sha || github.sha }}\n"
		if with != nil {
			persist := pr148Field(with, "persist-credentials")
			if persist == nil || persist.Value != "false" || len(with.Content) != 2 {
				return nil, errors.New("checkout options changed")
			}
			off, e = pr148Offset(data, with.Content[0])
			if e != nil {
				return nil, e
			}
			end = bytes.IndexByte(data[off:], '\n')
			if end < 0 {
				return nil, errors.New("checkout option line unavailable")
			}
			end += off + 1
			insert = "          ref: ${{ github.event.pull_request.head.sha || github.sha }}\n"
		}
		edits = append(edits, pr148Edit{end, end, insert})
	}
	if checkouts != 1 {
		return nil, errors.New("exactly one checkout required")
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].Start < edits[j].Start })
	var out bytes.Buffer
	last := 0
	for _, edit := range edits {
		if edit.Start < last {
			return nil, errors.New("overlapping workflow edits")
		}
		out.Write(data[last:edit.Start])
		out.WriteString(edit.Text)
		last = edit.End
	}
	out.Write(data[last:])
	after, e := pr148YAML(out.Bytes())
	if e != nil {
		return nil, e
	}
	// Independently remove the intended changes and compare all parsed values.
	afterJob := pr148Field(pr148Field(after, "jobs"), w.Job)
	pr148Field(afterJob, "runs-on").Value = route.Value
	for _, s := range pr148Field(afterJob, "steps").Content {
		u := pr148Field(s, "uses")
		if u == nil || !strings.HasPrefix(u.Value, "actions/checkout@") {
			continue
		}
		with := pr148Field(s, "with")
		var kept []*yaml.Node
		for i := 0; i < len(with.Content); i += 2 {
			if with.Content[i].Value != "ref" && !(w.Job == "test" && with.Content[i].Value == "persist-credentials") {
				kept = append(kept, with.Content[i:i+2]...)
			}
		}
		with.Content = kept
		if len(kept) == 0 {
			var fields []*yaml.Node
			for i := 0; i < len(s.Content); i += 2 {
				if s.Content[i].Value != "with" {
					fields = append(fields, s.Content[i:i+2]...)
				}
			}
			s.Content = fields
		}
	}
	beforeSemantic, _ := json.Marshal(pr148Semantic(root))
	afterSemantic, _ := json.Marshal(pr148Semantic(after))
	if !bytes.Equal(beforeSemantic, afterSemantic) {
		return nil, errors.New("acceptance semantics changed")
	}
	return out.Bytes(), nil
}
func pr148Semantic(n *yaml.Node) any {
	if n == nil {
		return nil
	}
	switch n.Kind {
	case yaml.MappingNode:
		m := map[string]any{}
		for i := 0; i < len(n.Content); i += 2 {
			m[n.Content[i].Value] = pr148Semantic(n.Content[i+1])
		}
		return m
	case yaml.SequenceNode:
		a := []any{}
		for _, c := range n.Content {
			a = append(a, pr148Semantic(c))
		}
		return a
	default:
		return []string{n.Tag, n.Value}
	}
}

type pr148PlanWorkflow struct {
	Path             string   `json:"path"`
	OriginalSHA      string   `json:"original_sha256"`
	ProposedSHA      string   `json:"proposed_sha256"`
	RunID            int64    `json:"original_run_id"`
	JobIDs           []int64  `json:"original_job_ids"`
	AnnotationHashes []string `json:"annotation_json_sha256"`
}
type pr148Proposal struct {
	RepoID            int64               `json:"repo_id"`
	PR                int                 `json:"pr"`
	Head              string              `json:"head"`
	Tree              string              `json:"tree"`
	Base              string              `json:"base"`
	Workflows         []pr148PlanWorkflow `json:"workflows"`
	ProposalOnly      bool                `json:"proposal_only"`
	SourceRevalidated bool                `json:"source_revalidated"`
	AllChecksSuccess  bool                `json:"all_required_checks_success"`
}

func inspectPR148(ctx context.Context, read api) (pr148Proposal, error) {
	c := candidate{1392152644, 148, pr148Head}
	p := pr148Proposal{RepoID: c.ID, PR: c.PR, Head: c.SHA, Tree: pr148Tree, Base: pr148Base, ProposalOnly: true}
	verifyScope := func() (repository, error) {
		r, e := verify(ctx, c, read)
		if e != nil {
			return r, e
		}
		var pr struct {
			State string
			Draft bool
			Head  struct{ SHA, Ref string }
			Base  struct{ SHA, Ref string }
		}
		if e = read(ctx, "repos/"+r.FullName+"/pulls/148", &pr); e != nil {
			return r, e
		}
		if !pr.Draft || pr.State != "open" || pr.Head.SHA != c.SHA || pr.Head.Ref != pr148Branch || pr.Base.SHA != pr148Base || pr.Base.Ref != "main" {
			return r, errors.New("original PR scope changed")
		}
		var commit struct{ Tree struct{ SHA string } }
		if e = read(ctx, "repos/"+r.FullName+"/git/commits/"+c.SHA, &commit); e != nil {
			return r, e
		}
		if commit.Tree.SHA != pr148Tree {
			return r, errors.New("source tree changed")
		}
		return r, nil
	}
	r, e := verifyScope()
	if e != nil {
		return p, e
	}
	for _, w := range pr148Workflows {
		var src struct{ Content, Encoding, Type string }
		if e = read(ctx, "repos/"+r.FullName+"/contents/"+w.Path+"?ref="+c.SHA, &src); e != nil {
			return p, e
		}
		data, e := base64.StdEncoding.DecodeString(strings.ReplaceAll(src.Content, "\n", ""))
		if e != nil || src.Encoding != "base64" || src.Type != "file" {
			return p, errors.New("original workflow unavailable")
		}
		proposed, e := proposePR148(data, w)
		if e != nil {
			return p, e
		}
		var run codeQLRun
		if e = read(ctx, fmt.Sprintf("repos/%s/actions/runs/%d", r.FullName, w.RunID), &run); e != nil {
			return p, e
		}
		if run.ID != w.RunID || run.RunAttempt != 1 || run.Repository.ID != c.ID || run.HeadRepo.ID != c.ID || run.HeadRepo.Fork || run.HeadSHA != c.SHA || run.Path != w.Path || run.Event != "pull_request" || run.Status != "completed" {
			return p, errors.New("original run identity rejected")
		}
		var jobs struct {
			Total int `json:"total_count"`
			Jobs  []struct {
				ID                 int64
				RunID              int64 `json:"run_id"`
				Status, Conclusion string
				RunnerID           int64 `json:"runner_id"`
				Steps              []json.RawMessage
			}
		}
		if e = read(ctx, fmt.Sprintf("repos/%s/actions/runs/%d/attempts/1/jobs?per_page=100", r.FullName, w.RunID), &jobs); e != nil {
			return p, e
		}
		if jobs.Total != w.Jobs || len(jobs.Jobs) != w.Jobs {
			return p, errors.New("original job coverage changed")
		}
		rec := pr148PlanWorkflow{Path: w.Path, OriginalSHA: digest(data), ProposedSHA: digest(proposed), RunID: w.RunID}
		for _, j := range jobs.Jobs {
			if j.ID < 1 || j.RunID != w.RunID || j.Status != "completed" || j.Conclusion != "failure" || j.RunnerID != 0 || len(j.Steps) != 0 {
				return p, errors.New("job is not an unstarted failure")
			}
			var annotations []json.RawMessage
			if e = read(ctx, fmt.Sprintf("repos/%s/check-runs/%d/annotations?per_page=100", r.FullName, j.ID), &annotations); e != nil {
				return p, e
			}
			budget := 0
			for _, annotation := range annotations {
				var value struct {
					Message string `json:"message"`
				}
				if json.Unmarshal(annotation, &value) != nil {
					return p, errors.New("invalid annotation")
				}
				if strings.TrimSpace(value.Message) == "The job was not started because an Actions budget is preventing further use." {
					budget++
				}
			}
			if budget != 1 || len(annotations) >= 100 {
				return p, errors.New("hosted budget cause not confirmed")
			}
			b, _ := json.Marshal(annotations)
			rec.AnnotationHashes = append(rec.AnnotationHashes, digest(b))
			rec.JobIDs = append(rec.JobIDs, j.ID)
		}
		p.Workflows = append(p.Workflows, rec)
	}
	if _, e = verifyScope(); e != nil {
		return p, e
	}
	p.SourceRevalidated = true
	return p, nil
}
func savePR148Proposal(ctx context.Context, dir string) error {
	p, e := inspectPR148(ctx, get)
	if e != nil {
		return e
	}
	b, e := json.MarshalIndent(p, "", "  ")
	if e != nil {
		return e
	}
	f, e := os.OpenFile(filepath.Join(dir, "pr148-proposal.private.json"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return errors.New("new owned receipt required")
	}
	_, e = f.Write(b)
	e = errors.Join(e, f.Sync(), f.Close())
	return e
}
