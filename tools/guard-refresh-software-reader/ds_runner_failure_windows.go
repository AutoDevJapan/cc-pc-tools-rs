//go:build windows

package main

import (
    "context"
    "errors"
    "strconv"
    "strings"
)

// A failure reason says where a maintenance run stopped, so that the first real
// runs are not blind. It is one token from the fixed vocabulary below: the stage
// that stopped and, for a server answer, its numeric HTTP status. It never
// carries a URL, response text, credential, path or raw error. No success,
// admission, update or retry decision depends on its value, and a qualified
// record cannot carry one. Its form is still checked: a worker record whose
// reason is outside the vocabulary or misplaced is not forwarded.
type dsRunnerFailureError struct{reason string}

func (e *dsRunnerFailureError) Error() string {
    if e==nil{return "ds_runner_failure_unavailable"}
    return e.reason
}

const dsRunnerFailureCancelled = "ds_runner_cancelled"
const dsRunnerFailureDeadline = "ds_runner_deadline_exceeded"

// A phase is one fixed server call or local check of the evidence flow.
var dsRunnerFailurePhases = []string{
    "owner","repository","workflow","run","jobs","artifacts",
    "producer_compare","producer_file","source_commit","source_tree",
    "job_log","job_log_object","artifact","artifact_object","archive",
    "attestation","trust_root","signature","recheck",
}
var dsRunnerFailureKinds = []string{"unavailable","malformed","too_large","paginated","mismatch","expired","absent"}

// Fixed reasons that are not a phase and kind pair. The target reasons name a
// step of the installed-target operations; a capture run never opens the target.
var dsRunnerFailureFixed = map[string]bool{
    "ds_runner_capture_not_bound":true,
    "ds_runner_credential_not_found":true,
    "ds_runner_credential_access_denied":true,
    "ds_runner_credential_unavailable":true,
    dsRunnerFailureCancelled:true,
    dsRunnerFailureDeadline:true,
    "ds_runner_target_open_failed":true,
    "ds_runner_target_read_failed":true,
    "ds_runner_target_snapshot_invalid":true,
    "ds_runner_target_predecessor_mismatch":true,
    "ds_runner_target_changed":true,
    "ds_runner_target_backup_failed":true,
    "ds_runner_target_stage_failed":true,
    "ds_runner_target_leaf_mismatch":true,
    "ds_runner_target_postcheck_failed":true,
    "ds_runner_target_postcheck_mismatch":true,
}

func dsRunnerFailureReasonAllowed(reason string)bool{
    if dsRunnerFailureFixed[reason]{return true}
    rest,ok:=strings.CutPrefix(reason,"ds_runner_");if !ok{return false}
    for _,phase:=range dsRunnerFailurePhases{
        tail,found:=strings.CutPrefix(rest,phase+"_");if !found{continue}
        for _,kind:=range dsRunnerFailureKinds{if tail==kind{return true}}
        code,isStatus:=strings.CutPrefix(tail,"http_")
        if isStatus{
            n,err:=strconv.Atoi(code)
            if err==nil&&n>=100&&n<=599&&strconv.Itoa(n)==code{return true}
        }
    }
    return false
}

// An unknown token returns nil, so free text can never become a reason.
func dsRunnerFailure(reason string)error{
    if !dsRunnerFailureReasonAllowed(reason){return nil}
    return &dsRunnerFailureError{reason:reason}
}
func dsRunnerPhaseFailure(phase,kind string)error{return dsRunnerFailure("ds_runner_"+phase+"_"+kind)}
func dsRunnerHTTPFailure(phase string,status int)error{return dsRunnerFailure("ds_runner_"+phase+"_http_"+strconv.Itoa(status))}

// The primary evidence failure stays; one fixed reason is added next to it.
func dsRunnerEvidenceReason(phase,kind string)error{return errors.Join(errDSRunnerEvidence,dsRunnerPhaseFailure(phase,kind))}
func dsRunnerHTTPEvidence(phase string,status int)error{return errors.Join(errDSRunnerEvidence,dsRunnerHTTPFailure(phase,status))}

func dsRunnerContextReason(err error)string{
    if errors.Is(err,context.DeadlineExceeded){return dsRunnerFailureDeadline}
    if errors.Is(err,context.Canceled){return dsRunnerFailureCancelled}
    return ""
}

