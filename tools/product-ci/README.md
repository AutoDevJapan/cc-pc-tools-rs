# Fixed candidate product CI

This Go module is independent of `pc-ci` and `linux-runner-bridge`. It neither
changes their admission policy nor registers old product directories. Four
numeric repository IDs, PR numbers and source revisions are explicit. Before
source retrieval, before execution and after execution, GitHub must confirm the
real permitted owner, private non-fork repository, administration access, same
head/base repository and unchanged open PR and branch SHA. Names stay in RAM.

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

The new finite container has an ownership label and unique name, non-root UID,
`--init`, dropped capabilities, no-new-privileges, a read-only image, bounded
memory/CPU/processes, and isolated tmpfs scratch. It has no network, GPU, host
mount, owner profile, authentication file or Docker socket. The host does not
supply any credential to the test command. This step executes no Cargo; future
Cargo acceptance must use the shared host Cargo gate and distinct target locks.

An exclusive per-candidate lock prevents duplicate execution. A failed cleanup
retains the lock. Normal return and deadline cancellation stop this owned Docker
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
