# Fixed Task runner software capture

This producer prepares the reviewed Task metadata command for the existing
Windows installation. It accepts no source path, revision, payload hash,
destination or authorization argument.

The application source is fixed at commit
`ea56c94fc2820dc349316d15bea38b7f1ae85595`,
repository tree `f9c9fce316d22f86517b7d766b7056ea0516fa29` and
module tree `d0d012463d11faec2b57efffc80e8c13366dd3fd`.
All 185 tracked files in `unattended-runner/` are compared with their
Git blobs and archive content before and after the build. The selected Go
package graph is captured separately; only the original main module and the
standard library are accepted. Source inventory and compiler/toolchain
observations are distinct evidence.

The dedicated workflow validates this helper on Windows for pull requests
that change its source. A single manual dispatch on the default branch
captures the application once. Pull requests do not publish a distribution.
The workflow uses the hosted Go toolchain and never invokes the PC's Go, Git,
installed runner or updater.

The capture binds repository, workflow revision, run, attempt, check-run and
the authenticated unique in-progress job ID. The output must be an AMD64 PE
with matching Go module and clean source revision. The same open output is
rechecked before an exclusive export; source proof and capture metadata are
written without overwriting earlier files. The workflow attests the executable,
source proof and capture metadata, then retains those exact three files for
seven days. The artifact ID and archive digest come from the upload action and
must later be reconciled independently with the authenticated run and job.

`data_access_granted`, `update_performed`, `send_allowed` and
`source_quiescence_proved` remain false. Source observations and
artifact signatures do not authorize installation, Task reads, source/media
registration, owner approval or question submission.

A later fixed updater must independently validate the actual signed artifact,
current old executable identity and ACL, and same-parent stage/backup before
replacement. This producer has no installed-target reader or write operation.
It does not relax resource classification or substitute an old artifact
for this generation.

The tests cover fixed environment binding, input graph boundaries, archive and
blob consistency, namespace changes, exclusive receipt creation, bounded
capture, authenticated job cardinality, cancellation, PE bounds and authority
separation. Static source preparation is not a test result. Record actual
compile, tests, signed output and installed runtime acceptance separately.

Before/after observations are not a universal immutable-toolchain guarantee.
The fixed hosted workflow and its authenticated execution remain the trust
boundary. Synchronous filesystem I/O is not claimed to be interruptible by a
context alone. Incomplete hosted files remain unsuccessful and are reclaimed
with the hosted job; no public output claims a successful installed update.

Attestation subjects and the bundle are supplied by the pinned
[GitHub attestation action](https://github.com/actions/attest/blob/1e69f48acb82d1966a394da916b4c1698aa569d6/action.yml).
The workflow revision and run fields use the
[documented GitHub contexts](https://docs.github.com/en/actions/reference/workflows-and-actions/contexts).
