//go:build windows

package main

import (
	"context"
	"errors"
	"io"
	"os"
	"reflect"
	"syscall"
	"testing"
)

func watchdogOwnedFixture(t *testing.T) (*watchdogNative, leaf) {
	t.Helper()
	l:=fixture(t)
	old,e:=openPin(l.Target,syscall.GENERIC_READ|0x20000,1,3)
	if e!=nil{t.Fatal(e)}
	id,e:=watchdogIdentity(old);ce:=old.f.Close()
	if e!=nil||ce!=nil{t.Fatal("fixture old identity",e,ce)}
	artifact,e:=openPin(l.Source,syscall.GENERIC_READ|0x20000,1,3)
	if e!=nil{t.Fatal(e)}
	candidate,e:=watchdogIdentity(artifact);ce=artifact.f.Close()
	if e!=nil||ce!=nil{t.Fatal("fixture artifact identity",e,ce)}
	origin:=watchdogTestOrigin();origin.Installed=id
	n,e:=prepareWatchdogNativeAt(context.Background(),l.Target,origin,candidate,l.Source)
	if n!=nil{t.Cleanup(func(){if e:=n.Close();e!=nil{t.Error("owned cleanup",e)}})}
	if e!=nil{t.Fatal(e)}
	return n,l
}

func watchdogOwnedRead(t *testing.T,p *pin,want string){
	t.Helper()
	if _,e:=p.f.Seek(0,0);e!=nil{t.Fatal(e)}
	b,e:=io.ReadAll(p.f)
	if e!=nil||string(b)!=want{t.Fatalf("held bytes: %q %v",b,e)}
}

func TestWatchdogNativePublishesSameArtifactSecurityAndRetainsOldObject(t *testing.T){
	n,l:=watchdogOwnedFixture(t);ctx:=context.Background()
	oldID:=n.origin.Installed;security:=descriptor(t,n.old)
	if e:=n.Stage(ctx);e!=nil{t.Fatal(e)}
	if n.stageWriter==nil||!n.stageWriterCloseAttempted{t.Fatal("write handle not sealed")}
	if e:=n.Publish(ctx);e!=nil{t.Fatal(e)}
	if !n.state.Published||!n.state.Verified||n.stage.verifyName(l.Target)!=nil||n.old.verifyName(n.backup())!=nil{
		t.Fatal("publish evidence",n.state)
	}
	now,e:=watchdogIdentity(n.old)
	if e!=nil||!watchdogSameHeld(now,oldID){t.Fatal("old object not retained")}
	if !reflect.DeepEqual(security,descriptor(t,n.stage)){t.Fatal("security changed")}
	watchdogOwnedRead(t,n.stage,"new verified bytes")
	watchdogOwnedRead(t,n.old,"old")
}

func TestWatchdogNativePostpublishRollbackPreservesBothBodies(t *testing.T){
	n,l:=watchdogOwnedFixture(t);ctx:=context.Background()
	if e:=n.Stage(ctx);e!=nil{t.Fatal(e)}
	if e:=n.Publish(ctx);e!=nil{t.Fatal(e)}
	if e:=n.Rollback(ctx);e!=nil{t.Fatal(e)}
	if !n.state.Published||!n.state.Restored||!n.state.CandidateRetired||
		n.old.verifyName(l.Target)!=nil||n.stage.verifyName(n.failed())!=nil{
		t.Fatal("rollback rewrote history",n.state)
	}
	watchdogOwnedRead(t,n.old,"old")
	watchdogOwnedRead(t,n.stage,"new verified bytes")
}

func TestWatchdogNativeRestoreAfterPublishCollisionNeverOverwritesForeignName(t *testing.T){
	n,l:=watchdogOwnedFixture(t);ctx:=context.Background()
	if e:=n.Stage(ctx);e!=nil{t.Fatal(e)}
	original:=n.rename
	n.rename=func(p *pin,dest string)error{
		if p==n.stage{
			if e:=os.WriteFile(dest,[]byte("foreign"),0600);e!=nil{return e}
			return errors.New("publish collision")
		}
		return original(p,dest)
	}
	if e:=n.Publish(ctx);e==nil{t.Fatal("collision accepted")}
	if e:=n.Rollback(ctx);e==nil{t.Fatal("overwrote concurrent public name")}
	if n.state.Restored{t.Fatal("false restored")}
	content(t,l.Target,"foreign")
	watchdogOwnedRead(t,n.old,"old")
	watchdogOwnedRead(t,n.stage,"new verified bytes")
}

