# ToDo2746 Windows guest handoff

Last saved: 2026-10-17. Branch `scheduler/todo-2746-godd-mcp-windows-guest`,
last pushed commit `fb1c9e1`.

## Completed

- The earlier credential-free host Job Object fixture is committed as
  `a218d5e`; its actual normal/cancel results are in
  `tools/windows-guest-fixture/evidence/todo-2746-20261017.json`.
- The offline Windows Server 2025 VM provisioner and LocalSystem COM/mTLS
  fixture are committed in `adbb087` and `fb1c9e1`. `go test ./...` and
  `go vet ./...` passed after these changes. They only compile the module; the
  Hyper-V guest path has not yet run.
- `vmms` is Running. The helper specifies a new Gen 2 VM with 2 vCPU, fixed
  2 GiB RAM, Secure Boot, no NIC, and no GPU adapter. It creates its VHDX and
  VM under the current safe-delete-owned root.

## In progress

- `safe-delete` owner ID: `bbcdd7148e9c73866e40b02e1ac45e25`.
- Root: `%LOCALAPPDATA%\Temp\codex-owned-bbcdd7148e9c73866e40b02e1ac45e25`.
- The official Microsoft direct Windows Server 2025 Evaluation ISO download
  (`https://aka.ms/WinServ2025iso-enus`) is active in this job's exec session
  83449. Its expected length from the Microsoft CDN HEAD response is
  8,152,356,864 bytes. The last exact file-size read was 5,363,462,144 bytes;
  a later curl update showed about 5.06 GiB. Re-read the current size. Check
  whether that session is still alive before starting another transfer.
  If it ended, resume the same file with curl `--continue-at -` and the same
  official URL; do not fetch a second copy.

## Remaining

1. Confirm the ISO has the expected byte length, calculate its SHA-256, and
   let the provisioner verify Microsoft's Authenticode signature on setup.exe.
2. Build the helper into the owned root and run its `vm-smoke` command with
   that ISO. This path uses CC-AdminBroker and transfers only the helper plus
   a public client-certificate pin into the guest.
3. Confirm the normal and cancellation fixture receipts, then separately
   confirm VM Off, assigned memory zero, and removal.
4. Copy the sanitized evidence JSON into the worktree, update ToDo2737's note
   while preserving its existing content, and commit/push the result.
5. Run safe-delete `plan`, issue its normal deletion in a separate call, then
   `finish`. Do not delete the root if VM ownership or removal is uncertain.
6. Save the final scheduler checkpoint using the current job, branch and HEAD.

No official runner or product source was used. The original CI has not run.
