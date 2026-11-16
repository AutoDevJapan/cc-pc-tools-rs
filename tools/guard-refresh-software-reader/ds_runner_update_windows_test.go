//go:build windows

package main

import (
    "context"
    "bytes"
    "encoding/json"
    "io"
    "errors"
    "os"
    "testing"
    "time"
)

type dsRunnerSyntheticStore struct{raw map[dsRunnerLeaf][]byte;meta map[dsRunnerLeaf]dsRunnerSnapshot;failStage,failClose,failPublish,failBeforePublish,failPublishClose,failAfterCommit,mutateCurrent bool;creates,publishes,closes,opens int;committed bool;cancelAfterCommit func()}
type dsRunnerSyntheticTarget struct{store *dsRunnerSyntheticStore;purpose dsRunnerRepairPurpose}
func dsRunnerSyntheticSnapshot(id string,size int)dsRunnerSnapshot{return dsRunnerSnapshot{id:softwareIdentity{Volume:"7",FileID:id,Created:"11"},parent:softwareIdentity{Volume:"7",FileID:"3",Created:"5"},acl:"synthetic-acl",size:int64(size)}}
func(s *dsRunnerSyntheticStore)access()dsRunnerMaintenanceAccess{return dsRunnerMaintenanceAccess{acquire:func(context.Context)(*dsRunnerCandidate,error){return dsRunnerFixtureCandidate([]byte("new candidate")),nil},open:func(ctx context.Context,p dsRunnerRepairPurpose)(dsRunnerTargetSession,error){if !dsRunnerPurposeValid(p){return nil,errDSRunnerEvidence};s.opens++;return &dsRunnerSyntheticTarget{store:s,purpose:p},nil}}}
func(t *dsRunnerSyntheticTarget)Read(ctx context.Context,l dsRunnerLeaf)([]byte,dsRunnerSnapshot,error){if ctx.Err()!=nil||!dsRunnerPurposeValid(t.purpose)||t.store.failAfterCommit&&t.store.committed&&l==dsRunnerCurrent{return nil,dsRunnerSnapshot{},errDSRunnerEvidence};raw,ok:=t.store.raw[l];if !ok{return nil,dsRunnerSnapshot{},errDSRunnerEvidence};return append([]byte(nil),raw...),t.store.meta[l],nil}
func(t *dsRunnerSyntheticTarget)Ensure(ctx context.Context,l dsRunnerLeaf,raw []byte)(dsRunnerSnapshot,bool,error){s:=t.store;if ctx.Err()!=nil||!dsRunnerPurposeValid(t.purpose){return dsRunnerSnapshot{},false,errDSRunnerEvidence};if l==dsRunnerStaged&&s.failStage{return dsRunnerSnapshot{},false,errors.New("synthetic stage I/O")};if prior,ok:=s.raw[l];ok{if digest(prior)!=digest(raw){return dsRunnerSnapshot{},false,errDSRunnerCollision};return s.meta[l],false,nil};s.creates++;s.raw[l]=append([]byte(nil),raw...);id:="21";if l==dsRunnerStaged{id="23"};s.meta[l]=dsRunnerSyntheticSnapshot(id,len(raw));return s.meta[l],true,nil}
func(t *dsRunnerSyntheticTarget)Recheck(ctx context.Context,l dsRunnerLeaf,want dsRunnerSnapshot)error{if ctx.Err()!=nil||!dsRunnerPurposeValid(t.purpose){return errDSRunnerEvidence};s:=t.store;if s.mutateCurrent&&len(s.raw)==3&&l==dsRunnerCurrent{m:=s.meta[l];m.id.FileID="99";s.meta[l]=m};if s.meta[l]!=want{return errDSRunnerEvidence};return nil}
func(t *dsRunnerSyntheticTarget)Publish(ctx context.Context,current,stage,backup dsRunnerSnapshot)(bool,error){s:=t.store;s.publishes++;if s.failPublish{return false,errors.New("synthetic publish I/O")};if s.failBeforePublish{m:=s.meta[dsRunnerStaged];m.id.FileID="98";s.meta[dsRunnerStaged]=m};if t.Recheck(ctx,dsRunnerCurrent,current)!=nil||t.Recheck(ctx,dsRunnerStaged,stage)!=nil||t.Recheck(ctx,dsRunnerSaved,backup)!=nil{return false,errors.Join(errDSRunnerPublishNotAttempted,errDSRunnerEvidence)};s.raw[dsRunnerCurrent]=append([]byte(nil),s.raw[dsRunnerStaged]...);after:=stage;after.id.Created=current.id.Created;s.meta[dsRunnerCurrent]=after;delete(s.raw,dsRunnerStaged);delete(s.meta,dsRunnerStaged);s.committed=true;if s.cancelAfterCommit!=nil{s.cancelAfterCommit()};if s.failPublishClose{return true,errors.Join(errDSRunnerEvidence,errDSRunnerCleanup,errors.New("synthetic post-publication close"))};return true,nil}
func(t *dsRunnerSyntheticTarget)Close()error{t.store.closes++;if t.store.failClose{return errors.New("synthetic close I/O")};return nil}
func dsRunnerSyntheticInitial()*dsRunnerSyntheticStore{return &dsRunnerSyntheticStore{raw:map[dsRunnerLeaf][]byte{dsRunnerCurrent:[]byte("old installed")},meta:map[dsRunnerLeaf]dsRunnerSnapshot{dsRunnerCurrent:dsRunnerSyntheticSnapshot("13",len("old installed"))}}}
func dsRunnerSyntheticRun(ctx context.Context,op string,access dsRunnerMaintenanceAccess)dsRunnerMaintenanceResult{p:=&dsRunnerWorkerPermit{ctx:ctx};p.self=p;return executeDSRunnerMaintenanceFor(ctx,op,p,access,digest([]byte("old installed")),int64(len("old installed")))}
func dsRunnerSyntheticExecute(ctx context.Context,op string,s *dsRunnerSyntheticStore)dsRunnerMaintenanceResult{return dsRunnerSyntheticRun(ctx,op,s.access())}
func TestDSRunnerMaintenanceVerifyDoesNotCreateLeaves(t *testing.T){s:=dsRunnerSyntheticInitial();r:=dsRunnerSyntheticExecute(context.Background(),"verify",s);if !r.Qualified||r.Prepared||r.UpdatePerformed||s.creates!=0||s.publishes!=0||s.opens!=1||s.closes!=1||r.SoftwareReplaceAllowed||r.DataAccessGranted||r.SourceAccessGranted||r.SendAllowed||r.NamespaceLeaseProved{t.Fatal("verify crossed an authority or I/O boundary")}}
func TestDSRunnerMaintenancePrepareThenApplyRevalidates(t *testing.T){s:=dsRunnerSyntheticInitial();before:=string(s.raw[dsRunnerCurrent]);r:=dsRunnerSyntheticExecute(context.Background(),"prepare",s);if !r.Qualified||!r.Prepared||r.UpdatePerformed||s.creates!=2||string(s.raw[dsRunnerCurrent])!=before{t.Fatal("prepare changed target or missed real leaves")};r=dsRunnerSyntheticExecute(context.Background(),"apply",s);if !r.Qualified||!r.Prepared||!r.UpdatePerformed||!r.InstalledVerified||s.creates!=2||s.publishes!=1||string(s.raw[dsRunnerCurrent])!="new candidate"||string(s.raw[dsRunnerSaved])!=before||s.closes!=2{t.Fatal("prepared objects were not revalidated and installed")};r=dsRunnerSyntheticExecute(context.Background(),"apply",s);if !r.InstalledVerified||r.UpdatePerformed||s.publishes!=1{t.Fatal("already-installed retry performed another replacement")}}
func TestDSRunnerMaintenancePartialCreateFailureRetainsAndRetries(t *testing.T){s:=dsRunnerSyntheticInitial();s.failStage=true;r:=dsRunnerSyntheticExecute(context.Background(),"prepare",s);if r.Qualified||r.Prepared||len(r.Created)!=1||s.creates!=1||string(s.raw[dsRunnerSaved])!="old installed"||string(s.raw[dsRunnerCurrent])!="old installed"||s.publishes!=0{t.Fatal("partial prepare lost owned backup or claimed success")};s.failStage=false;r=dsRunnerSyntheticExecute(context.Background(),"apply",s);if !r.Qualified||!r.UpdatePerformed||s.creates!=2||s.publishes!=1||string(s.raw[dsRunnerSaved])!="old installed"{t.Fatal("retry discarded the partial backup or missed revalidation")}}
func TestDSRunnerMaintenanceCollisionIsPreserved(t *testing.T){s:=dsRunnerSyntheticInitial();s.raw[dsRunnerStaged]=[]byte("unrelated bytes");s.meta[dsRunnerStaged]=dsRunnerSyntheticSnapshot("77",len("unrelated bytes"));r:=dsRunnerSyntheticExecute(context.Background(),"apply",s);if r.Qualified||r.Reason!="fixed_leaf_collision_preserved"||s.publishes!=0||string(s.raw[dsRunnerStaged])!="unrelated bytes"||s.meta[dsRunnerStaged].id.FileID!="77"{t.Fatal("collision was adopted or overwritten")}}
func TestDSRunnerMaintenanceGenerationChangeStopsBeforePublish(t *testing.T){s:=dsRunnerSyntheticInitial();s.mutateCurrent=true;r:=dsRunnerSyntheticExecute(context.Background(),"apply",s);if r.Qualified||r.CommitAttempted||r.CommitOutcome!=dsRunnerCommitNotAttempted||s.publishes!=0||len(s.raw)!=3{t.Fatal("changed generation was published or partial leaves lost")}}
func TestDSRunnerMaintenanceCommitAndReadbackFailureAreSeparate(t *testing.T){for _,kind:=range []string{"publish-failure","after-commit-read","close-failure","publish-close-failure","cancel-after-commit"}{t.Run(kind,func(t *testing.T){ctx,cancel:=context.WithCancel(context.Background());defer cancel();s:=dsRunnerSyntheticInitial();s.failPublish=kind=="publish-failure";s.failAfterCommit=kind=="after-commit-read";s.failClose=kind=="close-failure";s.failPublishClose=kind=="publish-close-failure";if kind=="cancel-after-commit"{s.cancelAfterCommit=cancel};r:=dsRunnerSyntheticExecute(ctx,"apply",s);if r.Qualified||r.InstalledVerified||!r.CommitAttempted||s.publishes!=1||r.UpdatePerformed!=(kind!="publish-failure")||s.closes!=1{t.Fatal("failure confused commit observation with successful installed verification")};want:=dsRunnerCommitConfirmed;if kind=="publish-failure"{want=dsRunnerCommitUnconfirmed};if r.CommitOutcome!=want||r.CleanupFailed!=(kind=="close-failure"||kind=="publish-close-failure")||r.Reason!="commit_outcome_requires_reconciliation"{t.Fatal("commit outcome or cleanup fact was lost")};if string(s.raw[dsRunnerSaved])!="old installed"{t.Fatal("failure deleted the backup")}})}}
func TestDSRunnerMaintenanceRejectsDetachedOrCancelledWorker(t *testing.T){for _,kind:=range []string{"detached","cancelled","operation"}{t.Run(kind,func(t *testing.T){ctx,cancel:=context.WithCancel(context.Background());defer cancel();permit:=&dsRunnerWorkerPermit{ctx:ctx};permit.self=permit;op:="apply";switch kind{case "detached":permit.self=nil;case "cancelled":cancel();case "operation":op="other"};calls:=0;r:=executeDSRunnerMaintenanceFor(ctx,op,permit,dsRunnerMaintenanceAccess{acquire:func(context.Context)(*dsRunnerCandidate,error){calls++;return nil,nil}},"",0);if r.Qualified||calls!=0{t.Fatal("unqualified worker reached artifact or target")}})}}

