# ToDo2757: PR205 post-merge delivery boundary

## Latest observation: 2026-10-19 14:20 JST, installed PR238 preflight succeeded

The same installed PR238 executable completed the fixed-source preflight at
`2026-10-19T05:19:37.4342888Z`: host exit0, guest exit0, 433.783 seconds.
The artifact was 273,854,976 bytes, within the closed 288 MiB cap. Full image
verification succeeded, including config/layer authentication; artifact SHA256
is `16f69e2fc939bfe70ae74c499d3516d64ed8ef9602a488b401c79a9d6c4d6ee1`.
The verification-only preflight neither retained nor published the image.

Fixed source/tree matched before and after, original CodeQL success was
reconfirmed, and the post-run live runner inventory is zero. Host process
absence, PID0, exact owned-container absence, volumes absent, all three
introduced images removed, phase released and no recovery needed were checked.
The raw log is 628,598 bytes, SHA256
`9c4132741240f6356871def590059a28b4d7355ff0d1ae159ec926343da01209`.
Log and final-state copies matched before the owned directory was planned,
normally deleted in a separate call and finished source_absent. Prior failure
logs and source/refusal history remain unchanged.

CC merge checks are still incomplete; the last observation has one success,
one in progress and five queued. Research2939 has a normally accepted queued
Sonnet job, not a completed analysis. Its remaining document/drain/tool semantics
are required before the finite official execution can be activated. This
successful build preflight does not establish official full/backend/release
success, actual drain or existing Prd revision/readiness/smoke. No production
change was made. The original official Rust raw-log refusal remains preserved.

## Historical observation: 2026-10-19 14:14 JST, installed PR238 preflight running

Independent Sonnet review2932 read the frozen four-file diff and full changed
files at `2edadd78ef72f8ce1d1a0a16695daba6d839bd56`, Must0. The original final
response equals its final event; its SHA256 is
`197DA5570C5D56579E6E9ECCD7101DD7568F01937F5DCBDD72B961CC583800B1`.
The supplied diff matches Git's exact base/head diff. The reviewer did not read
the preflight caller or all VM code; the parent checked the unchanged caller.
The reviewer did not rerun tests. Suggestions are retained with the original
response. Closed cap equality is intentional; the next real run must measure
the newly reached hash/copy/image-validation paths rather than assume success.

PR238 exact-head CI ended with eight success, one neutral and one skipped
check, no failures, and zero unresolved review threads. Normal merge is
`291fc8a72ed05ebf6b0de6e62fe305d68c4e7eb4`. Tested and merged module trees
both equal `99f6dd20c729de6c81c79732c6bfdbf6ca1692e4`. Both executables were
built from an archive of this exact merge, with the earlier bounded Go options.
After checking the formal guard, concurrent module tree and inactive helper,
normal distribution updated the standard executable and classified Linux leaf.
The owned build output and standard executable were on the same volume;
File.Replace retained the predecessor in the classified backup leaf.

- Windows SHA256: `78D47DFE0E83A75C98F7484029342CE1FA3BA1902F73CD1A3F40210EFEDACABE`.
- Linux SHA256: `8889961C44568ED0585DF126AE9D1C8CF46B443BADA7BA4EB7AC2FB66F9277ED`.
- Identical standard and PR238 receipt SHA256:
  `E0C1AC9562715E4352ACC4607CE62FD16805C14F7465030B96264B9034FA7F57`.

The installed executable started the same fixed-source preflight at
`2026-10-19T05:12:23.6513604Z`, host PID61884, owned directory
`8b948215f11dd8a2f3bd7f46e7290445`. Its process path and exact start ticks match.
Builder nonce is `b8132e9ab5601b7fa8c4498d233cf3f0`; outer inspection confirms
running, no OOM, 1.5CPU/6GiB/448PIDs, cap-drop ALL, NNP, no devices or host binds.
Do not start another preflight while it or its phase lease is active. Outcome,
artifact validation, final cleanup and merge-check results are still pending.

