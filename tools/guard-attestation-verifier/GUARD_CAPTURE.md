# Fixed guard capture attestation and payload check

This directory's guard capture tests check evidence from one explicitly issued
guard bootstrap run. They do not install, replace, start or admit software.
The normal runtime verifier remains unchanged.

## State and sequence

The next artifact tuple is **not issued**. The source pin is the reviewed commit
c1adc362778aae5270ceacea1214b9d17940589e, root tree
2137e3ff923aa680041d82f642d881305efd0e98, with the repair module tree
4a9e710eadbdb78e3842bb88361d9b5ce68b238e, contract module tree
7278e7e373d0c65ee7446ad54223e85b3fab565f and 253 selected inputs.
The producer/workflow revision is a separate, future main commit.

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
  bundle, and both selections must contain the same compact bundle bytes.
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
preserves the archive, all three payload buffers, compact bundle, publication
record and result in the existing diagnostic output directory with CreateNew.
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
