//go:build windows

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
)

var errUnslopCallerOwnerUnavailable = errors.New("capture_owner_unavailable")
var errUnslopCallerRoot = errors.New("capture_root_unavailable")

// The registry records ownership of already-held storage objects. It does not
// qualify a path, open a directory, or replace the normal purpose issuer.
var unslopCallerRoots = struct {
	sync.Mutex
	stores map[*unslopDurableStore]*unslopCallerHeldRoot
	identities map[softwareIdentity]*unslopCallerHeldRoot
}{stores:make(map[*unslopDurableStore]*unslopCallerHeldRoot),identities:make(map[softwareIdentity]*unslopCallerHeldRoot)}

type unslopCallerHeldRoot struct {
	store *unslopDurableStore
	heldRoot *os.File
	heldParents []syscall.Handle
	identity softwareIdentity
	caller *unslopOwnedCaller
	closeAttempted bool
	closeError error
}

func unslopCallerRootNamespace(path string) bool {
	clean:=func(p string)string{return strings.ToLower(strings.ReplaceAll(filepath.Clean(p),"\\","/"))}
	journal,capture:=clean(path),clean(unslopCaptureRoot)
	boundary:=func(parent string)string{return strings.TrimSuffix(parent,"/")+"/"}
	return journal!=""&&journal!="."&&journal!=capture&&!strings.HasPrefix(journal,boundary(capture))&&!strings.HasPrefix(capture,boundary(journal))
}

// Only an internal acquisition flow may adopt an actual qualified held store.
// This constructor is also used by synthetic tests; it is not a production
// purpose qualification and accepts no caller path, digest, or authority flag.
func newUnslopCallerHeldRoot(store *unslopDurableStore) (*unslopCallerHeldRoot,error) {
	if store==nil||!store.current()||!unslopCallerRootNamespace(store.path) {return nil,errUnslopCallerRoot}
	unslopCallerRoots.Lock();defer unslopCallerRoots.Unlock()
	if unslopCallerRoots.stores[store]!=nil||unslopCallerRoots.identities[store.id]!=nil {return nil,errUnslopCallerRoot}
	root:=&unslopCallerHeldRoot{store:store,heldRoot:store.root,identity:store.id,heldParents:append([]syscall.Handle(nil),store.parents...)}
	unslopCallerRoots.stores[store]=root;unslopCallerRoots.identities[store.id]=root
	return root,nil
}

func (r *unslopCallerHeldRoot) currentLocked() bool {
	return r!=nil&&r.store!=nil&&r.heldRoot!=nil&&!r.closeAttempted&&unslopCallerRoots.stores[r.store]==r&&unslopCallerRoots.identities[r.identity]==r&&r.store.root==r.heldRoot&&r.store.id==r.identity&&r.store.current()
}

func (r *unslopCallerHeldRoot) current() bool {
	unslopCallerRoots.Lock();defer unslopCallerRoots.Unlock()
	return r.currentLocked()
}

func (r *unslopCallerHeldRoot) owns(caller *unslopOwnedCaller) bool {
	unslopCallerRoots.Lock();defer unslopCallerRoots.Unlock()
	return r!=nil&&caller!=nil&&r.caller==caller&&caller.root==r&&r.store!=nil&&unslopCallerRoots.stores[r.store]==r&&unslopCallerRoots.identities[r.identity]==r
}

func (r *unslopCallerHeldRoot) claim(caller *unslopOwnedCaller) bool {
	unslopCallerRoots.Lock();defer unslopCallerRoots.Unlock()
	if !r.currentLocked()||r.caller!=nil||caller==nil||caller.root!=r {return false}
	r.caller=caller
	return true
}

// The caller lock is held. Root release follows the same capture's saved final
// and completed input cleanup; a caller-supplied success flag cannot release it.
func (r *unslopCallerHeldRoot) release(caller *unslopOwnedCaller) error {
	unslopCallerRoots.Lock()
	if r==nil||caller==nil||r.caller!=caller||caller.root!=r||!caller.started||!caller.capture.ResultSaved||caller.capture.OwnedInputsRetained||!caller.inputsReleased {unslopCallerRoots.Unlock();return errUnslopCallerRoot}
	if r.closeAttempted {err:=r.closeError;unslopCallerRoots.Unlock();return err}
	if !r.currentLocked(){unslopCallerRoots.Unlock();return errUnslopCallerRoot}
	r.closeAttempted=true;store:=r.store
	unslopCallerRoots.Unlock()
	err:=store.Close()
	unslopCallerRoots.Lock();defer unslopCallerRoots.Unlock()
	r.closeError=err
	if err==nil {delete(unslopCallerRoots.stores,store);delete(unslopCallerRoots.identities,r.identity);r.store=nil;r.heldRoot=nil;r.heldParents=nil}
	return err
}

type unslopCallerInputs struct {
	root *unslopCallerHeldRoot
	provider *softwareRustOriginProvider
	factory unslopToolchainCaptureFactory
}

// The normal root/provider/supervisor issuer is not installed. Metadata,
// historical hashes and this ownership adapter cannot enable that runtime.
func normalUnslopCallerInputs(ctx context.Context) (*unslopCallerInputs,error) {
	if ctx==nil {return nil,errUnslopCallerOwnerUnavailable}
	if err:=ctx.Err();err!=nil {return nil,err}
	return nil,errUnslopCallerOwnerUnavailable
}

// Abandon applies only before the ownership claim. A failed constructor cannot
// close a root that another caller already claimed, or retry a failed Close.
func (r *unslopCallerHeldRoot) abandon() error {
	unslopCallerRoots.Lock()
	if r==nil||r.caller!=nil||!r.currentLocked(){unslopCallerRoots.Unlock();return errUnslopCallerRoot}
	r.closeAttempted=true;store:=r.store
	unslopCallerRoots.Unlock()
	err:=store.Close()
	unslopCallerRoots.Lock();defer unslopCallerRoots.Unlock()
	r.closeError=err
	if err==nil {delete(unslopCallerRoots.stores,store);delete(unslopCallerRoots.identities,r.identity);r.store=nil;r.heldRoot=nil;r.heldParents=nil}
	return err
}
