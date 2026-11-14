//go:build windows

package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"reflect"
	"strconv"
	"sync"
)

var errUnslopRetainedInput = errors.New("retained_owner_input_invalid")
var errUnslopRetainedCapacity = errors.New("retained_owner_capacity")

// Tickets are in-process capabilities minted by the owner. The printable ID is
// only a correlation value; no JSON or ID lookup can release a resource.
type unslopRetainedTicket struct { sequence uint64 }

type unslopRetainedReport struct {
	OwnerID string `json:"owner_id"`
	State string `json:"state"`
	Collection string `json:"collection"`
	Reason string `json:"reason"`
	WaitReason string `json:"wait_reason,omitempty"`
	AbortRequested bool `json:"abort_requested"`
	WorkerTimedOut bool `json:"worker_timed_out"`
	CloseAttempts int `json:"close_attempts"`
	CloseFailures int `json:"close_failures"`
	Recheck string `json:"recheck"`
	ProtocolComplete bool `json:"protocol_complete"`
	WorkerExit *int `json:"worker_exit,omitempty"`
	NativeProcess *askNativeProcess `json:"native_process,omitempty"`
	Capture unslopToolchainCaptureResult `json:"capture"`
}

type unslopRetainedEntry struct {
	ticket *unslopRetainedTicket
	report unslopRetainedReport
	live *unslopLiveSession
	lease softwareRustHeldSession
	extra []io.Closer
	claimed []io.Closer
	flight *askOwnedProducerFlight
	cached *askProducerFlightResult
	observed bool
	abortRequested bool
	gate chan struct{}
}

type unslopRetainedOwner struct {
	mu sync.Mutex
	limit int
	identity string
	next uint64
	entries map[*unslopRetainedTicket]*unslopRetainedEntry
	claims map[io.Closer]*unslopRetainedTicket
	flights map[*askOwnedProducerFlight]*unslopRetainedTicket
	done map[<-chan askProducerFlightResult]*unslopRetainedTicket
}

func newUnslopRetainedOwner(limit int) (*unslopRetainedOwner,error) {
	if limit<1||limit>128 {return nil,errUnslopRetainedCapacity}
	var nonce [16]byte
	if _,err:=rand.Read(nonce[:]);err!=nil {return nil,errUnslopRetainedInput}
	return &unslopRetainedOwner{limit:limit,identity:hex.EncodeToString(nonce[:]),entries:make(map[*unslopRetainedTicket]*unslopRetainedEntry),claims:make(map[io.Closer]*unslopRetainedTicket),flights:make(map[*askOwnedProducerFlight]*unslopRetainedTicket),done:make(map[<-chan askProducerFlightResult]*unslopRetainedTicket)},nil
}

// Retain is called by the normal controller at an exclusive transfer boundary.
// It moves the live session and provider lease; it never acquires authority to
// read a toolchain. The worker may still run, but no other caller may use the
// old session or receive from its flight while ownership is being transferred.
func (o *unslopRetainedOwner) Retain(source **unslopLiveSession, lease *softwareRustHeldSession, extra []io.Closer, capture unslopToolchainCaptureResult) (*unslopRetainedTicket,error) {
	if o==nil||o.entries==nil||o.claims==nil||o.flights==nil||o.done==nil||len(o.identity)!=32||len(extra)>256||source==nil||*source==nil||lease==nil||!unslopRetainedPointer(*lease) {return nil,errUnslopRetainedInput}
	live:=*source
	if live.unslopNativeSession==nil||live.closed {return nil,errUnslopRetainedInput}
	owned,ok:=live.flight.(*unslopOwnedFlight)
	if !ok||owned==nil||owned.flight==nil||owned.flight.Done==nil||owned.flight.Abort==nil {return nil,errUnslopRetainedInput}
	// Only a source-bound provider can supply this lease. Matching its held
	// projection is a consistency check, not a replacement for that provider.
	extras,claims,valid:=unslopRetainedUniqueInputs(live,*lease,extra)
	if !valid {return nil,errUnslopRetainedInput}
	o.mu.Lock()
	defer o.mu.Unlock()
	if len(o.entries)>=o.limit||o.next==^uint64(0) {return nil,errUnslopRetainedCapacity}
	if o.flights[owned.flight]!=nil||o.done[owned.flight.Done]!=nil {return nil,errUnslopRetainedInput}
	for _,object:=range claims {if o.claims[object]!=nil {return nil,errUnslopRetainedInput}}
	if !rustToolchainInputsMatch((*lease).Held()) {return nil,errUnslopRetainedInput}
	o.next++
	ticket:=&unslopRetainedTicket{sequence:o.next}
	base:=*live.unslopNativeSession
	moved:=*live
	moved.unslopNativeSession=&base
	movedOwned:=*owned
	moved.flight=&movedOwned
	capture.Qualified=false
	capture.Receipt=nil
	entry:=&unslopRetainedEntry{ticket:ticket,live:&moved,lease:*lease,extra:extras,claimed:claims,flight:owned.flight,cached:owned.result,gate:make(chan struct{},1),report:unslopRetainedReport{OwnerID:"unslop-retained-"+o.identity+"-"+strconv.FormatUint(ticket.sequence,10),State:"retained",Collection:"pending",Recheck:"not_attempted",Reason:"retained_owner_collection_pending",Capture:capture}}
	entry.gate<-struct{}{}
	// Move all internal ownership before returning the capability. Old live
	// aliases become closed tombstones and cannot consume Done or inputs.
	*live.unslopNativeSession=unslopNativeSession{closed:true}
	*owned=unslopOwnedFlight{}
	*live=unslopLiveSession{unslopNativeSession:&unslopNativeSession{closed:true}}
	*source=nil
	*lease=nil
	o.entries[ticket]=entry
	o.flights[entry.flight]=ticket
	o.done[entry.flight.Done]=ticket
	for _,object:=range claims {o.claims[object]=ticket}
	return ticket,nil
}

