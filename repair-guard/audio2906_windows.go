package main

// One normal Web download for the existing 2895 production. This is neither
// a Downloads allowance nor recovery of the missing original private registry.
import (
	"os"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"
)

const audio2906Source = "C:/Users/makhmeto/Downloads/ElevenLabs_2026-10-19T06_16_40_Otani - Inviting, Clear and Measured_pvc_sp100_s50_sb75_v4.mp3"
const audio2906Receipt = "D:/CC/Work/resume-all-20261019-1104/video-2906-fresh-audio-guard.private.json"
const audio2906ReceiptSHA = "f1b0009c981f07ca80f8fc97e14fcc938c92c453b6ecaf34293e207c60b4ce27"
const audio2906ProductionSpec = "D:/CC/Tools/todo/specs/2895-d56c1438370f4ebdbf490371f02744a9.md"
const audio2906ProductionSpecSHA = "4fca840c00373530e786d34b180a56eaacdd361a9d2f8f7a5c7def6cc1e3fdc7"
const audio2906Destination = dailyVideoRoot + "/yukuhashi/2026-10-20"
const audio2906Input = audio2906Destination + "/narration-2906-v2.txt"
const audio2906InputSHA = "3e73a7c1fa98ed715b8a12adacf2a354ade484c547987a089bc69b7806e481ff"

type audio2906Metadata struct {
	Volume                    uint32
	ID, Size                  uint64
	Created, Written, Changed int64
	Links, Attributes         uint32
	Owner                     string
}

// Supplemental FILE_READ_ATTRIBUTES observation on 2026-10-19, independently
// compared with the producer's original size/time and normal Download record.
// The original receipt did NOT record an NTFS ID. No media bytes were opened.
var audio2906Expected = audio2906Metadata{
	Volume: 2229119083, ID: 5348024558439304, Size: 1928473,
	Created: 134368642843373104, Written: 134368642844993044, Changed: 134368642844993044,
	Links: 1, Attributes: syscall.FILE_ATTRIBUTE_ARCHIVE,
	Owner: "S-1-5-21-3727265621-1182195423-444854664-1001",
}

// Use one attribute/security-only handle for file identity, change time and
// owner. Never request FILE_READ_DATA or inspect/decode/hash media to classify.
func audio2906Identity(path string) (audio2906Metadata, error) {
	var out audio2906Metadata
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return out, err
	}
	h, err := syscall.CreateFile(p, 0x80|0x20000, 7, nil, syscall.OPEN_EXISTING, syscall.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return out, err
	}
	defer syscall.CloseHandle(h)
	var info syscall.ByHandleFileInformation
	if err := syscall.GetFileInformationByHandle(h, &info); err != nil {
		return out, err
	}
	var basic struct {
		Created, Accessed, Written, Changed int64
		Attributes, Padding                 uint32
	}
	r, _, err := syscall.NewLazyDLL("kernel32.dll").NewProc("GetFileInformationByHandleEx").Call(uintptr(h), 0, uintptr(unsafe.Pointer(&basic)), unsafe.Sizeof(basic))
	if r == 0 {
		return out, err
	}
	var owner *syscall.SID
	var descriptor uintptr
	status, _, _ := syscall.NewLazyDLL("advapi32.dll").NewProc("GetSecurityInfo").Call(uintptr(h), 1, 1, uintptr(unsafe.Pointer(&owner)), 0, 0, 0, uintptr(unsafe.Pointer(&descriptor)))
	if descriptor != 0 {
		defer syscall.LocalFree(syscall.Handle(descriptor))
	}
	if status != 0 {
		return out, syscall.Errno(status)
	}
	if owner == nil {
		return out, os.ErrInvalid
	}
	sid, err := owner.String()
	if err != nil {
		return out, err
	}
	out = audio2906Metadata{Volume: info.VolumeSerialNumber, ID: uint64(info.FileIndexHigh)<<32 | uint64(info.FileIndexLow),
		Size: uint64(info.FileSizeHigh)<<32 | uint64(info.FileSizeLow), Created: basic.Created, Written: basic.Written, Changed: basic.Changed,
		Links: info.NumberOfLinks, Attributes: info.FileAttributes, Owner: sid}
	return out, nil
}

type audio2906IO struct {
	source, receipt, receiptSHA, input, inputSHA, destination string
	expected                                                  audio2906Metadata
	at                                                        time.Time
	protected                                                 func(string) bool
	read                                                      func(string, int64) ([]byte, bool)
	identity                                                  func(string) (audio2906Metadata, error)
	qualified                                                 func() bool
}

func audio2906Qualified() bool {
	db, err := readObject(productStore, 64<<20)
	if err != nil || !dailyVideoActorAllowed(db, dailyVideoActorRoot, dailyVideoActorSpec, dailyVideoActorSpecSHA, sourceVerified, legacyMatch) ||
		!currentQualification(db, "youtube-daily", 2895, dailyVideoToolRoot, audio2906ProductionSpec, audio2906ProductionSpecSHA, legacyMatch) {
		return false
	}
	row := itemAt(db, 2895)
	if row == nil || row["dispatchable"] != true || row["stopped"] == true || row["owner_stopped"] == true {
		return false
	}
	switch str(row["status"]) {
	case "todo", "doing", "wait":
	default:
		return false
	}
	io := dailyVideoIO{jobs: dailyVideoJobsRoot, toolRoot: dailyVideoToolRoot, protected: legacyMatch, readObject: readObject}
	return currentPlain(audio2906Destination, true) && !legacyMatch(audio2906Destination) && dailyVideoJobOwned(row, audio2906Destination, io)
}

func audio2906PathAllowed(path string) bool {
	return audio2906AllowedWith(path, audio2906IO{
		source: audio2906Source, receipt: audio2906Receipt, receiptSHA: audio2906ReceiptSHA,
		input: audio2906Input, inputSHA: audio2906InputSHA, destination: audio2906Destination,
		expected: audio2906Expected, at: time.Now(), protected: legacyMatch, identity: audio2906Identity,
		read: func(p string, limit int64) ([]byte, bool) { return conversationBytes(p, limit, legacyMatch) }, qualified: audio2906Qualified,
	})
}

func audio2906AllowedWith(path string, io audio2906IO) bool {
	// Reject siblings/alternate namespaces before performing even metadata IO.
	if !filepath.IsAbs(path) || !same(path, io.source) ||
		io.at.Before(time.Date(2026, 10, 19, 6, 19, 39, 0, time.UTC)) ||
		!io.at.Before(time.Date(2026, 10, 20, 15, 0, 0, 0, time.UTC)) {
		return false
	}
	check := func() bool {
		for _, p := range []string{path, io.receipt, io.input, io.destination} {
			if io.protected(p) || !conversationPhysical(p) {
				return false
			}
		}
		if !currentPlain(path, false) || !io.qualified() {
			return false
		}
		info, err := io.identity(path)
		return err == nil && info == io.expected && info.Links == 1 &&
			info.Attributes&(syscall.FILE_ATTRIBUTE_DIRECTORY|syscall.FILE_ATTRIBUTE_REPARSE_POINT) == 0
	}
	if !check() {
		return false
	}
	// These immutable independent pins bind the normal generation/download,
	// producer, fixed spec, original script and exact production day. Not media.
	pins := func() bool {
		for _, receipt := range []struct{ path, sha string }{{io.receipt, io.receiptSHA}, {io.input, io.inputSHA}} {
			b, ok := io.read(receipt.path, 64<<10)
			if !ok || hashBytes(b) != receipt.sha {
				return false
			}
		}
		return true
	}
	return pins() && check() && pins()
}
