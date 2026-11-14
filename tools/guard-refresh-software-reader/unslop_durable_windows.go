//go:build windows

package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"unsafe"
)

const unslopDurableDomain = "godd-unslop-generation-outcome-v1"
const unslopDurableLimit = 64 << 10
const unslopDurableEvents = 32

var errUnslopDurable = errors.New("unslop_durable_record_unavailable")
var errUnslopDurableConflict = errors.New("unslop_durable_record_mismatch")

// A held, already-qualified directory is an internal storage capability, not
// authority to open a toolchain or construct a runtime from a caller path.
// The normal constructor does not supply one until its own admission exists.
type unslopDurableStore struct {
	mu sync.Mutex
	root *os.File
	parents []syscall.Handle
	path string
	id softwareIdentity
	write func(*os.File, []byte) (int, error)
	hook func(string) error
	readHook func() error
}

func newUnslopDurableStore(root *os.File) (*unslopDurableStore, error) {
	if root == nil { return nil, errUnslopDurable }
	info, err := root.Stat()
	if err != nil || !info.IsDir() { return nil, errUnslopDurable }
	h := syscall.Handle(root.Fd())
	var native syscall.ByHandleFileInformation
	name, ok := handlePath(h)
	if !ok || !sameCleanPath(name, root.Name()) || syscall.GetFileInformationByHandle(h, &native) != nil || native.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0 { return nil, errUnslopDurable }
	id := softwareIdentity{fmtUint(uint64(native.VolumeSerialNumber)), fmtUint(uint64(native.FileIndexHigh)<<32|uint64(native.FileIndexLow)), fmtUint(uint64(native.CreationTime.HighDateTime)<<32|uint64(native.CreationTime.LowDateTime))}
	var paths []string
	for parent:=filepath.Dir(name);;parent=filepath.Dir(parent) {paths=append(paths,parent);if filepath.Dir(parent)==parent{break}}
	var parents []syscall.Handle
	for n:=len(paths)-1;n>=0;n-- {pin,ok:=openPlain(paths[n],true);if !ok {for _,prior:=range parents{syscall.CloseHandle(prior)};return nil,errUnslopDurable};parents=append(parents,pin)}
	pin,ok:=openPlain(name,true)
	if !ok {for _,prior:=range parents{syscall.CloseHandle(prior)};return nil,errUnslopDurable}
	owned:=os.NewFile(uintptr(pin),name)
	s:=&unslopDurableStore{root:owned,parents:parents,path:filepath.Clean(name),id:id,write:func(f *os.File,b []byte)(int,error){return f.Write(b)}}
	if !s.current(){_ = s.Close();return nil,errUnslopDurable}
	return s,nil
}

func (s *unslopDurableStore) Close() error {
	if s==nil{return nil};s.mu.Lock();defer s.mu.Unlock()
	var errs []error
	if s.root!=nil {errs=append(errs,s.root.Close());s.root=nil}
	for n:=len(s.parents)-1;n>=0;n-- {errs=append(errs,syscall.CloseHandle(s.parents[n]))}
	s.parents=nil
	return errors.Join(errs...)
}

func (s *unslopDurableStore) current() bool {
	if s == nil || s.root == nil || s.write == nil { return false }
	h := syscall.Handle(s.root.Fd())
	var native syscall.ByHandleFileInformation
	name, ok := handlePath(h)
	if !ok || !sameCleanPath(name,s.path) || syscall.GetFileInformationByHandle(h,&native)!=nil || native.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT!=0 || native.FileAttributes&syscall.FILE_ATTRIBUTE_DIRECTORY==0 { return false }
	id := softwareIdentity{fmtUint(uint64(native.VolumeSerialNumber)),fmtUint(uint64(native.FileIndexHigh)<<32|uint64(native.FileIndexLow)),fmtUint(uint64(native.CreationTime.HighDateTime)<<32|uint64(native.CreationTime.LowDateTime))}
	return id == s.id
}

func (s *unslopDurableStore) reserve(generation string) error {
	if !s.current()||!unslopHex(generation,32){return errUnslopDurable}
	s.mu.Lock();defer s.mu.Unlock()
	for n:=1;n<=unslopDurableEvents;n++ {for _,suffix:=range []string{"",".pending"} {if _,err:=os.Lstat(filepath.Join(s.path,unslopDurableName(generation,n)+suffix));err==nil{return errUnslopDurableConflict}else if !errors.Is(err,os.ErrNotExist){return errUnslopDurable}}}
	p,err:=syscall.UTF16PtrFromString(filepath.Join(s.path,generation+".lock"));if err!=nil{return errUnslopDurable}
	h,err:=syscall.CreateFile(p,syscall.GENERIC_READ|syscall.GENERIC_WRITE,syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE,nil,syscall.CREATE_NEW,0x00200000,0)
	if err!=nil{return errUnslopDurableConflict}
	if syscall.FlushFileBuffers(h)!=nil {syscall.CloseHandle(h);return errUnslopDurable}
	if syscall.CloseHandle(h)!=nil{return errUnslopDurable}
	return nil
}

// Byte locks are released by Windows on process termination. A stale lock file
// is not a lock and does not justify deleting another writer's state.
func (s *unslopDurableStore) lock(generation string) (func(), error) {
	if !s.current() || !unslopHex(generation,32) { return nil,errUnslopDurable }
	path := filepath.Join(s.path,generation+".lock")
	p,err := syscall.UTF16PtrFromString(path)
	if err!=nil {return nil,errUnslopDurable}
	h,err := syscall.CreateFile(p,syscall.GENERIC_READ|syscall.GENERIC_WRITE,syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE,nil,syscall.OPEN_ALWAYS,0x00200000,0)
	if err!=nil {return nil,errUnslopDurable}
	var info syscall.ByHandleFileInformation
	actual,ok:=handlePath(h)
	if !ok||!sameCleanPath(actual,path)||syscall.GetFileInformationByHandle(h,&info)!=nil||info.FileAttributes&(syscall.FILE_ATTRIBUTE_REPARSE_POINT|syscall.FILE_ATTRIBUTE_DIRECTORY)!=0||info.NumberOfLinks!=1 {syscall.CloseHandle(h);return nil,errUnslopDurable}
	overlap:=&syscall.Overlapped{}
	kernel:=syscall.NewLazyDLL("kernel32.dll")
	locked,_,_:=kernel.NewProc("LockFileEx").Call(uintptr(h),3,0,1,0,uintptr(unsafe.Pointer(overlap)))
	if locked==0 {syscall.CloseHandle(h);return nil,errUnslopDurable}
	return func(){kernel.NewProc("UnlockFileEx").Call(uintptr(h),0,1,0,uintptr(unsafe.Pointer(overlap)));syscall.CloseHandle(h)},nil
}

