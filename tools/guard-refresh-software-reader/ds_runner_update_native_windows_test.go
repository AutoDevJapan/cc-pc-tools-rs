//go:build windows

package main

import (
    "context"
    "errors"
    "os"
    "path/filepath"
    "syscall"
    "strings"
    "testing"
    "time"
    "unsafe"
)

func TestDSRunnerNativeMetadataHandleHasNoReadDataAccess(t *testing.T){
    root:=physicalSoftwareFixture(t);path:=filepath.Join(root,"metadata-only.exe");if err:=os.WriteFile(path,[]byte("owned bytes"),0600);err!=nil{t.Fatal(err)}
    file,err:=dsRunnerOpenMetadata(path,false);if err!=nil{t.Fatal(err)};defer file.Close();if _,err=dsRunnerKernelID(file,false);err!=nil{t.Fatal(err)};if acl,err:=updateFileACL(file);err!=nil||acl==""{t.Fatal("metadata/ACL unavailable")}
    var n uint32;err=syscall.ReadFile(syscall.Handle(file.Fd()),make([]byte,1),&n,nil);if !errors.Is(err,syscall.ERROR_ACCESS_DENIED)||n!=0{t.Fatal("metadata handle read file data or fixture missed the access check")}
}
func TestDSRunnerNativeOwnedPrepareRetainsBytesAndExactACL(t *testing.T){
    root:=physicalSoftwareFixture(t);paths:=[3]string{filepath.Join(root,"current.exe"),filepath.Join(root,"stage.exe"),filepath.Join(root,"before.exe")};if err:=os.WriteFile(paths[0],[]byte("owned old"),0600);err!=nil{t.Fatal(err)}
    ctx,cancel:=context.WithTimeout(context.Background(),5*time.Second);defer cancel();permit:=&dsRunnerWorkerPermit{ctx:ctx};permit.self=permit;candidate:=dsRunnerFixtureCandidate([]byte("owned new"));defer candidate.Close()
    err:=candidate.with(ctx,func(payload []byte)(resultErr error){
        purpose:=dsRunnerRepairPurpose{candidate:candidate,pin:candidate.pin,permit:permit};session,err:=dsRunnerOpenNativeTargetAt(ctx,purpose,paths);if err!=nil{return err};defer func(){resultErr=errors.Join(resultErr,session.Close())}()
        old,oldID,err:=session.Read(ctx,dsRunnerCurrent);if err!=nil{return err};defer clear(old)
        backup,created,err:=session.Ensure(ctx,dsRunnerSaved,old);if err!=nil||!created{return errors.Join(err,errors.New("backup not created"))};stage,created,err:=session.Ensure(ctx,dsRunnerStaged,payload);if err!=nil||!created{return errors.Join(err,errors.New("stage not created"))}
        if stage.acl!=oldID.acl||backup.acl!=oldID.acl||stage.parent!=oldID.parent||backup.parent!=oldID.parent{return errors.New("ACL or parent changed")}
        if err=os.WriteFile(paths[0],[]byte("overwrite"),0600);err==nil{return errors.New("current body not retained")};if err=os.WriteFile(paths[1],[]byte("overwrite"),0600);err==nil{return errors.New("stage body not retained")}
        if _,created,err=session.Ensure(ctx,dsRunnerStaged,[]byte("other candidate"));!errors.Is(err,errDSRunnerCollision)||created{return errors.New("existing stage overwritten or adopted")}
        if err=session.Recheck(ctx,dsRunnerCurrent,oldID);err!=nil{return err};if err=session.Recheck(ctx,dsRunnerStaged,stage);err!=nil{return err};return session.Recheck(ctx,dsRunnerSaved,backup)
    });if err!=nil{t.Fatal(err)}
    if raw,err:=os.ReadFile(paths[0]);err!=nil||string(raw)!="owned old"{t.Fatal("prepare changed target")};if raw,err:=os.ReadFile(paths[1]);err!=nil||string(raw)!="owned new"{t.Fatal("prepared stage missing")};if raw,err:=os.ReadFile(paths[2]);err!=nil||string(raw)!="owned old"{t.Fatal("backup missing")}
}

