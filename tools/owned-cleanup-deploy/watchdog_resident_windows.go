//go:build windows

package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"encoding/base64"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"errors"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

const watchdogLaunchMutex = "Global\\CC-ResidentGuard-Launch-opencodex_watchdog"
const watchdogWatchMutex = "Global\\CC-OcxHealthWatchdog"

// This adapter retains exact Task Scheduler and process generations. The two
// mutex objects are existing runtime objects; it creates no new monitor/task.
type watchdogResidentNative struct{
	task watchdogTaskSnapshot
	chain []*watchdogProcess
	imageBirth uint64
	previousPID uint32
	candidate func()watchdogFileIdentity
	classify func(context.Context,string)error
	pins *watchdogPinScope
	launcherMutex syscall.Handle
	watchMutex syscall.Handle
	watchOwned bool
	threadPinned bool
	threadID uint32
	published time.Time
	closed bool
	closeErr error
	retainedDescriptors []unsafe.Pointer
	temporaryPins []*watchdogPinScope
	releaseErr error
	watchReleaseAttempted bool
	lifetime watchdogLifetimeCollection
	collection watchdogCollectionState
	recoveryHold *watchdogLifetimeHoldState
	retainedHolds []*watchdogLifetimeHoldState
	taskRead func(context.Context,string,string,string,uint32)(watchdogTaskSnapshot,error)
	observation *watchdogObservationOps
}

func openWatchdogResident(ctx context.Context,candidate func()watchdogFileIdentity)(r *watchdogResidentNative,err error){
	r=&watchdogResidentNative{candidate:candidate,classify:watchdogQualify,pins:newWatchdogPinScope()}
	if ctx==nil||ctx.Err()!=nil||candidate==nil{return r,errors.New("watchdog_resident_context")}
	runtime.LockOSThread()
	r.threadPinned=true
	id,_,_:=kernel.NewProc("GetCurrentThreadId").Call();r.threadID=uint32(id)
	r.task,err=r.readTaskSnapshot(ctx,"read","", "",0)
	if err!=nil{return r,err}
	if len(r.task.Instances)!=1{return r,errors.New("watchdog_task_instance_ambiguous")}
	r.chain,err=watchdogOwnedChain(r.task.Instances[0],r.task.Principal)
	if err!=nil{return r,err}
	r.previousPID=r.chain[len(r.chain)-1].pid
	r.imageBirth=r.chain[len(r.chain)-1].created
	r.lifetime,err=openNormalWatchdogLifetime(ctx,r.task,r.chain)
	if err!=nil||r.lifetime==nil{return r,errors.Join(errors.New("watchdog_lifetime_issuer_unavailable"),err)}
	return r,nil
}

func watchdogOwnedChain(instance watchdogTaskInstance,principal string)([]*watchdogProcess,error){return watchdogReadOwnedChain(instance,principal,false)}
func watchdogObserveOwnedChain(instance watchdogTaskInstance,principal string)([]*watchdogProcess,error){return watchdogReadOwnedChain(instance,principal,true)}
func watchdogReadOwnedChain(instance watchdogTaskInstance,principal string,starting bool)([]*watchdogProcess,error){
	rows,e:=watchdogProcessEntries();if e!=nil{return nil,e}
	return watchdogChainFromSnapshotMode(instance,principal,rows,func(pid,parent uint32)(*watchdogProcess,string,bool,error){
		process,e:=openWatchdogProcess(pid,parent);if e!=nil{return process,"",false,e}
		sid,e:=process.sid();return process,sid,process.live(),e
	},starting)
}

// Both normal launcher branches are closed shapes: the already-limited action
// starts the watchdog directly; privilege reduction inserts one launcher child.
// The callback only acquires observations; this helper issues no lifetime proof.
func watchdogChainFromSnapshot(instance watchdogTaskInstance,principal string,rows []watchdogProcessEntry,
	open func(uint32,uint32)(*watchdogProcess,string,bool,error))([]*watchdogProcess,error){
	return watchdogChainFromSnapshotMode(instance,principal,rows,open,false)
}
func watchdogChainFromSnapshotMode(instance watchdogTaskInstance,principal string,rows []watchdogProcessEntry,
	open func(uint32,uint32)(*watchdogProcess,string,bool,error),starting bool)(chain []*watchdogProcess,err error){
	lookup:=map[uint32]watchdogProcessEntry{}
	for _,e:=range rows{lookup[e.PID]=e}
	pid:=instance.PID
	for depth:=0;depth<3;depth++{
		entry,ok:=lookup[pid];if !ok{return chain,errors.New("watchdog_process_chain_missing")}
		process,sid,live,e:=open(pid,entry.Parent)
		if process!=nil{chain=append(chain,process)}
		if e!=nil{return chain,e}
		if process==nil||process.pid!=pid||process.parent!=entry.Parent||!live||process.closed{
			return chain,errors.New("watchdog_process_image_or_state")
		}
		if principal==""||sid!=principal{return chain,errors.New("watchdog_process_owner_mismatch")}
		if process.created==0||depth>0&&process.created<chain[depth-1].created{
			return chain,errors.New("watchdog_process_birth_order")
		}
		if depth>0&&same(process.image,watchdogTarget){
			if !watchdogChainShape(chain){return chain,errors.New("watchdog_process_image_or_state")}
			if e=watchdogChainRows(chain,rows);e!=nil{return chain,e}
			return chain,nil
		}
		if !same(process.image,watchdogLauncher){return chain,errors.New("watchdog_process_image_or_state")}
		var children []uint32
		for _,child:=range rows{if child.Parent==pid{children=append(children,child.PID)}}
		if len(children)==0&&starting&&depth<2{return chain,watchdogObservationPending("chain_starting")}
		if len(children)!=1{return chain,errors.New("watchdog_process_chain_ambiguous")}
		pid=children[0]
	}
	return chain,errors.New("watchdog_process_chain_count")
}

