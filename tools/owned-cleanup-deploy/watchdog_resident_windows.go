//go:build windows

package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
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

func watchdogOwnedChain(instance watchdogTaskInstance,principal string)(chain []*watchdogProcess,err error){
	rows,err:=watchdogProcessEntries()
	if err!=nil{return nil,err}
	return watchdogChainFromSnapshot(instance,principal,rows,func(pid,parent uint32)(*watchdogProcess,string,bool,error){
		process,e:=openWatchdogProcess(pid,parent)
		if e!=nil{return process,"",false,e}
		sid,e:=process.sid()
		return process,sid,process.live(),e
	})
}

// Both normal launcher branches are closed shapes: the already-limited action
// starts the watchdog directly; privilege reduction inserts one launcher child.
// The callback only acquires observations; this helper issues no lifetime proof.
func watchdogChainFromSnapshot(instance watchdogTaskInstance,principal string,rows []watchdogProcessEntry,
	open func(uint32,uint32)(*watchdogProcess,string,bool,error))(chain []*watchdogProcess,err error){
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
func (r *watchdogResidentNative) sample(ctx context.Context)(watchdogSample,error){
	path:="D:/CC/Tools/ocx-health-watchdog/status.json"
	if err:=r.classify(ctx,path);err!=nil{return watchdogSample{},err}
	scope:=newWatchdogPinScope()
	r.temporaryPins=append(r.temporaryPins,scope)
	raw,err:=watchdogReadMutable(scope,path,8192)
	err=errors.Join(err,scope.Close())
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

func (r *watchdogResidentNative) Observe(ctx context.Context,id watchdogFileIdentity)(watchdogObservation,error){
	result:=watchdogObservation{PreviousPID:r.previousPID,Published:r.published}
	if r.closed||!r.onThread()||id!=r.candidate()||r.published.IsZero(){
		return result,errors.New("watchdog_observation_unbound")
	}
	c,cancel:=context.WithTimeout(ctx,150*time.Second);defer cancel()
	var chain []*watchdogProcess
	for{
		if err:=c.Err();err!=nil{return result,err}
		t,err:=r.readTaskSnapshot(c,"read","", "",0)
		if err!=nil{return result,err}
		if t.View!=r.task.View||t.Principal!=r.task.Principal{return result,errors.New("watchdog_task_definition_changed")}
		if len(t.Instances)>1{return result,errors.New("watchdog_instance_ambiguous")}
		if len(t.Instances)==1{
			chain,err=watchdogOwnedChain(t.Instances[0],t.Principal)
			if chain!=nil{r.chain=append(r.chain,chain...)}
			if err!=nil{return result,err}
			break
		}
		select{case<-c.Done():return result,c.Err();case<-time.After(250*time.Millisecond):}
	}
	current:=chain[len(chain)-1]
	result.PID=current.pid
	// FILETIME is 100ns since 1601, and birth is observed on the same held
	// process handle whose image path was queried, not a response-supplied PID.
	if current.created<116444736000000000{return result,errors.New("watchdog_birth_epoch_invalid")}
	result.Started=time.Unix(0,int64(current.created-116444736000000000)*100).UTC()
	if result.PID==r.previousPID||result.Started.Before(r.published){return result,errors.New("watchdog_new_process_generation_invalid")}
	if err:=r.classify(c,watchdogTarget);err!=nil{return result,err}
	image,err:=r.pins.open(watchdogTarget,syscall.GENERIC_READ|0x20000,7,3)
	if err!=nil{return result,err}
	observed,err:=watchdogIdentityHeld(image,&r.retainedDescriptors)
	if err!=nil||observed!=id||r.pins.verifyName(image,watchdogTarget)!=nil{
		return result,errors.New("watchdog_new_image_generation_changed")
	}
	// The candidate object has remained held and unmodifiable since before
	// this process was created. Same path and retained generation are required;
	// a mere health endpoint answer is never this proof.
	result.OwnImage=observed
	result.OwnInstanceVerified=true
	for len(result.Samples)<6{
		if err:=c.Err();err!=nil{return result,err}
		if current.recheck()!=nil||!current.live(){return result,errors.New("watchdog_observed_process_changed")}
		s,err:=r.sample(c)
		if err!=nil{return result,err}
		if s.WatchdogPID!=current.pid||s.At.Before(result.Started)||time.Since(s.At)>25*time.Second||s.At.After(time.Now().Add(2*time.Second)){
			return result,errors.New("watchdog_observation_stale")}
		if len(result.Samples)==0||s.At.After(result.Samples[len(result.Samples)-1].At){result.Samples=append(result.Samples,s)}
		if len(result.Samples)<6{select{case<-c.Done():return result,c.Err();case<-time.After(time.Second):}}
	}
	if err:=r.eventsSince(c,r.published,current.pid);err!=nil{return result,err}
	if !result.valid(id){return result,errors.New("watchdog_health_only_acceptance_failed")}
	return result,nil
}

func (r *watchdogResidentNative) eventsSince(ctx context.Context,since time.Time,pid uint32)error{
	path:="D:/CC/Tools/ocx-health-watchdog/events.jsonl"
	if err:=r.classify(ctx,path);err!=nil{return err}
	scope:=newWatchdogPinScope()
	r.temporaryPins=append(r.temporaryPins,scope)
	raw,err:=watchdogReadMutable(scope,path,6<<20)
	err=errors.Join(err,scope.Close())
	if err!=nil{return err}
	scanner:=bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte,65536),65536)
	started:=false
	for scanner.Scan(){
		var row struct{At time.Time `json:"at"`;Event struct{Kind string `json:"kind"`;PID uint32 `json:"watchdog_pid"`} `json:"event"`}
		if json.Unmarshal(scanner.Bytes(),&row)!=nil{return errors.Join(errors.New("watchdog_event_schema"),scope.Close())}
		if row.At.IsZero()||strings.TrimSpace(row.Event.Kind)==""{return errors.Join(errors.New("watchdog_event_schema"),scope.Close())}
		if !row.At.Before(since)&&row.Event.Kind=="started"&&row.Event.PID==pid{started=true}
		if !row.At.Before(since)&&(row.Event.Kind=="termination_requested"||row.Event.Kind=="service_requested"){
			return errors.Join(errors.New("watchdog_runtime_action_observed"),scope.Close())
		}
	}
	if !started{return errors.Join(errors.New("watchdog_started_event_missing"),scope.Close())}
	return errors.Join(scanner.Err(),scope.Close())
}

func (r *watchdogResidentNative) onThread()bool{
	id,_,_:=kernel.NewProc("GetCurrentThreadId").Call()
	return r.threadPinned&&uint32(id)==r.threadID
}
func (r *watchdogResidentNative) Close()error{
	if r.closed{return r.closeErr}
	if !r.threadPinned{
		r.closed=true
		r.closeErr=errors.Join(r.closeCollectionResources(),closeWatchdogChain(r.chain),r.pins.Close(),watchdogNativeCleanupError())
		return r.closeErr
	}
	if !r.onThread(){return errors.New("watchdog_resident_thread_changed")}
	r.closed=true
	c,cancel:=context.WithTimeout(context.Background(),5*time.Second);defer cancel()
	r.closeErr=errors.Join(r.ReleaseHold(c),r.closeCollectionResources(),closeWatchdogChain(r.chain),r.pins.Close(),watchdogNativeCleanupError())
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