// Test-only fixed categories. A joined error may originate from cleanup;
// it does not independently identify ReplaceFileW as the failing operation.
type dsRunnerNativeDiagnostic struct {
    dsRunnerTargetSession
    publish string
    old, stage, post dsRunnerSnapshot
    postRead, postOK bool
}
func(d *dsRunnerNativeDiagnostic) Publish(ctx context.Context,old,stage,backup dsRunnerSnapshot)(bool,error){
    d.old,d.stage=old,stage
    committed,err:=d.dsRunnerTargetSession.Publish(ctx,old,stage,backup)
    switch {
    case committed && err==nil: d.publish="success"
    case committed: d.publish="success_post_error"
    case errors.Is(err,errDSRunnerPublishNotAttempted): d.publish="not_attempted"
    case errors.Is(err,syscall.Errno(32)): d.publish="joined_sharing_violation"
    case errors.Is(err,syscall.Errno(5)): d.publish="joined_access_denied"
    case errors.Is(err,syscall.Errno(2))||errors.Is(err,syscall.Errno(3)): d.publish="joined_not_found"
    case errors.Is(err,syscall.Errno(1175)): d.publish="joined_remove_replaced_failed"
    case errors.Is(err,syscall.Errno(1176)): d.publish="joined_move_replacement_failed"
    case errors.Is(err,syscall.Errno(1177)): d.publish="joined_move_replacement_after_remove_failed"
    default: d.publish="other_or_unknown"
    }
    return committed,err
}
func(d *dsRunnerNativeDiagnostic) Read(ctx context.Context,leaf dsRunnerLeaf)([]byte,dsRunnerSnapshot,error){
    raw,snapshot,err:=d.dsRunnerTargetSession.Read(ctx,leaf)
    if d.publish=="success"&&leaf==dsRunnerCurrent{d.postRead=true;if err==nil{d.postOK=true;d.post=snapshot}}
    return raw,snapshot,err
}

