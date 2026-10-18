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
gpt-6-luna/high, subwork-of2828. It is still queued; no actual review response or
actual-model identity has been received. It must review the UPDATED fixed
bec99388 head/tree, not old8858544. Both immutable saved specs were read back:
2830-50e520eda5e9401fbc74831fcb6ac133.md is the updated spec; the older saved
spec2830-3c0e66055e0245a9ba0bd6290fd7e539.md remains an original receipt.
The tracked TODO-2828-REVIEW-SPEC.md preserves the updated request.

Next on automatic resume after review2830: verify actual model/base/head/tree,
all reviewed files and any unread scope; receive original response with hash;
fix Must and run affected regressions, using the same review ToDo for finite
follow-up where possible. Then deliver final code pin/HANDOFF to parent2737.
Do not create an independent PR/merge or wait for parent2737 completion. The
parent still owns route/normalci run-label integration and all real acceptance.
Checkpoint request/receipt is TODO-2828-scheduler.private.json (ignored; same
owned task/job/branch and actual HEAD, scheduler command exit0).

Owned temporary root: 7f54ee797d015b59599f1686c8ccf8f8 under user's Temp,
created by safe-delete for synthetic Go tests. After all owned test processes
ended, plan returned247bytes/1entry (checkpoint request). Separate normal delete
and finish succeeded with state source_absent. No other temporary data is owned.
