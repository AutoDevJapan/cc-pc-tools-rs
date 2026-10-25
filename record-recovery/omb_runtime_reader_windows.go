//go:build windows

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

type omb3255GuardVerdict struct {
	Access     bool
	Unverified bool
	Blocked    bool
	Status     string
	Categories int
}

func omb3255Guard(ctx context.Context, path string) (omb3255GuardVerdict, error) {
	cmd := exec.CommandContext(ctx, "D:/CC/Tools/client-records/scrub391.exe", "--guard-path", path)
	hideCommand(cmd)
	cmd.WaitDelay = time.Second
	data, err := cmd.Output()
	if err != nil {
		return omb3255GuardVerdict{}, omb3255RuntimeUnavailable
	}
	value, err := omb3255Decode(data)
	if err != nil {
		return omb3255GuardVerdict{}, err
	}
	return omb3255ParseGuard(value)
}

func omb3255ParseGuard(value obj) (omb3255GuardVerdict, error) {
	if value["ok"] != true {
		return omb3255GuardVerdict{}, omb3255RuntimeUnavailable
	}
	access, a := value["access_allowed"].(bool)
	unknown, b := value["classification_unverified"].(bool)
	blocked, c := value["client_resource"].(bool)
	status := omb3255Text(value["protection_status"])
	categories, ok := value["protection_categories"].([]any)
	if !a || !b || !c || !ok || (status != "clear" && status != "unverified" && status != "protected") {
		return omb3255GuardVerdict{}, omb3255RuntimeUnavailable
	}
	verdict := omb3255GuardVerdict{access, unknown, blocked, status, len(categories)}
	if !verdict.valid() {
		return omb3255GuardVerdict{}, omb3255RuntimeUnavailable
	}
	return verdict, nil
}

func (g omb3255GuardVerdict) valid() bool {
	// The existing guard wire field client_resource is !access_allowed,
	// including an unknown classification. It is not a confirmed-client bit.
	// Confirmed matches are protected/category-bearing and always denied.
	if g.Blocked == g.Access || g.Categories < 0 {
		return false
	}
	switch g.Status {
	case "clear":
		return g.Access && !g.Unverified && g.Categories == 0
	case "unverified":
		return !g.Access && g.Unverified && g.Categories == 0
	case "protected":
		return !g.Access && !g.Unverified && g.Categories > 0
	}
	return false
}

func (g omb3255GuardVerdict) codeClear() bool {
	return g.valid() && g.Access && g.Status == "clear"
}

// This is admission to a fixed, independently bound metadata projection only.
// It does not change allowed(path) or reinterpret unknown files as client-free.
// A clear source anchor also requires the canonical legacy check to be readable
// (or recorded absent); a known client hit can never be overridden here.
func omb3255MetadataPurpose(source, binary, native omb3255GuardVerdict, normalBinding bool) bool {
	return normalBinding && source.codeClear() && binary.codeClear() && native.valid() && native.Categories == 0 && native.Status != "protected" &&
		(native.codeClear() || (native.Status == "unverified" && native.Unverified))
}

type omb3255PinnedRuntime struct {
	file *os.File
	path string
	info syscall.ByHandleFileInformation
}

func omb3255PlainRuntimePath(path string) bool {
	for p := filepath.Clean(path); ; p = filepath.Dir(p) {
		info, err := os.Lstat(p)
		if err != nil {
			return false
		}
		data, ok := info.Sys().(*syscall.Win32FileAttributeData)
		if !ok || data.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
			return false
		}
		if filepath.Dir(p) == p {
			return true
		}
	}
}

