//go:build windows

package main

import (
	"context"
	"errors"
	"encoding/json"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"
	"runtime"
	"os"
)

func watchdogTaskFixtureJSON()map[string]any{
	definition:=watchdogTaskFixtureXML()
	digest:=sha256.Sum256([]byte(definition))
	return map[string]any{
		"name":watchdogTask,"xml_sha256":hex.EncodeToString(digest[:]),"definition_xml":definition,"principal":"S-1-5-21-1",
		"s4u":true,"limited":true,"executable":watchdogLauncher,
		"arguments":"--launch-service opencodex_watchdog","working":"D:/CC/Tools/resident-guard",
		"resident_disabled":true,"credit_disabled":true,
		"instances":[]any{map[string]any{"guid":"{4AB9BD54-AC1C-494C-9B08-9EE212A74F3E}","pid":5312,"path":"\\CC-OcxHealthWatchdog","action":watchdogLauncher,"state":4}},
	}
}
func TestWatchdogFixedTaskDecoderRejectsChangedDefinitionsAndInstances(t *testing.T){
	for _,kind:=range []string{"normal","s4u","action","principal","xml","disabled","instance-pid","instance-guid","instance-path"}{
		t.Run(kind,func(t *testing.T){
			v:=watchdogTaskFixtureJSON()
			switch kind{
			case "s4u":v["s4u"]=false
			case "action":v["arguments"]="install"
			case "principal":v["principal"]=""
			case "xml":v["xml_sha256"]=""
			case "disabled":v["resident_disabled"]=false
			default:
				if strings.HasPrefix(kind,"instance-"){
					entry:=v["instances"].([]any)[0].(map[string]any)
					if kind=="instance-pid"{entry["pid"]=0}
					if kind=="instance-guid"{entry["guid"]="foreign"}
					if kind=="instance-path"{entry["path"]="\\other"}
				}
			}
			raw,_:=json.Marshal(v)
			if _,e:=decodeWatchdogTask(raw);(e==nil)!=(kind=="normal"){t.Fatal("task decoder")}
		})
	}
}
func TestWatchdogInstanceGUIDIsStrictlyFiniteData(t *testing.T){
	for _,s:=range []string{"","'; Stop-Process","{4AB9BD54-AC1C-494C-9B08-9EE212A74F3Z}","{4AB9BD54AC1C494C9B089EE212A74F3E}"}{
		if watchdogInstanceGUID(s){t.Fatal("unbound selector")}
	}
	if !watchdogInstanceGUID("{4AB9BD54-AC1C-494C-9B08-9EE212A74F3E}"){t.Fatal("valid GUID")}
}