type dsRunnerShortResultWriter struct{}
func(dsRunnerShortResultWriter)Write(p []byte)(int,error){if len(p)==0{return 0,nil};return len(p)-1,nil}
func TestDSRunnerNormalCallerUsesOwnedWorkerAndRejectsIncompleteResult(t *testing.T){
    for _,kind:=range []string{"normal","not-reaped","exit-failure","wrong-domain","wrong-operation","data-grant","trailing-json","short-write","wait-unverified","timed-out"}{t.Run(kind,func(t *testing.T){
        calls:=0;var out bytes.Buffer;var writer io.Writer=&out;if kind=="short-write"{writer=dsRunnerShortResultWriter{}}
        launch:=func(ctx context.Context,exe string,args ...string)askChildResult{calls++;if ctx==nil||exe==""||len(args)!=2||args[0]!=dsRunnerWorkerFlag||args[1]!="verify"{t.Fatal("caller did not use the fixed owned worker")};r:=dsRunnerMaintenanceResult{Domain:"ds-runner-fixed-software-maintenance-v1",Operation:"verify",Qualified:true,Reason:"verified_no_write",CommitOutcome:dsRunnerCommitNotAttempted};if kind=="wrong-domain"{r.Domain="other"};if kind=="wrong-operation"{r.Operation="apply"};if kind=="data-grant"{r.DataAccessGranted=true};raw,err:=json.Marshal(r);if err!=nil{t.Fatal(err)};raw=append(raw,'\n');if kind=="trailing-json"{raw=append(raw,[]byte("{}")...)};result:=askChildResult{Raw:raw,Exit:0,Started:true,Assigned:true,WaitVerified:true,Reaped:true};if kind=="not-reaped"{result.Reaped=false};if kind=="wait-unverified"{result.WaitVerified=false};if kind=="timed-out"{result.TimedOut=true};if kind=="exit-failure"{result.Exit=1};return result}
        code:=runDSRunnerMaintenanceWith([]string{"--verify-ds-runner-update"},writer,launch);if calls!=1{t.Fatal("caller did not perform exactly one worker attempt")};if kind=="normal"{var r dsRunnerMaintenanceResult;if code!=0||json.Unmarshal(out.Bytes(),&r)!=nil||!r.Qualified||!r.Reaped||r.DataAccessGranted||r.SourceAccessGranted||r.SendAllowed{t.Fatal("normal worker result lost")}}else if code==0{t.Fatal("incomplete worker/output was reported as successful")}
    })}
}
func TestDSRunnerNormalCallerRejectsUnknownInputsBeforeSpawn(t *testing.T){
    for _,args:=range [][]string{nil,{"--other"},{"--verify-ds-runner-update","--path","other"},{"--apply-ds-runner-update","--sha256","caller"}}{calls:=0;code:=runDSRunnerMaintenanceWith(args,io.Discard,func(context.Context,string,...string)askChildResult{calls++;return askChildResult{}});if code!=2||calls!=0{t.Fatal("caller-controlled inputs reached a worker")}}
    if runDSRunnerMaintenanceWorker([]string{dsRunnerWorkerFlag,"other"},io.Discard)!=2{t.Fatal("unknown worker action accepted")}
}
func TestDSRunnerMaintenanceClosesPartialConstructorResults(t *testing.T){
    ctx:=context.Background();permit:=&dsRunnerWorkerPermit{ctx:ctx};permit.self=permit;candidate:=dsRunnerFixtureCandidate([]byte("new candidate"));opened:=0
    r:=executeDSRunnerMaintenanceFor(ctx,"verify",permit,dsRunnerMaintenanceAccess{acquire:func(context.Context)(*dsRunnerCandidate,error){return candidate,errors.New("synthetic acquire failure")},open:func(context.Context,dsRunnerRepairPurpose)(dsRunnerTargetSession,error){opened++;return nil,nil}},"",0)
    candidate.mu.Lock();closed,raw:=candidate.closed,len(candidate.raw);candidate.mu.Unlock();if r.Qualified||opened!=0||!closed||raw!=0{t.Fatal("partial artifact constructor leaked a candidate")}
    store:=dsRunnerSyntheticInitial();access:=store.access();access.open=func(_ context.Context,p dsRunnerRepairPurpose)(dsRunnerTargetSession,error){return &dsRunnerSyntheticTarget{store:store,purpose:p},errors.New("synthetic target failure")}
    r=executeDSRunnerMaintenanceFor(ctx,"verify",permit,access,digest([]byte("old installed")),int64(len("old installed")));if r.Qualified||store.closes!=1||store.creates!=0{t.Fatal("partial target constructor leaked a session")}
}

