//go:build windows

package main

import (
    "context"
    "encoding/json"
    "errors"
    "io"
    "os"
    "time"
)

const dsRunnerTarget = "D:/CC/Tools/discord-ask/unattended-runner.exe"
const dsRunnerStage = "D:/CC/Tools/discord-ask/unattended-runner.ds4431.stage.exe"
const dsRunnerBackup = "D:/CC/Tools/discord-ask/unattended-runner.ds4431.before-385f4207.exe"
const dsRunnerOldSHA = "385f4207dd5491cc0c88060f71f1e270b543e42a1c45898b1de15348afb7efb4"
const dsRunnerOldBytes int64 = 14110208
const dsRunnerWorkerFlag = "--ds-runner-software-worker"
const dsRunnerMaintenanceDomain = "ds-runner-fixed-software-maintenance-v1"
var errDSRunnerCollision = errors.New("ds_runner_fixed_leaf_collision")
// The native replacement was provably not called: a recheck or the release of
// the staged body handle stopped the publish before ReplaceFile.
var errDSRunnerPublishNotAttempted = errors.New("ds_runner_publish_not_attempted")

// Commit outcomes. Only the native replacement call moves a leaf. A call that
// reported failure is "unconfirmed": ReplaceFile can fail after the namespace
// changed, so no result ever claims the target is unchanged. Only the outer
// caller reports "unknown", when no trustworthy worker result was observed.
const dsRunnerCommitNotAttempted = "not_attempted"
const dsRunnerCommitConfirmed = "confirmed"
const dsRunnerCommitUnconfirmed = "unconfirmed"
const dsRunnerCommitUnknown = "unknown"

