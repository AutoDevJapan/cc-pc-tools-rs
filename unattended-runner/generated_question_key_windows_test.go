//go:build windows

package main

import (
	"bytes"
	"testing"
)

func TestGeneratedQuestionWindowsPurposeKeyRequiresValidUserProtection(t *testing.T) {
	plain := bytes.Repeat([]byte{0x45}, 32)
	sealed, err := generatedQuestionProtect(plain)
	if err != nil || bytes.Equal(sealed, plain) {
		t.Fatal("purpose key was not encrypted", err)
	}
	opened, err := generatedQuestionUnprotect(sealed)
	if err != nil || !bytes.Equal(opened, plain) {
		t.Fatal("purpose key cannot round trip under current user", err)
	}
	eraseGeneratedKey(opened)
	sealed[len(sealed)/2] ^= 0x80
	if _, err = generatedQuestionUnprotect(sealed); err == nil {
		t.Fatal("tampered user-bound key accepted")
	}
}