func watchdogChainShape(chain []*watchdogProcess)bool{
	if len(chain)!=2&&len(chain)!=3{return false}
	seen:=map[uint32]bool{}
	for depth,p:=range chain{
		if p==nil||p.closed||p.pid==0||p.created==0||seen[p.pid]{return false}
		seen[p.pid]=true
		want:=watchdogLauncher;if depth==len(chain)-1{want=watchdogTarget}
		if !same(p.image,want)||depth>0&&(p.parent!=chain[depth-1].pid||p.created<chain[depth-1].created){return false}
	}
	return true
}

// The same closed shape is rechecked before Stop, including the leaf's lack of
// descendants. A two-process branch does not allow an arbitrary shorter tree.
func watchdogChainRows(chain []*watchdogProcess,rows []watchdogProcessEntry)error{
	if !watchdogChainShape(chain){return errors.New("watchdog_process_chain_count")}
	for depth,p:=range chain{
		found:=false
		for _,row:=range rows{if row.PID==p.pid&&row.Parent==p.parent{found=true;break}}
		if !found{return errors.New("watchdog_chain_changed_before_stop")}
		var children []uint32
		for _,row:=range rows{if row.Parent==p.pid{children=append(children,row.PID)}}
		if depth<len(chain)-1{
			if len(children)!=1||children[0]!=chain[depth+1].pid{return errors.New("watchdog_chain_descendants_changed")}
		}else if len(children)!=0{return errors.New("watchdog_uncollected_descendant")}
	}
	return nil
}
func closeWatchdogChain(chain []*watchdogProcess)error{
	var es []error
	for i:=len(chain)-1;i>=0;i--{es=append(es,chain[i].Close())}
	return errors.Join(es...)
}

func (r *watchdogResidentNative) Task(ctx context.Context)(watchdogTaskView,error){
	if r.closed{return watchdogTaskView{},errors.New("watchdog_resident_closed")}
	current,err:=r.readTaskSnapshot(ctx,"read","", "",0)
	if err!=nil{return watchdogTaskView{},err}
	return current.View,nil
}

func (r *watchdogResidentNative) acquireExistingHold()error{
	if !r.onThread(){return errors.New("watchdog_resident_thread_changed")}
	if r.releaseErr!=nil{return r.releaseErr}
	if r.launcherMutex==0{
		name,_:=syscall.UTF16PtrFromString(watchdogLaunchMutex)
		h,_,err:=kernel.NewProc("OpenMutexW").Call(0x100000,0,uintptr(unsafe.Pointer(name)))
		runtime.KeepAlive(name)
		if h==0{return err};r.launcherMutex=syscall.Handle(h)
	}
	if r.watchMutex==0{
		name,_:=syscall.UTF16PtrFromString(watchdogWatchMutex)
		h,_,err:=kernel.NewProc("OpenMutexW").Call(0x100001,0,uintptr(unsafe.Pointer(name)))
		runtime.KeepAlive(name)
		if h==0{return err};r.watchMutex=syscall.Handle(h)
	}
	return nil
}

