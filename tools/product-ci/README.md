# Fixed candidate product CI

This Go module is independent of `pc-ci` and `linux-runner-bridge`. It neither
changes their admission policy nor registers old product directories. Four
numeric repository IDs, PR numbers and source revisions are explicit. Before
source retrieval, before execution and after execution, GitHub must confirm the
real permitted owner, private non-fork repository, administration access, same
head/base repository and unchanged open PR and branch SHA. Names stay in RAM.
The Docker client always uses the verified local Windows Docker Desktop Linux
named pipe and strips all inherited Docker routing/configuration environment
variables. A profile's current context cannot redirect source transfer.

`product-ci inspect` reports only opaque identities. The initial finite
`node-contracts` command runs the first candidate's existing Node script tests;
it does **not** certify all acceptance gates. Docker-dependent integration,
native Windows/Linux, Lua and CodeQL checks remain required. No GitHub status is
written, budgets are not changed, and registration or launch never means success.

Create the receipt directory with `safe-delete create`. Pass its new absolute
path to the helper, and pass the official Node image by its verified amd64 digest.
The archive comes from GitHub for the fixed SHA, stays in RAM and is transferred
through Docker stdin after removing the identifying top-level directory. Links,
special files, multiple source roots and escaping paths are rejected. Source is
never unpacked on the host or added to the CC tools repository.

Inside the container, Git builds an index from the original archive. The index's
`write-tree` result must equal the fixed commit's tree from GitHub before tests
can run. This supports the existing tracked-file enumeration tests without
creating a replacement commit or claiming that an archive has a commit HEAD.
The single public YAML dependency is pinned to the original lockfile's version
and SHA512, retrieved without authentication and verified before transfer.
`node-retry` selects only failed scripts from the original TAP evidence.
It first binds that log to its original receipt's repo/PR/SHA, failure exit,
SHA256 and byte count. Receipts distinguish all-scripts from failed-subset.

The new finite container has an ownership label and unique name, non-root UID,
`--init`, dropped capabilities, no-new-privileges, a read-only image, bounded
memory/CPU/processes, and isolated tmpfs scratch. It has no network, GPU, host
mount, owner profile, authentication file or Docker socket. The host does not
supply any credential to the test command. This step executes no Cargo; future
Cargo acceptance must use the shared host Cargo gate and distinct target locks.

An exclusive per-candidate lock prevents duplicate execution. A failed cleanup
or uncertain create retains the lock and records the exact new instance name.
If a create command loses its result, only that instance's name/nonce/image may
recover its ID for disposal. Receipt paths are reserved exclusively and updated
through a synced temporary file and rename; reusing an execution directory is
rejected. Normal return and deadline cancellation stop this owned Docker
PID namespace, inspect its stopped state/PID zero and ownership, and remove only
that container and its volumes. Original test logs and their SHA256, source
revision/tree, archive digest, exit and cleanup results remain in the owned
receipt directory. Logs must not be printed into a model or report: they can
contain client identifiers. Receipts deliberately keep all-required-checks
success false. Image ownership and `safe-delete plan`/separate delete/`finish`
remain the invoking agent's responsibility after saving/delivering evidence.

`lifecycle-smoke <owned-dir> <image>` runs real normal-return and context-cancel
fixtures, observes an actual surviving child PID, then uses the same disposal
function as the product execution. It records stopped PID zero and owned-resource
removal. It does not claim recovery after host termination, logout or power loss.

GitHub's fixed archive endpoint is documented at
https://docs.github.com/en/rest/repos/repos#download-a-repository-archive-tar.

## Finite default-setup CodeQL jobs

For a repository without this label, first use
`codeql-bootstrap <repo-id> <new-owned-dir> <Linux-helper-binary>`. It configures
one official ephemeral runner offline, changes only the default-setup routing,
records the setup-validation run ID, and removes the offline container and
registration. It starts no listener and executes no product job. Then serve the
validation run's matrix jobs sequentially with `codeql-job`, before serving the
original candidate. A label reservation alone is never acceptance.

