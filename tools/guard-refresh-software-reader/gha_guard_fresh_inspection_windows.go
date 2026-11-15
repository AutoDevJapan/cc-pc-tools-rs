//go:build windows

package main

import (
    "bytes"
    "context"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "os"
    "strconv"
    "strings"
    "time"
)

const ghaFreshInspectionFlag="--inspect-fresh-guard-target"
const ghaFreshWorkerFlag="--fresh-guard-inspection-worker"

// Credential cleanup is an observation from the owned worker. The worker
// lifecycle fields below are populated only from the parent's askChildResult.
type ghaFreshWorkerResult struct {
    guardTargetMetadataResult
    CredentialCleanupState string `json:"credential_cleanup_state"`
}

type ghaFreshInspectionResult struct {
    guardTargetMetadataResult
    CredentialCleanupState string `json:"credential_cleanup_state"`
    WorkerResultObserved bool `json:"worker_result_observed"`
    WorkerStarted bool `json:"worker_started"`
    WorkerAssigned bool `json:"worker_assigned"`
    WorkerWaitVerified bool `json:"worker_wait_verified"`
    WorkerReaped bool `json:"worker_reaped"`
    WorkerTimedOut bool `json:"worker_timed_out"`
}

func ghaFreshAcquisitionReason(err error) string {
    var service *ghaGuardHTTPStatusError
    if errors.As(err,&service)&&service!=nil&&service.status>=100&&service.status<=599 {
        switch service.phase {
        case "credential_owner","authenticated_run","metadata","job_log","job_log_object","artifact","artifact_object","source_metadata":
            return fmt.Sprintf("guard_github_%s_http_%d",service.phase,service.status)
        }
    }
    var status *ghaFreshHTTPStatusError
    if errors.As(err,&status)&&status!=nil&&status.status>=100&&status.status<=599&&
        (status.phase=="runs"||status.phase=="metadata"||status.phase=="attestation") {
        return fmt.Sprintf("guard_fresh_public_%s_http_%d",status.phase,status.status)
    }
    switch {
    case errors.Is(err,context.Canceled),errors.Is(err,context.DeadlineExceeded):return "guard_target_cancelled"
    case errors.Is(err,errGHAGuardCredentialCleanup):return "guard_github_credential_cleanup_failed"
    case errors.Is(err,errGHAGuardResponseCleanup):return "guard_github_response_cleanup_failed"
    case errors.Is(err,errGHAGuardCredentialExpired):return "guard_github_artifact_expired"
    case errors.Is(err,errGHAGuardCredentialNotFound):return "guard_github_credential_not_found"
    case errors.Is(err,errGHAGuardCredentialAccessDenied):return "guard_github_credential_access_denied"
    case errors.Is(err,errGHAGuardCredentialIdentity):return "guard_github_credential_identity_mismatch"
    case errors.Is(err,errGHAGuardCredentialUnauthorized):return "guard_github_credential_unauthorized"
    case errors.Is(err,errGHAGuardCredentialForbidden):return "guard_github_credential_forbidden"
    default:return "guard_target_artifact_unavailable"
    }
}

func ghaFreshHTTPReasonAllowed(value string) bool {
    prefixes:=[]string{
        "guard_fresh_public_runs_http_","guard_fresh_public_metadata_http_","guard_fresh_public_attestation_http_",
        "guard_github_credential_owner_http_","guard_github_authenticated_run_http_",
        "guard_github_metadata_http_","guard_github_job_log_http_","guard_github_job_log_object_http_",
        "guard_github_artifact_http_","guard_github_artifact_object_http_","guard_github_source_metadata_http_",
    }
    for _,prefix:=range prefixes {
        if strings.HasPrefix(value,prefix) {
            suffix:=strings.TrimPrefix(value,prefix)
            code,err:=strconv.Atoi(suffix)
            return err==nil&&code>=100&&code<=599&&strconv.Itoa(code)==suffix
        }
    }
    return false
}

