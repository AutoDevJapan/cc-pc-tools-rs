//go:build windows

package main

import (
	"errors"
	"os"
	"syscall"
	"testing"
)

func watchdogMutableFixture(t *testing.T,raw []byte)(*watchdogPinFixture,*uint32){
	t.Helper();v:=watchdogNewPinFixture(t);var share uint32;var data syscall.ByHandleFileInformation;base:=v.scope.ops.create
	v.scope.ops.create=func(path string,access,s,d uint32)(*os.File,error){
		f,e:=base(path,access,s,d)
		if f!=nil&&access==syscall.GENERIC_READ{share=s;i:=v.info[f];i.FileSizeLow=uint32(len(raw));v.info[f]=i;data=i}
		if f!=nil&&access==0x80&&s==7{v.info[f]=data}
		return f,e
	}
	return v,&share
}
func TestWatchdogMutableSnapshotSharesWriterAndAtomicReplaceOnlyForObservations(t *testing.T){
	for _,path:=range []string{watchdogStatusPath,watchdogEventsPath}{
		t.Run(path,func(t *testing.T){
			raw:=[]byte("{}\n");v,share:=watchdogMutableFixture(t,raw);calls:=0
			got,e:=watchdogReadMutableWith(v.scope,path,8192,func(*os.File,int64)([]byte,error){calls++;return append([]byte(nil),raw...),nil})
			if e!=nil||string(got)!=string(raw)||calls!=2||*share!=syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE{t.Fatal("mutable snapshot/share",e,calls,*share)}
			if e=v.scope.Close();e!=nil{t.Fatal(e)}
		})
	}
}
func TestWatchdogMutableSnapshotRejectsChangingOrIncompleteGeneration(t *testing.T){
	for _,kind:=range []string{"bytes","size","identity","last-write","partial-line","read-error","oversize","foreign-path","rotation"}{
		t.Run(kind,func(t *testing.T){
			raw:=[]byte("{}\n");if kind=="partial-line"{raw=[]byte("{}")}
			v,_:=watchdogMutableFixture(t,raw);calls:=0;path:=watchdogEventsPath
			if kind=="foreign-path"{path=watchdogTarget}
			if kind=="rotation"{base:=v.scope.ops.create;v.scope.ops.create=func(p string,a,b,c uint32)(*os.File,error){f,e:=base(p,a,b,c);if f!=nil&&a==0x80&&b==7{i:=v.info[f];i.FileIndexLow++;v.info[f]=i};return f,e}}
			got,e:=watchdogReadMutableWith(v.scope,path,8192,func(f *os.File,n int64)([]byte,error){
				calls++;value:=append([]byte(nil),raw...);i:=v.info[f]
				if calls==1{switch kind{case "size":i.FileSizeLow++;case "identity":i.FileIndexLow++;case "last-write":i.LastWriteTime.LowDateTime++};v.info[f]=i}
				if kind=="bytes"&&calls==2{value[0]='x'}
				if kind=="read-error"{return nil,errors.New("read")}
				if kind=="oversize"{return make([]byte,n+1),nil}
				return value,nil
			})
			if e==nil||got!=nil{t.Fatal("unverified snapshot accepted")}
			_ = v.scope.Close()
		})
	}
}
func TestWatchdogMutableSnapshotKeepsFailedCloseInOwnedScope(t *testing.T){
	raw:=[]byte("{}\n");v,_:=watchdogMutableFixture(t,raw)
	_,e:=watchdogReadMutableWith(v.scope,watchdogStatusPath,8192,func(*os.File,int64)([]byte,error){return raw,nil})
	if e!=nil{t.Fatal(e)};v.closeFailAt=len(v.files)-1
	if !errors.Is(v.scope.Close(),v.cleanup)||!errors.Is(v.scope.Close(),v.cleanup){t.Fatal("mutable close failed fact lost")}
	for _,f:=range v.files{if v.counts[f]!=1{t.Fatal("close retried")}}
}
