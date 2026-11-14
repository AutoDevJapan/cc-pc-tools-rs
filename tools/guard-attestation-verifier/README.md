# Guard artifact attestation diagnostic

This isolated diagnostic checks the public Sigstore attestation for a single
fixed guard build. Its result is evidence about that attestation, not
permission to read or update an installed executable.

## Fixed subject

- Repository ID: 1406818713, workflow:
  .github/workflows/unattended-readiness.yml@refs/heads/main.
- Producer revision: f2f9e4c4cddba70240004e7084443c27c51baee9,
  workflow run 43574333045, attempt 1.
- Source checkout recorded in the source proof:
  80c4ed5cbd864585f4894c6286d3d5c92194c1f5.
- Subjects: guard.exe SHA-256
  a61e25065ae35d51f1ac8133959176b21e2c3740c1516b76f799371f1c27f9a2
  and source-proof.json SHA-256
  aeeefe291c6a10a0f8d1bf1ee1a0952daf29dba87046596980c7c509c690f8c8.

The included bundle is public, untrusted input to the test. A TUF client
initializes from the official embedded Sigstore Public Good root and fetches
the current authenticated trusted material. Verification requires the signed
certificate timestamp, transparency-log entry, observer timestamp, exact
certificate identity and both subject digests.

The diagnostic reads three big-endian 32-bit length-prefixed frames from
standard input, in order: the fixed guard bytes, the fixed source-proof bytes,
and actual-attestation.sigstore.json. The stream must end after the third
frame. The command returns failure after two minutes if input or output stalls;
it exits the process rather than leaving an abandoned I/O operation running.
Output may be absent or partial on failure. Consumers must accept the JSON
report only when the command exits with code 0; there is no second report
after a partial write. There are no path, hash, identity or trust-root
command-line options. Successful cryptographic verification returns a report with
cryptographic_verified: true. The report always leaves
source_proof_validated, run_metadata_validated, and update_authorized false.
Independently validate proof contents, GitHub run/job/artifact binding, target
identity and update authority before any use.

For the built diagnostic, setting GUARD_ATTESTATION_DIAGNOSTIC_FIXTURE=1
selects the embedded public bundle and verifies its signed digest claims with
the same TUF trust and certificate policy. This mode does not read or verify
the subject bytes, and its report states that fact.

The test uses the included real public bundle and official TUF root to verify
both fixed digest claims. It also checks altered signatures, a different run,
a different workflow revision, and a different subject. It does not include
or validate the actual guard binary or source-proof bytes in the repository.
The first dependency-generation run and its lock output have been collected.
The fixed-lock workflow checks the committed go.mod and go.sum using
go mod tidy -diff and -mod=readonly; this dependency check alone does not
establish cryptographic verification, payload validation or update authority.
