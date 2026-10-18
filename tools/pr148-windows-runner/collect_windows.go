//go:build windows && amd64

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"
)

var errResidue = errors.New("new resource residue remains; parent must retain incomplete cleanup")

// Each administrative mutation has an immutable attempt/result journal. An
// interrupted or denied mutation is never retried. Later snapshots may safely
// observe known completed phases without reapplying them.
func collectionAction(p Plan, name string, action func() error) error {
	path := filepath.Join(p.Root, name+"-result.json")
	var old struct{ OK bool }
	if b, e := os.ReadFile(path); e == nil {
		if json.Unmarshal(b, &old) != nil || !old.OK {
			return errors.New("prior collection action failed or uncertain; no retry")
		}
		return nil
	} else if !os.IsNotExist(e) {
		return e
	}
	if e := saveNew(filepath.Join(p.Root, name+"-attempt.json"), map[string]any{"single_attempt": true}); e != nil {
		return errors.New("collection action already attempted or uncertain; no retry")
	}
	e := action()
	write := saveNew(path, map[string]any{"OK": e == nil})
	return errors.Join(e, write)
}
func collect(p Plan) error {
	return collectMode(p, false)
}

func collectUnstarted(p Plan) error {
	if p.Fixture != "" {
		return errors.New("unstarted collection requires a real prepared resource")
	}
	return collectMode(p, true)
}

