//go:build windows && amd64

package sdkprep

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRecoveryAcceptsPowerShellUppercaseHashAndRejectsOtherVM(t *testing.T) {
	vm := VMReceipt{VMID: "f0561845-8dfe-4fcd-bd9d-2d909549a670", VHD: `D:\owned\guest.vhdx`}
	r := RecoveryReceipt{Instance: "test", VMID: vm.VMID, VMState: "Off", MemoryAssigned: 0, VMRemoved: true, VHD: vm.VHD, VHDDetached: true, VHDSize: 123, SDKVerified: true, ShutdownMode: "guest-integration-shutdown", VHDSHA256: strings.Repeat("A", 64)}
	data, _ := json.Marshal(r)
	got, err := decodeRecoveryReceipt(data, vm, "test")
	if err != nil || got.VHDSHA256 != strings.Repeat("a", 64) {
		t.Fatalf("uppercase provider hash: %v", err)
	}
	r.VMID = "other-vm"
	data, _ = json.Marshal(r)
	if _, err := decodeRecoveryReceipt(data, vm, "test"); err == nil {
		t.Fatal("different recovered VM accepted")
	}
}
