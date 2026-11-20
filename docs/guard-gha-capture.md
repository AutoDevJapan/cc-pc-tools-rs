# Guard software build evidence

The guard update requires a candidate made from a reviewed source revision and a separately verified producer result. The existing historical update is bound to another candidate and is not reusable for this purpose.

A proposed GitHub Actions producer must run only for the fixed reviewed source, capture the complete selected source and build inputs before and after compilation, and retain the resulting binary with its build evidence. The server run, attempt, job and upload step must be independently related to the artifact ID. The artifact archive digest and the binary digest are different checks.

The consumer must read a bounded archive, reject ambiguous entries, verify the binary bytes and provenance, and retain the same validated candidate through the existing fixed software preparation boundary. Current installed identity and ACL must be independently measured before preparation. A successful build or downloaded archive alone does not authorize replacement.

The proposed producer runs once on the dedicated GitHub-hosted Windows job after the producer and fixed guard source have passed review. It compares the fixed checkout against its Git tree and archive, captures input and Go package observations before and after one build, writes a bounded source proof and a capture manifest, then exposes the three fixed files for attestation and upload. The workflow revision and guard source revision are recorded separately. Runner and toolchain observations do not prove the complete compiler closure.

The helper is tested by the existing readiness workflow. A formal capture is a separate, dedicated dispatch after the producer and fixed guard source have passed review. Results for an earlier source pin do not establish acceptance of a later pin. The installed software and normal runtime remain separate. Authenticated job-to-artifact binding, verified attestation, complete compiler/dependency closure, retained candidate admission and installed acceptance must be checked for the selected capture.

## Source pin

The fixed guard source is pinned by five values in main.go: its commit (guardSource), its root tree (guardTree), the trees of its repair-guard and tools/civ6-ci-contract directories (guardRepairTree and guardContractTree) and the number of regular files below those two directories (guardInputCount). The workflow carries the commit as the checkout ref and, with the root tree, as the source and source_tree fields of the publication record. A test keeps those three workflow literals equal to the two constants that the capture publishes.

capture.json, source-proof.json and the publication record keep their fields. The module trees and the file count are only checked: the fixed guard source and the producer revision must both hold the two pinned module trees, and the captured inputs must have the pinned count. A guard source that differs from the repository's current guard modules is therefore refused before the build.

A new pin changes those five values and the three workflow literals together, taken from the Git objects of one reviewed commit on the main branch (its tree, the two directory entries of that tree, and the file count of those directories), and nothing else. The workflow job, the helper's child environment and build command, and the pins that consumers hold for earlier evidence are not part of a source pin.