func collectMode(p Plan, unstarted bool) error {
	seconds := p.CleanupSeconds
	if seconds == 0 {
		seconds = 1800
	}
	aclBudget := time.Duration(seconds) * time.Second
	sid, e := serviceSID(p)
	if e != nil {
		return e
	}
	processes, unreadable, e := ownedProcessCensus(sid)
	if e != nil || processes != 0 {
		return errors.Join(errors.New("owned process census prevents collection"), e)
	}
	if unstarted {
		if e = checkUnstartedFiles(p.Root, p.runner()); e != nil {
			return e
		}
		var job struct {
			apiJob
			Steps []json.RawMessage
		}
		if e = api(fmt.Sprintf("repos/%s/actions/jobs/%d", repository, p.JobID), &job); e != nil {
			return e
		}
		if e = checkUnstartedJob(p, job.apiJob, len(job.Steps)); e != nil {
			return e
		}
	}
	s, e := openService(p, 4|0x10000)
	if e == nil {
		state, pid, code, err := queryService(s)
		if err != nil || state != 1 || pid != 0 {
			closeSC(s)
			return errors.New("service must be stopped with PID zero")
		}
		if unstarted {
			if code != 0 {
				closeSC(s)
				return errors.New("unstarted worker evidence absent or changed")
			}
		} else {
			report, err := readLaunchResult(p)
			if err != nil || !report.Drained || report.AccountSID != sid || ((code == 0) != report.ServiceSuccess) {
				closeSC(s)
				return errors.New("protected launcher drain and SCM result required")
			}
		}
		if p.Fixture == "" {
			if err = auditCollection(p); err != nil {
				closeSC(s)
				return err
			}
		}
		e = collectionAction(p, "service-delete", func() error {
			r, _, callErr := advapi.NewProc("DeleteService").Call(s)
			if r == 0 {
				return callErr
			}
			return nil
		})
		closeSC(s)
		if e != nil {
			return e
		}
	} else if errors.Is(e, syscall.Errno(1060)) {
		var result struct{ OK bool }
		b, err := os.ReadFile(filepath.Join(p.Root, "service-delete-result.json"))
		if err != nil || json.Unmarshal(b, &result) != nil || !result.OK {
			return errors.New("absent service lacks owned completed deletion receipt")
		}
	} else {
		return e
	}
	// Only two administrator-installed inheritable grants exist. Remove each
	// once; Windows propagates inheritance. Do not spawn a process per target file.
	for _, row := range []struct{ name, path string }{{"root-ace-remove", p.Root}, {"runtime-ace-remove", p.runtime()}} {
		if e = collectionAction(p, row.name, func() error { return icaclsWithin(aclBudget, row.path, "/remove:g", "*"+sid) }); e != nil {
			return e
		}
	}
	aceCount := 0
	for _, path := range []string{p.Root, p.runtime()} {
		n, err := countGrantedSID(path, sid)
		if err != nil {
			return err
		}
		aceCount += n
	}
	probe, probeErr := openService(p, 4)
	absent := errors.Is(probeErr, syscall.Errno(1060))
	if probeErr == nil {
		closeSC(probe)
	}
	if !absent || aceCount != 0 {
		return errors.New("service deletion or explicit grant removal not confirmed")
	}
	if p.Fixture != "" || unstarted {
		for _, row := range []struct{ name, path string }{{"root-controller-restore", p.Root}, {"runtime-controller-restore", p.runtime()}} {
			if e = collectionAction(p, row.name, func() error { return icaclsWithin(aclBudget, row.path, "/grant:r", "*"+p.ControllerSID+":(OI)(CI)F") }); e != nil {
				return e
			}
		}
	}
	profile, e := registryPresent(syscall.HKEY_LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows NT\CurrentVersion\ProfileList\`+sid)
	if e != nil {
		return e
	}
	hive, e := registryPresent(syscall.HKEY_USERS, sid)
	if e != nil {
		return e
	}
	classes, e := registryPresent(syscall.HKEY_USERS, sid+"_Classes")
	if e != nil {
		return e
	}
	censusScope := "queryable processes; owned Job Object independently drained"
	if unstarted {
		censusScope = "queryable processes; worker start and authentication artifacts absent"
	}
	receipt := map[string]any{"unstarted_worker": unstarted, "service_absent": absent, "queryable_account_process_count": processes, "unreadable_process_count": unreadable, "process_census_scope": censusScope, "additional_ace_count": aceCount, "ace_scope": "administrator-installed root and runtime inheritable grants only", "profile_present": profile, "hive_present": hive, "classes_hive_present": classes, "root_retained": true, "credential_runtime": p.runner(), "official_ci_claimed": false, "known_residue": profile || hive || classes}
	if e = saveNew(filepath.Join(p.Root, fmt.Sprintf("collection-%d.json", time.Now().UnixNano())), receipt); e != nil {
		return e
	}
	if profile || hive || classes {
		return errResidue
	}
	return nil
}
func registryPresent(root syscall.Handle, key string) (bool, error) {
	var h syscall.Handle
	e := syscall.RegOpenKeyEx(root, wide(key), 0, 0x20019, &h)
	if e == nil {
		syscall.RegCloseKey(h)
		return true, nil
	}
	if errors.Is(e, syscall.ERROR_FILE_NOT_FOUND) {
		return false, nil
	}
	return false, e
}

func countGrantedSID(path, sid string) (int, error) {
	var sd uintptr
	var acl *struct {
		Revision, Sbz1    byte
		Size, Count, Sbz2 uint16
	}
	result, _, _ := advapi.NewProc("GetNamedSecurityInfoW").Call(ptr(path), 1, 4, 0, 0, uintptr(unsafe.Pointer(&acl)), 0, uintptr(unsafe.Pointer(&sd)))
	if result != 0 {
		return 0, syscall.Errno(result)
	}
	defer kernel.NewProc("LocalFree").Call(sd)
	if acl == nil {
		return 0, errors.New("null owned resource DACL")
	}
	var wanted uintptr
	r, _, e := convertSID.Call(ptr(sid), uintptr(unsafe.Pointer(&wanted)))
	if r == 0 {
		return 0, e
	}
	defer kernel.NewProc("LocalFree").Call(wanted)
	// ACL header is 8 bytes; AceCount is the WORD at offset 4.
	count := acl.Count
	grants := 0
	for i := uint16(0); i < count; i++ {
		var ace *struct {
			Type, Flags byte
			Size        uint16
			Mask        uint32
			SID         [1]uint32
		}
		r, _, e = advapi.NewProc("GetAce").Call(uintptr(unsafe.Pointer(acl)), uintptr(i), uintptr(unsafe.Pointer(&ace)))
		if r == 0 {
			return 0, e
		}
		if ace.Type == 0 {
			if ace.Size < 16 {
				return 0, errors.New("owned DACL ACE truncated")
			}
			equal, _, _ := advapi.NewProc("EqualSid").Call(uintptr(unsafe.Pointer(&ace.SID[0])), wanted)
			if equal != 0 {
				grants++
			}
		}
	}
	return grants, nil
}
func ownedProcessCount(sid string) (int, error) { n, _, e := ownedProcessCensus(sid); return n, e }
func ownedProcessCensus(sid string) (int, int, error) {
	snapshot, _, e := kernel.NewProc("CreateToolhelp32Snapshot").Call(2, 0)
	if snapshot == ^uintptr(0) {
		return 0, 0, e
	}
	defer syscall.CloseHandle(syscall.Handle(snapshot))
	var entry syscall.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	count, unreadable := 0, 0
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
						return 0, unreadable, err
					}
					if got == sid {
						count++
					}
				} else {
					unreadable++
				}
				syscall.CloseHandle(h)
			} else if errors.Is(err, syscall.ERROR_ACCESS_DENIED) {
				unreadable++
			} else if !errors.Is(err, syscall.Errno(87)) {
				return 0, unreadable, err
			}
		}
		entry.Size = uint32(unsafe.Sizeof(entry))
		e = syscall.Process32Next(syscall.Handle(snapshot), &entry)
	}
	if e != syscall.Errno(18) {
		return 0, unreadable, e
	}
	return count, unreadable, nil
}
