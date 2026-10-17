# ToDo 2783 resume checkpoint

State: Partial source implementation saved. Real SDK installation, version checks, and template evidence remain pending a guest resource allocation.

Completed:
- Re-read the startup owner instructions, worktree AGENTS.md (no CLAUDE.md exists here), original 2737 spec, and required skills. Confirmed this worktree is not client-protected and GitHub owner/repository remains AutoDevJapan/cc-pc-tools-rs.
- Used only the permitted 2746 fixture source/evidence commits f6d086b58256fdf9d33456c3e79737f1b6c0dc66 and ace29f8 as prior technical evidence; no old task folders, quarantine, or product source was accessed.
- Implemented isolated Go module tools/windows-ci-sdk-2737 with pinned public artifact versions/hashes, strict operation allowlist, bounded JSON, SHA-256 checks, TLS 1.3 mutual certificate pins, redirect/proxy restrictions, and private IPv4 binding.
- Implemented the Windows guest SDK backend for Git for Windows, PowerShell 7, Rust 1.97.1 MSVC components, Microsoft-signed Build Tools with pinned MSVC/Windows SDK component IDs, and sccache. It uses fixed installer paths/arguments, guest-only CARGO_HOME/RUSTUP_HOME/TARGET/SCCACHE_DIR, 30GB sccache cap, private SYSTEM-only logs, and no build/test/install of Rust projects.
- Implemented a Windows LocalSystem service entry point, strict public seed, in-memory Ed25519 server identity, nonce-bound COM1 handshake, private IPv4 TLS listener, per-guest firewall rule restricted to the helper executable/TCP 38473/LocalSubnet, and sanitized result codes.
- Implemented public host identity/seed creation, deterministic instance-scoped Hyper-V COM pipe naming/opening, nonce-bound COM response validation, and a fixed SDK prepare/version-check sequence.
- Verification passed: `go test -timeout 30s ./...`, `go vet ./...`, Windows amd64 `go build ./...`, and Windows cross-target `go vet ./...`. Two source-level issues found during iteration were fixed: a missing Windows error constant was defined locally, and a net.Pipe TLS rejection test was replaced with a direct pinned-certificate callback test to avoid deadlock while TLS sent its alert.
- Commits `67f6057`, `c8d1fa4`, and current uncommitted milestone work are on this scheduler branch; push the new milestone before stopping.

Not done:
- No guest resource was assigned; no safe-delete-owned host root or fresh VM exists.
- Host Hyper-V provisioning, official ISO validation, helper/seed media staging, LocalSystem service registration during standard Setup specialize, VM start/stop/remove, memory-zero/VHD-detach proof, SDK-only template export and its path/hash/size evidence remain.
- Guest Windows code has only cross-compiled. No installer, Microsoft signature check, guest firewall operation, version probe, or recovery behavior has run in a VM.
- No product source, Cargo build/test, runner registration, PR, official CI, or Prd operation was accessed or executed.

Resume:
1. Keep this scheduler branch and module; do not touch the old guest or quarantined data.
2. Finish host Hyper-V/ISO/setup staging without product source; use the exact fresh instance-named COM pipe and public seed containing only instance/client-certificate SHA-256.
3. Once guest resources are allocated, create a new safe-delete-owned root and fresh nonce guest capped at 2 vCPU/4 GiB, GPU/device passthrough/host mounts disabled, with only the existing Default Switch NIC.
4. Run the fixed SDK sequence through the LocalSystem service, verify each pinned version, preserve raw logs privately and sanitized evidence, then stop/remove the VM and confirm memory zero plus VHD detach.
5. Keep the SDK-only template in the owned root with absolute path, hash, size, and VM recovery receipt. Commit/push/checkpoint and report all out-of-scope CI/source steps as unrun.

Wait registration attempt: todo.exe wait-retry 2783 failed with todo_store_too_large (shared ToDo exceeds 64 MiB). No task state change was made. The same active job is retaining this work and waiting for guest resource allocation; do not repeat the oversized store write without a storage repair.

Additional preparation: created safe-delete-owned template root ff7aab696c82983c97d77980aac3c215 inside this worktree (guard classification client_resource=false). Built the Windows amd64 guest service binary there: 11,643,392 bytes, SHA256 6FEC75F43F9EF132D729E09F12B86E08DA710B9C6665717960CB426B19DB5148. This is a seed helper artifact only; it has not run in a guest. The safe-delete default Temp root was classified client_resource=true and was left untouched.
