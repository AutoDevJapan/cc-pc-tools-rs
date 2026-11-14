# Fixed source tree comparison for the guard candidate

The signed source observation contains a sorted inventory of the selected `repair-guard/` and `tools/civ6-ci-contract/` inputs. The candidate must not be issued solely because that observation is internally consistent.

Before the private issuer is created, the production constructor checks the same retained archive through `ghaGuardVerifyArchiveGitSource`. It accepts the fixed repository ID, source commit, root tree and exact 249 selected files. The authenticated GitHub transport comes from the existing normal authority; if that authority or its client is unavailable the operation fails closed.

The reader obtains the fixed commit and checks its root tree, reconstructs and hashes each Git tree traversed to the two selected subtrees, and rejects a truncated response, missing or extra input, non-regular mode, duplicate path, case collision, inconsistent size or object ID. Each selected blob is fetched with bounded reads and compared using Git blob SHA-1, length and SHA-256 to the signed inventory. The operation checks cancellation and limits in-flight blob requests. On any failure no candidate is issued.

This is source evidence. It does not establish the entire compiler, dependency cache, runner image, installed target identity or operating-system authority. Run/job binding, artifact verification and Sigstore verification remain separate required checks. The normal authenticated transport and installed target are not connected by these files.

The optional actual-artifact test uses synthetic source verification; it is not an end-to-end proof of this Git comparison. A dedicated real-source test and hosted CI are needed before deployment.
