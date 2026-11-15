//go:build windows

package main

import (
    "bytes"
    "context"
    "encoding/json"
    "errors"
    "strings"
    "testing"
)

func ghaFreshTestInspection() guardTargetMetadataResult {
    return guardTargetMetadataResult{
        Qualified:true,Reason:"verified_guard_target_metadata",Domain:guardTargetUpdateDomain,
        MetadataObserved:true,CurrentIdentity:&softwareIdentity{Volume:"1",FileID:"2",Created:"3"},
        ACLHash:strings.Repeat("a",64),ParentProof:strings.Repeat("b",64),Bytes:10,
    }
}

func ghaFreshTestChild(t *testing.T) askChildResult {
    t.Helper()
    raw,err:=json.Marshal(ghaFreshWorkerResult{guardTargetMetadataResult:ghaFreshTestInspection(),CredentialCleanupState:"completed"});if err!=nil{t.Fatal(err)}
    return askChildResult{Raw:raw,Exit:0,Started:true,Assigned:true,WaitVerified:true,Reaped:true}
}

func TestGHAFreshInspectionRejectsUnreapedOrUnboundWorker(t *testing.T) {
    child:=ghaFreshTestChild(t)
    result,ok:=ghaFreshDecodeInspection(child)
    if !ok||!result.Qualified||result.UpdatePerformed||result.DataAccessGranted||result.SoftwareReplaceAllowed{
        t.Fatal("metadata observation did not preserve its narrow purpose")
    }
    for name,change:=range map[string]func(*askChildResult){
        "not-started":func(c *askChildResult){c.Started=false},
        "not-assigned":func(c *askChildResult){c.Assigned=false},
        "not-waited":func(c *askChildResult){c.WaitVerified=false},
        "not-reaped":func(c *askChildResult){c.Reaped=false},
        "timeout":func(c *askChildResult){c.TimedOut=true},
        "exit-failed":func(c *askChildResult){c.Exit=1},
    } {
        t.Run(name,func(t *testing.T){
            changed:=ghaFreshTestChild(t);change(&changed)
            if _,ok:=ghaFreshDecodeInspection(changed);ok{t.Fatal("invalid worker accepted")}
        })
    }
    child.Reaped=false;child.TimedOut=true
    failed:=ghaFreshInspectionFromChild(child)
    if failed.Qualified||!failed.Cancelled||!failed.CleanupFailed||failed.Reason!="guard_target_cleanup_failed"{
        t.Fatal("unreaped timeout was not preserved")
    }
}

func TestGHAFreshInspectionCannotInventUpdatePermission(t *testing.T) {
    for name,change:=range map[string]func(*guardTargetMetadataResult){
        "other-domain":func(r *guardTargetMetadataResult){r.Domain="other"},
        "no-id":func(r *guardTargetMetadataResult){r.CurrentIdentity=nil},
        "fake-id":func(r *guardTargetMetadataResult){r.CurrentIdentity.FileID="0"},
        "no-acl":func(r *guardTargetMetadataResult){r.ACLHash=""},
        "other-reason":func(r *guardTargetMetadataResult){r.Reason="verified"},
        "cleanup":func(r *guardTargetMetadataResult){r.CleanupFailed=true},
        "replace":func(r *guardTargetMetadataResult){r.SoftwareReplaceAllowed=true},
        "data":func(r *guardTargetMetadataResult){r.DataAccessGranted=true},
        "performed":func(r *guardTargetMetadataResult){r.UpdatePerformed=true},
    } {
        t.Run(name,func(t *testing.T){
            changed:=ghaFreshTestInspection();change(&changed)
            raw,err:=json.Marshal(ghaFreshWorkerResult{guardTargetMetadataResult:changed,CredentialCleanupState:"completed"});if err!=nil{t.Fatal(err)}
            child:=ghaFreshTestChild(t);child.Raw=raw
            if _,ok:=ghaFreshDecodeInspection(child);ok{t.Fatal("invalid result accepted")}
        })
    }
    child:=ghaFreshTestChild(t)
    child.Raw=append(append([]byte(nil),bytes.TrimSuffix(child.Raw,[]byte("}"))...),[]byte(`,"qualified":true}`)...)
    if _,ok:=ghaFreshDecodeInspection(child);ok{t.Fatal("duplicate field accepted")}
}

