# ToDo 2783 resume checkpoint

State: Partial source milestone saved. Guest SDK installation and evidence remain incomplete because this job has no guest resource allocation.

Completed:
- Read D:/CC/AGENTS.md, worktree AGENTS.md (no CLAUDE.md is present in this checkout), the saved 2737 specification, and required shared-todo, cleanup, and Japanese reporting instructions.
- Confirmed this worktree is not a client resource and confirmed the GitHub API identity for AutoDevJapan/cc-pc-tools-rs.
- Read only the requested 2746 fixture source at f6d086b58256fdf9d33456c3e79737f1b6c0dc66 and docs/evidence at ace29f8.
- Added tools/windows-ci-sdk-2737 as an isolated Go module. It pins public artifacts and versions, rejects unknown SDK operations/JSON fields and non-allowlisted HTTPS origins, verifies SHA-256, and provides an instance-pinned private IPv4 TLS 1.3 mutual-auth transport with bounded requests/responses and redirects/proxies disabled.
- Added protocol, artifact, handler, and mTLS checks. Local go test ./..., go vet ./..., and Windows amd64 cross-build passed.
- Pinned Git for Windows 2.56.0.2, PowerShell 7.6.6, Rust 1.97.1 MSVC components, sccache 0.18.0, MSVC component 14.50.18, and Windows SDK 10.0.26100.6901. Public artifact digests are in catalog.go.

Not done:
- No guest resource, safe-delete SDK root, or new VM was created.
- Fixed guest Operations backend, Windows LocalSystem service integration, COM bootstrap, Microsoft signature verification, actual SDK installation/version measurement, SDK template, and VM shutdown/removal/VHD detach evidence remain to be implemented and verified.
- No product source, Cargo build/test, runner registration, product PR, or official CI was accessed or executed.

Resume:
1. Keep this branch and module. Do not restart the old guest or access quarantined data.
2. When guest resources are allocated, finish the fixed guest backend and LocalSystem service/COM bootstrap, then create a new safe-delete-owned root and fresh nonce VM with at most 2 vCPU/4 GiB and no GPU.
3. Install only the pinned public SDKs, verify versions from the actual guest service account, retain private logs and sanitized evidence, stop/remove the VM and verify memory zero and VHD detach.
4. Retain the SDK-only template in the still-owned root; record absolute path, SHA-256, size, and cleanup evidence. Commit/push and save scheduler checkpoint.
5. State clearly that original source, Cargo verification, runner registration, and official CI remain outside this task.