func watchdogTaskFixtureXML()string{
	return `<Task xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task"><Principals><Principal id="Author"><UserId>S-1-5-21-1</UserId><LogonType>S4U</LogonType><RunLevel>LeastPrivilege</RunLevel></Principal></Principals><Actions Context="Author"><Exec><Command>`+watchdogLauncher+`</Command><Arguments>--launch-service opencodex_watchdog</Arguments><WorkingDirectory>D:/CC/Tools/resident-guard</WorkingDirectory></Exec></Actions></Task>`
}
func TestWatchdogTaskDefinitionBindsExactActionAndPrincipalContext(t *testing.T){
	for _,kind:=range []string{"normal","utf16-declaration","extra-comhandler","extra-exec","missing-action","wrong-context","missing-context","extra-principal","missing-id","foreign-namespace","digest","projection"}{
		t.Run(kind,func(t *testing.T){
			v:=watchdogTaskFixtureJSON();x:=v["definition_xml"].(string)
			switch kind{
			case "utf16-declaration":x=`<?xml version="1.0" encoding="UTF-16"?>`+x
			case "extra-comhandler":x=strings.Replace(x,"</Actions>","<ComHandler><ClassId>foreign</ClassId></ComHandler></Actions>",1)
			case "extra-exec":x=strings.Replace(x,"</Actions>","<Exec><Command>other</Command></Exec></Actions>",1)
			case "missing-action":i:=strings.Index(x,"<Exec>");j:=strings.Index(x,"</Exec>")+len("</Exec>");x=x[:i]+x[j:]
			case "wrong-context":x=strings.Replace(x,`Context="Author"`,`Context="Other"`,1)
			case "missing-context":x=strings.Replace(x,` Context="Author"`,"",1)
			case "extra-principal":x=strings.Replace(x,"</Principals>",`<Principal id="Other"><UserId>S-1-5-21-2</UserId></Principal></Principals>`,1)
			case "missing-id":x=strings.Replace(x,` id="Author"`,"",1)
			case "foreign-namespace":x=strings.Replace(x,"<Exec>",`<Exec xmlns="urn:foreign">`,1)
			case "projection":v["principal"]="foreign"
			}
			digest:=sha256.Sum256([]byte(x));v["definition_xml"]=x;v["xml_sha256"]=hex.EncodeToString(digest[:])
			if kind=="digest"{v["xml_sha256"]=strings.Repeat("b",64)}
			raw,_:=json.Marshal(v)
			if _,e:=decodeWatchdogTask(raw);(e==nil)!=(kind=="normal"||kind=="utf16-declaration"){t.Fatalf("action/context verification: %v",e)}
		})
	}
}
func TestWatchdogTaskStopRechecksSameXMLImmediatelyBeforeStop(t *testing.T){
	i:=strings.Index(watchdogTaskScript,"if([string]$t.Xml -cne $xml)")
	stop:=strings.Index(watchdogTaskScript,"$found[0].Stop()")
	if i<0||stop<i||strings.Contains(watchdogTaskScript[i:stop],"GetTask("){t.Fatal("missing same-definition stop recheck")}
}

// The observations below exercise the same closed topology builder and
// pre-stop snapshot predicate as the native adapter, without opening a process.
func watchdogChainFixture(count int)(watchdogTaskInstance,[]watchdogProcessEntry,map[uint32]*watchdogProcess){
	instance:=watchdogTaskInstance{PID:5312}
	rows:=make([]watchdogProcessEntry,0,count)
	processes:=map[uint32]*watchdogProcess{}
	for depth:=0;depth<count;depth++{
		pid:=uint32(5312+depth);parent:=uint32(4300);if depth>0{parent=pid-1}
		image:=watchdogLauncher;if depth==count-1{image=watchdogTarget}
		rows=append(rows,watchdogProcessEntry{PID:pid,Parent:parent})
		processes[pid]=&watchdogProcess{pid:pid,parent:parent,created:uint64(100+depth),image:image}
	}
	return instance,rows,processes
}
func TestWatchdogNormalDirectAndReducedChainsRemainClosed(t *testing.T){
	for _,count:=range []int{2,3}{
		for _,kind:=range []string{"normal","foreign-image","foreign-sid","parent","birth","zero-birth","not-live","extra-child","leaf-child","missing","partial-open"}{
			t.Run(string(rune('0'+count))+"/"+kind,func(t *testing.T){
				instance,rows,processes:=watchdogChainFixture(count)
				leaf:=uint32(5312+count-1)
				switch kind{
				case "foreign-image":processes[leaf].image="D:/foreign.exe"
				case "parent":processes[leaf].parent=42
				case "birth":processes[leaf].created=99
				case "zero-birth":processes[leaf].created=0
				case "extra-child":rows=append(rows,watchdogProcessEntry{PID:7000,Parent:instance.PID})
				case "leaf-child":rows=append(rows,watchdogProcessEntry{PID:7000,Parent:leaf})
				case "missing":rows=rows[:len(rows)-1]
				}
				chain,e:=watchdogChainFromSnapshot(instance,"same-sid",rows,func(pid,parent uint32)(*watchdogProcess,string,bool,error){
					p:=processes[pid];sid:="same-sid";live:=true
					if pid==leaf&&kind=="foreign-sid"{sid="foreign"}
					if pid==leaf&&kind=="not-live"{live=false}
					if pid==leaf&&kind=="partial-open"{return p,sid,live,errors.New("partial")}
					return p,sid,live,nil
				})
				good:=kind=="normal"
				if (e==nil)!=good{t.Fatalf("normal topology/refusal: count=%d kind=%s chain=%d err=%v",count,kind,len(chain),e)}
				if good&&(!watchdogChainShape(chain)||len(chain)!=count||watchdogChainRows(chain,rows)!=nil){t.Fatal("pre-stop shape disagrees")}
				if kind=="partial-open"&&(len(chain)==0||chain[len(chain)-1]!=processes[leaf]){t.Fatal("partial held process reference lost")}
			})
		}
	}
	for _,count:=range []int{1,4}{
		instance,rows,processes:=watchdogChainFixture(count)
		if _,e:=watchdogChainFromSnapshot(instance,"same-sid",rows,func(pid,parent uint32)(*watchdogProcess,string,bool,error){return processes[pid],"same-sid",true,nil});e==nil{t.Fatalf("unsupported topology accepted: %d",count)}
	}
}
func TestWatchdogBothNormalChainsRecheckRowsBeforeStop(t *testing.T){
	for _,count:=range []int{2,3}{
		for _,kind:=range []string{"normal","changed-parent","extra-descendant","extra-sibling","wrong-shape"}{
			t.Run(string(rune('0'+count))+"/"+kind,func(t *testing.T){
				instance,rows,processes:=watchdogChainFixture(count)
				chain,e:=watchdogChainFromSnapshot(instance,"same-sid",rows,func(pid,parent uint32)(*watchdogProcess,string,bool,error){return processes[pid],"same-sid",true,nil})
				if e!=nil{t.Fatal(e)}
				switch kind{
				case "changed-parent":rows[len(rows)-1].Parent=42
				case "extra-descendant":rows=append(rows,watchdogProcessEntry{PID:7000,Parent:chain[len(chain)-1].pid})
				case "extra-sibling":rows=append(rows,watchdogProcessEntry{PID:7000,Parent:chain[0].pid})
				case "wrong-shape":chain[len(chain)-1].image=watchdogLauncher
				}
				if (watchdogChainRows(chain,rows)==nil)!=(kind=="normal"){t.Fatal("changed rows/shape reached Stop")}
			})
		}
	}
}