func (s *unslopDurableStore) read(path string) ([]byte,error) {
	if !s.current() || !sameParentPath(path,filepath.Join(s.path,"record")) {return nil,errUnslopDurable}
	if s.readHook!=nil&&s.readHook()!=nil {return nil,errUnslopDurable}
	h,ok:=openPlain(path,false)
	if !ok {
		if _,err:=os.Lstat(path);errors.Is(err,os.ErrNotExist) {return nil,os.ErrNotExist}
		return nil,errUnslopDurable
	}
	f:=os.NewFile(uintptr(h),path)
	defer f.Close()
	info,err:=f.Stat()
	if err!=nil||info.Size()<1||info.Size()>unslopDurableLimit {return nil,errUnslopDurable}
	raw,err:=io.ReadAll(io.LimitReader(f,unslopDurableLimit+1))
	if err!=nil||int64(len(raw))!=info.Size()||!s.current() {clear(raw);return nil,errUnslopDurable}
	return raw,nil
}

func unslopDurableName(generation string, sequence int) string {
	return fmt.Sprintf("%s.%02d.json",generation,sequence)
}

// No replacement or cleanup occurs here. Complete pending bytes can be
// retried; a partial write is resumed only from an exact prefix of the same
// live record. After restart an incomplete record remains unknown.
func (s *unslopDurableStore) put(ctx context.Context,generation string,sequence int,raw []byte) error {
	if ctx==nil||ctx.Err()!=nil||!s.current()||!unslopHex(generation,32)||sequence<1||sequence>unslopDurableEvents||len(raw)==0||len(raw)>unslopDurableLimit {return errUnslopDurable}
	path:=filepath.Join(s.path,unslopDurableName(generation,sequence))
	if old,err:=s.read(path);err==nil {defer clear(old);if bytes.Equal(old,raw){return nil};return errUnslopDurableConflict} else if !errors.Is(err,os.ErrNotExist){return err}
	pending:=path+".pending"
	p,err:=syscall.UTF16PtrFromString(pending)
	if err!=nil{return errUnslopDurable}
	h,err:=syscall.CreateFile(p,syscall.GENERIC_READ|syscall.GENERIC_WRITE,syscall.FILE_SHARE_READ,nil,syscall.OPEN_ALWAYS,0x00200000,0)
	if err!=nil{return errUnslopDurable}
	f:=os.NewFile(uintptr(h),pending)
	closed:=false
	defer func(){if !closed {_ = f.Close()}}()
	var info syscall.ByHandleFileInformation
	actual,ok:=handlePath(h)
	if !ok||!sameCleanPath(actual,pending)||syscall.GetFileInformationByHandle(h,&info)!=nil||info.FileAttributes&(syscall.FILE_ATTRIBUTE_REPARSE_POINT|syscall.FILE_ATTRIBUTE_DIRECTORY)!=0||info.NumberOfLinks!=1 {return errUnslopDurable}
	prefix,err:=io.ReadAll(io.LimitReader(f,unslopDurableLimit+1))
	defer clear(prefix)
	if err!=nil||len(prefix)>len(raw)||!bytes.Equal(prefix,raw[:len(prefix)]) {return errUnslopDurableConflict}
	if s.hook!=nil {if s.hook("before-write")!=nil{return errUnslopDurable}}
	if len(prefix)<len(raw) {
		n,err:=s.write(f,raw[len(prefix):])
		if err!=nil||n!=len(raw)-len(prefix) {return errUnslopDurable}
	}
	if f.Sync()!=nil{return errUnslopDurable}
	if s.hook!=nil {if s.hook("after-sync")!=nil{return errUnslopDurable}}
	if f.Close()!=nil{return errUnslopDurable}
	closed=true
	if ctx.Err()!=nil{return errUnslopDurable}
	if s.hook!=nil {if s.hook("before-publish")!=nil{return errUnslopDurable}}
	from,_:=syscall.UTF16PtrFromString(pending)
	to,_:=syscall.UTF16PtrFromString(path)
	moved,_,_:=syscall.NewLazyDLL("kernel32.dll").NewProc("MoveFileExW").Call(uintptr(unsafe.Pointer(from)),uintptr(unsafe.Pointer(to)),8)
	if moved==0 {return errUnslopDurable}
	if s.hook!=nil {if s.hook("after-publish")!=nil{return errUnslopDurable}}
	check,err:=s.read(path)
	defer clear(check)
	if err!=nil||!bytes.Equal(check,raw){return errUnslopDurableConflict}
	return nil
}

type unslopDurableReceipt struct {
	SHA string `json:"sha256"`
	CandidateSHA string `json:"candidate_sha256"`
	InputSHA string `json:"input_manifest_sha256"`
	CargoJSONSHA string `json:"cargo_json_sha256"`
	CargoStreamSHA string `json:"cargo_stream_sha256"`
	CommandSHA string `json:"command_sha256"`
	IdentitySHA string `json:"identity_sha256"`
	Bytes int `json:"bytes"`
	PID uint32 `json:"pid"`
	Created uint64 `json:"created"`
	Acknowledged bool `json:"acknowledged"`
	Reaped bool `json:"reaped"`
}

type unslopDurableCapture struct {
	ObservedQualified bool `json:"observed_qualified"`
	Published bool `json:"receipt_published"`
	Attempted bool `json:"capture_attempted"`
	Reason string `json:"reason"`
	CaptureReason string `json:"capture_reason"`
	ToolchainReason string `json:"toolchain_reason"`
	CleanupFailed bool `json:"toolchain_cleanup_failed"`
}

type unslopDurableEvent struct {
	Domain string `json:"domain"`
	Source string `json:"source"`
	CommandSHA string `json:"command_sha256"`
	Generation string `json:"generation"`
	Sequence int `json:"sequence"`
	PreviousSHA string `json:"previous_sha256"`
	Kind string `json:"kind"`
	BindingSHA string `json:"binding_sha256,omitempty"`
	Receipt *unslopDurableReceipt `json:"receipt,omitempty"`
	Published *bool `json:"published,omitempty"`
	Capture *unslopDurableCapture `json:"capture,omitempty"`
	Owner *unslopRetainedReport `json:"owner,omitempty"`
}

