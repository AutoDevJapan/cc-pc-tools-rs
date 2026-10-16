# Disposable Linux guest preparation for ToDo2737

This separate Go module prepares a new Hyper-V guest and token-free real
normal/cancel fixtures. It currently has **no product source transfer, official
runner registration, Cargo execution, workflow dispatch, or CI success path**.
The resident `product-ci`, CC runners and bridges are not deployed or changed.

The host uses only a newly safe-delete-owned root. The public Ubuntu image is
the Canonical release-20260926 Azure VHD tarball, pinned to SHA256
`3543723afd820d7a8a64ea7399376856a95ce15200439c38447170652a60a5f3`.
It is a GNU sparse tar: Go expands the holes into the logical VHD. The archive
is checked before parsing the same in-memory bytes; no product source is present.
The host's public VHD/seed files are SDK assets, not a product checkout.

`tools <root>` creates one independently labelled container from the already
verified credential-free SDK image, without host binds, devices, GPU, socket,
profile, privileged mode, or host PID namespace. It installs public genisoimage
in its own writable layer. This does not alter the base image or resident runner.
`extract <root>` expands the pinned public VHD. Do not repeat completed stages.

Build this module separately for Windows and Linux. The Linux binary has only
fixture operations; it rejects request-selected commands and paths. `test-linux
<root> <Linux-test-binary>` transfers the compiled test through exec stdin to the
owned tool container. Docker archive copy does not populate its live tmpfs;
`/work` remains a data-only noexec mount. Executable fixture code goes under
the owned container's `/usr/local/bin`. Linux tests observe an actual child and
the disappearance of its process group on normal completion and connection EOF.

`smoke <root> <Linux-helper>` builds a public cloud-init seed. The controller's
fresh TLS private key exists only in its RAM. The seed contains only its public
certificate hash and helper binary. The guest generates its own TLS private key
in RAM and emits only a public certificate hash/IP/instance via that VM's COM
pipe. The host binds this readiness to the exact owned VM, then uses mutual TLS
over its private network. A foreign client/server certificate is rejected.
The agent accepts at most two fixture sessions, never arbitrary shell/Cargo.

The Hyper-V script goes through CC-AdminBroker with no console or UAC. It creates
only the recorded nonce-named VM, its own VHDX and seed, and attaches the existing
Default Switch without changing it. It adds no GPU passthrough, host mount,
driver, network service or owner credentials. Cleanup requires exact VM ID,
name, owner note and configuration path, observes Off/memory zero, removes that
VM and checks absence. Failed/uncertain creation is retained for reconciliation.
`cleanup-tools <root>` checks only the recorded tool container, stops its actual
PID namespace to PID zero, removes it and volumes, and checks absence.

All normal/cancel/VM/cleanup results must be verified separately. Process fixture
success in Docker does not certify Hyper-V, official workflow steps or source
HEAD/tree. Host crash, power loss and logout recovery are not certified. Preserve
the first failure and correction evidence, then use safe-delete plan, a separate
normal delete call, and finish for the owned root. Do not remove active guests
or another task's data. An uncertain VM must be reconciled before root cleanup.

Primary references: [Canonical images](https://cloud-images.ubuntu.com/releases/noble/release-20260926/),
[NoCloud](https://docs.cloud-init.io/en/latest/reference/datasources/nocloud.html),
[Hyper-V cmdlets](https://learn.microsoft.com/en-us/powershell/module/hyper-v/).
