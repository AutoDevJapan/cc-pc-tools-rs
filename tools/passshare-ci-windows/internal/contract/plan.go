// Package contract admits only the explicitly reviewed PassShare Windows job.
package contract

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "errors"
 "fmt"
 "regexp"
 "strconv"
 "strings"
)

const Repository = "AutoDevJapan/PassShare"
const RepoID int64 = 1131498455
const OriginalHead = "c67bfb864acd51af0aafe972b2ea09e53d187ed7"
// A fixture marker, never a claim about the product's current tree.
const OriginalTree = "0000000000000000000000000000000000000001"
const WindowsLabel = "passshare-ci-windows"
const Workflow = ".github/workflows/desktop-build.yml"
const CandidateBranch = "scheduler/todo-2994-passshare-windows"

type ReadAPI func(context.Context, string, any) error

// Plan contains no credentials or caller-supplied executables. Its SHA and
// workflow digest must have been reviewed before the controller supplies it.
type Plan struct {
 Version int `json:"version"`
 RepoID int64 `json:"repo_id"`
 Head string `json:"head"`
 Tree string `json:"tree"`
 Event string `json:"event"`
 Branch string `json:"branch"`
 PR int `json:"pr"`
 ContextSHA string `json:"context_sha"`
 WorkflowSHA string `json:"workflow_sha"`
 WorkflowRef string `json:"workflow_ref"`
 Workflow string `json:"workflow"`
 WorkflowDigest string `json:"workflow_digest"`
 RunID int64 `json:"run_id"`
 Attempt int `json:"attempt"`
 JobID int64 `json:"job_id"`
 Job string `json:"job"`
 JobName string `json:"job_name"`
}

var shaPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
var digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
func ValidSHA(s string) bool { return shaPattern.MatchString(s) }
func Digest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func (p Plan) OS() string { return "Windows" }
func (p Plan) Ref() string { return "refs/heads/" + p.Branch }
func Labels(p Plan) []string { return []string{"self-hosted", "Windows", "X64", WindowsLabel} }
func (p Plan) Validate() error {
 if p.Version != 1 || p.RepoID != RepoID || !ValidSHA(p.Head) || !ValidSHA(p.Tree) || p.ContextSHA != p.Head || p.WorkflowSHA != p.Head || p.RunID < 1 || p.JobID < 1 || p.Attempt < 1 || p.PR != 0 { return errors.New("fixed PassShare source and official job required") }
 if p.Event != "push" && p.Event != "workflow_dispatch" { return errors.New("PR and fork events cannot run on this PC") }
 if p.Branch != "main" && !(p.Event == "workflow_dispatch" && p.Branch == CandidateBranch) { return errors.New("only main or the one reviewed dispatch branch admitted") }
 if p.Workflow != Workflow || p.Job != "windows" || p.JobName != "Windows build" || p.WorkflowRef != Repository+"/"+Workflow+"@"+p.Ref() || !digestPattern.MatchString(p.WorkflowDigest) { return errors.New("fixed Desktop Build workflow context required") }
 return nil
}
func MatchRunPath(p Plan, path string) bool { return path == p.Workflow || path == p.Workflow+"@"+p.Ref() || path == p.Workflow+"@"+p.Branch }
func CheckEvent(p Plan, env map[string]string, raw []byte) error {
 if e := p.Validate(); e != nil { return e }
 want := map[string]string{"GITHUB_REPOSITORY":Repository, "GITHUB_REPOSITORY_ID":strconv.FormatInt(RepoID,10), "GITHUB_SHA":p.Head, "GITHUB_RUN_ID":strconv.FormatInt(p.RunID,10), "GITHUB_RUN_ATTEMPT":strconv.Itoa(p.Attempt), "GITHUB_JOB":p.Job, "RUNNER_OS":"Windows", "GITHUB_EVENT_NAME":p.Event, "GITHUB_REF":p.Ref(), "GITHUB_WORKFLOW_SHA":p.WorkflowSHA, "GITHUB_WORKFLOW_REF":p.WorkflowRef}
 for k,v := range want { if env[k] != v { return fmt.Errorf("actual job context differs: %s",k) } }
 if len(raw)==0 || len(raw)>1<<20 { return errors.New("bounded actual event required") }
 var ev struct { Repository repository; Ref,After string }
 if json.Unmarshal(raw,&ev)!=nil || !sameRepo(ev.Repository) { return errors.New("actual repository differs") }
 if p.Event=="push" { if ev.Ref!=p.Ref() || ev.After!=p.Head { return errors.New("push payload differs") } } else if ev.Ref!=p.Branch && ev.Ref!=p.Ref() { return errors.New("dispatch payload differs") }
 return nil
}
func equalLabels(a,b []string) bool {
 if len(a)!=len(b) { return false }; counts:=map[string]int{}; for _,s:=range a { counts[strings.ToLower(s)]++ }; for _,s:=range b { counts[strings.ToLower(s)]-- }; for _,n:=range counts {if n!=0{return false}}; return true
}
