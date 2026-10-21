package main

import (
	"strings"
	"testing"
)

func testVMState() vmState {
	instance := strings.Repeat("a", 32)
	return vmState{Instance: instance, Name: "cc-ci-2737-" + instance, Root: `C:\owned\job`, Pipe: `\\.\pipe\windows-ci-sdk-2737-` + instance}
}

func TestWindowsPreparedVMBodyUsesRetainedCloneOnly(t *testing.T) {
	body, e := vmCreateBody(testVMState(), `C:\owned\job\guest.vhdx`, "", 2, 2, true)
	if e != nil {
		t.Fatalf("prepared Windows constructor rejected: %v", e)
	}
	for _, want := range []string{"PREPARED_CLONE_ABSENT", "PREPARED_CLONE_ATTACHED", "New-VM", "Set-VMComPort", "Start-VM", "Remove-Owned $vm $true", "Journal-VM $vm.Id.ToString() 'created'"} {
		if !strings.Contains(body, want) {
			t.Fatalf("prepared constructor lost %q", want)
		}
	}
	for _, forbidden := range []string{"Convert-VHD", "Resize-VHD", "OWNED_DISK_ALREADY_EXISTS", "Add-VMDvdDrive", "Set-VMDvdDrive"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("prepared constructor still contains %q", forbidden)
		}
	}
}

func TestLinuxVMBodyKeepsDiskConversionAndSeed(t *testing.T) {
	body, e := vmCreateBody(testVMState(), `C:\public\linux.vhdx`, "", 3, 4, false)
	if e != nil {
		t.Fatal(e)
	}
	for _, want := range []string{"OWNED_DISK_ALREADY_EXISTS", "Convert-VHD -Path " + psQuote(`C:\public\linux.vhdx`) + " -DestinationPath $disk -VHDType Dynamic", "Resize-VHD -Path $disk -SizeBytes 64GB", "Add-VMDvdDrive", "Set-VMProcessor -VM $vm -Count 3", "-StartupBytes 4GB"} {
		if !strings.Contains(body, want) {
			t.Fatalf("Linux constructor lost %q", want)
		}
	}
	if strings.Contains(body, "PREPARED_CLONE_ABSENT") {
		t.Fatal("Linux constructor used the prepared-clone path")
	}
}