type watchdogCheckFinalTaskFixture struct{
	*watchdogTestResident
	cancelAt int
	cancel context.CancelFunc
}
func(f *watchdogCheckFinalTaskFixture) Task(ctx context.Context)(watchdogTaskView,error){
	view,e:=f.watchdogTestResident.Task(ctx)
	if f.taskCalls==f.cancelAt{f.cancel()}
	return view,e
}
func TestWatchdogCheckOnlyFinalTaskCancellationCannotQualify(t *testing.T){
	for _,kind:=range []string{"normal","first-task-cancel","final-task-cancel","cancel-and-close-failure"}{
		t.Run(kind,func(t *testing.T){
			ctx,cancel:=context.WithCancel(context.Background());defer cancel()
			all,files,resident:=watchdogTestResources();at:=0
			if kind=="first-task-cancel"{at=1}else if kind!="normal"{at=2}
			if kind=="cancel-and-close-failure"{resident.fail="close"}
			all.Resident=&watchdogCheckFinalTaskFixture{watchdogTestResident:resident,cancelAt:at,cancel:cancel}
			result:=runWatchdogUpdate(ctx,false,func(context.Context)(*watchdogUpdateResources,error){return all,nil})
			good:=kind=="normal"
			if result.Qualified!=good||result.Updated||result.ObservationAccepted||result.DataAccessGranted||result.GeneralGuardChanged||len(resident.calls)!=0||strings.Join(files.calls,",")!="check"{
				t.Fatalf("check-only cancellation/mutation: %+v files=%v resident=%v",result,files.calls,resident.calls)
			}
			if !good&&(result.Reason!="watchdog_check_final_recheck_failed"||ctx.Err()!=context.Canceled||result.TaskUnchanged){t.Fatal("final cancellation became success",result)}
			if files.closeCount!=1||resident.closeCount!=1{t.Fatal("acquired resources not closed once")}
			if result.CleanupFailed!=(kind=="cancel-and-close-failure"){t.Fatal("primary/cleanup facts mixed",result)}
		})
	}
}

