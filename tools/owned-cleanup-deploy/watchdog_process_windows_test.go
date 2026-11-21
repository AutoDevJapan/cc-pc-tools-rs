//go:build windows

package main

import (
	"testing"
	"unsafe"
)

func TestWatchdogProcessEntryLayoutUsesWindowsX64Fields(t *testing.T){
	if unsafe.Sizeof(uintptr(0))!=8{t.Skip("Windows x64 structure")}
	var v watchdogProcessEntry
	if unsafe.Offsetof(v.PID)!=8||unsafe.Offsetof(v.Parent)!=32||
		unsafe.Offsetof(v.Exe)!=44||unsafe.Sizeof(v)!=568{
		t.Fatal("PROCESSENTRY32W layout differs from native ABI")
	}
}
