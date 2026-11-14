# Fixed hosted evidence acquisition

This slice prepares a fixed-purpose acquisition path for the existing guard candidate. It does not provide the missing normal authentication authority, issue an installed-target grant, or enable a command.

The authenticated GitHub client retrieves the fixed workflow, job log and run/job/artifact metadata. The authenticated client retains the first redirect response without following it, then closes its body before a separate credential-free HTTPS client reads an approved object-storage host. The object client rejects any further redirect and closes its response. Both stages have finite deadlines and byte limits. The publication, fixed attestation identifier, archive size and digest, canonical capture, source proof and payload are checked against the same acquired archive. Failed acquisition discards partial buffers.

A later normal host must supply a genuinely acquired Sigstore bundle and the existing code-owned GitHub authority. The fixed bridge then runs the real in-process signature verifier and verifies the selected Git source using that same authority client before returning the candidate. A bundle, path, digest or status alone does not grant authorization. The current normal authority is unavailable and the bridge is not routed by the dispatcher.

This is an acquisition slice, not proof that the host provides a bundle or that the object-storage host for a future run is permitted. The actual attestation lookup, normal host handoff, installed target identity, ACL and update remain separate. Client-pointer checks do not prove an immutable owner lease or revocation lifetime; that lease and the bundle-to-server attestation association must be supplied and verified by the normal host before this bridge is routed. No producer build, actual target open, candidate execution or installation is performed by the tests.

The test transport uses synthetic workflow metadata, archive, bundle and redirects. Test success would not establish real GitHub credentials, Sigstore verification or an installed-target grant.