func TestGHAFreshInspectionPreservesCredentialReasonOnly(t *testing.T) {
    for err,want:=range map[error]string{
        errGHAGuardCredentialExpired:"guard_github_artifact_expired",
        errGHAGuardCredentialNotFound:"guard_github_credential_not_found",
        errGHAGuardCredentialUnauthorized:"guard_github_credential_unauthorized",
        errGHAGuardCredentialForbidden:"guard_github_credential_forbidden",
        errors.New("private detail"):"guard_target_artifact_unavailable",
        &ghaFreshHTTPStatusError{phase:"runs",status:403}:"guard_fresh_public_http_403",
        &ghaFreshHTTPStatusError{phase:"metadata",status:429}:"guard_fresh_public_http_429",
    } {
        if got:=ghaFreshAcquisitionReason(err);got!=want{t.Fatalf("reason %q != %q",got,want)}
    }
    failed:=guardTargetMetadataResult{Domain:guardTargetUpdateDomain,Reason:"guard_github_credential_not_found"}
    raw,_:=json.Marshal(ghaFreshWorkerResult{guardTargetMetadataResult:failed,CredentialCleanupState:"unknown"})
    child:=askChildResult{Raw:raw,Exit:1,Started:true,Assigned:true,WaitVerified:true,Reaped:true}
    got:=ghaFreshInspectionFromChild(child)
    if got.Qualified||got.Reason!="guard_github_credential_not_found"||got.CurrentIdentity!=nil{t.Fatal("failure projection incorrect")}
    failed.Reason="guard_fresh_public_http_403";child.Raw,_=json.Marshal(ghaFreshWorkerResult{guardTargetMetadataResult:failed,CredentialCleanupState:"unknown"})
    if got=ghaFreshInspectionFromChild(child);got.Qualified||got.Reason!="guard_fresh_public_http_403"{t.Fatal("HTTP status lost")}
    failed.Reason="guard_fresh_public_http_999";child.Raw,_=json.Marshal(ghaFreshWorkerResult{guardTargetMetadataResult:failed,CredentialCleanupState:"unknown"})
    if got=ghaFreshInspectionFromChild(child);got.Reason!="guard_target_worker_unqualified"{t.Fatal("invalid HTTP status accepted")}
}

func TestGHAFreshWorkerRejectsForeignArguments(t *testing.T) {
    var out bytes.Buffer
    if runGHAFreshInspectionWorker([]string{"wrong"},&out)!=2||out.Len()!=0{t.Fatal("foreign worker command accepted")}
    if runGHAFreshInspection([]string{"wrong"},&out)!=2||out.Len()!=0{t.Fatal("foreign inspection command accepted")}
}

