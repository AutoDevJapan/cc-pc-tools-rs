//go:build windows

package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func watchdogTestID() watchdogFileIdentity {
	return watchdogFileIdentity{
		Volume: 1, File: 2, Created: 3, Bytes: 256, Links: 1,
		SHA256: strings.Repeat("a",64), SecuritySHA256: strings.Repeat("b",64),
	}
}

func watchdogTestOrigin() watchdogOrigin {
	o := watchdogOrigin{
		Target: watchdogTarget, Installed: watchdogTestID(),
		InstallReceiptSHA256: strings.Repeat("c",64),
		BuildReceiptSHA256: strings.Repeat("d",64), SourceCommit: strings.Repeat("e",40),
	}
	o.Installed.File = 1
	return o
}

func watchdogTestTask() watchdogTaskView {
	return watchdogTaskView{
		Name: watchdogTask, XMLSHA256: strings.Repeat("f",64), S4U: true, Limited: true,
		Executable: watchdogLauncher, Arguments: "--launch-service opencodex_watchdog",
		WorkingDirectory: "D:/CC/Tools/resident-guard",
		ResidentGuardDisabled: true, CreditWatchdogDisabled: true,
	}
}

func watchdogTestObservation(id watchdogFileIdentity) watchdogObservation {
	start := time.Date(2026,1,1,0,0,0,0,time.UTC)
	v := watchdogObservation{
		OwnInstanceVerified: true, OwnImage: id, PID: 12, PreviousPID: 11,
		Started: start, Published: start.Add(-time.Second),
	}
	for i:=0;i<6;i++ {
		v.Samples = append(v.Samples, watchdogSample{
			At: start.Add(time.Duration(i)*10*time.Second),
			WatchdogPID:12, Healthy:true, ObservationKind:"health_only",
		})
	}
	return v
}

type watchdogTestFiles struct {
	state watchdogSteps
	calls []string
	fail string
	closeCount int
}
func (f *watchdogTestFiles) Check(context.Context) error {
	f.calls=append(f.calls,"check")
	if f.fail=="check" { return errors.New("check") }
	return nil
}
func (f *watchdogTestFiles) Stage(context.Context) error {
	f.calls=append(f.calls,"stage"); f.state.StageCreated=true
	if f.fail=="stage" { return errors.New("stage") }
	return nil
}
func (f *watchdogTestFiles) Publish(context.Context) error {
	f.calls=append(f.calls,"publish")
	if f.fail=="retire-unknown" {f.state.NamespaceUnknown=true;return errors.New("unconfirmed retire")}
	f.state.OldRetired=true
	if f.fail=="before-publish" { return errors.New("before") }
	f.state.Published=true
	if f.fail=="postcheck" { return errors.New("postcheck") }
	f.state.Verified=true
	return nil
}
func (f *watchdogTestFiles) Rollback(ctx context.Context) error {
	f.calls=append(f.calls,"rollback")
	if ctx.Err()!=nil { return ctx.Err() }
	if f.fail=="rollback" { return errors.New("rollback") }
	f.state.RestoreAttempted=true; f.state.Restored=true
	f.state.CandidateRetired=f.state.Published
	return nil
}
func (f *watchdogTestFiles) Candidate() watchdogFileIdentity { return watchdogTestID() }
func (f *watchdogTestFiles) Steps() watchdogSteps { return f.state }
func (f *watchdogTestFiles) Close() error {
	f.closeCount++
	if f.fail=="close" { return errors.New("files close") }
	return nil
}