// The same Observe consumer runs with controlled Task/chain/status/event seams.
// Only GetCurrentThreadId is native metadata; no Task, process or service starts.
type watchdogObserveFixture struct{
	r *watchdogResidentNative
	now time.Time
	kind,mode string
	tasks,chains,samples,events,pauses,images int
	acquired []*watchdogProcess
	closed map[*watchdogProcess]int
	owner *watchdogLifetimeFixture
	cancel context.CancelFunc
	closeFail bool
}
func watchdogObserveFixtureNew(t *testing.T,kind,mode string)*watchdogObserveFixture{
	t.Helper();published:=time.Date(2026,1,1,0,0,0,0,time.UTC)
	var original watchdogTaskSnapshot
	raw,_:=json.Marshal(watchdogTaskFixtureJSON());var e error;original,e=decodeWatchdogTask(raw);if e!=nil{t.Fatal(e)}
	original.Instances[0].PID=1111
	id:=watchdogTestID();owner:=&watchdogLifetimeFixture{}
	r:=&watchdogResidentNative{task:original,previousPID:11,published:published,candidate:func()watchdogFileIdentity{return id},lifetime:owner,pins:newWatchdogPinScope()}
	r.collection.begin(true);r.collection.confirm()
	f:=&watchdogObserveFixture{r:r,now:published.Add(2*time.Second),kind:kind,mode:mode,closed:map[*watchdogProcess]int{},owner:owner}
	r.observation=&watchdogObservationOps{task:f.task,chain:f.chain,closeChain:f.closeChain,checkProcess:func(p *watchdogProcess)error{if p.closed{return errors.New("changed")};return nil},
		image:func(context.Context,watchdogFileIdentity)error{f.images++;if kind=="image"{return errors.New("image_changed")};return nil},sample:f.sample,events:f.eventRead,now:func()time.Time{return f.now},pause:f.pause}
	runtime.LockOSThread();r.threadPinned=true
	thread,_,_:=kernel.NewProc("GetCurrentThreadId").Call();r.threadID=uint32(thread)
	t.Cleanup(func(){_ = r.Close()})
	return f
}
func(f *watchdogObserveFixture) pending(n int)bool{return f.mode=="deadline"||n==1}
func(f *watchdogObserveFixture) task(context.Context)(watchdogTaskSnapshot,error){
	f.tasks++;v:=watchdogTaskFixtureJSON();i:=v["instances"].([]any)[0].(map[string]any)
	i["guid"]="{5AB9BD54-AC1C-494C-9B08-9EE212A74F3E}";i["pid"]=5312
	if f.kind=="queued"&&f.pending(f.tasks){i["state"]=2;i["pid"]=0;i["action"]=""}
	if f.kind=="pid-pending"&&f.pending(f.tasks){i["pid"]=0}
	if f.kind=="action-pending"&&f.pending(f.tasks){i["action"]=""}
	if (f.kind=="queued"||f.kind=="pid-pending"||f.kind=="action-pending")&&f.mode=="foreign"&&f.tasks>1{i["action"]="D:/foreign.exe"}
	if f.kind=="definition"{v["arguments"]="foreign"}
	if f.kind=="instance-pid"&&f.tasks>1{i["pid"]=5412}
	if f.kind=="instance-guid"&&f.tasks>1{i["guid"]="{6AB9BD54-AC1C-494C-9B08-9EE212A74F3E}"}
	if f.kind=="ready-to-queued"&&f.tasks>1{i["state"]=2}
	if f.kind=="unknown-task"{return watchdogTaskSnapshot{},errors.New("unknown_task_error")}
	raw,_:=json.Marshal(v);return decodeWatchdogObservationTask(raw)
}
func(f *watchdogObserveFixture) chain(i watchdogTaskInstance,principal string)([]*watchdogProcess,error){
	f.chains++;count:=2;if f.kind=="chain-reduced"{count=3};instance,rows,processes:=watchdogChainFixture(count);instance.GUID=i.GUID
	for depth:=0;depth<count;depth++{processes[uint32(5312+depth)].created=uint64(f.r.published.Unix()+11644473600)*10000000+10000000+uint64(depth)}
	if (f.kind=="chain"||f.kind=="chain-reduced"||f.kind=="prefix-change")&&f.pending(f.chains){rows=rows[:len(rows)-1]}
	if f.kind=="prefix-change"&&f.chains>1{processes[5312].created++}
	if (f.kind=="chain"||f.kind=="chain-reduced")&&f.mode=="foreign"&&f.chains>1{processes[5312].image="D:/foreign.exe"}
	if f.kind=="birth"{processes[5312].created-=20000000}
	return watchdogChainFromSnapshotMode(instance,principal,rows,func(pid,parent uint32)(*watchdogProcess,string,bool,error){p:=processes[pid];f.acquired=append(f.acquired,p);sid:=principal;if f.kind=="owner"{sid="foreign"};return p,sid,true,nil},true)
}
func(f *watchdogObserveFixture) closeChain(chain []*watchdogProcess)error{
	var result error
	for _,p:=range chain{if p!=nil{if !p.closed{p.closed=true;f.closed[p]++;if f.closeFail{p.closeErr=errors.New("fixture_close_failed")}};result=errors.Join(result,p.closeErr)}}
	return result
}
func(f *watchdogObserveFixture) sample(context.Context)(watchdogSample,error){
	f.samples++
	if f.kind=="unknown-sample"{return watchdogSample{},errors.New("unknown_sample_error")}
	if f.kind=="old-status"&&f.pending(f.samples){return watchdogSample{At:f.r.published.Add(-time.Second),WatchdogPID:f.r.previousPID},nil}
	if (f.kind=="mutable"||f.kind=="rename")&&f.pending(f.samples){return watchdogSample{},watchdogObservationPending("mutable_snapshot_raced")}
	if (f.kind=="mutable"||f.kind=="rename")&&f.mode=="foreign"&&f.samples>1{return watchdogSample{},errors.New("foreign_metadata")}
	at:=f.now
	if f.samples>1&&f.events==0{f.now=f.now.Add(9*time.Second);at=f.now}
	pid:=uint32(5313);if f.kind=="chain-reduced"{pid=5314};if f.kind=="old-status"&&f.mode=="foreign"&&f.samples>1||f.kind=="sample-pid"{pid=9999}
	s:=watchdogSample{At:at,WatchdogPID:pid,Healthy:true,ObservationKind:"health_only"}
	if f.kind=="health"{s.Healthy=false}
	return s,nil
}
func(f *watchdogObserveFixture) eventRead(context.Context,time.Time,uint32)error{
	f.events++
	if f.kind=="append"&&f.pending(f.events){return watchdogObservationPending("events_appending")}
	if f.kind=="append"&&f.mode=="foreign"&&f.events>1{return errors.New("foreign_event")}
	return nil
}
func(f *watchdogObserveFixture) pause(ctx context.Context,d time.Duration)error{
	f.pauses++;if f.cancel!=nil{f.cancel()}
	if e:=ctx.Err();e!=nil{return e}
	f.now=f.now.Add(d);return nil
}
func(f *watchdogObserveFixture) assertClosedOnce(t *testing.T){
	t.Helper();if e:=f.r.Close();e!=nil&&!f.closeFail{t.Fatal(e)};_ = f.r.Close()
	for _,p:=range f.acquired{if f.closed[p]!=1{t.Fatal("partial/selected process not closed once",p.pid,f.closed[p])}}
	if f.owner.closeCalls!=1||!f.r.collection.confirmed||!f.r.collection.required||f.r.collection.stopAttempted!=true{t.Fatal("original collection/owner was replaced")}
}
func TestWatchdogObserveWaitsOnlyForBoundedStartupReadiness(t *testing.T){
	for _,kind:=range []string{"chain","chain-reduced","queued","pid-pending","action-pending","old-status","mutable","rename","append"}{
		for _,mode:=range []string{"progress","deadline","foreign"}{
			t.Run(kind+"/"+mode,func(t *testing.T){
				f:=watchdogObserveFixtureNew(t,kind,mode);result,e:=f.r.Observe(context.Background(),watchdogTestID())
				if mode=="progress"{if e!=nil||!result.valid(watchdogTestID())||f.pauses==0{t.Fatal("transient startup could not converge",result,e)}}else{
					if e==nil{t.Fatal("unready/foreign became success")}
					if mode=="deadline"&&!errors.Is(e,context.DeadlineExceeded){t.Fatal("deadline not shared",e)}
					if mode=="foreign"&&(errors.Is(e,context.DeadlineExceeded)||watchdogObservationRetry(e)){t.Fatal("foreign error retried",e)}
				}
				if f.now.Sub(f.r.published)>152*time.Second{t.Fatal("retry extended the observation deadline")}
				f.assertClosedOnce(t)
			})
		}
	}
}
func TestWatchdogObserveForeignInvalidChangedAndCancelStayTerminal(t *testing.T){
	for _,kind:=range []string{"definition","instance-pid","instance-guid","ready-to-queued","prefix-change","unknown-task","unknown-sample","birth","owner","image","sample-pid","health","cancel","close-failed"}{
		t.Run(kind,func(t *testing.T){
			mode:="foreign";source:=kind;if kind=="cancel"||kind=="close-failed"{source="chain";mode="deadline"}
			f:=watchdogObserveFixtureNew(t,source,mode);ctx,cancel:=context.WithCancel(context.Background());defer cancel()
			if kind=="cancel"{f.cancel=cancel};if kind=="close-failed"{f.closeFail=true}
			_,e:=f.r.Observe(ctx,watchdogTestID());if e==nil||watchdogObservationRetry(e)||errors.Is(e,context.DeadlineExceeded){t.Fatal("invalid state became retry/success",e)}
			if kind=="cancel"&&!errors.Is(e,context.Canceled){t.Fatal("cancel lost",e)}
			if kind=="close-failed"&&f.pauses!=0{t.Fatal("cleanup failure was retried")}
			f.assertClosedOnce(t)
		})
	}
}

