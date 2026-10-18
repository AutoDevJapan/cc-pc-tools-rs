# ToDo2757: PR205 post-merge delivery boundary

## Latest observation: 2026-10-19, actual compatibility build failed

The older sections below are retained history. Normal distribution through
ToDo2829 is complete; do not reinstall or return to the original-record wait.
Current installed Windows and Linux hashes both match the exact PR205 build
hashes below and both standard receipts. The two receipts have identical bytes
(SHA256 `976DD58F1145756422A5A613235755727CB17C92995A656D0C9CD61066F8DC40`).
This direct comparison resolves the previously recorded current-hash discrepancy;
it does not establish why the earlier observation differed. The current guard
classifies those four exact executable/receipt leaves as verified, non-client
resources. A separately supplied receipt elsewhere remains protected and was
not read. Parent directories/configuration are not generally authorized.

Live repair-source metadata confirmed the allowed owner, private/admin/nonfork,
and PR205 closed/merged with the exact tested head and merge recorded below.
The installed preflight was then actually executed. Its only argument after
`release-build-preflight` is a fresh safe-delete owned directory. On Windows use
its native absolute path: the current `filepath.Clean(dir) == dir` validation
rejects forward slashes before acquiring the lease or reading product source.
The first such input failure and its safe receipt remain preserved separately.

The native-path execution verified the fixed product merge/tree before and
after the build, the original four CodeQL successes at their fixed attempt,
zero registered runners, and zero original product containers. It started the
audited builder with the sealed context (849233 bytes, 301 files, 17 directories).
The actual builder exited 1, without OOM, while restoring base-image filesystem
ownership: `chown` returned `operation not permitted`. No artifact was produced.
The private original log is 125 bytes, SHA256
`5DF944EF820504F8DF05A65A82F0879D6B85C3C8ECE98ACD1A7629475C24F984`;
its text and paths were not displayed. The host CLI exit code was not captured;
the builder exit code comes from Docker's inspected state, not inference.

The receipt confirms outer isolation, 1.5 CPU/6 GiB/448 PIDs for the builder
within the 2 CPU/8 GiB/512 PID phase, PID0, container and volumes absent,
introduced tool image removed, and the lease released without recovery needed.
The host process and lease are also absent. The private raw log and build state
were copied with matching hashes before safe-delete plan, separate normal
deletion, and finish `source_absent` for owner
`85e37d22ae43d68222c3ffec0e7c3e57`.

The upstream pinned builder's [security documentation](https://github.com/osscontainertools/kaniko/blob/v1.28.6/README.md#security)
states that unpack and RUN permissions depend on the base and instructions.
The previous scratch fixture therefore did not establish real-base compatibility.
Do not add capabilities, ignore ownership errors, alter the sealed product input,
or treat this build failure as official CI success.

Finite implementation ToDo2848 now owns the CC builder compatibility repair,
real ownership/isolation fixtures, independent review, normal CI/merge, and
formal Windows/Linux distribution. Its saved specification preserves the outer
cap-drop ALL/NNP, resource, seal and cleanup constraints. It must not run product
jobs or modify product code. Parent2757 resumes the same installed preflight
after that actual distribution; registration alone is not the dependency's
completion. The first add attempt rejected a provider-prefixed model argument
under the CLI's model syntax; using its documented short model form succeeded.

Official full/backend/preconditions/release, actual drain, existing Prd revision
and probes remain unfinished. Historical CC merge failures/cancellations and
the original Rust official-log refusal remain preserved. Neither task2752 nor
the original-record recovery is marked complete.

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

## 2026-10-19: PR226 distribution accepted, real preflight running

The later builder repair2848 is actually complete. Its return note is preserved
in the ignored evidence area; the old PR205 chown failure and its 125-byte raw
log remain unchanged. This section supersedes the dependency wait above.

