//go:build windows && amd64

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// collectionServiceStatus accepts only a stopped service with PID zero. The
// service identity is shared by every job, so its last SCM exit code is not a
// "never started" proof; an unstarted job is instead proven by the untouched
// launcher report and the absence of worker/authentication artifacts.
func collectionServiceStatus(state, pid uint32) error {
	if state != 1 || pid != 0 {
		return errors.New("service must be stopped with PID zero")
	}
	return nil
}

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
	return collectWithEvents(p, false)
}

func collectUnstarted(p Plan) error {
	if p.Fixture != "" {
		return errors.New("unstarted collection requires a real prepared resource")
	}
	return collectWithEvents(p, true)
}

// collectWithEvents brackets a collection with credential-free ledger lines.
func collectWithEvents(p Plan, unstarted bool) (err error) {
	emitEvent("collect-begin", p, map[string]any{"unstarted": unstarted})
	defer func() {
		emitEvent("collect-end", p, map[string]any{"unstarted": unstarted, "complete": err == nil, "shared_profile_retained": err == nil})
	}()
	return collectMode(p, unstarted)
}

// collectMode returns this job's namespace to the idle shared identity. The
// per-job service ACEs and ownership are removed, the account must have no
// process and the service must be stopped; the service itself and its single
// OS profile/hive are deliberately kept for the next job and recorded as such.
func collectMode(p Plan, unstarted bool) error {
	seconds := p.CleanupSeconds
	if seconds == 0 {
		seconds = 1800
	}
	aclBudget := time.Duration(seconds) * time.Second
	// Exclusive for the whole collection: no start, stop or claim can interleave.
	unlock, e := lockClaim(p)
	if e != nil {
		return e
	}
	defer unlock()
	sid, e := serviceSID(p)
	if e != nil {
		return fmt.Errorf("shared service must exist: %w", e)
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
	// Immutable run identity and absence of this plan's unique registration.
	if p.Fixture == "" {
		if e = auditCollection(p); e != nil {
			return e
		}
	}
	s, e := openService(p, 4|1)
	if e != nil {
		return fmt.Errorf("shared service must exist: %w", e)
	}
	defer closeSC(s)
	if e = requireBoundTo(s, p); e != nil {
		return e
	}
	state, pid, code, err := queryService(s)
	if err != nil {
		return fmt.Errorf("collection service status query: %w", err)
	}
	if err = collectionServiceStatus(state, pid); err != nil {
		return err
	}
	if !unstarted {
		report, err := readLaunchResult(p)
		if err != nil || !report.Drained || report.AccountSID != sid || ((code == 0) != report.ServiceSuccess) {
			return errors.New("protected launcher drain and SCM result required")
		}
	}
	// Remove the two administrator-installed inheritable grants once each, then
	// take ownership of the whole tree away from the shared account: the next job
	// runs as the same SID and would otherwise keep WRITE_DAC on these files.
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
	if aceCount != 0 {
		return errors.New("explicit service grant removal not confirmed")
	}
	// Explicit ACEs below the two inherited-grant roots (a worker can disable
	// inheritance and grant itself or Everyone anything) are replaced by the
	// root's inherited ones for every descendant; the root itself is untouched.
	if e = collectionAction(p, "tree-acl-reset", func() error {
		return icaclsWithin(aclBudget, filepath.Join(p.Root, "*"), "/reset", "/T", "/C", "/L")
	}); e != nil {
		return e
	}
	if e = collectionAction(p, "owner-reset", func() error {
		return icaclsWithin(aclBudget, p.Root, "/setowner", "*S-1-5-32-544", "/T", "/C", "/L")
	}); e != nil {
		return e
	}
	// The launcher report of a never-started job still carries an explicit grant.
	if e = collectionAction(p, "report-ace-remove", func() error {
		return icaclsWithin(aclBudget, filepath.Join(p.Root, "launcher-result.json"), "/remove:g", "*"+sid)
	}); e != nil {
		return e
	}
	// The controller gets the root back (credentials of the deregistered ephemeral
	// runner included, never read here) so the owned root can be deleted by the
	// normal safe-delete path, which the shared identity now requires.
	{
		for _, row := range []struct{ name, path string }{{"root-controller-restore", p.Root}, {"runtime-controller-restore", p.runtime()}} {
			if e = collectionAction(p, row.name, func() error { return icaclsWithin(aclBudget, row.path, "/grant:r", "*"+p.ControllerSID+":(OI)(CI)F") }); e != nil {
				return e
			}
		}
	}
	// Recursive proof, not a sample: no entry below the root still grants the
	// shared SID, so a root that is later moved or kept is just as isolated.
	entries, links, e := verifyTreeClean(p.Root, sid)
	if e != nil {
		return e
	}
	// The identity must still be idle after the ACL work.
	state, pid, _, err = queryService(s)
	if err != nil {
		return fmt.Errorf("post-collection service status query: %w", err)
	}
	if err = collectionServiceStatus(state, pid); err != nil {
		return err
	}
	processes, unreadable, e = ownedProcessCensus(sid)
	if e != nil || processes != 0 {
		return errors.Join(errors.New("post-collection owned process census prevents reuse"), e)
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
	leaves, e := serviceProfileLeaves()
	if e != nil {
		return e
	}
	total, own := 0, 0
	for _, l := range leaves {
		if strings.HasPrefix(l, "CC-CIW-") {
			total++
		}
		if l == p.svc() {
			own++
		}
	}
	receipt := map[string]any{
		"unstarted_worker": unstarted, "service": p.svc(), "service_retained": true, "service_idle_confirmed": true,
		"queryable_account_process_count": processes, "unreadable_process_count": unreadable,
		"process_census_scope": "queryable processes; owned Job Object independently drained",
		"additional_ace_count": aceCount, "owner_reset": true, "tree_acl_reset": true, "tree_entries_verified_without_service_grant": entries, "tree_links_verified_without_service_grant": links, "ace_scope": "administrator-installed root and runtime inheritable grants only",
		"shared_profile_retained": profile, "shared_hive_loaded": hive, "shared_classes_hive_loaded": classes,
		"shared_profile_entries_for_this_identity": own, "ciw_profile_leaf_count_total": total,
		"profile_scope":      "the single continuing profile of this module's fixed identity; legacy per-job CC-CIW-<instance> profiles are counted in the total",
		"credential_runtime": p.runner(), "official_ci_claimed": false,
	}
	if e = saveNew(filepath.Join(p.Root, fmt.Sprintf("collection-%d.json", time.Now().UnixNano())), receipt); e != nil {
		return e
	}
	// Last step: durable proof that lets the identity move to the next job.
	return markCollected(p)
}

// verifyTreeClean walks every entry below root (without following links) and
// fails if any DACL grants sid. Links and other reparse entries are checked
// through their own handle, never through their target.
func verifyTreeClean(root, sid string) (entries, links int, err error) {
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		var g int
		var e error
		if d.Type()&(fs.ModeSymlink|fs.ModeIrregular) != 0 {
			g, e = countGrantedSIDNoFollow(path, sid)
			links++
		} else {
			g, e = countGrantedSID(path, sid)
			entries++
		}
		if e != nil {
			return fmt.Errorf("cannot verify %s: %w", filepath.Base(path), e)
		}
		if g != 0 {
			return fmt.Errorf("service SID still granted below the collected root: %s", filepath.Base(path))
		}
		return nil
	})
	return entries, links, err
}