A fixed-blob YAML projection resolves the earlier unknown job-condition tokens.
The declared boolean input concerns drain confirmation, is required and defaults
false. Both verification jobs require deployment enabled, main, and either push
or that confirmation. Promotion additionally requires both verification results
to be success. The preconditions step maps push to the string true, otherwise
to the input; this is not proof of an actual drain. All projected condition
tokens are understood, raw source was not saved, and main matched before/after.
This does not activate a workflow or resolve the remaining document/tool gaps.
Finite research2939 owns those remaining document semantics and real SDK versions;
it must preserve the earlier2878 result and must not run product jobs or deploy.

PR229 merge checks ended with three CodeQL failures, four successes and one skip.
All three failed jobs stopped at checkout; their official raw logs confirm
bridge HTTP403 before analysis. Logs and numeric fingerprints are retained.
The underlying bridge decision is not proven by the HTTP status alone. These
historical failures are not rewritten as current-source successes or rerun on
an obsolete main by relaxing admission. Current PR238 CI is separate evidence.

The distribution-build directory was separately planned, normally deleted and
finished source_absent. The condition-projection executable was likewise removed.
Official full/backend/preconditions/release, actual drain, existing Prd runtime
revision/readiness/smoke and the original Rust official-log refusal remain open.

## Historical observation: 2026-10-19 13:23 JST, output limit diagnosed

The installed PR229 preflight ended at `2026-10-19T04:21:26.4362938Z`, host
exit1 and guest exit124. Its new numeric marker records an actual output of
273,962,496 bytes against the 268,435,456-byte cap. This confirms the output
check as the source of this failure; the prior run's artifact size remains
unobserved. The original log is 640433 bytes, SHA256
`7F040C1E38C078ECEDDF9A152C58665FDE201ADDAE2EA7CA81D03B951171409A`.
Its bytes and final build state were preserved with matching copy hashes.

The fixed source was verified before and after the build, original CodeQL
successes remained verified, and no runner was registered. PID0, container and
volume absence, removal of introduced images, phase release and no recovery
needed were confirmed. The preflight directory was planned, normally deleted
in a separate call and finished as source_absent.

The next CC-only fix sets a closed 288 MiB product output cap. The fixture cap
stays at 96 MiB and the phase stays at 2CPU/8GiB/512PIDs. Artifact parsing,
config/layer hashes, cancellation, source identity and isolation remain required.
The budget regression now also counts an artifact's worth of retained small
metadata, within the same 1 GiB host-transfer allocation. This source is not yet
reviewed, merged or distributed, and must not run a product job.

A fresh read of the fixed source's existing production environment metadata
confirmed the same environment ID, zero protection rules/required reviewers and
no deployment branch policy. No secrets or settings were read or changed.
This resolves that specific research2878 gap, not its document/tool/condition
semantics gaps. A first condition projection missed folded YAML text; its empty
result was retained and not accepted. A second projection preserves operators
but still contains opaque references, so it is not execution authorization.

Official full/backend/preconditions/release, actual drain and existing Prd
revision/readiness/smoke remain unfinished. The CC merge checks are separate.

## Historical observation: 2026-10-19 13:13 JST, installed PR229 preflight running

Independent Sonnet review2879 read the complete frozen diff at
`a0548183b6f515bbcda2d3072e2fb591791072a7`, Must0. Its final event equals its
saved response, and the supplied diff matches Git's exact base/head diff. The
reviewer's Git was unavailable; its document reading covered the changed
section rather than the full history. The parent verified the actual Git HEAD.
Exact-head CI ended with eight success, one neutral and one skipped check;
unresolved review threads were zero. Normal PR229 merge is
`1123681cae4da1a1c01f718563ed4fa656203cba`. Tested and merged module trees both
equal `5449d42515a70f401e9ef13fd2c3e98f1388c1de`.

Windows and Linux executables were built from an archive of that exact merge,
with the same bounded Go build options used earlier. Normal distribution used
the classified standard executable, Linux leaf and receipt leaves. The build
source and destination are on the same volume; File.Replace moved the owned
build output directly into the standard executable and retained its predecessor
as backup. An unused proposed staging leaf was unverified and was not read or
written. No alternate executable was used for product execution.

- Windows SHA256: `5CAD0057BBAE2A5BA2097401E80BD2E960F835E54FBF2038B977EF811BB5DA04`.
- Linux SHA256: `3CD7695418C6DAE0CFA37CFCE6DF4EB8C789F6395443D2909C4638865614E0DC`.

