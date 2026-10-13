//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"
	"unsafe"
)

var (
	advapi32            = syscall.NewLazyDLL("advapi32.dll")
	saferCreateLevel    = advapi32.NewProc("SaferCreateLevel")
	saferComputeToken   = advapi32.NewProc("SaferComputeTokenFromLevel")
	saferCloseLevel     = advapi32.NewProc("SaferCloseLevel")
	setTokenInformation = advapi32.NewProc("SetTokenInformation")
	isTokenRestricted   = advapi32.NewProc("IsTokenRestricted")
	getLengthSID        = advapi32.NewProc("GetLengthSid")
)

func restrictionState(token syscall.Token) (bool, bool, error) {
	var length uint32
	_ = syscall.GetTokenInformation(token, 25, nil, 0, &length)
	if length < uint32(unsafe.Sizeof(syscall.SIDAndAttributes{})) || length > 4096 {
		return false, false, errors.New("token_integrity_query_failed")
	}
	buffer := make([]byte, length)
	if syscall.GetTokenInformation(token, 25, &buffer[0], length, &length) != nil {
		return false, false, errors.New("token_integrity_query_failed")
	}
	label := (*syscall.SIDAndAttributes)(unsafe.Pointer(&buffer[0]))
	sid, err := label.Sid.String()
	if err != nil {
		return false, false, errors.New("token_integrity_invalid")
	}
	restricted, _, _ := isTokenRestricted.Call(uintptr(token))
	return restricted != 0, sid == "S-1-16-8192", nil
}

func administratorEnabled(token syscall.Token) (bool, error) {
	var length uint32
	_ = syscall.GetTokenInformation(token, 2, nil, 0, &length)
	if length < 8 || length > 64*1024 {
		return false, errors.New("token_groups_query_failed")
	}
	buffer := make([]byte, length)
	if syscall.GetTokenInformation(token, 2, &buffer[0], length, &length) != nil {
		return false, errors.New("token_groups_query_failed")
	}
	type groupsHeader struct {
		Count  uint32
		Groups [1]syscall.SIDAndAttributes
	}
	header := (*groupsHeader)(unsafe.Pointer(&buffer[0]))
	start := unsafe.Offsetof(header.Groups)
	entrySize := unsafe.Sizeof(syscall.SIDAndAttributes{})
	if uintptr(length) < start || uintptr(header.Count) > (uintptr(length)-start)/entrySize {
		return false, errors.New("token_groups_invalid")
	}
	groups := unsafe.Slice(&header.Groups[0], int(header.Count))
	for _, group := range groups {
		sid, err := group.Sid.String()
		if err != nil {
			return false, errors.New("token_group_sid_invalid")
		}
		if sid == "S-1-5-32-544" {
			return group.Attributes&0x4 != 0 && group.Attributes&0x10 == 0, nil
		}
	}
	return false, nil
}

func makeLimitedToken(current syscall.Token) (syscall.Token, error) {
	var level syscall.Handle
	ok, _, _ := saferCreateLevel.Call(2, 0x20000, 1, uintptr(unsafe.Pointer(&level)), 0)
	if ok == 0 {
		return 0, errors.New("normal_user_level_unavailable")
	}
	defer saferCloseLevel.Call(uintptr(level))
	var restricted syscall.Token
	// Open the existing NORMALUSER level and derive a lower-privilege token.
	// No policy is changed; SAFER_TOKEN_MAKE_INERT is never used.
	ok, _, _ = saferComputeToken.Call(uintptr(level), uintptr(current), uintptr(unsafe.Pointer(&restricted)), 0, 0)
	if ok == 0 {
		return 0, errors.New("restricted_token_create_failed")
	}
	mediumSID, err := syscall.StringToSid("S-1-16-8192")
	if err != nil {
		restricted.Close()
		return 0, errors.New("medium_sid_unavailable")
	}
	label := syscall.SIDAndAttributes{Sid: mediumSID, Attributes: 0x20}
	sidLength, _, _ := getLengthSID.Call(uintptr(unsafe.Pointer(mediumSID)))
	_, alreadyMedium, _ := restrictionState(restricted)
	if !alreadyMedium {
		ok, _, _ = setTokenInformation.Call(uintptr(restricted), 25, uintptr(unsafe.Pointer(&label)), uintptr(unsafe.Sizeof(label))+sidLength)
		if ok == 0 {
			restricted.Close()
			return 0, errors.New("restricted_integrity_failed")
		}
	}
	administrator, err := administratorEnabled(restricted)
	if err != nil || administrator {
		restricted.Close()
		return 0, errors.New("restricted_admin_membership_unchanged")
	}
	_, medium, err := restrictionState(restricted)
	if err != nil || !medium {
		restricted.Close()
		return 0, errors.New("restricted_integrity_unconfirmed")
	}
	return restricted, nil
}

