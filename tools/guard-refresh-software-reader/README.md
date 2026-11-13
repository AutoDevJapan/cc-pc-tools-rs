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
Rust uses the six required crates in a deterministic workspace. Before any
resolution, the fixed software purpose verifies and expands every public
package archive selected by the original admitted lock. Each archive's held
bytes must hash to its locked checksum; those same bytes become the owned
compiler inputs. Resolution then derives a smaller lock from those verified
inputs. Every resulting package version, registry and checksum must already
occur in the original lock; its digest is recorded with the snapshot.
The fixed logical cache and its separately qualified physical root must have
the same held directory identity before a package body is opened. Resolution
then uses the verified directory source and a Cargo home without a shared
registry link; it cannot unpack an unverified shared archive first.
Creation rejects traversal, links, duplicate or aliased paths and special
entries, with finite compressed, expanded, file and entry limits checked
before file creation. Extracted files remain held through compilation. An
explicit Cargo source replacement selects this namespace; generated checksum
metadata alone cannot qualify content. The shared registry cache is not
modified, and failed preparations preserve their incomplete target.

The MP4 Rust child uses a separate Cargo home containing the fixed compiler
configuration and no shared registry link. The earlier privacy-only software
purpose retains its separate existing public-registry cache setup. The home is
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

The current fixed input revision also includes repository-history verification
for project labels. Its sender and runner inputs are checked independently,
and all build, export, preparation and receipt names belong to that revision.
An old exported binary or producer receipt cannot satisfy this new generation.
Candidate digests remain unbound until the reviewed producer completes; source
verification alone cannot authorize installation or a real question.

Compiler failures report only a fixed cause and, when present, a validated
Rust error code. Compiler stdout and stderr are drained into fixed-capacity
tail buffers and erased on return; paths, source lines and error messages are
not returned or written to receipts. Protocol workers retain their stricter
complete-output and empty-stderr requirements. A diagnostic never authorizes
preparation or makes a failed build successful.
Archive-input failures also retain fixed read/qualification stages, OS error
numbers when available, completed/expected archive counts and elapsed time.
These counters describe admitted work, not a completed or qualified snapshot.
The existing input deadline and all verification checks remain in force.
The pipe reader also owns and clears its fixed read scratch buffer; this is a
bounded application-buffer guarantee, not complete erasure of Go or OS memory.

New snapshot directories temporarily exclude new entries. Original ACLs are
captured before any directory is sealed, restored in parent-first order, and
read back. A final exact namespace check follows sealing before compilation.
New files are flushed and checked through their creator handle, then handed
to a read-only file object for compiler compatibility. During this handoff,
overlapping handles exclude replacement and a temporary read-only DACL on
only that operation-owned file blocks ordinary new writers. After acquiring
the final read-only sharing pin, the complete held content is hashed again
against its pre-handoff digest. A same-owner ACL change cannot make altered
content pass that check. The original DACL is then restored and read back.
Cleanup attempts every handle even when one operation fails, including failed
construction; primary and cleanup errors remain distinguishable. The
producer's final response must confirm cleanup and match its ready artifact,
snapshot, lock and configuration before a successful export receipt is issued.
An incomplete restore or unknown child result preserves artifacts and cannot
report a qualified completed distribution. This maintenance grants no access
to conversation records, images or neighboring files.
