// Package pressci describes only the finite original Press PR175 checks.
// Reading an outline does not publish routes, dispatch a run or admit a runner.
package pressci

import (
	"cc/product-ci/normalci"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"gopkg.in/yaml.v3"
	"net/url"
	"strconv"
	"strings"
)

const Repository = "AutoDevJapan/GoDD-Press"
const RepoID int64 = 1386702183
const PR = 175
const Head = "b2b6d47b7f9e17617d16a9079a70de272bc3c04c"
const Tree = "dac21168dfdb55b007da5c06f36c05e66de0a9f7"
const Base = "4dc5c332d54d19325f122df1f0e4a52e9dd3a629"
const Ref = "fix/todo2642-schedule-predecessor-overflow"

type workflow struct{ name, blob string }

func workflows() []workflow {
	return []workflow{
		{"local-checks.yml", "2458c4f5eb22af26bc8c1717c14eb3d00416ebc4"},
		{"rust-v2-https.yml", "b2812cf7a167a34bb6fceea6517b7adaa58b8ecf"},
		{"windows-hidden-fixtures.yml", "6d311663d5caa916a2698a94186d79f045195f52"},
	}
}

type Job struct {
	Workflow, ID, Name, OS string
	MatrixKey, MatrixValue string
	TimeoutMinutes         int
}
type Workflow struct {
	Path, Blob, SHA256 string
	Dispatch           bool
	Events             []string
	Jobs               []Job
}
type Outline struct {
	Repository                         string `json:"-"`
	RepoID                             int64
	PR                                 int
	Head, Tree, Base                   string
	Workflows                          []Workflow
	SourceRevalidated                  bool
	RunnerAdmission, OfficialCISuccess bool
}

// InspectOriginal reads only API source bytes in RAM. The complete original
// workflow blob hashes, head/tree/base/ref and Draft PR are immutable inputs.
func InspectOriginal(ctx context.Context, read normalci.API) (Outline, error) {
	return inspectOriginal(ctx, read, workflows())
}

func inspectOriginal(ctx context.Context, read normalci.API, expected []workflow) (Outline, error) {
	verify := func() error {
		r, e := normalci.Verify(ctx, normalci.Candidate{ID: RepoID, PR: PR, SHA: Head}, read)
		if e != nil || r.FullName != Repository || r.Owner.Login != "AutoDevJapan" {
			return errors.New("Press original identity rejected")
		}
		var p struct {
			Draft bool
			Base  struct{ SHA string }
			Head  struct{ Ref string }
		}
		if read(ctx, "repos/"+Repository+"/pulls/175", &p) != nil || !p.Draft || p.Base.SHA != Base || p.Head.Ref != Ref {
			return errors.New("Press original base or Draft changed")
		}
		var commit struct {
			SHA  string
			Tree struct{ SHA string }
		}
		if read(ctx, "repos/"+Repository+"/git/commits/"+Head, &commit) != nil || commit.SHA != Head || commit.Tree.SHA != Tree {
			return errors.New("Press original tree changed")
		}
		return nil
	}
	if e := verify(); e != nil {
		return Outline{}, e
	}
	out := Outline{Repository: Repository, RepoID: RepoID, PR: PR, Head: Head, Tree: Tree, Base: Base}
	for _, w := range expected {
		var content struct{ SHA, Encoding, Content string }
		if read(ctx, "repos/"+Repository+"/contents/.github/workflows/"+w.name+"?ref="+url.QueryEscape(Head), &content) != nil || content.SHA != w.blob || content.Encoding != "base64" {
			return Outline{}, errors.New("Press original workflow unavailable")
		}
		data, e := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(content.Content), ""))
		if e != nil || blobID(data) != w.blob {
			return Outline{}, errors.New("Press original workflow blob rejected")
		}
		item, e := outlineWorkflow(data, w.name)
		if e != nil {
			return Outline{}, e
		}
		item.Blob = w.blob
		out.Workflows = append(out.Workflows, item)
	}
	if e := verify(); e != nil {
		return Outline{}, e
	}
	out.SourceRevalidated = true
	return out, nil
}

func blobID(data []byte) string {
	h := sha1.New()
	fmt.Fprintf(h, "blob %d%c", len(data), 0)
	_, _ = h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}