// serviceProfileLeaves lists the profile directory names of every virtual
// service account in ProfileList (read-only).
func serviceProfileLeaves() ([]string, error) {
	var h syscall.Handle
	if e := syscall.RegOpenKeyEx(syscall.HKEY_LOCAL_MACHINE, wide(`SOFTWARE\Microsoft\Windows NT\CurrentVersion\ProfileList`), 0, 0x20019, &h); e != nil {
		return nil, e
	}
	defer syscall.RegCloseKey(h)
	var out []string
	for i := uint32(0); ; i++ {
		var name [256]uint16
		n := uint32(len(name))
		e := syscall.RegEnumKeyEx(h, i, &name[0], &n, nil, nil, nil, nil)
		if e == syscall.Errno(259) {
			return out, nil
		}
		if e != nil {
			return nil, e
		}
		sid := syscall.UTF16ToString(name[:n])
		if !strings.HasPrefix(sid, "S-1-5-80-") {
			continue
		}
		var k syscall.Handle
		if e = syscall.RegOpenKeyEx(h, wide(sid), 0, 0x20019, &k); e != nil {
			return nil, e
		}
		var typ uint32
		var buf [1024]uint16
		size := uint32(len(buf) * 2)
		e = syscall.RegQueryValueEx(k, wide("ProfileImagePath"), nil, &typ, (*byte)(unsafe.Pointer(&buf[0])), &size)
		syscall.RegCloseKey(k)
		if e != nil {
			return nil, e
		}
		out = append(out, filepath.Base(syscall.UTF16ToString(buf[:])))
	}
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

type aclHeader struct {
	Revision, Sbz1    byte
	Size, Count, Sbz2 uint16
}

func countGrantedSID(path, sid string) (int, error) {
	var sd uintptr
	var acl *aclHeader
	result, _, _ := advapi.NewProc("GetNamedSecurityInfoW").Call(ptr(path), 1, 4, 0, 0, uintptr(unsafe.Pointer(&acl)), 0, uintptr(unsafe.Pointer(&sd)))
	if result != 0 {
		return 0, syscall.Errno(result)
	}
	defer kernel.NewProc("LocalFree").Call(sd)
	return countAclGrants(acl, sid)
}

// countGrantedSIDNoFollow reads the DACL of a link or reparse point itself
// (not its target) through a handle opened with FILE_FLAG_OPEN_REPARSE_POINT.
func countGrantedSIDNoFollow(path, sid string) (int, error) {
	h, e := syscall.CreateFile(wide(path), 0x20000, 7, nil, 3, 0x02000000|0x00200000, 0)
	if e != nil {
		return 0, e
	}
	defer syscall.CloseHandle(h)
	var sd uintptr
	var acl *aclHeader
	result, _, _ := advapi.NewProc("GetSecurityInfo").Call(uintptr(h), 1, 4, 0, 0, uintptr(unsafe.Pointer(&acl)), 0, uintptr(unsafe.Pointer(&sd)))
	if result != 0 {
		return 0, syscall.Errno(result)
	}
	defer kernel.NewProc("LocalFree").Call(sd)
	return countAclGrants(acl, sid)
}

func countAclGrants(acl *aclHeader, sid string) (int, error) {
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
