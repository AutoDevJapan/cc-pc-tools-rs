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