func TestDSRunnerNativeOwnedApplyRechecksInstalledGeneration(t *testing.T){
    root:=physicalSoftwareFixture(t);paths:=[3]string{filepath.Join(root,"current.exe"),filepath.Join(root,"stage.exe"),filepath.Join(root,"before.exe")};oldBytes:=[]byte("owned predecessor");newBytes:=[]byte("owned successor");if err:=os.WriteFile(paths[0],oldBytes,0600);err!=nil{t.Fatal(err)}
    ctx,cancel:=context.WithTimeout(context.Background(),5*time.Second);defer cancel();permit:=&dsRunnerWorkerPermit{ctx:ctx};permit.self=permit
    var diag *dsRunnerNativeDiagnostic
    access:=dsRunnerMaintenanceAccess{acquire:func(context.Context)(*dsRunnerCandidate,error){return dsRunnerFixtureCandidate(newBytes),nil},open:func(ctx context.Context,p dsRunnerRepairPurpose)(dsRunnerTargetSession,error){
        session,err:=dsRunnerOpenNativeTargetAt(ctx,p,paths);if err!=nil||session==nil{return session,err}
        diag=&dsRunnerNativeDiagnostic{dsRunnerTargetSession:session,publish:"not_called"};return diag,nil
    }}
    result:=executeDSRunnerMaintenanceFor(ctx,"apply",permit,access,digest(oldBytes),int64(len(oldBytes)))
    if !result.Qualified||!result.UpdatePerformed||!result.InstalledVerified||result.CommitOutcome!=dsRunnerCommitConfirmed||result.Current==nil||result.Stage==nil||result.Backup==nil||result.Current.FileID!=result.Stage.FileID{
        category:="not_opened";postRead,postOK,idMatch,createdMatch,aclMatch,parentMatch:=false,false,false,false,false,false
        if diag!=nil{
            category=diag.publish;postRead,postOK=diag.postRead,diag.postOK
            if postOK{idMatch=diag.post.id.Volume==diag.stage.id.Volume&&diag.post.id.FileID==diag.stage.id.FileID;createdMatch=diag.post.id.Created==diag.old.id.Created;aclMatch=diag.post.acl==diag.old.acl;parentMatch=diag.post.parent==diag.old.parent}
            if postOK&&!aclMatch{t.Log(dsRunnerACLDiagnostic(diag.old.acl,diag.post.acl))}
        }
        createdStage,createdBackup:=false,false;for _,name:=range result.Created{if name==dsRunnerStage{createdStage=true};if name==dsRunnerBackup{createdBackup=true}}
        currentStageID:=result.Current!=nil&&result.Stage!=nil&&result.Current.FileID==result.Stage.FileID
        t.Fatalf("owned native replacement did not complete: reason=%s outcome=%s attempted=%t performed=%t cleanup_failed=%t created_count=%d created_stage=%t created_backup=%t current_present=%t stage_present=%t backup_present=%t current_stage_id_equal=%t publish=%s post_read=%t post_ok=%t post_stage_id_equal=%t post_old_creation_equal=%t post_old_acl_equal=%t post_old_parent_equal=%t",result.Reason,result.CommitOutcome,result.CommitAttempted,result.UpdatePerformed,result.CleanupFailed,len(result.Created),createdStage,createdBackup,result.Current!=nil,result.Stage!=nil,result.Backup!=nil,currentStageID,category,postRead,postOK,idMatch,createdMatch,aclMatch,parentMatch)
    }
    if raw,err:=os.ReadFile(paths[0]);err!=nil||string(raw)!=string(newBytes){t.Fatal("installed bytes mismatch")};if raw,err:=os.ReadFile(paths[2]);err!=nil||string(raw)!=string(oldBytes){t.Fatal("backup mismatch")};if _,err:=os.Stat(paths[1]);!os.IsNotExist(err){t.Fatal("stage name did not move")}
    if result.DataAccessGranted||result.SourceAccessGranted||result.SendAllowed||result.SoftwareReplaceAllowed||result.NamespaceLeaseProved{t.Fatal("physical software result issued another authority")}
}
type dsRunnerFixtureCloser struct{calls int;err error}
func(c *dsRunnerFixtureCloser)Close()error{c.calls++;return c.err}
func TestDSRunnerCloseOwnedMarksFailureOnceAndNeverRetries(t *testing.T){
    failure:=errors.New("synthetic close failure")
    for _,test:=range []struct{name string;err error;cleanup bool}{{"success",nil,false},{"already-closed",os.ErrClosed,false},{"failure",failure,true}}{t.Run(test.name,func(t *testing.T){
        c:=&dsRunnerFixtureCloser{err:test.err};err:=dsRunnerCloseOwned(c)
        if c.calls!=1||errors.Is(err,errDSRunnerCleanup)!=test.cleanup||test.cleanup&&!errors.Is(err,failure)||!test.cleanup&&err!=nil{t.Fatal("close fact was lost, duplicated or retried")}
    })}
}
// A zero os.File reports an error from Close without owning any handle. It
// stands in for a failed close of a body, metadata or parent handle.
func TestDSRunnerNativeCloseKeepsFailedHandlesAndReportsThemAgain(t *testing.T){
    failing:=func()*os.File{return new(os.File)}
    if failing().Close()==nil{t.Fatal("fixture missed the close failure")}
    n:=&dsRunnerNativeTarget{objects:map[dsRunnerLeaf]*dsRunnerNativeObject{dsRunnerCurrent:{file:failing(),held:failing()}},parents:[]*os.File{failing()}}
    if err:=n.Close();!errors.Is(err,errDSRunnerCleanup)||len(n.objects)!=0||len(n.parents)!=0||!n.closed||len(n.unclosed)!=3{t.Fatal("native close dropped a failed handle or its cleanup fact")}
    if err:=n.Close();!errors.Is(err,errDSRunnerCleanup)||len(n.unclosed)!=3{t.Fatal("a second close claimed success or closed a failed handle again")}
    clean:=&dsRunnerNativeTarget{objects:map[dsRunnerLeaf]*dsRunnerNativeObject{}}
    if clean.Close()!=nil||clean.Close()!=nil||len(clean.unclosed)!=0{t.Fatal("a session without failed handles reported a cleanup failure")}
}
// The operation that met a failed close may return another error, so the first
// Close of the session has to report a handle that stayed referenced earlier.
func TestDSRunnerNativeFirstCloseReportsAHandleRetainedEarlier(t *testing.T){
    n:=&dsRunnerNativeTarget{objects:map[dsRunnerLeaf]*dsRunnerNativeObject{}}
    if n.closeOwned(new(os.File))==nil||len(n.unclosed)!=1{t.Fatal("fixture missed the close failure or the handle was not retained")}
    if err:=n.Close();!errors.Is(err,errDSRunnerCleanup)||len(n.unclosed)!=1{t.Fatal("the first close dropped a handle retained earlier")}
    if err:=n.Close();!errors.Is(err,errDSRunnerCleanup)||len(n.unclosed)!=1{t.Fatal("a second close claimed success or closed the failed handle again")}
}
func TestDSRunnerNativeRejectedMetadataHandleIsReturnedOnlyWhenItsCloseFailed(t *testing.T){
    if f,err:=dsRunnerRejectOpened(new(os.File),errDSRunnerEvidence);f==nil||!errors.Is(err,errDSRunnerEvidence)||!errors.Is(err,errDSRunnerCleanup){t.Fatal("a rejected handle whose close failed was not handed back with both facts")}
    root:=physicalSoftwareFixture(t);path:=filepath.Join(root,"rejected.exe");if err:=os.WriteFile(path,[]byte("owned bytes"),0600);err!=nil{t.Fatal(err)}
    file,err:=dsRunnerOpenMetadata(path,false);if err!=nil{t.Fatal(err)}
    if f,err:=dsRunnerRejectOpened(file,errDSRunnerEvidence);f!=nil||!errors.Is(err,errDSRunnerEvidence)||errors.Is(err,errDSRunnerCleanup){t.Fatal("a rejected handle that closed cleanly was handed back or marked")}
}

