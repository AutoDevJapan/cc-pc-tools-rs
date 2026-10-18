# ToDo2757: PR205 post-merge delivery boundary

PR205 was normally merged after independent review and successful exact-head CI.
Formal distribution and product execution have not occurred.

## Accepted CC source

- Tested/reviewed head: `84f32ec498c627251721dfcf63e66d6f99cd8425`.
- Normal merge: `857c1e67b1a5f7b8c9467208ca5e46ed67901478`.
- Identical tested and merged module tree:
  `6df5158ea6228d659a1756373edf6cb9f0807e85`.
- Independent Sonnet review2813 covered the initial seven-file change at
  `ae8ad820c96b633a051563cbabf5d696cd661769`, Must0, complete diff read.
- Independent Sonnet review2826 covered the additional four-file change,
  final integration diff and final source at the tested head, Must0,
  complete diff read. Original response SHA256:
  `B1CBDC381922A2C367EA5DD05C4524A17046639343631607B6CE26B3230EF5A8`.
  Its actual agent event equals the saved final response, and the executed
  prompt includes the frozen final packet.
- Exact-head CI: ten terminal checks, eight success, one neutral, one skipped,
  zero failure. Runs: helper39126567222, CodeQL39126567250,
  cc-jobs39126567306. These are CC checks, not the product's official release.
- Earlier helper run39126345781 failed before the new runner-contract step
  could start, because the old PR checkout lacked a directory newly required
  by formal main. Normal integration of main0344d1b retained that directory
  and its tests. The above final source passed both helper platforms.
- Initial post-merge observation: seven checks queued. Their final results
  remain to be read; the PR result is not substituted for a merge-run result.

## Preserved build, no distribution

Both executables were built from a Git archive of the exact normal merge,
not from a mutable guest or working-tree copy. Build options were
`go build -p=2 -trimpath`, amd64, CGO disabled, GOMAXPROCS2.

- Windows SHA256:
  `AEA7A64A6BB98A1284416589BA1DE1FA11A758059AD4B010BA9A3E0CA31041FC`.
- Linux SHA256:
  `02F2AA4B7BA01BEA298EDCC5E6E35D946C1A2CEAD2723CBCC9EC24F5C03AC0A3`.

Copies and their hashes were preserved in the assigned worktree's private
ignored evidence area. They are storage copies only and must not be executed
as an alternative to formal distribution. The owned build directory
77f6625d82e895b1a820a648590cf20b was planned, normally deleted in a separate
call, and finished as source_absent after the copies matched their hashes.

## Actual technical stop

Before writing the standard distribution destination, the formal protection
classifier returned classification_unverified=true, client_resource=true,
original_registry_recovered=false. No distribution write, binary replacement,
product preflight execution, container or runner registration followed that
result. A prior installed-binary hash read occurred before this classification;
it is not represented as a no-read operation.

The existing original-record recovery is ToDo2467 and remains unfinished.
This task does not modify that record set, classifier, exclusions, registry,
manifest, queue, old product folders or other tasks' state. API ownership of
CC source and a successful build do not resolve an unknown physical-path
classification. Do not install or execute the stored build from another path
as a workaround.

## Resume

After the existing protection recovery completes, re-read its actual result,
validate the exact same destination through the same classifier, and check for
concurrent source/distribution changes. Confirm merge-run CI, distribute the
reviewed module through the normal path, then run the fixed product build
compatibility preflight. Record actual build/artifact/kernel/cleanup results.

The product source remains fixed at merge41090/tree007b. Its original four
CodeQL jobs succeeded earlier. Product full/backend/preconditions/release,
actual drain, existing Prd revision/readiness/smoke remain unfinished. The
original official Rust job raw-log escape refusal remains a missing artifact;
no alternative log path is used. Neither CC merge nor this build completes the
product or any parent task.

## 2026-10-19 resume check

The exact merge commit `857c1e67b1a5f7b8c9467208ca5e46ed67901478` has eight
completed post-merge checks: four succeeded, two failed, and two were
cancelled. No check remains running. The failures are not treated as success;
their causes still need to be established and corrected. The structured
receipt, including check IDs, is in the ignored private evidence area.

ToDo2829 is complete. Its saved receipt says the normal distribution was
applied, the Windows and Linux builds were verified, and the receipt matched.
The installed Windows executable matches the PR205 build hash. Running the
formal classifier on that exact executable returned `client_resource=false`
and `classification_unverified=false`; `original_registry_recovered` remains
false, so the original records are not treated as restored.

The two failed checks are not source-test results. The ToDo2829 note identifies
an outdated installed helper rejection on Windows and an HTTP 403 before
checkout on the Go job; the implementation checks did not start. Both failures
still need a current-source, fixed-scope recheck and cause resolution. No
product preflight, container, runner, or production change was made during
this resume. ToDo2467 remains unfinished; preserve its scope and records.

The current-source receipts and the installed Linux helper agree on its
current SHA, but that SHA differs from the earlier expected value recorded
for the PR205 build. The source commit and product module tree still match the
fixed PR205 merge. Treat the helper-hash discrepancy as unresolved; the
private saved build copy is not an execution substitute. The compatibility
preflight has not started. A read-only shared subtask (2846) is deriving the
supported invocation from the installed tool's source and documentation; it
has not run the helper or any product job. Do not rerun the historical checks
on the old merge source.
