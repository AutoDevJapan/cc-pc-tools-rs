//go:build windows

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

type watchdogPinFixture struct {
	scope *watchdogPinScope
	root string
	creates int
	createFailAt int
	partialAt int
	metadataFailAt int
	pathFailAt int
	linkAt int
	reparseAt int
	closeFailAt int
	files []*os.File
	info map[*os.File]syscall.ByHandleFileInformation
	paths map[*os.File]string
	counts map[*os.File]int
	primary error
	cleanup error
}

func watchdogNewPinFixture(t *testing.T) *watchdogPinFixture {
	t.Helper()
	v:=&watchdogPinFixture{
		root:t.TempDir(),info:map[*os.File]syscall.ByHandleFileInformation{},
		paths:map[*os.File]string{},counts:map[*os.File]int{},
		primary:errors.New("fixture-primary"),cleanup:errors.New("fixture-close"),
	}
	v.scope=&watchdogPinScope{ops:watchdogPinOperations{
		create:func(path string,access,share,disposition uint32)(*os.File,error){
			v.creates++
			if v.creates==v.createFailAt{return nil,v.primary}
			f,e:=os.CreateTemp(v.root,"owned-pin-")
			if e!=nil{return nil,e}
			v.files=append(v.files,f);v.paths[f]=path
			i:=syscall.ByHandleFileInformation{VolumeSerialNumber:1,FileIndexLow:uint32(v.creates),
				CreationTime:syscall.Filetime{LowDateTime:1},NumberOfLinks:1}
			if access==0x80&&share==3{i.FileAttributes=0x10}
			if v.creates==v.linkAt{i.NumberOfLinks=2}
			if v.creates==v.reparseAt{i.FileAttributes|=0x400}
			v.info[f]=i
			if v.creates==v.partialAt{return f,v.primary}
			return f,nil
		},
		info:func(f *os.File)(syscall.ByHandleFileInformation,error){
			for i,item:=range v.files{if item==f&&i+1==v.metadataFailAt{return syscall.ByHandleFileInformation{},v.primary}}
			return v.info[f],nil
		},
		path:func(f *os.File)(string,error){
			for i,item:=range v.files{if item==f&&i+1==v.pathFailAt{return "",v.primary}}
			return v.paths[f],nil
		},
		close:func(f *os.File)error{
			v.counts[f]++
			for i,item:=range v.files{if item==f&&i+1==v.closeFailAt{return v.cleanup}}
			return f.Close()
		},
	}}
	t.Cleanup(func(){for _,f:=range v.files{_ = f.Close()}})
	return v
}

func TestWatchdogPinPartialAcquisitionKeepsPrimaryAndCleanupOnce(t *testing.T){
	for _,kind:=range []string{"partial","metadata","path","link","reparse"}{
		t.Run(kind,func(t *testing.T){
			v:=watchdogNewPinFixture(t);v.closeFailAt=1
			switch kind{
			case "partial":v.partialAt=1
			case "metadata":v.metadataFailAt=1
			case "path":v.pathFailAt=1
			case "link":v.linkAt=1
			case "reparse":v.reparseAt=1
			}
			p,e:=v.scope.open(filepath.Join(v.root,"target"),1,1,3)
			if p!=nil||e==nil||!errors.Is(e,v.cleanup)||len(v.scope.entries)!=1||v.scope.entries[0].pin.f!=v.files[0]{
				t.Fatalf("lost rejected partial: %v",e)
			}
			if (kind=="partial"||kind=="metadata"||kind=="path")&&!errors.Is(e,v.primary){t.Fatal("primary lost")}
			if !errors.Is(v.scope.Close(),v.cleanup)||!errors.Is(v.scope.Close(),v.cleanup)||v.counts[v.files[0]]!=1{
				t.Fatal("close lost or retried")
			}
		})
	}
}

