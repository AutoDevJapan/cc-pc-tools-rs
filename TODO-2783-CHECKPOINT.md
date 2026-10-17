# ToDo 2783 resume checkpoint

State: Host provisioning/recovery source and a fixed CLI entrypoint are implemented and locally verified. Real ISO download, VM provisioning, SDK installation, version checks, and template evidence remain unexecuted because no guest resource is assigned.

Completed:
- Re-read the startup owner instructions, worktree AGENTS.md (no CLAUDE.md exists here), original 2737 spec, and required skills. Confirmed this worktree is not client-protected and GitHub owner/repository remains AutoDevJapan/cc-pc-tools-rs.
- Used only the permitted 2746 fixture source/evidence commits f6d086b58256fdf9d33456c3e79737f1b6c0dc66 and ace29f8 as prior technical evidence; no old task folders, quarantine, or product source was accessed.
- Implemented isolated Go module tools/windows-ci-sdk-2737 with pinned public artifact versions/hashes, strict operation allowlist, bounded JSON, SHA-256 checks, TLS 1.3 mutual certificate pins, redirect/proxy restrictions, and private IPv4 binding.
- Implemented the Windows guest SDK backend for Git for Windows, PowerShell 7, Rust 1.97.1 MSVC components, Microsoft-signed Build Tools with pinned MSVC/Windows SDK component IDs, and sccache. It uses fixed installer paths/arguments, guest-only CARGO_HOME/RUSTUP_HOME/TARGET/SCCACHE_DIR, 30GB sccache cap, private SYSTEM-only logs, and no build/test/install of Rust projects.
- Implemented a Windows LocalSystem service entry point, strict public seed, in-memory Ed25519 server identity, nonce-bound COM1 handshake, private IPv4 TLS listener, per-guest firewall rule restricted to the helper executable/TCP 38473/LocalSubnet, and sanitized result codes.
- Implemented public host identity/seed creation, deterministic instance-scoped Hyper-V COM pipe naming/opening, nonce-bound COM response validation, and a fixed SDK prepare/version-check sequence.
- Verification passed: `go test -timeout 30s ./...`, `go vet ./...`, Windows amd64 `go build ./...`, and Windows cross-target `go vet ./...`. Two source-level issues found during iteration were fixed: a missing Windows error constant was defined locally, and a net.Pipe TLS rejection test was replaced with a direct pinned-certificate callback test to avoid deadlock while TLS sent its alert.
- Added fixed Windows Setup answer/staging assets (EULA only; no OOBE skip, account, or password), a standard specialize-stage LocalSystem service registration, a fixed Gen1 VM provisioner (2 vCPU, fixed 4 GiB, one Default Switch NIC, no GPU), and exact-VM stop/remove/VHD-detach recovery script that retains the SDK VHDX.
- Added host methods to create and verify a safe-delete-owned root, download the Microsoft Evaluation Center image only over allowlisted HTTPS, build the helper and public seed ISO, provision through CC-AdminBroker, prepare over the instance-bound COM channel, validate the SDK receipt, and capture template/recovery evidence.
- Added a no-argument Windows host CLI restricted to this scheduler worktree/branch. It does not run automatically; it has not been launched.
- Verification after the host workflow addition passed: `go test -timeout 30s ./...`, `go vet ./...`, Windows amd64 `go build ./...`, and Windows amd64 `go vet ./...`. `git diff --check` also passed.
- Current source edits are not yet committed; commit/push this milestone and save the scheduler workspace checkpoint before stopping.

Not done:
- No guest resource was assigned; no fresh VM exists. An earlier safe-delete-owned host root and guest helper artifact exist, but they do not prove SDK readiness.
- Host Hyper-V provisioning code exists but has not executed. The official ISO is not downloaded or independently hash-verified; helper/seed media staging, LocalSystem service registration during standard Setup specialize, VM start/stop/remove, memory-zero/VHD-detach proof, SDK-only template export and its path/hash/size evidence remain.
- Guest Windows code has only cross-compiled. No installer, Microsoft signature check, guest firewall operation, version probe, or recovery behavior has run in a VM.
- No product source, Cargo build/test, runner registration, PR, official CI, or Prd operation was accessed or executed.