func TestWatchdogObservationDecoderDoesNotWeakenStopDecoder(t *testing.T){
	for _,kind:=range []string{"running","queued","pid-pending","action-pending","foreign-action","invalid-state","foreign-path","changed-definition","multiple"}{
		t.Run(kind,func(t *testing.T){
			v:=watchdogTaskFixtureJSON();i:=v["instances"].([]any)[0].(map[string]any)
			switch kind{case "queued":i["state"]=2;i["pid"]=0;i["action"]="";case "pid-pending":i["pid"]=0;case "action-pending":i["action"]="";case "foreign-action":i["action"]="foreign";case "invalid-state":i["state"]=0;case "foreign-path":i["path"]="foreign";case "changed-definition":v["arguments"]="foreign";case "multiple":v["instances"]=append(v["instances"].([]any),i)}
			raw,_:=json.Marshal(v);observed,e:=decodeWatchdogObservationTask(raw);good:=kind=="running"||kind=="queued"||kind=="pid-pending"||kind=="action-pending"
			if (e==nil)!=good{t.Fatal("observation decoder boundary",e)}
			_,strict:=decodeWatchdogTask(raw);if (strict==nil)!=(kind=="running"){t.Fatal("strict stop/check decoder changed",strict)}
			if good&&observed.Instances[0].PID!=uint32(i["pid"].(int)){t.Fatal("observation invented process identity")}
		})
	}
}