func unslopDurableReason(reason string) bool {
	switch reason {
	case "", "missing_producer_provenance", "toolchain_origin_provider_unavailable", "toolchain_origin_unavailable", "unslop_toolchain_provider_unavailable", "toolchain_inputs_mismatch", "toolchain_cancelled", "toolchain_consumer_failed", "toolchain_callback_invalid", "toolchain_inputs_consumed", "unslop_cancelled", "capture_inputs_unqualified_before_build", "capture_producer_failed_or_uncollected", "capture_cargo_evidence_unqualified", "capture_inputs_changed_after_build", "capture_pending_preserved", "capture_export_close_failed", "capture_artifact_unqualified", "capture_pending_failed_preserved", "capture_ack_failed", "capture_producer_ack_or_exit_unverified", "capture_cleanup_failed_preserved", "capture_receipt_seal_failed_preserved", "capture_receipt_failed_preserved", "captured_release_receipt_published": return true
	}
	return false
}

func unslopDurableCaptureOf(r unslopToolchainCaptureResult) (*unslopDurableCapture,error) {
	if !unslopDurableReason(r.Reason)||!unslopDurableReason(r.CaptureReason)||!unslopDurableReason(r.ToolchainReason)||r.DataAccessGranted||r.UpdatePerformed {return nil,errUnslopDurableConflict}
	return &unslopDurableCapture{r.Qualified,r.ReceiptPublished,r.CaptureAttempted,r.Reason,r.CaptureReason,r.ToolchainReason,r.ToolchainCleanupFailed},nil
}

func unslopDurableReceiptOf(r unslopCaptureReceipt) (*unslopDurableReceipt,error) {
	if r.Domain!=unslopCaptureDomain||r.Source!=unslopCaptureSource||r.InstalledTarget!=unslopInstalledPath||r.Namespace!=unslopCaptureRoot||r.ToolCallID!=nil||r.NativeProcess==nil||r.NativeProcess.PID==0||r.NativeProcess.Created==0||r.Bytes<1||r.Bytes>32<<20||!r.CompleteCompiledInputs||!unslopCompleteIdentity(r.CandidateIdentity) {return nil,errUnslopDurableConflict}
	for _,sha:=range []string{r.InputManifestSHA,r.CandidateSHA,r.CargoJSONSHA,r.CargoStreamSHA,r.CommandSHA} {if !unslopHex(sha,64){return nil,errUnslopDurableConflict}}
	raw,_:=json.Marshal(r)
	identity,_:=json.Marshal(r.CandidateIdentity)
	return &unslopDurableReceipt{digest(raw),r.CandidateSHA,r.InputManifestSHA,r.CargoJSONSHA,r.CargoStreamSHA,r.CommandSHA,digest(identity),r.Bytes,r.NativeProcess.PID,r.NativeProcess.Created,r.ProducerAcknowledged,r.ProducerReaped},nil
}

func unslopDurableOwnerValid(r unslopRetainedReport) bool {
	prefix:="unslop-retained-"
	if !strings.HasPrefix(r.OwnerID,prefix)||len(r.OwnerID)>96||len(r.OwnerID)<len(prefix)+34 {return false}
	parts:=strings.Split(strings.TrimPrefix(r.OwnerID,prefix),"-")
	if len(parts)!=2||!unslopHex(parts[0],32)||parts[1]=="" {return false}
	for _,c:=range parts[1] {if c<'0'||c>'9'{return false}}
	if r.Capture.Receipt!=nil||r.Capture.Qualified||r.Capture.DataAccessGranted||r.Capture.UpdatePerformed {return false}
	if _,err:=unslopDurableCaptureOf(r.Capture);err!=nil{return false}
	valid:=func(value string,values ...string)bool {for _,v:=range values {if value==v{return true}};return false}
	if r.State=="released"||r.State=="release_failed"||r.State=="release_in_progress" {
		if r.Collection!="verified"||r.CloseAttempts!=1||r.NativeProcess==nil||r.NativeProcess.PID==0||r.NativeProcess.Creation==0||r.WorkerExit==nil&&r.State!="release_in_progress"{return false}
	}
	if r.State=="released"&&(r.CloseFailures!=0||r.Recheck!="verified"){return false}
	if r.State=="release_failed"&&r.CloseFailures==0&&r.Recheck!="failed"{return false}
	return valid(r.State,"retained","release_in_progress","released","release_failed")&&valid(r.Collection,"pending","unverified","verified")&&valid(r.Recheck,"not_attempted","verified","failed")&&valid(r.WaitReason,"","retained_owner_wait_cancelled","retained_owner_wait_deadline")&&valid(r.Reason,"retained_owner_collection_pending","retained_owner_collection_unverified","retained_owner_release_in_progress","retained_owner_released","retained_owner_release_failed")&&r.CloseAttempts>=0&&r.CloseAttempts<=1&&r.CloseFailures>=0&&r.CloseFailures<=258
}

func (e unslopDurableEvent) valid() bool {
	if e.Domain!=unslopDurableDomain||e.Source!=unslopCaptureSource||e.CommandSHA!=fixedUnslopToolchainPurpose().CommandSHA||!unslopHex(e.Generation,32)||e.Sequence<1||e.Sequence>unslopDurableEvents||e.Sequence==1&&e.PreviousSHA!=""||e.Sequence>1&&!unslopHex(e.PreviousSHA,64) {return false}
	switch e.Kind {
	case "started": return e.Sequence==1&&e.BindingSHA==""&&e.Receipt==nil&&e.Capture==nil&&e.Owner==nil&&e.Published==nil
	case "binding": return unslopHex(e.BindingSHA,64)&&e.Receipt==nil&&e.Capture==nil&&e.Owner==nil&&e.Published==nil
	case "staged","sealed","publish-intent":
		if e.Receipt==nil||e.Capture!=nil||e.Owner!=nil||e.Published!=nil||e.BindingSHA!="" {return false}
		r:=e.Receipt
		for _,sha:=range []string{r.SHA,r.CandidateSHA,r.InputSHA,r.CargoJSONSHA,r.CargoStreamSHA,r.CommandSHA,r.IdentitySHA} {if !unslopHex(sha,64){return false}}
		return r.Bytes>0&&r.Bytes<=32<<20&&r.PID>0&&r.Created>0&&(e.Kind=="staged"||r.Acknowledged&&r.Reaped)
	case "publication": return e.Published!=nil&&e.Receipt==nil&&e.Capture==nil&&e.Owner==nil&&e.BindingSHA==""
	case "final": return e.Capture!=nil&&unslopDurableReason(e.Capture.Reason)&&unslopDurableReason(e.Capture.CaptureReason)&&unslopDurableReason(e.Capture.ToolchainReason)&&e.Receipt==nil&&e.Owner==nil&&e.Published==nil&&e.BindingSHA==""&&(!e.Capture.ObservedQualified||e.Capture.Published&&!e.Capture.CleanupFailed&&e.Capture.Reason=="captured_release_receipt_published"&&e.Capture.CaptureReason=="captured_release_receipt_published"&&e.Capture.ToolchainReason=="toolchain_inputs_consumed")
	case "owner","owner-intent": return e.Owner!=nil&&unslopDurableOwnerValid(*e.Owner)&&e.Receipt==nil&&e.Capture==nil&&e.Published==nil&&e.BindingSHA==""
	}
	return false
}

