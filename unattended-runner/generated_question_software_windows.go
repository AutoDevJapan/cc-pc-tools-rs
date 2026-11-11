//go:build windows

package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"unsafe"
)

// Software identity is distinct from data classification. Pin the installed
// object without write/delete sharing while checking its signer and owner;
// the caller keeps this lease until initialization has consumed that object.
func generatedQuestionOpenSoftware(ctx context.Context, path string) (io.Closer, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return nil, errConversationMedia
	}
	if !generatedQuestionInstalledPath(cache, path) {
		return nil, errConversationMedia
	}
	for current := filepath.Dir(path); ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, errConversationMedia
		}
		if conversationMediaSamePath(current, cache) {
			break
		}
		if filepath.Dir(current) == current {
			return nil, errConversationMedia
		}
	}
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, errConversationMedia
	}
	h, err := syscall.CreateFile(p, syscall.GENERIC_READ|0x20000, syscall.FILE_SHARE_READ, nil, syscall.OPEN_EXISTING, syscall.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, errConversationMedia
	}
	f := os.NewFile(uintptr(h), path)
	failed := true
	defer func() {
		if failed {
			f.Close()
		}
	}()
	var info syscall.ByHandleFileInformation
	if syscall.GetFileInformationByHandle(h, &info) != nil || info.FileAttributes&(syscall.FILE_ATTRIBUTE_DIRECTORY|syscall.FILE_ATTRIBUTE_REPARSE_POINT) != 0 || info.NumberOfLinks != 1 {
		return nil, errConversationMedia
	}
	var systemDir [32768]uint16
	n, _, _ := syscall.NewLazyDLL("kernel32.dll").NewProc("GetSystemDirectoryW").Call(uintptr(unsafe.Pointer(&systemDir[0])), uintptr(len(systemDir)))
	if n == 0 || n >= uintptr(len(systemDir)) {
		return nil, errConversationMedia
	}
	ps := filepath.Join(syscall.UTF16ToString(systemDir[:n]), "WindowsPowerShell", "v1.0", "powershell.exe")
	const script = `$ErrorActionPreference='Stop';$env:PSModulePath=Join-Path $PSHOME 'Modules';$p=([Console]::In.ReadToEnd()|ConvertFrom-Json).path;$s=Get-AuthenticodeSignature -LiteralPath $p;$o=(Get-Acl -LiteralPath $p).GetOwner([Security.Principal.SecurityIdentifier]);$u=[Security.Principal.WindowsIdentity]::GetCurrent().User;[ordered]@{valid=($s.Status -eq 'Valid');publisher=$(if($s.SignerCertificate){$s.SignerCertificate.GetNameInfo([Security.Cryptography.X509Certificates.X509NameType]::SimpleName,$false)}else{''});ownerMatch=($o -eq $u)}|ConvertTo-Json -Compress`
	cmd := exec.CommandContext(ctx, ps, "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", script)
	hide(cmd)
	input, _ := json.Marshal(map[string]string{"path": path})
	cmd.Stdin = strings.NewReader(string(input))
	cmd.Stderr = io.Discard
	raw, err := cmd.Output()
	var result struct {
		Valid      bool
		Publisher  string
		OwnerMatch bool
	}
	if err != nil || len(raw) > 4096 || historyStrictJSON(raw, &result) != nil || !generatedQuestionSoftwareFacts(result.Valid, result.Publisher, result.OwnerMatch) {
		return nil, errConversationMedia
	}
	var after syscall.ByHandleFileInformation
	if syscall.GetFileInformationByHandle(h, &after) != nil || info.VolumeSerialNumber != after.VolumeSerialNumber || info.FileIndexHigh != after.FileIndexHigh || info.FileIndexLow != after.FileIndexLow || info.FileSizeHigh != after.FileSizeHigh || info.FileSizeLow != after.FileSizeLow || info.LastWriteTime != after.LastWriteTime || info.CreationTime != after.CreationTime {
		return nil, errConversationMedia
	}
	failed = false
	return f, nil
}

func generatedQuestionInstalledPath(cache, path string) bool {
	root := filepath.Join(cache, "OpenAI", "Codex", "bin")
	parent := filepath.Dir(path)
	return filepath.IsAbs(path) && strings.EqualFold(filepath.Base(path), "codex.exe") && regexp.MustCompile(`^[a-f0-9]{16,64}$`).MatchString(filepath.Base(parent)) && conversationMediaSamePath(filepath.Dir(parent), root)
}

func generatedQuestionSoftwareFacts(valid bool, publisher string, sameOwner bool) bool {
	return valid && publisher == "OpenAI OpCo, LLC" && sameOwner
}
