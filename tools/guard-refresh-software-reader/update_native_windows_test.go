//go:build windows

package main

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"unsafe"
)

func TestUpdateNativeHeldCreateNewSameParentACLReadbackAndCollision(t *testing.T) {
	root := physicalSoftwareFixture(t)
	oldPath := filepath.Join(root, "old.exe")
	if e := os.WriteFile(oldPath, []byte("owned-old"), 0600); e != nil {
		t.Fatal(e)
	}
	value, e := openUpdateObject(oldPath)
	if e != nil {
		t.Fatal(e)
	}
	defer value.Close()
	o := value.(*nativeUpdateObject)
	acl, e := o.ACL()
	if e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(root, "stage.exe")
	if _, e = o.CreateNew(path, []byte("new"), acl); e == nil {
		t.Fatal("production operation accepted arbitrary test leaf")
	}
	id, e := o.createNewSibling(path, []byte("owned-new"), acl)
	if e != nil {
		if observed, oe := openUpdateObject(path); oe == nil {
			defer observed.Close()
			actual, _ := observed.ACL()
			t.Log("oldACLHash", digest([]byte(acl)), "createdACLHash", digest([]byte(actual)), "identity", observed.ID())
		}
		t.Fatal("owned CreateNew/readback", e)
	}
	if id.FileID == "" {
		t.Fatal("identity missing")
	}
	if _, e = o.createNewSibling(path, []byte("replacement"), acl); e == nil {
		t.Fatal("existing output overwritten")
	}
	if _, e = o.createNewSibling(filepath.Join(root, "other", "new.exe"), []byte("new"), acl); e == nil {
		t.Fatal("other parent admitted")
	}
	if _, e = o.createNewSibling(filepath.Join(root, "wrong-acl.exe"), []byte("new"), "different-acl"); e == nil {
		t.Fatal("unverified ACL admitted")
	}
	got, e := openUpdateObject(path)
	if e != nil {
		t.Fatal(e)
	}
	defer got.Close()
	actualACL, e := got.ACL()
	if e != nil || actualACL != acl || string(got.Raw()) != "owned-new" || got.ID() != id {
		t.Fatal("named created object differs")
	}
	if e := os.WriteFile(oldPath, []byte("swap"), 0600); e == nil {
		t.Fatal("held old bytes not pinned")
	}
}

type updateACLMetadata struct {
	Control   uint16
	ACEs      uint16
	Inherited uint16
	Hash      string
}

func testUpdateACLMetadata(t *testing.T, f *os.File) updateACLMetadata {
	t.Helper()
	var sd uintptr
	var dacl unsafe.Pointer
	c, _, _ := updateGetSecurity.Call(f.Fd(), 1, 7, 0, 0, uintptr(unsafe.Pointer(&dacl)), 0, uintptr(unsafe.Pointer(&sd)))
	if c != 0 || sd == 0 || dacl == nil {
		t.Fatal("owned ACL metadata capture")
	}
	defer updateLocalFree.Call(sd)
	var control uint16
	var rev uint32
	ok, _, _ := updateSDControl.Call(sd, uintptr(unsafe.Pointer(&control)), uintptr(unsafe.Pointer(&rev)))
	if ok == 0 {
		t.Fatal("control capture")
	}
	count := *(*uint16)(unsafe.Add(dacl, 4))
	var inherited uint16
	for i := uint16(0); i < count; i++ {
		var ace unsafe.Pointer
		ok, _, _ := updateAdvapi.NewProc("GetAce").Call(uintptr(dacl), uintptr(i), uintptr(unsafe.Pointer(&ace)))
		if ok == 0 || ace == nil {
			t.Fatal("ACE metadata")
		}
		if *(*byte)(unsafe.Add(ace, 1))&0x10 != 0 {
			inherited++
		}
	}
	s, e := updateFileACL(f)
	if e != nil {
		t.Fatal(e)
	}
	return updateACLMetadata{control, count, inherited, digest([]byte(s))}
}

func TestUpdateACLCreateDescriptorInheritanceThenExactHandleCopy(t *testing.T) {
	root := physicalSoftwareFixture(t)
	path := filepath.Join(root, "old.exe")
	if e := os.WriteFile(path, []byte("owned"), 0600); e != nil {
		t.Fatal(e)
	}
	obj, e := openUpdateObject(path)
	if e != nil {
		t.Fatal(e)
	}
	defer obj.Close()
	o := obj.(*nativeUpdateObject)
	acl, e := o.ACL()
	if e != nil {
		t.Fatal(e)
	}
	text, e := syscall.UTF16PtrFromString(acl)
	if e != nil {
		t.Fatal(e)
	}
	var sd uintptr
	ok, _, _ := updateStringSD.Call(uintptr(unsafe.Pointer(text)), 1, uintptr(unsafe.Pointer(&sd)), 0)
	if ok == 0 {
		t.Fatal("descriptor conversion")
	}
	defer updateLocalFree.Call(sd)
	sa := syscall.SecurityAttributes{Length: uint32(unsafe.Sizeof(syscall.SecurityAttributes{})), SecurityDescriptor: sd}
	ptr, e := syscall.UTF16PtrFromString(filepath.Join(root, "created.exe"))
	if e != nil {
		t.Fatal(e)
	}
	h, e := syscall.CreateFile(ptr, syscall.GENERIC_READ|syscall.GENERIC_WRITE|0xe0000, syscall.FILE_SHARE_READ, &sa, syscall.CREATE_NEW, 0x00200000, 0)
	if e != nil {
		t.Fatal(e)
	}
	f := os.NewFile(uintptr(h), "owned-created")
	defer f.Close()
	old := testUpdateACLMetadata(t, o.f)
	created := testUpdateACLMetadata(t, f)
	if e := copyUpdateACL(o.f, f); e != nil {
		t.Fatal(e)
	}
	corrected := testUpdateACLMetadata(t, f)
	if old.Hash != corrected.Hash {
		t.Fatal("held descriptor did not restore exact ACL")
	}
	t.Logf("GetSecurityInfoFlags=7 CreateNewSDDLHash=%s old=%+v creation=%+v afterHandleSet=%+v", digest([]byte(acl)), old, created, corrected)
}
