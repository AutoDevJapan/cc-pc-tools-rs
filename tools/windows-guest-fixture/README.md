# Disposable Windows guest lifecycle fixture

This Windows amd64 Go helper provisions one new Hyper-V guest from official
Windows Server 2025 Evaluation media. It uses only a safe-delete-owned work
root and CC-AdminBroker. It does not inspect existing VM names, disks, or guest
contents. Each created VM has 2 vCPU, fixed 2 GiB RAM, no virtual NIC, and no
GPU adapter. The ISO route is Generation 1 BIOS with Secure Boot unavailable;
the prebuilt evaluation VHD route remains Generation 2 with Secure Boot. No
product source, owner profile, credentials, network, or external runner
registration is supplied.

The helper verifies the Microsoft-signed setup image and the selected
evaluation edition, then boots the guest from the official ISO with a second,
small seed DVD containing the standard `Autounattend.xml`. The ISO route uses
a Generation 1 VM because Hyper-V exposes the application COM-port device to
the guest there. Windows Setup creates an active MBR system partition and a
Windows partition, installs the evaluation image, and runs the service
staging command as LocalSystem during `specialize`. The helper checks that
the blank IDE disk is tried first, so setup falls through to the ISO initially
and the installed system boots from disk after its first restart. The host
reopens the exact owned COM pipe if setup restarts the guest before service
readiness or interrupts the TLS handshake. Before mTLS, the host and guest
exchange a nonce-bound COM probe so the bidirectional serial channel is
verified independently. The answer
file has no `oobeSystem` settings, user account, password, or OOBE skip. OOBE
is not hidden or completed by this helper. The service speaks only on the VM's
COM pipe. Ephemeral host and guest Ed25519/TLS 1.3 identities are generated in
memory. The guest receives only the host certificate SHA256; the host pins the
server certificate reported by its exact VM COM pipe.

`vm-smoke-vhd <owned-evaluation-vhd-root>` is an alternate path using the
Evaluation Center's preinstalled VHD. The helper copies the official VHD into
a nonce-specific task VHDX, verifies its `IMAGE_STATE_GENERALIZE_RESEAL_TO_OOBE`
state, and places only its own helper binary plus the public seed JSON in that
copy. It registers `CCGuest2746` as an automatic LocalSystem service in the
copy's offline SYSTEM hive, unloads the temporary hive, and boots the copy.
The original Microsoft VHD remains unchanged. The helper does not add an
OOBE answer file or create a user; normal OOBE remains in place. The service
starts before the first interactive logon and communicates only over the
VM's COM pipe.

The `vm-smoke <owned-root> <official-evaluation-iso>` command runs two
credential-free fixtures inside the guest. The normal case observes two live
child processes, waits for one to return normally, closes the owned Job
Object, and verifies both children exit and the object disappears. The cancel
case starts two live children, reports their PIDs and live observation, then
accepts an explicit cancellation message, closes the same Job Object, and
verifies process and namespace cleanup. A separate cleanup phase turns off
and removes only the VM identified by its saved GUID, notes, configuration
path, and COM pipe, then verifies state Off, assigned memory zero, and removal.

Run this helper from the safe-delete-owned root after downloading the direct
public image URL:

```powershell
go build -o <owned-root>\windows-guest-fixture.exe .
<owned-root>\windows-guest-fixture.exe vm-smoke <owned-root> <owned-root>\windows-server-2025-eval.iso
<owned-root>\windows-guest-fixture.exe vm-smoke-vhd <owned-root>\windows-eval-vhd
```

For the VHD command, place the official, unmodified evaluation file at
`<owned-root>\windows-eval-vhd\windows-server-2025-eval.vhdx` and pass that
`windows-eval-vhd` directory as the command's root. Microsoft's Evaluation
Center links to the public direct download `https://aka.ms/WinServ2025vhd-enus`.

If provisioning or a fixture is uncertain, keep the owned root and its VM
receipt for recovery. After a verified VM removal, preserve the private
evidence JSON outside the owned root before using `safe-delete plan`, a
separate normal deletion call, and `safe-delete finish`.

If the owned root contains both `guest.vhdx` and `vm-created.private.json`, a
subsequent `vm-smoke` invocation follows the receipt's exact VM GUID. It may
resume that same VM only while it is Off and its notes, configuration path,
VHD, DVDs, COM pipe, CPU, memory, NIC, GPU, and Secure Boot settings match. If
the receipt's VM was already verified removed, the helper preserves that
attempt's receipts and may reuse the VHDX only after an elevated read-only
check matches the expected guest partition layout and confirms that the
Windows partition contains no installed image. It resets only that exact
receipt-owned VHDX and uses a fresh VM nonce. Any identity mismatch,
unexpected partition, filesystem label, or guest file stops the retry.

The result demonstrates only this finite Windows guest and its live
normal/cancel cleanup. It does not execute the GoDD product workflow, the 13
original CI steps, Cargo, or an official runner. It does not guarantee
recovery after host shutdown, logout, or reboot.

Microsoft references: [Windows Server 2025 Evaluation](https://www.microsoft.com/en-us/evalcenter/download-windows-server-2025),
[Windows Setup answer file discovery](https://learn.microsoft.com/en-us/windows-hardware/manufacture/desktop/windows-setup-automation-overview?view=windows-11),
[EULA acceptance](https://learn.microsoft.com/en-us/windows-hardware/customize/desktop/unattend/microsoft-windows-setup-userdata-accepteula),
and [specialize RunSynchronous](https://learn.microsoft.com/en-us/windows-hardware/customize/desktop/unattend/microsoft-windows-deployment-runsynchronous-runsynchronouscommand-path).
