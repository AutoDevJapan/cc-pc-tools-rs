# Finite original PR3613 Windows guest backend (ToDo 2828)

## Non-administrator stock runner (2026-10-20)

Official Windows attempt 5 passed the previous unit group (2,641 passed,
zero failed), then failed two later integration assertions. Diagnosis 3112
matched both comparison hashes to `Ok(System)` versus `Ok(User)` in the same
ownership helper. Its caller tests and most unrelated code were unread; this
was a static diagnosis, not a new test execution. The existing SCM agent was
LocalSystem and used `CreateProcess`, inheriting that identity into the runner.

The SCM agent remains LocalSystem for clock, source proof and control. Stock
configuration and listening now use a compiled LocalService (`S-1-5-19`)
primary token, obtained through the documented passwordless service logon.
Both its supplied token and the actual suspended child's token must have that
SID and no Administrators group before execution. There is no fallback to
inherited SYSTEM, request-selected account, new user, password or host service.
The same suspended/assign/resume, handle allowlist and bounded tree drain apply.
Session teardown closes the token after the runner tree and hook handlers drain.

Only fixed guest tools receive read/execute access; fixed runner, profile,
temporary and cache directories receive modify access. The SDK root grants
traverse only, without inheritance. Private logs, downloads and bootstrap state
receive no runner grant. The verified checkout is owned by its actual executor,
without adding `safe.directory`; the SYSTEM source auditor still reads anchored
bytes and runs Git only against its own private metadata snapshot.

Review 3123 found that the service's former staging-based temporary environment
could place those snapshots in a worker-writable directory. The service now
creates a fresh `private\source-proof` directory under the retained SYSTEM-only
private parent before any source preparation. Its process-global `TMP`, `TEMP`
and `SystemTemp` remain there for the whole session. Configuration/listening
receive a separate copied environment with staging-based `TMP`/`TEMP` and no
`SystemTemp`; clearing child inputs cannot mutate the service environment.
An existing proof-temp entry is rejected. No runner grant includes that path or
its private parent. Real synthetic materialization and repeated Git proof pass
even with an unusable worker temp, and a child-process fixture confirms that
the worker temp selection does not change the parent's proof temp. These are
host synthetic checks, not actual LocalService/guest ACL acceptance.

The first actual launch of this account separation stopped before registration.
The generated root grant used bare `:X`, which real `icacls` rejects with exit
87. It now uses `:(X)`: an advanced right requires parentheses. A regression
executes that exact generated command against a public temporary directory and
reads back the ACL, requiring traverse only (plus optional synchronize) with no
inheritance or propagation. The earlier declaration-only check did not catch
this CLI grammar error. Corrected guest startup and workflow acceptance remain
pending; the pre-registration failure is preserved separately.

