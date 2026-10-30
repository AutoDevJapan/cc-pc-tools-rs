//go:build windows && amd64

// A finite diagnostic, not a resident collector. No ETL, file-name events,
// process-start events, VSS mutations, registry writes, or existing trace changes.
package main

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

const sessionName = "CC-VSS-4011-Finite-DiskIO"

var adv = syscall.NewLazyDLL(filepath.Join(os.Getenv("SystemRoot"), "System32", "advapi32.dll"))
var kernel = syscall.NewLazyDLL(filepath.Join(os.Getenv("SystemRoot"), "System32", "kernel32.dll"))
var tdh = syscall.NewLazyDLL(filepath.Join(os.Getenv("SystemRoot"), "System32", "tdh.dll")).NewProc("TdhGetProperty")
var control = adv.NewProc("ControlTraceW")

type guid struct {
	A    uint32
	B, C uint16
	D    [8]byte
}

var diskProvider = guid{0x3d6fa8d4, 0xfe05, 0x11d0, [8]byte{0x9d, 0xda, 0, 0xc0, 0x4f, 0xd7, 0xba, 0x7c}}

type wnode struct {
	Size, Provider     uint32
	History, Timestamp uint64
	GUID               guid
	Clock, Flags       uint32
}
type properties struct {
	W                                                                                      wnode
	BufferSize, MinimumBuffers, MaximumBuffers, MaximumFileSize                            uint32
	Mode, FlushTimer, EnableFlags, AgeLimit                                                uint32
	NumberOfBuffers, FreeBuffers, EventsLost, BuffersWritten, LogBuffersLost, RealtimeLost uint32
	LoggerThread                                                                           uintptr
	FileOffset, NameOffset                                                                 uint32
	Name                                                                                   [1024]uint16
}
type logfile struct {
	FileName, LoggerName           *uint16
	CurrentTime                    int64
	BuffersRead, Mode              uint32
	CurrentEvent                   [88]byte
	Header                         [280]byte
	BufferCallback                 uintptr
	BufferSize, Filled, EventsLost uint32
	Callback                       uintptr
	KernelTrace                    uint32
	Context                        uintptr
}
type header struct {
	Size, Type, Flags, Property     uint16
	Thread, Process                 uint32
	Timestamp                       uint64
	Provider                        guid
	ID                              uint16
	Version, Channel, Level, Opcode uint8
	Task                            uint16
	Keyword, ProcessorTime          uint64
	Activity                        guid
}
type record struct {
	H                               header
	BufferContext                   uint32
	ExtendedCount, UserLength       uint16
	Extended, UserData, UserContext uintptr
}
type descriptor struct {
	Name            uint64
	Index, Reserved uint32
}
type extent struct {
	Volume        string `json:"volume"`
	Disk          uint32 `json:"disk"`
	Start, Length uint64
}
type key struct {
	Volume string
	Thread uint32
}
type sample struct{ Bytes, Count, First uint64 }
type row struct {
	Volume        string `json:"volume"`
	Issuer        string `json:"issuer"`
	Bytes, Events uint64
}
type result struct {
	Phase                                                           string `json:"phase"`
	StartedUTC, FinishedUTC, TraceStartedUTC, TraceStoppedUTC       string
	Seconds                                                         int `json:"seconds"`
	StartCode, OpenCode, ProcessCode, StopCode, AbsentCode          uint32
	EventsLost, RealtimeLost, Unparsed, MapOverflow                 uint64
	LayoutUnchanged, ConsumerDrained, WatchdogExited, SessionAbsent bool
	Extents                                                         []extent
	Rows                                                            []row
	RawPayloadRetained                                              bool   `json:"raw_payload_retained"`
	FileNamesEnabled                                                bool   `json:"file_names_enabled"`
	Limitation                                                      string `json:"limitation"`
}

