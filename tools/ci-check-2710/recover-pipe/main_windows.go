// Finite recovery of tar's unread zero padding in our own capture process.
// No process memory, environment, credential, or nonzero pipe data is exposed.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

type handleEntry struct {
	Handle                             syscall.Handle
	Count, PointerCount                uintptr
	Access, Type, Attributes, Reserved uint32
}
type peekResult struct {
	available, n uint32
	prefix       [64]byte
	ok           bool
}

func main() {
	if len(os.Args) != 3 {
		os.Exit(120)
	}
	pid, e := strconv.ParseUint(os.Args[1], 10, 32)
	if e != nil {
		os.Exit(121)
	}
	proc, e := syscall.OpenProcess(0x1440, false, uint32(pid))
	if e != nil {
		os.Exit(122)
	}
	defer syscall.CloseHandle(proc)
	k := syscall.NewLazyDLL("kernel32.dll")
	var nameLen uint32 = 4096
	name := make([]uint16, nameLen)
	ok, _, _ := k.NewProc("QueryFullProcessImageNameW").Call(uintptr(proc), 0, uintptr(unsafe.Pointer(&name[0])), uintptr(unsafe.Pointer(&nameLen)))
	if ok == 0 || !strings.EqualFold(filepath.Clean(syscall.UTF16ToString(name[:nameLen])), filepath.Clean(os.Args[2])) {
		os.Exit(123)
	}
	nt := syscall.NewLazyDLL("ntdll.dll").NewProc("NtQueryInformationProcess")
	var data []byte
	for size := 65536; size <= 1<<22; size *= 2 {
		data = make([]byte, size)
		var needed uint32
		status, _, _ := nt.Call(uintptr(proc), 51, uintptr(unsafe.Pointer(&data[0])), uintptr(size), uintptr(unsafe.Pointer(&needed)))
		if uint32(status) == 0 {
			break
		}
		data = nil
	}
	if len(data) == 0 {
		os.Exit(124)
	}
	count := *(*uintptr)(unsafe.Pointer(&data[0]))
	offset := 2 * unsafe.Sizeof(uintptr(0))
	if count > 8192 || offset+count*unsafe.Sizeof(handleEntry{}) > uintptr(len(data)) {
		os.Exit(125)
	}
	entries := unsafe.Slice((*handleEntry)(unsafe.Pointer(&data[offset])), count)
	var chosen syscall.Handle
	matches := 0
	var available uint32
	for _, entry := range entries {
		if entry.Access&1 == 0 {
			continue
		}
		var h syscall.Handle
		r, _, _ := k.NewProc("DuplicateHandle").Call(uintptr(proc), uintptr(entry.Handle), ^uintptr(0), uintptr(unsafe.Pointer(&h)), 0, 0, 2)
		if r == 0 {
			continue
		}
		kind, _, _ := k.NewProc("GetFileType").Call(uintptr(h))
		if kind != 3 {
			syscall.CloseHandle(h)
			continue
		}
		ch := make(chan peekResult, 1)
		go func(handle syscall.Handle) {
			var p peekResult
			r, _, _ := k.NewProc("PeekNamedPipe").Call(uintptr(handle), uintptr(unsafe.Pointer(&p.prefix[0])), 64, uintptr(unsafe.Pointer(&p.n)), uintptr(unsafe.Pointer(&p.available)), 0)
			p.ok = r != 0
			ch <- p
		}(h)
		select {
		case p := <-ch:
			zero := p.n > 0
			for _, b := range p.prefix[:p.n] {
				if b != 0 {
					zero = false
				}
			}
			if p.ok && p.available > 0 && zero {
				chosen = h
				matches++
				available = p.available
			} else {
				syscall.CloseHandle(h)
			}
		case <-time.After(300 * time.Millisecond): /* process exit closes a stalled duplicate */
		}
	}
	fmt.Printf("owned_process_verified=true zero_padding_pipes=%d available_bytes=%d\n", matches, available)
	if matches != 1 {
		os.Exit(126)
	}
	defer syscall.CloseHandle(chosen)
	total := 0
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) && total < 1<<20 {
		var avail uint32
		r, _, err := k.NewProc("PeekNamedPipe").Call(uintptr(chosen), 0, 0, 0, uintptr(unsafe.Pointer(&avail)), 0)
		if r == 0 {
			if err == syscall.ERROR_BROKEN_PIPE {
				fmt.Printf("zero_padding_drained=%d\n", total)
				return
			}
			os.Exit(127)
		}
		if avail == 0 {
			time.Sleep(25 * time.Millisecond)
			continue
		}
		buf := make([]byte, avail)
		var n uint32
		if e = syscall.ReadFile(chosen, buf, &n, nil); e != nil {
			os.Exit(128)
		}
		for _, b := range buf[:n] {
			if b != 0 {
				os.Exit(129)
			}
		}
		total += int(n)
	}
	os.Exit(130)
}
