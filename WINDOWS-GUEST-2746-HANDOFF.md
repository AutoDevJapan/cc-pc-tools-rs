# ToDo2746 Windows guest handoff

Last saved: 2026-10-17. Branch `scheduler/todo-2746-godd-mcp-windows-guest`,
last pushed commit `ba49db8` (the new preinstalled-evaluation-VHD path is uncommitted).

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
- Offline WIM deployment was stopped after native DISM and the PowerShell Dism
  cmdlet returned `0x80070003`, including after confirming the WIM is readable,
  the target volume accepts a small write, and the target is addressable by
  both a drive letter and Volume GUID. No WIM payload was applied. All VHD and
  ISO mounts were detached after each attempt. The VHD now holds only the
  task-created EFI/MSR/OS layout and empty image-stage folder; no Windows hive
  or new VM exists.
- The helper is switching back to standard Windows Setup from the official
  evaluation DVD. The first live guest attempt used `FirstBootDevice` and ran
  30 minutes without a COM readiness line or disk partitions; its exact VM was
  stopped, assigned memory 0, removed, and verified absent. The new path will
  set an explicit DVD-first UEFI boot order and pause the VM after boot failure,
  then let the answer file partition/install the VHD and stage the LocalSystem
  service during `specialize`. It preserves normal OOBE and creates no account.
  On retry the helper checks the prior cleanup receipt, GUID absence, VHD
  attachment, partition labels/types, and empty OS contents before clearing
  only its own partial VHD. It allows one empty nonce-named image-stage folder
  and preserves prior receipts byte-for-byte. The change was committed and
  pushed as `7aac50c`.
- The `7aac50c` attempt created its own Off VM and recorded the exact UEFI
  order, but stopped before `Start-VM`: the verification read `BootSource.Path`
  even though Hyper-V returns the selected component under `BootSource.Device`
  (an actual `DvdDrive`). A read-only query confirmed the VM is Off with 2 vCPU,
  fixed 2 GiB, Secure Boot On/MicrosoftWindows, no NIC/GPU, two DVD drives
  including the official ISO, and a detached task-owned VHDX. The failure is
  in our verification expression, not a demonstrated Windows Setup boot
  failure. Both initial-create and recovery checks now compare
  `BootOrder[0].Device.Path` to the exact official ISO. This correction is
  committed as `ba49db8`.
- The `ba49db8` ISO-first retry resumed that exact Off VM and did start it. At
  the 30-minute readiness deadline, the guest service was still absent; the
  helper stopped and removed the VM, verified memory assignment 0, and
  detached the VHD and both ISOs. Its failure receipt says
  `guest service readiness was not observed`. A read-only mount found zero
  partitions in the task VHDX. Hyper-V's task-GUID-filtered Worker log records
  event 18603, “failed to boot an operating system”; thus Windows Setup never
  began partitioning. The ISO's EFI boot files exist and the x64 EFI loader
  signature is valid, but the precise firmware-level reason remains unknown.
- The official Microsoft Evaluation Center VHD link
  `https://aka.ms/WinServ2025vhd-enus` returned an 11,686,379,520-byte VHDX.
  After identifying its `vhdxfile` signature, it was stored with a `.vhdx`
  suffix under the safe-delete-owned root's `windows-eval-vhd` directory.
  `Get-VHD` confirms a detached dynamic 64 GiB image. A read-only mount found
  the expected EFI/MSR/NTFS layout; reading only the official image's SOFTWARE
  setup identity confirmed Windows Server 2025 Datacenter Evaluation in
  `IMAGE_STATE_GENERALIZE_RESEAL_TO_OOBE`. No account or password data was
  read. The source image is left unchanged.
- A new `vm-smoke-vhd` path is being added. It converts the official VHD into
  a nonce-specific task VHDX, validates the OOBE reseal state, copies only the
  helper executable and public seed JSON, and registers the fixture service
  in the clone's offline SYSTEM hive as an automatic LocalSystem service. It
  unloads the temporary hive, keeps the source VHD untouched, and leaves
  normal OOBE enabled. It creates a fixed-memory Gen 2 VM with Secure Boot, no
  DVD/NIC/GPU, and an explicit VHD-first boot order. The implementation is
  committed/pushed as `6dc63dd`; Go compile and vet passed, with no Go test
  files in the package.
