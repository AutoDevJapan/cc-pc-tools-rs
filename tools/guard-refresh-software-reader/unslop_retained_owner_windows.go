//go:build windows

package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"reflect"
	"strconv"
	"sync"
	"syscall"
)

var errUnslopRetainedInput = errors.New("retained_owner_input_invalid")
var errUnslopRetainedCapacity = errors.New("retained_owner_capacity")

// Tickets are in-process capabilities minted by the owner. The printable ID is
// only a correlation value; no JSON or ID lookup can release a resource.
type unslopRetainedTicket struct { sequence uint64 }

// A reservation pins one owner's ticket before a durable intent is attempted.
// It is retained on save failure and cannot be moved to another owner/session.
type unslopRetainedReservation struct {
	owner *unslopRetainedOwner
	ticket *unslopRetainedTicket
	source *unslopLiveSession
	lease softwareRustHeldSession
	claimed []io.Closer
	handles []syscall.Handle
	flight *askOwnedProducerFlight
	report unslopRetainedReport
}

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
	claimedHandles []syscall.Handle
	flight *askOwnedProducerFlight
	cached *askProducerFlightResult
	observed bool
	abortRequested bool
	gate chan struct{}
	durable *unslopDurableController
	saved *unslopRetainedSaveProof
}

type unslopRetainedOwner struct {
	mu sync.Mutex
	limit int
	identity string
	next uint64
	entries map[*unslopRetainedTicket]*unslopRetainedEntry
	claims map[io.Closer]*unslopRetainedTicket
	handles map[syscall.Handle]*unslopRetainedTicket
	flights map[*askOwnedProducerFlight]*unslopRetainedTicket
	done map[<-chan askProducerFlightResult]*unslopRetainedTicket
	reservations map[*unslopRetainedTicket]*unslopRetainedReservation
}

func newUnslopRetainedOwner(limit int) (*unslopRetainedOwner,error) {
	if limit<1||limit>128 {return nil,errUnslopRetainedCapacity}
	var nonce [16]byte
	if _,err:=rand.Read(nonce[:]);err!=nil {return nil,errUnslopRetainedInput}
	return &unslopRetainedOwner{limit:limit,identity:hex.EncodeToString(nonce[:]),entries:make(map[*unslopRetainedTicket]*unslopRetainedEntry),claims:make(map[io.Closer]*unslopRetainedTicket),handles:make(map[syscall.Handle]*unslopRetainedTicket),flights:make(map[*askOwnedProducerFlight]*unslopRetainedTicket),done:make(map[<-chan askProducerFlightResult]*unslopRetainedTicket),reservations:make(map[*unslopRetainedTicket]*unslopRetainedReservation)},nil
}

// Retain is called by the normal controller at an exclusive transfer boundary.
// It moves the live session and provider lease; it never acquires authority to
// read a toolchain. The worker may still run, but no other caller may use the
// old session or receive from its flight while ownership is being transferred.
func (o *unslopRetainedOwner) Retain(source **unslopLiveSession, lease *softwareRustHeldSession, extra []io.Closer, capture unslopToolchainCaptureResult) (*unslopRetainedTicket,error) {
	return o.retainBeforeMove(source,lease,extra,capture,nil)
}

func (o *unslopRetainedOwner) retainBeforeMove(source **unslopLiveSession, lease *softwareRustHeldSession, extra []io.Closer, capture unslopToolchainCaptureResult, before func(unslopRetainedReport)error) (*unslopRetainedTicket,error) {
	return o.retainReserved(source,lease,extra,capture,nil,before)
}

