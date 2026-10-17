# Windows CI SDK guest helper

This isolated Go module contains the fixed Windows SDK guest agent, operation
backend, protocol, and artifact verification for the finite SDK preparation
task. The Windows guest code has not yet been run in a VM.

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
recorded as component versions; the Microsoft-signed installer verification
and actual installation still need to be implemented and measured in the
dedicated guest.

## Remaining implementation and evidence

- Implement the fixed guest `Operations` backend and Windows LocalSystem service
- Install the helper as a LocalSystem service during normal Windows Setup's
  `specialize` phase. The service binary and public seed staging have not been
  exercised in a guest.
- Add host Hyper-V provisioning, official ISO validation, seed/helper staging,
  and LocalSystem service registration during normal Windows Setup.
- Provision a new safe-delete-owned root and nonce VM only after the scheduler
  assigns guest resources. Install the pinned SDKs and verify versions using the
  guest service account; preserve private logs and sanitized evidence.
- Stop and remove that VM, verify zero assigned memory and VHD detach, and retain
  the SDK-only template with its SHA-256 and size in the owned root.

No VM or SDK installer was run by the current implementation step. The host
COM named-pipe adapter only compiles; it has not been connected to Hyper-V. No
product source, Cargo build, runner registration, or official CI was accessed
or executed.

## Pin sources

- [Git for Windows releases](https://github.com/git-for-windows/git/releases)
- [PowerShell 7.6.6 release and hashes](https://github.com/PowerShell/PowerShell/releases/tag/v7.6.6)
- [Rust 1.97.1 channel manifest](https://static.rust-lang.org/dist/channel-rust-1.97.1.toml)
- [sccache releases](https://github.com/mozilla/sccache/releases)
- [Microsoft Build Tools component IDs](https://learn.microsoft.com/en-us/visualstudio/install/workload-component-id-vs-build-tools)
