package contract

import (
 "context"
 "encoding/base64"
 "errors"
 "fmt"
 "net/url"
 "strings"
)

type repository struct {
 ID int64
 FullName string `json:"full_name"`
 Private,Fork bool
 DefaultBranch string `json:"default_branch"`
 Owner struct { Login string }
 Permissions struct { Admin bool }
}
func sameRepo(r repository) bool { return r.ID==RepoID && r.FullName==Repository && !r.Fork }
type Run struct {
 ID int64
 RunAttempt int `json:"run_attempt"`
 Path,Event,Status,Conclusion string
 HeadSHA string `json:"head_sha"`
 HeadBranch string `json:"head_branch"`
 Repository repository
 HeadRepo repository `json:"head_repository"`
}
type Job struct {
 ID int64
 RunID int64 `json:"run_id"`
 RunAttempt int `json:"run_attempt"`
 Name,Status,Conclusion string
 Labels []string
 RunnerName string `json:"runner_name"`
 Steps []struct { Name,Status,Conclusion string }
}
func Verify(ctx context.Context,p Plan,read ReadAPI) error {
 if e:=p.Validate();e!=nil{return e}
 var r repository
 if e:=read(ctx,fmt.Sprintf("repositories/%d",RepoID),&r);e!=nil{return e}
 if !sameRepo(r)||r.Owner.Login!="AutoDevJapan"||r.Private||r.DefaultBranch!="main"||!r.Permissions.Admin{return errors.New("explicit owned public PassShare repository required")}
 var ref struct {Object struct{SHA string}}
 // The only two allowed branch spellings contain no arbitrary URL input.
 branch:=strings.Join(strings.Split(p.Branch,"/"),"/")
 if e:=read(ctx,"repos/"+Repository+"/git/ref/heads/"+branch,&ref);e!=nil{return e}
 if ref.Object.SHA!=p.Head{return errors.New("obsolete source rejected")}
 var c struct { SHA string; Tree struct{SHA string} }
 if e:=read(ctx,"repos/"+Repository+"/git/commits/"+p.Head,&c);e!=nil{return e}
 if c.SHA!=p.Head||c.Tree.SHA!=p.Tree{return errors.New("source commit/tree differs")}
 b,e:=contents(ctx,read,Workflow,p.WorkflowSHA);if e!=nil{return e}
 if Digest(b)!=p.WorkflowDigest || string(b)!=string(DesktopWorkflow) {return errors.New("reviewed executable workflow bytes differ")}
 var run Run
 if e=read(ctx,fmt.Sprintf("repos/%s/actions/runs/%d",Repository,p.RunID),&run);e!=nil{return e}
 if run.ID!=p.RunID||run.RunAttempt!=p.Attempt||run.HeadSHA!=p.Head||run.HeadBranch!=p.Branch||run.Event!=p.Event||!MatchRunPath(p,run.Path)||!sameRepo(run.Repository)||!sameRepo(run.HeadRepo){return errors.New("official immutable run differs")}
 j,e:=ObserveJob(ctx,p,read);if e!=nil{return e}
 if j.Status!="queued"&&j.Status!="in_progress"&&j.Status!="completed" {return errors.New("unsupported job state")}
 // Linux checks and the hosted admission job must never use this PC label.
 var jobs struct { Total int `json:"total_count"`; Jobs []Job }
 if e=read(ctx,fmt.Sprintf("repos/%s/actions/runs/%d/attempts/%d/jobs?per_page=100",Repository,p.RunID,p.Attempt),&jobs);e!=nil{return e}
 if jobs.Total!=len(jobs.Jobs)||jobs.Total<2||jobs.Total>4{return errors.New("complete bounded Desktop Build job inventory required")}
 found:=false
 for _,other:=range jobs.Jobs { if other.ID==p.JobID {found=true;continue}; for _,l:=range other.Labels {if strings.EqualFold(l,WindowsLabel){return errors.New("a second job requests this PC")}} }
 if !found{return errors.New("target job absent from run attempt")}
 return nil
}
func contents(ctx context.Context,read ReadAPI,path,sha string)([]byte,error){
 var v struct {Type,Encoding,Content string}
 if e:=read(ctx,"repos/"+Repository+"/contents/"+path+"?ref="+url.QueryEscape(sha),&v);e!=nil{return nil,e}
 if v.Type!="file"||v.Encoding!="base64"||len(v.Content)>2<<20{return nil,errors.New("workflow response rejected")}
 return base64.StdEncoding.DecodeString(strings.ReplaceAll(v.Content,"\n",""))
}
func ObserveJob(ctx context.Context,p Plan,read ReadAPI)(Job,error){
 var j Job
 if e:=p.Validate();e!=nil{return j,e}
 if e:=read(ctx,fmt.Sprintf("repos/%s/actions/jobs/%d",Repository,p.JobID),&j);e!=nil{return j,e}
 if j.ID!=p.JobID||j.RunID!=p.RunID||j.RunAttempt!=p.Attempt||j.Name!=p.JobName||!equalLabels(j.Labels,Labels(p)){return Job{},errors.New("official job identity differs")}
 return j,nil
}
func VerifyAssigned(ctx context.Context,p Plan,runner string,read ReadAPI)error{
 if runner==""{return errors.New("unique runner name required")}
 if e:=Verify(ctx,p,read);e!=nil{return e}
 j,e:=ObserveJob(ctx,p,read);if e!=nil{return e}
 if j.RunnerName!=runner||(j.Status!="in_progress"&&j.Status!="completed"){return errors.New("actual assigned runner differs")};return nil
}
func RequireSuccess(ctx context.Context,p Plan,runner string,read ReadAPI)error{
 if e:=VerifyAssigned(ctx,p,runner,read);e!=nil{return e}
 j,e:=ObserveJob(ctx,p,read);if e!=nil{return e}
 if j.Status!="completed"||j.Conclusion!="success"||len(j.Steps)==0{return errors.New("official job success not confirmed")};return nil
}
