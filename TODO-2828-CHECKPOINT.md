# ToDo 2828: Windows runner backend

Scope: implement only tools/windows-ci-runner-2737 and minimal SDK exports on
scheduler/todo-2828-godd-mcp-windows-ci-guest. No VM, source fetch, runner
registration, original PR, separate PR, merge, or Cargo execution.

Startup: guard returned client_resource=false; GitHub API confirmed private,
admin, nonfork AutoDevJapan/cc-pc-tools-rs id 1406818713. Read D:/CC/AGENTS.md,
repository AGENTS.md and docs/test-build-scope.md (2026-10-17).

Dependencies imported at their canonical paths:
- tools/ci-source-2737 and tools/product-ci/normalci from
  c373a18c2aa455051226bc9569f0edfc7b526a3e, preserving source proof and admission.
- tools/windows-ci-sdk-2737 from 9622a766e99a378427eccf9f21967f0fb9ca890f.
  Asset evidence is a receipt, not an instruction to read/restart/copy the VHD.
- Existing newer product-ci files are retained, not rolled back to parent.

Implemented: typed seed/session/backend, fixed guest entry/hooks, bounded
framing, stock runner archive pin, Job Object cancellation/drain, explicit
EOF/cancel/service/deadline causes, SDK callback integration. README/HANDOFF
describe public API and all offline clone staging paths/service/COM/TLS steps.
Parent alone adapts normalci route expression/label; legacy is rejected.

Verified: Windows runner tests/race/vet, sourcepacket tests/vet, SDK tests/vet
including existing command callers and named pipe, normalci type package/vet.
No original source, real VM/SCM/runner, SDK re-preparation or Cargo execution.
Fixed implementation candidate, already committed/pushed and remote matched:
- HEAD bec99388b45319bfed7e3db67f1cbd24422883fd
- tree 6c5c18608b0d7940178baaa97938e6dd0a8fb5cd
- dependency import commit f03eb1f4d8165a9f079a8530957a8901510b8efa
- timeout distinction and its regression were added after initial8858544.
  Final Windows runner race tests passed3.967s, vet passed. Prior successful
  unchanged dependency tests remain valid; no unnecessary full/Cargo run.

Review ToDo2830 was registered through ordinary shared-ToDo --spec with
gpt-6-luna/high, subwork-of2828. On resume job20261019-005520-todo-2828, its three
original responses were received. Initial002939 was unread/preparation failure,
004034 and004939 read all23 fixed bec99388 files using read-only GitHub API and
returned Must0/Suggestion2/Nit0, tests0. Original text lacked the implementer
model; job.json AND exact-thread rollout turn_context.model prove distinct actual
models: implementation gpt-6.1-sol / review gpt-6-luna. Models were not inferred
from requested names or the generic "GPT-6" response. See reviews/receipt.json
and TODO-2828-REVIEW-RECEIPT.md. All3 local SHA256s and committed Git blob bytes
were independently matched. Response files remain byte-identical, including
intentional Markdown hard-break spaces, through scoped .gitattributes.
Both immutable original saved specs were read back:
2830-50e520eda5e9401fbc74831fcb6ac133.md is the updated spec; the older saved
spec2830-3c0e66055e0245a9ba0bd6290fd7e539.md remains an original receipt.
The tracked TODO-2828-REVIEW-SPEC.md preserves the updated request.

Both suggestions were implemented at code HEAD
b60e85e7f66abb3329264bfc74895a3d3c2be10f,
tree e192d0a34d9c546e146575681d3d9365c6185ee0:
1. Diagnostic enumeration bounds all entries2048/depth16 with ReadDir64 batches
   and cancellation, including empty directories. Original1024file/64MiB held.
2. Failed events must match post-listen stage/numeric RunnerID. Timeout/process
   needs confirmed drain/exit/hash; drain-unconfirmed is false and not timeout.
   Invalid failure is rejected BEFORE Observe. Positive results are unchanged.
Additional regressions passed2.618s; module race passed4.280s and vet passed.
Unchanged SDK/sourcepacket/normalci results were retained, not rerun.

Finite follow-up review ToDo2833 (gpt-6-luna/high, --spec, subwork-of2828) covers
only6 new source/test/doc files from base93c0c42 to fixedb60e85e/treee192d0a.
The saved2833 spec was read back. It is queued at handoff; no response received.
This is a new delta, not a duplicate of2830. Reviewer must not wait for parents.

Next on automatic resume after review2833: check its actual model/fixed version/
six read files, receive original response/hash, fix any real Must and run affected
regressions. Then provide final code pin/HANDOFF to parent2737 and finish2828.
Do not create an independent PR/merge or wait for parent2737 completion. The
parent still owns route/normalci run-label integration and all real acceptance.
Checkpoint request/receipt is TODO-2828-scheduler.private.json (ignored; same
owned task/job/branch and actual HEAD, scheduler command exit0).

Owned temporary root: 7f54ee797d015b59599f1686c8ccf8f8 under user's Temp,
created by safe-delete for synthetic Go tests. After all owned test processes
ended, plan returned247bytes/1entry (checkpoint request). Separate normal delete
and finish succeeded with state source_absent. No other temporary data is owned.

Current resume's temporary root:6da4e1425cb1912e591ad6e383648595 under user's
Temp, created with safe-delete for suggestion regression tests. Tests/vet have
ended; plan returned0bytes/0entries. Separate normal delete then finish succeeded
with source_absent. This worktree and committed evidence are retained. No other
worker or SDK data was cleaned.
