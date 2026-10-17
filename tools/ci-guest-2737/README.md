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

The first Azure image booted, but its readiness probe expired; that VM was
stopped and removed with memory zero. This does not establish the cause of its
missing readiness. A second, separate attempt used the general Canonical
NoCloud-compatible qcow2, pinned to SHA256
`6a81c37564db9b1ee84e141922625e1d7c5b389b99bb3c572e0243607d5bb4d2`.
`prepare-generic` converts those same verified bytes with public qemu-utils in
the owned tool container, then transfers only the SDK VHDX to the host.
`smoke-generic <root> <Linux-helper>` uses a new attempt directory and VM, without
overwriting the first attempt. It passed both real fixtures and mutual TLS,
then verified VM Off, memory zero, removal and absence. This is preparation
evidence, not product CI. Boot console data stays in private attempt evidence.
Memory is explicitly fixed; New-VM inherited dynamic memory on this PC, so its
startup-memory parameter alone did not establish a bound.

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

The constructor now persists the returned VM ID immediately after New-VM,
reads receipts even after errors, and rolls back that exact VM on configuration
or receipt-write failure. Both injected post-creation failure and real exclusive
receipt-write failure passed on Hyper-V, including authoritative CIM absence.
Broker flights are drained before interpreting results. No helper can turn a
missing receipt into proof of absence. Crash/power-loss recovery is still pending.

`capture-sdk` saves only the credential-free public runner/rustup SDK from the
fixed image. `sdk-smoke` sends bounded hash-checked SDK frames over mutual TLS
to a new owned guest. The guest installs public dependencies and Rust, then
probes runner/Cargo/Docker/buildx/sccache as the fresh guest user. It cannot
register a runner or receive product source. The public archive extraction
test passed against the exact SDK digest, after preserving initial directory,
internal symlink and tmpfs-capacity failures. Relative links may point only to
regular files inside that SDK and are created after regular extraction. A typed
guest failure is read concurrently with upload so a reset cannot hide it.
SDK VM bootstrap is a separate result; successful archive extraction is not
successful bootstrap or official CI. Command output is bounded and private.

The corrected SDK bootstrap subsequently passed in a new Hyper-V guest: runner
2.338.0, Cargo 1.97.1, Docker 29.1.3, buildx 0.30.1, sccache 0.18.0. These are
version probes, with Docker accessible to the fresh guest user, not Cargo tests
or official CI. The guest VM was stopped and removed with memory zero. Its
source-free, unregistered SDK VHDX remains an owned asset; verify its receipt/hash
and absence of the original VM before cloning it. The tool container is paused
at PID zero, preserving genisoimage in its writable layer. Validate its exact
recorded identity before restarting; stopped tmpfs seed/test data does not survive.

The follow-up constructor fix reserves a separate exclusive fixed-size recovery
journal before New-VM and writes the returned ID there before ordinary receipts
or rollback. A combined ordinary-receipt failure and injected rollback failure
passed: the host recovered the exact ID from the journal, then removed that VM.
This does not certify total storage failure or crash/power-loss recovery.

`source-smoke` uses the verified, stopped SDK VHDX, checks the original VM's CIM
absence and image digest, and transfers the fixed source packet over mTLS. It
cannot select another repo/ref/head or a host checkout location. The guest
alone materializes the source. Exact b194 HEAD, tree and all 1,904 saved blobs
were verified, followed by API revalidation and VM cleanup. The source proof
explicitly records `official_ci_success=false`. No Cargo/runner/workflow is
connected yet. Product source and commit metadata are never logged or written
on the host. `inspect-source` is only for in-memory guidance inspection; its
stdout is source data and must never be redirected to a host file.

`test-gate` now exercises the canonical Windows Cargo gate, without compiling
Rust. Its child receives only the admitted build/test caps, waits for a start
acknowledgement, and drains on normal completion or stdin EOF. The parent probes
the actual per-target Windows byte lock before work and after child completion.
Both real normal/cancel fixtures passed. The minimal host environment forwards
APPDATA for existing OAuth configuration, plus PATHEXT/ComSpec for native
PowerShell execution; no credential values or inherited admission markers are
forwarded. Read-only experiments confirmed both initial context failures.