// The acquire seam is package-private and supplies no target permission.
// Production passes only the real same-process candidate factory.
func ghaFreshInspectWith(ctx context.Context,audit *ghaFreshCredentialAudit,acquire func(context.Context)(guardTargetArtifact,error)) ghaFreshWorkerResult {
    var acquisition error
    result:=observeGuardTargetMetadataWith(ctx,guardTargetMetadataFactories{
        artifact:func(c context.Context)(guardTargetArtifact,error){
            if acquire==nil{return nil,errGuardTargetArtifactUnavailable}
            value,err:=acquire(c);acquisition=err
            return value,err
        },
        purpose:guardTargetOpenNormalPurpose,
    })
    state:="unknown"
    if audit!=nil&&(audit.state=="completed"||audit.state=="failed"){state=audit.state}
    if errors.Is(acquisition,context.Canceled)||errors.Is(acquisition,context.DeadlineExceeded){result.Cancelled=true}
    if errors.Is(acquisition,errGHAGuardCredentialCleanup)||errors.Is(acquisition,errGHAGuardResponseCleanup)||state=="failed" {
        result.CleanupFailed=true
        result.Qualified=false
    }
    if !result.Qualified&&result.Reason=="guard_target_artifact_unavailable"&&acquisition!=nil {
        result.Reason=ghaFreshAcquisitionReason(acquisition)
    }
    if result.Qualified&&state!="completed" {
        result.Qualified=false
        result.Reason="guard_github_credential_cleanup_unobserved"
    }
    return ghaFreshWorkerResult{guardTargetMetadataResult:result,CredentialCleanupState:state}
}

// Called only after the existing inherited-pipe and exact-parent worker check.
// Candidate, purpose and target metadata stay inside this same owned worker.
func runGHAFreshInspectionWorker(args []string,out io.Writer) int {
    if len(args)!=1||args[0]!=ghaFreshWorkerFlag||out==nil{return 2}
    ctx,cancel:=context.WithTimeout(context.Background(),3*time.Minute)
    defer cancel()
    audit:=&ghaFreshCredentialAudit{state:"unknown"}
    result:=ghaFreshInspectWith(ctx,audit,func(c context.Context)(guardTargetArtifact,error){
        return guardTargetArtifactFromServer(c,func(inner context.Context)(*ghaGuardVerifiedCandidate,error){
            return ghaFreshOpenVerifiedCandidateObserved(inner,audit)
        })
    })
    if json.NewEncoder(out).Encode(result)!=nil||!result.Qualified{return 1}
    return 0
}

func ghaFreshPositiveDecimal(s string) bool {
    v,err:=strconv.ParseUint(s,10,64)
    return err==nil&&v>0&&strconv.FormatUint(v,10)==s
}

// Worker output is a metadata observation only. Even a fully valid result
// cannot authorize file preparation, replacement, data access or execution.
func ghaFreshDecodeInspection(child askChildResult) (ghaFreshWorkerResult,bool) {
    var result ghaFreshWorkerResult
    if !child.Started||!child.Assigned||!child.WaitVerified||!child.Reaped||
        child.TimedOut||child.Exit!=0||len(child.Raw)==0||len(child.Raw)>65536 {
        return result,false
    }
    raw:=bytes.TrimSpace(child.Raw)
    if _,err:=ghaFreshObject(raw);err!=nil{return result,false}
    dec:=json.NewDecoder(bytes.NewReader(raw));dec.DisallowUnknownFields()
    if dec.Decode(&result)!=nil||dec.Decode(new(any))!=io.EOF{return ghaFreshWorkerResult{},false}
    canonical,err:=json.Marshal(result)
    if err!=nil||!bytes.Equal(raw,canonical){return ghaFreshWorkerResult{},false}
    id:=result.CurrentIdentity
    if !result.Qualified||result.Reason!="verified_guard_target_metadata"||
        result.Domain!=guardTargetUpdateDomain||!result.MetadataObserved||id==nil||
        !ghaFreshPositiveDecimal(id.Volume)||!ghaFreshPositiveDecimal(id.FileID)||
        !ghaFreshPositiveDecimal(id.Created)||!ghaGuardHex(result.ACLHash,64)||
        !ghaGuardHex(result.ParentProof,64)||result.Bytes<=0||result.Bytes>64<<20||
        result.Cancelled||result.CleanupFailed||result.SoftwareReplaceAllowed||
        result.DataAccessGranted||result.UpdatePerformed||result.CredentialCleanupState!="completed" {
        return ghaFreshWorkerResult{},false
    }
    return result,true
}

