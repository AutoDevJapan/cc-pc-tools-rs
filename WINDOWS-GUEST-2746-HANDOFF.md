# ToDo2746 Windows guest handoff

Last saved: 2026-10-17. Branch `scheduler/todo-2746-godd-mcp-windows-guest`,
last pushed commit `68a3668` (current offline-deployment changes still need commit/push).

## Completed

- The earlier credential-free host Job Object fixture is committed as
  `a218d5e`; its actual normal/cancel results are in
  `tools/windows-guest-fixture/evidence/todo-2746-20261017.json`.
- The standard Windows Server 2025 unattended installer and LocalSystem
  COM/mTLS fixture are committed through `e86c004`; subsequent recovery and
  handoff updates are pushed through `68a3668`. `go test ./...`, `go vet
  ./...`, and `git diff --check` passed on the current working changes. They
  only compile/check the module. The first real guest boot did not reach its
  COM readiness service; details and the revised image path are below.
- The answer file accepts EULA, selects the Standard Evaluation image, and
  runs the service installer during `specialize`. It contains no account or
  password settings and no `oobeSystem` pass. The guest keeps normal OOBE.
- `vmms` is Running. The helper specifies a new Gen 2 VM with 2 vCPU, fixed
  2 GiB RAM, Secure Boot, no NIC, and no GPU adapter. It creates its VHDX and
  VM under the current safe-delete-owned root.

## In progress

- `safe-delete` owner ID: `bbcdd7148e9c73866e40b02e1ac45e25`.
- Root: `%LOCALAPPDATA%\Temp\codex-owned-bbcdd7148e9c73866e40b02e1ac45e25`.
- The official Microsoft direct Windows Server 2025 Evaluation ISO is fully
  downloaded from `https://aka.ms/WinServ2025iso-enus`. Its measured length is
  8,152,356,864 bytes, matching the CDN response. A SHA-256 read completed but
  its output was truncated; the helper calculates and stores the complete hash
  in the final evidence after the run.
- The first seed ISO attempt failed before any VM/VHDX creation because IMAPI
  rejected media type `4`; its bounded diagnostic is retained as
  `seed-iso-failure.private.json`. The helper now uses the documented DVD-R
  media enum `9`, gives each seed ISO its own instance-scoped name, and refuses
  to reprovision if a VHDX or VM receipt exists. The VM receipt validator was
  aligned with that instance-scoped ISO name.
- The next run again stopped before VM creation: PowerShell's late-bound IMAPI
  COM wrapper does not expose the returned `IStream.Read` method. The bounded
  diagnostic replaced the earlier one at the same owned path; the latest
  helper now queries the documented `IStream` interface and copies its stream
  via the COM vtable. A read-only in-memory probe confirmed that interface
  returns bytes; a later full run created an 8.3 MiB seed ISO successfully.
- With seed ISO creation fixed, the next run created an 8.3 MiB seed image but
  CC-AdminBroker's PowerShell 7 invocation failed before any VM/VHDX receipt:
  `run-as-admin.ps1` reported that `admin_broker.exe` could not run in a
  pipeline. Invoking the same script with a harmless, owned no-op probe failed
  in the helper's minimized environment but succeeded with the inherited
  environment. Adding only `PATHEXT` to the minimized child environment fixed
  the harmless probe. The helper now uses Windows PowerShell 5.1 and an
  explicit standard `PATHEXT`; the VM provisioning attempt with that change is
  recorded below.

- With the minimized-environment broker probe fixed, the latest provisioning
  attempt reached the official ISO mount but incorrectly looked for
  `C:\setup.exe`; no VM was created in that attempt. A targeted, read-only
  broker probe confirmed the ISO mount/unmount path. The helper now re-queries
  the exact ISO with `Get-DiskImage -ImagePath`, gets its volume with the
  documented `Get-DiskImage | Get-Volume` route, and checks the expected setup
  files. The next attempt passed ISO validation and created the VM/VHDX, then
  stopped at a false-negative Secure Boot check. The VM is Off, 2 vCPU, fixed
  2 GiB, Secure Boot On/MicrosoftWindows, 0 NICs, 0 GPU adapters, 2 DVDs, and
  1 VHD. Its receipt and VHDX remain in the safe-delete-owned root. A targeted
  read-only broker query verified those settings by the receipt's exact VM
  GUID. The cause was treating PowerShell's enum-like `SecureBoot` value as a
  boolean; the helper now compares the string value explicitly and has a
  receipt-bound resume path that replaces the unconsumed seed DVD with a fresh
  in-memory host identity before starting this same Off VM. Go compilation,
  vet, and diff checks passed. Inspection also confirmed the Windows
  PowerShell receipt has a UTF-8 BOM; Go receipt readers now strip that marker
  before JSON decoding.