func omb3255HandlePath(handle syscall.Handle) (string, bool) {
	buffer := make([]uint16, 32768)
	n, _, _ := syscall.NewLazyDLL("kernel32.dll").NewProc("GetFinalPathNameByHandleW").Call(uintptr(handle), uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)), 0)
	if n == 0 || n >= uintptr(len(buffer)) {
		return "", false
	}
	path := syscall.UTF16ToString(buffer[:n])
	if strings.HasPrefix(path, `\\?\`) {
		path = strings.TrimPrefix(path, `\\?\`)
	}
	return path, true
}

func omb3255OwnedHandle(handle syscall.Handle) bool {
	identity, err := user.Current()
	if err != nil {
		return false
	}
	adv := syscall.NewLazyDLL("advapi32.dll")
	kernel := syscall.NewLazyDLL("kernel32.dll")
	var owner, descriptor unsafe.Pointer
	code, _, _ := adv.NewProc("GetSecurityInfo").Call(uintptr(handle), 1, 1, uintptr(unsafe.Pointer(&owner)), 0, 0, 0, uintptr(unsafe.Pointer(&descriptor)))
	if code != 0 || owner == nil || descriptor == nil {
		return false
	}
	defer kernel.NewProc("LocalFree").Call(uintptr(descriptor))
	var text *uint16
	ok, _, _ := adv.NewProc("ConvertSidToStringSidW").Call(uintptr(owner), uintptr(unsafe.Pointer(&text)))
	if ok == 0 || text == nil {
		return false
	}
	defer kernel.NewProc("LocalFree").Call(uintptr(unsafe.Pointer(text)))
	chars := unsafe.Slice(text, 256)
	end := 0
	for end < len(chars) && chars[end] != 0 {
		end++
	}
	sid := syscall.UTF16ToString(chars[:end])
	return sid == identity.Uid || sid == "S-1-5-18" || sid == "S-1-5-32-544"
}

func omb3255OpenPinnedRuntime(path string) (*omb3255PinnedRuntime, error) {
	if !filepath.IsAbs(path) || !omb3255PlainRuntimePath(path) {
		return nil, omb3255RuntimeUnavailable
	}
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, omb3255RuntimeUnavailable
	}
	// READ_CONTROL (0x00020000) permits the same handle's owner check.
	handle, err := syscall.CreateFile(name, syscall.GENERIC_READ|0x00020000, syscall.FILE_SHARE_READ, nil, syscall.OPEN_EXISTING, 0x00200000|0x08000000, 0)
	if err != nil {
		return nil, omb3255RuntimeUnavailable
	}
	file := os.NewFile(uintptr(handle), path)
	kept := false
	defer func() {
		if !kept {
			file.Close()
		}
	}()
	var info syscall.ByHandleFileInformation
	actual, ok := omb3255HandlePath(handle)
	if syscall.GetFileInformationByHandle(handle, &info) != nil || info.NumberOfLinks != 1 || info.FileAttributes&(syscall.FILE_ATTRIBUTE_REPARSE_POINT|syscall.FILE_ATTRIBUTE_DIRECTORY) != 0 || !ok || !omb3255SamePath(actual, path) || !omb3255OwnedHandle(handle) {
		return nil, omb3255RuntimeUnavailable
	}
	result := &omb3255PinnedRuntime{file, path, info}
	if !result.Unchanged() {
		return nil, omb3255RuntimeUnavailable
	}
	kept = true
	return result, nil
}

func (source *omb3255PinnedRuntime) Read(b []byte) (int, error) { return source.file.Read(b) }
func (source *omb3255PinnedRuntime) Close() error               { return source.file.Close() }
func (source *omb3255PinnedRuntime) Unchanged() bool {
	var after syscall.ByHandleFileInformation
	handle := syscall.Handle(source.file.Fd())
	actual, ok := omb3255HandlePath(handle)
	if syscall.GetFileInformationByHandle(handle, &after) != nil {
		return false
	}
	before := source.info
	before.LastAccessTime = after.LastAccessTime
	return before == after && after.NumberOfLinks == 1 && after.FileAttributes&(syscall.FILE_ATTRIBUTE_REPARSE_POINT|syscall.FILE_ATTRIBUTE_DIRECTORY) == 0 && ok && omb3255SamePath(actual, source.path) && omb3255PlainRuntimePath(source.path) && omb3255OwnedHandle(handle)
}

func omb3255AcquireRuntime() (result omb3255RuntimeAcquisition) {
	return omb3255AcquireRuntimeFor(omb3255ExpectedRoots())
}

func omb3255AcquireAdmissionRuntime() omb3255RuntimeAcquisition {
	return omb3255AcquireRuntimeFor(reviewAdmissionExpectedRoots())
}
func acquireUnstartedReviewRuntime() omb3255RuntimeAcquisition {
	return omb3255AcquireRuntimeFor(unstartedReviewRuntimeRoots())
}

func omb3255AcquireRuntimeFor(expected []omb3255ExpectedRoot) (result omb3255RuntimeAcquisition) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	revision, commitMS := omb3255BuildIdentity()
	location, err := os.Getwd()
	if err != nil || revision == "" || !omb3255SamePath(location, omb3255SourceRoot) {
		return omb3255RuntimeAcquisition{state: "source_identity_unavailable"}
	}
	git := exec.CommandContext(ctx, "git", "-C", omb3255SourceRoot, "rev-parse", "HEAD")
	hideCommand(git)
	git.WaitDelay = time.Second
	head, err := git.Output()
	if err != nil || strings.TrimSpace(string(head)) != revision {
		return omb3255RuntimeAcquisition{state: "source_identity_unavailable"}
	}
	anchor := filepath.Join(omb3255SourceRoot, "record-recovery", "main.go")
	sourceGuard, err := omb3255Guard(ctx, anchor)
	if err != nil || !sourceGuard.codeClear() {
		return omb3255RuntimeAcquisition{state: "source_guard_unavailable"}
	}
	binaryGuard, err := omb3255Guard(ctx, omb3255Codex)
	if err != nil || !binaryGuard.codeClear() {
		return omb3255RuntimeAcquisition{state: "binary_guard_unavailable"}
	}
	binary, err := omb3255OpenPinnedRuntime(omb3255Codex)
	if err != nil {
		return omb3255RuntimeAcquisition{state: "binary_open_unavailable"}
	}
	defer func() {
		stable := binary.Unchanged()
		closeErr := binary.Close()
		if !stable || closeErr != nil {
			result = omb3255RuntimeAcquisition{state: "binary_identity_unavailable"}
		}
	}()
	hash := sha256.New()
	size, err := io.Copy(hash, io.LimitReader(binary, 512<<20+1))
	if err != nil || size <= 0 || size > 512<<20 || hex.EncodeToString(hash.Sum(nil)) != omb3255CodexSHA || !binary.Unchanged() {
		return omb3255RuntimeAcquisition{state: "binary_identity_unavailable"}
	}
	rpc, err := omb3255StartMetadataRPC(ctx)
	if err != nil {
		return omb3255RuntimeAcquisition{state: "metadata_reader_unavailable"}
	}
	defer func() {
		if rpc.Close() != nil {
			result = omb3255RuntimeAcquisition{state: "metadata_reader_close_unavailable"}
		}
	}()
	var admitted []omb3255RuntimeRef
	var nativePath string
	collect := func() ([]omb3255RuntimeRef, string, error) {
		return omb3255CollectRefsFrom(func(endpoint string) (obj, error) {
			return omb3255NormalGET(ctx, endpoint)
		}, rpc.Call, revision, commitMS, expected)
	}
	recheckProtection := func() bool {
		source, err := omb3255Guard(ctx, anchor)
		if err != nil {
			return false
		}
		tool, err := omb3255Guard(ctx, omb3255Codex)
		if err != nil {
			return false
		}
		native, err := omb3255Guard(ctx, nativePath)
		if err != nil {
			return false
		}
		return omb3255MetadataPurpose(source, tool, native, omb3255RuntimeRefsValid(admitted)) && binary.Unchanged()
	}
	return omb3255InspectRuntimeState(omb3255RuntimeDeps{
		Prepare: func() ([]omb3255RuntimeRef, error) {
			refs, path, err := collect()
			if err != nil {
				return nil, err
			}
			admitted, nativePath = refs, path
			if !recheckProtection() {
				return nil, omb3255RuntimeUnavailable
			}
			return refs, nil
		},
		Open: func() (omb3255RuntimeStream, error) {
			if !recheckProtection() || !omb3255SamePath(nativePath, omb3255ExpectedNativePath()) {
				return nil, omb3255RuntimeUnavailable
			}
			stream, err := omb3255OpenPinnedRuntime(nativePath)
			if err != nil {
				return nil, err
			}
			return stream, nil
		},
		Recheck: func(before []omb3255RuntimeRef) bool {
			after, path, err := collect()
			return err == nil && reflect.DeepEqual(before, after) && omb3255SamePath(path, nativePath) && recheckProtection()
		},
	})
}