func TestWatchdogPinAncestorFailureRetainsEarlierPinsForOuterClose(t *testing.T){
	for _,failClose:=range []bool{false,true}{
		t.Run(map[bool]string{false:"normal-close",true:"failed-close"}[failClose],func(t *testing.T){
			v:=watchdogNewPinFixture(t);v.createFailAt=2
			if failClose{v.closeFailAt=1}
			pins,e:=v.scope.parents(filepath.Join(v.root,"a","b","target.exe"))
			if !errors.Is(e,v.primary)||len(pins)!=1||len(v.scope.entries)!=1||v.counts[v.files[0]]!=0{
				t.Fatal("partial ancestors disappeared",e)
			}
			closed:=v.scope.Close();again:=v.scope.Close()
			if failClose&&(!errors.Is(closed,v.cleanup)||!errors.Is(again,v.cleanup)){t.Fatal("ancestor close discarded")}
			if !failClose&&(closed!=nil||again!=nil){t.Fatal("normal ancestor close failed")}
			if v.counts[v.files[0]]!=1{t.Fatal("ancestor closed twice")}
		})
	}
}

func TestWatchdogNamedVerificationCloseFailureCannotReturnSuccess(t *testing.T){
	for _,kind:=range []string{"normal","close","identity-and-close","metadata-and-close"}{
		t.Run(kind,func(t *testing.T){
			v:=watchdogNewPinFixture(t);target:=filepath.Join(v.root,"target")
			held,e:=v.scope.open(target,1,1,3);if e!=nil{t.Fatal(e)}
			if kind!="normal"{v.closeFailAt=2}
			baseCreate:=v.scope.ops.create
			v.scope.ops.create=func(path string,a,b,c uint32)(*os.File,error){
				f,e:=baseCreate(path,a,b,c)
				if f!=nil{
					i:=v.info[f];i.FileIndexLow=held.info.FileIndexLow;v.info[f]=i
					if kind=="identity-and-close"{i.FileIndexLow++;v.info[f]=i}
				}
				return f,e
			}
			if kind=="metadata-and-close"{v.metadataFailAt=2}
			e=v.scope.verifyName(held,target)
			if kind=="normal"&&e!=nil{t.Fatal(e)}
			if kind!="normal"&&(e==nil||!errors.Is(e,v.cleanup)){t.Fatalf("temporary close became success: %v",e)}
			closeErr:=v.scope.Close()
			if kind!="normal"&&!errors.Is(closeErr,v.cleanup){t.Fatal("outer cleanup lost temporary")}
			_ = v.scope.Close()
			for _,f:=range v.files{if v.counts[f]!=1{t.Fatal("temporary close retried",v.counts)}}
		})
	}
}

func TestWatchdogPinCleanupFailureReachesControllerReconciliation(t *testing.T){
	v:=watchdogNewPinFixture(t);v.partialAt=1;v.closeFailAt=1
	_,primary:=v.scope.open(filepath.Join(v.root,"target"),1,1,3)
	native:=&watchdogNative{pins:v.scope}
	resources,_,resident:=watchdogTestResources();resources.Files=native
	result:=runWatchdogUpdate(context.Background(),true,func(context.Context)(*watchdogUpdateResources,error){
		return resources,primary
	})
	if result.Qualified||!result.CleanupFailed||!result.ReconciliationRequired||
		result.retained!=resources||resident.closeCount!=1||v.counts[v.files[0]]!=1{
		t.Fatalf("outer lost acquisition cleanup: %+v",result)
	}
}

func TestWatchdogPinScopeRefusesReopenAfterCloseAndKeepsFailedReferences(t *testing.T){
	v:=watchdogNewPinFixture(t);v.closeFailAt=1
	p,e:=v.scope.open(filepath.Join(v.root,"target"),1,1,3);if e!=nil{t.Fatal(e)}
	if !errors.Is(v.scope.Close(),v.cleanup){t.Fatal("close failure missing")}
	if _,e=v.scope.open(filepath.Join(v.root,"other"),1,1,3);e==nil{t.Fatal("reopened closed scope")}
	if v.creates!=1||v.scope.entries[0].pin!=p||v.scope.entries[0].pin.f!=v.files[0]||v.counts[v.files[0]]!=1{
		t.Fatal("failed resource not retained")
	}
	if !strings.Contains(v.scope.Close().Error(),"fixture-close"){t.Fatal("sticky close fact lost")}
}
