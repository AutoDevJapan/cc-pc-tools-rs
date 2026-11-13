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

Source verification uses one bounded Git archive, compares every input's Git
blob ID and SHA-256, and shares an eight-second context across all Git calls.
The limit is not renewed for each child. No preparation starts if that context
has expired; any partial creation remains unsuccessful and is reported. Local
remote-tracking refs do not prove fresh remote state: deployment must also bind
the exact input tree to its current merge and fetch evidence.

The eight-second context does not interrupt synchronous Windows I/O. The public
ask verify/prepare entry owns a separate suspended worker in a kill-on-close
Windows Job, gives it ten seconds, and waits for its process tree to terminate.
An expired worker cannot report qualification or preparation success. Failure
or timeout then uses a separate two-second read-only inspection of the three
compiled software names. Its `reconciliation` reports present identity/hash/ACL,
absent, or unknown and always denies switching. Up to two seconds are reserved
for collection after each worker: the caller must await this recovery result,
not kill the outer supervisor at the old ten-second producer limit. If that
outer result is unavailable, preserve all leaves and perform no replacement.

The deployment controller independently reacquires the original and both new
objects and checks their identities, bytes and ACL before changing a name.
The controller's separate review, runtime acceptance and publication conditions
continue to apply. Preparing software grants no access to neighboring records.

Windows validation: `go test -count=1 -v .` and `go vet .`. Tests use only owned
synthetic software and do not prepare the installed program's actual leaves.

The separate Cargo software purpose owns one final-source producer and its
fresh isolated target. It retains all 207 source inputs, then keeps both output
aliases held while the receiving process checks the same identity and bytes,
creates its fixed export and acknowledges receipt. A successful producer exit
and re-pinned export precede publication of the completed producer receipt.
The ordinary resource guard and its hardlink rejection remain unchanged.

Producer receipts are written and flushed under a new `.pending` name before
an overwrite-free final rename. Cancellation detected after pending creation
and by the final check before publication preserves that pending file and
reports failure; cancellation before creation leaves no pending file. A successful final rename is the commit
point: cancellation racing after that final check does not undo or relabel the
completed receipt. Synchronous Windows I/O is not claimed to be interruptible.
Incomplete targets and exports are preserved where present. Creation and state
are reported when observed and remain unknown otherwise. They are never adopted
on a later run merely because their file IDs or sizes match.

The MP4 sender and verifier use separate fixed software purposes and fresh
build/export names. Neither accepts a caller-selected path or digest. Source
buffers are matched to merged input blobs, then materialized under a new build
namespace and held through output acknowledgement. The Go compiler receives
only selected, proved files and explicitly disables automatic PGO inputs.
Rust uses the six required crates in a deterministic workspace. An offline
derived lock is admitted only when every package version, registry and checksum
already occurs in the original lock; its digest is recorded with the snapshot.

The Rust child uses a separate Cargo home containing a fixed compiler/cache
configuration and the existing public registry cache only. Its directory is
sealed against new entries through acknowledgement, so a later legacy `config`
cannot override `config.toml`. The held configuration and the exact home
namespace are rechecked before output acknowledgement. It starts at an
operation-owned temporary drive root backed by a sealed empty directory,
excluding physical ancestor configuration without changing shared ACLs. The
exact local drive mapping and empty cwd are checked after sealing. The outer
supervisor owns the mapping and, while it remains alive, removes it after
confirmed worker-tree termination, including worker timeout or crash. A changed
mapping or unknown child state is preserved and reported unsuccessfully.
After a drive is defined, preparation failure reports its exact name and cleanup state;
this does not guarantee cleanup after the supervisor itself is killed. Ordinary user
configuration and credentials are not copied. The child environment is explicit;
selected Windows/toolchain paths and the fixed compiler/cache inputs join the
configuration digest, while inherited Cargo profile/linker overrides and product
binding variables are omitted. The known
compiler and sccache remain enabled. The build recipe and effective configuration
are separate from application source provenance.

New snapshot directories temporarily exclude new entries. Original ACLs are
captured before any directory is sealed, restored in parent-first order, and
read back. A final exact namespace check follows sealing before compilation.
Cleanup attempts every handle even when one operation fails, including failed
construction; primary and cleanup errors remain distinguishable. The
producer's final response must confirm cleanup and match its ready artifact,
snapshot, lock and configuration before a successful export receipt is issued.
An incomplete restore or unknown child result preserves artifacts and cannot
report a qualified completed distribution. This maintenance grants no access
to conversation records, images or neighboring files.