Resume:
1. Keep this scheduler branch and module; do not touch the old guest or quarantined data.
2. Once guest resources are allocated, run the no-argument host CLI from this worktree's assigned scheduler branch. It creates a new safe-delete-owned root and fresh nonce guest capped at 2 vCPU/4 GiB, with GPU/device passthrough/host mounts disabled and only the existing Default Switch NIC.
3. The CLI downloads the public Microsoft Evaluation Center ISO, stages a public seed with only the instance and client certificate hash, and starts the VM through CC-AdminBroker. The ordinary Setup specialize flow installs the LocalSystem service without bypassing OOBE or creating an account.
4. Run the fixed SDK sequence through the LocalSystem service, verify each pinned version, preserve logs privately and sanitized evidence, then stop/remove the VM and confirm memory zero plus VHD detach.
5. Keep the SDK-only template in the owned root with absolute path, hash, size, and VM recovery receipt. Commit/push/checkpoint and report all out-of-scope CI/source steps as unrun.

Wait registration attempt: todo.exe wait-retry 2783 failed with todo_store_too_large (shared ToDo exceeds 64 MiB). No task state change was made. The same active job is retaining this work and waiting for guest resource allocation; do not repeat the oversized store write without a storage repair.

Additional preparation: created safe-delete-owned template root `ff7aab696c82983c97d77980aac3c215` inside this worktree (guard classification `client_resource=false`). Built the Windows amd64 guest service binary there: 11,643,392 bytes, SHA256 `6FEC75F43F9EF132D729E09F12B86E08DA710B9C6665717960CB426B19DB5148`. This is a seed helper artifact only; it has not run in a guest. A separate safe-delete default Temp root was classified `client_resource=true` and was left untouched.

After source commit `22e826a9f5d5d858e868ae9c15ec31fc7d8d5a8f`, rebuilt the helper in the same owned root: 11,659,264 bytes, SHA256 `F501B692C9818B28281FC822D96D1926487622C549057B89C0897BE19EEAA01D`. The first rebuild guard check targeted a path one directory too deep and failed closed before running `go build`; correcting the root to the worktree root passed scrub391 (`ok=true`, `client_resource=false`, classification verified). The failed check created no files and changed no shared state.

Resource check 2026-10-17: scheduler status reported no busy resource and no guest allocation for this task; general job slots were available. This does not authorize VM creation. The earlier `todo.exe wait-retry 2783` attempt failed with `todo_store_too_large` at the 64 MiB shared-store limit. That registration has not been retried. The same active job remains responsible for resuming once a guest resource is assigned.

Checkpoint request attempt: the first temporary request-file path was resolved relative to the worktree's parent instead of the worktree root, so file creation failed and the scheduler call returned nonzero. No request file or workspace checkpoint was created by that attempt. The corrected request path is inside the previously verified safe-delete-owned root.

2026-10-18 resumed work: item_33 is completed with exit code 0; shared ToDo 2783 remains doing. The worktree is clean at HEAD/remote `3dd4f75` before new changes. The safe-delete-owned root guard recheck passed and D: had 3,032,535,543,808 free bytes. VM provisioning was not started.

ISO preparation failure and repair: the first helper build used a path relative to the Go module although the owned root is at the worktree root; no executable was produced. The corrected absolute path built a 10,254,336-byte helper. Its first official ISO request failed before creating a partial ISO; the safe response was only `network-or-protocol`. A separate no-follow request to the official `aka.ms` URL observed HTTP 301 to the exact hostname `software-static.download.prss.microsoft.com`. Added only that exact Microsoft hostname to the redirect allowlist and added a lookalike-host rejection test. A subsequent helper build was initially invoked from the worktree root and failed because no go.mod exists there; the module-root invocation is the corrected command. No ISO download has yet succeeded.

COM startup refinement: the guest now emits sdk-ready with instance/private-IPv4/server certificate SHA-256 first; the host then sends a fresh nonce probe and validates the echo. This avoids sending host bytes before Windows Setup has started the LocalSystem guest service. Go tests and Windows amd64 cross-build/vet passed after the change.