A runner-only update may complete synchronously with no new run ID. Zero means
no validation run was created. If the previous generated workflow cannot be
retried, `codeql-revalidate <repo-id> <new-owned-dir> <Linux-helper-binary>` performs
the documented default-setup restart while the official label is offline. It
records the original configuration, rejects active dynamic runs, immediately
re-enables the same suite/threat/route and checks every original language. Rust
is not sent through the incomplete REST language-write enumeration. During
validation GitHub can temporarily return an empty language list: only a fresh
same-repo/default-SHA generated matrix requesting all original languages on the
dedicated label is admitted. Completion and uploads still need real jobs; a
historical run, missing Rust or hosted job cannot certify recovery. JS/TS legacy
language aliases compare as their canonical `javascript-typescript` coverage.

After the validation's official jobs finish, run `codeql-finalize <repo-id>
<revalidation-dir>` to require successful analysis jobs and the original final
language/suite/threat settings. GitHub's conditional `Adjust Configuration` job
may be skipped; no analysis language may be skipped. Freshness uses the latest
dynamic run ID recorded before mutation, rather than the host's wall clock.
The recorded default source remains fixed; an advancing default branch requires
a new inspected plan and cannot silently replace the validation source.

If the host crashes after persisting its revalidation intent,
`codeql-recover <repo-id> <revalidation-dir>` resumes only
the persisted bootstrap nonce. New bootstraps hold a kernel byte lock and save
their identity in the admission file: recovery refuses a live owner or another
operation's lock. It checks/removes the exact owned container, re-enables the
recorded setup while its offline GitHub label still exists, then deregisters
that exact name and releases admission. Failed re-enablement keeps the offline
registration and admission for another bounded recovery. Legacy empty locks
require an audited ownership reconciliation; this command never clears them.
An ambiguous pending API response is resolved only by a unique newer same-source
generated run with the full dedicated matrix. No registration or job success
is inferred from recovery alone.
The recovery handle explicitly permits Windows delete sharing, so the owned
admission file can be removed while the kernel lock still prevents a competing
recovery. A native Windows test exercises this exact final removal. Crashes in
the earlier offline-only phase have not changed setup; their nonce/verified
container ID must be reconciled before another bootstrap is admitted.

`codeql-route <repo-id> <new-owned-dir>` changes only the already-configured
default setup's runner type/label. It records the previous configuration and
mutation intent first. Languages, queries and threat model must remain equal.
Advanced workflows (the other two candidates) are rejected by this mode.
No hosted budget, org access or original candidate source changes.
This lower-level routing command requires that its dedicated label already
exists. Mutation failures retain only HTTP/exit classification and a stderr
fingerprint; tokens, response bodies and identifying diagnostics are not logged.

`codeql-job <repo-id> <run-id> <new-owned-dir> <Linux-helper-binary>` serves one
official dynamic CodeQL job using a new non-root, read-only, init-enabled
container of the verified credential-free SDK image. It has bounded CPU/RAM,
tmpfs-only storage and no host bind, profile, GPU or socket. The immutable base
is shared; it is neither modified nor removed. Only the newly built Go helper
crosses Docker stdin. Registration uses RAM and the official runner's token
input environment. Configuration output is suppressed. Credentials and runner
diagnostics remain in disposable guest tmpfs. No resident bridge is invoked.

Registration is repo-scoped, has a unique name, no default labels, one dedicated
label, and `--ephemeral --disableupdate`. Both hooks bind the exact repo, run,
attempt and source. A separate loopback-only, nonce-authenticated callback
rechecks GitHub's current source when each hook actually runs. It exposes no
request-selected commands or Cargo operations. Hooks live outside the official
runner application directory. The completed hook verifies actual Git HEAD/tree and absence
of checkout credentials. The host checks the live original candidate before
registration and after execution. Default-branch setup validation is explicitly
separate from candidate evidence. A runner exit never certifies the workflow:
official jobs, logs and CodeQL uploads still require verification. Receipts
retain `all_required_checks_success=false`.

## Fixed native secret-scan workflow

`secret-job <repo-id> <run-id> <new-owned-dir> <Linux-helper> <gitleaks-ELF>`
serves the first candidate's original PR secret gate. Its workflow path and
bytes are fixed by digest. It rejects another repo, fork, PR, source head,
`pull_request_target`, changed executable workflow, or unknown job kind before
registration. The official scanner ELF from the pinned public image has its
own fixed digest; the host verifies it, transfers it through stdin into tmpfs,
and the source workflow verifies that same digest before running it. No Docker
socket or product-source bind is exposed.

A normal PR job has GitHub's merge SHA in `GITHUB_SHA`. This mode binds that
context to the same PR's API merge SHA, separately from the fixed analyzed
source. The reviewed workflow explicitly checks out the original PR head;
the completed hook still requires that actual Git HEAD/tree and removal of
checkout authentication. Dynamic CodeQL never accepts this merge-context
exception. Both modes share queue/live admission and real namespace cleanup.
The source workflow retains its entire PR Git range, policy-file rejection,
read-only repository permissions, redaction, failure exit code and timeouts.
An official completed/success job and original logs must be checked after the
ephemeral runner ends; its exit alone never produces a success status.

## Existing advanced CodeQL workflows

`advanced-job <repo-id> <run-id> <new-owned-dir> <Linux-helper>` supports only
the two original advanced-setup candidates. Default setup must stay disabled.
The existing full `workflow_dispatch` must use that same PR branch and fixed
head, repo and reviewed workflow digest. Original languages, none-build modes,
queries/custom config, changes policy and final gates are preserved. Only the
two runner-label scalar values change in the product workflow. A superseded
automatic policy run is cancelled only when its same fixed head is explicitly
replaced by the original full dispatch; skipped analyses are never counted.

Before registration, the fresh guest receives the fixed GitHub source through
stdin. Every actual file is hashed with Git's filters disabled, compared with
the original API blob IDs, and indexed with the original modes and gitlinks.
Referenced submodule repositories are not fetched. The real commit bytes must
hash to the fixed SHA before installing the shallow detached HEAD. This also
gives policy/aggregation jobs without checkout steps physical source evidence.
An ordinary `git add` is insufficient: it can omit tracked ignored files or
apply attributes. Any archive/blob/tree/commit mismatch stops before a token
or official listener is created. Actions may perform their original checkout;
the completed hook still requires the actual fixed HEAD/tree and no auth.

This mode performs CodeQL's no-build analysis only; Rust and the resident Cargo
adapter are excluded from its PATH. Native Cargo, Windows, Lua and other
acceptance still need their separate gated resources. Normal return and deadline
cancellation stop and inspect the actual namespace before removal and before
releasing per-candidate admission. Any residual remote registration is removed
only by its unique name in the same repo and absence is checked again. Uncertain
creation, cleanup or registration absence retains admission for recovery.

Run `TestCodeQLRealLifecycle` with `CC_PRODUCT_LIFECYCLE_DIR` pointing at a new
safe-delete-owned directory for real normal/cancel process tests, without tokens
or GitHub registration. Preserve those receipts before cleanup.

Also set `CC_PRODUCT_GUEST_BINARY` to the freshly built Linux helper to exercise
the actual guest-to-host admission connection with a fixture whose head advances.
This is a token-free fixture and cannot certify product CodeQL. After uncertain
creation the smoke sequence stops; resolve the recorded instance before another
attempt. See `docs/product-ci-codeql-2648.md` for the saved recovery checkpoint.

Official API and runner contracts:
https://docs.github.com/en/rest/code-scanning/code-scanning#update-a-code-scanning-default-setup-configuration
https://docs.github.com/en/actions/reference/runners/self-hosted-runners

## One audited post-merge CodeQL run (ToDo2757)

`merged-inspect` performs read-only checks for repository 1285649124, PR1002,
merge `41090d19329b81dd1bd076f3336d723823be4b82`, tree
`007b84082008ea9effd5052f63856273aabbfb17`, ordered original parents, original
PR head commit and tree, original PR base SHA, and current main. No other merge or latest-head mode
exists. It checks the original default-setup settings and original four jobs
in run38941323954/workflow331939928/attempt1, and the existing dedicated queue.

After normal source review, CI, merge and official distribution,
`merged-codeql-job <new-owned-dir> <official-Linux-helper>` serves one job of
that exact run. It reuses the original candidate lock, container isolation,
resource limits, live hooks, actual checkout HEAD/tree/authentication proof and
namespace/registration cleanup. It does not change route configuration or
accept an arbitrary job, run, revision or label. The other four original
open-PR candidate entries and their commands are unchanged. Runner exit is
still not official analysis success. Promotion, full verification, integration
and production revision/smoke are separate outstanding acceptance conditions.

Read failures in the merged receipt name their fixed stage (repository, PR,
current main, merge commit, or original PR head commit). Transport errors use only
closed categories; raw stderr, response bodies, and endpoint names never enter
the error. A `not-found` category is not proof that a branch was deleted or
that permissions changed. It is terminal for that read: no fallback ref, API,
identity, or runner is tried. In particular, a successful current-main read
does not override a failed fixed-commit read or authorize a job.

The temporary source branch is used by the unchanged open-PR verifier, but
is not an execution source for this exact merged receipt. Normal merge cleanup
may remove it. The merged path requires the original closed PR's fixed head
and base SHA, both fixed commit trees, exact ordered merge parents, and fixed
current main on every before/after verification. It does not try another ref
after a 404 or accept an unavailable commit. Only the one audited merge can
use this proof; the generic/open-PR modes retain their original branch check.

## Fixed release-source inventory

`release-inspect` is a read-only preparation command for the original audited
merge. It verifies the exact merged receipt before and after reading the eight
fixed source blobs. Both Git blob SHA1 and SHA256 must match. Parsing consumes
copies of the same verified RAM bytes; source names and content never enter
the report or error messages.

The report distinguishes workflow-wide and root-environment secret/token
mentions, explicit inherited secrets, script parsing failures, and literal
Docker/Compose calls. Shell parsing does not expand host environment values
or execute substitutions. Argument values, job names, conditions and source
text are hashes, while public Docker flag names use a closed vocabulary.

This inventory covers literal shell syntax only. It does not resolve Compose
models, prove all called scripts are secret-free, calculate the canonical trust
digest, or authorize execution. `ExecutionAuthorized` remains false. It adds no
runner, mutation endpoint, candidate, ref, label, profile or fallback path.

### Sealed release build input

The release context primitive reconstructs every recursive Git tree object
before accepting its file list, then verifies every archive file against that
tree. It rejects truncation, missing or extra files, duplicate entries, links,
gitlinks, special modes, multiple roots, escaping paths and damaged gzip
trailers. File data stays in RAM; it is never extracted on the host. Empty
tracked directories are retained. Tar permissions are canonicalized to Git's
executable bit, without transferring special permission bits.

Only the fixed release wrapper can select the audited product tree; the eight
audited blob SHA256 values must also occur in the verified snapshot. A future
builder receives a copied, verified archive through stdin, rather than reading
guest checkout paths or trusting guest hashes. Names and bytes have no exported
JSON fields. This change supplies the input boundary only: it adds no CLI,
network call, Docker execution, runner registration, or release eligibility.

`release-source-inspect` connects that boundary to the same official GitHub
gate, for the single original merge only. It verifies the merged receipt,
reconstructs the fixed recursive tree before fetching the fixed tarball, seals
all source bytes and the eight audited blobs, then checks the merged receipt
again. Only after every check succeeds does it publish counts, digests and the
existing limited workflow inventory parsed from those same sealed bytes. No
source files are written to the host. Errors and fmt diagnostics redact names
and content. A partial read, changed main or failed archive check produces no
success receipt. Transport retries retain the existing finite read-only policy.

This command exercises the eventual builder's input reader without starting a
builder, product command, runner, or deployment. It never authorizes execution.
The existing inventory's limits on Compose resolution, token permissions,
transitive secrets and workflow trust semantics still apply.

### Bounded release builder (ToDo2799)

`release_build.go` and `release_build_exec.go` add the build component that the
release orchestration will use later. They add no CLI, no `main.go` wiring, no
candidate, ref, queue entry, runner label or workflow change, and they do not
grant execution: the caller must separately hold the job/source/role/queue/budget
admission and the exclusive phase reservation below.

Input. `prepareAuditedReleaseBuild(sealedReleaseContext)` accepts only the
sealed snapshot of the fixed merge: tree `007b8408...`, canonical input SHA-256
`fa0096ec...`, and the audited Compose and Dockerfile pins (path SHA-256, Git
blob, content SHA-256, Dockerfile size) plus the context directory path SHA-256.
Missing, unknown or modified entries are rejected. The pinned Compose bytes are
parsed again: exactly one service builds, its build mapping has only `context` and
`dockerfile`, and both must resolve to the pinned paths. Only the context
subtree is copied from the verified RAM bytes into a gzip tar (uid/gid 0, file
bytes and executable bit kept). The `sealedReleaseBuildInput` has no exported
field and prints as a fixed placeholder. On the fixed snapshot this yields 301
files, 17 directories, 3,663,675 file bytes and a 849,233 byte gzip context.

Execution. `executeAuditedReleaseBuild(ctx, input, ownedDir)` runs one Kaniko
v1.28.6 container (the maintained upstream fork, pinned by digest) through the
ordinary Docker daemon. The only argv variables are an opaque nonce and the
validated Dockerfile name. Fixed options: `--context=tar://stdin`,
`--no-push --no-push-cache --cache=false`, output to `/kaniko/output.tar`,
`--cap-drop ALL`, `no-new-privileges`, `--init`, root uid 0 inside a
disposable read-write layer, a pids limit, memory with swap equal to memory, and
two small tmpfs mounts. There is no Docker socket, bind, device, privilege,
`--force`, TLS bypass, host PID/IPC/network, published port, credential or host
environment. The Docker client uses an empty config directory inside `ownedDir`.

