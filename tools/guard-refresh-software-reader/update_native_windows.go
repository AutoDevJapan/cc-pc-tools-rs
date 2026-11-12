//go:build windows

package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"unsafe"
)

var updateAdvapi = syscall.NewLazyDLL("advapi32.dll")
var updateGetSecurity = updateAdvapi.NewProc("GetSecurityInfo")
var updateSDString = updateAdvapi.NewProc("ConvertSecurityDescriptorToStringSecurityDescriptorW")
var updateStringSD = updateAdvapi.NewProc("ConvertStringSecurityDescriptorToSecurityDescriptorW")
var updateSetSecurity = updateAdvapi.NewProc("SetSecurityInfo")
var updateSDControl = updateAdvapi.NewProc("GetSecurityDescriptorControl")
var updateLocalFree = syscall.NewLazyDLL("kernel32.dll").NewProc("LocalFree")
var errUpdateObject = errors.New("software_object_unavailable")

type nativeUpdateObject struct {
	f       *os.File
	parents []syscall.Handle
	raw     []byte
	id      softwareIdentity
}

func fileSoftwareIdentity(f *os.File) (softwareIdentity, error) {
	var info syscall.ByHandleFileInformation
	if syscall.GetFileInformationByHandle(syscall.Handle(f.Fd()), &info) != nil || info.NumberOfLinks != 1 || info.FileAttributes&(syscall.FILE_ATTRIBUTE_REPARSE_POINT|syscall.FILE_ATTRIBUTE_DIRECTORY) != 0 {
		return softwareIdentity{}, errUpdateObject
	}
	return softwareIdentity{fmt.Sprint(info.VolumeSerialNumber), fmt.Sprint(uint64(info.FileIndexHigh)<<32 | uint64(info.FileIndexLow)), fmt.Sprint(uint64(info.CreationTime.HighDateTime)<<32 | uint64(info.CreationTime.LowDateTime))}, nil
}

func openUpdateObject(path string) (updateObject, error) {
	o := &nativeUpdateObject{}
	ok := false
	defer func() {
		if !ok {
			o.Close()
		}
	}()
	var parents []string
	for p := filepath.Dir(path); ; p = filepath.Dir(p) {
		parents = append(parents, p)
		if filepath.Dir(p) == p {
			break
		}
	}
	for i := len(parents) - 1; i >= 0; i-- {
		h, good := openPlain(parents[i], true)
		if !good {
			return nil, errUpdateObject
		}
		o.parents = append(o.parents, h)
	}
	h, good := openPlain(path, false)
	if !good {
		return nil, errUpdateObject
	}
	o.f = os.NewFile(uintptr(h), path)
	stat, e := o.f.Stat()
	if e != nil || stat.Size() <= 0 || stat.Size() > 64<<20 {
		return nil, errUpdateObject
	}
	o.raw, e = io.ReadAll(io.LimitReader(o.f, 64<<20+1))
	if e != nil || int64(len(o.raw)) != stat.Size() {
		return nil, errUpdateObject
	}
	o.id, e = fileSoftwareIdentity(o.f)
	if e != nil {
		return nil, e
	}
	ok = true
	return o, nil
}

func (o *nativeUpdateObject) Raw() []byte          { return o.raw }
func (o *nativeUpdateObject) ID() softwareIdentity { return o.id }
func (o *nativeUpdateObject) ACL() (string, error) { return updateFileACL(o.f) }
func (o *nativeUpdateObject) Close() error {
	clear(o.raw)
	o.raw = nil
	var last error
	if o.f != nil {
		last = o.f.Close()
		o.f = nil
	}
	for i := len(o.parents) - 1; i >= 0; i-- {
		if e := syscall.CloseHandle(o.parents[i]); e != nil {
			last = e
		}
	}
	o.parents = nil
	return last
}

func updateFileACL(f *os.File) (string, error) {
	var sd uintptr
	code, _, _ := updateGetSecurity.Call(f.Fd(), 1, 7, 0, 0, 0, 0, uintptr(unsafe.Pointer(&sd)))
	if code != 0 || sd == 0 {
		return "", errUpdateObject
	}
	defer updateLocalFree.Call(sd)
	var text *uint16
	var n uint32
	ok, _, _ := updateSDString.Call(sd, 1, 7, uintptr(unsafe.Pointer(&text)), uintptr(unsafe.Pointer(&n)))
	if ok == 0 || text == nil || n == 0 || n > 1<<20 {
		return "", errUpdateObject
	}
	defer updateLocalFree.Call(uintptr(unsafe.Pointer(text)))
	return syscall.UTF16ToString(unsafe.Slice(text, n)), nil
}