type unslopDurableJournal struct {
	store *unslopDurableStore
	generation string
	events []unslopDurableEvent
	hashes []string
	pending *unslopDurableEvent
}

func (j *unslopDurableJournal) load() error {
	// Publish a new cache only after the entire stored prefix is verified.
	// A transient read failure must not erase the last sealed receipt.
	next:=unslopDurableJournal{store:j.store,generation:j.generation}
	for n:=1;n<=unslopDurableEvents;n++ {
		raw,err:=j.store.read(filepath.Join(j.store.path,unslopDurableName(j.generation,n)))
		if errors.Is(err,os.ErrNotExist) {
			for later:=n+1;later<=unslopDurableEvents;later++ {if _,err:=os.Lstat(filepath.Join(j.store.path,unslopDurableName(j.generation,later)));err==nil{return errUnslopDurableConflict}else if !errors.Is(err,os.ErrNotExist){return errUnslopDurable}}
			j.events,j.hashes=next.events,next.hashes
			return nil
		}
		if err!=nil{return err}
		var e unslopDurableEvent
		dec:=json.NewDecoder(bytes.NewReader(raw));dec.DisallowUnknownFields()
		if dec.Decode(&e)!=nil||dec.Decode(new(any))!=io.EOF||!e.valid()||e.Generation!=j.generation||e.Sequence!=n {clear(raw);return errUnslopDurableConflict}
		canonical,_:=json.Marshal(e)
		if !bytes.Equal(canonical,raw)||n>1&&e.PreviousSHA!=next.hashes[n-2] {clear(raw);return errUnslopDurableConflict}
		if n==1&&e.Kind!="started" {clear(raw);return errUnslopDurableConflict}
		if next.last(e.Kind)!=nil&&e.Kind!="owner"||!next.transition(e){clear(raw);return errUnslopDurableConflict}
		next.events=append(next.events,e);next.hashes=append(next.hashes,digest(raw));clear(raw)
	}
	j.events,j.hashes=next.events,next.hashes
	return nil
}

func (j *unslopDurableJournal) append(ctx context.Context,e unslopDurableEvent) (string,error) {
	if j==nil||j.store==nil||ctx==nil||ctx.Err()!=nil{return "",errUnslopDurable}
	if j.pending!=nil {
		pending:=*j.pending
		if _,err:=j.appendOne(ctx,pending);err!=nil{return "",err}
		j.pending=nil
	}
	return j.appendOne(ctx,e)
}

// A pending event is cleared only by append after its exact bytes have been
// read back. Lock, load and validation errors must not discard the event.
func (j *unslopDurableJournal) appendOne(ctx context.Context,e unslopDurableEvent) (string,error) {
	if j==nil||j.store==nil||ctx==nil||ctx.Err()!=nil{return "",errUnslopDurable}
	remember:=func(){if j.pending==nil {pending:=e;j.pending=&pending}}
	j.store.mu.Lock();defer j.store.mu.Unlock()
	unlock,err:=j.store.lock(j.generation);if err!=nil{remember();return "",err};defer unlock()
	if err=j.load();err!=nil{remember();return "",err}
	// Named phases are immutable. Repeating the same final/receipt is a readback,
	// never a second capture or publication. Owner snapshots can advance.
	for n,old:=range j.events {
		if old.Kind!=e.Kind {continue}
		if e.Kind=="owner"&&j.last("owner")!=&j.events[n] {continue}
		e.Domain=old.Domain;e.Source=old.Source;e.CommandSHA=old.CommandSHA;e.Generation=old.Generation;e.Sequence=old.Sequence;e.PreviousSHA=old.PreviousSHA
		left,_:=json.Marshal(old);right,_:=json.Marshal(e)
		if bytes.Equal(left,right){return j.hashes[n],nil}
		if e.Kind!="owner" {return "",errUnslopDurableConflict}
	}
	if len(j.events)>=unslopDurableEvents{return "",errUnslopDurable}
	e.Domain=unslopDurableDomain;e.Source=unslopCaptureSource;e.CommandSHA=fixedUnslopToolchainPurpose().CommandSHA;e.Generation=j.generation;e.Sequence=len(j.events)+1;e.PreviousSHA=""
	if len(j.hashes)>0 {e.PreviousSHA=j.hashes[len(j.hashes)-1]}
	if !e.valid()||len(j.events)==0&&e.Kind!="started"||len(j.events)>0&&e.Kind=="started" {return "",errUnslopDurableConflict}
	if !j.transition(e){return "",errUnslopDurableConflict}
	raw,_:=json.Marshal(e)
	if err=j.store.put(ctx,j.generation,e.Sequence,raw);err!=nil{remember();return "",err}
	if err=j.load();err!=nil||len(j.events)!=e.Sequence {remember();return "",errUnslopDurable}
	if j.hashes[len(j.hashes)-1]!=digest(raw){remember();return "",errUnslopDurableConflict}
	return digest(raw),nil
}

func (j *unslopDurableJournal) last(kind string) *unslopDurableEvent {
	for n:=len(j.events)-1;n>=0;n-- {if j.events[n].Kind==kind{return &j.events[n]}}
	return nil
}