Because the product Dockerfile pulls a digest-pinned base image and runs apt and
uv, the product profile uses Docker's ordinary outbound bridge network. The
fixture profile (CC-authored Go probe only) uses `--network none`.

Budget. The builder is fixed at 1.5 CPU, 6 GiB, 448 PIDs. With the idle runner
(0.5 CPU, 1 GiB, 64 PIDs) and 1 GiB of host byte transfer the phase is 2 CPU,
8 GiB, 512 PIDs. This component takes no exclusion and never raises a limit. A
container labelled `cc.release-build` left over from an earlier run makes the
phase busy (`phase-busy`) before anything is pulled or created. Whatever the saved
flags say, a release (including by recovery) also requires that no builder of any
nonce exists.
Host RAM holds
the gzip context, one verified copy for stdin, and one exactly sized artifact
(256 MiB maximum; 32 MiB for the fixture), all inside the transfer budget. The
artifact is a complete Docker-archive image (config digest, every listed layer once,
compressed and uncompressed digests checked, each layer a well-formed tar, total
expansion bounded, validation stopped by cancellation). The component never writes it
to the host (only the opt-in fixture test saves its probe-only image into its owned
directory for inspection); the
raw builder log (32 MiB maximum) goes
to a private file in `ownedDir` and only its size and SHA-256 are reported.

