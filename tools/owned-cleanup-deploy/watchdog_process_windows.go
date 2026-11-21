//go:build windows

package main

import (
	"context"
	"errors"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// Process handles are retained with query+synchronize rights only.
// No TerminateProcess, PID kill, token reassignment or privilege change exists.
type watchdogProcess struct{
	handle syscall.Handle
	pid uint32
	parent uint32
	created uint64
	image string
	closed bool
	closeErr error
}
type watchdogProcessEntry struct{
	Size uint32
	Usage uint32
	PID uint32
	Heap uintptr
	Module uint32
	Threads uint32
	Parent uint32
	Priority int32
	Flags uint32
	Exe [260]uint16
}

var watchdogRetainedNative struct{ sync.Mutex; handles []syscall.Handle }
func watchdogCloseNative(h syscall.Handle)error{
	err:=syscall.CloseHandle(h)
	if err!=nil{watchdogRetainedNative.Lock();watchdogRetainedNative.handles=append(watchdogRetainedNative.handles,h);watchdogRetainedNative.Unlock()}
	return err
}
func watchdogNativeCleanupError()error{
	watchdogRetainedNative.Lock();defer watchdogRetainedNative.Unlock()
	if len(watchdogRetainedNative.handles)!=0{return errors.New("watchdog_native_close_failed")}
	return nil
}

func watchdogProcessEntries()(rows []watchdogProcessEntry,err error){
	h,_,callErr:=kernel.NewProc("CreateToolhelp32Snapshot").Call(2,0)
	if h==uintptr(syscall.InvalidHandle){return nil,callErr}
	defer func(){err=errors.Join(err,watchdogCloseNative(syscall.Handle(h)))}()
	var item watchdogProcessEntry
	item.Size=uint32(unsafe.Sizeof(item))
	ok,_,firstErr:=kernel.NewProc("Process32FirstW").Call(h,uintptr(unsafe.Pointer(&item)))
	if ok==0{return nil,firstErr}
	for{
		rows=append(rows,item)
		item.Size=uint32(unsafe.Sizeof(item))
		ok,_,nextErr:=kernel.NewProc("Process32NextW").Call(h,uintptr(unsafe.Pointer(&item)))
		if ok==0{
			if nextErr!=syscall.Errno(18){return nil,nextErr}
			break
		}
	}
	return rows,nil
}

func openWatchdogProcess(pid uint32,parent uint32)(*watchdogProcess,error){
	if pid==0{return nil,errors.New("watchdog_process_pid_missing")}
	h,err:=syscall.OpenProcess(0x1000|0x100000,false,pid)
	if err!=nil{return nil,err}
	p:=&watchdogProcess{handle:h,pid:pid,parent:parent}
	if err=p.recheck();err!=nil{return p,err}
	image:=make([]uint16,32768);size:=uint32(len(image))
	ok,_,e:=kernel.NewProc("QueryFullProcessImageNameW").Call(uintptr(h),0,uintptr(unsafe.Pointer(&image[0])),uintptr(unsafe.Pointer(&size)))
	if ok==0||size==0||size>=uint32(len(image)){return p,e}
	p.image=syscall.UTF16ToString(image[:size])
	return p,nil
}
func (p *watchdogProcess) recheck()error{
	if p==nil||p.closed{return errors.New("watchdog_process_not_held")}
	var created,exit,k,user syscall.Filetime
	if err:=syscall.GetProcessTimes(p.handle,&created,&exit,&k,&user);err!=nil{return err}
	birth:=uint64(created.HighDateTime)<<32|uint64(created.LowDateTime)
	if birth==0||p.created!=0&&birth!=p.created{return errors.New("watchdog_process_birth_changed")}
	p.created=birth
	id,_,e:=kernel.NewProc("GetProcessId").Call(uintptr(p.handle))
	if uint32(id)!=p.pid{return errors.Join(errors.New("watchdog_process_pid_changed"),e)}
	return nil
}
func (p *watchdogProcess) live()bool{
	if p==nil||p.closed{return false}
	state,err:=syscall.WaitForSingleObject(p.handle,0)
	return err==nil&&state==258
}
func (p *watchdogProcess) wait(ctx context.Context)error{
	for{
		if ctx.Err()!=nil{return ctx.Err()}
		state,err:=syscall.WaitForSingleObject(p.handle,0)
		if err!=nil{return err}
		if state==0{return nil}
		if state!=258{return errors.New("watchdog_process_wait_unknown")}
		select{case<-ctx.Done():return ctx.Err();case<-time.After(25*time.Millisecond):}
	}
}
func (p *watchdogProcess) Close()error{
	if p==nil{return nil}
	if p.closed{return p.closeErr};p.closed=true
	p.closeErr=watchdogCloseNative(p.handle)
	return p.closeErr
}

func (p *watchdogProcess) sid()(string,error){
	var token syscall.Token
	if err:=syscall.OpenProcessToken(p.handle,syscall.TOKEN_QUERY,&token);err!=nil{return "",err}
	info,err:=token.GetTokenUser()
	var sid string
	if err==nil{sid,err=info.User.Sid.String()}
	return sid,errors.Join(err,watchdogCloseNative(syscall.Handle(token)))
}