The formal classifier accepted exactly the current Windows executable, Linux
helper and two receipt leaves as verified non-client resources. The standard
receipt and PR226 receipt have identical SHA256
`92DD05E33D941200A908F3F4D35894B50A49F8476B5DDDAF05BDE48259CB1C36`.
The installed files match their actual recorded hashes:

- Windows: `41B11CF2EC78C97A276D593F0C979CA0ED5B5790EF25400C3C7C08CFEDF39700`.
- Linux: `1F1BD8318F0F5F89279B31F43BA864BFDC1EE8747A654B1A3380920D010FDD3C`.
- Tested head: `cdfb9b97bf6c8893bc4c615503a98a3c2d787a33`.
- Normal merge: `7e68b65f32504512930af02ae0f19fbfe16fb42e`.
- Module tree: `2c591717f0f8f4c5794177533b7120d0a2693361`.

A live API read independently confirmed the CC repository's allowed owner,
private/admin/nonfork identity, PR226 closed/merged, exact head/merge and same
head/base repository. The actual merge has eight terminal checks: seven success
and package-tools skipped, zero failure/cancellation. These CC checks do not
establish product CI success. Original PR205 merge failures remain history.

The exact installed Windows executable started the same fixed product
`release-build-preflight` in a new safe-delete owned directory, ID
`5d1cfaaf1fe627565ab8ccc05a5b24bf`. Host PID42708 and its process start time,
stdout/stderr and eventual host exit record are in the private evidence area.
Do not start another preflight while it or its original phase lease is active.
The current builder nonce is `e0c2676e88400ec2d2e77cc2afab6ba1`.
Outer inspection confirms running, no OOM, 1.5CPU/6GiB/448PIDs, cap-drop ALL
and no-new-privileges. Outcome/artifact/cleanup are still pending at this entry.

Finite read-only research2878 was registered to resolve the remaining workflow
permissions, exact Compose operations, transitive setup requirements, drain and
trust semantics. It cannot edit parent source or run product jobs. Its saved
spec preserves the fixed source/API/RAM-only and secret boundaries. This is
necessary analysis, not another builder repair or a product CI result.

## 2026-10-19: real VM build reached artifact validation but failed

The PR226 installed preflight finished with host exit1 and Docker/guest exit124.
It verified the fixed product source before and after execution and the original
four CodeQL successes. The original ownership error did not recur; the log has
one package-install success and no matched image-build error, OOM or disk-full
signature. Exit124 from the guest output-checking block still cannot distinguish
missing/empty output from output larger than the 256MiB cap: its measured size
was cleared before the only result was emitted. The actual artifact size is
unobserved. This is neither a timeout diagnosis nor a product CI success.

The raw log is preserved, 474068 bytes, SHA256
`C85392720F5290E07D0BF1C73F7A58575673237403C5196D1E7DCA15FCC37061`.
The complete host stdout, host exit, final build state, and safe classifications
are stored separately in the private evidence area. Container PID0, removal,
container/volume absence, all three introduced images removed, phase release
and no recovery required are true. The raw log/state copies matched their hashes;
owned directory `5d1cfaaf1fe627565ab8ccc05a5b24bf` was planned, normally deleted
in a separate call and finished as source_absent.

The next CC-only change records `artifact bytes=<number> limit=<number>` before
the failed result clears the size. It changes no product input, admission,
resource/output cap, success decision, artifact authentication or lifecycle.
The real shell fixture executes the actual guest output block with CC-authored
files: absent, empty, over cap, exactly at cap, below cap and failed build all
passed. Rejected files are not copied; valid copies match their input. Fixtures
use an existing image with pull disabled, named containers, network none,
cap-drop ALL, NNP, 0.25CPU/64MiB/32PIDs and verify container absence.
Related Go race tests and vet passed before the final fixture cleanup tightening;
the final six-case real fixture also passed. Exact-head CI/review/distribution
and the new installed product preflight are still required.

Research2878 is running. Its eventual semantic readback and the remaining
finite official CI/release/drain/Prd work are not represented by this diagnostic
change. Do not raise the artifact cap based on an unmeasured guess.
