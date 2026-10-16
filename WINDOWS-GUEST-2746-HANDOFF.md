# ToDo2746 Windows guest handoff

Last saved: 2026-10-17. Branch `scheduler/todo-2746-godd-mcp-windows-guest`,
last pushed commit `e86c004`.

## Completed

- The earlier credential-free host Job Object fixture is committed as
  `a218d5e`; its actual normal/cancel results are in
  `tools/windows-guest-fixture/evidence/todo-2746-20261017.json`.
- The standard Windows Server 2025 unattended installer and LocalSystem
  COM/mTLS fixture are committed through `e86c004`. `go test ./...` and
  `go vet ./...` passed after the latest code changes. They only compile the
  module; the Hyper-V guest path has not yet run.
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

## Remaining

1. Build the helper into the owned root and run its `vm-smoke` command with
   the downloaded ISO. It verifies Microsoft's Authenticode signature and
   the evaluation image name before creating the VM.
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