func unslopRetainedPointer(value any) bool {
	if value==nil {return false}
	v:=reflect.ValueOf(value)
	return v.Kind()==reflect.Pointer&&!v.IsNil()
}

func unslopRetainedUniqueInputs(live *unslopLiveSession, lease softwareRustHeldSession, inputs []io.Closer) ([]io.Closer,[]io.Closer,bool) {
	seen:=make(map[io.Closer]bool)
	add:=func(c io.Closer) bool {
		if !unslopRetainedPointer(c)||seen[c] {return false}
		seen[c]=true;return true
	}
	if !add(live)||!add(live.unslopNativeSession)||!add(lease) {return nil,nil,false}
	addObject:=func(c updateObject) bool {
		if !add(c) {return false}
		if native,ok:=c.(*nativeUpdateObject);ok&&native.f!=nil {return add(native.f)}
		return true
	}
	if live.evidence!=nil&&!addObject(live.evidence) {return nil,nil,false}
	if live.manifest!=nil&&!addObject(live.manifest) {return nil,nil,false}
	for _,c:=range live.tools {if !addObject(c) {return nil,nil,false}}
	for _,s:=range []*softwareBuildSnapshot{live.source,live.configuration} {
		if s==nil {continue};if !add(s) {return nil,nil,false}
		for _,f:=range s.files {if !add(f) {return nil,nil,false}}
	}
	var unique []io.Closer
	for _,c:=range inputs {
		if !unslopRetainedPointer(c) {return nil,nil,false}
		if seen[c] {continue};seen[c]=true;unique=append(unique,c)
	}
	claims:=make([]io.Closer,0,len(seen))
	for object:=range seen {claims=append(claims,object)}
	return unique,claims,true
}

func (o *unslopRetainedOwner) Snapshot(ticket *unslopRetainedTicket) unslopRetainedReport {
	if o==nil {return unslopRetainedReport{State:"unknown",Reason:"retained_owner_unknown"}}
	o.mu.Lock();defer o.mu.Unlock()
	entry:=o.entries[ticket]
	if entry==nil {return unslopRetainedReport{State:"unknown",Reason:"retained_owner_unknown"}}
	return unslopRetainedCopyReport(entry.report)
}

func unslopRetainedCopyReport(r unslopRetainedReport) unslopRetainedReport {
	if r.WorkerExit!=nil {value:=*r.WorkerExit;r.WorkerExit=&value}
	if r.NativeProcess!=nil {value:=*r.NativeProcess;r.NativeProcess=&value}
	r.Capture.Qualified=false
	r.Capture.Receipt=nil
	return r
}

func (o *unslopRetainedOwner) Tickets() []*unslopRetainedTicket {
	if o==nil {return nil};o.mu.Lock();defer o.mu.Unlock()
	out:=make([]*unslopRetainedTicket,0,len(o.entries))
	for ticket:=range o.entries {out=append(out,ticket)}
	return out
}

// Forget only follows a recorded successful release. Failed or unknown owners
// retain their references and fixed result for normal reconciliation.
func (o *unslopRetainedOwner) Forget(ticket *unslopRetainedTicket) bool {
	if o==nil {return false};o.mu.Lock();defer o.mu.Unlock()
	entry:=o.entries[ticket]
	if entry==nil||entry.report.State!="released" {return false}
	for _,object:=range entry.claimed {delete(o.claims,object)}
	for flight,owner:=range o.flights {if owner==ticket {delete(o.flights,flight)}}
	for done,owner:=range o.done {if owner==ticket {delete(o.done,done)}}
	delete(o.entries,ticket)
	return true
}

func (o *unslopRetainedOwner) Close(ctx context.Context,ticket *unslopRetainedTicket) unslopRetainedReport {
	return o.Drain(ctx,ticket)
}

func (o *unslopRetainedOwner) requestAbort(entry *unslopRetainedEntry, ctx context.Context) {
	o.mu.Lock()
	var abort context.CancelFunc
	if entry.report.State=="retained" {
		entry.report.WaitReason="retained_owner_wait_cancelled"
		if ctx.Err()==context.DeadlineExceeded {entry.report.WaitReason="retained_owner_wait_deadline"}
		if !entry.abortRequested {entry.abortRequested=true;entry.report.AbortRequested=true;abort=entry.flight.Abort}
	}
	o.mu.Unlock()
	if abort!=nil {abort()}
}

