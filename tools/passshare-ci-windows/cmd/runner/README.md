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
components. The guard entries remain in private CARGO_HOME/bin. Node is pinned
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
Final-code normal/cancel/reuse fixtures and SDK runtime acceptance are in progress.
No fixture is official CI success. Parent2994 owns final review, official
job/source/hash verification, registration and product execution.

Resume in the same scheduler branch; no branch switch has occurred. Owned roots:
`6ed420bb25ca63a6596944477262b79e` (first normal, collected),
`b7200bee85f34092e1f46a488c4d72f1` (build/controller),
`6c7a7f3096023e3d38e786e22b71f852` (next cancel),
`7155bdd37afc8e75e4dbf39509a5f18c` (next normal reuse).
Preserve evidence before safe-delete plan / separate ordinary deletion / finish.
The unused empty Temp root `f26cea1e035f07ff555dd9d149d80033` was quarantined by
safe-delete; it was never used for source, SDK or fixtures.
