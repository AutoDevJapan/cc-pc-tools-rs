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
		calls     int
		state     string
	}{
		{"owned", own, 72, 0, 1, "stopped"}, {"same_name_foreign", guid{A: 16}, 72, 0, 0, "foreign_session_present"},
		{"missing_handle", own, 0, 0, 0, "owned_handle_unavailable"}, {"query_refused", own, 72, 5, 0, "query_failed"}, {"absent", own, 0, 4201, 0, "absent"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			stats, r := stopVerified(own, func() (*properties, uint32) { p := props(tc.id); p.W.History = tc.handle; return p, tc.queryCode }, func(h uint64, p *properties) uint32 {
				calls++
				if h != 72 || p.W.GUID != own {
					t.Fatal("wrong native session selected")
				}
				p.EventsLost = 3
				return 0
			})
			if r.State != tc.state || r.QueryCode != tc.queryCode || calls != tc.calls || r.Attempted != (tc.calls == 1) {
				t.Fatalf("result=%+v calls=%d", r, calls)
			}
			if tc.calls == 1 && stats.EventsLost != 3 {
				t.Fatal("final STOP loss statistics not returned")
			}
		})
	}
}
func TestOwnershipGUIDRoundtripAndBadInput(t *testing.T) {
	own := guid{A: 15, B: 2, C: 3, D: [8]byte{1, 2, 3, 4, 5, 6, 7, 8}}
	got, e := parseGUID(encodeGUID(own))
	if e != nil || got != own {
		t.Fatal("durable ownership bytes differ")
	}
	for _, input := range []string{"", "ab", strings.Repeat("0", 32), strings.Repeat("g", 32)} {
		if _, e := parseGUID(input); e == nil {
			t.Fatal("invalid ownership accepted")
		}
	}
	b, _ := json.Marshal(result{OwnedGUID: encodeGUID(own), PrimaryStop: stopResult{State: "stop_failed", Attempted: true, NativeCode: 5}, CleanupStop: &stopResult{State: "stopped", Attempted: true}})
	var r result
	if json.Unmarshal(b, &r) != nil || r.PrimaryStop.NativeCode != 5 || r.CleanupStop.State != "stopped" || r.OwnedGUID != encodeGUID(own) {
		t.Fatal("first failure overwritten by cleanup")
	}
}
func TestNativeCallbackSignatureRegisters(t *testing.T) {
	p := syscall.NewCallback(func(r *record) uintptr { return 0 })
	if p == 0 {
		t.Fatal("native event-record callback did not register")
	}
}
