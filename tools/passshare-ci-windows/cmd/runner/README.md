# PassShare Windows execution helper (ToDo2997, parent2994)

Only this directory is owned by this child. Parent baseline imported by normal
Git fast-forward: `1c0c153` (`internal/contract`, module files, planner and workflow).
Original runtime source is retained unchanged in Git at
`28c9b9587345f082821c62af804365cf1a64bfe9:tools/civ6-ci-windows`.

## Adaptation from the fixed source

All runtime Go files and related tests were copied into `cmd/runner`, without a
second Go module. Imports use `cc/passshare-ci-windows/internal/contract`.
No original runner, product, shared gate, shared SDK ACL or cache was changed.
Admission uses the parent's public PassShare ID1131498455 contract: main push
or its one reviewed candidate branch dispatch, `desktop-build.yml / windows /
Windows build`. General PR/fork inputs are rejected. The source destination is
`runtime/runner/_work/PassShare/PassShare`; Git objects and tracked bytes are
verified, with no owner source/profile or credential file read.

Namespaces: demand-start services `CC-PSW-PassShare` and `CC-PSW-fixture`;
per-job runner/pipe `CC-PSW-<instance>`; lock files under
`C:\ProgramData\CC\passshare-ci-windows`; collection markers under
`HKLM\SOFTWARE\CC\passshare-ci-windows`. The lock path is fixed even when
the controller's environment is minimal. The original environment-dependent
path could split the administrator and gate child's lock namespace.

Token handling remains stdin/RAM/one-message PID-checked pipe. Service workers
disable Authenticated Users allows, retain the protected launcher boundary,
use CREATE_NO_WINDOW and remain in a finite Job Object. The Cargo gate holds
until SCM STOPPED plus launcher drain proof; active count zero and retained
handles signalled are required. Collection resets per-job descendant ACLs and
ownership, checks absence of the service SID's grants, confirms the idle service,
and writes the durable registry marker last. The one continuing service profile
is retained and measured; it is not claimed as deleted.