func TestDSRunnerCaptureVerificationDoesNotOpenInstalledTarget(t *testing.T){
    ctx:=context.Background();permit:=&dsRunnerWorkerPermit{ctx:ctx};permit.self=permit;candidate:=dsRunnerFixtureCandidate([]byte("new candidate"));calls:=0
    r:=executeDSRunnerMaintenanceFor(ctx,"capture",permit,dsRunnerMaintenanceAccess{acquire:func(context.Context)(*dsRunnerCandidate,error){calls++;return candidate,nil}},"",0)
    if !r.Qualified||r.CandidateSHA!=digest([]byte("new candidate"))||r.Current!=nil||r.Prepared||r.CommitAttempted||r.UpdatePerformed||r.InstalledVerified||r.DataAccessGranted||r.SourceAccessGranted||r.SendAllowed||calls!=1{t.Fatal("artifact verification depended on installed target or issued another result")}
    candidate.mu.Lock();closed,n:=candidate.closed,len(candidate.raw);candidate.mu.Unlock();if !closed||n!=0{t.Fatal("capture-only candidate not closed")}
}

func TestDSRunnerCommitOutcomeSeparatesNotAttemptedFromUnconfirmed(t *testing.T){
    plain:=errors.New("synthetic native failure")
    for _,test:=range []struct{name string;committed bool;err error;want string}{
        {"success",true,nil,dsRunnerCommitConfirmed},
        {"committed-with-cleanup-error",true,errors.Join(errDSRunnerCleanup,plain),dsRunnerCommitConfirmed},
        {"stopped-before-the-native-call",false,errors.Join(errDSRunnerPublishNotAttempted,errDSRunnerEvidence),dsRunnerCommitNotAttempted},
        {"native-call-failed",false,plain,dsRunnerCommitUnconfirmed},
        {"failure-without-error",false,nil,dsRunnerCommitUnconfirmed},
    }{
        if got:=dsRunnerCommitOutcomeFor(test.committed,test.err);got!=test.want{t.Fatalf("%s: outcome %s, want %s",test.name,got,test.want)}
    }
}
func TestDSRunnerMaintenancePublishStoppedBeforeNativeCallIsNotAttempted(t *testing.T){
    s:=dsRunnerSyntheticInitial();s.failBeforePublish=true
    r:=dsRunnerSyntheticExecute(context.Background(),"apply",s)
    if r.Qualified||r.CommitAttempted||r.UpdatePerformed||r.CommitOutcome!=dsRunnerCommitNotAttempted||r.Reason!="partial_prepare_preserved"||s.publishes!=1||len(r.Created)!=2||string(s.raw[dsRunnerSaved])!="old installed"{t.Fatal("a publish that stopped before the native call was not recorded as not attempted")}
}
func TestDSRunnerMaintenanceKeepsCleanupFactWithEveryEarlyFailure(t *testing.T){
    cleanup:=errors.Join(errDSRunnerCleanup,errors.New("synthetic close"))
    guard:=errors.Join(errGHAGuardUnqualified,errGHAGuardResponseCleanup)
    plain:=errors.New("synthetic failure")
    for _,test:=range []struct{name string;acquire,open error;cleanup bool}{
        {"acquire-cleanup",cleanup,nil,true},
        {"acquire-guard-response-cleanup",guard,nil,true},
        {"acquire-plain",plain,nil,false},
        {"open-cleanup",nil,cleanup,true},
        {"open-plain",nil,plain,false},
    }{t.Run(test.name,func(t *testing.T){
        s:=dsRunnerSyntheticInitial();access:=s.access()
        if test.acquire!=nil{access.acquire=func(context.Context)(*dsRunnerCandidate,error){return nil,test.acquire}}
        if test.open!=nil{access.open=func(context.Context,dsRunnerRepairPurpose)(dsRunnerTargetSession,error){return nil,test.open}}
        r:=dsRunnerSyntheticRun(context.Background(),"apply",access)
        if r.Qualified||r.InstalledVerified||r.CleanupFailed!=test.cleanup||r.CommitAttempted||r.CommitOutcome!=dsRunnerCommitNotAttempted||r.Reason!="qualification_unavailable"||s.publishes!=0{t.Fatal("primary failure or cleanup fact was lost before the target was used")}
    })}
}

