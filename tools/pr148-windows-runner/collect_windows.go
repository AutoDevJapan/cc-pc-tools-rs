//go:build windows && amd64

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

// Collection does not delete the root, unload someone else's hive, change an
// SDK/gate ACL, or retry a denied cleanup. The parent preserves evidence first,
// then uses safe-delete plan -> separate normal delete -> finish.
func collect(p Plan) error {
	s, e := openService(p, 4|0x10000)
	if e != nil {
		return e
	}
	state, pid, _, e := queryService(s)
	if e != nil {
		closeSC(s)
		return e
	}
	if state != 1 || pid != 0 {
		closeSC(s)
		return errors.New("service has not stopped; collection refused")
	}
	sid, e := serviceSID(p)
	if e != nil {
		closeSC(s)
		return e
	}
	var drain struct {
		Drained bool `json:"job_drained"`
	}
	b, e := os.ReadFile(filepath.Join(p.runtime(), "drain.json"))
	_, startErr := os.Lstat(filepath.Join(p.runtime(), "started.json"))
	neverStarted := os.IsNotExist(startErr)
	if (e != nil || json.Unmarshal(b, &drain) != nil || !drain.Drained) && !neverStarted {
		closeSC(s)
		return errors.New("Job Object completion evidence missing; retain resource")
	}
	if neverStarted {
		count, err := ownedProcessCount(sid)
		if err != nil || count != 0 {
			closeSC(s)
			return errors.New("unstarted service process absence not proven")
		}
	}
	if p.Fixture == "" {
		if e = audit(p); e != nil {
			closeSC(s)
			return e
		}
	}
	r, _, e := advapi.NewProc("DeleteService").Call(s)
	closeSC(s)
	if r == 0 {
		return e
	}
	// Probe only this SID. Do not enumerate names/paths of other accounts.
	var hive syscall.Handle
	hiveErr := syscall.RegOpenKeyEx(syscall.HKEY_USERS, wide(sid), 0, 0x20019, &hive)
	if hiveErr == nil {
		syscall.RegCloseKey(hive)
	}
	if hiveErr != nil && hiveErr != syscall.ERROR_FILE_NOT_FOUND {
		return hiveErr
	}
	var classes syscall.Handle
	classesErr := syscall.RegOpenKeyEx(syscall.HKEY_USERS, wide(sid+"_Classes"), 0, 0x20019, &classes)
	if classesErr == nil {
		syscall.RegCloseKey(classes)
	}
	if classesErr != nil && classesErr != syscall.ERROR_FILE_NOT_FOUND {
		return classesErr
	}
	var profile syscall.Handle
	profileErr := syscall.RegOpenKeyEx(syscall.HKEY_LOCAL_MACHINE, wide(`SOFTWARE\Microsoft\Windows NT\CurrentVersion\ProfileList\`+sid), 0, 0x20019, &profile)
	if profileErr == nil {
		syscall.RegCloseKey(profile)
	}
	if profileErr != nil && profileErr != syscall.ERROR_FILE_NOT_FOUND {
		return profileErr
	}
	// Remove only explicit service grants installed in this new root. Runtime
	// stays administrator-only; never grant owner read access to credentials.
	aceErr := icacls(p.Root, "/remove:g", "*"+sid, "/T")
	left := false
	aceCount := 0
	e = filepath.Walk(p.Root, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		c := exec.Command(`C:\Windows\System32\icacls.exe`, path)
		hidden(c)
		b, e := c.Output()
		if e != nil {
			return e
		}
		if strings.Contains(string(b), p.name()) || strings.Contains(string(b), sid) {
			aceCount++
			left = true
		}
		return nil
	})
	probe, probeErr := openService(p, 4)
	absent := errors.Is(probeErr, syscall.Errno(1060))
	if probeErr == nil {
		closeSC(probe)
	}
	processes, processErr := ownedProcessCount(sid)
	if p.Fixture != "" && absent && processes == 0 && processErr == nil && aceErr == nil && e == nil && !left {
		// These resources never contained runner credentials. Restore controller
		// cleanup access only to its own new fixture, after drain + service removal.
		aceErr = icacls(p.Root, "/grant:r", "*"+p.ControllerSID+":(OI)(CI)F")
		if aceErr == nil {
			aceErr = icacls(p.runtime(), "/grant:r", "*"+p.ControllerSID+":(OI)(CI)F")
		}
	}
	receipt := map[string]any{"service_absent": absent, "process_count": processes, "process_query_ok": processErr == nil, "additional_ace_count": aceCount, "ace_query_ok": aceErr == nil && e == nil, "profile_present": profileErr == nil, "hive_present": hiveErr == nil, "classes_hive_present": classesErr == nil, "root_retained": true, "credential_runtime": p.runner(), "official_ci_claimed": false}
	writeErr := saveNew(filepath.Join(p.Root, "collection.json"), receipt)
	if !absent || processes != 0 || processErr != nil || left || aceErr != nil || e != nil || profileErr == nil || hiveErr == nil || classesErr == nil {
		return errors.Join(errors.New("new service resource residue remains; parent must retain this work as incomplete"), writeErr, processErr, aceErr, e)
	}
	return writeErr
}

func ownedProcessCount(sid string) (int, error) {
	snapshot, _, e := kernel.NewProc("CreateToolhelp32Snapshot").Call(2, 0)
	if snapshot == ^uintptr(0) {
		return 0, e
	}
	defer syscall.CloseHandle(syscall.Handle(snapshot))
	var entry syscall.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	count := 0
	e = syscall.Process32First(syscall.Handle(snapshot), &entry)
	for e == nil {
		if entry.ProcessID != 0 {
			h, err := syscall.OpenProcess(0x1000, false, entry.ProcessID)
			if err == nil {
				var token syscall.Handle
				r, _, _ := openToken.Call(uintptr(h), 8, uintptr(unsafe.Pointer(&token)))
				if r != 0 {
					got, err := tokenUserSID(token)
					syscall.CloseHandle(token)
					if err != nil {
						syscall.CloseHandle(h)
						return 0, err
					}
					if got == sid {
						count++
					}
				}
				syscall.CloseHandle(h)
			} else if err != syscall.Errno(87) && err != syscall.ERROR_ACCESS_DENIED {
				return 0, fmt.Errorf("process census: %w", err)
			}
		}
		entry.Size = uint32(unsafe.Sizeof(entry))
		e = syscall.Process32Next(syscall.Handle(snapshot), &entry)
	}
	if e != syscall.Errno(18) {
		return 0, e
	}
	return count, nil
}
