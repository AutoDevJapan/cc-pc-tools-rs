// A finite, read-only query of the documented VSS minimum. It never creates,
// deletes, resizes, or migrates snapshots, and does not open volume contents.
// Interface IDs and method order: Microsoft WinSDK vsmgmt.h.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"syscall"
	"time"
	"unsafe"
)

type guid struct {
	A uint32
	B uint16
	C uint16
	D [8]byte
}

type comObject struct{ vtable *[4]uintptr }

func failed(hr uintptr) bool { return int32(uint32(hr)) < 0 }

func query() (int64, string, uint32) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ole := syscall.NewLazyDLL("ole32.dll")
	hr, _, _ := ole.NewProc("CoInitializeEx").Call(0, 0)
	if failed(hr) {
		return 0, "CoInitializeEx", uint32(hr)
	}
	defer ole.NewProc("CoUninitialize").Call()
	class := guid{0x0b5a2c52, 0x3eb9, 0x470a, [8]byte{0x96, 0xe2, 0x6c, 0x6d, 0x45, 0x70, 0xe4, 0x0f}}
	iid := guid{0xfa7df749, 0x66e7, 0x4986, [8]byte{0xa2, 0x7f, 0xe2, 0xf0, 0x4a, 0xe5, 0x37, 0x72}}
	iid2 := guid{0x0f61ec39, 0xfe82, 0x45f2, [8]byte{0xa3, 0xf0, 0x76, 0x8b, 0x5d, 0x42, 0x71, 0x02}}
	var obj, obj2 *comObject
	hr, _, _ = ole.NewProc("CoCreateInstance").Call(uintptr(unsafe.Pointer(&class)), 0, 5, uintptr(unsafe.Pointer(&iid)), uintptr(unsafe.Pointer(&obj)))
	if failed(hr) || obj == nil {
		return 0, "CoCreateInstance", uint32(hr)
	}
	defer syscall.SyscallN(obj.vtable[2], uintptr(unsafe.Pointer(obj)))
	hr, _, _ = syscall.SyscallN(obj.vtable[0], uintptr(unsafe.Pointer(obj)), uintptr(unsafe.Pointer(&iid2)), uintptr(unsafe.Pointer(&obj2)))
	if failed(hr) || obj2 == nil {
		return 0, "QueryInterface", uint32(hr)
	}
	defer syscall.SyscallN(obj2.vtable[2], uintptr(unsafe.Pointer(obj2)))
	var minimum int64
	hr, _, _ = syscall.SyscallN(obj2.vtable[3], uintptr(unsafe.Pointer(obj2)), uintptr(unsafe.Pointer(&minimum)))
	return minimum, "GetMinDiffAreaSize", uint32(hr)
}

func main() {
	minimum, phase, hr := query()
	result := struct {
		UTC     string `json:"captured_utc"`
		Source  string `json:"source"`
		Phase   string `json:"phase"`
		HRESULT string `json:"hresult"`
		Minimum int64  `json:"minimum_bytes"`
		Changes int    `json:"changes"`
	}{time.Now().UTC().Format(time.RFC3339Nano), "IVssSnapshotMgmt2", phase, fmt.Sprintf("0x%08X", hr), minimum, 0}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		os.Exit(1)
	}
	if int32(hr) < 0 || minimum <= 0 {
		os.Exit(1)
	}
}