func (r *watchdogResidentNative) stopChain(ctx context.Context,snapshot watchdogTaskSnapshot,chain []*watchdogProcess,published bool)(watchdogStopped,error){
	if r.closed||ctx.Err()!=nil||len(snapshot.Instances)!=1||!watchdogChainShape(chain){
		return watchdogStopped{},errors.New("watchdog_stop_generation_missing")
	}
	current,err:=r.readTaskSnapshot(ctx,"read","", "",0)
	if err!=nil||current.View!=r.task.View||current.Principal!=r.task.Principal||
		len(current.Instances)!=1||current.Instances[0]!=snapshot.Instances[0]{
		return watchdogStopped{},errors.New("watchdog_instance_changed_before_stop")
	}
	for _,p:=range chain{if p.recheck()!=nil||!p.live(){return watchdogStopped{},errors.New("watchdog_held_process_changed")}}
	// Snapshot parent links are compared again while all handles stay held.
	rows,e:=watchdogProcessEntries()
	if e!=nil{return watchdogStopped{},e}
	if e=watchdogChainRows(chain,rows);e!=nil{return watchdogStopped{},e}
	if r.lifetime==nil{return watchdogStopped{},errWatchdogCollectionUnknown}
	if err=r.lifetime.BeforeStop(ctx,current,chain);err!=nil{return watchdogStopped{},err}
	if err=r.acquireExistingHold();err!=nil{return watchdogStopped{},err}
	// Holding the existing launch object keeps CreateMutex/ALREADY_EXISTS true
	// after the old launcher exits. Hold ownership of the watchdog mutex is
	// acquired only after every retained process has signalled.
	i:=current.Instances[0]
	r.collection.begin(true)
	if _,err=r.readTaskSnapshot(ctx,"stop",i.GUID,current.View.XMLSHA256,i.PID);err!=nil{
		return watchdogStopped{},err
	}
	for _,p:=range chain{if err=p.wait(ctx);err!=nil{return watchdogStopped{},err}}
	wait,err:=syscall.WaitForSingleObject(r.watchMutex,0)
	if err!=nil||(wait!=0&&wait!=0x80){return watchdogStopped{},errors.New("watchdog_watch_mutex_unavailable")}
	r.watchOwned=true
	r.watchReleaseAttempted=false
	after,err:=r.readTaskSnapshot(ctx,"read","", "",0)
	if err!=nil||after.View!=r.task.View||len(after.Instances)!=0{
		return watchdogStopped{},errors.New("watchdog_stop_not_quiescent")
	}
	if err=r.confirmCollection(ctx,current,published);err!=nil{return watchdogStopped{},err}
	return watchdogStopped{OwnedGeneration:true,AllReaped:true,HoldAcquired:true},nil
}
func (r *watchdogResidentNative) StopAndHold(ctx context.Context)(watchdogStopped,error){
	c,cancel:=context.WithTimeout(ctx,30*time.Second);defer cancel()
	return r.stopChain(c,r.task,r.chain,false)
}
func (r *watchdogResidentNative) ReleaseHold(ctx context.Context)error{
	if !r.onThread(){return errors.New("watchdog_resident_thread_changed")}
	if !r.closed&&r.published.IsZero(){
		if r.lifetime==nil{return errWatchdogCollectionUnknown}
		at:=time.Now().UTC()
		if e:=r.lifetime.BeforeRelease(ctx,r.task,r.candidate(),at);e!=nil{return e}
		r.published=at
	}
	var es []error
	if r.recoveryHold!=nil{
		// A newly issued hold has its own release-once result. An earlier
		// generation's failed release/Close must not skip this new resource.
		return errors.Join(r.releaseErr,r.recoveryHold.release(ctx))
	}
	if r.releaseErr!=nil{return r.releaseErr}
	if r.watchOwned&&!r.watchReleaseAttempted{
		r.watchReleaseAttempted=true
		ok,_,err:=kernel.NewProc("ReleaseMutex").Call(uintptr(r.watchMutex))
		if ok==0{
			r.releaseErr=errors.Join(errors.New("watchdog_watch_mutex_release_failed"),err)
			return r.releaseErr
		}else{r.watchOwned=false}
	}
	// Do not retry a failed Close; retain failed handles through the common
	// fixed cleanup ledger. The object may or may not have been released.
	for _,h:=range []*syscall.Handle{&r.watchMutex,&r.launcherMutex}{
		if *h!=0{es=append(es,watchdogCloseNative(*h));*h=0}
	}
	r.releaseErr=errors.Join(es...)
	return r.releaseErr
}
func (r *watchdogResidentNative) StopPublishedAndHold(ctx context.Context,id watchdogFileIdentity)(watchdogStopped,error){
	if r.closed||id!=r.candidate(){return watchdogStopped{},errors.New("watchdog_published_identity_changed")}
	snapshot,err:=r.readTaskSnapshot(ctx,"read","", "",0)
	if err!=nil{return watchdogStopped{},err}
	if snapshot.View!=r.task.View||snapshot.Principal!=r.task.Principal{return watchdogStopped{},errors.New("watchdog_recovery_task_changed")}
	if len(snapshot.Instances)==0{return r.holdQuiescentPublished(ctx,snapshot,id)}
	if len(snapshot.Instances)!=1{return watchdogStopped{},errors.New("watchdog_new_instance_unavailable")}
	chain,err:=watchdogOwnedChain(snapshot.Instances[0],snapshot.Principal)
	if chain!=nil{r.chain=append(r.chain,chain...)}
	if err!=nil{return watchdogStopped{},err}
	last:=chain[len(chain)-1]
	if r.published.IsZero()||last.created<116444736000000000||
		time.Unix(0,int64(last.created-116444736000000000)*100).Before(r.published){
		return watchdogStopped{},errors.New("watchdog_recovery_process_not_new_generation")
	}
	return r.stopChain(ctx,snapshot,chain,true)
}