func TestWatchdogObservationMutableRacesAreTypedAndCleanupIsNeverRetried(t *testing.T){
	for _,kind:=range []string{"normal","append","rotation","bytes","physical","foreign-path","link","read-error","close-error"}{
		t.Run(kind,func(t *testing.T){
			raw:=[]byte("{}\n");if kind=="append"{raw=[]byte("{}")};v,_:=watchdogMutableFixture(t,raw);path:=watchdogEventsPath
			if kind=="foreign-path"{path=watchdogTarget}
			if kind=="close-error"{v.closeFailAt=1}
			if kind=="rotation"{base:=v.scope.ops.create;v.scope.ops.create=func(p string,a,b,c uint32)(*os.File,error){f,e:=base(p,a,b,c);if f!=nil&&a==0x80&&b==7{i:=v.info[f];i.FileIndexLow++;v.info[f]=i};return f,e}}
			calls:=0;value,e:=watchdogReadObservationMutableWith(v.scope,path,8192,func(f *os.File,n int64)([]byte,error){
				calls++;got:=append([]byte(nil),raw...);i:=v.info[f]
				if calls==1{switch kind{case "physical":i.FileIndexLow++;case "link":i.NumberOfLinks=2};v.info[f]=i}
				if kind=="bytes"&&calls==2{got[0]='x'}
				if kind=="read-error"{return nil,errors.New("unclassified_read")};return got,nil
			})
			transient:=kind=="append"||kind=="rotation"||kind=="bytes"
			if kind=="normal"||kind=="close-error"{if e!=nil||string(value)!=string(raw){t.Fatal("normal snapshot",e)}}else if e==nil||watchdogObservationRetry(e)!=transient{t.Fatal("readiness error classification",e)}
			cleanup:=v.scope.Close();if (cleanup!=nil)!=(kind=="close-error"){t.Fatal("cleanup counter not exercised",cleanup)}
			if kind=="close-error"{
				if !errors.Is(cleanup,v.cleanup)||v.counts[v.files[0]]!=1{t.Fatal("intended cleanup fault not exercised",cleanup)}
				if watchdogObservationRetry(errors.Join(watchdogObservationPending("fixture_race"),cleanup)){t.Fatal("pending plus cleanup became transient")}
			}
			if cleanup!=nil{e=errors.Join(errors.New("cleanup"),e,cleanup);if watchdogObservationRetry(e){t.Fatal("cleanup became transient")}}
			again:=v.scope.Close();if kind=="close-error"&&!errors.Is(again,v.cleanup){t.Fatal("cached cleanup fault lost",again)}
			for _,f:=range v.files{if v.counts[f]!=1{t.Fatal("read pin Close retried")}}
		})
	}
}