func (j *unslopDurableJournal) transition(e unslopDurableEvent) bool {
	switch e.Kind {
	case "started":return len(j.events)==0
	case "binding":return j.last("started")!=nil&&j.last("staged")==nil&&j.last("final")==nil
	case "staged":return j.last("binding")!=nil&&j.last("final")==nil&&!e.Receipt.Acknowledged&&!e.Receipt.Reaped
	case "sealed":
		old:=j.last("staged");if old==nil||j.last("final")!=nil{return false}
		left,right:=*old.Receipt,*e.Receipt;left.SHA="";right.SHA="";left.Acknowledged=false;right.Acknowledged=false;left.Reaped=false;right.Reaped=false
		return left==right
	case "publish-intent":old:=j.last("sealed");return old!=nil&&*old.Receipt==*e.Receipt&&j.last("final")==nil
	case "publication":return j.last("publish-intent")!=nil&&j.last("final")==nil
	case "final":
		if e.Capture.Published {old:=j.last("publication");return old!=nil&&*old.Published}
		if j.last("publish-intent")!=nil&&j.last("publication")==nil{return false}
		return j.last("publication")==nil||!*j.last("publication").Published
	case "owner":
		if intent:=j.last("owner-intent");intent!=nil&&intent.Owner.OwnerID!=e.Owner.OwnerID {return false}
		if final:=j.last("final");final!=nil {
			captured,err:=unslopDurableCaptureOf(e.Owner.Capture);if err!=nil{return false}
			expected:=*final.Capture;expected.ObservedQualified=false
			if *captured!=expected{return false}
		}
		old:=j.last("owner");return old==nil||old.Owner.OwnerID==e.Owner.OwnerID&&old.Owner.State!="released"&&old.Owner.State!="release_failed"
	case "owner-intent":return j.last("owner")==nil&&e.Owner.State=="retained"&&e.Owner.Collection=="pending"
	}
	return false
}

type unslopDurableCaptureResult struct {
	unslopToolchainCaptureResult
	Generation string `json:"generation,omitempty"`
	ResultSaved bool `json:"result_saved"`
	DurableReason string `json:"durable_reason"`
	RetainedOwner *unslopRetainedReport `json:"retained_owner,omitempty"`
}

type unslopDurableController struct {
	mu sync.Mutex
	journal *unslopDurableJournal
	provider rustToolchainOriginProvider
	factory unslopToolchainCaptureFactory
	reserved,started,attempted,recovered bool
	result *unslopToolchainCaptureResult
	observedLive *unslopLiveSession
	observedHeld rustToolchainHeld
	observedBinding unslopRustToolchainBinding
	owner *unslopRetainedOwner
	ticket *unslopRetainedTicket
	pendingSource *unslopLiveSession
	pendingLease softwareRustHeldSession
	pendingReservation *unslopRetainedReservation
}

func newUnslopDurableController(store *unslopDurableStore,provider rustToolchainOriginProvider,factory unslopToolchainCaptureFactory) (*unslopDurableController,error) {
	if store==nil||!store.current(){return nil,errUnslopDurable}
	var nonce [16]byte
	if _,err:=rand.Read(nonce[:]);err!=nil{return nil,errUnslopDurable}
	return &unslopDurableController{journal:&unslopDurableJournal{store:store,generation:hex.EncodeToString(nonce[:])},provider:provider,factory:factory},nil
}

func (c *unslopDurableController) begin(ctx context.Context) error {
	if c==nil||c.journal==nil||c.recovered{return errUnslopDurable}
	if ctx==nil||ctx.Err()!=nil{return errUnslopDurable}
	if !c.reserved {if err:=c.journal.store.reserve(c.journal.generation);err!=nil{return err};c.reserved=true}
	if _,err:=c.journal.append(ctx,unslopDurableEvent{Kind:"started"});err!=nil{return err}
	c.started=true
	return nil
}

type unslopDurablePending struct {
	unslopPendingCapture
	journal *unslopDurableJournal
	sealed *unslopDurableReceipt
}

func (p *unslopDurablePending) Seal(r unslopCaptureReceipt) error {
	receipt,err:=unslopDurableReceiptOf(r);if err!=nil{return err}
	if err=p.unslopPendingCapture.Seal(r);err!=nil{return err}
	if _,err=p.journal.append(context.Background(),unslopDurableEvent{Kind:"sealed",Receipt:receipt});err!=nil{return err}
	p.sealed=receipt
	return nil
}

func (p *unslopDurablePending) Commit(ctx context.Context) error {
	if p.sealed==nil{return errUnslopDurableConflict}
	if _,err:=p.journal.append(ctx,unslopDurableEvent{Kind:"publish-intent",Receipt:p.sealed});err!=nil{return err}
	err:=p.unslopPendingCapture.Commit(ctx)
	published:=p.unslopPendingCapture.Published()
	if _,saveErr:=p.journal.append(context.Background(),unslopDurableEvent{Kind:"publication",Published:&published});saveErr!=nil{return errUnslopDurable}
	return err
}

// Capture is the actual caller of the existing capture/provider controller.
// Only a code-constructed runtime can install it in runUnslopPurpose.
func (c *unslopDurableController) Capture(ctx context.Context) unslopDurableCaptureResult {
	if c==nil{return unslopDurableCaptureResult{DurableReason:"durable_controller_unavailable"}}
	c.mu.Lock();defer c.mu.Unlock()
	if c.attempted||c.recovered||c.owner!=nil{return c.currentResult(false,"durable_generation_already_used")}
	if c.begin(ctx)!=nil{return c.currentResult(false,"durable_start_save_failed")}
	c.attempted=true
	factory:=func(ctx context.Context,held rustToolchainHeld,binding unslopRustToolchainBinding)(unslopCaptureAccess,error){
		if c.factory==nil{return unslopCaptureAccess{},errUnslopDurable}
		if _,err:=c.journal.append(ctx,unslopDurableEvent{Kind:"binding",BindingSHA:binding.SHA});err!=nil{return unslopCaptureAccess{},err}
		access,err:=c.factory(ctx,held,binding);if err!=nil{return access,err}
		begin:=access.begin
		access.begin=func(ctx context.Context)(unslopProducerSession,error){
			if begin==nil{return nil,errUnslopDurable}
			session,err:=begin(ctx)
			if live,ok:=session.(*unslopLiveSession);ok&&err==nil {c.observedLive=live;c.observedHeld=held;c.observedBinding=binding}
			return session,err
		}
		stage:=access.stage
		access.stage=func(ctx context.Context,r unslopCaptureReceipt,raw []byte)(unslopPendingCapture,error){
			receipt,err:=unslopDurableReceiptOf(r);if err!=nil{return nil,err}
			if _,err=c.journal.append(ctx,unslopDurableEvent{Kind:"staged",Receipt:receipt});err!=nil{return nil,err}
			if stage==nil{return nil,errUnslopDurable}
			pending,err:=stage(ctx,r,raw)
			if pending==nil{return nil,err}
			return &unslopDurablePending{unslopPendingCapture:pending,journal:c.journal},err
		}
		return access,nil
	}
	result:=captureUnslopReleaseWithToolchain(ctx,c.provider,factory)
	copy:=unslopDurableCloneResult(result);c.result=&copy
	return c.saveFinal(ctx)
}