func (o *unslopRetainedOwner) retainReserved(source **unslopLiveSession, lease *softwareRustHeldSession, extra []io.Closer, capture unslopToolchainCaptureResult, reservation **unslopRetainedReservation, before func(unslopRetainedReport)error) (*unslopRetainedTicket,error) {
	if o==nil||o.entries==nil||o.claims==nil||o.handles==nil||o.flights==nil||o.done==nil||o.reservations==nil||len(o.identity)!=32||len(extra)>256||source==nil||*source==nil||lease==nil||!unslopRetainedPointer(*lease) {return nil,errUnslopRetainedInput}
	live:=*source
	if live.unslopNativeSession==nil||live.closed {return nil,errUnslopRetainedInput}
	owned,ok:=live.flight.(*unslopOwnedFlight)
	if !ok||owned==nil||owned.flight==nil||owned.flight.Done==nil||owned.flight.Abort==nil {return nil,errUnslopRetainedInput}
	// Only a source-bound provider can supply this lease. Matching its held
	// projection is a consistency check, not a replacement for that provider.
	o.mu.Lock()
	defer o.mu.Unlock()
	var reserved *unslopRetainedReservation
	var reservedTicket *unslopRetainedTicket
	if reservation!=nil {reserved=*reservation}
	if reserved!=nil {
		if reserved.owner!=o||reserved.source!=live||reserved.lease!=*lease||o.reservations[reserved.ticket]!=reserved {return nil,errUnslopRetainedInput}
		reservedTicket=reserved.ticket
	} else if len(o.entries)+len(o.reservations)>=o.limit||o.next==^uint64(0) {return nil,errUnslopRetainedCapacity}
	for _,prior:=range o.reservations {
		if prior!=reserved&&(prior.source==live||prior.lease==*lease){return nil,errUnslopRetainedInput}
	}
	if o.flights[owned.flight]!=nil&&o.flights[owned.flight]!=reservedTicket||o.done[owned.flight.Done]!=nil&&o.done[owned.flight.Done]!=reservedTicket {return nil,errUnslopRetainedInput}
	extras,claims,handles,valid:=o.unslopRetainedUniqueInputs(live,*lease,extra,reservedTicket)
	if !valid {return nil,errUnslopRetainedInput}
	for _,object:=range claims {if o.claims[object]!=nil&&o.claims[object]!=reservedTicket {return nil,errUnslopRetainedInput}}
	for _,h:=range handles {if o.handles[h]!=nil&&o.handles[h]!=reservedTicket {return nil,errUnslopRetainedInput}}
	if reserved!=nil {
		if owned.flight!=reserved.flight||len(claims)!=len(reserved.claimed)||len(handles)!=len(reserved.handles){return nil,errUnslopRetainedInput}
		for _,object:=range claims {if o.claims[object]!=reservedTicket{return nil,errUnslopRetainedInput}}
		for _,h:=range handles {if o.handles[h]!=reservedTicket{return nil,errUnslopRetainedInput}}
	}
	if !rustToolchainInputsMatch((*lease).Held()) {return nil,errUnslopRetainedInput}
	next:=o.next+1
	ticket:=&unslopRetainedTicket{sequence:next}
	if reserved!=nil {ticket=reserved.ticket;next=o.next} else if reservation!=nil {
		reserved=&unslopRetainedReservation{owner:o,ticket:ticket,source:live,lease:*lease,claimed:claims,handles:handles,flight:owned.flight}
		o.reservations[ticket]=reserved
		for _,object:=range claims {o.claims[object]=ticket}
		for _,h:=range handles {o.handles[h]=ticket}
		o.flights[owned.flight]=ticket;o.done[owned.flight.Done]=ticket
		o.next=next
		*reservation=reserved
	}
	base:=*live.unslopNativeSession
	moved:=*live
	moved.unslopNativeSession=&base
	movedOwned:=*owned
	moved.flight=&movedOwned
	capture.Qualified=false
	capture.Receipt=nil
	entry:=&unslopRetainedEntry{ticket:ticket,live:&moved,lease:*lease,extra:extras,claimed:claims,claimedHandles:handles,flight:owned.flight,cached:owned.result,gate:make(chan struct{},1),report:unslopRetainedReport{OwnerID:"unslop-retained-"+o.identity+"-"+strconv.FormatUint(ticket.sequence,10),State:"retained",Collection:"pending",Recheck:"not_attempted",Reason:"retained_owner_collection_pending",Capture:capture}}
	if reserved!=nil {reserved.report=unslopRetainedCopyReport(entry.report)}
	if before!=nil {if err:=before(unslopRetainedCopyReport(entry.report));err!=nil{return nil,err}}
	if ownedLease,ok:=entry.lease.(*softwareRustOwnedSession);ok {
		movedLease,err:=ownedLease.move();if err!=nil{return nil,errUnslopRetainedInput}
		movedLease.owner=o;movedLease.ticket=ticket
		entry.lease=movedLease
	}
	o.next=next
	entry.gate<-struct{}{}
	// Move all internal ownership before returning the capability. Old live
	// aliases become closed tombstones and cannot consume Done or inputs.
	*live.unslopNativeSession=unslopNativeSession{closed:true}
	*owned=unslopOwnedFlight{}
	*live=unslopLiveSession{unslopNativeSession:&unslopNativeSession{closed:true}}
	*source=nil
	*lease=nil
	o.entries[ticket]=entry
	delete(o.reservations,ticket)
	if reservation!=nil {*reservation=nil}
	o.flights[entry.flight]=ticket
	o.done[entry.flight.Done]=ticket
	for _,object:=range claims {o.claims[object]=ticket}
	for _,h:=range handles {o.handles[h]=ticket}
	return ticket,nil
}

func unslopRetainedPointer(value any) bool {
	if value==nil {return false}
	v:=reflect.ValueOf(value)
	return v.Kind()==reflect.Pointer&&!v.IsNil()
}

