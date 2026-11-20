//go:build windows

package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

// This scope is specific to the watchdog consumer. Legacy cleanup-tool helpers
// keep their existing behavior. Every file acquired here, including rejected
// partials and temporary named pins, remains owned until a single close attempt.
type watchdogPinOperations struct {
	create func(string,uint32,uint32,uint32)(*os.File,error)
	info func(*os.File)(syscall.ByHandleFileInformation,error)
	path func(*os.File)(string,error)
	close func(*os.File)error
}

func watchdogSystemPinOperations() watchdogPinOperations {
	return watchdogPinOperations{
		create:func(path string,access,share,disposition uint32)(*os.File,error){
			h,err:=syscall.CreateFile(utf(path),access,share,nil,disposition,
				syscall.FILE_FLAG_OPEN_REPARSE_POINT|syscall.FILE_FLAG_BACKUP_SEMANTICS,0)
			if err!=nil{return nil,err}
			return os.NewFile(uintptr(h),path),nil
		},
		info:func(f *os.File)(syscall.ByHandleFileInformation,error){
			var i syscall.ByHandleFileInformation
			err:=syscall.GetFileInformationByHandle(syscall.Handle(f.Fd()),&i)
			return i,err
		},
		path:func(f *os.File)(string,error){
			name:=make([]uint16,32768)
			n,_,err:=kernel.NewProc("GetFinalPathNameByHandleW").Call(f.Fd(),
				uintptr(unsafe.Pointer(&name[0])),uintptr(len(name)),0)
			if n==0||n>=uintptr(len(name)){return "",err}
			return strings.TrimPrefix(syscall.UTF16ToString(name[:n]),`\\?\`),nil
		},
		close:func(f *os.File)error{return f.Close()},
	}
}

type watchdogPinEntry struct {
	pin *pin
	closeAttempted bool
	closeErr error
}

type watchdogPinScope struct {
	ops watchdogPinOperations
	entries []*watchdogPinEntry
	closed bool
}

func newWatchdogPinScope() *watchdogPinScope {
	return &watchdogPinScope{ops:watchdogSystemPinOperations()}
}

func (s *watchdogPinScope) closePin(p *pin) error {
	if p==nil{return nil}
	for _,entry:=range s.entries {
		if entry.pin==p {
			if !entry.closeAttempted {
				entry.closeAttempted=true
				entry.closeErr=s.ops.close(p.f)
			}
			return entry.closeErr
		}
	}
	return errors.New("watchdog_pin_not_owned")
}

func (s *watchdogPinScope) cleanupError() error {
	var es []error
	for _,entry:=range s.entries {
		es=append(es,entry.closeErr)
	}
	return errors.Join(es...)
}

func (s *watchdogPinScope) Close() error {
	if !s.closed {
		s.closed=true
		for i:=len(s.entries)-1;i>=0;i-- {
			_ = s.closePin(s.entries[i].pin)
		}
	}
	// Failed pins stay referenced. Repeated calls return facts without another
	// Close, and successful later operations cannot erase an earlier failure.
	return s.cleanupError()
}

func (s *watchdogPinScope) open(path string,access,share,disposition uint32)(*pin,error){
	if s==nil||s.closed||!filepath.IsAbs(path)||strings.ContainsAny(path,"\x00\r\n"){
		return nil,errors.New("watchdog_pin_path_or_state_invalid")
	}
	file,primary:=s.ops.create(path,access,share,disposition)
	if file==nil {
		if primary==nil{primary=errors.New("watchdog_pin_open_returned_nil")}
		return nil,primary
	}
	p:=&pin{f:file}
	s.entries=append(s.entries,&watchdogPinEntry{pin:p})
	reject:=func(err error)(*pin,error){
		return nil,errors.Join(err,s.closePin(p))
	}
	if primary!=nil{return reject(primary)}
	info,err:=s.ops.info(file)
	if err!=nil{return reject(err)}
	p.info=info
	if info.FileAttributes&0x400!=0||info.NumberOfLinks!=1{
		return reject(errors.New("watchdog_pin_link_or_identity"))
	}
	name,err:=s.ops.path(file)
	if err!=nil{return reject(err)}
	if !same(name,path){return reject(errors.New("watchdog_pin_physical_path_changed"))}
	return p,nil
}

func (s *watchdogPinScope) parents(path string)([]*pin,error){
	var names []string
	for name:=filepath.Dir(path);;name=filepath.Dir(name){
		names=append(names,name)
		if same(name,filepath.Dir(name)){break}
	}
	var result []*pin
	for i:=len(names)-1;i>=0;i--{
		p,err:=s.open(names[i],0x80,3,3)
		if err!=nil{
			// All acquired ancestors remain scope-owned. The constructor must
			// return this scope in its partial resource, not a detached nil.
			return result,err
		}
		if p.info.FileAttributes&0x10==0{
			return result,errors.Join(errors.New("watchdog_pin_ancestor_not_directory"),s.closePin(p))
		}
		result=append(result,p)
	}
	return result,nil
}

func (s *watchdogPinScope) verifyName(held *pin,path string)(primary error){
	if s==nil||s.closed||held==nil{return errors.New("watchdog_pin_held_missing")}
	owned:=false
	for _,entry:=range s.entries{if entry.pin==held&&!entry.closeAttempted{owned=true;break}}
	if !owned{return errors.New("watchdog_pin_held_not_owned")}
	named,err:=s.open(path,0x80,7,3)
	if err!=nil{return err}
	defer func(){primary=errors.Join(primary,s.closePin(named))}()
	current,err:=s.ops.info(held.f)
	if err!=nil{return err}
	physical:=func(a,b syscall.ByHandleFileInformation)bool{
		return a.VolumeSerialNumber==b.VolumeSerialNumber&&
			a.FileIndexHigh==b.FileIndexHigh&&a.FileIndexLow==b.FileIndexLow
	}
	if !physical(current,held.info)||!physical(named.info,current){
		return errors.New("watchdog_pin_public_identity_changed")
	}
	return nil
}
