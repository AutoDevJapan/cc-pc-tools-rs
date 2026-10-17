//go:build windows && amd64

package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

func cleanupProfile(p Plan) error {
	// Only the new, credential-free account created by this exact instance is
	// eligible. This is not an interface for arbitrary profile/SID deletion.
	if p.Fixture == "" {
		return errors.New("real runner profile cleanup belongs to parent")
	}
	var prepared struct{ SID, Service string }
	b, e := os.ReadFile(filepath.Join(p.Root, "prepared.json"))
	if e != nil || json.Unmarshal(b, &prepared) != nil {
		return errors.New("new service creation receipt required")
	}
	sid, e := serviceSID(p)
	if e != nil || prepared.Service != p.name() || prepared.SID != sid {
		return errors.New("new service ownership receipt mismatch")
	}
	s, e := openService(p, 4)
	if e == nil {
		closeSC(s)
		return errors.New("service must be absent before profile cleanup")
	}
	if !errors.Is(e, syscall.Errno(1060)) {
		return e
	}
	count, e := ownedProcessCount(sid)
	if e != nil || count != 0 {
		return errors.New("new account process completion required")
	}
	// Durable single-attempt marker BEFORE the OS operation. A denial is never
	// retried through this command or another mechanism.
	if e = saveNew(filepath.Join(p.Root, "profile-cleanup-attempt.json"), map[string]any{"sid": sid, "single_attempt": true}); e != nil {
		return errors.New("profile cleanup already attempted or uncertain")
	}
	var token syscall.Handle
	r, _, e := openToken.Call(^uintptr(0), 0x20|8, uintptr(unsafe.Pointer(&token)))
	if r == 0 {
		return e
	}
	defer syscall.CloseHandle(token)
	for _, name := range []string{"SeBackupPrivilege", "SeRestorePrivilege"} {
		var luid uint64
		r, _, e = advapi.NewProc("LookupPrivilegeValueW").Call(0, ptr(name), uintptr(unsafe.Pointer(&luid)))
		if r == 0 {
			return e
		}
		tp := struct {
			Count           uint32
			Low, High, Attr uint32
		}{Count: 1, Low: uint32(luid), High: uint32(luid >> 32), Attr: 2}
		r, _, e = advapi.NewProc("AdjustTokenPrivileges").Call(uintptr(token), 0, uintptr(unsafe.Pointer(&tp)), 0, 0, 0)
		if r == 0 || e == syscall.Errno(1300) {
			return errors.New("profile cleanup privilege unavailable")
		}
	}
	r, _, e = syscall.NewLazyDLL("userenv.dll").NewProc("DeleteProfileW").Call(ptr(sid), 0, 0)
	code := uint32(0)
	if r == 0 {
		if errno, ok := e.(syscall.Errno); ok {
			code = uint32(errno)
		}
	}
	writeErr := saveNew(filepath.Join(p.Root, "profile-cleanup-result.json"), map[string]any{"delete_profile_returned_success": r != 0, "win32_error": code, "requires_fresh_absence_check": true})
	if r == 0 {
		return errors.Join(errors.New("new profile cleanup refused; retain residue without retry"), writeErr)
	}
	return writeErr
}
