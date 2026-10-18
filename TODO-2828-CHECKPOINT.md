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
Next: commit/push fixed review candidate; ordinary shared-ToDo review using a
different actual model, resolve Must, checkpoint and deliver fixed pin to2737.
Do not create an independent PR/merge or wait for parent2737 completion.

Owned temporary root: 7f54ee797d015b59599f1686c8ccf8f8 under user's Temp,
created by safe-delete for synthetic Go tests. Plan -> separate delete -> finish,
or authorized quarantine on refusal. No other temporary data is owned.