Rust installs **stable minimal only**, without a second version or extra
components. The guard entries remain in private CARGO_HOME/bin. Preparation
rejects SDK roots longer than 100 UTF-16 units before copying or claiming the
service: the measured long-root library path was 273 characters and MSVC
failed LNK1104, while the shorter original registered worktree produced a
230-character path and linked successfully. Cargo's target is `runtime/target`,
outside the checkout (the original helper used the other product's tools/target).
Node is pinned
to **v22.16.0**, verified against the official Node SHASUMS256 inventory before
extraction into the job root. pnpm **10.11.0** installs under that private Node
prefix with npm scripts disabled and private config/cache. Installed machine
MSVC and WebView2 are read-only; no broad shared SDK grants are added. Tauri
itself is the pinned product dependency/action managed by the parent workflow.

## Parent CLI integration

Build: `go build -trimpath -o <owned-build-root>/bootstrap.exe ./cmd/runner`.
Plan is the flattened parent's `contract.Plan` plus `instance`, `root`,
`controller_sid`, `seconds` (1..14400), optional `cleanup_seconds` (60..14400,
zero=1800), `runner_version`, `runner_digest`, and fixture-only `fixture`
(`normal` or `cancel`) / `runtime_fixture`. Root must be a new safe-delete
`codex-owned-<instance>` root. Unknown fields and trailing JSON are rejected.
Credential-free fixtures use only the parent's OriginalHead/OriginalTree markers.

| CLI | caller | result |
| --- | --- | --- |
| `bootstrap.exe prepare <root> <official-runner.zip>` | CC-AdminBroker | immutable helper, claimed service, verified source/SDK, prepared receipt |
| `<root>/helper.exe execute <root>` | non-admin controller, allocated Cargo | token on stdin only; finite official job, drain and official result validation |
| `<root>/helper.exe stop <root>` | controller | one bound-service stop request |
| `<root>/helper.exe status <root>` | controller | JSON SCM state/PID |
| `bootstrap.exe collect <root>` | CC-AdminBroker | bound idle service/ACL/owner/profile/remote-registration checks, durable marker |
| `bootstrap.exe collect-unstarted <root>` | CC-AdminBroker | strict never-started real-job abort path |

`audit`, hooks, service/worker and tool aliases retain their original roles.
Machine mutation is invoked only through CC-AdminBroker. Successful broker
submission is not proof: verify collect-end, collection receipt and service state.

**Required parent changes**: workflow source hook must use `PSW_HELPER` and
`PSW_ROOT`, exported by the worker. The baseline workflow still uses `CIW_*`.
Its Rust1.95.0 assertions and hard-coded manifest must instead measure the
prepared stable toolchain; the specification explicitly requests stable only.
pnpm/Node assertions remain 10.11.0/v22.16.0. Update the parent's expected
workflow bytes/digest accordingly. No module dependency additions are needed.

Official distribution API rechecked 2026-10-19:
`actions/runner` release **v2.338.0**, asset
`actions-runner-win-x64-2.338.0.zip`, SHA256
`f48e0750a21812bca5f82de5f7f5aeae71abee647fab5a582f1742d07eba455f`.
Prepare independently rechecks the versioned release name/URL/digest, then
verifies and extracts the same opened archive bytes. No official registration
or product source/build has been performed by this child.

## Verification and resume

`go test -race ./cmd/runner` and `go vet ./cmd/runner` passed on Windows amd64;
helper build and test executable compiled in a safe-delete-owned root. The
restricted-controller fixture now grants the caller's restricted token its own
default process DACL (and requests TOKEN_ADJUST_DEFAULT) before non-UI creation.
It never obtains another user's token. Initial controller failures and later
successful launcher/collection evidence are preserved separately.

The first normal lifecycle ran (worker exit0, drained, three retained members),
then collection removed grants and reset ownership/32 descendants. One profile
and loaded hive remained by design, zero queryable account processes; one
unclassifiable process is reported rather than treated as a complete census.
Final runtime source is `c29e0ed`; its tested helper SHA256 is
`ef7f61b0dff7ea94e5955c9df0232cc8b54e9f06a49a9e4670b33cc05177def7`.
The [five measured cases](evidence/fixtures-20261019.json) preserve initial
normal, lifecycle cancel, long-root SDK failure, short-root SDK normal, and
short-root SDK cancel. The latter two run the final code through the actual
shared Cargo gate with a non-admin controller. Both install stable minimal
(measured rustc1.99.1), Node22/pnpm10.11.0, use installed MSVC14.44.35207 and
WebView2 155.0.4283.45 read-only, verify a dedicated foreground sccache process,
run genuine checkout-v4 main/post against synthetic Git, compile and execute
the dependency-free Rust test, reject changed source, and propagate an actual
Cargo exit101. Cancel observes a real Rust test PID alive before one SCM stop.
This checkout-v4 fixture does not claim to prove the parent's particular pinned
workflow action version; that official context is the parent's acceptance.

| case | worker exit | Job drain | collection |
| --- | --- | --- | --- |
| initial normal | 0 | 3 held member handles | 32 entries reset |
| lifecycle cancel/reuse | 125 | 4 held member handles | 29 entries reset |
| long-root SDK failure | 1 | true; failure preserved | 7860 entries reset |
| final SDK normal/reuse | 0 | 2 held member handles | see raw receipt |
| final SDK cancel/reuse | 125 | 10 held member handles | see raw receipt |

Every collect kept exactly one continuing fixture profile, zero additional
service grants and zero queryable processes; the known unclassifiable process
count is reported, never treated as proof of a complete census. Retained
handles signalled and active-zero Job drain are independently measured.
The stale initial helper's `stop` was refused while another root owned the
identity. The final service is stopped; no real service was installed.

Go race/vet/build passed for the changed Windows package; Linux vet also passed.
No fixture is official CI success. Parent2994 owns final review, official
job/source/hash verification, registration and product execution. A full Tauri
product build, signing, artifact hashes and official job are intentionally unrun.

Resume in the same scheduler branch; no branch switch has occurred. Owned roots:
`6ed420bb25ca63a6596944477262b79e` (first normal, collected, deleted/finish source_absent),
`b7200bee85f34092e1f46a488c4d72f1` (build/controller),
`6c7a7f3096023e3d38e786e22b71f852` (cancel, collected, deleted/finish source_absent),
`7155bdd37afc8e75e4dbf39509a5f18c` (failed SDK, collected),
`c0d0c333fca8d97bab2fb8219b1ee1a8` (final SDK normal, collected),
`5eac22dc8041f9b3a471dbb392f86b99` (final SDK cancel, collected).
Evidence is preserved before safe-delete plan / separate ordinary deletion / finish.
The unused empty Temp root `f26cea1e035f07ff555dd9d149d80033` was quarantined by
safe-delete; it was never used for source, SDK or fixtures.

**Remaining file cleanup, ToDo3004**: safe-delete refuses the first SDK root
with LINK_OR_REPARSE_POINT, including its quarantine command. Official rustup
created 13 relative aliases pointing to the same directory's rustup.exe.
No alternative deletion was attempted. The three collected SDK roots are
preserved for finite ownership/target validation and normal cleanup-tool repair
by [the separate task](cleanup-spec.md). Their ACL/process collection is complete;
their filesystem removal is not. This task waits for3004 when all other work is
saved; it is not an owner decision. Do not rerun fixtures or official jobs while
waiting solely for that cleanup result.

At the handoff boundary, the build/controller root is also scheduled for ordinary
safe-delete removal after its note files have been delivered. Rebuild it from
the saved commit if later needed. Parent integration may use the already
pushed runtime commits while SDK filesystem cleanup is pending. This child
does not create or merge a separate PR; the parent's combined review and CI
own that step, as specified in the original split.

An unused short auxiliary worktree was created before its guard result was
acted upon; after the guard returned protected/unverified, no SDK execution
or content inspection followed there. Its empty owned ID
`6605cd9989d526cb22ad782c620b01c4` could not be quarantined (cross-drive
destination refusal). Preserve it and the auxiliary worktree; do not change
the protection registry or report their paths. The successful SDK fixtures
instead used the explicit original registered worktree, guard confirmed false.
