//go:build windows

package main

import (
	"bytes"
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"syscall"
	"unsafe"
)

// This input-free reader identifies one previously observed installed program.
// It does not admit client-records, update a file, or issue a data-access grant.
const installedGuardPath = "D:/CC/Tools/client-records/scrub391.exe"
const installedGuardSHA = "428d97e198b09ba7c2a10a319553aa0ed178fa5b74971ac158d72ae2b2eba841"
const installedGuardRevision = "f13663f77577b5cc7578a16b0695186f31e832cd"
const installedGuardModule = "github.com/AutoDevJapan/cc-pc-tools-rs/repair-guard"

type softwareIdentity struct {
	Volume  string `json:"volume"`
	FileID  string `json:"file_id"`
	Created string `json:"created"`
}

type softwareObservation struct {
	Verified          bool              `json:"verified"`
	Domain            string            `json:"domain,omitempty"`
	SHA256            string            `json:"sha256,omitempty"`
	Bytes             int               `json:"bytes,omitempty"`
	SourceRevision    string            `json:"source_revision,omitempty"`
	Identity          *softwareIdentity `json:"identity,omitempty"`
	DataAccessGranted bool              `json:"data_access_granted"`
	UpdatePerformed   bool              `json:"update_performed"`
}

func pinnedBuildMatches(raw []byte, expectedSHA string, read func([]byte) (*debug.BuildInfo, error)) bool {
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != expectedSHA {
		return false
	}
	info, err := read(raw)
	if err != nil || info.Path != installedGuardModule {
		return false
	}
	settings := map[string]string{}
	for _, s := range info.Settings {
		if _, exists := settings[s.Key]; exists {
			return false
		}
		settings[s.Key] = s.Value
	}
	return settings["vcs"] == "git" && settings["vcs.revision"] == installedGuardRevision && settings["vcs.modified"] == "false" && settings["GOOS"] == "windows" && settings["GOARCH"] == "amd64"
}

func handlePath(handle syscall.Handle) (string, bool) {
	buffer := make([]uint16, 32768)
	n, _, _ := syscall.NewLazyDLL("kernel32.dll").NewProc("GetFinalPathNameByHandleW").Call(uintptr(handle), uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)), 0)
	if n == 0 || n >= uintptr(len(buffer)) {
		return "", false
	}
	s := syscall.UTF16ToString(buffer[:n])
	s = strings.TrimPrefix(s, `\\?\`)
	return filepath.Clean(s), true
}

func openPlain(path string, directory bool) (syscall.Handle, bool) {
	ptr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, false
	}
	access, share, flags := uint32(syscall.GENERIC_READ), uint32(syscall.FILE_SHARE_READ), uint32(0x00200000)
	if directory {
		access = 0x80
		share = syscall.FILE_SHARE_READ | syscall.FILE_SHARE_WRITE
		flags |= 0x02000000
	}
	h, err := syscall.CreateFile(ptr, access, share, nil, syscall.OPEN_EXISTING, flags, 0)
	if err != nil {
		return 0, false
	}
	var info syscall.ByHandleFileInformation
	actual, ok := handlePath(h)
	if !ok || !strings.EqualFold(actual, filepath.Clean(path)) || syscall.GetFileInformationByHandle(h, &info) != nil || info.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0 || (info.FileAttributes&syscall.FILE_ATTRIBUTE_DIRECTORY != 0) != directory || !directory && info.NumberOfLinks != 1 {
		syscall.CloseHandle(h)
		return 0, false
	}
	return h, true
}

// Parents are retained without delete sharing before the child is opened.
// The file excludes write/delete sharing until the verified bytes are consumed.
func withPinnedSoftware(path string, consume func([]byte, softwareIdentity) bool) bool {
	var parents []string
	for p := filepath.Dir(path); ; p = filepath.Dir(p) {
		parents = append(parents, p)
		if filepath.Dir(p) == p {
			break
		}
	}
	var handles []syscall.Handle
	defer func() {
		for i := len(handles) - 1; i >= 0; i-- {
			syscall.CloseHandle(handles[i])
		}
	}()
	for i := len(parents) - 1; i >= 0; i-- {
		h, ok := openPlain(parents[i], true)
		if !ok {
			return false
		}
		handles = append(handles, h)
	}
	h, ok := openPlain(path, false)
	if !ok {
		return false
	}
	f := os.NewFile(uintptr(h), path)
	defer f.Close()
	stat, err := f.Stat()
	if err != nil || stat.Size() <= 0 || stat.Size() > 64<<20 {
		return false
	}
	raw, err := io.ReadAll(io.LimitReader(f, 64<<20+1))
	if err != nil || int64(len(raw)) != stat.Size() {
		return false
	}
	var info syscall.ByHandleFileInformation
	if syscall.GetFileInformationByHandle(h, &info) != nil {
		return false
	}
	id := softwareIdentity{fmt.Sprint(info.VolumeSerialNumber), fmt.Sprint(uint64(info.FileIndexHigh)<<32 | uint64(info.FileIndexLow)), fmt.Sprint(uint64(info.CreationTime.HighDateTime)<<32 | uint64(info.CreationTime.LowDateTime))}
	return consume(raw, id)
}

func observeInstalledGuard() softwareObservation {
	result := softwareObservation{}
	withPinnedSoftware(installedGuardPath, func(raw []byte, id softwareIdentity) bool {
		if !pinnedBuildMatches(raw, installedGuardSHA, func(b []byte) (*debug.BuildInfo, error) { return buildinfo.Read(bytes.NewReader(b)) }) {
			return false
		}
		result = softwareObservation{Verified: true, Domain: "owner-refresh-known-guard-software-v1", SHA256: installedGuardSHA, Bytes: len(raw), SourceRevision: installedGuardRevision, Identity: &id}
		return true
	})
	return result
}

func run(args []string, out io.Writer, observe func() softwareObservation, operations ...updateOperations) int {
	if len(args) == 1 && (args[0] == "--verify-refresh-guard-update" || args[0] == "--prepare-refresh-guard-update") {
		op := updateOperations{verify: observeFixedUpdate, prepare: prepareFixedUpdate}
		if len(operations) == 1 {
			op = operations[0]
		}
		return runUpdate(args, out, op)
	}
	if len(args) != 1 || args[0] != "--verify-installed-refresh-guard" {
		_ = json.NewEncoder(out).Encode(softwareObservation{})
		return 2
	}
	r := observe()
	if !r.Verified {
		r = softwareObservation{}
	}
	if json.NewEncoder(out).Encode(r) != nil || !r.Verified {
		return 1
	}
	return 0
}

func main() { os.Exit(run(os.Args[1:], os.Stdout, observeInstalledGuard)) }