// A regular owned fixture whose DACL starts without automatic inheritance.
// The native update must restore the exact descriptor, not merely ignore it.
func dsRunnerOwnedACLFixture(t *testing.T,path string,auto bool)string{
    t.Helper()
    ptr,err:=syscall.UTF16PtrFromString(path);if err!=nil{t.Fatal(err)}
    h,err:=syscall.CreateFile(ptr,0x60080,syscall.FILE_SHARE_READ|syscall.FILE_SHARE_DELETE,nil,syscall.OPEN_EXISTING,0x00200000,0);if err!=nil{t.Fatal("owned ACL fixture open",err)}
    var descriptor uintptr
    code,_,_:=updateGetSecurity.Call(uintptr(h),1,4,0,0,0,0,uintptr(unsafe.Pointer(&descriptor)))
    if code!=0||descriptor==0{_ = syscall.CloseHandle(h);t.Fatal("owned ACL fixture descriptor")}
    bits:=uintptr(0);if auto{bits=0x0400}
    ok,_,_:=updateAdvapi.NewProc("SetSecurityDescriptorControl").Call(descriptor,0x0400,bits)
    status,_,_:=updateNTSetSecurity.Call(uintptr(h),4,descriptor)
    free,_,_:=updateLocalFree.Call(descriptor);closeErr:=syscall.CloseHandle(h)
    if ok==0||status!=0||free!=0||closeErr!=nil{t.Fatal("owned ACL fixture setup failed")}
    f,err:=dsRunnerOpenMetadata(path,false);if err!=nil{t.Fatal(err)}
    before,err:=updateFileACL(f);if closeErr:=f.Close();err!=nil||closeErr!=nil{t.Fatal("before descriptor unavailable")}
    _,_,flags,_,valid:=dsRunnerSDDLParts(before);if !valid||strings.Contains(flags,"AI")!=auto{t.Fatal("owned ACL fixture inheritance mode mismatch")}
    return before
}
func dsRunnerOwnedNonAutoFixture(t *testing.T,path string)string{return dsRunnerOwnedACLFixture(t,path,false)}

