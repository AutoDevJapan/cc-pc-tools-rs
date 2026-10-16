# ToDo2746 Windows guest handoff

Last saved: 2026-10-17. Branch `scheduler/todo-2746-godd-mcp-windows-guest`,
last pushed commit `f0dbdb5` (current recovery changes still need commit/push).

## Completed

- The earlier credential-free host Job Object fixture is committed as
  `a218d5e`; its actual normal/cancel results are in
  `tools/windows-guest-fixture/evidence/todo-2746-20261017.json`.
- The standard Windows Server 2025 unattended installer and LocalSystem
  COM/mTLS fixture are committed through `e86c004`; subsequent recovery and
  handoff updates are pushed through `f0dbdb5`. `go test ./...`, `go vet
  ./...`, and `git diff --check` passed on the current working changes. They
  only compile/check the module; the Hyper-V guest path has not yet run.
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
  vet, and diff checks passed; this recovery change and the live resume path
  are not yet committed or run. Inspection also confirmed the Windows
  PowerShell receipt has a UTF-8 BOM; Go receipt readers now strip that marker
  before JSON decoding.

## Remaining

1. Commit/push the current recovery changes. Build into the owned root and run
   `vm-smoke` with the already-downloaded ISO. Since both VHDX and receipt exist,
   it must resume the single receipt-identified VM and reject any identity or
   configuration mismatch. Do not rerun the fresh-provision path or create a
   second VM.
2. Confirm the answer file is discovered from the separate seed DVD, the
   LocalSystem service starts through normal Setup, and both real fixtures
   report process and namespace cleanup.
3. Confirm the normal and cancellation fixture receipts, then separately
   confirm VM Off, assigned memory zero, and removal.
4. Copy the sanitized evidence JSON into the worktree, update ToDo2737's note
   while preserving its existing content, and commit/push the result.
5. Run safe-delete `plan`, issue its normal deletion in a separate call, then
   `finish`. Do not delete the root if VM ownership or removal is uncertain.
6. Save the final scheduler checkpoint using the current job, branch and HEAD.

No official runner or product source was used. The original CI has not run.
