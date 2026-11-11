//go:build windows && amd64

// One-shot investigation of VSS coordinator exclusion. This executable never
// adds a volume, creates a shadow, changes storage, or reads backup metadata.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"time"
	"unsafe"
)

const (
	initializeForBackupSlot = 5
	setBackupStateSlot      = 6
	abortBackupSlot         = 15
	setContextSlot          = 35
	startSnapshotSetSlot    = 36
	clientAccessible        = 0x1d // persistent, client-accessible, no-auto-release, no-writers
	copyBackup              = 5
	setInProgress           = 0x80042316
)

func abortCompleted(hr uint32) bool {
	// COM+ reports that Abort was delivered without any writer subscriber in
	// the no-writers context. Do not treat partial subscriber failure as success.
	return hr == 0 || hr == 0x00040202 // EVENT_S_NOSUBSCRIBERS, winerror.h
}

// Slot order comes from Microsoft's vsbackup.h IVssBackupComponents : IUnknown.
// https://github.com/microsoft/win32metadata/blob/main/generation/WinSDK/RecompiledIdlHeaders/um/vsbackup.h
type component struct{ table *[37]uintptr }
type guid struct {
	A uint32
	B uint16
	C uint16
	D [8]byte
}

type callResult struct {
	Object    string `json:"object"`
	Operation string `json:"operation"`
	HRESULT   string `json:"hresult"`
}

type report struct {
	StartedUTC            string       `json:"started_utc"`
	FinishedUTC           string       `json:"finished_utc"`
	Phase                 string       `json:"phase"`
	PhaseBeforeCleanup    string       `json:"phase_before_cleanup"`
	Calls                 []callResult `json:"calls"`
	OwnedEmptySetsStarted int          `json:"owned_empty_sets_started"`
	OwnAbortsSucceeded    int          `json:"own_aborts_succeeded"`
	ObjectsReleased       int          `json:"objects_released"`
	ContenderBlocked      bool         `json:"contender_blocked"`
	ExclusionObserved     bool         `json:"exclusion_observed"`
	AvailabilityObserved  bool         `json:"availability_observed"`
	Scope                 string       `json:"scope"`
	VolumeAddCalls        int          `json:"volume_add_calls"`
	ShadowCreateCalls     int          `json:"shadow_create_calls"`
	ShadowDeleteCalls     int          `json:"shadow_delete_calls"`
	StorageChangeCalls    int          `json:"storage_change_calls"`
	WriterMetadataCalls   int          `json:"writer_metadata_calls"`
}

func (c *component) call(slot int, args ...uintptr) uint32 {
	params := append([]uintptr{uintptr(unsafe.Pointer(c))}, args...)
	h, _, _ := syscall.SyscallN(c.table[slot], params...)
	runtime.KeepAlive(c)
	return uint32(h)
}