// Observation files alone permit compatible writer sharing. A consistent
// same-object write or atomic name replacement is pending; invalid metadata,
// parent/path changes and cleanup errors remain hard failures.
func watchdogReadObservationMutableWith(scope *watchdogPinScope,path string,limit int64,read func(*os.File,int64)([]byte,error))([]byte,error){
	if scope==nil||read==nil||limit<1||limit>6<<20||(path!=watchdogStatusPath&&path!=watchdogEventsPath){return nil,errors.New("watchdog_mutable_scope_invalid")}
	if _,e:=scope.parents(path);e!=nil{return nil,e}
	held,e:=scope.open(path,syscall.GENERIC_READ,syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE,syscall.OPEN_EXISTING)
	if e!=nil{if os.IsNotExist(e){return nil,watchdogObservationPending("observation_not_written")};return nil,e}
	valid:=func(i syscall.ByHandleFileInformation)bool{return i.FileAttributes&(0x10|0x400)==0&&i.NumberOfLinks==1&&i.FileSizeHigh==0&&int64(i.FileSizeLow)<=limit}
	physical:=func(a,b syscall.ByHandleFileInformation)bool{return a.VolumeSerialNumber==b.VolumeSerialNumber&&a.FileIndexHigh==b.FileIndexHigh&&a.FileIndexLow==b.FileIndexLow&&a.CreationTime==b.CreationTime}
	before,e:=scope.ops.info(held.f);if e!=nil{return nil,e}
	if !valid(held.info)||!valid(before)||!physical(held.info,before){return nil,errors.New("watchdog_mutable_metadata_unverified")}
	a,e:=read(held.f,limit);if e!=nil{return nil,e}
	middle,e:=scope.ops.info(held.f);if e!=nil{return nil,e}
	b,e:=read(held.f,limit);if e!=nil{return nil,e}
	after,e:=scope.ops.info(held.f);if e!=nil{return nil,e}
	if !valid(middle)||!valid(after)||!physical(before,middle)||!physical(middle,after)||len(a)>int(limit)||len(b)>int(limit){return nil,errors.New("watchdog_mutable_metadata_unverified")}
	if held.info.LastWriteTime!=before.LastWriteTime||held.info.FileSizeLow!=before.FileSizeLow||
		before.LastWriteTime!=middle.LastWriteTime||middle.LastWriteTime!=after.LastWriteTime||before.FileSizeLow!=middle.FileSizeLow||middle.FileSizeLow!=after.FileSizeLow||
		len(a)!=int(before.FileSizeLow)||!bytes.Equal(a,b){return nil,watchdogObservationPending("mutable_snapshot_raced")}
	if path==watchdogEventsPath&&(len(a)==0||a[len(a)-1]!='\n'){return nil,watchdogObservationPending("events_appending")}
	named,e:=scope.open(path,0x80,7,3)
	if e!=nil{if os.IsNotExist(e){return nil,watchdogObservationPending("mutable_name_rotating")};return nil,e}
	var primary error
	current,e:=scope.ops.info(held.f)
	if e!=nil{primary=e}else if !valid(current)||!valid(named.info)||!physical(current,before){primary=errors.New("watchdog_mutable_metadata_unverified")}else if !physical(named.info,current){primary=watchdogObservationPending("mutable_name_rotating")}else if current.LastWriteTime!=after.LastWriteTime||current.FileSizeLow!=after.FileSizeLow||named.info.LastWriteTime!=current.LastWriteTime||named.info.FileSizeLow!=current.FileSizeLow{primary=watchdogObservationPending("mutable_snapshot_raced")}
	if cleanup:=scope.closePin(named);cleanup!=nil{return nil,errors.Join(errors.New("watchdog_observation_read_cleanup_failed"),primary,cleanup)}
	if primary!=nil{return nil,primary}
	return a,nil
}
func(r *watchdogResidentNative) readObservationMutable(ctx context.Context,path string,limit int64)([]byte,error){
	if e:=r.classify(ctx,path);e!=nil{return nil,e}
	scope:=newWatchdogPinScope();r.temporaryPins=append(r.temporaryPins,scope)
	raw,primary:=watchdogReadObservationMutableWith(scope,path,limit,func(f *os.File,n int64)([]byte,error){if _,e:=f.Seek(0,io.SeekStart);e!=nil{return nil,e};return io.ReadAll(io.LimitReader(f,n+1))})
	if cleanup:=scope.Close();cleanup!=nil{return nil,errors.Join(errors.New("watchdog_observation_read_cleanup_failed"),primary,cleanup)}
	return raw,primary
}

func (r *watchdogResidentNative) sample(ctx context.Context)(watchdogSample,error){
	path:="D:/CC/Tools/ocx-health-watchdog/status.json"
	raw,err:=r.readObservationMutable(ctx,path,8192)
	if err!=nil{return watchdogSample{},err}
	var v struct{
		At time.Time `json:"at"`
		PID uint32 `json:"watchdog_pid"`
		Healthy *bool `json:"healthy"`
		Kind string `json:"observation_kind"`
		Identity json.RawMessage `json:"identity"`
		Metrics json.RawMessage `json:"metrics"`
		Eligible *bool `json:"eligible"`
		Failures *uint32 `json:"failures"`
		Error json.RawMessage `json:"error"`
		Writes *uint64 `json:"observation_write_failures"`
	}
	if json.Unmarshal(raw,&v)!=nil||v.At.IsZero()||v.Healthy==nil||v.Eligible==nil||v.Failures==nil||v.Writes==nil||
		len(v.Identity)==0||len(v.Metrics)==0||len(v.Error)==0{return watchdogSample{},errors.New("watchdog_status_schema")}
	return watchdogSample{
		At:v.At,WatchdogPID:v.PID,Healthy:*v.Healthy,ObservationKind:v.Kind,
		IdentityPresent:!bytes.Equal(bytes.TrimSpace(v.Identity),[]byte("null")),
		MetricsPresent:!bytes.Equal(bytes.TrimSpace(v.Metrics),[]byte("null")),
		Eligible:*v.Eligible,Failures:*v.Failures,
		ErrorPresent:!bytes.Equal(bytes.TrimSpace(v.Error),[]byte("null")),WriteFailures:*v.Writes,
	},nil
}


