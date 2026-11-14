//go:build windows

package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnslopCallerRootConsumesOnlyHeldStore(t *testing.T) {
	if root,err:=newUnslopCallerHeldRoot(nil);err==nil||root!=nil {t.Fatal("nil root became owned")}
	store:=newDurableTestStore(t)
	root,err:=newUnslopCallerHeldRoot(store);if err!=nil {t.Fatal(err)}
	if !root.current()||root.store!=store||root.heldRoot!=store.root {t.Fatal("held root was copied or detached")}
	if other,err:=newUnslopCallerHeldRoot(store);err==nil||other!=nil {t.Fatal("same store minted a second owner")}
	if _,err:=normalUnslopCallerInputs(context.Background());err!=errUnslopCallerOwnerUnavailable {t.Fatal("normal missing issuer acquired runtime",err)}
}

func TestUnslopCallerRootNamespaceCannotUseCaptureNamespace(t *testing.T) {
	for _,p:=range []string{unslopCaptureRoot,unslopCaptureRoot+"/journal",unslopCaptureRoot+"/source/journal"} {
		if unslopCallerRootNamespace(p) {t.Fatal("journal accepted capture namespace",p)}
	}
	if !unslopCallerRootNamespace(unslopSourceRoot+"/target/unslop-outcomes-v1") {t.Fatal("separate journal namespace was rejected")}
}

func TestUnslopCallerRootRefusesPrematureAndForeignRelease(t *testing.T) {
	caller,_,_,_,_:=ownedCallerFixture(t,false)
	if err:=caller.root.release(caller);err==nil||caller.root.closeAttempted||caller.root.store==nil {t.Fatal("unstarted caller released its root")}
	other:=&unslopOwnedCaller{}
	if err:=caller.root.release(other);err==nil||caller.root.closeAttempted||caller.root.store==nil {t.Fatal("foreign caller released original root")}
	caller.Begin(context.Background())
	if err:=caller.root.release(caller);err==nil||caller.root.closeAttempted||caller.root.store==nil {t.Fatal("uncollected caller released its root")}
}

func TestUnslopCallerRootRejectsSeparateWrapperForSamePhysicalDirectory(t *testing.T) {
	store:=newDurableTestStore(t)
	root,err:=newUnslopCallerHeldRoot(store);if err!=nil {t.Fatal(err)}
	defer func(){unslopCallerRoots.Lock();defer unslopCallerRoots.Unlock();delete(unslopCallerRoots.stores,store);delete(unslopCallerRoots.identities,root.identity)}()
	otherStore,err:=newUnslopDurableStore(store.root);if err!=nil {t.Fatal(err)}
	defer otherStore.Close()
	if otherStore==store||otherStore.id!=store.id {t.Fatal("fixture did not create a distinct wrapper for the same directory")}
	if other,err:=newUnslopCallerHeldRoot(otherStore);err==nil||other!=nil||!root.current() {t.Fatal("same physical directory acquired another owner")}
	independent:=newDurableTestStore(t)
	other,err:=newUnslopCallerHeldRoot(independent);if err!=nil {t.Fatal("distinct physical root was rejected",err)}
	defer func(){unslopCallerRoots.Lock();defer unslopCallerRoots.Unlock();delete(unslopCallerRoots.stores,independent);delete(unslopCallerRoots.identities,other.identity)}()
	if !other.current()||other.identity==root.identity {t.Fatal("independent root positive did not reach a distinct identity")}
}

func TestUnslopCallerMissingProviderReleasesOnlyUnusedRoot(t *testing.T) {
	root,err:=newUnslopCallerHeldRoot(newDurableTestStore(t));if err!=nil {t.Fatal(err)}
	held:=root.heldRoot
	if caller,err:=newUnslopOwnedCaller(root,nil,nil);err==nil||caller!=nil||!root.closeAttempted||root.store!=nil {t.Fatal("unused root was not released")}
	if _,err:=held.Stat();err==nil {t.Fatal("unused root handle remained open")}
	caller,_,_,_,_:=ownedCallerFixture(t,false)
	if other,err:=newUnslopOwnedCaller(caller.root,nil,nil);err==nil||other!=nil||caller.root.closeAttempted||caller.root.store==nil {t.Fatal("foreign failure closed an already claimed root")}
}


func TestUnslopCallerRootNamespaceRejectsVolumeAncestor(t *testing.T) {
	volume:=filepath.VolumeName(unslopCaptureRoot)
	if volume=="" {t.Fatal("fixed capture has no Windows volume")}
	canonicalRoot:=filepath.Clean(volume+"/")
	if !filepath.IsAbs(canonicalRoot)||len(canonicalRoot)!=len(volume)+1||!strings.EqualFold(canonicalRoot[:len(volume)],volume)||canonicalRoot[len(canonicalRoot)-1]!='\\' {t.Fatal("fixture did not obtain the canonical volume root",canonicalRoot)}
	cases:=[]struct{name,path string;separate bool}{
		{"same-volume-root",canonicalRoot,false},
		{"same-volume-root-lower",strings.ToLower(volume)+"/",false},
		{"same-volume-root-extra-separator",volume+"//",false},
		{"capture-parent",filepath.Dir(unslopCaptureRoot),false},
		{"capture-grandparent",filepath.Dir(filepath.Dir(unslopCaptureRoot)),false},
		{"capture-self",unslopCaptureRoot,false},
		{"capture-child",filepath.Join(unslopCaptureRoot,"journal"),false},
		{"sibling",filepath.Join(filepath.Dir(unslopCaptureRoot),"unslop-outcomes-v1"),true},
		{"prefix-sibling",unslopCaptureRoot+"-journal",true},
		{"same-volume-other-branch",filepath.Join(canonicalRoot,"owned-independent-journal"),true},
	}
	for _,c:=range cases {t.Run(c.name,func(t *testing.T){
		if got:=unslopCallerRootNamespace(c.path);got!=c.separate {t.Fatal("capture and journal namespace boundary mismatch",c.name,got,c.separate)}
	})}
}