func TestWatchdogNativeAbsentPublicAfterPublishFailureRestoresOld(t *testing.T){
	n,l:=watchdogOwnedFixture(t);ctx:=context.Background()
	if e:=n.Stage(ctx);e!=nil{t.Fatal(e)}
	original:=n.rename
	n.rename=func(p *pin,dest string)error{
		if p==n.stage{return errors.New("publish failed before mutation")}
		return original(p,dest)
	}
	if e:=n.Publish(ctx);e==nil{t.Fatal("fault not reached")}
	if e:=n.Rollback(ctx);e!=nil{t.Fatal(e)}
	if n.state.Published||!n.state.Restored||n.old.verifyName(l.Target)!=nil{
		t.Fatal("prepublish recovery",n.state)
	}
	watchdogOwnedRead(t,n.old,"old")
}

func TestWatchdogNativeFailedLeafCollisionRetainsPublishedGeneration(t *testing.T){
	n,l:=watchdogOwnedFixture(t);ctx:=context.Background()
	if e:=n.Stage(ctx);e!=nil{t.Fatal(e)}
	if e:=n.Publish(ctx);e!=nil{t.Fatal(e)}
	if e:=os.WriteFile(n.failed(),[]byte("foreign failed leaf"),0600);e!=nil{t.Fatal(e)}
	if e:=n.Rollback(ctx);e==nil{t.Fatal("failed-name collision accepted")}
	if n.state.Restored||n.state.CandidateRetired||n.stage.verifyName(l.Target)!=nil{
		t.Fatal("published object lost")
	}
	content(t,n.failed(),"foreign failed leaf")
}

func TestWatchdogNativeUnknownOriginRejectedBeforeInstalledOpen(t *testing.T){
	origin:=watchdogTestOrigin();origin.InstallReceiptSHA256=""
	n,e:=prepareWatchdogNative(context.Background(),origin,watchdogTestID(),"not-opened")
	if n!=nil||e==nil{t.Fatal("unknown origin opened target")}
}

func TestWatchdogNativeRefusesChangedOriginAndChangedArtifact(t *testing.T){
	for _,kind:=range []string{"origin-hash","origin-created","artifact-hash","artifact-file"}{
		t.Run(kind,func(t *testing.T){
			n,_:=watchdogOwnedFixture(t)
			switch kind{
			case "origin-hash":n.origin.Installed.SHA256=string(make([]byte,64))
			case "origin-created":n.origin.Installed.Created++
			case "artifact-hash":n.expected.SHA256=string(make([]byte,64))
			case "artifact-file":n.expected.File++
			}
			if e:=n.Stage(context.Background());e==nil||n.state.StageCreated{
				t.Fatal("changed generation created stage")
			}
		})
	}
}

func TestWatchdogDeferredBaselineCanHandoffToStoppedTransaction(t *testing.T){
	l:=fixture(t);ctx:=context.Background()
	old,e:=openPin(l.Target,syscall.GENERIC_READ|0x20000,5,3);if e!=nil{t.Fatal(e)}
	id,e:=watchdogIdentity(old);ce:=old.f.Close();if e!=nil||ce!=nil{t.Fatal(e,ce)}
	candidate,e:=openPin(l.Source,syscall.GENERIC_READ|0x20000,1,3);if e!=nil{t.Fatal(e)}
	artifact,e:=watchdogIdentity(candidate);ce=candidate.f.Close();if e!=nil||ce!=nil{t.Fatal(e,ce)}
	origin:=watchdogTestOrigin();origin.Installed=id
	d:=&watchdogDeferredNative{target:l.Target,source:l.Source,origin:origin,artifact:artifact}
	defer func(){if e:=d.Close();e!=nil{t.Error(e)}}()
	if e:=d.Check(ctx);e!=nil{t.Fatal(e)}
	if d.transaction!=nil||d.baseline==nil{t.Fatal("Check acquired writable transaction")}
	if e:=d.Stage(ctx);e!=nil{t.Fatal(e)}
	if e:=d.Publish(ctx);e!=nil{t.Fatal(e)}
	if d.transaction.old.info.FileIndexLow!=d.baseline.info.FileIndexLow||
		d.transaction.old.info.FileIndexHigh!=d.baseline.info.FileIndexHigh{
		t.Fatal("baseline not the stopped object")
	}
	if e:=d.Rollback(ctx);e!=nil{t.Fatal(e)}
}

func TestWatchdogNativeCancelledAfterRetirementStillHasSameHeldOld(t *testing.T){
	n,l:=watchdogOwnedFixture(t);ctx,cancel:=context.WithCancel(context.Background());defer cancel()
	if e:=n.Stage(ctx);e!=nil{t.Fatal(e)}
	original:=n.rename
	n.rename=func(p *pin,dest string)error{
		e:=original(p,dest)
		if p==n.old&&e==nil{cancel()}
		return e
	}
	if e:=n.Publish(ctx);!errors.Is(e,context.Canceled){t.Fatalf("cancel cause: %v",e)}
	if !n.state.OldRetired||n.state.Published{t.Fatal("phase facts")}
	if e:=n.Rollback(context.Background());e!=nil{t.Fatal(e)}
	if n.old.verifyName(l.Target)!=nil{t.Fatal("restoration not same held old")}
}
