# Fixed installed guard software maintenance

`--verify-installed-refresh-guard` keeps its existing observation-only behavior.
It identifies the previously observed installed executable without admitting
data files or changing software.

`--verify-refresh-guard-update` validates one compiled software update without
writing files. `--prepare-refresh-guard-update` repeats that validation and
creates exactly the compiled stage and backup names with `CREATE_NEW`.
Neither command accepts a path, digest, receipt, or authorization supplied by
the caller. Neither replaces the installed executable or changes the generic
resource guard.

Preparation pins the original program, candidate, build proof and their plain
parents. The original file identity and recorded build flags must match the independently
observed baseline. The candidate digest and dirty build record must match the
fixed input proof and its merged source blobs. The exact held buffers are used
for copying; Owner, Group and DACL are copied from the original handle, then
bytes, file identity and ACL are read back. An existing output is preserved.
Partial preparation is reported and retained for inspection.

`--verify-ask-privacy-update` and `--prepare-ask-privacy-update` maintain the
one compiled `discord_ask.exe` privacy repair. They accept no caller path or
proof. The predecessor's exact bytes and file identity, the candidate and its
input proof, and unchanged repository input blobs on merged main must match.
Preparation creates only its two fixed sibling software leaves and preserves
their inherited ACL and exact held bytes. Partial failures stay unsuccessful
and report every created identity; they do not grant record or media access.

The deployment controller independently reacquires the original and both new
objects and checks their identities, bytes and ACL before changing a name.
The controller's separate review, runtime acceptance and publication conditions
continue to apply. Preparing software grants no access to neighboring records.

Windows validation: `go test -count=1 -v .` and `go vet .`. Tests use only owned
synthetic software and do not prepare the installed program's actual leaves.