func props(id guid) *properties {
	p := new(properties)
	p.W.Size = uint32(unsafe.Sizeof(*p))
	p.W.GUID = id
	p.W.Flags = 0x20000
	p.W.Clock = 2
	p.NameOffset = uint32(unsafe.Offsetof(p.Name))
	p.BufferSize = 16
	p.MaximumBuffers = 128
	p.Mode = 0x02000100
	p.EnableFlags = 0x10000100
	p.FlushTimer = 1
	name, _ := syscall.UTF16FromString(sessionName)
	copy(p.Name[:], name)
	return p
}
func query(id guid, code uint32) (*properties, uint32) {
	p := props(id)
	n, _ := syscall.UTF16PtrFromString(sessionName)
	c, _, _ := control.Call(0, uintptr(unsafe.Pointer(n)), uintptr(unsafe.Pointer(p)), uintptr(code))
	return p, uint32(c)
}

// Stop by the queried native handle after matching the GUID; never stop by
// the fixed display name alone or replace an existing session.
func stopVerified(id guid, read func() (*properties, uint32), stop func(uint64, *properties) uint32) (*properties, uint32) {
	q, c := read()
	if c != 0 {
		return q, c
	}
	if q.W.GUID != id || q.W.History == 0 {
		return q, 13
	}
	p := props(id)
	return p, stop(q.W.History, p)
}
func stopMatchingStats(id guid) (*properties, uint32) {
	return stopVerified(id, func() (*properties, uint32) { return query(id, 0) }, func(h uint64, p *properties) uint32 {
		code, _, _ := control.Call(uintptr(h), 0, uintptr(unsafe.Pointer(p)), 1)
		return uint32(code)
	})
}
func stopMatching(id guid) uint32 { _, c := stopMatchingStats(id); return c }
func getExtents() ([]extent, error) {
	var out []extent
	for _, volume := range []string{"C:", "D:"} {
		name, _ := syscall.UTF16PtrFromString(`\\.\` + volume)
		h, e := syscall.CreateFile(name, 0, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE, nil, syscall.OPEN_EXISTING, 0, 0)
		if e != nil {
			return nil, fmt.Errorf("volume extent open failed")
		}
		b := make([]byte, 256)
		var n uint32
		e = syscall.DeviceIoControl(h, 0x00560000, nil, 0, &b[0], uint32(len(b)), &n, nil)
		syscall.CloseHandle(h)
		if e != nil || n < 32 || *(*uint32)(unsafe.Pointer(&b[0])) != 1 {
			return nil, fmt.Errorf("single extent metadata required")
		}
		x := extent{Volume: volume, Disk: *(*uint32)(unsafe.Pointer(&b[8])), Start: *(*uint64)(unsafe.Pointer(&b[16])), Length: *(*uint64)(unsafe.Pointer(&b[24]))}
		if x.Length == 0 || x.Start+x.Length < x.Start {
			return nil, fmt.Errorf("invalid extent")
		}
		out = append(out, x)
	}
	if out[0].Disk == out[1].Disk && out[0].Start < out[1].Start+out[1].Length && out[1].Start < out[0].Start+out[0].Length {
		return nil, fmt.Errorf("overlapping extents")
	}
	return out, nil
}
func volumeFor(es []extent, disk uint32, off, bytes uint64) string {
	if bytes == 0 || off+bytes < off {
		return "unresolved"
	}
	for _, e := range es {
		if e.Disk == disk && off >= e.Start && off+bytes <= e.Start+e.Length {
			return e.Volume
		}
	}
	return "other"
}
func numeric(r *record, name string, size uint32) (uint64, bool) {
	// Only four documented numeric DiskIo properties are ever requested.
	if name != "DiskNumber" && name != "TransferSize" && name != "ByteOffset" && name != "IssuingThreadId" {
		return 0, false
	}
	n, _ := syscall.UTF16PtrFromString(name)
	d := descriptor{Name: uint64(uintptr(unsafe.Pointer(n))), Index: 0xffffffff}
	var v uint64
	c, _, _ := tdh.Call(uintptr(unsafe.Pointer(r)), 0, 0, 1, uintptr(unsafe.Pointer(&d)), uintptr(size), uintptr(unsafe.Pointer(&v)))
	runtime.KeepAlive(n)
	return v, uint32(c) == 0
}
func times(h syscall.Handle, thread bool) (uint64, bool) {
	var a, b, c, d syscall.Filetime
	var e error
	if thread {
		v, _, _ := kernel.NewProc("GetThreadTimes").Call(uintptr(h), uintptr(unsafe.Pointer(&a)), uintptr(unsafe.Pointer(&b)), uintptr(unsafe.Pointer(&c)), uintptr(unsafe.Pointer(&d)))
		if v == 0 {
			return 0, false
		}
	} else {
		e = syscall.GetProcessTimes(h, &a, &b, &c, &d)
	}
	return uint64(a.HighDateTime)<<32 | uint64(a.LowDateTime), e == nil
}
func category(path string) string {
	n := strings.ToLower(filepath.Base(path))
	switch n {
	case "chrome.exe", "msedge.exe", "firefox.exe":
		return "browser"
	case "codex.exe", "codex_jobs.exe", "scheduler.exe", "todo.exe", "cc-console.exe":
		return "known_automation"
	case "cargo.exe", "rustc.exe", "go.exe", "compile.exe", "link.exe", "sccache.exe":
		return "compiler_or_cache"
	case "vmmemwsl", "wsl.exe", "vmmem", "com.docker.backend.exe":
		return "virtualization"
	case "bun.exe", "node.exe":
		return "javascript_runtime"
	}
	if strings.HasPrefix(strings.ToLower(path), strings.ToLower(filepath.Join(os.Getenv("SystemRoot"), ""))+`\`) {
		return "windows"
	}
	return "other"
}
func issuer(thread uint32, first uint64) string {
	if thread == 0 || first == 0 {
		return "unattributed"
	}
	h, _, _ := kernel.NewProc("OpenThread").Call(0x0800, 0, uintptr(thread))
	if h == 0 {
		return "unattributed"
	}
	defer syscall.CloseHandle(syscall.Handle(h))
	created, ok := times(syscall.Handle(h), true)
	if !ok || created > first {
		return "unattributed"
	} // rejects a reused TID
	pid, _, _ := kernel.NewProc("GetProcessIdOfThread").Call(h)
	if pid == 4 {
		return "system_issuer_not_original_writer"
	}
	p, e := syscall.OpenProcess(0x1000, false, uint32(pid))
	if e != nil {
		return "unattributed"
	}
	defer syscall.CloseHandle(p)
	created, ok = times(p, false)
	if !ok || created > first {
		return "unattributed"
	}
	b := make([]uint16, 32768)
	n := uint32(len(b))
	good, _, _ := kernel.NewProc("QueryFullProcessImageNameW").Call(uintptr(p), 0, uintptr(unsafe.Pointer(&b[0])), uintptr(unsafe.Pointer(&n)))
	if good == 0 {
		return "unattributed"
	}
	return category(syscall.UTF16ToString(b[:n])) // path stays in memory; no name is exported
}
func aggregate(m map[key]sample) []row {
	totals := make(map[string]row)
	for k, s := range m {
		c := issuer(k.Thread, s.First)
		id := k.Volume + "/" + c
		r := totals[id]
		r.Volume = k.Volume
		r.Issuer = c
		r.Bytes += s.Bytes
		r.Events += s.Count
		totals[id] = r
	}
	var rows []row
	for _, r := range totals {
		rows = append(rows, r)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Volume != rows[j].Volume {
			return rows[i].Volume < rows[j].Volume
		}
		return rows[i].Issuer < rows[j].Issuer
	})
	return rows
}
func watchdog(id guid) int {
	done := make(chan struct{})
	go func() { _, _ = bufio.NewReader(io.LimitReader(os.Stdin, 1)).ReadByte(); close(done) }()
	select {
	case <-done:
	case <-time.After(45 * time.Second):
		// A native start may have been delayed. Keep the cleanup companion alive
		// beyond the caller's 65s timeout, then perform one final identity check.
		_ = stopMatching(id)
		select {
		case <-done:
		case <-time.After(30 * time.Second):
		}
	}
	code := stopMatching(id)
	if code == 0 || code == 4201 || code == 13 {
		return 0
	}
	return 1
}
func startWatchdog(id guid) (*exec.Cmd, io.WriteCloser, error) {
	self, e := os.Executable()
	if e != nil {
		return nil, nil, e
	}
	raw := unsafe.Slice((*byte)(unsafe.Pointer(&id)), 16)
	c := exec.Command(self, "--owned-cleanup", hex.EncodeToString(raw))
	c.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000, HideWindow: true}
	pipe, e := c.StdinPipe()
	if e != nil {
		return nil, nil, e
	}
	if e = c.Start(); e != nil {
		pipe.Close()
		return nil, nil, e
	}
	return c, pipe, nil
}
func collect() (r result) {
	r.Phase = "preparation_failed_no_trace"
	r.StartedUTC = time.Now().UTC().Format(time.RFC3339Nano)
	r.Seconds = 20
	r.Limitation = "Issued disk writes only. System paging writes do not identify the original application; short current sample cannot prove a historical cause."
	defer func() { r.FinishedUTC = time.Now().UTC().Format(time.RFC3339Nano) }()
	var e error
	r.Extents, e = getExtents()
	if e != nil {
		return
	}
	var id guid
	if _, e = rand.Read(unsafe.Slice((*byte)(unsafe.Pointer(&id)), 16)); e != nil {
		return
	}
	watch, pipe, e := startWatchdog(id)
	if e != nil {
		return
	}
	defer func() {
		pipe.Close()
		done := make(chan error, 1)
		go func() { done <- watch.Wait() }()
		select {
		case e := <-done:
			r.WatchdogExited = e == nil
		case <-time.After(7 * time.Second):
			r.WatchdogExited = false
		}
		if !r.WatchdogExited {
			r.Phase = "watchdog_exit_unverified"
		}
	}()
	p := props(id)
	name, _ := syscall.UTF16PtrFromString(sessionName)
	var handle uint64
	c, _, _ := adv.NewProc("StartTraceW").Call(uintptr(unsafe.Pointer(&handle)), uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(p)))
	r.StartCode = uint32(c)
	if c != 0 {
		r.Phase = "start_refused_existing_sessions_untouched"
		return
	}
	r.TraceStartedUTC = time.Now().UTC().Format(time.RFC3339Nano)
	stopped := false
	defer func() {
		if !stopped {
			r.StopCode = stopMatching(id)
		}
		_, r.AbsentCode = query(id, 0)
		r.SessionAbsent = r.AbsentCode == 4201
		if !r.SessionAbsent {
			r.Phase = "owned_trace_cleanup_unverified"
		}
	}()
	samples := make(map[key]sample)
	var mu sync.Mutex
	var unparsed, overflow uint64
	callback := syscall.NewCallback(func(er *record) uintptr {
		if er.H.Provider != diskProvider || er.H.Opcode != 11 {
			return 0
		}
		disk, dok := numeric(er, "DiskNumber", 4)
		n, nok := numeric(er, "TransferSize", 4)
		off, ook := numeric(er, "ByteOffset", 8)
		tid, tok := numeric(er, "IssuingThreadId", 4)
		mu.Lock()
		defer mu.Unlock()
		if !dok || !nok || !ook || !tok || n == 0 {
			unparsed++
			return 0
		}
		k := key{volumeFor(r.Extents, uint32(disk), off, n), uint32(tid)}
		s, exists := samples[k]
		if !exists && len(samples) >= 4096 {
			overflow++
			return 0
		}
		s.Bytes += n
		s.Count++
		if s.First == 0 || er.H.Timestamp < s.First {
			s.First = er.H.Timestamp
		}
		samples[k] = s
		return 0
	})
	lf := logfile{LoggerName: name, Mode: 0x10000100, Callback: callback} // REAL_TIME | EVENT_RECORD
	consumer, _, err := adv.NewProc("OpenTraceW").Call(uintptr(unsafe.Pointer(&lf)))
	if consumer == ^uintptr(0) {
		r.OpenCode = uint32(err.(syscall.Errno))
		r.Phase = "consumer_open_failed"
		return
	}
	closed := false
	defer func() {
		if !closed {
			adv.NewProc("CloseTrace").Call(consumer)
		}
	}()
	drained := make(chan uint32, 1)
	go func() {
		h := uint64(consumer)
		s, _, _ := adv.NewProc("ProcessTrace").Call(uintptr(unsafe.Pointer(&h)), 1, 0, 0)
		drained <- uint32(s)
	}()
	time.Sleep(20 * time.Second)
	stats, stopCode := stopMatchingStats(id)
	r.StopCode = stopCode
	r.TraceStoppedUTC = time.Now().UTC().Format(time.RFC3339Nano)
	stopped = r.StopCode == 0
	// STOP flushes the real-time buffers; ProcessTrace must finish before reading totals.
	select {
	case s := <-drained:
		r.ProcessCode = s
		r.ConsumerDrained = s == 0
	case <-time.After(8 * time.Second):
		adv.NewProc("CloseTrace").Call(consumer)
		closed = true
		select {
		case s := <-drained:
			r.ProcessCode = s
		case <-time.After(3 * time.Second):
			r.Phase = "consumer_drain_unverified"
			return
		}
	}
	adv.NewProc("CloseTrace").Call(consumer)
	closed = true
	// A complete result needs STOP statistics, so query immediately before STOP is
	// insufficient: loss is also checked via the consumer's final logfile fields.
	r.EventsLost = uint64(stats.EventsLost) + uint64(lf.EventsLost)
	r.RealtimeLost = uint64(stats.RealtimeLost)
	after, e := getExtents()
	r.LayoutUnchanged = e == nil && len(after) == len(r.Extents)
	if r.LayoutUnchanged {
		for i := range after {
			if after[i] != r.Extents[i] {
				r.LayoutUnchanged = false
			}
		}
	}
	mu.Lock()
	r.Rows = aggregate(samples)
	r.Unparsed = unparsed
	r.MapOverflow = overflow
	mu.Unlock()
	r.Phase = "sample_incomplete_not_cause_evidence"
	if r.ConsumerDrained && r.StopCode == 0 && r.EventsLost == 0 && r.RealtimeLost == 0 && r.Unparsed == 0 && r.MapOverflow == 0 && r.LayoutUnchanged && len(r.Rows) > 0 {
		r.Phase = "finite_sample_complete_retention_unverified"
	}
	return
}
func main() {
	if len(os.Args) == 3 && os.Args[1] == "--owned-cleanup" {
		b, e := hex.DecodeString(os.Args[2])
		if e != nil || len(b) != 16 {
			os.Exit(2)
		}
		var id guid
		copy(unsafe.Slice((*byte)(unsafe.Pointer(&id)), 16), b)
		os.Exit(watchdog(id))
	}
	if len(os.Args) != 2 || os.Args[1] != "--collect-20s" {
		fmt.Fprintln(os.Stderr, "usage: diskio-once --collect-20s (administrative finite diagnostic)")
		os.Exit(2)
	}
	if unsafe.Sizeof(properties{}) != 2168 || unsafe.Sizeof(logfile{}) != 448 || unsafe.Sizeof(record{}) != 112 {
		fmt.Fprintln(os.Stderr, "unsupported native ABI")
		os.Exit(2)
	}
	r := collect()
	if json.NewEncoder(os.Stdout).Encode(r) != nil {
		os.Exit(1)
	}
	if r.Phase != "finite_sample_complete_retention_unverified" || !r.SessionAbsent || !r.WatchdogExited {
		os.Exit(1)
	}
}
