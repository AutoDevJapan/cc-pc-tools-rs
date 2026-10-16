# Disposable Windows guest lifecycle fixture

This Windows amd64 Go helper provisions one new Hyper-V guest from the public
Windows Server 2025 Evaluation ISO. It uses only a safe-delete-owned work root
and CC-AdminBroker. It does not inspect existing VM names, disks, or guest
contents. The created VM has 2 vCPU, fixed 2 GiB RAM, Secure Boot, no virtual
NIC, and no GPU adapter. It receives only this helper executable and a public
certificate pin; no product source, owner profile, credentials, network, or
external runner registration is supplied. Its second DVD contains only the
standard answer file, the staging script, the helper executable, and the
public seed JSON.

The helper verifies the Microsoft-signed setup image, applies the selected
evaluation WIM offline to a new GPT VHDX with DISM, and uses BCDBoot to create
the UEFI boot files. It places the standard `Autounattend.xml` in Windows
Panther and on a second, small seed ISO. At first boot from the VHDX, Windows
Setup runs the service staging command as LocalSystem during `specialize`.
The answer file has no `oobeSystem` settings, user account, password, or OOBE
skip. OOBE is not hidden or completed by this helper. The service speaks only
on the VM's COM pipe. Ephemeral host and guest Ed25519/TLS 1.3 identities are
generated in memory. The guest receives only the host certificate SHA256; the
host pins the server certificate reported by its exact VM COM pipe.

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
```

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
mount confirms it has zero partitions; the retry uses a fresh VM nonce. Any
identity mismatch or nonblank VHDX stops the retry.

The result demonstrates only this finite Windows guest and its live
normal/cancel cleanup. It does not execute the GoDD product workflow, the 13
original CI steps, Cargo, or an official runner. It does not guarantee
recovery after host shutdown, logout, or reboot.

Microsoft references: [Windows Server 2025 Evaluation](https://www.microsoft.com/en-us/evalcenter/download-windows-server-2025),
[DISM image apply](https://learn.microsoft.com/en-us/windows-hardware/manufacture/desktop/capture-and-apply-windows-system-and-recovery-partitions?view=windows-11),
[BCDBoot](https://learn.microsoft.com/en-us/windows-hardware/manufacture/desktop/bcdboot-command-line-options-techref-di?view=windows-11),
[Answer file discovery](https://learn.microsoft.com/en-us/windows-hardware/manufacture/desktop/windows-setup-automation-overview?view=windows-11),
[EULA acceptance](https://learn.microsoft.com/en-us/windows-hardware/customize/desktop/unattend/microsoft-windows-setup-userdata-accepteula),
and [specialize RunSynchronous](https://learn.microsoft.com/en-us/windows-hardware/customize/desktop/unattend/microsoft-windows-deployment-runsynchronous-runsynchronouscommand-path).
