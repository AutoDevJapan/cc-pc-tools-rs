//go:build windows

package main

import (
    "context"
    "errors"
    "fmt"
    "io"
    "os"
    "path/filepath"
    "runtime"
    "syscall"
    "unsafe"
)

type dsRunnerNativeObject struct{file,held *os.File;snapshot dsRunnerSnapshot;path,sealSHA string}
type dsRunnerNativeTarget struct{purpose dsRunnerRepairPurpose;paths [3]string;parents []*os.File;parentIDs []softwareIdentity;parentACLs []string;objects map[dsRunnerLeaf]*dsRunnerNativeObject;unclosed []*os.File;mutex syscall.Handle;ownsMutex,closed,lockedThread bool;threadID uint32}
func dsRunnerKernelID(f *os.File,directory bool)(softwareIdentity,error){
    var info syscall.ByHandleFileInformation
    if f==nil||syscall.GetFileInformationByHandle(syscall.Handle(f.Fd()),&info)!=nil||info.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT!=0||(info.FileAttributes&syscall.FILE_ATTRIBUTE_DIRECTORY!=0)!=directory||!directory&&info.NumberOfLinks!=1{return softwareIdentity{},errDSRunnerEvidence}
    return softwareIdentity{Volume:fmt.Sprint(info.VolumeSerialNumber),FileID:fmt.Sprint(uint64(info.FileIndexHigh)<<32|uint64(info.FileIndexLow)),Created:fmt.Sprint(uint64(info.CreationTime.HighDateTime)<<32|uint64(info.CreationTime.LowDateTime))},nil
}
// A failed close is kept as one fixed cleanup fact together with the primary
// failure and is never retried; an already closed file is not a new failure.
// Whether the OS handle is still open is unknown, so a session keeps such a file
// referenced (closeOwned) and reports it on every Close, the first one included;
// nothing is reopened, closed again or deleted.
func dsRunnerCloseOwned(c io.Closer)error{
    if c==nil{return nil}
    err:=c.Close();if err==nil||errors.Is(err,os.ErrClosed){return nil}
    return errors.Join(errDSRunnerCleanup,err)
}
func(n *dsRunnerNativeTarget)closeOwned(f *os.File)error{
    err:=dsRunnerCloseOwned(f);n.retain(f,err);return err
}
func(n *dsRunnerNativeTarget)retain(f *os.File,closeErr error){
    if f!=nil&&closeErr!=nil{n.unclosed=append(n.unclosed,f)}
}
// Metadata handles do not request FILE_READ_DATA. Data opens happen only after
// the private candidate plus fixed software-maintenance purpose is established.
func dsRunnerOpenMetadata(path string,directory bool)(*os.File,error){
    ptr,err:=syscall.UTF16PtrFromString(path);if err!=nil{return nil,errDSRunnerEvidence}
    share,flags:=uint32(syscall.FILE_SHARE_READ|syscall.FILE_SHARE_DELETE),uint32(0x00200000)
    if directory{share=syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE;flags|=0x02000000}
    h,err:=syscall.CreateFile(ptr,0x80|0x20000,share,nil,syscall.OPEN_EXISTING,flags,0);if err!=nil{return nil,err}
    f:=os.NewFile(uintptr(h),path);actual,ok:=handlePath(h)
    if !ok||!sameCleanPath(actual,path){return dsRunnerRejectOpened(f,errDSRunnerEvidence)};if _,err=dsRunnerKernelID(f,directory);err!=nil{return dsRunnerRejectOpened(f,err)};return f,nil
}
// A rejected metadata handle is closed once. When that close fails the file is
// returned with the error so that its owner keeps it; otherwise no file returns.
func dsRunnerRejectOpened(f *os.File,primary error)(*os.File,error){
    if closeErr:=dsRunnerCloseOwned(f);closeErr!=nil{return f,errors.Join(primary,closeErr)}
    return nil,primary
}
func openDSRunnerNativeTarget(ctx context.Context,purpose dsRunnerRepairPurpose)(dsRunnerTargetSession,error){
    return dsRunnerOpenNativeTargetAt(ctx,purpose,[3]string{dsRunnerTarget,dsRunnerStage,dsRunnerBackup})
}
func dsRunnerOpenNativeTargetAt(ctx context.Context,purpose dsRunnerRepairPurpose,paths [3]string)(result dsRunnerTargetSession,resultErr error){
    if ctx==nil||ctx.Err()!=nil||!dsRunnerPurposeValid(purpose){return nil,errDSRunnerEvidence}
    if !filepath.IsAbs(paths[0])||!sameParentPath(paths[0],paths[1])||!sameParentPath(paths[0],paths[2])||sameCleanPath(paths[1],paths[2]){return nil,errDSRunnerEvidence}
    n:=&dsRunnerNativeTarget{purpose:purpose,paths:paths,objects:map[dsRunnerLeaf]*dsRunnerNativeObject{}};issued:=false
    defer func(){if !issued{resultErr=errors.Join(resultErr,n.Close())}}()
    runtime.LockOSThread();n.lockedThread=true;thread,_,_:=syscall.NewLazyDLL("kernel32.dll").NewProc("GetCurrentThreadId").Call();n.threadID=uint32(thread)
    name,_:=syscall.UTF16PtrFromString("Local\\AutoDevJapan.DSRunnerSoftwareMaintenance4431")
    h,_,_:=syscall.NewLazyDLL("kernel32.dll").NewProc("CreateMutexW").Call(0,0,uintptr(unsafe.Pointer(name)));runtime.KeepAlive(name);if h==0{return nil,errDSRunnerEvidence};n.mutex=syscall.Handle(h)
    for{if ctx.Err()!=nil{return nil,errDSRunnerEvidence};status,err:=syscall.WaitForSingleObject(n.mutex,100);if err!=nil{return nil,errDSRunnerEvidence};if status==0||status==0x80{n.ownsMutex=true;break};if status!=0x102{return nil,errDSRunnerEvidence}}
    var ancestors []string;for p:=filepath.Dir(paths[0]);;p=filepath.Dir(p){ancestors=append(ancestors,p);if filepath.Dir(p)==p{break}}
    for i:=len(ancestors)-1;i>=0;i--{f,err:=dsRunnerOpenMetadata(ancestors[i],true);if err!=nil{n.retain(f,err);return nil,err};n.parents=append(n.parents,f);id,err:=dsRunnerKernelID(f,true);if err!=nil{return nil,err};acl,err:=updateFileACL(f);if err!=nil||acl==""{return nil,errDSRunnerEvidence};n.parentIDs=append(n.parentIDs,id);n.parentACLs=append(n.parentACLs,acl)}
    if _,err:=n.metadata(ctx,dsRunnerCurrent);err!=nil{return nil,err};issued=true;return n,nil
}
func(n *dsRunnerNativeTarget)valid(ctx context.Context)bool{if n==nil||n.closed||!n.ownsMutex||!n.lockedThread||ctx==nil||ctx.Err()!=nil||!dsRunnerPurposeValid(n.purpose){return false};thread,_,_:=syscall.NewLazyDLL("kernel32.dll").NewProc("GetCurrentThreadId").Call();return uint32(thread)==n.threadID}
func(n *dsRunnerNativeTarget)parentsMatch()bool{
    if len(n.parents)==0||len(n.parents)!=len(n.parentIDs)||len(n.parents)!=len(n.parentACLs){return false}
    for i,f:=range n.parents{id,err:=dsRunnerKernelID(f,true);if err!=nil||id!=n.parentIDs[i]{return false};actual,ok:=handlePath(syscall.Handle(f.Fd()));if !ok||!sameCleanPath(actual,f.Name()){return false};acl,err:=updateFileACL(f);if err!=nil||acl!=n.parentACLs[i]{return false}};return true
}
func(n *dsRunnerNativeTarget)observe(o *dsRunnerNativeObject)(dsRunnerSnapshot,error){
    if !n.parentsMatch(){return dsRunnerSnapshot{},errDSRunnerEvidence};id,err:=dsRunnerKernelID(o.file,false);if err!=nil{return dsRunnerSnapshot{},err};actual,ok:=handlePath(syscall.Handle(o.file.Fd()));if !ok||!sameCleanPath(actual,o.path){return dsRunnerSnapshot{},errDSRunnerEvidence};acl,err:=updateFileACL(o.file);if err!=nil||acl==""{return dsRunnerSnapshot{},errDSRunnerEvidence};stat,err:=o.file.Stat();if err!=nil{return dsRunnerSnapshot{},err}
    s:=dsRunnerSnapshot{id:id,parent:n.parentIDs[len(n.parentIDs)-1],acl:acl,size:stat.Size()};if !dsRunnerValidSnapshot(s){return dsRunnerSnapshot{},errDSRunnerEvidence};return s,nil
}
func(n *dsRunnerNativeTarget)metadata(ctx context.Context,leaf dsRunnerLeaf)(*dsRunnerNativeObject,error){
    if !n.valid(ctx)||leaf>dsRunnerSaved{return nil,errDSRunnerEvidence};if o:=n.objects[leaf];o!=nil{actual,err:=n.observe(o);if err!=nil||actual!=o.snapshot{return nil,errDSRunnerEvidence};return o,nil}
    f,err:=dsRunnerOpenMetadata(n.paths[leaf],false);if err!=nil{n.retain(f,err);return nil,err};o:=&dsRunnerNativeObject{file:f,path:n.paths[leaf]};snapshot,err:=n.observe(o);if err!=nil{return nil,errors.Join(err,n.closeOwned(f))};o.snapshot=snapshot;n.objects[leaf]=o;return o,nil
}
func(n *dsRunnerNativeTarget)Read(ctx context.Context,leaf dsRunnerLeaf)(raw []byte,snapshot dsRunnerSnapshot,resultErr error){
    o,err:=n.metadata(ctx,leaf);if err!=nil{return nil,snapshot,err};snapshot=o.snapshot
    if o.held==nil{
        ptr,err:=syscall.UTF16PtrFromString(o.path);if err!=nil{return nil,snapshot,errDSRunnerEvidence};h,err:=syscall.CreateFile(ptr,syscall.GENERIC_READ|0x20000,syscall.FILE_SHARE_READ|syscall.FILE_SHARE_DELETE,nil,syscall.OPEN_EXISTING,0x00200000,0);if err!=nil{return nil,snapshot,err};o.held=os.NewFile(uintptr(h),o.path)
    }
    defer func(){if resultErr!=nil{clear(raw);raw=nil}}()
    id,err:=dsRunnerKernelID(o.held,false);if err!=nil||id!=snapshot.id{return nil,snapshot,errDSRunnerEvidence};acl,err:=updateFileACL(o.held);if err!=nil||acl!=snapshot.acl{return nil,snapshot,errDSRunnerEvidence}
    if _,err=o.held.Seek(0,io.SeekStart);err!=nil{return nil,snapshot,err};raw,err=io.ReadAll(io.LimitReader(o.held,32<<20+1));if err!=nil||int64(len(raw))!=snapshot.size||ctx.Err()!=nil{return raw,snapshot,errDSRunnerEvidence}
    after,err:=n.observe(o);if err!=nil||after!=snapshot||o.sealSHA!=""&&digest(raw)!=o.sealSHA{return raw,snapshot,errDSRunnerEvidence};if o.sealSHA==""{o.sealSHA=digest(raw)};return raw,snapshot,nil
}
func(n *dsRunnerNativeTarget)Ensure(ctx context.Context,leaf dsRunnerLeaf,want []byte)(snapshot dsRunnerSnapshot,created bool,resultErr error){
    if !n.valid(ctx)||(leaf!=dsRunnerStaged&&leaf!=dsRunnerSaved)||len(want)==0||len(want)>32<<20{return snapshot,false,errDSRunnerEvidence}
    current,err:=n.metadata(ctx,dsRunnerCurrent);if err!=nil{return snapshot,false,err}
    existing,err:=n.metadata(ctx,leaf);if err==nil{raw,s,readErr:=n.Read(ctx,leaf);defer clear(raw);if readErr!=nil||s.acl!=current.snapshot.acl||digest(raw)!=digest(want)||len(raw)!=len(want){return snapshot,false,errDSRunnerCollision};_ = existing;return s,false,nil}
    if !errors.Is(err,syscall.ERROR_FILE_NOT_FOUND){return snapshot,false,errDSRunnerCollision}
    sdText,err:=syscall.UTF16PtrFromString(current.snapshot.acl);if err!=nil{return snapshot,false,errDSRunnerEvidence};var sd uintptr;ok,_,_:=updateStringSD.Call(uintptr(unsafe.Pointer(sdText)),1,uintptr(unsafe.Pointer(&sd)),0);runtime.KeepAlive(sdText);if ok==0||sd==0{return snapshot,false,errDSRunnerEvidence};defer updateLocalFree.Call(sd)
    sa:=syscall.SecurityAttributes{Length:uint32(unsafe.Sizeof(syscall.SecurityAttributes{})),SecurityDescriptor:sd};name,err:=syscall.UTF16PtrFromString(n.paths[leaf]);if err!=nil{return snapshot,false,errDSRunnerEvidence}
    h,err:=syscall.CreateFile(name,syscall.GENERIC_READ|syscall.GENERIC_WRITE|0xe0000,syscall.FILE_SHARE_READ,&sa,syscall.CREATE_NEW,0x00200000,0);if err!=nil{return snapshot,false,err};created=true;f:=os.NewFile(uintptr(h),n.paths[leaf]);closed:=false;defer func(){if !closed{resultErr=errors.Join(resultErr,n.closeOwned(f))}}()
    id,err:=dsRunnerKernelID(f,false);if err!=nil{return snapshot,created,err};actual,good:=handlePath(h);if !good||!sameCleanPath(actual,n.paths[leaf]){return snapshot,created,errDSRunnerEvidence}
    if copyUpdateACL(current.file,f)!=nil{return snapshot,created,errDSRunnerEvidence};written,err:=f.Write(want);if err!=nil||written!=len(want)||f.Sync()!=nil||ctx.Err()!=nil{return snapshot,created,errDSRunnerEvidence};if _,err=f.Seek(0,io.SeekStart);err!=nil{return snapshot,created,err};readback,err:=io.ReadAll(io.LimitReader(f,int64(len(want))+1));defer clear(readback);if err!=nil||len(readback)!=len(want)||digest(readback)!=digest(want){return snapshot,created,errDSRunnerEvidence}
    if err=n.closeOwned(f);err!=nil{closed=true;return snapshot,created,err};closed=true
    meta,err:=n.metadata(ctx,leaf);if err!=nil||meta.snapshot.id!=id{return snapshot,created,errDSRunnerEvidence};sealed,s,err:=n.Read(ctx,leaf);defer clear(sealed);if err!=nil||s.acl!=current.snapshot.acl||len(sealed)!=len(want)||digest(sealed)!=digest(want){return snapshot,created,errDSRunnerEvidence};return s,created,nil
}
func(n *dsRunnerNativeTarget)Recheck(ctx context.Context,leaf dsRunnerLeaf,want dsRunnerSnapshot)error{
    if !n.valid(ctx){return errDSRunnerEvidence};o,err:=n.metadata(ctx,leaf);if err!=nil||o.snapshot!=want||o.sealSHA==""{return errDSRunnerEvidence};raw,s,err:=n.Read(ctx,leaf);defer clear(raw);if err!=nil||s!=want{return errDSRunnerEvidence};return nil
}
// ReplaceFile is one native replacement operation, not a compare-exchange by
// file ID. Body handles exclude competing data writes while retained. The
// replacement API requires the stage body handle to close immediately before
// its own exclusive open. The cooperative mutex does not prove quiescence
// against arbitrary same-user namespace operations, including that interval.
// A call that reports failure is not proof of an unchanged target: with no
// backup argument ReplaceFile can fail after the replaced file left its original
// name (documented for a failed rename of the replacement). The result is
// (false, error) without the not-attempted marker for every failure from the
// call onward, and the caller records it as unconfirmed.
func(n *dsRunnerNativeTarget)Publish(ctx context.Context,current,stage,backup dsRunnerSnapshot)(committed bool,resultErr error){
    if n.Recheck(ctx,dsRunnerCurrent,current)!=nil||n.Recheck(ctx,dsRunnerStaged,stage)!=nil||n.Recheck(ctx,dsRunnerSaved,backup)!=nil||ctx.Err()!=nil{return false,errors.Join(errDSRunnerPublishNotAttempted,errDSRunnerEvidence)}
    target,err:=syscall.UTF16PtrFromString(n.paths[dsRunnerCurrent]);if err!=nil{return false,errors.Join(errDSRunnerPublishNotAttempted,err)};replacement,err:=syscall.UTF16PtrFromString(n.paths[dsRunnerStaged]);if err!=nil{return false,errors.Join(errDSRunnerPublishNotAttempted,err)}
    staged:=n.objects[dsRunnerStaged];if staged==nil||staged.held==nil{return false,errors.Join(errDSRunnerPublishNotAttempted,errDSRunnerEvidence)};if err=n.closeOwned(staged.held);err!=nil{staged.held=nil;return false,errors.Join(errDSRunnerPublishNotAttempted,err)};staged.held=nil
    ok,_,callErr:=syscall.NewLazyDLL("kernel32.dll").NewProc("ReplaceFileW").Call(uintptr(unsafe.Pointer(target)),uintptr(unsafe.Pointer(replacement)),0,0,0,0);runtime.KeepAlive(target);runtime.KeepAlive(replacement);committed=ok!=0
    for _,leaf:=range []dsRunnerLeaf{dsRunnerCurrent,dsRunnerStaged}{if o:=n.objects[leaf];o!=nil{if o.held!=nil{resultErr=errors.Join(resultErr,n.closeOwned(o.held));o.held=nil};resultErr=errors.Join(resultErr,n.closeOwned(o.file));delete(n.objects,leaf)}}
    if !committed{return false,errors.Join(errDSRunnerEvidence,resultErr,callErr)};if resultErr!=nil||ctx.Err()!=nil{return true,errors.Join(errDSRunnerEvidence,resultErr,ctx.Err())};return true,nil
}
func(n *dsRunnerNativeTarget)Close()(resultErr error){
    if n==nil{return nil};if n.closed{if len(n.unclosed)>0||n.mutex!=0{return errDSRunnerCleanup};return nil};if n.lockedThread{thread,_,_:=syscall.NewLazyDLL("kernel32.dll").NewProc("GetCurrentThreadId").Call();if uint32(thread)!=n.threadID{return errDSRunnerCleanup}};n.closed=true
    defer func(){if n.lockedThread{n.lockedThread=false;runtime.UnlockOSThread()}}()
    for leaf,o:=range n.objects{if o.held!=nil{resultErr=errors.Join(resultErr,n.closeOwned(o.held));o.held=nil};resultErr=errors.Join(resultErr,n.closeOwned(o.file));delete(n.objects,leaf)}
    for i:=len(n.parents)-1;i>=0;i--{resultErr=errors.Join(resultErr,n.closeOwned(n.parents[i]))};n.parents=nil
    // A file whose close failed earlier is still referenced here. Report it now even
    // when the operation that met the failure returned another error.
    if len(n.unclosed)>0{resultErr=errors.Join(resultErr,errDSRunnerCleanup)}
    if n.mutex!=0{if n.ownsMutex{ok,_,err:=syscall.NewLazyDLL("kernel32.dll").NewProc("ReleaseMutex").Call(uintptr(n.mutex));if ok==0{resultErr=errors.Join(resultErr,errDSRunnerCleanup,err)}};if e:=syscall.CloseHandle(n.mutex);e!=nil{resultErr=errors.Join(resultErr,errDSRunnerCleanup,e)}else{n.mutex=0};n.ownsMutex=false};return
}