type watchdogTestResident struct {
	calls []string
	task watchdogTaskView
	observation watchdogObservation
	fail string
	taskCalls int
	closeCount int
	cancel context.CancelFunc
}
func (r *watchdogTestResident) Task(context.Context) (watchdogTaskView,error) {
	r.taskCalls++
	if r.fail=="task-change" && r.taskCalls>1 {
		changed:=r.task;changed.XMLSHA256=strings.Repeat("1",64);return changed,nil
	}
	return r.task,nil
}
func (r *watchdogTestResident) StopAndHold(context.Context) (watchdogStopped,error) {
	r.calls=append(r.calls,"stop")
	if r.fail=="stop" { return watchdogStopped{},errors.New("stop") }
	return watchdogStopped{true,true,true},nil
}
func (r *watchdogTestResident) ReleaseHold(context.Context) error {
	r.calls=append(r.calls,"release")
	if r.fail=="release" { return errors.New("release") }
	return nil
}
func (r *watchdogTestResident) Observe(context.Context,watchdogFileIdentity) (watchdogObservation,error) {
	r.calls=append(r.calls,"observe")
	if r.cancel!=nil { r.cancel() }
	if r.fail=="observe" || r.fail=="rollback-stop" {
		return watchdogObservation{},errors.New("observe")
	}
	return r.observation,nil
}
func (r *watchdogTestResident) StopPublishedAndHold(ctx context.Context,_ watchdogFileIdentity) (watchdogStopped,error) {
	r.calls=append(r.calls,"stop-published")
	if ctx.Err()!=nil { return watchdogStopped{},ctx.Err() }
	if r.fail=="rollback-stop" { return watchdogStopped{},errors.New("stop") }
	return watchdogStopped{true,true,true},nil
}
func (r *watchdogTestResident) Close() error {
	r.closeCount++
	if r.fail=="close" { return errors.New("resident close") }
	return nil
}
func watchdogTestResources() (*watchdogUpdateResources,*watchdogTestFiles,*watchdogTestResident) {
	f:=&watchdogTestFiles{}
	r:=&watchdogTestResident{task:watchdogTestTask(),observation:watchdogTestObservation(watchdogTestID())}
	all:=&watchdogUpdateResources{
		Origin:watchdogTestOrigin(),Artifact:watchdogTestID(),Files:f,Resident:r,
		Recheck:func(ctx context.Context) error { return ctx.Err() },
	}
	return all,f,r
}

func TestWatchdogNormalUnavailableNeverOpensInstalled(t *testing.T) {
	for _,apply:=range []bool{false,true} {
		v:=runWatchdogUpdate(context.Background(),apply,openNormalWatchdogUpdate)
		if v.Qualified || v.Updated || v.Reason!="watchdog_distribution_issuer_unavailable" {
			t.Fatalf("normal unavailable changed: %+v",v)
		}
	}
}

func TestWatchdogControllerAcceptsOwnedObservationAndCheckDoesNoMutation(t *testing.T) {
	for _,apply:=range []bool{false,true} {
		all,f,r:=watchdogTestResources()
		v:=runWatchdogUpdate(context.Background(),apply,func(context.Context)(*watchdogUpdateResources,error){return all,nil})
		if !v.Qualified || !v.TaskUnchanged || v.Updated!=apply || v.ObservationAccepted!=apply ||
			f.closeCount!=1 || r.closeCount!=1 || v.DataAccessGranted || v.GeneralGuardChanged {
			t.Fatalf("result: %+v files=%+v resident=%+v",v,f,r)
		}
		if !apply && (len(r.calls)!=0 || strings.Join(f.calls,",")!="check") {
			t.Fatal("check mutated")
		}
	}
}

func TestWatchdogUnknownOriginAndTaskRefuseBeforeStop(t *testing.T) {
	for _,kind:=range []string{"origin","artifact","recheck","task-s4u","task-action","task-xml","task-disabled"} {
		t.Run(kind,func(t *testing.T){
			all,_,r:=watchdogTestResources()
			switch kind {
			case "origin":all.Origin.InstallReceiptSHA256=""
			case "artifact":all.Artifact.Links=2
			case "recheck":all.Recheck=func(context.Context)error{return errors.New("stale")}
			case "task-s4u":r.task.S4U=false
			case "task-action":r.task.Arguments="install"
			case "task-xml":r.task.XMLSHA256=""
			case "task-disabled":r.task.ResidentGuardDisabled=false
			}
			v:=runWatchdogUpdate(context.Background(),true,func(context.Context)(*watchdogUpdateResources,error){return all,nil})
			if v.Qualified || v.Updated || len(r.calls)!=0 {t.Fatalf("unknown reached stop: %+v",v)}
		})
	}
}

func TestWatchdogPostpublishFailureKeepsCommitAndRollbackFacts(t *testing.T) {
	for _,kind:=range []string{"before-publish","postcheck","observe","release","task-change","rollback","rollback-stop"} {
		t.Run(kind,func(t *testing.T){
			all,f,r:=watchdogTestResources()
			switch kind {
			case "before-publish","postcheck":f.fail=kind
			case "rollback":f.fail=kind;r.fail="observe"
			default:r.fail=kind
			}
			v:=runWatchdogUpdate(context.Background(),true,func(context.Context)(*watchdogUpdateResources,error){return all,nil})
			wantRestore:=kind!="rollback"&&kind!="rollback-stop"&&kind!="task-change"
			wantReconcile:=!wantRestore||kind=="release"
			if v.Qualified || !v.RollbackAttempted || v.RollbackSucceeded!=wantRestore ||
				v.ReconciliationRequired!=wantReconcile || v.Updated!=(kind!="before-publish") {
				t.Fatalf("facts lost: %+v",v)
			}
			if kind=="postcheck" && strings.Contains(strings.Join(r.calls,","),"stop-published") {
				t.Fatal("published but never released hold: unnecessary stop")
			}
		})
	}
}