func dsRunnerFailureFixture()dsRunnerMaintenanceResult{
    return dsRunnerMaintenanceResult{Domain:dsRunnerMaintenanceDomain,Operation:"apply",CleanupFailed:true,CommitAttempted:true,CommitOutcome:dsRunnerCommitConfirmed,UpdatePerformed:true,Reason:"commit_outcome_requires_reconciliation",Created:[]string{dsRunnerBackup,dsRunnerStage}}
}
func dsRunnerObservedChild(t *testing.T,r dsRunnerMaintenanceResult,exit int)askChildResult{
    t.Helper()
    raw,err:=json.Marshal(r);if err!=nil{t.Fatal(err)}
    return askChildResult{Raw:append(raw,'\n'),Exit:exit,Started:true,Assigned:true,WaitVerified:true,Reaped:true}
}
func TestDSRunnerObserveWorkerForwardsFailureFactsAndKeepsSuccessSeparate(t *testing.T){
    r,observed:=dsRunnerObserveWorker("apply",dsRunnerObservedChild(t,dsRunnerFailureFixture(),1))
    if !observed||!r.Reaped||r.Qualified||r.InstalledVerified||!r.UpdatePerformed||!r.CleanupFailed||!r.CommitAttempted||r.CommitOutcome!=dsRunnerCommitConfirmed||r.Reason!="commit_outcome_requires_reconciliation"||len(r.Created)!=2{t.Fatal("failure facts of an exit-one worker were not preserved")}
    unconfirmed:=dsRunnerFailureFixture();unconfirmed.CleanupFailed=false;unconfirmed.UpdatePerformed=false;unconfirmed.CommitOutcome=dsRunnerCommitUnconfirmed
    if r,observed=dsRunnerObserveWorker("apply",dsRunnerObservedChild(t,unconfirmed,1));!observed||r.UpdatePerformed||!r.CommitAttempted||r.CommitOutcome!=dsRunnerCommitUnconfirmed{t.Fatal("an unconfirmed replacement was reported as unchanged or performed")}
    good:=dsRunnerMaintenanceResult{Domain:dsRunnerMaintenanceDomain,Operation:"verify",Qualified:true,Reason:"verified_no_write",CommitOutcome:dsRunnerCommitNotAttempted}
    if r,observed=dsRunnerObserveWorker("verify",dsRunnerObservedChild(t,good,0));!observed||!r.Qualified||!r.Reaped{t.Fatal("a successful worker record was not observed")}
}
func TestDSRunnerObserveWorkerRejectsUntrustedObservations(t *testing.T){
    for _,test:=range []struct{name string;result func(*dsRunnerMaintenanceResult);child func(*askChildResult)}{
        {"not-started",nil,func(c *askChildResult){c.Started=false}},
        {"not-assigned",nil,func(c *askChildResult){c.Assigned=false}},
        {"wait-unverified",nil,func(c *askChildResult){c.WaitVerified=false}},
        {"not-reaped",nil,func(c *askChildResult){c.Reaped=false}},
        {"timed-out",nil,func(c *askChildResult){c.TimedOut=true}},
        {"exit-two",nil,func(c *askChildResult){c.Exit=2}},
        {"exit-zero-with-failure-record",nil,func(c *askChildResult){c.Exit=0}},
        {"no-output",nil,func(c *askChildResult){c.Raw=nil}},
        {"trailing-record",nil,func(c *askChildResult){c.Raw=append(c.Raw,[]byte("{}")...)}},
        {"wrong-domain",func(r *dsRunnerMaintenanceResult){r.Domain="other"},nil},
        {"wrong-operation",func(r *dsRunnerMaintenanceResult){r.Operation="verify"},nil},
        {"claimed-reap",func(r *dsRunnerMaintenanceResult){r.Reaped=true},nil},
        {"software-grant",func(r *dsRunnerMaintenanceResult){r.SoftwareReplaceAllowed=true},nil},
        {"data-grant",func(r *dsRunnerMaintenanceResult){r.DataAccessGranted=true},nil},
        {"source-grant",func(r *dsRunnerMaintenanceResult){r.SourceAccessGranted=true},nil},
        {"send-grant",func(r *dsRunnerMaintenanceResult){r.SendAllowed=true},nil},
        {"lease-claim",func(r *dsRunnerMaintenanceResult){r.NamespaceLeaseProved=true},nil},
        {"free-text-reason",func(r *dsRunnerMaintenanceResult){r.Reason="free text"},nil},
        {"worker-reports-unknown",func(r *dsRunnerMaintenanceResult){r.CommitOutcome=dsRunnerCommitUnknown},nil},
        {"unnamed-outcome",func(r *dsRunnerMaintenanceResult){r.CommitOutcome="maybe"},nil},
        {"performed-without-confirmation",func(r *dsRunnerMaintenanceResult){r.CommitOutcome=dsRunnerCommitUnconfirmed},nil},
        {"confirmed-without-attempt",func(r *dsRunnerMaintenanceResult){r.CommitAttempted=false},nil},
        {"foreign-created-name",func(r *dsRunnerMaintenanceResult){r.Created=[]string{"C:/other.exe"}},nil},
        {"repeated-created-name",func(r *dsRunnerMaintenanceResult){r.Created=[]string{dsRunnerStage,dsRunnerStage}},nil},
        {"malformed-acl-digest",func(r *dsRunnerMaintenanceResult){r.ACLSHA="not-hex"},nil},
        {"malformed-identity",func(r *dsRunnerMaintenanceResult){r.Current=&softwareIdentity{Volume:"x",FileID:"1",Created:"1"}},nil},
    }{t.Run(test.name,func(t *testing.T){
        r:=dsRunnerFailureFixture();if test.result!=nil{test.result(&r)}
        child:=dsRunnerObservedChild(t,r,1);if test.child!=nil{test.child(&child)}
        if _,observed:=dsRunnerObserveWorker("apply",child);observed{t.Fatal("an untrusted worker observation was accepted")}
    })}
}