// Drain creates no goroutine or timer. One caller owns the exact Done receive;
// cancellation leaves the entry retained so the supervisor can call again.
func (o *unslopRetainedOwner) Drain(ctx context.Context,ticket *unslopRetainedTicket) unslopRetainedReport {
	if o==nil||ctx==nil {return unslopRetainedReport{State:"unknown",Reason:"retained_owner_unknown"}}
	o.mu.Lock()
	entry:=o.entries[ticket]
	if entry==nil {o.mu.Unlock();return unslopRetainedReport{State:"unknown",Reason:"retained_owner_unknown"}}
	if entry.report.State!="retained" {r:=unslopRetainedCopyReport(entry.report);o.mu.Unlock();return r}
	if entry.observed {
		o.mu.Unlock()
		if ctx.Err()!=nil {o.requestAbort(entry,ctx)}
		return o.Snapshot(ticket)
	}
	o.mu.Unlock()
	if ctx.Err()!=nil {o.requestAbort(entry,ctx);return o.Snapshot(ticket)}
	select {case <-entry.gate:case <-ctx.Done():o.requestAbort(entry,ctx);return o.Snapshot(ticket)}
	defer func(){entry.gate<-struct{}{}}()
	o.mu.Lock()
	if entry.report.State!="retained" {r:=unslopRetainedCopyReport(entry.report);o.mu.Unlock();return r}
	if entry.observed {
		o.mu.Unlock()
		if ctx.Err()!=nil {o.requestAbort(entry,ctx)}
		return o.Snapshot(ticket)
	}
	o.mu.Unlock()
	if ctx.Err()!=nil {o.requestAbort(entry,ctx);return o.Snapshot(ticket)}
	var completion askProducerFlightResult
	available:=true
	if entry.cached!=nil {completion=*entry.cached;entry.cached=nil} else {
		select {
		case value,ok:=<-entry.flight.Done:completion=value;available=ok
		case <-ctx.Done():o.requestAbort(entry,ctx);return o.Snapshot(ticket)
		}
	}
	defer clear(completion.Child.Raw)
	if completion.Child.Capture!=nil {defer clear(completion.Child.Capture.Stdout)}
	o.mu.Lock()
	entry.observed=true
	entry.report.Collection="unverified"
	entry.report.Reason="retained_owner_collection_unverified"
	entry.report.ProtocolComplete=completion.ProtocolComplete
	entry.report.WorkerTimedOut=completion.Child.TimedOut
	if available&&completion.Child.Started&&completion.Child.WaitVerified {
		exit:=completion.Child.Exit;entry.report.WorkerExit=&exit
	}
	if completion.NativeProcess!=nil {id:=*completion.NativeProcess;entry.report.NativeProcess=&id}
	collected:=unslopRetainedCollectionVerified(available,completion)
	if !collected {r:=unslopRetainedCopyReport(entry.report);o.mu.Unlock();return r}
	entry.report.State="release_in_progress"
	entry.report.Collection="verified"
	entry.report.Reason="retained_owner_release_in_progress"
	entry.report.CloseAttempts=1
	o.mu.Unlock()
	// Native collection is already proven. Caller cancellation cannot cause a
	// second close or abandon release halfway through. Recheck uses the
	// caller-provided cleanup context; cancellation is recorded as a failure
	// but cannot turn an already proven collection into a second close.
	held:=entry.lease.Held()
	checkErr:=ctx.Err()
	if checkErr==nil {
		checkErr=errUnslopRetainedInput
		if rustToolchainInputsMatch(held) {
			checkErr=held.Recheck(ctx)
			if ctx.Err()!=nil {checkErr=ctx.Err()} else if !rustToolchainInputsMatch(held) {checkErr=errUnslopRetainedInput}
		}
	}
	failures:=0
	entry.live.completed=true
	if entry.live.Close()!=nil {failures++}
	for _,input:=range entry.extra {if input.Close()!=nil {failures++}}
	if entry.lease.Close()!=nil {failures++}
	o.mu.Lock()
	entry.report.CloseFailures=failures
	entry.report.Recheck="verified"
	if checkErr!=nil {entry.report.Recheck="failed"}
	if failures>0||checkErr!=nil {entry.report.State="release_failed";entry.report.Reason="retained_owner_release_failed"} else {entry.report.State="released";entry.report.Reason="retained_owner_released";entry.live=nil;entry.lease=nil;entry.extra=nil;entry.flight=nil}
	r:=unslopRetainedCopyReport(entry.report)
	o.mu.Unlock()
	return r
}

// The result is obtained from this transferred flight, never from caller JSON.
func unslopRetainedCollectionVerified(available bool, completion askProducerFlightResult) bool {
	return available&&completion.Child.Started&&completion.Child.Assigned&&completion.Child.WaitVerified&&completion.Child.Reaped&&completion.NativeProcess!=nil&&completion.NativeProcess.PID!=0&&completion.NativeProcess.Creation!=0
}