func run(contender bool) (r report) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	r.StartedUTC = time.Now().UTC().Format(time.RFC3339Nano)
	r.Scope = "two components in one process; not proof that a storage relocation is atomic"
	if !contender {
		r.Scope = "one normal empty set after cleanup; no shadow or storage changes"
	}
	r.Phase = "entrypoints_unavailable"
	defer func() { r.FinishedUTC = time.Now().UTC().Format(time.RFC3339Nano) }()
	root := filepath.Join(os.Getenv("SystemRoot"), "System32")
	ole := syscall.NewLazyDLL(filepath.Join(root, "ole32.dll"))
	vss := syscall.NewLazyDLL(filepath.Join(root, "vssapi.dll"))
	initCOM := ole.NewProc("CoInitializeEx")
	initSecurity := ole.NewProc("CoInitializeSecurity")
	uninitCOM := ole.NewProc("CoUninitialize")
	// The SDK's public CreateVssBackupComponents inline is exactly a call to
	// this exported entry point with ppBackup. No undocumented interface is used.
	create := vss.NewProc("CreateVssBackupComponentsInternal")
	for _, p := range []*syscall.LazyProc{initCOM, initSecurity, uninitCOM, create} {
		if p.Find() != nil {
			return
		}
	}
	record := func(object, op string, hr uint32) {
		r.Calls = append(r.Calls, callResult{object, op, fmt.Sprintf("0x%08X", hr)})
	}
	h, _, _ := initCOM.Call(0, 0) // COINIT_MULTITHREADED
	record("process", "CoInitializeEx", uint32(h))
	if int32(uint32(h)) < 0 {
		r.Phase = "com_initialization_failed"
		return
	}
	defer uninitCOM.Call()
	h, _, _ = initSecurity.Call(0, ^uintptr(0), 0, 0, 6, 3, 0, 0, 0)
	record("process", "CoInitializeSecurity", uint32(h))
	if int32(uint32(h)) < 0 {
		r.Phase = "com_security_failed"
		return
	}
	var objects []*component
	var started []*component
	defer func() {
		r.PhaseBeforeCleanup = r.Phase
		// Only these empty sets are aborted, and only if our Start returned S_OK.
		// No AddToSnapshotSet, PrepareForBackup, or DoSnapshotSet is invoked.
		for i := len(started) - 1; i >= 0; i-- {
			hr := started[i].call(abortBackupSlot)
			record(fmt.Sprintf("owned-set-%d", i+1), "AbortBackup", hr)
			if abortCompleted(hr) {
				r.OwnAbortsSucceeded++
			}
		}
		for i := len(objects) - 1; i >= 0; i-- {
			objects[i].call(2) // IUnknown::Release before CoUninitialize
			r.ObjectsReleased++
		}
		r.ExclusionObserved = r.ContenderBlocked && r.OwnedEmptySetsStarted == 1 &&
			r.OwnAbortsSucceeded == 1 && r.ObjectsReleased == len(objects)
		r.AvailabilityObserved = !contender && r.OwnedEmptySetsStarted == 1 &&
			r.OwnAbortsSucceeded == 1 && r.ObjectsReleased == 1
		if r.OwnAbortsSucceeded != r.OwnedEmptySetsStarted {
			r.Phase = "own_empty_set_abort_unverified"
		}
	}()
	labels := []string{"first"}
	if contender {
		labels = append(labels, "contender")
	}
	for _, label := range labels {
		var c *component
		h, _, _ = create.Call(uintptr(unsafe.Pointer(&c)))
		record(label, "CreateVssBackupComponents", uint32(h))
		if uint32(h) != 0 || c == nil {
			r.Phase = "component_creation_failed"
			return
		}
		objects = append(objects, c)
		for _, op := range []struct {
			name string
			slot int
			args []uintptr
		}{
			{"InitializeForBackup", initializeForBackupSlot, []uintptr{0}},
			{"SetContext", setContextSlot, []uintptr{clientAccessible}},
			{"SetBackupState", setBackupStateSlot, []uintptr{0, 0, copyBackup, 0}},
		} {
			hr := c.call(op.slot, op.args...)
			record(label, op.name, hr)
			if hr != 0 {
				r.Phase = "component_setup_failed"
				return
			}
		}
		var id guid
		// Bounded caller must save stderr if a native RPC does not return.
		fmt.Fprintln(os.Stderr, "calling StartSnapshotSet:", label)
		// Keep the output-pointer conversion inside the syscall expression so
		// Go's uintptr escape handling pins its storage for the native call.
		rawHR, _, _ := syscall.SyscallN(c.table[startSnapshotSetSlot], uintptr(unsafe.Pointer(c)), uintptr(unsafe.Pointer(&id)))
		hr := uint32(rawHR)
		runtime.KeepAlive(c)
		runtime.KeepAlive(&id)
		record(label, "StartSnapshotSet", hr)
		if hr == 0 {
			started = append(started, c)
			r.OwnedEmptySetsStarted++
		} else if label == "contender" && hr == setInProgress {
			r.ContenderBlocked = true
		} else {
			r.Phase = "start_unavailable_no_retry"
			return
		}
	}
	r.Phase = "probe_completed"
	return
}

func main() {
	if len(os.Args) != 2 || (os.Args[1] != "--probe" && os.Args[1] != "--available") {
		fmt.Fprintln(os.Stderr, "usage: vss-lockprobe --probe | --available")
		os.Exit(2)
	}
	r := run(os.Args[1] == "--probe")
	if json.NewEncoder(os.Stdout).Encode(r) != nil {
		os.Exit(1)
	}
	if r.Phase != "probe_completed" {
		os.Exit(1)
	}
}