The SYSTEM-specific variable follows [GetTempPath2's documented SystemTemp
override and ACL requirement](https://learn.microsoft.com/en-us/windows/win32/api/fileapi/nf-fileapi-gettemppath2w).

Related Windows race tests and vet pass for this module and its controller.
Returning the launcher to inherited identity makes the new denial regression
fail; it is a mutation check of that former behavior. Actual LocalService
logon, child execution and ACL acceptance in the dedicated guest remain to be
verified after separate-model review. No new official CI result is claimed.

Primary reference: [LogonUser's LocalService example](https://learn.microsoft.com/en-us/windows/win32/api/winbase/nf-winbase-logonuserw).

## Stock runner ancestor enumeration (2026-10-21)

The diagnostic runtime reached physical source proof, then configuration
executed and exited1 with confirmed drain. The bounded console hash
`3308b9329027b71c3828f241c60bae77f26fb5c3c3cf36f68035c1521f0e0d3f`
exactly matches the stock ancestor-enumeration error for the SDK root, including
the terminal prefix and CRLF. The stock runner's
[Program permission check](https://github.com/actions/runner/blob/v2.338.0/src/Runner.Listener/Program.cs)
calls [IOUtil.ValidateExecutePermission](https://github.com/actions/runner/blob/v2.338.0/src/Runner.Sdk/Util/IOUtil.cs),
which enumerates every ancestor. Traverse-only SDK access cannot satisfy it.
The recovered diagnostic file contained largely zero-filled regions, so its
missing error lines are not evidence that no error occurred.

Keep the SDK root's traverse-only grant and all private/bootstrap boundaries.
The stock runner now uses the separate compiled sibling directory
`C:\ProgramData\AutoDevJapan\windows-ci-runner-2737`; its existing modify grant
moves with its mutable tree. Its checkout remains beneath that runner root.
Unpacking still rejects an existing root, source proof stays in SYSTEM-only
temporary storage, and account/path choices are not request inputs. No SDK
listing grant, permission-check bypass, workflow change or new host runner is
introduced. A synthetic ancestor-walk fixture fails with the old nested layout
and passes with the sibling; its negative control still refuses SDK listing.
Actual new-layout LocalService/registration/workflow acceptance is pending.

## Guest UTC before registration (2026-10-20)

The first actual session reached physical source proof and offline runner
registration, then failed to create a listening session. Its bounded console
log (724 bytes, SHA256 `36bf6560fa51357be9fc033f33a73e29509e630d88cf3e1eec48340567452898`)
reported deleted registration and missing Windows PowerShell. Recovered stock
diagnostic timestamps were sixteen hours ahead of the host's UTC. The clock
skew is observed; its causal relationship to the server error remains an
inference pending corrected real execution. No Windows Cargo step started.

The mutually pinned, single-use TLS channel now exchanges host UTC immediately
after its fixed plan is checked, before source transfer or registration. Only
the fixed LocalSystem guest implementation calls `SetSystemTime`; tests inject
a setter and never change the PC clock. The host binds the acknowledgement to
the instance and exact request, checks the readback and a maximum five-second
round trip, and persists numeric before/after observations before authority is
issued. A failed update, readback, transport, or persistence blocks registration.
The original 26-minute session deadline, source proof, ordered hooks, listener
drain, job ownership, and required Windows workflow remain in force.

The fixed guest PATH includes both Windows PowerShell for the stock `.cmd`
wrappers and the retained PowerShell 7 for workflow steps. These changes have
synthetic Windows race/vet coverage; actual UTC correction, listening, thirteen
Cargo steps, completion hook and official Windows CI still require acceptance
from the reviewed, same-revision host and guest executables.

Primary references: [SetSystemTime (UTC and scoped privilege)](https://learn.microsoft.com/en-us/windows/win32/api/sysinfoapi/nf-sysinfoapi-setsystemtime),
[Hyper-V does not correct a guest clock more than five seconds ahead](https://learn.microsoft.com/en-us/troubleshoot/windows-server/virtualization/virtual-machine-time-not-synced).

This Go module connects the retained SDK to the **stock GitHub Actions runner**.
It is a library plus a guest SCM/hook entry, not a host VM controller. It has no
GitHub API client, arbitrary execution RPC, request-selected executable, path,
URL, environment, shell, workflow replacement or host Cargo admission marker.
No real product source, VM, SCM service, runner registration or Cargo was run
while implementing it. See [HANDOFF.md](HANDOFF.md) for verification and limits.

## Fixed inputs and library provenance

| Input | Pin |
| --- | --- |
| Tool repository | AutoDevJapan/cc-pc-tools-rs, id 1406818713 |
| Parent source/admission | c373a18c2aa455051226bc9569f0edfc7b526a3e |
| Source proof implementation reviewed by 2800 | c0f59e12e7a1cb08fe1fec08cfbebc2c71113c5a |
| SDK source | 9622a766e99a378427eccf9f21967f0fb9ca890f |
| SDK runtime / finalizer | 2381383d097779fb153689b5c83eb4c4f31f2456 / f7697c927cd0723e5d1c5cd595a471e9760ce5f1 |
| Official runner | v2.338.0, Windows x64 |
| Public runner ZIP SHA256 | f48e0750a21812bca5f82de5f7f5aeae71abee647fab5a582f1742d07eba455f |

The SDK asset receipt at
[todo-2783-sdk-asset-20261018.json](../windows-ci-sdk-2737/evidence/todo-2783-sdk-asset-20261018.json)
records the existing SDK-only template and its recovery. This implementation
does not access, rehash, copy, boot, remove or modify that asset. Parent2737 owns
the receiving check and a new disposable clone. The source/admission libraries
are imported **at the same canonical repository paths**, not duplicated under
another implementation. Their admission and secure checkout proof are unchanged.
The baseline `normalci.Plan` still returns the legacy label: parent must connect
its run-bound routes before `NewSeed` accepts its binding. Do not loosen the
guest to accept the legacy label.

## Host Go API (direct link by parent2737)

Module path:
`github.com/autodevjapan/cc-pc-tools-rs/tools/windows-ci-runner-2737`.
Require v0.0.0 and replace it with `../windows-ci-runner-2737`; retain the same
sourcepacket/product-ci/SDK replacements in the host module. Use this branch's
fixed code commit to build the guest and hash that binary before staging.

1. Hold the canonical shared Cargo lease for the **whole** VM lifetime. Retain
   its admitted `jobs` and `threads` (both 1..10). This module neither acquires
   nor releases that lease. A guest env variable is never admission evidence.
2. Explicitly pin `sourcepacket.Pin{HeadSHA,TreeSHA,WorkflowSHA256}` and obtain
   `normalci.Binding` from the original linked `normalci.Plan` with official API
   reads. Repository id1154066087, PR3613 and source ref
   `codex/todo2641-http-response-pseudo-20261015` are fixed. Current product
   head `b194d66524ef86330bb73ca3f22f9e5dd6ae0a07`, tree
   `f7eea82c96449b7749f98dd6c4b0344b64f1d651` are **not tool-code versions**.
   Parent must deliberately pin the source/workflow digest after any authorized
   route change; this module never follows a newer head.
3. Generate a cryptographic fresh 32-lowercase-hex instance and
   `sdkprep.NewHostIdentity(instance)` in RAM. Build
   `NewSeed(pin,binding,instance,sdkprep.CertificateHash(identity.Leaf),jobs,threads)`.
   `Plan` fields are Source, Instance, RunID, JobID, RunnerID, Attempt, Jobs,
   Threads, Role, Name, Label, Inputs. Seed RunnerID is exactly zero. RunID,
   JobID and attempt are positive. Role is only `windows-native-check`, Inputs
   is empty, name is `todo2737-<32hex>`, and label is exactly
   `cc-product-2737-1154066087-windows-windows-native-check-<RunID decimal>`.
   Binding additionally verifies repo/PR, SHA/tree/workflow SHA256, Windows OS,
   20-minute timeout and nonempty official job name. Another role/run is rejected.
4. `PublicSeeds(seed,identity)` returns `seed.json` and `runner-seed.json` public
   bytes. Neither contains a private key, token, source or arbitrary commands.
   Stage them offline as below on the new clone only. Keep the identity key
   RAM-only and clear its ed25519 bytes after session/recovery.
5. Boot the new clone with parent-owned nonce/VM/IP/COM bookkeeping, read its
   dedicated COM stream using SDK `SendCOMBootstrap` (never `PrepareGuestSDK`),
   then `Dial(ctx,bootstrap,identity,seed)`. SDK `COMPipeName`/
   `OpenOwnedGuestCOMPipe` remain the canonical host COM helpers.
6. Fetch/encode the explicitly pinned source with `sourcepacket` **in RAM**;
   call `HostSession(ctx,conn,seed,packet,controller)`. No original source is
   written to the host by this module. This implementation used synthetic
   packets only and did not call Fetch.

`Controller` has the following callbacks; these run inside the parent's lease:

| Method | Required parent action before returning success |
| --- | --- |
| Queued(ctx,Plan) → normalci.Binding,error | Re-read original official queued job with normalci.Plan, same identity/pin/job/attempt/role/label, RunnerID0; called initially, after source proof and before listen |
| Token(ctx,Plan) → []byte,error | Verify queued identity and nonce inventory; durably save registration intent; obtain short-lived registration token through official repo API; return RAM bytes only |
| Registered(ctx,Plan) → int64,error | Official API: exact name, exactly one expected label, offline, not busy, unique positive numeric runner ID; durably save ID before return |
| Claim(ctx,Plan,runnerID,Event) → error | Re-read official numeric JobID/run/attempt, exact runnerID/name/label/ref/event/source/workflow/inputs claim; use shared normalci claim policy; single stage claim only |
| Observe(ctx,Event) → error | Persist only bounded stage/numeric/hash/proof metadata; no source/token/log/credential body |

The host sends source only after the first queue check. On source-ready it
checks physical proof and queued identity, saves that metadata, acks, then calls
Token. Configuration does not start the listener. After configured, the host
verifies/persists the numeric runner identity and rechecks the queue, then sends
the positive listen ack. Start/complete require physical proof, exact stock
context and positive official host claim before the hook returns successfully.
`HostSession` returns the final Event and error; a successful `runner-exited`
has zero Exit and Drained=true. **It is not official job success.** Parent must
independently poll the official completion, preserve its bounded private log,
then recover VM/disk/registration and finally release its Cargo lease. Parent
must fence/retain admission whenever VM/process quiescence remains unknown.

Host channel EOF is `ErrHostEOF`, malformed framing is `ErrProtocol`, and host
context cancellation/deadline remains a context error. Guest distinguishes
ErrHostEOF, ErrHostCancel and ErrServiceStop. A process timeout while the channel
is alive emits Failed/FailureCode=timeout and returns DeadlineExceeded, separate
from Complete and RunnerExited. Process/drain failures emit process or
drain-unconfirmed. Failure telemetry must match the registered numeric RunnerID
and a post-listen phase. Timeout/process requires Drained=true, a measured Exit
and a valid stdout SHA256; drain-unconfirmed requires Drained=false and may omit
unavailable exit/hash data. Inconsistent failures are ErrProtocol and are not
persisted by Observe. No failure metadata is official completion or a cleanup ack.

## Complete offline staging contract for the new clone

Build `go build -trimpath -o <owned-output>/windows-ci-runner-agent.exe
./cmd/windows-ci-runner-agent` from this module on Windows amd64. Record tool
commit/tree, executable SHA256 and the source/admission/SDK pins. Only parent
boots the clone or invokes broker provisioning. No installer or host registration
is part of this module. Parent downloads the **public** payload once from
[the pinned official release](https://github.com/actions/runner/releases/tag/v2.338.0)
using the compiled `RunnerURL`, validates `RunnerSHA256`, and stages the ZIP.
Guest validates that same SHA256 over the bytes it actually extracts.

Mount only the parent's new copied disk offline; preserve its normal boot and
the existing `WindowsCISDK2737` LocalSystem automatic service. Replace that
clone's old helper in place at
`C:\Windows\System32\WindowsCISDK2737\windows-ci-sdk-agent.exe` with the new
runner executable **and also** stage an identical copy as
`C:\Windows\System32\WindowsCISDK2737\windows-ci-runner-agent.exe` for the
fixed hook/guest entry. Since the executable validates its exact path, update
the clone's offline service ImagePath under
`HKLM\SYSTEM\ControlSet00N\Services\WindowsCISDK2737` to the quoted latter
path; select ControlSet00N through the offline SYSTEM hive Select/Current.
The original helper copy can instead be left unused. Do not edit a host service
or the retained image. Keep Start=2, Type=0x10 and ObjectName=LocalSystem.
Preserve the received public helper directory's permissions; runner state is
under the existing SYSTEM-only SDK tree. Do not copy any owner profile or keys.

In that same helper directory stage exactly:

| File | Content |
| --- | --- |
| windows-ci-runner-agent.exe | Fixed-code Windows amd64 build |
| seed.json | First byte slice from PublicSeeds (SDK Instance/client cert hash) |
| runner-seed.json | Second slice from PublicSeeds (Seed, including immutable Plan) |
| actions-runner-win-x64-2.338.0.zip | Public official ZIP, exact RunnerSHA256 |

Before unmounting, ensure no `runner`, `runner-hooks`, `runner-listener.log`,
previous `.runner`/`.credentials`/source exist in the new clone. These locations
must be fresh, not cleaned on behalf of another attempt. Configure this clone's
COM1 to SDK `COMPipeName(instance)` and parent-owned fresh VM nonce. Use only
the existing approved switch and guest firewall flow; never alter host firewall,
switch, shared runner or other Org. Detach the mounted VHD before boot.

The compiled entry accepts no service arguments and dispatches through the SDK
SCM. It reads both offline seeds and requires exact agreement. SDK waits for
normal Windows Setup completion, finds a private IPv4, creates a fresh server
ed25519 key **in RAM**, pins the client hash with SDK `ServerTLSConfig`, and
listens on TCP38473. The SDK's guest-only rule names the current helper and
LocalSubnet. COM emits only sdk-ready (instance/IP/server certificate hash) and
challenge/probe-ack. Source, SDK ZIP bytes, tokens and private keys never use COM.
TLS1.3 mTLS uses the fresh client/server pins and exact same instance. The only
HTTP endpoint in runner mode is POST `/v1/windows-runner`, Upgrade
`cc-windows-runner-2737-v1`, and a single connection consumes the clone even on
failure. SDK preparation/versions endpoints are absent in runner mode.

## Guest SDK environment and stock execution

The existing root is `C:\ProgramData\AutoDevJapan\windows-ci-sdk-2737`:

| Location | Purpose |
| --- | --- |
| git\cmd, git\bin | Received Git2.56.0.windows.2 |
| powershell\7 | Received PowerShell7.6.6 |
| rust-1.97.1\bin | Received rustc/Cargo1.97.1, fmt/Clippy |
| vs-build-tools\Common7\Tools\VsDevCmd.bat | Fixed arch x64, host x64, vcvars14.50, winsdk10.0.26100.0 |
| sccache\…\sccache.exe | Exactly one received sccache0.18 executable; reject ambiguity/links |
| cargo-home, rustup-home, target | Dedicated guest Cargo/Rustup/output directories |
| sccache-cache | Dedicated guest cache, 30G |
| service-profile, staging | Dedicated profile/temp, no owner environment |
| runner-hooks | Fixed start.ps1/complete.ps1 calling the compiled hook entry |
| private\runner-listener.log | Private stdout prefix, hard8MiB maximum, SHA256 returned |

The stock runner and its registration files are under the separate fixed
`C:\ProgramData\AutoDevJapan\windows-ci-runner-2737` root. Its
`_work\GoDD-MCP-Alpha\GoDD-MCP-Alpha` child is the materialized source and the
actual stock `GITHUB_WORKSPACE`; it is not beneath the SDK's private ancestor.

The child environment is an allowlist; Jobs/Threads from seed become
CARGO_BUILD_JOBS/RUST_TEST_THREADS. Explicit original workflow serial flags
remain authoritative. The environment is no proof of the host gate. VsDevCmd
sets INCLUDE/LIB/toolset/SDK for stock run.cmd. There is no hand-written copy of
the thirteen Cargo commands, profile selection, timeout, permissions, if or gate.
Original workflow bytes and source blobs are never normalized. Sourcepacket
Materialize and VerifyCheckout retain their full secure no-follow/handle-hash/
metadata/identity checks. At start they prove the pre-materialized tree before
stock checkout; at complete they prove the physical tree after actual checkout
and post-action auth cleanup. The start hook alone does not prove checkout ran.

Configuration uses stock `config.cmd --unattended --ephemeral
--no-default-labels --disableupdate --url https://github.com/AutoDevJapan/GoDD-MCP-Alpha
--name <bound-name> --labels <bound-label> --work _work` with token only in
`ACTIONS_RUNNER_INPUT_TOKEN`, consumed/removed by stock CommandSettings. Config
stdout/stderr are never retained or returned. Offline means **not listening**,
not disconnected: official runner configuration still needs GitHub connectivity.
No registration API is called by this Go module or its tests.

Only after the host's persisted numeric runner ack does fixed stock `run.cmd`
start. Stock synchronous `ACTIONS_RUNNER_HOOK_JOB_STARTED` and
`ACTIONS_RUNNER_HOOK_JOB_COMPLETED` call the external fixed hooks. A per-session
RAM capability authenticates the loopback hook channel. It is unrelated to Cargo
admission. Required GITHUB_* context and empty dispatch inputs are checked;
the parent supplies the numeric JobID/RunnerID claim from the official API.
The [official hook documentation](https://docs.github.com/en/actions/how-tos/manage-runners/self-hosted-runners/run-scripts)
states start is before steps and complete is before official completion. No VM
shutdown occurs in either hook. Both await positive host ack, are single-use,
and have a90-second local timeout. The workflow SHA context is a **commit SHA**;
the workflow blob SHA256 is independently bound in source proof/Plan/Binding.

Every executed config/listener command uses CREATE_NO_WINDOW, starts suspended,
joins an owned kill-on-close Job Object, then resumes only on suspend-count1.
Inherited handles are restricted to NUL/stdout. Cancel, EOF, deadline or service
stop interrupt the entire session and terminate/drain the tree. Drain checks
job ActiveProcesses0, the retained root handle, and same-job descendant handles
retained before termination; no PID lookup after stopping proves liveness.
EOF during configuration also cancels its tree. Protocol order rejects reentry,
unexpected stages, wrong role/run/pin/inputs/runner, unknown/duplicate JSON keys,
truncated frames and oversized allocations (control32KiB/source256MiB).

Before registration acknowledgement, a failed configuration emits a bounded
`failed` observation after the registration token bytes are cleared. Its code
is one of `configuration-setup/create/identity/assign/resume/execute`, plus only
an observed numeric Win32 error for creation/identity/assignment/resume failures.
A measured exit is included only with confirmed drain and a valid output hash.
No command line, exception text, token, or output body is transmitted. The host
accepts this failure only while waiting for `configured`, with runner identity0;
it persists the observation and returns failure without registering, claiming,
or starting the listener. This observation does not authorize resource cleanup.
The metadata distinguishes a native launch failure from a measured child exit;
it does not by itself diagnose or fix the failed stock configuration.

Stdout is a bounded private prefix, returned only as SHA256. Stock `_diag` logs
remain private on the disposable disk, use1MiB pages, and cancel the tree at a
sampled64MiB/1024-file threshold (100ms sampling may overshoot within an interval;
this is not a hard filesystem quota). Every scan also caps total entries,
including empty directories, at2048 and depth at16, uses64-entry ReadDir batches,
and observes cancellation between entries. No diagnostic or credential body is sent
to host/model. Parent retains that disk on unconfirmed cleanup for its authorized
recovery; the guest never claims VM/registration/host-lock recovery.