Lifecycle. The nonce, container name, tool image digest and limits are written to
`release-build-state.json` in the owned directory before the container is
created. If the create result is lost, only the exact nonce is recovered and the
recovered container is never started. The unconfirmed state is saved before the
request is sent. Only the daemon's own error reply proves that a create did not
happen; otherwise an empty search is not proof of absence, and no clock is used.
Disposal (and any later recovery) takes the name with a second, inert create: it
either succeeds, so a late arrival of the first request can no longer win the name,
or conflicts because the first one exists and is found by its nonce. The container
is never started and is removed. This cannot prove that a request stuck inside the
daemon will never land; what it guarantees is that nothing but a container whose ID
the daemon confirmed is ever started, so a late arrival is an inert "created"
container with no CPU, memory or PIDs. It blocks the next phase (fail closed) until
removed, and the next run's phase check removes exactly such leftovers (never
started, exact name/label/image pattern, no bind mount) while anything that ever ran
keeps the phase busy. After normal exit, failure, cancellation or
timeout the Docker client has drained all I/O before the container is stopped.
Disposal records stop and PID 0, removes the container with its anonymous volumes,
confirms absence by listing, and removes a tool image that this run introduced.
A run succeeds only if the build ran, exited 0 under verified isolation and produced a
validated artifact; otherwise it is `build-incomplete`. Volume names are saved before
the container is removed. The product entry accepts only the input made by
`prepareAuditedReleaseBuild` for the fixed pins, and the fixture profile cannot be
reached from a sealed input. Failures carry a fixed class only: never Docker stderr,
argv, names or bytes.
`releaseBuildReceipt.PhaseMayRelease` must be true before the caller releases
its exclusion. Otherwise `RecoveryRequired` is set, the owned directory must be
kept, and `recoverAuditedReleaseBuild(ctx, ownedDir)` removes exactly the
resources named in the saved reservation.

