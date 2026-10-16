# Disposable Windows guest lifecycle fixture

This Windows amd64 Go helper provisions one new Hyper-V guest from the public
Windows Server 2025 Evaluation ISO. It uses only a safe-delete-owned work root
and CC-AdminBroker. It does not inspect existing VM names, disks, or guest
contents. The created VM has 2 vCPU, fixed 2 GiB RAM, Secure Boot, no virtual
NIC, and no GPU adapter. It receives only this helper executable and a public
certificate pin; no product source, owner profile, credentials, network, or
external runner registration is supplied.

The helper applies the Windows image to a new VHDX while it is mounted
offline, adds one LocalSystem automatic service to that image's own SYSTEM
hive, creates its boot files, then unmounts the new VHDX and ISO before making
the VM. It does not create an answer file, skip OOBE, create an account, or
change a password. The Windows service starts through SCM and speaks only on
the VM's COM1 named pipe. Ephemeral host and guest Ed25519/TLS 1.3 identities
are generated in memory. The guest receives only the host certificate SHA256;
the host pins the server certificate reported by its exact VM COM pipe.

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

The result demonstrates only this finite Windows guest and its live
normal/cancel cleanup. It does not execute the GoDD product workflow, the 13
original CI steps, Cargo, or an official runner. It does not guarantee
recovery after host shutdown, logout, or reboot.