type watchdogObserveUpdateFixture struct{*watchdogTestResident;native *watchdogResidentNative}
func(f *watchdogObserveUpdateFixture) Observe(ctx context.Context,id watchdogFileIdentity)(watchdogObservation,error){return f.native.Observe(ctx,id)}
func(f *watchdogObserveUpdateFixture) Close()error{return errors.Join(f.native.Close(),f.watchdogTestResident.Close())}
func TestWatchdogUpdateDoesNotRollbackWhileObservationIsStillNotReady(t *testing.T){
	for _,mode:=range []string{"progress","deadline"}{
		t.Run(mode,func(t *testing.T){
			f:=watchdogObserveFixtureNew(t,"chain",mode);all,files,resident:=watchdogTestResources();resident.task=f.r.task.View
			all.Resident=&watchdogObserveUpdateFixture{watchdogTestResident:resident,native:f.r}
			result:=runWatchdogUpdate(context.Background(),true,func(context.Context)(*watchdogUpdateResources,error){return all,nil})
			if mode=="progress"{
				if !result.Qualified||!result.ObservationAccepted||result.RollbackAttempted||strings.Contains(strings.Join(files.calls,","),"rollback"){t.Fatal("not-ready forced rollback",result)}
			}else if result.Qualified||result.Reason!="watchdog_postpublish_observation_failed"||!result.RollbackAttempted||!result.RollbackSucceeded{t.Fatal("finite deadline did not use existing recovery",result)}
			if f.pauses==0||resident.closeCount!=1||files.closeCount!=1{t.Fatal("same consumer not exercised")}
			f.assertClosedOnce(t)
		})
	}
}

