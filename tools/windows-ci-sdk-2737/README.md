# Windows CI SDK guest helper

This isolated Go module contains the fixed Windows SDK guest agent, operation
backend, protocol, and artifact verification for the finite SDK preparation
task. The host has provisioned fresh Hyper-V guests and verified recovery of
failed attempts. Ordinary Windows Server Setup is running in the current
dedicated guest; SDK installation and version readiness are not yet confirmed.

The LocalSystem service accepts only `status`, `prepare-sdk`, and
`verify-versions`, with SDK IDs from the fixed set in `catalog.go`. Requests
have no command, path, URL, or environment fields. Artifact URLs must use the
public source allowlist and match the pinned SHA-256 digest. The transport
binds to a private IPv4 address, requires TLS 1.3 mutual certificate pins,
disables proxy use and redirects, and limits request and response sizes. A
nonce-bound COM exchange supplies only the instance, guest IPv4, and server
certificate hash. The guest firewall rule is limited to this service executable,
TCP port 38473, and `LocalSubnet`.

The pinned public artifacts are Git for Windows 2.56.0.2, PowerShell 7.6.6,
Rust 1.97.1 MSVC components (cargo, clippy, rustc, rust-std, rustfmt), and
sccache 0.18.0. MSVC Build Tools 14.50.18 and Windows SDK 10.0.26100.6901 are
recorded as component versions. The fixed guest backend verifies publisher
signatures or public release hashes, installs SDKs without compiling Rust
projects, and probes versions as LocalSystem. Actual guest results are recorded
separately from source compilation and host provisioning.

## Execution and evidence

- Run `windows-ci-sdk-host` without arguments from this task's scheduler worktree.
  The task allocates its own VM after checking actual Hyper-V/CPU/RAM/disk
  availability; no separate guest-allocation notification is required.
- The host creates a new safe-delete-owned root and fresh nonce VM (Gen1,
  2 vCPU, fixed 4 GiB, existing Default Switch only). A copied official ISO is
  measured against the saved validated public-input hash. Source images remain
  intact. No quarantined input is moved, modified, or deleted.
- Normal Windows Setup accepts the EULA and registers the service in
  `specialize`. No OOBE settings, accounts, or passwords are supplied.
- The fixed backend installs the pinned SDKs and verifies versions using the
  guest service account. Preserve private raw logs and sanitized evidence.
- Stop and remove that VM, verify zero assigned memory and VHD detach, and retain
  the SDK-only template with its SHA-256 and size in the owned root.

The real Windows named-pipe regression test checks that an idle overlapped read
times out. Provisioning failures, SDK preparation results, and VM recovery have
separate private receipts. Initial failures remain preserved. A running Setup
screen or a recovered failed VM does not establish SDK readiness.

No product source, Cargo build/test/install, runner registration, PR dispatch,
official CI, or production deployment is part of this module's execution.

## Pin sources

- [Git for Windows releases](https://github.com/git-for-windows/git/releases)
- [PowerShell 7.6.6 release and hashes](https://github.com/PowerShell/PowerShell/releases/tag/v7.6.6)
- [Rust 1.97.1 channel manifest](https://static.rust-lang.org/dist/channel-rust-1.97.1.toml)
- [sccache releases](https://github.com/mozilla/sccache/releases)
- [Microsoft Build Tools component IDs](https://learn.microsoft.com/en-us/visualstudio/install/workload-component-id-vs-build-tools)
