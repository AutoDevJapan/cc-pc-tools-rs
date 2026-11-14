# Guard software build evidence

The guard update requires a candidate made from a reviewed source revision and a separately verified producer result. The existing historical update is bound to another candidate and is not reusable for this purpose.

A proposed GitHub Actions producer must run only for the fixed reviewed source, capture the complete selected source and build inputs before and after compilation, and retain the resulting binary with its build evidence. The server run, attempt, job and upload step must be independently related to the artifact ID. The artifact archive digest and the binary digest are different checks.

The consumer must read a bounded archive, reject ambiguous entries, verify the binary bytes and provenance, and retain the same validated candidate through the existing fixed software preparation boundary. Current installed identity and ACL must be independently measured before preparation. A successful build or downloaded archive alone does not authorize replacement.

The proposed producer runs once on the dedicated GitHub-hosted Windows job after the producer and fixed guard source have passed review. It compares the fixed checkout against its Git tree and archive, captures input and Go package observations before and after one build, writes a bounded source proof and a capture manifest, then exposes the three fixed files for attestation and upload. The workflow revision and guard source revision are recorded separately. Runner and toolchain observations do not prove the complete compiler closure.

This candidate has not yet been compiled or run. The proposed workflow calls it only on the dedicated dispatch; that workflow has not yet been published or executed. The installed software and normal runtime remain unconnected. Authenticated job-to-artifact binding, verified attestation, complete compiler/dependency closure, retained candidate admission and installed acceptance remain unfinished.
