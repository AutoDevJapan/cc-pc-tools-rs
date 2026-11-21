//go:build windows

package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"syscall"
)

const watchdogStatusPath = "D:/CC/Tools/ocx-health-watchdog/status.json"
const watchdogEventsPath = "D:/CC/Tools/ocx-health-watchdog/events.jsonl"

// Only mutable observations use read/write/delete sharing. Installed executable
// and artifact pins retain their original exclusion and purpose requirements.
// An atomic rename may leave this handle on a prior complete generation; its
// timestamp/PID remain observation data, never current ownership or admission.
func watchdogReadMutable(scope *watchdogPinScope,path string,limit int64) ([]byte,error) {
	return watchdogReadMutableWith(scope,path,limit,func(f *os.File,n int64)([]byte,error){
		if _,e:=f.Seek(0,io.SeekStart);e!=nil{return nil,e}
		return io.ReadAll(io.LimitReader(f,n+1))
	})
}
func watchdogReadMutableWith(scope *watchdogPinScope,path string,limit int64,read func(*os.File,int64)([]byte,error)) ([]byte,error) {
	if scope==nil||read==nil||limit<1||limit>6<<20||
		(path!=watchdogStatusPath&&path!=watchdogEventsPath) { return nil,errors.New("watchdog_mutable_scope_invalid") }
	if _,e:=scope.parents(path);e!=nil{return nil,e}
	p,e:=scope.open(path,syscall.GENERIC_READ,syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE,syscall.OPEN_EXISTING)
	if e!=nil{return nil,e}
	before,e:=scope.ops.info(p.f)
	if e!=nil{return nil,e}
	valid:=func(i syscall.ByHandleFileInformation)bool{
		return i.FileAttributes&(0x10|0x400)==0&&i.NumberOfLinks==1&&i.FileSizeHigh==0&&int64(i.FileSizeLow)<=limit
	}
	if !valid(before){return nil,errors.New("watchdog_mutable_metadata_unverified")}
	a,e:=read(p.f,limit)
	if e!=nil{return nil,e}
	middle,e:=scope.ops.info(p.f)
	if e!=nil{return nil,e}
	b,e:=read(p.f,limit)
	if e!=nil{return nil,e}
	after,e:=scope.ops.info(p.f)
	if e!=nil{return nil,e}
	// Compare physical generation, creation/last-write/size and complete bytes.
	// Access-time changes are irrelevant. Concurrent append/overwrite, partial
	// last lines, bounds or inconsistent metadata stay unknown, never success.
	stable:=func(x,y syscall.ByHandleFileInformation)bool{
		return valid(x)&&valid(y)&&x.VolumeSerialNumber==y.VolumeSerialNumber&&
			x.FileIndexHigh==y.FileIndexHigh&&x.FileIndexLow==y.FileIndexLow&&x.CreationTime==y.CreationTime&&
			x.LastWriteTime==y.LastWriteTime&&x.FileSizeHigh==y.FileSizeHigh&&x.FileSizeLow==y.FileSizeLow
	}
	if !stable(p.info,before)||!stable(before,middle)||!stable(middle,after)||
		len(a)>int(limit)||len(a)!=int(before.FileSizeLow)||!bytes.Equal(a,b) {
		return nil,errors.New("watchdog_mutable_snapshot_unknown")
	}
	if path==watchdogEventsPath&&(len(a)==0||a[len(a)-1]!='\n'){
		return nil,errors.New("watchdog_events_incomplete")
	}
	if e=scope.verifyName(p,path);e!=nil{return nil,errors.Join(errors.New("watchdog_mutable_named_generation_unknown"),e)}
	return a,nil
}
