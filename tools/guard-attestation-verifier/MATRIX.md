# Matrix main artifact attestation check

This module also holds an opt-in test, `matrix_attestation_test.go`, that checks
the public build attestation of one fixed GoDD-Matrix artifact. A passing run is
evidence about that attestation only. It does not read, install, replace or
start any program, and it is not a permission to do so.

## Fixed subject

| Item | Value |
| --- | --- |
| Repository | AutoDevJapan/GoDD-Matrix, ID 1293650669, public; owner ID 246834983 |
| Workflow | `.github/workflows/mcp-artifact.yml` on `refs/heads/main` |
| Source commit | `ece931cb71c191ab1b977629e3fdac1846bb07d1` |
| Run | 44655701843, attempt 1, trigger `workflow_dispatch`, GitHub-hosted runner |
| Subject | `godd-matrix-mcp-ece931cb71c191ab1b977629e3fdac1846bb07d1.zip`, SHA-256 `14fd21b3e610b0cf7b307eb7746dd9d4ae99c4fb7744722f6c9296f096bb20e3` |
| Workflow artifact | 14593605352 (3,941,903 bytes) |
| Attestation | 69945085, Rekor log index 4013169934, as reported by the run log |

Pull-request runs of the same workflow can build a byte-identical zip. Their
attestations carry the same digest, a differently named subject and a
pull-request identity, and they are listed for the same digest. The test must
not select them.

## What the test does

1. Obtains the Sigstore public-good trusted root through the TUF client that
   this module already uses.
2. Reads the public attestation listing for the subject digest from one fixed
   GitHub API URL: one page, no redirects, at most 4 MiB. When `GH_TOKEN` is set
   the job token is used first and is sent to `api.github.com` only; a refused
   request falls back to an anonymous one. A transport error, or a failure that is not a
   4xx status, is retried once.
3. Verifies every listed bundle of the repository with sigstore-go v1.4.0,
   configured to require one signed certificate timestamp, one transparency-log
   entry and one observer timestamp. The library checks the log entry (the
   inclusion promise, and the inclusion proof with its signed checkpoint,
   against the Rekor key of the trusted root), the observer timestamp, the
   certificate chain at that time, the signed certificate timestamp, the DSSE
   signature, that the artifact digest is one of the statement subjects, and
   last the certificate identity.
4. The identity policy fixes the subject alternative name, the OIDC issuer, the
   build signer and build configuration URI and digest, the source repository
   URI, ID, digest and ref, the owner URI and ID, the runner environment, the
   trigger, the run URI and the repository visibility at signing.
5. Requires exactly one bundle to verify. The selection is the verification
   result; it is never a flag set by hand.
6. On the selected bundle, requires the in-toto v1 statement with the SLSA
   provenance v1 predicate type, exactly one subject with the fixed name and
   digest, a verified transparency-log time inside the run window (the run was
   created at 2026-11-20T01:43:37Z and its job timeout is 20 minutes), and the
   Rekor log index 4013169934.

## Controls

Every control uses a real bundle and the same trusted root, in the same run as
the positive result.

- Same digest, other entries: at least one listed bundle with a pull-request
  identity must be rejected at the identity stage, which means every
  cryptographic step passed and only the policy refused it.
- Policy mutations: 18 subtests, each changing one policy field to another
  non-empty value (subject alternative name, issuer, build configuration URI,
  ref, trigger, run ID, run attempt, source revision, repository ID and URI,
  owner ID and URI, runner environment, visibility, subject digest). The bundle
  must be refused, at the identity stage for identity fields and at the
  signature stage for the digest, and the error must name the mutated field.
- Bundle tampering: 5 subtests (DSSE signature, DSSE payload, leaf certificate,
  transparency-log index covered by the inclusion promise, inclusion-proof root
  hash). The unmodified, re-encoded bundle must still verify, so a refusal
  comes from the edit.
- Other repository and other digest: the fixture bundle of the guard build is
  refused by the Matrix identity policy, and the Matrix bundle is refused for
  another digest.
- Cardinality: two copies of the verified entry, and no verified entry, are both
  refused.
- A cancelled context is refused, and the signed statement fields (subject name,
  run URI) are enforced.

Unit tests that need no network also run in every invocation: the fixed tuple is
self-consistent, every policy field has a mutation that changes only that field,
the listing parser and the single-page check fail closed, the one-entry rule
holds, and every tamper edit finds and changes its target.

## Reading a run

- The test is `TestMatrixMainAttestationActual`. A run counts only when the job
  log shows `--- PASS: TestMatrixMainAttestationActual`, its subtests, and the
  line `matrix_result passed=true controls=29`. A `--- SKIP` line means the check
  was not executed and is not an acceptance.
- The log lines `matrix_listing`, `matrix_entry`, `matrix_selected`,
  `matrix_identity` and `matrix_statement` record counts, Rekor indexes,
  identities and digests. No response body, URL query or token is printed.
- When `GUARD_ATTESTATION_DIAGNOSTIC_DIR` is set, the test also writes
  `matrix-main-attestation-report.json` there with the same fixed fields, so the
  retained diagnostic output carries it.
- The earlier tests of this module verify a different bundle (the guard build).
  Their success says nothing about the Matrix attestation.

## What this does not establish

- Nothing about any installed program, a host or an update. The result grants no
  access or replacement permission.
- The attestation ID 69945085 is not authenticated by the listing. It is tied to
  the signed Rekor log index that the workflow run reported for that attestation.
- The zip itself is not downloaded or read here. A local copy has to be compared
  with the digest above separately.
- The run event and attempt are covered by the signed certificate identity; they
  are not looked up again through the Actions API.
- The predicate content is not interpreted beyond its type.
- A locally saved or browser-downloaded copy of a bundle is never an input to
  this check.

## Environment

- `MATRIX_MAIN_ATTESTATION_ACTUAL=1` opts in. Without it the test is skipped, and
  a skipped run is not an acceptance. The hosted job sets it for the step that
  runs this module's tests; removing it there retires the live check.
- `GH_TOKEN` is optional and is used only as described above.
- `GUARD_ATTESTATION_DIAGNOSTIC_DIR` selects where the report is created; the
  file is created exclusively and an existing file is never overwritten.