// Helper process for the owned-child tests below. It is entered only through the
// real owned child with the inherited permit pipe; a plain test run returns at once.
func TestDSRunnerWorkerChild(t *testing.T){
    mode:=os.Getenv("DS_RUNNER_WORKER_CHILD")
    if mode==""{return}
    if !askWorkerParent(){os.Exit(6)}
    op:="apply";s:=dsRunnerSyntheticInitial();access:=s.access();ctx,cancel:=context.WithCancel(context.Background());defer cancel()
    switch mode{
    case "exit-two":os.Exit(2)
    case "garbage":_,_ = os.Stdout.Write([]byte("not a result\n"));os.Exit(1)
    case "hang":time.Sleep(time.Minute);os.Exit(0)
    case "success":op="verify"
    case "publish-failure","stderr":s.failPublish=true
    case "after-commit-read":s.failAfterCommit=true
    case "close-failure":s.failClose=true
    case "publish-close-failure":s.failPublishClose=true
    case "cancel-after-commit":s.cancelAfterCommit=cancel
    case "acquire-cleanup":access.acquire=func(context.Context)(*dsRunnerCandidate,error){return nil,errors.Join(errGHAGuardUnqualified,errGHAGuardResponseCleanup)}
    case "open-nil-cleanup":access.open=func(context.Context,dsRunnerRepairPurpose)(dsRunnerTargetSession,error){return nil,errors.Join(errDSRunnerCleanup,errors.New("synthetic constructor close"))}
    default:os.Exit(7)
    }
    r:=dsRunnerSyntheticRun(ctx,op,access)
    wrote:=writeDSRunnerMaintenanceResult(os.Stdout,r)
    if mode=="stderr"{_,_ = os.Stderr.Write([]byte("unexpected diagnostic\n"))}
    os.Exit(dsRunnerWorkerExit(op,r,wrote))
}
func dsRunnerOwnedWorkerLauncher(limit time.Duration)func(context.Context,string,...string)askChildResult{
    return func(ctx context.Context,_ string,_ ...string)askChildResult{
        bounded,cancel:=context.WithTimeout(ctx,limit);defer cancel()
        return askOwnedChild(bounded,os.Args[0],"-test.run=^TestDSRunnerWorkerChild$")
    }
}
func TestDSRunnerOuterReportsOwnedWorkerFactsThroughRealChildren(t *testing.T){
    for _,test:=range []struct{mode,arg,reason,commit string;code int;performed,cleanup,qualified bool;created int}{
        {"success","--verify-ds-runner-update","verified_no_write",dsRunnerCommitNotAttempted,0,false,false,true,0},
        {"publish-failure","--apply-ds-runner-update","commit_outcome_requires_reconciliation",dsRunnerCommitUnconfirmed,1,false,false,false,2},
        {"after-commit-read","--apply-ds-runner-update","commit_outcome_requires_reconciliation",dsRunnerCommitConfirmed,1,true,false,false,2},
        {"close-failure","--apply-ds-runner-update","commit_outcome_requires_reconciliation",dsRunnerCommitConfirmed,1,true,true,false,2},
        {"publish-close-failure","--apply-ds-runner-update","commit_outcome_requires_reconciliation",dsRunnerCommitConfirmed,1,true,true,false,2},
        {"cancel-after-commit","--apply-ds-runner-update","commit_outcome_requires_reconciliation",dsRunnerCommitConfirmed,1,true,false,false,2},
        {"acquire-cleanup","--apply-ds-runner-update","qualification_unavailable",dsRunnerCommitNotAttempted,1,false,true,false,0},
        {"open-nil-cleanup","--apply-ds-runner-update","qualification_unavailable",dsRunnerCommitNotAttempted,1,false,true,false,0},
    }{t.Run(test.mode,func(t *testing.T){
        t.Setenv("DS_RUNNER_WORKER_CHILD",test.mode)
        var out bytes.Buffer
        code:=runDSRunnerMaintenanceWith([]string{test.arg},&out,dsRunnerOwnedWorkerLauncher(20*time.Second))
        var r dsRunnerMaintenanceResult
        if code!=test.code||json.Unmarshal(out.Bytes(),&r)!=nil{t.Fatal("outer exit code or result record is wrong")}
        if !r.Reaped||r.Reason!=test.reason||r.CommitOutcome!=test.commit||r.UpdatePerformed!=test.performed||r.CleanupFailed!=test.cleanup||r.Qualified!=test.qualified||len(r.Created)!=test.created||r.InstalledVerified{t.Fatal("outer replaced or lost the worker's facts")}
    })}
}
func TestDSRunnerOuterReportsUnknownForUntrustedRealChildren(t *testing.T){
    for _,mode:=range []string{"exit-two","garbage","stderr","hang"}{t.Run(mode,func(t *testing.T){
        t.Setenv("DS_RUNNER_WORKER_CHILD",mode)
        limit:=20*time.Second;if mode=="hang"{limit=2*time.Second}
        var out bytes.Buffer
        code:=runDSRunnerMaintenanceWith([]string{"--apply-ds-runner-update"},&out,dsRunnerOwnedWorkerLauncher(limit))
        var r dsRunnerMaintenanceResult
        if code!=1||json.Unmarshal(out.Bytes(),&r)!=nil||r.Reason!="worker_outcome_unknown"||r.CommitOutcome!=dsRunnerCommitUnknown||!r.CommitAttempted||r.UpdatePerformed||r.Qualified||r.InstalledVerified||len(r.Created)!=0{t.Fatal("untrusted worker output was not reported as unknown")}
    })}
}