// A typed observation-only pending state never authorizes Stop or publication.
// Unknown errors and errors combined with cleanup failure are not this type.
type watchdogObserveNotReady struct{reason string}
func(e *watchdogObserveNotReady) Error()string{return "watchdog_observation_not_ready_"+e.reason}
func watchdogObservationPending(reason string)error{return &watchdogObserveNotReady{reason:reason}}
func watchdogObservationRetry(err error)bool{_,ok:=err.(*watchdogObserveNotReady);return ok}

type watchdogObservationOps struct{
	task func(context.Context)(watchdogTaskSnapshot,error)
	chain func(watchdogTaskInstance,string)([]*watchdogProcess,error)
	closeChain func([]*watchdogProcess)error
	checkProcess func(*watchdogProcess)error
	image func(context.Context,watchdogFileIdentity)error
	sample func(context.Context)(watchdogSample,error)
	events func(context.Context,time.Time,uint32)error
	now func()time.Time
	pause func(context.Context,time.Duration)error
}
func watchdogObservePause(ctx context.Context,d time.Duration)error{
	select{case<-ctx.Done():return ctx.Err();case<-time.After(d):return nil}
}

// The strict task reader remains the only reader used by Stop/check. This
// observation reader reuses the same fixed read script and definition checks,
// but preserves queued/incomplete instance fields as data, never fake identities.
func readWatchdogObservationTask(ctx context.Context)(watchdogTaskSnapshot,error){
	if ctx==nil||ctx.Err()!=nil{return watchdogTaskSnapshot{},errors.New("watchdog_task_context")}
	powershell:=filepath.Join(os.Getenv("SystemRoot"),"System32/WindowsPowerShell/v1.0/powershell.exe")
	if !filepath.IsAbs(powershell){return watchdogTaskSnapshot{},errors.New("watchdog_systemroot_unavailable")}
	if e:=watchdogQualify(ctx,powershell);e!=nil{return watchdogTaskSnapshot{},e}
	c,cancel:=context.WithTimeout(ctx,15*time.Second);defer cancel()
	words:=syscall.StringToUTF16(watchdogTaskScript);raw:=make([]byte,(len(words)-1)*2)
	for i,w:=range words[:len(words)-1]{raw[i*2]=byte(w);raw[i*2+1]=byte(w>>8)}
	cmd:=exec.CommandContext(c,powershell,"-NoProfile","-NonInteractive","-EncodedCommand",base64.StdEncoding.EncodeToString(raw))
	cmd.SysProcAttr=&syscall.SysProcAttr{HideWindow:true,CreationFlags:0x08000000};cmd.WaitDelay=time.Second
	env:=[]string{}
	for _,v:=range os.Environ(){key:=strings.ToUpper(strings.SplitN(v,"=",2)[0]);if !strings.HasPrefix(key,"WATCHDOG_FIXED_"){env=append(env,v)}}
	cmd.Env=append(env,"WATCHDOG_FIXED_TASK_MODE=read","WATCHDOG_FIXED_INSTANCE=","WATCHDOG_FIXED_XML=","WATCHDOG_FIXED_ENGINE=0")
	var stdout,stderr watchdogLimitedOutput;cmd.Stdout=&stdout;cmd.Stderr=&stderr
	e:=cmd.Run()
	if e!=nil||c.Err()!=nil||stdout.overflow||stderr.overflow||stderr.Len()!=0{return watchdogTaskSnapshot{},errors.New("watchdog_task_command_unconfirmed")}
	return decodeWatchdogObservationTask(stdout.Bytes())
}
func decodeWatchdogObservationTask(raw []byte)(watchdogTaskSnapshot,error){
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw,&fields)!=nil||fields==nil{return watchdogTaskSnapshot{},errors.New("watchdog_task_schema")}
	var instances []watchdogTaskInstance
	if json.Unmarshal(fields["instances"],&instances)!=nil{return watchdogTaskSnapshot{},errors.New("watchdog_task_schema")}
	// Project only an empty instance list for the unchanged definition verifier;
	// the real list below is kept separately and is never made running/nonzero.
	fields["instances"]=json.RawMessage("[]")
	definition,e:=json.Marshal(fields);if e!=nil{return watchdogTaskSnapshot{},e}
	snapshot,e:=decodeWatchdogTask(definition);if e!=nil{return snapshot,e}
	snapshot.Instances=instances
	for _,i:=range instances{
		if !watchdogInstanceGUID(i.GUID)||i.Path!="\\CC-OcxHealthWatchdog"||
			(i.Action!=""&&!same(i.Action,watchdogLauncher))||(i.State!=2&&i.State!=4){
			return watchdogTaskSnapshot{},errors.New("watchdog_running_instance_unverified")
		}
	}
	if len(instances)>1{return watchdogTaskSnapshot{},errors.New("watchdog_instance_ambiguous")}
	return snapshot,nil
}

func watchdogObservationBirth(created uint64)(time.Time,error){
	if created<116444736000000000{return time.Time{},errors.New("watchdog_birth_epoch_invalid")}
	return time.Unix(int64(created/10000000)-11644473600,int64(created%10000000)*100).UTC(),nil
}

