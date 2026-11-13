//go:build windows

package main

import (
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"syscall"
	"unsafe"
)

// A duplicated reduced-rights handle still retains the original Windows file
// object's write sharing state. ReOpenFile creates a new read-only file object.
// During that transition, a temporary DACL on this newly created owned file
// excludes new writers; overlapping handles continuously exclude replacement.
func (s *softwareBuildSnapshot) readOnlySnapshotFile(creator *os.File, hook func(string) error) (result *os.File, err error) {
	step := func(name string) error {
		if hook != nil {
			return hook(name)
		}
		return nil
	}
	id, err := fileSoftwareIdentity(creator)
	if err != nil {
		return nil, err
	}
	before, err := snapshotHeldFileDigest(creator)
	if err != nil {
		return nil, err
	}
	ptr, e := syscall.UTF16PtrFromString(creator.Name())
	if e != nil {
		return nil, e
	}
	control, e := syscall.CreateFile(ptr, 0x00020000|0x00040000, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE, nil, syscall.OPEN_EXISTING, 0x00200000, 0)
	if e != nil {
		return nil, e
	}
	var original, dacl uintptr
	code, _, _ := updateGetSecurity.Call(uintptr(control), 1, 4, 0, 0, uintptr(unsafe.Pointer(&dacl)), 0, uintptr(unsafe.Pointer(&original)))
	if code != 0 || original == 0 {
		syscall.CloseHandle(control)
		return nil, errors.New("snapshot_file_acl_capture_failed")
	}
	var flags uint16
	var revision uint32
	ok, _, _ := updateSDControl.Call(original, uintptr(unsafe.Pointer(&flags)), uintptr(unsafe.Pointer(&revision)))
	if ok == 0 {
		updateLocalFree.Call(original)
		syscall.CloseHandle(control)
		return nil, errUpdateObject
	}
	acl := snapshotDirectoryACL{handle: control, descriptor: original, dacl: dacl, protected: flags&0x1000 != 0, autoInherited: flags&0x0400 != 0}
	var bridge, reader *os.File
	aclChanged, restored := false, false
	defer func() {
		if bridge != nil {
			err = errors.Join(err, bridge.Close())
		}
		if result == nil && reader != nil {
			err = errors.Join(err, reader.Close())
		}
		if aclChanged && !restored {
			if restoreErr := restoreSnapshotACL(acl); restoreErr != nil {
				s.handoffACLs = append(s.handoffACLs, acl)
				err = errors.Join(err, errSnapshotCleanup, restoreErr)
				return
			}
		}
		updateLocalFree.Call(original)
		if e := syscall.CloseHandle(control); e != nil {
			err = errors.Join(err, e)
		}
	}()
	token, e := syscall.OpenCurrentProcessToken()
	if e != nil {
		return nil, e
	}
	user, e := token.GetTokenUser()
	token.Close()
	if e != nil {
		return nil, e
	}
	sid, e := user.User.Sid.String()
	if e != nil {
		return nil, e
	}
	text, e := syscall.UTF16PtrFromString("D:P(A;;GR;;;" + sid + ")")
	if e != nil {
		return nil, e
	}
	var readonly uintptr
	ok, _, _ = updateStringSD.Call(uintptr(unsafe.Pointer(text)), 1, uintptr(unsafe.Pointer(&readonly)), 0)
	if ok == 0 {
		return nil, errUpdateObject
	}
	defer updateLocalFree.Call(readonly)
	if e := step("before_restrict"); e != nil {
		return nil, e
	}
	code, _, _ = updateNTSetSecurity.Call(uintptr(control), 4, readonly)
	if code != 0 {
		return nil, errors.New("snapshot_file_acl_restrict_failed")
	}
	aclChanged = true
	if e := step("restricted"); e != nil {
		return nil, e
	}
	reopen := syscall.NewLazyDLL("kernel32.dll").NewProc("ReOpenFile")
	h, _, openErr := reopen.Call(creator.Fd(), syscall.GENERIC_READ, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE, 0x00200000)
	if h == uintptr(syscall.InvalidHandle) {
		return nil, errors.Join(errors.New("snapshot_read_bridge_failed"), openErr)
	}
	bridge = os.NewFile(h, creator.Name())
	if current, e := fileSoftwareIdentity(bridge); e != nil || current != id {
		return nil, errUpdateObject
	}
	if e := step("bridge_held"); e != nil {
		return nil, e
	}
	if e := creator.Close(); e != nil {
		return nil, e
	}
	if e := step("creator_closed"); e != nil {
		return nil, e
	}
	h, _, openErr = reopen.Call(bridge.Fd(), syscall.GENERIC_READ, syscall.FILE_SHARE_READ, 0x00200000)
	if h == uintptr(syscall.InvalidHandle) {
		return nil, errors.Join(errors.New("snapshot_read_pin_failed"), openErr)
	}
	reader = os.NewFile(h, creator.Name())
	if current, e := fileSoftwareIdentity(reader); e != nil || current != id {
		return nil, errUpdateObject
	}
	if after, e := snapshotHeldFileDigest(reader); e != nil || after != before {
		return nil, errors.New("snapshot_handoff_content_changed")
	}
	if e := step("reader_held"); e != nil {
		return nil, e
	}
	if e := bridge.Close(); e != nil {
		return nil, e
	}
	bridge = nil
	if e := restoreSnapshotACL(acl); e != nil {
		return nil, e
	}
	restored = true
	if e := step("restored"); e != nil {
		return nil, e
	}
	return reader, nil
}

func snapshotHeldFileDigest(file *os.File) ([32]byte, error) {
	var zero [32]byte
	info, err := file.Stat()
	if err != nil || info.Size() < 0 || info.Size() > cargoVendorFileLimit {
		return zero, errUpdateObject
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		return zero, err
	}
	hash := sha256.New()
	var scratch [8192]byte
	defer clear(scratch[:])
	n, err := io.CopyBuffer(hash, io.LimitReader(file, info.Size()+1), scratch[:])
	if err != nil || n != info.Size() {
		return zero, errUpdateObject
	}
	var digest [32]byte
	copy(digest[:], hash.Sum(nil))
	_, err = file.Seek(0, io.SeekStart)
	return digest, err
}
