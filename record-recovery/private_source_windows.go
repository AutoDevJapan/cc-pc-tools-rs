//go:build windows

package main

import (
	"errors"
	"os/exec"
	"os/user"
	"syscall"
	"unsafe"
)

func hideCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}

// Validate the ownership and protected DACL used by privateDir. These are local
// acquisition receipts from the owner's explicitly supplied prior work, not an
// arbitrary folder discovered from a quoted path.
func verifyPrivateSource(path string) error {
	bad := errors.New("acquisition_private_ownership_or_acl_unverified")
	u, e := user.Current()
	if e != nil {
		return bad
	}
	p, e := syscall.UTF16PtrFromString(path)
	if e != nil {
		return bad
	}
	adv := syscall.NewLazyDLL("advapi32.dll")
	kernel := syscall.NewLazyDLL("kernel32.dll")
	var owner, dacl, sd unsafe.Pointer
	r, _, _ := adv.NewProc("GetNamedSecurityInfoW").Call(uintptr(unsafe.Pointer(p)), 1, 5, uintptr(unsafe.Pointer(&owner)), 0, uintptr(unsafe.Pointer(&dacl)), 0, uintptr(unsafe.Pointer(&sd)))
	if r != 0 {
		return bad
	}
	defer kernel.NewProc("LocalFree").Call(uintptr(sd))
	var control uint16
	var revision uint32
	r, _, _ = adv.NewProc("GetSecurityDescriptorControl").Call(uintptr(sd), uintptr(unsafe.Pointer(&control)), uintptr(unsafe.Pointer(&revision)))
	if r == 0 || control&0x1000 == 0 || dacl == nil {
		return bad
	}
	sidText := func(sid unsafe.Pointer) string {
		var ptr *uint16
		r, _, _ := adv.NewProc("ConvertSidToStringSidW").Call(uintptr(sid), uintptr(unsafe.Pointer(&ptr)))
		if r == 0 || ptr == nil {
			return ""
		}
		defer kernel.NewProc("LocalFree").Call(uintptr(unsafe.Pointer(ptr)))
		chars := unsafe.Slice(ptr, 256)
		end := 0
		for end < len(chars) && chars[end] != 0 {
			end++
		}
		return syscall.UTF16ToString(chars[:end])
	}
	if sidText(owner) != u.Uid {
		return bad
	}
	type aclHeader struct {
		Revision byte
		Pad      byte
		Size     uint16
		Count    uint16
		Pad2     uint16
	}
	header := (*aclHeader)(dacl)
	allowed := map[string]bool{}
	if header.Count == 0 || header.Count > 16 {
		return bad
	}
	for i := uint16(0); i < header.Count; i++ {
		var ace unsafe.Pointer
		r, _, _ := adv.NewProc("GetAce").Call(uintptr(dacl), uintptr(i), uintptr(unsafe.Pointer(&ace)))
		if r == 0 || ace == nil {
			return bad
		}
		// ACCESS_ALLOWED_ACE: ACE_HEADER(4), access mask(4), SID starts at 8.
		bytes := unsafe.Slice((*byte)(ace), 8)
		if bytes[0] != 0 || bytes[1]&0x10 != 0 {
			return bad
		}
		mask := *(*uint32)(unsafe.Add(ace, 4))
		if mask&0x1f01ff != 0x1f01ff {
			return bad
		}
		sid := sidText(unsafe.Add(ace, 8))
		if sid != u.Uid && sid != "S-1-5-18" && sid != "S-1-5-32-544" {
			return bad
		}
		allowed[sid] = true
	}
	if !allowed[u.Uid] || !allowed["S-1-5-18"] || !allowed["S-1-5-32-544"] {
		return bad
	}
	return nil
}
