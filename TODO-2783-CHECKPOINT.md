# ToDo 2783 resume checkpoint

State: SDK guest preparation has not started. No VM/guest resource was assigned to this job.

Completed:
- Read D:/CC/AGENTS.md, this worktree AGENTS.md, shared-todo and safe-delete-owned-temp-cleanup instructions, Japanese reporting checklist, and the saved 2737 specification.
- Confirmed the active worktree is not classified as a client resource.
- Confirmed GitHub API identity for AutoDevJapan/cc-pc-tools-rs: private repository, admin permission.
- Read only the requested fixture source at f6d086b58256fdf9d33456c3e79737f1b6c0dc66 and evidence/docs at ace29f8.
- Existing guest evidence demonstrates the finite 2746 fixture and cleanup only; it does not demonstrate SDK delivery or SDK version checks.

Not done:
- No safe-delete-owned SDK root or new VM was created.
- No SDKs were downloaded or installed.
- No Go module was implemented or guest-tested.
- No product source, Cargo build/test, runner registration, or official CI was accessed or executed.
- No resource was acquired or requested.

Resume:
1. Continue only after the scheduler assigns resources sufficient for a fresh Hyper-V guest (target 2 vCPU, 4 GiB, no GPU).
2. Create a new safe-delete-owned root and fresh nonce VM; do not restart the old guest or touch quarantine.
3. Implement the isolated module under tools/windows-ci-sdk-2737, using only the fixed public SDK/tool versions from the original saved specification.
4. Perform guest service version checks, collect private evidence, stop/remove the VM and verify VHD detach, retain the SDK template in the owned root, then commit/push and checkpoint.
5. Report the unexecuted official CI, Cargo verification, and runner registration explicitly.
