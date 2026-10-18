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

Next: implement typed seed/session/backend, fixed guest entry/hooks, process
tree cancellation/drain and synthetic tests. Parent alone adapts normalci route
expression/label to run+role contract; guest must reject the legacy label.
Then Go tests/vet, commit/push, ordinary shared-ToDo different-model review,
fix any Must, checkpoint and provide integration handoff to 2737.

Owned temporary root: 7f54ee797d015b59599f1686c8ccf8f8 under user's Temp,
created by safe-delete for synthetic Go tests. Plan -> separate delete -> finish,
or authorized quarantine on refusal. No other temporary data is owned.
