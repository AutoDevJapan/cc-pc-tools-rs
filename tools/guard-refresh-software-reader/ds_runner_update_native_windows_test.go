//go:build windows

package main

import (
    "context"
    "errors"
    "os"
    "path/filepath"
    "syscall"
    "testing"
    "time"
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

func TestDSRunnerNativeOwnedApplyRechecksInstalledGeneration(t *testing.T){
    root:=physicalSoftwareFixture(t);paths:=[3]string{filepath.Join(root,"current.exe"),filepath.Join(root,"stage.exe"),filepath.Join(root,"before.exe")};oldBytes:=[]byte("owned predecessor");newBytes:=[]byte("owned successor");if err:=os.WriteFile(paths[0],oldBytes,0600);err!=nil{t.Fatal(err)}
    ctx,cancel:=context.WithTimeout(context.Background(),5*time.Second);defer cancel();permit:=&dsRunnerWorkerPermit{ctx:ctx};permit.self=permit
    access:=dsRunnerMaintenanceAccess{acquire:func(context.Context)(*dsRunnerCandidate,error){return dsRunnerFixtureCandidate(newBytes),nil},open:func(ctx context.Context,p dsRunnerRepairPurpose)(dsRunnerTargetSession,error){return dsRunnerOpenNativeTargetAt(ctx,p,paths)}}
    result:=executeDSRunnerMaintenanceFor(ctx,"apply",permit,access,digest(oldBytes),int64(len(oldBytes)))
    if !result.Qualified||!result.UpdatePerformed||!result.InstalledVerified||result.CommitOutcome!=dsRunnerCommitConfirmed||result.Current==nil||result.Stage==nil||result.Backup==nil||result.Current.FileID!=result.Stage.FileID{t.Fatalf("owned native replacement did not complete: %s",result.Reason)}
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