func TestDSRunnerNativeOwnedApplyRestoresExactNonInheritedDACL(t *testing.T){
    root:=physicalSoftwareFixture(t);paths:=[3]string{filepath.Join(root,"current.exe"),filepath.Join(root,"stage.exe"),filepath.Join(root,"before.exe")}
    oldBytes,newBytes:=[]byte("owned predecessor"),[]byte("owned successor")
    if err:=os.WriteFile(paths[0],oldBytes,0600);err!=nil{t.Fatal(err)}
    before:=dsRunnerOwnedNonAutoFixture(t,paths[0])
    ctx,cancel:=context.WithTimeout(context.Background(),10*time.Second);defer cancel();permit:=&dsRunnerWorkerPermit{ctx:ctx};permit.self=permit
    faults:=&dsRunnerACLTestFaults{}
    access:=dsRunnerMaintenanceAccess{acquire:func(context.Context)(*dsRunnerCandidate,error){return dsRunnerFixtureCandidate(newBytes),nil},open:func(ctx context.Context,p dsRunnerRepairPurpose)(dsRunnerTargetSession,error){session,err:=dsRunnerOpenNativeTargetAt(ctx,p,paths);if err!=nil{return session,err};n:=session.(*dsRunnerNativeTarget);n.faults=faults;return n,nil}}
    result:=executeDSRunnerMaintenanceFor(ctx,"apply",permit,access,digest(oldBytes),int64(len(oldBytes)))
    if !result.Qualified||!result.UpdatePerformed||!result.InstalledVerified||result.CommitOutcome!=dsRunnerCommitConfirmed{t.Fatal("non-inherited ACL update failed",result.Reason)}
    if faults.setterCalls!=1{t.Fatal("non-inherited fixture did not exercise restoration")}
    f,err:=dsRunnerOpenMetadata(paths[0],false);if err!=nil{t.Fatal(err)};after,err:=updateFileACL(f);closeErr:=f.Close()
    if err!=nil||closeErr!=nil||after!=before{t.Fatal("exact original descriptor was not restored")}
    if raw,err:=os.ReadFile(paths[0]);err!=nil||string(raw)!=string(newBytes){t.Fatal("candidate bytes not installed")}
    if raw,err:=os.ReadFile(paths[2]);err!=nil||string(raw)!=string(oldBytes){t.Fatal("backup changed")}
}

// If the descriptor already matches after replacement, no DACL write is needed.
// The assertions fail if this fixture did not actually exercise that branch.
func TestDSRunnerNativeOwnedApplySkipsExactACLSetter(t *testing.T){
    root:=physicalSoftwareFixture(t);paths:=[3]string{filepath.Join(root,"current.exe"),filepath.Join(root,"stage.exe"),filepath.Join(root,"before.exe")}
    oldBytes,newBytes:=[]byte("owned predecessor"),[]byte("owned successor")
    if err:=os.WriteFile(paths[0],oldBytes,0600);err!=nil{t.Fatal(err)}
    before:=dsRunnerOwnedACLFixture(t,paths[0],true)
    ctx,cancel:=context.WithTimeout(context.Background(),10*time.Second);defer cancel();permit:=&dsRunnerWorkerPermit{ctx:ctx};permit.self=permit
    faults:=&dsRunnerACLTestFaults{}
    access:=dsRunnerMaintenanceAccess{acquire:func(context.Context)(*dsRunnerCandidate,error){return dsRunnerFixtureCandidate(newBytes),nil},open:func(ctx context.Context,p dsRunnerRepairPurpose)(dsRunnerTargetSession,error){session,err:=dsRunnerOpenNativeTargetAt(ctx,p,paths);if err!=nil{return session,err};n,ok:=session.(*dsRunnerNativeTarget);if !ok{return nil,errDSRunnerEvidence};n.faults=faults;return n,nil}}
    result:=executeDSRunnerMaintenanceFor(ctx,"apply",permit,access,digest(oldBytes),int64(len(oldBytes)))
    if !result.Qualified||!result.UpdatePerformed||!result.InstalledVerified||result.CommitOutcome!=dsRunnerCommitConfirmed||faults.setterCalls!=0{t.Fatal("exact ACL control did not succeed without a setter",result.Reason)}
    f,err:=dsRunnerOpenMetadata(paths[0],false);if err!=nil{t.Fatal(err)};after,err:=updateFileACL(f);closeErr:=f.Close()
    if err!=nil||closeErr!=nil||after!=before{t.Fatal("exact ACL control changed the descriptor")}
    if raw,err:=os.ReadFile(paths[0]);err!=nil||string(raw)!=string(newBytes){t.Fatal("candidate bytes not installed")}
}

