# Fixed Guard reader software capture

This producer builds the reviewed Guard refresh reader once on a GitHub-hosted
Windows runner and captures the evidence for that single build. It accepts no
source path, revision, payload hash, destination or authorization argument.

The reader is the Go module `tools/guard-refresh-software-reader`. Generation 0
is a rehearsal: it fixes the module as it exists on main
`95c1004e0017cb9d8d3b16f93daa72706eaccca1`, repository tree
`047674bc84f483d12702ebd0d3332f45e2984390` and module tree
`4f188fbc69396af166f06a74af03df9d502afc3f`. It exercises the hosted build,
the evidence files, the attestation and the acceptance steps below before the
final reader source exists. A later generation changes only `readerSource`,
`readerTree`, `readerModuleTree` and `readerInputCount` in
`main_windows.go`, the checkout `ref` and the three source literals of the
publication record in the workflow, and this paragraph.

All 111 tracked files of the module are compared with their Git blobs and
archive content before and after the build. The selected Go package graph is
captured separately. Besides the standard library, only third-party modules
whose zip and `go.mod` checksums are locked by the module's own `go.sum` are
accepted: the lock is read from the inventoried file, no replacement is
allowed, and the module list embedded in the linked executable must equal the
captured graph, version by version and checksum by checksum. Source inventory
and compiler/toolchain observations are distinct evidence.

The dedicated workflow validates this helper on Windows for pull requests that
change its source. A single manual dispatch on the default branch builds the
reader once. Pull requests do not publish a distribution. The workflow uses
the hosted Go toolchain and never invokes the PC's Go, Git, installed reader
or updater. The module graph is fetched from the Go module proxy and checked
against the committed `go.sum`; the build does not modify it.

The capture binds repository, workflow revision, run, attempt, check-run and
the authenticated unique in-progress job ID. The output must be an AMD64 PE
with matching Go module and clean source revision. The same open output is
rechecked before an exclusive export; source proof and capture metadata are
written without overwriting earlier files. The workflow attests the
executable, source proof and capture metadata, then retains those exact three
files for seven days. The artifact ID and archive digest come from the upload
action and must later be reconciled independently with the authenticated run
and job. A failed capture prints the fixed failure name and the source line of the
failing check and, for a failed child command, a bounded printable tail of
its error stream; no request, response body or credential is printed.

`data_access_granted`, `update_performed`, `send_allowed` and
`source_quiescence_proved` remain false. Source observations and artifact
signatures do not authorize installation, Task reads, source/media
registration, owner approval, question submission or any update.

## Accepting the artifact on the PC

No installed program can verify the first reader executable. Acceptance
therefore compares independent server facts with the downloaded bytes and
executes nothing:

1. The run is a `workflow_dispatch` on `main` of this repository, attempt 1;
   its job `Guard reader software artifact` concluded `success`, and the
   job's `head_sha` equals `producer_revision` of the publication record.
2. The run holds one non-expired artifact named
   `guard-reader-software-<run>-<attempt>-<job>`; its server size and digest
   equal the downloaded archive.
3. The archive holds exactly `guard-refresh-software-reader.exe`,
   `capture.json` and `source-proof.json`. Their SHA-256 values equal the
   publication record in the job summary, `capture.json` repeats the source,
   tree, module tree and producer revision of that record, and the source
   commit is the reviewed commit the caller expects.
4. An attestation exists for the digest of each of the three files and its
   subjects name them. This is a data check; the Sigstore signature is not
   verified by this producer. The acceptance record must say whether another
   verified tool checked it.

The accepted executable is then identified by its SHA-256 together with the
run, job, artifact and attestation identifiers. Acceptance grants no update,
data access, send or source-quiescence authority; the reader still validates
the signed artifacts it is given, the current target identity and ACL, and the
same-parent stage and backup before any replacement.

## Hosts

The producer runs only on the hosted runner and has no host entry of its own.
The executable it publishes is a command line program that agents run on this
PC. Accepting that use is separate from accepting the artifact above: the
operations chosen as representative must run from both Codex and Claude Code
against the same executable digest and give equivalent results. Registration,
configuration, source, tests and a run on one host do not complete it, and
neither host's result replaces the other's.

## Boundaries

The tests cover fixed environment binding, the input graph and its locked
third-party modules, the `go.sum` lock and the binary dependency list,
archive and blob consistency, namespace changes, exclusive receipt creation,
bounded capture, authenticated job cardinality, cancellation, PE bounds and
authority separation. Static source preparation is not a test result. Record
actual compile, tests, signed output and installed runtime acceptance
separately.

Before/after observations are not a universal immutable-toolchain guarantee.
The fixed hosted workflow and its authenticated execution remain the trust
boundary. Synchronous filesystem I/O is not claimed to be interruptible by a
context alone. Incomplete hosted files remain unsuccessful and are reclaimed
with the hosted job; no public output claims a successful installed update.

Attestation subjects and the bundle are supplied by the pinned
[GitHub attestation action](https://github.com/actions/attest/blob/1e69f48acb82d1966a394da916b4c1698aa569d6/action.yml).
The workflow revision and run fields use the
[documented GitHub contexts](https://docs.github.com/en/actions/reference/workflows-and-actions/contexts).
