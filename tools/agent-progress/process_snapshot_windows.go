//go:build windows

package main

import (
	"context"
	"errors"
	"os"
	"syscall"
	"unsafe"
)

// Native layout from Microsoft's NtQuerySystemInformation documentation:
// https://learn.microsoft.com/en-us/windows/win32/api/winternl/nf-winternl-ntquerysysteminformation
// MS-TSTS names the three times inside the 48-byte reserved prefix and gives
// CreateTime's units (100ns since 1601 UTC):
// https://learn.microsoft.com/en-us/openspecs/windows_protocols/ms-tsts/96702c7d-6f5f-4965-8f29-5fdbea73278e
// MS-TSTS is a protocol structure, not a promise about the native ABI. This
// semantic overlay is therefore used only on 64-bit Windows and only after a
// self-process GetProcessTimes identity cross-check on each acquisition.
// No process names, buffer pointers, or thread data are dereferenced/exported.
type nativeUnicodeString struct {
	Length, MaximumLength uint16
	Buffer                uintptr
}

type systemProcessInformation struct {
	NextEntryOffset, NumberOfThreads                                           uint32
	ReservedPrefix                                                             [3]int64
	CreateTime, UserTime, KernelTime                                           int64
	ImageName                                                                  nativeUnicodeString
	BasePriority                                                               int32
	UniqueProcessID, ParentProcessID                                           uintptr
	HandleCount, SessionID                                                     uint32
	Reserved3                                                                  uintptr
	PeakVirtualSize, VirtualSize                                               uintptr
	Reserved4                                                                  uint32
	PeakWorkingSetSize, WorkingSetSize                                         uintptr
	Reserved5, QuotaPagedPoolUsage, Reserved6                                  uintptr
	QuotaNonPagedPoolUsage, PagefileUsage, PeakPagefileUsage, PrivatePageCount uintptr
	Reserved7                                                                  [6]int64
}

type systemThreadInformation struct {
	Reserved1                          [3]int64
	Reserved2                          uint32
	StartAddress                       uintptr
	ClientID                           [2]uintptr
	Priority, BasePriority             int32
	Reserved3, ThreadState, WaitReason uint32
}

const (
	systemProcessInformationClass        = 5
	statusInfoLengthMismatch      uint32 = 0xc0000004
	statusBufferTooSmall          uint32 = 0xc0000023
	statusBufferOverflow          uint32 = 0x80000005
	processSnapshotMaxBytes              = 32 << 20
	processSnapshotAttempts              = 6
)

var ntQuerySystemInformation = syscall.NewLazyDLL("ntdll.dll").NewProc("NtQuerySystemInformation")

func supportedProcessLayout() bool {
	var h systemProcessInformation
	return unsafe.Sizeof(uintptr(0)) == 8 && unsafe.Sizeof(nativeUnicodeString{}) == 16 &&
		unsafe.Sizeof(h) == 256 && unsafe.Sizeof(systemThreadInformation{}) == 80 &&
		unsafe.Offsetof(h.CreateTime) == 32 && unsafe.Offsetof(h.UniqueProcessID) == 80 &&
		unsafe.Offsetof(h.SessionID) == 100
}

func parseProcessSnapshot(data []byte, returned uint32) (map[uint32]processObservation, error) {
	if !supportedProcessLayout() {
		return nil, errors.New("unsupported_process_layout")
	}
	headerSize := int(unsafe.Sizeof(systemProcessInformation{}))
	threadSize := uint64(unsafe.Sizeof(systemThreadInformation{}))
	if returned == 0 || uint64(returned) > uint64(len(data)) || int(returned) < headerSize {
		return nil, errors.New("invalid_process_length")
	}
	data = data[:int(returned)]
	processes := map[uint32]processObservation{}
	for offset := 0; ; {
		remaining := len(data) - offset
		if remaining < headerSize {
			return nil, errors.New("truncated_process_header")
		}
		var h systemProcessInformation
		// Copy into aligned typed storage. Never cast an untrusted next-offset
		// directly to a pointer or dereference the returned ImageName.Buffer.
		copy(unsafe.Slice((*byte)(unsafe.Pointer(&h)), headerSize), data[offset:offset+headerSize])
		entryBytes := remaining
		if h.NextEntryOffset != 0 {
			next := uint64(h.NextEntryOffset)
			if next%8 != 0 || next < uint64(headerSize) || next > uint64(remaining-headerSize) {
				return nil, errors.New("invalid_process_next_offset")
			}
			entryBytes = int(next)
		}
		if uint64(h.NumberOfThreads)*threadSize > uint64(entryBytes-headerSize) ||
			uint64(h.UniqueProcessID) > uint64(^uint32(0)) || h.CreateTime < 0 {
			return nil, errors.New("invalid_process_entry")
		}
		pid := uint32(h.UniqueProcessID)
		if _, duplicate := processes[pid]; duplicate {
			return nil, errors.New("duplicate_process_identity")
		}
		p := processObservation{known: true, alive: true, session: h.SessionID}
		if h.CreateTime > 0 {
			n := uint64(h.CreateTime)
			p.created = &n
		}
		processes[pid] = p
		if h.NextEntryOffset == 0 {
			return processes, nil
		}
		offset += int(h.NextEntryOffset)
	}
}

type processSnapshotQuery func([]byte, *uint32) uint32

func queryProcessSnapshot(ctx context.Context, query processSnapshotQuery) (map[uint32]processObservation, error) {
	size := 64 << 10
	for attempt := 0; attempt < processSnapshotAttempts; attempt++ {
		if ctx.Err() != nil {
			return nil, errors.New("process_snapshot_deadline")
		}
		buffer := make([]byte, size)
		var returned uint32
		status := query(buffer, &returned)
		if ctx.Err() != nil {
			return nil, errors.New("process_snapshot_deadline")
		}
		if status == statusInfoLengthMismatch || status == statusBufferTooSmall || status == statusBufferOverflow {
			next := uint64(size) * 2
			if uint64(returned) > next {
				next = uint64(returned) + (64 << 10)
			}
			if next > processSnapshotMaxBytes {
				return nil, errors.New("process_snapshot_byte_limit")
			}
			size = int(next)
			continue
		}
		if int32(status) < 0 {
			return nil, errors.New("process_snapshot_query_failed")
		}
		return parseProcessSnapshot(buffer, returned)
	}
	return nil, errors.New("process_snapshot_retry_limit")
}

func validatedProcessSnapshot(ctx context.Context) (map[uint32]processObservation, error) {
	if !supportedProcessLayout() || ntQuerySystemInformation.Find() != nil {
		return nil, errors.New("process_snapshot_unavailable")
	}
	selfPID := uint32(os.Getpid())
	self := nativeProbe(selfPID)
	if !self.known || !self.alive || self.created == nil {
		return nil, errors.New("process_snapshot_identity_unverified")
	}
	processes, err := queryProcessSnapshot(ctx, func(buffer []byte, returned *uint32) uint32 {
		status, _, _ := ntQuerySystemInformation.Call(systemProcessInformationClass,
			uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)), uintptr(unsafe.Pointer(returned)))
		return uint32(status)
	})
	if err != nil {
		return nil, err
	}
	observed, exists := processes[selfPID]
	if !exists || observed.created == nil || *observed.created != *self.created {
		return nil, errors.New("process_snapshot_identity_unverified")
	}
	return processes, nil
}