// The context is the root cause once it has ended, whichever stage reported it.
// Only a typed error from the vocabulary becomes a reason; an error whose text
// merely looks like one does not.
func dsRunnerFailureReason(err error)string{
    if err==nil{return ""}
    if reason:=dsRunnerContextReason(err);reason!=""{return reason}
    var typed *dsRunnerFailureError
    if errors.As(err,&typed)&&typed!=nil&&dsRunnerFailureReasonAllowed(typed.reason){return typed.reason}
    return ""
}
func dsRunnerFailureReasonFor(ctx context.Context,err error)string{
    if err==nil{return ""}
    if ctx!=nil&&ctx.Err()!=nil{
        if reason:=dsRunnerContextReason(ctx.Err());reason!=""{return reason}
    }
    return dsRunnerFailureReason(err)
}

func dsRunnerCredentialFailure(err error)error{
    switch{
    case errors.Is(err,errGHAGuardCredentialNotFound):return dsRunnerFailure("ds_runner_credential_not_found")
    case errors.Is(err,errGHAGuardCredentialAccessDenied):return dsRunnerFailure("ds_runner_credential_access_denied")
    }
    return dsRunnerFailure("ds_runner_credential_unavailable")
}

// The phase is derived only from the fixed GitHub endpoint. It is reporting,
// not authentication and not a source of an allowed URL.
func dsRunnerEndpointPhase(endpoint string)string{
    if endpoint=="https://api.github.com/user"{return "owner"}
    if endpoint==dsRunnerAPI{return "repository"}
    rest,ok:=strings.CutPrefix(endpoint,dsRunnerAPI+"/");if !ok{return ""}
    switch{
    case rest=="actions/workflows/task-runner-capture.yml":return "workflow"
    case strings.HasPrefix(rest,"actions/runs/")&&strings.HasSuffix(rest,"/attempts/1/jobs?per_page=100"):return "jobs"
    case strings.HasPrefix(rest,"actions/runs/")&&strings.HasSuffix(rest,"/artifacts?per_page=100"):return "artifacts"
    case strings.HasPrefix(rest,"actions/runs/"):return "run"
    case strings.HasPrefix(rest,"actions/jobs/")&&strings.HasSuffix(rest,"/logs"):return "job_log"
    case strings.HasPrefix(rest,"actions/artifacts/")&&strings.HasSuffix(rest,"/zip"):return "artifact"
    case strings.HasPrefix(rest,"compare/"):return "producer_compare"
    case strings.HasPrefix(rest,"contents/"):return "producer_file"
    case strings.HasPrefix(rest,"git/commits/"):return "source_commit"
    case strings.HasPrefix(rest,"git/trees/"):return "source_tree"
    case strings.HasPrefix(rest,"attestations/"):return "attestation"
    }
    return ""
}

// The guard downloader reports its own fixed phase and numeric status. A status
// of a phase outside this vocabulary is not forwarded under another phase.
func dsRunnerLiveFailure(phase string,err error)error{
    var status *ghaGuardHTTPStatusError
    if errors.As(err,&status)&&status!=nil{
        if reason:=dsRunnerHTTPFailure(status.phase,status.status);reason!=nil{return errors.Join(err,reason)}
    }
    return errors.Join(err,dsRunnerPhaseFailure(phase,"unavailable"))
}

// A collision already has its own result reason and is kept as it is.
func dsRunnerTargetFailure(err error,reason string)error{
    if err==nil||errors.Is(err,errDSRunnerCollision){return err}
    return errors.Join(err,dsRunnerFailure(reason))
}

// A reason is accepted only in a failed record, and a capture record has no
// target step to name.
func dsRunnerFailureReasonFits(op string,r dsRunnerMaintenanceResult)bool{
    if r.FailureReason==""{return true}
    if r.Qualified||!dsRunnerFailureReasonAllowed(r.FailureReason){return false}
    if op=="capture"&&strings.HasPrefix(r.FailureReason,"ds_runner_target_"){return false}
    return true
}

// Written only by the caller, when no trustworthy worker record exists. These
// tokens are outside the worker vocabulary, so a worker cannot claim them.
func dsRunnerOuterFailureReason(child askChildResult)string{
    switch{
    case !child.Started:return "ds_runner_worker_not_started"
    case child.TimedOut:return "ds_runner_worker_timed_out"
    case !child.Assigned||!child.WaitVerified||!child.Reaped:return "ds_runner_worker_not_confirmed"
    case child.Exit!=0&&child.Exit!=1:return "ds_runner_worker_exit_other"
    case !child.ResultStdoutComplete||!child.ResultStderrComplete:return "ds_runner_worker_output_incomplete"
    }
    return "ds_runner_worker_record_invalid"
}