// S4U can return an unfiltered admin token even when the task is Limited.
// Reduce only our own noninteractive child; do not alter any account,
// policy, group, password, logon session or existing service process.
func reduceS4UPrivileges(child, persistent bool) (bool, int, error) {
	var sessionID uint32
	ok, _, _ := kernel32.NewProc("ProcessIdToSessionId").Call(uintptr(os.Getpid()), uintptr(unsafe.Pointer(&sessionID)))
	if ok == 0 {
		return true, 1, errors.New("session_id_unavailable")
	}
	if sessionID != 0 {
		return false, 0, nil
	}
	process, err := syscall.GetCurrentProcess()
	if err != nil {
		return true, 1, errors.New("current_process_unavailable")
	}
	var current syscall.Token
	if err := syscall.OpenProcessToken(process, 0x8|0x2|0x1, &current); err != nil {
		if code, ok := err.(syscall.Errno); ok {
			return true, 1, fmt.Errorf("current_token_unavailable_%d", uint32(code))
		}
		return true, 1, errors.New("current_token_unavailable")
	}
	defer current.Close()
	administrator, err := administratorEnabled(current)
	_, medium, restrictionErr := restrictionState(current)
	if err != nil || restrictionErr != nil {
		return true, 1, errors.New("current_role_unavailable")
	}
	if !administrator && medium {
		return false, 0, nil
	}
	if child {
		return true, 1, errors.New("restricted_child_role_not_limited")
	}
	restricted, err := makeLimitedToken(current)
	if err != nil {
		return true, 1, err
	}
	defer restricted.Close()
	executable, err := os.Executable()
	if err != nil {
		return true, 1, errors.New("own_executable_unavailable")
	}
	arguments := append(append([]string{executable}, os.Args[1:]...), "--restricted-child")
	quoted := make([]string, len(arguments))
	for index, argument := range arguments {
		quoted[index] = syscall.EscapeArg(argument)
	}
	commandLine, err := syscall.UTF16PtrFromString(strings.Join(quoted, " "))
	if err != nil {
		return true, 1, errors.New("child_arguments_invalid")
	}
	application, err := syscall.UTF16PtrFromString(executable)
	if err != nil {
		return true, 1, errors.New("own_executable_invalid")
	}
	startup := syscall.StartupInfo{Cb: uint32(unsafe.Sizeof(syscall.StartupInfo{})), Flags: 1, ShowWindow: 0}
	var information syscall.ProcessInformation
	// Same S4U session and environment. No password or interactive desktop.
	if err := syscall.CreateProcessAsUser(restricted, application, commandLine, nil, nil, false, 0x08000000, nil, nil, &startup, &information); err != nil {
		if code, ok := err.(syscall.Errno); ok {
			return true, 1, fmt.Errorf("restricted_child_start_failed_%d", uint32(code))
		}
		return true, 1, errors.New("restricted_child_start_failed")
	}
	defer syscall.CloseHandle(information.Process)
	defer syscall.CloseHandle(information.Thread)
	wait := uint32(110_000)
	if persistent {
		wait = syscall.INFINITE
	}
	result, err := syscall.WaitForSingleObject(information.Process, wait)
	if err != nil || result != syscall.WAIT_OBJECT_0 {
		syscall.TerminateProcess(information.Process, 1)
		return true, 1, errors.New("restricted_child_deadline")
	}
	var code uint32
	if syscall.GetExitCodeProcess(information.Process, &code) != nil {
		return true, 1, errors.New("restricted_child_exit_unconfirmed")
	}
	return true, int(code), nil
}
