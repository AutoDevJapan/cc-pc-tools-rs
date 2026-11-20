# Fixed guard capture attestation and payload check

This directory's guard capture tests check evidence from one explicitly issued
guard bootstrap run. They do not install, replace, start or admit software.
The normal runtime verifier remains unchanged.

## State and sequence

The data-only tuple is **issued** for formal run
[44726482600, attempt 1](https://github.com/AutoDevJapan/cc-pc-tools-rs/actions/runs/44726482600),
job 136255100998. The fixed guard source M is
c1adc362778aae5270ceacea1214b9d17940589e, root tree
2137e3ff923aa680041d82f642d881305efd0e98, with the repair module tree
4a9e710eadbdb78e3842bb88361d9b5ce68b238e, contract module tree
7278e7e373d0c65ee7446ad54223e85b3fab565f and 253 selected inputs.
The actual producer D is 09a0de57c493e7c96a1fb1133068d3e2a8246806,
root tree fefabdb394fd97ad779df1a59662c7fc798f4e4e. M and D are separate
revisions and have different root trees; their two selected module trees agree.

The one formal capture completed successfully and both artifacts were received.
The data pin has Issued=true and the actual opt-in is enabled. Neither fact is
cryptographic acceptance. The first actual verification at candidate
4ce180879ecf7211ccb0e039b80ab166080bead6, verifier run 44729013923,
failed at pe_signature, with all checked flags false and controls=0.
The later d7fd3b03 verifier run 44734224017 retained a verified-unbound PE
bundle while still failing bundle_digest. Its 11231-byte API representation
differs from the retained seed only in the direct verificationMaterial member
order. This is a new byte observation; it does not retroactively establish the
cause of the earlier runs. The original seed and its data pin remain unchanged.
There has been no installed update or acceptance from either host.

The sequence separates preparation, factual issuance and actual verification:

1. Review the three new files and run the offline controls plus the old public
   bundle's explicitly labelled cryptographic rehearsal in the hosted verifier.
2. Merge the logic before the single formal bootstrap dispatch. Keep
   repair-guard and tools/civ6-ci-contract unchanged from the source pin through
   that dispatch. The producer checks both checkouts' module trees.
3. Within hours of capture, issue only the data in guard_capture_pin_test.go,
   binding the actual run, attempt, job, check run, artifact, times, payload
   lengths/digests, bundle and signed log index. No environment variable or
   command-line parameter can replace that tuple.
4. Enable GUARD_CAPTURE_ATTESTATION_ACTUAL=1 with that issuance. Obtain the
   first actual acceptance and retain the payload before the artifact's one-day
   retention expires. No second formal build is implied by a missing result.

An explicit opt-in with an unissued or malformed pin fails. Without the opt-in
the actual test is skipped, and that skip is not acceptance.

## Evidence chain

- The authenticated Actions API must identify a successful main
  workflow_dispatch run at the producer revision, the exact successful job in
  that run attempt, its check run and all five capture/attest/upload/record
  steps in order. A job name alone does not identify the job.
- The artifact's ID, derived name, run, head, repository, bytes, digest and
  timestamps must match the pin. The job log has exactly one canonical
  guard_gha_publication_v1 record with the same tuple.
- API requests carry GH_TOKEN only to the fixed api.github.com repository.
  Redirects are never automatic. Downloads use a separate, headerless client;
  the artifact/log destination must be an HTTPS Actions object host. The source
  archive must come from the fixed repository at codeload.github.com. Each
  response and expanded archive has a byte bound and a whole-operation deadline.
  No failed request switches credentials, endpoint or authentication mode.
- The zip is hashed in memory before parsing. It contains exactly guard.exe,
  source-proof.json and capture.json, with no duplicate, path, directory,
  symlink or encrypted member. Their lengths and digests must match the pin.
  The same buffers are parsed, inspected and preserved without executing them.
- Listings may embed a bundle or provide its bounded tokenless object URL.
  Both subject-digest listings must select exactly one cryptographically valid
  bundle. Each listing buffer is verified as received. Both compact selections
  must bind to exactly the pinned retained-seed bytes through the limited
  representation rule below; their received bytes are retained separately.
  The TUF public-good root and existing sigstore-go verifier check the
  certificate chain/EKU, SCT, DSSE signature, subject digest, transparency-log
  promise, inclusion proof/checkpoint and observer timestamp. This check
  explicitly requires all Rekor v1 proof components to be present.
- Identity fixes SAN, OIDC issuer, workflow URI/digest, producer revision,
  repository/owner IDs and URIs, public visibility, main ref,
  workflow_dispatch event, GitHub-hosted runner and exact run/attempt URI.
  The signed statement has exactly two subjects, guard.exe and
  source-proof.json, with one SHA-256 each. Its verified log time must be
  within the actual pinned job start/end window.
- capture.json is not a signed subject. Its authority is the exact zip digest
  and publication record. Its fields must agree with the signed proof, the PE
  and the server tuple. It must never be described as a third signed subject.
- The canonical proof has compiler_exit=0, equal before/after observations,
  the fixed command and sorted, complete source inventory. Its nested
  observation, graph, image and command digests are recalculated.
- The source and producer trees are read from the Git Data API and their tree
  object hashes are rebuilt from every non-truncated recursive entry. The
  selected module entries must match the signed inventory exactly. A bounded
  source archive supplies the selected file bytes: each must match both its
  calculated Git blob SHA-1 and the signed SHA-256. A compare response must
  identify the source as the producer's ancestor. The workflow/helper blobs
  are independently fixed in the issued pin.
- The exact PE must be AMD64. Its Go build info must name the guard module,
  the source revision, an unmodified checkout, windows/amd64, CGO disabled and
  trimpath. Go omits -pgo when it is off; a present profile setting is refused.
  The Go version must match the toolchain observation.

The final server read must agree with the first read. A complete acceptance
preserves the archive, all three payload buffers, both received compact bundles,
the seed representation, publication record and result in the existing diagnostic output directory with CreateNew.
Any write/close/sync error or collision fails; an old result is not overwritten.

## Controls and refutations

Offline source controls cover unissued/invalid pins; duplicate/unknown/null
publication fields; source/producer, run/job/check/artifact substitution;
step order and skips; token isolation across redirects; bad destinations,
overflow, close error and cancellation; exact zip inventory and digests;
before/after, compiler exit, graph/image/command bindings; Merkle/tree and
selected source bytes; build info; two subjects and the signing time window;
missing log proof components; exclusive result publication.

The old committed bundle is used only as a rehearsal for both digest policies,
18 non-empty identity/digest mutations, five byte tamper controls, cancellation
and rejection under a different producer identity. It never proves a new
capture or payload acceptance. Synthetic objects in offline tests are parser
and binding fixtures, never a replacement for cryptographic verification.

The actual test repeats the real-bundle identity and tamper controls, rejects
zero/two valid entries and checks a cancelled verification. A same-digest PR
bundle cannot satisfy the main certificate identity; a manually assigned
"verified" flag is not the actual selection path.

## Results and expiry

The acceptance marker is guard_capture_result passed=true with
signature_checked=true, zip_checked=true, source_closure_checked=true and
build_info_checked=true, plus PASS for TestGuardCaptureAttestationActual and
its controls. A SKIP or absent marker never meets this condition.

After artifact expiry the test may recheck the public signed claims only.
Its report keeps passed=false and zip_checked=false, records
artifact_expired_payload_not_executed and skips the payload check. It neither
turns an expired missing payload into success nor keeps CI permanently red
solely because the one-day archive expired. Earlier retained evidence remains
a historical acceptance, not a new execution.

## Limits and ownership

Compiler, cache, DLL and all dependency bytes are not a complete immutable
toolchain closure. Runner/toolchain and build graph fields are observations.
The producer-specific Git archive digest is signed observation data; the
verifier independently checks selected source bytes from the server archive,
not a byte-for-byte recreation of git archive. The Git tree SHA-1 binding,
GitHub server metadata and authenticated API transport
are distinct from Sigstore's signed claims. The attestation ID is a server
record label tied to the publication and bundle URL, not an extra signed field.

Every result fixes update_authorized=false, data_access_granted=false,
installed_tested=false, hosts_tested=false and compiler_closure_complete=false.
No normal root issuer, held installed target/ACL, update permit, installer or
Codex/Claude Code runtime acceptance is supplied here.

Only guard bootstrap is covered. Reader capture, a common engine, production
main.go, dependency locks, existing Matrix tests and producer schemas are
unchanged. These tests reuse the matrixPolicy, matrixVerify, listing,
selection and tamper helpers in matrix_attestation_test.go; removing that file
requires moving those helpers without changing their verified semantics.

Workflow changes have two stages: first add actions:read and these three files
to the verifier input inventory, and give the combined tests a sufficient
finite budget; then add the actual opt-in only with issuance. The existing
one-day upload retention and two attestation subjects remain unchanged.


## Retained seed bundle

The bootstrap job retains the official attest action's bundle-path in a
separate artifact named guard-bootstrap-bundle-<run>-<attempt>-<job>.
It uploads only that generated file, using the same pinned upload action,
one-day retention, no compression and no overwrite. A missing file fails
the job. This does not build or sign anything a second time.

The capture artifact remains exactly three entries; the signed subjects remain
guard.exe and source-proof.json. The bundle artifact is seed evidence, not a
third subject, a new permission or a replacement for signature verification.

Before issuing the data pin:

1. Bind the successful seed-upload step and the artifact metadata to the exact
   run, attempt, job and producer revision. The derived artifact name alone is
   insufficient. Record its server ID, archive digest, size and expiry.
2. Download that artifact through the normal authenticated artifact endpoint.
   Consume the returned file reference's download URL unchanged, with a finite
   deadline, no redirects and an archive byte bound. Match the same buffer's
   length and SHA-256 to the server metadata before reading its contents.
3. Require exactly one regular member, attestation.json, within the bundle
   limit. Retain the original bytes and their hash. Compute the pin's
   BundleBytes and BundleSHA from JSON.Compact bytes, preserving key order,
   number spellings and string escapes. Read RekorIndex as an untrusted input
   and compare it with the exact job's action log.
4. Bind the statement's two subject digests to the same capture artifact and
   publication record. Populate only the reviewed data pin and enable the
   actual check with the one-line workflow change.
5. Require the existing hosted verifier to select and verify both public
   subject listings and bind each to the original compact seed through the
   limited representation rule below, then validate the payload, source
   closure and build information. Seed collection alone is not success.

The publication record schema is unchanged: it has the attestation ID and the
capture artifact digests, but no bundle byte length, bundle digest or log index.
Those values are measured from the retained seed and confirmed by the later
cryptographic check. Both artifacts must be retained locally through the normal
source-qualified receipt before their original one-day lifetime ends.

## API representation and retained-seed binding

BundleBytes and BundleSHA still identify the original retained JSON.Compact
seed. The verifier never replaces them with the observed API digest. A received
compact bundle with that exact length and digest follows the existing path.
Only after signature, identity, statement/time and unique selection succeed
may a differing digest use this limited byte projection:

- Require the existing three root members in their original order and exactly
  certificate, tlogEntries and timestampVerificationData under the direct
  verificationMaterial object. Reject duplicate and case-fold duplicate keys,
  escaped direct key names, unknown or missing direct members, noncompact
  input and trailing data. Only the original seed order and the observed API
  order tlogEntries, timestampVerificationData, certificate are supported;
  other direct permutations fail closed. The received bundle remains bounded.
- Place only those three direct members in the retained producer order
  certificate, tlogEntries, timestampVerificationData. Copy each complete raw
  value span and every byte outside that object unchanged. No payload, signature,
  certificate, log body, proof, checkpoint, number or string is decoded and
  re-encoded by the projection. All array and nested-object order is retained.
- Require the resulting length and SHA-256 to equal the unchanged seed pin.
  Semantic JSON equality alone is insufficient. A different scalar spelling,
  nested order, raw signed material or any other byte fails the seed binding.
  This rule is not general JSON canonicalization or a new source authority.

Both subject policies still verify their own received API bytes. They must
project to byte-identical pinned seed bytes. The result's bundle_sha256 is the
received PE representation hash; pin.bundle_sha256 remains the seed hash.
Preservation writes guard-capture-bundle.json for the PE listing,
guard-capture-proof-bundle.json for the proof listing and
guard-capture-seed-representation.json for the reconstructed pinned bytes.
The four fixed binding facts (size, seed representation, log index and
attestation record) are evaluated independently after verified selection and
recorded as booleans, while the first failure still controls rejection.

A failed seed binding may retain guard-capture-pe-verified-unbound-bundle.json
or the corresponding proof diagnostic. Such a file is cryptographically
checked but unbound evidence, never acceptance, an alternate pin or an update
permit. Old CI failures and later byte observations remain separate records.