func (c *unslopDurableController) currentResult(saved bool,reason string) unslopDurableCaptureResult {
	r:=unslopDurableCaptureResult{ResultSaved:saved,DurableReason:reason}
	if c.journal!=nil {r.Generation=c.journal.generation}
	if c.result!=nil{r.unslopToolchainCaptureResult=unslopDurableCloneResult(*c.result)}
	if c.owner!=nil&&c.ticket!=nil {report:=c.owner.Snapshot(c.ticket);if unslopDurableOwnerValid(report){r.RetainedOwner=&report}}
	if c.owner==nil&&c.pendingReservation!=nil {report:=unslopRetainedCopyReport(c.pendingReservation.report);if unslopDurableOwnerValid(report){r.RetainedOwner=&report}}
	if !unslopDurableReason(r.Reason)||!unslopDurableReason(r.CaptureReason)||!unslopDurableReason(r.ToolchainReason) {
		r.unslopToolchainCaptureResult=unslopToolchainCaptureResult{unslopCaptureResult:unslopCaptureResult{Reason:"missing_producer_provenance",ReceiptPublished:r.ReceiptPublished}}
		r.ResultSaved=false;r.DurableReason="durable_result_invalid"
	}
	r.DataAccessGranted=false;r.UpdatePerformed=false
	if !r.ResultSaved||c.recovered {r.Qualified=false}
	if !r.Qualified {r.Receipt=nil}
	return r
}

func unslopDurableCloneResult(r unslopToolchainCaptureResult) unslopToolchainCaptureResult {
	if r.Receipt!=nil {receipt:=*r.Receipt;if receipt.NativeProcess!=nil {process:=*receipt.NativeProcess;receipt.NativeProcess=&process};r.Receipt=&receipt}
	return r
}

func (c *unslopDurableController) saveFinal(ctx context.Context) unslopDurableCaptureResult {
	if c.result==nil{return c.currentResult(false,"durable_final_result_unavailable")}
	projection,err:=unslopDurableCaptureOf(*c.result)
	if err!=nil{return c.currentResult(false,"durable_result_invalid")}
	if c.result.Receipt!=nil {
		receipt,err:=unslopDurableReceiptOf(*c.result.Receipt);sealed:=c.journal.last("sealed")
		if err!=nil||sealed==nil||*receipt!=*sealed.Receipt{return c.currentResult(false,"durable_result_invalid")}
	}
	if _,err=c.journal.append(ctx,unslopDurableEvent{Kind:"final",Capture:projection});err!=nil{return c.currentResult(false,"durable_final_save_failed")}
	return c.currentResult(true,"durable_final_saved")
}

func (c *unslopDurableController) RetryFinal(ctx context.Context) unslopDurableCaptureResult {
	if c==nil{return unslopDurableCaptureResult{DurableReason:"durable_controller_unavailable"}}
	c.mu.Lock();defer c.mu.Unlock()
	if c.recovered{return c.currentResult(false,"durable_recovered_metadata_only")}
	return c.saveFinal(ctx)
}

// This proof cannot be decoded from JSON or made from a caller's success bool.
// It binds one live owner/ticket/report to a completed same-byte readback.
type unslopRetainedSaveProof struct {
	controller *unslopDurableController
	owner *unslopRetainedOwner
	ticket *unslopRetainedTicket
	generation,reportSHA,recordSHA string
}

func (c *unslopDurableController) SaveRetained(ctx context.Context,owner *unslopRetainedOwner,ticket *unslopRetainedTicket) error {
	_,_,err:=c.SaveRetainedReport(ctx,owner,ticket)
	return err
}

func (c *unslopDurableController) SaveRetainedReport(ctx context.Context,owner *unslopRetainedOwner,ticket *unslopRetainedTicket) (unslopRetainedReport,string,error) {
	report,recordSHA,_,err:=c.saveRetainedReport(ctx,owner,ticket)
	return report,recordSHA,err
}

// The target is returned only after the controller has bound this exact live
// entry. A storage error may cancel that flight; a rejected target may not.
func (c *unslopDurableController) saveRetainedReport(ctx context.Context,owner *unslopRetainedOwner,ticket *unslopRetainedTicket) (unslopRetainedReport,string,*unslopRetainedEntry,error) {
	var empty unslopRetainedReport
	if c==nil||owner==nil||ticket==nil{return empty,"",nil,errUnslopDurable}
	c.mu.Lock();defer c.mu.Unlock()
	if c.recovered||c.pendingReservation!=nil||c.owner!=nil&&(c.owner!=owner||c.ticket!=ticket){return empty,"",nil,errUnslopDurableConflict}
	owner.mu.Lock()
	entry:=owner.entries[ticket]
	if entry==nil||entry.ticket!=ticket||entry.durable!=nil&&entry.durable!=c {owner.mu.Unlock();return empty,"",nil,errUnslopDurableConflict}
	if entry.report.State!="released"&&(entry.flight==nil||entry.flight.Done==nil||entry.flight.Abort==nil||owner.flights[entry.flight]!=ticket||owner.done[entry.flight.Done]!=ticket) {owner.mu.Unlock();return empty,"",nil,errUnslopDurableConflict}
	report:=unslopRetainedCopyReport(entry.report)
	if c.result!=nil {
		expected:=unslopDurableCloneResult(*c.result);expected.Qualified=false;expected.Receipt=nil
		left,_:=json.Marshal(expected);right,_:=json.Marshal(report.Capture)
		if !bytes.Equal(left,right){owner.mu.Unlock();return empty,"",nil,errUnslopDurableConflict}
	}
	if !unslopDurableOwnerValid(report){owner.mu.Unlock();return empty,"",nil,errUnslopDurableConflict}
	entry.durable=c
	owner.mu.Unlock()
	c.owner=owner;c.ticket=ticket
	if !c.started {if err:=c.begin(ctx);err!=nil{return empty,"",entry,err}}
	if c.attempted {if r:=c.saveFinal(ctx);!r.ResultSaved{return empty,"",entry,errUnslopDurable}}
	recordSHA,err:=c.journal.append(ctx,unslopDurableEvent{Kind:"owner",Owner:&report})
	if err!=nil{return empty,"",entry,err}
	raw,_:=json.Marshal(report)
	owner.mu.Lock();defer owner.mu.Unlock()
	entry=owner.entries[ticket]
	if entry==nil||entry.durable!=c{return empty,"",nil,errUnslopDurableConflict}
	current,_:=json.Marshal(unslopRetainedCopyReport(entry.report))
	if !bytes.Equal(current,raw){return empty,"",nil,errUnslopDurableConflict}
	entry.saved=&unslopRetainedSaveProof{c,owner,ticket,c.journal.generation,digest(raw),recordSHA}
	return unslopRetainedCopyReport(report),recordSHA,entry,nil
}

