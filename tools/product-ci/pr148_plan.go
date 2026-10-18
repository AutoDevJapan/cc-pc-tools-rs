package main

import (
	"cc/product-ci/pr148scope"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const pr148Head = pr148scope.Head
const pr148Tree = pr148scope.Tree
const pr148Base = pr148scope.Base
const pr148Branch = pr148scope.Branch
const pr148Dedicated = pr148scope.Dedicated

type pr148Workflow = pr148scope.Workflow

var pr148Workflows = pr148scope.Workflows()

func pr148YAML(b []byte) (*yaml.Node, error)                 { return pr148scope.YAML(b) }
func pr148Field(n *yaml.Node, k string) *yaml.Node           { return pr148scope.Field(n, k) }
func pr148Offset(b []byte, n *yaml.Node) (int, error)        { return pr148scope.Offset(b, n) }
func pr148Route(w pr148Workflow) string                      { return pr148scope.Route(w) }
func pr148CheckoutRef() string                               { return pr148scope.CheckoutRef() }
func proposePR148(b []byte, w pr148Workflow) ([]byte, error) { return pr148scope.Propose(b, w) }
func pr148Semantic(n *yaml.Node) any                         { return pr148scope.Semantic(n) }

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
		if r.FullName != pr148Repo || r.Owner.Login != "AutoDevJapan" {
			return r, errors.New("original PR148 repository or owner changed")
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
				Name               string
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
		seenNames := map[string]bool{}
		for _, j := range jobs.Jobs {
			validName := j.Name == w.Job
			if w.Job == "test" {
				validName = j.Name == "test (ubuntu-latest)" || j.Name == "test (windows-latest)"
			}
			if !validName || seenNames[j.Name] {
				return p, errors.New("original job name or OS coverage changed")
			}
			seenNames[j.Name] = true
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
					Level   string `json:"annotation_level"`
				}
				if json.Unmarshal(annotation, &value) != nil {
					return p, errors.New("invalid annotation")
				}
				if strings.TrimSpace(value.Message) == "The job was not started because an Actions budget is preventing further use." {
					budget++
				} else if value.Level != "notice" {
					return p, errors.New("non-budget failure annotation present")
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
	if e := pr148OwnedReceiptRoot(ctx, dir); e != nil {
		return e
	}
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