// After the initial six samples, event readiness must not suspend current-health
// checks. The same Observe and update consumer are used; no Task/host is started.
func watchdogEventHealthFixture(t *testing.T,mode string)*watchdogObserveFixture{
	t.Helper();f:=watchdogObserveFixtureNew(t,"event-health","progress")
	baseSample:=f.r.observation.sample
	f.r.observation.events=func(context.Context,time.Time,uint32)error{
		f.events++
		if mode=="post-event"{return nil}
		if f.events==1||mode=="deadline"{
			if f.events==1{f.now=f.now.Add(40*time.Second)}
			return watchdogObservationPending("events_appending")
		}
		return nil
	}
	f.r.observation.sample=func(ctx context.Context)(watchdogSample,error){
		s,e:=baseSample(ctx);if e!=nil{return s,e}
		if f.events>0{
			switch mode{
			case "unhealthy","post-event":s.Healthy=false
			case "write-failed":s.WriteFailures=1
			case "foreign-pid":s.WatchdogPID=9999
			case "stale":s.At=f.r.published.Add(52*time.Second)
			case "malformed":return watchdogSample{},errors.New("watchdog_status_schema")
			case "sample-pending":if f.events==1&&f.samples==7{return watchdogSample{},watchdogObservationPending("mutable_snapshot_raced")}
			}
		}
		return s,nil
	}
	return f
}
func TestWatchdogObserveRechecksHealthAcrossEventPendingAndAfterEventRead(t *testing.T){
	for _,mode:=range []string{"healthy","sample-pending","unhealthy","write-failed","foreign-pid","stale","malformed","post-event","deadline"}{
		t.Run(mode,func(t *testing.T){
			f:=watchdogEventHealthFixture(t,mode)
			result,e:=f.r.Observe(context.Background(),watchdogTestID())
			good:=mode=="healthy"||mode=="sample-pending"
			if good{
				if e!=nil||!result.valid(watchdogTestID())||f.events!=2||f.samples<8||f.now.Sub(result.Samples[len(result.Samples)-1].At)>25*time.Second{t.Fatal("event convergence lost current health",mode,result,e)}
			}else{
				if e==nil||watchdogObservationRetry(e){t.Fatal("old healthy samples authorized acceptance",mode,result,e)}
				if (mode=="deadline")!=errors.Is(e,context.DeadlineExceeded){t.Fatal("foreign health was retried/deadline lost",mode,e)}
				if mode!="deadline"&&f.events!=1{t.Fatal("health contradiction was not immediate",f.events)}
			}
			if f.samples<=6{t.Fatal("current sample was not reacquired after six",f.samples)}
			if f.now.Sub(f.r.published)>152*time.Second{t.Fatal("event retry extended overall deadline")}
			f.assertClosedOnce(t)
		})
	}
}
func TestWatchdogUpdateRejectsHealthLostWhileWaitingForEvents(t *testing.T){
	for _,mode:=range []string{"healthy","unhealthy","write-failed","foreign-pid","stale","post-event"}{
		t.Run(mode,func(t *testing.T){
			f:=watchdogEventHealthFixture(t,mode);all,files,resident:=watchdogTestResources();resident.task=f.r.task.View
			all.Resident=&watchdogObserveUpdateFixture{watchdogTestResident:resident,native:f.r}
			result:=runWatchdogUpdate(context.Background(),true,func(context.Context)(*watchdogUpdateResources,error){return all,nil})
			if mode=="healthy"{
				if !result.Qualified||!result.ObservationAccepted||result.RollbackAttempted{t.Fatal("healthy delayed events refused",result)}
			}else if result.Qualified||result.ObservationAccepted||!result.RollbackAttempted||!result.RollbackSucceeded||result.Reason!="watchdog_postpublish_observation_failed"{t.Fatal("event ready hid health failure",mode,result)}
			if f.samples<=6||files.closeCount!=1||resident.closeCount!=1{t.Fatal("same consumer or final health was skipped")}
			f.assertClosedOnce(t)
		})
	}
}