func (c *unslopDurableController) cancelRetainedAfterSaveFailure(ctx context.Context,owner *unslopRetainedOwner,ticket *unslopRetainedTicket,target *unslopRetainedEntry) unslopRetainedReport {
	unknown:=unslopRetainedReport{State:"unknown",Reason:"retained_owner_unknown"}
	if c==nil||owner==nil||ticket==nil||target==nil {return unknown}
	c.mu.Lock()
	if c.recovered||c.owner!=owner||c.ticket!=ticket {c.mu.Unlock();return unknown}
	owner.mu.Lock()
	valid:=owner.entries[ticket]==target&&target.durable==c&&target.ticket==ticket
	report:=unslopRetainedCopyReport(target.report)
	canAbort:=valid&&target.report.State=="retained"&&target.flight!=nil&&owner.flights[target.flight]==ticket&&owner.done[target.flight.Done]==ticket
	owner.mu.Unlock();c.mu.Unlock()
	if !valid{return unknown}
	if canAbort&&ctx!=nil&&ctx.Err()!=nil {return owner.Drain(ctx,ticket)}
	return report
}

func (p *unslopRetainedSaveProof) matches(owner *unslopRetainedOwner,ticket *unslopRetainedTicket,entry *unslopRetainedEntry) bool {
	if p==nil||p.controller==nil||p.owner!=owner||p.ticket!=ticket||entry.durable!=p.controller||p.generation!=p.controller.journal.generation||!unslopHex(p.recordSHA,64){return false}
	raw,_:=json.Marshal(unslopRetainedCopyReport(entry.report))
	return digest(raw)==p.reportSHA
}

type unslopDurableDrainResult struct {
	Report unslopRetainedReport `json:"report"`
	Saved bool `json:"saved"`
	RecordSHA string `json:"record_sha256,omitempty"`
	Forgotten bool `json:"forgotten"`
	Reason string `json:"reason"`
}

func (c *unslopDurableController) DrainAndSave(ctx context.Context,owner *unslopRetainedOwner,ticket *unslopRetainedTicket) unslopDurableDrainResult {
	if c==nil||owner==nil{return unslopDurableDrainResult{Reason:"durable_controller_unavailable"}}
	// Save the pre-drain state before Close or other collection side effects.
	if _,_,target,err:=c.saveRetainedReport(ctx,owner,ticket);err!=nil {
		// Cancellation still belongs to this exact flight even when storage is
		// unavailable. Rejected and recovered targets never reach Drain.
		report:=c.cancelRetainedAfterSaveFailure(ctx,owner,ticket,target)
		if !unslopDurableOwnerValid(report){report=unslopRetainedReport{State:"unknown",Reason:"retained_owner_unknown"}}
		return unslopDurableDrainResult{Report:report,Reason:"durable_owner_save_failed"}
	}
	report:=owner.Drain(ctx,ticket)
	if !unslopDurableOwnerValid(report){return unslopDurableDrainResult{Report:unslopRetainedReport{State:"unknown",Reason:"retained_owner_unknown"},Reason:"durable_owner_save_failed"}}
	return c.saveDrainedReport(ctx,owner,ticket,report)
}

func (c *unslopDurableController) saveDrainedReport(ctx context.Context,owner *unslopRetainedOwner,ticket *unslopRetainedTicket,report unslopRetainedReport) unslopDurableDrainResult {
	r:=unslopDurableDrainResult{Report:report,Reason:"durable_owner_save_failed"}
	saved,recordSHA,err:=c.SaveRetainedReport(ctx,owner,ticket)
	if err!=nil{return r}
	r.Report=saved
	r.RecordSHA=recordSHA
	r.Saved=true;r.Reason="durable_owner_saved"
	if saved.State=="released" {r.Forgotten=owner.Forget(ticket);if !r.Forgotten {r.Reason="durable_owner_not_forgotten"}}
	return r
}

func (c *unslopDurableController) RetryRetained(ctx context.Context) unslopDurableDrainResult {
	if c==nil{return unslopDurableDrainResult{Reason:"durable_controller_unavailable"}}
	c.mu.Lock()
	if c.owner==nil&&c.pendingReservation!=nil {report:=unslopRetainedCopyReport(c.pendingReservation.report);c.mu.Unlock();return unslopDurableDrainResult{Report:report,Reason:"durable_owner_transfer_pending"}}
	owner,ticket:=c.owner,c.ticket;c.mu.Unlock()
	return c.DrainAndSave(ctx,owner,ticket)
}