- The first `vm-smoke-vhd` attempt stopped at the owned-path check before
  conversion or VM creation. AdminBroker identified that the script passed
  the helper executable from the parent root while requiring every input to
  be under the dedicated VHD root. No guest VHDX or VM was created; the exact
  failure was retained. The code now passes the already-created helper copy
  from the seed directory; this correction was committed/pushed as `c8d8320`.
- After that fix was pushed as `c8d8320`, the next attempt stopped before
  conversion: the local Hyper-V `Convert-VHD` command does not accept
  `-VHDFormat`. The command's parameter list was checked locally, and the
  official Microsoft Learn reference confirms the destination filename
  extension selects VHD versus VHDX. No clone or VM was created. The helper
  now relies on its `.vhdx` destination suffix and keeps `-VHDType Dynamic`;
  the correction was committed/pushed as `629be9e`.
- The next VHD attempt converted the Microsoft VHD and copied the helper/seed
  into its task-owned clone, then stopped when the temporary SYSTEM hive could
  not unload. The failure receipt and AdminBroker output were retained. The
  SYSTEM hive remained loaded after that child process exited; the exact
  `CC2746System_*` alias was identified by its task-specific prefix, unloaded
  through AdminBroker, and the guest VHDX was detached. A read-only check then
  confirmed the helper, seed and preparation marker in the clone; a separate
  temporary hive read confirmed `CCGuest2746` has Auto start, LocalSystem, and
  the expected image path. The old clone has a stale client-certificate pin
  because its host key was cleared after the failed run, so it will not be
  reused. The cause was an undisposed RegistryKey handle for the offline
  `Select` subkey. Source now explicitly disposes it before unloading. This
  correction is uncommitted and untried; the failed clone remains detached in
  the safe-delete-owned root for final cleanup.
- After fixing the hive handle and rerunning with a fresh VHD clone, the guest
  booted but the service did not send its COM readiness line. The fixture
  helper stopped/removed the VM and verified 0 assigned memory; both guest
  VHDX copies and the official source are detached. The failed clone's System
  event log has Service Control Manager event 7000 for `CCGuest2746`: the
  service executable was not found. The helper and seed files exist in the
  clone, and a temporary hive read confirmed the service is Auto/LocalSystem
  with ImagePath `\SystemRoot\System32\cc2746-guest.exe --service`. The
  working diagnosis is that SCM did not resolve that NT-style prefix for this
  Win32 service. Microsoft examples use `%SystemRoot%` in expandable service
  paths; the helper now writes `%SystemRoot%\System32\cc2746-guest.exe
  --service` as REG_EXPAND_SZ. This correction is uncommitted and untried.
- After that path correction was pushed as `c9bcaef`, a fresh guest again
  started the service but emitted no readiness line. Its System log records
  the service entering Running, then event 7023 “Incorrect function”. The VM
  was stopped/removed, assigned memory 0, and the clone/source VHDs detached.
  The current service opens COM1 with `os.OpenFile`; Microsoft documents that
  serial communications handles require `CreateFile` with share mode zero and
  `OPEN_EXISTING`. The next change uses that documented exclusive-open
  contract through `CreateFileW`. This is a cause hypothesis, not yet verified.
  The current correction is uncommitted and untried.

## Remaining

1. Rerun `gofmt`, `go test ./...`, `go vet ./...`, and `git diff --check` for
   the Win32 COM1 open correction; commit and push it.
2. Build the helper into the safe-delete-owned parent root and run
   `windows-guest-fixture.exe vm-smoke-vhd <root>\windows-eval-vhd` once. The
   command should preserve the Microsoft source VHD and use only its copied
   nonce-owned guest VHDX.
3. Confirm guest readiness, the normal and cancellation child-process
   receipts, then separately verify VM Off, assigned memory zero, and removal.
4. Copy sanitized evidence into the worktree, update ToDo2737's note
   while preserving its existing content, and commit/push the result.
5. Run safe-delete `plan`, issue its normal deletion in a separate call, then
   `finish`. Do not delete the root if VM ownership or removal is uncertain.
6. Save the final scheduler checkpoint using the current job, branch and HEAD.

No official runner or product source was used. The original CI has not run.
