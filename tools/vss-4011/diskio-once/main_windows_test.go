//go:build windows && amd64

package main

import (
	"encoding/json"
	"os"
	"strings"
	"syscall"
	"testing"
	"unsafe"
)

func TestNativeLayoutsAndOffsets(t *testing.T) {
	if unsafe.Sizeof(wnode{}) != 48 || unsafe.Offsetof(properties{}.Name) != 120 || unsafe.Sizeof(properties{}) != 2168 || unsafe.Sizeof(header{}) != 80 || unsafe.Sizeof(record{}) != 112 || unsafe.Sizeof(logfile{}) != 448 {
		t.Fatal("native ABI differs")
	}
	if unsafe.Offsetof(logfile{}.Callback) != 424 || unsafe.Offsetof(logfile{}.EventsLost) != 416 || unsafe.Offsetof(record{}.UserData) != 96 {
		t.Fatal("native field offsets differ")
	}
}
func TestPropertiesEnableOnlyNumericDiskEvents(t *testing.T) {
	p := props(guid{A: 5})
	if p.FileOffset != 0 || p.MaximumFileSize != 0 || p.Mode != 0x02000100 || p.EnableFlags != 0x10000100 || p.W.Clock != 2 {
		t.Fatal("trace may persist data or enable unrelated events")
	}
	if p.BufferSize*p.MaximumBuffers > 2048 || p.NameOffset != 120 {
		t.Fatal("trace buffer contract changed")
	}
}
func TestVolumeBoundaryCases(t *testing.T) {
	es := []extent{{Volume: "C:", Disk: 0, Start: 100, Length: 100}, {Volume: "D:", Disk: 1, Start: 100, Length: 100}}
	for _, tc := range []struct {
		disk   uint32
		off, n uint64
		want   string
	}{
		{0, 100, 100, "C:"}, {0, 199, 1, "C:"}, {1, 100, 1, "D:"}, {0, 99, 2, "other"}, {0, 199, 2, "other"}, {2, 100, 1, "other"}, {0, 100, 0, "unresolved"}, {0, ^uint64(0), 2, "unresolved"},
	} {
		if got := volumeFor(es, tc.disk, tc.off, tc.n); got != tc.want {
			t.Fatalf("%+v: %s", tc, got)
		}
	}
}
func TestCategoriesNeverExportPrivateExecutable(t *testing.T) {
	for _, tc := range []struct{ path, want string }{
		{`C:\Apps\chrome.exe`, "browser"}, {`C:\Tools\bun.exe`, "javascript_runtime"}, {`D:\Build\rustc.exe`, "compiler_or_cache"}, {`C:\Apps\private-app-name.exe`, "other"},
		{os.Getenv("SystemRoot") + `\System32\svchost.exe`, "windows"},
	} {
		if c := category(tc.path); c != tc.want {
			t.Fatalf("category %s, want %s", c, tc.want)
		}
	}
	b, _ := json.Marshal(aggregate(map[key]sample{{Volume: "C:", Thread: 0}: {Bytes: 32, Count: 2, First: 1}}))
	if strings.Contains(string(b), "private") || !strings.Contains(string(b), "unattributed") {
		t.Fatal("unknown thread not anonymous")
	}
}
func TestNonNumericPropertyRejectedBeforeTDH(t *testing.T) {
	if _, ok := numeric(nil, "FileName", 8); ok {
		t.Fatal("file-name property accepted")
	}
	if _, ok := numeric(nil, "CommandLine", 4); ok {
		t.Fatal("command line property accepted")
	}
}

func TestCleanupPreservesForeignAndUnknownSessions(t *testing.T) {
	own := guid{A: 15}
	for _, tc := range []struct {
		name      string
		id        guid
		handle    uint64
		queryCode uint32
		wantCalls int
		wantCode  uint32
	}{
		{"owned", own, 72, 0, 1, 0}, {"same_name_foreign", guid{A: 16}, 72, 0, 0, 13}, {"missing_handle", own, 0, 0, 0, 13}, {"query_refused", own, 72, 5, 0, 5}, {"absent", own, 0, 4201, 0, 4201},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			_, code := stopVerified(own, func() (*properties, uint32) { p := props(tc.id); p.W.History = tc.handle; return p, tc.queryCode }, func(h uint64, p *properties) uint32 {
				calls++
				if h != 72 || p.W.GUID != own {
					t.Fatal("wrong native session selected")
				}
				p.EventsLost = 3
				return 0
			})
			if code != tc.wantCode || calls != tc.wantCalls {
				t.Fatalf("calls=%d code=%d", calls, code)
			}
		})
	}
}

func TestNativeCallbackSignatureRegisters(t *testing.T) {
	p := syscall.NewCallback(func(r *record) uintptr { return 0 })
	if p == 0 {
		t.Fatal("native event-record callback did not register")
	}
}