// The public entry supplies only compiled paths. The physical writer is tested
// on owned files; it does not change classification or publish the installed PE.
func (o *nativeUpdateObject) CreateNew(path string, raw []byte, acl string) (softwareIdentity, error) {
	if !sameSoftwareParent(path) {
		return softwareIdentity{}, errUpdateObject
	}
	return o.createNewSibling(path, raw, acl)
}

func (o *nativeUpdateObject) createNewSibling(path string, raw []byte, acl string) (softwareIdentity, error) {
	if !sameParentPath(path, o.f.Name()) {
		return softwareIdentity{}, errUpdateObject
	}
	current, e := o.ACL()
	if e != nil || current != acl {
		return softwareIdentity{}, errUpdateObject
	}
	text, e := syscall.UTF16PtrFromString(acl)
	if e != nil {
		return softwareIdentity{}, errUpdateObject
	}
	var sd uintptr
	ok, _, _ := updateStringSD.Call(uintptr(unsafe.Pointer(text)), 1, uintptr(unsafe.Pointer(&sd)), 0)
	runtime.KeepAlive(text)
	if ok == 0 || sd == 0 {
		return softwareIdentity{}, errUpdateObject
	}
	defer updateLocalFree.Call(sd)
	sa := syscall.SecurityAttributes{Length: uint32(unsafe.Sizeof(syscall.SecurityAttributes{})), SecurityDescriptor: sd}
	ptr, e := syscall.UTF16PtrFromString(path)
	if e != nil {
		return softwareIdentity{}, errUpdateObject
	}
	h, e := syscall.CreateFile(ptr, syscall.GENERIC_READ|syscall.GENERIC_WRITE|0xe0000, syscall.FILE_SHARE_READ, &sa, syscall.CREATE_NEW, 0x00200000, 0)
	if e != nil {
		return softwareIdentity{}, errUpdateObject
	}
	f := os.NewFile(uintptr(h), path)
	defer f.Close()
	id, e := fileSoftwareIdentity(f)
	if e != nil {
		return softwareIdentity{FileID: "unknown_created"}, errUpdateObject
	}
	actual, good := handlePath(h)
	if !good || !sameCleanPath(actual, path) {
		return id, errUpdateObject
	}
	if e := copyUpdateACL(o.f, f); e != nil {
		return id, e
	}
	n, e := f.Write(raw)
	if e != nil || n != len(raw) || f.Sync() != nil {
		return id, errUpdateObject
	}
	if _, e = f.Seek(0, io.SeekStart); e != nil {
		return id, errUpdateObject
	}
	check, e := io.ReadAll(io.LimitReader(f, int64(len(raw))+1))
	if e != nil {
		return id, errUpdateObject
	}
	defer clear(check)
	gotACL, e := updateFileACL(f)
	if e != nil {
		return id, errors.New("software_acl_readback_unavailable")
	}
	if gotACL != acl {
		return id, errors.New("software_acl_mismatch")
	}
	if len(check) != len(raw) || digest(check) != digest(raw) {
		return id, errUpdateObject
	}
	return id, nil
}

func copyUpdateACL(source, target *os.File) error {
	var sd, owner, group, dacl uintptr
	code, _, _ := updateGetSecurity.Call(source.Fd(), 1, 7, uintptr(unsafe.Pointer(&owner)), uintptr(unsafe.Pointer(&group)), uintptr(unsafe.Pointer(&dacl)), 0, uintptr(unsafe.Pointer(&sd)))
	if code != 0 || sd == 0 {
		return errors.New("software_acl_capture_failed")
	}
	defer updateLocalFree.Call(sd)
	var control uint16
	var revision uint32
	ok, _, _ := updateSDControl.Call(sd, uintptr(unsafe.Pointer(&control)), uintptr(unsafe.Pointer(&revision)))
	if ok == 0 {
		return errors.New("software_acl_control_failed")
	}
	flags := uint32(7 | 0x20000000)
	if control&0x1000 != 0 {
		flags = 7 | 0x80000000
	}
	code, _, _ = updateSetSecurity.Call(target.Fd(), 1, uintptr(flags), owner, group, dacl, 0)
	if code != 0 {
		return errors.New("software_acl_apply_failed")
	}
	return nil
}

func sameCleanPath(a, b string) bool { return strings.EqualFold(filepath.Clean(a), filepath.Clean(b)) }
func sameParentPath(a, b string) bool {
	return sameCleanPath(filepath.Dir(a), filepath.Dir(b)) && !sameCleanPath(a, b)
}