type watchdogObserveProcessIdentity struct{pid,parent uint32;created uint64;image string}
type watchdogObserveGeneration struct{guid string;engine uint32;ready bool;prefix []watchdogObserveProcessIdentity}
func(g *watchdogObserveGeneration) instance(snapshot watchdogTaskSnapshot,original watchdogTaskSnapshot)error{
	if snapshot.View!=original.View||snapshot.Principal!=original.Principal{return errors.New("watchdog_task_definition_changed")}
	if len(snapshot.Instances)==0{
		if g.guid!=""{return errors.New("watchdog_observed_instance_disappeared")}
		return watchdogObservationPending("instance_absent")
	}
	if len(snapshot.Instances)!=1{return errors.New("watchdog_instance_ambiguous")}
	i:=snapshot.Instances[0]
	if !watchdogInstanceGUID(i.GUID)||i.Path!="\\CC-OcxHealthWatchdog"||
		(i.Action!=""&&!same(i.Action,watchdogLauncher))||(i.State!=2&&i.State!=4){return errors.New("watchdog_running_instance_unverified")}
	if len(original.Instances)==1&&(i.GUID==original.Instances[0].GUID||i.PID!=0&&i.PID==original.Instances[0].PID){return errors.New("watchdog_old_task_generation")}
	if g.guid!=""&&g.guid!=i.GUID{return errors.New("watchdog_task_instance_generation_changed")}
	if g.engine!=0&&i.PID!=g.engine{return errors.New("watchdog_task_instance_generation_changed")}
	if g.ready&&(i.State!=4||i.Action==""){return errors.New("watchdog_task_instance_generation_changed")}
	g.guid=i.GUID;if i.PID!=0{g.engine=i.PID}
	if i.State==2||i.PID==0||i.Action==""{return watchdogObservationPending("instance_starting")}
	g.ready=true
	return nil
}
func(g *watchdogObserveGeneration) chain(chain []*watchdogProcess,published time.Time)error{
	for depth,p:=range chain{
		if p==nil||p.closed||p.pid==0||p.created==0{return errors.New("watchdog_observed_process_changed")}
		started,e:=watchdogObservationBirth(p.created);if e!=nil||started.Before(published){return errors.New("watchdog_new_process_generation_invalid")}
		if depth==0&&p.pid!=g.engine{return errors.New("watchdog_task_instance_generation_changed")}
		if depth<len(g.prefix){old:=g.prefix[depth];if old.pid!=p.pid||old.parent!=p.parent||old.created!=p.created||!same(old.image,p.image){return errors.New("watchdog_process_generation_changed")}}
	}
	if len(chain)<len(g.prefix){return errors.New("watchdog_process_generation_changed")}
	if len(chain)>len(g.prefix){for _,p:=range chain[len(g.prefix):]{g.prefix=append(g.prefix,watchdogObserveProcessIdentity{pid:p.pid,parent:p.parent,created:p.created,image:p.image})}}
	return nil
}

func(r *watchdogResidentNative) observationImage(ctx context.Context,id watchdogFileIdentity)error{
	if e:=r.classify(ctx,watchdogTarget);e!=nil{return e}
	image,e:=r.pins.open(watchdogTarget,syscall.GENERIC_READ|0x20000,7,3);if e!=nil{return e}
	observed,e:=watchdogIdentityHeld(image,&r.retainedDescriptors)
	if e!=nil||observed!=id||r.pins.verifyName(image,watchdogTarget)!=nil{return errors.New("watchdog_new_image_generation_changed")}
	return nil
}
func(r *watchdogResidentNative) observationOperations()watchdogObservationOps{
	if r.observation!=nil{return *r.observation}
	return watchdogObservationOps{
		task:func(ctx context.Context)(watchdogTaskSnapshot,error){if r.taskRead!=nil{return r.readTaskSnapshot(ctx,"read","","",0)};return readWatchdogObservationTask(ctx)},
		chain:watchdogObserveOwnedChain,closeChain:closeWatchdogChain,
		checkProcess:func(p *watchdogProcess)error{if p.recheck()!=nil||!p.live(){return errors.New("watchdog_observed_process_changed")};return nil},
		image:r.observationImage,sample:r.sample,events:r.eventsSince,now:time.Now,pause:watchdogObservePause,
	}
}

