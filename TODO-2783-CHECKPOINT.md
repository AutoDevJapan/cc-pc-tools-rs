# ToDo 2783 resume checkpoint

State: Partial implementation saved. Guest SDK installation and image evidence remain unverified; this job still has no guest resource allocation.

Completed:
- Re-read D:/CC/AGENTS.md, this worktree AGENTS.md (no CLAUDE.md exists in this checkout), the saved 2737 specification, and the prior owner/project instructions. Confirmed the worktree remains outside client protection and the GitHub repository is AutoDevJapan/cc-pc-tools-rs.
- Kept the fixed 2746 source/evidence scope from commits f6d086b58256fdf9d33456c3e79737f1b6c0dc66 and ace29f8; no old task folders, quarantine, or product source were accessed.
- Added the isolated tools/windows-ci-sdk-2737 Go module with pinned public SDK artifact versions/hashes, strict request allowlists, hash verification, private IPv4 TLS 1.3 mutual authentication, redirect/proxy restrictions, and bounded JSON messages.
- Added guest Windows backend for Git for Windows, PowerShell 7, Rust 1.97.1 with rustfmt/clippy, Microsoft-signed Build Tools with fixed MSVC/Windows SDK components, and sccache 0.18.0. Installations use fixed executable paths/arguments; no arbitrary command, path, shell, or repository RPC is exposed. Rust build/test/install operations are not invoked.
- Added the Windows LocalSystem service entry point, public seed validation, fresh guest Ed25519/TLS identity in RAM, nonce-bound COM1 bootstrap, private IPv4 listener, guest-only inbound firewall rule limited to port 38473/LocalSubnet/service executable, and sanitized API responses. Installer output is saved only to the SYSTEM-restricted guest directory.
- Added a host-side public seed builder, COM bootstrap client, and fixed prepare/version-check sequence. The host private key remains in the caller's RAM identity and is not included in the seed.
- `go test -timeout 30s ./...`, `go vet ./...`, Windows amd64 `go build ./...`, and cross-target `go vet ./...` pass. A prior COM mTLS test attempt deadlocked on `net.Pipe` while the server sent a TLS alert; it was replaced with a direct certificate-pin callback assertion and now passes.

Not done:
- No safe-delete-owned host root or new VM was created; no guest resource was allocated.
- Host-side Hyper-V provisioning, official ISO intake/verification, seed/helper staging, LocalSystem service registration through normal Setup, and the exact VM COM named-pipe adapter remain to be implemented.
- Guest service/backend has only cross-compiled; SDK installers, Microsoft signature validation, actual version probes, firewall behavior, shutdown, and VHD detach have not run in a guest.
- The SDK-only VHDX template and its path/hash/size/recovery evidence remain absent.
- No product source, Cargo build/test, runner registration, product PR, or official CI was accessed or executed.

Resume:
1. Continue this same branch/module; do not restart the old guest or access quarantine.
2. Finish host provisioning/COM adapter and package LocalSystem service in the normal Setup `specialize` stage.
3. After guest resources are assigned, create a new safe-delete-owned root and fresh nonce VM capped at 2 vCPU/4 GiB, with no GPU/device passthrough/host mounts and only the existing Default Switch NIC.
4. Run the fixed preparation sequence and version checks through the guest service, preserve private logs and sanitized receipts, then stop/remove the VM and confirm assigned memory zero and VHD detach.
5. Preserve the SDK-only template inside the still-owned root with absolute path, SHA-256, size, and VM cleanup evidence; commit/push and checkpoint. Explicitly leave original source, Cargo verification, runner registration, and official CI unclaimed.
