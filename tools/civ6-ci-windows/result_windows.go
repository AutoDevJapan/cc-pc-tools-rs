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

type launchResult struct {
	Instance                string   `json:"instance"`
	ServicePID              int      `json:"service_pid"`
	ServiceSuccess          bool     `json:"service_success"`
	WorkerPID               uint32   `json:"worker_pid"`
	WorkerExit              *uint32  `json:"worker_exit"`
	MemberPIDs              []uint32 `json:"retained_member_pids"`
	Drained                 bool     `json:"job_drained"`
	Cancelled               bool     `json:"cancelled"`
	AccountSID              string   `json:"account_sid_observed"`
	TokenAUDenyOnlyVerified bool     `json:"token_au_deny_only_verified"`
	HooksVerified           bool     `json:"hooks_verified"`
	FixtureChildPID         uint32   `json:"fixture_child_pid,omitempty"`
}

var launchReport launchResult
var activeReport *os.File

func publishLauncherReport() error {
	b, e := json.MarshalIndent(launchReport, "", "  ")
	if e != nil {
		return e
	}
	if activeReport == nil {
		return errors.New("protected report handle unavailable")
	}
	if _, e = activeReport.WriteAt(b, 0); e != nil {
		return e
	}
	if e = activeReport.Truncate(int64(len(b))); e != nil {
		return e
	}
	return activeReport.Sync()
}

func prepareLauncherReport(p Plan, sid string) error {
	path := filepath.Join(p.Root, "launcher-result.json")
	if e := writeNewBytes(path, []byte("{}")); e != nil {
		return e
	}
	if e := icacls(path, "/inheritance:r", "/grant:r", "*S-1-5-18:F", "*S-1-5-32-544:F", "*"+p.ControllerSID+":R", "*"+sid+":F"); e != nil {
		return e
	}
	return icacls(path, "/setowner", "*S-1-5-32-544")
}
func openLauncherReport(p Plan) (*os.File, error) {
	h, e := syscall.CreateFile(wide(filepath.Join(p.Root, "launcher-result.json")), 0x80000000|0x40000000|0x40000, 1, nil, 3, 0x80, 0)
	if e != nil {
		return nil, e
	}
	f := os.NewFile(uintptr(h), "protected-launcher-report")
	var sd uintptr
	r, _, e := convertDescriptor.Call(ptr("D:P(A;;FA;;;SY)(A;;FA;;;BA)(A;;FR;;;"+p.ControllerSID+")"), 1, uintptr(unsafe.Pointer(&sd)), 0)
	if r == 0 {
		f.Close()
		return nil, e
	}
	defer kernel.NewProc("LocalFree").Call(sd)
	r, _, e = setObjectSecurity.Call(uintptr(h), 4, sd)
	if r == 0 {
		f.Close()
		return nil, e
	}
	return f, nil
}
func readLaunchResult(p Plan) (r launchResult, e error) {
	b, e := os.ReadFile(filepath.Join(p.Root, "launcher-result.json"))
	if e != nil {
		return r, e
	}
	if len(b) > 65536 {
		return r, errors.New("launcher report bound")
	}
	e = json.Unmarshal(b, &r)
	if e != nil || r.Instance != p.Instance || r.ServicePID <= 0 {
		return r, errors.New("protected launcher report rejected")
	}
	return r, nil
}
func validateLauncherSuccess(p Plan) error {
	r, e := readLaunchResult(p)
	if e != nil {
		return e
	}
	sid, e := serviceSID(p)
	if e != nil {
		return e
	}
	if !r.ServiceSuccess || !r.Drained || r.Cancelled || !r.TokenAUDenyOnlyVerified || r.AccountSID != sid || r.WorkerExit == nil || *r.WorkerExit != 0 || !r.HooksVerified {
		return errors.New("runner success lacks launcher, token, exit or all hook evidence")
	}
	return nil
}