// Negative controls only remove permission to proceed after a real owned-file
// ReplaceFile success. They never supply candidate bytes or force success.
func TestDSRunnerNativePostCommitRejectsUnsafeACLRestoration(t *testing.T){
    for _,mode:=range []string{"owner","group","identity","parent","bytes","cancel"}{t.Run(mode,func(t *testing.T){
        root:=physicalSoftwareFixture(t)
        oldBytes,newBytes:=[]byte("owned predecessor"),[]byte("owned successor")
        var controlACL string
        // Run the real restoration first under the same directory and exact
        // predecessor descriptor as the fault case. A skipped setter is a failed
        // fixture, not a successful negative control.
        for _,trial:=range []string{"control",mode}{
            paths:=[3]string{filepath.Join(root,trial+"-current.exe"),filepath.Join(root,trial+"-stage.exe"),filepath.Join(root,trial+"-before.exe")}
            if err:=os.WriteFile(paths[0],oldBytes,0600);err!=nil{t.Fatal(err)}
            acl:=dsRunnerOwnedNonAutoFixture(t,paths[0]);if trial=="control"{controlACL=acl}else if acl!=controlACL{t.Fatal("fault and control fixtures have different descriptors")}
            ctx,cancel:=context.WithTimeout(context.Background(),10*time.Second);defer cancel();permit:=&dsRunnerWorkerPermit{ctx:ctx};permit.self=permit
            faults:=&dsRunnerACLTestFaults{}
            switch trial{case "owner":faults.ownerMismatch=true;case "group":faults.groupMismatch=true;case "identity":faults.identityMismatch=true;case "parent":faults.parentMismatch=true;case "bytes":faults.bytesMismatch=true;case "cancel":faults.cancel=cancel}
            access:=dsRunnerMaintenanceAccess{acquire:func(context.Context)(*dsRunnerCandidate,error){return dsRunnerFixtureCandidate(newBytes),nil},open:func(ctx context.Context,p dsRunnerRepairPurpose)(dsRunnerTargetSession,error){
                session,err:=dsRunnerOpenNativeTargetAt(ctx,p,paths);if err!=nil{return session,err}
                concrete,ok:=session.(*dsRunnerNativeTarget);if !ok{return nil,errDSRunnerEvidence};concrete.faults=faults;return concrete,nil
            }}
            result:=executeDSRunnerMaintenanceFor(ctx,"apply",permit,access,digest(oldBytes),int64(len(oldBytes)))
            if trial=="control"{
                if !result.Qualified||!result.InstalledVerified||!result.UpdatePerformed||result.CommitOutcome!=dsRunnerCommitConfirmed||faults.setterCalls!=1{t.Fatal("control did not perform one ACL restoration",mode,result.Reason)}
            }else{
                if !result.CommitAttempted||!result.UpdatePerformed||result.CommitOutcome!=dsRunnerCommitConfirmed||result.Qualified||result.InstalledVerified||result.Reason!="commit_outcome_requires_reconciliation"{t.Fatal("post-commit failure lost confirmed fact",mode,result.Reason)}
                if faults.setterCalls!=0{t.Fatal("unsafe precondition reached ACL setter",mode)}
            }
            if got,err:=os.ReadFile(paths[2]);err!=nil||string(got)!=string(oldBytes){t.Fatal("backup not retained",mode,trial)}
        }
    })}
}

func TestDSRunnerRestoreMetadataAloneDoesNotReplaceBodyExclusion(t *testing.T){
    root:=physicalSoftwareFixture(t);name:=filepath.Join(root,"owned.exe")
    if err:=os.WriteFile(name,[]byte("owned"),0600);err!=nil{t.Fatal(err)}
    meta,err:=dsRunnerOpenMetadataAccess(name,false,true);if err!=nil{t.Fatal("metadata with write DACL unavailable",err)};defer meta.Close()
    ptr,err:=syscall.UTF16PtrFromString(name);if err!=nil{t.Fatal(err)}
    bodyHandle,err:=syscall.CreateFile(ptr,syscall.GENERIC_READ|0x20000,syscall.FILE_SHARE_READ|syscall.FILE_SHARE_DELETE,nil,syscall.OPEN_EXISTING,0x00200000,0)
    if err!=nil{t.Fatal("body open while metadata held",err)}
    body:=os.NewFile(uintptr(bodyHandle),name);if body==nil{_ = syscall.CloseHandle(bodyHandle);t.Fatal("body wrapper unavailable")};defer body.Close()
    other,err:=syscall.CreateFile(ptr,syscall.GENERIC_WRITE,syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE,nil,syscall.OPEN_EXISTING,0,0)
    if err==nil{_ = syscall.CloseHandle(other);t.Fatal("body allowed a competing writer")}
    if !errors.Is(err,syscall.Errno(32)){t.Fatal("competing writer was not blocked by sharing",err)}
    a,err:=dsRunnerKernelID(meta,false);if err!=nil{t.Fatal(err)};b,err:=dsRunnerKernelID(body,false)
    if err!=nil||a!=b{t.Fatal("metadata and body are different files")}
}

