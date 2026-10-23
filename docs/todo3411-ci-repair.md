# ToDo3411: PR393 Windows CI finite repair

- Parent: ToDo3392 / job20261023-102927-todo-3392. Base: `90dd0c09f34541183672f6e1afda0f74fd363b05`.
- This worker: `20261023-114022-todo-3411`, branch `scheduler/todo-3411-3392-ci`.
- Scope: synthetic capacity fixture and its parity regressions only. No runtime/queue/state edits, real providers, GPU, deployment, extra review worker or PR.
- Saved CI run39888787786/job120203241992: 2335/2336 passed, capacity test queued at 120024 ms. The old snapshot lacks retry counts, cooldown and pool attribution, so it cannot alone identify the exact historical branch.
- Baseline original test on the fixed source: PASS, 67.140 s. Full stdout/stderr and actual exit 0 in `.ci-work/baseline-capacity.*`.
- Minimal Red: retain the original supervisor/queue/fake CLI chain and delay only the fixture's public log response by 1500 ms. `capacity_metadata_collection_latency_does_not_become_a_technical_cooldown` FAILS in 3.163 s, actual nextest exit 100. It reaches queued/error none/agent0/wait null/admission false/index complete, exactly the observable CI state, plus the newly exposed `capacity_wait_pending=true`, wait count1, pool verified false and a 900-second deadline. Full output in `.ci-work/red-slow-observation.*`.
- Mechanism: the sampler takes `at` before the metadata queries. The fixture generates `timestamp=now()` when answering `observe logs`; with >1 s query latency it appears to be a future failure and is correctly rejected by the production attribution filter. The real attempt already exited. Return its recorded attempt timestamp instead. Preserve the strict future/stale/model/account checks and production retry semantics.
- The deliberate 1500 ms is fault injection into response latency, not an attempt to synchronize successful child startup or enlarge a timeout. The regression stops at terminal status OR the incorrect cooldown, without waiting 900 s.

## Resume

Red recorded. Next: fix only synthetic log event timestamps, rerun the delayed case and original once/always capacity contract, then affected parity/native queue regressions. Keep complete output/command/HEAD/exit under `.ci-work`; record hashes here. Commit and push the finite diff, save normal scheduler checkpoint with this job and actual HEAD, return base/head and remaining formal CI/review/integration to parent3392. Do not mark PR393 CI successful before the parent checks its formal run.