### Fixed build compatibility preflight

`release-build-preflight <new-safe-delete-owned-dir>` connects the audited
builder to a bounded host-only compatibility check. It takes no repository,
ref, image, job, label, environment or deployment parameter. It does not register
a runner, execute an Actions job, publish a status, load/push the resulting image,
or deploy. `ExecutionAuthorized` remains false: a successful preflight is not an
official workflow or release result.

The command first holds the existing per-product exclusion, also blocking the
older open-PR/CodeQL entry points. Its durable record binds the exact merged
source/tree, owned-directory hash, random nonce and 2 CPU/8 GiB/512 PID phase.
A kernel lock prevents a second live owner or recovery from entering. The lease
nonce identifies this reservation; it is not the builder's separate nonce.
The directory hash binds recovery to the same builder state directory. The
original four CodeQL jobs must be successful at the original attempt. No repo
runner may be registered and no original product runner/container may exist.
The entire fixed source is read and sealed through the normal gate, and the
audited build input is prepared from that RAM snapshot. Current main and idle
conditions are checked again immediately before recording the build intent.
Current source is verified again after the builder returns.

Only `PhaseMayRelease=true` together with `RecoveryRequired=false` permits
removing the durable exclusion. Clean build failures release it; uncertain
cleanup retains it after process exit. `release-build-recover <same-owned-dir>`
takes the same kernel lock and validates the saved source, budget and directory
binding before recovering the recorded builder resources. Recovery does not
rebuild, fetch another source, require main to remain current, or claim build
success. Missing/corrupt builder state fails closed and keeps the lease.
Lease persistence is not an atomic journal. An interrupted intent write or a
crash after intent persistence but before the builder's first state write may
therefore require a bounded technical investigation; recovery never treats a
missing/corrupt record as permission to release the exclusion.