func TestDSRunnerNativePostCommitACLSetterFailureIsReconciled(t *testing.T){
    root:=physicalSoftwareFixture(t);paths:=[3]string{filepath.Join(root,"current.exe"),filepath.Join(root,"stage.exe"),filepath.Join(root,"before.exe")}
    oldBytes,newBytes:=[]byte("owned predecessor"),[]byte("owned successor")
    if err:=os.WriteFile(paths[0],oldBytes,0600);err!=nil{t.Fatal(err)}
    ptr,err:=syscall.UTF16PtrFromString(paths[0]);if err!=nil{t.Fatal(err)}
    h,err:=syscall.CreateFile(ptr,0x60080,syscall.FILE_SHARE_READ|syscall.FILE_SHARE_DELETE,nil,syscall.OPEN_EXISTING,0x00200000,0);if err!=nil{t.Fatal("owned ACL fixture open",err)}
    var descriptor uintptr
    code,_,_:=updateGetSecurity.Call(uintptr(h),1,4,0,0,0,0,uintptr(unsafe.Pointer(&descriptor)))
    if code!=0||descriptor==0{_ = syscall.CloseHandle(h);t.Fatal("owned ACL fixture descriptor")}
    ok,_,_:=updateAdvapi.NewProc("SetSecurityDescriptorControl").Call(descriptor,0x0400,0)
    status,_,_:=updateNTSetSecurity.Call(uintptr(h),4,descriptor)
    free,_,_:=updateLocalFree.Call(descriptor);closeErr:=syscall.CloseHandle(h)
    if ok==0||status!=0||free!=0||closeErr!=nil{t.Fatal("owned ACL fixture setup failed")}
    f,err:=dsRunnerOpenMetadata(paths[0],false);if err!=nil{t.Fatal(err)}
    before,err:=updateFileACL(f);if closeErr:=f.Close();err!=nil||closeErr!=nil{t.Fatal("before descriptor unavailable")}
    _,_,flags,_,valid:=dsRunnerSDDLParts(before);if !valid||strings.Contains(flags,"AI"){t.Fatal("fixture retained auto-inherited flag")}
    ctx,cancel:=context.WithTimeout(context.Background(),10*time.Second);defer cancel();permit:=&dsRunnerWorkerPermit{ctx:ctx};permit.self=permit
    faults:=&dsRunnerACLTestFaults{denySet:true}
    access:=dsRunnerMaintenanceAccess{acquire:func(context.Context)(*dsRunnerCandidate,error){return dsRunnerFixtureCandidate(newBytes),nil},open:func(ctx context.Context,p dsRunnerRepairPurpose)(dsRunnerTargetSession,error){session,err:=dsRunnerOpenNativeTargetAt(ctx,p,paths);if err!=nil{return session,err};n:=session.(*dsRunnerNativeTarget);n.faults=faults;return n,nil}}
    result:=executeDSRunnerMaintenanceFor(ctx,"apply",permit,access,digest(oldBytes),int64(len(oldBytes)))
    if result.Qualified||result.InstalledVerified||!result.UpdatePerformed||!result.CommitAttempted||result.CommitOutcome!=dsRunnerCommitConfirmed||result.Reason!="commit_outcome_requires_reconciliation"{t.Fatal("setter failure lost confirmed commit")}
    if faults.setterCalls!=1{t.Fatal("fixture did not reach a single setter attempt")}
    if raw,err:=os.ReadFile(paths[2]);err!=nil||string(raw)!=string(oldBytes){t.Fatal("backup changed")}
}


func TestDSRunnerNativeApplyWithoutDACLRightStopsBeforeReplacement(t *testing.T){
    root:=physicalSoftwareFixture(t);paths:=[3]string{filepath.Join(root,"current.exe"),filepath.Join(root,"stage.exe"),filepath.Join(root,"before.exe")}
    oldBytes,newBytes:=[]byte("owned predecessor"),[]byte("owned successor");if err:=os.WriteFile(paths[0],oldBytes,0600);err!=nil{t.Fatal(err)}
    ctx,cancel:=context.WithTimeout(context.Background(),10*time.Second);defer cancel();permit:=&dsRunnerWorkerPermit{ctx:ctx};permit.self=permit
    access:=dsRunnerMaintenanceAccess{acquire:func(context.Context)(*dsRunnerCandidate,error){return dsRunnerFixtureCandidate(newBytes),nil},open:func(ctx context.Context,p dsRunnerRepairPurpose)(dsRunnerTargetSession,error){return dsRunnerOpenNativeTargetMode(ctx,p,paths,false)}}
    result:=executeDSRunnerMaintenanceFor(ctx,"apply",permit,access,digest(oldBytes),int64(len(oldBytes)))
    if result.Qualified||result.InstalledVerified||result.CommitAttempted||result.UpdatePerformed||result.CommitOutcome!=dsRunnerCommitNotAttempted {t.Fatal("apply without DACL right attempted replacement")}
    if got,err:=os.ReadFile(paths[0]);err!=nil||string(got)!=string(oldBytes){t.Fatal("predecessor changed")}
}

