# Fixed guard candidate and metadata purpose

The candidate adapter consumes only the package-private server candidate.
It does not turn a signature report, JSON, a digest, a byte slice, a caller
boolean, a historical installer pin, or an owner-name string into an issuer.
The server factory must associate the authenticated run, attempt, job, upload
and artifact with the bounded archive and verify the exact payload and proof
against their cryptographic attestation before returning that candidate.

The adapter atomically claims ownership of that exact candidate once before
checking a partial acquisition error. The issuer returns an opaque owned wrapper;
only that wrapper can inspect, borrow or close the candidate after the move.
A second or concurrent claim, an active raw borrower, a closed candidate, and
a copied owned wrapper are rejected. A failed claim never closes the raw alias
or another adapter's resources. The adapter snapshots the
complete immutable producer generation and validates one borrowed payload's
length, digest and bounded PE signature before admission. Every later borrow
rechecks the same candidate pointer, generation, length and digest before and
after the callback. A copied wrapper, closed candidate, changed generation,
mutation, cancellation or unsuccessful callback cannot retain admission.
The bounded PE signature is not a full executable or compiler-closure proof.

## Fixed owner software purpose

The existing owner request to repair the installed guard supplies the purpose
for the one compiled software target and the metadata-only operation. The
private purpose constructor accepts only the exact artifact adapter above.
It binds the artifact wrapper, underlying candidate and producer generation;
no public path, target, hash, JSON or authority argument is accepted.

The purpose permits a single metadata lease only while the candidate payload
is actively borrowed. It uses the existing metadata opener, which requests
FILE_READ_ATTRIBUTES and READ_CONTROL and retains the same non-reparse
objects through observation. It does not request file-data read/write, change
an ACL, authorize a preparation/replace, execute the candidate, qualify the
parent store, or grant access to adjacent records. Existing exclusions stay
in force. An inspection result is not a serialized capability for later work.

## Close and failure ownership

The metadata controller closes its metadata lease, then the purpose, then
the artifact. Closing the purpose revokes only that purpose; it never closes
the candidate. Artifact Close revokes its wrapper once and closes the exact
candidate. A normal Close waits for candidate borrowing to drain. Close
reentered during an active borrow reports incomplete cleanup; the candidate
retains its buffer until that borrow ends. The wrapper never calls the
candidate's Close a second time to hide a prior cleanup failure. The caller
must not copy or concurrently move an owning artifact.

Partial artifact acquisition claims the candidate even if its generation is
invalid and returns that owning wrapper together with the failure so that the
existing controller closes it exactly once. Generation and payload checks still
reject use of the partial resource; cleanup ownership does not authorize it. Partial
metadata opens remain the controller's cleanup responsibility. The result
continues to keep primary failure, cancellation and cleanup as separate facts.
A successful metadata observation leaves software_replace_allowed,
data_access_granted and update_performed false.

## Integration and verification status

The new adapter and the two normal factory delegations are prepared source.
The normal server factory currently returns unavailable, so metadata open
cannot be reached through the normal route yet. An actual verified-candidate
instance, authenticated transport, complete source-proof verification and
normal cryptographic issuer integration remain upstream work. A synthetic
package-private issuer in tests proves adapter behavior only.

The candidate depends on the separately owned fixed server v2 Claim/owned
generation/borrow API. Both source packets must be combined for compilation
and independent review. Static signature agreement and source hashes do not
substitute for those checks. The public dispatcher remains unchanged.

Tests are prepared for missing/partial issuer, changed generation and bytes,
wrong wrapper/purpose, single metadata open, mutation, cancellation, panic,
borrow reentry and separate cleanup. The ownership delta adds sequential and
concurrent duplicate loaders, partial/error cleanup, old raw aliases, copied
owned wrappers, a claim during raw borrowing and active owned-borrow drain. Local Go, gofmt, compilation and tests
have not run. No actual installed target was opened, read, executed or
modified, and no normal issuer or runtime grant is claimed by this slice.
