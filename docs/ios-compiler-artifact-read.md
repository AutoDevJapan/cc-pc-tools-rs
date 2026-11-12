# Read the original iOS compiler diagnostic

The two input-free Windows operations inspect only repository
`AutoDevJapan/phone-friction-control-ios-sdk-audit`, run `43067567260`, attempt
1, job `130686487618`, artifact `13876552306` and source
`05c636ee1cfbf51db47772d334aff993d0470adf`.

- `--verify-ios-compiler-artifact` returns qualified status and the fixed tuple,
  current physical file identities, lengths and SHA-256 digests.
- `--consume-ios-compiler-artifact` returns a programmatic JSON response with
  those same eleven verified file buffers encoded as base64. Capture it directly
  in the intended read-only consumer; do not display a bulk diagnostic response
  or use it as an execution, deployment or extraction input.

The normal GitHub metadata reader verifies repository/owner, original workflow,
push event, run/attempt/source, the failed compiler job and successful artifact
upload, then the original artifact's digest. A failed compiler diagnostic is
eligible for inspection; it is not evidence that compilation or the SDK passed.
Remote download expiry does not authenticate or invalidate retained local
bytes, and these operations never download a replacement.

Only then does the reader open the original retained archive and eleven exact
files through held physical directory and file handles. Reparse points,
hardlinks, aliases, different bytes, duplicate ZIP entries and unsafe names
fail. The archive buffer that matches the provider digest is the buffer parsed;
the original file buffers that match its entries are the buffers consumed.
Write and delete sharing stay excluded until verification and consumption end.

The platform issuer seals a manifest under a dedicated purpose domain and
verifies that seal before handing off bytes. This is a current-invocation
read-only qualification, not a caller-supplied signature or a historical
creation/extraction attestation. No reusable receipt token or caller-selected
path, hash or signing input is accepted. Metadata explicitly leaves historical
creation unverified and grants no general path access or registry recovery.

The eleven files are `input.swift`, `input-sha256.txt`, `scope.txt`,
`versions.txt`, `root-replacement.sil`, `root-replacement.ll`,
`root-replacement.s`, `silgen.stderr.txt`, `ir.stderr.txt`, `asm.stderr.txt`
and `status.txt`. Other bounded regular ZIP diagnostic entries are not opened
or returned. Handles close and temporary buffers are erased on success and
failure. Cleanup failure remains explicit and prevents a successful CLI exit.
File and ZIP reads use one explicitly allocated bounded buffer. Output is
captured in one bounded owned buffer through one erased base64 scratch buffer,
instead of pooling diagnostic content in a JSON encoder. It is published only
after original handles close successfully. The caller must capture the complete
response and require exit zero plus qualified, consumed and cleanup-complete
metadata before using any file bytes. Incomplete output is discarded.
The guarantee covers these owned buffers and the returned
platform allocation, not complete secure erasure of Go runtime, compression
library, operating-system or consumer-owned copies.

The fixed metadata provider is created inside an owned Windows job before it
can run. Its gate and descendants are terminated and the kernel active-process
count must reach zero on normal exit, deadline and failure. This does not prove
the behavior of unrelated processes or containers.

This feature does not establish Swift ABI correctness, runtime drain behavior,
or product completion. Those remain the original SDK consumer's acceptance.
