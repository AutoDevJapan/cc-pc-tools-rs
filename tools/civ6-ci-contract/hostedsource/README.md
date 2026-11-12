# Fixed hosted CI source verification

This package checks a finite continuation of the PR149 merge of the original
routed tip and main `f0e993f8dd486620d9b41471456fd106b12c700b` through the fixed
initial hosted merge `daff9d61edfb9a54f07288bcaf8b2ece6c3e82a5`. It retains the hosted Ubuntu and
Windows jobs while the earlier local runner supply remains unavailable.
The original source, admission, migration, cleanup and budget contracts remain
unchanged. A successful check grants no runner registration or execution rights.

The complete candidate tree must match the fixed main tree except the reviewed
three workflow blobs and the existing executable-inode regression test blob.
Workflow generation retains action versions, commands, OS matrix, permissions,
doctests and failure handling. It adds PR-scoped concurrency, fixed head checkout
and release binary hashes. Only initial PR attempts share the versioned PR
cancellation group; manual reruns use run IDs and cannot cancel that group.
The Linux test uses a dedicated child, writer/holder
negative controls and three unique execution markers; its portable output parser
alone cannot establish Linux execution.

`civ6-ci-route/cmd/check-hosted-main` reads the local Git graph and returns separate
source, remote-freshness, runner-admission and official-CI fields. It never writes
Git refs, starts jobs, issues tokens or reads an execution ledger. Remote head and
main freshness, independent reviews and actual final-head CI remain required.

The fixture under `testdata/cli_contract.rs` is the MIT-licensed regression test
from AutoDevJapan/civ6-banseiki blob
`18aca67c4a635d8739abfa5a2dc92d21835f8488`.