`bootstrap-offline <owned-root> <SDK-root> <Linux-helper>` reserves that gate,
starts a fresh SDK guest, materializes the pinned source, then configures the
official ephemeral runner with a separate offline-only label. It never starts
run.sh/listener or accepts a workflow job. The registration token crosses only
RAM/mTLS and the stock runner environment; configuration output is discarded.
The host verifies offline/not-busy state via the official API, stops/removes the
exact VM, deletes only its nonce-named registration and confirms absence, then
drains the child and observes target-lock release. The corrected bootstrap
passed. The initial missing APPDATA and PATHEXT failures remain separate.
No actual Cargo build, job hook, workflow dispatch or CI success is certified.

The next, unexercised `linux-job <owned-root> <SDK-root> <Linux-helper>
<request.json>` path now connects canonical Cargo admission to a single official
job. The explicit request contains Source (head/tree/workflow hash), RunID,
Attempt, Role (`verify` or `ci-gate`), and empty Inputs. Executable/program paths
are not accepted. It cannot advance a ref, enable a route variable, dispatch a
workflow, or prepare Windows. The actual original manual workflow has no inputs;
its workflow_call release inputs are not accepted as manual job authority.

The public seed fixes the expected revision. mTLS source transfer precedes the
in-memory registration token; an exclusive intent precedes registration, and
the numeric runner ID is saved before starting the listener. The official API
resolver must verify queued ownership before registration/listening, and the
same job/runner claim at both hooks. Hooks verify real Git HEAD/tree/index,
all saved blobs/modes and absence of authentication/filter/hook configuration.
They bind workflow/run/attempt/ref/job/name/event/inputs and allow one start and
one completion. Hook failure blocks progress; hook completion is not official
success. The listener must exit and the official job API must independently
report the matching SHA/runner/job success. Official job logs are privately
bounded and hashed, including partial download evidence on failure.

Cancellation closes the TLS stream; process-group cleanup and VM shutdown run
before Cargo admission is released. A durable exclusive execution intent blocks
reuse after uncertain cleanup. If VM removal cannot be proved, the gate retains
its lease until read-only broker observations prove absence or Off/memory-zero;
it does not repeat a rejected stop/delete. That exceptional fence may require
an independent technical recovery job, never owner PC operation. Registration
cleanup and final intent reconciliation remain explicit after such a failure.
The new controller, hook, lease-fence and log-download path has NOT accepted an
official job or undergone real end-to-end cancellation yet. Passing unit tests
and compiled Linux/Windows binaries are not official CI evidence.

Review2795 found two Must issues in the previous head3826503: caller-selected
ProductCI execution and parent-link escape in physical source verification.
The former field is now rejected (including its old spelling/case variants).
The existing product-ci resolver/identity/YAML policy was MOVED into its
`normalci` package, and both the CLI and controller link that single code. No
second host executable is launched for admission. The running controller must
report its fixed module, linked policy and clean Git build revision before a
live job; `inspect-executor` performs only this read-only check. Its binary hash,
code revision and actual PID are trace receipts, not signed code attestation or
request-supplied authority. Build from the committed clean source for live use.

The source module opens every ancestor from the filesystem root without
following links. Linux uses openat/O_NOFOLLOW and revalidates identities after
reading; Windows refuses reparse points and holds parent handles without
write/delete sharing. Both hash bytes read from those handles, reject shared
hardlinks and validate file modes. Git reads an isolated private snapshot of
the real metadata bytes, not an untrusted source path; checked config bytes
are reused without a second read. Links, alternate/common metadata, replacement
refs and secondary worktree configuration cannot select external state.
The initial materializer uses this same physical verifier before its receipt.
These are implementations and synthetic acceptance results pending delta
review; they do not certify new original-source or official-job execution.

Current validation: Windows tests/vet for sourcepacket, guest and product-ci;
real Linux sourcepacket (8 top-level tests) and guest helper (9 tests), all pass.
Tests include actual altered-file/config rejection and wire acknowledgement/
cancellation. A test invocation before Linux test binaries finished compiling
was rejected before execution; the subsequent actual tests passed. No VM,
runner registration or original source execution was started by those tests.

Primary references: [Canonical images](https://cloud-images.ubuntu.com/releases/noble/release-20260926/),
[NoCloud](https://docs.cloud-init.io/en/latest/reference/datasources/nocloud.html),
[Hyper-V cmdlets](https://learn.microsoft.com/en-us/powershell/module/hyper-v/).
GitHub's [current billing reference](https://docs.github.com/en/billing/concepts/product-billing/github-actions)
lists self-hosted runner execution as free (checked 2026-10-17). No budget or
payment settings are changed; cache/artifact policies of the original workflow
must still be retained.