The exact installed executable started the same fixed-source preflight, host
PID65720/start `2026-10-19T04:12:53.3118224Z`, owned directory
`567048e51abd16ec80fc6be537ace40a`, builder nonce
`4480520a31b01c4d85bc7b31116a9b94`. Do not start another while this invocation
or its lease is active. Merge checks remain separate from PR CI and are pending
in the last observation. Original source/log/refusal records remain preserved.

Review2879's suggestions clarify the earlier exit124: its origin in the output
check was inferred, not proven. The new numeric marker can distinguish an
oversized output from zero bytes, but zero still combines absent and empty
output. There is no artifact-cap, source, admission or success-condition change.
The real six-case fixture already executes the guest block; adding an ordinary
CI string assertion would not replace that behavioral evidence. Additional SHA
assertions in the fixture are optional; this change does not alter SHA logic.

Research2878's final Sonnet response was received and matched its final event.
It reads a verified sanitized structural model; some conditions, versions and
document semantics remain opaque. It is not full semantic approval, live
environment approval, an execution profile or a product CI/deployment result.
Resolve those explicit gaps before activating the remaining official workflows.

The two new distribution-build temporary directories were separately planned,
normally deleted and finished as source_absent. The active preflight directory
and its private streams remain retained. Official full/backend/preconditions/
release, actual drain, existing Prd revision/readiness/smoke are still unfinished.

## Historical observation: 2026-10-19, initial compatibility build failed

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

## 2026-10-19: real VM build returned exit124; output-check origin inferred

The PR226 installed preflight finished with host exit1 and Docker/guest exit124.
It verified the fixed product source before and after execution and the original
four CodeQL successes. The original ownership error did not recur; the log has
one package-install success and no matched image-build error, OOM or disk-full
signature. Exit124 was inferred to come from the guest output-checking block,
but a builder-returned exit124 was not excluded by the available evidence. It
still cannot distinguish
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

## Latest observation: 2026-10-20, official fixed-source tests actually started

PR238 merge291fc8a now has eight terminal checks: seven success and one
package-tools skip, no failure/cancellation/pending. This resolves its old merge
check wait; historical PR205/229 failures remain unchanged. Repair repository
ID1406818713 is now public with the same allowed owner/admin/nonfork identity.
The separately saved owner instruction for task2988 explains that visibility
change. The fixed product remains private and its main still equals41090.
No visibility, budget, admission or source setting was changed in this task.

Research2939's actual Sonnet response was received from completed job
20261020-145549-todo-2939, with original bytes/hash preserved privately. It
confirms five fixed blobs, pre/post identity and tool versions. Its remaining
opaque condition/approval/customer meanings remain unresolved. Its version
container was removed/absent, but its PID0 and limit readback failed; do not
upgrade that receipt to fully verified cleanup evidence.

Both original verification workflows have workflow_dispatch with no inputs.
Their exact blobs and SHA256 match the frozen source. Official workflow IDs
325305202 and312434359 were matched to the fixed tree paths in RAM. Neither
had an existing run on41090. Fixed PR/head/merge/tree/ordered parents/main were
rechecked before dispatch through the same distributed GitHub gate.

Normal dispatch returned204 for both workflows. Actual run39382032899
(full verification), job118493090193, and run39382036786 (backend integration),
job118493102572, both attempt1, workflow_dispatch, head41090, have started
steps on the original ubuntu-latest official runners. Thus the previous
budget/steps0 condition does not currently prevent these two test workflows.
This task did not increase the budget or register any substitute runner.
Do not dispatch duplicates; read these exact run/job IDs through to completion.

This is actual test execution, not success. It is separate from promotion
run38941324877 and does not satisfy its needs results or prove actual drain.
Promotion/preconditions/release/Prd revision/readiness/smoke remain unfinished.
No product source, trust digest, label, deployment or original refusal changed.
Private evidence: todo2757-official-dispatch-inspection.private.json,
todo2757-official-workflow-ids.private.json,
todo2757-official-dispatches-20261020.private.json and
todo2757-official-test-runs.private.json in the existing ignored evidence area.