func ghaFreshSupervisedInspection() ghaFreshInspectionResult {
    failure:=ghaFreshInspectionResult{guardTargetMetadataResult:guardTargetMetadataResult{Domain:guardTargetUpdateDomain,Reason:"guard_target_worker_unqualified"},CredentialCleanupState:"unknown"}
    executable,err:=os.Executable();if err!=nil{return failure}
    // This deadline bounds credential acquisition, network operations and all
    // child processes as one flight; it is not a grant to modify the target.
    ctx,cancel:=context.WithTimeout(context.Background(),3*time.Minute+30*time.Second)
    ctx=context.WithValue(ctx,askStrictJobKey{},true)
    child:=askOwnedChild(ctx,executable,ghaFreshWorkerFlag)
    cancel()
    defer clear(child.Raw)
    return ghaFreshInspectionFromChild(child)
}

func ghaFreshInspectionFromChild(child askChildResult) ghaFreshInspectionResult {
    failure:=ghaFreshInspectionResult{
        guardTargetMetadataResult:guardTargetMetadataResult{Domain:guardTargetUpdateDomain,Reason:"guard_target_worker_unqualified"},
        CredentialCleanupState:"unknown",WorkerStarted:child.Started,WorkerAssigned:child.Assigned,
        WorkerWaitVerified:child.WaitVerified,WorkerReaped:child.Reaped,WorkerTimedOut:child.TimedOut,
    }
    if child.Started&&!child.Reaped {
        failure.CleanupFailed=true;failure.Cancelled=child.TimedOut
        failure.Reason="guard_target_cleanup_failed";return failure
    }
    if child.TimedOut {failure.Cancelled=true;failure.Reason="guard_target_cancelled";return failure}
    if value,ok:=ghaFreshDecodeInspection(child);ok {
        failure.guardTargetMetadataResult=value.guardTargetMetadataResult
        failure.CredentialCleanupState=value.CredentialCleanupState
        failure.WorkerResultObserved=true
        return failure
    }
    // Preserve only fixed failure facts from a properly exited child. Native
    // worker lifecycle facts above never come from the child's JSON.
    if child.Started&&child.Assigned&&child.WaitVerified&&child.Reaped&&child.Exit==1 {
        raw:=bytes.TrimSpace(child.Raw)
        if _,err:=ghaFreshObject(raw);err==nil {
            var value ghaFreshWorkerResult
            dec:=json.NewDecoder(bytes.NewReader(raw));dec.DisallowUnknownFields()
            if dec.Decode(&value)==nil&&dec.Decode(new(any))==io.EOF&&!value.Qualified&&value.Domain==guardTargetUpdateDomain {
                if value.CredentialCleanupState!="unknown"&&value.CredentialCleanupState!="completed"&&value.CredentialCleanupState!="failed" {return failure}
                failure.WorkerResultObserved=true
                failure.CredentialCleanupState=value.CredentialCleanupState
                failure.Cancelled=value.Cancelled
                failure.CleanupFailed=value.CleanupFailed||value.CredentialCleanupState=="failed"
                switch value.Reason {
                case "guard_target_cancelled","guard_github_artifact_expired","guard_github_credential_not_found",
                    "guard_github_credential_access_denied","guard_github_credential_identity_mismatch",
                    "guard_github_credential_unauthorized","guard_github_credential_forbidden",
                    "guard_github_credential_cleanup_failed","guard_github_credential_cleanup_unobserved","guard_github_response_cleanup_failed",
                    "guard_target_artifact_unavailable","guard_target_purpose_unavailable",
                    "guard_target_metadata_unavailable","guard_target_generation_changed",
                    "guard_target_cleanup_failed":
                    failure.Reason=value.Reason
                    failure.Cancelled=failure.Cancelled||value.Reason=="guard_target_cancelled"
                    failure.CleanupFailed=failure.CleanupFailed||value.Reason=="guard_target_cleanup_failed"||value.Reason=="guard_github_credential_cleanup_failed"||value.Reason=="guard_github_response_cleanup_failed"
                default:
                    if ghaFreshHTTPReasonAllowed(value.Reason){failure.Reason=value.Reason}
                }
            }
        }
    }
    return failure
}

func runGHAFreshInspection(args []string,out io.Writer) int {
    if len(args)!=1||args[0]!=ghaFreshInspectionFlag||out==nil{return 2}
    result:=ghaFreshSupervisedInspection()
    if json.NewEncoder(out).Encode(result)!=nil||!result.Qualified{return 1}
    return 0
}