// The normal caller uses its actual capture result, never a supplied generation
// or success flag, when handing a live session and lease to the registry.
func (c *unslopDurableController) Retain(ctx context.Context,owner *unslopRetainedOwner,source **unslopLiveSession,lease *softwareRustHeldSession,extra []io.Closer) (*unslopRetainedTicket,error) {
	if c==nil||owner==nil{return nil,errUnslopDurable}
	c.mu.Lock()
	if c.recovered||!c.attempted||c.result==nil||c.owner!=nil||len(extra)!=0||source==nil||*source==nil||lease==nil||!unslopRetainedPointer(*lease) {c.mu.Unlock();return nil,errUnslopDurableConflict}
	if c.observedLive==nil||c.observedLive!=*source {c.mu.Unlock();return nil,errUnslopDurableConflict}
	if c.pendingSource!=nil&&(c.pendingSource!=*source||c.pendingLease!=*lease){c.mu.Unlock();return nil,errUnslopDurableConflict}
	if c.pendingReservation!=nil&&c.pendingReservation.owner!=owner {c.mu.Unlock();return nil,errUnslopDurableConflict}
	held:=(*lease).Held()
	if len(held.Cargo.Raw)==0||len(held.Rustc.Raw)==0||len(c.observedHeld.Cargo.Raw)==0||len(c.observedHeld.Rustc.Raw)==0||&held.Cargo.Raw[0]!=&c.observedHeld.Cargo.Raw[0]||&held.Rustc.Raw[0]!=&c.observedHeld.Rustc.Raw[0] {c.mu.Unlock();return nil,errUnslopDurableConflict}
	bound,valid:=bindUnslopRustToolchain(fixedUnslopToolchainPurpose(),held)
	if !valid||bound.SHA!=c.observedBinding.SHA {c.mu.Unlock();return nil,errUnslopDurableConflict}
	c.pendingSource=*source;c.pendingLease=*lease
	result:=unslopDurableCloneResult(*c.result)
	ticket,err:=owner.retainReserved(source,lease,extra,result,&c.pendingReservation,func(report unslopRetainedReport)error {
		_,err:=c.journal.append(ctx,unslopDurableEvent{Kind:"owner-intent",Owner:&report});return err
	})
	if err!=nil {c.mu.Unlock();return nil,err}
	owner.mu.Lock();owner.entries[ticket].durable=c;owner.mu.Unlock()
	c.owner=owner;c.ticket=ticket
	c.pendingSource=nil;c.pendingLease=nil
	c.mu.Unlock()
	// A failed save does not undo the move: the returned ticket and registry
	// still own all resources, and no proof permits Forget.
	return ticket,c.SaveRetained(ctx,owner,ticket)
}

type unslopDurableRecovery struct {
	Generation string `json:"generation"`
	State string `json:"state"`
	Capture *unslopDurableCapture `json:"capture,omitempty"`
	Owner *unslopRetainedReport `json:"owner,omitempty"`
	ReceiptSHA string `json:"receipt_sha256,omitempty"`
	ReceiptPublished bool `json:"receipt_published"`
	Publication string `json:"publication"`
	PendingWrite bool `json:"pending_write"`
	Qualified bool `json:"qualified"`
	DataAccessGranted bool `json:"data_access_granted"`
}

// Recover reads metadata only. It never creates an OS owner, accepts a PID as
// proof of collection, resumes a build, or grants use of a stored receipt.
func (s *unslopDurableStore) Recover(ctx context.Context,generation string) (unslopDurableRecovery,error) {
	r:=unslopDurableRecovery{Generation:generation,State:"durable_recovery_unknown",Publication:"unknown"}
	if s==nil||ctx==nil||ctx.Err()!=nil||!unslopHex(generation,32){return r,errUnslopDurable}
	if _,err:=os.Lstat(filepath.Join(s.path,generation+".lock"));err!=nil{return r,errUnslopDurable}
	s.mu.Lock();defer s.mu.Unlock()
	unlock,err:=s.lock(generation);if err!=nil{return r,err};defer unlock()
	j:=&unslopDurableJournal{store:s,generation:generation}
	if err=j.load();err!=nil{return r,err}
	if len(j.events)==0 {r.State="durable_recovery_incomplete";return r,nil}
	r.State="durable_recovery_incomplete"
	r.Publication="not_started"
	if e:=j.last("sealed");e!=nil {r.ReceiptSHA=e.Receipt.SHA}
	if j.last("publish-intent")!=nil {r.Publication="unknown"}
	if e:=j.last("publication");e!=nil {r.ReceiptPublished=*e.Published;if *e.Published {r.Publication="published"}else{r.Publication="not_published"}}
	if e:=j.last("final");e!=nil {value:=*e.Capture;r.Capture=&value;r.ReceiptPublished=r.ReceiptPublished||value.Published;r.State="durable_capture_result_saved"}
	if e:=j.last("owner-intent");e!=nil {value:=unslopRetainedCopyReport(*e.Owner);r.Owner=&value;r.State="durable_owner_transfer_unconfirmed"}
	if e:=j.last("owner");e!=nil {value:=unslopRetainedCopyReport(*e.Owner);r.Owner=&value;r.ReceiptPublished=r.ReceiptPublished||value.Capture.ReceiptPublished;if value.State=="released" {r.State="durable_release_result_saved"}else{r.State="durable_owner_reconciliation_required"}}
	if _,err:=os.Lstat(filepath.Join(s.path,unslopDurableName(generation,len(j.events)+1)+".pending"));err==nil {
		r.PendingWrite=true
		if r.Owner!=nil {r.State="durable_owner_reconciliation_required"}else{r.State="durable_recovery_incomplete"}
	}else if !errors.Is(err,os.ErrNotExist){return r,errUnslopDurable}
	return r,nil
}

func restoreUnslopDurableController(store *unslopDurableStore,generation string) (*unslopDurableController,unslopDurableRecovery,error) {
	recovery,err:=store.Recover(context.Background(),generation)
	if err!=nil{return nil,recovery,err}
	c:=&unslopDurableController{journal:&unslopDurableJournal{store:store,generation:generation},started:true,attempted:true,recovered:true}
	if recovery.Capture!=nil {
		p:=recovery.Capture
		c.result=&unslopToolchainCaptureResult{unslopCaptureResult:unslopCaptureResult{Reason:p.Reason,ReceiptPublished:p.Published},CaptureAttempted:p.Attempted,CaptureReason:p.CaptureReason,ToolchainReason:p.ToolchainReason,ToolchainCleanupFailed:p.CleanupFailed}
	}
	return c,recovery,nil
}

func (s *unslopDurableStore) RecoverAll(ctx context.Context) ([]unslopDurableRecovery,error) {
	if s==nil||!s.current(){return nil,errUnslopDurable}
	directory,err:=os.Open(s.path);if err!=nil{return nil,errUnslopDurable}
	names,err:=directory.Readdirnames(2049);_ = directory.Close()
	if err!=nil&&err!=io.EOF||len(names)>2048{return nil,errUnslopDurable}
	seen:=map[string]bool{}
	var out []unslopDurableRecovery
	for _,name:=range names {
		if !strings.HasSuffix(name,".lock"){continue}
		generation:=strings.TrimSuffix(name,".lock")
		if !unslopHex(generation,32)||seen[generation] {continue}
		if len(seen)>=32{return nil,errUnslopDurable}
		seen[generation]=true
		recovery,err:=s.Recover(ctx,generation);if err!=nil{return nil,err}
		out=append(out,recovery)
	}
	return out,nil
}
