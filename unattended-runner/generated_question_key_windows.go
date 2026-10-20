//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

type generatedKeyBlob struct {
	Size uint32
	Data *byte
}

func generatedQuestionProtect(b []byte) ([]byte, error)   { return generatedQuestionDPAPI(b, true) }
func generatedQuestionUnprotect(b []byte) ([]byte, error) { return generatedQuestionDPAPI(b, false) }
func generatedQuestionDPAPI(b []byte, protect bool) ([]byte, error) {
	if len(b) == 0 || len(b) > 4096 {
		return nil, errConversationMedia
	}
	in := generatedKeyBlob{uint32(len(b)), &b[0]}
	out := generatedKeyBlob{}
	entropy := []byte(generatedEvidenceDomain)
	salt := generatedKeyBlob{uint32(len(entropy)), &entropy[0]}
	name := "CryptUnprotectData"
	if protect {
		name = "CryptProtectData"
	}
	ok, _, _ := syscall.NewLazyDLL("crypt32.dll").NewProc(name).Call(uintptr(unsafe.Pointer(&in)), 0, uintptr(unsafe.Pointer(&salt)), 0, 0, 1, uintptr(unsafe.Pointer(&out)))
	if out.Data != nil {
		defer syscall.NewLazyDLL("kernel32.dll").NewProc("LocalFree").Call(uintptr(unsafe.Pointer(out.Data)))
	}
	if ok == 0 || out.Data == nil || out.Size == 0 || out.Size > 4096 {
		return nil, errConversationMedia
	}
	view := unsafe.Slice(out.Data, int(out.Size))
	result := append([]byte(nil), view...)
	if !protect {
		eraseGeneratedKey(view)
	}
	return result, nil
}