// Fixed job IDs and matrix values are public control metadata. No command,
// checkout, event condition, permission or timeout is rewritten here.
func outlineWorkflow(data []byte, name string) (Workflow, error) {
	root, e := normalci.ParseYAML(data)
	if e != nil {
		return Workflow{}, e
	}
	h := sha256.Sum256(data)
	out := Workflow{Path: ".github/workflows/" + name, SHA256: hex.EncodeToString(h[:])}
	events := normalci.Field(root, "on")
	if events == nil || events.Kind != yaml.MappingNode {
		return Workflow{}, errors.New("Press workflow events rejected")
	}
	for i := 0; i < len(events.Content); i += 2 {
		out.Events = append(out.Events, events.Content[i].Value)
		if events.Content[i].Value == "workflow_dispatch" {
			out.Dispatch = true
		}
	}
	jobs := normalci.Field(root, "jobs")
	if jobs == nil || jobs.Kind != yaml.MappingNode {
		return Workflow{}, errors.New("Press workflow jobs rejected")
	}
	type jobSpec struct {
		id, os, key string
		values      []string
	}
	var specs []jobSpec
	switch name {
	case "local-checks.yml":
		specs = []jobSpec{{id: "rust-core", os: "Linux"}, {id: "rust-integration", os: "Linux", key: "shard", values: []string{"1", "2", "3", "4"}}, {id: "rust-field-read-signing", os: "Linux"}, {id: "rust-aux", os: "Linux"}, {id: "browser-package", os: "Linux"}, {id: "browser-cold-start", os: "Linux"}, {id: "local-gates", os: "Linux"}}
	case "rust-v2-https.yml":
		specs = []jobSpec{{id: "rust-v2-https", key: "os", values: []string{"ubuntu-24.04", "windows-2022"}}}
	case "windows-hidden-fixtures.yml":
		specs = []jobSpec{{id: "junction-fixtures", os: "Windows"}}
	default:
		return Workflow{}, errors.New("Press workflow not admitted")
	}
	if len(jobs.Content) != 2*len(specs) {
		return Workflow{}, errors.New("Press job set changed")
	}
	for _, spec := range specs {
		job := normalci.Field(jobs, spec.id)
		if job == nil || normalci.Field(job, "name") != nil {
			return Workflow{}, errors.New("Press job identity changed")
		}
		timeout := 360 // GitHub's original default when the workflow has no timeout.
		if n := normalci.Field(job, "timeout-minutes"); n != nil {
			timeout, e = strconv.Atoi(n.Value)
			if e != nil || timeout < 1 || timeout > 360 {
				return Workflow{}, errors.New("Press job timeout rejected")
			}
		}
		values := spec.values
		if spec.key == "" {
			if normalci.Field(job, "strategy") != nil {
				return Workflow{}, errors.New("Press unexpected matrix")
			}
			values = []string{""}
		} else {
			strategy := normalci.Field(job, "strategy")
			matrix := normalci.Field(strategy, "matrix")
			axis := normalci.Field(matrix, spec.key)
			if normalci.Field(strategy, "fail-fast") == nil || normalci.Field(strategy, "fail-fast").Value != "false" || matrix == nil || len(matrix.Content) != 2 || axis == nil || axis.Kind != yaml.SequenceNode || len(axis.Content) != len(values) {
				return Workflow{}, errors.New("Press required matrix changed")
			}
			for i, value := range values {
				if axis.Content[i].Value != value {
					return Workflow{}, errors.New("Press required matrix value changed")
				}
			}
		}
		for _, value := range values {
			osName := spec.os
			label := "ubuntu-24.04"
			if spec.key == "os" {
				label = value
				osName = "Linux"
				if value == "windows-2022" {
					osName = "Windows"
				}
			}
			if osName == "Windows" {
				label = "windows-2022"
			}
			if spec.key == "os" {
				label = "${{ matrix.os }}"
			}
			runner := normalci.Field(job, "runs-on")
			if runner == nil || runner.Value != label {
				return Workflow{}, errors.New("Press original OS route changed")
			}
			display := spec.id
			if value != "" {
				display += " (" + value + ")"
			}
			out.Jobs = append(out.Jobs, Job{Workflow: out.Path, ID: spec.id, Name: display, OS: osName, MatrixKey: spec.key, MatrixValue: value, TimeoutMinutes: timeout})
		}
	}
	return out, nil
}