- The first resume attempt stopped at its identity check before changing VM
  state. A read-only query by the same receipt GUID confirmed name and notes
  match, the VM is Off, and its actual ConfigurationLocation is a child folder
  under the owned `vm` directory rather than exactly that parent. The helper
  now requires the configuration path to remain inside that owned directory.
  A second resume attempt stopped before changing VM state because
  `Get-VMComPort` does not expose the `Number` property that the helper used;
  a read-only query confirmed the receipt's exact unique pipe is configured.
  Recovery and cleanup now match the COM port by that exact pipe path.
- The corrected same-VM resume ran for the 30-minute readiness deadline but
  received no guest service readiness. It then used its exact receipt to stop
  the VM, verify assigned memory 0, and remove it. A read-only Get-VM by that
  GUID confirmed it is absent. The VHDX is detached; a read-only mount found no
  partitions and it remains 4 MiB, so Windows Setup never began applying the
  image. The original failure receipt did not preserve which serial substage
  timed out, so only “no readiness within 30 minutes” is confirmed.
- The offline image attempt failed at DISM `/Apply-Image` with HRESULT
  `0x80070003`; the DISM log records the official source WIM and ApplyDir
  ending in a quoted trailing backslash. No new VM was created. Its VHDX is detached and
  about 196 MiB. A read-only probe found four partitions (EFI, OS, and two
  MSRs) with task-specific filesystem labels. Whether the OS partition has a
  Windows directory is not yet confirmed.
- Offline provisioning via native DISM, the PowerShell Dism cmdlet, and a
  guest-volume stage directory all returned `0x80070003`. The AdminBroker
  read-only probe confirmed the WIM can be queried, the target volume accepts
  a small write/remove, and source/target checks pass. The failed WIM applies
  left the OS partition without a Windows directory, and VHD/ISO mounts were
  detached afterward. The next approach applies to a per-instance staging
  directory on the same guest OS partition, then moves the applied image into
  the OS root. The Dism cmdlet takes the target by Volume GUID path; PowerShell
  creates and moves files using the same volume's temporary drive letter.
- The most recent retry passed old-receipt and partition checks, reset/formatted
  only the task-owned VHD, then stopped because PowerShell `New-Item` rejected
  the Volume GUID path for the staging directory. No WIM payload or new VM was
  created; the VHD is detached and has the expected EFI/MSR/OS layout. The
  helper now creates the stage directory through the exact OS drive letter,
  then passes its equivalent Volume GUID path to Dism. The helper accepts at
  most one regular, empty stage
  folder whose name matches its fixed prefix and 32-hex nonce; any contents,
  reparse point, Windows tree, or other OS-root entry still blocks a reset.
  The previous VM/cleanup/failure attempt records are preserved idempotently
  only when the existing bytes match. These recovery changes are uncommitted.

## Remaining

1. Run `gofmt`, `go test ./...`, `go vet ./...`, and `git diff --check`; commit
   and push the volume-GUID stage path and empty-stage recovery changes.
2. Build the helper into the owned root and run `vm-smoke` against the already
   downloaded ISO. Receipt-bound recovery must confirm the previous VM is
   absent and the owned partial OS partition is empty except for allowed
   system metadata and the one verified empty stage directory.
3. Confirm guest readiness, the normal and cancellation child-process
   receipts, then separately verify VM Off, assigned memory zero, and removal.
4. Copy sanitized evidence into the worktree, update ToDo2737's note
   while preserving its existing content, and commit/push the result.
5. Run safe-delete `plan`, issue its normal deletion in a separate call, then
   `finish`. Do not delete the root if VM ownership or removal is uncertain.
6. Save the final scheduler checkpoint using the current job, branch and HEAD.

No official runner or product source was used. The original CI has not run.