func TestDSRunnerNativePostCommitACLCleanupFailureIsReconciled(t *testing.T){
    root:=physicalSoftwareFixture(t);paths:=[3]string{filepath.Join(root,"current.exe"),filepath.Join(root,"stage.exe"),filepath.Join(root,"before.exe")}
    oldBytes,newBytes:=[]byte("owned predecessor"),[]byte("owned successor")
    if err:=os.WriteFile(paths[0],oldBytes,0600);err!=nil{t.Fatal(err)}
    ptr,err:=syscall.UTF16PtrFromString(paths[0]);if err!=nil{t.Fatal(err)}
    h,err:=syscall.CreateFile(ptr,0x60080,syscall.FILE_SHARE_READ|syscall.FILE_SHARE_DELETE,nil,syscall.OPEN_EXISTING,0x00200000,0);if err!=nil{t.Fatal("owned ACL fixture open",err)}
    var descriptor uintptr
    code,_,_:=updateGetSecurity.Call(uintptr(h),1,4,0,0,0,0,uintptr(unsafe.Pointer(&descriptor)))
    if code!=0||descriptor==0{_ = syscall.CloseHandle(h);t.Fatal("owned ACL fixture descriptor")}
    ok,_,_:=updateAdvapi.NewProc("SetSecurityDescriptorControl").Call(descriptor,0x0400,0)
    status,_,_:=updateNTSetSecurity.Call(uintptr(h),4,descriptor)
    free,_,_:=updateLocalFree.Call(descriptor);closeErr:=syscall.CloseHandle(h)
    if ok==0||status!=0||free!=0||closeErr!=nil{t.Fatal("owned ACL fixture setup failed")}
    f,err:=dsRunnerOpenMetadata(paths[0],false);if err!=nil{t.Fatal(err)}
    before,err:=updateFileACL(f);if closeErr:=f.Close();err!=nil||closeErr!=nil{t.Fatal("before descriptor unavailable")}
    _,_,flags,_,valid:=dsRunnerSDDLParts(before);if !valid||strings.Contains(flags,"AI"){t.Fatal("fixture retained auto-inherited flag")}
    ctx,cancel:=context.WithTimeout(context.Background(),10*time.Second);defer cancel();permit:=&dsRunnerWorkerPermit{ctx:ctx};permit.self=permit
    faults:=&dsRunnerACLTestFaults{postSetFailure:true}
    access:=dsRunnerMaintenanceAccess{acquire:func(context.Context)(*dsRunnerCandidate,error){return dsRunnerFixtureCandidate(newBytes),nil},open:func(ctx context.Context,p dsRunnerRepairPurpose)(dsRunnerTargetSession,error){session,err:=dsRunnerOpenNativeTargetAt(ctx,p,paths);if err!=nil{return session,err};n:=session.(*dsRunnerNativeTarget);n.faults=faults;return n,nil}}
    result:=executeDSRunnerMaintenanceFor(ctx,"apply",permit,access,digest(oldBytes),int64(len(oldBytes)))
    if result.Qualified||result.InstalledVerified||!result.UpdatePerformed||!result.CommitAttempted||result.CommitOutcome!=dsRunnerCommitConfirmed||!result.CleanupFailed||result.Reason!="commit_outcome_requires_reconciliation"{t.Fatal("post-set cleanup failure lost confirmed commit")}
    if faults.setterCalls!=1{t.Fatal("fixture did not reach a single setter attempt")}
    if raw,err:=os.ReadFile(paths[2]);err!=nil||string(raw)!=string(oldBytes){t.Fatal("backup changed")}
}