// The controller passes concrete owners, not arbitrary Close callbacks. Their
// known file/handle leaves must be disjoint across the whole ownership graph.
func (o *unslopRetainedOwner) unslopRetainedUniqueInputs(live *unslopLiveSession, lease softwareRustHeldSession, inputs []io.Closer,reservation *unslopRetainedTicket) ([]io.Closer, []io.Closer, []syscall.Handle, bool) {
	seen := make(map[io.Closer]bool)
	handles := make(map[syscall.Handle]bool)
	add := func(c io.Closer) bool {
		if !unslopRetainedPointer(c) || seen[c] || o.claims[c] != nil&&o.claims[c]!=reservation { return false }
		seen[c] = true
		return true
	}
	addHandle := func(h syscall.Handle) bool {
		if h == 0 || h == syscall.InvalidHandle || handles[h] { return false }
		handles[h] = true
		return true
	}
	addHandles := func(values []syscall.Handle) bool {
		for _, h := range values { if !addHandle(h) { return false } }
		return true
	}
	addFile := func(f *os.File) bool {
		return add(f) && addHandle(syscall.Handle(f.Fd()))
	}
	addSnapshot := func(s *softwareBuildSnapshot) bool {
		if !add(s) { return false }
		for _, f := range s.files { if !addFile(f) { return false } }
		if !addHandles(s.dirs) { return false }
		for _, a := range s.sealed { if !addHandle(a.handle) { return false } }
		for _, a := range s.handoffACLs { if !addHandle(a.handle) { return false } }
		return true
	}
	addNative := func(c *nativeUpdateObject) bool {
		if !add(c) { return false }
		if c.f != nil && !addFile(c.f) { return false }
		return addHandles(c.parents)
	}
	addObject := func(c updateObject) bool {
		if native, ok := c.(*nativeUpdateObject); ok { return addNative(native) }
		// Other implementations are the existing internal updateObject seams.
		// They are not accepted as caller-supplied extra owners below.
		return add(c)
	}
	if !add(live) || !add(live.unslopNativeSession) || !add(lease) { return nil, nil, nil, false }
	actualLease:=lease
	if owned,ok:=lease.(*softwareRustOwnedSession);ok {
		actual,valid:=owned.actual()
		if !valid||!add(owned.capability)||!add(actual) {return nil,nil,nil,false}
		actualLease=actual
	}
	if native, ok := actualLease.(*softwareRustNativeSession); ok {
		for _, node := range native.nodes { if !addFile(node.f) { return nil, nil, nil, false } }
	}
	if live.evidence != nil && !addObject(live.evidence) { return nil, nil, nil, false }
	if live.manifest != nil && !addObject(live.manifest) { return nil, nil, nil, false }
	for _, c := range live.tools { if !addObject(c) { return nil, nil, nil, false } }
	for _, s := range []*softwareBuildSnapshot{live.source, live.configuration} {
		if s != nil && !addSnapshot(s) { return nil, nil, nil, false }
	}
	if !addHandles(live.parents) { return nil, nil, nil, false }
	var unique []io.Closer
	for _, c := range inputs {
		if !unslopRetainedPointer(c) { return nil, nil, nil, false }
		// Repeated identical owners, including an already-owned file leaf,
		// are not closed again. Distinct wrappers must have disjoint leaves.
		switch c.(type) {
		case *os.File, *nativeUpdateObject, *softwareBuildSnapshot:
		default: return nil, nil, nil, false
		}
		if seen[c] { continue }
		valid := false
		switch value := c.(type) {
		case *os.File: valid = addFile(value)
		case *nativeUpdateObject: valid = addNative(value)
		case *softwareBuildSnapshot: valid = addSnapshot(value)
		}
		if !valid { return nil, nil, nil, false }
		unique = append(unique, c)
	}
	claims := make([]io.Closer, 0, len(seen))
	for object := range seen { claims = append(claims, object) }
	claimedHandles := make([]syscall.Handle, 0, len(handles))
	for h := range handles { claimedHandles = append(claimedHandles, h) }
	return unique, claims, claimedHandles, true
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
	if entry==nil||entry.report.State!="released"||!entry.saved.matches(o,ticket,entry) {return false}
	for _,object:=range entry.claimed {if o.claims[object]==ticket {delete(o.claims,object)}}
	for _,h:=range entry.claimedHandles {if o.handles[h]==ticket {delete(o.handles,h)}}
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
	if failures>0||checkErr!=nil {
		entry.report.State="release_failed"
		entry.report.Reason="retained_owner_release_failed"
	} else {
		// Closed Windows handle values can be reused before this result is
		// persisted and forgotten. Retain the result and object references,
		// but stop reserving numeric handles whose release is now proven.
		for _,h:=range entry.claimedHandles {if o.handles[h]==ticket {delete(o.handles,h)}}
		entry.claimedHandles=nil
		entry.report.State="released"
		entry.report.Reason="retained_owner_released"
		entry.live=nil;entry.lease=nil;entry.extra=nil;entry.flight=nil
	}
	r:=unslopRetainedCopyReport(entry.report)
	o.mu.Unlock()
	return r
}

// The result is obtained from this transferred flight, never from caller JSON.
func unslopRetainedCollectionVerified(available bool, completion askProducerFlightResult) bool {
	return available&&completion.Child.Started&&completion.Child.Assigned&&completion.Child.WaitVerified&&completion.Child.Reaped&&completion.NativeProcess!=nil&&completion.NativeProcess.PID!=0&&completion.NativeProcess.Creation!=0
}
