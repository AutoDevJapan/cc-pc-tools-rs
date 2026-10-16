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