type dsRunnerLeaf uint8
const(dsRunnerCurrent dsRunnerLeaf=iota;dsRunnerStaged;dsRunnerSaved)
type dsRunnerSnapshot struct{id softwareIdentity;parent softwareIdentity;acl string;size int64}
func dsRunnerValidSnapshot(s dsRunnerSnapshot)bool{return s.id.Volume!=""&&s.id.FileID!=""&&s.id.Created!=""&&s.parent.Volume!=""&&s.parent.FileID!=""&&s.parent.Created!=""&&s.acl!=""&&s.size>0&&s.size<=32<<20}
type dsRunnerWorkerPermit struct{self *dsRunnerWorkerPermit;ctx context.Context}
type dsRunnerRepairPurpose struct{candidate *dsRunnerCandidate;pin dsRunnerCapturePin;permit *dsRunnerWorkerPermit}
func dsRunnerPurposeValid(p dsRunnerRepairPurpose)bool{
    if p.permit==nil||p.permit.self!=p.permit||p.permit.ctx==nil||p.permit.ctx.Err()!=nil||p.candidate==nil{return false}
    c:=p.candidate;c.mu.Lock();defer c.mu.Unlock();return c.self==c&&!c.closed&&c.active&&c.issuer!=nil&&c.pin==p.pin&&dsRunnerPinValid(p.pin)&&digest(c.raw)==p.pin.OutputSHA
}
type dsRunnerTargetSession interface{
    Read(context.Context,dsRunnerLeaf)([]byte,dsRunnerSnapshot,error)
    Ensure(context.Context,dsRunnerLeaf,[]byte)(dsRunnerSnapshot,bool,error)
    Recheck(context.Context,dsRunnerLeaf,dsRunnerSnapshot)error
    Publish(context.Context,dsRunnerSnapshot,dsRunnerSnapshot,dsRunnerSnapshot)(bool,error)
    Close()error
}
type dsRunnerMaintenanceAccess struct{
    acquire func(context.Context)(*dsRunnerCandidate,error)
    open func(context.Context,dsRunnerRepairPurpose)(dsRunnerTargetSession,error)
}
type dsRunnerMaintenanceResult struct{
    Domain string `json:"domain"`
    Operation string `json:"operation"`
    Qualified bool `json:"qualified"`
    Prepared bool `json:"prepared"`
    UpdatePerformed bool `json:"update_performed"`
    InstalledVerified bool `json:"installed_verified"`
    CleanupFailed bool `json:"cleanup_failed"`
    CommitAttempted bool `json:"commit_attempted"`
    CommitOutcome string `json:"commit_outcome"`
    Reason string `json:"reason"`
    Current *softwareIdentity `json:"current_identity,omitempty"`
    Stage *softwareIdentity `json:"stage_identity,omitempty"`
    Backup *softwareIdentity `json:"backup_identity,omitempty"`
    ACLSHA string `json:"acl_sha256,omitempty"`
    CandidateSHA string `json:"candidate_sha256,omitempty"`
    Created []string `json:"created,omitempty"`
    Reaped bool `json:"reaped"`
    SoftwareReplaceAllowed bool `json:"software_replace_allowed"`
    DataAccessGranted bool `json:"data_access_granted"`
    SourceAccessGranted bool `json:"source_access_granted"`
    SendAllowed bool `json:"send_allowed"`
    NamespaceLeaseProved bool `json:"namespace_lease_proved"`
}
func dsRunnerMaintenanceOperation(s string)bool{return s=="capture"||s=="verify"||s=="prepare"||s=="apply"}
func dsRunnerCommitOutcomeFor(committed bool,err error)string{
    if committed{return dsRunnerCommitConfirmed}
    if errors.Is(err,errDSRunnerPublishNotAttempted){return dsRunnerCommitNotAttempted}
    return dsRunnerCommitUnconfirmed
}
func executeDSRunnerMaintenance(ctx context.Context,op string,permit *dsRunnerWorkerPermit,access dsRunnerMaintenanceAccess)(r dsRunnerMaintenanceResult){
    return executeDSRunnerMaintenanceFor(ctx,op,permit,access,dsRunnerOldSHA,dsRunnerOldBytes)
}
// The normal entry supplies the compiled predecessor. Owned synthetic tests use
// this internal seam without opening an installed executable or issuing a grant.
func executeDSRunnerMaintenanceFor(ctx context.Context,op string,permit *dsRunnerWorkerPermit,access dsRunnerMaintenanceAccess,oldSHA string,oldBytes int64)(r dsRunnerMaintenanceResult){
    r=dsRunnerMaintenanceResult{Domain:dsRunnerMaintenanceDomain,Operation:op,Reason:"qualification_unavailable",CommitOutcome:dsRunnerCommitNotAttempted}
    if ctx==nil||ctx.Err()!=nil||!dsRunnerMaintenanceOperation(op)||permit==nil||permit.self!=permit||permit.ctx!=ctx||access.acquire==nil||op!="capture"&&access.open==nil{return}
    candidate,err:=access.acquire(ctx)
    if candidate!=nil{defer func(){closeErr:=candidate.Close();cleanup,cancel:=context.WithTimeout(context.Background(),5*time.Second);defer cancel();waitErr:=candidate.waitClosed(cleanup);if closeErr!=nil||waitErr!=nil{r.Qualified=false;r.Prepared=false;r.InstalledVerified=false;r.CleanupFailed=true;if r.Reason=="authenticated_capture_verified_no_target_access"||r.Reason=="verified_no_write"||r.Reason=="prepared_verified_fixed_leaves"||r.Reason=="installed_rechecked"||r.Reason=="already_installed_verified"{r.Reason="cleanup_unknown"}}}()}
    if err!=nil||candidate==nil{r.CleanupFailed=dsRunnerCleanupFact(err);return}
    err=candidate.with(ctx,func(payload []byte)(innerErr error){
        purpose:=dsRunnerRepairPurpose{candidate:candidate,pin:candidate.pin,permit:permit};if !dsRunnerPurposeValid(purpose){return errDSRunnerEvidence}
        if op=="capture"{r.Qualified=true;r.CandidateSHA=purpose.pin.OutputSHA;r.Reason="authenticated_capture_verified_no_target_access";return nil}
        target,err:=access.open(ctx,purpose);if target!=nil{defer func(){if closeErr:=target.Close();closeErr!=nil{innerErr=errors.Join(innerErr,errDSRunnerCleanup,closeErr)}}()};if err!=nil||target==nil{if dsRunnerCleanupFact(err){return errors.Join(errDSRunnerEvidence,errDSRunnerCleanup)};return errDSRunnerEvidence}
        current,snapshot,err:=target.Read(ctx,dsRunnerCurrent);defer clear(current);if err!=nil{return err}
        if !dsRunnerValidSnapshot(snapshot){return errDSRunnerEvidence};r.Current=&snapshot.id;r.ACLSHA=digest([]byte(snapshot.acl));r.CandidateSHA=purpose.pin.OutputSHA
        if digest(current)==purpose.pin.OutputSHA&&len(current)==len(payload){r.Qualified=true;r.InstalledVerified=true;r.Reason="already_installed_verified";return nil}
        if int64(len(current))!=oldBytes||digest(current)!=oldSHA{return errDSRunnerEvidence}
        if target.Recheck(ctx,dsRunnerCurrent,snapshot)!=nil{return errDSRunnerEvidence}
        r.Qualified=true;r.Reason="verified_no_write";if op=="verify"{return nil}
        backup,created,err:=target.Ensure(ctx,dsRunnerSaved,current);if created{r.Created=append(r.Created,dsRunnerBackup);if backup.id.FileID!=""{r.Backup=&backup.id}};if err!=nil{return err};r.Backup=&backup.id
        stage,created,err:=target.Ensure(ctx,dsRunnerStaged,payload);if created{r.Created=append(r.Created,dsRunnerStage);if stage.id.FileID!=""{r.Stage=&stage.id}};if err!=nil{return err};r.Stage=&stage.id
        if !dsRunnerValidSnapshot(backup)||!dsRunnerValidSnapshot(stage)||backup.acl!=snapshot.acl||stage.acl!=snapshot.acl||backup.parent!=snapshot.parent||stage.parent!=snapshot.parent{return errDSRunnerEvidence}
        if target.Recheck(ctx,dsRunnerCurrent,snapshot)!=nil||target.Recheck(ctx,dsRunnerSaved,backup)!=nil||target.Recheck(ctx,dsRunnerStaged,stage)!=nil||ctx.Err()!=nil{return errDSRunnerEvidence}
        r.Prepared=true;r.Reason="prepared_verified_fixed_leaves";if op=="prepare"{return nil}
        r.CommitAttempted=true;r.CommitOutcome=dsRunnerCommitUnconfirmed;committed,err:=target.Publish(ctx,snapshot,stage,backup);r.CommitOutcome=dsRunnerCommitOutcomeFor(committed,err);r.CommitAttempted=r.CommitOutcome!=dsRunnerCommitNotAttempted;r.UpdatePerformed=committed;if err!=nil{return err}
        actual,id,err:=target.Read(ctx,dsRunnerCurrent);defer clear(actual);if err!=nil{return err}
        if !committed||!dsRunnerValidSnapshot(id)||id.id.Volume!=stage.id.Volume||id.id.FileID!=stage.id.FileID||id.id.Created!=snapshot.id.Created||id.parent!=snapshot.parent||id.acl!=snapshot.acl||digest(actual)!=purpose.pin.OutputSHA||len(actual)!=len(payload)||ctx.Err()!=nil{return errDSRunnerEvidence}
        r.Current=&id.id;r.InstalledVerified=true;r.Reason="installed_rechecked";return nil
    })
    if err!=nil{r.Qualified=false;r.Prepared=false;r.InstalledVerified=false;r.CleanupFailed=r.CleanupFailed||dsRunnerCleanupFact(err);if errors.Is(err,errDSRunnerCollision){r.Reason="fixed_leaf_collision_preserved"}else if r.CommitAttempted{r.Reason="commit_outcome_requires_reconciliation"}else if len(r.Created)>0{r.Reason="partial_prepare_preserved"}else{r.Reason="qualification_unavailable"}}
    return
}
func performDSRunnerMaintenance(ctx context.Context,op string,permit *dsRunnerWorkerPermit)dsRunnerMaintenanceResult{
    return executeDSRunnerMaintenance(ctx,op,permit,dsRunnerMaintenanceAccess{acquire:openNormalDSRunnerCandidate,open:func(ctx context.Context,p dsRunnerRepairPurpose)(dsRunnerTargetSession,error){return dsRunnerOpenNativeTargetMode(ctx,p,[3]string{dsRunnerTarget,dsRunnerStage,dsRunnerBackup},op=="apply")}})
}
func runDSRunnerMaintenanceWorker(args []string,out io.Writer)int{
    if len(args)!=2||args[0]!=dsRunnerWorkerFlag||!dsRunnerMaintenanceOperation(args[1])||!askWorkerParent(){return 2}
    ctx,cancel:=context.WithTimeout(context.Background(),3*time.Minute);defer cancel();permit:=&dsRunnerWorkerPermit{ctx:ctx};permit.self=permit
    r:=performDSRunnerMaintenance(ctx,args[1],permit);permit.self=nil
    if r.CandidateSHA!=""&&r.CandidateSHA!=dsRunnerAdmittedCapture.OutputSHA{return 1}
    return dsRunnerWorkerExit(args[1],r,writeDSRunnerMaintenanceResult(out,r))
}
// Exit zero means a qualified result whose operation reached its goal. Any other
// written result is a failure that still carries its facts to the caller.
func dsRunnerWorkerExit(op string,r dsRunnerMaintenanceResult,wrote bool)int{
    if !wrote||!dsRunnerResultSuccess(op,r){return 1}
    return 0
}
// Normal calls accept only the compiled capture digest. This check carries no
// authority: the worker independently authenticates the actual source bytes.
func dsRunnerFixedCandidateHashMatches(raw []byte)bool{
    var r dsRunnerMaintenanceResult
    return dsRunnerCanonical(bytesTrimDSRunner(raw),&r,64<<10)==nil&&(r.CandidateSHA==""||r.CandidateSHA==dsRunnerAdmittedCapture.OutputSHA)
}
func runDSRunnerMaintenance(args []string,out io.Writer)int{
    return runDSRunnerMaintenanceWith(args,out,func(ctx context.Context,exe string,args ...string)askChildResult{
        r:=askOwnedChild(ctx,exe,args...)
        if !dsRunnerFixedCandidateHashMatches(r.Raw){clear(r.Raw);r.Raw=nil}
        return r
    })
}
func runDSRunnerMaintenanceWith(args []string,out io.Writer,launch func(context.Context,string,...string)askChildResult)int{
    if launch==nil{return 2}
    if len(args)!=1{return 2};op:="";switch args[0]{case "--verify-ds-runner-capture":op="capture";case "--verify-ds-runner-update":op="verify";case "--prepare-ds-runner-update":op="prepare";case "--apply-ds-runner-update":op="apply";default:return 2}
    exe,err:=os.Executable();if err!=nil{return 1};ctx,cancel:=context.WithTimeout(context.Background(),3*time.Minute+10*time.Second);defer cancel()
    child:=launch(context.WithValue(ctx,askResultCompleteKey{},true),exe,dsRunnerWorkerFlag,op);defer clear(child.Raw)
    r,observed:=dsRunnerObserveWorker(op,child)
    if !observed{r=dsRunnerUnknownResult(op,child.Reaped)}
    if !writeDSRunnerMaintenanceResult(out,r)||!observed||child.Exit!=0{return 1}
    return 0
}
// A worker result is observed only when the owned child proved started, assigned,
// waited and reaped without a timeout, ended with exit zero or one, and produced
// exactly one canonical record with internally consistent facts. The facts of a
// failed worker are forwarded as they are; they never become a success.
func dsRunnerObserveWorker(op string,child askChildResult)(r dsRunnerMaintenanceResult,observed bool){
    if !child.Started||!child.Assigned||!child.WaitVerified||!child.Reaped||!child.ResultStdoutComplete||!child.ResultStderrComplete||child.TimedOut||child.Exit!=0&&child.Exit!=1{return}
    var worker dsRunnerMaintenanceResult
    if dsRunnerCanonical(bytesTrimDSRunner(child.Raw),&worker,64<<10)!=nil||!dsRunnerWorkerResultValid(op,worker){return}
    if dsRunnerWorkerExit(op,worker,true)!=child.Exit||child.Exit==0&&!dsRunnerResultSuccess(op,worker){return}
    worker.Reaped=true
    return worker,true
}
func dsRunnerResultSuccess(op string,r dsRunnerMaintenanceResult)bool{
    if !dsRunnerWorkerResultValid(op,r){return false}
    switch r.Reason{
    case "authenticated_capture_verified_no_target_access","already_installed_verified","verified_no_write","prepared_verified_fixed_leaves","installed_rechecked":return true
    }
    return false
}
// The worker never claims a reap, grant, or unknown outcome. Its reason and
// result fields must describe one reachable operation state before forwarding.
func dsRunnerWorkerResultValid(op string,r dsRunnerMaintenanceResult)bool{
    if !dsRunnerMaintenanceOperation(op)||r.Domain!=dsRunnerMaintenanceDomain||r.Operation!=op||r.Reaped||r.SoftwareReplaceAllowed||r.DataAccessGranted||r.SourceAccessGranted||r.SendAllowed||r.NamespaceLeaseProved{return false}
    switch r.CommitOutcome{
    case dsRunnerCommitNotAttempted:
    case dsRunnerCommitConfirmed,dsRunnerCommitUnconfirmed:if op!="apply"{return false}
    default:return false
    }
    if r.CommitAttempted!=(r.CommitOutcome!=dsRunnerCommitNotAttempted)||r.UpdatePerformed!=(r.CommitOutcome==dsRunnerCommitConfirmed){return false}
    if r.ACLSHA!=""&&!ghaGuardHex(r.ACLSHA,64)||r.CandidateSHA!=""&&!ghaGuardHex(r.CandidateSHA,64)||len(r.Created)>2{return false}
    seen:=map[string]bool{}
    for _,created:=range r.Created{if created!=dsRunnerBackup&&created!=dsRunnerStage||seen[created]{return false};seen[created]=true}
    for _,id:=range []*softwareIdentity{r.Current,r.Stage,r.Backup}{if id!=nil&&!(dsRunnerDecimal(id.Volume)&&dsRunnerDecimal(id.FileID)&&dsRunnerDecimal(id.Created)){return false}}
    // A snapshot and its ACL digest are recorded together. Any target leaf
    // observation occurs only after the candidate and current were recorded.
    if (r.Current==nil)!=(r.ACLSHA=="")||r.Current!=nil&&r.CandidateSHA==""{return false}
    if (r.Stage!=nil||r.Backup!=nil||len(r.Created)>0)&&r.Current==nil{return false}
    if r.Stage!=nil&&r.Backup==nil&&!seen[dsRunnerStage]{return false}
    if seen[dsRunnerStage]&&!seen[dsRunnerBackup]&&r.Backup==nil{return false}
    if len(r.Created)==2&&(r.Created[0]!=dsRunnerBackup||r.Created[1]!=dsRunnerStage){return false}
    if op=="capture"&&(r.Current!=nil||r.Stage!=nil||r.Backup!=nil||len(r.Created)!=0){return false}
    if op=="verify"&&(r.Stage!=nil||r.Backup!=nil||len(r.Created)!=0){return false}
    if r.CommitOutcome!=dsRunnerCommitNotAttempted&&(r.Current==nil||r.Stage==nil||r.Backup==nil){return false}
    noCommit:=r.CommitOutcome==dsRunnerCommitNotAttempted
    fixedTarget:=r.Current!=nil&&r.ACLSHA!=""&&r.CandidateSHA!=""
    fixedLeaves:=fixedTarget&&r.Stage!=nil&&r.Backup!=nil
    switch r.Reason{
    case "authenticated_capture_verified_no_target_access":
        return op=="capture"&&r.Qualified&&!r.Prepared&&!r.InstalledVerified&&!r.CleanupFailed&&noCommit&&r.CandidateSHA!=""
    case "already_installed_verified":
        return op!="capture"&&r.Qualified&&!r.Prepared&&r.InstalledVerified&&!r.CleanupFailed&&noCommit&&fixedTarget&&r.Stage==nil&&r.Backup==nil&&len(r.Created)==0
    case "verified_no_write":
        return op=="verify"&&r.Qualified&&!r.Prepared&&!r.InstalledVerified&&!r.CleanupFailed&&noCommit&&fixedTarget
    case "prepared_verified_fixed_leaves":
        return op=="prepare"&&r.Qualified&&r.Prepared&&!r.InstalledVerified&&!r.CleanupFailed&&noCommit&&fixedLeaves
    case "installed_rechecked":
        return op=="apply"&&r.Qualified&&r.Prepared&&r.InstalledVerified&&!r.CleanupFailed&&r.CommitOutcome==dsRunnerCommitConfirmed&&fixedLeaves&&r.Current.Volume==r.Stage.Volume&&r.Current.FileID==r.Stage.FileID
    case "cleanup_unknown":
        if r.Qualified||r.Prepared||r.InstalledVerified||!r.CleanupFailed{return false}
        if op=="capture"{return noCommit&&r.CandidateSHA!=""}
        if op=="apply"&&r.CommitOutcome==dsRunnerCommitConfirmed{return fixedLeaves}
        if !noCommit||!fixedTarget{return false}
        return op=="prepare"&&fixedLeaves||op!="prepare"&&r.Stage==nil&&r.Backup==nil
    case "qualification_unavailable":
        return !r.Qualified&&!r.Prepared&&!r.InstalledVerified&&noCommit&&len(r.Created)==0&&(op!="capture"||r.CandidateSHA=="")
    case "fixed_leaf_collision_preserved":
        return (op=="prepare"||op=="apply")&&!r.Qualified&&!r.Prepared&&!r.InstalledVerified&&noCommit&&fixedTarget
    case "partial_prepare_preserved":
        return (op=="prepare"||op=="apply")&&!r.Qualified&&!r.Prepared&&!r.InstalledVerified&&noCommit&&fixedTarget&&len(r.Created)>0
    case "commit_outcome_requires_reconciliation":
        return op=="apply"&&!r.Qualified&&!r.Prepared&&!r.InstalledVerified&&r.CommitOutcome!=dsRunnerCommitNotAttempted&&fixedLeaves
    }
    return false
}
func dsRunnerDecimal(s string)bool{
    if s==""||len(s)>20{return false}
    for _,c:=range s{if c<'0'||c>'9'{return false}}
    return true
}
// Without a trustworthy worker record nothing is known about the replacement of
// an apply run, so the caller reports it as attempted with an unknown outcome.
func dsRunnerUnknownResult(op string,reaped bool)dsRunnerMaintenanceResult{
    r:=dsRunnerMaintenanceResult{Domain:dsRunnerMaintenanceDomain,Operation:op,Reason:"worker_outcome_unknown",CommitOutcome:dsRunnerCommitNotAttempted,Reaped:reaped}
    if op=="apply"{r.CommitAttempted=true;r.CommitOutcome=dsRunnerCommitUnknown}
    return r
}
func bytesTrimDSRunner(raw []byte)[]byte{for len(raw)>0&&(raw[len(raw)-1]=='\n'||raw[len(raw)-1]=='\r'){raw=raw[:len(raw)-1]};return raw}

func writeDSRunnerMaintenanceResult(out io.Writer,r dsRunnerMaintenanceResult)bool{if out==nil{return false};raw,err:=json.Marshal(r);if err!=nil{return false};raw=append(raw,'\n');n,err:=out.Write(raw);return err==nil&&n==len(raw)}