func (r *watchdogResidentNative) Observe(ctx context.Context,id watchdogFileIdentity)(watchdogObservation,error){
	result:=watchdogObservation{PreviousPID:r.previousPID,Published:r.published}
	if ctx==nil||r.closed||!r.onThread()||r.candidate==nil||id!=r.candidate()||r.published.IsZero(){return result,errors.New("watchdog_observation_unbound")}
	return r.observeWith(ctx,id,r.observationOperations())
}
func(r *watchdogResidentNative) observeWith(ctx context.Context,id watchdogFileIdentity,ops watchdogObservationOps)(watchdogObservation,error){
	result:=watchdogObservation{PreviousPID:r.previousPID,Published:r.published}
	if ctx==nil||r.closed||r.candidate==nil||id!=r.candidate()||r.published.IsZero()||
		ops.task==nil||ops.chain==nil||ops.closeChain==nil||ops.checkProcess==nil||ops.image==nil||ops.sample==nil||ops.events==nil||ops.now==nil||ops.pause==nil{return result,errors.New("watchdog_observation_unbound")}
	c,cancel:=context.WithTimeout(ctx,150*time.Second);defer cancel()
	deadline:=ops.now().Add(150*time.Second)
	check:=func()error{if e:=c.Err();e!=nil{return e};if !ops.now().Before(deadline){return context.DeadlineExceeded};if id!=r.candidate(){return errors.New("watchdog_published_identity_changed")};return nil}
	pause:=func(d time.Duration)error{if e:=check();e!=nil{return e};left:=deadline.Sub(ops.now());if d>left{d=left};if e:=ops.pause(c,d);e!=nil{return e};return check()}
	var generation watchdogObserveGeneration
	var current *watchdogProcess
	checkSample:=func(s watchdogSample)error{
		// An unchanged predecessor status may precede the first new sample only.
		if len(result.Samples)==0&&s.WatchdogPID==r.previousPID&&!s.At.After(r.published)&&s.At.Before(result.Started){return watchdogObservationPending("predecessor_status")}
		if s.WatchdogPID!=current.pid||s.At.Before(result.Started)||ops.now().Sub(s.At)>25*time.Second||s.At.After(ops.now().Add(2*time.Second)){return errors.New("watchdog_observation_stale")}
		if !s.Healthy||s.ObservationKind!="health_only"||s.IdentityPresent||s.MetricsPresent||s.Eligible||s.Failures!=0||s.ErrorPresent||s.WriteFailures!=0{return errors.New("watchdog_health_only_acceptance_failed")}
		last:=len(result.Samples)-1
		if last>=0&&s.At.Before(result.Samples[last].At){return errors.New("watchdog_observation_stale")}
		if last<0||s.At.After(result.Samples[last].At){
			if len(result.Samples)<6{result.Samples=append(result.Samples,s)}else{result.Samples[last]=s}
		}
		return nil
	}
	for{
		if e:=check();e!=nil{return result,e}
		snapshot,e:=ops.task(c);if e!=nil{return result,e}
		if e=check();e!=nil{return result,e}
		if e=generation.instance(snapshot,r.task);e!=nil{
			if watchdogObservationRetry(e){if e=pause(250*time.Millisecond);e!=nil{return result,e};continue};return result,e
		}
		if current==nil{
			chain,primary:=ops.chain(snapshot.Instances[0],snapshot.Principal)
			if len(chain)>0{r.chain=append(r.chain,chain...)}
			if e=generation.chain(chain,r.published);e!=nil{return result,errors.Join(primary,e)}
			if primary!=nil{
				cleanup:=ops.closeChain(chain)
				if cleanup!=nil{return result,errors.Join(primary,cleanup)}
				if watchdogObservationRetry(primary){if e=pause(250*time.Millisecond);e!=nil{return result,e};continue};return result,primary
			}
			if !watchdogChainShape(chain){return result,errors.New("watchdog_process_chain_count")}
			current=chain[len(chain)-1];result.PID=current.pid
			result.Started,e=watchdogObservationBirth(current.created);if e!=nil{return result,e}
			if result.PID==r.previousPID||result.Started.Before(r.published){return result,errors.New("watchdog_new_process_generation_invalid")}
			if e=ops.image(c,id);e!=nil{return result,e}
			result.OwnImage=id;result.OwnInstanceVerified=true
		}
		if e=ops.checkProcess(current);e!=nil{return result,e}
		// Continue observing current health even after the initial six samples.
		s,e:=ops.sample(c)
		if e!=nil{if watchdogObservationRetry(e){if e=pause(250*time.Millisecond);e!=nil{return result,e};continue};return result,e}
		if e=check();e!=nil{return result,e}
		if e=checkSample(s);e!=nil{if watchdogObservationRetry(e){if e=pause(250*time.Millisecond);e!=nil{return result,e};continue};return result,e}
		if len(result.Samples)<6{if e=pause(time.Second);e!=nil{return result,e};continue}
		if e=ops.events(c,r.published,current.pid);e!=nil{
			if watchdogObservationRetry(e){if e=pause(250*time.Millisecond);e!=nil{return result,e};continue};return result,e
		}
		// Event acquisition may block or have retried. Bind acceptance to the
		// same Task/process and a fresh status read after the events are ready.
		snapshot,e=ops.task(c);if e!=nil{return result,e}
		if e=generation.instance(snapshot,r.task);e!=nil{return result,e}
		if e=check();e!=nil{return result,e}
		s,e=ops.sample(c)
		if e!=nil{if watchdogObservationRetry(e){if e=pause(250*time.Millisecond);e!=nil{return result,e};continue};return result,e}
		if e=check();e!=nil{return result,e}
		if e=checkSample(s);e!=nil{return result,e}
		if e=ops.checkProcess(current);e!=nil{return result,e}
		if e=check();e!=nil{return result,e}
		if ops.now().Sub(s.At)>25*time.Second{return result,errors.New("watchdog_observation_stale")}
		if !result.valid(id){return result,errors.New("watchdog_health_only_acceptance_failed")}
		return result,nil
	}
}

func(r *watchdogResidentNative) closeOwnedChains()error{
	if r.observation!=nil&&r.observation.closeChain!=nil{return r.observation.closeChain(r.chain)}
	return closeWatchdogChain(r.chain)
}