func TestGHAFreshInspectionPreservesCompositeFailureFlags(t *testing.T) {
    cases:=[]struct{name,reason,state,want string;cancelled,cleanup,wantCancel,wantCleanup bool}{
        {"http-cancel","guard_github_job_log_http_401","unknown","guard_github_job_log_http_401",true,false,true,false},
        {"http-cleanup","guard_github_artifact_http_403","failed","guard_github_artifact_http_403",false,true,false,true},
        {"http-both","guard_github_metadata_http_429","failed","guard_github_metadata_http_429",true,true,true,true},
        {"unknown-both","guard_fresh_public_http_999","failed","guard_target_worker_unqualified",true,true,true,true},
        {"ordinary","guard_target_artifact_unavailable","unknown","guard_target_artifact_unavailable",true,true,true,true},
        {"cleanup-reason","guard_github_credential_cleanup_failed","failed","guard_github_credential_cleanup_failed",false,false,false,true},
    }
    for _,tc:=range cases {t.Run(tc.name,func(t *testing.T){
        value:=ghaFreshWorkerResult{guardTargetMetadataResult:guardTargetMetadataResult{Domain:guardTargetUpdateDomain,Reason:tc.reason,Cancelled:tc.cancelled,CleanupFailed:tc.cleanup},CredentialCleanupState:tc.state}
        raw,err:=json.Marshal(value);if err!=nil{t.Fatal(err)}
        child:=askChildResult{Raw:raw,Exit:1,Started:true,Assigned:true,WaitVerified:true,Reaped:true}
        result:=ghaFreshInspectionFromChild(child)
        if result.Qualified||result.Reason!=tc.want||result.Cancelled!=tc.wantCancel||
           result.CleanupFailed!=tc.wantCleanup||result.CredentialCleanupState!=tc.state||
           !result.WorkerStarted||!result.WorkerAssigned||!result.WorkerWaitVerified||!result.WorkerReaped||
           !result.WorkerResultObserved||result.CurrentIdentity!=nil||
           result.SoftwareReplaceAllowed||result.DataAccessGranted||result.UpdatePerformed {
            t.Fatalf("composite facts lost or promoted: %+v",result)
        }
    })}
}

func TestGHAFreshInspectionDoesNotBorrowWorkerReapClaims(t *testing.T) {
    child:=ghaFreshTestChild(t)
    child.Raw=append(append([]byte(nil),bytes.TrimSuffix(child.Raw,[]byte("}"))...),[]byte(`,"worker_reaped":true}`)...)
    child.Reaped=false
    result:=ghaFreshInspectionFromChild(child)
    if result.Qualified||result.WorkerReaped||result.WorkerResultObserved||result.CredentialCleanupState!="unknown"{t.Fatal("worker JSON supplied parent reap")}
    child.Reaped=true
    result=ghaFreshInspectionFromChild(child)
    if result.Qualified||result.WorkerResultObserved||!result.WorkerReaped{t.Fatal("unknown worker field accepted")}
}

func TestGHAFreshInspectionRequiresObservedCredentialCleanup(t *testing.T) {
    value:=ghaFreshWorkerResult{guardTargetMetadataResult:ghaFreshTestInspection(),CredentialCleanupState:"unknown"}
    raw,err:=json.Marshal(value);if err!=nil{t.Fatal(err)}
    child:=ghaFreshTestChild(t);child.Raw=raw
    if _,ok:=ghaFreshDecodeInspection(child);ok{t.Fatal("unobserved close accepted")}
    failure:=ghaFreshInspectionFromChild(askChildResult{})
    if failure.WorkerStarted||failure.WorkerReaped||failure.CredentialCleanupState!="unknown"{t.Fatal("unstarted worker observed as complete")}
}

func TestGHAFreshStatusAndCleanupRemainSeparate(t *testing.T) {
    audit:=&ghaFreshCredentialAudit{state:"failed"}
    joined:=errors.Join(ghaGuardHTTPFailure("job_log",403),errGHAGuardCredentialCleanup,context.Canceled)
    result:=ghaFreshInspectWith(context.Background(),audit,func(context.Context)(guardTargetArtifact,error){return nil,joined})
    if result.Qualified||result.Reason!="guard_github_job_log_http_403"||!result.Cancelled||!result.CleanupFailed||result.CredentialCleanupState!="failed"{t.Fatal("joined outcome lost")}
    raw,err:=json.Marshal(result);if err!=nil{t.Fatal(err)}
    parent:=ghaFreshInspectionFromChild(askChildResult{Raw:raw,Exit:1,Started:true,Assigned:true,WaitVerified:true,Reaped:true})
    if parent.Qualified||parent.Reason!="guard_github_job_log_http_403"||!parent.Cancelled||!parent.CleanupFailed||parent.CredentialCleanupState!="failed"||!parent.WorkerReaped||!parent.WorkerResultObserved{t.Fatal("reaped parent lost child cleanup failure")}
}