func TestWatchdogCancelledObservationStillUsesFiniteRecovery(t *testing.T) {
	ctx,cancel:=context.WithCancel(context.Background());defer cancel()
	all,f,r:=watchdogTestResources();r.cancel=cancel;r.fail="observe"
	// The issuer's generation is independent of caller cancellation.
	all.Recheck=func(context.Context)error{return nil}
	v:=runWatchdogUpdate(ctx,true,func(context.Context)(*watchdogUpdateResources,error){return all,nil})
	if v.Qualified || !v.RollbackSucceeded || !strings.Contains(strings.Join(f.calls,","),"rollback") {
		t.Fatalf("cancel suppressed recovery: %+v",v)
	}
}

func TestWatchdogPartialOpenClosesEveryResourceAndPreservesPrimary(t *testing.T) {
	all,f,r:=watchdogTestResources();f.fail="close";r.fail="close"
	v:=runWatchdogUpdate(context.Background(),true,func(context.Context)(*watchdogUpdateResources,error){return all,errors.New("primary")})
	if v.Qualified || !v.CleanupFailed || v.Reason!="watchdog_distribution_issuer_unavailable" ||
		f.closeCount!=1 || r.closeCount!=1 || v.retained!=all {
		t.Fatalf("partial cleanup: %+v",v)
	}
	_ = all.close()
	if f.closeCount!=1 || r.closeCount!=1 {t.Fatal("failed close retried")}
}

func TestWatchdogUnconfirmedStopNeverReachesStage(t *testing.T) {
	all,f,r:=watchdogTestResources();r.fail="stop"
	v:=runWatchdogUpdate(context.Background(),true,func(context.Context)(*watchdogUpdateResources,error){return all,nil})
	if v.Qualified||v.Updated||!v.ReconciliationRequired||strings.Join(f.calls,",")!="check"||
		f.closeCount!=1||r.closeCount!=1 {
		t.Fatalf("unknown stop reached namespace: %+v %+v",v,f)
	}
}

func TestWatchdogUnconfirmedRetireIsNotNoChangeOrGuessingRollback(t *testing.T) {
	all,f,_:=watchdogTestResources();f.fail="retire-unknown"
	v:=runWatchdogUpdate(context.Background(),true,func(context.Context)(*watchdogUpdateResources,error){return all,nil})
	if v.Qualified||v.Updated||!v.ReconciliationRequired||v.RollbackAttempted||
		strings.Contains(strings.Join(f.calls,","),"rollback")||!v.Steps.NamespaceUnknown {
		t.Fatalf("unknown outcome erased: %+v",v)
	}
}

func TestWatchdogHealthOnlyCannotSupplyOwnershipOrTermination(t *testing.T) {
	for _,kind:=range []string{"normal","proxy-identity","metrics","eligible","failures","wrong-pid","same-pid","old-pid-unknown","stale","too-short","too-late","write-fail","event","image","own-proof","early-start"} {
		t.Run(kind,func(t *testing.T){
			id:=watchdogTestID();v:=watchdogTestObservation(id)
			switch kind {
			case "proxy-identity":v.Samples[0].IdentityPresent=true
			case "metrics":v.Samples[0].MetricsPresent=true
			case "eligible":v.Samples[0].Eligible=true
			case "failures":v.Samples[0].Failures=1
			case "wrong-pid":v.Samples[0].WatchdogPID++
			case "same-pid":v.PreviousPID=v.PID
			case "old-pid-unknown":v.PreviousPID=0
			case "stale":v.Samples[0].At=v.Started.Add(-time.Second)
			case "too-short":v.Samples=v.Samples[:5]
			case "too-late":for i:=range v.Samples{v.Samples[i].At=v.Samples[i].At.Add(151*time.Second)}
			case "write-fail":v.Samples[0].WriteFailures=1
			case "event":v.TerminationOrServiceEvent=true
			case "image":v.OwnImage.File++
			case "own-proof":v.OwnInstanceVerified=false
			case "early-start":v.Started=v.Published.Add(-time.Second)
			}
			if v.valid(id)!=(kind=="normal") {t.Fatal("observation boundary")}
		})
	}
}

func TestWatchdogCancelledEntryNeverCallsOpener(t *testing.T) {
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 calls:=0
 v:=runWatchdogUpdate(ctx,true,func(context.Context)(*watchdogUpdateResources,error){
  calls++;return nil,errors.New("must not open")
 })
 if calls!=0||v.Qualified||v.Updated||v.Reason!="watchdog_context_invalid" {
  t.Fatalf("cancelled entry opened: calls=%d result=%+v",calls,v)
 }
}