func (r *watchdogResidentNative) eventsSince(ctx context.Context,since time.Time,pid uint32)error{
	path:="D:/CC/Tools/ocx-health-watchdog/events.jsonl"
	raw,err:=r.readObservationMutable(ctx,path,6<<20)
	if err!=nil{return err}
	scanner:=bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte,65536),65536)
	started:=false
	for scanner.Scan(){
		var row struct{At time.Time `json:"at"`;Event struct{Kind string `json:"kind"`;PID uint32 `json:"watchdog_pid"`} `json:"event"`}
		if json.Unmarshal(scanner.Bytes(),&row)!=nil{return errors.New("watchdog_event_schema")}
		if row.At.IsZero()||strings.TrimSpace(row.Event.Kind)==""{return errors.New("watchdog_event_schema")}
		if !row.At.Before(since)&&row.Event.Kind=="started"&&row.Event.PID==pid{started=true}
		if !row.At.Before(since)&&(row.Event.Kind=="termination_requested"||row.Event.Kind=="service_requested"){
			return errors.New("watchdog_runtime_action_observed")
		}
	}
	if !started{return errors.New("watchdog_started_event_missing")}
	return scanner.Err()
}

func (r *watchdogResidentNative) onThread()bool{
	id,_,_:=kernel.NewProc("GetCurrentThreadId").Call()
	return r.threadPinned&&uint32(id)==r.threadID
}
func (r *watchdogResidentNative) Close()error{
	if r.closed{return r.closeErr}
	if !r.threadPinned{
		r.closed=true
		r.closeErr=errors.Join(r.closeCollectionResources(),r.closeOwnedChains(),r.pins.Close(),watchdogNativeCleanupError())
		return r.closeErr
	}
	if !r.onThread(){return errors.New("watchdog_resident_thread_changed")}
	r.closed=true
	c,cancel:=context.WithTimeout(context.Background(),5*time.Second);defer cancel()
	r.closeErr=errors.Join(r.ReleaseHold(c),r.closeCollectionResources(),r.closeOwnedChains(),r.pins.Close(),watchdogNativeCleanupError())
	for _,scope:=range r.temporaryPins{r.closeErr=errors.Join(r.closeErr,scope.Close())}
	if len(r.retainedDescriptors)!=0{r.closeErr=errors.Join(r.closeErr,errors.New("watchdog_security_release_failed"))}
	// A failed ReleaseMutex is not retried or treated as released. The finite
	// helper must report cleanup failure and exit; its OS thread is not reused.
	if r.threadPinned&&!r.watchOwned{r.threadPinned=false;runtime.UnlockOSThread()}
	return r.closeErr
}


func (r *watchdogResidentNative) readTaskSnapshot(ctx context.Context,mode,guid,xml string,pid uint32)(watchdogTaskSnapshot,error){
	if r.taskRead!=nil{return r.taskRead(ctx,mode,guid,xml,pid)}
	return readWatchdogTask(ctx,mode,guid,xml,pid)
}
func (r *watchdogResidentNative) confirmCollection(ctx context.Context,snapshot watchdogTaskSnapshot,published bool) error {
	if ctx==nil||ctx.Err()!=nil||r.lifetime==nil||!r.collection.required||
		!r.task.View.valid()||snapshot.View!=r.task.View||r.task.Principal==""||snapshot.Principal!=r.task.Principal{return errWatchdogCollectionUnknown}
	if published&&(r.candidate==nil||!r.candidate().valid()||r.published.IsZero()){return errWatchdogCollectionUnknown}
	var err error
	if published { err=r.lifetime.ConfirmPublishedStopped(ctx,snapshot,r.candidate(),r.published) } else { err=r.lifetime.ConfirmStopped(ctx,snapshot) }
	if err!=nil||ctx.Err()!=nil{return errors.Join(errWatchdogCollectionUnknown,err,ctx.Err())}
	r.collection.confirm()
	return nil
}
func (r *watchdogResidentNative) holdQuiescentPublished(ctx context.Context,snapshot watchdogTaskSnapshot,id watchdogFileIdentity)(watchdogStopped,error){
	if ctx==nil||ctx.Err()!=nil||r.closed||r.lifetime==nil||r.published.IsZero()||!id.valid()||id!=r.candidate()||
		snapshot.View!=r.task.View||snapshot.Principal!=r.task.Principal||len(snapshot.Instances)!=0 {
		return watchdogStopped{},errWatchdogCollectionUnknown
	}
	r.collection.begin(false)
	h,err:=r.lifetime.AcquireQuiescentHold(ctx,snapshot,id,r.published)
	if h!=nil {
		s:=&watchdogLifetimeHoldState{hold:h}
		r.retainedHolds=append(r.retainedHolds,s)
		if err==nil{r.recoveryHold=s}
	}
	if err!=nil||h==nil{return watchdogStopped{},errors.Join(errWatchdogCollectionUnknown,err)}
	current,err:=r.readTaskSnapshot(ctx,"read","","",0)
	if err!=nil||current.View!=r.task.View||current.Principal!=r.task.Principal||len(current.Instances)!=0||id!=r.candidate(){
		return watchdogStopped{},errors.Join(errWatchdogCollectionUnknown,err)
	}
	if err=r.confirmCollection(ctx,current,true);err!=nil{return watchdogStopped{},err}
	return watchdogStopped{OwnedGeneration:true,AllReaped:true,HoldAcquired:true},nil
}
func (r *watchdogResidentNative) closeCollectionResources() error {
	var es []error
	es=append(es,r.collection.cleanupError())
	for _,h:=range r.retainedHolds{es=append(es,h.close())}
	if r.lifetime!=nil{es=append(es,r.lifetime.Close())}
	return errors.Join(es...)
}