The 55 minute CLI deadline includes the builder's fixed 45 minute cap and source
verification; cleanup retains the component's independent bounded context.
Capture stdout's safe JSON receipt and stderr's fixed error class even when the
command exits nonzero. Keep the owned directory for recovery until the receipt
confirms `LockReleased=true`, and preserve its private log and state evidence
before the usual safe-delete plan/delete/finish procedure. The artifact stays
in RAM and is discarded after its digest/size are rechecked.

Verification. Unit tests cover wrong tree/archive/Compose/Dockerfile/context/
bytes/modes, unexpected Compose shapes, overlimit logs and artifacts, secret-like
build output, weaker isolation, cancellation, deadlines, uncertain creates and
unconfirmed disposal, using an in-memory daemon. The opt-in test
`TestReleaseBuilderRealFixture` (`CC_RELEASE_BUILD_FIXTURE_ROOT`, a fresh
safe-delete owned directory) runs the real daemon with the CC-authored probe:
normal (kernel CPU/memory/PID limits, CapEff 0 and NoNewPrivs read back from the
built image), cancel and timeout after the probe printed a marker proving its child
exists, a lost create response, a refused removal followed by recovery, and the
product profile's isolation values (created and inspected, never started; recorded
as an isolation check, not as a build).

Not established here: whether the product Dockerfile builds with all capabilities
dropped (base image extraction, apt, uv), its artifact size, and the product
build time. Any needed change must be an audited change to the fixed profile;
nothing is relaxed at run time. The fixture results are not a product build.
