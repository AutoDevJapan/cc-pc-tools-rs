# Guard artifact verification and update boundary

The hosted build produces evidence for a fixed guard source. A later consumer must independently associate the server run, attempt, job, upload step and artifact, verify the archive and both attestations, and hold the verified candidate through update preparation. A name, a JSON field or a digest alone is not update authority.

The metadata reader is prepared to reject a different repository, source, producer revision, run attempt, job, check run or artifact. It also requires the expected successful ordered job steps. Source-proof validation checks the captured before and after snapshots, input inventory and fixed command against the same archive bytes. The Sigstore verifier checks the fixed artifact subjects and workflow identity using the public TUF trust root; observed runner and toolchain details remain observations. The reader implementation has not yet been compiled or executed.

The current verified-candidate factory returns failure until a fixed authenticated GitHub transport retrieves the exact job publication and artifact, and the source-proof and Sigstore checks are connected to a private issuer. GitHub credentials must be provided through that normal transport; JSON, a prior report or a digest cannot create a candidate. The installed guard's current identity and ACL are not established by a previous build record. Before any update, a fixed software-purpose provider must establish the exact current target and candidate generation while respecting existing protected-data exclusions.

The documented administrative broker may be considered for a separately reviewed fixed operation once those proofs exist. Neither this document nor the hosted build alone authorizes a replacement.
